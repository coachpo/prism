package contract_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestModelCRUD(t *testing.T) {
	harness := newModelContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S8 Access Strategy")
	targetModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s8-target-model", stringPtr("S8 Target Model"), &strategyID, true)
	otherFamilyModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "anthropic", "s8-anthropic-target", stringPtr("S8 Anthropic Target"), &strategyID, true)
	selectionPolicy := "weighted_eligible_context"
	fallbackPolicy := "redistribute_ineligible_weight"
	initialWeight := 7
	initialTargetPriority := 2
	updatedWeight := 3
	updatedTargetPriority := 0
	_ = otherFamilyModelID

	missingHeader := harness.requestJSON(t, harness.client, http.MethodGet, "/api/models", nil, nil)
	assertErrorResponseCode(t, missingHeader, http.StatusBadRequest, profiledomain.ScopeErrorCodeHeaderMissing, fmt.Sprintf("%s header is required", profiledomain.ProfileIDHeader))

	legacyShape := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "legacy-shape", "model_type": "native", "loadbalance_strategy_id": strategyID},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, legacyShape, http.StatusBadRequest)

	missingStrategy := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "missing-strategy", "access_targets": []map[string]any{modelAccessTarget("model", "s8-target-model", nil, 0, true)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, missingStrategy, http.StatusBadRequest, "loadbalance_strategy_id is required")

	createSelfTarget := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "s8-create-self-target", "loadbalance_strategy_id": strategyID, "access_targets": []map[string]any{modelAccessTarget("model", "s8-create-self-target", nil, 0, true)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, createSelfTarget, http.StatusBadRequest, "Model access target cannot target itself")

	createDraftResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{
			"vendor_id":               vendorID,
			"api_family":              "openai",
			"model_id":                "s8-access-draft",
			"display_name":            "S8 Access Draft",
			"loadbalance_strategy_id": strategyID,
			"is_enabled":              false,
			"access_targets":          []map[string]any{},
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, createDraftResponse, http.StatusCreated)
	var createDraftPayload map[string]any
	decodeJSONResponse(t, createDraftResponse, &createDraftPayload)
	assertNoLegacyModelFields(t, createDraftPayload)
	assertFacadeFields(t, createDraftPayload, false, nil, nil)
	assertAccessTargets(t, createDraftPayload, nil)
	if got := createDraftPayload["is_enabled"]; got != false {
		t.Fatalf("expected disabled draft create to persist is_enabled=false, got %+v", createDraftPayload)
	}
	if got := createDraftPayload["display_name"]; got != "S8 Access Draft" {
		t.Fatalf("expected draft display name to persist, got %+v", createDraftPayload)
	}
	assertModelLoadbalanceStrategySummary(t, createDraftPayload["loadbalance_strategy"], strategyID, "S8 Access Strategy")

	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{
			"vendor_id":               vendorID,
			"api_family":              "openai",
			"model_id":                "s8-access-model",
			"display_name":            "S8 Access Model",
			"loadbalance_strategy_id": strategyID,
			"facade_enabled":          true,
			"facade_selection_policy": selectionPolicy,
			"facade_fallback_policy":  fallbackPolicy,
			"is_enabled":              true,
			"access_targets":          []map[string]any{modelAccessTargetWithMetadata("model", "s8-target-model", nil, 0, modelIntPtr(initialWeight), modelIntPtr(initialTargetPriority), true)},
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, createResponse, http.StatusCreated)
	var createPayload map[string]any
	decodeJSONResponse(t, createResponse, &createPayload)
	sourceModelConfigID := jsonInt(t, createPayload["id"])
	assertNoLegacyModelFields(t, createPayload)
	assertFacadeFields(t, createPayload, true, &selectionPolicy, &fallbackPolicy)
	assertAccessTargets(t, createPayload, []expectedAccessTarget{{TargetType: "model", TargetModelID: "s8-target-model", Position: 0, Weight: modelIntPtr(initialWeight), TargetPriority: modelIntPtr(initialTargetPriority), IsEnabled: true}})
	assertStoredModelFacadeFields(t, harness, sourceModelConfigID, true, &selectionPolicy, &fallbackPolicy)
	assertStoredModelTargetMetadata(t, harness, sourceModelConfigID, targetModelID, initialWeight, initialTargetPriority)
	assertModelLoadbalanceStrategySummary(t, createPayload["loadbalance_strategy"], strategyID, "S8 Access Strategy")

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d", sourceModelConfigID), nil, modelHeader(defaultProfileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	assertNoLegacyModelFields(t, detailPayload)
	assertFacadeFields(t, detailPayload, true, &selectionPolicy, &fallbackPolicy)
	assertAccessTargets(t, detailPayload, []expectedAccessTarget{{TargetType: "model", TargetModelID: "s8-target-model", Position: 0, Weight: modelIntPtr(initialWeight), TargetPriority: modelIntPtr(initialTargetPriority), IsEnabled: true}})
	assertModelLoadbalanceStrategySummary(t, detailPayload["loadbalance_strategy"], strategyID, "S8 Access Strategy")

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/models", nil, modelHeader(defaultProfileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listPayload []any
	decodeJSONResponse(t, listResponse, &listPayload)
	listItem := findModelListItemByModelID(t, listPayload, "s8-access-model")
	assertNoLegacyModelFields(t, listItem)
	assertFacadeFields(t, listItem, true, &selectionPolicy, &fallbackPolicy)
	assertAccessTargets(t, listItem, []expectedAccessTarget{{TargetType: "model", TargetModelID: "s8-target-model", Position: 0, Weight: modelIntPtr(initialWeight), TargetPriority: modelIntPtr(initialTargetPriority), IsEnabled: true}})
	assertModelLoadbalanceStrategySummary(t, listItem["loadbalance_strategy"], strategyID, "S8 Access Strategy")

	enabledZeroTargetsCreate := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{
			"vendor_id":               vendorID,
			"api_family":              "openai",
			"model_id":                "s8-enabled-zero-targets",
			"loadbalance_strategy_id": strategyID,
			"is_enabled":              true,
			"access_targets":          []map[string]any{},
		},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, enabledZeroTargetsCreate, http.StatusBadRequest, "enabled models must include at least one enabled access target")

	duplicateCreate := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "s8-access-model", "loadbalance_strategy_id": strategyID, "access_targets": []map[string]any{modelAccessTarget("model", "s8-target-model", nil, 0, true)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, duplicateCreate, http.StatusConflict, "Model ID 's8-access-model' already exists")

	updateResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", sourceModelConfigID),
		map[string]any{
			"display_name":            nil,
			"facade_enabled":          false,
			"facade_selection_policy": nil,
			"facade_fallback_policy":  nil,
			"access_targets": []map[string]any{
				modelAccessTargetWithMetadata("model", "s8-target-model", nil, 0, modelIntPtr(updatedWeight), modelIntPtr(updatedTargetPriority), true),
			},
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, updateResponse, http.StatusOK)
	var updatePayload map[string]any
	decodeJSONResponse(t, updateResponse, &updatePayload)
	assertNoLegacyModelFields(t, updatePayload)
	assertFacadeFields(t, updatePayload, false, nil, nil)
	assertAccessTargets(t, updatePayload, []expectedAccessTarget{{TargetType: "model", TargetModelID: "s8-target-model", Position: 0, Weight: modelIntPtr(updatedWeight), TargetPriority: modelIntPtr(updatedTargetPriority), IsEnabled: true}})
	assertStoredModelFacadeFields(t, harness, sourceModelConfigID, false, nil, nil)
	assertStoredModelTargetMetadata(t, harness, sourceModelConfigID, targetModelID, updatedWeight, updatedTargetPriority)
	if updatePayload["display_name"] != "s8-access-model" {
		t.Fatalf("expected nil display_name update to reset to model_id, got %+v", updatePayload)
	}

	zeroEnabledTargets := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", sourceModelConfigID),
		map[string]any{"access_targets": []map[string]any{modelAccessTarget("model", "s8-target-model", nil, 0, false)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, zeroEnabledTargets, http.StatusBadRequest, "enabled models must include at least one enabled access target")

	wrongFamilyTarget := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", sourceModelConfigID),
		map[string]any{"access_targets": []map[string]any{modelAccessTarget("model", "s8-anthropic-target", nil, 0, true)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, wrongFamilyTarget, http.StatusBadRequest, "Model access targets must use the same api_family as the source model")

	selfTarget := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", sourceModelConfigID),
		map[string]any{"access_targets": []map[string]any{modelAccessTarget("model", "s8-access-model", nil, 0, true)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, selfTarget, http.StatusBadRequest, "Model access target cannot target itself")

	deleteReferencedTarget := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", targetModelID), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, deleteReferencedTarget, http.StatusConflict, "Cannot delete: models [s8-access-model] target this model")

	cycleUpdate := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", targetModelID),
		map[string]any{"access_targets": []map[string]any{modelAccessTarget("model", "s8-access-model", nil, 0, true)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, cycleUpdate, http.StatusBadRequest, "access_targets cannot introduce a model target cycle")

	renameResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", sourceModelConfigID),
		map[string]any{"model_id": "s8-access-model-renamed", "display_name": nil},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, renameResponse, http.StatusOK)
	var renamedPayload map[string]any
	decodeJSONResponse(t, renameResponse, &renamedPayload)
	if renamedPayload["model_id"] != "s8-access-model-renamed" || renamedPayload["display_name"] != "s8-access-model-renamed" {
		t.Fatalf("expected rename payload to resync display_name, got %+v", renamedPayload)
	}

	deleteSource := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", sourceModelConfigID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteSource, http.StatusOK)
	assertNoSourceAccessTargets(t, harness, sourceModelConfigID)
	deleteTarget := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", targetModelID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteTarget, http.StatusOK)
}

func TestModelFacadePoliciesAndTargetMetadata(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Facade Policy Strategy")
	terminalTargetID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "facade-terminal-target", nil, &strategyID, true)
	nestedFacadeTargetID := modelInsertFacadeModel(t, harness, profileID, &vendorID, "openai", "nested-facade-target", nil, &strategyID, true)
	selectionPolicy := "weighted_eligible_context"
	fallbackPolicy := "redistribute_ineligible_weight"

	invalidSelection := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "openai",
		"model_id":                "invalid-facade-selection",
		"loadbalance_strategy_id": strategyID,
		"facade_enabled":          true,
		"facade_selection_policy": "invalid",
		"facade_fallback_policy":  fallbackPolicy,
		"access_targets":          []map[string]any{modelAccessTargetWithMetadata("model", "facade-terminal-target", nil, 0, modelIntPtr(1), modelIntPtr(0), true)},
	}, modelHeader(profileID))
	assertErrorResponse(t, invalidSelection, http.StatusBadRequest, "facade_selection_policy must be 'weighted_eligible_context'")

	invalidFallback := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "openai",
		"model_id":                "invalid-facade-fallback",
		"loadbalance_strategy_id": strategyID,
		"facade_enabled":          true,
		"facade_selection_policy": selectionPolicy,
		"facade_fallback_policy":  "invalid",
		"access_targets":          []map[string]any{modelAccessTargetWithMetadata("model", "facade-terminal-target", nil, 0, modelIntPtr(1), modelIntPtr(0), true)},
	}, modelHeader(profileID))
	assertErrorResponse(t, invalidFallback, http.StatusBadRequest, "facade_fallback_policy must be 'redistribute_ineligible_weight'")

	missingSelection := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "openai",
		"model_id":                "missing-facade-selection",
		"loadbalance_strategy_id": strategyID,
		"facade_enabled":          true,
		"facade_fallback_policy":  fallbackPolicy,
		"access_targets":          []map[string]any{modelAccessTargetWithMetadata("model", "facade-terminal-target", nil, 0, modelIntPtr(1), modelIntPtr(0), true)},
	}, modelHeader(profileID))
	assertErrorResponse(t, missingSelection, http.StatusBadRequest, "facade_selection_policy is required when facade_enabled is true")

	missingFallback := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "openai",
		"model_id":                "missing-facade-fallback",
		"loadbalance_strategy_id": strategyID,
		"facade_enabled":          true,
		"facade_selection_policy": selectionPolicy,
		"access_targets":          []map[string]any{modelAccessTargetWithMetadata("model", "facade-terminal-target", nil, 0, modelIntPtr(1), modelIntPtr(0), true)},
	}, modelHeader(profileID))
	assertErrorResponse(t, missingFallback, http.StatusBadRequest, "facade_fallback_policy is required when facade_enabled is true")

	anthropicFacadeCreate := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "anthropic",
		"model_id":                "anthropic-facade-create",
		"loadbalance_strategy_id": strategyID,
		"facade_enabled":          true,
		"facade_selection_policy": selectionPolicy,
		"facade_fallback_policy":  fallbackPolicy,
		"is_enabled":              false,
		"access_targets":          []map[string]any{},
	}, modelHeader(profileID))
	assertErrorResponse(t, anthropicFacadeCreate, http.StatusBadRequest, "facade_enabled requires api_family 'openai'")

	anthropicFacadeSourceID := modelInsertModel(t, harness, profileID, &vendorID, "anthropic", "anthropic-facade-update", nil, &strategyID, false)
	enableAnthropicFacade := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/models/%d", anthropicFacadeSourceID), map[string]any{
		"facade_enabled":          true,
		"facade_selection_policy": selectionPolicy,
		"facade_fallback_policy":  fallbackPolicy,
	}, modelHeader(profileID))
	assertErrorResponse(t, enableAnthropicFacade, http.StatusBadRequest, "facade_enabled requires api_family 'openai'")

	invalidWeight := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "openai",
		"model_id":                "invalid-weight-model",
		"loadbalance_strategy_id": strategyID,
		"access_targets":          []map[string]any{modelAccessTargetWithMetadata("model", "facade-terminal-target", nil, 0, modelIntPtr(0), modelIntPtr(0), true)},
	}, modelHeader(profileID))
	assertErrorResponse(t, invalidWeight, http.StatusBadRequest, "weight must be greater than 0")

	invalidTargetPriority := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "openai",
		"model_id":                "invalid-target-priority-model",
		"loadbalance_strategy_id": strategyID,
		"access_targets":          []map[string]any{modelAccessTargetWithMetadata("model", "facade-terminal-target", nil, 0, modelIntPtr(1), modelIntPtr(-1), true)},
	}, modelHeader(profileID))
	assertErrorResponse(t, invalidTargetPriority, http.StatusBadRequest, "target_priority must be greater than or equal to 0")

	nestedTargetCreate := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "openai",
		"model_id":                "nested-source-model",
		"loadbalance_strategy_id": strategyID,
		"access_targets":          []map[string]any{modelAccessTargetWithMetadata("model", "nested-facade-target", nil, 0, modelIntPtr(1), modelIntPtr(0), true)},
	}, modelHeader(profileID))
	assertErrorResponse(t, nestedTargetCreate, http.StatusBadRequest, "nested facades are not supported")

	nonFacadeTargetID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "non-facade-target", nil, &strategyID, true)
	nonFacadeEndpointID := modelInsertEndpoint(t, harness, profileID, "Non Facade Target Endpoint", 0)
	_ = modelInsertConnection(t, harness, profileID, nonFacadeTargetID, nonFacadeEndpointID, 0, true, nil)
	referrerCreate := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "openai",
		"model_id":                "facade-referrer",
		"loadbalance_strategy_id": strategyID,
		"access_targets":          []map[string]any{modelAccessTargetWithMetadata("model", "non-facade-target", nil, 0, modelIntPtr(1), modelIntPtr(0), true)},
	}, modelHeader(profileID))
	assertStatus(t, referrerCreate, http.StatusCreated)
	enableNestedFacade := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/models/%d", nonFacadeTargetID), map[string]any{
		"facade_enabled":          true,
		"facade_selection_policy": selectionPolicy,
		"facade_fallback_policy":  fallbackPolicy,
	}, modelHeader(profileID))
	assertErrorResponse(t, enableNestedFacade, http.StatusBadRequest, "nested facades are not supported")

	sourceModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "target-metadata-source", nil, &strategyID, true)
	createTargetResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/targets", sourceModelID), map[string]any{
		"target_type":     "model",
		"target_model_id": "facade-terminal-target",
		"position":        0,
		"weight":          9,
		"target_priority": 4,
		"is_enabled":      true,
	}, modelHeader(profileID))
	assertStatus(t, createTargetResponse, http.StatusCreated)
	var createdTargets []any
	decodeJSONResponse(t, createTargetResponse, &createdTargets)
	assertAccessTargets(t, map[string]any{"access_targets": createdTargets}, []expectedAccessTarget{{TargetType: "model", TargetModelID: "facade-terminal-target", Position: 0, Weight: modelIntPtr(9), TargetPriority: modelIntPtr(4), IsEnabled: true}})
	createdTargetID := jsonInt(t, asMap(t, createdTargets[0])["id"])
	assertStoredModelTargetMetadata(t, harness, sourceModelID, terminalTargetID, 9, 4)

	updateTargetResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/targets/%d", sourceModelID, createdTargetID), map[string]any{
		"weight":          6,
		"target_priority": 1,
	}, modelHeader(profileID))
	assertStatus(t, updateTargetResponse, http.StatusOK)
	var updatedTargets []any
	decodeJSONResponse(t, updateTargetResponse, &updatedTargets)
	assertAccessTargets(t, map[string]any{"access_targets": updatedTargets}, []expectedAccessTarget{{TargetType: "model", TargetModelID: "facade-terminal-target", Position: 0, Weight: modelIntPtr(6), TargetPriority: modelIntPtr(1), IsEnabled: true}})
	assertStoredModelTargetMetadata(t, harness, sourceModelID, terminalTargetID, 6, 1)

	listTargetsResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d/targets", sourceModelID), nil, modelHeader(profileID))
	assertStatus(t, listTargetsResponse, http.StatusOK)
	var listedTargets []any
	decodeJSONResponse(t, listTargetsResponse, &listedTargets)
	assertAccessTargets(t, map[string]any{"access_targets": listedTargets}, []expectedAccessTarget{{TargetType: "model", TargetModelID: "facade-terminal-target", Position: 0, Weight: modelIntPtr(6), TargetPriority: modelIntPtr(1), IsEnabled: true}})

	endpointID := modelInsertEndpoint(t, harness, profileID, "Target Metadata Connection Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, sourceModelID, endpointID, 1, true, nil)
	connectionTargetID := modelLoadConnectionTargetID(t, harness, sourceModelID, connectionID)

	connectionWeightUpdate := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/targets/%d", sourceModelID, connectionTargetID), map[string]any{"weight": 2}, modelHeader(profileID))
	assertErrorResponse(t, connectionWeightUpdate, http.StatusBadRequest, "weight must be omitted for terminal targets")
	connectionPriorityUpdate := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/targets/%d", sourceModelID, connectionTargetID), map[string]any{"target_priority": 0}, modelHeader(profileID))
	assertErrorResponse(t, connectionPriorityUpdate, http.StatusBadRequest, "target_priority must be omitted for terminal targets")
	assertConnectionTargetState(t, harness, sourceModelID, connectionTargetID, connectionID, 1, true)
	_ = nestedFacadeTargetID
}

