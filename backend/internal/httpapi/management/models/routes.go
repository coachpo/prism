package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/contextcapability"
	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/coachpo/prism/backend/internal/targetcompat"
)

type endpointModelsBatchRequest struct {
	EndpointIDs []int `json:"endpoint_ids"`
}

type routingPlanValidationIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

const (
	facadeSelectionPolicyWeightedEligibleContext     = "weighted_eligible_context"
	facadeFallbackPolicyRedistributeIneligibleWeight = "redistribute_ineligible_weight"
	facadeEnabledRequiresOpenAIDetail                = "facade_enabled requires api_family 'openai'"
	nestedFacadesNotSupportedDetail                  = "nested facades are not supported"
)

func (s *Service) handleListModels(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) ([]modelConfigListResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		records, err := listModelRecords(r.Context(), tx, profile.ID)
		if err != nil {
			return nil, err
		}
		counts, err := listConnectionCountsByModel(r.Context(), tx, profile.ID)
		if err != nil {
			return nil, err
		}
		vendors, strategies, accessTargets, health, err := loadModelRelations(r.Context(), tx, profile.ID, records)
		if err != nil {
			return nil, err
		}
		response := make([]modelConfigListResponse, 0, len(records))
		for _, record := range records {
			response = append(response, buildModelListResponse(record, vendors, strategies, accessTargets, counts, health))
		}
		return response, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetModel(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (modelConfigResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return modelConfigResponse{}, err
		}
		record, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, false)
		if err != nil {
			return modelConfigResponse{}, err
		}
		if !found {
			return modelConfigResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		vendors, strategies, accessTargets, _, err := loadModelRelations(r.Context(), tx, profile.ID, []modelRecord{record})
		if err != nil {
			return modelConfigResponse{}, err
		}
		return buildModelDetailResponse(record, vendors, strategies, accessTargets), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	var requestBody modelCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	normalizeCreateRequest(&requestBody)
	if err := validateCreateRequest(requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (modelConfigResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return modelConfigResponse{}, err
		}
		if requestBody.VendorID != nil {
			if err := ensureVendorExists(r.Context(), tx, *requestBody.VendorID); err != nil {
				return modelConfigResponse{}, err
			}
		}
		if err := ensureModelIDAvailable(r.Context(), tx, profile.ID, requestBody.ModelID, nil); err != nil {
			return modelConfigResponse{}, err
		}
		if err := ensureLoadbalanceStrategyExists(r.Context(), tx, profile.ID, *requestBody.LoadbalanceStrategyID); err != nil {
			return modelConfigResponse{}, err
		}
		capabilitySettings, err := contextcapability.NormalizeModelSettings(requestBody.ContextWindowTokens, requestBody.DefaultOutputTokenReserve, requestBody.MaxContextUtilization, requestBody.PreferredContextUtilizationThreshold)
		if err != nil {
			return modelConfigResponse{}, contextCapabilityDomainError(err)
		}
		now := s.nowUTC()
		record := modelRecord{ProfileID: profile.ID, VendorID: requestBody.VendorID, APIFamily: requestBody.APIFamily, ModelID: requestBody.ModelID, DisplayName: resolvePersistedDisplayName(requestBody.ModelID, requestBody.DisplayName), LoadbalanceStrategyID: requestBody.LoadbalanceStrategyID, ContextWindowTokens: capabilitySettings.ContextWindowTokens, DefaultOutputTokenReserve: capabilitySettings.DefaultOutputTokenReserve, MaxContextUtilization: capabilitySettings.MaxContextUtilization, PreferredContextUtilizationThreshold: capabilitySettings.PreferredContextUtilizationThreshold, FacadeEnabled: resolveFacadeEnabled(requestBody.FacadeEnabled), FacadeSelectionPolicy: requestBody.FacadeSelectionPolicy, FacadeFallbackPolicy: requestBody.FacadeFallbackPolicy, ContextOverflowPromotionTargetID: requestBody.ContextOverflowPromotionTargetID, IsEnabled: resolveIsEnabled(requestBody.IsEnabled), CreatedAt: now, UpdatedAt: now}
		created, err := insertModel(r.Context(), tx, record)
		if err != nil {
			return modelConfigResponse{}, err
		}
		if err := lockProfileAccessTargetRows(r.Context(), tx, profile.ID); err != nil {
			return modelConfigResponse{}, err
		}
		resolvedTargets, err := resolveAccessTargets(r.Context(), tx, profile.ID, &created.ID, created.ModelID, created.APIFamily, requestBody.AccessTargets)
		if err != nil {
			return modelConfigResponse{}, err
		}
		if err := validateFacadeWriteContract(r.Context(), tx, profile.ID, nil, created, resolvedTargets); err != nil {
			return modelConfigResponse{}, err
		}
		if created.IsEnabled && !hasEnabledResolvedAccessTarget(resolvedTargets) {
			return modelConfigResponse{}, routingPlanValidationIssueError("model_no_enabled_targets", "access_targets", "enabled models must include at least one enabled access target")
		}
		if err := ensureAccessTargetGraphAcyclic(r.Context(), tx, profile.ID, created.ID, resolvedTargets); err != nil {
			return modelConfigResponse{}, err
		}
		if err := replaceAccessTargets(r.Context(), tx, profile.ID, created.ID, resolvedTargets, now); err != nil {
			return modelConfigResponse{}, err
		}
		if err := s.validateContextOverflowPromotionTarget(r.Context(), tx, profile.ID, created); err != nil {
			return modelConfigResponse{}, err
		}
		vendors, strategies, accessTargets, _, err := loadModelRelations(r.Context(), tx, profile.ID, []modelRecord{created})
		if err != nil {
			return modelConfigResponse{}, err
		}
		return buildModelDetailResponse(created, vendors, strategies, accessTargets), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateModel(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody modelUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	normalizeUpdateRequest(&requestBody)
	if err := validateUpdateRequest(requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (modelConfigResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return modelConfigResponse{}, err
		}
		current, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, true)
		if err != nil {
			return modelConfigResponse{}, err
		}
		if !found {
			return modelConfigResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		if err := lockProfileAccessTargetRows(r.Context(), tx, profile.ID); err != nil {
			return modelConfigResponse{}, err
		}
		currentAccessTargetsByModel, err := loadAccessTargetsForModels(r.Context(), tx, profile.ID, []int{current.ID})
		if err != nil {
			return modelConfigResponse{}, err
		}
		if requestBody.VendorID.Set && requestBody.VendorID.Value != nil {
			if err := ensureVendorExists(r.Context(), tx, *requestBody.VendorID.Value); err != nil {
				return modelConfigResponse{}, err
			}
		}
		next := current
		originalModelID := current.ModelID
		if requestBody.VendorID.Set {
			next.VendorID = requestBody.VendorID.Value
		}
		if requestBody.APIFamily.Set {
			next.APIFamily = *requestBody.APIFamily.Value
		}
		if requestBody.ModelID.Set {
			next.ModelID = *requestBody.ModelID.Value
		}
		if requestBody.DisplayName.Set {
			next.DisplayName = resolvePersistedDisplayName(next.ModelID, requestBody.DisplayName.Value)
		}
		if requestBody.FacadeEnabled.Set {
			next.FacadeEnabled = requestBody.FacadeEnabled.Value
		}
		if requestBody.FacadeSelectionPolicy.Set {
			next.FacadeSelectionPolicy = requestBody.FacadeSelectionPolicy.Value
		}
		if requestBody.FacadeFallbackPolicy.Set {
			next.FacadeFallbackPolicy = requestBody.FacadeFallbackPolicy.Value
		}
		if requestBody.ContextOverflowPromotionTargetID.Set {
			next.ContextOverflowPromotionTargetID = requestBody.ContextOverflowPromotionTargetID.Value
		}
		if requestBody.IsEnabled.Set {
			next.IsEnabled = requestBody.IsEnabled.Value
		}
		if requestBody.LoadbalanceStrategyID.Set {
			next.LoadbalanceStrategyID = requestBody.LoadbalanceStrategyID.Value
		}
		if requestBody.ContextWindowTokens.Set {
			if requestBody.ContextWindowTokens.Value == nil {
				next.ContextWindowTokens = nil
			} else {
				resolvedContextWindowTokens, normalizeErr := contextcapability.NormalizeContextWindowTokens(requestBody.ContextWindowTokens.Value)
				if normalizeErr != nil {
					return modelConfigResponse{}, contextCapabilityFieldDomainError("context_window_tokens", normalizeErr)
				}
				next.ContextWindowTokens = resolvedContextWindowTokens
			}
		}
		if requestBody.DefaultOutputTokenReserve.Set {
			if requestBody.DefaultOutputTokenReserve.Value == nil {
				return modelConfigResponse{}, requiredContextCapabilityFieldError("default_output_token_reserve")
			}
			resolvedOutputTokenReserve, normalizeErr := contextcapability.NormalizeOutputTokenReserve(requestBody.DefaultOutputTokenReserve.Value)
			if normalizeErr != nil {
				return modelConfigResponse{}, contextCapabilityFieldDomainError("default_output_token_reserve", normalizeErr)
			}
			next.DefaultOutputTokenReserve = resolvedOutputTokenReserve
		}
		if requestBody.MaxContextUtilization.Set {
			if requestBody.MaxContextUtilization.Value == nil {
				return modelConfigResponse{}, requiredContextCapabilityFieldError("max_context_utilization")
			}
			resolvedMaxContextUtilization, normalizeErr := contextcapability.NormalizeMaxContextUtilization(requestBody.MaxContextUtilization.Value)
			if normalizeErr != nil {
				return modelConfigResponse{}, contextCapabilityFieldDomainError("max_context_utilization", normalizeErr)
			}
			next.MaxContextUtilization = resolvedMaxContextUtilization
		}
		if requestBody.PreferredContextUtilizationThreshold.Set {
			if requestBody.PreferredContextUtilizationThreshold.Value == nil {
				next.PreferredContextUtilizationThreshold = nil
			} else {
				resolvedPreferredContextUtilizationThreshold, normalizeErr := contextcapability.NormalizePreferredContextUtilizationThreshold(requestBody.PreferredContextUtilizationThreshold.Value, next.MaxContextUtilization)
				if normalizeErr != nil {
					return modelConfigResponse{}, contextCapabilityFieldDomainError("preferred_context_utilization_threshold", normalizeErr)
				}
				next.PreferredContextUtilizationThreshold = resolvedPreferredContextUtilizationThreshold
			}
		}
		resolvedPreferredContextUtilizationThreshold, normalizeErr := contextcapability.NormalizePreferredContextUtilizationThreshold(next.PreferredContextUtilizationThreshold, next.MaxContextUtilization)
		if normalizeErr != nil {
			return modelConfigResponse{}, contextCapabilityFieldDomainError("preferred_context_utilization_threshold", normalizeErr)
		}
		next.PreferredContextUtilizationThreshold = resolvedPreferredContextUtilizationThreshold
		if requestBody.APIFamily.Set && next.APIFamily != current.APIFamily && hasConnectionAccessTargetRecords(currentAccessTargetsByModel[current.ID]) {
			return modelConfigResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "Cannot change api_family while private connections exist"}
		}
		if requestBody.ModelID.Set && next.ModelID != current.ModelID {
			if err := ensureModelIDAvailable(r.Context(), tx, profile.ID, next.ModelID, &current.ID); err != nil {
				return modelConfigResponse{}, err
			}
		}
		if next.LoadbalanceStrategyID == nil {
			return modelConfigResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "loadbalance_strategy_id is required"}
		}
		if err := ensureLoadbalanceStrategyExists(r.Context(), tx, profile.ID, *next.LoadbalanceStrategyID); err != nil {
			return modelConfigResponse{}, err
		}
		currentAccessTargets := currentAccessTargetsByModel[current.ID]
		targetInputs := accessTargetRequestsFromRecords(currentAccessTargets)
		if requestBody.AccessTargets.Set {
			targetInputs = requestBody.AccessTargets.Value
		}
		preservedConnectionTargets := preservedConnectionTargetsFromRecords(currentAccessTargets)
		if requestBody.AccessTargets.Set {
			targetInputs = placeModelTargetRequestsAroundPreservedConnections(targetInputs, preservedConnectionTargets)
		} else {
			targetInputs = modelAccessTargetRequestsFromRecords(currentAccessTargets)
		}
		if err := validateAccessTargetsForSourceModel(next.ModelID, targetInputs); err != nil {
			return modelConfigResponse{}, err
		}
		resolvedTargets, err := resolveAccessTargets(r.Context(), tx, profile.ID, &current.ID, next.ModelID, next.APIFamily, targetInputs)
		if err != nil {
			return modelConfigResponse{}, err
		}
		if err := validateFacadeWriteContract(r.Context(), tx, profile.ID, &current.ID, next, resolvedTargets); err != nil {
			return modelConfigResponse{}, err
		}
		if next.IsEnabled && !hasEnabledResolvedOrPreservedAccessTarget(resolvedTargets, preservedConnectionTargets) {
			return modelConfigResponse{}, routingPlanValidationIssueError("model_no_enabled_targets", "access_targets", "enabled models must include at least one enabled access target")
		}
		if err := ensureAccessTargetGraphAcyclic(r.Context(), tx, profile.ID, current.ID, resolvedTargets); err != nil {
			return modelConfigResponse{}, err
		}
		next.UpdatedAt = s.nowUTC()
		updated, err := updateModel(r.Context(), tx, next)
		if err != nil {
			return modelConfigResponse{}, err
		}
		if err := replaceAccessTargetsPreservingConnections(r.Context(), tx, profile.ID, updated.ID, resolvedTargets, preservedConnectionTargets, next.UpdatedAt); err != nil {
			return modelConfigResponse{}, err
		}
		if err := s.validateContextOverflowPromotionTarget(r.Context(), tx, profile.ID, updated); err != nil {
			return modelConfigResponse{}, err
		}
		if updated.ModelID != originalModelID {
			if err := syncRenamedModelReferences(r.Context(), tx, profile.ID, originalModelID, updated.ModelID); err != nil {
				return modelConfigResponse{}, err
			}
		}
		vendors, strategies, accessTargets, _, err := loadModelRelations(r.Context(), tx, profile.ID, []modelRecord{updated})
		if err != nil {
			return modelConfigResponse{}, err
		}
		return buildModelDetailResponse(updated, vendors, strategies, accessTargets), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (deletedResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return deletedResponse{}, err
		}
		record, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, true)
		if err != nil {
			return deletedResponse{}, err
		}
		if !found {
			return deletedResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		if err := lockProfileAccessTargetRows(r.Context(), tx, profile.ID); err != nil {
			return deletedResponse{}, err
		}
		referrers, err := listAccessTargetReferrers(r.Context(), tx, profile.ID, record.ID, nil)
		if err != nil {
			return deletedResponse{}, err
		}
		if len(referrers) > 0 {
			return deletedResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("Cannot delete: models [%s] target this model", joinModelIDs(referrers))}
		}
		if err := deleteSourceAccessTargetsAndOwnedConnections(r.Context(), tx, profile.ID, record.ID); err != nil {
			return deletedResponse{}, err
		}
		if err := deleteModel(r.Context(), tx, record.ID); err != nil {
			return deletedResponse{}, err
		}
		return deletedResponse{Deleted: true}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

type accessTargetMutationItem struct {
	ID      int
	Request modelAccessTargetRequest
}

func (s *Service) handleListModelTargets(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) ([]modelAccessTargetResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		model, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, false)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		return loadModelTargetResponses(r.Context(), tx, profile.ID, model.ID)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateModelTarget(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody modelAccessTargetCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) ([]modelAccessTargetResponse, error) {
		profile, model, items, err := s.loadModelTargetMutationState(r.Context(), tx, r, modelConfigID)
		if err != nil {
			return nil, err
		}
		request, err := accessTargetRequestFromCreate(requestBody, len(items))
		if err != nil {
			return nil, err
		}
		items, err = insertAccessTargetMutationItem(items, request)
		if err != nil {
			return nil, err
		}
		return s.replaceModelTargetsFromMutationItems(r.Context(), tx, profile.ID, model, items)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateModelTarget(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	targetID, err := routeInt(r, "target_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody modelAccessTargetUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) ([]modelAccessTargetResponse, error) {
		profile, model, items, err := s.loadModelTargetMutationState(r.Context(), tx, r, modelConfigID)
		if err != nil {
			return nil, err
		}
		items, err = updateAccessTargetMutationItem(items, targetID, requestBody)
		if err != nil {
			return nil, err
		}
		if isAccessTargetMetadataOnlyUpdate(requestBody) {
			return s.updateModelTargetMetadataFromMutationItems(r.Context(), tx, profile.ID, model, items)
		}
		return s.replaceModelTargetsFromMutationItems(r.Context(), tx, profile.ID, model, items)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleMoveModelTargetPosition(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	targetID, err := routeInt(r, "target_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody modelAccessTargetMoveRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) ([]modelAccessTargetResponse, error) {
		profile, model, items, err := s.loadModelTargetMutationState(r.Context(), tx, r, modelConfigID)
		if err != nil {
			return nil, err
		}
		items, err = moveAccessTargetMutationItem(items, targetID, requestBody.ToIndex)
		if err != nil {
			return nil, err
		}
		return s.updateModelTargetMetadataFromMutationItems(r.Context(), tx, profile.ID, model, items)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteModelTarget(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	targetID, err := routeInt(r, "target_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) ([]modelAccessTargetResponse, error) {
		profile, model, items, err := s.loadModelTargetMutationState(r.Context(), tx, r, modelConfigID)
		if err != nil {
			return nil, err
		}
		deletedPrivateConnection, err := s.deletePrivateConnectionTargetFromMutationItems(r.Context(), tx, profile.ID, model, targetID, items)
		if err != nil {
			return nil, err
		}
		if deletedPrivateConnection {
			return loadModelTargetResponses(r.Context(), tx, profile.ID, model.ID)
		}
		items, err = deleteAccessTargetMutationItem(items, targetID)
		if err != nil {
			return nil, err
		}
		return s.replaceModelTargetsFromMutationItems(r.Context(), tx, profile.ID, model, items)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) deletePrivateConnectionTargetFromMutationItems(ctx context.Context, tx pgx.Tx, profileID int, model modelRecord, targetID int, items []accessTargetMutationItem) (bool, error) {
	index := findAccessTargetMutationIndex(items, targetID)
	if index == -1 {
		return false, &domainError{StatusCode: http.StatusNotFound, Detail: "Model access target not found"}
	}
	item := items[index]
	if !targetcompat.IsTerminalTargetAccessTargetType(item.Request.TargetType) {
		return false, nil
	}
	if item.Request.ConnectionID == nil {
		return true, fmt.Errorf("connection access target %d is missing connection id", targetID)
	}
	if err := ensurePrivateConnectionTargetDeleteAllowed(ctx, tx, profileID, model, targetID); err != nil {
		return true, err
	}
	if err := lockConnectionRow(ctx, tx, profileID, *item.Request.ConnectionID); err != nil {
		return true, err
	}
	if err := deleteModelAccessTargetRow(ctx, tx, targetID); err != nil {
		return true, err
	}
	if err := deleteConnectionRow(ctx, tx, *item.Request.ConnectionID); err != nil {
		return true, err
	}
	if err := compactModelAccessTargetPositions(ctx, tx, profileID, model.ID, s.nowUTC()); err != nil {
		return true, err
	}
	return true, nil
}

func ensurePrivateConnectionTargetDeleteAllowed(ctx context.Context, exec queryExecutor, profileID int, model modelRecord, deletingTargetID int) error {
	if !model.IsEnabled {
		return nil
	}
	enabledCount, err := countEnabledModelAccessTargetsExcluding(ctx, exec, profileID, model.ID, deletingTargetID)
	if err != nil {
		return err
	}
	if enabledCount == 0 {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "enabled models must include at least one enabled access target"}
	}
	return nil
}

func (s *Service) loadModelTargetMutationState(ctx context.Context, tx pgx.Tx, r *http.Request, modelConfigID int) (profiledomain.Profile, modelRecord, []accessTargetMutationItem, error) {
	profile, err := resolveEffectiveProfile(ctx, tx, r)
	if err != nil {
		return profiledomain.Profile{}, modelRecord{}, nil, err
	}
	model, found, err := loadModelRecord(ctx, tx, profile.ID, modelConfigID, true)
	if err != nil {
		return profiledomain.Profile{}, modelRecord{}, nil, err
	}
	if !found {
		return profiledomain.Profile{}, modelRecord{}, nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
	}
	if err := lockProfileAccessTargetRows(ctx, tx, profile.ID); err != nil {
		return profiledomain.Profile{}, modelRecord{}, nil, err
	}
	items, err := loadAccessTargetMutationItems(ctx, tx, profile.ID, model.ID)
	if err != nil {
		return profiledomain.Profile{}, modelRecord{}, nil, err
	}
	return profile, model, items, nil
}

func (s *Service) replaceModelTargetsFromMutationItems(ctx context.Context, tx pgx.Tx, profileID int, model modelRecord, items []accessTargetMutationItem) ([]modelAccessTargetResponse, error) {
	requests := accessTargetRequestsFromMutationItems(items)
	requests = normalizeAccessTargets(requests)
	if err := validateAccessTargets(requests); err != nil {
		return nil, err
	}
	modelRequests := modelAccessTargetRequestsOnly(requests)
	preservedConnectionTargets := preservedConnectionTargetsFromMutationItems(items)
	if err := validateAccessTargetsForSourceModel(model.ModelID, modelRequests); err != nil {
		return nil, err
	}
	resolvedTargets, err := resolveAccessTargets(ctx, tx, profileID, &model.ID, model.ModelID, model.APIFamily, modelRequests)
	if err != nil {
		return nil, err
	}
	if err := validateFacadeWriteContract(ctx, tx, profileID, &model.ID, model, resolvedTargets); err != nil {
		return nil, err
	}
	if model.IsEnabled && !hasEnabledResolvedOrPreservedAccessTarget(resolvedTargets, preservedConnectionTargets) {
		return nil, routingPlanValidationIssueError("model_no_enabled_targets", "access_targets", "enabled models must include at least one enabled access target")
	}
	if err := ensureAccessTargetGraphAcyclic(ctx, tx, profileID, model.ID, resolvedTargets); err != nil {
		return nil, err
	}
	now := s.nowUTC()
	if err := replaceAccessTargetsPreservingConnections(ctx, tx, profileID, model.ID, resolvedTargets, preservedConnectionTargets, now); err != nil {
		return nil, err
	}
	return loadModelTargetResponses(ctx, tx, profileID, model.ID)
}

func (s *Service) updateModelTargetMetadataFromMutationItems(ctx context.Context, tx pgx.Tx, profileID int, model modelRecord, items []accessTargetMutationItem) ([]modelAccessTargetResponse, error) {
	normalizeMutationItemPositions(items)
	requests := accessTargetRequestsFromMutationItems(items)
	if err := validateAccessTargets(requests); err != nil {
		return nil, err
	}
	if model.IsEnabled && !hasEnabledAccessTargetMutationItem(items) {
		return nil, routingPlanValidationIssueError("model_no_enabled_targets", "access_targets", "enabled models must include at least one enabled access target")
	}
	if err := updateAccessTargetMetadata(ctx, tx, profileID, model.ID, items, s.nowUTC()); err != nil {
		return nil, err
	}
	return loadModelTargetResponses(ctx, tx, profileID, model.ID)
}

func loadModelTargetResponses(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) ([]modelAccessTargetResponse, error) {
	accessTargets, err := loadAccessTargetsForModels(ctx, exec, profileID, []int{modelConfigID})
	if err != nil {
		return nil, err
	}
	return accessTargetResponsesFromRecords(accessTargets[modelConfigID]), nil
}

func loadAccessTargetMutationItems(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) ([]accessTargetMutationItem, error) {
	accessTargets, err := loadAccessTargetsForModels(ctx, exec, profileID, []int{modelConfigID})
	if err != nil {
		return nil, err
	}
	records := cloneAccessTargetRecords(accessTargets[modelConfigID])
	sortAccessTargetRecords(records)
	items := make([]accessTargetMutationItem, 0, len(records))
	for _, record := range records {
		items = append(items, accessTargetMutationItem{ID: record.ID, Request: accessTargetRequestFromRecord(record)})
	}
	normalizeMutationItemPositions(items)
	return items, nil
}

func accessTargetRequestFromCreate(input modelAccessTargetCreateRequest, existingCount int) (modelAccessTargetRequest, error) {
	position := existingCount
	if input.Position != nil {
		position = *input.Position
	}
	if position < 0 || position > existingCount {
		return modelAccessTargetRequest{}, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("position must be between 0 and %d", existingCount)}
	}
	request := modelAccessTargetRequest{TargetType: targetcompat.NormalizeAccessTargetType(input.TargetType), TargetModelID: normalizeOptionalString(input.TargetModelID, false, false), ConnectionID: copyIntPtr(input.ConnectionID), Position: position, Weight: copyIntPtr(input.Weight), TargetPriority: copyIntPtr(input.TargetPriority), IsEnabled: input.IsEnabled}
	if err := validatePublicAccessTarget(request); err != nil {
		return modelAccessTargetRequest{}, err
	}
	return request, nil
}

func accessTargetRequestFromRecord(record accessTargetRecord) modelAccessTargetRequest {
	enabled := record.IsEnabled
	request := modelAccessTargetRequest{TargetType: record.TargetType, Position: record.Position, Weight: copyIntPtr(record.Weight), TargetPriority: copyIntPtr(record.TargetPriority), IsEnabled: &enabled}
	if targetcompat.IsModelAccessTargetType(record.TargetType) && record.TargetModel != nil {
		request.TargetModelID = stringPtr(record.TargetModel.ModelID)
	}
	if targetcompat.IsTerminalTargetAccessTargetType(record.TargetType) {
		request.ConnectionID = copyIntPtr(record.TargetConnectionID)
	}
	return request
}

func insertAccessTargetMutationItem(items []accessTargetMutationItem, request modelAccessTargetRequest) ([]accessTargetMutationItem, error) {
	position := request.Position
	if position < 0 || position > len(items) {
		return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("position must be between 0 and %d", len(items))}
	}
	normalizeMutationItemPositions(items)
	for index := range items {
		if items[index].Request.Position >= position {
			items[index].Request.Position++
		}
	}
	items = append(items, accessTargetMutationItem{Request: request})
	normalizeMutationItemPositions(items)
	return items, nil
}

func updateAccessTargetMutationItem(items []accessTargetMutationItem, targetID int, input modelAccessTargetUpdateRequest) ([]accessTargetMutationItem, error) {
	index := findAccessTargetMutationIndex(items, targetID)
	if index == -1 {
		return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model access target not found"}
	}
	item := items[index]
	if targetcompat.IsTerminalTargetAccessTargetType(item.Request.TargetType) {
		return updateConnectionAccessTargetMutationItem(items, targetID, index, input)
	}
	updated := item.Request
	if input.TargetType.Set {
		if input.TargetType.Value != nil && targetcompat.IsTerminalTargetAccessTargetType(*input.TargetType.Value) {
			return nil, connectionAccessTargetsManagedError()
		}
		if input.TargetType.Value == nil {
			updated.TargetType = ""
		} else {
			updated.TargetType = targetcompat.NormalizeAccessTargetType(*input.TargetType.Value)
		}
		if targetcompat.IsModelAccessTargetType(updated.TargetType) {
			updated.ConnectionID = nil
		}
		if targetcompat.IsTerminalTargetAccessTargetType(updated.TargetType) {
			updated.TargetModelID = nil
		}
	}
	if input.TargetModelID.Set {
		updated.TargetModelID = normalizeOptionalString(input.TargetModelID.Value, false, false)
		if !input.TargetType.Set && updated.TargetModelID != nil && strings.TrimSpace(*updated.TargetModelID) != "" {
			updated.TargetType = targetcompat.AccessTargetTypeModel
			updated.ConnectionID = nil
		}
	}
	if input.ConnectionID.Set || input.TargetConnectionID.Set {
		return nil, connectionAccessTargetsManagedError()
	}
	if input.Weight.Set {
		if input.Weight.Value == nil {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "weight is required"}
		}
		updated.Weight = copyIntPtr(input.Weight.Value)
	}
	if input.TargetPriority.Set {
		if input.TargetPriority.Value == nil {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "target_priority is required"}
		}
		updated.TargetPriority = copyIntPtr(input.TargetPriority.Value)
	}
	if input.IsEnabled.Set {
		updated.IsEnabled = &input.IsEnabled.Value
	}
	item.Request = updated
	items[index] = item
	if input.Position.Set {
		if input.Position.Value == nil {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "position is required"}
		}
		return moveAccessTargetMutationItem(items, targetID, *input.Position.Value)
	}
	normalizeMutationItemPositions(items)
	return items, nil
}

func moveAccessTargetMutationItem(items []accessTargetMutationItem, targetID int, toIndex int) ([]accessTargetMutationItem, error) {
	if len(items) == 0 {
		return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model access target not found"}
	}
	if toIndex < 0 || toIndex >= len(items) {
		return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("to_index must be between 0 and %d", len(items)-1)}
	}
	normalizeMutationItemPositions(items)
	fromIndex := findAccessTargetMutationIndex(items, targetID)
	if fromIndex == -1 {
		return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model access target not found"}
	}
	moved := items[fromIndex]
	items = append(items[:fromIndex], items[fromIndex+1:]...)
	items = append(items[:toIndex], append([]accessTargetMutationItem{moved}, items[toIndex:]...)...)
	assignMutationItemPositions(items)
	return items, nil
}

func deleteAccessTargetMutationItem(items []accessTargetMutationItem, targetID int) ([]accessTargetMutationItem, error) {
	index := findAccessTargetMutationIndex(items, targetID)
	if index == -1 {
		return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model access target not found"}
	}
	if targetcompat.IsTerminalTargetAccessTargetType(items[index].Request.TargetType) {
		return nil, connectionAccessTargetsManagedError()
	}
	items = append(items[:index], items[index+1:]...)
	assignMutationItemPositions(items)
	return items, nil
}

func accessTargetRequestsFromMutationItems(items []accessTargetMutationItem) []modelAccessTargetRequest {
	normalizeMutationItemPositions(items)
	requests := make([]modelAccessTargetRequest, 0, len(items))
	for _, item := range items {
		requests = append(requests, item.Request)
	}
	return requests
}

func updateConnectionAccessTargetMutationItem(items []accessTargetMutationItem, targetID int, index int, input modelAccessTargetUpdateRequest) ([]accessTargetMutationItem, error) {
	if input.TargetType.Set || input.TargetModelID.Set || input.ConnectionID.Set || input.TargetConnectionID.Set {
		return nil, connectionAccessTargetsManagedError()
	}
	if input.Weight.Set && input.Weight.Value != nil {
		return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "weight must be omitted for terminal targets"}
	}
	if input.TargetPriority.Set && input.TargetPriority.Value != nil {
		return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "target_priority must be omitted for terminal targets"}
	}
	if input.IsEnabled.Set {
		items[index].Request.IsEnabled = &input.IsEnabled.Value
	}
	if input.Position.Set {
		if input.Position.Value == nil {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "position is required"}
		}
		return moveAccessTargetMutationItem(items, targetID, *input.Position.Value)
	}
	normalizeMutationItemPositions(items)
	return items, nil
}

