package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

const defaultOpenAIProbeEndpointVariant = "responses_minimal"

func (s *Service) handleListConnectionsBatch(w http.ResponseWriter, r *http.Request) {
	var requestBody modelConnectionsBatchRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	normalizedModelIDs := dedupeIntValues(requestBody.ModelConfigIDs)
	if len(normalizedModelIDs) == 0 {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "model_config_ids must contain at least one model config id")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (modelConnectionsBatchResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return modelConnectionsBatchResponse{}, err
		}
		if err := ensureModelConfigIDsExist(r.Context(), tx, profile.ID, normalizedModelIDs); err != nil {
			return modelConnectionsBatchResponse{}, err
		}
		connectionsByModel, err := listConnectionsByModelIDs(r.Context(), tx, profile.ID, normalizedModelIDs)
		if err != nil {
			return modelConnectionsBatchResponse{}, err
		}
		items := make([]modelConnectionsBatchItem, 0, len(normalizedModelIDs))
		for _, modelConfigID := range normalizedModelIDs {
			connections := connectionsByModel[modelConfigID]
			if connections == nil {
				connections = []connectionResponse{}
			}
			items = append(items, modelConnectionsBatchItem{ModelConfigID: modelConfigID, Connections: connections})
		}
		return modelConnectionsBatchResponse{Items: items}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleListConnections(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) ([]connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		return listConnections(r.Context(), tx, profile.ID)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetConnection(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionResponse{}, err
		}
		connection, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, false)
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		return connection, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	var requestBody connectionCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if requestBody.Priority.Set {
		writeError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "priority is not allowed on create")
		return
	}
	apiFamily, err := validateAPIFamily(requestBody.APIFamily, true)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionResponse{}, err
		}
		if requestBody.EndpointID != nil && requestBody.EndpointCreate != nil {
			return connectionResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
		}
		if requestBody.EndpointID == nil && requestBody.EndpointCreate == nil {
			return connectionResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
		}
		authType, err := validateAuthType(requestBody.AuthType)
		if err != nil {
			return connectionResponse{}, err
		}
		if err := validateLimiter("qps_limit", requestBody.QPSLimit); err != nil {
			return connectionResponse{}, err
		}
		if err := validateLimiter("max_in_flight_non_stream", requestBody.MaxInFlightNonStream); err != nil {
			return connectionResponse{}, err
		}
		if err := validateLimiter("max_in_flight_stream", requestBody.MaxInFlightStream); err != nil {
			return connectionResponse{}, err
		}
		openAIProbeVariant, err := resolveOpenAIProbeEndpointVariant(apiFamily, requestBody.OpenAIProbeEndpointVariant)
		if err != nil {
			return connectionResponse{}, err
		}
		pricingTemplateID, err := validatePricingTemplateID(r.Context(), tx, profile.ID, requestBody.PricingTemplateID)
		if err != nil {
			return connectionResponse{}, err
		}
		endpoint, err := s.resolveCreateEndpoint(r.Context(), tx, profile.ID, requestBody.EndpointID, requestBody.EndpointCreate)
		if err != nil {
			return connectionResponse{}, err
		}
		now := s.nowUTC()
		item := connectionResponse{ProfileID: profile.ID, APIFamily: apiFamily, EndpointID: endpoint.ID, IsActive: resolvedBool(requestBody.IsActive, true), Priority: 0, Name: normalizeOptionalString(requestBody.Name), AuthType: authType, CustomHeaders: normalizeHeaders(requestBody.CustomHeaders), OpenAIProbeEndpointVariant: openAIProbeVariant, PricingTemplateID: pricingTemplateID, QPSLimit: requestBody.QPSLimit, MaxInFlightNonStream: requestBody.MaxInFlightNonStream, MaxInFlightStream: requestBody.MaxInFlightStream, HealthStatus: "unknown", CreatedAt: now, UpdatedAt: now}
		connectionID, err := insertConnection(r.Context(), tx, item)
		if err != nil {
			return connectionResponse{}, err
		}
		created, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, false)
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		return created, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateConnection(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody connectionUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if requestBody.Priority.Set {
		writeError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "priority is not allowed on update")
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionResponse{}, err
		}
		current, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, true)
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		if requestBody.EndpointID.Set && requestBody.EndpointID.Value != nil && requestBody.EndpointCreate.Set && requestBody.EndpointCreate.Value != nil {
			return connectionResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "endpoint_id and endpoint_create are mutually exclusive"}
		}
		next := current
		if requestBody.APIFamily.Set {
			if requestBody.APIFamily.Value == nil {
				return connectionResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "api_family is required"}
			}
			apiFamily, err := validateAPIFamily(*requestBody.APIFamily.Value, true)
			if err != nil {
				return connectionResponse{}, err
			}
			if apiFamily != current.APIFamily {
				if err := ensureConnectionAPIFamilyUpdateAllowed(r.Context(), tx, profile.ID, current.ID, apiFamily); err != nil {
					return connectionResponse{}, err
				}
			}
			next.APIFamily = apiFamily
			if next.APIFamily != "openai" {
				next.OpenAIProbeEndpointVariant = nil
			}
		}

		if requestBody.EndpointCreate.Set && requestBody.EndpointCreate.Value != nil {
			endpoint, err := s.createInlineEndpoint(r.Context(), tx, profile.ID, *requestBody.EndpointCreate.Value)
			if err != nil {
				return connectionResponse{}, err
			}
			next.EndpointID = endpoint.ID
		}
		if requestBody.EndpointID.Set && requestBody.EndpointID.Value != nil {
			endpoint, found, err := loadProfileEndpointRecord(r.Context(), tx, profile.ID, *requestBody.EndpointID.Value)
			if err != nil {
				return connectionResponse{}, err
			}
			if !found {
				return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
			}
			next.EndpointID = endpoint.ID
		}
		if requestBody.IsActive.Set {
			next.IsActive = requestBody.IsActive.Value
		}
		if requestBody.Name.Set {
			next.Name = normalizeOptionalString(requestBody.Name.Value)
		}
		if requestBody.AuthType.Set {
			authType, err := validateAuthType(requestBody.AuthType.Value)
			if err != nil {
				return connectionResponse{}, err
			}
			next.AuthType = authType
		}
		if requestBody.CustomHeaders.Set {
			headers := normalizeHeaders(requestBody.CustomHeaders.Value)
			next.CustomHeaders = headers
		}
		if requestBody.OpenAIProbeEndpointVariant.Set {
			variant, err := resolveOpenAIProbeEndpointVariant(next.APIFamily, requestBody.OpenAIProbeEndpointVariant.Value)
			if err != nil {
				return connectionResponse{}, err
			}
			next.OpenAIProbeEndpointVariant = variant
		}
		if requestBody.PricingTemplateID.Set {
			pricingTemplateID, err := validatePricingTemplateID(r.Context(), tx, profile.ID, requestBody.PricingTemplateID.Value)
			if err != nil {
				return connectionResponse{}, err
			}
			next.PricingTemplateID = pricingTemplateID
		}
		if requestBody.QPSLimit.Set {
			if err := validateLimiter("qps_limit", requestBody.QPSLimit.Value); err != nil {
				return connectionResponse{}, err
			}
			next.QPSLimit = requestBody.QPSLimit.Value
		}
		if requestBody.MaxInFlightNonStream.Set {
			if err := validateLimiter("max_in_flight_non_stream", requestBody.MaxInFlightNonStream.Value); err != nil {
				return connectionResponse{}, err
			}
			next.MaxInFlightNonStream = requestBody.MaxInFlightNonStream.Value
		}
		if requestBody.MaxInFlightStream.Set {
			if err := validateLimiter("max_in_flight_stream", requestBody.MaxInFlightStream.Value); err != nil {
				return connectionResponse{}, err
			}
			next.MaxInFlightStream = requestBody.MaxInFlightStream.Value
		}
		next.UpdatedAt = s.nowUTC()
		if err := updateConnectionRow(r.Context(), tx, next); err != nil {
			return connectionResponse{}, err
		}
		updated, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, current.ID, false)
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		return updated, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleSetConnectionPricingTemplate(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody connectionPricingTemplateUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if !requestBody.PricingTemplateID.Set {
		writeError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "pricing_template_id is required")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionResponse{}, err
		}
		current, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, true)
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		pricingTemplateID, err := validatePricingTemplateID(r.Context(), tx, profile.ID, requestBody.PricingTemplateID.Value)
		if err != nil {
			return connectionResponse{}, err
		}
		current.PricingTemplateID = pricingTemplateID
		current.UpdatedAt = s.nowUTC()
		if err := updateConnectionRow(r.Context(), tx, current); err != nil {
			return connectionResponse{}, err
		}
		updated, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, false)
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		return updated, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (deletedResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return deletedResponse{}, err
		}
		current, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, true)
		if err != nil {
			return deletedResponse{}, err
		}
		if !found {
			return deletedResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		references, err := listConnectionReferenceRows(r.Context(), tx, profile.ID, current.ID)
		if err != nil {
			return deletedResponse{}, err
		}
		if len(references) > 0 {
			return deletedResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("Cannot delete: models [%s] target this connection", joinConnectionReferenceModelIDs(references))}
		}
		if err := deleteConnectionRow(r.Context(), tx, current.ID); err != nil {
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

func (s *Service) handleListConnectionReferences(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionReferencesResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionReferencesResponse{}, err
		}
		if _, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, false); err != nil {
			return connectionReferencesResponse{}, err
		} else if !found {
			return connectionReferencesResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		records, err := listConnectionReferenceRows(r.Context(), tx, profile.ID, connectionID)
		if err != nil {
			return connectionReferencesResponse{}, err
		}
		return connectionReferencesResponse{ConnectionID: connectionID, Items: connectionReferenceResponsesFromRecords(records)}, nil
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

func lockProfileRow(ctx context.Context, tx pgx.Tx, profileID int) error {
	if err := tx.QueryRow(ctx, `SELECT id FROM profiles WHERE id = $1 FOR UPDATE`, profileID).Scan(new(int)); err != nil {
		return fmt.Errorf("lock profile %d: %w", profileID, err)
	}
	return nil
}

func (s *Service) resolveCreateEndpoint(ctx context.Context, tx pgx.Tx, profileID int, endpointID *int, inline *endpointCreateRequest) (endpointRecord, error) {
	if endpointID != nil {
		endpoint, found, err := loadProfileEndpointRecord(ctx, tx, profileID, *endpointID)
		if err != nil {
			return endpointRecord{}, err
		}
		if !found {
			return endpointRecord{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}
		return endpoint, nil
	}
	if inline != nil {
		return s.createInlineEndpoint(ctx, tx, profileID, *inline)
	}
	return endpointRecord{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
}

func (s *Service) createInlineEndpoint(ctx context.Context, tx pgx.Tx, profileID int, inline endpointCreateRequest) (endpointRecord, error) {
	endpointName := strings.TrimSpace(inline.Name)
	if endpointName == "" {
		return endpointRecord{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "endpoint_create.name must not be empty"}
	}
	normalizedURL := endpointdomain.NormalizeBaseURL(inline.BaseURL)
	if warnings := endpointdomain.ValidateBaseURL(normalizedURL); len(warnings) > 0 {
		return endpointRecord{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: strings.Join(warnings, "; ")}
	}
	if err := lockProfileRow(ctx, tx, profileID); err != nil {
		return endpointRecord{}, err
	}
	if err := ensureUniqueEndpointName(ctx, tx, profileID, endpointName); err != nil {
		return endpointRecord{}, err
	}
	encryptedAPIKey, err := endpointdomain.EncryptSecret(inline.APIKey, s.secretEncryptionKey, s.now)
	if err != nil {
		return endpointRecord{}, err
	}
	position, err := nextEndpointPosition(ctx, tx, profileID)
	if err != nil {
		return endpointRecord{}, err
	}
	return insertEndpoint(ctx, tx, endpointRecord{ProfileID: profileID, Name: endpointName, BaseURL: normalizedURL, APIKey: encryptedAPIKey, Position: position, CreatedAt: s.nowUTC(), UpdatedAt: s.nowUTC()})
}

func normalizeConnectionPriorities(items []connectionResponse, currentTime time.Time) bool {
	changed := false
	for index := range items {
		if items[index].Priority == index {
			continue
		}
		items[index].Priority = index
		items[index].UpdatedAt = currentTime
		changed = true
	}
	return changed
}

func validateLimiter(fieldName string, value *int) error {
	if value != nil && *value < 1 {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s must be >= 1 when provided", fieldName)}
	}
	return nil
}

func validateAPIFamily(value string, required bool) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		if required {
			return "", &domainError{StatusCode: http.StatusBadRequest, Detail: "api_family is required"}
		}
		return "", nil
	}
	if normalized != "openai" && normalized != "anthropic" && normalized != "gemini" {
		return "", &domainError{StatusCode: http.StatusBadRequest, Detail: "api_family must be one of 'openai', 'anthropic', or 'gemini'"}
	}
	return normalized, nil
}

func ensureConnectionAPIFamilyUpdateAllowed(ctx context.Context, exec queryExecutor, profileID int, connectionID int, apiFamily string) error {
	references, err := listConnectionReferenceRows(ctx, exec, profileID, connectionID)
	if err != nil {
		return err
	}
	for _, reference := range references {
		if reference.APIFamily != apiFamily {
			return &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("Cannot change api_family: models [%s] target this connection", joinConnectionReferenceModelIDs(references))}
		}
	}
	return nil
}