func TestModelContextCapabilities(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Context Capability Strategy")
	targetModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "context-capability-target", nil, &strategyID, true)
	_ = targetModelID

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "openai",
		"model_id":                "context-capability-model",
		"loadbalance_strategy_id": strategyID,
		"context_window_tokens":   128000,
		"access_targets":          []map[string]any{modelAccessTarget("model", "context-capability-target", nil, 0, true)},
	}, modelHeader(profileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	modelConfigID := jsonInt(t, created["id"])
	if jsonInt(t, created["context_window_tokens"]) != 128000 || jsonInt(t, created["default_output_token_reserve"]) != 4096 || jsonFloat(t, created["max_context_utilization"]) != 0.9 {
		t.Fatalf("expected created model context capability defaults, got %+v", created)
	}
	assertStoredModelContextCapabilities(t, harness, modelConfigID, intPtr(128000), 4096, 0.9)

	updateResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/models/%d", modelConfigID), map[string]any{
		"context_window_tokens":        256000,
		"default_output_token_reserve": 2048,
		"max_context_utilization":      0.75,
	}, modelHeader(profileID))
	assertStatus(t, updateResponse, http.StatusOK)
	var updated map[string]any
	decodeJSONResponse(t, updateResponse, &updated)
	if jsonInt(t, updated["context_window_tokens"]) != 256000 || jsonInt(t, updated["default_output_token_reserve"]) != 2048 || jsonFloat(t, updated["max_context_utilization"]) != 0.75 {
		t.Fatalf("expected updated model context capability values, got %+v", updated)
	}
	assertStoredModelContextCapabilities(t, harness, modelConfigID, intPtr(256000), 2048, 0.75)

	invalidContextWindow := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "invalid-context-window-model", "loadbalance_strategy_id": strategyID, "context_window_tokens": 0, "access_targets": []map[string]any{modelAccessTarget("model", "context-capability-target", nil, 0, true)}}, modelHeader(profileID))
	assertErrorResponse(t, invalidContextWindow, http.StatusBadRequest, "context_window_tokens must be greater than or equal to 1 when provided")

	invalidReserve := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "invalid-context-reserve-model", "loadbalance_strategy_id": strategyID, "default_output_token_reserve": 0, "access_targets": []map[string]any{modelAccessTarget("model", "context-capability-target", nil, 0, true)}}, modelHeader(profileID))
	assertErrorResponse(t, invalidReserve, http.StatusBadRequest, "default_output_token_reserve must be greater than or equal to 1 when provided")

	invalidUtilization := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "invalid-context-utilization-model", "loadbalance_strategy_id": strategyID, "max_context_utilization": 1.1, "access_targets": []map[string]any{modelAccessTarget("model", "context-capability-target", nil, 0, true)}}, modelHeader(profileID))
	assertErrorResponse(t, invalidUtilization, http.StatusBadRequest, "max_context_utilization must be greater than 0 and less than or equal to 1 when provided")
}

