package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sort"
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

func TestLoadbalanceStrategyGet(t *testing.T) {
	harness := newS11ContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/loadbalance/strategies",
		map[string]any{
			"name":          "S11 Adaptive Detail",
			"strategy_type": "adaptive",
			"routing_policy": map[string]any{
				"kind":              "adaptive",
				"routing_objective": "maximize_availability",
				"hedge": map[string]any{
					"enabled":                 true,
					"delay_ms":                1500,
					"max_additional_attempts": 1,
				},
				"circuit_breaker": map[string]any{
					"failure_status_codes":        []int{403, 422, 429, 500, 502, 503, 504, 529},
					"base_open_seconds":           60,
					"failure_threshold":           2,
					"backoff_multiplier":          2,
					"max_open_seconds":            900,
					"jitter_ratio":                0.2,
					"ban_mode":                    "off",
					"max_open_strikes_before_ban": 0,
					"ban_duration_seconds":        0,
				},
				"admission": map[string]any{
					"respect_qps_limit":        true,
					"respect_in_flight_limits": true,
				},
			},
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
	if detail["name"] != "S11 Adaptive Detail" || detail["strategy_type"] != "adaptive" || jsonInt(t, detail["attached_model_count"]) != 0 {
		t.Fatalf("expected adaptive detail payload for edit flow, got %+v", detail)
	}
	routingPolicy := asMap(t, detail["routing_policy"])
	if routingPolicy["kind"] != "adaptive" || routingPolicy["routing_objective"] != "maximize_availability" {
		t.Fatalf("expected routing_policy detail payload, got %+v", detail)
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
			"strategy_type":        "legacy",
			"legacy_strategy_type": "round-robin",
			"auto_recovery":        map[string]any{"mode": "disabled"},
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

	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/loadbalance/strategies",
		map[string]any{
			"name":                 "S11 Legacy Primary",
			"strategy_type":        "legacy",
			"legacy_strategy_type": "round-robin",
			"auto_recovery": map[string]any{
				"mode":         "enabled",
				"status_codes": []int{403, 422, 429, 500, 502, 503, 504, 529},
				"cooldown": map[string]any{
					"base_seconds":         45,
					"failure_threshold":    4,
					"backoff_multiplier":   3.5,
					"max_cooldown_seconds": 720,
					"jitter_ratio":         0.35,
				},
				"ban": map[string]any{
					"mode":                            "temporary",
					"max_cooldown_strikes_before_ban": 3,
					"ban_duration_seconds":            1800,
				},
			},
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	strategyID := jsonInt(t, created["id"])
	if created["strategy_type"] != "legacy" || created["legacy_strategy_type"] != "round-robin" {
		t.Fatalf("expected created legacy strategy payload, got %+v", created)
	}
	if created["routing_policy"] != nil {
		t.Fatalf("expected legacy strategy response to omit routing_policy, got %+v", created)
	}

	duplicateName := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/loadbalance/strategies",
		map[string]any{
			"name":          "S11 Legacy Primary",
			"strategy_type": "adaptive",
			"routing_policy": map[string]any{
				"kind": "adaptive",
			},
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
			"name":          "S11 Adaptive Primary",
			"strategy_type": "adaptive",
			"routing_policy": map[string]any{
				"kind":              "adaptive",
				"routing_objective": "maximize_availability",
				"hedge": map[string]any{
					"enabled":                 true,
					"delay_ms":                1200,
					"max_additional_attempts": 2,
				},
				"circuit_breaker": map[string]any{
					"failure_status_codes":        []int{403, 422, 429, 500, 502, 503, 504, 529},
					"base_open_seconds":           90,
					"failure_threshold":           3,
					"backoff_multiplier":          2.5,
					"max_open_seconds":            1200,
					"jitter_ratio":                0.25,
					"ban_mode":                    "manual",
					"max_open_strikes_before_ban": 2,
					"ban_duration_seconds":        0,
				},
				"admission": map[string]any{
					"respect_qps_limit":        false,
					"respect_in_flight_limits": true,
				},
			},
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, updated, http.StatusOK)
	var updatedPayload map[string]any
	decodeJSONResponse(t, updated, &updatedPayload)
	if updatedPayload["name"] != "S11 Adaptive Primary" || updatedPayload["strategy_type"] != "adaptive" || updatedPayload["legacy_strategy_type"] != nil || updatedPayload["auto_recovery"] != nil {
		t.Fatalf("expected adaptive update payload, got %+v", updatedPayload)
	}

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

	listAfterDelete := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/strategies", nil, modelHeader(defaultProfileID))
	assertStatus(t, listAfterDelete, http.StatusOK)
	decodeJSONResponse(t, listAfterDelete, &emptyList)
	if len(emptyList) != 0 {
		t.Fatalf("expected deleted strategy to disappear from list, got %+v", emptyList)
	}
}

func TestLoadbalanceStrategyDefaults(t *testing.T) {
	t.Run("creates defaults and stays idempotent", func(t *testing.T) {
		harness := newS11ContractHarness(t)
		defaultProfileID := modelLoadDefaultProfileID(t, harness)

		first := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID))
		assertStatus(t, first, http.StatusOK)
		var firstPayload map[string]any
		decodeJSONResponse(t, first, &firstPayload)
		assertStringList(t, firstPayload["created_names"], []string{"Default legacy routing", "Default adaptive routing"})
		assertStringList(t, firstPayload["existing_names"], []string{})
		if jsonInt(t, firstPayload["created_count"]) != 2 {
			t.Fatalf("expected two created defaults, got %+v", firstPayload)
		}
		items := asSliceOfMaps(t, firstPayload["items"])
		assertStrategyNames(t, items, []string{"Default adaptive routing", "Default legacy routing"})

		second := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID))
		assertStatus(t, second, http.StatusOK)
		var secondPayload map[string]any
		decodeJSONResponse(t, second, &secondPayload)
		if jsonInt(t, secondPayload["created_count"]) != 0 {
			t.Fatalf("expected idempotent defaults call to create nothing, got %+v", secondPayload)
		}
		assertStringList(t, secondPayload["created_names"], []string{})
		assertStringList(t, secondPayload["existing_names"], []string{"Default legacy routing", "Default adaptive routing"})
	})

	t.Run("creates only missing default", func(t *testing.T) {
		harness := newS11ContractHarness(t)
		defaultProfileID := modelLoadDefaultProfileID(t, harness)
		s11InsertStrategy(t, harness, defaultProfileID, "Default legacy routing", "legacy", stringPtr("round-robin"), map[string]any{
			"mode":         "enabled",
			"status_codes": []int{403, 422, 429, 500, 502, 503, 504, 529},
			"cooldown":     map[string]any{"base_seconds": 60, "failure_threshold": 2, "backoff_multiplier": 2.0, "max_cooldown_seconds": 900, "jitter_ratio": 0.2},
			"ban":          map[string]any{"mode": "off", "max_cooldown_strikes_before_ban": 0, "ban_duration_seconds": 0},
		}, nil)

		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID))
		assertStatus(t, response, http.StatusOK)
		var payload map[string]any
		decodeJSONResponse(t, response, &payload)
		if jsonInt(t, payload["created_count"]) != 1 {
			t.Fatalf("expected one missing default to be created, got %+v", payload)
		}
		assertStringList(t, payload["created_names"], []string{"Default adaptive routing"})
		assertStringList(t, payload["existing_names"], []string{"Default legacy routing"})
	})

	t.Run("rejects canonical name conflicts", func(t *testing.T) {
		harness := newS11ContractHarness(t)
		defaultProfileID := modelLoadDefaultProfileID(t, harness)
		s11InsertStrategy(t, harness, defaultProfileID, "Default adaptive routing", "legacy", stringPtr("single"), map[string]any{"mode": "disabled"}, nil)

		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies/defaults", nil, modelHeader(defaultProfileID))
		assertStatus(t, response, http.StatusConflict)
		var payload map[string]any
		decodeJSONResponse(t, response, &payload)
		detail := asMap(t, payload["detail"])
		if detail["message"] != "Canonical loadbalance strategy default name conflict" {
			t.Fatalf("expected canonical conflict message, got %+v", payload)
		}
		assertStringList(t, detail["conflicting_names"], []string{"Default adaptive routing"})
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
	return &contractHarness{client: client, conn: conn, dsn: settings.DatabaseURL, mailer: nil, server: server, service: nil, url: server.URL}
}

func modelLoadModelConfigID(t *testing.T, harness *contractHarness, profileID int, modelID string) int {
	t.Helper()
	var modelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM model_configs WHERE profile_id = $1 AND model_id = $2 LIMIT 1`, profileID, modelID).Scan(&modelConfigID); err != nil {
		t.Fatalf("load model config id for %q: %v", modelID, err)
	}
	return modelConfigID
}

func s11InsertStrategy(t *testing.T, harness *contractHarness, profileID int, name string, strategyType string, legacyStrategyType *string, autoRecovery any, routingPolicy any) int {
	t.Helper()
	now := time.Now().UTC()
	var strategyID int
	var autoRecoveryValue any
	var routingPolicyValue any
	if autoRecovery != nil {
		autoRecoveryValue = mustModelJSON(t, autoRecovery)
	}
	if routingPolicy != nil {
		routingPolicyValue = mustModelJSON(t, routingPolicy)
	}
	if err := harness.conn.QueryRow(
		context.Background(),
		`INSERT INTO loadbalance_strategies (profile_id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8)
		 RETURNING id`,
		profileID,
		name,
		strategyType,
		nullableTestString(legacyStrategyType),
		autoRecoveryValue,
		routingPolicyValue,
		now,
		now,
	).Scan(&strategyID); err != nil {
		t.Fatalf("insert loadbalance strategy %q: %v", name, err)
	}
	return strategyID
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
	if len(actual) != len(want) {
		t.Fatalf("expected strategy names %v, got %v", want, actual)
	}
	for index := range want {
		if actual[index] != want[index] {
			t.Fatalf("expected strategy names %v, got %v", want, actual)
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
