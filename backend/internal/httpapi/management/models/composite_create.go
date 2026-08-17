package models

import (
	"context"
	"fmt"
	"net/http"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	"github.com/coachpo/prism/backend/internal/providerauth"
	"github.com/jackc/pgx/v5"
)

func validateInitialTerminalTargetShape(initial *modelInitialTerminalTargetRequest) error {
	if initial == nil {
		return nil
	}
	if initial.EndpointID != nil && initial.EndpointCreate != nil {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
	}
	if initial.EndpointID == nil && initial.EndpointCreate == nil {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
	}
	return nil
}

func resolveCompositeModelEnabled(explicit *bool, hasInitialTarget bool) bool {
	if explicit != nil {
		return *explicit
	}
	return hasInitialTarget
}

func resolveCompositeCapability(owner modelRecord, initial *modelInitialTerminalTargetRequest) (*string, error) {
	if !providerauth.IsOpenAI(owner.APIFamily) {
		return nil, nil
	}
	capability := owner.OpenAIAcceptedFormat
	if initial.OpenAITextCapability != nil {
		capability = initial.OpenAITextCapability
	}
	if !providerauth.OpenAITextModesMatch(owner.OpenAIAcceptedFormat, capability) {
		return nil, routingPlanValidationIssueErrorWithStatus(http.StatusUnprocessableEntity, modelrouting.OpenAITextModeMismatchIssueCode, "initial_terminal_target.openai_text_capability", "openai_text_capability must equal the owner model openai_accepted_format")
	}
	return capability, nil
}

// resolveCompositeImageCapability defaults the initial Terminal Target's image
// capability to the owner model's own image dimension. An explicit value may
// widen it but never narrow it, because a target that serves fewer image
// operations than the owner accepts would leave those operations unroutable.
func resolveCompositeImageCapability(owner modelRecord, initial *modelInitialTerminalTargetRequest) (*string, error) {
	if !providerauth.IsOpenAI(owner.APIFamily) {
		return nil, nil
	}
	capability := owner.OpenAIImageOperations
	if initial.OpenAIImageCapability != nil {
		capability = initial.OpenAIImageCapability
	}
	if owner.OpenAIImageOperations != nil && !providerauth.OpenAIImageCapabilitiesCover(owner.OpenAIImageOperations, capability) {
		return nil, routingPlanValidationIssueErrorWithStatus(http.StatusUnprocessableEntity, modelrouting.OpenAIImageUncoveredIssueCode, "initial_terminal_target.openai_image_capability", "openai_image_capability must serve every image operation the owner model accepts")
	}
	return capability, nil
}

func compositeEndpointCreate(input *modelEndpointCreateRequest) *connections.EndpointCreateRequest {
	if input == nil {
		return nil
	}
	return &connections.EndpointCreateRequest{Name: input.Name, BaseURL: input.BaseURL, APIKey: input.APIKey}
}

func lockProfileRowForModel(ctx context.Context, tx pgx.Tx, profileID int) error {
	if err := tx.QueryRow(ctx, `SELECT id FROM profiles WHERE id = $1 FOR UPDATE`, profileID).Scan(new(int)); err != nil {
		return fmt.Errorf("lock profile %d: %w", profileID, err)
	}
	return nil
}

func resolvedBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
