package contracttest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	managementconfigrules "github.com/coachpo/prism/backend/internal/httpapi/management/configrules"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
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
	if initialPayload["reporting_currency_epoch"] != "1" || initialPayload["pricing_migration_state"] != "ready" {
		t.Fatalf("expected active epoch 1 and ready migration state, got %+v", initialPayload)
	}
	if _, ok := initialPayload["endpoint_fx_mappings"]; ok {
		t.Fatalf("endpoint_fx_mappings must not exist in the steady-state response, got %+v", initialPayload)
	}

	// Legacy currency-code authoring is rejected; currency migration is the
	// only code-change path.
	codeChange := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/costing", map[string]any{
		"report_currency_code":   "EUR",
		"report_currency_symbol": "€",
		"timezone_preference":    "Europe/Helsinki",
	}, modelHeader(defaultProfileID))
	assertErrorResponse(t, codeChange, http.StatusUnprocessableEntity, "unknown_field: report_currency_code is not accepted; migrate the reporting currency through the currency migration flow")

	// FX authoring fields are rejected.
	fxAuthoring := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/costing", map[string]any{
		"report_currency_symbol": "$",
		"endpoint_fx_mappings":   []map[string]any{{"model_id": "s11-costing-model", "endpoint_id": 1, "fx_rate": "0.92"}},
	}, modelHeader(defaultProfileID))
	assertErrorResponse(t, fxAuthoring, http.StatusUnprocessableEntity, "unknown_field: endpoint_fx_mappings is not accepted; FX authoring was removed")

	// Symbol-only + timezone updates are allowed and keep the code locked.
	updatedPayload, loadedPayload := putThenGetJSON(t, harness, "/api/settings/costing", map[string]any{
		"report_currency_symbol": " US$ ",
		"timezone_preference":    " Europe/Helsinki ",
	}, modelHeader(defaultProfileID))
	if updatedPayload["profile_id"] != float64(defaultProfileID) || updatedPayload["report_currency_code"] != "USD" || updatedPayload["report_currency_symbol"] != "US$" || updatedPayload["timezone_preference"] != "Europe/Helsinki" {
		t.Fatalf("expected updated costing settings payload, got %+v", updatedPayload)
	}
	if updatedPayload["reporting_currency_epoch"] != "1" {
		t.Fatalf("expected symbol-only update to keep epoch 1, got %+v", updatedPayload)
	}
	if loadedPayload["report_currency_code"] != "USD" || loadedPayload["report_currency_symbol"] != "US$" || loadedPayload["timezone_preference"] != "Europe/Helsinki" {
		t.Fatalf("expected costing settings round-trip to persist, got %+v", loadedPayload)
	}

	// The active epoch row carries the same canonical symbol (SPEC 5.3).
	var epochSymbol string
	if err := harness.conn.QueryRow(context.Background(), `SELECT epochs.currency_symbol FROM reporting_currency_epochs AS epochs JOIN user_settings AS settings ON settings.current_reporting_currency_epoch_id = epochs.id WHERE settings.profile_id = $1`, defaultProfileID).Scan(&epochSymbol); err != nil {
		t.Fatalf("load active epoch symbol: %v", err)
	}
	if epochSymbol != "US$" {
		t.Fatalf("expected active epoch symbol to follow the settings symbol, got %q", epochSymbol)
	}

	// Stale CAS is rejected.
	stale := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/costing", map[string]any{
		"expected_updated_at":    "2000-01-01T00:00:00Z",
		"report_currency_symbol": "$",
	}, modelHeader(defaultProfileID))
	assertErrorResponse(t, stale, http.StatusConflict, "costing_settings_changed")
}

