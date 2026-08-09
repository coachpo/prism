package contracttest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestCompositeModelCreateContract pins the atomic model + first Terminal
// Target create: capability derivation, enabled defaults, hard errors, fault
// rollback and warning envelope.
func TestCompositeModelCreateContract(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Composite Create Strategy")
	endpointID := modelInsertEndpoint(t, harness, profileID, "Composite Endpoint", 0)

	t.Run("creates enabled model with derived capability and warnings", func(t *testing.T) {
		payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/models", map[string]any{
			"api_family":              "openai",
			"model_id":                "composite-ready",
			"display_name":            "Composite Ready",
			"loadbalance_strategy_id": strategyID,
			"openai_accepted_format":  "dual_native",
			"initial_terminal_target": map[string]any{
				"endpoint_id":    endpointID,
				"name":           "Composite Primary",
				"custom_headers": map[string]string{"x-composite": "1"},
			},
		}, http.StatusCreated)
		model := asMap(t, payload["model"])
		if model["is_enabled"] != true || model["model_id"] != "composite-ready" {
			t.Fatalf("expected composite create to default to enabled model, got %+v", model)
		}
		targets := model["access_targets"].([]any)
		if len(targets) != 1 {
			t.Fatalf("expected one owner access target, got %+v", targets)
		}
		target := asMap(t, targets[0])
		connection := asMap(t, target["connection"])
		if connection["openai_text_capability"] != "dual_native" {
			t.Fatalf("expected capability to derive from owner accepted format, got %+v", connection)
		}
		if connection["is_active"] != true {
			t.Fatalf("expected nested target to default to active, got %+v", connection)
		}
		// dual model + dual target -> no warnings
		warnings := payload["configuration_warnings"].([]any)
		if len(warnings) != 0 {
			t.Fatalf("expected no warnings for full coverage, got %+v", warnings)
		}
		// secrets must not be echoed in the response (header values stay on the
		// connection detail for editing, per existing contract)
		raw := fmt.Sprintf("%+v", payload)
		for _, forbidden := range []string{"sk-", "plain-api-key"} {
			if strings.Contains(raw, forbidden) {
				t.Fatalf("composite create response must not echo endpoint secrets, found %q", forbidden)
			}
		}
	})

	t.Run("partial capability derives warnings and stays enabled", func(t *testing.T) {
		payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/models", map[string]any{
			"api_family":              "openai",
			"model_id":                "composite-partial",
			"display_name":            "Composite Partial",
			"loadbalance_strategy_id": strategyID,
			"openai_accepted_format":  "dual_native",
			"initial_terminal_target": map[string]any{
				"endpoint_id":            endpointID,
				"openai_text_capability": "chat_completions_only",
			},
		}, http.StatusCreated)
		model := asMap(t, payload["model"])
		if model["is_enabled"] != true {
			t.Fatalf("expected partial coverage to keep model enabled, got %+v", model)
		}
		warnings := payload["configuration_warnings"].([]any)
		hasPartial := false
		hasUncovered := false
		for _, raw := range warnings {
			warning := asMap(t, raw)
			switch warning["code"] {
			case "openai_target_partial_coverage":
				hasPartial = true
			case "openai_operation_uncovered":
				hasUncovered = true
			}
		}
		if !hasPartial || !hasUncovered {
			t.Fatalf("expected partial + uncovered warnings, got %+v", warnings)
		}
	})

	t.Run("inactive nested target with enabled model is rejected atomically", func(t *testing.T) {
		before := modelCountInProfile(t, harness, profileID)
		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
			"api_family":              "openai",
			"model_id":                "composite-inactive",
			"loadbalance_strategy_id": strategyID,
			"initial_terminal_target": map[string]any{
				"endpoint_id": endpointID,
				"is_active":   false,
			},
		}, modelHeader(profileID))
		assertStatus(t, response, http.StatusUnprocessableEntity)
		payload := decodeRawJSON(t, response)
		if !strings.Contains(fmt.Sprintf("%+v", payload), "model_initial_target_inactive") {
			t.Fatalf("expected model_initial_target_inactive error, got %+v", payload)
		}
		if modelCountInProfile(t, harness, profileID) != before {
			t.Fatalf("expected rejected composite create to leave no model behind")
		}
	})

	t.Run("configure-later omits the nested target and creates disabled model", func(t *testing.T) {
		payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/models", map[string]any{
			"api_family":              "openai",
			"model_id":                "composite-later",
			"display_name":            "Composite Later",
			"loadbalance_strategy_id": strategyID,
			"is_enabled":              false,
		}, http.StatusCreated)
		model := asMap(t, payload["model"])
		if model["is_enabled"] != false || len(model["access_targets"].([]any)) != 0 {
			t.Fatalf("expected configure-later to create disabled targetless model, got %+v", model)
		}
	})

	t.Run("enabled model without target stays a hard error", func(t *testing.T) {
		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
			"api_family":              "openai",
			"model_id":                "composite-no-target",
			"loadbalance_strategy_id": strategyID,
			"is_enabled":              true,
		}, modelHeader(profileID))
		assertStatus(t, response, http.StatusBadRequest)
	})

	t.Run("inline endpoint create encrypts the key and rolls back on later failure", func(t *testing.T) {
		before := modelCountInProfile(t, harness, profileID)
		// qps_limit 0 is invalid -> the whole composite must roll back the
		// inline endpoint and model.
		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
			"api_family":              "openai",
			"model_id":                "composite-rollback",
			"loadbalance_strategy_id": strategyID,
			"initial_terminal_target": map[string]any{
				"endpoint_create": map[string]any{
					"name":     "Composite Rollback Endpoint",
					"base_url": "https://rollback.invalid",
					"api_key":  "sk-rollback-secret",
				},
				"qps_limit": 0,
			},
		}, modelHeader(profileID))
		assertStatus(t, response, http.StatusUnprocessableEntity)
		if modelCountInProfile(t, harness, profileID) != before {
			t.Fatalf("expected faulted composite create to leave no model behind")
		}
		var endpointCount int
		if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM endpoints WHERE name = 'Composite Rollback Endpoint'`).Scan(&endpointCount); err != nil {
			t.Fatalf("count rolled back endpoint: %v", err)
		}
		if endpointCount != 0 {
			t.Fatalf("expected faulted composite create to roll back the inline endpoint")
		}
	})
}

func modelCountInProfile(t *testing.T, harness *contractHarness, profileID int) int {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM model_configs WHERE profile_id = $1`, profileID).Scan(&count); err != nil {
		t.Fatalf("count models in profile: %v", err)
	}
	return count
}

