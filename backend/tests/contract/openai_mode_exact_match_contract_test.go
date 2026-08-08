package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

// openAIModeExactMatch helpers -------------------------------------------------

func modelInsertOpenAIModelWithMode(t *testing.T, harness *contractHarness, profileID int, modelID string, mode string, strategyID int, isEnabled bool) int {
	t.Helper()
	now := time.Now().UTC()
	var modelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', $2, $2, $3, $4, $5, $6, $6) RETURNING id`, profileID, modelID, strategyID, mode, isEnabled, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert openai model %q mode %q: %v", modelID, mode, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return modelConfigID
}

func modelInsertOpenAIConnectionWithCapability(t *testing.T, harness *contractHarness, profileID int, endpointID int, capability string, isActive bool) int {
	t.Helper()
	now := time.Now().UTC()
	var connectionID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, NULL, NULL, NULL, $3, $4, 0, 'mode-conn', NULL, NULL, 'healthy', NULL, NULL, $5, $5) RETURNING id`, profileID, endpointID, capability, isActive, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert openai connection capability %q: %v", capability, err)
	}
	return connectionID
}

func modelInsertOpenAIConnectionTarget(t *testing.T, harness *contractHarness, profileID int, sourceModelConfigID int, connectionID int, position int, isEnabled bool) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, $5, $6, $6)`, profileID, sourceModelConfigID, connectionID, position, isEnabled, now); err != nil {
		t.Fatalf("insert openai connection target for model %d connection %d: %v", sourceModelConfigID, connectionID, err)
	}
}

func modelLoadModelTargetIDByModelID(t *testing.T, harness *contractHarness, sourceModelConfigID int, targetModelID string) int {
	t.Helper()
	var targetID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT mat.id FROM model_access_targets mat JOIN model_configs tgt ON tgt.id = mat.target_model_config_id WHERE mat.source_model_config_id = $1 AND tgt.model_id = $2`, sourceModelConfigID, targetModelID).Scan(&targetID); err != nil {
		t.Fatalf("load model target id for source %d target %q: %v", sourceModelConfigID, targetModelID, err)
	}
	return targetID
}

func assertStoredModelMode(t *testing.T, harness *contractHarness, modelConfigID int, wantMode string) {
	t.Helper()
	var mode string
	if err := harness.conn.QueryRow(context.Background(), `SELECT openai_accepted_format FROM model_configs WHERE id = $1`, modelConfigID).Scan(&mode); err != nil {
		t.Fatalf("load model %d openai_accepted_format: %v", modelConfigID, err)
	}
	if mode != wantMode {
		t.Fatalf("expected model %d openai_accepted_format %q, got %q", modelConfigID, wantMode, mode)
	}
}

func assertStoredConnectionCapability(t *testing.T, harness *contractHarness, connectionID int, wantCapability string) {
	t.Helper()
	var capability string
	if err := harness.conn.QueryRow(context.Background(), `SELECT openai_text_capability FROM connections WHERE id = $1`, connectionID).Scan(&capability); err != nil {
		t.Fatalf("load connection %d openai_text_capability: %v", connectionID, err)
	}
	if capability != wantCapability {
		t.Fatalf("expected connection %d openai_text_capability %q, got %q", connectionID, wantCapability, capability)
	}
}

func assertModeMismatchRejection(t *testing.T, response *http.Response, wantStatus int) {
	t.Helper()
	assertStatus(t, response, wantStatus)
	payload := decodeJSONMap(t, response)
	issues, ok := payload["routing_plan_issues"].([]any)
	if !ok || len(issues) != 1 {
		t.Fatalf("expected one routing_plan_issue, got %+v", payload)
	}
	issue := asMap(t, issues[0])
	if issue["code"] != "target_openai_mode_mismatch" {
		t.Fatalf("expected target_openai_mode_mismatch issue, got %+v", issue)
	}
}

// Authoring a cross-mode model access target is rejected with 422, including
// disabled targets; same-mode authoring still succeeds.
func TestOpenAIModeExactMatchModelTargetAuthoring(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Mode Exact Match Model Target Strategy")

	responsesModelID := modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-responses-source", "responses_only", strategyID, false)
	modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-chat-target", "chat_completions_only", strategyID, false)
	modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-dual-target", "dual_native", strategyID, false)
	modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-responses-target", "responses_only", strategyID, false)

	// Cross-mode model target authoring is rejected (enabled and disabled alike).
	assertModeMismatchRejection(t, modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(responsesModelID), modelTargetBody("mode-chat-target", 0, true)), http.StatusUnprocessableEntity)
	assertModeMismatchRejection(t, modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(responsesModelID), modelTargetBody("mode-chat-target", 0, false)), http.StatusUnprocessableEntity)
	assertModeMismatchRejection(t, modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(responsesModelID), modelTargetBody("mode-dual-target", 0, true)), http.StatusUnprocessableEntity)
	assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_access_targets WHERE source_model_config_id = $1`, responsesModelID, 0)

	// Same-mode authoring succeeds.
	assertStatus(t, modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(responsesModelID), modelTargetBody("mode-responses-target", 0, true)), http.StatusCreated)
	assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_access_targets WHERE source_model_config_id = $1`, responsesModelID, 1)

	// Retargeting an existing same-mode target to a cross-mode model is rejected.
	sameModeTargetID := modelLoadModelTargetIDByModelID(t, harness, responsesModelID, "mode-responses-target")
	response := modelResponse(t, harness, profileID, http.MethodPatch, modelTargetItemPath(responsesModelID, sameModeTargetID), map[string]any{"target_type": "model", "target_model_id": "mode-chat-target", "is_enabled": true})
	assertModeMismatchRejection(t, response, http.StatusUnprocessableEntity)

	// A cross-mode PATCH must not persist the retarget.
	assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_access_targets WHERE source_model_config_id = $1`, responsesModelID, 1)

	// Same-mode model -> model relations continue to work for dual_native too.
	dualSourceID := modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-dual-source", "dual_native", strategyID, false)
	assertStatus(t, modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(dualSourceID), modelTargetBody("mode-dual-target", 0, true)), http.StatusCreated)
}

