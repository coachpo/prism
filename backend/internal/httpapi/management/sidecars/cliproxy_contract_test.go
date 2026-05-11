package sidecars

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	cliProxyManagementPrefix       = "/v0/management"
	cliProxyManagementKey          = "fixture-management-key"
	conditionUnobservable          = "condition_unobservable"
	maxCLIProxyContractBodyBytes   = int64(4 << 20)
	cliProxyCodeUnauthorized       = "unauthorized"
	cliProxyCodeForbidden          = "forbidden"
	cliProxyCodeManagementDisabled = "management_disabled"
	cliProxyCodeMalformedJSON      = "malformed_json"
	cliProxyCodeOversizedBody      = "oversized_body"
	cliProxyCodeTimeout            = "timeout"
	cliProxyCodeUpstreamStatus     = "upstream_status"
)

var cliProxySupportedManagementPaths = []string{
	"/auth-files",
	"/auth-files/status",
	"/auth-files/fields",
	"/gemini-api-key",
	"/claude-api-key",
	"/codex-api-key",
	"/vertex-api-key",
	"/openai-compatibility",
	"/api-call",
}

var cliProxyProviderInventoryPaths = []string{
	"/gemini-api-key",
	"/claude-api-key",
	"/codex-api-key",
	"/vertex-api-key",
	"/openai-compatibility",
}

var cliProxyProviderResponseKeys = map[string]string{
	"/gemini-api-key":       "gemini-api-key",
	"/claude-api-key":       "claude-api-key",
	"/codex-api-key":        "codex-api-key",
	"/vertex-api-key":       "vertex-api-key",
	"/openai-compatibility": "openai-compatibility",
}

func TestCLIProxyManagementContractAllowlist(t *testing.T) {
	expected := []string{
		"/auth-files",
		"/auth-files/status",
		"/auth-files/fields",
		"/gemini-api-key",
		"/claude-api-key",
		"/codex-api-key",
		"/vertex-api-key",
		"/openai-compatibility",
		"/api-call",
	}
	if !slices.Equal(cliProxySupportedManagementPaths, expected) {
		t.Fatalf("supported CLIProxyAPI management paths changed: got %v want %v", cliProxySupportedManagementPaths, expected)
	}
	if supported := SupportedCLIProxyManagementPaths(); !slices.Equal(supported, expected) {
		t.Fatalf("client supported CLIProxyAPI management paths changed: got %v want %v", supported, expected)
	}
	copied := SupportedCLIProxyManagementPaths()
	copied[0] = "/mutated"
	if SupportedCLIProxyManagementPaths()[0] != "/auth-files" {
		t.Fatalf("supported path reporting must return a defensive copy")
	}
	if slices.Contains(cliProxySupportedManagementPaths, "/usage-queue") {
		t.Fatalf("destructive /usage-queue management behavior must not be modeled as supported")
	}
}

func TestCLIProxyManagementContractAPICallWrappedPayload(t *testing.T) {
	harness := newCLIProxyContractHarness(t, map[string]cliProxyRoute{
		"/api-call": cliProxyAPICallRoute(t),
	})
	body := strings.NewReader(`{"authIndex":"auth-camel","method":"GET","url":"https://upstream.example/backend-api/wham/usage","header":{"Accept":"application/json"},"data":"{}"}`)
	var payload CLIProxyAPICallResponse
	contractErr := fetchCLIProxyJSON(context.Background(), harness.client, http.MethodPost, harness.url("/api-call"), body, cliProxyManagementHeaders(cliProxyManagementKey, ""), &payload)
	if contractErr != nil {
		t.Fatalf("POST /api-call contract request failed: %v", contractErr)
	}
	if payload.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected wrapped upstream status 429 to remain payload data, got %d", payload.StatusCode)
	}
	if got := payload.Header["Retry-After"]; len(got) != 1 || got[0] != "60" {
		t.Fatalf("expected wrapped Retry-After header, got %+v", payload.Header)
	}
	if string(payload.Body) != `"{\"plan_type\":\"plus\"}"` {
		t.Fatalf("unexpected wrapped body: %s", payload.Body)
	}
	var wrappedBody string
	if err := json.Unmarshal(payload.Body, &wrappedBody); err != nil || wrappedBody != `{"plan_type":"plus"}` {
		t.Fatalf("expected raw body to decode as wrapped string, body=%s err=%v", payload.Body, err)
	}
}

