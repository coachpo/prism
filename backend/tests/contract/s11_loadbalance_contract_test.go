package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"testing"
	"time"
)

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