func TestDeleteReferencedModel(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Delete Referenced Strategy")
	targetID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "delete-target", nil, &strategyID, true)
	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "delete-source", "loadbalance_strategy_id": strategyID, "access_targets": []map[string]any{modelAccessTarget("model", "delete-target", nil, 0, true)}}, modelHeader(profileID))
	assertStatus(t, createResponse, http.StatusCreated)
	deleteReferenced := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", targetID), nil, modelHeader(profileID))
	assertErrorResponse(t, deleteReferenced, http.StatusConflict, "Cannot delete: models [delete-source] target this model")
}

func TestModelDeleteRemovesOwnedPrivateConnections(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Task 5 Delete Owner Strategy")
	ownerID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "task5-delete-owner", nil, &strategyID, true)
	targetID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "task5-delete-owner-target", nil, &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Task 5 Delete Owner Endpoint", 0)
	connectionAID := modelInsertConnection(t, harness, profileID, ownerID, endpointID, 0, true, nil)
	connectionBID := modelInsertConnection(t, harness, profileID, ownerID, endpointID, 1, true, nil)
	modelInsertModelTarget(t, harness, profileID, ownerID, targetID, 2, true)

	deleteOwner := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", ownerID), nil, modelHeader(profileID))
	assertStatus(t, deleteOwner, http.StatusOK)
	assertStoredConnectionCount(t, harness, connectionAID, 0)
	assertStoredConnectionCount(t, harness, connectionBID, 0)
	assertNoSourceAccessTargets(t, harness, ownerID)
	assertModelConfigCount(t, harness, ownerID, 0)
	assertEndpointCount(t, harness, endpointID, 1)

	rollbackOwnerID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "task5-delete-rollback-owner", nil, &strategyID, true)
	rollbackConnectionID := modelInsertConnection(t, harness, profileID, rollbackOwnerID, endpointID, 0, true, nil)
	if _, err := harness.conn.Exec(context.Background(), `CREATE OR REPLACE FUNCTION task5_fail_connection_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced private connection delete failure'; END; $$`); err != nil {
		t.Fatalf("install connection delete failure function: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `CREATE TRIGGER task5_fail_connection_delete BEFORE DELETE ON connections FOR EACH ROW EXECUTE FUNCTION task5_fail_connection_delete()`); err != nil {
		t.Fatalf("install connection delete failure trigger: %v", err)
	}

	failedDelete := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", rollbackOwnerID), nil, modelHeader(profileID))
	assertErrorResponse(t, failedDelete, http.StatusInternalServerError, "Internal server error")
	assertModelConfigCount(t, harness, rollbackOwnerID, 1)
	assertStoredConnectionCount(t, harness, rollbackConnectionID, 1)
	assertModelConnectionTargetCount(t, harness, rollbackOwnerID, 1)
}

