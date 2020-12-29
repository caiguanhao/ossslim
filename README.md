# ossslim

Slim Aliyun OSS client.

You can create (upload), get (download), delete, list files on OSS.

Usage:

```golang
package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/caiguanhao/ossslim"
)

func main() {
	client := ossslim.Client{
		AccessKeyId:     "xxxxxxxxxxxxxxxx",
		AccessKeySecret: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		Prefix:          "https://abc.oss-cn-hongkong.aliyuncs.com",
		Bucket:          "abc",
	}

	buf := make([]byte, 1024)
	_, err := rand.Read(buf)
	if err != nil {
		panic(err)
	}

	file := time.Now().UTC().Format("tmp/20060102150405")
	req, err := client.Upload(file, bytes.NewReader(buf), nil, "")
	if err != nil {
		panic(err)
	}
	fmt.Println(req.URL())
	// https://abc.oss-cn-hongkong.aliyuncs.com/tmp/20201229130519

	result, err := client.List("tmp/", false)
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Files)
	// [{tmp/20201229130519 2020-12-29T13:05:20.000Z "13BB571C15022FC9211A85AF85272B85" 1024}]

	var buffer bytes.Buffer
	req, err = client.Download(file, &buffer)
	if err != nil {
		panic(err)
	}
	fmt.Println(bytes.Equal(buf, buffer.Bytes()))
	// true

	err = client.Delete(file)
	if err != nil {
		panic(err)
	}

	result, err = client.List("tmp/", false)
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Files)
	// []
}
```
