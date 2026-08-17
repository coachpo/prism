package runtimetest

import (
	"context"
	"net/http"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

// TestRuntimeOpenAIPlanningRejectionCodesE2E pins the stable, mutually
// distinguishable planning codes plus the ordinary dynamic 503 across real
// runtime proxy paths. Each bullet below names a code; the subtests that
// currently exercise them are listed after it:
//
//   - openai_operation_not_supported (400): root model does not accept the operation;
//   - openai_no_compatible_terminal_target (503): no capability-compatible leaf;
//   - openai_no_eligible_terminal_target (503): compatible leaf exists but statically
//     ineligible. Covered here for an inactive connection and a disabled row.
//     Single-strategy truncation reaches the same code but has no subtest here;
//     do not cite this file as evidence that it is covered;
//   - ordinary 503 without planning code: statically eligible compatible route was
//     dynamically unavailable (Ban/retry);
//   - terminal_target_schedule_closed (503): every evaluated terminal target is
//     outside its configured routing window;
//   - terminal_target_schedule_unresolvable (503): every evaluated terminal target
//     has an unresolvable routing timezone.
func TestRuntimeOpenAIPlanningRejectionCodesE2E(t *testing.T) {
	t.Run("inactive connection classifies as no eligible terminal target", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		suffix := randomSuffix()
		strategyID := harness.seedLegacyStrategy(t, profileID, "planning-code-"+suffix, "fill-first")
		publicModelID := "planning-public-" + suffix
		modelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
		endpointID := harness.seedEndpoint(t, profileID, "planning-endpoint-"+suffix, "https://planning-code.invalid", "planning-key")
		connectionID := harness.seedConnectionWithOpenAITextCapability(t, profileID, modelConfigID, endpointID, "planning-connection-"+suffix, nil, nil, 0, runtimeStringPtr("dual_native"))

		if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET is_active = FALSE, updated_at = $1 WHERE id = $2`, time.Now().UTC(), connectionID); err != nil {
			t.Fatalf("deactivate connection: %v", err)
		}
		harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(publicModelID, "inactive connection"), nil)
		assertStatus(t, response, http.StatusServiceUnavailable)
		payload := runtimeResponsePayload(t, response)
		if payload["error"] != "openai_no_eligible_terminal_target" {
			t.Fatalf("expected openai_no_eligible_terminal_target, got %+v", payload)
		}
	})

	t.Run("disabled row with compatible connection classifies as no eligible terminal target", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		suffix := randomSuffix()
		strategyID := harness.seedLegacyStrategy(t, profileID, "planning-disabled-"+suffix, "fill-first")
		publicModelID := "planning-disabled-public-" + suffix
		modelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
		endpointID := harness.seedEndpoint(t, profileID, "planning-disabled-endpoint-"+suffix, "https://planning-disabled.invalid", "planning-disabled-key")
		harness.seedConnectionWithOpenAITextCapability(t, profileID, modelConfigID, endpointID, "planning-disabled-connection-"+suffix, nil, nil, 0, runtimeStringPtr("chat_completions_only"))
		compatibleConnectionID := harness.seedConnectionWithOpenAITextCapability(t, profileID, modelConfigID, endpointID, "planning-disabled-compatible-"+suffix, nil, nil, 1, runtimeStringPtr("dual_native"))

		if _, err := harness.conn.Exec(context.Background(),
			`UPDATE model_access_targets SET is_enabled = FALSE, updated_at = $1 WHERE profile_id = $2 AND target_connection_id = $3`,
			time.Now().UTC(), profileID, compatibleConnectionID); err != nil {
			t.Fatalf("disable compatible access target row: %v", err)
		}
		harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

		response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{"model": publicModelID, "input": "disabled compatible row"}, nil)
		assertStatus(t, response, http.StatusServiceUnavailable)
		payload := runtimeResponsePayload(t, response)
		if payload["error"] != "openai_no_eligible_terminal_target" {
			t.Fatalf("expected openai_no_eligible_terminal_target, got %+v", payload)
		}
	})

	t.Run("banned compatible route keeps ordinary service unavailable", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		suffix := randomSuffix()
		strategyID := harness.seedLegacyStrategy(t, profileID, "planning-banned-"+suffix, "fill-first")
		publicModelID := "planning-banned-public-" + suffix
		modelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
		endpointID := harness.seedEndpoint(t, profileID, "planning-banned-endpoint-"+suffix, "https://planning-banned.invalid", "planning-banned-key")
		connectionID := harness.seedConnectionWithOpenAITextCapability(t, profileID, modelConfigID, endpointID, "planning-banned-connection-"+suffix, nil, nil, 0, runtimeStringPtr("dual_native"))

		bannedUntil := time.Now().UTC().Add(10 * time.Minute)
		harness.seedRuntimeState(t, runtimeStateSeed{
			ProfileID:      profileID,
			ConnectionID:   connectionID,
			BanMode:        "until_reset",
			CircuitState:   "open",
			BlockedUntilAt: &bannedUntil,
		})

		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(publicModelID, "banned route"), nil)
		assertStatus(t, response, http.StatusServiceUnavailable)
		payload := runtimeResponsePayload(t, response)
		if payload["error"] != nil {
			t.Fatalf("expected dynamic unavailability to keep ordinary 503 without planning code, got %+v", payload)
		}
	})

	t.Run("closed routing window classifies as terminal target schedule closed", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		suffix := randomSuffix()
		strategyID := harness.seedLegacyStrategy(t, profileID, "planning-schedule-close-"+suffix, "fill-first")
		publicModelID := "planning-schedule-close-public-" + suffix
		modelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
		endpointID := harness.seedEndpoint(t, profileID, "planning-schedule-close-endpoint-"+suffix, "https://planning-schedule-close.invalid", "planning-schedule-close-key")
		connectionID := harness.seedConnectionWithOpenAITextCapability(t, profileID, modelConfigID, endpointID, "planning-schedule-close-connection-"+suffix, nil, nil, 0, runtimeStringPtr("dual_native"))

		// Any weekday other than today, full day: closed at the request instant
		// on every day of the week regardless of the harness clock. weekday_mask
		// is a 7-bit ISO bitmap (bit0=Monday .. bit6=Sunday).
		closedWeekdayBit := (int(time.Now().UTC().Weekday()) + 7) % 7
		closedWeekdayBit = (closedWeekdayBit + 1) % 7
		harness.updateConnectionRoutingSchedule(t, profileID, connectionID, "UTC", [][3]int{{1 << closedWeekdayBit, 0, 1440}})
		harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(publicModelID, "closed window"), nil)
		assertStatus(t, response, http.StatusServiceUnavailable)
		payload := runtimeResponsePayload(t, response)
		if payload["error"] != "terminal_target_schedule_closed" {
			t.Fatalf("expected terminal_target_schedule_closed, got %+v", payload)
		}
		if payload["schedule_excluded_connection_count"] != float64(1) {
			t.Fatalf("expected one excluded connection on the wire, got %+v", payload)
		}
		if payload["schedule_earliest_next_open_at_known"] != true {
			t.Fatalf("expected earliest next open known on the wire, got %+v", payload)
		}
	})

	t.Run("unresolvable routing timezone classifies as schedule unresolvable", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		suffix := randomSuffix()
		strategyID := harness.seedLegacyStrategy(t, profileID, "planning-schedule-tz-"+suffix, "fill-first")
		publicModelID := "planning-schedule-tz-public-" + suffix
		modelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
		endpointID := harness.seedEndpoint(t, profileID, "planning-schedule-tz-endpoint-"+suffix, "https://planning-schedule-tz.invalid", "planning-schedule-tz-key")
		connectionID := harness.seedConnectionWithOpenAITextCapability(t, profileID, modelConfigID, endpointID, "planning-schedule-tz-connection-"+suffix, nil, nil, 0, runtimeStringPtr("dual_native"))

		// Direct database injection of a bad timezone (the column has no DB
		// CHECK); the failure must be confined to this single connection.
		harness.updateConnectionRoutingSchedule(t, profileID, connectionID, "Not/AZone", [][3]int{{1, 0, 60}})
		harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(publicModelID, "bad timezone"), nil)
		assertStatus(t, response, http.StatusServiceUnavailable)
		payload := runtimeResponsePayload(t, response)
		if payload["error"] != "terminal_target_schedule_unresolvable" {
			t.Fatalf("expected terminal_target_schedule_unresolvable, got %+v", payload)
		}
	})
}
