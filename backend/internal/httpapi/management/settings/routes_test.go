package settings

import (
	"strings"
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

func TestAuditSettingsDefaultsStableFamilyOrder(t *testing.T) {
	response := buildAuditSettingsResponse(7, []auditSettingsRow{{APIFamily: "gemini", AuditEnabled: true}})

	if response.ProfileID != 7 || len(response.Settings) != 3 {
		t.Fatalf("expected three audit settings for profile 7, got %+v", response)
	}
	wantFamilies := []string{"openai", "anthropic", "gemini"}
	for index, family := range wantFamilies {
		setting := response.Settings[index]
		if setting.APIFamily != family {
			t.Fatalf("expected family order %v, got %+v", wantFamilies, response.Settings)
		}
		if family != "gemini" && (setting.AuditEnabled || setting.AuditCaptureBodies) {
			t.Fatalf("expected missing family %s to default false/false, got %+v", family, setting)
		}
	}
	if !response.Settings[2].AuditEnabled || response.Settings[2].AuditCaptureBodies {
		t.Fatalf("expected existing gemini row to preserve booleans, got %+v", response.Settings[2])
	}
}

func TestAuditSettingsValidationNormalizesAndOrdersFamilies(t *testing.T) {
	request := auditSettingsUpdateRequest{Settings: []auditSetting{
		{APIFamily: " Gemini ", AuditEnabled: true, AuditCaptureBodies: true},
		{APIFamily: "openai", AuditEnabled: false},
		{APIFamily: "ANTHROPIC", AuditEnabled: true},
	}}

	if err := normalizeAndValidateAuditSettingsRequest(&request); err != nil {
		t.Fatalf("expected valid audit settings, got %v", err)
	}
	wantFamilies := []string{"openai", "anthropic", "gemini"}
	for index, family := range wantFamilies {
		if request.Settings[index].APIFamily != family {
			t.Fatalf("expected canonical family order %v, got %+v", wantFamilies, request.Settings)
		}
	}
}

func TestAuditSettingsValidationRejectsInvalidPayloads(t *testing.T) {
	testCases := []struct {
		name    string
		request auditSettingsUpdateRequest
		detail  string
	}{
		{
			name: "unknown family",
			request: auditSettingsUpdateRequest{Settings: []auditSetting{
				{APIFamily: "openai"},
				{APIFamily: "anthropic"},
				{APIFamily: "mistral"},
			}},
			detail: "not supported",
		},
		{
			name: "duplicate family",
			request: auditSettingsUpdateRequest{Settings: []auditSetting{
				{APIFamily: "openai"},
				{APIFamily: "openai"},
				{APIFamily: "gemini"},
			}},
			detail: "Duplicate audit setting for api_family=openai",
		},
		{
			name: "missing family",
			request: auditSettingsUpdateRequest{Settings: []auditSetting{
				{APIFamily: "openai"},
				{APIFamily: "anthropic"},
			}},
			detail: "settings must include exactly openai, anthropic, and gemini",
		},
		{
			name: "capture requires enabled",
			request: auditSettingsUpdateRequest{Settings: []auditSetting{
				{APIFamily: "openai", AuditCaptureBodies: true},
				{APIFamily: "anthropic"},
				{APIFamily: "gemini"},
			}},
			detail: "audit_capture_bodies requires audit_enabled",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := normalizeAndValidateAuditSettingsRequest(&testCase.request)
			if err == nil || !strings.Contains(err.Error(), testCase.detail) {
				t.Fatalf("expected error containing %q, got %v", testCase.detail, err)
			}
		})
	}
}
