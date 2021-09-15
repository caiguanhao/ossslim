package ossslim

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type (
	// An OSS client must have prefix, bucket, access key ID and access key secret.
	// Prefix should be a string like this: https://<your-bucket>.<region>.aliyuncs.com.
	Client struct {
		AccessKeyId     string
		AccessKeySecret string
		Prefix          string
		Bucket          string
	}

	Request struct {
		ResponseContentLength *int64

		client *Client
		ctx    context.Context

		remote      string
		queryString string
		contentType string
		method      string
		date        string
		contentMd5  string

		reqBody  io.Reader
		respBody io.Writer

		async bool
	}

	Directory struct {
		Name string `xml:"Prefix"`
	}

	File struct {
		Name         string `xml:"Key"`
		LastModified string
		ETag         string
		Size         int64
	}

	ListResult struct {
		Prefix string
		Files  []File
		Dirs   []Directory
	}

	responseError struct {
		XMLName        xml.Name `xml:"Error"`
		Code           string   `xml:"Code"`
		Message        string   `xml:"Message"`
		RequestId      string   `xml:"RequestId"`
		HostId         string   `xml:"HostId"`
		OSSAccessKeyId string   `xml:"OSSAccessKeyId"`
	}

	fileList struct {
		Name        string
		Prefix      string
		Marker      string
		MaxKeys     int
		Delimiter   string
		IsTruncated bool
		NextMarker  string
		Files       []File      `xml:"Contents"`
		Directories []Directory `xml:"CommonPrefixes"`
	}

	keyOnly struct {
		Key string `xml:"Key"`
	}

	deleteReq struct {
		XMLName xml.Name  `xml:"Delete"`
		Quiet   bool      `xml:"Quiet"`
		Files   []keyOnly `xml:"Object"`
	}
)

// Upload wraps UploadWithContext using context.Background.
func (c *Client) Upload(remote string, reqBody io.Reader, reqBodyMd5 []byte, contentType string) (*Request, error) {
	return c.UploadWithContext(context.Background(), remote, reqBody, reqBodyMd5, contentType)
}

// Upload creates and executes a upload request for reqBody (io.Reader) to
// remote path, returns the request and error. reqBodyMd5 can be nil, OSS will
// run MD5 check if it is provided.  If contentType is empty,
// "application/octet-stream" will be used. If the body is bytes, use
// bytes.NewReader. If it is a string, use strings.NewReader.
func (c *Client) UploadWithContext(ctx context.Context, remote string, reqBody io.Reader, reqBodyMd5 []byte, contentType string) (*Request, error) {
	req := &Request{
		client:      c,
		ctx:         ctx,
		remote:      remote,
		reqBody:     reqBody,
		contentType: contentType,
		contentMd5:  base64.StdEncoding.EncodeToString(reqBodyMd5),
		method:      "PUT",
	}
	err := req.do()
	return req, err
}

// Download wraps DownloadWithContext using context.Background.
func (c *Client) Download(remote string, respBody io.Writer) (*Request, error) {
	return c.download(context.Background(), remote, respBody, false)
}

// Download creates and executes a download request from remote path to
// respBody (io.Writer), returns the request and error. You can use
// bytes.Buffer to download the file to memory. If you want to have more than
// one destination, use io.MultiWriter.
func (c *Client) DownloadWithContext(ctx context.Context, remote string, respBody io.Writer) (*Request, error) {
	return c.download(ctx, remote, respBody, false)
}

// DownloadAsync wraps DownloadAsyncWithContext using context.Background.
func (c *Client) DownloadAsync(remote string, respBody io.Writer) (*Request, error) {
	return c.download(context.Background(), remote, respBody, true)
}

// DownloadAsync is like Download but won't wait till download is complete.
func (c *Client) DownloadAsyncWithContext(ctx context.Context, remote string, respBody io.Writer) (*Request, error) {
	return c.download(ctx, remote, respBody, true)
}

// Delete wraps DeleteWithContext using context.Background.
func (c *Client) Delete(remotes ...string) error {
	return c.DeleteWithContext(context.Background(), remotes...)
}

// Delete creates and executes a delete request for multiple remote keys
// (paths) at the same time.
func (c *Client) DeleteWithContext(ctx context.Context, remotes ...string) error {
	var reqBody bytes.Buffer
	reqBody.WriteString(xml.Header)
	files := []keyOnly{}
	for _, remote := range remotes {
		files = append(files, keyOnly{
			Key: remote,
		})
	}
	if err := xml.NewEncoder(&reqBody).Encode(deleteReq{
		Quiet: true,
		Files: files,
	}); err != nil {
		return err
	}
	md5sum := md5.New()
	md5sum.Write(reqBody.Bytes())
	req := &Request{
		client:     c,
		ctx:        ctx,
		remote:     "/?delete",
		reqBody:    &reqBody,
		contentMd5: base64.StdEncoding.EncodeToString(md5sum.Sum(nil)),
		method:     "POST",
	}
	err := req.do()
	return err
}

