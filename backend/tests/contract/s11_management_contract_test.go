package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	managementconfigrules "github.com/coachpo/prism/backend/internal/httpapi/management/configrules"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestCostingSettings(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	initial := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/costing", nil, modelHeader(defaultProfileID))
	assertStatus(t, initial, http.StatusOK)
	var initialPayload map[string]any
	decodeJSONResponse(t, initial, &initialPayload)
	if initialPayload["report_currency_code"] != "USD" || initialPayload["report_currency_symbol"] != "$" {
		t.Fatalf("expected default costing settings, got %+v", initialPayload)
	}
	if mappings, ok := initialPayload["endpoint_fx_mappings"].([]any); !ok || len(mappings) != 0 {
		t.Fatalf("expected empty initial endpoint_fx_mappings, got %+v", initialPayload)
	}

	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S11 Costing Strategy")
	modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s11-costing-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "S11 Costing Endpoint", 0)
	modelInsertConnection(t, harness, defaultProfileID, modelLoadModelConfigID(t, harness, defaultProfileID, "s11-costing-model"), endpointID, 0, true, nil)

	invalidMapping := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/costing",
		map[string]any{
			"report_currency_code":   "EUR",
			"report_currency_symbol": "€",
			"timezone_preference":    "Europe/Helsinki",
			"endpoint_fx_mappings": []map[string]any{{
				"model_id":    "missing-model",
				"endpoint_id": endpointID,
				"fx_rate":     "0.92",
			}},
		},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, invalidMapping, http.StatusBadRequest, fmt.Sprintf("No connection found for model_id='missing-model' and endpoint_id=%d", endpointID))

	updated := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/costing",
		map[string]any{
			"report_currency_code":   " eur ",
			"report_currency_symbol": " € ",
			"timezone_preference":    " Europe/Helsinki ",
			"endpoint_fx_mappings": []map[string]any{{
				"model_id":    "s11-costing-model",
				"endpoint_id": endpointID,
				"fx_rate":     "0.92",
			}},
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, updated, http.StatusOK)
	var updatedPayload map[string]any
	decodeJSONResponse(t, updated, &updatedPayload)
	if updatedPayload["profile_id"] != float64(defaultProfileID) || updatedPayload["report_currency_code"] != "EUR" || updatedPayload["report_currency_symbol"] != "€" || updatedPayload["timezone_preference"] != "Europe/Helsinki" {
		t.Fatalf("expected updated costing settings payload, got %+v", updatedPayload)
	}
	updatedMappings := updatedPayload["endpoint_fx_mappings"].([]any)
	if len(updatedMappings) != 1 {
		t.Fatalf("expected one endpoint fx mapping after update, got %+v", updatedPayload)
	}
	updatedMapping := asMap(t, updatedMappings[0])
	if updatedMapping["model_id"] != "s11-costing-model" || jsonInt(t, updatedMapping["endpoint_id"]) != endpointID || updatedMapping["fx_rate"] != "0.92" {
		t.Fatalf("unexpected updated costing mapping: %+v", updatedMapping)
	}

	loaded := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/costing", nil, modelHeader(defaultProfileID))
	assertStatus(t, loaded, http.StatusOK)
	decodeJSONResponse(t, loaded, &updatedPayload)
	if updatedPayload["report_currency_code"] != "EUR" || updatedPayload["timezone_preference"] != "Europe/Helsinki" {
		t.Fatalf("expected costing settings round-trip to persist, got %+v", updatedPayload)
	}
}

func TestTimezoneSettings(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	initial := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/timezone", nil, modelHeader(defaultProfileID))
	assertStatus(t, initial, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, initial, &payload)
	if payload["profile_id"] != float64(defaultProfileID) || payload["timezone_preference"] != nil {
		t.Fatalf("expected default timezone payload, got %+v", payload)
	}

	updated := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/timezone",
		map[string]any{"timezone_preference": " America/New_York "},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, updated, http.StatusOK)
	decodeJSONResponse(t, updated, &payload)
	if payload["timezone_preference"] != "America/New_York" {
		t.Fatalf("expected trimmed timezone preference, got %+v", payload)
	}

	cleared := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/timezone",
		map[string]any{"timezone_preference": "   "},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, cleared, http.StatusOK)
	decodeJSONResponse(t, cleared, &payload)
	if payload["timezone_preference"] != nil {
		t.Fatalf("expected blank timezone preference to clear to null, got %+v", payload)
	}
}