func TestModelDeleteStillBlockedWhenOtherModelsTargetIt(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Task 5 Referenced Delete Strategy")
	ownerID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "task5-referenced-owner", nil, &strategyID, true)
	referrerID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "task5-referenced-referrer", nil, &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Task 5 Referenced Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, ownerID, endpointID, 0, true, nil)
	modelInsertModelTarget(t, harness, profileID, referrerID, ownerID, 0, true)

	deleteOwner := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", ownerID), nil, modelHeader(profileID))
	assertErrorResponse(t, deleteOwner, http.StatusConflict, "Cannot delete: models [task5-referenced-referrer] target this model")
	assertModelConfigCount(t, harness, ownerID, 1)
	assertStoredConnectionCount(t, harness, connectionID, 1)
	assertModelConnectionTargetCount(t, harness, ownerID, 1)
}

func TestWrongFamilyTarget(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Wrong Family Strategy")
	modelInsertModel(t, harness, profileID, &vendorID, "anthropic", "wrong-family-target", nil, &strategyID, true)
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "wrong-family-source", "loadbalance_strategy_id": strategyID, "access_targets": []map[string]any{modelAccessTarget("model", "wrong-family-target", nil, 0, true)}}, modelHeader(profileID))
	assertErrorResponse(t, response, http.StatusBadRequest, "Model access targets must use the same api_family as the source model")
}

func TestWrongProfileTarget(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	otherProfileID := modelInsertProfile(t, harness, "Other Profile")
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Wrong Profile Strategy")
	otherEndpointID := modelInsertEndpoint(t, harness, otherProfileID, "Other Profile Endpoint", 0)
	otherConnectionID := modelInsertStandaloneConnection(t, harness, otherProfileID, "openai", otherEndpointID, 0, true, nil)
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "wrong-profile-source", "loadbalance_strategy_id": strategyID, "access_targets": []map[string]any{modelAccessTarget("connection", "", &otherConnectionID, 0, true)}}, modelHeader(profileID))
	assertErrorResponse(t, response, http.StatusBadRequest, modelConnectionTargetsManagedDetail())
}

