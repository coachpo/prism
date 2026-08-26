package contracttest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGlobalLogRetentionSettingsAndJobs(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	assertLogRetentionPayload(t, requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/log-retention", nil, modelHeader(defaultProfileID), http.StatusOK), nil, nil, nil, nil)
	invalid := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/log-retention",
		map[string]any{"operation_id": "s11-invalid", "expected_revision": "1", "policies": map[string]any{
			"request_logs_retention_days":       0,
			"statistics_retention_days":         nil,
			"audit_logs_retention_days":         nil,
			"loadbalance_events_retention_days": nil,
		}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, invalid, http.StatusUnprocessableEntity, "invalid retention policy")
	updatedPayload, loadedPayload := putThenGetLogRetention(t, harness, defaultProfileID, intRef(14), intRef(30), intRef(7), intRef(45))
	assertLogRetentionPayload(t, updatedPayload, intRef(14), intRef(30), intRef(7), intRef(45))
	assertLogRetentionPayload(t, loadedPayload, intRef(14), intRef(30), intRef(7), intRef(45))
	clearedPayload, loadedPayload := putThenGetLogRetention(t, harness, defaultProfileID, intRef(21), nil, intRef(90), nil)
	assertLogRetentionPayload(t, clearedPayload, intRef(21), nil, intRef(90), nil)
	assertLogRetentionPayload(t, loadedPayload, intRef(21), nil, intRef(90), nil)
	for _, legacy := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/settings/retention"},
		{method: http.MethodDelete, path: "/api/stats/requests"},
	} {
		assertStatus(t, harness.requestJSON(t, harness.client, legacy.method, legacy.path, nil, modelHeader(defaultProfileID)), http.StatusNotFound)
	}
	// Destructive manual cleanup requires the fresh-preflight -> sealed-job
	// flow (SPEC §6); the old table/cutoff create route is removed.
	job := createManualRetentionJobViaPreflight(t, harness, defaultProfileID, "request_logs")
	if job["state"] != "queued" || job["dataset"] != "request_logs" {
		t.Fatalf("expected queued manual log-retention job, got %+v", job)
	}
}

func retentionIsDestructive(current map[string]any, requestLogs *int, statistics *int, audit *int, loadbalance *int) bool {
	pairs := []struct {
		field string
		after *int
	}{
		{"request_logs_retention_days", requestLogs},
		{"statistics_retention_days", statistics},
		{"audit_logs_retention_days", audit},
		{"loadbalance_events_retention_days", loadbalance},
	}
	for _, pair := range pairs {
		var before *int
		if value, ok := current[pair.field].(float64); ok {
			converted := int(value)
			before = &converted
		}
		if before == nil && pair.after != nil {
			return true
		}
		if before != nil && pair.after != nil && *pair.after < *before {
			return true
		}
	}
	return false
}

