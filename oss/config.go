package main

import (
	"os"
	"strings"

	"github.com/gopsql/goconf"
)

type (
	config struct {
		OSSAccessKeyId     string
		OSSAccessKeySecret string
		OSSPrefix          string
		OSSBucket          string
	}
)

func readConfig(path string, config *config) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return goconf.Unmarshal(content, config)
}

func writeConfig(path string, config *config) error {
	content, err := goconf.Marshal(config)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(path, ".go") {
		content = append([]byte("// vi: set filetype=go :\n"), content...)
	}
	return os.WriteFile(path, content, 0644)
}