func isAccessTargetMetadataOnlyUpdate(input modelAccessTargetUpdateRequest) bool {
	return !input.TargetType.Set && !input.TargetModelID.Set && !input.ConnectionID.Set && !input.TargetConnectionID.Set
}

func hasEnabledAccessTargetMutationItem(items []accessTargetMutationItem) bool {
	for _, item := range items {
		if item.Request.IsEnabled == nil || *item.Request.IsEnabled {
			return true
		}
	}
	return false
}

func modelAccessTargetRequestsOnly(values []modelAccessTargetRequest) []modelAccessTargetRequest {
	items := make([]modelAccessTargetRequest, 0, len(values))
	for _, value := range values {
		if targetcompat.IsModelAccessTargetType(value.TargetType) {
			items = append(items, value)
		}
	}
	return items
}

func modelAccessTargetRequestsFromRecords(records []accessTargetRecord) []modelAccessTargetRequest {
	requests := accessTargetRequestsFromRecords(records)
	return modelAccessTargetRequestsOnly(requests)
}

func preservedConnectionTargetsFromRecords(records []accessTargetRecord) []preservedConnectionAccessTarget {
	ordered := cloneAccessTargetRecords(records)
	sortAccessTargetRecords(ordered)
	items := make([]preservedConnectionAccessTarget, 0)
	for _, record := range ordered {
		if !targetcompat.IsTerminalTargetAccessTargetType(record.TargetType) {
			continue
		}
		items = append(items, preservedConnectionAccessTarget{ID: record.ID, Position: record.Position, IsEnabled: record.IsEnabled})
	}
	return items
}