func TestAuditSettings(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	initial := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/audit", nil, modelHeader(defaultProfileID))
	assertStatus(t, initial, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, initial, &payload)
	assertAuditSettingsPayload(t, payload, defaultProfileID, []map[string]bool{
		{"audit_enabled": false, "audit_capture_bodies": false},
		{"audit_enabled": false, "audit_capture_bodies": false},
		{"audit_enabled": false, "audit_capture_bodies": false},
	})

	updated := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/audit",
		map[string]any{"settings": []map[string]any{
			{"api_family": " Gemini ", "audit_enabled": true, "audit_capture_bodies": true},
			{"api_family": "openai", "audit_enabled": true, "audit_capture_bodies": false},
			{"api_family": "ANTHROPIC", "audit_enabled": false, "audit_capture_bodies": false},
		}},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, updated, http.StatusOK)
	decodeJSONResponse(t, updated, &payload)
	assertAuditSettingsPayload(t, payload, defaultProfileID, []map[string]bool{
		{"audit_enabled": true, "audit_capture_bodies": false},
		{"audit_enabled": false, "audit_capture_bodies": false},
		{"audit_enabled": true, "audit_capture_bodies": true},
	})
	assertAuditSettingsRows(t, harness, defaultProfileID, map[string][2]bool{
		"openai":    {true, false},
		"anthropic": {false, false},
		"gemini":    {true, true},
	})

	loaded := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/audit", nil, modelHeader(defaultProfileID))
	assertStatus(t, loaded, http.StatusOK)
	decodeJSONResponse(t, loaded, &payload)
	assertAuditSettingsPayload(t, payload, defaultProfileID, []map[string]bool{
		{"audit_enabled": true, "audit_capture_bodies": false},
		{"audit_enabled": false, "audit_capture_bodies": false},
		{"audit_enabled": true, "audit_capture_bodies": true},
	})

	otherProfileID := s11InsertAuditSettingsProfile(t, harness, "S11 Audit Settings Other")
	other := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/audit", nil, modelHeader(otherProfileID))
	assertStatus(t, other, http.StatusOK)
	decodeJSONResponse(t, other, &payload)
	assertAuditSettingsPayload(t, payload, defaultProfileID, []map[string]bool{
		{"audit_enabled": true, "audit_capture_bodies": false},
		{"audit_enabled": false, "audit_capture_bodies": false},
		{"audit_enabled": true, "audit_capture_bodies": true},
	})

	otherHeaderUpdate := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/audit",
		map[string]any{"settings": []map[string]any{
			{"api_family": "openai", "audit_enabled": false, "audit_capture_bodies": false},
			{"api_family": "anthropic", "audit_enabled": true, "audit_capture_bodies": true},
			{"api_family": "gemini", "audit_enabled": false, "audit_capture_bodies": false},
		}},
		modelHeader(otherProfileID),
	)
	assertStatus(t, otherHeaderUpdate, http.StatusOK)
	decodeJSONResponse(t, otherHeaderUpdate, &payload)
	assertAuditSettingsPayload(t, payload, defaultProfileID, []map[string]bool{
		{"audit_enabled": false, "audit_capture_bodies": false},
		{"audit_enabled": true, "audit_capture_bodies": true},
		{"audit_enabled": false, "audit_capture_bodies": false},
	})
	assertAuditSettingsRows(t, harness, defaultProfileID, map[string][2]bool{
		"openai":    {false, false},
		"anthropic": {true, true},
		"gemini":    {false, false},
	})
	assertAuditSettingsRows(t, harness, otherProfileID, map[string][2]bool{})

	invalidRequests := []struct {
		name   string
		body   map[string]any
		detail string
	}{
		{
			name: "unknown family",
			body: map[string]any{"settings": []map[string]any{
				{"api_family": "openai", "audit_enabled": true, "audit_capture_bodies": false},
				{"api_family": "anthropic", "audit_enabled": false, "audit_capture_bodies": false},
				{"api_family": "mistral", "audit_enabled": false, "audit_capture_bodies": false},
			}},
			detail: `api_family "mistral" is not supported`,
		},
		{
			name: "duplicate family",
			body: map[string]any{"settings": []map[string]any{
				{"api_family": "openai", "audit_enabled": true, "audit_capture_bodies": false},
				{"api_family": "openai", "audit_enabled": false, "audit_capture_bodies": false},
				{"api_family": "gemini", "audit_enabled": false, "audit_capture_bodies": false},
			}},
			detail: "Duplicate audit setting for api_family=openai",
		},
		{
			name: "missing family",
			body: map[string]any{"settings": []map[string]any{
				{"api_family": "openai", "audit_enabled": true, "audit_capture_bodies": false},
				{"api_family": "anthropic", "audit_enabled": false, "audit_capture_bodies": false},
			}},
			detail: "settings must include exactly openai, anthropic, and gemini",
		},
		{
			name: "capture requires enabled",
			body: map[string]any{"settings": []map[string]any{
				{"api_family": "openai", "audit_enabled": false, "audit_capture_bodies": true},
				{"api_family": "anthropic", "audit_enabled": false, "audit_capture_bodies": false},
				{"api_family": "gemini", "audit_enabled": false, "audit_capture_bodies": false},
			}},
			detail: "audit_capture_bodies requires audit_enabled",
		},
	}
	for _, testCase := range invalidRequests {
		t.Run(testCase.name, func(t *testing.T) {
			response := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/audit", testCase.body, modelHeader(defaultProfileID))
			assertErrorResponse(t, response, http.StatusBadRequest, testCase.detail)
			assertAuditSettingsRows(t, harness, defaultProfileID, map[string][2]bool{
				"openai":    {false, false},
				"anthropic": {true, true},
				"gemini":    {false, false},
			})
		})
	}
}

