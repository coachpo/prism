package runtimetest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func TestRuntimeLoadBalanceSingleDoesNotFailOverAfterPrimaryFailure(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "single primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-single-secondary"})
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:    activeProfileID,
		prefix:       "loadbalance-single",
		strategyType: "single",
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryUpstream.baseURL("/loadbalance/single/primary"), priority: 0},
			{label: "secondary", baseURL: secondaryUpstream.baseURL("/loadbalance/single/secondary"), priority: 1},
		},
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "single no failover"), nil)
	assertStatus(t, response, http.StatusServiceUnavailable)
	if got := len(primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected single strategy to call the primary upstream once, got %d requests", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected single strategy to avoid secondary failover, got %d requests", got)
	}
}

func TestRuntimeLoadBalanceFillFirstFailsOverToNextEligibleConnection(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "fill-first primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-fill-first-secondary"})
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:    activeProfileID,
		prefix:       "loadbalance-fill-first",
		strategyType: "fill-first",
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryUpstream.baseURL("/loadbalance/fill-first/primary"), priority: 0},
			{label: "secondary", baseURL: secondaryUpstream.baseURL("/loadbalance/fill-first/secondary"), priority: 1},
		},
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "fill-first failover"), nil)
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-fill-first-secondary")
	if got := len(primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected fill-first strategy to call the primary upstream once, got %d requests", got)
	}
	secondaryRequests := secondaryUpstream.requestsSnapshot()
	if len(secondaryRequests) != 1 {
		t.Fatalf("expected fill-first strategy to fail over to the secondary upstream once, got %d requests", len(secondaryRequests))
	}
	if requestModelID(t, secondaryRequests[0].Body) != route.targetModelID {
		t.Fatalf("expected fill-first failover body model %q, got %q", route.targetModelID, requestModelID(t, secondaryRequests[0].Body))
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, activeProfileID, 2, 2)
}

func TestRuntimeLoadBalanceFillFirstHonorsConfiguredNon5xxFailureStatus(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	primaryUpstream := newScriptedUpstream(t, http.StatusRequestTimeout, map[string]any{"error": "configured timeout"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-configured-http-secondary"})
	strategyID := harness.seedLegacyStrategy(t, activeProfileID, "runtime-configured-http-"+randomSuffix(), "fill-first")
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE loadbalance_strategies SET failure_status_codes = ARRAY[$1]::integer[], updated_at = $2 WHERE id = $3`,
		http.StatusRequestTimeout,
		time.Now().UTC(),
		strategyID,
	); err != nil {
		t.Fatalf("configure non-5xx failover status: %v", err)
	}
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:  activeProfileID,
		prefix:     "configured-http-failover",
		strategyID: strategyID,
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryUpstream.baseURL("/loadbalance/configured-http/primary"), priority: 0},
			{label: "secondary", baseURL: secondaryUpstream.baseURL("/loadbalance/configured-http/secondary"), priority: 1},
		},
	})
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{activeProfileID}})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "configured 408 failover"), nil)
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-configured-http-secondary")
	if got := len(primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected one configured 408 primary request, got %d", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected one secondary request after configured 408, got %d", got)
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, activeProfileID, 2, 2)
}

func TestRuntimeLoadBalanceFillFirstDoesNotFailOverUnconfigured429(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	primaryUpstream := newScriptedUpstream(t, http.StatusTooManyRequests, map[string]any{"error": "not configured for failover"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-unconfigured-429-secondary"})
	strategyID := harness.seedLegacyStrategy(t, activeProfileID, "runtime-unconfigured-429-"+randomSuffix(), "fill-first")
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE loadbalance_strategies SET failure_status_codes = ARRAY[$1]::integer[], updated_at = $2 WHERE id = $3`,
		http.StatusRequestTimeout,
		time.Now().UTC(),
		strategyID,
	); err != nil {
		t.Fatalf("configure 408-only failover status: %v", err)
	}
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:  activeProfileID,
		prefix:     "unconfigured-429",
		strategyID: strategyID,
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryUpstream.baseURL("/loadbalance/unconfigured-429/primary"), priority: 0},
			{label: "secondary", baseURL: secondaryUpstream.baseURL("/loadbalance/unconfigured-429/secondary"), priority: 1},
		},
	})
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{activeProfileID}})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "unconfigured 429 definitive"), nil)
	assertStatus(t, response, http.StatusTooManyRequests)
	if got := len(primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected one primary 429 request, got %d", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected unconfigured 429 to remain definitive, got %d secondary requests", got)
	}
}

