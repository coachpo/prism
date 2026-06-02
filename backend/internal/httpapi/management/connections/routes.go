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

	"github.com/coachpo/prism/backend/internal/contextcapability"
	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/coachpo/prism/backend/internal/targetcompat"
)

const defaultOpenAIProbeEndpointVariant = "responses_minimal"

const (
	openAIUpstreamOperationResponses       = "openai.responses"
	openAIUpstreamOperationChatCompletions = "openai.chat_completions"
)

const ownerScopedConnectionMutationDetail = "terminal target mutations must use owner-scoped routes under " + targetcompat.OwnerScopedConnectionRoutePath

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

func (s *Service) handleListModelConnections(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) ([]connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		owner, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, false)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		return listConnectionsForModel(r.Context(), tx, profile.ID, owner.ID)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, withOwnerScopedConnectionOverrideMetadataList(response))
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

func (s *Service) handleCreateModelConnection(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody connectionCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if requestBody.Priority.Set {
		writeError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "priority is not allowed on create")
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionResponse{}, err
		}
		owner, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, true)
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		if err := validateOwnerScopedAPIFamily(requestBody.APIFamily, owner.APIFamily); err != nil {
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
		openAIProbeVariant, err := resolveOpenAIProbeEndpointVariant(owner.APIFamily, requestBody.OpenAIProbeEndpointVariant)
		if err != nil {
			return connectionResponse{}, err
		}
		pricingTemplateID, err := validatePricingTemplateID(r.Context(), tx, profile.ID, requestBody.PricingTemplateID)
		if err != nil {
			return connectionResponse{}, err
		}
		if err := validateConnectionContextCapabilitiesCreate(owner, requestBody); err != nil {
			return connectionResponse{}, err
		}
		endpoint, err := s.resolveCreateEndpoint(r.Context(), tx, profile.ID, requestBody.EndpointID, requestBody.EndpointCreate)
		if err != nil {
			return connectionResponse{}, err
		}
		position, err := nextModelAccessTargetPosition(r.Context(), tx, profile.ID, owner.ID)
		if err != nil {
			return connectionResponse{}, err
		}
		capabilitySettings, err := contextcapability.NormalizeConnectionSettings(contextcapability.Settings{ContextWindowTokens: contextcapability.CopyIntPtr(owner.ContextWindowTokens), DefaultOutputTokenReserve: owner.DefaultOutputTokenReserve, MaxContextUtilization: owner.MaxContextUtilization, PreferredContextUtilizationThreshold: contextcapability.CopyFloat64Ptr(owner.PreferredContextUtilizationThreshold)}, requestBody.ContextWindowTokens, requestBody.DefaultOutputTokenReserve, requestBody.MaxContextUtilization, requestBody.PreferredContextUtilizationThreshold)
		if err != nil {
			return connectionResponse{}, connectionContextCapabilityDomainError(err)
		}
		now := s.nowUTC()
		item := connectionResponse{
			ProfileID:                                      profile.ID,
			APIFamily:                                      owner.APIFamily,
			EndpointID:                                     endpoint.ID,
			ContextWindowTokens:                            capabilitySettings.ContextWindowTokens,
			ContextWindowTokensOverridden:                  requestBody.ContextWindowTokens != nil,
			DefaultOutputTokenReserve:                      capabilitySettings.DefaultOutputTokenReserve,
			DefaultOutputTokenReserveOverridden:            requestBody.DefaultOutputTokenReserve != nil,
			MaxContextUtilization:                          capabilitySettings.MaxContextUtilization,
			MaxContextUtilizationOverridden:                requestBody.MaxContextUtilization != nil,
			PreferredContextUtilizationThreshold:           capabilitySettings.PreferredContextUtilizationThreshold,
			PreferredContextUtilizationThresholdOverridden: requestBody.PreferredContextUtilizationThreshold != nil,
			IsActive:                   resolvedBool(requestBody.IsActive, true),
			Priority:                   position,
			Name:                       normalizeOptionalString(requestBody.Name),
			AuthType:                   authType,
			CustomHeaders:              normalizeHeaders(requestBody.CustomHeaders),
			OpenAIProbeEndpointVariant: openAIProbeVariant,
			PricingTemplateID:          pricingTemplateID,
			QPSLimit:                   requestBody.QPSLimit,
			MaxInFlightNonStream:       requestBody.MaxInFlightNonStream,
			MaxInFlightStream:          requestBody.MaxInFlightStream,
			HealthStatus:               "unknown",
			CreatedAt:                  now,
			UpdatedAt:                  now,
		}
		connectionID, err := insertConnection(r.Context(), tx, item)
		if err != nil {
			return connectionResponse{}, err
		}
		if err := insertOwnerConnectionTarget(r.Context(), tx, profile.ID, owner.ID, connectionID, position, now); err != nil {
			return connectionResponse{}, err
		}
		created, found, err := loadModelConnectionRecord(r.Context(), tx, profile.ID, owner.ID, connectionID)
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
	writeJSON(w, http.StatusCreated, withOwnerScopedConnectionOverrideMetadata(response))
}