func TestModelAccessTargetConnectionOwnerUniqueness(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Connection Owner Strategy")
	endpointID := modelInsertEndpoint(t, harness, profileID, "Connection Owner Endpoint", 0)
	connectionID := modelInsertStandaloneConnection(t, harness, profileID, "openai", endpointID, 0, true, nil)

	firstSourceID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "connection-owner-source-a", nil, &strategyID, true)
	modelInsertConnectionTarget(t, harness, profileID, firstSourceID, connectionID, 0, true)

	var ownerCount int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM model_access_targets WHERE target_connection_id = $1`, connectionID).Scan(&ownerCount); err != nil {
		t.Fatalf("count connection owners for %d: %v", connectionID, err)
	}
	if ownerCount != 1 {
		t.Fatalf("expected one connection owner after first insert, got %d", ownerCount)
	}

	secondSourceID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "connection-owner-source-b", nil, &strategyID, true)
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
}

func TestModelTargetRejectsArbitraryConnectionAssignment(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Reject Connection Assignment Strategy")
	targetModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "reject-connection-target-model", nil, &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Reject Connection Assignment Endpoint", 0)
	connectionID := modelInsertStandaloneConnection(t, harness, profileID, "openai", endpointID, 0, true, nil)

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "reject-connection-source", "loadbalance_strategy_id": strategyID, "access_targets": []map[string]any{modelAccessTarget("connection", "", &connectionID, 0, true)}}, modelHeader(profileID))
	assertErrorResponse(t, createResponse, http.StatusBadRequest, modelConnectionTargetsManagedDetail())
	assertConnectionTargetCount(t, harness, connectionID, 0)

	sourceModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "reject-connection-existing-source", nil, &strategyID, true)
	updateResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/models/%d", sourceModelID), map[string]any{"access_targets": []map[string]any{modelAccessTarget("connection", "", &connectionID, 0, true)}}, modelHeader(profileID))
	assertErrorResponse(t, updateResponse, http.StatusBadRequest, modelConnectionTargetsManagedDetail())
	assertConnectionTargetCount(t, harness, connectionID, 0)

	addConnectionTarget := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/targets", sourceModelID), map[string]any{"target_type": "connection", "connection_id": connectionID, "position": 0, "is_enabled": true}, modelHeader(profileID))
	assertErrorResponse(t, addConnectionTarget, http.StatusBadRequest, modelConnectionTargetsManagedDetail())
	assertConnectionTargetCount(t, harness, connectionID, 0)

	addModelTarget := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/targets", sourceModelID), map[string]any{"target_type": "model", "target_model_id": "reject-connection-target-model", "position": 0, "is_enabled": true}, modelHeader(profileID))
	assertStatus(t, addModelTarget, http.StatusCreated)
	_ = targetModelID
	assertConnectionTargetCount(t, harness, connectionID, 0)
}

func TestModelTargetRejectsConnectionRetarget(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Reject Connection Retarget Strategy")
	targetModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "reject-retarget-model-target", nil, &strategyID, true)
	sourceModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "reject-retarget-source", nil, &strategyID, true)
	modelInsertModelTarget(t, harness, profileID, sourceModelID, targetModelID, 0, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Reject Connection Retarget Endpoint", 0)
	connectionAID := modelInsertStandaloneConnection(t, harness, profileID, "openai", endpointID, 0, true, nil)
	connectionBID := modelInsertStandaloneConnection(t, harness, profileID, "openai", endpointID, 1, true, nil)
	modelInsertConnectionTarget(t, harness, profileID, sourceModelID, connectionAID, 1, true)
	targetID := modelLoadConnectionTargetID(t, harness, sourceModelID, connectionAID)

	retargetResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/targets/%d", sourceModelID, targetID), map[string]any{"target_type": "connection", "target_connection_id": connectionBID, "position": 0, "is_enabled": false}, modelHeader(profileID))
	assertErrorResponse(t, retargetResponse, http.StatusBadRequest, modelConnectionTargetsManagedDetail())
	assertConnectionTargetState(t, harness, sourceModelID, targetID, connectionAID, 1, true)
	assertConnectionTargetCount(t, harness, connectionBID, 0)

	toggleResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/targets/%d", sourceModelID, targetID), map[string]any{"position": 1, "is_enabled": false}, modelHeader(profileID))
	assertStatus(t, toggleResponse, http.StatusOK)
	assertConnectionTargetState(t, harness, sourceModelID, targetID, connectionAID, 1, false)
}

func TestModelUpdatePreservesOwnedPrivateConnectionTargets(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Preserve Private Connection Strategy")
	targetModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "preserve-model-target", nil, &strategyID, true)
	sourceModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "preserve-source", nil, &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Preserve Private Connection Endpoint", 0)
	connectionID := modelInsertStandaloneConnection(t, harness, profileID, "openai", endpointID, 0, true, nil)
	modelInsertConnectionTarget(t, harness, profileID, sourceModelID, connectionID, 0, true)
	connectionTargetID := modelLoadConnectionTargetID(t, harness, sourceModelID, connectionID)

	updateResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/models/%d", sourceModelID), map[string]any{"access_targets": []map[string]any{modelAccessTarget("model", "preserve-model-target", nil, 0, true)}}, modelHeader(profileID))
	assertStatus(t, updateResponse, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, updateResponse, &payload)
	assertAccessTargets(t, payload, []expectedAccessTarget{{TargetType: "connection", ConnectionID: connectionID, Position: 0, IsEnabled: true}, {TargetType: "model", TargetModelID: "preserve-model-target", Position: 1, IsEnabled: true}})
	assertConnectionTargetState(t, harness, sourceModelID, connectionTargetID, connectionID, 0, true)
	_ = targetModelID
}

func TestModelsByEndpoints(t *testing.T) {
	harness := newModelContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S8 Helper Strategy")
	modelAID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "helper-a", stringPtr("Helper A"), &strategyID, true)
	modelBID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "helper-b", nil, &strategyID, true)
	endpointAID := modelInsertEndpoint(t, harness, defaultProfileID, "S8 Helper Endpoint A", 0)
	endpointBID := modelInsertEndpoint(t, harness, defaultProfileID, "S8 Helper Endpoint B", 1)
	modelInsertConnection(t, harness, defaultProfileID, modelBID, endpointAID, 2, true, nil)
	modelInsertConnection(t, harness, defaultProfileID, modelBID, endpointBID, 1, true, nil)
	modelInsertConnection(t, harness, defaultProfileID, modelAID, endpointBID, 0, true, nil)
	modelInsertConnection(t, harness, defaultProfileID, modelAID, endpointBID, 3, false, nil)
	modelInsertRequestLog(t, harness, defaultProfileID, "helper-a", "openai", 200, "helper-a-success")
	modelInsertRequestLog(t, harness, defaultProfileID, "helper-a", "openai", 500, "helper-a-failure")

	helperResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models/by-endpoints",
		map[string]any{"endpoint_ids": []int{endpointBID, 999999, endpointAID}},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, helperResponse, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, helperResponse, &payload)
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

	emptyInput := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models/by-endpoints",
		map[string]any{"endpoint_ids": []int{}},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, emptyInput, http.StatusOK)
	var emptyPayload map[string]any
	decodeJSONResponse(t, emptyInput, &emptyPayload)
	if items, ok := emptyPayload["items"].([]any); !ok || len(items) != 0 {
		t.Fatalf("expected empty endpoint_ids to return an empty items list, got %+v", emptyPayload)
	}
}

func TestModelsByEndpoint(t *testing.T) {
	harness := newModelContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S8 Endpoint Strategy")
	modelZID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "z-model", nil, &strategyID, true)
	modelAID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "a-model", stringPtr("Model A"), &strategyID, true)
	facadeID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "facade-model", stringPtr("Facade Model"), &strategyID, true)
	disabledFacadeID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "disabled-facade", nil, &strategyID, false)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "S8 By Endpoint", 0)
	modelInsertConnection(t, harness, defaultProfileID, modelZID, endpointID, 5, true, nil)
	modelInsertConnection(t, harness, defaultProfileID, modelAID, endpointID, 1, false, nil)
	modelInsertModelTarget(t, harness, defaultProfileID, facadeID, modelZID, 0, true)
	modelInsertModelTarget(t, harness, defaultProfileID, disabledFacadeID, modelZID, 0, true)

	response := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/by-endpoint/%d", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, response, http.StatusOK)
	var models []map[string]any
	decodeJSONResponse(t, response, &models)
	if len(models) != 3 {
		t.Fatalf("expected by-endpoint helper to return three enabled reachable models, got %+v", models)
	}
	if models[0]["model_id"] != "a-model" || models[1]["model_id"] != "facade-model" || models[2]["model_id"] != "z-model" {
		t.Fatalf("expected by-endpoint helper to sort by model_id, got %+v", models)
	}
	assertModelListItemCounts(t, models[0], modelAID, 1, 0, 0, nil)
	assertModelListItemCounts(t, models[1], facadeID, 1, 1, 0, nil)
	assertModelListItemCounts(t, models[2], modelZID, 1, 1, 0, nil)
}

func TestEndpointBatchDedupe(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Endpoint Batch Dedupe Strategy")
	terminalID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "batch-terminal", nil, &strategyID, true)
	facadeID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "batch-facade", nil, &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Endpoint Batch Dedupe", 0)
	terminalConnectionID := modelInsertConnection(t, harness, profileID, terminalID, endpointID, 0, true, nil)
	facadeConnectionID := modelInsertConnection(t, harness, profileID, facadeID, endpointID, 1, true, nil)
	modelInsertModelTarget(t, harness, profileID, facadeID, terminalID, 0, true)

	assertConnectionTargetCount(t, harness, terminalConnectionID, 1)
	assertConnectionTargetCount(t, harness, facadeConnectionID, 1)

	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models/by-endpoints", map[string]any{"endpoint_ids": []int{endpointID}}, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one endpoint envelope, got %+v", payload)
	}
	item := asMap(t, items[0])
	assertEndpointModelsBatchItem(t, item, endpointID, []string{"batch-facade", "batch-terminal"})
	models := item["models"].([]any)
	assertModelListItemCounts(t, asMap(t, models[0]), facadeID, 2, 2, 0, nil)
	assertModelListItemCounts(t, asMap(t, models[1]), terminalID, 1, 1, 0, nil)
}

func TestReachableConnectionCount(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Reachable Count Strategy")
	terminalID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "count-terminal", nil, &strategyID, true)
	facadeID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "count-facade", nil, &strategyID, true)
	recursiveFacadeID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "count-recursive-facade", nil, &strategyID, true)
	endpointAID := modelInsertEndpoint(t, harness, profileID, "Reachable Count Endpoint A", 0)
	endpointBID := modelInsertEndpoint(t, harness, profileID, "Reachable Count Endpoint B", 1)
	modelInsertConnection(t, harness, profileID, terminalID, endpointAID, 0, true, nil)
	modelInsertConnection(t, harness, profileID, terminalID, endpointAID, 1, false, nil)
	modelInsertConnection(t, harness, profileID, terminalID, endpointBID, 2, true, nil)
	inactiveTargetConnectionID := modelInsertStandaloneConnection(t, harness, profileID, "openai", endpointAID, 3, true, nil)
	modelInsertModelTarget(t, harness, profileID, facadeID, terminalID, 0, true)
	modelInsertConnectionTarget(t, harness, profileID, facadeID, inactiveTargetConnectionID, 1, false)
	modelInsertModelTarget(t, harness, profileID, recursiveFacadeID, facadeID, 0, true)

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/models", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var models []any
	decodeJSONResponse(t, response, &models)
	assertModelListItemCounts(t, findModelListItemByModelID(t, models, "count-terminal"), terminalID, 3, 2, 0, nil)
	assertModelListItemCounts(t, findModelListItemByModelID(t, models, "count-facade"), facadeID, 3, 2, 0, nil)
	assertModelListItemCounts(t, findModelListItemByModelID(t, models, "count-recursive-facade"), recursiveFacadeID, 3, 2, 0, nil)
}

func newModelContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "model_contract_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	startupService, err := startup.New(startup.Options{DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "model-contract-secret"})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup service: %v", err)
	}

	settings := config.Settings{Host: "127.0.0.1", Port: 8000, AppEnv: config.EnvironmentProduction, DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "model-contract-secret", CORSAllowedOrigins: "http://localhost:5173,http://127.0.0.1:5173"}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	modelsService, err := managementmodels.NewService(settings, managementmodels.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build models service: %v", err)
	}
	t.Cleanup(modelsService.Close)

	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "model-contract-test", ModelsService: modelsService})
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar
	return &contractHarness{client: client, conn: conn, dsn: settings.DatabaseURL, mailer: nil, server: server, service: nil, url: server.URL}
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

func modelLoadVendorIDByKey(t *testing.T, harness *contractHarness, key string) int {
	t.Helper()
	var vendorID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM vendors WHERE key = $1 LIMIT 1`, key).Scan(&vendorID); err != nil {
		t.Fatalf("load vendor %q: %v", key, err)
	}
	return vendorID
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