func TestRuntimeLoadBalanceFillFirstFailsOverOnPreHeaderTransportError(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	closedPrimary := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	primaryBaseURL := closedPrimary.URL
	closedPrimary.Close()
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-transport-secondary"})
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:    activeProfileID,
		prefix:       "transport-failover",
		strategyType: "fill-first",
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryBaseURL + "/loadbalance/transport/primary", priority: 0},
			{label: "secondary", baseURL: secondaryUpstream.baseURL("/loadbalance/transport/secondary"), priority: 1},
		},
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "transport failover"), nil)
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-transport-secondary")
	if got := len(secondaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected one secondary request after pre-header transport failure, got %d", got)
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, activeProfileID, 2, 2)
}

func TestRuntimeLoadBalanceFillFirstFailsOverOnResponseHeaderTimeout(t *testing.T) {
	primaryUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(primaryUpstream.Close)
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-header-timeout-secondary"})
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 50 * time.Millisecond
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{RuntimeOptions: runtimeapi.Options{
		HTTPClient: &http.Client{Transport: transport, Timeout: 2 * time.Second},
	}})
	activeProfileID := harness.activeProfileID(t)
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:    activeProfileID,
		prefix:       "header-timeout-failover",
		strategyType: "fill-first",
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryUpstream.URL + "/loadbalance/header-timeout/primary", priority: 0},
			{label: "secondary", baseURL: secondaryUpstream.baseURL("/loadbalance/header-timeout/secondary"), priority: 1},
		},
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "response header timeout failover"), nil)
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-header-timeout-secondary")
	if got := len(secondaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected one secondary request after response-header timeout, got %d", got)
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, activeProfileID, 2, 2)
}

func TestRuntimeBudgetUsesOneOverallDeadlineAcrossAttempts(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	requestBudget := 500 * time.Millisecond
	primaryDelay := 250 * time.Millisecond
	secondaryStarted := make(chan struct{}, 1)

	primaryUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-time.After(primaryDelay)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "budget primary unavailable"})
	}))
	t.Cleanup(primaryUpstream.Close)
	secondaryUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case secondaryStarted <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "chatcmpl-budget-secondary"})
		}
	}))
	t.Cleanup(secondaryUpstream.Close)

	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:    activeProfileID,
		prefix:       "loadbalance-budget",
		suffix:       suffix,
		strategyType: "fill-first",
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryUpstream.URL + "/loadbalance/budget/primary", priority: 0},
			{label: "secondary", baseURL: secondaryUpstream.URL + "/loadbalance/budget/secondary", priority: 1},
		},
	})

	rawBody, err := json.Marshal(chatCompletionsBody(route.publicModelID, "shared request budget"))
	if err != nil {
		t.Fatalf("marshal runtime budget request: %v", err)
	}
	budgetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), requestBudget)
		defer cancel()
		harness.server.Config.Handler.ServeHTTP(w, r.WithContext(ctx))
	}))
	defer budgetServer.Close()
	budgetClient := budgetServer.Client()
	request, err := http.NewRequest(http.MethodPost, budgetServer.URL+"/v1/chat/completions", bytes.NewReader(rawBody))
	if err != nil {
		t.Fatalf("build runtime budget request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")

	startedAt := time.Now()
	response, err := budgetClient.Do(request)
	elapsed := time.Since(startedAt)
	if err != nil {
		t.Fatalf("perform runtime budget request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusBadGateway {
		body := readResponseBody(t, response)
		t.Fatalf("expected overall deadline exhaustion to surface as 502 after failover, got status %d body %s", response.StatusCode, body)
	}
	if elapsed > requestBudget+(250*time.Millisecond) {
		t.Fatalf("expected runtime request to stay within one overall budget, took %s for a %s budget", elapsed, requestBudget)
	}

	select {
	case <-secondaryStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected secondary upstream attempt to start before the overall request deadline")
	}
}

func TestRuntimeLoadBalanceConcurrentRoundRobinRequestsUseDistinctCursorClaims(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	firstUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-concurrent-round-robin-first"})
	secondUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-concurrent-round-robin-second"})
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID: activeProfileID,
		prefix:    "loadbalance-concurrent-round-robin",
		endpoints: []selectorEndpointSeed{
			{label: "first", baseURL: firstUpstream.baseURL("/loadbalance/concurrent/round-robin/first"), priority: 0},
			{label: "second", baseURL: secondUpstream.baseURL("/loadbalance/concurrent/round-robin/second"), priority: 1},
		},
	})

	results := executeConcurrentRuntimeJSONRequests(t, harness, 8, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "concurrent round-robin cursor claims"), nil)
	for _, result := range results {
		if result.Err != nil {
			t.Fatalf("expected concurrent round-robin request to succeed, got error: %v", result.Err)
		}
		if result.StatusCode != http.StatusOK {
			t.Fatalf("expected concurrent round-robin request status 200, got %d with body %s", result.StatusCode, result.Body)
		}
	}

	firstRequests := firstUpstream.requestsSnapshot()
	secondRequests := secondUpstream.requestsSnapshot()
	if len(firstRequests) != 4 || len(secondRequests) != 4 {
		t.Fatalf("expected concurrent round-robin requests to split evenly across both upstreams, got first=%d second=%d", len(firstRequests), len(secondRequests))
	}
	for _, request := range append(firstRequests, secondRequests...) {
		if got := requestModelID(t, request.Body); got != route.targetModelID {
			t.Fatalf("expected concurrent round-robin request model %q, got %q", route.targetModelID, got)
		}
	}
	if nextCursor := loadRoundRobinNextCursor(t, harness, activeProfileID, route.targetModelConfigID, 2); nextCursor != 0 {
		t.Fatalf("expected concurrent round-robin next_cursor to wrap to 0 after 8 claims, got %d", nextCursor)
	}
}

