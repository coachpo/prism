package settings

import (
	"testing"
)

func TestCanonicalOwnerSemanticSnapshotIgnoresAppendEvidence(t *testing.T) {
	before := map[string]any{
		"kind":                       "observe",
		"policy_generation":          "4",
		"retention_revocation_epoch": "2",
		"fence_generation":           "8",
		"purge_state":                "idle",
		"coverage_revision":          "coverage-1",
		"coverage_hash":              "hash-1",
		"generated_at":               "2026-08-11T10:00:00Z",
		"materialization_cut": map[string]any{
			"kind":                  "request_visibility_cut",
			"request_committed_cut": "2026-08-11T09:59:00Z",
		},
		"actual_coverage": map[string]any{
			"earliest":  "2026-08-01T00:00:00Z",
			"latest":    "2026-08-11T09:59:00Z",
			"gaps":      []any{},
			"complete":  true,
			"freshness": "fresh",
			"precision": "owner_bounds",
			"source":    "Requests.actual_coverage",
		},
	}
	after := map[string]any{
		"kind":                       "observe",
		"policy_generation":          "4",
		"retention_revocation_epoch": "2",
		"fence_generation":           "8",
		"purge_state":                "idle",
		"coverage_revision":          "coverage-2",
		"coverage_hash":              "hash-2",
		"generated_at":               "2026-08-11T10:00:02Z",
		"materialization_cut": map[string]any{
			"kind":                  "request_visibility_cut",
			"request_committed_cut": "2026-08-11T10:00:01Z",
		},
		"actual_coverage": map[string]any{
			"earliest":  "2026-08-01T00:00:00Z",
			"latest":    "2026-08-11T10:00:01Z",
			"gaps":      []any{},
			"complete":  true,
			"freshness": "fresh",
			"precision": "owner_bounds",
			"source":    "Requests.actual_coverage",
		},
	}
	if got, want := canonicalOwnerSemanticSnapshotHash(before), canonicalOwnerSemanticSnapshotHash(after); got != want {
		t.Fatalf("append-only coverage evidence must not stale the semantic owner snapshot: got %s want %s", got, want)
	}
	after["fence_generation"] = "9"
	if canonicalOwnerSemanticSnapshotHash(before) == canonicalOwnerSemanticSnapshotHash(after) {
		t.Fatal("semantic fence changes must stale the owner snapshot")
	}
}