func placeModelTargetRequestsAroundPreservedConnections(requests []modelAccessTargetRequest, preserved []preservedConnectionAccessTarget) []modelAccessTargetRequest {
	if len(requests) == 0 || len(preserved) == 0 {
		return requests
	}
	reservedPositions := map[int]struct{}{}
	for _, target := range preserved {
		reservedPositions[target.Position] = struct{}{}
	}
	placed := make([]modelAccessTargetRequest, len(requests))
	copy(placed, requests)
	indices := make([]int, 0, len(placed))
	for index := range placed {
		indices = append(indices, index)
	}
	sort.SliceStable(indices, func(left int, right int) bool {
		leftRequest := placed[indices[left]]
		rightRequest := placed[indices[right]]
		if leftRequest.Position == rightRequest.Position {
			return accessTargetInputKey(leftRequest) < accessTargetInputKey(rightRequest)
		}
		return leftRequest.Position < rightRequest.Position
	})
	nextPosition := 0
	for _, index := range indices {
		for {
			if _, reserved := reservedPositions[nextPosition]; !reserved {
				break
			}
			nextPosition++
		}
		placed[index].Position = nextPosition
		nextPosition++
	}
	return placed
}

func preservedConnectionTargetsFromMutationItems(items []accessTargetMutationItem) []preservedConnectionAccessTarget {
	normalizeMutationItemPositions(items)
	preserved := make([]preservedConnectionAccessTarget, 0)
	for _, item := range items {
		if !targetcompat.IsTerminalTargetAccessTargetType(item.Request.TargetType) {
			continue
		}
		enabled := true
		if item.Request.IsEnabled != nil {
			enabled = *item.Request.IsEnabled
		}
		preserved = append(preserved, preservedConnectionAccessTarget{ID: item.ID, Position: item.Request.Position, IsEnabled: enabled, Update: true})
	}
	return preserved
}