func TestTimezoneSettings(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	// Timezone is the Pricing-owned costing surface (Settings SPEC §11.1): the
	// standalone timezone route was removed; timezone_preference shares the
	// costing CAS with report currency.
	payload := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/costing", nil, modelHeader(defaultProfileID), http.StatusOK)
	if payload["timezone_preference"] != nil {
		t.Fatalf("expected default null timezone preference, got %+v", payload)
	}
	updatedAt := asString(t, payload["updated_at"])

	payload = requestJSONStatus[map[string]any](t, harness, http.MethodPut, "/api/settings/costing", map[string]any{"expected_updated_at": updatedAt, "timezone_preference": " America/New_York "}, modelHeader(defaultProfileID), http.StatusOK)
	if payload["timezone_preference"] != "America/New_York" {
		t.Fatalf("expected trimmed timezone preference, got %+v", payload)
	}
	updatedAt = asString(t, payload["updated_at"])

	payload = requestJSONStatus[map[string]any](t, harness, http.MethodPut, "/api/settings/costing", map[string]any{"expected_updated_at": updatedAt, "timezone_preference": "   "}, modelHeader(defaultProfileID), http.StatusOK)
	if payload["timezone_preference"] != nil {
		t.Fatalf("expected blank timezone preference to clear to null, got %+v", payload)
	}
}

func asString(t *testing.T, value any) string {
	t.Helper()
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	t.Fatalf("expected string, got %T %+v", value, value)
	return ""
}

