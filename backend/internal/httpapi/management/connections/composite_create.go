package connections

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

// OwnerConnectionCreateResult is the redacted, secret-free result of creating
// a model-owned Terminal Target. It carries identity and configuration counts
// only; endpoint keys, header values and custom request parameter values never
// leave the store layer.
type OwnerConnectionCreateResult struct {
	ConnectionID                int
	Position                    int
	Name                        *string
	EndpointID                  int
	IsActive                    bool
	OpenAITextCapability        *string
	OpenAIImageCapability       *string
	PricingTemplateID           *int
	QPSLimit                    *int
	MaxInFlightNonStream        *int
	MaxInFlightStream           *int
	CustomHeaderCount           int
	CustomRequestParameterCount int
}

// OwnerScopedConnectionCreateInput is the validated input for creating a
// model-owned Terminal Target inside an existing transaction (used by the
// model composite-create flow). It mirrors the owner-scoped connection create
// request without any HTTP dependencies.
type OwnerScopedConnectionCreateInput struct {
	OwnerModelID               int
	OwnerAPIFamily             string
	OwnerOpenAIAcceptedFormat  *string
	OwnerOpenAIImageOperations *string
	APIFamily                  string
	EndpointID                 *int
	EndpointCreate             *EndpointCreateRequest
	Name                       *string
	IsActive                   *bool
	AuthType                   *string
	CustomHeaders              map[string]string
	CustomRequestParameters    CustomRequestParametersInput
	OpenAITextCapability       *string
	OpenAIImageCapability      *string
	PricingTemplateID          *int
	QPSLimit                   *int
	MaxInFlightNonStream       *int
	MaxInFlightStream          *int
}

// CreateOwnerScopedConnectionTx creates a private Connection plus its owner
// Access Target inside the caller's transaction. The caller is responsible for
// the transaction lifecycle (lock ordering, commit/rollback, planning
// invalidation). All validations mirror the owner-scoped create route.
func (s *Service) CreateOwnerScopedConnectionTx(ctx context.Context, tx pgx.Tx, profileID int, input OwnerScopedConnectionCreateInput) (OwnerConnectionCreateResult, error) {
	owner := modelRecord{
		ID:                    input.OwnerModelID,
		ProfileID:             profileID,
		APIFamily:             input.OwnerAPIFamily,
		IsEnabled:             true,
		OpenAIAcceptedFormat:  input.OwnerOpenAIAcceptedFormat,
		OpenAIImageOperations: input.OwnerOpenAIImageOperations,
	}
	if err := validateOwnerScopedAPIFamily(input.APIFamily, owner.APIFamily); err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	if input.EndpointID != nil && input.EndpointCreate != nil {
		return OwnerConnectionCreateResult{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
	}
	if input.EndpointID == nil && input.EndpointCreate == nil {
		return OwnerConnectionCreateResult{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
	}
	authType, err := validateAuthType(input.AuthType)
	if err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	if err := validateLimiter("qps_limit", input.QPSLimit); err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	if err := validateLimiter("max_in_flight_non_stream", input.MaxInFlightNonStream); err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	if err := validateLimiter("max_in_flight_stream", input.MaxInFlightStream); err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	openAITextCapability, err := resolveOpenAITextCapabilityCreate(owner.APIFamily, input.OpenAITextCapability)
	if err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	if err := ensureOpenAITextCapabilityMatchesOwnerModes(owner.APIFamily, owner.OpenAIAcceptedFormat, openAITextCapability); err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	openAIImageCapability, err := resolveOpenAIImageCapabilityCreate(owner.APIFamily, input.OpenAIImageCapability)
	openAIImageCapability = defaultOpenAIImageCapabilityFromOwner(owner.APIFamily, owner.OpenAIImageOperations, openAIImageCapability)
	if err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	if err := ensureOpenAIImageCapabilityCoversOwnerOperations(owner.APIFamily, owner.OpenAIImageOperations, openAIImageCapability); err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	if err := ensureOpenAIConnectionDimensionsPresent(owner.APIFamily, openAITextCapability, openAIImageCapability); err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	pricingTemplateID, err := validatePricingTemplateID(ctx, tx, profileID, input.PricingTemplateID)
	if err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	customRequestParameters, err := resolveCustomRequestParametersCreate(optionalCustomRequestParameters{Set: input.CustomRequestParameters.Set, Raw: input.CustomRequestParameters.Raw})
	if err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	endpoint, err := s.resolveCreateEndpoint(ctx, tx, profileID, input.EndpointID, input.EndpointCreate)
	if err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	position, err := nextModelAccessTargetPosition(ctx, tx, profileID, owner.ID)
	if err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	now := s.nowUTC()
	item := connectionResponse{
		ProfileID:               profileID,
		APIFamily:               owner.APIFamily,
		EndpointID:              endpoint.ID,
		IsActive:                resolvedBool(input.IsActive, true),
		Priority:                position,
		Name:                    normalizeOptionalString(input.Name),
		AuthType:                authType,
		CustomHeaders:           normalizeHeaders(input.CustomHeaders),
		CustomRequestParameters: customRequestParameters,
		OpenAITextCapability:    openAITextCapability,
		OpenAIImageCapability:   openAIImageCapability,
		PricingTemplateID:       pricingTemplateID,
		QPSLimit:                input.QPSLimit,
		MaxInFlightNonStream:    input.MaxInFlightNonStream,
		MaxInFlightStream:       input.MaxInFlightStream,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	connectionID, err := insertTerminalTarget(ctx, tx, terminalTargetRecordFromConnectionResponse(item))
	if err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	if err := insertOwnerTerminalTargetAccess(ctx, tx, profileID, owner.ID, connectionID, position, now); err != nil {
		return OwnerConnectionCreateResult{}, err
	}
	return OwnerConnectionCreateResult{
		ConnectionID:                connectionID,
		Position:                    position,
		Name:                        item.Name,
		EndpointID:                  endpoint.ID,
		IsActive:                    item.IsActive,
		OpenAITextCapability:        openAITextCapability,
		OpenAIImageCapability:       openAIImageCapability,
		PricingTemplateID:           pricingTemplateID,
		QPSLimit:                    input.QPSLimit,
		MaxInFlightNonStream:        input.MaxInFlightNonStream,
		MaxInFlightStream:           input.MaxInFlightStream,
		CustomHeaderCount:           len(item.CustomHeaders),
		CustomRequestParameterCount: customRequestParameterCount(customRequestParameters),
	}, nil
}

func customRequestParameterCount(parameters *terminaltarget.CustomRequestParameters) int {
	if parameters == nil {
		return 0
	}
	return parameters.TopLevelKeyCount()
}
