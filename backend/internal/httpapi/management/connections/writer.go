package connections

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

// InlineEndpointCreate is the HTTP-neutral inline endpoint payload used by the
// shared owner-scoped connection writer. It never leaves the transaction.
type InlineEndpointCreate struct {
	Name    string
	BaseURL string
	APIKey  string
}

// OwnerModel is the HTTP-neutral owner-model identity consumed by the shared
// owner-scoped connection writer. It carries only what writes and warnings
// need; the full model record stays with the caller.
type OwnerModel struct {
	ID                    int
	ProfileID             int
	ModelID               string
	APIFamily             string
	OpenAIAcceptedFormat  *string
	OpenAIImageOperations *string
}

// OwnerConnectionCreateInput is the HTTP-neutral input for creating an
// owner-scoped Terminal Target inside a caller-owned transaction. Endpoint
// selection is XOR: exactly one of EndpointID or EndpointCreate must be set.
type OwnerConnectionCreateInput struct {
	EndpointID     *int
	EndpointCreate *InlineEndpointCreate
	IsActive       bool
	Name           *string
	AuthType       *string
	// UpstreamModelID is presence-aware: an explicit JSON null is a 422 (the
	// identity can never be absent), a provided value is validated (trim only,
	// non-blank, at most 200 Unicode characters), and omission defaults to the
	// owner model's current model_id before any write.
	UpstreamModelID         optionalString
	CustomHeaders           map[string]string
	CustomRequestParameters *terminaltarget.CustomRequestParameters
	RoutingSchedule         RoutingScheduleInput
	OpenAITextCapability    *string
	OpenAIImageCapability   *string
	PricingTemplateID       *int
	QPSLimit                *int
	MaxInFlightNonStream    *int
	MaxInFlightStream       *int
}

// CreateOwnerConnection validates and inserts an endpoint (existing or inline,
// with encrypted API key), a model-private Connection and its enabled owner
// Access Target, all inside the caller's transaction. It returns the created
// connection record, the assigned stage position, the owner edge id and the
// owner-scoped configuration warnings computed on the proposed state.
//
// The caller owns the transaction: on any error the whole composite create
// rolls back and no intermediate entity is observable.
func CreateOwnerConnection(ctx context.Context, tx pgx.Tx, profileID int, owner OwnerModel, secretEncryptionKey string, now func() time.Time, input OwnerConnectionCreateInput) (connectionResponse, int, int, []modelrouting.ConfigurationWarning, error) {
	if (input.EndpointID == nil) == (input.EndpointCreate == nil) {
		return connectionResponse{}, 0, 0, nil, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
	}
	upstreamModelID, err := resolveUpstreamModelIDCreate(owner.ModelID, input.UpstreamModelID)
	if err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	authType, err := validateAuthType(input.AuthType)
	if err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	if err := validateLimiter("qps_limit", input.QPSLimit); err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	if err := validateLimiter("max_in_flight_non_stream", input.MaxInFlightNonStream); err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	if err := validateLimiter("max_in_flight_stream", input.MaxInFlightStream); err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	// Both HTTP-neutral create chains resolve the schedule through this shared
	// helper. CreateOwnerScopedConnectionTx does not call this function, so it
	// repeats the call rather than inheriting it.
	routingScheduleTimezone, routingWindows, err := resolveRoutingScheduleCreate(input.RoutingSchedule)
	if err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	openAITextCapability, err := resolveOpenAITextCapabilityCreate(owner.APIFamily, input.OpenAITextCapability)
	if err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	if err := ensureOpenAITextCapabilityMatchesOwnerModes(owner.APIFamily, owner.OpenAIAcceptedFormat, openAITextCapability); err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	openAIImageCapability, err := resolveOpenAIImageCapabilityCreate(owner.APIFamily, input.OpenAIImageCapability)
	openAIImageCapability = defaultOpenAIImageCapabilityFromOwner(owner.APIFamily, owner.OpenAIImageOperations, openAIImageCapability)
	if err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	if err := ensureOpenAIImageCapabilityCoversOwnerOperations(owner.APIFamily, owner.OpenAIImageOperations, openAIImageCapability); err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	if err := ensureOpenAIConnectionDimensionsPresent(owner.APIFamily, openAITextCapability, openAIImageCapability); err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	pricingTemplateID, err := validatePricingTemplateID(ctx, tx, profileID, input.PricingTemplateID)
	if err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	endpoint, err := resolveWriterEndpoint(ctx, tx, profileID, input.EndpointID, input.EndpointCreate, secretEncryptionKey, now)
	if err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	position, err := nextModelAccessTargetPosition(ctx, tx, profileID, owner.ID)
	if err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	nowUTC := now().UTC()
	customHeaders, err := mustResolveCustomHeadersWrite(input.CustomHeaders)
	if err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	item := connectionResponse{
		ProfileID:               profileID,
		APIFamily:               owner.APIFamily,
		EndpointID:              endpoint.ID,
		IsActive:                input.IsActive,
		Priority:                position,
		Name:                    normalizeOptionalString(input.Name),
		AuthType:                authType,
		UpstreamModelID:         &upstreamModelID,
		CustomHeaders:           customHeaders,
		CustomRequestParameters: input.CustomRequestParameters,
		RoutingSchedule:         routingSchedulePayloadFromRecord(routingScheduleTimezone, routingWindows),
		OpenAITextCapability:    openAITextCapability,
		OpenAIImageCapability:   openAIImageCapability,
		PricingTemplateID:       pricingTemplateID,
		QPSLimit:                input.QPSLimit,
		MaxInFlightNonStream:    input.MaxInFlightNonStream,
		MaxInFlightStream:       input.MaxInFlightStream,
		CreatedAt:               nowUTC,
		UpdatedAt:               nowUTC,
	}
	connectionID, err := insertTerminalTarget(ctx, tx, terminalTargetRecordFromConnectionResponse(item))
	if err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	if err := replaceConnectionRoutingWindows(ctx, tx, profileID, connectionID, routingWindows, nowUTC); err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	accessTargetID, err := insertOwnerTerminalTargetAccessReturningID(ctx, tx, profileID, owner.ID, connectionID, position, nowUTC)
	if err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	created, found, err := loadModelConnectionRecord(ctx, tx, profileID, owner.ID, connectionID, nowUTC)
	if err != nil {
		return connectionResponse{}, 0, 0, nil, err
	}
	if !found {
		return connectionResponse{}, 0, 0, nil, &DomainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
	}
	warnings := ownerScopedConnectionWarnings(owner, openAITextCapability, openAIImageCapability, accessTargetID, connectionID)
	return created, position, accessTargetID, warnings, nil
}

