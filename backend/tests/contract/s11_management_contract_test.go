package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
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
)

func TestCostingSettings(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	initialPayload := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/costing", nil, modelHeader(defaultProfileID), http.StatusOK)
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

	invalidMapping := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/costing", map[string]any{
		"report_currency_code":   "EUR",
		"report_currency_symbol": "€",
		"timezone_preference":    "Europe/Helsinki",
		"endpoint_fx_mappings": []map[string]any{{
			"model_id":    "missing-model",
			"endpoint_id": endpointID,
			"fx_rate":     "0.92",
		}},
	}, modelHeader(defaultProfileID))
	assertErrorResponse(t, invalidMapping, http.StatusBadRequest, fmt.Sprintf("No connection found for model_id='missing-model' and endpoint_id=%d", endpointID))
	updatedPayload, loadedPayload := putThenGetJSON(t, harness, "/api/settings/costing", map[string]any{
		"report_currency_code":   " eur ",
		"report_currency_symbol": " € ",
		"timezone_preference":    " Europe/Helsinki ",
		"endpoint_fx_mappings": []map[string]any{{
			"model_id":    "s11-costing-model",
			"endpoint_id": endpointID,
			"fx_rate":     "0.92",
		}},
	}, modelHeader(defaultProfileID))
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

	if loadedPayload["report_currency_code"] != "EUR" || loadedPayload["timezone_preference"] != "Europe/Helsinki" {
		t.Fatalf("expected costing settings round-trip to persist, got %+v", loadedPayload)
	}
}

func TestTimezoneSettings(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	payload := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/timezone", nil, modelHeader(defaultProfileID), http.StatusOK)
	if payload["profile_id"] != float64(defaultProfileID) || payload["timezone_preference"] != nil {
		t.Fatalf("expected default timezone payload, got %+v", payload)
	}

	payload = requestJSONStatus[map[string]any](t, harness, http.MethodPut, "/api/settings/timezone", map[string]any{"timezone_preference": " America/New_York "}, modelHeader(defaultProfileID), http.StatusOK)
	if payload["timezone_preference"] != "America/New_York" {
		t.Fatalf("expected trimmed timezone preference, got %+v", payload)
	}

	payload = requestJSONStatus[map[string]any](t, harness, http.MethodPut, "/api/settings/timezone", map[string]any{"timezone_preference": "   "}, modelHeader(defaultProfileID), http.StatusOK)
	if payload["timezone_preference"] != nil {
		t.Fatalf("expected blank timezone preference to clear to null, got %+v", payload)
	}
}