// Authoring a connection with a capability different from the owner model mode
// is rejected with 422 (active and inactive alike); same-mode authoring works.
func TestOpenAIModeExactMatchConnectionAuthoring(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Mode Exact Match Connection Strategy")

	responsesModelID := modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-conn-responses-owner", "responses_only", strategyID, false)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Mode Connection Endpoint", 0)

	for _, inactive := range []bool{false, true} {
		response := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", responsesModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "chat_completions_only", "is_active": inactive, "name": "Cross Mode Connection"}, modelHeader(profileID))
		assertErrorResponse(t, response, http.StatusUnprocessableEntity, "openai_text_capability must equal the owner model openai_accepted_format")
	}
	assertConnectionNameCount(t, harness, profileID, "Cross Mode Connection", 0)

	// Same-mode create succeeds.
	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", responsesModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "is_active": true, "name": "Same Mode Connection"}, modelHeader(profileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	connectionID := jsonInt(t, created["id"])
	assertConnectionOwnerTarget(t, harness, responsesModelID, connectionID, 0, true)

	// Updating the connection capability to a cross-mode value is rejected.
	updateResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", responsesModelID, connectionID), map[string]any{"openai_text_capability": "chat_completions_only"}, modelHeader(profileID))
	assertErrorResponse(t, updateResponse, http.StatusUnprocessableEntity, "openai_text_capability must equal the owner model openai_accepted_format")
	assertStoredConnectionCapability(t, harness, connectionID, "responses_only")
}

