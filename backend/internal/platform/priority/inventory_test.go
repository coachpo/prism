package priority

import (
	"bytes"
	"strings"
	"testing"
)

func TestDefaultInventoryHasNoUnclassifiedEntries(t *testing.T) {
	t.Parallel()

	if count := DefaultInventory().UnclassifiedCount(); count != 0 {
		t.Fatalf("DefaultInventory().UnclassifiedCount() = %d, want 0", count)
	}
}

func TestWriteMarkdownInventoryIncludesRouteAndResourceSeeds(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := WriteMarkdownInventory(&output, DefaultInventory()); err != nil {
		t.Fatalf("WriteMarkdownInventory returned error: %v", err)
	}
	report := output.String()
	for _, want := range []string{
		"# Prism Priority Inventory Phase 0",
		"`/v1/*` | `proxy`",
		"`/api/auth/session` | `management` | `M1`",
		"`/api/config/bootstrap` | `management` | `M2`",
		"`/api/stats/*` | `management` | `M3`",
		"runtime telemetry outbox",
		"runtimeFeedbackContext",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("expected report to contain %q, got:\n%s", want, report)
		}
	}
}

func TestWriteMarkdownInventoryEscapesPipesInsideTableCells(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := WriteMarkdownInventory(&output, DefaultInventory()); err != nil {
		t.Fatalf("WriteMarkdownInventory returned error: %v", err)
	}
	report := output.String()

	for _, forbidden := range []string{
		"/api/loadbalance/current-state|events",
		"/api/stats/requests|statistics",
	} {
		if strings.Contains(report, forbidden) {
			t.Fatalf("expected report to escape raw pipe in %q, got:\n%s", forbidden, report)
		}
	}
	for _, want := range []string{
		"`/api/loadbalance/current-state\\|events{/{event_id}}`",
		"`/api/stats/requests\\|statistics`",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("expected report to contain escaped path %q, got:\n%s", want, report)
		}
	}
}