func TestAuditSettings(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	wantDefault := map[string][2]bool{"openai": auditExpect(false, false), "anthropic": auditExpect(false, false), "gemini": auditExpect(false, false)}
	wantUpdated := map[string][2]bool{"openai": auditExpect(true, false), "anthropic": auditExpect(false, false), "gemini": auditExpect(true, true)}
	wantOtherHeader := map[string][2]bool{"openai": auditExpect(false, false), "anthropic": auditExpect(true, true), "gemini": auditExpect(false, false)}
	payload := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/audit", nil, modelHeader(defaultProfileID), http.StatusOK)
	assertAuditSettingsPayload(t, payload, defaultProfileID, wantDefault)
	payload = requestJSONStatus[map[string]any](t, harness, http.MethodPut, "/api/settings/audit", auditSettingsRequest(
		auditSetting(" Gemini ", true, true),
		auditSetting("openai", true, false),
		auditSetting("ANTHROPIC", false, false),
	), modelHeader(defaultProfileID), http.StatusOK)
	assertAuditSettingsPayload(t, payload, defaultProfileID, wantUpdated)
	assertAuditSettingsRows(t, harness, defaultProfileID, wantUpdated)
	payload = requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/audit", nil, modelHeader(defaultProfileID), http.StatusOK)
	assertAuditSettingsPayload(t, payload, defaultProfileID, wantUpdated)
	otherProfileID := s11InsertAuditSettingsProfile(t, harness, "S11 Audit Settings Other")
	payload = requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/audit", nil, modelHeader(otherProfileID), http.StatusOK)
	assertAuditSettingsPayload(t, payload, defaultProfileID, wantUpdated)
	payload = requestJSONStatus[map[string]any](t, harness, http.MethodPut, "/api/settings/audit", auditSettingsRequest(
		auditSetting("openai", false, false),
		auditSetting("anthropic", true, true),
		auditSetting("gemini", false, false),
	), modelHeader(otherProfileID), http.StatusOK)
	assertAuditSettingsPayload(t, payload, defaultProfileID, wantOtherHeader)
	assertAuditSettingsRows(t, harness, defaultProfileID, wantOtherHeader)
	assertAuditSettingsRows(t, harness, otherProfileID, map[string][2]bool{})
	invalidRequests := []struct {
		name   string
		body   map[string]any
		detail string
	}{
		{
			name: "unknown family",
			body: auditSettingsRequest(
				auditSetting("openai", true, false),
				auditSetting("anthropic", false, false),
				auditSetting("mistral", false, false),
			),
			detail: `api_family "mistral" is not supported`,
		},
		{
			name: "duplicate family",
			body: auditSettingsRequest(
				auditSetting("openai", true, false),
				auditSetting("openai", false, false),
				auditSetting("gemini", false, false),
			),
			detail: "Duplicate audit setting for api_family=openai",
		},
		{
			name: "missing family",
			body: auditSettingsRequest(
				auditSetting("openai", true, false),
				auditSetting("anthropic", false, false),
			),
			detail: "settings must include exactly openai, anthropic, and gemini",
		},
		{
			name: "capture requires enabled",
			body: auditSettingsRequest(
				auditSetting("openai", false, true),
				auditSetting("anthropic", false, false),
				auditSetting("gemini", false, false),
			),
			detail: "audit_capture_bodies requires audit_enabled",
		},
	}
	for _, testCase := range invalidRequests {
		t.Run(testCase.name, func(t *testing.T) {
			response := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/audit", testCase.body, modelHeader(defaultProfileID))
			assertErrorResponse(t, response, http.StatusBadRequest, testCase.detail)
			assertAuditSettingsRows(t, harness, defaultProfileID, wantOtherHeader)
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

func TestAdditionalManagementRouteContracts(t *testing.T) {
	for _, tc := range []struct {
		name                string
		routePattern        string
		methods             []string
		profileScoped       bool
		invalidatesPlanning bool
	}{
		{name: "pricing-template import", routePattern: "/api/pricing-templates/import", methods: []string{http.MethodPost}, profileScoped: true, invalidatesPlanning: true},
		{name: "loadbalance incidents", routePattern: "/api/loadbalance/incidents", methods: []string{http.MethodGet}, profileScoped: true, invalidatesPlanning: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertManagementRouteContract(t, tc.routePattern, tc.methods, tc.profileScoped, tc.invalidatesPlanning, tc.name)
		})
	}
}

func TestGlobalLogRetentionSettingsAndJobs(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	assertLogRetentionPayload(t, requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/log-retention", nil, modelHeader(defaultProfileID), http.StatusOK), nil, nil, nil, nil)
	invalid := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/log-retention",
		map[string]any{"request_logs_retention_days": 0},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, invalid, http.StatusBadRequest, "request_logs_retention_days must be >= 1 when provided")
	updatedPayload, loadedPayload := putThenGetJSON(
		t,
		harness,
		"/api/settings/log-retention",
		map[string]any{
			"request_logs_retention_days":       14,
			"statistics_retention_days":         30,
			"audit_logs_retention_days":         7,
			"loadbalance_events_retention_days": 45,
		},
		modelHeader(defaultProfileID),
	)
	assertLogRetentionPayload(t, updatedPayload, intRef(14), intRef(30), intRef(7), intRef(45))
	assertLogRetentionPayload(t, loadedPayload, intRef(14), intRef(30), intRef(7), intRef(45))
	clearedPayload, loadedPayload := putThenGetJSON(
		t,
		harness,
		"/api/settings/log-retention",
		map[string]any{
			"request_logs_retention_days":       21,
			"statistics_retention_days":         nil,
			"audit_logs_retention_days":         90,
			"loadbalance_events_retention_days": nil,
		},
		modelHeader(defaultProfileID),
	)
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
	jobResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/maintenance/log-retention/jobs",
		map[string]any{"table": "request_logs", "reason": "contract guardrail"},
		withHeader(modelHeader(defaultProfileID), "Idempotency-Key", "s11-log-retention-job"),
	)
	assertStatus(t, jobResponse, http.StatusAccepted)
	jobPayload := decodeJSONMap(t, jobResponse)
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

func TestLoadbalanceStrategies(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	emptyList := requestJSONStatus[[]map[string]any](t, harness, http.MethodGet, "/api/loadbalance/strategies", nil, modelHeader(defaultProfileID), http.StatusOK)
	if len(emptyList) != 0 {
		t.Fatalf("expected empty loadbalance strategy list at test start, got %+v", emptyList)
	}

	for _, tc := range []struct {
		name            string
		body            map[string]any
		wantDetail      string
		wantContains    []string
		wantAnyContains []string
	}{
		{
			name:       "timeout policy",
			body:       map[string]any{"name": "S11 Timeout Legacy", "legacy_strategy_type": "round-robin", "timeout_policy": map[string]any{"attempt_open_timeout_ms": 2000}},
			wantDetail: `json: unknown field "timeout_policy"`,
		},
		{
			name:       "removed retry limit",
			body:       map[string]any{"name": "S11 Removed Retry Limit", "legacy_strategy_type": "round-robin", removedRetryAttemptsField(): 4},
			wantDetail: fmt.Sprintf("json: unknown field %q", removedRetryAttemptsField()),
		},
		{
			name:       "removed ban mode",
			body:       legacyStrategyPayload("S11 Removed Ban Mode Rejected", "round-robin", nil, removedBanModeValue(), 60000, 2.0, 0.2, 900000, 2, 2, 0),
			wantDetail: "ban_mode must be one of 'off', 'temporary', or 'until_reset'",
		},
		{
			name:       "threshold below cycle",
			body:       legacyStrategyPayload("S11 Threshold Below Cycle", "round-robin", nil, "temporary", 60000, 2.0, 0.2, 900000, 5, 4, 60),
			wantDetail: "ban_cumulative_retry_attempt_threshold must be greater than or equal to cycle_retry_attempt_limit when ban_mode is 'temporary' or 'until_reset'",
		},
		{
			name:         "legacy cheapest eligible context",
			body:         legacyStrategyPayload("S11 Removed Cheapest Eligible Context", "cheapest_eligible_context", []int{503, 429, 500}, "temporary", 1500, 2.5, 0.15, 600000, 3, 5, 120),
			wantContains: []string{"legacy_strategy_type"},
		},
		{
			name:            "adaptive payload",
			body:            map[string]any{"name": "S11 Adaptive Rejected", "strategy_type": "adaptive", "routing_policy": map[string]any{"kind": "adaptive"}},
			wantContains:    []string{"unknown field"},
			wantAnyContains: []string{"strategy_type", "routing_policy"},
		},
		{
			name:         "auto recovery payload",
			body:         map[string]any{"name": "S11 Auto Recovery Rejected", "legacy_strategy_type": "single", "auto_recovery": map[string]any{"mode": "disabled"}},
			wantContains: []string{`unknown field "auto_recovery"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies", tc.body, modelHeader(defaultProfileID))
			assertStatus(t, response, http.StatusBadRequest)
			detail := fmt.Sprint(decodeJSONMap(t, response)["detail"])
			if tc.wantDetail != "" && detail != tc.wantDetail {
				t.Fatalf("expected detail %q, got %q", tc.wantDetail, detail)
			}
			if len(tc.wantContains) > 0 {
				assertContainsAll(t, detail, tc.wantContains...)
			}
			if len(tc.wantAnyContains) > 0 && !containsAny(detail, tc.wantAnyContains...) {
				t.Fatalf("expected %q to contain one of %v", detail, tc.wantAnyContains)
			}
		})
	}
	created := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/strategies", legacyStrategyPayload("S11 Legacy Primary", "round-robin", []int{504, 500, 429}, "temporary", 45000, 3.5, 0.4, 720000, 4, 6, 1800), modelHeader(defaultProfileID), http.StatusCreated)
	strategyID := jsonInt(t, created["id"])
	if created["legacy_strategy_type"] != "round-robin" || created["ban_mode"] != "temporary" || jsonInt(t, created["cycle_retry_attempt_limit"]) != 4 || jsonInt(t, created["ban_cumulative_retry_attempt_threshold"]) != 6 {
		t.Fatalf("expected created Ban Policy strategy payload, got %+v", created)
	}
	assertLegacyStrategyPolicy(t, created, []int{429, 500, 504}, "temporary", 45000, 3.5, 0.4, 720000, 4, 6, 1800)
	detail := requestJSONStatus[map[string]any](t, harness, http.MethodGet, fmt.Sprintf("/api/loadbalance/strategies/%d", strategyID), nil, modelHeader(defaultProfileID), http.StatusOK)
	if detail["name"] != "S11 Legacy Primary" || detail["legacy_strategy_type"] != "round-robin" || jsonInt(t, detail["attached_model_count"]) != 0 {
		t.Fatalf("expected legacy-only detail payload for edit flow, got %+v", detail)
	}
	assertLegacyStrategyPolicy(t, detail, []int{429, 500, 504}, "temporary", 45000, 3.5, 0.4, 720000, 4, 6, 1800)
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
	updatedPayload := requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d", strategyID), legacyStrategyPayload("S11 Legacy Updated", "single", []int{503, 403}, "until_reset", 0, 2.5, 0.1, 120000, 2, 2, 0), modelHeader(defaultProfileID), http.StatusOK)
	if updatedPayload["name"] != "S11 Legacy Updated" || updatedPayload["legacy_strategy_type"] != "single" {
		t.Fatalf("expected updated Ban Policy payload, got %+v", updatedPayload)
	}
	assertLegacyStrategyPolicy(t, updatedPayload, []int{403, 503}, "until_reset", 0, 2.5, 0.1, 120000, 2, 2, 0)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	modelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s11-attached-model", nil, "native", &strategyID, true)
	blockedDelete := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/loadbalance/strategies/%d", strategyID), nil, modelHeader(defaultProfileID))
	assertStatus(t, blockedDelete, http.StatusConflict)
	blockedPayload := decodeJSONMap(t, blockedDelete)
	blockedDetail := asMap(t, blockedPayload["detail"])
	if blockedDetail["message"] != "Cannot delete loadbalance strategy that is attached to models" || jsonInt(t, blockedDetail["attached_model_count"]) != 1 {
		t.Fatalf("expected attached-model delete conflict detail, got %+v", blockedPayload)
	}
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM model_configs WHERE id = $1`, modelID); err != nil {
		t.Fatalf("delete attached model: %v", err)
	}
	assertDeletedPayload(t, requestJSONStatus[map[string]any](t, harness, http.MethodDelete, fmt.Sprintf("/api/loadbalance/strategies/%d", strategyID), nil, modelHeader(defaultProfileID), http.StatusOK))
}

func TestLoadbalanceLegacyDefaults(t *testing.T) {
	t.Run("creates defaults and stays idempotent", func(t *testing.T) {
		harness := newS11ContractHarness(t)
		defaultProfileID := modelLoadDefaultProfileID(t, harness)
		firstPayload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID), http.StatusOK)
		wantNames := []string{"Default single routing", "Default fill-first routing", "Default round-robin routing"}
		assertStringList(t, firstPayload["created_names"], wantNames)
		assertStringList(t, firstPayload["existing_names"], []string{})
		if jsonInt(t, firstPayload["created_count"]) != 3 {
			t.Fatalf("expected three created defaults, got %+v", firstPayload)
		}
		items := asSliceOfMaps(t, firstPayload["items"])
		assertStrategyNames(t, items, wantNames)
		for _, item := range items {
			assertLegacyStrategyPolicy(t, item, []int{403, 422, 429, 500, 502, 503, 504, 529}, "off", 60000, 2.0, 0.2, 900000, 3, 0, 0)
		}
		secondPayload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID), http.StatusOK)
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
		payload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID), http.StatusOK)
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
		payload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID), http.StatusConflict)
		detail := asMap(t, payload["detail"])
		if detail["message"] != "Canonical loadbalance strategy default name conflict" {
			t.Fatalf("expected canonical conflict message, got %+v", payload)
		}
		assertStringList(t, detail["conflicting_names"], []string{"Default fill-first routing"})
	})
}

func TestConfigRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec configRuleCRUDSpec
	}{
		{
			name: "header blocklist",
			spec: configRuleCRUDSpec{
				listPath:              "/api/config/header-blocklist-rules",
				systemKey:             "pattern",
				systemValue:           "cf-",
				systemWant:            map[string]any{"is_system": true},
				invalidBody:           map[string]any{"name": "Bad Prefix", "match_type": "prefix", "pattern": "x-bad", "enabled": true},
				invalidDetail:         "prefix pattern must end with '-'",
				createBody:            map[string]any{"name": "Custom Header", "match_type": "prefix", "pattern": " X-Custom- ", "enabled": true},
				createWant:            map[string]any{"pattern": "x-custom-", "match_type": "prefix", "is_system": false},
				duplicateBody:         map[string]any{"name": "Duplicate", "match_type": "prefix", "pattern": "x-custom-", "enabled": true},
				duplicateDetail:       "Rule with match_type='prefix' and pattern='x-custom-' already exists",
				updateBody:            map[string]any{"name": "Updated Header", "match_type": "exact", "pattern": "x-custom-token", "enabled": false},
				updateWant:            map[string]any{"name": "Updated Header", "match_type": "exact", "pattern": "x-custom-token", "enabled": false},
				systemImmutableBody:   map[string]any{"pattern": "cf-ray"},
				systemImmutableDetail: "Cannot modify pattern on a system rule. Only 'enabled' is mutable.",
				deleteSystemDetail:    "Header blocklist rule not found",
			},
		},
		{
			name: "user agent rules",
			spec: configRuleCRUDSpec{
				listPath:              "/api/config/user-agent-client-rules",
				systemKey:             "name",
				systemValue:           "Claude Code",
				systemWant:            map[string]any{"pattern": "claude(?:\\s|-)?(?:code|cli)", "is_system": true},
				invalidBody:           map[string]any{"name": "Bad Regex", "pattern": "(", "enabled": true},
				invalidDetail:         "pattern must be a valid regular expression",
				createBody:            map[string]any{"name": "My SDK", "pattern": "my-sdk", "enabled": true},
				createWant:            map[string]any{"name": "My SDK", "pattern": "my-sdk", "is_system": false},
				updateBody:            map[string]any{"name": "My SDK v2", "pattern": "my-sdk/v2", "enabled": false},
				updateWant:            map[string]any{"name": "My SDK v2", "pattern": "my-sdk/v2", "enabled": false},
				systemImmutableBody:   map[string]any{"name": "Changed Claude"},
				systemImmutableDetail: "Cannot modify name on a system rule. Only 'enabled' is mutable.",
				deleteSystemDetail:    "User agent client rule not found",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			harness := newS11ContractHarness(t)
			runConfigRuleCRUDContract(t, harness, modelLoadDefaultProfileID(t, harness), tc.spec)
		})
	}
}