func hasConnectionAccessTargetRecords(records []accessTargetRecord) bool {
	for _, record := range records {
		if targetcompat.IsTerminalTargetAccessTargetType(record.TargetType) {
			return true
		}
	}
	return false
}

func hasEnabledResolvedOrPreservedAccessTarget(resolved []resolvedAccessTarget, preserved []preservedConnectionAccessTarget) bool {
	if hasEnabledResolvedAccessTarget(resolved) {
		return true
	}
	for _, target := range preserved {
		if target.IsEnabled {
			return true
		}
	}
	return false
}

func normalizeMutationItemPositions(items []accessTargetMutationItem) {
	sort.SliceStable(items, func(left int, right int) bool {
		if items[left].Request.Position == items[right].Request.Position {
			return items[left].ID < items[right].ID
		}
		return items[left].Request.Position < items[right].Request.Position
	})
	assignMutationItemPositions(items)
}

func assignMutationItemPositions(items []accessTargetMutationItem) {
	for index := range items {
		items[index].Request.Position = index
	}
}

func findAccessTargetMutationIndex(items []accessTargetMutationItem, targetID int) int {
	for index, item := range items {
		if item.ID == targetID {
			return index
		}
	}
	return -1
}

func (s *Service) handleModelsByEndpoint(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) ([]modelConfigListResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		rows, err := listEndpointModelRows(r.Context(), tx, profile.ID, []int{endpointID})
		if err != nil {
			return nil, err
		}
		records, counts := collectEndpointModelCounts(rows)
		sort.Slice(records, func(left int, right int) bool {
			return records[left].ModelID < records[right].ModelID
		})
		vendors, strategies, accessTargets, health, err := loadModelRelations(r.Context(), tx, profile.ID, records)
		if err != nil {
			return nil, err
		}
		response := make([]modelConfigListResponse, 0, len(records))
		for _, record := range records {
			response = append(response, buildModelListResponse(record, vendors, strategies, accessTargets, counts, health))
		}
		return response, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleModelsByEndpoints(w http.ResponseWriter, r *http.Request) {
	var requestBody endpointModelsBatchRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (endpointModelsBatchResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return endpointModelsBatchResponse{}, err
		}
		if len(requestBody.EndpointIDs) == 0 {
			return endpointModelsBatchResponse{Items: []endpointModelsBatchItem{}}, nil
		}
		rows, err := listEndpointModelRows(r.Context(), tx, profile.ID, requestBody.EndpointIDs)
		if err != nil {
			return endpointModelsBatchResponse{}, err
		}
		byEndpointRecords, byEndpointCounts, allRecords := collectBatchEndpointModels(rows)
		vendors, strategies, accessTargets, health, err := loadModelRelations(r.Context(), tx, profile.ID, allRecords)
		if err != nil {
			return endpointModelsBatchResponse{}, err
		}
		items := make([]endpointModelsBatchItem, 0, len(requestBody.EndpointIDs))
		for _, endpointID := range requestBody.EndpointIDs {
			records := byEndpointRecords[endpointID]
			sort.Slice(records, func(left int, right int) bool {
				return records[left].ModelID < records[right].ModelID
			})
			models := make([]modelConfigListResponse, 0, len(records))
			for _, record := range records {
				models = append(models, buildModelListResponse(record, vendors, strategies, accessTargets, byEndpointCounts[endpointID], health))
			}
			items = append(items, endpointModelsBatchItem{EndpointID: endpointID, Models: models})
		}
		return endpointModelsBatchResponse{Items: items}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func resolveEffectiveProfile(ctx context.Context, tx pgx.Tx, r *http.Request) (profiledomain.Profile, error) {
	return profiledomain.ResolveEffectiveProfile(ctx, tx, r.Header.Get(profiledomain.ProfileIDHeader))
}

func loadModelRelations(ctx context.Context, tx pgx.Tx, profileID int, records []modelRecord) (map[int]vendorRecord, map[int]strategyRecord, map[int][]accessTargetRecord, map[string]modelHealthStats, error) {
	vendorIDs := uniqueIntValues(records, func(record modelRecord) *int { return record.VendorID })
	strategyIDs := uniqueIntValues(records, func(record modelRecord) *int { return record.LoadbalanceStrategyID })
	modelIDs := uniqueModelIDs(records)
	vendors, err := loadVendorRecordsByIDs(ctx, tx, vendorIDs)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	strategies, err := loadStrategyRecordsByIDs(ctx, tx, profileID, strategyIDs)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	accessTargets, err := loadAccessTargetsForModels(ctx, tx, profileID, modelIDs)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	health, err := listModelHealthStats(ctx, tx, profileID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return vendors, strategies, accessTargets, health, nil
}

func collectEndpointModelCounts(rows []endpointModelConnectionRow) ([]modelRecord, map[int]modelConnectionCounts) {
	recordsByID := map[int]modelRecord{}
	counts := map[int]modelConnectionCounts{}
	seenTerminalConnections := map[int]map[int]struct{}{}
	for _, row := range rows {
		recordsByID[row.ReachableModelID] = row.ReachableModelData
		if _, ok := seenTerminalConnections[row.ReachableModelID]; !ok {
			seenTerminalConnections[row.ReachableModelID] = map[int]struct{}{}
		}
		if _, seen := seenTerminalConnections[row.ReachableModelID][row.TerminalConnectionID]; seen {
			continue
		}
		seenTerminalConnections[row.ReachableModelID][row.TerminalConnectionID] = struct{}{}
		count := counts[row.ReachableModelID]
		count.Total++
		if row.ConnectionIsActive {
			count.Active++
		}
		counts[row.ReachableModelID] = count
	}
	records := make([]modelRecord, 0, len(recordsByID))
	for _, record := range recordsByID {
		records = append(records, record)
	}
	return records, counts
}

func collectBatchEndpointModels(rows []endpointModelConnectionRow) (map[int][]modelRecord, map[int]map[int]modelConnectionCounts, []modelRecord) {
	byEndpointRecords := map[int][]modelRecord{}
	byEndpointCounts := map[int]map[int]modelConnectionCounts{}
	allRecordsByID := map[int]modelRecord{}
	seenByEndpoint := map[int]map[int]struct{}{}
	seenTerminalConnections := map[int]map[int]map[int]struct{}{}
	for _, row := range rows {
		allRecordsByID[row.ReachableModelID] = row.ReachableModelData
		if _, ok := byEndpointCounts[row.EndpointID]; !ok {
			byEndpointCounts[row.EndpointID] = map[int]modelConnectionCounts{}
		}
		if _, ok := seenTerminalConnections[row.EndpointID]; !ok {
			seenTerminalConnections[row.EndpointID] = map[int]map[int]struct{}{}
		}
		if _, ok := seenTerminalConnections[row.EndpointID][row.ReachableModelID]; !ok {
			seenTerminalConnections[row.EndpointID][row.ReachableModelID] = map[int]struct{}{}
		}
		if _, seen := seenTerminalConnections[row.EndpointID][row.ReachableModelID][row.TerminalConnectionID]; !seen {
			seenTerminalConnections[row.EndpointID][row.ReachableModelID][row.TerminalConnectionID] = struct{}{}
			count := byEndpointCounts[row.EndpointID][row.ReachableModelID]
			count.Total++
			if row.ConnectionIsActive {
				count.Active++
			}
			byEndpointCounts[row.EndpointID][row.ReachableModelID] = count
		}
		if _, ok := seenByEndpoint[row.EndpointID]; !ok {
			seenByEndpoint[row.EndpointID] = map[int]struct{}{}
		}
		if _, seen := seenByEndpoint[row.EndpointID][row.ReachableModelID]; !seen {
			byEndpointRecords[row.EndpointID] = append(byEndpointRecords[row.EndpointID], row.ReachableModelData)
			seenByEndpoint[row.EndpointID][row.ReachableModelID] = struct{}{}
		}
	}
	allRecords := make([]modelRecord, 0, len(allRecordsByID))
	for _, record := range allRecordsByID {
		allRecords = append(allRecords, record)
	}
	sortModelRecordsByID(allRecords)
	return byEndpointRecords, byEndpointCounts, allRecords
}

func uniqueIntValues(records []modelRecord, selector func(modelRecord) *int) []int {
	seen := map[int]struct{}{}
	values := make([]int, 0)
	for _, record := range records {
		value := selector(record)
		if value == nil {
			continue
		}
		if _, ok := seen[*value]; ok {
			continue
		}
		seen[*value] = struct{}{}
		values = append(values, *value)
	}
	sort.Ints(values)
	return values
}

func uniqueModelIDs(records []modelRecord) []int {
	seen := map[int]struct{}{}
	values := make([]int, 0, len(records))
	for _, record := range records {
		if _, ok := seen[record.ID]; ok {
			continue
		}
		seen[record.ID] = struct{}{}
		values = append(values, record.ID)
	}
	sort.Ints(values)
	return values
}

func normalizeCreateRequest(requestBody *modelCreateRequest) {
	requestBody.APIFamily = strings.ToLower(strings.TrimSpace(requestBody.APIFamily))
	requestBody.ModelID = strings.TrimSpace(requestBody.ModelID)
	requestBody.DisplayName = normalizeOptionalString(requestBody.DisplayName, false, true)
	requestBody.FacadeSelectionPolicy = normalizeOptionalString(requestBody.FacadeSelectionPolicy, true, true)
	requestBody.FacadeFallbackPolicy = normalizeOptionalString(requestBody.FacadeFallbackPolicy, true, true)
	requestBody.ContextOverflowPromotionTargetID = normalizeOptionalString(requestBody.ContextOverflowPromotionTargetID, false, true)
	requestBody.AccessTargets = normalizeAccessTargets(requestBody.AccessTargets)
}

func normalizeUpdateRequest(requestBody *modelUpdateRequest) {
	requestBody.APIFamily = optionalString{Set: requestBody.APIFamily.Set, Value: normalizeOptionalString(requestBody.APIFamily.Value, true, false)}
	requestBody.ModelID = optionalString{Set: requestBody.ModelID.Set, Value: normalizeOptionalString(requestBody.ModelID.Value, false, false)}
	requestBody.DisplayName = optionalString{Set: requestBody.DisplayName.Set, Value: normalizeOptionalString(requestBody.DisplayName.Value, false, true)}
	requestBody.FacadeSelectionPolicy = optionalString{Set: requestBody.FacadeSelectionPolicy.Set, Value: normalizeOptionalString(requestBody.FacadeSelectionPolicy.Value, true, true)}
	requestBody.FacadeFallbackPolicy = optionalString{Set: requestBody.FacadeFallbackPolicy.Set, Value: normalizeOptionalString(requestBody.FacadeFallbackPolicy.Value, true, true)}
	requestBody.ContextOverflowPromotionTargetID = optionalString{Set: requestBody.ContextOverflowPromotionTargetID.Set, Value: normalizeOptionalString(requestBody.ContextOverflowPromotionTargetID.Value, false, true)}
	requestBody.AccessTargets = optionalAccessTargets{Set: requestBody.AccessTargets.Set, Value: normalizeAccessTargets(requestBody.AccessTargets.Value)}
}

func normalizeOptionalString(value *string, lower bool, emptyToNil bool) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if lower {
		trimmed = strings.ToLower(trimmed)
	}
	if emptyToNil && trimmed == "" {
		return nil
	}
	resolved := trimmed
	return &resolved
}

func normalizeAccessTargets(values []modelAccessTargetRequest) []modelAccessTargetRequest {
	if values == nil {
		return nil
	}
	normalized := make([]modelAccessTargetRequest, 0, len(values))
	for _, value := range values {
		normalizedTarget := value
		normalizedTarget.TargetType = targetcompat.NormalizeAccessTargetType(value.TargetType)
		normalizedTarget.TargetModelID = normalizeOptionalString(value.TargetModelID, false, false)
		normalized = append(normalized, normalizedTarget)
	}
	return normalized
}

func validateCreateRequest(requestBody modelCreateRequest) error {
	if strings.TrimSpace(requestBody.APIFamily) == "" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "api_family is required"}
	}
	if !isValidAPIFamily(requestBody.APIFamily) {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "api_family must be one of 'openai', 'anthropic', or 'gemini'"}
	}
	if strings.TrimSpace(requestBody.ModelID) == "" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "model_id is required"}
	}
	if requestBody.LoadbalanceStrategyID == nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "loadbalance_strategy_id is required"}
	}
	if err := validateFacadePolicyValues(requestBody.FacadeSelectionPolicy, requestBody.FacadeFallbackPolicy); err != nil {
		return err
	}
	if err := validateModelContextCapabilitiesCreate(requestBody); err != nil {
		return err
	}
	if err := validatePublicAccessTargets(requestBody.AccessTargets); err != nil {
		return err
	}
	return validateAccessTargetsForSourceModel(requestBody.ModelID, requestBody.AccessTargets)
}

