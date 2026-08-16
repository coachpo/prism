package provider_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdaptersDoNotOwnRouteReasonOrAccountingWrites(t *testing.T) {
	root := "."
	forbidden := []string{
		"route" + "_" + "reason",
		"request" + "_" + "logs",
		"audit" + "_" + "logs",
		"usage" + "_" + "request" + "_" + "events",
		"telemetry" + "_" + "outbox",
		"pricing",
		"AccountingSink",
		"Reserve(",
		"Bridge" + "Adapter",
		"Current" + "Behavior" + "Bridge",
		"Err" + "Behavior" + "Not" + "Migrated",
		"Behavior" + "Not" + "Migrated",
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, term := range forbidden {
			if strings.Contains(string(content), term) {
				t.Fatalf("provider adapter source %s contains forbidden ownership marker %q", path, term)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk provider adapter source: %v", err)
	}
}
