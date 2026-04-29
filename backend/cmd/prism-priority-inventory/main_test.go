package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunMarkdownFailOnUnclassifiedSucceedsForSeededInventory(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	err := run([]string{"--format=markdown", "--fail-on-unclassified", "./..."}, &output)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	report := output.String()
	for _, want := range []string{
		"# Prism Priority Inventory Phase 0",
		"## HTTP Routes",
		"## Resource Families",
		"classified",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, report)
		}
	}
}

func TestRunRejectsUnsupportedFormat(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	err := run([]string{"--format=json", "./..."}, &output)
	if err == nil {
		t.Fatal("expected unsupported format to return an error")
	}
	if !strings.Contains(err.Error(), "only markdown is supported") {
		t.Fatalf("expected unsupported format error, got %v", err)
	}
}

func TestRunRequiresPackagePattern(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	err := run([]string{"--format=markdown"}, &output)
	if err == nil {
		t.Fatal("expected missing package pattern to return an error")
	}
}