// ownerScopedConnectionWarnings computes direct coverage warnings for a
// connection against its owner model's accepted operation set.
// ownerScopedConnectionWarnings classifies both dimensions at once so a single
// warning lists every accepted operation the target leaves unserved, whether it
// is a text or an image operation.
func ownerScopedConnectionWarnings(owner OwnerModel, capability *string, imageCapability *string, accessTargetID int, connectionID int) []modelrouting.ConfigurationWarning {
	if !providerauth.IsOpenAI(owner.APIFamily) {
		return nil
	}
	if owner.OpenAIAcceptedFormat == nil && owner.OpenAIImageOperations == nil {
		return nil
	}
	if capability == nil && imageCapability == nil {
		return nil
	}
	path := "openai_text_capability"
	if owner.OpenAIAcceptedFormat == nil {
		path = "openai_image_capability"
	}
	return modelrouting.GenerateOpenAIWarningsForTargetDimensions(owner.OpenAIAcceptedFormat, owner.OpenAIImageOperations, capability, imageCapability, path, owner.ID, accessTargetID, connectionID)
}

func resolveWriterEndpoint(ctx context.Context, tx pgx.Tx, profileID int, endpointID *int, inline *InlineEndpointCreate, secretEncryptionKey string, now func() time.Time) (endpointRecord, error) {
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
		return insertWriterInlineEndpoint(ctx, tx, profileID, *inline, secretEncryptionKey, now)
	}
	return endpointRecord{}, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
}

func insertWriterInlineEndpoint(ctx context.Context, tx pgx.Tx, profileID int, inline InlineEndpointCreate, secretEncryptionKey string, now func() time.Time) (endpointRecord, error) {
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
	metadata, err := endpointdomain.BuildSecretMetadata(inline.APIKey, secretEncryptionKey, now)
	if err != nil {
		return endpointRecord{}, err
	}
	nowUTC := now().UTC()
	return insertEndpoint(ctx, tx, endpointRecord{
		ProfileID:         profileID,
		Name:              endpointName,
		BaseURL:           normalizedURL,
		APIKey:            metadata.EncryptedValue,
		APIKeyFingerprint: metadata.Fingerprint,
		APIKeyUpdatedAt:   metadata.KeyUpdatedAt,
		ConfigRevision:    1,
		CreatedAt:         nowUTC,
		UpdatedAt:         nowUTC,
	})
}
