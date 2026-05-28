package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

type endpointModelsBatchRequest struct {
	EndpointIDs []int `json:"endpoint_ids"`
}

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
		now := s.nowUTC()
		record := modelRecord{ProfileID: profile.ID, VendorID: requestBody.VendorID, APIFamily: requestBody.APIFamily, ModelID: requestBody.ModelID, DisplayName: resolvePersistedDisplayName(requestBody.ModelID, requestBody.DisplayName), LoadbalanceStrategyID: requestBody.LoadbalanceStrategyID, IsEnabled: resolveIsEnabled(requestBody.IsEnabled), CreatedAt: now, UpdatedAt: now}
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
		if created.IsEnabled && !hasEnabledResolvedAccessTarget(resolvedTargets) {
			return modelConfigResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "enabled models must include at least one enabled access target"}
		}
		if err := ensureAccessTargetGraphAcyclic(r.Context(), tx, profile.ID, created.ID, resolvedTargets); err != nil {
			return modelConfigResponse{}, err
		}
		if err := replaceAccessTargets(r.Context(), tx, profile.ID, created.ID, resolvedTargets, now); err != nil {
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
		if requestBody.IsEnabled.Set {
			next.IsEnabled = requestBody.IsEnabled.Value
		}
		if requestBody.LoadbalanceStrategyID.Set {
			next.LoadbalanceStrategyID = requestBody.LoadbalanceStrategyID.Value
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
		targetInputs := accessTargetRequestsFromRecords(currentAccessTargetsByModel[current.ID])
		if requestBody.AccessTargets.Set {
			targetInputs = requestBody.AccessTargets.Value
		}
		if err := validateAccessTargetsForSourceModel(next.ModelID, targetInputs); err != nil {
			return modelConfigResponse{}, err
		}
		resolvedTargets, err := resolveAccessTargets(r.Context(), tx, profile.ID, &current.ID, next.ModelID, next.APIFamily, targetInputs)
		if err != nil {
			return modelConfigResponse{}, err
		}
		if next.IsEnabled && !hasEnabledResolvedAccessTarget(resolvedTargets) {
			return modelConfigResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "enabled models must include at least one enabled access target"}
		}
		if err := ensureAccessTargetGraphAcyclic(r.Context(), tx, profile.ID, current.ID, resolvedTargets); err != nil {
			return modelConfigResponse{}, err
		}
		next.UpdatedAt = s.nowUTC()
		updated, err := updateModel(r.Context(), tx, next)
		if err != nil {
			return modelConfigResponse{}, err
		}
		if err := replaceAccessTargets(r.Context(), tx, profile.ID, updated.ID, resolvedTargets, next.UpdatedAt); err != nil {
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
		if err := deleteSourceAccessTargets(r.Context(), tx, record.ID); err != nil {
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
		return s.replaceModelTargetsFromMutationItems(r.Context(), tx, profile.ID, model, items)
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
	if err := validateAccessTargetsForSourceModel(model.ModelID, requests); err != nil {
		return nil, err
	}
	resolvedTargets, err := resolveAccessTargets(ctx, tx, profileID, &model.ID, model.ModelID, model.APIFamily, requests)
	if err != nil {
		return nil, err
	}
	if model.IsEnabled && !hasEnabledResolvedAccessTarget(resolvedTargets) {
		return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "enabled models must include at least one enabled access target"}
	}
	if err := ensureAccessTargetGraphAcyclic(ctx, tx, profileID, model.ID, resolvedTargets); err != nil {
		return nil, err
	}
	now := s.nowUTC()
	if err := replaceAccessTargets(ctx, tx, profileID, model.ID, resolvedTargets, now); err != nil {
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
	return modelAccessTargetRequest{TargetType: strings.ToLower(strings.TrimSpace(input.TargetType)), TargetModelID: normalizeOptionalString(input.TargetModelID, false, false), ConnectionID: copyIntPtr(input.ConnectionID), Position: position, IsEnabled: input.IsEnabled}, nil
}

func accessTargetRequestFromRecord(record accessTargetRecord) modelAccessTargetRequest {
	enabled := record.IsEnabled
	request := modelAccessTargetRequest{TargetType: record.TargetType, Position: record.Position, IsEnabled: &enabled}
	if record.TargetType == "model" && record.TargetModel != nil {
		request.TargetModelID = stringPtr(record.TargetModel.ModelID)
	}
	if record.TargetType == "connection" {
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
	updated := item.Request
	if input.TargetType.Set {
		if input.TargetType.Value == nil {
			updated.TargetType = ""
		} else {
			updated.TargetType = strings.ToLower(strings.TrimSpace(*input.TargetType.Value))
		}
		if updated.TargetType == "model" {
			updated.ConnectionID = nil
		}
		if updated.TargetType == "connection" {
			updated.TargetModelID = nil
		}
	}
	if input.TargetModelID.Set {
		updated.TargetModelID = normalizeOptionalString(input.TargetModelID.Value, false, false)
		if !input.TargetType.Set && updated.TargetModelID != nil && strings.TrimSpace(*updated.TargetModelID) != "" {
			updated.TargetType = "model"
			updated.ConnectionID = nil
		}
	}
	if input.ConnectionID.Set {
		updated.ConnectionID = copyIntPtr(input.ConnectionID.Value)
		if !input.TargetType.Set && updated.ConnectionID != nil {
			updated.TargetType = "connection"
			updated.TargetModelID = nil
		}
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
	requestBody.AccessTargets = normalizeAccessTargets(requestBody.AccessTargets)
}

func normalizeUpdateRequest(requestBody *modelUpdateRequest) {
	requestBody.APIFamily = optionalString{Set: requestBody.APIFamily.Set, Value: normalizeOptionalString(requestBody.APIFamily.Value, true, false)}
	requestBody.ModelID = optionalString{Set: requestBody.ModelID.Set, Value: normalizeOptionalString(requestBody.ModelID.Value, false, false)}
	requestBody.DisplayName = optionalString{Set: requestBody.DisplayName.Set, Value: normalizeOptionalString(requestBody.DisplayName.Value, false, true)}
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
		normalizedTarget.TargetType = strings.ToLower(strings.TrimSpace(value.TargetType))
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
	if err := validateAccessTargets(requestBody.AccessTargets); err != nil {
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
	if requestBody.AccessTargets.Set {
		return validateAccessTargets(requestBody.AccessTargets.Value)
	}
	return nil
}

func validateAccessTargets(accessTargets []modelAccessTargetRequest) error {
	seenTargets := map[string]struct{}{}
	seenPositions := map[int]struct{}{}
	for _, accessTarget := range accessTargets {
		if accessTarget.TargetType != "model" && accessTarget.TargetType != "connection" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "target_type must be 'model' or 'connection'"}
		}
		if accessTarget.Position < 0 {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "position must be greater than or equal to 0"}
		}
		if _, ok := seenPositions[accessTarget.Position]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "access_targets must contain unique position values"}
		}
		seenPositions[accessTarget.Position] = struct{}{}
		targetKey, err := validateAccessTargetPointerContract(accessTarget)
		if err != nil {
			return err
		}
		if _, ok := seenTargets[targetKey]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "access_targets must contain unique target references"}
		}
		seenTargets[targetKey] = struct{}{}
	}
	for expectedPosition := 0; expectedPosition < len(accessTargets); expectedPosition++ {
		if _, ok := seenPositions[expectedPosition]; !ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "access_targets positions must be contiguous starting at 0"}
		}
	}
	return nil
}

