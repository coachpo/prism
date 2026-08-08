package contracttest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/jackc/pgx/v5/pgconn"
)

const modelConnectionTargetsManagedDetail = "terminal targets are managed through model-scoped connection routes"

func modelJSON[T any](t *testing.T, harness *contractHarness, profileID int, method string, path string, body any, wantStatus int) T {
	t.Helper()
	return requestJSONStatus[T](t, harness, method, path, body, modelHeader(profileID), wantStatus)
}

func modelResponse(t *testing.T, harness *contractHarness, profileID int, method string, path string, body any) *http.Response {
	t.Helper()
	return harness.requestJSON(t, harness.client, method, path, body, modelHeader(profileID))
}

func modelPath(modelID int) string { return fmt.Sprintf("/api/models/%d", modelID) }

func modelTargetListPath(modelID int) string { return fmt.Sprintf("/api/models/%d/targets", modelID) }

func modelTargetItemPath(modelID int, targetID int) string {
	return fmt.Sprintf("/api/models/%d/targets/%d", modelID, targetID)
}

func openAIModelBody(strategyID int, modelID string, extra map[string]any) map[string]any {
	body := map[string]any{"api_family": "openai", "model_id": modelID, "openai_accepted_format": "dual_native", "loadbalance_strategy_id": strategyID}
	for key, value := range extra {
		body[key] = value
	}
	return body
}

func modelTargetBody(modelID string, position int, isEnabled bool) map[string]any {
	return map[string]any{"target_type": "model", "target_model_id": modelID, "position": position, "is_enabled": isEnabled}
}

func assertOpenAIModelPayload(t *testing.T, payload map[string]any, strategyID int, strategyName string, wantDisplayName string, wantEnabled bool, wantTargets []expectedAccessTarget) {
	t.Helper()
	assertNoLegacyModelFields(t, payload)
	assertAccessTargets(t, payload, wantTargets)
	if payload["openai_accepted_format"] != "dual_native" || payload["display_name"] != wantDisplayName || payload["is_enabled"] != wantEnabled {
		t.Fatalf("unexpected model payload: %+v", payload)
	}
	assertModelLoadbalanceStrategySummary(t, payload["loadbalance_strategy"], strategyID, strategyName)
}

func assertRoutingPlanIssue(t *testing.T, response *http.Response, wantDetail string, wantCode string, wantPath string) {
	t.Helper()
	assertStatus(t, response, http.StatusBadRequest)
	payload := decodeJSONMap(t, response)
	if payload["detail"] != wantDetail {
		t.Fatalf("unexpected routing detail: %+v", payload)
	}
	issues, ok := payload["routing_plan_issues"].([]any)
	if !ok || len(issues) != 1 {
		t.Fatalf("expected one routing_plan_issue, got %+v", payload)
	}
	issue := asMap(t, issues[0])
	if issue["code"] != wantCode || issue["path"] != wantPath || issue["message"] != wantDetail {
		t.Fatalf("unexpected routing_plan_issue: %+v", issue)
	}
}