// Changing a persisted entity mode that breaks an existing relation is a 409.
func TestOpenAIModeExactMatchModeChangeConflicts(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Mode Exact Match Change Conflict Strategy")

	t.Run("model mode change breaks own connection", func(t *testing.T) {
		ownerModelID := modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-change-owner", "dual_native", strategyID, false)
		endpointID := modelInsertEndpoint(t, harness, profileID, "Mode Change Owner Endpoint", 0)
		connectionID := modelInsertOpenAIConnectionWithCapability(t, harness, profileID, endpointID, "dual_native", true)
		modelInsertOpenAIConnectionTarget(t, harness, profileID, ownerModelID, connectionID, 0, true)
		response := modelResponse(t, harness, profileID, http.MethodPut, modelPath(ownerModelID), map[string]any{"openai_accepted_format": "chat_completions_only"})
		assertErrorResponse(t, response, http.StatusConflict, "Cannot change openai_accepted_format while connection access targets exist with a different openai_text_capability")
		assertStoredModelMode(t, harness, ownerModelID, "dual_native")
	})

	t.Run("model mode change breaks inbound referrer", func(t *testing.T) {
		referrerModelID := modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-change-referrer", "dual_native", strategyID, false)
		targetModelID := modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-change-target", "dual_native", strategyID, false)
		modelInsertModelTarget(t, harness, profileID, referrerModelID, targetModelID, 0, true)
		response := modelResponse(t, harness, profileID, http.MethodPut, modelPath(targetModelID), map[string]any{"openai_accepted_format": "responses_only"})
		assertErrorResponse(t, response, http.StatusConflict, "Cannot change openai_accepted_format: models [mode-change-referrer] target this model")
		assertStoredModelMode(t, harness, targetModelID, "dual_native")
	})

	t.Run("model mode change breaks outbound model target", func(t *testing.T) {
		sourceModelID := modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-change-outbound-source", "dual_native", strategyID, false)
		targetModelID := modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-change-outbound-target", "dual_native", strategyID, false)
		modelInsertModelTarget(t, harness, profileID, sourceModelID, targetModelID, 0, true)
		response := modelResponse(t, harness, profileID, http.MethodPut, modelPath(sourceModelID), map[string]any{"openai_accepted_format": "responses_only"})
		assertErrorResponse(t, response, http.StatusConflict, "Cannot change openai_accepted_format while model access targets exist with a different openai_accepted_format")
		assertStoredModelMode(t, harness, sourceModelID, "dual_native")
	})

	t.Run("connection capability change breaking owner is rejected", func(t *testing.T) {
		referencingModelID := modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-change-conn-ref", "dual_native", strategyID, false)
		endpointID := modelInsertEndpoint(t, harness, profileID, "Mode Change Conn Endpoint", 0)
		connectionID := modelInsertOpenAIConnectionWithCapability(t, harness, profileID, endpointID, "dual_native", true)
		modelInsertOpenAIConnectionTarget(t, harness, profileID, referencingModelID, connectionID, 0, true)
		updateResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", referencingModelID, connectionID), map[string]any{"openai_text_capability": "responses_only"}, modelHeader(profileID))
		// The connection is owner-scoped, so the owner equality check rejects first.
		assertErrorResponse(t, updateResponse, http.StatusUnprocessableEntity, "openai_text_capability must equal the owner model openai_accepted_format")
		assertStoredConnectionCapability(t, harness, connectionID, "dual_native")
	})

	t.Run("harmless mode change succeeds", func(t *testing.T) {
		standaloneModelID := modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-change-standalone", "dual_native", strategyID, false)
		response := modelResponse(t, harness, profileID, http.MethodPut, modelPath(standaloneModelID), map[string]any{"openai_accepted_format": "responses_only"})
		assertStatus(t, response, http.StatusOK)
		assertStoredModelMode(t, harness, standaloneModelID, "responses_only")
	})
}

