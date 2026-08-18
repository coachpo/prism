package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

const ownerScopedConnectionMutationDetail = "terminal target mutations must use owner-scoped routes under /api/models/{model_config_id}/connections"

func (s *Service) handleListConnectionsBatch(w http.ResponseWriter, r *http.Request) {
	var requestBody modelConnectionsBatchRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	normalizedModelIDs := dedupeIntValues(requestBody.ModelConfigIDs)
	if len(normalizedModelIDs) == 0 {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "model_config_ids must contain at least one model config id")
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
		connectionsByModel, err := listConnectionsByModelIDs(r.Context(), tx, profile.ID, normalizedModelIDs, s.now().UTC())
		if err != nil {
			return modelConnectionsBatchResponse{}, err
		}
		items := make([]modelConnectionsBatchItem, 0, len(normalizedModelIDs))
		for _, modelConfigID := range normalizedModelIDs {
			connections := connectionsByModel[modelConfigID]
			if connections == nil {
				connections = []connectionResponse{}
			}
			items = append(items, modelConnectionsBatchItem{ModelConfigID: modelConfigID, Connections: maskConnectionsForWire(connections)})
		}
		return modelConnectionsBatchResponse{Items: items}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleListModelConnections(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
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
			return nil, &DomainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		connections, err := listConnectionsForModel(r.Context(), tx, profile.ID, owner.ID, s.now().UTC())
		if err != nil {
			return nil, err
		}
		return maskConnectionsForWire(connections), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleListConnections(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) ([]connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		connections, err := listConnections(r.Context(), tx, profile.ID, s.now().UTC())
		if err != nil {
			return nil, err
		}
		return maskConnectionsForWire(connections), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetConnection(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionResponse{}, err
		}
		connection, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, false, s.now().UTC())
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		return connection.maskedForWire(), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateModelConnection(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody connectionCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if requestBody.Priority.Set {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "priority is not allowed on create")
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionMutationEnvelope, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionMutationEnvelope{}, err
		}
		owner, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, true)
		if err != nil {
			return connectionMutationEnvelope{}, err
		}
		if !found {
			return connectionMutationEnvelope{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		if err := validateOwnerScopedAPIFamily(requestBody.APIFamily, owner.APIFamily); err != nil {
			return connectionMutationEnvelope{}, err
		}
		var inlineEndpoint *InlineEndpointCreate
		if requestBody.EndpointCreate != nil {
			inlineEndpoint = &InlineEndpointCreate{Name: requestBody.EndpointCreate.Name, BaseURL: requestBody.EndpointCreate.BaseURL, APIKey: requestBody.EndpointCreate.APIKey}
		}
		customRequestParameters, err := resolveCustomRequestParametersCreate(requestBody.CustomRequestParameters)
		if err != nil {
			return connectionMutationEnvelope{}, err
		}
		created, _, _, warnings, err := CreateOwnerConnection(r.Context(), tx, profile.ID, OwnerModel{ID: owner.ID, ProfileID: owner.ProfileID, ModelID: owner.ModelID, APIFamily: owner.APIFamily, OpenAIAcceptedFormat: owner.OpenAIAcceptedFormat}, s.secretEncryptionKey, s.now, OwnerConnectionCreateInput{
			EndpointID:              requestBody.EndpointID,
			EndpointCreate:          inlineEndpoint,
			IsActive:                resolvedBool(requestBody.IsActive, true),
			Name:                    requestBody.Name,
			AuthType:                requestBody.AuthType,
			CustomHeaders:           requestBody.CustomHeaders,
			CustomRequestParameters: customRequestParameters,
			RoutingSchedule:         requestBody.RoutingSchedule,
			OpenAITextCapability:    requestBody.OpenAITextCapability,
			OpenAIImageCapability:   requestBody.OpenAIImageCapability,
			PricingTemplateID:       requestBody.PricingTemplateID,
			QPSLimit:                requestBody.QPSLimit,
			MaxInFlightNonStream:    requestBody.MaxInFlightNonStream,
			MaxInFlightStream:       requestBody.MaxInFlightStream,
		})
		if err != nil {
			return connectionMutationEnvelope{}, err
		}
		accessTargets, err := loadOwnerMutationAccessTargets(r.Context(), tx, profile.ID, owner.ID)
		if err != nil {
			return connectionMutationEnvelope{}, err
		}
		return connectionMutationEnvelope{Connection: created.maskedForWire(), AccessTargets: accessTargets, ConfigurationWarnings: warnings}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusCreated, response)
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
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody connectionUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if requestBody.Priority.Set {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "priority is not allowed on update")
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionMutationEnvelope, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionMutationEnvelope{}, err
		}
		owner, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, true)
		if err != nil {
			return connectionMutationEnvelope{}, err
		}
		if !found {
			return connectionMutationEnvelope{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		if err := lockProfileAccessTargetRows(r.Context(), tx, profile.ID); err != nil {
			return connectionMutationEnvelope{}, err
		}
		current, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, true, s.now().UTC())
		if err != nil {
			return connectionMutationEnvelope{}, err
		}
		if !found {
			return connectionMutationEnvelope{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		if _, found, err := loadConnectionOwnerReference(r.Context(), tx, profile.ID, owner.ID, current.ID, true); err != nil {
			return connectionMutationEnvelope{}, err
		} else if !found {
			return connectionMutationEnvelope{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Connection not found for owner model"}
		}
		next, err := s.applyOwnerScopedConnectionUpdate(r.Context(), tx, profile.ID, owner, current, requestBody)
		if err != nil {
			return connectionMutationEnvelope{}, err
		}
		if err := updateTerminalTarget(r.Context(), tx, terminalTargetRecordFromConnectionResponse(next)); err != nil {
			return connectionMutationEnvelope{}, err
		}
		// Gated on Set so a PATCH that only touches name leaves the window rows
		// (and their ids) alone, and written before the read-back so the
		// response reflects the new windows rather than the previous ones.
		if requestBody.RoutingSchedule.Set {
			_, nextWindows := routingScheduleConfigFromResponse(next)
			if err := replaceConnectionRoutingWindows(r.Context(), tx, profile.ID, current.ID, nextWindows, s.now().UTC()); err != nil {
				return connectionMutationEnvelope{}, err
			}
		}
		updated, found, err := loadModelConnectionRecord(r.Context(), tx, profile.ID, owner.ID, current.ID, s.now().UTC())
		if err != nil {
			return connectionMutationEnvelope{}, err
		}
		if !found {
			return connectionMutationEnvelope{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		accessTargets, err := loadOwnerMutationAccessTargets(r.Context(), tx, profile.ID, owner.ID)
		if err != nil {
			return connectionMutationEnvelope{}, err
		}
		accessTargetID := 0
		for _, target := range accessTargets {
			if target.ConnectionID != nil && *target.ConnectionID == connectionID {
				accessTargetID = target.ID
				break
			}
		}
		warnings := ownerScopedConnectionWarnings(OwnerModel{ID: owner.ID, ProfileID: owner.ProfileID, ModelID: owner.ModelID, APIFamily: owner.APIFamily, OpenAIAcceptedFormat: owner.OpenAIAcceptedFormat, OpenAIImageOperations: owner.OpenAIImageOperations}, updated.OpenAITextCapability, updated.OpenAIImageCapability, accessTargetID, connectionID)
		return connectionMutationEnvelope{Connection: updated.maskedForWire(), AccessTargets: accessTargets, ConfigurationWarnings: warnings}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) applyOwnerScopedConnectionUpdate(ctx context.Context, tx pgx.Tx, profileID int, owner modelRecord, current connectionResponse, requestBody connectionUpdateRequest) (connectionResponse, error) {
	if requestBody.EndpointID.Set && requestBody.EndpointID.Value != nil && requestBody.EndpointCreate.Set && requestBody.EndpointCreate.Value != nil {
		return connectionResponse{}, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "endpoint_id and endpoint_create are mutually exclusive"}
	}
	next := current
	if requestBody.APIFamily.Set {
		if requestBody.APIFamily.Value == nil {
			return connectionResponse{}, &DomainError{StatusCode: http.StatusBadRequest, Detail: "api_family is required"}
		}
		apiFamily, err := validateAPIFamily(*requestBody.APIFamily.Value, true)
		if err != nil {
			return connectionResponse{}, err
		}
		if !providerauth.SameAPIFamily(apiFamily, owner.APIFamily) {
			return connectionResponse{}, &DomainError{StatusCode: http.StatusBadRequest, Detail: "api_family must match owner model api_family"}
		}
		next.APIFamily = apiFamily
	}
	if !providerauth.SameAPIFamily(next.APIFamily, owner.APIFamily) {
		return connectionResponse{}, &DomainError{StatusCode: http.StatusConflict, Detail: "Connection api_family must match owner model api_family"}
	}
	openAITextCapability, err := resolveOpenAITextCapabilityUpdate(current.APIFamily, next.APIFamily, current.OpenAITextCapability, requestBody.OpenAITextCapability)
	if err != nil {
		return connectionResponse{}, err
	}
	if err := ensureOpenAITextCapabilityMatchesOwnerModes(owner.APIFamily, owner.OpenAIAcceptedFormat, openAITextCapability); err != nil {
		return connectionResponse{}, err
	}
	openAIImageCapability, err := resolveOpenAIImageCapabilityUpdate(current.APIFamily, next.APIFamily, current.OpenAIImageCapability, requestBody.OpenAIImageCapability)
	if err != nil {
		return connectionResponse{}, err
	}
	openAIImageCapability = defaultOpenAIImageCapabilityFromOwner(owner.APIFamily, owner.OpenAIImageOperations, openAIImageCapability)
	if err := ensureOpenAIImageCapabilityCoversOwnerOperations(owner.APIFamily, owner.OpenAIImageOperations, openAIImageCapability); err != nil {
		return connectionResponse{}, err
	}
	if err := ensureOpenAIConnectionDimensionsPresent(next.APIFamily, openAITextCapability, openAIImageCapability); err != nil {
		return connectionResponse{}, err
	}
	next.OpenAITextCapability = openAITextCapability
	next.OpenAIImageCapability = openAIImageCapability

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
			return connectionResponse{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
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
		rawHeaders, err := loadConnectionCustomHeadersRaw(ctx, tx, profileID, current.ID)
		if err != nil {
			return connectionResponse{}, err
		}
		headers, err := resolveCustomHeadersWrite(rawHeaders, requestBody.CustomHeaders.Value)
		if err != nil {
			return connectionResponse{}, err
		}
		next.CustomHeaders = headers
	}
	if requestBody.CustomRequestParameters.Set {
		customRequestParameters, err := resolveCustomRequestParametersUpdate(current.CustomRequestParameters, requestBody.CustomRequestParameters)
		if err != nil {
			return connectionResponse{}, err
		}
		next.CustomRequestParameters = customRequestParameters
	}
	if requestBody.RoutingSchedule.Set {
		currentTimezone, currentWindows := routingScheduleConfigFromResponse(current)
		timezone, windows, err := resolveRoutingScheduleUpdate(currentTimezone, currentWindows, requestBody.RoutingSchedule)
		if err != nil {
			return connectionResponse{}, err
		}
		next.RoutingSchedule = routingSchedulePayloadFromRecord(timezone, windows)
	}
	if requestBody.PricingTemplateID.Set {
		if !requestBody.ExpectedConnectionUpdatedAt.Set || !requestBody.ExpectedPricingTemplateID.Set {
			return connectionResponse{}, &domainError{
				StatusCode: http.StatusUnprocessableEntity,
				Detail:     "pricing_template_id updates require expected_connection_updated_at and expected_pricing_template_id",
				Fields:     map[string]any{"pricing_cas_required": []string{"expected_connection_updated_at", "expected_pricing_template_id"}},
			}
		}
		if requestBody.ExpectedConnectionUpdatedAt.Value == nil {
			return connectionResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "Connection updated_at does not match expected_connection_updated_at"}
		}
		currentUpdatedAt, parseErr := time.Parse(time.RFC3339Nano, *requestBody.ExpectedConnectionUpdatedAt.Value)
		if parseErr != nil || !currentUpdatedAt.Equal(current.UpdatedAt) {
			return connectionResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "Connection updated_at does not match expected_connection_updated_at"}
		}
		expectedTemplateID := requestBody.ExpectedPricingTemplateID.Value
		if !equalOptionalInt(expectedTemplateID, current.PricingTemplateID) {
			return connectionResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "Pricing template reference does not match expected_pricing_template_id"}
		}
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

func (s *Service) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	s.writeConnectionMutationRouteError(w, r)
}

func (s *Service) handleDeleteModelConnection(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (deletedConnectionMutationEnvelope, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		owner, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, true)
		if err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		if !found {
			return deletedConnectionMutationEnvelope{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		if err := lockProfileAccessTargetRows(r.Context(), tx, profile.ID); err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		current, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, true, s.now().UTC())
		if err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		if !found {
			return deletedConnectionMutationEnvelope{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		reference, found, err := loadConnectionOwnerReference(r.Context(), tx, profile.ID, owner.ID, current.ID, true)
		if err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		if !found {
			return deletedConnectionMutationEnvelope{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Connection not found for owner model"}
		}
		if err := ensureOwnerConnectionDeleteAllowed(r.Context(), tx, profile.ID, owner, reference.TargetID); err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		if err := deleteModelAccessTargetRow(r.Context(), tx, reference.TargetID); err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		if err := deleteTerminalTarget(r.Context(), tx, current.ID); err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		if err := compactModelAccessTargetPositions(r.Context(), tx, profile.ID, owner.ID, s.nowUTC()); err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		accessTargets, err := loadOwnerMutationAccessTargets(r.Context(), tx, profile.ID, owner.ID)
		if err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		return deletedConnectionMutationEnvelope{Deleted: true, AccessTargets: accessTargets, ConfigurationWarnings: []modelrouting.ConfigurationWarning{}}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
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
		return &DomainError{StatusCode: http.StatusBadRequest, Detail: "enabled models must include at least one enabled access target"}
	}
	return nil
}

func (s *Service) handleRejectModelConnectionLegacyMutation(w http.ResponseWriter, r *http.Request) {
	s.writeConnectionMutationRouteError(w, r)
}

func (s *Service) writeConnectionMutationRouteError(w http.ResponseWriter, r *http.Request) {
	responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, ownerScopedConnectionMutationDetail)
}

func (s *Service) handleListConnectionReferences(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionReferencesResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionReferencesResponse{}, err
		}
		if _, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, false, s.now().UTC()); err != nil {
			return connectionReferencesResponse{}, err
		} else if !found {
			return connectionReferencesResponse{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
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
	responseutil.WriteJSON(w, http.StatusOK, response)
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

func (s *Service) resolveCreateEndpoint(ctx context.Context, tx pgx.Tx, profileID int, endpointID *int, inline *EndpointCreateRequest) (endpointRecord, error) {
	if endpointID != nil {
		endpoint, found, err := loadProfileEndpointRecord(ctx, tx, profileID, *endpointID)
		if err != nil {
			return endpointRecord{}, err
		}
		if !found {
			return endpointRecord{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}
		return endpoint, nil
	}
	if inline != nil {
		return s.createInlineEndpoint(ctx, tx, profileID, endpointCreateRequest{Name: inline.Name, BaseURL: inline.BaseURL, APIKey: inline.APIKey})
	}
	return endpointRecord{}, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
}

func (s *Service) createInlineEndpoint(ctx context.Context, tx pgx.Tx, profileID int, inline endpointCreateRequest) (endpointRecord, error) {
	endpointName := strings.TrimSpace(inline.Name)
	if endpointName == "" {
		return endpointRecord{}, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "endpoint_create.name must not be empty"}
	}
	normalizedURL := endpointdomain.NormalizeBaseURL(inline.BaseURL)
	if warnings := endpointdomain.ValidateBaseURL(normalizedURL); len(warnings) > 0 {
		return endpointRecord{}, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: strings.Join(warnings, "; ")}
	}
	if err := lockProfileRow(ctx, tx, profileID); err != nil {
		return endpointRecord{}, err
	}
	if err := ensureUniqueEndpointName(ctx, tx, profileID, endpointName); err != nil {
		return endpointRecord{}, err
	}
	metadata, err := endpointdomain.BuildSecretMetadata(inline.APIKey, s.secretEncryptionKey, s.nowUTC)
	if err != nil {
		return endpointRecord{}, err
	}
	now := s.nowUTC()
	return insertEndpoint(ctx, tx, endpointRecord{
		ProfileID:         profileID,
		Name:              endpointName,
		BaseURL:           normalizedURL,
		APIKey:            metadata.EncryptedValue,
		APIKeyFingerprint: metadata.Fingerprint,
		APIKeyUpdatedAt:   metadata.KeyUpdatedAt,
		ConfigRevision:    1,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
}

func validateLimiter(fieldName string, value *int) error {
	if value != nil && *value < 1 {
		return &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s must be >= 1 when provided", fieldName)}
	}
	return nil
}

func validateAPIFamily(value string, required bool) (string, error) {
	normalized := providerauth.NormalizeAPIFamily(value)
	if normalized == "" {
		if required {
			return "", &DomainError{StatusCode: http.StatusBadRequest, Detail: "api_family is required"}
		}
		return "", nil
	}
	if !providerauth.IsSupportedAPIFamily(normalized) {
		return "", &DomainError{StatusCode: http.StatusBadRequest, Detail: "api_family must be one of 'openai', 'anthropic', or 'gemini'"}
	}
	return normalized, nil
}

func validateOwnerScopedAPIFamily(value string, ownerAPIFamily string) error {
	apiFamily, err := validateAPIFamily(value, false)
	if err != nil {
		return err
	}
	if apiFamily != "" && !providerauth.SameAPIFamily(apiFamily, ownerAPIFamily) {
		return &DomainError{StatusCode: http.StatusBadRequest, Detail: "api_family must match owner model api_family"}
	}
	return nil
}

func ensureOpenAITextCapabilityMatchesOwnerModes(ownerAPIFamily string, ownerMode *string, capability *string) error {
	if !providerauth.IsOpenAI(ownerAPIFamily) {
		return nil
	}
	if !providerauth.OpenAITextModesMatch(ownerMode, capability) {
		return &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "openai_text_capability must equal the owner model openai_accepted_format"}
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
	normalized := providerauth.NormalizeAPIFamily(*value)
	if normalized == "" || !providerauth.IsSupportedAuthType(normalized) {
		return nil, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "auth_type must be one of 'openai', 'anthropic', 'gemini', or 'gemini_api_key'"}
	}
	return &normalized, nil
}

const (
	openAITextCapabilityResponsesOnly       = "responses_only"
	openAITextCapabilityChatCompletionsOnly = "chat_completions_only"
	openAITextCapabilityDualNative          = "dual_native"
)

// resolveOpenAITextCapabilityCreate no longer requires a text capability on its
// own: an image-only Terminal Target legitimately has none. The joint
// requirement that at least one dimension be present is enforced by
// ensureOpenAIConnectionDimensionsPresent.
func resolveOpenAITextCapabilityCreate(apiFamily string, value *string) (*string, error) {
	return normalizeOpenAITextCapability(apiFamily, value, false)
}

func resolveOpenAITextCapabilityUpdate(previousAPIFamily string, nextAPIFamily string, current *string, update optionalString) (*string, error) {
	if !providerauth.IsOpenAI(nextAPIFamily) {
		if update.Set && update.Value != nil && strings.TrimSpace(*update.Value) != "" {
			return nil, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "openai_text_capability is only supported for OpenAI-family connections"}
		}
		return nil, nil
	}
	if update.Set {
		return normalizeOpenAITextCapability(nextAPIFamily, update.Value, false)
	}
	if providerauth.IsOpenAI(previousAPIFamily) && current != nil && strings.TrimSpace(*current) != "" {
		return normalizeOpenAITextCapability(nextAPIFamily, current, false)
	}
	return nil, nil
}

func normalizeOpenAITextCapability(apiFamily string, value *string, requiredForOpenAI bool) (*string, error) {
	if !providerauth.IsOpenAI(apiFamily) {
		if value != nil && strings.TrimSpace(*value) != "" {
			return nil, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "openai_text_capability is only supported for OpenAI-family connections"}
		}
		return nil, nil
	}
	if value == nil || strings.TrimSpace(*value) == "" {
		if requiredForOpenAI {
			return nil, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "openai_text_capability is required for OpenAI-family connections"}
		}
		return nil, nil
	}
	capability := strings.ToLower(strings.TrimSpace(*value))
	switch capability {
	case openAITextCapabilityResponsesOnly, openAITextCapabilityChatCompletionsOnly, openAITextCapabilityDualNative:
		return &capability, nil
	default:
		return nil, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "openai_text_capability is invalid"}
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
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	return responseutil.SanitizeDecodeError(decoder.Decode(target))
}

func decodeJSONRawBody(request *http.Request) ([]byte, error) {
	defer func() { _ = request.Body.Close() }()
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var connectionErr *DomainError
	if errors.As(err, &connectionErr) {
		responseutil.WriteErrorFields(w, r, corsSnapshot, connectionErr.StatusCode, connectionErr.Detail, connectionErr.Fields)
		return
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	responseutil.WriteError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
	fmt.Fprintf(os.Stderr, "connections writeDomainError unhandled: %v\n", err)
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

func equalOptionalInt(left *int, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
