package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoForbiddenExecution(t *testing.T) {
	forbiddenWords := []string{
		"os/exec",
		"exec.Command",
		"bash -c",
		"sh -c",
		"docker exec",
		"ContainerExecCreate",
		"ContainerExecStart",
		"ContainerCreate",
		"ContainerRemove",
		"ContainerRename",
		"ContainerUpdate",
		"ImageBuild",
		"ImagePull",
		"ImagePush",
		"VolumeCreate",
		"VolumeRemove",
		"NetworkCreate",
		"NetworkRemove",
	}

	err := filepath.Walk("./", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(b)

		for _, word := range forbiddenWords {
			if strings.Contains(content, word) && !strings.Contains(path, "test.go") {
				t.Errorf("Forbidden execution mechanism '%s' found in %s", word, path)
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to walk directory: %v", err)
	}
}