func TestAuditSettings(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	wantDefault := map[string]string{"openai": "disabled", "anthropic": "disabled", "gemini": "disabled"}
	wantUpdated := map[string]string{"openai": "metadata_only", "anthropic": "disabled", "gemini": "body_capture"}
	wantOtherHeader := map[string]string{"openai": "disabled", "anthropic": "body_capture", "gemini": "disabled"}
	payload := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/audit", nil, modelHeader(defaultProfileID), http.StatusOK)
	assertAuditSettingsPayload(t, payload, wantDefault)
	payload = putAuditSettingsV2(t, harness, defaultProfileID, auditPolicy("gemini", "body_capture"), auditPolicy("openai", "metadata_only"), auditPolicy("anthropic", "disabled"))
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
	payload = putAuditSettingsV2(t, harness, otherProfileID, auditPolicy("openai", "disabled"), auditPolicy("anthropic", "body_capture"), auditPolicy("gemini", "disabled"))
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

func TestLoadbalanceStrategies(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	// Startup seeds the three canonical strategies with an explicit default:
	// steady state is a non-empty strategy set with exactly one is_default row.
	seedItems := requestJSONStatus[[]map[string]any](t, harness, http.MethodGet, "/api/loadbalance/strategies", nil, modelHeader(defaultProfileID), http.StatusOK)
	if len(seedItems) != 3 {
		t.Fatalf("expected startup to seed three canonical strategies, got %+v", seedItems)
	}
	if seedItems[0]["is_default"] != true || seedItems[0]["name"] != "Default fill-first routing" {
		t.Fatalf("expected canonical fill-first as the explicit default first in the list, got %+v", seedItems[0])
	}
	for _, item := range seedItems {
		if item["is_default"] == true && item["name"] != "Default fill-first routing" {
			t.Fatalf("expected only fill-first to be the default, got %+v", item)
		}
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
			wantDetail: `unknown field "timeout_policy"`,
		},
		{
			name:       "removed retry limit",
			body:       map[string]any{"name": "S11 Removed Retry Limit", "legacy_strategy_type": "round-robin", removedRetryAttemptsField(): 4},
			wantDetail: fmt.Sprintf("unknown field %q", removedRetryAttemptsField()),
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
	t.Run("startup seeds canonical defaults and action stays idempotent", func(t *testing.T) {
		harness := newS11ContractHarness(t)
		defaultProfileID := modelLoadDefaultProfileID(t, harness)
		wantNames := []string{"Default single routing", "Default fill-first routing", "Default round-robin routing"}
		firstPayload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID), http.StatusOK)
		assertStringSliceEqual(t, strategyDefaultsCanonicalNames(t, firstPayload["existing"]), wantNames)
		assertStringSliceEqual(t, strategyDefaultsCanonicalNames(t, firstPayload["created"]), []string{})
		if firstPayload["complete"] != true || firstPayload["default_changed"] != false {
			t.Fatalf("expected complete all-existing result with unchanged default, got %+v", firstPayload)
		}
		if jsonInt(t, firstPayload["default_strategy_id"]) <= 0 {
			t.Fatalf("expected a default strategy id, got %+v", firstPayload)
		}
		secondPayload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID), http.StatusOK)
		assertStringSliceEqual(t, strategyDefaultsCanonicalNames(t, secondPayload["created"]), []string{})
		assertStringSliceEqual(t, strategyDefaultsCanonicalNames(t, secondPayload["existing"]), wantNames)
		if jsonInt(t, secondPayload["default_strategy_id"]) != jsonInt(t, firstPayload["default_strategy_id"]) {
			t.Fatalf("expected idempotent defaults call to keep the default, got %+v then %+v", firstPayload, secondPayload)
		}
	})
	t.Run("creates only missing defaults", func(t *testing.T) {
		harness := newS11ContractHarness(t)
		defaultProfileID := modelLoadDefaultProfileID(t, harness)
		var singleID int
		for _, item := range requestJSONStatus[[]map[string]any](t, harness, http.MethodGet, "/api/loadbalance/strategies", nil, modelHeader(defaultProfileID), http.StatusOK) {
			if item["name"] == "Default single routing" {
				singleID = jsonInt(t, item["id"])
			}
		}
		requestJSONStatus[map[string]any](t, harness, http.MethodDelete, fmt.Sprintf("/api/loadbalance/strategies/%d", singleID), nil, modelHeader(defaultProfileID), http.StatusOK)
		payload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID), http.StatusOK)
		assertStringSliceEqual(t, strategyDefaultsCanonicalNames(t, payload["created"]), []string{"Default single routing"})
		assertStringSliceEqual(t, strategyDefaultsCanonicalNames(t, payload["existing"]), []string{"Default fill-first routing", "Default round-robin routing"})
		if payload["complete"] != true || payload["default_changed"] != false {
			t.Fatalf("expected repaired completeness without default change, got %+v", payload)
		}
	})
	t.Run("rejects conflicting canonical default payload", func(t *testing.T) {
		harness := newS11ContractHarness(t)
		defaultProfileID := modelLoadDefaultProfileID(t, harness)
		// Rename the seeded canonical fill-first row away, then occupy the
		// canonical name with a conflicting subtype/payload row.
		var fillFirstID int
		for _, item := range requestJSONStatus[[]map[string]any](t, harness, http.MethodGet, "/api/loadbalance/strategies", nil, modelHeader(defaultProfileID), http.StatusOK) {
			if item["name"] == "Default fill-first routing" {
				fillFirstID = jsonInt(t, item["id"])
			}
		}
		requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d", fillFirstID), legacyStrategyPayload("Custom fill-first", "fill-first", []int{403, 422, 429, 500, 502, 503, 504, 529}, "off", 60000, 2.0, 0.2, 900000, 3, 0, 0), modelHeader(defaultProfileID), http.StatusOK)
		s11InsertStrategy(t, harness, defaultProfileID, "Default fill-first routing", "round-robin", "off", 0)
		payload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID), http.StatusConflict)
		detail := asMap(t, payload["detail"])
		if detail["message"] != "Canonical loadbalance strategy default name conflict" {
			t.Fatalf("expected canonical conflict message, got %+v", payload)
		}
		assertStringSliceEqual(t, asStringSlice(t, detail["conflicting_names"]), []string{"Default fill-first routing"})
	})
}

func asStringSlice(t *testing.T, raw any) []string {
	t.Helper()
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected string slice payload, got %T %+v", raw, raw)
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.(string))
	}
	return result
}

