package contracttest

import (
	"context"
	"net/http"
	"testing"
)

func TestDirectRequestEntryManagementContract(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Direct Entry Contract Strategy")

	entry := modelMutationModel(t, harness, profileID, http.MethodPost, "/api/models", openAIModelBody(strategyID, "direct-entry-contract", map[string]any{
		"is_enabled": false,
	}), http.StatusCreated)
	if entry["direct_request_enabled"] != true {
		t.Fatalf("create omission must default direct_request_enabled=true: %+v", entry)
	}
	explicitEntry := modelMutationModel(t, harness, profileID, http.MethodPost, "/api/models", openAIModelBody(strategyID, "direct-entry-explicit", map[string]any{
		"direct_request_enabled": true,
	}), http.StatusCreated)
	if explicitEntry["direct_request_enabled"] != true {
		t.Fatalf("explicit true must be persisted: %+v", explicitEntry)
	}

	nonEntry := modelMutationModel(t, harness, profileID, http.MethodPost, "/api/models", openAIModelBody(strategyID, "model-target-contract", map[string]any{
		"direct_request_enabled": false,
	}), http.StatusCreated)
	nonEntryID := jsonInt(t, nonEntry["id"])
	if nonEntry["direct_request_enabled"] != false {
		t.Fatalf("explicit false must be persisted: %+v", nonEntry)
	}
	warnings := nonEntry["configuration_warnings"].([]any)
	if !containsWarningCode(t, warnings, "model_target_unreferenced") {
		t.Fatalf("expected an explanatory unreferenced warning, got %+v", nonEntry["configuration_warnings"])
	}
	unreferencedDetail := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, modelPath(nonEntryID), nil, http.StatusOK)
	if jsonInt(t, unreferencedDetail["incoming_model_target_count"]) != 0 || !containsWarningCode(t, unreferencedDetail["configuration_warnings"].([]any), "model_target_unreferenced") {
		t.Fatalf("detail must expose the zero incoming count and warning: %+v", unreferencedDetail)
	}

	for _, test := range []struct {
		modelID string
		value   any
	}{{modelID: "invalid-direct-entry-null", value: nil}, {modelID: "invalid-direct-entry-string", value: "false"}} {
		assertStatus(t, modelResponse(t, harness, profileID, http.MethodPost, "/api/models", openAIModelBody(strategyID, test.modelID, map[string]any{"direct_request_enabled": test.value})), http.StatusUnprocessableEntity)
		items := modelJSON[[]any](t, harness, profileID, http.MethodGet, "/api/models", nil, http.StatusOK)
		for _, item := range items {
			if asMap(t, item)["model_id"] == test.modelID {
				t.Fatalf("invalid create must not persist model %q", test.modelID)
			}
		}
	}

	modelTargetID := jsonInt(t, entry["id"])
	targets := accessTargetMutationEnvelopeTargets(t, harness, profileID, http.MethodPost, modelTargetListPath(modelTargetID), modelTargetBody("model-target-contract", 0, true), http.StatusCreated)
	if len(targets) != 1 {
		t.Fatalf("expected one model target: %+v", targets)
	}
	listed := findModelListItemByModelID(t, modelJSON[[]any](t, harness, profileID, http.MethodGet, "/api/models", nil, http.StatusOK), "model-target-contract")
	if jsonInt(t, listed["incoming_model_target_count"]) != 1 {
		t.Fatalf("expected incoming model-target count 1, got %+v", listed)
	}
	if warnings := listed["configuration_warnings"].([]any); containsWarningCode(t, warnings, "model_target_unreferenced") {
		t.Fatalf("referenced non-entry model should have no unreferenced warning: %+v", warnings)
	}
	parent := findModelListItemByModelID(t, modelJSON[[]any](t, harness, profileID, http.MethodGet, "/api/models", nil, http.StatusOK), "direct-entry-contract")
	parentTargets := parent["access_targets"].([]any)
	targetSummary := asMap(t, asMap(t, parentTargets[0])["target_model"])
	if targetSummary["model_id"] != "model-target-contract" || targetSummary["direct_request_enabled"] != false || jsonInt(t, targetSummary["incoming_model_target_count"]) != 1 {
		t.Fatalf("Model Target summary must preserve the non-entry qualification: %+v", targetSummary)
	}

	// PATCH/PUT omission preserves the bit; explicit true/false toggles it.
	if got := modelMutationModel(t, harness, profileID, http.MethodPut, modelPath(nonEntryID), map[string]any{}, http.StatusOK)["direct_request_enabled"]; got != false {
		t.Fatalf("update omission must preserve false, got %v", got)
	}
	if got := modelMutationModel(t, harness, profileID, http.MethodPut, modelPath(nonEntryID), map[string]any{"direct_request_enabled": true}, http.StatusOK)["direct_request_enabled"]; got != true {
		t.Fatalf("explicit true must toggle entry qualification, got %v", got)
	}
	if got := modelMutationModel(t, harness, profileID, http.MethodPut, modelPath(nonEntryID), map[string]any{"direct_request_enabled": false}, http.StatusOK)["direct_request_enabled"]; got != false {
		t.Fatalf("explicit false must toggle entry qualification, got %v", got)
	}

	beforeInvalidUpdates := loadDirectRequestEntryPersistenceSnapshot(t, harness, profileID, nonEntryID)
	for _, test := range []struct {
		name  string
		value any
	}{{name: "null", value: nil}, {name: "string", value: "false"}} {
		t.Run("invalid update "+test.name, func(t *testing.T) {
			assertStatus(t, modelResponse(t, harness, profileID, http.MethodPut, modelPath(nonEntryID), map[string]any{"direct_request_enabled": test.value}), http.StatusUnprocessableEntity)
			afterInvalidUpdate := loadDirectRequestEntryPersistenceSnapshot(t, harness, profileID, nonEntryID)
			if afterInvalidUpdate != beforeInvalidUpdates {
				t.Fatalf("invalid direct-request update changed persisted model state:\nbefore=%+v\nafter=%+v", beforeInvalidUpdates, afterInvalidUpdate)
			}
		})
	}
	final := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, modelPath(nonEntryID), nil, http.StatusOK)
	if final["direct_request_enabled"] != false {
		t.Fatalf("invalid direct-request writes must leave the stored value unchanged: %+v", final)
	}
}

