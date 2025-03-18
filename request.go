package ossslim

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
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
		Response              *http.Response
		ResponseContentLength *int64

		client *Client
		ctx    context.Context

		remote      string
		canonRes    string
		queries     url.Values
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

	ImageInfo struct {
		Size   int64
		Format string
		Width  int
		Height int
	}

	imageInfo struct {
		Size struct {
			Value string `json:"value"`
		} `json:"FileSize"`
		Format struct {
			Value string `json:"value"`
		} `json:"Format"`
		Width struct {
			Value string `json:"value"`
		} `json:"ImageWidth"`
		Height struct {
			Value string `json:"value"`
		} `json:"ImageHeight"`
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

func (c *Client) Exists(remote string) (bool, *Request, error) {
	return c.ExistsWithContext(context.Background(), remote)
}

func (c *Client) ExistsWithContext(ctx context.Context, remote string) (exists bool, req *Request, err error) {
	req = &Request{
		client: c,
		ctx:    ctx,
		remote: remote,
		method: "HEAD",
	}
	err = req.do()
	if req.Response != nil {
		exists = req.Response.StatusCode == 200
	}
	return
}

func (c *Client) ImageInfo(remote string) (*ImageInfo, *Request, error) {
	return c.ImageInfoWithContext(context.Background(), remote)
}

func (c *Client) ImageInfoWithContext(ctx context.Context, remote string) (info *ImageInfo, req *Request, err error) {
	var response bytes.Buffer
	req = &Request{
		client:   c,
		ctx:      ctx,
		remote:   remote,
		method:   "GET",
		respBody: &response,
		queries:  url.Values{},
	}
	req.queries.Set("x-oss-process", "image/info")
	err = req.do()
	if err == nil && req.Response != nil {
		var imgInfo imageInfo
		err = json.NewDecoder(&response).Decode(&imgInfo)
		if err != nil {
			return
		}
		size, _ := strconv.ParseInt(imgInfo.Size.Value, 10, 64)
		width, _ := strconv.Atoi(imgInfo.Width.Value)
		height, _ := strconv.Atoi(imgInfo.Height.Value)
		info = &ImageInfo{
			Size:   size,
			Format: imgInfo.Format.Value,
			Width:  width,
			Height: height,
		}
	}
	return
}

// PostForm generates field names and values ("token") for multipart form. This
// is generally used when frontend user asks backend server for a token to
// upload a file to OSS. The "key" is the path to remote file. If "maxSize" is
// greater than 0, file larger than the "maxSize" bytes limit will not be
// uploaded. The token will be expired after "duration" time, default is 10
// minutes. You can provide "extraConditions" like below to add limits to the
// uploaded file:
//  client.PostForm(key, 0, 0,
//  	[]string{"starts-with", "$content-type", "application/"},
//  	map[string]string{"x-oss-object-acl": "public-read"},
//  )
// For more info, visit https://help.aliyun.com/document_detail/31988.html#title-5go-s2f-dnw
func (c *Client) PostForm(key string, maxSize int64, duration time.Duration, extraConditions ...interface{}) map[string]string {
	key = strings.TrimPrefix(key, "/")
	conditions := []interface{}{
		map[string]string{"bucket": c.Bucket},
		map[string]string{"key": key},
	}
	if maxSize > 0 {
		conditions = append(conditions, []interface{}{"content-length-range", 0, maxSize})
	}
	if duration <= 0 {
		duration = 10 * time.Minute
	}
	for _, cond := range extraConditions {
		conditions = append(conditions, cond)
	}
	policyJson, _ := json.Marshal(struct {
		Expiration time.Time   `json:"expiration"`
		Conditions interface{} `json:"conditions"`
	}{
		time.Now().UTC().Round(time.Second).Add(duration),
		conditions,
	})
	policy := base64.StdEncoding.EncodeToString(policyJson)
	mac := hmac.New(sha1.New, []byte(c.AccessKeySecret))
	mac.Write([]byte(policy))
	return map[string]string{
		"key":            key,
		"policy":         policy,
		"OSSAccessKeyId": c.AccessKeyId,
		"signature":      base64.StdEncoding.EncodeToString(mac.Sum(nil)),
	}
}

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
// (paths) at the same time. If you have more than 1000 keys to delete, this
// function will split them into groups of 1000 and delete them one by one.
func (c *Client) DeleteWithContext(ctx context.Context, remotes ...string) error {
	size := len(remotes)
	if size == 0 {
		return nil
	}
	var reqBody bytes.Buffer
	reqBody.WriteString(xml.Header)
	files := []keyOnly{}
	var current, rest []string
	if size > 1000 {
		current, rest = remotes[:1000], remotes[1000:]
	} else {
		current = remotes
	}
	for _, remote := range current {
		files = append(files, keyOnly{
			Key: strings.TrimPrefix(remote, "/"),
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
	if err == nil {
		log.Println("Deleted", len(current), "files")
	}
	if err == nil && len(rest) > 0 {
		err = c.DeleteWithContext(ctx, rest...)
	}
	return err
}

// DeleteRecursiveWithContext deletes all files under the specified prefix except those
// that match any of the exception functions. Each exception function should return true
// for files that should be preserved. Returns lists of deleted and undeleted files, and
// any error encountered. If deletion fails, all files are marked as undeleted.
func (c *Client) DeleteRecursiveWithContext(ctx context.Context, prefix string, exceptions ...func(string) bool) (deleted, undeleted []string, err error) {
	var list ListResult
	list, err = c.ListWithContext(ctx, prefix, true)
	if err != nil {
		return
	}
outer:
	for i := range list.Files {
		for _, except := range exceptions {
			if except(list.Files[i].Name) {
				undeleted = append(undeleted, list.Files[i].Name)
				continue outer
			}
		}
		deleted = append(deleted, list.Files[i].Name)
	}
	err = c.DeleteWithContext(ctx, deleted...)
	if err != nil {
		undeleted = append(undeleted, deleted...)
		deleted = nil
	}
	return
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
	url := strings.TrimSuffix(req.client.Prefix, "/") + req.getRemote()
	qs := req.queries.Encode()
	if qs == "" {
		return url
	}
	return url + "?" + qs
}

func (req *Request) list(prefix string, marker string, result *ListResult, recursive bool) (err error) {
	req.remote = "/"
	req.canonRes = "/"
	prefix = strings.Trim(prefix, "/") + "/"
	if prefix == "/" {
		prefix = ""
	}
	req.queries = url.Values{}
	req.queries.Set("max-keys", "1000")
	req.queries.Set("prefix", prefix)
	req.queries.Set("marker", marker)
	if !recursive {
		req.queries.Set("delimiter", "/")
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
	req.Response = resp
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
	if resp.StatusCode == 404 && req.method == "HEAD" {
		return
	}
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

func (req *Request) queryString() string {
	if len(req.queries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("?")
	for k := range req.queries {
		for _, v := range req.queries[k] {
			if b.Len() > 1 {
				b.WriteByte('&')
			}
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(v)
		}
	}
	return b.String()
}

func (req *Request) getRemote() string {
	if !strings.HasPrefix(req.remote, "/") {
		return "/" + req.remote
	}
	return req.remote
}

func (req *Request) canonicalizedResource() string {
	if req.canonRes != "" {
		return "/" + req.client.Bucket + req.canonRes
	}
	return "/" + req.client.Bucket + req.getRemote() + req.queryString()
}

func (req *Request) signature() string {
	msg := strings.Join([]string{
		req.method,
		req.contentMd5,
		req.contentType,
		req.date,
		req.canonicalizedResource(),
	}, "\n")
	mac := hmac.New(sha1.New, []byte(req.client.AccessKeySecret))
	mac.Write([]byte(msg))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