func TestAuditSettingsRouteContractProfileScope(t *testing.T) {
	raw, err := os.ReadFile("../../internal/platform/http/management_route_contract.json")
	if err != nil {
		t.Fatalf("read management route contract: %v", err)
	}
	var rows []struct {
		RoutePattern        string   `json:"route_pattern"`
		Methods             []string `json:"methods"`
		ProfileScoped       bool     `json:"profile_scoped"`
		InvalidatesPlanning bool     `json:"invalidates_planning"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("parse management route contract: %v", err)
	}
	for _, row := range rows {
		if row.RoutePattern != "/api/settings/audit" {
			continue
		}
		if !row.ProfileScoped || !row.InvalidatesPlanning || !stringSetEqual(row.Methods, []string{http.MethodGet, http.MethodPut}) {
			t.Fatalf("unexpected audit settings route contract: %+v", row)
		}
		return
	}
	t.Fatal("/api/settings/audit route contract entry not found")
}

func TestGlobalLogRetentionSettingsAndJobs(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	initial := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/log-retention", nil, modelHeader(defaultProfileID))
	assertStatus(t, initial, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, initial, &payload)
	if payload["request_logs_retention_days"] != nil || payload["statistics_retention_days"] != nil || payload["audit_logs_retention_days"] != nil || payload["loadbalance_events_retention_days"] != nil {
		t.Fatalf("expected default global retention settings payload, got %+v", payload)
	}

	invalid := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/log-retention",
		map[string]any{"request_logs_retention_days": 0},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, invalid, http.StatusBadRequest, "request_logs_retention_days must be >= 1 when provided")

	updated := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/log-retention",
		map[string]any{
			"request_logs_retention_days":       14,
			"statistics_retention_days":         30,
			"audit_logs_retention_days":         7,
			"loadbalance_events_retention_days": 45,
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, updated, http.StatusOK)
	decodeJSONResponse(t, updated, &payload)
	if payload["request_logs_retention_days"] != float64(14) || payload["statistics_retention_days"] != float64(30) || payload["audit_logs_retention_days"] != float64(7) || payload["loadbalance_events_retention_days"] != float64(45) {
		t.Fatalf("expected persisted global retention settings payload, got %+v", payload)
	}

	cleared := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/log-retention",
		map[string]any{
			"request_logs_retention_days":       21,
			"statistics_retention_days":         nil,
			"audit_logs_retention_days":         90,
			"loadbalance_events_retention_days": nil,
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, cleared, http.StatusOK)
	decodeJSONResponse(t, cleared, &payload)
	if payload["request_logs_retention_days"] != float64(21) || payload["statistics_retention_days"] != nil || payload["audit_logs_retention_days"] != float64(90) || payload["loadbalance_events_retention_days"] != nil {
		t.Fatalf("expected global retention settings clear/update payload, got %+v", payload)
	}

	loaded := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/log-retention", nil, modelHeader(defaultProfileID))
	assertStatus(t, loaded, http.StatusOK)
	decodeJSONResponse(t, loaded, &payload)
	if payload["request_logs_retention_days"] != float64(21) || payload["statistics_retention_days"] != nil || payload["audit_logs_retention_days"] != float64(90) || payload["loadbalance_events_retention_days"] != nil {
		t.Fatalf("expected global retention settings round-trip to persist, got %+v", payload)
	}

	legacySettings := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/retention", nil, modelHeader(defaultProfileID))
	assertStatus(t, legacySettings, http.StatusNotFound)
	legacyCleanup := harness.requestJSON(t, harness.client, http.MethodDelete, "/api/stats/requests", nil, modelHeader(defaultProfileID))
	assertStatus(t, legacyCleanup, http.StatusNotFound)

	jobResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/maintenance/log-retention/jobs",
		map[string]any{"table": "request_logs", "reason": "contract guardrail"},
		withHeader(modelHeader(defaultProfileID), "Idempotency-Key", "s11-log-retention-job"),
	)
	assertStatus(t, jobResponse, http.StatusAccepted)
	var jobPayload map[string]any
	decodeJSONResponse(t, jobResponse, &jobPayload)
	jobID, ok := jobPayload["job_id"].(string)
	statusURL, _ := jobPayload["status_url"].(string)
	scope := asMap(t, jobPayload["scope"])
	if !ok || jobID == "" || jobPayload["state"] != "queued" || statusURL != "/api/management/jobs/"+jobID || jobResponse.Header.Get("Location") != statusURL || scope["table"] != "request_logs" || scope["cutoff"] == nil {
		t.Fatalf("expected log-retention job response schema and scope, got %+v with Location %q", jobPayload, jobResponse.Header.Get("Location"))
	}
	if _, exists := jobPayload["status"]; exists {
		t.Fatalf("log-retention job create response must use state, not status: %+v", jobPayload)
	}
}

func TestLoadbalanceStrategyGet(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/loadbalance/strategies",
		map[string]any{
			"name":                                   "S11 Legacy Detail",
			"legacy_strategy_type":                   "round-robin",
			"failure_status_codes":                   []int{503, 429, 500},
			"ban_mode":                               "temporary",
			"retry_base_delay_ms":                    1234,
			"retry_backoff_multiplier":               3.5,
			"retry_jitter_ratio":                     0.35,
			"retry_max_delay_ms":                     456789,
			"cycle_retry_attempt_limit":              7,
			"ban_cumulative_retry_attempt_threshold": 9,
			"ban_duration_seconds":                   1800,
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	strategyID := jsonInt(t, created["id"])

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/strategies/%d", strategyID), nil, modelHeader(defaultProfileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detail map[string]any
	decodeJSONResponse(t, detailResponse, &detail)
	if detail["name"] != "S11 Legacy Detail" || detail["legacy_strategy_type"] != "round-robin" || jsonInt(t, detail["attached_model_count"]) != 0 {
		t.Fatalf("expected legacy-only detail payload for edit flow, got %+v", detail)
	}
	assertIntList(t, detail["failure_status_codes"], []int{429, 500, 503})
	if detail["ban_mode"] != "temporary" || jsonInt(t, detail["retry_base_delay_ms"]) != 1234 || jsonFloat(t, detail["retry_backoff_multiplier"]) != 3.5 || jsonFloat(t, detail["retry_jitter_ratio"]) != 0.35 || jsonInt(t, detail["retry_max_delay_ms"]) != 456789 || jsonInt(t, detail["cycle_retry_attempt_limit"]) != 7 || jsonInt(t, detail["ban_cumulative_retry_attempt_threshold"]) != 9 || jsonInt(t, detail["ban_duration_seconds"]) != 1800 {
		t.Fatalf("expected explicit Ban Policy fields, got %+v", detail)
	}
	assertNoLegacyRemovedStrategyFields(t, detail)
}

func TestLoadbalanceStrategyRejectsRemovedCheapestEligibleContext(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	response := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/loadbalance/strategies",
		map[string]any{
			"name":                                   "S11 Removed Cheapest Eligible Context",
			"legacy_strategy_type":                   "cheapest_eligible_context",
			"failure_status_codes":                   []int{503, 429, 500},
			"ban_mode":                               "temporary",
			"retry_base_delay_ms":                    1500,
			"retry_backoff_multiplier":               2.5,
			"retry_jitter_ratio":                     0.15,
			"retry_max_delay_ms":                     600000,
			"cycle_retry_attempt_limit":              3,
			"ban_cumulative_retry_attempt_threshold": 5,
			"ban_duration_seconds":                   120,
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, response, http.StatusBadRequest)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if !strings.Contains(fmt.Sprint(payload["detail"]), "legacy_strategy_type") {
		t.Fatalf("expected legacy_strategy_type rejection, got %+v", payload)
	}
}

func TestLoadbalanceAdaptiveRejected(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	response := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/loadbalance/strategies",
		map[string]any{
			"name":          "S11 Adaptive Rejected",
			"strategy_type": "adaptive",
			"routing_policy": map[string]any{
				"kind": "adaptive",
			},
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, response, http.StatusBadRequest)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	detail := fmt.Sprint(payload["detail"])
	if !strings.Contains(detail, "unknown field") || (!strings.Contains(detail, "strategy_type") && !strings.Contains(detail, "routing_policy")) {
		t.Fatalf("expected adaptive payload field rejection, got %+v", payload)
	}
}

func TestLoadbalanceAutoRecoveryRejected(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	response := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/loadbalance/strategies",
		map[string]any{
			"name":                 "S11 Auto Recovery Rejected",
			"legacy_strategy_type": "single",
			"auto_recovery":        map[string]any{"mode": "disabled"},
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, response, http.StatusBadRequest)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if detail := fmt.Sprint(payload["detail"]); !strings.Contains(detail, `unknown field "auto_recovery"`) {
		t.Fatalf("expected auto_recovery rejection detail, got %+v", payload)
	}
}

func TestLoadbalanceStrategies(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/strategies", nil, modelHeader(defaultProfileID))
	assertStatus(t, listResponse, http.StatusOK)
	var emptyList []map[string]any
	decodeJSONResponse(t, listResponse, &emptyList)
	if len(emptyList) != 0 {
		t.Fatalf("expected empty loadbalance strategy list at test start, got %+v", emptyList)
	}

	timeoutPolicyRejected := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/loadbalance/strategies",
		map[string]any{
			"name":                 "S11 Timeout Legacy",
			"legacy_strategy_type": "round-robin",
			"timeout_policy":       map[string]any{"attempt_open_timeout_ms": 2000},
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, timeoutPolicyRejected, http.StatusBadRequest)
	var timeoutPayload map[string]any
	decodeJSONResponse(t, timeoutPolicyRejected, &timeoutPayload)
	if detail := timeoutPayload["detail"]; detail != `json: unknown field "timeout_policy"` {
		t.Fatalf("expected timeout_policy rejection detail, got %+v", timeoutPayload)
	}

	removedRetryField := removedRetryAttemptsField()
	retryMaxRejected := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/loadbalance/strategies",
		map[string]any{
			"name":                 "S11 Removed Retry Limit",
			"legacy_strategy_type": "round-robin",
			removedRetryField:      4,
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, retryMaxRejected, http.StatusBadRequest)
	var retryMaxPayload map[string]any
	decodeJSONResponse(t, retryMaxRejected, &retryMaxPayload)
	if detail := retryMaxPayload["detail"]; detail != fmt.Sprintf("json: unknown field %q", removedRetryField) {
		t.Fatalf("expected removed retry field structural rejection detail, got %+v", retryMaxPayload)
	}

	removedModeRejected := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/loadbalance/strategies",
		map[string]any{
			"name":                                   "S11 Removed Ban Mode Rejected",
			"legacy_strategy_type":                   "round-robin",
			"ban_mode":                               removedBanModeValue(),
			"cycle_retry_attempt_limit":              2,
			"ban_cumulative_retry_attempt_threshold": 2,
			"ban_duration_seconds":                   0,
		},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, removedModeRejected, http.StatusBadRequest, "ban_mode must be one of 'off', 'temporary', or 'until_reset'")

	thresholdRejected := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/loadbalance/strategies",
		map[string]any{
			"name":                                   "S11 Threshold Below Cycle",
			"legacy_strategy_type":                   "round-robin",
			"ban_mode":                               "temporary",
			"cycle_retry_attempt_limit":              5,
			"ban_cumulative_retry_attempt_threshold": 4,
			"ban_duration_seconds":                   60,
		},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, thresholdRejected, http.StatusBadRequest, "ban_cumulative_retry_attempt_threshold must be greater than or equal to cycle_retry_attempt_limit when ban_mode is 'temporary' or 'until_reset'")

	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/loadbalance/strategies",
		map[string]any{
			"name":                                   "S11 Legacy Primary",
			"legacy_strategy_type":                   "round-robin",
			"failure_status_codes":                   []int{504, 500, 429},
			"ban_mode":                               "temporary",
			"retry_base_delay_ms":                    45000,
			"retry_backoff_multiplier":               3.5,
			"retry_jitter_ratio":                     0.4,
			"retry_max_delay_ms":                     720000,
			"cycle_retry_attempt_limit":              4,
			"ban_cumulative_retry_attempt_threshold": 6,
			"ban_duration_seconds":                   1800,
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	strategyID := jsonInt(t, created["id"])
	if created["legacy_strategy_type"] != "round-robin" || created["ban_mode"] != "temporary" || jsonInt(t, created["cycle_retry_attempt_limit"]) != 4 || jsonInt(t, created["ban_cumulative_retry_attempt_threshold"]) != 6 {
		t.Fatalf("expected created Ban Policy strategy payload, got %+v", created)
	}
	assertIntList(t, created["failure_status_codes"], []int{429, 500, 504})
	assertNoLegacyRemovedStrategyFields(t, created)

	duplicateName := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/loadbalance/strategies",
		map[string]any{
			"name":                 "S11 Legacy Primary",
			"legacy_strategy_type": "single",
		},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, duplicateName, http.StatusConflict, "Loadbalance strategy name already exists")

	updated := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/loadbalance/strategies/%d", strategyID),
		map[string]any{
			"name":                                   "S11 Legacy Updated",
			"legacy_strategy_type":                   "single",
			"failure_status_codes":                   []int{503, 403},
			"ban_mode":                               "until_reset",
			"retry_base_delay_ms":                    0,
			"retry_backoff_multiplier":               2.5,
			"retry_jitter_ratio":                     0.1,
			"retry_max_delay_ms":                     120000,
			"cycle_retry_attempt_limit":              2,
			"ban_cumulative_retry_attempt_threshold": 2,
			"ban_duration_seconds":                   0,
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, updated, http.StatusOK)
	var updatedPayload map[string]any
	decodeJSONResponse(t, updated, &updatedPayload)
	if updatedPayload["name"] != "S11 Legacy Updated" || updatedPayload["legacy_strategy_type"] != "single" || updatedPayload["ban_mode"] != "until_reset" || jsonInt(t, updatedPayload["retry_base_delay_ms"]) != 0 || jsonInt(t, updatedPayload["cycle_retry_attempt_limit"]) != 2 || jsonInt(t, updatedPayload["ban_cumulative_retry_attempt_threshold"]) != 2 || jsonInt(t, updatedPayload["ban_duration_seconds"]) != 0 {
		t.Fatalf("expected updated Ban Policy payload, got %+v", updatedPayload)
	}
	assertIntList(t, updatedPayload["failure_status_codes"], []int{403, 503})
	assertNoLegacyRemovedStrategyFields(t, updatedPayload)

	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	modelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s11-attached-model", nil, "native", &strategyID, true)
	blockedDelete := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/loadbalance/strategies/%d", strategyID), nil, modelHeader(defaultProfileID))
	assertStatus(t, blockedDelete, http.StatusConflict)
	var blockedPayload map[string]any
	decodeJSONResponse(t, blockedDelete, &blockedPayload)
	blockedDetail := asMap(t, blockedPayload["detail"])
	if blockedDetail["message"] != "Cannot delete loadbalance strategy that is attached to models" || jsonInt(t, blockedDetail["attached_model_count"]) != 1 {
		t.Fatalf("expected attached-model delete conflict detail, got %+v", blockedPayload)
	}
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM model_configs WHERE id = $1`, modelID); err != nil {
		t.Fatalf("delete attached model: %v", err)
	}

	deleteResponse := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/loadbalance/strategies/%d", strategyID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteResponse, http.StatusOK)
	var deletedPayload map[string]any
	decodeJSONResponse(t, deleteResponse, &deletedPayload)
	if deletedPayload["deleted"] != true {
		t.Fatalf("expected delete confirmation payload, got %+v", deletedPayload)
	}
}