func validateAccessTargetsForSourceModel(sourceModelID string, accessTargets []modelAccessTargetRequest) error {
	sourceModelID = strings.TrimSpace(sourceModelID)
	if sourceModelID == "" {
		return nil
	}
	for _, accessTarget := range accessTargets {
		if accessTarget.TargetType != "model" || accessTarget.TargetModelID == nil {
			continue
		}
		if strings.TrimSpace(*accessTarget.TargetModelID) == sourceModelID {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "Model access target cannot target itself"}
		}
	}
	return nil
}

func validateAccessTargetPointerContract(accessTarget modelAccessTargetRequest) (string, error) {
	if accessTarget.TargetType == "model" {
		if accessTarget.TargetModelID == nil || strings.TrimSpace(*accessTarget.TargetModelID) == "" {
			return "", &domainError{StatusCode: http.StatusBadRequest, Detail: "target_model_id is required for model access targets"}
		}
		if accessTarget.ConnectionID != nil {
			return "", &domainError{StatusCode: http.StatusBadRequest, Detail: "connection_id must be omitted for model access targets"}
		}
		return "model:" + strings.TrimSpace(*accessTarget.TargetModelID), nil
	}
	if accessTarget.ConnectionID == nil || *accessTarget.ConnectionID <= 0 {
		return "", &domainError{StatusCode: http.StatusBadRequest, Detail: "connection_id is required for connection access targets"}
	}
	if accessTarget.TargetModelID != nil && strings.TrimSpace(*accessTarget.TargetModelID) != "" {
		return "", &domainError{StatusCode: http.StatusBadRequest, Detail: "target_model_id must be omitted for connection access targets"}
	}
	return fmt.Sprintf("connection:%d", *accessTarget.ConnectionID), nil
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
		writeError(w, r, corsSnapshot, modelErr.StatusCode, modelErr.Detail)
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
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
	writeJSON(w, statusCode, map[string]string{"detail": detail})
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