func assertStringSliceEqual(t *testing.T, got []string, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("expected string slice %v, got %v", want, got)
	}
}

func strategyDefaultsCanonicalNames(t *testing.T, raw any) []string {
	t.Helper()
	items := asSliceOfMaps(t, raw)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, fmt.Sprint(item["canonical_name"]))
	}
	return names
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
			connectionsService, err := managementconnections.NewService(settings, managementconnections.Options{Pool: pool})
			if err != nil {
				t.Fatalf("build connections service: %v", err)
			}
			t.Cleanup(connectionsService.Close)
			return platformhttp.Dependencies{
				SettingsService:    settingsService,
				LoadbalanceService: loadbalanceService,
				ConfigRulesService: configRulesService,
				ConnectionsService: connectionsService,
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

func putAuditSettingsV2(t *testing.T, harness *contractHarness, profileID int, policies ...map[string]any) map[string]any {
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

func TestCurrencyMigrationAtomicCutover(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	profileID := defaultProfileID

	// Seed two active pricing templates with current prices.
	templateA := insertContractPricingTemplateWithPrices(t, harness, profileID, "Migrate Template A", "2", "5", "0", "0", "0")
	templateB := insertContractPricingTemplateWithPrices(t, harness, profileID, "Migrate Template B", "1", "3", "0.5", "0", "0")

	settings := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/costing", nil, modelHeader(profileID), http.StatusOK)
	updatedAt := settings["updated_at"].(string)

	// Draft creation and chunk upload keep each request bounded; preview only
	// references the sealed server-side draft.
	var preview map[string]any
	var draftID, operationID string
	if err := harness.conn.QueryRow(context.Background(), `SELECT gen_random_uuid()::text, gen_random_uuid()::text`).Scan(&draftID, &operationID); err != nil {
		t.Fatalf("generate currency draft identifiers: %v", err)
	}
	templates := requestJSONStatus[[]map[string]any](t, harness, http.MethodGet, "/api/pricing-templates", nil, modelHeader(profileID), http.StatusOK)
	if len(templates) != 2 {
		t.Fatalf("expected two active pricing templates, got %+v", templates)
	}
	draft := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/settings/costing/currency-migration-drafts", map[string]any{
		"draft_id": draftID, "migration_operation_id": operationID, "operation_kind": "currency_cutover",
		"target_currency_code": "EUR", "target_currency_symbol": "€", "expected_inventory_id": nil,
		"expected_inventory_hash": nil, "expected_inventory_generation": nil, "expected_reporting_currency_epoch": 1,
		"expected_settings_updated_at": updatedAt,
	}, modelHeader(profileID), http.StatusCreated)
	chunkItems := make([]map[string]any, 0, len(templates))
	for _, template := range templates {
		chunkItems = append(chunkItems, map[string]any{
			"template_id": template["id"], "expected_version": template["version"], "expected_updated_at": template["updated_at"],
			"input_price": template["input_price"], "output_price": template["output_price"], "cached_input_price": template["cached_input_price"],
			"cache_creation_price": template["cache_creation_price"], "reasoning_price": template["reasoning_price"],
		})
	}
	requestJSONStatus[map[string]any](t, harness, http.MethodPut, "/api/settings/costing/currency-migration-drafts/"+draftID+"/chunks/1", map[string]any{"items": chunkItems}, modelHeader(profileID), http.StatusOK)
	draft = requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/settings/costing/currency-migration-drafts/"+draftID+"/seal", nil, modelHeader(profileID), http.StatusOK)
	preview = requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/settings/costing/currency-migrations/preview", map[string]any{
		"operation_kind": "currency_cutover", "migration_operation_id": operationID, "draft_id": draftID, "draft_hash": draft["draft_hash"],
	}, modelHeader(profileID), http.StatusOK)
	if preview["current_currency_code"] != "USD" || jsonInt(t, preview["current_epoch"]) != 1 || jsonInt(t, preview["next_epoch"]) != 2 || preview["epoch_change"] != true {
		t.Fatalf("expected USD epoch 1 -> EUR epoch 2 preview, got %+v", preview)
	}
	if jsonInt(t, preview["template_count"]) != 2 || jsonInt(t, preview["revision_change_count"]) != 2 {
		t.Fatalf("expected two templates to bump versions, got %+v", preview)
	}
	previewHash := preview["preview_hash"].(string)

	// No writes happened during preview.
	var epochCount int
	if err := harness.conn.QueryRow(context.Background(), `SELECT count(*) FROM reporting_currency_epochs WHERE profile_id = $1`, profileID).Scan(&epochCount); err != nil {
		t.Fatalf("count epochs after preview: %v", err)
	}
	if epochCount != 1 {
		t.Fatalf("preview must not create epochs, got %d", epochCount)
	}
	var settingsCurrency string
	if err := harness.conn.QueryRow(context.Background(), `SELECT report_currency_code FROM user_settings WHERE profile_id = $1`, profileID).Scan(&settingsCurrency); err != nil {
		t.Fatalf("load settings currency: %v", err)
	}
	if settingsCurrency != "USD" {
		t.Fatalf("preview must not change settings currency, got %q", settingsCurrency)
	}

	// Stale commit (wrong preview hash) fails closed.
	staleCommit := harness.requestJSON(t, harness.client, http.MethodPost, "/api/settings/costing/currency-migrations/commit", map[string]any{
		"operation_kind": "currency_cutover", "migration_operation_id": operationID, "draft_id": draftID,
		"draft_hash": draft["draft_hash"], "preview_hash": "not-the-hash",
	}, modelHeader(profileID))
	assertErrorResponse(t, staleCommit, http.StatusConflict, "currency_migration_stale: preview no longer matches the sealed draft or current settings")

	// Commit atomically cuts over.
	commit := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/settings/costing/currency-migrations/commit", map[string]any{
		"operation_kind": "currency_cutover", "migration_operation_id": operationID, "draft_id": draftID,
		"draft_hash": draft["draft_hash"], "preview_hash": previewHash,
	}, modelHeader(profileID), http.StatusOK)
	if commit["new_currency_code"] != "EUR" || jsonInt(t, commit["new_epoch"]) != 2 || jsonInt(t, commit["revision_change_count"]) != 2 {
		t.Fatalf("expected EUR epoch 2 with two revisions, got %+v", commit)
	}

	// Settings + epoch pointers switched.
	var settingsCurrency2 string
	var settingsEpoch int
	if err := harness.conn.QueryRow(context.Background(), `SELECT settings.report_currency_code, epochs.epoch FROM user_settings AS settings JOIN reporting_currency_epochs AS epochs ON epochs.id = settings.current_reporting_currency_epoch_id WHERE settings.profile_id = $1`, profileID).Scan(&settingsCurrency2, &settingsEpoch); err != nil {
		t.Fatalf("load migrated settings: %v", err)
	}
	if settingsCurrency2 != "EUR" || settingsEpoch != 2 {
		t.Fatalf("expected settings to point at EUR epoch 2, got code=%q epoch=%d", settingsCurrency2, settingsEpoch)
	}

	// Old epoch superseded; new epoch active.
	var oldSuperseded bool
	var newActive bool
	if err := harness.conn.QueryRow(context.Background(), `SELECT superseded_at IS NOT NULL FROM reporting_currency_epochs WHERE profile_id = $1 AND epoch = 1`, profileID).Scan(&oldSuperseded); err != nil {
		t.Fatalf("load old epoch state: %v", err)
	}
	if err := harness.conn.QueryRow(context.Background(), `SELECT superseded_at IS NULL FROM reporting_currency_epochs WHERE profile_id = $1 AND epoch = 2`, profileID).Scan(&newActive); err != nil {
		t.Fatalf("load new epoch state: %v", err)
	}
	if !oldSuperseded || !newActive {
		t.Fatalf("expected old epoch superseded and new epoch active, got %v/%v", oldSuperseded, newActive)
	}

	// Both templates moved to v2 with EUR revisions; history retained.
	assertTemplateRevisionAtEpoch(t, harness, profileID, templateA, 2, "EUR", "2", "5")
	assertTemplateRevisionAtEpoch(t, harness, profileID, templateB, 2, "EUR", "1", "3")
	assertTemplateRevisionCount(t, harness, templateA, 2)

	// Immutable migration ledger records the cutover.
	var ledgerCount int
	var ledgerKind string
	if err := harness.conn.QueryRow(context.Background(), `SELECT count(*), operation_kind FROM currency_migration_ledger WHERE profile_id = $1 GROUP BY operation_kind`, profileID).Scan(&ledgerCount, &ledgerKind); err != nil {
		t.Fatalf("load migration ledger: %v", err)
	}
	if ledgerCount != 1 || ledgerKind != "currency_cutover" {
		t.Fatalf("expected one currency_cutover ledger, got %d %q", ledgerCount, ledgerKind)
	}

	// Costing settings now report EUR epoch 2.
	after := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/costing", nil, modelHeader(profileID), http.StatusOK)
	if after["report_currency_code"] != "EUR" || after["reporting_currency_epoch"] != "2" {
		t.Fatalf("expected costing settings to report EUR epoch 2, got %+v", after)
	}

	// A second draft with the SAME target fails closed even when there are no
	// active templates left to migrate; there is no empty-instance shortcut.
	var duplicateDraftID, duplicateOperationID string
	if err := harness.conn.QueryRow(context.Background(), `SELECT gen_random_uuid()::text, gen_random_uuid()::text`).Scan(&duplicateDraftID, &duplicateOperationID); err != nil {
		t.Fatalf("generate duplicate currency draft identifiers: %v", err)
	}
	duplicate := harness.requestJSON(t, harness.client, http.MethodPost, "/api/settings/costing/currency-migration-drafts", map[string]any{
		"draft_id": duplicateDraftID, "migration_operation_id": duplicateOperationID, "operation_kind": "currency_cutover",
		"target_currency_code": "EUR", "target_currency_symbol": "€", "expected_inventory_id": nil,
		"expected_inventory_hash": nil, "expected_inventory_generation": nil, "expected_reporting_currency_epoch": 2,
		"expected_settings_updated_at": after["updated_at"].(string),
	}, modelHeader(profileID))
	assertErrorResponse(t, duplicate, http.StatusConflict, "currency_migration_required: target currency must differ from the current reporting currency")
}