func newS11ContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	return newContractHarnessFor(t, "s11_contract", contractHarnessOptions{
		SecretEncryptionKey: "s11-contract-secret",
		Version:             "s11-contract-test",
		DependenciesBuilder: func(t *testing.T, testContext context.Context, harness *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies {
			t.Helper()
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
			return platformhttp.Dependencies{
				SettingsService:    settingsService,
				LoadbalanceService: loadbalanceService,
				ConfigRulesService: configRulesService,
			}
		},
	})
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

func assertAuditSettingsPayload(t *testing.T, payload map[string]any, profileID int, want map[string][2]bool) {
	t.Helper()
	if payload["profile_id"] != float64(profileID) {
		t.Fatalf("expected profile_id %d, got %+v", profileID, payload)
	}
	items := asSliceOfMaps(t, payload["settings"])
	wantFamilies := []string{"openai", "anthropic", "gemini"}
	if len(items) != len(wantFamilies) || len(want) != len(wantFamilies) {
		t.Fatalf("expected three audit settings, got %+v", payload)
	}
	for index, family := range wantFamilies {
		item := items[index]
		if item["api_family"] != family {
			t.Fatalf("expected audit families %v, got %+v", wantFamilies, items)
		}
		wantValues, ok := want[family]
		if !ok || item["audit_enabled"] != wantValues[0] || item["audit_capture_bodies"] != wantValues[1] {
			t.Fatalf("unexpected audit setting at %s: got %+v want %+v", family, item, wantValues)
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

func requestJSONStatus[T any](t *testing.T, harness *contractHarness, method string, path string, body any, headers map[string]string, wantStatus int) T {
	t.Helper()
	response := harness.requestJSON(t, harness.client, method, path, body, headers)
	assertStatus(t, response, wantStatus)
	var payload T
	decodeJSONResponse(t, response, &payload)
	return payload
}

func decodeJSONMap(t *testing.T, response *http.Response) map[string]any {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return payload
}

func putThenGetJSON(t *testing.T, harness *contractHarness, path string, body map[string]any, headers map[string]string) (map[string]any, map[string]any) {
	t.Helper()
	return requestJSONStatus[map[string]any](t, harness, http.MethodPut, path, body, headers, http.StatusOK), requestJSONStatus[map[string]any](t, harness, http.MethodGet, path, nil, headers, http.StatusOK)
}

func legacyStrategyPayload(name string, legacyStrategyType string, failureStatusCodes []int, banMode string, retryBaseDelayMS int, retryBackoffMultiplier float64, retryJitterRatio float64, retryMaxDelayMS int, cycleRetryAttemptLimit int, banCumulativeRetryAttemptThreshold int, banDurationSeconds int) map[string]any {
	payload := map[string]any{
		"name":                                   name,
		"legacy_strategy_type":                   legacyStrategyType,
		"ban_mode":                               banMode,
		"retry_base_delay_ms":                    retryBaseDelayMS,
		"retry_backoff_multiplier":               retryBackoffMultiplier,
		"retry_jitter_ratio":                     retryJitterRatio,
		"retry_max_delay_ms":                     retryMaxDelayMS,
		"cycle_retry_attempt_limit":              cycleRetryAttemptLimit,
		"ban_cumulative_retry_attempt_threshold": banCumulativeRetryAttemptThreshold,
		"ban_duration_seconds":                   banDurationSeconds,
	}
	if failureStatusCodes != nil {
		payload["failure_status_codes"] = failureStatusCodes
	}
	return payload
}

func auditExpect(enabled bool, capture bool) [2]bool {
	return [2]bool{enabled, capture}
}

func auditSettingsRequest(settings ...map[string]any) map[string]any {
	return map[string]any{"settings": settings}
}

func auditSetting(apiFamily string, auditEnabled bool, auditCaptureBodies bool) map[string]any {
	return map[string]any{"api_family": apiFamily, "audit_enabled": auditEnabled, "audit_capture_bodies": auditCaptureBodies}
}

type managementRouteContractRow struct {
	RoutePattern        string   `json:"route_pattern"`
	Methods             []string `json:"methods"`
	ProfileScoped       bool     `json:"profile_scoped"`
	InvalidatesPlanning bool     `json:"invalidates_planning"`
}

func assertManagementRouteContract(t *testing.T, routePattern string, methods []string, profileScoped bool, invalidatesPlanning bool, name string) {
	t.Helper()
	raw, err := os.ReadFile("../../internal/platform/http/management_route_contract.json")
	if err != nil {
		t.Fatalf("read management route contract: %v", err)
	}
	var rows []managementRouteContractRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("parse management route contract: %v", err)
	}
	for _, row := range rows {
		if row.RoutePattern != routePattern {
			continue
		}
		if row.ProfileScoped != profileScoped || row.InvalidatesPlanning != invalidatesPlanning || !stringSetEqual(row.Methods, methods) {
			t.Fatalf("unexpected %s route contract: %+v", name, row)
		}
		return
	}
	t.Fatalf("%s route contract entry not found", routePattern)
}

func assertLogRetentionPayload(t *testing.T, payload map[string]any, requestLogsRetentionDays *int, statisticsRetentionDays *int, auditLogsRetentionDays *int, loadbalanceEventsRetentionDays *int) {
	t.Helper()
	for key, want := range map[string]*int{
		"request_logs_retention_days":       requestLogsRetentionDays,
		"statistics_retention_days":         statisticsRetentionDays,
		"audit_logs_retention_days":         auditLogsRetentionDays,
		"loadbalance_events_retention_days": loadbalanceEventsRetentionDays,
	} {
		if want == nil {
			if payload[key] != nil {
				t.Fatalf("expected %s to be null, got %+v", key, payload)
			}
			continue
		}
		if payload[key] != float64(*want) {
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

func assertLegacyStrategyPolicy(t *testing.T, payload map[string]any, failureStatusCodes []int, banMode string, retryBaseDelayMS int, retryBackoffMultiplier float64, retryJitterRatio float64, retryMaxDelayMS int, cycleRetryAttemptLimit int, banCumulativeRetryAttemptThreshold int, banDurationSeconds int) {
	t.Helper()
	assertIntList(t, payload["failure_status_codes"], failureStatusCodes)
	if payload["ban_mode"] != banMode || jsonInt(t, payload["retry_base_delay_ms"]) != retryBaseDelayMS || jsonFloat(t, payload["retry_backoff_multiplier"]) != retryBackoffMultiplier || jsonFloat(t, payload["retry_jitter_ratio"]) != retryJitterRatio || jsonInt(t, payload["retry_max_delay_ms"]) != retryMaxDelayMS || jsonInt(t, payload["cycle_retry_attempt_limit"]) != cycleRetryAttemptLimit || jsonInt(t, payload["ban_cumulative_retry_attempt_threshold"]) != banCumulativeRetryAttemptThreshold || jsonInt(t, payload["ban_duration_seconds"]) != banDurationSeconds {
		t.Fatalf("unexpected Ban Policy payload: %+v", payload)
	}
	assertNoLegacyRemovedStrategyFields(t, payload)
}

type configRuleCRUDSpec struct {
	listPath, systemKey, systemValue, invalidDetail, duplicateDetail, systemImmutableDetail, deleteSystemDetail string
	systemWant, invalidBody, createBody, createWant, duplicateBody, updateBody, updateWant, systemImmutableBody map[string]any
}

func runConfigRuleCRUDContract(t *testing.T, harness *contractHarness, profileID int, spec configRuleCRUDSpec) {
	t.Helper()
	headers := modelHeader(profileID)
	systemRule := findRule(t, requestJSONStatus[[]map[string]any](t, harness, http.MethodGet, spec.listPath, nil, headers, http.StatusOK), spec.systemKey, spec.systemValue)
	assertMapFields(t, systemRule, spec.systemWant)
	assertErrorResponse(t, harness.requestJSON(t, harness.client, http.MethodPost, spec.listPath, spec.invalidBody, headers), http.StatusBadRequest, spec.invalidDetail)
	created := requestJSONStatus[map[string]any](t, harness, http.MethodPost, spec.listPath, spec.createBody, headers, http.StatusCreated)
	assertMapFields(t, created, spec.createWant)
	createdID := jsonInt(t, created["id"])
	if spec.duplicateBody != nil {
		assertErrorResponse(t, harness.requestJSON(t, harness.client, http.MethodPost, spec.listPath, spec.duplicateBody, headers), http.StatusConflict, spec.duplicateDetail)
	}
	assertStatus(t, harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("%s/%d", spec.listPath, createdID), nil, headers), http.StatusOK)
	assertMapFields(t, requestJSONStatus[map[string]any](t, harness, http.MethodPatch, fmt.Sprintf("%s/%d", spec.listPath, createdID), spec.updateBody, headers, http.StatusOK), spec.updateWant)
	assertStatus(t, harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("%s/%d", spec.listPath, jsonInt(t, systemRule["id"])), map[string]any{"enabled": false}, headers), http.StatusOK)
	assertErrorResponse(t, harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("%s/%d", spec.listPath, jsonInt(t, systemRule["id"])), spec.systemImmutableBody, headers), http.StatusBadRequest, spec.systemImmutableDetail)
	assertDeletedPayload(t, requestJSONStatus[map[string]any](t, harness, http.MethodDelete, fmt.Sprintf("%s/%d", spec.listPath, createdID), nil, headers, http.StatusOK))
	assertErrorResponse(t, harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("%s/%d", spec.listPath, jsonInt(t, systemRule["id"])), nil, headers), http.StatusNotFound, spec.deleteSystemDetail)
}

func assertMapFields(t *testing.T, payload map[string]any, want map[string]any) {
	t.Helper()
	for key, expected := range want {
		if payload[key] != expected {
			t.Fatalf("expected %s=%v, got %+v", key, expected, payload)
		}
	}
}

func intRef(value int) *int {
	return &value
}

func stringSetEqual(left []string, right []string) bool {
	sortedLeft, sortedRight := append([]string(nil), left...), append([]string(nil), right...)
	slices.Sort(sortedLeft)
	slices.Sort(sortedRight)
	return slices.Equal(sortedLeft, sortedRight)
}

func assertStringList(t *testing.T, raw any, want []string) {
	t.Helper()
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected string list payload, got %T %+v", raw, raw)
	}
	actual := make([]string, len(items))
	for i, item := range items {
		actual[i] = item.(string)
	}
	if !slices.Equal(actual, want) {
		t.Fatalf("expected string list %v, got %v", want, actual)
	}
}

func assertStrategyNames(t *testing.T, items []map[string]any, want []string) {
	t.Helper()
	actual := make([]string, len(items))
	for i, item := range items {
		actual[i] = item["name"].(string)
	}
	sort.Strings(actual)
	want = append([]string(nil), want...)
	sort.Strings(want)
	if !slices.Equal(actual, want) {
		t.Fatalf("expected strategy names %v, got %v", want, actual)
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
	result := make([]map[string]any, len(items))
	for i, item := range items {
		result[i] = asMap(t, item)
	}
	return result
}

func findRule(t *testing.T, rules []map[string]any, key string, value string) map[string]any {
	t.Helper()
	for _, rule := range rules {
		if rule[key] == value {
			return rule
		}
	}
	t.Fatalf("expected rule with %s=%q, got %+v", key, value, rules)
	return nil
}

func nullableTestString(value *string) any {
	if value != nil {
		return *value
	}
	return nil
}
