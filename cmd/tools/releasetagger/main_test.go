package main

import (
	"strings"
	"testing"
)

func TestGenerateTags(t *testing.T) {
	version := "v1.2.3"
	goListOutput := `
github.com/uber/cadence	/path/to/cadence
github.com/uber/cadence/cmd/server	/path/to/cadence/cmd/server
github.com/uber/cadence/common/archiver/gcloud	/path/to/cadence/common/archiver/gcloud
github.com/some/other/module	/path/to/other/module
`
	root := "/path/to/cadence"

	expectedTags := "cmd/server/v1.2.3 common/archiver/gcloud/v1.2.3"
	expectedOutput := "git tag " + expectedTags + "\ngit push origin " + expectedTags + "\n"

	output, err := generateTags(version, goListOutput, root)
	if err != nil {
		t.Fatalf("generateTags failed: %v", err)
	}

	if strings.TrimSpace(output) != strings.TrimSpace(expectedOutput) {
		t.Errorf("Expected output:\n%s\nGot:\n%s", expectedOutput, output)
	}
}

func TestGenerateTags_NoSubmodules(t *testing.T) {
	version := "v1.2.3"
	goListOutput := `
github.com/uber/cadence	/path/to/cadence
github.com/some/other/module	/path/to/other/module
`
	root := "/path/to/cadence"

	expectedOutput := ""

	output, err := generateTags(version, goListOutput, root)
	if err != nil {
		t.Fatalf("generateTags failed: %v", err)
	}

	if strings.TrimSpace(output) != strings.TrimSpace(expectedOutput) {
		t.Errorf("Expected output:\n%s\nGot:\n%s", expectedOutput, output)
	}
}
