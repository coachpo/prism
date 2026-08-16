package runtimetest

import (
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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

// TestRuntimeAttributionIdentifiedOnPlanningRejection covers the planning
// failure builder, a second site with the same omission. A model with no
// connection at all rejects before any attempt is launched; whatever telemetry
// that path emits must still be materializable.
func TestRuntimeAttributionIdentifiedOnPlanningRejection(t *testing.T) {
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{})
	profileID := harness.activeProfileID(t)
	proxyAPIKey := harness.insertProxyAPIKey(t, "planning-identified")
	publicModelID := "planning-id-public-" + randomSuffix()
	harness.seedModel(t, profileID, "openai", publicModelID, "chat_completions_only", nil)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "planning rejection attribution"}},
		"model":    publicModelID,
	}, map[string]string{"Authorization": "Bearer " + proxyAPIKey.RawKey})
	if response.StatusCode < 400 {
		t.Fatalf("expected a planning rejection, got %d", response.StatusCode)
	}

	// This path may legitimately emit no telemetry at all. What it must never
	// do is enqueue a row the database will not accept.
	assertRuntimeOutboxDrained(t, harness.conn, profileID, 15*time.Second)
	counts := loadRuntimeTelemetryCounts(t, harness.conn, profileID)
	if counts.UsageEvents > 0 {
		assertLatestRuntimeAttribution(t, harness.conn, profileID, runtimeAttributionExpectation{
			State:        "identified",
			AuthEnforced: false,
			KeyID:        proxyAPIKey.ID,
			Name:         proxyAPIKey.Name,
		})
	}
}

// assertRuntimeOutboxDrained fails when a telemetry row is still pending after
// the timeout, which is the observable signature of a payload the database
// will never accept.
//
// The settle window matters: enqueue happens on the request path but is
// observable a moment later, so returning on the first zero reading would pass
// before the row under test even exists.
func assertRuntimeOutboxDrained(t *testing.T, conn *pgx.Conn, profileID int, timeout time.Duration) {
	t.Helper()
	time.Sleep(500 * time.Millisecond)
	deadline := time.Now().Add(timeout)
	counts := loadRuntimeTelemetryCounts(t, conn, profileID)
	for time.Now().Before(deadline) {
		if counts.OutboxRows == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
		counts = loadRuntimeTelemetryCounts(t, conn, profileID)
	}
	t.Fatalf("telemetry outbox never drained: %d row(s) still pending after %s; a row the materializer cannot insert blocks every later row", counts.OutboxRows, timeout)
}
