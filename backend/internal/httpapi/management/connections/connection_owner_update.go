package connections

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

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

func equalOptionalInt(left *int, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