func TestModelCRUD(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S8 Access Strategy")
	targetModelID := modelInsertModel(t, harness, profileID, "openai", "s8-target-model", stringPtr("S8 Target Model"), &strategyID, true)
	modelInsertModel(t, harness, profileID, "anthropic", "s8-anthropic-target", stringPtr("S8 Anthropic Target"), &strategyID, true)

	defaultScopedModel := findModelListItemByModelID(t, requestJSONStatus[[]any](t, harness, http.MethodGet, "/api/models", nil, nil, http.StatusOK), "s8-target-model")
	if jsonInt(t, defaultScopedModel["profile_id"]) != profileID {
		t.Fatalf("expected missing profile header to resolve Default profile %d, got %+v", profileID, defaultScopedModel)
	}

	draft := modelJSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/models", openAIModelBody(strategyID, "s8-access-draft", map[string]any{"display_name": "S8 Access Draft"}), http.StatusCreated)
	assertOpenAIModelPayload(t, draft, strategyID, "S8 Access Strategy", "S8 Access Draft", false, nil)

	source := modelJSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/models", openAIModelBody(strategyID, "s8-access-model", map[string]any{"display_name": "S8 Access Model"}), http.StatusCreated)
	sourceModelID := jsonInt(t, source["id"])
	assertOpenAIModelPayload(t, source, strategyID, "S8 Access Strategy", "S8 Access Model", false, nil)

	wantTargets := []expectedAccessTarget{{TargetType: "model", TargetModelID: "s8-target-model", Position: 0, IsEnabled: true}}
	createdTargets := modelJSON[[]any](t, harness, profileID, http.MethodPost, modelTargetListPath(sourceModelID), modelTargetBody("s8-target-model", 0, true), http.StatusCreated)
	assertAccessTargets(t, map[string]any{"access_targets": createdTargets}, wantTargets)
	createdTargetID := jsonInt(t, asMap(t, createdTargets[0])["id"])
	assertStoredModelTargetFlat(t, harness, sourceModelID, targetModelID, 0, true)

	modelJSON[map[string]any](t, harness, profileID, http.MethodPut, modelPath(sourceModelID), map[string]any{"is_enabled": true}, http.StatusOK)
	listedModels := modelJSON[[]any](t, harness, profileID, http.MethodGet, "/api/models", nil, http.StatusOK)
	for _, payload := range []map[string]any{
		modelJSON[map[string]any](t, harness, profileID, http.MethodGet, modelPath(sourceModelID), nil, http.StatusOK),
		findModelListItemByModelID(t, listedModels, "s8-access-model"),
	} {
		assertOpenAIModelPayload(t, payload, strategyID, "S8 Access Strategy", "S8 Access Model", true, wantTargets)
		if targets := payload["access_targets"].([]any); len(targets) != 1 || asMap(t, asMap(t, targets[0])["target_model"])["openai_accepted_format"] != "dual_native" {
			t.Fatalf("expected target model openai_accepted_format dual_native, got %+v", payload)
		}
	}
	if otherFamilyItem := findModelListItemByModelID(t, listedModels, "s8-anthropic-target"); otherFamilyItem["openai_accepted_format"] != nil {
		t.Fatalf("expected non-OpenAI model to omit openai_accepted_format, got %+v", otherFamilyItem)
	}

	updated := modelJSON[map[string]any](t, harness, profileID, http.MethodPut, modelPath(sourceModelID), map[string]any{"display_name": nil}, http.StatusOK)
	assertOpenAIModelPayload(t, updated, strategyID, "S8 Access Strategy", "s8-access-model", true, wantTargets)
	assertStoredModelTargetFlat(t, harness, sourceModelID, targetModelID, 0, true)

	validations := []struct {
		name  string
		run   func() *http.Response
		check func(*http.Response)
	}{
		{"legacy shape", func() *http.Response {
			return modelResponse(t, harness, profileID, http.MethodPost, "/api/models", openAIModelBody(strategyID, "legacy-shape", map[string]any{"model_type": "native"}))
		}, func(response *http.Response) { assertStatus(t, response, http.StatusBadRequest) }},
		{"missing strategy", func() *http.Response {
			return modelResponse(t, harness, profileID, http.MethodPost, "/api/models", map[string]any{"api_family": "openai", "model_id": "missing-strategy", "openai_accepted_format": "dual_native"})
		}, func(response *http.Response) {
			assertErrorResponse(t, response, http.StatusBadRequest, "loadbalance_strategy_id is required")
		}},
		{"legacy access_targets", func() *http.Response {
			return modelResponse(t, harness, profileID, http.MethodPost, "/api/models", openAIModelBody(strategyID, "s8-legacy-targets", map[string]any{"access_targets": []map[string]any{}}))
		}, func(response *http.Response) {
			assertErrorResponse(t, response, http.StatusBadRequest, "Invalid request body")
		}},
		{"enabled zero targets", func() *http.Response {
			return modelResponse(t, harness, profileID, http.MethodPost, "/api/models", openAIModelBody(strategyID, "s8-enabled-zero-targets", map[string]any{"is_enabled": true}))
		}, func(response *http.Response) {
			assertErrorResponse(t, response, http.StatusBadRequest, "enabled models must include at least one enabled access target")
		}},
		{"duplicate create", func() *http.Response {
			return modelResponse(t, harness, profileID, http.MethodPost, "/api/models", openAIModelBody(strategyID, "s8-access-model", nil))
		}, func(response *http.Response) {
			assertErrorResponse(t, response, http.StatusConflict, "Model ID 's8-access-model' already exists")
		}},
		{"disable only target", func() *http.Response {
			return modelResponse(t, harness, profileID, http.MethodPatch, modelTargetItemPath(sourceModelID, createdTargetID), map[string]any{"is_enabled": false})
		}, func(response *http.Response) {
			assertErrorResponse(t, response, http.StatusBadRequest, "enabled models must include at least one enabled access target")
		}},
		{"wrong family target", func() *http.Response {
			return modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(sourceModelID), modelTargetBody("s8-anthropic-target", 1, true))
		}, func(response *http.Response) {
			assertErrorResponse(t, response, http.StatusBadRequest, "Model access targets must use the same api_family as the source model")
		}},
		{"self target", func() *http.Response {
			return modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(sourceModelID), modelTargetBody("s8-access-model", 1, true))
		}, func(response *http.Response) {
			assertErrorResponse(t, response, http.StatusBadRequest, "Model access target cannot target itself")
		}},
		{"delete referenced target", func() *http.Response {
			return modelResponse(t, harness, profileID, http.MethodDelete, modelPath(targetModelID), nil)
		}, func(response *http.Response) {
			assertErrorResponse(t, response, http.StatusConflict, "Cannot delete: models [s8-access-model] target this model")
		}},
		{"cycle update", func() *http.Response {
			return modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(targetModelID), modelTargetBody("s8-access-model", 0, true))
		}, func(response *http.Response) {
			assertErrorResponse(t, response, http.StatusBadRequest, "access_targets cannot introduce a model target cycle")
		}},
	}
	for _, tc := range validations {
		t.Run(tc.name, func(t *testing.T) { tc.check(tc.run()) })
	}

	renamed := modelJSON[map[string]any](t, harness, profileID, http.MethodPut, modelPath(sourceModelID), map[string]any{"model_id": "s8-access-model-renamed", "display_name": nil}, http.StatusOK)
	if renamed["model_id"] != "s8-access-model-renamed" || renamed["display_name"] != "s8-access-model-renamed" {
		t.Fatalf("expected rename payload to resync display_name, got %+v", renamed)
	}

	assertStatus(t, modelResponse(t, harness, profileID, http.MethodDelete, modelPath(sourceModelID), nil), http.StatusOK)
	assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_access_targets WHERE source_model_config_id = $1`, sourceModelID, 0)
	assertStatus(t, modelResponse(t, harness, profileID, http.MethodDelete, modelPath(targetModelID), nil), http.StatusOK)
}

func TestModelTargetMetadataAndObsoleteFields(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Target Metadata Strategy")
	terminalTargetID := modelInsertModel(t, harness, profileID, "openai", "metadata-target", nil, &strategyID, true)
	sourceModelID := modelInsertModel(t, harness, profileID, "openai", "target-metadata-source", nil, &strategyID, true)

	assertErrorResponse(t, modelResponse(t, harness, profileID, http.MethodPost, "/api/models", openAIModelBody(strategyID, "obsolete-weight-model", map[string]any{"access_targets": []map[string]any{}})), http.StatusBadRequest, "Invalid request body")
	for path, body := range map[string]map[string]any{
		"target_priority": {"target_type": "model", "target_model_id": "metadata-target", "position": 0, "target_priority": 0, "is_enabled": true},
		"weight":          {"target_type": "model", "target_model_id": "metadata-target", "position": 0, "weight": 1, "is_enabled": true},
	} {
		assertObsoleteAccessTargetError(t, modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(sourceModelID), body), path)
	}

	wantTargets := []expectedAccessTarget{{TargetType: "model", TargetModelID: "metadata-target", Position: 0, IsEnabled: true}}
	createdTargets := modelJSON[[]any](t, harness, profileID, http.MethodPost, modelTargetListPath(sourceModelID), modelTargetBody("metadata-target", 0, true), http.StatusCreated)
	assertAccessTargets(t, map[string]any{"access_targets": createdTargets}, wantTargets)
	createdTargetID := jsonInt(t, asMap(t, createdTargets[0])["id"])
	assertStoredModelTargetFlat(t, harness, sourceModelID, terminalTargetID, 0, true)
	assertAccessTargets(t, map[string]any{"access_targets": modelJSON[[]any](t, harness, profileID, http.MethodPatch, modelTargetItemPath(sourceModelID, createdTargetID), map[string]any{"position": 0, "is_enabled": true}, http.StatusOK)}, wantTargets)
	assertAccessTargets(t, map[string]any{"access_targets": modelJSON[[]any](t, harness, profileID, http.MethodGet, modelTargetListPath(sourceModelID), nil, http.StatusOK)}, wantTargets)

	endpointID := modelInsertEndpoint(t, harness, profileID, "Target Metadata Connection Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, sourceModelID, endpointID, 1, true, nil)
	connectionTargetID := modelLoadConnectionTargetID(t, harness, sourceModelID, connectionID)
	for path, body := range map[string]map[string]any{
		"weight":          {"weight": 2},
		"target_priority": {"target_priority": 0},
	} {
		assertObsoleteAccessTargetError(t, modelResponse(t, harness, profileID, http.MethodPatch, modelTargetItemPath(sourceModelID, connectionTargetID), body), path)
	}
	assertConnectionTargetState(t, harness, sourceModelID, connectionTargetID, connectionID, 1, true)
}

func TestModelValidationContracts(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Validation Strategy")

	t.Run("openai accepted format", func(t *testing.T) {
		anthropicModelID := modelInsertModel(t, harness, profileID, "anthropic", "openai-format-validation-anthropic", nil, &strategyID, false)
		openAIModelID := modelInsertModel(t, harness, profileID, "openai", "openai-format-validation-openai", nil, &strategyID, false)
		request, err := http.NewRequest(http.MethodPost, harness.url+"/api/models", strings.NewReader(fmt.Sprintf(`{"api_family":"openai","model_id":"missing-openai-accepted-format","loadbalance_strategy_id":%d,"is_enabled":false}`, strategyID)))
		if err != nil {
			t.Fatalf("build raw request: %v", err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(profiledomain.ProfileIDHeader, fmt.Sprintf("%d", profileID))
		response, err := harness.client.Do(request)
		if err != nil {
			t.Fatalf("perform raw request: %v", err)
		}
		t.Cleanup(func() { _ = response.Body.Close() })
		assertErrorResponse(t, response, http.StatusBadRequest, "openai_accepted_format is required when api_family is 'openai'")
		assertStatus(t, modelResponse(t, harness, profileID, http.MethodPut, modelPath(openAIModelID), map[string]any{
			"context_window_tokens": 8192, "default_output_token_reserve": 4096, "max_context_utilization": 0.9, "preferred_context_utilization_threshold": 0.8,
		}), http.StatusBadRequest)
		assertErrorResponse(t, modelResponse(t, harness, profileID, http.MethodPut, modelPath(anthropicModelID), map[string]any{"openai_accepted_format": "dual_native"}), http.StatusBadRequest, "openai_accepted_format is only allowed when api_family is 'openai'")
	})

	t.Run("preferred context", func(t *testing.T) {
		assertErrorResponse(t, modelResponse(t, harness, profileID, http.MethodPost, "/api/models", openAIModelBody(strategyID, "preferred-context-model", map[string]any{
			"max_context_utilization": 0.75, "preferred_context_utilization_threshold": 0.70,
		})), http.StatusBadRequest, "Invalid request body")
	})

	t.Run("wrong family target", func(t *testing.T) {
		modelInsertModel(t, harness, profileID, "anthropic", "wrong-family-target", nil, &strategyID, true)
		sourceID := modelInsertModel(t, harness, profileID, "openai", "wrong-family-source", nil, &strategyID, false)
		assertErrorResponse(t, modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(sourceID), modelTargetBody("wrong-family-target", 0, true)), http.StatusBadRequest, "Model access targets must use the same api_family as the source model")
	})

	t.Run("sparse positions", func(t *testing.T) {
		modelInsertModel(t, harness, profileID, "openai", "sparse-position-target", nil, &strategyID, true)
		sourceID := modelInsertModel(t, harness, profileID, "openai", "sparse-position-source", nil, &strategyID, false)
		assertErrorResponse(t, modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(sourceID), modelTargetBody("sparse-position-target", 3, true)), http.StatusBadRequest, "position must be between 0 and 0")
	})

	t.Run("self target routing issue", func(t *testing.T) {
		sourceID := modelInsertModel(t, harness, profileID, "openai", "routing-issue-self-target", nil, &strategyID, false)
		assertRoutingPlanIssue(t, modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(sourceID), modelTargetBody("routing-issue-self-target", 0, true)), "Model access target cannot target itself", "model_graph_cycle", "access_targets[0].target_model_id")
	})

	t.Run("wrong profile target", func(t *testing.T) {
		otherProfileID := modelInsertProfile(t, harness, "Other Profile")
		otherEndpointID := modelInsertEndpoint(t, harness, otherProfileID, "Other Profile Endpoint", 0)
		otherConnectionID := modelInsertStandaloneConnection(t, harness, otherProfileID, "openai", otherEndpointID, 0, true, nil)
		sourceID := modelInsertModel(t, harness, profileID, "openai", "wrong-profile-source", nil, &strategyID, false)
		assertErrorResponse(t, modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(sourceID), map[string]any{"target_type": "connection", "connection_id": otherConnectionID, "position": 0, "is_enabled": true}), http.StatusBadRequest, modelConnectionTargetsManagedDetail)
	})
}

func TestModelDeleteContracts(t *testing.T) {
	t.Run("referenced target", func(t *testing.T) {
		harness := newEndpointConnectionContractHarness(t)
		profileID := modelLoadDefaultProfileID(t, harness)
		strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Delete Referenced Strategy")
		targetID := modelInsertModel(t, harness, profileID, "openai", "delete-target", nil, &strategyID, true)
		sourceID := modelInsertModel(t, harness, profileID, "openai", "delete-source", nil, &strategyID, true)
		modelInsertModelTarget(t, harness, profileID, sourceID, targetID, 0, true)
		assertErrorResponse(t, modelResponse(t, harness, profileID, http.MethodDelete, modelPath(targetID), nil), http.StatusConflict, "Cannot delete: models [delete-source] target this model")
	})

	t.Run("delete owner and rollback", func(t *testing.T) {
		harness := newEndpointConnectionContractHarness(t)
		profileID := modelLoadDefaultProfileID(t, harness)
		strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Task 5 Delete Owner Strategy")
		ownerID := modelInsertModel(t, harness, profileID, "openai", "task5-delete-owner", nil, &strategyID, true)
		targetID := modelInsertModel(t, harness, profileID, "openai", "task5-delete-owner-target", nil, &strategyID, true)
		endpointID := modelInsertEndpoint(t, harness, profileID, "Task 5 Delete Owner Endpoint", 0)
		connectionAID := modelInsertConnection(t, harness, profileID, ownerID, endpointID, 0, true, nil)
		connectionBID := modelInsertConnection(t, harness, profileID, ownerID, endpointID, 1, true, nil)
		modelInsertModelTarget(t, harness, profileID, ownerID, targetID, 2, true)
		assertStatus(t, modelResponse(t, harness, profileID, http.MethodDelete, modelPath(ownerID), nil), http.StatusOK)
		assertStoredConnectionCount(t, harness, connectionAID, 0)
		assertStoredConnectionCount(t, harness, connectionBID, 0)
		assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_access_targets WHERE source_model_config_id = $1`, ownerID, 0)
		assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_configs WHERE id = $1`, ownerID, 0)
		assertCountQuery(t, harness, `SELECT COUNT(*) FROM endpoints WHERE id = $1`, endpointID, 1)

		rollbackOwnerID := modelInsertModel(t, harness, profileID, "openai", "task5-delete-rollback-owner", nil, &strategyID, true)
		rollbackConnectionID := modelInsertConnection(t, harness, profileID, rollbackOwnerID, endpointID, 0, true, nil)
		if _, err := harness.conn.Exec(context.Background(), `CREATE OR REPLACE FUNCTION task5_fail_connection_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced private connection delete failure'; END; $$`); err != nil {
			t.Fatalf("install connection delete failure function: %v", err)
		}
		if _, err := harness.conn.Exec(context.Background(), `CREATE TRIGGER task5_fail_connection_delete BEFORE DELETE ON connections FOR EACH ROW EXECUTE FUNCTION task5_fail_connection_delete()`); err != nil {
			t.Fatalf("install connection delete failure trigger: %v", err)
		}
		assertErrorResponse(t, modelResponse(t, harness, profileID, http.MethodDelete, modelPath(rollbackOwnerID), nil), http.StatusInternalServerError, "Internal server error")
		assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_configs WHERE id = $1`, rollbackOwnerID, 1)
		assertStoredConnectionCount(t, harness, rollbackConnectionID, 1)
		assertModelConnectionTargetCount(t, harness, rollbackOwnerID, 1)
	})

	t.Run("delete still blocked when targeted", func(t *testing.T) {
		harness := newEndpointConnectionContractHarness(t)
		profileID := modelLoadDefaultProfileID(t, harness)
		strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Task 5 Referenced Delete Strategy")
		ownerID := modelInsertModel(t, harness, profileID, "openai", "task5-referenced-owner", nil, &strategyID, true)
		referrerID := modelInsertModel(t, harness, profileID, "openai", "task5-referenced-referrer", nil, &strategyID, true)
		endpointID := modelInsertEndpoint(t, harness, profileID, "Task 5 Referenced Endpoint", 0)
		connectionID := modelInsertConnection(t, harness, profileID, ownerID, endpointID, 0, true, nil)
		modelInsertModelTarget(t, harness, profileID, referrerID, ownerID, 0, true)
		assertErrorResponse(t, modelResponse(t, harness, profileID, http.MethodDelete, modelPath(ownerID), nil), http.StatusConflict, "Cannot delete: models [task5-referenced-referrer] target this model")
		assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_configs WHERE id = $1`, ownerID, 1)
		assertStoredConnectionCount(t, harness, connectionID, 1)
		assertModelConnectionTargetCount(t, harness, ownerID, 1)
	})
}