func TestCLIProxyManagementContractAuthFilesPayload(t *testing.T) {
	harness := newCLIProxyContractHarness(t, map[string]cliProxyRoute{
		"/auth-files": cliProxyFixtureRoute(t, http.StatusOK, liveAuthFilesContractFixture),
	})
	var payload cliProxyAuthFilesResponse
	contractErr := fetchCLIProxyJSON(context.Background(), harness.client, http.MethodGet, harness.url("/auth-files"), nil, cliProxyManagementHeaders(cliProxyManagementKey, ""), &payload)
	if contractErr != nil {
		t.Fatalf("GET /auth-files contract request failed: %v", contractErr)
	}
	if !jsonMatchesFixture(t, payload, liveAuthFilesContractFixture) {
		t.Fatalf("expected /auth-files payload to match frozen upstream fixture, got %+v", payload)
	}
	observations, err := validateCLIProxyAuthFilesContract(payload)
	if err != nil {
		t.Fatalf("validate /auth-files fixture: %v", err)
	}
	primary := findCLIProxyAuthObservation(t, observations, "gemini-primary.json")
	if primary.Priority == nil || *primary.Priority != 20 {
		t.Fatalf("expected gemini-primary priority=20, got %+v", primary.Priority)
	}
	deprioritized := findCLIProxyAuthObservation(t, observations, "claude-deprioritized.json")
	if deprioritized.Priority == nil || *deprioritized.Priority != 0 {
		t.Fatalf("expected priority 0 fixture row, got %+v", deprioritized.Priority)
	}
	if got := cliProxyPriorityMeaning(*deprioritized.Priority); got != "lowest/deprioritized" {
		t.Fatalf("priority 0 must mean lowest/deprioritized scheduling priority, got %q", got)
	}
	if missing := deprioritized.UnobservableFields; !slices.Equal(missing, []string{"quota", "model_states"}) {
		t.Fatalf("expected unavailable live quota/model_states to be condition_unobservable, got %+v", deprioritized)
	}
}

