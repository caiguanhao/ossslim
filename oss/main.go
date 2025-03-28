package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/caiguanhao/ossslim"
)

var (
	client ossslim.Client
	dryrun bool
	nomd5  bool
)

func main() {
	var createConfig bool
	var configFile string
	var extsIgnore list
	var recursiveDelete bool
	var except list

	flag.BoolVar(&createConfig, "C", false, "create config file and exit")
	flag.StringVar(&configFile, "c", "oss.config", "config file location")
	flag.BoolVar(&dryrun, "n", false, "show only URLs, don't upload")
	flag.BoolVar(&nomd5, "nomd5", false, "do not compute md5")
	flag.Var(&extsIgnore, "noext", "file extensions to ignore (for example -noext html)")
	flag.BoolVar(&recursiveDelete, "recursive-delete", false, "delete all files with prefix and exit")
	flag.Var(&except, "except", "except files with prefix when delete")
	flag.Parse()

	if createConfig {
		if err := writeConfig(configFile, &config{
			OSSAccessKeyId:     "LTAIxxxxxxxxxxxxxxxxxxxx",
			OSSAccessKeySecret: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			OSSPrefix:          "https://xxxxxxxx.oss-cn-hangzhou.aliyuncs.com",
			OSSBucket:          "xxxxxxxx",
		}); err != nil {
			log.Fatalln(err)
		}
		log.Println("created config file:", configFile)
		return
	}

	var currentConfig config
	if err := readConfig(configFile, &currentConfig); err != nil {
		log.Fatalln(err)
	}

	args := flag.Args()
	if len(args) != 1 {
		log.Fatalln("must provide only one directory")
	}
	root := args[0]

	client = ossslim.Client{
		AccessKeyId:     currentConfig.OSSAccessKeyId,
		AccessKeySecret: currentConfig.OSSAccessKeySecret,
		Prefix:          currentConfig.OSSPrefix,
		Bucket:          currentConfig.OSSBucket,
	}

	if recursiveDelete {
		_, undeleted, err := client.DeleteRecursiveWithContext(context.Background(), root, func(path string) bool {
			return except.HasPrefix(path)
		})
		if err != nil {
			log.Fatalln(err)
		}
		for _, f := range undeleted {
			log.Println("Not deleted:", f)
		}
		return
	}

	jobs := make(chan string)
	go func() {
		defer close(jobs)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			ext := strings.TrimPrefix(filepath.Ext(path), ".")
			if extsIgnore.Has(ext) {
				return nil
			}
			name, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			jobs <- name
			return nil
		})
		if err != nil {
			log.Fatalln(err)
		}
	}()

	concurrency := runtime.NumCPU()
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			for path := range jobs {
				upload(root, path)
			}
		}()
	}
	wg.Wait()
}

func upload(root, path string) {
	contentType := contentTypeForExtension(filepath.Ext(path))
	if dryrun {
		fmt.Printf("%s (%s)\n", client.URL(path), contentType)
		return
	}
	var buffer bytes.Buffer
	file, err := os.Open(filepath.Join(root, path))
	if err != nil {
		log.Fatalln(err)
		return
	}
	defer file.Close()
	if nomd5 == false {
		md5sum := md5.New()
		n, err := io.Copy(io.MultiWriter(&buffer, md5sum), file)
		if err != nil {
			log.Fatalln(err)
			return
		}
		req, err := client.Upload(path, &buffer, md5sum.Sum(nil), contentType)
		if err != nil {
			log.Fatalln("failed to upload to", req.URL(), err)
			return
		}
		log.Printf("uploaded to %s (%d bytes)\n", req.URL(), n)
	} else {
		req, err := client.Upload(path, file, nil, contentType)
		if err != nil {
			log.Fatalln("failed to upload to", req.URL(), err)
			return
		}
		log.Printf("uploaded to %s\n", req.URL())
	}
}

type list []string

func (s list) String() string {
	return strings.Join(s, ", ")
}

func (s *list) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s list) Has(str string) bool {
	for _, element := range s {
		if element == str {
			return true
		}
	}
	return false
}

func (s list) HasPrefix(str string) bool {
	for _, element := range s {
		if strings.HasPrefix(str, strings.TrimPrefix(element, "/")) {
			return true
		}
	}
	return false
}