func (s *Service) handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	s.writeConnectionMutationRouteError(w, r)
}

func (s *Service) handleUpdateConnection(w http.ResponseWriter, r *http.Request) {
	s.writeConnectionMutationRouteError(w, r)
}

func (s *Service) handleUpdateModelConnection(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
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
		owner, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, true)
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		if err := lockProfileAccessTargetRows(r.Context(), tx, profile.ID); err != nil {
			return connectionResponse{}, err
		}
		current, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, true)
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		if _, found, err := loadConnectionOwnerReference(r.Context(), tx, profile.ID, owner.ID, current.ID, true); err != nil {
			return connectionResponse{}, err
		} else if !found {
			return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found for owner model"}
		}
		next, err := s.applyOwnerScopedConnectionUpdate(r.Context(), tx, profile.ID, owner, current, requestBody)
		if err != nil {
			return connectionResponse{}, err
		}
		if err := updateConnectionRow(r.Context(), tx, next); err != nil {
			return connectionResponse{}, err
		}
		updated, found, err := loadModelConnectionRecord(r.Context(), tx, profile.ID, owner.ID, current.ID)
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
	writeJSON(w, http.StatusOK, withOwnerScopedConnectionOverrideMetadata(response))
}

func (s *Service) applyOwnerScopedConnectionUpdate(ctx context.Context, tx pgx.Tx, profileID int, owner modelRecord, current connectionResponse, requestBody connectionUpdateRequest) (connectionResponse, error) {
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
		if apiFamily != owner.APIFamily {
			return connectionResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "api_family must match owner model api_family"}
		}
		next.APIFamily = apiFamily
	}
	if next.APIFamily != owner.APIFamily {
		return connectionResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "Connection api_family must match owner model api_family"}
	}
	if next.APIFamily != "openai" {
		next.OpenAIProbeEndpointVariant = nil
	}

	if requestBody.EndpointCreate.Set && requestBody.EndpointCreate.Value != nil {
		endpoint, err := s.createInlineEndpoint(ctx, tx, profileID, *requestBody.EndpointCreate.Value)
		if err != nil {
			return connectionResponse{}, err
		}
		next.EndpointID = endpoint.ID
	}
	if requestBody.EndpointID.Set && requestBody.EndpointID.Value != nil {
		endpoint, found, err := loadProfileEndpointRecord(ctx, tx, profileID, *requestBody.EndpointID.Value)
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}
		next.EndpointID = endpoint.ID
	}
	if requestBody.ContextWindowTokens.Set {
		if requestBody.ContextWindowTokens.Value == nil {
			next.ContextWindowTokens = contextcapability.CopyIntPtr(owner.ContextWindowTokens)
			next.ContextWindowTokensOverridden = false
		} else {
			resolvedContextWindowTokens, normalizeErr := contextcapability.NormalizeContextWindowTokens(requestBody.ContextWindowTokens.Value)
			if normalizeErr != nil {
				return connectionResponse{}, connectionContextCapabilityFieldError("context_window_tokens", normalizeErr)
			}
			next.ContextWindowTokens = resolvedContextWindowTokens
			next.ContextWindowTokensOverridden = true
		}
	}
	if requestBody.DefaultOutputTokenReserve.Set {
		if requestBody.DefaultOutputTokenReserve.Value == nil {
			next.DefaultOutputTokenReserve = owner.DefaultOutputTokenReserve
			next.DefaultOutputTokenReserveOverridden = false
		} else {
			resolvedOutputTokenReserve, normalizeErr := contextcapability.NormalizeOutputTokenReserve(requestBody.DefaultOutputTokenReserve.Value)
			if normalizeErr != nil {
				return connectionResponse{}, connectionContextCapabilityFieldError("default_output_token_reserve", normalizeErr)
			}
			next.DefaultOutputTokenReserve = resolvedOutputTokenReserve
			next.DefaultOutputTokenReserveOverridden = true
		}
	}
	if requestBody.MaxContextUtilization.Set {
		if requestBody.MaxContextUtilization.Value == nil {
			next.MaxContextUtilization = owner.MaxContextUtilization
			next.MaxContextUtilizationOverridden = false
		} else {
			resolvedMaxContextUtilization, normalizeErr := contextcapability.NormalizeMaxContextUtilization(requestBody.MaxContextUtilization.Value)
			if normalizeErr != nil {
				return connectionResponse{}, connectionContextCapabilityFieldError("max_context_utilization", normalizeErr)
			}
			next.MaxContextUtilization = resolvedMaxContextUtilization
			next.MaxContextUtilizationOverridden = true
		}
	}
	if requestBody.PreferredContextUtilizationThreshold.Set {
		if requestBody.PreferredContextUtilizationThreshold.Value == nil {
			next.PreferredContextUtilizationThreshold = contextcapability.CopyFloat64Ptr(owner.PreferredContextUtilizationThreshold)
			next.PreferredContextUtilizationThresholdOverridden = false
		} else {
			resolvedPreferredContextUtilizationThreshold, normalizeErr := contextcapability.NormalizePreferredContextUtilizationThreshold(requestBody.PreferredContextUtilizationThreshold.Value, next.MaxContextUtilization)
			if normalizeErr != nil {
				return connectionResponse{}, connectionContextCapabilityFieldError("preferred_context_utilization_threshold", normalizeErr)
			}
			next.PreferredContextUtilizationThreshold = resolvedPreferredContextUtilizationThreshold
			next.PreferredContextUtilizationThresholdOverridden = true
		}
	}
	resolvedPreferredContextUtilizationThreshold, normalizeErr := contextcapability.NormalizePreferredContextUtilizationThreshold(next.PreferredContextUtilizationThreshold, next.MaxContextUtilization)
	if normalizeErr != nil {
		return connectionResponse{}, connectionContextCapabilityFieldError("preferred_context_utilization_threshold", normalizeErr)
	}
	next.PreferredContextUtilizationThreshold = resolvedPreferredContextUtilizationThreshold
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
		next.CustomHeaders = normalizeHeaders(requestBody.CustomHeaders.Value)
	}
	if requestBody.OpenAIProbeEndpointVariant.Set {
		variant, err := resolveOpenAIProbeEndpointVariant(next.APIFamily, requestBody.OpenAIProbeEndpointVariant.Value)
		if err != nil {
			return connectionResponse{}, err
		}
		next.OpenAIProbeEndpointVariant = variant
	}
	if requestBody.PricingTemplateID.Set {
		pricingTemplateID, err := validatePricingTemplateID(ctx, tx, profileID, requestBody.PricingTemplateID.Value)
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
	return next, nil
}

