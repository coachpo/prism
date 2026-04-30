package priority

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestDefaultInventoryHasNoUnclassifiedEntries(t *testing.T) {
	t.Parallel()

	inventory := DefaultInventory()
	if count := inventory.UnclassifiedCount(); count != 0 {
		t.Fatalf("DefaultInventory().UnclassifiedCount() = %d, want 0", count)
	}
	if err := ValidateInventory(inventory); err != nil {
		t.Fatalf("ValidateInventory(DefaultInventory()) returned error: %v", err)
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
		"## Jobs",
		"`/v1/*` | `proxy`",
		"`/api/auth/session` | `management` | `M1`",
		"`/api/config/bootstrap` | `management` | `M2`",
		"`/api/stats/*` | `management` | `M3`",
		"runtime telemetry outbox",
		"AsyncDashboardPublisher.handleScheduledPublish",
		"runtimeFeedbackPipeline.TryEnqueue",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("expected report to contain %q, got:\n%s", want, report)
		}
	}
}

func TestValidateInventoryRejectsUnclassifiedEntries(t *testing.T) {
	t.Parallel()

	inventory := Inventory{
		Routes:    []RouteInventoryEntry{{Name: "missing route", Method: "GET", Path: "/api/missing", Source: "negative fixture", Classified: false}},
		Resources: []ResourceInventoryEntry{{Family: "DB", Name: "missing resource", Location: "negative fixture", Classified: false}},
		Jobs:      []JobInventoryEntry{{Name: "missing job", Source: "negative fixture", Classified: false}},
	}

	err := ValidateInventory(inventory)
	if !errors.Is(err, ErrInventoryValidation) {
		t.Fatalf("expected inventory validation error, got %v", err)
	}
	var validationErr InventoryValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected InventoryValidationError, got %T", err)
	}
	wantKinds := map[string]bool{"route": false, "resource": false, "job": false}
	for _, problem := range validationErr.Problems {
		if problem.Reason == "unclassified" {
			wantKinds[problem.Kind] = true
		}
	}
	for kind, found := range wantKinds {
		if !found {
			t.Fatalf("expected unclassified %s problem in %+v", kind, validationErr.Problems)
		}
	}
}

func TestValidateInventoryRejectsInvalidMetadata(t *testing.T) {
	t.Parallel()

	inventory := Inventory{
		Routes: []RouteInventoryEntry{{Name: "invalid route", Method: "GET", Path: "/api/invalid", Priority: PriorityBackground, ManagementTier: ManagementTierM1, Source: "negative fixture", Classified: true}},
		Jobs:   []JobInventoryEntry{{Name: "invalid job", Priority: PriorityManagement, BackgroundSubclass: BackgroundSubclassHigh, Source: "negative fixture", Classified: true}},
	}

	err := ValidateInventory(inventory)
	if !errors.Is(err, ErrInventoryValidation) {
		t.Fatalf("expected inventory validation error, got %v", err)
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
