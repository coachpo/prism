package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		vendors, strategies, proxyTargets, health, err := loadModelRelations(r.Context(), tx, profile.ID, records)
		if err != nil {
			return nil, err
		}
		response := make([]modelConfigListResponse, 0, len(records))
		for _, record := range records {
			response = append(response, buildModelListResponse(record, vendors, strategies, proxyTargets, counts, health))
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
		vendors, strategies, proxyTargets, _, err := loadModelRelations(r.Context(), tx, profile.ID, []modelRecord{record})
		if err != nil {
			return modelConfigResponse{}, err
		}
		connections, err := loadConnectionsForModel(r.Context(), tx, profile.ID, record.ID)
		if err != nil {
			return modelConfigResponse{}, err
		}
		return buildModelDetailResponse(record, vendors, strategies, proxyTargets, connections), nil
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
		modelType := resolvedModelType(requestBody.ModelType)
		if modelType == "native" {
			if err := ensureLoadbalanceStrategyExists(r.Context(), tx, profile.ID, *requestBody.LoadbalanceStrategyID); err != nil {
				return modelConfigResponse{}, err
			}
		}
		targetModels, err := resolveProxyTargets(r.Context(), tx, profile.ID, modelType, requestBody.ProxyTargets, requestBody.APIFamily, stringPtr(requestBody.ModelID))
		if err != nil {
			return modelConfigResponse{}, err
		}
		now := s.nowUTC()
		record := modelRecord{ProfileID: profile.ID, VendorID: requestBody.VendorID, APIFamily: requestBody.APIFamily, ModelID: requestBody.ModelID, DisplayName: resolvePersistedDisplayName(requestBody.ModelID, requestBody.DisplayName), ModelType: modelType, LoadbalanceStrategyID: requestBody.LoadbalanceStrategyID, IsEnabled: resolveIsEnabled(requestBody.IsEnabled), CreatedAt: now, UpdatedAt: now}
		created, err := insertModel(r.Context(), tx, record)
		if err != nil {
			return modelConfigResponse{}, err
		}
		if created.ModelType == "proxy" {
			if err := replaceProxyTargets(r.Context(), tx, created.ID, created.ModelType, targetModels, requestBody.ProxyTargets); err != nil {
				return modelConfigResponse{}, err
			}
		}
		vendors, strategies, proxyTargets, _, err := loadModelRelations(r.Context(), tx, profile.ID, []modelRecord{created})
		if err != nil {
			return modelConfigResponse{}, err
		}
		return buildModelDetailResponse(created, vendors, strategies, proxyTargets, []connectionResponse{}), nil
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
		if requestBody.VendorID.Set && requestBody.VendorID.Value != nil {
			if err := ensureVendorExists(r.Context(), tx, *requestBody.VendorID.Value); err != nil {
				return modelConfigResponse{}, err
			}
		}
		connectionsBefore, err := loadConnectionsForModel(r.Context(), tx, profile.ID, current.ID)
		if err != nil {
			return modelConfigResponse{}, err
		}
		currentProxyTargets, err := loadProxyTargetsForModels(r.Context(), tx, []int{current.ID})
		if err != nil {
			return modelConfigResponse{}, err
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
		if requestBody.ModelType.Set {
			next.ModelType = *requestBody.ModelType.Value
		}
		if requestBody.IsEnabled.Set {
			next.IsEnabled = requestBody.IsEnabled.Value
		}
		if requestBody.LoadbalanceStrategyID.Set {
			next.LoadbalanceStrategyID = requestBody.LoadbalanceStrategyID.Value
		}
		newProxyTargets := cloneProxyTargets(currentProxyTargets[current.ID])
		if requestBody.ProxyTargets.Set {
			newProxyTargets = cloneProxyTargets(requestBody.ProxyTargets.Value)
		}
		if requestBody.ModelID.Set && next.ModelID != current.ModelID {
			if err := ensureModelIDAvailable(r.Context(), tx, profile.ID, next.ModelID, &current.ID); err != nil {
				return modelConfigResponse{}, err
			}
		}
		referrers, err := listProxyReferrers(r.Context(), tx, profile.ID, current.ModelID, &current.ID)
		if err != nil {
			return modelConfigResponse{}, err
		}
		if current.ModelType == "native" && len(referrers) > 0 {
			if next.ModelType != "native" {
				return modelConfigResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Cannot convert native model to proxy while proxy models [%s] point to it", joinModelIDs(referrers))}
			}
			if next.APIFamily != current.APIFamily {
				return modelConfigResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Cannot change api_family for native model while proxy models [%s] point to it", joinModelIDs(referrers))}
			}
		}
		if next.ModelType == "proxy" && current.ModelType != "proxy" && len(connectionsBefore) > 0 {
			return modelConfigResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Cannot convert native model with connections to proxy. Delete connections first."}
		}
		if next.ModelType == "proxy" && !requestBody.ProxyTargets.Set {
			return modelConfigResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "proxy_targets is required for proxy models"}
		}
		targetModels, err := resolveProxyTargets(r.Context(), tx, profile.ID, next.ModelType, newProxyTargets, next.APIFamily, stringPtr(next.ModelID))
		if err != nil {
			return modelConfigResponse{}, err
		}
		if next.ModelType == "proxy" {
			if requestBody.LoadbalanceStrategyID.Set && requestBody.LoadbalanceStrategyID.Value != nil {
				return modelConfigResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "loadbalance_strategy_id must be null for proxy models"}
			}
			next.LoadbalanceStrategyID = nil
		}
		if err := validateModelTypeAndTargets(next.ModelType, newProxyTargets, next.LoadbalanceStrategyID); err != nil {
			return modelConfigResponse{}, err
		}
		if next.ModelType == "native" {
			if err := ensureLoadbalanceStrategyExists(r.Context(), tx, profile.ID, *next.LoadbalanceStrategyID); err != nil {
				return modelConfigResponse{}, err
			}
		}
		next.UpdatedAt = s.nowUTC()
		updated, err := updateModel(r.Context(), tx, next)
		if err != nil {
			return modelConfigResponse{}, err
		}
		if err := replaceProxyTargets(r.Context(), tx, updated.ID, next.ModelType, targetModels, newProxyTargets); err != nil {
			return modelConfigResponse{}, err
		}
		if updated.ModelID != originalModelID {
			if err := syncRenamedModelReferences(r.Context(), tx, profile.ID, originalModelID, updated.ModelID); err != nil {
				return modelConfigResponse{}, err
			}
		}
		vendors, strategies, proxyTargets, _, err := loadModelRelations(r.Context(), tx, profile.ID, []modelRecord{updated})
		if err != nil {
			return modelConfigResponse{}, err
		}
		connections, err := loadConnectionsForModel(r.Context(), tx, profile.ID, updated.ID)
		if err != nil {
			return modelConfigResponse{}, err
		}
		return buildModelDetailResponse(updated, vendors, strategies, proxyTargets, connections), nil
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
		if record.ModelType == "native" {
			referrers, err := listProxyReferrers(r.Context(), tx, profile.ID, record.ModelID, nil)
			if err != nil {
				return deletedResponse{}, err
			}
			if len(referrers) > 0 {
				return deletedResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Cannot delete: proxy models [%s] point to this model", joinModelIDs(referrers))}
			}
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
		vendors, strategies, proxyTargets, health, err := loadModelRelations(r.Context(), tx, profile.ID, records)
		if err != nil {
			return nil, err
		}
		response := make([]modelConfigListResponse, 0, len(records))
		for _, record := range records {
			response = append(response, buildModelListResponse(record, vendors, strategies, proxyTargets, counts, health))
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
		vendors, strategies, proxyTargets, health, err := loadModelRelations(r.Context(), tx, profile.ID, allRecords)
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
				models = append(models, buildModelListResponse(record, vendors, strategies, proxyTargets, byEndpointCounts[endpointID], health))
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

func loadModelRelations(ctx context.Context, tx pgx.Tx, profileID int, records []modelRecord) (map[int]vendorRecord, map[int]strategyRecord, map[int][]proxyTargetReference, map[string]modelHealthStats, error) {
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
	proxyTargets, err := loadProxyTargetsForModels(ctx, tx, modelIDs)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	health, err := listModelHealthStats(ctx, tx, profileID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return vendors, strategies, proxyTargets, health, nil
}

func collectEndpointModelCounts(rows []endpointModelConnectionRow) ([]modelRecord, map[int]modelConnectionCounts) {
	recordsByID := map[int]modelRecord{}
	counts := map[int]modelConnectionCounts{}
	for _, row := range rows {
		recordsByID[row.ConnectionModelID] = row.ConnectionModelData
		count := counts[row.ConnectionModelID]
		count.Total++
		if row.ConnectionIsActive {
			count.Active++
		}
		counts[row.ConnectionModelID] = count
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
	for _, row := range rows {
		allRecordsByID[row.ConnectionModelID] = row.ConnectionModelData
		if _, ok := byEndpointCounts[row.EndpointID]; !ok {
			byEndpointCounts[row.EndpointID] = map[int]modelConnectionCounts{}
		}
		count := byEndpointCounts[row.EndpointID][row.ConnectionModelID]
		count.Total++
		if row.ConnectionIsActive {
			count.Active++
		}
		byEndpointCounts[row.EndpointID][row.ConnectionModelID] = count
		if _, ok := seenByEndpoint[row.EndpointID]; !ok {
			seenByEndpoint[row.EndpointID] = map[int]struct{}{}
		}
		if _, seen := seenByEndpoint[row.EndpointID][row.ConnectionModelID]; !seen {
			byEndpointRecords[row.EndpointID] = append(byEndpointRecords[row.EndpointID], row.ConnectionModelData)
			seenByEndpoint[row.EndpointID][row.ConnectionModelID] = struct{}{}
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
	requestBody.ModelType = strings.ToLower(strings.TrimSpace(requestBody.ModelType))
	requestBody.DisplayName = normalizeOptionalString(requestBody.DisplayName, false, true)
	requestBody.ProxyTargets = normalizeProxyTargets(requestBody.ProxyTargets)
}

func normalizeUpdateRequest(requestBody *modelUpdateRequest) {
	requestBody.APIFamily = optionalString{Set: requestBody.APIFamily.Set, Value: normalizeOptionalString(requestBody.APIFamily.Value, true, false)}
	requestBody.ModelType = optionalString{Set: requestBody.ModelType.Set, Value: normalizeOptionalString(requestBody.ModelType.Value, true, false)}
	requestBody.DisplayName = optionalString{Set: requestBody.DisplayName.Set, Value: normalizeOptionalString(requestBody.DisplayName.Value, false, true)}
	requestBody.ProxyTargets = optionalProxyTargets{Set: requestBody.ProxyTargets.Set, Value: normalizeProxyTargets(requestBody.ProxyTargets.Value)}
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

func normalizeProxyTargets(values []proxyTargetReference) []proxyTargetReference {
	if values == nil {
		return nil
	}
	normalized := make([]proxyTargetReference, 0, len(values))
	for _, value := range values {
		normalized = append(normalized, proxyTargetReference{TargetModelID: strings.TrimSpace(value.TargetModelID), Position: value.Position})
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
	if err := validateModelTypeAndTargets(resolvedModelType(requestBody.ModelType), requestBody.ProxyTargets, requestBody.LoadbalanceStrategyID); err != nil {
		return err
	}
	return nil
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
	if requestBody.ModelType.Set {
		if requestBody.ModelType.Value == nil || !isValidModelType(*requestBody.ModelType.Value) {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "model_type must be 'native' or 'proxy'"}
		}
	}
	if requestBody.ProxyTargets.Set {
		if err := validateProxyTargets(requestBody.ProxyTargets.Value); err != nil {
			return err
		}
	}
	if requestBody.ModelType.Set && requestBody.ModelType.Value != nil {
		if *requestBody.ModelType.Value == "native" && requestBody.ProxyTargets.Set && len(requestBody.ProxyTargets.Value) > 0 {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "proxy_targets must be empty for native models"}
		}
		if *requestBody.ModelType.Value == "proxy" && requestBody.ProxyTargets.Set {
			if err := validateProxyTargetContract(*requestBody.ModelType.Value, requestBody.ProxyTargets.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateModelTypeAndTargets(modelType string, proxyTargets []proxyTargetReference, loadbalanceStrategyID *int) error {
	if !isValidModelType(modelType) {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "model_type must be 'native' or 'proxy'"}
	}
	if err := validateProxyTargetContract(modelType, proxyTargets); err != nil {
		return err
	}
	if modelType == "native" {
		if loadbalanceStrategyID == nil {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "loadbalance_strategy_id is required for native models"}
		}
		return nil
	}
	if loadbalanceStrategyID != nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "loadbalance_strategy_id must be null for proxy models"}
	}
	return nil
}

func validateProxyTargetContract(modelType string, proxyTargets []proxyTargetReference) error {
	if modelType == "native" {
		if len(proxyTargets) > 0 {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "proxy_targets must be empty for native models"}
		}
		return nil
	}
	if len(proxyTargets) == 0 {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "proxy_targets is required for proxy models"}
	}
	return validateProxyTargets(proxyTargets)
}

func validateProxyTargets(proxyTargets []proxyTargetReference) error {
	seenTargets := map[string]struct{}{}
	seenPositions := map[int]struct{}{}
	for _, proxyTarget := range proxyTargets {
		targetModelID := strings.TrimSpace(proxyTarget.TargetModelID)
		if targetModelID == "" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "target_model_id must not be empty"}
		}
		if proxyTarget.Position < 0 {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "position must be greater than or equal to 0"}
		}
		if _, ok := seenTargets[targetModelID]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "proxy_targets must contain unique target_model_id values"}
		}
		if _, ok := seenPositions[proxyTarget.Position]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "proxy_targets must contain unique position values"}
		}
		seenTargets[targetModelID] = struct{}{}
		seenPositions[proxyTarget.Position] = struct{}{}
	}
	for expectedPosition := 0; expectedPosition < len(proxyTargets); expectedPosition++ {
		if _, ok := seenPositions[expectedPosition]; !ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "proxy_targets positions must be contiguous starting at 0"}
		}
	}
	return nil
}

func resolvedModelType(value string) string {
	if strings.TrimSpace(value) == "" {
		return "native"
	}
	return value
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

func isValidModelType(value string) bool {
	return value == "native" || value == "proxy"
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
	return json.NewDecoder(request.Body).Decode(target)
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