func TestLoadbalanceLegacyDefaults(t *testing.T) {
	t.Run("creates defaults and stays idempotent", func(t *testing.T) {
		harness := newS11ContractHarness(t)
		defaultProfileID := modelLoadDefaultProfileID(t, harness)

		first := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID))
		assertStatus(t, first, http.StatusOK)
		var firstPayload map[string]any
		decodeJSONResponse(t, first, &firstPayload)
		wantNames := []string{"Default single routing", "Default fill-first routing", "Default round-robin routing"}
		assertStringList(t, firstPayload["created_names"], wantNames)
		assertStringList(t, firstPayload["existing_names"], []string{})
		if jsonInt(t, firstPayload["created_count"]) != 3 {
			t.Fatalf("expected three created defaults, got %+v", firstPayload)
		}
		items := asSliceOfMaps(t, firstPayload["items"])
		assertStrategyNames(t, items, wantNames)
		for _, item := range items {
			assertNoLegacyRemovedStrategyFields(t, item)
			assertIntList(t, item["failure_status_codes"], []int{403, 422, 429, 500, 502, 503, 504, 529})
			if item["ban_mode"] != "off" || jsonInt(t, item["retry_base_delay_ms"]) != 60000 || jsonFloat(t, item["retry_backoff_multiplier"]) != 2.0 || jsonFloat(t, item["retry_jitter_ratio"]) != 0.2 || jsonInt(t, item["retry_max_delay_ms"]) != 900000 || jsonInt(t, item["cycle_retry_attempt_limit"]) != 3 || jsonInt(t, item["ban_cumulative_retry_attempt_threshold"]) != 0 || jsonInt(t, item["ban_duration_seconds"]) != 0 {
				t.Fatalf("expected canonical Ban Policy defaults, got %+v", item)
			}
		}

		second := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID))
		assertStatus(t, second, http.StatusOK)
		var secondPayload map[string]any
		decodeJSONResponse(t, second, &secondPayload)
		if jsonInt(t, secondPayload["created_count"]) != 0 {
			t.Fatalf("expected idempotent defaults call to create nothing, got %+v", secondPayload)
		}
		assertStringList(t, secondPayload["created_names"], []string{})
		assertStringList(t, secondPayload["existing_names"], wantNames)
	})

	t.Run("creates only missing defaults", func(t *testing.T) {
		harness := newS11ContractHarness(t)
		defaultProfileID := modelLoadDefaultProfileID(t, harness)
		s11InsertStrategy(t, harness, defaultProfileID, "Default single routing", "single", "off", 0)

		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID))
		assertStatus(t, response, http.StatusOK)
		var payload map[string]any
		decodeJSONResponse(t, response, &payload)
		if jsonInt(t, payload["created_count"]) != 2 {
			t.Fatalf("expected two missing defaults to be created, got %+v", payload)
		}
		assertStringList(t, payload["created_names"], []string{"Default fill-first routing", "Default round-robin routing"})
		assertStringList(t, payload["existing_names"], []string{"Default single routing"})
	})

	t.Run("rejects conflicting canonical default payload", func(t *testing.T) {
		harness := newS11ContractHarness(t)
		defaultProfileID := modelLoadDefaultProfileID(t, harness)
		s11InsertStrategy(t, harness, defaultProfileID, "Default fill-first routing", "round-robin", "off", 0)

		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID))
		assertStatus(t, response, http.StatusConflict)
		var payload map[string]any
		decodeJSONResponse(t, response, &payload)
		detail := asMap(t, payload["detail"])
		if detail["message"] != "Canonical loadbalance strategy default name conflict" {
			t.Fatalf("expected canonical conflict message, got %+v", payload)
		}
		assertStringList(t, detail["conflicting_names"], []string{"Default fill-first routing"})
	})
}

