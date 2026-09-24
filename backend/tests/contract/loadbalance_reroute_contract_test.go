package contracttest

import (
	"fmt"
	"net/http"
	"testing"
)

func TestLoadbalanceStrategyRerouteStatusCodes(t *testing.T) {
	harness := newS11ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	for _, seeded := range requestJSONStatus[[]map[string]any](t, harness, http.MethodGet, "/api/loadbalance/strategies", nil, modelHeader(profileID), http.StatusOK) {
		assertIntList(t, seeded["reroute_status_codes"], []int{400})
	}

	strategy := func(name string, failure []int, reroute any) map[string]any {
		payload := map[string]any{"name": name, "legacy_strategy_type": "fill-first", "failure_status_codes": failure}
		if reroute != nil {
			payload["reroute_status_codes"] = reroute
		}
		return payload
	}
	for _, tc := range []struct {
		name    string
		body    map[string]any
		want    []int
		wantErr string
	}{
		{name: "omitted takes the default", body: strategy("Reroute Default", []int{503}, nil), want: []int{400}},
		{name: "omitted skips a failover 400", body: strategy("Reroute Failover 400", []int{400, 503}, nil), want: []int{}},
		{name: "explicit set is sorted", body: strategy("Reroute Sorted", []int{503}, []int{404, 400}), want: []int{400, 404}},
		{name: "empty set disables", body: strategy("Reroute Disabled", []int{503}, []int{}), want: []int{}},
		{name: "5xx is rejected", body: strategy("Reroute 5xx", []int{503}, []int{500}), wantErr: "reroute_status_codes must contain only 4xx HTTP status codes other than 429"},
		{name: "429 is rejected", body: strategy("Reroute 429", []int{503}, []int{429}), wantErr: "reroute_status_codes must contain only 4xx HTTP status codes other than 429"},
		{name: "duplicates are rejected", body: strategy("Reroute Duplicate", []int{503}, []int{400, 400}), wantErr: "reroute_status_codes must not contain duplicates"},
		{name: "overlap is rejected", body: strategy("Reroute Overlap", []int{400, 503}, []int{400}), wantErr: "reroute_status_codes must not overlap failure_status_codes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantErr != "" {
				assertErrorResponse(t, harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/strategies", tc.body, modelHeader(profileID)), http.StatusBadRequest, tc.wantErr)
				for _, item := range requestJSONStatus[[]map[string]any](t, harness, http.MethodGet, "/api/loadbalance/strategies", nil, modelHeader(profileID), http.StatusOK) {
					if item["name"] == tc.body["name"] {
						t.Fatalf("expected a rejected strategy not to be written, found %+v", item)
					}
				}
				return
			}
			created := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/strategies", tc.body, modelHeader(profileID), http.StatusCreated)
			assertIntList(t, created["reroute_status_codes"], tc.want)
			detail := requestJSONStatus[map[string]any](t, harness, http.MethodGet, fmt.Sprintf("/api/loadbalance/strategies/%d", jsonInt(t, created["id"])), nil, modelHeader(profileID), http.StatusOK)
			assertIntList(t, detail["reroute_status_codes"], tc.want)
		})
	}

	created := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/strategies", strategy("Reroute Update", []int{503}, nil), modelHeader(profileID), http.StatusCreated)
	updated := requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d", jsonInt(t, created["id"])), strategy("Reroute Update", []int{503}, []int{400, 413}), modelHeader(profileID), http.StatusOK)
	assertIntList(t, updated["reroute_status_codes"], []int{400, 413})
	preview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/strategies/preview", strategy("", []int{503}, []int{413}), modelHeader(profileID), http.StatusOK)
	assertIntList(t, asMap(t, preview["normalized_policy"])["reroute_status_codes"], []int{413})
}
