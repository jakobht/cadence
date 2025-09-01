package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <version>")
		os.Exit(1)
	}
	version := os.Args[1]

	var submodules []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Base(path) == "go.mod" {
			dir := filepath.Dir(path)
			// Exclude vendor directories and the idls submodule
			if strings.Contains(dir, "vendor") || strings.Contains(dir, "idls") {
				return filepath.SkipDir
			}
			if dir != "." {
				submodules = append(submodules, dir)
			}
		}
		return nil
	})

	if err != nil {
		fmt.Printf("Error finding submodules: %v\n", err)
		os.Exit(1)
	}

	for _, submodule := range submodules {
		// Clean up path for tags
		tagPath := strings.TrimPrefix(submodule, "./")
		tag := fmt.Sprintf("%s/%s", tagPath, version)
		fmt.Printf("git tag %s\n", tag)
		fmt.Printf("git push origin %s\n", tag)
	}
}