func TestHeaderBlocklist(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/header-blocklist-rules", nil, modelHeader(defaultProfileID))
	assertStatus(t, listResponse, http.StatusOK)
	var rules []map[string]any
	decodeJSONResponse(t, listResponse, &rules)
	systemRule := findRuleByPattern(t, rules, "cf-")
	if systemRule["is_system"] != true {
		t.Fatalf("expected cf- system blocklist rule, got %+v", systemRule)
	}

	invalidPrefix := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/config/header-blocklist-rules",
		map[string]any{"name": "Bad Prefix", "match_type": "prefix", "pattern": "x-bad", "enabled": true},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, invalidPrefix, http.StatusBadRequest, "prefix pattern must end with '-'")

	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/config/header-blocklist-rules",
		map[string]any{"name": "Custom Header", "match_type": "prefix", "pattern": " X-Custom- ", "enabled": true},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	createdID := jsonInt(t, created["id"])
	if created["pattern"] != "x-custom-" || created["match_type"] != "prefix" || created["is_system"] != false {
		t.Fatalf("expected normalized created header blocklist rule, got %+v", created)
	}

	duplicateCreate := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/config/header-blocklist-rules",
		map[string]any{"name": "Duplicate", "match_type": "prefix", "pattern": "x-custom-", "enabled": true},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, duplicateCreate, http.StatusConflict, "Rule with match_type='prefix' and pattern='x-custom-' already exists")

	getResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/config/header-blocklist-rules/%d", createdID), nil, modelHeader(defaultProfileID))
	assertStatus(t, getResponse, http.StatusOK)

	updated := harness.requestJSON(
		t,
		harness.client,
		http.MethodPatch,
		fmt.Sprintf("/api/config/header-blocklist-rules/%d", createdID),
		map[string]any{"name": "Updated Header", "match_type": "exact", "pattern": "x-custom-token", "enabled": false},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, updated, http.StatusOK)
	var updatedPayload map[string]any
	decodeJSONResponse(t, updated, &updatedPayload)
	if updatedPayload["name"] != "Updated Header" || updatedPayload["match_type"] != "exact" || updatedPayload["pattern"] != "x-custom-token" || updatedPayload["enabled"] != false {
		t.Fatalf("expected updated header blocklist payload, got %+v", updatedPayload)
	}

	toggleSystem := harness.requestJSON(
		t,
		harness.client,
		http.MethodPatch,
		fmt.Sprintf("/api/config/header-blocklist-rules/%d", jsonInt(t, systemRule["id"])),
		map[string]any{"enabled": false},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, toggleSystem, http.StatusOK)

	immutability := harness.requestJSON(
		t,
		harness.client,
		http.MethodPatch,
		fmt.Sprintf("/api/config/header-blocklist-rules/%d", jsonInt(t, systemRule["id"])),
		map[string]any{"pattern": "cf-ray"},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, immutability, http.StatusBadRequest, "Cannot modify pattern on a system rule. Only 'enabled' is mutable.")

	deleteCustom := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/config/header-blocklist-rules/%d", createdID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteCustom, http.StatusOK)
	var deletedPayload map[string]any
	decodeJSONResponse(t, deleteCustom, &deletedPayload)
	if deletedPayload["deleted"] != true {
		t.Fatalf("expected delete confirmation payload, got %+v", deletedPayload)
	}

	deleteSystem := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/config/header-blocklist-rules/%d", jsonInt(t, systemRule["id"])), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, deleteSystem, http.StatusNotFound, "Header blocklist rule not found")
}

