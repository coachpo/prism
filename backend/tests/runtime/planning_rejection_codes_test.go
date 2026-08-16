package runtimetest

import (
	"context"
	"net/http"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

// TestRuntimeOpenAIPlanningRejectionCodesE2E pins the three stable, mutually
// distinguishable planning codes plus the ordinary dynamic 503 across real
// runtime proxy paths:
//
//   - openai_operation_not_supported (400): root model does not accept the operation;
//   - openai_no_compatible_terminal_target (503): no capability-compatible leaf;
//   - openai_no_eligible_terminal_target (503): compatible leaf exists but statically
//     ineligible (inactive connection, disabled row, single truncation);
//   - ordinary 503 without planning code: statically eligible compatible route was
//     dynamically unavailable (Ban/retry).
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
}
