package runtimetest

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRuntimeLeaseNonStreamInFlightExclusivity(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	primaryUpstream := newBlockingScriptedUpstream(t, 1, http.StatusOK, map[string]any{"id": "chatcmpl-lease-non-stream-primary"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-lease-non-stream-secondary"})
	strategyID := harness.seedAdaptiveStrategy(t, activeProfileID, "runtime-lease-non-stream-"+suffix)
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:  activeProfileID,
		prefix:     "lease-non-stream",
		suffix:     suffix,
		strategyID: strategyID,
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryUpstream.baseURL("/loadbalance/lease/non-stream/primary"), priority: 0},
			{label: "secondary", baseURL: secondaryUpstream.baseURL("/loadbalance/lease/non-stream/secondary"), priority: 1},
		},
	})
	maxInFlightNonStream := 1
	harness.updateConnectionAdmissionLimits(t, route.connectionIDs[0], nil, &maxInFlightNonStream, nil)
	requestBody := chatCompletionsBody(route.publicModelID, "non-stream lease exclusivity")
	firstResultCh := startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/chat/completions", requestBody, nil)

	primaryUpstream.waitUntilReady(t, 5*time.Second)
	inflightState := loadRuntimeState(t, harness, activeProfileID, route.connectionIDs[0])
	if inflightState.InFlightNonStream != 1 {
		t.Fatalf("expected one in-flight non-stream attempt, got %+v", inflightState)
	}

	secondResponse := performPriorityRequest(t, harness.client, 2*time.Second, http.MethodPost, harness.url+"/v1/chat/completions", requestBody, nil)
	secondBody := readResponseBody(t, secondResponse)
	if secondResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected overlapping non-stream lease request status 200, got %d with body %s", secondResponse.StatusCode, secondBody)
	}
	if got := len(primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected non-stream lease to keep the primary at one launched request, got %d", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected overlapping request to reach the secondary once while the primary lease is held, got %d", got)
	}
	if got := requestModelID(t, secondaryUpstream.requestsSnapshot()[0].Body); got != route.targetModelID {
		t.Fatalf("expected secondary fallback request model %q, got %q", route.targetModelID, got)
	}

	primaryUpstream.releaseRequests()
	firstResult := awaitAsyncRequest(t, firstResultCh, 5*time.Second)
	if firstResult.Err != nil {
		t.Fatalf("expected first non-stream lease request to succeed after release, got error: %v", firstResult.Err)
	}
	if firstResult.StatusCode != http.StatusOK {
		t.Fatalf("expected first non-stream lease request status 200, got %d with body %s", firstResult.StatusCode, firstResult.Body)
	}
	releasedState := loadRuntimeState(t, harness, activeProfileID, route.connectionIDs[0])
	if releasedState.InFlightNonStream != 0 {
		t.Fatalf("expected non-stream in-flight ownership to release after request completion, got %+v", releasedState)
	}
}

func TestRuntimeLeaseRemainsHeldUntilResponseBodyCompletion(t *testing.T) {
	for _, test := range []struct {
		name      string
		streaming bool
	}{
		{name: "non-stream", streaming: false},
		{name: "stream", streaming: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			suffix := randomSuffix()
			upstream := newHeaderFirstBlockingScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-header-first-lease"})
			strategyID := harness.seedAdaptiveStrategy(t, profileID, "runtime-header-first-lease-"+suffix)
			route := seedSelectorRoute(t, harness, selectorRouteSeed{
				profileID:  profileID,
				prefix:     "header-first-lease",
				suffix:     suffix,
				strategyID: strategyID,
				endpoints:  []selectorEndpointSeed{{label: "only", baseURL: upstream.baseURL("/loadbalance/lease/header-first")}},
			})
			limit := 1
			if test.streaming {
				harness.updateConnectionAdmissionLimits(t, route.connectionIDs[0], nil, nil, &limit)
			} else {
				harness.updateConnectionAdmissionLimits(t, route.connectionIDs[0], nil, &limit, nil)
			}
			requestBody := chatCompletionsBody(route.publicModelID, "header-first response body lease")
			requestBody["stream"] = test.streaming

			firstResultCh := startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/chat/completions", requestBody, nil)
			upstream.waitUntilHeadersSent(t, 5*time.Second)

			inflight := loadRuntimeState(t, harness, profileID, route.connectionIDs[0])
			if (!test.streaming && inflight.InFlightNonStream != 1) || (test.streaming && inflight.InFlightStream != 1) {
				t.Fatalf("expected the response-body lease to remain in flight after headers, got %+v", inflight)
			}
			second := performPriorityRequest(t, harness.client, time.Second, http.MethodPost, harness.url+"/v1/chat/completions", requestBody, nil)
			secondBody := readResponseBody(t, second)
			if second.StatusCode != http.StatusServiceUnavailable || !strings.Contains(secondBody, "admission_exhausted") {
				t.Fatalf("expected overlapping request admission_exhausted 503, got status %d body %s", second.StatusCode, secondBody)
			}
			if got := len(upstream.requestsSnapshot()); got != 1 {
				t.Fatalf("expected exactly one upstream request while the body lease is held, got %d", got)
			}

			upstream.releaseRequests()
			first := awaitAsyncRequest(t, firstResultCh, 5*time.Second)
			if first.Err != nil || first.StatusCode != http.StatusOK {
				t.Fatalf("expected first response to complete after body release, got %+v", first)
			}
		})
	}
}