func TestRuntimeExpiredRetryWindowAllowsConcurrentAttempts(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	primaryUpstream := newBlockingScriptedUpstream(t, 2, http.StatusOK, map[string]any{"id": "chatcmpl-retry-window-primary"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-retry-window-secondary"})
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:    activeProfileID,
		prefix:       "retry-window",
		strategyType: "fill-first",
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryUpstream.baseURL("/loadbalance/retry-window/primary"), priority: 0},
			{label: "secondary", baseURL: secondaryUpstream.baseURL("/loadbalance/retry-window/secondary"), priority: 1},
		},
	})
	retryAvailableAt := time.Now().UTC().Add(-1 * time.Minute)
	priorFailureKind := "transient_http"
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:               activeProfileID,
		ConnectionID:            route.connectionIDs[0],
		CycleRetryAttempts:      1,
		CumulativeRetryAttempts: 1,
		LastFailureKind:         &priorFailureKind,
		LastRetryDelayMS:        60000,
		BanMode:                 "off",
		NextRetryAt:             &retryAvailableAt,
	})
	requestBody := chatCompletionsBody(route.publicModelID, "retry window concurrent attempt")
	resultChans := []<-chan concurrentRuntimeRequestResult{
		startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/chat/completions", requestBody, nil),
		startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/chat/completions", requestBody, nil),
	}

	primaryUpstream.waitUntilReady(t, 5*time.Second)
	primaryRequests := primaryUpstream.requestsSnapshot()
	if got := len(primaryRequests); got != 2 {
		t.Fatalf("expected expired retry window to allow two primary attempts, got %d", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected expired retry window to avoid fallback attempts, got %d secondary requests", got)
	}
	for index, request := range primaryRequests {
		if got := requestModelID(t, request.Body); got != route.targetModelID {
			t.Fatalf("expected primary request %d model %q, got %q", index, route.targetModelID, got)
		}
	}

	primaryUpstream.releaseRequests()
	for index, resultCh := range resultChans {
		result := awaitAsyncRequest(t, resultCh, 5*time.Second)
		if result.Err != nil {
			t.Fatalf("expected retry-window request %d to succeed after release, got error: %v", index, result.Err)
		}
		if result.StatusCode != http.StatusOK {
			t.Fatalf("expected retry-window request %d status 200, got %d with body %s", index, result.StatusCode, result.Body)
		}
	}
	releasedState := loadRuntimeState(t, harness, activeProfileID, route.connectionIDs[0])
	if releasedState.CycleRetryAttempts != 0 || releasedState.CumulativeRetryAttempts != 0 || releasedState.NextRetryAt.Valid || releasedState.BanMode != "off" {
		t.Fatalf("expected successful retry-window attempts to clear retry state, got %+v", releasedState)
	}
}
