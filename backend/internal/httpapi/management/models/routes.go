package models

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

func (s *Service) handleListModels(w http.ResponseWriter, r *http.Request) {
	includeReadiness := false
	for _, value := range r.URL.Query()["include"] {
		if strings.TrimSpace(value) == "route_readiness" {
			includeReadiness = true
		}
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (any, error) {
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
		strategies, accessTargets, health, err := loadModelRelations(r.Context(), tx, profile.ID, records)
		if err != nil {
			return nil, err
		}
		response := make([]modelConfigListResponse, 0, len(records))
		summaries := map[int]modelrouting.RoutingSummary{}
		if err := attachRoutingSummaries(records, accessTargets, strategies, summaries); err != nil {
			return nil, err
		}
		readinessSummaries := map[int]modelrouting.ModelRouteReadinessSummary{}
		if includeReadiness {
			var readiness modelrouting.ProfileRouteReadiness
			readiness, readinessSummaries, err = analyzeProfileRouteReadiness(r.Context(), tx, profile.ID, records)
			if err != nil {
				return nil, err
			}
			for _, record := range records {
				response = append(response, buildModelListResponse(record, strategies, accessTargets, counts, health, s.now().UTC()))
			}
			for index := range response {
				if summary, ok := readinessSummaries[response[index].ID]; ok {
					summary := summary
					response[index].RouteReadiness = &summary
				}
				if summary, ok := summaries[response[index].ID]; ok {
					summary := summary
					response[index].RoutingSummary = &summary
				}
			}
			return modelListReadinessEnvelope{Items: response, RouteReadiness: readiness}, nil
		}
		for _, record := range records {
			item := buildModelListResponse(record, strategies, accessTargets, counts, health, s.now().UTC())
			if summary, ok := summaries[record.ID]; ok {
				summary := summary
				item.RoutingSummary = &summary
			}
			response = append(response, item)
		}
		return response, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetModel(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
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
		strategies, accessTargets, _, err := loadModelRelations(r.Context(), tx, profile.ID, []modelRecord{record})
		if err != nil {
			return modelConfigResponse{}, err
		}
		return buildModelDetailResponse(record, strategies, accessTargets, s.now().UTC()), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	requestBody, err := decodeModelCreateRequest(r)
	if err != nil {
		writeDecodeError(w, r, s.corsSnapshot(), err)
		return
	}
	normalizeCreateRequest(&requestBody)
	if err := validateCreateRequest(requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if err := validateInitialTerminalTargetShape(requestBody.InitialTerminalTarget); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	hasInitialTarget := requestBody.InitialTerminalTarget != nil
	modelEnabled := resolveCompositeModelEnabled(requestBody.IsEnabled, hasInitialTarget)
	if !hasInitialTarget && requestBody.IsEnabled != nil && *requestBody.IsEnabled {
		writeDomainError(w, r, s.corsSnapshot(), routingPlanValidationIssueError("model_no_enabled_targets", "access_targets", "enabled models must include at least one enabled access target"))
		return
	}
	if hasInitialTarget && modelEnabled && !resolvedBool(requestBody.InitialTerminalTarget.IsActive, true) {
		writeDomainError(w, r, s.corsSnapshot(), routingPlanValidationIssueErrorWithStatus(http.StatusUnprocessableEntity, "model_initial_target_inactive", "initial_terminal_target.is_active", "enabled models cannot be created with an inactive first terminal target"))
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (modelMutationEnvelope, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return modelMutationEnvelope{}, err
		}
		if err := ensureModelIDAvailable(r.Context(), tx, profile.ID, requestBody.ModelID, nil); err != nil {
			return modelMutationEnvelope{}, err
		}
		if err := ensureLoadbalanceStrategyExists(r.Context(), tx, profile.ID, *requestBody.LoadbalanceStrategyID); err != nil {
			return modelMutationEnvelope{}, err
		}
		// Profile costing first lock: pricing-bearing composite creates take
		// the profile row lock before any model/connection rows (the Pricing
		// pair's dedicated costing-settings row is out of scope for this
		// delivery; the profile lock is the closest shared first lock).
		if hasInitialTarget {
			if err := lockProfileRowForModel(r.Context(), tx, profile.ID); err != nil {
				return modelMutationEnvelope{}, err
			}
		}
		now := s.nowUTC()
		record := modelRecord{ProfileID: profile.ID, APIFamily: requestBody.APIFamily, ModelID: requestBody.ModelID, DisplayName: resolvePersistedDisplayName(requestBody.ModelID, requestBody.DisplayName), LoadbalanceStrategyID: requestBody.LoadbalanceStrategyID, OpenAIAcceptedFormat: requestBody.OpenAIAcceptedFormat.Value, OpenAIImageOperations: requestBody.OpenAIImageOperations.Value, IsEnabled: modelEnabled, CreatedAt: now, UpdatedAt: now}
		created, err := insertModel(r.Context(), tx, record)
		if err != nil {
			return modelMutationEnvelope{}, err
		}
		var connectionResult *connections.OwnerConnectionCreateResult
		if hasInitialTarget {
			connectionResult, err = s.createCompositeConnection(r.Context(), tx, profile.ID, created, requestBody.InitialTerminalTarget)
			if err != nil {
				return modelMutationEnvelope{}, err
			}
		}
		strategies, accessTargets, _, err := loadModelRelations(r.Context(), tx, profile.ID, []modelRecord{created})
		if err != nil {
			return modelMutationEnvelope{}, err
		}
		detail := buildModelDetailResponse(created, strategies, accessTargets, s.now().UTC())
		warnings, err := modelMutationWarnings(r.Context(), tx, profile.ID, created.ID)
		if err != nil {
			return modelMutationEnvelope{}, err
		}
		envelope := modelMutationEnvelope{
			Model:                 &detail,
			ConfigurationWarnings: warnings,
		}
		if connectionResult != nil {
			envelope.Connection = compositeConnectionEnvelope(connectionResult)
		}
		return envelope, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateModel(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	requestBody, err := decodeModelUpdateRequest(r)
	if err != nil {
		writeDecodeError(w, r, s.corsSnapshot(), err)
		return
	}
	normalizeUpdateRequest(&requestBody)
	if err := validateUpdateRequest(requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (modelMutationEnvelope, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return modelMutationEnvelope{}, err
		}
		current, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, true)
		if err != nil {
			return modelMutationEnvelope{}, err
		}
		if !found {
			return modelMutationEnvelope{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		if err := lockProfileAccessTargetRows(r.Context(), tx, profile.ID); err != nil {
			return modelMutationEnvelope{}, err
		}
		currentAccessTargetsByModel, err := loadAccessTargetsForModels(r.Context(), tx, profile.ID, []int{current.ID})
		if err != nil {
			return modelMutationEnvelope{}, err
		}
		next := current
		if requestBody.APIFamily.Set {
			next.APIFamily = *requestBody.APIFamily.Value
		}
		if requestBody.ModelID.Set {
			next.ModelID = *requestBody.ModelID.Value
		}
		if requestBody.DisplayName.Set {
			next.DisplayName = resolvePersistedDisplayName(next.ModelID, requestBody.DisplayName.Value)
		}
		if requestBody.OpenAIAcceptedFormat.Set {
			next.OpenAIAcceptedFormat = requestBody.OpenAIAcceptedFormat.Value
		} else if next.APIFamily != "openai" {
			next.OpenAIAcceptedFormat = nil
		}
		if requestBody.OpenAIImageOperations.Set {
			next.OpenAIImageOperations = requestBody.OpenAIImageOperations.Value
		} else if next.APIFamily != "openai" {
			next.OpenAIImageOperations = nil
		}
		if requestBody.IsEnabled.Set {
			next.IsEnabled = requestBody.IsEnabled.Value
		}
		if requestBody.LoadbalanceStrategyID.Set {
			next.LoadbalanceStrategyID = requestBody.LoadbalanceStrategyID.Value
		}
		if requestBody.APIFamily.Set && next.APIFamily != current.APIFamily && hasConnectionAccessTargetRecords(currentAccessTargetsByModel[current.ID]) {
			return modelMutationEnvelope{}, &domainError{StatusCode: http.StatusConflict, Detail: "Cannot change api_family while private connections exist"}
		}
		if requestBody.OpenAIAcceptedFormat.Set && providerauth.IsOpenAI(next.APIFamily) && !providerauth.OpenAITextModesMatch(next.OpenAIAcceptedFormat, current.OpenAIAcceptedFormat) {
			if err := ensureOpenAIAcceptedFormatChangeAllowed(r.Context(), tx, profile.ID, current.ID, currentAccessTargetsByModel[current.ID], next.OpenAIAcceptedFormat); err != nil {
				return modelMutationEnvelope{}, err
			}
		}
		if requestBody.ModelID.Set && next.ModelID != current.ModelID {
			if err := ensureModelIDAvailable(r.Context(), tx, profile.ID, next.ModelID, &current.ID); err != nil {
				return modelMutationEnvelope{}, err
			}
		}
		if next.LoadbalanceStrategyID == nil {
			return modelMutationEnvelope{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "loadbalance_strategy_id is required"}
		}
		if err := validateOpenAIDimensionsForModel(next.APIFamily, next.OpenAIAcceptedFormat, requestBody.OpenAIAcceptedFormat.Set, next.OpenAIImageOperations, requestBody.OpenAIImageOperations.Set); err != nil {
			return modelMutationEnvelope{}, err
		}
		if err := ensureLoadbalanceStrategyExists(r.Context(), tx, profile.ID, *next.LoadbalanceStrategyID); err != nil {
			return modelMutationEnvelope{}, err
		}
		currentAccessTargets := currentAccessTargetsByModel[current.ID]
		preservedConnectionTargets := preservedConnectionTargetsFromRecords(currentAccessTargets)
		targetInputs := modelAccessTargetRequestsFromRecords(currentAccessTargets)
		if err := validateAccessTargetsForSourceModel(next.ModelID, targetInputs); err != nil {
			return modelMutationEnvelope{}, err
		}
		resolvedTargets, err := resolveAccessTargets(r.Context(), tx, profile.ID, &current.ID, next.ModelID, next.APIFamily, next.OpenAIAcceptedFormat, next.OpenAIImageOperations, targetInputs)
		if err != nil {
			return modelMutationEnvelope{}, err
		}
		if next.IsEnabled && !hasEnabledResolvedOrPreservedAccessTarget(resolvedTargets, preservedConnectionTargets) {
			return modelMutationEnvelope{}, routingPlanValidationIssueError("model_no_enabled_targets", "access_targets", "enabled models must include at least one enabled access target")
		}
		if err := ensureAccessTargetGraphAcyclic(r.Context(), tx, profile.ID, current.ID, resolvedTargets); err != nil {
			return modelMutationEnvelope{}, err
		}
		next.UpdatedAt = s.nowUTC()
		updated, err := updateModel(r.Context(), tx, next)
		if err != nil {
			return modelMutationEnvelope{}, err
		}
		strategies, accessTargets, _, err := loadModelRelations(r.Context(), tx, profile.ID, []modelRecord{updated})
		if err != nil {
			return modelMutationEnvelope{}, err
		}
		warnings, err := modelMutationWarnings(r.Context(), tx, profile.ID, updated.ID)
		if err != nil {
			return modelMutationEnvelope{}, err
		}
		detail := buildModelDetailResponse(updated, strategies, accessTargets, s.now().UTC())
		return modelMutationEnvelope{Model: &detail, ConfigurationWarnings: warnings}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
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
	responseutil.WriteJSON(w, http.StatusOK, response)
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
		if !modelrouting.IsTerminalTargetType(record.TargetType) {
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

func hasConnectionAccessTargetRecords(records []accessTargetRecord) bool {
	for _, record := range records {
		if modelrouting.IsTerminalTargetType(record.TargetType) {
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

func hasEnabledResolvedAccessTarget(targets []resolvedAccessTarget) bool {
	for _, target := range targets {
		if target.IsEnabled {
			return true
		}
	}
	return false
}

func accessTargetInputKey(value modelAccessTargetRequest) string {
	if value.TargetType == "model" && value.TargetModelID != nil {
		return "model:" + strings.TrimSpace(*value.TargetModelID)
	}
	if value.TargetType == "connection" && value.ConnectionID != nil {
		return fmt.Sprintf("connection:%d", *value.ConnectionID)
	}
	return value.TargetType
}