func TestCurrencyMigrationBlockedByTieredTemplate(t *testing.T) {
	harness := newS11ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	templateID := insertContractPricingTemplateWithPrices(t, harness, profileID, "Tiered Currency Guard", "2", "5", "1", "2", "3")
	now := time.Now().UTC()
	var revisionID int64
	if err := harness.conn.QueryRow(context.Background(), `WITH current_revision AS (SELECT revisions.* FROM pricing_templates AS templates JOIN pricing_template_revisions AS revisions ON revisions.id = templates.current_revision_id WHERE templates.id = $1), inserted AS (INSERT INTO pricing_template_revisions (template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, tier_input_tokens_above, tier_input_price, tier_output_price, tier_cached_input_price, tier_cache_creation_price, tier_reasoning_price, effective_at, created_at, created_by_kind, created_by_operation_id) SELECT template_id, version + 1, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, 272000, '4', '18', '2', '5', '20', $2, $2, 'legacy_backfill', NULL FROM current_revision RETURNING id) UPDATE pricing_templates SET current_revision_id = inserted.id, updated_at = $2 FROM inserted WHERE pricing_templates.id = $1 RETURNING inserted.id`, templateID, now).Scan(&revisionID); err != nil {
		t.Fatalf("attach tiered revision for currency guard: %v", err)
	}
	if revisionID < 1 {
		t.Fatalf("expected tiered revision identity, got %d", revisionID)
	}
	settings := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/settings/costing", nil, modelHeader(profileID), http.StatusOK)
	var draftID, operationID string
	if err := harness.conn.QueryRow(context.Background(), `SELECT gen_random_uuid()::text, gen_random_uuid()::text`).Scan(&draftID, &operationID); err != nil {
		t.Fatalf("generate currency guard identifiers: %v", err)
	}
	blocked := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/settings/costing/currency-migration-drafts", map[string]any{
		"draft_id": draftID, "migration_operation_id": operationID, "operation_kind": "currency_cutover",
		"target_currency_code": "EUR", "target_currency_symbol": "€", "expected_inventory_id": nil,
		"expected_inventory_hash": nil, "expected_inventory_generation": nil, "expected_reporting_currency_epoch": 1,
		"expected_settings_updated_at": settings["updated_at"],
	}, modelHeader(profileID), http.StatusConflict)
	if blocked["code"] != "currency_migration_blocked_by_tiered_templates" {
		t.Fatalf("expected tiered-template currency guard code, got %+v", blocked)
	}
	details := asMap(t, blocked["details"])
	if details["current_currency_code"] != "USD" {
		t.Fatalf("expected current currency in tier guard details, got %+v", details)
	}
	items := details["templates"].([]any)
	if len(items) != 1 || jsonInt(t, asMap(t, items[0])["template_id"]) != templateID || jsonInt(t, asMap(t, items[0])["input_tokens_above"]) != 272000 {
		t.Fatalf("expected affected tiered template evidence, got %+v", details)
	}
	tier := asMap(t, items[0])
	if tier["input_price"] != "4" || tier["output_price"] != "18" || tier["cached_input_price"] != "2" || tier["cache_creation_price"] != "5" || tier["reasoning_price"] != "20" {
		t.Fatalf("expected all five tier prices in the currency guard details, got %+v", tier)
	}
}