func modelInsertModel(t *testing.T, harness *contractHarness, profileID int, vendorID *int, apiFamily string, modelID string, displayName *string, args ...any) int {
	t.Helper()
	strategyID, isEnabled := parseModelInsertArgs(t, args)
	now := time.Now().UTC()
	var modelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`, profileID, nullableTestInt(vendorID), apiFamily, modelID, displayName, nullableTestInt(strategyID), isEnabled, now, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert model %q: %v", modelID, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return modelConfigID
}

func modelInsertFacadeModel(t *testing.T, harness *contractHarness, profileID int, vendorID *int, apiFamily string, modelID string, displayName *string, strategyID *int, isEnabled bool) int {
	t.Helper()
	now := time.Now().UTC()
	var modelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, loadbalance_strategy_id, facade_enabled, facade_selection_policy, facade_fallback_policy, is_enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, TRUE, $7, $8, $9, $10, $10) RETURNING id`, profileID, nullableTestInt(vendorID), apiFamily, modelID, displayName, nullableTestInt(strategyID), "weighted_eligible_context", "redistribute_ineligible_weight", isEnabled, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert facade model %q: %v", modelID, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return modelConfigID
}

func parseModelInsertArgs(t *testing.T, args []any) (*int, bool) {
	t.Helper()
	if len(args) == 2 {
		return modelStrategyArg(t, args[0]), args[1].(bool)
	}
	if len(args) == 3 {
		return modelStrategyArg(t, args[1]), args[2].(bool)
	}
	t.Fatalf("unexpected modelInsertModel args: %d", len(args))
	return nil, false
}

func modelStrategyArg(t *testing.T, value any) *int {
	t.Helper()
	if value == nil {
		return nil
	}
	strategyID, ok := value.(*int)
	if !ok {
		t.Fatalf("expected *int strategy arg, got %T", value)
	}
	return strategyID
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
	now := time.Now().UTC()
	var connectionID int
	var apiFamily string
	if err := harness.conn.QueryRow(context.Background(), `SELECT api_family FROM model_configs WHERE id = $1 AND profile_id = $2`, modelConfigID, profileID).Scan(&apiFamily); err != nil {
		t.Fatalf("load model %d api family: %v", modelConfigID, err)
	}
	var headersValue any
	if customHeaders != nil {
		headersValue = mustModelJSON(t, customHeaders)
	}
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, NULL, NULL, NULL, NULL, NULL, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`, profileID, apiFamily, endpointID, isActive, priority, fmt.Sprintf("conn-%d", priority), nil, headersValue, "healthy", nil, nil, now, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection for model %d endpoint %d: %v", modelConfigID, endpointID, err)
	}
	modelInsertConnectionTarget(t, harness, profileID, modelConfigID, connectionID, priority, true)
	return connectionID
}

func modelInsertModelTarget(t *testing.T, harness *contractHarness, profileID int, sourceModelConfigID int, targetModelConfigID int, position int, isEnabled bool) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, weight, target_priority, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, $4, 1, $4, $5, $6, $6)`, profileID, sourceModelConfigID, targetModelConfigID, position, isEnabled, now); err != nil {
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
	now := time.Now().UTC()
	var connectionID int
	var headersValue any
	if customHeaders != nil {
		headersValue = mustModelJSON(t, customHeaders)
	}
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, NULL, NULL, NULL, NULL, NULL, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`, profileID, apiFamily, endpointID, isActive, priority, fmt.Sprintf("conn-%d", priority), nil, headersValue, "healthy", nil, nil, now, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert standalone connection endpoint %d: %v", endpointID, err)
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

func modelInsertFXRateSetting(t *testing.T, harness *contractHarness, profileID int, modelID string, endpointID int, fxRate string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO endpoint_fx_rate_settings (profile_id, model_id, endpoint_id, fx_rate, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`, profileID, modelID, endpointID, fxRate, now, now); err != nil {
		t.Fatalf("insert endpoint fx rate setting: %v", err)
	}
}

func modelLoadFXRateModelID(t *testing.T, harness *contractHarness, profileID int, endpointID int) string {
	t.Helper()
	var modelID string
	if err := harness.conn.QueryRow(context.Background(), `SELECT model_id FROM endpoint_fx_rate_settings WHERE profile_id = $1 AND endpoint_id = $2 LIMIT 1`, profileID, endpointID).Scan(&modelID); err != nil {
		t.Fatalf("load endpoint fx rate setting: %v", err)
	}
	return modelID
}

func assertStoredModelContextCapabilities(t *testing.T, harness *contractHarness, modelConfigID int, wantContextWindowTokens *int, wantDefaultOutputTokenReserve int, wantMaxContextUtilization float64) {
	t.Helper()
	var contextWindowTokens sql.NullInt32
	var defaultOutputTokenReserve int
	var maxContextUtilization float64
	if err := harness.conn.QueryRow(context.Background(), `SELECT context_window_tokens, default_output_token_reserve, max_context_utilization FROM model_configs WHERE id = $1`, modelConfigID).Scan(&contextWindowTokens, &defaultOutputTokenReserve, &maxContextUtilization); err != nil {
		t.Fatalf("load model %d context capabilities: %v", modelConfigID, err)
	}
	if wantContextWindowTokens == nil {
		if contextWindowTokens.Valid {
			t.Fatalf("expected model %d context_window_tokens to be NULL, got %d", modelConfigID, contextWindowTokens.Int32)
		}
	} else if !contextWindowTokens.Valid || int(contextWindowTokens.Int32) != *wantContextWindowTokens {
		t.Fatalf("expected model %d context_window_tokens %d, got %+v", modelConfigID, *wantContextWindowTokens, contextWindowTokens)
	}
	if defaultOutputTokenReserve != wantDefaultOutputTokenReserve || maxContextUtilization != wantMaxContextUtilization {
		t.Fatalf("expected model %d reserve/utilization %d/%0.2f, got %d/%0.2f", modelConfigID, wantDefaultOutputTokenReserve, wantMaxContextUtilization, defaultOutputTokenReserve, maxContextUtilization)
	}
}