func TestModelConnectionTargetContracts(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Connection Target Strategy")

	t.Run("owner uniqueness", func(t *testing.T) {
		endpointID := modelInsertEndpoint(t, harness, profileID, "Connection Owner Endpoint", 0)
		connectionID := modelInsertStandaloneConnection(t, harness, profileID, "openai", endpointID, 0, true, nil)
		firstSourceID := modelInsertModel(t, harness, profileID, "openai", "connection-owner-source-a", nil, &strategyID, true)
		modelInsertConnectionTarget(t, harness, profileID, firstSourceID, connectionID, 0, true)
		assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_access_targets WHERE target_connection_id = $1`, connectionID, 1)

		secondSourceID := modelInsertModel(t, harness, profileID, "openai", "connection-owner-source-b", nil, &strategyID, true)
		now := time.Now().UTC()
		_, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4)`, profileID, secondSourceID, connectionID, now)
		if err == nil {
			t.Fatal("expected duplicate connection owner insert to fail")
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Fatalf("expected PostgreSQL unique constraint error, got %T: %v", err, err)
		}
		if pgErr.Code != "23505" || pgErr.ConstraintName != "uq_model_access_targets_connection_owner" {
			t.Fatalf("expected uq_model_access_targets_connection_owner unique violation, got code=%s constraint=%s error=%v", pgErr.Code, pgErr.ConstraintName, err)
		}
	})

	t.Run("reject arbitrary connection assignment", func(t *testing.T) {
		modelInsertModel(t, harness, profileID, "openai", "reject-connection-target-model", nil, &strategyID, true)
		endpointID := modelInsertEndpoint(t, harness, profileID, "Reject Connection Assignment Endpoint", 0)
		connectionID := modelInsertStandaloneConnection(t, harness, profileID, "openai", endpointID, 0, true, nil)
		assertErrorResponse(t, modelResponse(t, harness, profileID, http.MethodPost, "/api/models", openAIModelBody(strategyID, "reject-connection-source", map[string]any{
			"access_targets": []map[string]any{{"target_type": "connection", "connection_id": connectionID, "position": 0, "is_enabled": true}},
		})), http.StatusBadRequest, "Invalid request body")

		sourceModelID := modelInsertModel(t, harness, profileID, "openai", "reject-connection-existing-source", nil, &strategyID, true)
		assertErrorResponse(t, modelResponse(t, harness, profileID, http.MethodPut, modelPath(sourceModelID), map[string]any{
			"access_targets": []map[string]any{{"target_type": "connection", "connection_id": connectionID, "position": 0, "is_enabled": true}},
		}), http.StatusBadRequest, "Invalid request body")
		assertErrorResponse(t, modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(sourceModelID), map[string]any{"target_type": "connection", "connection_id": connectionID, "position": 0, "is_enabled": true}), http.StatusBadRequest, modelConnectionTargetsManagedDetail)
		assertStatus(t, modelResponse(t, harness, profileID, http.MethodPost, modelTargetListPath(sourceModelID), modelTargetBody("reject-connection-target-model", 0, true)), http.StatusCreated)
		assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_access_targets WHERE target_connection_id = $1`, connectionID, 0)
	})

	t.Run("reject connection retarget", func(t *testing.T) {
		targetModelID := modelInsertModel(t, harness, profileID, "openai", "reject-retarget-model-target", nil, &strategyID, true)
		sourceModelID := modelInsertModel(t, harness, profileID, "openai", "reject-retarget-source", nil, &strategyID, true)
		modelInsertModelTarget(t, harness, profileID, sourceModelID, targetModelID, 0, true)
		endpointID := modelInsertEndpoint(t, harness, profileID, "Reject Connection Retarget Endpoint", 0)
		connectionAID := modelInsertStandaloneConnection(t, harness, profileID, "openai", endpointID, 0, true, nil)
		connectionBID := modelInsertStandaloneConnection(t, harness, profileID, "openai", endpointID, 1, true, nil)
		modelInsertConnectionTarget(t, harness, profileID, sourceModelID, connectionAID, 1, true)
		targetID := modelLoadConnectionTargetID(t, harness, sourceModelID, connectionAID)
		assertErrorResponse(t, modelResponse(t, harness, profileID, http.MethodPatch, modelTargetItemPath(sourceModelID, targetID), map[string]any{"target_type": "connection", "target_connection_id": connectionBID, "position": 0, "is_enabled": false}), http.StatusBadRequest, modelConnectionTargetsManagedDetail)
		assertConnectionTargetState(t, harness, sourceModelID, targetID, connectionAID, 1, true)
		assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_access_targets WHERE target_connection_id = $1`, connectionBID, 0)
		assertAccessTargets(t, map[string]any{"access_targets": modelJSON[[]any](t, harness, profileID, http.MethodPatch, modelTargetItemPath(sourceModelID, targetID), map[string]any{"position": 1, "is_enabled": false}, http.StatusOK)}, []expectedAccessTarget{
			{TargetType: "model", TargetModelID: "reject-retarget-model-target", Position: 0, IsEnabled: true},
			{TargetType: "connection", ConnectionID: connectionAID, Position: 1, IsEnabled: false},
		})
	})

	t.Run("preserve owned private connection targets", func(t *testing.T) {
		targetModelID := modelInsertModel(t, harness, profileID, "openai", "preserve-model-target", nil, &strategyID, true)
		sourceModelID := modelInsertModel(t, harness, profileID, "openai", "preserve-source", nil, &strategyID, true)
		endpointID := modelInsertEndpoint(t, harness, profileID, "Preserve Private Connection Endpoint", 0)
		connectionID := modelInsertConnection(t, harness, profileID, sourceModelID, endpointID, 0, true, nil)
		connectionTargetID := modelLoadConnectionTargetID(t, harness, sourceModelID, connectionID)
		assertAccessTargets(t, map[string]any{"access_targets": modelJSON[[]any](t, harness, profileID, http.MethodPost, modelTargetListPath(sourceModelID), modelTargetBody("preserve-model-target", 1, true), http.StatusCreated)}, []expectedAccessTarget{
			{TargetType: "connection", ConnectionID: connectionID, Position: 0, IsEnabled: true},
			{TargetType: "model", TargetModelID: "preserve-model-target", Position: 1, IsEnabled: true},
		})
		assertConnectionTargetState(t, harness, sourceModelID, connectionTargetID, connectionID, 0, true)
		_ = targetModelID
	})
}