func putThenGetLogRetention(t *testing.T, harness *contractHarness, profileID int, requestLogs *int, statistics *int, audit *int, loadbalance *int) (map[string]any, map[string]any) {
	t.Helper()
	getResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/log-retention", nil, modelHeader(profileID))
	assertStatus(t, getResponse, http.StatusOK)
	var parsed struct {
		Revision string         `json:"revision"`
		Policies map[string]any `json:"policies"`
	}
	if err := json.Unmarshal([]byte(readResponseBody(t, getResponse)), &parsed); err != nil {
		t.Fatalf("decode log-retention settings response: %v", err)
	}
	body := map[string]any{
		"operation_id":      "s11-retention-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"expected_revision": parsed.Revision,
		"policies": map[string]any{
			"request_logs_retention_days":       requestLogs,
			"statistics_retention_days":         statistics,
			"audit_logs_retention_days":         audit,
			"loadbalance_events_retention_days": loadbalance,
		},
	}
	// Destructive transitions (NULL -> N or shortening) require a fresh
	// preflight token plus the confirmation keyword (SPEC §5.2/§6).
	if retentionIsDestructive(parsed.Policies, requestLogs, statistics, audit, loadbalance) {
		preflight := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/maintenance/log-retention/preflights", map[string]any{
			"kind":                       "policy_change",
			"operation_id":               body["operation_id"],
			"preflight_attempt_id":       "s11-retention-attempt",
			"expected_settings_revision": parsed.Revision,
			"policies":                   body["policies"],
		}, modelHeader(profileID), http.StatusCreated)
		token, ok := preflight["preflight_token"].(string)
		if !ok || token == "" {
			t.Fatalf("expected retention preflight token, got %+v", preflight)
		}
		body["preflight_token"] = token
		body["confirmation"] = map[string]any{"keyword": "DELETE"}
	}
	putResponse := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/log-retention", body, modelHeader(profileID))
	assertStatus(t, putResponse, http.StatusOK)
	var putPayload struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal([]byte(readResponseBody(t, putResponse)), &putPayload); err != nil {
		t.Fatalf("decode log-retention PUT response: %v", err)
	}
	loaded := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/log-retention", nil, modelHeader(profileID), http.StatusOK)
	return putPayload.Settings, loaded
}

func createManualRetentionJobViaPreflight(t *testing.T, harness *contractHarness, profileID int, dataset string) map[string]any {
	t.Helper()
	preflight := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/maintenance/log-retention/preflights", map[string]any{
		"kind":                 "manual_cleanup",
		"operation_id":         "s11-purge-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"preflight_attempt_id": "s11-attempt-1",
		"dataset":              dataset,
		"selection":            map[string]any{"mode": "keep_days", "days": 7},
	}, modelHeader(profileID), http.StatusCreated)
	token, ok := preflight["preflight_token"].(string)
	if !ok || token == "" {
		t.Fatalf("expected preflight token, got %+v", preflight)
	}
	jobResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/maintenance/log-retention/jobs",
		map[string]any{"operation_id": preflight["operation_id"], "preflight_token": token, "confirmation": map[string]any{"keyword": "DELETE"}},
		modelHeader(profileID),
	)
	assertStatus(t, jobResponse, http.StatusAccepted)
	var jobPayload struct {
		Job map[string]any `json:"job"`
	}
	if err := json.Unmarshal([]byte(readResponseBody(t, jobResponse)), &jobPayload); err != nil {
		t.Fatalf("decode log-retention job response: %v", err)
	}
	return jobPayload.Job
}

func assertLogRetentionPayload(t *testing.T, payload map[string]any, requestLogsRetentionDays *int, statisticsRetentionDays *int, auditLogsRetentionDays *int, loadbalanceEventsRetentionDays *int) {
	t.Helper()
	policies, ok := payload["policies"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested policies object, got %+v", payload)
	}
	for key, want := range map[string]*int{
		"request_logs_retention_days":       requestLogsRetentionDays,
		"statistics_retention_days":         statisticsRetentionDays,
		"audit_logs_retention_days":         auditLogsRetentionDays,
		"loadbalance_events_retention_days": loadbalanceEventsRetentionDays,
	} {
		if want == nil {
			if policies[key] != nil {
				t.Fatalf("expected %s to be null, got %+v", key, payload)
			}
			continue
		}
		if policies[key] != float64(*want) {
			t.Fatalf("expected %s=%d, got %+v", key, *want, payload)
		}
	}
}

func assertDeletedPayload(t *testing.T, payload map[string]any) {
	t.Helper()
	if payload["deleted"] != true {
		t.Fatalf("expected delete confirmation payload, got %+v", payload)
	}
}

func assertContainsAll(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, fragment := range want {
		if !strings.Contains(got, fragment) {
			t.Fatalf("expected %q to contain %q", got, fragment)
		}
	}
}

func containsAny(got string, want ...string) bool {
	for _, fragment := range want {
		if strings.Contains(got, fragment) {
			return true
		}
	}
	return false
}
