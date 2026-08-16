package runtimetest

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

// TestRuntimeAttemptBudgetExhaustionAt64Launches verifies the executor hard
// safety bound (Requests SPEC §3.4): with 65 failing target candidates, the
// ingress launches exactly 64 upstream attempts, terminates with the gateway
// `attempt_budget_exhausted` error, and never constructs a 65th upstream row.
func TestRuntimeAttemptBudgetExhaustionAt64Launches(t *testing.T) {
	if testing.Short() {
		t.Skip("64-launch boundary test requires a full runtime harness")
	}
	gate := newRuntimeTelemetryMaterializeGate()
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
			WorkerCount:     1,
			PollInterval:    25 * time.Millisecond,
			ShutdownTimeout: time.Second,
			WakeupBuffer:    1,
			Hooks: &runtimeapi.TelemetryOutboxHooks{
				BeforeMaterialize: gate.Wait,
			},
		}},
	})
	profileID := harness.activeProfileID(t)
	modelID := "budget-model-" + randomSuffix()

	// 65 failing targets, all returning 429 so failover keeps walking.
	upstreams := make([]*scriptedUpstream, 0, 65)
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "budget-strategy-"+randomSuffix(), "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "openai", modelID, "native", &strategyID)
	connectionIDs := make([]int, 0, 65)
	for index := 0; index < 65; index++ {
		upstream := newScriptedUpstream(t, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"message": fmt.Sprintf("budget target %d unavailable", index), "type": "rate_limit_exceeded"}})
		upstreams = append(upstreams, upstream)
		endpointID := harness.seedEndpoint(t, profileID, fmt.Sprintf("budget-endpoint-%d-%s", index, randomSuffix()), upstream.baseURL("/budget"), fmt.Sprintf("budget-key-%d", index), index)
		connectionID := harness.seedConnection(t, profileID, modelConfigID, endpointID, fmt.Sprintf("budget-connection-%d-%s", index, randomSuffix()), nil, nil, index)
		connectionIDs = append(connectionIDs, connectionID)
	}
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "trigger the launch safety bound"}},
		"model":    modelID,
	}, nil)
	// The gateway terminates with 503 attempt_budget_exhausted.
	assertStatus(t, response, http.StatusServiceUnavailable)
	body := readResponseBody(t, response)
	if !strings.Contains(body, "attempt_budget_exhausted") && !strings.Contains(body, "maximum of 64 upstream attempts") {
		t.Fatalf("expected attempt_budget_exhausted gateway error, got %s", body)
	}

	// Exactly 64 attempts were launched across all upstreams; the 65th
	// candidate was never opened.
	totalLaunched := 0
	for _, upstream := range upstreams {
		totalLaunched += len(upstream.requestsSnapshot())
	}
	if totalLaunched != 64 {
		t.Fatalf("expected exactly 64 launched upstream attempts at the safety bound, got %d", totalLaunched)
	}

	// Telemetry persisted 64 upstream rows and the gateway terminal code in
	// the finalized usage event; no 65th row exists.
	gate.Release()
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 64, UsageEvents: 1, OutboxRows: 0}, 15*time.Second)

	var attemptCount int
	queryCtx, queryCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer queryCancel()
	if err := harness.conn.QueryRow(queryCtx, `SELECT COUNT(*) FROM request_logs WHERE profile_id = $1 AND model_id = $2 AND row_kind = 'upstream'`, profileID, modelID).Scan(&attemptCount); err != nil {
		t.Fatalf("count budget request rows: %v", err)
	}
	if attemptCount != 64 {
		t.Fatalf("expected 64 retained upstream rows, got %d", attemptCount)
	}

	var finalErrorCode string
	if err := harness.conn.QueryRow(queryCtx, `SELECT COALESCE(final_error_code, '') FROM usage_request_events WHERE profile_id = $1 AND model_id = $2 ORDER BY created_at DESC LIMIT 1`, profileID, modelID).Scan(&finalErrorCode); err != nil {
		t.Fatalf("read final error code: %v", err)
	}
	if finalErrorCode != "attempt_budget_exhausted" {
		t.Fatalf("expected finalized usage event to carry attempt_budget_exhausted, got %q", finalErrorCode)
	}
}