func TestModelEndpointHelpers(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Endpoint Helper Strategy")

	t.Run("by endpoints", func(t *testing.T) {
		modelAID := modelInsertModel(t, harness, profileID, "openai", "helper-a", stringPtr("Helper A"), &strategyID, true)
		modelBID := modelInsertModel(t, harness, profileID, "openai", "helper-b", nil, &strategyID, true)
		endpointAID := modelInsertEndpoint(t, harness, profileID, "S8 Helper Endpoint A", 0)
		endpointBID := modelInsertEndpoint(t, harness, profileID, "S8 Helper Endpoint B", 1)
		modelInsertConnection(t, harness, profileID, modelBID, endpointAID, 2, true, nil)
		modelInsertConnection(t, harness, profileID, modelBID, endpointBID, 1, true, nil)
		modelInsertConnection(t, harness, profileID, modelAID, endpointBID, 0, true, nil)
		modelInsertConnection(t, harness, profileID, modelAID, endpointBID, 3, false, nil)
		modelInsertRequestLog(t, harness, profileID, "helper-a", "openai", 200, "helper-a-success")
		modelInsertRequestLog(t, harness, profileID, "helper-a", "openai", 500, "helper-a-failure")
		payload := modelJSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/models/by-endpoints", map[string]any{"endpoint_ids": []int{endpointBID, 999999, endpointAID}}, http.StatusOK)
		items := payload["items"].([]any)
		if len(items) != 3 {
			t.Fatalf("expected helper route to preserve three endpoint envelopes, got %+v", payload)
		}
		assertEndpointModelsBatchItem(t, asMap(t, items[0]), endpointBID, []string{"helper-a", "helper-b"})
		assertEndpointModelsBatchItem(t, asMap(t, items[1]), 999999, nil)
		assertEndpointModelsBatchItem(t, asMap(t, items[2]), endpointAID, []string{"helper-b"})
		firstModels := asMap(t, items[0])["models"].([]any)
		assertModelListItemCounts(t, asMap(t, firstModels[0]), modelAID, 2, 1, 2, 50)
		assertModelListItemCounts(t, asMap(t, firstModels[1]), modelBID, 1, 1, 0, nil)
		emptyPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/models/by-endpoints", map[string]any{"endpoint_ids": []int{}}, http.StatusOK)
		if items, ok := emptyPayload["items"].([]any); !ok || len(items) != 0 {
			t.Fatalf("expected empty endpoint_ids to return an empty items list, got %+v", emptyPayload)
		}
	})

	t.Run("by endpoint", func(t *testing.T) {
		modelZID := modelInsertModel(t, harness, profileID, "openai", "z-model", nil, &strategyID, true)
		modelAID := modelInsertModel(t, harness, profileID, "openai", "a-model", stringPtr("Model A"), &strategyID, true)
		facadeID := modelInsertModel(t, harness, profileID, "openai", "facade-model", stringPtr("Facade Model"), &strategyID, true)
		disabledFacadeID := modelInsertModel(t, harness, profileID, "openai", "disabled-facade", nil, &strategyID, false)
		endpointID := modelInsertEndpoint(t, harness, profileID, "S8 By Endpoint", 0)
		modelInsertConnection(t, harness, profileID, modelZID, endpointID, 5, true, nil)
		modelInsertConnection(t, harness, profileID, modelAID, endpointID, 1, false, nil)
		modelInsertModelTarget(t, harness, profileID, facadeID, modelZID, 0, true)
		modelInsertModelTarget(t, harness, profileID, disabledFacadeID, modelZID, 0, true)
		models := modelJSON[[]map[string]any](t, harness, profileID, http.MethodGet, fmt.Sprintf("/api/models/by-endpoint/%d", endpointID), nil, http.StatusOK)
		if len(models) != 3 || models[0]["model_id"] != "a-model" || models[1]["model_id"] != "facade-model" || models[2]["model_id"] != "z-model" {
			t.Fatalf("expected by-endpoint helper to sort enabled reachable models, got %+v", models)
		}
		assertModelListItemCounts(t, models[0], modelAID, 1, 0, 0, nil)
		assertModelListItemCounts(t, models[1], facadeID, 1, 1, 0, nil)
		assertModelListItemCounts(t, models[2], modelZID, 1, 1, 0, nil)
	})

	t.Run("batch dedupe", func(t *testing.T) {
		terminalID := modelInsertModel(t, harness, profileID, "openai", "batch-terminal", nil, &strategyID, true)
		facadeID := modelInsertModel(t, harness, profileID, "openai", "batch-facade", nil, &strategyID, true)
		endpointID := modelInsertEndpoint(t, harness, profileID, "Endpoint Batch Dedupe", 0)
		terminalConnectionID := modelInsertConnection(t, harness, profileID, terminalID, endpointID, 0, true, nil)
		facadeConnectionID := modelInsertConnection(t, harness, profileID, facadeID, endpointID, 1, true, nil)
		modelInsertModelTarget(t, harness, profileID, facadeID, terminalID, 0, true)
		assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_access_targets WHERE target_connection_id = $1`, terminalConnectionID, 1)
		assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_access_targets WHERE target_connection_id = $1`, facadeConnectionID, 1)
		payload := modelJSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/models/by-endpoints", map[string]any{"endpoint_ids": []int{endpointID}}, http.StatusOK)
		items := payload["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("expected one endpoint envelope, got %+v", payload)
		}
		item := asMap(t, items[0])
		assertEndpointModelsBatchItem(t, item, endpointID, []string{"batch-facade", "batch-terminal"})
		models := item["models"].([]any)
		assertModelListItemCounts(t, asMap(t, models[0]), facadeID, 2, 2, 0, nil)
		assertModelListItemCounts(t, asMap(t, models[1]), terminalID, 1, 1, 0, nil)
	})

	t.Run("reachable connection count", func(t *testing.T) {
		terminalID := modelInsertModel(t, harness, profileID, "openai", "count-terminal", nil, &strategyID, true)
		facadeID := modelInsertModel(t, harness, profileID, "openai", "count-facade", nil, &strategyID, true)
		recursiveFacadeID := modelInsertModel(t, harness, profileID, "openai", "count-recursive-facade", nil, &strategyID, true)
		endpointAID := modelInsertEndpoint(t, harness, profileID, "Reachable Count Endpoint A", 0)
		endpointBID := modelInsertEndpoint(t, harness, profileID, "Reachable Count Endpoint B", 1)
		modelInsertConnection(t, harness, profileID, terminalID, endpointAID, 0, true, nil)
		modelInsertConnection(t, harness, profileID, terminalID, endpointAID, 1, false, nil)
		modelInsertConnection(t, harness, profileID, terminalID, endpointBID, 2, true, nil)
		inactiveTargetConnectionID := modelInsertStandaloneConnection(t, harness, profileID, "openai", endpointAID, 3, true, nil)
		modelInsertModelTarget(t, harness, profileID, facadeID, terminalID, 0, true)
		modelInsertConnectionTarget(t, harness, profileID, facadeID, inactiveTargetConnectionID, 1, false)
		modelInsertModelTarget(t, harness, profileID, recursiveFacadeID, facadeID, 0, true)
		models := modelJSON[[]any](t, harness, profileID, http.MethodGet, "/api/models", nil, http.StatusOK)
		assertModelListItemCounts(t, findModelListItemByModelID(t, models, "count-terminal"), terminalID, 3, 2, 0, nil)
		assertModelListItemCounts(t, findModelListItemByModelID(t, models, "count-facade"), facadeID, 3, 2, 0, nil)
		assertModelListItemCounts(t, findModelListItemByModelID(t, models, "count-recursive-facade"), recursiveFacadeID, 3, 2, 0, nil)
	})
}