// List wraps ListWithContext using context.Background.
func (c *Client) List(prefix string, recursive bool) (ListResult, error) {
	return c.ListWithContext(context.Background(), prefix, recursive)
}

// List creates and executes a list request for remote files and directories
// under prefix, recursively if recursive is set to true.
func (c *Client) ListWithContext(ctx context.Context, prefix string, recursive bool) (result ListResult, err error) {
	req := &Request{
		client: c,
		ctx:    ctx,
	}
	err = req.list(prefix, "", &result, recursive)
	return
}

// URL generates URL without query string for remote file.
func (c *Client) URL(remote string) string {
	if !strings.HasPrefix(remote, "/") {
		remote = "/" + remote
	}
	return strings.TrimSuffix(c.Prefix, "/") + remote
}

func (c *Client) download(ctx context.Context, remote string, respBody io.Writer, async bool) (*Request, error) {
	req := &Request{
		client:   c,
		ctx:      ctx,
		remote:   remote,
		method:   "GET",
		respBody: respBody,
		async:    async,
	}
	err := req.do()
	return req, err
}

func (req *Request) String() string {
	return req.URL()
}

func (req *Request) URL() string {
	return strings.TrimSuffix(req.client.Prefix, "/") + req.getRemote() + req.queryString
}

func (req *Request) list(prefix string, marker string, result *ListResult, recursive bool) (err error) {
	req.remote = "/"
	prefix = strings.Trim(prefix, "/") + "/"
	if prefix == "/" {
		prefix = ""
	}
	req.queryString = "?max-keys=1000&prefix=" + url.QueryEscape(prefix) + "&marker=" + url.QueryEscape(marker)
	if !recursive {
		req.queryString += "&delimiter=/"
	}
	req.method = "GET"
	var response bytes.Buffer
	req.respBody = &response
	err = req.do()
	var list fileList
	if err := xml.NewDecoder(&response).Decode(&list); err != nil {
		return err
	}
	result.Files = append(result.Files, list.Files...)
	result.Dirs = append(result.Dirs, list.Directories...)
	result.Prefix = list.Prefix
	if list.IsTruncated {
		err = req.list(prefix, list.NextMarker, result, recursive)
	}
	return
}

func (req *Request) do() (err error) {
	var httpReq *http.Request
	httpReq, err = http.NewRequestWithContext(req.ctx, req.method, req.URL(), req.reqBody)
	if err != nil {
		return
	}
	if req.contentType == "" {
		req.contentType = "application/octet-stream"
	}
	httpReq.Header.Set("Content-Type", req.contentType)
	req.date = time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT") // don't use time.RFC1123
	httpReq.Header.Set("Date", req.date)
	if req.contentMd5 != "" {
		httpReq.Header.Set("Content-MD5", req.contentMd5)
	}
	httpReq.Header.Set("Authorization", fmt.Sprintf("OSS %s:%s", req.client.AccessKeyId, req.signature()))
	client := &http.Client{}
	var resp *http.Response
	resp, err = client.Do(httpReq)
	if err != nil {
		return
	}
	cl := resp.ContentLength
	req.ResponseContentLength = &cl
	if resp.StatusCode == 200 {
		if req.respBody == nil {
			resp.Body.Close()
			return
		}
		if req.async {
			go func() {
				defer resp.Body.Close()
				io.Copy(req.respBody, resp.Body)
			}()
			return
		}
		defer resp.Body.Close()
		io.Copy(req.respBody, resp.Body)
		return
	}
	defer resp.Body.Close()
	var body []byte
	body, err = ioutil.ReadAll(resp.Body)
	if err == nil {
		errResp := responseError{}
		err = xml.Unmarshal(body, &errResp)
		if err == nil && len(errResp.Message) > 0 {
			err = errors.New(errResp.Message)
		} else {
			err = errors.New(strings.TrimSpace(string(body)))
		}
	}
	return
}

func (req *Request) getRemote() string {
	if !strings.HasPrefix(req.remote, "/") {
		return "/" + req.remote
	}
	return req.remote
}

func (req *Request) signature() string {
	msg := strings.Join([]string{
		req.method,
		req.contentMd5,
		req.contentType,
		req.date,
		fmt.Sprintf("/%s%s", req.client.Bucket, req.getRemote()),
	}, "\n")
	mac := hmac.New(sha1.New, []byte(req.client.AccessKeySecret))
	mac.Write([]byte(msg))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