func (s *Service) handleSetConnectionPricingTemplate(w http.ResponseWriter, r *http.Request) {
	s.writeConnectionMutationRouteError(w, r)
}

func (s *Service) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	s.writeConnectionMutationRouteError(w, r)
}

func (s *Service) handleDeleteModelConnection(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
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
		owner, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, true)
		if err != nil {
			return deletedResponse{}, err
		}
		if !found {
			return deletedResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		if err := lockProfileAccessTargetRows(r.Context(), tx, profile.ID); err != nil {
			return deletedResponse{}, err
		}
		current, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, true)
		if err != nil {
			return deletedResponse{}, err
		}
		if !found {
			return deletedResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		reference, found, err := loadConnectionOwnerReference(r.Context(), tx, profile.ID, owner.ID, current.ID, true)
		if err != nil {
			return deletedResponse{}, err
		}
		if !found {
			return deletedResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Connection not found for owner model"}
		}
		if err := ensureOwnerConnectionDeleteAllowed(r.Context(), tx, profile.ID, owner, reference.TargetID); err != nil {
			return deletedResponse{}, err
		}
		if err := deleteModelAccessTargetRow(r.Context(), tx, reference.TargetID); err != nil {
			return deletedResponse{}, err
		}
		if err := deleteConnectionRow(r.Context(), tx, current.ID); err != nil {
			return deletedResponse{}, err
		}
		if err := compactModelAccessTargetPositions(r.Context(), tx, profile.ID, owner.ID, s.nowUTC()); err != nil {
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

func ensureOwnerConnectionDeleteAllowed(ctx context.Context, exec queryExecutor, profileID int, owner modelRecord, deletingTargetID int) error {
	if !owner.IsEnabled {
		return nil
	}
	enabledCount, err := countEnabledModelAccessTargetsExcluding(ctx, exec, profileID, owner.ID, deletingTargetID)
	if err != nil {
		return err
	}
	if enabledCount == 0 {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "enabled models must include at least one enabled access target"}
	}
	return nil
}

func (s *Service) handleRejectModelConnectionLegacyMutation(w http.ResponseWriter, r *http.Request) {
	s.writeConnectionMutationRouteError(w, r)
}

func (s *Service) handleLegacyModelConnectionNotFound(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func (s *Service) writeConnectionMutationRouteError(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, ownerScopedConnectionMutationDetail)
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

func withOwnerScopedConnectionOverrideMetadata(item connectionResponse) connectionResponse {
	overrides := &contextCapabilityOverridesResponse{}
	if item.ContextWindowTokensOverridden {
		overrides.ContextWindowTokens = contextcapability.CopyIntPtr(item.ContextWindowTokens)
	}
	if item.DefaultOutputTokenReserveOverridden {
		reserve := item.DefaultOutputTokenReserve
		overrides.DefaultOutputTokenReserve = &reserve
	}
	if item.MaxContextUtilizationOverridden {
		maxContextUtilization := item.MaxContextUtilization
		overrides.MaxContextUtilization = &maxContextUtilization
	}
	if item.PreferredContextUtilizationThresholdOverridden {
		overrides.PreferredContextUtilizationThreshold = contextcapability.CopyFloat64Ptr(item.PreferredContextUtilizationThreshold)
	}
	item.ContextCapabilityOverrides = overrides
	return item
}

func withOwnerScopedConnectionOverrideMetadataList(items []connectionResponse) []connectionResponse {
	for index := range items {
		items[index] = withOwnerScopedConnectionOverrideMetadata(items[index])
	}
	return items
}

func connectionContextCapabilityDomainError(err error) error {
	return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: err.Error()}
}

func connectionContextCapabilityFieldError(fieldName string, err error) error {
	return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s %s", fieldName, err.Error())}
}

func validateConnectionContextCapabilitiesCreate(owner modelRecord, requestBody connectionCreateRequest) error {
	if _, err := contextcapability.NormalizeContextWindowTokens(requestBody.ContextWindowTokens); err != nil {
		return connectionContextCapabilityFieldError("context_window_tokens", err)
	}
	if requestBody.DefaultOutputTokenReserve != nil {
		if _, err := contextcapability.NormalizeOutputTokenReserve(requestBody.DefaultOutputTokenReserve); err != nil {
			return connectionContextCapabilityFieldError("default_output_token_reserve", err)
		}
	}
	resolvedMaxContextUtilization := owner.MaxContextUtilization
	if requestBody.MaxContextUtilization != nil {
		var err error
		resolvedMaxContextUtilization, err = contextcapability.NormalizeMaxContextUtilization(requestBody.MaxContextUtilization)
		if err != nil {
			return connectionContextCapabilityFieldError("max_context_utilization", err)
		}
	}
	if _, err := contextcapability.NormalizePreferredContextUtilizationThreshold(requestBody.PreferredContextUtilizationThreshold, resolvedMaxContextUtilization); err != nil {
		return connectionContextCapabilityFieldError("preferred_context_utilization_threshold", err)
	}
	return nil
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

func validateOwnerScopedAPIFamily(value string, ownerAPIFamily string) error {
	apiFamily, err := validateAPIFamily(value, false)
	if err != nil {
		return err
	}
	if apiFamily != "" && apiFamily != ownerAPIFamily {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "api_family must match owner model api_family"}
	}
	return nil
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

func deriveOpenAIUpstreamOperation(apiFamily string, probeEndpointVariant *string) *string {
	if !strings.EqualFold(strings.TrimSpace(apiFamily), "openai") {
		return nil
	}
	variant := defaultOpenAIProbeEndpointVariant
	if probeEndpointVariant != nil && strings.TrimSpace(*probeEndpointVariant) != "" {
		variant = strings.TrimSpace(*probeEndpointVariant)
	}
	switch variant {
	case "chat_completions_minimal", "chat_completions_reasoning_none":
		return stringPtr(openAIUpstreamOperationChatCompletions)
	default:
		return stringPtr(openAIUpstreamOperationResponses)
	}
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