type expectedAccessTarget struct {
	TargetType     string
	TargetModelID  string
	ConnectionID   int
	Position       int
	Weight         *int
	TargetPriority *int
	IsEnabled      bool
}

func modelAccessTarget(targetType string, targetModelID string, connectionID *int, position int, isEnabled bool) map[string]any {
	return modelAccessTargetWithMetadata(targetType, targetModelID, connectionID, position, nil, nil, isEnabled)
}

func modelAccessTargetWithMetadata(targetType string, targetModelID string, connectionID *int, position int, weight *int, targetPriority *int, isEnabled bool) map[string]any {
	item := map[string]any{"target_type": targetType, "position": position, "is_enabled": isEnabled}
	if targetType == "model" {
		item["target_model_id"] = targetModelID
	}
	if targetType == "connection" && connectionID != nil {
		item["connection_id"] = *connectionID
	}
	if weight != nil {
		item["weight"] = *weight
	}
	if targetPriority != nil {
		item["target_priority"] = *targetPriority
	}
	return item
}

func assertNoLegacyModelFields(t *testing.T, payload map[string]any) {
	t.Helper()
	for _, key := range []string{"model_type", "proxy_selection_strategy", "proxy_targets", "connections"} {
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
	for index, raw := range items {
		item := asMap(t, raw)
		expected := want[index]
		if item["target_type"] != expected.TargetType || jsonInt(t, item["position"]) != expected.Position || item["is_enabled"] != expected.IsEnabled {
			t.Fatalf("unexpected access target at %d: got %+v want %+v", index, item, expected)
		}
		if expected.TargetType == "model" {
			if item["target_model_id"] != expected.TargetModelID {
				t.Fatalf("expected model target %q at %d, got %+v", expected.TargetModelID, index, item)
			}
			if expected.Weight != nil && jsonInt(t, item["weight"]) != *expected.Weight {
				t.Fatalf("expected model target weight %d at %d, got %+v", *expected.Weight, index, item)
			}
			if expected.TargetPriority != nil && jsonInt(t, item["target_priority"]) != *expected.TargetPriority {
				t.Fatalf("expected model target target_priority %d at %d, got %+v", *expected.TargetPriority, index, item)
			}
			if targetModel := asMap(t, item["target_model"]); targetModel["model_id"] != expected.TargetModelID {
				t.Fatalf("expected hydrated target_model %q at %d, got %+v", expected.TargetModelID, index, item)
			}
			continue
		}
		if item["weight"] != nil || item["target_priority"] != nil {
			t.Fatalf("expected connection target metadata to stay null at %d, got %+v", index, item)
		}
		if jsonInt(t, item["connection_id"]) != expected.ConnectionID {
			t.Fatalf("expected connection target %d at %d, got %+v", expected.ConnectionID, index, item)
		}
		connection := asMap(t, item["connection"])
		if jsonInt(t, connection["id"]) != expected.ConnectionID {
			t.Fatalf("expected hydrated connection %d at %d, got %+v", expected.ConnectionID, index, item)
		}
	}
}

func assertFacadeFields(t *testing.T, payload map[string]any, wantEnabled bool, wantSelectionPolicy *string, wantFallbackPolicy *string) {
	t.Helper()
	if payload["facade_enabled"] != wantEnabled {
		t.Fatalf("expected facade_enabled=%v, got %+v", wantEnabled, payload)
	}
	if wantSelectionPolicy == nil {
		if payload["facade_selection_policy"] != nil {
			t.Fatalf("expected facade_selection_policy null, got %+v", payload)
		}
	} else if payload["facade_selection_policy"] != *wantSelectionPolicy {
		t.Fatalf("expected facade_selection_policy=%q, got %+v", *wantSelectionPolicy, payload)
	}
	if wantFallbackPolicy == nil {
		if payload["facade_fallback_policy"] != nil {
			t.Fatalf("expected facade_fallback_policy null, got %+v", payload)
		}
	} else if payload["facade_fallback_policy"] != *wantFallbackPolicy {
		t.Fatalf("expected facade_fallback_policy=%q, got %+v", *wantFallbackPolicy, payload)
	}
}

func assertStoredModelFacadeFields(t *testing.T, harness *contractHarness, modelConfigID int, wantEnabled bool, wantSelectionPolicy *string, wantFallbackPolicy *string) {
	t.Helper()
	var facadeEnabled bool
	var facadeSelectionPolicy sql.NullString
	var facadeFallbackPolicy sql.NullString
	if err := harness.conn.QueryRow(context.Background(), `SELECT facade_enabled, facade_selection_policy, facade_fallback_policy FROM model_configs WHERE id = $1`, modelConfigID).Scan(&facadeEnabled, &facadeSelectionPolicy, &facadeFallbackPolicy); err != nil {
		t.Fatalf("load model %d facade fields: %v", modelConfigID, err)
	}
	if facadeEnabled != wantEnabled {
		t.Fatalf("expected stored facade_enabled=%v for model %d, got %v", wantEnabled, modelConfigID, facadeEnabled)
	}
	if wantSelectionPolicy == nil {
		if facadeSelectionPolicy.Valid {
			t.Fatalf("expected stored facade_selection_policy NULL for model %d, got %q", modelConfigID, facadeSelectionPolicy.String)
		}
	} else if !facadeSelectionPolicy.Valid || facadeSelectionPolicy.String != *wantSelectionPolicy {
		t.Fatalf("expected stored facade_selection_policy=%q for model %d, got %+v", *wantSelectionPolicy, modelConfigID, facadeSelectionPolicy)
	}
	if wantFallbackPolicy == nil {
		if facadeFallbackPolicy.Valid {
			t.Fatalf("expected stored facade_fallback_policy NULL for model %d, got %q", modelConfigID, facadeFallbackPolicy.String)
		}
	} else if !facadeFallbackPolicy.Valid || facadeFallbackPolicy.String != *wantFallbackPolicy {
		t.Fatalf("expected stored facade_fallback_policy=%q for model %d, got %+v", *wantFallbackPolicy, modelConfigID, facadeFallbackPolicy)
	}
}

func assertStoredModelTargetMetadata(t *testing.T, harness *contractHarness, sourceModelConfigID int, targetModelConfigID int, wantWeight int, wantTargetPriority int) {
	t.Helper()
	var weight int
	var targetPriority int
	if err := harness.conn.QueryRow(context.Background(), `SELECT weight, target_priority FROM model_access_targets WHERE source_model_config_id = $1 AND target_model_config_id = $2`, sourceModelConfigID, targetModelConfigID).Scan(&weight, &targetPriority); err != nil {
		t.Fatalf("load model target metadata source=%d target=%d: %v", sourceModelConfigID, targetModelConfigID, err)
	}
	if weight != wantWeight || targetPriority != wantTargetPriority {
		t.Fatalf("expected model target metadata weight=%d target_priority=%d, got weight=%d target_priority=%d", wantWeight, wantTargetPriority, weight, targetPriority)
	}
}

func modelLoadConnectionTargetID(t *testing.T, harness *contractHarness, sourceModelConfigID int, connectionID int) int {
	t.Helper()
	var targetID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM model_access_targets WHERE source_model_config_id = $1 AND target_connection_id = $2 LIMIT 1`, sourceModelConfigID, connectionID).Scan(&targetID); err != nil {
		t.Fatalf("load connection target %d for model %d: %v", connectionID, sourceModelConfigID, err)
	}
	return targetID
}

func modelConnectionTargetsManagedDetail() string {
	return "terminal targets are managed through model-scoped connection routes"
}

func assertConnectionTargetCount(t *testing.T, harness *contractHarness, connectionID int, want int) {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM model_access_targets WHERE target_connection_id = $1`, connectionID).Scan(&count); err != nil {
		t.Fatalf("count connection target rows for connection %d: %v", connectionID, err)
	}
	if count != want {
		t.Fatalf("expected %d connection target rows for connection %d, got %d", want, connectionID, count)
	}
}