func assertTemplateRevisionAtEpoch(t *testing.T, harness *contractHarness, profileID int, templateID int, version int, currency string, input string, output string) {
	t.Helper()
	var gotVersion int
	var gotCurrency string
	var gotInput string
	var gotOutput string
	var gotEpoch int
	if err := harness.conn.QueryRow(context.Background(), `SELECT revisions.version, revisions.currency_code, revisions.input_price, revisions.output_price, revisions.reporting_currency_epoch FROM pricing_template_revisions AS revisions JOIN pricing_templates AS templates ON templates.current_revision_id = revisions.id WHERE templates.id = $1`, templateID).Scan(&gotVersion, &gotCurrency, &gotInput, &gotOutput, &gotEpoch); err != nil {
		t.Fatalf("load migrated template %d revision: %v", templateID, err)
	}
	if gotVersion != version || gotCurrency != currency || gotInput != input || gotOutput != output || gotEpoch != 2 {
		t.Fatalf("template %d expected v%d %s %s/%s epoch 2, got v%d %s %s/%s epoch %d", templateID, version, currency, input, output, gotVersion, gotCurrency, gotInput, gotOutput, gotEpoch)
	}
}

func assertTemplateRevisionCount(t *testing.T, harness *contractHarness, templateID int, want int) {
	t.Helper()
	var got int
	if err := harness.conn.QueryRow(context.Background(), `SELECT count(*) FROM pricing_template_revisions WHERE template_id = $1`, templateID).Scan(&got); err != nil {
		t.Fatalf("count template %d revisions: %v", templateID, err)
	}
	if got != want {
		t.Fatalf("expected %d revisions for template %d, got %d", want, templateID, got)
	}
}
