package ossslim

import (
	"bytes"
	"crypto/md5"
	"io/ioutil"
	"os"
	"testing"
	"time"
)

func NewClientFromEnv(t *testing.T) *Client {
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
	client := NewClientFromEnv(t)

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
	client := NewClientFromEnv(t)
	_, err := client.Upload("any", bytes.NewReader([]byte{0}), md5sum([]byte{1}), "")
	if err == nil || err.Error() != "The Content-MD5 you specified was invalid." {
		t.Fatal("server didn't respond correct error")
	} else {
		t.Log("server responded correct error")
	}
}

func md5sum(content []byte) []byte {
	md5sum := md5.New()
	md5sum.Write(content)
	return md5sum.Sum(nil)
}