func connectionReferenceResponsesFromRecords(records []connectionReferenceRecord) []connectionReferenceResponse {
	items := make([]connectionReferenceResponse, 0, len(records))
	for _, record := range records {
		items = append(items, connectionReferenceResponse{TargetID: record.TargetID, ModelConfigID: record.ModelConfigID, ModelID: record.ModelID, APIFamily: record.APIFamily, Position: record.Position, IsEnabled: record.IsEnabled})
	}
	return items
}

func joinConnectionReferenceModelIDs(records []connectionReferenceRecord) string {
	modelIDs := make([]string, 0, len(records))
	seen := map[string]struct{}{}
	for _, record := range records {
		if _, ok := seen[record.ModelID]; ok {
			continue
		}
		seen[record.ModelID] = struct{}{}
		modelIDs = append(modelIDs, record.ModelID)
	}
	return strings.Join(modelIDs, ", ")
}

func validateAuthType(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "auth_type must be one of 'openai', 'anthropic', or 'gemini'"}
	}
	normalized = strings.ToLower(normalized)
	if normalized != "openai" && normalized != "anthropic" && normalized != "gemini" {
		return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "auth_type must be one of 'openai', 'anthropic', or 'gemini'"}
	}
	return &normalized, nil
}

func resolveOpenAIProbeEndpointVariant(apiFamily string, value *string) (*string, error) {
	if apiFamily != "openai" {
		if value != nil && strings.TrimSpace(*value) != "" {
			return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "openai_probe_endpoint_variant is only supported for OpenAI-family connections"}
		}
		return nil, nil
	}
	if value == nil || strings.TrimSpace(*value) == "" {
		return stringPtr(defaultOpenAIProbeEndpointVariant), nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized != "responses_minimal" && normalized != "responses_reasoning_none" && normalized != "chat_completions_minimal" && normalized != "chat_completions_reasoning_none" {
		return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "openai_probe_endpoint_variant is invalid"}
	}
	return &normalized, nil
}

func normalizeHeaders(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	return value
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	resolved := *value
	return &resolved
}

func resolvedBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func dedupeIntValues(values []int) []int {
	seen := map[int]struct{}{}
	items := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	return items
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
	var connectionErr *domainError
	if errors.As(err, &connectionErr) {
		writeError(w, r, corsSnapshot, connectionErr.StatusCode, connectionErr.Detail)
		return
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail any) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
	writeJSON(w, statusCode, map[string]any{"detail": detail})
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