type directRequestEntryPersistenceSnapshot struct {
	ModelRow               string
	AccessTargetRows       string
	OwnedConnectionRows    string
	ModelsDevBindingRows   string
	PiBindingRows          string
	RuntimeGenerationRows  string
	RouteWitnessGeneration string
}

// loadDirectRequestEntryPersistenceSnapshot freezes the complete model row and
// every relation a model update could legitimately disturb. The JSON row
// snapshots include updated_at, while the generation snapshots prove that an
// invalid management request did not publish a routing or witness mutation.
func loadDirectRequestEntryPersistenceSnapshot(t *testing.T, harness *contractHarness, profileID int, modelConfigID int) directRequestEntryPersistenceSnapshot {
	t.Helper()
	var snapshot directRequestEntryPersistenceSnapshot
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT
			COALESCE((SELECT to_jsonb(model_row)::text FROM model_configs AS model_row WHERE model_row.profile_id = $1 AND model_row.id = $2), 'null'),
			COALESCE((SELECT jsonb_agg(to_jsonb(target_row) ORDER BY target_row.id)::text FROM model_access_targets AS target_row WHERE target_row.profile_id = $1 AND (target_row.source_model_config_id = $2 OR target_row.target_model_config_id = $2)), '[]'),
			COALESCE((SELECT jsonb_agg(to_jsonb(connection_row) ORDER BY connection_row.id)::text FROM connections AS connection_row WHERE connection_row.profile_id = $1 AND EXISTS (SELECT 1 FROM model_access_targets AS owner_target WHERE owner_target.profile_id = $1 AND owner_target.source_model_config_id = $2 AND owner_target.target_connection_id = connection_row.id)), '[]'),
			COALESCE((SELECT jsonb_agg(to_jsonb(binding_row) ORDER BY binding_row.model_config_id)::text FROM model_catalog_bindings AS binding_row WHERE binding_row.model_config_id = $2), '[]'),
			COALESCE((SELECT jsonb_agg(to_jsonb(binding_row) ORDER BY binding_row.model_config_id)::text FROM model_pi_catalog_bindings AS binding_row WHERE binding_row.model_config_id = $2), '[]'),
			COALESCE((SELECT jsonb_agg(to_jsonb(generation_row) ORDER BY generation_row.domain, generation_row.scope_type, generation_row.scope_id)::text FROM runtime_cache_generations AS generation_row), '[]'),
			COALESCE((SELECT to_jsonb(generation_row)::text FROM route_witness_generations AS generation_row WHERE generation_row.profile_id = $1), 'null')`,
		profileID,
		modelConfigID,
	).Scan(
		&snapshot.ModelRow,
		&snapshot.AccessTargetRows,
		&snapshot.OwnedConnectionRows,
		&snapshot.ModelsDevBindingRows,
		&snapshot.PiBindingRows,
		&snapshot.RuntimeGenerationRows,
		&snapshot.RouteWitnessGeneration,
	); err != nil {
		t.Fatalf("load direct-request persistence snapshot: %v", err)
	}
	return snapshot
}

func containsWarningCode(t *testing.T, warnings []any, want string) bool {
	t.Helper()
	for _, value := range warnings {
		if asMap(t, value)["code"] == want {
			return true
		}
	}
	return false
}