// Non-OpenAI authoring is untouched by mode equality.
func TestOpenAIModeExactMatchNonOpenAIRegression(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Mode Exact Match Non OpenAI Strategy")

	anthropicSourceID := modelInsertModel(t, harness, profileID, "anthropic", "mode-anthropic-source", nil, &strategyID, true)
	modelInsertModel(t, harness, profileID, "anthropic", "mode-anthropic-target", nil, &strategyID, true)
	anthropicOtherID := modelInsertModel(t, harness, profileID, "anthropic", "mode-anthropic-other", nil, &strategyID, true)
	modelInsertModelTarget(t, harness, profileID, anthropicSourceID, anthropicOtherID, 0, true)

	// Cross-family authoring still rejects with the family mismatch channel (400).
	modelInsertModel(t, harness, profileID, "gemini", "mode-gemini-target", nil, &strategyID, true)
	response := modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(anthropicSourceID), modelTargetBody("mode-gemini-target", 1, true))
	assertRoutingPlanIssue(t, response, "Model access targets must use the same api_family as the source model", "target_api_family_mismatch", "access_targets[1].target_model_id")

	// Same-family non-OpenAI target authoring succeeds.
	assertStatus(t, modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(anthropicSourceID), modelTargetBody("mode-anthropic-target", 1, true)), http.StatusCreated)

	// Anthropic connection authoring is unaffected by mode checks.
	endpointID := modelInsertEndpoint(t, harness, profileID, "Mode Anthropic Endpoint", 0)
	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", anthropicSourceID), map[string]any{"api_family": "anthropic", "endpoint_id": endpointID, "is_active": true, "name": "Anthropic Mode Connection"}, modelHeader(profileID))
	assertStatus(t, createResponse, http.StatusCreated)
}

// Concurrent model mode change and target authoring never persists a cross-mode
// relation: either the mode change is blocked (409) or the target add is blocked (422).
func TestOpenAIModeExactMatchConcurrentWrites(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Mode Exact Match Concurrent Strategy")

	sourceModelID := modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-concurrent-source", "dual_native", strategyID, false)
	modelInsertOpenAIModelWithMode(t, harness, profileID, "mode-concurrent-chat-target", "chat_completions_only", strategyID, false)

	var wg sync.WaitGroup
	statuses := make([]int, 2)
	// Request 1: change source mode to chat_completions_only (would make target add valid).
	wg.Add(1)
	go func() {
		defer wg.Done()
		response := modelResponse(t, harness, profileID, http.MethodPut, modelPath(sourceModelID), map[string]any{"openai_accepted_format": "chat_completions_only"})
		statuses[0] = response.StatusCode
		_ = response.Body.Close()
	}()
	// Request 2: author a cross-mode target while the mode change races.
	wg.Add(1)
	go func() {
		defer wg.Done()
		response := modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(sourceModelID), modelTargetBody("mode-concurrent-chat-target", 0, true))
		statuses[1] = response.StatusCode
		_ = response.Body.Close()
	}()
	wg.Wait()

	// The final persisted state must not contain a cross-mode relation.
	var crossModeCount int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM model_access_targets mat JOIN model_configs src ON src.id = mat.source_model_config_id JOIN model_configs tgt ON tgt.id = mat.target_model_config_id WHERE mat.profile_id = $1 AND mat.source_model_config_id = $2 AND src.openai_accepted_format IS DISTINCT FROM tgt.openai_accepted_format`, profileID, sourceModelID).Scan(&crossModeCount); err != nil {
		t.Fatalf("query cross-mode relation count: %v", err)
	}
	if crossModeCount != 0 {
		t.Fatalf("expected no persisted cross-mode relation after concurrent writes, got %d", crossModeCount)
	}
	// Both writes may legitimately succeed only when the mode change committed
	// first and made the relation same-mode; otherwise one write is rejected.
	rejected := 0
	for _, status := range statuses {
		if status == http.StatusConflict || status == http.StatusUnprocessableEntity {
			rejected++
		}
	}
	if rejected == 0 {
		var sourceMode string
		if err := harness.conn.QueryRow(context.Background(), `SELECT openai_accepted_format FROM model_configs WHERE id = $1`, sourceModelID).Scan(&sourceMode); err != nil {
			t.Fatalf("load source model mode: %v", err)
		}
		if sourceMode != "chat_completions_only" {
			t.Fatalf("expected both-success concurrent writes to leave a same-mode state, got source mode %q with statuses %v", sourceMode, statuses)
		}
	}
}
