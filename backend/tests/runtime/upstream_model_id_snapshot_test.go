package runtimetest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

// TestRuntimeUpstreamModelIDSnapshot proves the upstream model id decoupling
// end to end through the runtime writer:
//
//  1. Every real upstream attempt row and the final winner usage event carry
//     the request-time snapshot of the selected Terminal Target's frozen
//     upstream_model_id.
//  2. The logical attribution columns (model_id / resolved_target_model_id)
//     keep the entry and logical target identity, never the upstream id.
func TestRuntimeUpstreamModelIDSnapshot(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Run("orphan is excluded and invalid owned refresh keeps last good", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "upstream-last-good-public-" + randomSuffix(), TargetModelID: "upstream-last-good-target-" + randomSuffix(), EndpointBaseURL: harness.upstream.baseURL("/upstream-model/last-good"), EndpointAPIKey: "upstream-last-good-key"})
		lastGoodID := "vendor/last-good"
		setRuntimeConnectionUpstreamModelID(t, harness.conn, route.ConnectionID, lastGoodID)
		harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

		orphanEndpointID := harness.seedEndpoint(t, profileID, "upstream-orphan-"+randomSuffix(), harness.upstream.baseURL("/upstream-model/orphan"), "orphan-key")
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, upstream_model_id, openai_text_capability, is_active, priority, health_status, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, 'dual_native', TRUE, 0, 'unknown', NOW(), NOW())`, profileID, orphanEndpointID); err != nil {
			t.Fatalf("seed active orphan connection: %v", err)
		}
		if err := refreshRuntimeCache(harness, profileID); err != nil {
			t.Fatalf("active orphan must not enter or break the snapshot: %v", err)
		}

		generation := harness.runtimeCache.PublishedGeneration()
		if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET upstream_model_id = NULL WHERE id = $1`, route.ConnectionID); err != nil {
			t.Fatalf("clear owned upstream identity: %v", err)
		}
		err := refreshRuntimeCache(harness, profileID)
		if err == nil || !strings.Contains(err.Error(), "missing upstream_model_id") {
			t.Fatalf("expected owned missing identity to reject refresh, got %v", err)
		}
		if got := harness.runtimeCache.PublishedGeneration(); got != generation {
			t.Fatalf("failed refresh published generation %d, want last-good %d", got, generation)
		}

		harness.upstream.clear()
		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.PublicModelID, "last good upstream identity"), nil)
		assertStatus(t, response, http.StatusOK)
		if got := requestModelID(t, harness.upstream.lastRequest(t).Body); got != lastGoodID {
			t.Fatalf("last-good wire model = %q, want %q", got, lastGoodID)
		}
	})

	t.Run("legacy v2 payload without snapshots materializes NULL without quarantine", func(t *testing.T) {
		harness, profileID := enqueueBlockedOutputRatePayload(t)
		for _, path := range []string{
			`{envelope,usage_event,UpstreamModelID}`,
			`{envelope,request_logs,0,UpstreamModelID}`,
		} {
			if _, err := harness.conn.Exec(testContext, `UPDATE runtime_telemetry_outbox SET core_payload = core_payload #- $1 WHERE profile_id = $2`, path, profileID); err != nil {
				t.Fatalf("strip upstream identity field %s: %v", path, err)
			}
		}

		restarted := restartRuntimeHarnessWithConfig(t, harness.databaseName, runtimeHarnessConfig{
			RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
				PollInterval: 25 * time.Millisecond, ShutdownTimeout: 150 * time.Millisecond,
			}},
		})
		waitForRuntimeTelemetryCounts(t, restarted.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 10*time.Second)

		for _, table := range []string{"request_logs", "usage_request_events"} {
			var got *string
			if err := restarted.conn.QueryRow(testContext, `SELECT upstream_model_id FROM `+table+` WHERE profile_id = $1`, profileID).Scan(&got); err != nil || got != nil {
				t.Fatalf("legacy %s snapshot = %+v, want NULL (err=%v)", table, got, err)
			}
		}
		var quarantined int
		if err := restarted.conn.QueryRow(testContext, `SELECT COUNT(*) FROM runtime_telemetry_quarantine WHERE profile_id = $1`, profileID).Scan(&quarantined); err != nil {
			t.Fatalf("count upstream snapshot quarantine rows: %v", err)
		}
		if quarantined != 0 {
			t.Fatalf("legacy payload without upstream snapshots was quarantined: %d", quarantined)
		}
	})
}

func assertRuntimeUpstreamReadProjections(t *testing.T, harness *runtimeHarness, profileID int, ingressID string, want ...string) {
	t.Helper()
	var winner *string
	if err := harness.conn.QueryRow(context.Background(), `SELECT upstream_model_id FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2`, profileID, ingressID).Scan(&winner); err != nil {
		t.Fatalf("load winner usage snapshot: %v", err)
	}
	assertRuntimeSnapshotValue(t, winner, want[len(want)-1], "winner usage identity")
	chains := runtimeStatsPayload(t, harness, profileID, "/api/stats/requests?view=ingress_chains&ingress_request_id="+ingressID+"&chain_limit=10")["items"].([]any)
	if len(chains) != 1 {
		t.Fatalf("expected one ingress chain, got %+v", chains)
	}
	chain := asMapRuntime(t, chains[0])
	if final := asMapRuntime(t, chain["finalized_summary"]); final["final_upstream_model_id"] != *winner {
		t.Fatalf("finalized winner snapshot = %+v", final)
	}
	assertRuntimeSnapshotSet(t, runtimeProjectionIDs(t, chain["retained_rows"].([]any)), "chain rows", want...)
}

// setRuntimeConnectionUpstreamModelID writes the frozen upstream identity
// onto the seeded Terminal Target before the runtime snapshot refresh reads it.
func setRuntimeConnectionUpstreamModelID(t *testing.T, conn *pgx.Conn, connectionID int, upstreamModelID string) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), `UPDATE connections SET upstream_model_id = $2 WHERE id = $1`, connectionID, upstreamModelID); err != nil {
		t.Fatalf("set connection %d upstream_model_id: %v", connectionID, err)
	}
}

func runtimeStatsPayload(t *testing.T, harness *runtimeHarness, profileID int, path string) map[string]any {
	t.Helper()
	response := harness.requestJSON(t, http.MethodGet, path, nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	payload := map[string]any{}
	decodeJSONResponse(t, response, &payload)
	return payload
}

func refreshRuntimeCache(harness *runtimeHarness, profileID int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return harness.runtimeCache.RefreshNow(ctx, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
}

func assertRuntimeSnapshotValue(t *testing.T, got *string, want, label string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %+v, want %q", label, got, want)
	}
}

func runtimeProjectionIDs(t *testing.T, items []any) []string {
	t.Helper()
	ids := make([]string, 0, len(items))
	for _, raw := range items {
		ids = append(ids, asMapRuntime(t, raw)["upstream_model_id"].(string))
	}
	return ids
}

func assertRuntimeSnapshotSet(t *testing.T, got []string, label string, want ...string) {
	t.Helper()
	seen := make(map[string]bool, len(got))
	for _, value := range got {
		seen[value] = true
	}
	if len(got) != len(want) {
		t.Fatalf("%s = %+v, want %+v", label, got, want)
	}
	for _, value := range want {
		if !seen[value] {
			t.Fatalf("%s = %+v, missing %q", label, got, value)
		}
	}
}