func modelHeader(profileID int) map[string]string {
	return map[string]string{profiledomain.ProfileIDHeader: fmt.Sprintf("%d", profileID)}
}

func modelLoadDefaultProfileID(t *testing.T, harness *contractHarness) int {
	t.Helper()
	var profileID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM profiles WHERE is_default = TRUE ORDER BY id ASC LIMIT 1`).Scan(&profileID); err != nil {
		t.Fatalf("load default profile id: %v", err)
	}
	return profileID
}

func modelInsertProfile(t *testing.T, harness *contractHarness, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var profileID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ($1, NULL, FALSE, FALSE, TRUE, 1, NULL, $2, $2) RETURNING id`, name, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile %q: %v", name, err)
	}
	return profileID
}

func modelInsertLoadbalanceStrategy(t *testing.T, harness *contractHarness, profileID int, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var strategyID int
	if err := harness.conn.QueryRow(
		context.Background(),
		`INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode,
			retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit,
			ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at)
		 VALUES ($1, $2, 'single', $3::integer[], 'until_reset', 60000, 2.0, 0.2, 900000, 2, 4, 0, $4, $4)
		 RETURNING id`,
		profileID,
		name,
		[]int32{403, 422, 429, 500, 502, 503, 504, 529},
		now,
	).Scan(&strategyID); err != nil {
		t.Fatalf("insert loadbalance strategy %q: %v", name, err)
	}
	return strategyID
}

