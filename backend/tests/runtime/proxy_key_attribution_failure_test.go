package runtimetest

import (
	"net/http"
	"testing"
	"time"
)

// unroutableUpstreamBaseURL points at the reserved discard port, which is
// closed on a normal host. A request there fails at the transport layer
// instead of returning an HTTP status, which is what forces the gateway-side
// total-failure path rather than a relayed upstream 4xx.
const unroutableUpstreamBaseURL = "http://127.0.0.1:9"

// TestRuntimeAttributionIdentifiedOnAllConnectionsFailed covers the gap that
// let a wedged telemetry pipeline ship: every existing attribution test drives
// a 200 upstream, so the failure envelope builders were never exercised. A
// keyed request whose only target is unreachable must still produce a usage
// event the database accepts, and the outbox must drain — otherwise one such
// request silently stops all recording for the whole instance.
func TestRuntimeAttributionIdentifiedOnAllConnectionsFailed(t *testing.T) {
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{})
	profileID := harness.activeProfileID(t)
	proxyAPIKey := harness.insertProxyAPIKey(t, "failure-identified")
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "failure-id-public-" + randomSuffix(),
		TargetModelID:   "failure-id-target-" + randomSuffix(),
		EndpointBaseURL: unroutableUpstreamBaseURL,
		EndpointAPIKey:  "failure-upstream-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "all connections failed attribution"}},
		"model":    route.PublicModelID,
	}, map[string]string{"Authorization": "Bearer " + proxyAPIKey.RawKey})
	assertStatus(t, response, http.StatusBadGateway)

	// OutboxRows: 0 is the assertion that matters most. A usage event the
	// database rejects leaves its row pending forever and head-of-line blocks
	// every later row.
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 15*time.Second)
	assertLatestRuntimeAttribution(t, harness.conn, profileID, runtimeAttributionExpectation{
		State:        "identified",
		AuthEnforced: false,
		KeyID:        proxyAPIKey.ID,
		Name:         proxyAPIKey.Name,
	})
}

// TestRuntimeAttributionNoneOnAllConnectionsFailed guards the other legal
// branch of the attribution constraint: the same failure path without a key
// must record none with both snapshot columns null.
func TestRuntimeAttributionNoneOnAllConnectionsFailed(t *testing.T) {
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{})
	profileID := harness.activeProfileID(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "failure-none-public-" + randomSuffix(),
		TargetModelID:   "failure-none-target-" + randomSuffix(),
		EndpointBaseURL: unroutableUpstreamBaseURL,
		EndpointAPIKey:  "failure-none-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "all connections failed without a key"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusBadGateway)

	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 15*time.Second)
	assertLatestRuntimeAttribution(t, harness.conn, profileID, runtimeAttributionExpectation{
		State:        "none",
		AuthEnforced: false,
	})
}