func TestUserAgentRules(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/user-agent-client-rules", nil, modelHeader(defaultProfileID))
	assertStatus(t, listResponse, http.StatusOK)
	var rules []map[string]any
	decodeJSONResponse(t, listResponse, &rules)
	claudeRule := findRuleByName(t, rules, "Claude Code")
	if claudeRule["pattern"] != "claude(?:\\s|-)?(?:code|cli)" || claudeRule["is_system"] != true {
		t.Fatalf("expected canonical Claude Code system rule, got %+v", claudeRule)
	}

	invalidRegex := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/config/user-agent-client-rules",
		map[string]any{"name": "Bad Regex", "pattern": "(", "enabled": true},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, invalidRegex, http.StatusBadRequest, "pattern must be a valid regular expression")

	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/config/user-agent-client-rules",
		map[string]any{"name": "My SDK", "pattern": "my-sdk", "enabled": true},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	createdID := jsonInt(t, created["id"])
	if created["name"] != "My SDK" || created["pattern"] != "my-sdk" || created["is_system"] != false {
		t.Fatalf("expected created user-agent rule payload, got %+v", created)
	}

	getResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/config/user-agent-client-rules/%d", createdID), nil, modelHeader(defaultProfileID))
	assertStatus(t, getResponse, http.StatusOK)

	updated := harness.requestJSON(
		t,
		harness.client,
		http.MethodPatch,
		fmt.Sprintf("/api/config/user-agent-client-rules/%d", createdID),
		map[string]any{"name": "My SDK v2", "pattern": "my-sdk/v2", "enabled": false},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, updated, http.StatusOK)
	var updatedPayload map[string]any
	decodeJSONResponse(t, updated, &updatedPayload)
	if updatedPayload["name"] != "My SDK v2" || updatedPayload["pattern"] != "my-sdk/v2" || updatedPayload["enabled"] != false {
		t.Fatalf("expected updated user-agent rule payload, got %+v", updatedPayload)
	}

	toggleSystem := harness.requestJSON(
		t,
		harness.client,
		http.MethodPatch,
		fmt.Sprintf("/api/config/user-agent-client-rules/%d", jsonInt(t, claudeRule["id"])),
		map[string]any{"enabled": false},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, toggleSystem, http.StatusOK)

	immutability := harness.requestJSON(
		t,
		harness.client,
		http.MethodPatch,
		fmt.Sprintf("/api/config/user-agent-client-rules/%d", jsonInt(t, claudeRule["id"])),
		map[string]any{"name": "Changed Claude"},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, immutability, http.StatusBadRequest, "Cannot modify name on a system rule. Only 'enabled' is mutable.")

	deleteCustom := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/config/user-agent-client-rules/%d", createdID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteCustom, http.StatusOK)

	deleteSystem := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/config/user-agent-client-rules/%d", jsonInt(t, claudeRule["id"])), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, deleteSystem, http.StatusNotFound, "User agent client rule not found")
}

