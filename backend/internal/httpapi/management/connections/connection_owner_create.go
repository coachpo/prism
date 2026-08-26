package connections

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

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
