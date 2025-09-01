package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const mainModule = "github.com/uber/cadence"

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <version>")
		os.Exit(1)
	}
	version := os.Args[1]

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Path}}\t{{.Dir}}", "all")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		fmt.Printf("Error running 'go list': %v\n", err)
		os.Exit(1)
	}

	var tags []string
	root, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error getting current directory: %v\n", err)
		os.Exit(1)
	}

	lines := strings.Split(out.String(), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 2 {
			continue
		}
		modulePath := parts[0]
		moduleDir := parts[1]

		if strings.HasPrefix(modulePath, mainModule+"/") {
			relPath, err := filepath.Rel(root, moduleDir)
			if err != nil {
				fmt.Printf("Error getting relative path for %s: %v\n", moduleDir, err)
				continue
			}
			tag := fmt.Sprintf("%s/%s", relPath, version)
			tags = append(tags, tag)
		}
	}

	if len(tags) > 0 {
		fmt.Printf("git tag %s\n", strings.Join(tags, " "))
		fmt.Printf("git push origin %s\n", strings.Join(tags, " "))
	}
}