func validateUpdateRequest(requestBody modelUpdateRequest) error {
	if requestBody.APIFamily.Set {
		if requestBody.APIFamily.Value == nil || !isValidAPIFamily(*requestBody.APIFamily.Value) {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "api_family must be one of 'openai', 'anthropic', or 'gemini'"}
		}
	}
	if requestBody.ModelID.Set && (requestBody.ModelID.Value == nil || strings.TrimSpace(*requestBody.ModelID.Value) == "") {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "model_id is required"}
	}
	if requestBody.LoadbalanceStrategyID.Set && requestBody.LoadbalanceStrategyID.Value == nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "loadbalance_strategy_id is required"}
	}
	if err := validateFacadePolicyValues(requestBody.FacadeSelectionPolicy.Value, requestBody.FacadeFallbackPolicy.Value); err != nil {
		return err
	}
	if err := validateModelContextCapabilitiesUpdate(requestBody); err != nil {
		return err
	}
	if requestBody.AccessTargets.Set {
		return validatePublicAccessTargets(requestBody.AccessTargets.Value)
	}
	return nil
}

func validateModelContextCapabilitiesCreate(requestBody modelCreateRequest) error {
	if _, err := contextcapability.NormalizeContextWindowTokens(requestBody.ContextWindowTokens); err != nil {
		return contextCapabilityFieldDomainError("context_window_tokens", err)
	}
	if _, err := contextcapability.NormalizeOutputTokenReserve(requestBody.DefaultOutputTokenReserve); err != nil {
		return contextCapabilityFieldDomainError("default_output_token_reserve", err)
	}
	resolvedMaxContextUtilization, err := contextcapability.NormalizeMaxContextUtilization(requestBody.MaxContextUtilization)
	if err != nil {
		return contextCapabilityFieldDomainError("max_context_utilization", err)
	}
	if _, err := contextcapability.NormalizePreferredContextUtilizationThreshold(requestBody.PreferredContextUtilizationThreshold, resolvedMaxContextUtilization); err != nil {
		return contextCapabilityFieldDomainError("preferred_context_utilization_threshold", err)
	}
	return nil
}