func modelInsertModel(t *testing.T, harness *contractHarness, profileID int, first any, second any, third any, rest ...any) int {
	t.Helper()
	var apiFamily, modelID string
	var displayName *string
	args := rest
	if family, ok := first.(string); ok {
		apiFamily, modelID, displayName = family, typedArg[string](t, second, "model_id"), optionalArg[*string](t, third, "display_name")
	} else {
		if len(rest) == 0 {
			t.Fatal("missing display_name argument")
		}
		apiFamily, modelID, displayName, args = typedArg[string](t, second, "api_family"), typedArg[string](t, third, "model_id"), optionalArg[*string](t, rest[0], "display_name"), rest[1:]
	}
	var strategyID *int
	var isEnabled bool
	switch len(args) {
	case 2:
		strategyID, isEnabled = optionalArg[*int](t, args[0], "strategy"), typedArg[bool](t, args[1], "enabled")
	case 3:
		strategyID, isEnabled = optionalArg[*int](t, args[1], "strategy"), typedArg[bool](t, args[2], "enabled")
	default:
		t.Fatalf("unexpected modelInsertModel args: %d", len(args))
	}
	now := time.Now().UTC()
	var modelConfigID int
	var openAIAcceptedFormat any
	if apiFamily == "openai" {
		openAIAcceptedFormat = "dual_native"
	}
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`, profileID, apiFamily, modelID, displayName, nullableTestInt(strategyID), openAIAcceptedFormat, isEnabled, now, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert model %q: %v", modelID, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return modelConfigID
}

func typedArg[T any](t *testing.T, value any, name string) T {
	t.Helper()
	typed, ok := value.(T)
	if !ok {
		t.Fatalf("expected %s %T, got %T", name, *new(T), value)
	}
	return typed
}

func optionalArg[T any](t *testing.T, value any, name string) T {
	t.Helper()
	if value == nil {
		var zero T
		return zero
	}
	return typedArg[T](t, value, name)
}

func modelInsertEndpoint(t *testing.T, harness *contractHarness, profileID int, name string, position int) int {
	t.Helper()
	now := time.Now().UTC()
	var endpointID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, profileID, name, fmt.Sprintf("https://%s.invalid", strings.ToLower(strings.ReplaceAll(name, " ", "-"))), "plain-api-key", position, now, now).Scan(&endpointID); err != nil {
		t.Fatalf("insert endpoint %q: %v", name, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return endpointID
}

func modelInsertConnection(t *testing.T, harness *contractHarness, profileID int, modelConfigID int, endpointID int, priority int, isActive bool, customHeaders map[string]string) int {
	t.Helper()
	var apiFamily string
	if err := harness.conn.QueryRow(context.Background(), `SELECT api_family FROM model_configs WHERE id = $1 AND profile_id = $2`, modelConfigID, profileID).Scan(&apiFamily); err != nil {
		t.Fatalf("load model %d api family: %v", modelConfigID, err)
	}
	connectionID := modelInsertConnectionRow(t, harness, profileID, apiFamily, endpointID, priority, isActive, customHeaders)
	modelInsertConnectionTarget(t, harness, profileID, modelConfigID, connectionID, priority, true)
	return connectionID
}

func modelInsertModelTarget(t *testing.T, harness *contractHarness, profileID int, sourceModelConfigID int, targetModelConfigID int, position int, isEnabled bool) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, $4, $5, $6, $6)`, profileID, sourceModelConfigID, targetModelConfigID, position, isEnabled, now); err != nil {
		t.Fatalf("insert model target for model %d target %d: %v", sourceModelConfigID, targetModelConfigID, err)
	}
}