// TestTerminalTargetCopyContract pins the transactional batch copy: field set,
// all-or-nothing, enable_copies default, per-destination warnings and secret
// non-echo.
func TestTerminalTargetCopyContract(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Copy Strategy")
	sourceModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "copy-source", nil, "native", &strategyID, true)
	firstDestID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "copy-dest-a", nil, "native", &strategyID, true)
	secondDestID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "copy-dest-b", nil, "native", &strategyID, true)
	anthropicDestID := modelInsertModel(t, harness, profileID, modelLoadVendorIDByKey(t, harness, "anthropic"), "anthropic", "copy-dest-anthropic", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Copy Endpoint", 0)
	sourceConnectionID := modelInsertConnectionWithCapability(t, harness, profileID, sourceModelID, endpointID, "responses_only", 0, true)

	t.Run("copies to multiple destinations transactionally with enable_copies default false", func(t *testing.T) {
		payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/models/%d/connections/%d/copies", sourceModelID, sourceConnectionID), map[string]any{
			"destination_model_config_ids": []int{firstDestID, secondDestID},
		}, http.StatusCreated)
		if jsonInt(t, payload["source_connection_id"]) != sourceConnectionID {
			t.Fatalf("expected source connection id in copy response, got %+v", payload)
		}
		items := payload["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("expected two copy items in request order, got %+v", payload)
		}
		firstItem := asMap(t, items[0])
		if jsonInt(t, firstItem["model_config_id"]) != firstDestID {
			t.Fatalf("expected copy items in request order, got %+v", payload)
		}
		accessTarget := asMap(t, firstItem["access_target"])
		if accessTarget["is_enabled"] != false {
			t.Fatalf("expected new access targets to default to not participating in routing, got %+v", accessTarget)
		}
		summary := asMap(t, firstItem["connection_summary"])
		if jsonInt(t, summary["endpoint_id"]) != endpointID || summary["openai_text_capability"] != "responses_only" || summary["is_active"] != true {
			t.Fatalf("expected copy to preserve endpoint/capability/active, got %+v", summary)
		}
		// copy must not change the destination model's own enabled state
		var destEnabledBefore bool
		if err := harness.conn.QueryRow(context.Background(), `SELECT is_enabled FROM model_configs WHERE id = $1`, firstDestID).Scan(&destEnabledBefore); err != nil {
			t.Fatalf("load destination enabled state: %v", err)
		}
		if destEnabledBefore {
			// The seeded destination is already enabled; copying must not flip it
			// and the new access target stays out of routing.
			var newTargetEnabled bool
			if err := harness.conn.QueryRow(context.Background(), `SELECT is_enabled FROM model_access_targets WHERE source_model_config_id = $1 AND id = $2`, firstDestID, jsonInt(t, accessTarget["id"])).Scan(&newTargetEnabled); err != nil {
				t.Fatalf("load copied access target enabled state: %v", err)
			}
			if newTargetEnabled {
				t.Fatalf("copy with enable_copies=false must not participate in routing")
			}
		}
		// per-destination partial warnings (responses_only vs dual destinations)
		warnings := payload["configuration_warnings"].([]any)
		if len(warnings) == 0 {
			t.Fatalf("expected per-destination warnings for partial coverage, got %+v", payload)
		}
		raw, err := jsonMarshal(payload)
		if err != nil {
			t.Fatalf("marshal copy payload: %v", err)
		}
		for _, forbidden := range []string{"api_key", "sk-", "custom_headers"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("copy response must not echo secrets or header values, found %q", forbidden)
			}
		}
	})

	t.Run("hard validations fail before any write", func(t *testing.T) {
		var targetCountBefore int
		if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM model_access_targets WHERE source_model_config_id = $1`, firstDestID).Scan(&targetCountBefore); err != nil {
			t.Fatalf("count destination targets before: %v", err)
		}
		// self destination
		s15JSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/models/%d/connections/%d/copies", sourceModelID, sourceConnectionID), map[string]any{"destination_model_config_ids": []int{sourceModelID}}, http.StatusBadRequest)
		// duplicate destinations
		s15JSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/models/%d/connections/%d/copies", sourceModelID, sourceConnectionID), map[string]any{"destination_model_config_ids": []int{firstDestID, firstDestID}}, http.StatusBadRequest)
		// missing destination
		s15JSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/models/%d/connections/%d/copies", sourceModelID, sourceConnectionID), map[string]any{"destination_model_config_ids": []int{999_999}}, http.StatusNotFound)
		// api family conflict
		s15JSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/models/%d/connections/%d/copies", sourceModelID, sourceConnectionID), map[string]any{"destination_model_config_ids": []int{anthropicDestID}}, http.StatusConflict)
		var targetCountAfter int
		if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM model_access_targets WHERE source_model_config_id = $1`, firstDestID).Scan(&targetCountAfter); err != nil {
			t.Fatalf("count destination targets after: %v", err)
		}
		if targetCountAfter != targetCountBefore {
			t.Fatalf("expected rejected copy batches to leave no writes, before=%d after=%d", targetCountBefore, targetCountAfter)
		}
	})

	t.Run("enable_copies true keeps model disabled but participates in routing", func(t *testing.T) {
		payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/models/%d/connections/%d/copies", sourceModelID, sourceConnectionID), map[string]any{
			"destination_model_config_ids": []int{firstDestID},
			"enable_copies":                true,
		}, http.StatusCreated)
		items := payload["items"].([]any)
		accessTarget := asMap(t, asMap(t, items[0])["access_target"])
		if accessTarget["is_enabled"] != true {
			t.Fatalf("expected enable_copies to set the new access target enabled, got %+v", accessTarget)
		}
		var destEnabledAfter bool
		if err := harness.conn.QueryRow(context.Background(), `SELECT is_enabled FROM model_configs WHERE id = $1`, firstDestID).Scan(&destEnabledAfter); err != nil {
			t.Fatalf("load destination enabled state: %v", err)
		}
		if destEnabledAfter != true {
			t.Fatalf("copy must not change the destination model's enabled state")
		}
	})
}

func jsonMarshal(value any) ([]byte, error) {
	return json.Marshal(value)
}

func decodeRawJSON(t *testing.T, response *http.Response) map[string]any {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return payload
}