func validateModelContextCapabilitiesUpdate(requestBody modelUpdateRequest) error {
	if requestBody.ContextWindowTokens.Set && requestBody.ContextWindowTokens.Value != nil {
		if _, err := contextcapability.NormalizeContextWindowTokens(requestBody.ContextWindowTokens.Value); err != nil {
			return contextCapabilityFieldDomainError("context_window_tokens", err)
		}
	}
	if requestBody.DefaultOutputTokenReserve.Set {
		if requestBody.DefaultOutputTokenReserve.Value == nil {
			return requiredContextCapabilityFieldError("default_output_token_reserve")
		}
		if _, err := contextcapability.NormalizeOutputTokenReserve(requestBody.DefaultOutputTokenReserve.Value); err != nil {
			return contextCapabilityFieldDomainError("default_output_token_reserve", err)
		}
	}
	if requestBody.MaxContextUtilization.Set {
		if requestBody.MaxContextUtilization.Value == nil {
			return requiredContextCapabilityFieldError("max_context_utilization")
		}
		if _, err := contextcapability.NormalizeMaxContextUtilization(requestBody.MaxContextUtilization.Value); err != nil {
			return contextCapabilityFieldDomainError("max_context_utilization", err)
		}
	}
	if requestBody.PreferredContextUtilizationThreshold.Set && requestBody.PreferredContextUtilizationThreshold.Value != nil {
		if _, err := contextcapability.NormalizePreferredContextUtilizationThreshold(requestBody.PreferredContextUtilizationThreshold.Value, 1); err != nil {
			return contextCapabilityFieldDomainError("preferred_context_utilization_threshold", err)
		}
	}
	return nil
}

func contextCapabilityDomainError(err error) error {
	return &domainError{StatusCode: http.StatusBadRequest, Detail: err.Error()}
}

func contextCapabilityFieldDomainError(fieldName string, err error) error {
	return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("%s %s", fieldName, err.Error())}
}

func requiredContextCapabilityFieldError(fieldName string) error {
	return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("%s is required", fieldName)}
}

func validateFacadePolicyValues(selectionPolicy *string, fallbackPolicy *string) error {
	if selectionPolicy != nil && *selectionPolicy != facadeSelectionPolicyWeightedEligibleContext {
		return routingPlanValidationIssueError("facade_selection_policy_invalid", "facade_selection_policy", "facade_selection_policy must be 'weighted_eligible_context'")
	}
	if fallbackPolicy != nil && *fallbackPolicy != facadeFallbackPolicyRedistributeIneligibleWeight {
		return routingPlanValidationIssueError("facade_fallback_policy_invalid", "facade_fallback_policy", "facade_fallback_policy must be 'redistribute_ineligible_weight'")
	}
	return nil
}