func modelInsertConnectionTarget(t *testing.T, harness *contractHarness, profileID int, sourceModelConfigID int, connectionID int, position int, isEnabled bool) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, $5, $6, $6)`, profileID, sourceModelConfigID, connectionID, position, isEnabled, now); err != nil {
		t.Fatalf("insert connection target for model %d connection %d: %v", sourceModelConfigID, connectionID, err)
	}
}

func modelInsertStandaloneConnection(t *testing.T, harness *contractHarness, profileID int, apiFamily string, endpointID int, priority int, isActive bool, customHeaders map[string]string) int {
	t.Helper()
	return modelInsertConnectionRow(t, harness, profileID, apiFamily, endpointID, priority, isActive, customHeaders)
}

func modelInsertConnectionRow(t *testing.T, harness *contractHarness, profileID int, apiFamily string, endpointID int, priority int, isActive bool, customHeaders map[string]string) int {
	t.Helper()
	now := time.Now().UTC()
	var connectionID int
	var headersValue any
	if customHeaders != nil {
		headersValue = mustModelJSON(t, customHeaders)
	}
	var openAITextCapability any
	if apiFamily == "openai" {
		openAITextCapability = "dual_native"
	}
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, NULL, NULL, NULL, NULL, $14, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`, profileID, apiFamily, endpointID, isActive, priority, fmt.Sprintf("conn-%d", priority), nil, headersValue, "healthy", nil, nil, now, now, openAITextCapability).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection endpoint %d: %v", endpointID, err)
	}
	return connectionID
}

