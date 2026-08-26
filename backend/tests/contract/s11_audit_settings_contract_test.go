package contracttest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestAuditSettings(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	wantDefault := map[string]string{"openai": "disabled", "anthropic": "disabled", "gemini": "disabled"}
	wantUpdated := map[string]string{"openai": "metadata_only", "anthropic": "disabled", "gemini": "body_capture"}
	wantOtherHeader := map[string]string{"openai": "disabled", "anthropic": "body_capture", "gemini": "disabled"}
	payload := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/audit", nil, modelHeader(defaultProfileID), http.StatusOK)
	assertAuditSettingsPayload(t, payload, wantDefault)
	payload = putAuditSettings(t, harness, defaultProfileID, auditPolicy("gemini", "body_capture"), auditPolicy("openai", "metadata_only"), auditPolicy("anthropic", "disabled"))
	assertAuditSettingsPayload(t, payload, wantUpdated)
	assertAuditSettingsRows(t, harness, defaultProfileID, wantUpdated)
	payload = requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/audit", nil, modelHeader(defaultProfileID), http.StatusOK)
	assertAuditSettingsPayload(t, payload, wantUpdated)
	// Profiles are frozen to Default id=1: the effective profile resolver pins
	// every request to the Default profile, so the other-profile request reads
	// and writes the same singleton settings surface.
	otherProfileID := s11InsertAuditSettingsProfile(t, harness, "S11 Audit Settings Other")
	payload = requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/audit", nil, modelHeader(otherProfileID), http.StatusOK)
	assertAuditSettingsPayload(t, payload, wantUpdated)
	payload = putAuditSettings(t, harness, otherProfileID, auditPolicy("openai", "disabled"), auditPolicy("anthropic", "body_capture"), auditPolicy("gemini", "disabled"))
	assertAuditSettingsPayload(t, payload, wantOtherHeader)
	assertAuditSettingsRows(t, harness, defaultProfileID, wantOtherHeader)
	assertAuditSettingsRows(t, harness, otherProfileID, map[string]string{})
	invalidRequests := []struct {
		name   string
		body   map[string]any
		status int
		detail string
	}{
		{
			name: "unknown family",
			body: auditPolicyRequest(
				auditPolicy("openai", "metadata_only"),
				auditPolicy("anthropic", "disabled"),
				auditPolicy("mistral", "disabled"),
			),
			status: http.StatusUnprocessableEntity,
			detail: "unknown audit family",
		},
		{
			name: "duplicate family",
			body: auditPolicyRequest(
				auditPolicy("openai", "metadata_only"),
				auditPolicy("openai", "disabled"),
				auditPolicy("gemini", "disabled"),
			),
			status: http.StatusUnprocessableEntity,
			detail: "duplicate audit family",
		},
		{
			name: "missing family",
			body: auditPolicyRequest(
				auditPolicy("openai", "metadata_only"),
				auditPolicy("anthropic", "disabled"),
			),
			status: http.StatusUnprocessableEntity,
			detail: "settings must include exactly openai, anthropic, and gemini",
		},
		{
			name: "unknown mode",
			body: auditPolicyRequest(
				auditPolicy("openai", "unknown_mode"),
				auditPolicy("anthropic", "disabled"),
				auditPolicy("gemini", "disabled"),
			),
			status: http.StatusUnprocessableEntity,
			detail: "unknown audit mode",
		},
	}
	for _, testCase := range invalidRequests {
		t.Run(testCase.name, func(t *testing.T) {
			response := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/audit", testCase.body, modelHeader(defaultProfileID))
			assertErrorResponse(t, response, testCase.status, testCase.detail)
			assertAuditSettingsRows(t, harness, defaultProfileID, wantOtherHeader)
		})
	}
}

func s11InsertAuditSettingsProfile(t *testing.T, harness *contractHarness, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var profileID int
	if err := harness.conn.QueryRow(
		context.Background(),
		`INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, NULL, FALSE, FALSE, TRUE, 1, $2, $2) RETURNING id`,
		name,
		now,
	).Scan(&profileID); err != nil {
		t.Fatalf("insert audit settings profile: %v", err)
	}
	return profileID
}

func assertAuditSettingsPayload(t *testing.T, payload map[string]any, want map[string]string) {
	t.Helper()
	items := asSliceOfMaps(t, payload["policies"])
	wantFamilies := []string{"openai", "anthropic", "gemini"}
	if len(items) != len(wantFamilies) || len(want) != len(wantFamilies) {
		t.Fatalf("expected three audit policies, got %+v", payload)
	}
	for index, family := range wantFamilies {
		item := items[index]
		if item["family"] != family {
			t.Fatalf("expected audit families %v, got %+v", wantFamilies, items)
		}
		if wantMode, ok := want[family]; !ok || item["mode"] != wantMode {
			t.Fatalf("unexpected audit policy at %s: got %+v want %+v", family, item, want)
		}
	}
}

func putAuditSettings(t *testing.T, harness *contractHarness, profileID int, policies ...map[string]any) map[string]any {
	t.Helper()
	getResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/audit", nil, modelHeader(profileID))
	assertStatus(t, getResponse, http.StatusOK)
	var parsed struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal([]byte(readResponseBody(t, getResponse)), &parsed); err != nil {
		t.Fatalf("decode audit settings response: %v", err)
	}
	body := map[string]any{
		"operation_id":      "contract-audit-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"expected_revision": parsed.Revision,
		"policies":          policies,
	}
	response := requestJSONStatus[map[string]any](t, harness, http.MethodPut, "/api/settings/audit", body, modelHeader(profileID), http.StatusOK)
	settings, ok := response["settings"].(map[string]any)
	if !ok {
		t.Fatalf("expected settings object in audit PUT response, got %+v", response)
	}
	return settings
}

func auditPolicyRequest(policies ...map[string]any) map[string]any {
	return map[string]any{"operation_id": "contract-audit-invalid", "expected_revision": "1", "policies": policies}
}

func auditPolicy(family string, mode string) map[string]any {
	return map[string]any{"family": family, "mode": mode}
}

func assertAuditSettingsRows(t *testing.T, harness *contractHarness, profileID int, want map[string]string) {
	t.Helper()
	rows, err := harness.conn.Query(context.Background(), `SELECT api_family, audit_enabled, audit_capture_bodies FROM profile_api_family_audit_settings WHERE profile_id = $1 ORDER BY api_family`, profileID)
	if err != nil {
		t.Fatalf("query audit settings rows: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var family string
		var enabled bool
		var capture bool
		if err := rows.Scan(&family, &enabled, &capture); err != nil {
			t.Fatalf("scan audit settings row: %v", err)
		}
		switch {
		case !enabled:
			got[family] = "disabled"
		case capture:
			got[family] = "body_capture"
		default:
			got[family] = "metadata_only"
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit settings rows: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected audit settings rows %+v, got %+v", want, got)
	}
	for family, wantMode := range want {
		if gotMode, ok := got[family]; !ok || gotMode != wantMode {
			t.Fatalf("expected audit settings rows %+v, got %+v", want, got)
		}
	}
}

func asSliceOfMaps(t *testing.T, raw any) []map[string]any {
	t.Helper()
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected []any payload, got %T %+v", raw, raw)
	}
	result := make([]map[string]any, len(items))
	for i, item := range items {
		result[i] = asMap(t, item)
	}
	return result
}