func validateFacadeWriteContract(ctx context.Context, exec queryExecutor, profileID int, modelConfigID *int, record modelRecord, resolvedTargets []resolvedAccessTarget) error {
	if err := validateFacadeConfiguration(record); err != nil {
		return err
	}
	if err := ensureNoNestedFacadeTargets(resolvedTargets); err != nil {
		return err
	}
	if !record.FacadeEnabled || modelConfigID == nil {
		return nil
	}
	referrers, err := listAccessTargetReferrers(ctx, exec, profileID, *modelConfigID, nil)
	if err != nil {
		return err
	}
	if len(referrers) > 0 {
		return routingPlanValidationIssueError("nested_facade_target", "facade_enabled", nestedFacadesNotSupportedDetail)
	}
	return nil
}

func validateFacadeConfiguration(record modelRecord) error {
	if err := validateFacadePolicyValues(record.FacadeSelectionPolicy, record.FacadeFallbackPolicy); err != nil {
		return err
	}
	if !record.FacadeEnabled {
		return nil
	}
	if record.APIFamily != "openai" {
		return routingPlanValidationIssueError("model_api_family_invalid", "api_family", facadeEnabledRequiresOpenAIDetail)
	}
	if record.FacadeSelectionPolicy == nil {
		return routingPlanValidationIssueError("facade_selection_policy_missing", "facade_selection_policy", "facade_selection_policy is required when facade_enabled is true")
	}
	if record.FacadeFallbackPolicy == nil {
		return routingPlanValidationIssueError("facade_fallback_policy_missing", "facade_fallback_policy", "facade_fallback_policy is required when facade_enabled is true")
	}
	return nil
}

func ensureNoNestedFacadeTargets(resolvedTargets []resolvedAccessTarget) error {
	for _, target := range resolvedTargets {
		if target.Model != nil && target.Model.FacadeEnabled {
			return routingPlanValidationIssueError("nested_facade_target", accessTargetIssuePath(target.Position, "target_model_id"), nestedFacadesNotSupportedDetail)
		}
	}
	return nil
}

type promotionTargetTerminalStats struct {
	LargestUsableContextWindowTokens int
	TerminalConnectionIDs            map[int]struct{}
}

func (stats promotionTargetTerminalStats) overlaps(other promotionTargetTerminalStats) bool {
	if len(stats.TerminalConnectionIDs) == 0 || len(other.TerminalConnectionIDs) == 0 {
		return false
	}
	for connectionID := range stats.TerminalConnectionIDs {
		if _, ok := other.TerminalConnectionIDs[connectionID]; ok {
			return true
		}
	}
	return false
}

func validateConfiguredPromotionTarget(ctx context.Context, exec queryExecutor, profileID int, source modelRecord) error {
	targetModelID := strings.TrimSpace(nullablePromotionTargetID(source.ContextOverflowPromotionTargetID))
	if targetModelID == "" {
		return nil
	}
	target, foundInProfile, foundElsewhere, err := loadPromotionTargetModelRecord(ctx, exec, profileID, targetModelID)
	if err != nil {
		return err
	}
	if !foundInProfile {
		if foundElsewhere {
			return promotionTargetValidationIssueError(promotionTargetValidationCodeCrossProfile, "context_overflow_promotion_target_id must reference a model in the selected profile")
		}
		return promotionTargetValidationIssueError(promotionTargetValidationCodeUnknown, "context_overflow_promotion_target_id must reference an existing model")
	}
	if target.ID == source.ID || target.ModelID == source.ModelID {
		return promotionTargetValidationIssueError(promotionTargetValidationCodeSelf, "context_overflow_promotion_target_id cannot reference the source model")
	}
	if !target.IsEnabled {
		return promotionTargetValidationIssueError(promotionTargetValidationCodeDisabled, "context_overflow_promotion_target_id must reference an enabled model")
	}
	if target.FacadeEnabled {
		return promotionTargetValidationIssueError(promotionTargetValidationCodeFacade, "context_overflow_promotion_target_id must reference a non-facade model")
	}
	if target.APIFamily != source.APIFamily {
		return promotionTargetValidationIssueError(promotionTargetValidationCodeAPIFamilyMismatch, "context_overflow_promotion_target_id must reference a model with the same api_family")
	}
	sourceStats, err := loadPromotionTargetTerminalStats(ctx, exec, profileID, source.ID, source.APIFamily)
	if err != nil {
		return err
	}
	targetStats, err := loadPromotionTargetTerminalStats(ctx, exec, profileID, target.ID, target.APIFamily)
	if err != nil {
		return err
	}
	if sourceStats.overlaps(targetStats) {
		return promotionTargetValidationIssueError(promotionTargetValidationCodeSameTerminal, "context_overflow_promotion_target_id must not resolve to the same terminal target as the source model")
	}
	if targetStats.LargestUsableContextWindowTokens <= sourceStats.LargestUsableContextWindowTokens {
		return promotionTargetValidationIssueError(promotionTargetValidationCodeContextWindowNotLarger, "context_overflow_promotion_target_id must reference a model with a strictly larger usable context window")
	}
	return nil
}

func nullablePromotionTargetID(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func promotionTargetValidationIssueError(code string, detail string) error {
	return routingPlanValidationIssueError(code, contextOverflowPromotionTargetField, detail)
}

func loadPromotionTargetModelRecord(ctx context.Context, exec queryExecutor, profileID int, targetModelID string) (modelRecord, bool, bool, error) {
	record, found, err := loadModelRecordByModelID(ctx, exec, profileID, targetModelID)
	if err != nil {
		return modelRecord{}, false, false, err
	}
	if found {
		return record, true, false, nil
	}
	foundElsewhere, err := promotionTargetModelExistsOutsideProfile(ctx, exec, profileID, targetModelID)
	if err != nil {
		return modelRecord{}, false, false, err
	}
	return modelRecord{}, false, foundElsewhere, nil
}

func loadModelRecordByModelID(ctx context.Context, exec queryExecutor, profileID int, modelID string) (modelRecord, bool, error) {
	record, err := scanModelRecord(exec.QueryRow(ctx, `SELECT `+modelRecordSelectColumns+` FROM model_configs WHERE profile_id = $1 AND model_id = $2 LIMIT 1`, profileID, modelID))
	if err == pgx.ErrNoRows {
		return modelRecord{}, false, nil
	}
	if err != nil {
		return modelRecord{}, false, fmt.Errorf("load model %q in profile %d: %w", modelID, profileID, err)
	}
	return record, true, nil
}

func promotionTargetModelExistsOutsideProfile(ctx context.Context, exec queryExecutor, profileID int, modelID string) (bool, error) {
	var existingID int
	err := exec.QueryRow(ctx, `SELECT id FROM model_configs WHERE profile_id <> $1 AND model_id = $2 LIMIT 1`, profileID, modelID).Scan(&existingID)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query cross-profile promotion target %q: %w", modelID, err)
	}
	return true, nil
}