func TestCLIProxyManagementContractAuthFilesEnvelopeFailures(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		wantErr string
	}{
		{name: "missing files key with provider inventory is not auth success", fixture: `{"metadata":{"row_count":1},"gemini-api-key":[{"api-key":"redacted-provider-key","auth-index":"auth_001"}]}`, wantErr: "files must be present"},
		{name: "legacy auth_files only", fixture: cliProxyAuthFilesFixtureWithEnvelopeKey(t, "auth_files"), wantErr: "files must be present"},
		{name: "files null", fixture: `{"files":null,"metadata":{"row_count":0}}`, wantErr: "files must be an array"},
		{name: "files not array", fixture: `{"files":{"id":"auth-gemini-primary","name":"gemini-primary.json"},"metadata":{"row_count":1}}`, wantErr: "files must be an array"},
		{name: "empty files array succeeds", fixture: `{"files":[],"metadata":{"row_count":0}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := decodeCLIProxyAuthFilesEnvelope([]byte(tt.fixture))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected empty files array to decode successfully, got %v", err)
				}
				if len(payload.Files) != 0 {
					t.Fatalf("expected empty files array, got %+v", payload.Files)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected %q error, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestCLIProxyManagementContractAuthFilesRejectsMalformedRows(t *testing.T) {
	payload, err := decodeCLIProxyAuthFilesEnvelope([]byte(cliProxyAuthFilesFixtureWithout(t, "name")))
	if err != nil {
		t.Fatalf("decode malformed-row fixture envelope: %v", err)
	}
	_, err = validateCLIProxyAuthFilesContract(payload)
	if err == nil || !strings.Contains(err.Error(), "files[0].name is required") {
		t.Fatalf("expected malformed row rejection, got %v", err)
	}
}

func TestCLIProxyManagementContractAuthFilesDiskScanFallback(t *testing.T) {
	payload := decodeCLIProxyFixture[cliProxyAuthFilesResponse](t, diskScanAuthFilesFallbackFixture)
	observations, err := validateCLIProxyAuthFilesContract(payload)
	if err != nil {
		t.Fatalf("disk-scan degraded /auth-files fixture should remain usable: %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("expected one disk-scan observation, got %+v", observations)
	}
	observation := observations[0]
	expectedFields := []string{"quota", "model_states", "recent_requests"}
	if observation.Condition != conditionUnobservable || !slices.Equal(observation.UnobservableFields, expectedFields) {
		t.Fatalf("expected degraded disk scan fields to become condition_unobservable %v, got %+v", expectedFields, observation)
	}
	if observation.QuotaObservable || observation.ModelStatesObservable || observation.RecentRequestsObservable {
		t.Fatalf("degraded disk scan must not invent unavailable live fields, got %+v", observation)
	}
}

func TestCLIProxyManagementContractAuthStatusPatch(t *testing.T) {
	harness := newCLIProxyContractHarness(t, map[string]cliProxyRoute{
		"/auth-files/status": cliProxyStatusPatchRoute(t),
	})
	patch := strings.NewReader(`{"name":"gemini-primary.json","disabled":true}`)
	var response cliProxyAuthStatusResponse
	contractErr := fetchCLIProxyJSON(context.Background(), harness.client, http.MethodPatch, harness.url("/auth-files/status"), patch, cliProxyManagementHeaders("", cliProxyManagementKey), &response)
	if contractErr != nil {
		t.Fatalf("PATCH /auth-files/status contract request failed: %v", contractErr)
	}
	if response.Status != "ok" || !response.Disabled {
		t.Fatalf("expected source-backed status patch response, got %+v", response)
	}
}

func TestCLIProxyManagementContractEditableAuthFieldsPatch(t *testing.T) {
	harness := newCLIProxyContractHarness(t, map[string]cliProxyRoute{
		"/auth-files/fields": cliProxyFieldsPatchRoute(t),
	})
	patch := strings.NewReader(`{"name":"gemini-primary.json","prefix":"team-a/","proxy_url":"http://127.0.0.1:18080","headers":{"X-Fixture":"true"},"priority":0,"note":"deprioritized fixture"}`)
	var response cliProxyAuthFieldsResponse
	contractErr := fetchCLIProxyJSON(context.Background(), harness.client, http.MethodPatch, harness.url("/auth-files/fields"), patch, cliProxyManagementHeaders(cliProxyManagementKey, ""), &response)
	if contractErr != nil {
		t.Fatalf("PATCH /auth-files/fields contract request failed: %v", contractErr)
	}
	if response.Status != "ok" || response.Updated == "" {
		t.Fatalf("expected source-backed fields patch response, got %+v", response)
	}
	if response.Priority == nil || *response.Priority != 0 {
		t.Fatalf("expected editable fields patch to accept priority=0, got %+v", response.Priority)
	}
	if got := cliProxyPriorityMeaning(*response.Priority); got != "lowest/deprioritized" {
		t.Fatalf("priority 0 after fields PATCH must stay lowest/deprioritized, got %q", got)
	}
}

func TestCLIProxyManagementContractProviderInventory(t *testing.T) {
	harness := newCLIProxyContractHarness(t, providerInventoryRoutes(t, ""))
	collection, err := collectCLIProxyProviderInventories(context.Background(), harness.client, harness.server.URL, cliProxyManagementHeaders(cliProxyManagementKey, ""), cliProxyProviderInventoryPaths)
	if err != nil {
		t.Fatalf("collect provider inventory fixtures: %v", err)
	}
	if collection.Partial {
		t.Fatalf("expected complete provider inventory collection, got %+v", collection)
	}
	for _, path := range cliProxyProviderInventoryPaths {
		inventory, ok := collection.Inventories[path]
		if !ok {
			t.Fatalf("expected provider inventory for %s, got %+v", path, collection.Inventories)
		}
		if inventory.Priority == nil || *inventory.Priority != 10 {
			t.Fatalf("expected provider %s priority=10, got %+v", path, inventory.Priority)
		}
		if missing := inventory.UnobservableFields; !slices.Equal(missing, []string{"quota", "model_states", "recent_requests"}) {
			t.Fatalf("expected provider live fields unavailable as condition_unobservable, got %+v", inventory)
		}
	}
}

func TestCLIProxyManagementContractUnavailableFieldsAreUnobservable(t *testing.T) {
	tests := []struct {
		name  string
		field string
	}{
		{name: "missing quota", field: "quota"},
		{name: "missing priority", field: "priority"},
		{name: "missing recent_requests", field: "recent_requests"},
		{name: "missing model_states", field: "model_states"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := cliProxyAuthFilesFixtureWithout(t, tt.field)
			payload := decodeCLIProxyFixture[cliProxyAuthFilesResponse](t, fixture)
			observations, err := validateCLIProxyAuthFilesContract(payload)
			if err != nil {
				t.Fatalf("missing optional live field %s should not fail contract: %v", tt.field, err)
			}
			observation := findCLIProxyAuthObservation(t, observations, "gemini-primary.json")
			if observation.Condition != conditionUnobservable || !slices.Contains(observation.UnobservableFields, tt.field) {
				t.Fatalf("expected missing %s to become condition_unobservable, got %+v", tt.field, observation)
			}
		})
	}
}

func TestCLIProxyManagementContractHTTPFailures(t *testing.T) {
	tests := []struct {
		name          string
		route         cliProxyRoute
		headers       map[string]string
		clientTimeout time.Duration
		wantCode      string
	}{
		{name: "401 missing management auth", route: cliProxyFixtureRoute(t, http.StatusOK, liveAuthFilesContractFixture), wantCode: cliProxyCodeUnauthorized},
		{name: "403 wrong management auth", route: cliProxyFixtureRoute(t, http.StatusOK, liveAuthFilesContractFixture), headers: cliProxyManagementHeaders("wrong-key", ""), wantCode: cliProxyCodeForbidden},
		{name: "404 management disabled", headers: cliProxyManagementHeaders(cliProxyManagementKey, ""), wantCode: cliProxyCodeManagementDisabled},
		{name: "malformed JSON", route: cliProxyMalformedJSONRoute, headers: cliProxyManagementHeaders(cliProxyManagementKey, ""), wantCode: cliProxyCodeMalformedJSON},
		{name: "oversized body", route: cliProxyOversizedBodyRoute, headers: cliProxyManagementHeaders(cliProxyManagementKey, ""), wantCode: cliProxyCodeOversizedBody},
		{name: "timeout", route: cliProxyTimeoutRoute, headers: cliProxyManagementHeaders(cliProxyManagementKey, ""), clientTimeout: 10 * time.Millisecond, wantCode: cliProxyCodeTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes := map[string]cliProxyRoute{}
			if tt.route != nil {
				routes["/auth-files"] = tt.route
			}
			harness := newCLIProxyContractHarness(t, routes)
			client := harness.client
			if tt.clientTimeout > 0 {
				client = &http.Client{Timeout: tt.clientTimeout}
			}
			var payload map[string]any
			contractErr := fetchCLIProxyJSON(context.Background(), client, http.MethodGet, harness.url("/auth-files"), nil, tt.headers, &payload)
			if contractErr == nil {
				t.Fatalf("expected %s failure", tt.wantCode)
			}
			if contractErr.Code != tt.wantCode {
				t.Fatalf("expected %s failure, got %s (%v)", tt.wantCode, contractErr.Code, contractErr)
			}
		})
	}
}

func TestCLIProxyManagementContractProviderPartialFailure(t *testing.T) {
	harness := newCLIProxyContractHarness(t, providerInventoryRoutes(t, "/claude-api-key"))
	collection, err := collectCLIProxyProviderInventories(context.Background(), harness.client, harness.server.URL, cliProxyManagementHeaders(cliProxyManagementKey, ""), cliProxyProviderInventoryPaths)
	if err != nil {
		t.Fatalf("partial provider failures should not fail the whole collection: %v", err)
	}
	if !collection.Partial {
		t.Fatalf("expected partial provider inventory collection, got %+v", collection)
	}
	if _, ok := collection.Inventories["/gemini-api-key"]; !ok {
		t.Fatalf("expected successful provider endpoint to remain observable, got %+v", collection.Inventories)
	}
	if _, ok := collection.Inventories["/claude-api-key"]; ok {
		t.Fatalf("failed provider endpoint must not invent inventory, got %+v", collection.Inventories)
	}
	if got := collection.PathConditions["/claude-api-key"]; got != conditionUnobservable {
		t.Fatalf("expected failed provider endpoint to become condition_unobservable, got %q", got)
	}
}

type cliProxyRoute func(http.ResponseWriter, *http.Request)

type cliProxyContractHarness struct {
	client        *http.Client
	server        *httptest.Server
	managementKey string
}

func newCLIProxyContractHarness(t *testing.T, routes map[string]cliProxyRoute) *cliProxyContractHarness {
	t.Helper()
	harness := &cliProxyContractHarness{client: http.DefaultClient, managementKey: cliProxyManagementKey}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, cliProxyManagementPrefix) {
			http.NotFound(w, r)
			return
		}
		if !cliProxyAuthorized(r, harness.managementKey) {
			writeCLIProxyFixtureJSON(w, cliProxyAuthFailureStatus(r), `{"error":"management auth required"}`)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, cliProxyManagementPrefix)
		route, ok := routes[path]
		if !ok {
			writeCLIProxyFixtureJSON(w, http.StatusNotFound, `{"error":"management API disabled"}`)
			return
		}
		route(w, r)
	})
	harness.server = httptest.NewServer(handler)
	t.Cleanup(harness.server.Close)
	return harness
}

func (h *cliProxyContractHarness) url(path string) string {
	return h.server.URL + cliProxyManagementPrefix + path
}

func cliProxyAuthorized(r *http.Request, managementKey string) bool {
	return r.Header.Get("X-Management-Key") == managementKey || r.Header.Get("Authorization") == "Bearer "+managementKey
}

func cliProxyAuthFailureStatus(r *http.Request) int {
	if r.Header.Get("X-Management-Key") == "" && r.Header.Get("Authorization") == "" {
		return http.StatusUnauthorized
	}
	return http.StatusForbidden
}

func cliProxyManagementHeaders(xManagementKey string, bearerKey string) map[string]string {
	headers := map[string]string{}
	if xManagementKey != "" {
		headers["X-Management-Key"] = xManagementKey
	}
	if bearerKey != "" {
		headers["Authorization"] = "Bearer " + bearerKey
	}
	return headers
}

func cliProxyFixtureRoute(t *testing.T, status int, fixture string) cliProxyRoute {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		writeCLIProxyFixtureJSON(w, status, fixture)
	}
}

func cliProxyAPICallRoute(t *testing.T) cliProxyRoute {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeCLIProxyFixtureJSON(w, http.StatusMethodNotAllowed, `{"error":"POST required"}`)
			return
		}
		var request CLIProxyAPICallRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeCLIProxyFixtureJSON(w, http.StatusBadRequest, `{"error":"invalid request body"}`)
			return
		}
		if request.AuthIndex != "auth-camel" || request.Method != http.MethodGet || request.URL != "https://upstream.example/backend-api/wham/usage" || request.Header["Accept"] != "application/json" || request.Data != "{}" {
			t.Fatalf("unexpected wrapped api-call request: %+v", request)
		}
		writeCLIProxyFixtureJSON(w, http.StatusOK, `{"status_code":429,"header":{"Retry-After":["60"],"Content-Type":["application/json"]},"body":"{\"plan_type\":\"plus\"}"}`)
	}
}

func cliProxyStatusPatchRoute(t *testing.T) cliProxyRoute {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeCLIProxyFixtureJSON(w, http.StatusMethodNotAllowed, `{"error":"PATCH required"}`)
			return
		}
		var patch cliProxyAuthStatusPatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeCLIProxyFixtureJSON(w, http.StatusBadRequest, `{"error":"invalid request body"}`)
			return
		}
		if patch.Name != "gemini-primary.json" || patch.Disabled == nil || !*patch.Disabled {
			t.Fatalf("unexpected source-backed status patch payload: %+v", patch)
		}
		writeCLIProxyFixtureJSON(w, http.StatusOK, `{"status":"ok","disabled":true}`)
	}
}

func cliProxyFieldsPatchRoute(t *testing.T) cliProxyRoute {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeCLIProxyFixtureJSON(w, http.StatusMethodNotAllowed, `{"error":"PATCH required"}`)
			return
		}
		var patch cliProxyAuthFieldsPatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeCLIProxyFixtureJSON(w, http.StatusBadRequest, `{"error":"invalid request body"}`)
			return
		}
		if patch.Name != "gemini-primary.json" || patch.Priority == nil || *patch.Priority != 0 || patch.Headers["X-Fixture"] != "true" {
			t.Fatalf("unexpected source-backed fields patch payload: %+v", patch)
		}
		writeCLIProxyFixtureJSON(w, http.StatusOK, `{"status":"ok","updated":"gemini-primary.json","priority":0}`)
	}
}

func writeCLIProxyFixtureJSON(w http.ResponseWriter, status int, fixture string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(fixture))
}

func cliProxyMalformedJSONRoute(w http.ResponseWriter, _ *http.Request) {
	writeCLIProxyFixtureJSON(w, http.StatusOK, `{"files":`)
}

func cliProxyOversizedBodyRoute(w http.ResponseWriter, _ *http.Request) {
	writeCLIProxyFixtureJSON(w, http.StatusOK, strings.Repeat(" ", int(maxCLIProxyContractBodyBytes)+1))
}

func cliProxyTimeoutRoute(w http.ResponseWriter, _ *http.Request) {
	time.Sleep(100 * time.Millisecond)
	writeCLIProxyFixtureJSON(w, http.StatusOK, liveAuthFilesContractFixture)
}

type cliProxyContractError struct {
	Code   string
	Status int
	Path   string
	Err    error
}

func (err *cliProxyContractError) Error() string {
	if err.Err != nil {
		return fmt.Sprintf("%s %s: %v", err.Code, err.Path, err.Err)
	}
	return fmt.Sprintf("%s %s status=%d", err.Code, err.Path, err.Status)
}

func fetchCLIProxyJSON(ctx context.Context, client *http.Client, method string, url string, body io.Reader, headers map[string]string, target any) *cliProxyContractError {
	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return &cliProxyContractError{Code: "request_build", Path: url, Err: err}
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) || strings.Contains(err.Error(), "Client.Timeout") {
			return &cliProxyContractError{Code: cliProxyCodeTimeout, Path: url, Err: err}
		}
		return &cliProxyContractError{Code: "request_failed", Path: url, Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return &cliProxyContractError{Code: cliProxyCodeUnauthorized, Status: response.StatusCode, Path: url}
	}
	if response.StatusCode == http.StatusForbidden {
		return &cliProxyContractError{Code: cliProxyCodeForbidden, Status: response.StatusCode, Path: url}
	}
	if response.StatusCode == http.StatusNotFound {
		return &cliProxyContractError{Code: cliProxyCodeManagementDisabled, Status: response.StatusCode, Path: url}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &cliProxyContractError{Code: cliProxyCodeUpstreamStatus, Status: response.StatusCode, Path: url}
	}
	limitedBody, err := io.ReadAll(io.LimitReader(response.Body, maxCLIProxyContractBodyBytes+1))
	if err != nil {
		return &cliProxyContractError{Code: "read_failed", Path: url, Err: err}
	}
	if int64(len(limitedBody)) > maxCLIProxyContractBodyBytes {
		return &cliProxyContractError{Code: cliProxyCodeOversizedBody, Path: url}
	}
	if err := json.Unmarshal(limitedBody, target); err != nil {
		return &cliProxyContractError{Code: cliProxyCodeMalformedJSON, Path: url, Err: err}
	}
	return nil
}

type cliProxyAuthFilesResponse struct {
	Files []cliProxyAuthFile `json:"files"`
}

type cliProxyAuthFile struct {
	ID             string                  `json:"id,omitempty"`
	AuthIndex      string                  `json:"authIndex,omitempty"`
	Name           string                  `json:"name"`
	Type           string                  `json:"type,omitempty"`
	Provider       string                  `json:"provider,omitempty"`
	Label          string                  `json:"label,omitempty"`
	Status         string                  `json:"status,omitempty"`
	StatusMessage  string                  `json:"status_message,omitempty"`
	Disabled       bool                    `json:"disabled"`
	Unavailable    bool                    `json:"unavailable"`
	RuntimeOnly    bool                    `json:"runtime_only,omitempty"`
	Source         string                  `json:"source,omitempty"`
	Size           int64                   `json:"size,omitempty"`
	Path           string                  `json:"path,omitempty"`
	Priority       *int                    `json:"priority,omitempty"`
	Success        int                     `json:"success,omitempty"`
	Failed         int                     `json:"failed,omitempty"`
	RecentRequests *[]cliProxyRecentBucket `json:"recent_requests,omitempty"`
	Note           string                  `json:"note,omitempty"`
	Quota          *json.RawMessage        `json:"quota,omitempty"`
	ModelStates    *json.RawMessage        `json:"model_states,omitempty"`
}

type cliProxyRecentBucket struct {
	WindowStart  string `json:"window_start,omitempty"`
	WindowEnd    string `json:"window_end,omitempty"`
	SuccessCount int    `json:"success_count,omitempty"`
	FailureCount int    `json:"failure_count,omitempty"`
}

type cliProxyAuthFileObservation struct {
	Name                     string
	Priority                 *int
	QuotaObservable          bool
	ModelStatesObservable    bool
	RecentRequestsObservable bool
	Condition                string
	UnobservableFields       []string
}

type cliProxyAuthStatusPatch struct {
	Name     string `json:"name"`
	Disabled *bool  `json:"disabled"`
}

type cliProxyAuthStatusResponse struct {
	Status   string `json:"status"`
	Disabled bool   `json:"disabled"`
}

type cliProxyAuthFieldsPatch struct {
	Name     string            `json:"name"`
	Prefix   *string           `json:"prefix"`
	ProxyURL *string           `json:"proxy_url"`
	Headers  map[string]string `json:"headers"`
	Priority *int              `json:"priority"`
	Note     *string           `json:"note"`
}

type cliProxyAuthFieldsResponse struct {
	Status   string `json:"status"`
	Updated  string `json:"updated"`
	Priority *int   `json:"priority,omitempty"`
}

type cliProxyProviderInventory struct {
	Path                     string
	ResponseKey              string
	Items                    []map[string]any
	Priority                 *int
	Condition                string
	UnobservableFields       []string
	QuotaObservable          bool
	ModelStatesObservable    bool
	RecentRequestsObservable bool
}

type cliProxyProviderInventoryCollection struct {
	Inventories    map[string]cliProxyProviderInventory
	Partial        bool
	PathConditions map[string]string
}

func validateCLIProxyAuthFilesContract(payload cliProxyAuthFilesResponse) ([]cliProxyAuthFileObservation, error) {
	if len(payload.Files) == 0 {
		return nil, fmt.Errorf("files must include at least one entry")
	}
	observations := make([]cliProxyAuthFileObservation, 0, len(payload.Files))
	for index, authFile := range payload.Files {
		if authFile.Name == "" {
			return nil, fmt.Errorf("files[%d].name is required", index)
		}
		if authFile.Source == "memory" && authFile.ID == "" {
			return nil, fmt.Errorf("files[%d].id is required for memory auth-manager entries", index)
		}
		missing := missingCLIProxyLiveFields(authFile)
		observation := cliProxyAuthFileObservation{
			Name:                     authFile.Name,
			Priority:                 authFile.Priority,
			QuotaObservable:          authFile.Quota != nil,
			ModelStatesObservable:    authFile.ModelStates != nil,
			RecentRequestsObservable: authFile.RecentRequests != nil,
		}
		if authFile.Priority != nil && *authFile.Priority < 0 {
			return nil, fmt.Errorf("files[%d].priority must be non-negative", index)
		}
		if len(missing) > 0 {
			observation.Condition = conditionUnobservable
			observation.UnobservableFields = missing
		}
		observations = append(observations, observation)
	}
	return observations, nil
}

func missingCLIProxyLiveFields(authFile cliProxyAuthFile) []string {
	missing := []string{}
	if authFile.Priority == nil {
		missing = append(missing, "priority")
	}
	if authFile.Quota == nil {
		missing = append(missing, "quota")
	}
	if authFile.ModelStates == nil {
		missing = append(missing, "model_states")
	}
	if authFile.RecentRequests == nil {
		missing = append(missing, "recent_requests")
	}
	return missing
}

func validateCLIProxyProviderInventoryContract(path string, raw map[string]any) (cliProxyProviderInventory, error) {
	responseKey, ok := cliProxyProviderResponseKeys[path]
	if !ok {
		return cliProxyProviderInventory{}, fmt.Errorf("unsupported provider inventory path %s", path)
	}
	itemsRaw, ok := raw[responseKey]
	if !ok {
		return cliProxyProviderInventory{}, fmt.Errorf("provider inventory %s missing top-level key %q", path, responseKey)
	}
	items, ok := itemsRaw.([]any)
	if !ok || len(items) == 0 {
		return cliProxyProviderInventory{}, fmt.Errorf("provider inventory %s must include non-empty %q array", path, responseKey)
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		return cliProxyProviderInventory{}, fmt.Errorf("provider inventory %s first item has unexpected shape", path)
	}
	if _, ok := first["api-key"]; !ok && path != "/openai-compatibility" {
		return cliProxyProviderInventory{}, fmt.Errorf("provider inventory %s first item missing api-key", path)
	}
	priority, priorityOK := intFromJSONNumber(first["priority"])
	if !priorityOK {
		return cliProxyProviderInventory{}, fmt.Errorf("provider inventory %s first item missing priority", path)
	}
	if priority < 0 {
		return cliProxyProviderInventory{}, fmt.Errorf("provider inventory %s priority must be non-negative", path)
	}
	inventory := cliProxyProviderInventory{
		Path:                     path,
		ResponseKey:              responseKey,
		Items:                    []map[string]any{first},
		Priority:                 &priority,
		Condition:                conditionUnobservable,
		UnobservableFields:       []string{"quota", "model_states", "recent_requests"},
		QuotaObservable:          first["quota"] != nil,
		ModelStatesObservable:    first["model_states"] != nil,
		RecentRequestsObservable: first["recent_requests"] != nil,
	}
	if inventory.QuotaObservable || inventory.ModelStatesObservable || inventory.RecentRequestsObservable {
		inventory.Condition = ""
		inventory.UnobservableFields = nil
	}
	return inventory, nil
}

func intFromJSONNumber(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	default:
		return 0, false
	}
}

func collectCLIProxyProviderInventories(ctx context.Context, client *http.Client, serverURL string, headers map[string]string, paths []string) (cliProxyProviderInventoryCollection, error) {
	collection := cliProxyProviderInventoryCollection{
		Inventories:    map[string]cliProxyProviderInventory{},
		PathConditions: map[string]string{},
	}
	for _, path := range paths {
		var raw map[string]any
		contractErr := fetchCLIProxyJSON(ctx, client, http.MethodGet, serverURL+cliProxyManagementPrefix+path, nil, headers, &raw)
		if contractErr != nil {
			collection.Partial = true
			collection.PathConditions[path] = conditionUnobservable
			continue
		}
		inventory, err := validateCLIProxyProviderInventoryContract(path, raw)
		if err != nil {
			return collection, err
		}
		collection.Inventories[path] = inventory
	}
	return collection, nil
}

func providerInventoryRoutes(t *testing.T, failingPath string) map[string]cliProxyRoute {
	t.Helper()
	routes := map[string]cliProxyRoute{}
	for _, path := range cliProxyProviderInventoryPaths {
		if path == failingPath {
			routes[path] = cliProxyFixtureRoute(t, http.StatusBadGateway, `{"error":"provider inventory unavailable"}`)
			continue
		}
		routes[path] = cliProxyFixtureRoute(t, http.StatusOK, providerInventoryFixture(t, path))
	}
	return routes
}

func providerInventoryFixture(t *testing.T, path string) string {
	t.Helper()
	fixtureByPath := map[string]string{
		"/gemini-api-key":       geminiProviderInventoryFixture,
		"/claude-api-key":       claudeProviderInventoryFixture,
		"/codex-api-key":        codexProviderInventoryFixture,
		"/vertex-api-key":       vertexProviderInventoryFixture,
		"/openai-compatibility": openAICompatibilityInventoryFixture,
	}
	fixture, ok := fixtureByPath[path]
	if !ok {
		t.Fatalf("unsupported provider inventory path %s", path)
	}
	assertProviderFixtureHasNoRawSecretMaterial(t, []byte(fixture))
	return fixture
}

func assertProviderFixtureHasNoRawSecretMaterial(t *testing.T, raw []byte) {
	t.Helper()
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode provider fixture for secret scan: %v", err)
	}
	walkJSONMaps(payload, func(key string, value any) {
		if key != "api-key" {
			return
		}
		secret, ok := value.(string)
		if !ok || !strings.HasPrefix(secret, "redacted-") {
			t.Fatalf("provider inventory fixture must use redacted fake api-key values, got %q", secret)
		}
	})
}

func walkJSONMaps(value any, visit func(string, any)) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			visit(key, nested)
			walkJSONMaps(nested, visit)
		}
	case []any:
		for _, nested := range typed {
			walkJSONMaps(nested, visit)
		}
	}
}

func findCLIProxyAuthObservation(t *testing.T, observations []cliProxyAuthFileObservation, name string) cliProxyAuthFileObservation {
	t.Helper()
	for _, observation := range observations {
		if observation.Name == name {
			return observation
		}
	}
	t.Fatalf("missing auth file observation %s in %+v", name, observations)
	return cliProxyAuthFileObservation{}
}

func cliProxyPriorityMeaning(priority int) string {
	if priority == 0 {
		return "lowest/deprioritized"
	}
	return "higher scheduling precedence"
}

func decodeCLIProxyFixture[T any](t *testing.T, fixture string) T {
	t.Helper()
	var payload T
	if err := json.Unmarshal([]byte(fixture), &payload); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return payload
}

func jsonMatchesFixture[T any](t *testing.T, payload T, fixture string) bool {
	t.Helper()
	expected := decodeCLIProxyFixture[T](t, fixture)
	return reflect.DeepEqual(payload, expected)
}

func decodeCLIProxyAuthFilesEnvelope(raw []byte) (cliProxyAuthFilesResponse, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return cliProxyAuthFilesResponse{}, err
	}
	filesRaw, ok := envelope["files"]
	if !ok {
		return cliProxyAuthFilesResponse{}, fmt.Errorf("files must be present")
	}
	if len(filesRaw) == 0 || string(filesRaw) == "null" || filesRaw[0] != '[' {
		return cliProxyAuthFilesResponse{}, fmt.Errorf("files must be an array")
	}
	var payload cliProxyAuthFilesResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return cliProxyAuthFilesResponse{}, err
	}
	if payload.Files == nil {
		return payload, fmt.Errorf("files must be an array")
	}
	return payload, nil
}

func cliProxyAuthFilesFixtureWithEnvelopeKey(t *testing.T, key string) string {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(liveAuthFilesContractFixture), &payload); err != nil {
		t.Fatalf("decode auth-files fixture: %v", err)
	}
	files, ok := payload["files"]
	if !ok {
		t.Fatalf("auth-files fixture missing files key")
	}
	encoded, err := json.Marshal(map[string]json.RawMessage{
		key:        files,
		"metadata": json.RawMessage(`{"row_count":2,"generated_at":"2026-05-10T17:00:00Z"}`),
	})
	if err != nil {
		t.Fatalf("encode auth-files fixture with %s key: %v", key, err)
	}
	return string(encoded)
}

func cliProxyAuthFilesFixtureWithout(t *testing.T, field string) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(liveAuthFilesContractFixture), &payload); err != nil {
		t.Fatalf("decode auth-files fixture: %v", err)
	}
	files, ok := payload["files"].([]any)
	if !ok || len(files) == 0 {
		t.Fatalf("auth-files fixture has unexpected shape: %+v", payload)
	}
	first, ok := files[0].(map[string]any)
	if !ok {
		t.Fatalf("auth-files fixture first row has unexpected shape: %+v", files[0])
	}
	delete(first, field)
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode auth-files fixture without %s: %v", field, err)
	}
	return string(encoded)
}

const liveAuthFilesContractFixture = `{
  "files": [
    {
      "id": "auth-gemini-primary",
      "auth_index": "auth_001",
      "name": "gemini-primary.json",
      "type": "gemini",
      "provider": "gemini",
      "label": "Gemini primary",
      "status": "active",
      "status_message": "",
      "disabled": false,
      "unavailable": false,
      "runtime_only": false,
      "source": "file",
      "size": 512,
      "path": "/mock/cliproxy/auth/gemini-primary.json",
      "priority": 20,
      "success": 4,
      "failed": 0,
      "recent_requests": [
        {
          "window_start": "2026-05-10T17:00:00Z",
          "window_end": "2026-05-10T17:01:00Z",
          "success_count": 4,
          "failure_count": 0
        }
      ],
      "note": "source-backed fixture row"
    },
    {
      "id": "auth-claude-deprioritized",
      "auth_index": "auth_002",
      "name": "claude-deprioritized.json",
      "type": "claude",
      "provider": "claude",
      "label": "Claude deprioritized",
      "status": "active",
      "disabled": false,
      "unavailable": false,
      "runtime_only": false,
      "source": "file",
      "size": 384,
      "path": "/mock/cliproxy/auth/claude-deprioritized.json",
      "priority": 0,
      "success": 1,
      "failed": 2,
      "recent_requests": [
        {
          "window_start": "2026-05-10T17:04:00Z",
          "window_end": "2026-05-10T17:05:00Z",
          "success_count": 0,
          "failure_count": 1
        }
      ],
      "note": "priority 0 means lowest/deprioritized"
    }
  ]
}`

const diskScanAuthFilesFallbackFixture = `{
  "files": [
    {
      "name": "gemini-disk-scan-only.json",
      "type": "gemini",
      "email": "fixture@example.invalid",
      "source": "disk_scan",
      "size": 256,
      "priority": 5,
      "note": "auth manager unavailable; live-only fields are unobservable"
    }
  ]
}`

const geminiProviderInventoryFixture = `{
  "gemini-api-key": [
    {
      "api-key": "redacted-gemini-key",
      "priority": 10,
      "prefix": "team-a/",
      "base-url": "https://generativelanguage.googleapis.com",
      "proxy-url": "",
      "models": [
        {"name": "gemini-2.5-pro", "alias": "gemini-pro"}
      ],
      "headers": {"X-Fixture": "true"},
      "excluded-models": ["gemini-legacy"],
      "auth-index": "auth_001"
    }
  ]
}`

const claudeProviderInventoryFixture = `{
  "claude-api-key": [
    {
      "api-key": "redacted-claude-key",
      "priority": 10,
      "prefix": "team-a/",
      "base-url": "https://api.anthropic.com",
      "proxy-url": "",
      "models": [
        {"name": "claude-3-7-sonnet", "alias": "claude-sonnet"}
      ],
      "headers": {"anthropic-version": "2023-06-01"},
      "excluded-models": [],
      "auth-index": "auth_002"
    }
  ]
}`

const codexProviderInventoryFixture = `{
  "codex-api-key": [
    {
      "api-key": "redacted-codex-key",
      "priority": 10,
      "prefix": "team-a/",
      "base-url": "https://api.openai.com",
      "websockets": true,
      "proxy-url": "",
      "models": [
        {"name": "gpt-5-codex", "alias": "codex"}
      ],
      "headers": {"X-Fixture": "true"},
      "excluded-models": [],
      "auth-index": "auth_003"
    }
  ]
}`

const vertexProviderInventoryFixture = `{
  "vertex-api-key": [
    {
      "api-key": "redacted-vertex-key",
      "priority": 10,
      "prefix": "team-a/",
      "base-url": "https://vertex.fixture.invalid",
      "proxy-url": "",
      "models": [
        {"name": "publishers/google/models/gemini-2.5-pro", "alias": "vertex-gemini"}
      ],
      "headers": {"X-Fixture": "true"},
      "excluded-models": [],
      "auth-index": "auth_004"
    }
  ]
}`

const openAICompatibilityInventoryFixture = `{
  "openai-compatibility": [
    {
      "name": "fixture-openai-compatible",
      "priority": 10,
      "disabled": false,
      "prefix": "team-a/",
      "base-url": "https://openai-compatible.fixture.invalid/v1",
      "api-key-entries": [
        {"api-key": "redacted-openai-compat-key", "proxy-url": "", "auth-index": "auth_005"}
      ],
      "models": [
        {"name": "fixture-model", "alias": "fixture-alias"}
      ],
      "headers": {"X-Fixture": "true"},
      "auth-index": ""
    }
  ]
}`
