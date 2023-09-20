package ossslim

import (
	"bytes"
	"crypto/md5"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"testing"
	"time"
)

func newClientFromEnv(t *testing.T) *Client {
	accessKeyId := os.Getenv("OSS_ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("OSS_ACCESS_KEY_SECRET")
	prefix := os.Getenv("OSS_PREFIX")
	bucket := os.Getenv("OSS_BUCKET")

	if accessKeyId == "" {
		t.Fatal("please provide env: OSS_ACCESS_KEY_ID")
	}
	if accessKeySecret == "" {
		t.Fatal("please provide env: OSS_ACCESS_KEY_SECRET")
	}
	if prefix == "" {
		t.Fatal("please provide env: OSS_PREFIX")
	}
	if bucket == "" {
		t.Fatal("please provide env: OSS_BUCKET")
	}

	return &Client{
		AccessKeyId:     accessKeyId,
		AccessKeySecret: accessKeySecret,
		Prefix:          prefix,
		Bucket:          bucket,
	}
}

func TestRequest(t *testing.T) {
	client := newClientFromEnv(t)

	exists, _, err := client.Exists("not-exists")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("exists == true")
	} else {
		t.Log("exists != true passed")
	}

	dir := time.Now().UTC().Format("tmp20060102150405/")
	files := []string{"request.go", "request_test.go"}
	sums := map[string][]byte{}
	for _, file := range files {
		f, err := ioutil.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		path := dir + file
		sums[path] = md5sum(f)
		req, err := client.Upload(path, bytes.NewReader(f), sums[file], "")
		if err != nil {
			t.Fatal(err)
		}
		t.Log("uploaded to", req.URL())
		exists, _, err := client.Exists(path)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Log("exists == true passed")
		} else {
			t.Fatal("exists != true")
		}
		_, _, err = client.ImageInfo(path)
		if err == nil || err.Error() != "This image format is not supported." {
			t.Fatal("should have error")
		} else {
			t.Log("image info error test passed")
		}

	}
	result, err := client.List(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, file := range result.Files {
		if _, ok := sums[file.Name]; !ok {
			continue
		}
		var buf bytes.Buffer
		req, err := client.Download(file.Name, &buf)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("retrieved", req.URL())
		if bytes.Equal(sums[file.Name], md5sum(buf.Bytes())) {
			t.Log("md5 sums of upload and download file are equal")
		} else {
			t.Fatal("md5 sums of upload and download file are not equal")
		}
		keys = append(keys, file.Name)
	}
	err = client.Delete(keys...)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("removed", keys)
}

func TestUploadBadMd5(t *testing.T) {
	client := newClientFromEnv(t)
	_, err := client.Upload("any", bytes.NewReader([]byte{0}), md5sum([]byte{1}), "")
	if err == nil || err.Error() != "The Content-MD5 you specified was invalid." {
		t.Fatal("server didn't respond correct error")
	} else {
		t.Log("server responded correct error")
	}
}

func TestPostForm(t *testing.T) {
	file, err := ioutil.ReadFile("request_test.go")
	if err != nil {
		panic(err)
	}
	client := newClientFromEnv(t)
	key := time.Now().UTC().Format("/tmp20060102150405/foo")
	const MB = 1 << 20
	params := client.PostForm(key, 1*MB, 1*time.Minute, map[string]string{"x-oss-object-acl": "public-read"})
	params["x-oss-object-acl"] = "public-read"
	postFile(t, client, key, params, file)
}

func postFile(t *testing.T, client *Client, key string, params map[string]string, content []byte) {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	for key, value := range params {
		if err := writer.WriteField(key, value); err != nil {
			panic(err)
		}
	}
	part, err := writer.CreateFormFile("file", "foo")
	if err != nil {
		panic(err)
	}
	if _, err := part.Write(content); err != nil {
		panic(err)
	}
	if err = writer.Close(); err != nil {
		panic(err)
	}
	req, err := http.NewRequest("POST", client.Prefix, body)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()
	respBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}
	if res.StatusCode == 204 {
		t.Log("Successfully uploaded", key)
	} else {
		t.Log("Response body:", string(respBody))
		t.Errorf("Incorrect status code returned: %d", res.StatusCode)
	}
	err = client.Delete(key)
	if err == nil {
		t.Log("Removed", key)
	} else {
		t.Fatal(err)
	}
}

func md5sum(content []byte) []byte {
	md5sum := md5.New()
	md5sum.Write(content)
	return md5sum.Sum(nil)
}

var pngData = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x0c, 0x00, 0x00, 0x00, 0x22,
	0x01, 0x03, 0x00, 0x00, 0x00, 0x51, 0x1d, 0xaa, 0xe2, 0x00, 0x00, 0x00,
	0x03, 0x50, 0x4c, 0x54, 0x45, 0x47, 0x70, 0x4c, 0x82, 0xfa, 0xd2, 0xd2,
	0x00, 0x00, 0x00, 0x01, 0x74, 0x52, 0x4e, 0x53, 0x00, 0x40, 0xe6, 0xd8,
	0x66, 0x00, 0x00, 0x00, 0x0b, 0x49, 0x44, 0x41, 0x54, 0x78, 0x01, 0x63,
	0xa0, 0x0b, 0x00, 0x00, 0x00, 0x66, 0x00, 0x01, 0x31, 0xf3, 0x6f, 0xd9,
	0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

var pngMd5 = []byte{
	0x52, 0x7b, 0xe3, 0xf1, 0xbc, 0xb8, 0xd8, 0xe3, 0xeb, 0x88, 0xc9, 0x42,
	0x02, 0x79, 0x23, 0x63,
}

func TestPNG(t *testing.T) {
	client := newClientFromEnv(t)
	path := time.Now().UTC().Format("tmp20060102150405.png")
	req, err := client.Upload(path, bytes.NewReader(pngData), pngMd5, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("uploaded to", req.URL())
	exists, _, err := client.Exists(path)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Log("exists == true passed")
	} else {
		t.Fatal("exists != true")
	}
	info, _, err := client.ImageInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size == 96 {
		t.Log("image info size test passed")
	} else {
		t.Fatal("wrong size")
	}
	if info.Width == 12 {
		t.Log("image info width test passed")
	} else {
		t.Fatal("wrong width")
	}
	if info.Height == 34 {
		t.Log("image info width test passed")
	} else {
		t.Fatal("wrong height")
	}
	err = client.Delete(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("removed", path)
}