func modelInsertRequestLog(t *testing.T, harness *contractHarness, profileID int, modelID string, apiFamily string, statusCode int, requestID string) {
	t.Helper()
	now := time.Now().UTC()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", now))
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, status_code, response_time_ms, is_stream, success_flag, billable_flag, priced_flag, unpriced_reason, request_path, created_at) VALUES ($1, $2, $3, $4, NULL, $5, $6, FALSE, $7, $8, $9, NULL, $10, $11)`, profileID, modelID, apiFamily, requestID, statusCode, 120, statusCode >= 200 && statusCode < 300, true, true, "/v1/chat/completions", now); err != nil {
		t.Fatalf("insert request log %q: %v", requestID, err)
	}
}

type expectedAccessTarget struct {
	TargetType    string
	TargetModelID string
	ConnectionID  int
	Position      int
	IsEnabled     bool
}

func assertNoLegacyModelFields(t *testing.T, payload map[string]any) {
	t.Helper()
	for _, key := range []string{"model_type", "proxy_selection_strategy", "proxy_targets", "connections", "vendor", "vendor_id", "vendor_key", "vendor_name", "facade_enabled", "facade_selection_policy", "facade_fallback_policy"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("expected payload to omit legacy field %s, got %+v", key, payload)
		}
	}
}

func assertModelLoadbalanceStrategySummary(t *testing.T, raw any, wantID int, wantName string) {
	t.Helper()
	summary := asMap(t, raw)
	if jsonInt(t, summary["id"]) != wantID || summary["name"] != wantName {
		t.Fatalf("unexpected loadbalance strategy identity: got %+v want id=%d name=%q", summary, wantID, wantName)
	}
	if _, ok := summary[removedRetryAttemptsField()]; ok {
		t.Fatalf("model strategy summary must not include removed retry field: %+v", summary)
	}
	if summary["legacy_strategy_type"] != "single" || summary["ban_mode"] != "until_reset" {
		t.Fatalf("unexpected loadbalance strategy policy identifiers: %+v", summary)
	}
	if jsonInt(t, summary["retry_base_delay_ms"]) != 60000 || jsonInt(t, summary["retry_max_delay_ms"]) != 900000 || jsonInt(t, summary["cycle_retry_attempt_limit"]) != 2 || jsonInt(t, summary["ban_cumulative_retry_attempt_threshold"]) != 4 || jsonInt(t, summary["ban_duration_seconds"]) != 0 {
		t.Fatalf("unexpected loadbalance strategy retry/ban policy fields: %+v", summary)
	}
}

func assertAccessTargets(t *testing.T, payload map[string]any, want []expectedAccessTarget) {
	t.Helper()
	items, ok := payload["access_targets"].([]any)
	if !ok || len(items) != len(want) {
		t.Fatalf("expected access_targets %v, got %+v", want, payload)
	}
	targets := make([]map[string]any, len(items))
	for index, raw := range items {
		item := asMap(t, raw)
		for _, field := range []string{"weight", "target_priority"} {
			if _, ok := item[field]; ok {
				t.Fatalf("expected access target response to omit %s at %d, got %+v", field, index, item)
			}
		}
		targets[index] = item
	}
	assertTargetRouteOrder(t, targets, want)
}

func assertStoredModelTargetFlat(t *testing.T, harness *contractHarness, sourceModelConfigID int, targetModelConfigID int, wantPosition int, wantEnabled bool) {
	t.Helper()
	var position int
	var isEnabled bool
	if err := harness.conn.QueryRow(context.Background(), `SELECT position, is_enabled FROM model_access_targets WHERE source_model_config_id = $1 AND target_model_config_id = $2`, sourceModelConfigID, targetModelConfigID).Scan(&position, &isEnabled); err != nil {
		t.Fatalf("load model target source=%d target=%d: %v", sourceModelConfigID, targetModelConfigID, err)
	}
	if position != wantPosition || isEnabled != wantEnabled {
		t.Fatalf("expected model target position=%d is_enabled=%v, got position=%d is_enabled=%v", wantPosition, wantEnabled, position, isEnabled)
	}
}

func assertObsoleteAccessTargetError(t *testing.T, response *http.Response, path string) {
	t.Helper()
	assertRoutingPlanIssue(t, response, fmt.Sprintf("%s is obsolete and must be omitted", path), "obsolete_access_target_field", path)
}

func modelLoadConnectionTargetID(t *testing.T, harness *contractHarness, sourceModelConfigID int, connectionID int) int {
	t.Helper()
	var targetID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM model_access_targets WHERE source_model_config_id = $1 AND target_connection_id = $2 LIMIT 1`, sourceModelConfigID, connectionID).Scan(&targetID); err != nil {
		t.Fatalf("load connection target %d for model %d: %v", connectionID, sourceModelConfigID, err)
	}
	return targetID
}

func modelLoadModelTargetID(t *testing.T, harness *contractHarness, sourceModelConfigID int, targetModelConfigID int) int {
	t.Helper()
	var targetID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM model_access_targets WHERE source_model_config_id = $1 AND target_model_config_id = $2 LIMIT 1`, sourceModelConfigID, targetModelConfigID).Scan(&targetID); err != nil {
		t.Fatalf("load model target %d for model %d: %v", targetModelConfigID, sourceModelConfigID, err)
	}
	return targetID
}

func assertCountQuery(t *testing.T, harness *contractHarness, query string, arg int, want int) {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), query, arg).Scan(&count); err != nil {
		t.Fatalf("count rows for arg %d: %v", arg, err)
	}
	if count != want {
		t.Fatalf("expected %d rows for arg %d, got %d", want, arg, count)
	}
}

func assertEndpointCount(t *testing.T, harness *contractHarness, endpointID int, want int) {
	assertCountQuery(t, harness, `SELECT COUNT(*) FROM endpoints WHERE id = $1`, endpointID, want)
}

func assertConnectionTargetState(t *testing.T, harness *contractHarness, sourceModelConfigID int, targetID int, connectionID int, position int, isEnabled bool) {
	t.Helper()
	var gotSourceID int
	var gotConnectionID int
	var gotPosition int
	var gotEnabled bool
	if err := harness.conn.QueryRow(context.Background(), `SELECT source_model_config_id, target_connection_id, position, is_enabled FROM model_access_targets WHERE id = $1`, targetID).Scan(&gotSourceID, &gotConnectionID, &gotPosition, &gotEnabled); err != nil {
		t.Fatalf("load connection target row %d: %v", targetID, err)
	}
	if gotSourceID != sourceModelConfigID || gotConnectionID != connectionID || gotPosition != position || gotEnabled != isEnabled {
		t.Fatalf("unexpected connection target row %d: got source=%d connection=%d position=%d enabled=%v", targetID, gotSourceID, gotConnectionID, gotPosition, gotEnabled)
	}
}

func assertEndpointModelsBatchItem(t *testing.T, item map[string]any, wantEndpointID int, wantModelIDs []string) {
	t.Helper()
	if jsonInt(t, item["endpoint_id"]) != wantEndpointID {
		t.Fatalf("expected endpoint_id %d, got %+v", wantEndpointID, item)
	}
	models, ok := item["models"].([]any)
	if !ok {
		t.Fatalf("expected models list, got %+v", item)
	}
	if len(models) != len(wantModelIDs) {
		t.Fatalf("expected model ids %v, got %+v", wantModelIDs, item)
	}
	for index, wantModelID := range wantModelIDs {
		model := asMap(t, models[index])
		if model["model_id"] != wantModelID {
			t.Fatalf("expected model_id %q at index %d, got %+v", wantModelID, index, model)
		}
	}
}

func findModelListItemByModelID(t *testing.T, items []any, modelID string) map[string]any {
	t.Helper()
	for _, raw := range items {
		item := asMap(t, raw)
		if item["model_id"] == modelID {
			return item
		}
	}
	t.Fatalf("expected model_id %q in %+v", modelID, items)
	return nil
}

func assertModelListItemCounts(t *testing.T, item map[string]any, wantID int, wantConnectionCount int, wantActiveCount int, wantHealthTotal int, wantHealthRate any) {
	t.Helper()
	if jsonInt(t, item["id"]) != wantID || jsonInt(t, item["connection_count"]) != wantConnectionCount || jsonInt(t, item["active_connection_count"]) != wantActiveCount || jsonInt(t, item["health_total_requests"]) != wantHealthTotal {
		t.Fatalf("unexpected model helper row counts: %+v", item)
	}
	if wantHealthRate == nil {
		if item["health_success_rate"] != nil {
			t.Fatalf("expected nil health_success_rate, got %+v", item)
		}
		return
	}
	if item["health_success_rate"] != float64(wantHealthRate.(int)) {
		t.Fatalf("expected health_success_rate %v, got %+v", wantHealthRate, item)
	}
}

func nullableTestInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func mustModelJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(raw)
}
