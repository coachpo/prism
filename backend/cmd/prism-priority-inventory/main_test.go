package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/platform/priority"
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
		"## Jobs",
		"## Resource Families",
		"classified",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, report)
		}
	}
}

func TestRunFailOnUnclassifiedRejectsInjectedInventory(t *testing.T) {
	t.Parallel()

	inventory := priority.Inventory{
		Routes:    []priority.RouteInventoryEntry{{Name: "missing route", Method: "GET", Path: "/api/missing", Source: "negative fixture", Classified: false}},
		Resources: []priority.ResourceInventoryEntry{{Family: "DB", Name: "missing resource", Location: "negative fixture", Classified: false}},
		Jobs:      []priority.JobInventoryEntry{{Name: "missing job", Source: "negative fixture", Classified: false}},
	}
	var output bytes.Buffer
	err := runWithInventory([]string{"--format=markdown", "--fail-on-unclassified", "./..."}, &output, inventory)
	if !errors.Is(err, priority.ErrInventoryValidation) {
		t.Fatalf("expected inventory validation error, got %v", err)
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
