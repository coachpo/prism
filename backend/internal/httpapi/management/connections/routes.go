package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

const defaultOpenAIProbeEndpointVariant = "responses_minimal"

func (s *Service) handleListConnectionsBatch(w http.ResponseWriter, r *http.Request) {
	var requestBody modelConnectionsBatchRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	normalizedModelIDs := dedupeIntValues(requestBody.ModelConfigIDs)
	if len(normalizedModelIDs) == 0 {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "model_config_ids must contain at least one model config id")
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleListConnections(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) ([]connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		if _, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID); err != nil {
			return nil, err
		} else if !found {
			return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		return listConnectionsForModel(r.Context(), tx, profile.ID, modelConfigID)
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	var requestBody connectionCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	if requestBody.Priority.Set {
		writeError(w, r, s.allowedOrigins, http.StatusUnprocessableEntity, "priority is not allowed on create")
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionResponse{}, err
		}
		model, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID)
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
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
		openAIProbeVariant, err := resolveOpenAIProbeEndpointVariant(model.APIFamily, requestBody.OpenAIProbeEndpointVariant)
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
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return connectionResponse{}, err
		}
		existingConnections, err := listConnectionsForModel(r.Context(), tx, profile.ID, modelConfigID)
		if err != nil {
			return connectionResponse{}, err
		}
		if normalizeConnectionPriorities(existingConnections, s.nowUTC()) {
			if err := persistConnectionPriorities(r.Context(), tx, existingConnections); err != nil {
				return connectionResponse{}, err
			}
		}
		now := s.nowUTC()
		item := connectionResponse{ProfileID: profile.ID, ModelConfigID: modelConfigID, EndpointID: endpoint.ID, IsActive: resolvedBool(requestBody.IsActive, true), Priority: len(existingConnections), Name: normalizeOptionalString(requestBody.Name), AuthType: authType, CustomHeaders: normalizeHeaders(requestBody.CustomHeaders), OpenAIProbeEndpointVariant: openAIProbeVariant, PricingTemplateID: pricingTemplateID, QPSLimit: requestBody.QPSLimit, MaxInFlightNonStream: requestBody.MaxInFlightNonStream, MaxInFlightStream: requestBody.MaxInFlightStream, HealthStatus: "unknown", CreatedAt: now, UpdatedAt: now}
		connectionID, err := insertConnection(r.Context(), tx, item)
		if err != nil {
			return connectionResponse{}, err
		}
		if err := clearRoundRobinStateForModel(r.Context(), tx, profile.ID, modelConfigID); err != nil {
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateConnection(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	var requestBody connectionUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	if requestBody.Priority.Set {
		writeError(w, r, s.allowedOrigins, http.StatusUnprocessableEntity, "priority is not allowed on update")
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
		model, found, err := loadModelRecord(r.Context(), tx, profile.ID, current.ModelConfigID)
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		if requestBody.EndpointID.Set && requestBody.EndpointID.Value != nil && requestBody.EndpointCreate.Set && requestBody.EndpointCreate.Value != nil {
			return connectionResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "endpoint_id and endpoint_create are mutually exclusive"}
		}
		next := current
		clearConnectionState := false
		clearRoundRobinState := false

		if requestBody.EndpointCreate.Set && requestBody.EndpointCreate.Value != nil {
			endpoint, err := s.createInlineEndpoint(r.Context(), tx, profile.ID, *requestBody.EndpointCreate.Value)
			if err != nil {
				return connectionResponse{}, err
			}
			if endpoint.ID != next.EndpointID {
				clearConnectionState = true
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
			if endpoint.ID != next.EndpointID {
				clearConnectionState = true
			}
			next.EndpointID = endpoint.ID
		}
		if requestBody.IsActive.Set {
			if requestBody.IsActive.Value != current.IsActive {
				clearConnectionState = true
				clearRoundRobinState = true
			}
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
			if !reflect.DeepEqual(authType, current.AuthType) {
				clearConnectionState = true
			}
			next.AuthType = authType
		}
		if requestBody.CustomHeaders.Set {
			headers := normalizeHeaders(requestBody.CustomHeaders.Value)
			if !reflect.DeepEqual(headers, current.CustomHeaders) {
				clearConnectionState = true
			}
			next.CustomHeaders = headers
		}
		if requestBody.OpenAIProbeEndpointVariant.Set {
			variant, err := resolveOpenAIProbeEndpointVariant(model.APIFamily, requestBody.OpenAIProbeEndpointVariant.Value)
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
		if clearConnectionState {
			if err := clearConnectionRuntimeState(r.Context(), tx, profile.ID, current.ID); err != nil {
				return connectionResponse{}, err
			}
		}
		if clearRoundRobinState {
			if err := clearRoundRobinStateForModel(r.Context(), tx, profile.ID, current.ModelConfigID); err != nil {
				return connectionResponse{}, err
			}
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleMoveConnectionPriority(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	var requestBody connectionPriorityMoveRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) ([]connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		if _, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID); err != nil {
			return nil, err
		} else if !found {
			return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return nil, err
		}
		connections, err := listConnectionsForModel(r.Context(), tx, profile.ID, modelConfigID)
		if err != nil {
			return nil, err
		}
		currentIndex := -1
		for index := range connections {
			if connections[index].ID == connectionID {
				currentIndex = index
				break
			}
		}
		if currentIndex == -1 {
			return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		if requestBody.ToIndex < 0 || requestBody.ToIndex >= len(connections) {
			return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("to_index must be between 0 and %d", len(connections)-1)}
		}
		if requestBody.ToIndex == currentIndex {
			if normalizeConnectionPriorities(connections, s.nowUTC()) {
				if err := persistConnectionPriorities(r.Context(), tx, connections); err != nil {
					return nil, err
				}
			}
			return connections, nil
		}
		moved := connections[currentIndex]
		connections = append(connections[:currentIndex], connections[currentIndex+1:]...)
		connections = append(connections[:requestBody.ToIndex], append([]connectionResponse{moved}, connections[requestBody.ToIndex:]...)...)
		normalizeConnectionPriorities(connections, s.nowUTC())
		if err := persistConnectionPriorities(r.Context(), tx, connections); err != nil {
			return nil, err
		}
		if err := clearRoundRobinStateForModel(r.Context(), tx, profile.ID, modelConfigID); err != nil {
			return nil, err
		}
		return connections, nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleSetConnectionPricingTemplate(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	var requestBody connectionPricingTemplateUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	if !requestBody.PricingTemplateID.Set {
		writeError(w, r, s.allowedOrigins, http.StatusUnprocessableEntity, "pricing_template_id is required")
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (deletedResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return deletedResponse{}, err
		}
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return deletedResponse{}, err
		}
		current, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, true)
		if err != nil {
			return deletedResponse{}, err
		}
		if !found {
			return deletedResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		if err := clearConnectionRuntimeState(r.Context(), tx, profile.ID, current.ID); err != nil {
			return deletedResponse{}, err
		}
		if err := deleteConnectionRow(r.Context(), tx, current.ID); err != nil {
			return deletedResponse{}, err
		}
		remaining, err := listConnectionsForModel(r.Context(), tx, profile.ID, current.ModelConfigID)
		if err != nil {
			return deletedResponse{}, err
		}
		if normalizeConnectionPriorities(remaining, s.nowUTC()) {
			if err := persistConnectionPriorities(r.Context(), tx, remaining); err != nil {
				return deletedResponse{}, err
			}
		}
		if err := clearRoundRobinStateForModel(r.Context(), tx, profile.ID, current.ModelConfigID); err != nil {
			return deletedResponse{}, err
		}
		return deletedResponse{Deleted: true}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetConnectionOwner(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionOwnerResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionOwnerResponse{}, err
		}
		record, found, err := loadConnectionOwner(r.Context(), tx, profile.ID, connectionID)
		if err != nil {
			return connectionOwnerResponse{}, err
		}
		if !found {
			return connectionOwnerResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		if record.EndpointID == nil || record.EndpointName == nil || record.EndpointBaseURL == nil {
			return connectionOwnerResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Connection endpoint is missing"}
		}
		if record.ModelID == nil {
			return connectionOwnerResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		return connectionOwnerResponse{ConnectionID: record.ConnectionID, ModelConfigID: record.ModelConfigID, ModelID: *record.ModelID, ConnectionName: record.ConnectionName, EndpointID: *record.EndpointID, EndpointName: *record.EndpointName, EndpointBaseURL: *record.EndpointBaseURL}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
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

func writeDomainError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, err error) {
	var connectionErr *domainError
	if errors.As(err, &connectionErr) {
		writeError(w, r, allowedOrigins, connectionErr.StatusCode, connectionErr.Detail)
		return
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		writeError(w, r, allowedOrigins, profileErr.StatusCode, profileErr.Detail)
		return
	}
	writeError(w, r, allowedOrigins, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, statusCode int, detail any) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		if _, ok := allowedOrigins[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
	}
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