func assertConnectionTargetState(t *testing.T, harness *contractHarness, sourceModelConfigID int, targetID int, connectionID int, position int, isEnabled bool) {
	t.Helper()
	var gotSourceID int
	var gotConnectionID int
	var gotPosition int
	var gotEnabled bool
	var gotWeight sql.NullInt32
	var gotTargetPriority sql.NullInt32
	if err := harness.conn.QueryRow(context.Background(), `SELECT source_model_config_id, target_connection_id, position, weight, target_priority, is_enabled FROM model_access_targets WHERE id = $1`, targetID).Scan(&gotSourceID, &gotConnectionID, &gotPosition, &gotWeight, &gotTargetPriority, &gotEnabled); err != nil {
		t.Fatalf("load connection target row %d: %v", targetID, err)
	}
	if gotSourceID != sourceModelConfigID || gotConnectionID != connectionID || gotPosition != position || gotEnabled != isEnabled {
		t.Fatalf("unexpected connection target row %d: got source=%d connection=%d position=%d enabled=%v", targetID, gotSourceID, gotConnectionID, gotPosition, gotEnabled)
	}
	if gotWeight.Valid || gotTargetPriority.Valid {
		t.Fatalf("expected connection target row %d metadata to stay NULL, got weight=%+v target_priority=%+v", targetID, gotWeight, gotTargetPriority)
	}
}

func assertNoSourceAccessTargets(t *testing.T, harness *contractHarness, sourceModelConfigID int) {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM model_access_targets WHERE source_model_config_id = $1`, sourceModelConfigID).Scan(&count); err != nil {
		t.Fatalf("count source access targets for model %d: %v", sourceModelConfigID, err)
	}
	if count != 0 {
		t.Fatalf("expected source access targets for model %d to be deleted, got %d", sourceModelConfigID, count)
	}
}

func assertModelConfigCount(t *testing.T, harness *contractHarness, modelConfigID int, want int) {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM model_configs WHERE id = $1`, modelConfigID).Scan(&count); err != nil {
		t.Fatalf("count model %d: %v", modelConfigID, err)
	}
	if count != want {
		t.Fatalf("expected model %d count %d, got %d", modelConfigID, want, count)
	}
}

func assertEndpointCount(t *testing.T, harness *contractHarness, endpointID int, want int) {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM endpoints WHERE id = $1`, endpointID).Scan(&count); err != nil {
		t.Fatalf("count endpoint %d: %v", endpointID, err)
	}
	if count != want {
		t.Fatalf("expected endpoint %d count %d, got %d", endpointID, want, count)
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

func TestModelPreferredContext(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Preferred Context Strategy")
	targetModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "preferred-context-target", nil, &strategyID, true)
	_ = targetModelID

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "openai",
		"model_id":                "preferred-context-model",
		"loadbalance_strategy_id": strategyID,
		"max_context_utilization": 0.75,
		"preferred_context_utilization_threshold": 0.70,
		"access_targets": []map[string]any{modelAccessTarget("model", "preferred-context-target", nil, 0, true)},
	}, modelHeader(profileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	modelConfigID := jsonInt(t, created["id"])
	if jsonFloat(t, created["preferred_context_utilization_threshold"]) != 0.7 {
		t.Fatalf("expected created model preferred_context_utilization_threshold=0.7, got %+v", created)
	}
	assertStoredModelPreferredContextThreshold(t, harness, modelConfigID, modelFloat64Ptr(0.7))

	clearResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/models/%d", modelConfigID), map[string]any{
		"preferred_context_utilization_threshold": nil,
		"max_context_utilization":                 0.80,
	}, modelHeader(profileID))
	assertStatus(t, clearResponse, http.StatusOK)
	var cleared map[string]any
	decodeJSONResponse(t, clearResponse, &cleared)
	if cleared["preferred_context_utilization_threshold"] != nil {
		t.Fatalf("expected cleared preferred_context_utilization_threshold to be null, got %+v", cleared)
	}
	assertStoredModelPreferredContextThreshold(t, harness, modelConfigID, nil)

	invalidZero := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "openai",
		"model_id":                "preferred-context-invalid-zero",
		"loadbalance_strategy_id": strategyID,
		"preferred_context_utilization_threshold": 0,
		"access_targets": []map[string]any{modelAccessTarget("model", "preferred-context-target", nil, 0, true)},
	}, modelHeader(profileID))
	assertErrorResponse(t, invalidZero, http.StatusBadRequest, "preferred_context_utilization_threshold must be greater than 0 and less than or equal to 1 when provided")

	invalidHigh := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "openai",
		"model_id":                "preferred-context-invalid-high",
		"loadbalance_strategy_id": strategyID,
		"preferred_context_utilization_threshold": 1.1,
		"access_targets": []map[string]any{modelAccessTarget("model", "preferred-context-target", nil, 0, true)},
	}, modelHeader(profileID))
	assertErrorResponse(t, invalidHigh, http.StatusBadRequest, "preferred_context_utilization_threshold must be greater than 0 and less than or equal to 1 when provided")

	invalidCrossField := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{
		"vendor_id":               vendorID,
		"api_family":              "openai",
		"model_id":                "preferred-context-invalid-cross-field",
		"loadbalance_strategy_id": strategyID,
		"max_context_utilization": 0.70,
		"preferred_context_utilization_threshold": 0.75,
		"access_targets": []map[string]any{modelAccessTarget("model", "preferred-context-target", nil, 0, true)},
	}, modelHeader(profileID))
	assertErrorResponse(t, invalidCrossField, http.StatusBadRequest, "preferred_context_utilization_threshold must be less than or equal to max_context_utilization when provided")

	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET max_context_utilization = 0.75, preferred_context_utilization_threshold = 0.70 WHERE id = $1`, modelConfigID); err != nil {
		t.Fatalf("reset model preferred context state: %v", err)
	}
	invalidLowerMax := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/models/%d", modelConfigID), map[string]any{
		"max_context_utilization": 0.60,
	}, modelHeader(profileID))
	assertErrorResponse(t, invalidLowerMax, http.StatusBadRequest, "preferred_context_utilization_threshold must be less than or equal to max_context_utilization when provided")
}

func assertStoredModelPreferredContextThreshold(t *testing.T, harness *contractHarness, modelConfigID int, want *float64) {
	t.Helper()
	var preferred sql.NullFloat64
	if err := harness.conn.QueryRow(context.Background(), `SELECT preferred_context_utilization_threshold FROM model_configs WHERE id = $1`, modelConfigID).Scan(&preferred); err != nil {
		t.Fatalf("load model %d preferred_context_utilization_threshold: %v", modelConfigID, err)
	}
	if want == nil {
		if preferred.Valid {
			t.Fatalf("expected model %d preferred_context_utilization_threshold NULL, got %0.2f", modelConfigID, preferred.Float64)
		}
		return
	}
	if !preferred.Valid || preferred.Float64 != *want {
		t.Fatalf("expected model %d preferred_context_utilization_threshold %0.2f, got %+v", modelConfigID, *want, preferred)
	}
}

func modelIntPtr(value int) *int {
	resolved := value
	return &resolved
}

func modelFloat64Ptr(value float64) *float64 {
	resolved := value
	return &resolved
}