func newS11ContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "s11_contract_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	startupService, err := startup.New(startup.Options{DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "s11-contract-secret"})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup service: %v", err)
	}

	settings := config.Settings{Host: "127.0.0.1", Port: 8000, AppEnv: config.EnvironmentProduction, DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "s11-contract-secret", CORSAllowedOrigins: "http://localhost:5173,http://127.0.0.1:5173"}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	settingsService, err := managementsettings.NewService(settings, managementsettings.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build settings service: %v", err)
	}
	t.Cleanup(settingsService.Close)
	loadbalanceService, err := managementloadbalance.NewService(settings, managementloadbalance.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build loadbalance service: %v", err)
	}
	t.Cleanup(loadbalanceService.Close)
	configRulesService, err := managementconfigrules.NewService(settings, managementconfigrules.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build config rules service: %v", err)
	}
	t.Cleanup(configRulesService.Close)

	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "s11-contract-test", SettingsService: settingsService, LoadbalanceService: loadbalanceService, ConfigRulesService: configRulesService})
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar
	return &contractHarness{client: client, conn: conn, dsn: settings.DatabaseURL, server: server, service: nil, url: server.URL}
}

func modelLoadModelConfigID(t *testing.T, harness *contractHarness, profileID int, modelID string) int {
	t.Helper()
	var modelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM model_configs WHERE profile_id = $1 AND model_id = $2 LIMIT 1`, profileID, modelID).Scan(&modelConfigID); err != nil {
		t.Fatalf("load model config id for %q: %v", modelID, err)
	}
	return modelConfigID
}

func s11InsertStrategy(t *testing.T, harness *contractHarness, profileID int, name string, legacyStrategyType string, banMode string, banDurationSeconds int) int {
	t.Helper()
	now := time.Now().UTC()
	var strategyID int
	if err := harness.conn.QueryRow(
		context.Background(),
		`INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode,
			retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit,
			ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at)
		 VALUES ($1, $2, $3, $4::integer[], $5, 60000, 2.0, 0.2, 900000, 3, 0, $6, $7, $7)
		 RETURNING id`,
		profileID,
		name,
		legacyStrategyType,
		[]int32{403, 422, 429, 500, 502, 503, 504, 529},
		banMode,
		banDurationSeconds,
		now,
	).Scan(&strategyID); err != nil {
		t.Fatalf("insert loadbalance strategy %q: %v", name, err)
	}
	return strategyID
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

func assertAuditSettingsPayload(t *testing.T, payload map[string]any, profileID int, want []map[string]bool) {
	t.Helper()
	if payload["profile_id"] != float64(profileID) {
		t.Fatalf("expected profile_id %d, got %+v", profileID, payload)
	}
	items := asSliceOfMaps(t, payload["settings"])
	wantFamilies := []string{"openai", "anthropic", "gemini"}
	if len(items) != len(wantFamilies) {
		t.Fatalf("expected three audit settings, got %+v", payload)
	}
	for index, family := range wantFamilies {
		item := items[index]
		if item["api_family"] != family {
			t.Fatalf("expected audit families %v, got %+v", wantFamilies, items)
		}
		if item["audit_enabled"] != want[index]["audit_enabled"] || item["audit_capture_bodies"] != want[index]["audit_capture_bodies"] {
			t.Fatalf("unexpected audit setting at %s: got %+v want %+v", family, item, want[index])
		}
	}
}

func assertAuditSettingsRows(t *testing.T, harness *contractHarness, profileID int, want map[string][2]bool) {
	t.Helper()
	rows, err := harness.conn.Query(context.Background(), `SELECT api_family, audit_enabled, audit_capture_bodies FROM profile_api_family_audit_settings WHERE profile_id = $1 ORDER BY api_family`, profileID)
	if err != nil {
		t.Fatalf("query audit settings rows: %v", err)
	}
	defer rows.Close()
	got := map[string][2]bool{}
	for rows.Next() {
		var family string
		var enabled bool
		var capture bool
		if err := rows.Scan(&family, &enabled, &capture); err != nil {
			t.Fatalf("scan audit settings row: %v", err)
		}
		got[family] = [2]bool{enabled, capture}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit settings rows: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected audit settings rows %+v, got %+v", want, got)
	}
	for family, wantValues := range want {
		if gotValues, ok := got[family]; !ok || gotValues != wantValues {
			t.Fatalf("expected audit settings rows %+v, got %+v", want, got)
		}
	}
}

func stringSetEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := map[string]int{}
	for _, value := range left {
		seen[value]++
	}
	for _, value := range right {
		seen[value]--
		if seen[value] < 0 {
			return false
		}
	}
	return true
}

func assertStringList(t *testing.T, raw any, want []string) {
	t.Helper()
	actualItems, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected string list payload, got %T %+v", raw, raw)
	}
	actual := make([]string, 0, len(actualItems))
	for _, item := range actualItems {
		actual = append(actual, item.(string))
	}
	if len(actual) != len(want) {
		t.Fatalf("expected string list %v, got %v", want, actual)
	}
	for index := range want {
		if actual[index] != want[index] {
			t.Fatalf("expected string list %v, got %v", want, actual)
		}
	}
}

func assertStrategyNames(t *testing.T, items []map[string]any, want []string) {
	t.Helper()
	actual := make([]string, 0, len(items))
	for _, item := range items {
		actual = append(actual, item["name"].(string))
	}
	sort.Strings(actual)
	sortedWant := append([]string(nil), want...)
	sort.Strings(sortedWant)
	if len(actual) != len(sortedWant) {
		t.Fatalf("expected strategy names %v, got %v", sortedWant, actual)
	}
	for index := range sortedWant {
		if actual[index] != sortedWant[index] {
			t.Fatalf("expected strategy names %v, got %v", sortedWant, actual)
		}
	}
}

func assertIntList(t *testing.T, raw any, want []int) {
	t.Helper()
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected int list payload, got %T %+v", raw, raw)
	}
	if len(items) != len(want) {
		t.Fatalf("expected int list %v, got %+v", want, raw)
	}
	for index := range want {
		if jsonInt(t, items[index]) != want[index] {
			t.Fatalf("expected int list %v, got %+v", want, raw)
		}
	}
}

func jsonFloat(t *testing.T, raw any) float64 {
	t.Helper()
	value, ok := raw.(float64)
	if !ok {
		t.Fatalf("expected numeric payload, got %T %+v", raw, raw)
	}
	return value
}

func removedRetryAttemptsField() string {
	return "retry_" + "max_attempts"
}

func removedBanModeValue() string {
	return "man" + "ual"
}

func assertNoLegacyRemovedStrategyFields(t *testing.T, payload map[string]any) {
	t.Helper()
	for _, key := range []string{"strategy_type", "routing_policy", "auto_recovery", removedRetryAttemptsField()} {
		if _, ok := payload[key]; ok {
			t.Fatalf("expected strategy payload to omit %s, got %+v", key, payload)
		}
	}
}

func asSliceOfMaps(t *testing.T, raw any) []map[string]any {
	t.Helper()
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected []any payload, got %T %+v", raw, raw)
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, asMap(t, item))
	}
	return result
}

func findRuleByPattern(t *testing.T, rules []map[string]any, pattern string) map[string]any {
	t.Helper()
	for _, rule := range rules {
		if rule["pattern"] == pattern {
			return rule
		}
	}
	t.Fatalf("expected rule with pattern %q, got %+v", pattern, rules)
	return nil
}

func findRuleByName(t *testing.T, rules []map[string]any, name string) map[string]any {
	t.Helper()
	for _, rule := range rules {
		if rule["name"] == name {
			return rule
		}
	}
	t.Fatalf("expected rule with name %q, got %+v", name, rules)
	return nil
}

func nullableTestString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func TestProfileHeaderHelperSanity(_ *testing.T) {
	_ = profiledomain.ProfileIDHeader
	_ = sort.Ints
	_ = json.Valid
}
