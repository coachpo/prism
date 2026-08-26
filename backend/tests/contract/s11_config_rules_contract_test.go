package contracttest

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
)

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