func loadPromotionTargetTerminalStats(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, apiFamily string) (promotionTargetTerminalStats, error) {
	rows, err := exec.Query(ctx, `WITH RECURSIVE terminal_reachability AS (
		SELECT model_access_targets.target_model_config_id AS next_model_config_id,
			model_access_targets.target_connection_id AS terminal_connection_id,
			connections.context_window_tokens,
			connections.max_context_utilization,
			ARRAY[model_access_targets.source_model_config_id] || CASE WHEN model_access_targets.target_model_config_id IS NULL THEN ARRAY[]::integer[] ELSE ARRAY[model_access_targets.target_model_config_id] END AS path
		FROM model_access_targets
		LEFT JOIN model_configs AS target_models ON target_models.id = model_access_targets.target_model_config_id
		LEFT JOIN connections ON connections.id = model_access_targets.target_connection_id AND connections.profile_id = model_access_targets.profile_id
		WHERE model_access_targets.profile_id = $1
			AND model_access_targets.source_model_config_id = $2
			AND model_access_targets.is_enabled = TRUE
			AND (
				(model_access_targets.target_model_config_id IS NOT NULL
					AND target_models.profile_id = model_access_targets.profile_id
					AND target_models.is_enabled = TRUE
					AND target_models.facade_enabled = FALSE
					AND target_models.api_family = $3)
				OR
				(model_access_targets.target_connection_id IS NOT NULL
					AND connections.profile_id = model_access_targets.profile_id
					AND connections.api_family = $3)
			)
		UNION ALL
		SELECT child.target_model_config_id AS next_model_config_id,
			child.target_connection_id AS terminal_connection_id,
			connections.context_window_tokens,
			connections.max_context_utilization,
			terminal_reachability.path || CASE WHEN child.target_model_config_id IS NULL THEN ARRAY[]::integer[] ELSE ARRAY[child.target_model_config_id] END AS path
		FROM terminal_reachability
		JOIN model_access_targets AS child ON child.profile_id = $1 AND child.source_model_config_id = terminal_reachability.next_model_config_id
		LEFT JOIN model_configs AS target_models ON target_models.id = child.target_model_config_id
		LEFT JOIN connections ON connections.id = child.target_connection_id AND connections.profile_id = child.profile_id
		WHERE terminal_reachability.next_model_config_id IS NOT NULL
			AND child.is_enabled = TRUE
			AND (
				(child.target_model_config_id IS NOT NULL
					AND target_models.profile_id = child.profile_id
					AND target_models.is_enabled = TRUE
					AND target_models.facade_enabled = FALSE
					AND target_models.api_family = $3
					AND NOT child.target_model_config_id = ANY(terminal_reachability.path))
				OR
				(child.target_connection_id IS NOT NULL
					AND connections.profile_id = child.profile_id
					AND connections.api_family = $3)
			)
		)
		SELECT terminal_connection_id, context_window_tokens, max_context_utilization
		FROM terminal_reachability
		WHERE terminal_connection_id IS NOT NULL
		ORDER BY terminal_connection_id ASC`, profileID, modelConfigID, apiFamily)
	if err != nil {
		return promotionTargetTerminalStats{}, fmt.Errorf("query terminal promotion stats for model %d: %w", modelConfigID, err)
	}
	defer rows.Close()

	stats := promotionTargetTerminalStats{TerminalConnectionIDs: map[int]struct{}{}}
	for rows.Next() {
		var terminalConnectionID int
		var contextWindowTokens sql.NullInt32
		var maxContextUtilization sql.NullFloat64
		if err := rows.Scan(&terminalConnectionID, &contextWindowTokens, &maxContextUtilization); err != nil {
			return promotionTargetTerminalStats{}, fmt.Errorf("scan terminal promotion stats for model %d: %w", modelConfigID, err)
		}
		stats.TerminalConnectionIDs[terminalConnectionID] = struct{}{}
		usableContextWindowTokens := promotionTargetUsableContextWindowTokens(contextWindowTokens, maxContextUtilization)
		if usableContextWindowTokens > stats.LargestUsableContextWindowTokens {
			stats.LargestUsableContextWindowTokens = usableContextWindowTokens
		}
	}
	if err := rows.Err(); err != nil {
		return promotionTargetTerminalStats{}, fmt.Errorf("iterate terminal promotion stats for model %d: %w", modelConfigID, err)
	}
	return stats, nil
}

func promotionTargetUsableContextWindowTokens(contextWindowTokens sql.NullInt32, maxContextUtilization sql.NullFloat64) int {
	if !contextWindowTokens.Valid || contextWindowTokens.Int32 <= 0 {
		return 0
	}
	if !maxContextUtilization.Valid || maxContextUtilization.Float64 <= 0 || maxContextUtilization.Float64 > 1 {
		return 0
	}
	return int(math.Floor(float64(contextWindowTokens.Int32) * maxContextUtilization.Float64))
}

func routingPlanValidationIssueError(code string, path string, detail string) error {
	return routingPlanValidationError(http.StatusBadRequest, detail, []routingPlanValidationIssue{{
		Code:    strings.TrimSpace(code),
		Path:    strings.TrimSpace(path),
		Message: strings.TrimSpace(detail),
	}})
}

func routingPlanValidationError(statusCode int, detail string, issues []routingPlanValidationIssue) error {
	if len(issues) == 0 {
		return &domainError{StatusCode: statusCode, Detail: detail}
	}
	return &domainError{
		StatusCode: statusCode,
		Detail:     detail,
		Fields: map[string]any{
			"routing_plan_issues": issues,
		},
	}
}

func accessTargetIssuePath(index int, field string) string {
	path := fmt.Sprintf("access_targets[%d]", index)
	if strings.TrimSpace(field) == "" {
		return path
	}
	return path + "." + strings.TrimSpace(field)
}

func validatePublicAccessTargets(accessTargets []modelAccessTargetRequest) error {
	if err := validateAccessTargets(accessTargets); err != nil {
		return err
	}
	for _, accessTarget := range accessTargets {
		if err := validatePublicAccessTarget(accessTarget); err != nil {
			return err
		}
	}
	return nil
}

func validatePublicAccessTarget(accessTarget modelAccessTargetRequest) error {
	if targetcompat.IsTerminalTargetAccessTargetType(accessTarget.TargetType) || accessTarget.ConnectionID != nil {
		return connectionAccessTargetsManagedError()
	}
	return nil
}

func connectionAccessTargetsManagedError() error {
	return &domainError{StatusCode: http.StatusBadRequest, Detail: "terminal targets are managed through model-scoped connection routes"}
}

func validateAccessTargets(accessTargets []modelAccessTargetRequest) error {
	issues := modelrouting.ValidateAuthoredAccessTargets(modelRoutingTargetsFromRequests(accessTargets), modelRoutingValidationOptions())
	return modelRoutingIssuesError(issues)
}

func validateAccessTargetsForSourceModel(sourceModelID string, accessTargets []modelAccessTargetRequest) error {
	issues := modelrouting.ValidateSourceModelTargets(
		modelrouting.ModelNode{ModelID: strings.TrimSpace(sourceModelID)},
		modelRoutingTargetsFromRequests(accessTargets),
		modelRoutingValidationOptions(),
	)
	return modelRoutingIssuesError(issues)
}

func modelRoutingTargetsFromRequests(accessTargets []modelAccessTargetRequest) []modelrouting.AuthoredAccessTarget {
	items := make([]modelrouting.AuthoredAccessTarget, 0, len(accessTargets))
	for _, target := range accessTargets {
		items = append(items, modelRoutingTargetFromRequest(target))
	}
	return items
}

func modelRoutingTargetFromRequest(target modelAccessTargetRequest) modelrouting.AuthoredAccessTarget {
	return modelrouting.AuthoredAccessTarget{TargetType: target.TargetType, Position: target.Position, IsEnabled: target.IsEnabled, TargetModelID: target.TargetModelID, TerminalTargetID: target.ConnectionID, Weight: target.Weight, TargetPriority: target.TargetPriority}
}

func modelRoutingValidationOptions() modelrouting.ValidationOptions {
	return modelrouting.ValidationOptions{IssuePath: func(code string, field string, index int, target modelrouting.AuthoredAccessTarget) string {
		if code == "target_duplicate" {
			return accessTargetIssuePath(index, "")
		}
		return accessTargetIssuePath(index, field)
	}}
}

func modelRoutingIssuesError(issues []modelrouting.ValidationIssue) error {
	if len(issues) == 0 {
		return nil
	}
	return routingPlanValidationError(http.StatusBadRequest, issues[0].Message, modelRoutingValidationIssues(issues))
}

func modelRoutingValidationIssues(issues []modelrouting.ValidationIssue) []routingPlanValidationIssue {
	items := make([]routingPlanValidationIssue, 0, len(issues))
	for _, issue := range issues {
		items = append(items, routingPlanValidationIssue{Code: issue.Code, Path: issue.Path, Message: issue.Message})
	}
	return items
}

func resolvePersistedDisplayName(modelID string, displayName *string) *string {
	if displayName == nil {
		return stringPtr(modelID)
	}
	return displayName
}

func resolveIsEnabled(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

func resolveFacadeEnabled(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func isValidAPIFamily(value string) bool {
	return value == "openai" || value == "anthropic" || value == "gemini"
}

func joinModelIDs(records []modelRecord) string {
	modelIDs := make([]string, 0, len(records))
	for _, record := range records {
		modelIDs = append(modelIDs, record.ModelID)
	}
	return strings.Join(modelIDs, ", ")
}

func decodeJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var modelErr *domainError
	if errors.As(err, &modelErr) {
		writeErrorFields(w, r, corsSnapshot, modelErr.StatusCode, modelErr.Detail, modelErr.Fields)
		return
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail string) {
	writeErrorFields(w, r, corsSnapshot, statusCode, detail, nil)
}

func writeErrorFields(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail string, fields map[string]any) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
	if len(fields) == 0 {
		writeJSON(w, statusCode, map[string]string{"detail": detail})
		return
	}
	payload := map[string]any{"detail": detail}
	for key, value := range fields {
		payload[key] = value
	}
	writeJSON(w, statusCode, payload)
}

func routeInt(request *http.Request, name string) (int, error) {
	value := strings.TrimSpace(chi.URLParam(request, name))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}

func stringPtr(value string) *string {
	resolved := value
	return &resolved
}
