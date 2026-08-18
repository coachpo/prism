package connections

import (
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

// Authoring rules for the OpenAI image dimension of a Terminal Target.
//
// The image dimension is independent of openai_text_capability: a Terminal
// Target may serve text only, images only, or both, and an OpenAI target that
// declares neither serves nothing. That joint requirement is what replaces the
// old "text capability is required for OpenAI" rule, and it mirrors
// ck_connections_openai_dimensions.
//
// Unlike the text dimension, which requires strict equality with the owner
// model, the image dimension requires containment: the target must serve every
// image operation the owner accepts, and may serve more.

func resolveOpenAIImageCapabilityCreate(apiFamily string, value *string) (*string, error) {
	return normalizeOpenAIImageCapability(apiFamily, value)
}

func resolveOpenAIImageCapabilityUpdate(previousAPIFamily string, nextAPIFamily string, current *string, update optionalString) (*string, error) {
	if !providerauth.IsOpenAI(nextAPIFamily) {
		if update.Set && update.Value != nil && strings.TrimSpace(*update.Value) != "" {
			return nil, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "openai_image_capability is only supported for OpenAI-family connections"}
		}
		return nil, nil
	}
	if update.Set {
		return normalizeOpenAIImageCapability(nextAPIFamily, update.Value)
	}
	if providerauth.IsOpenAI(previousAPIFamily) {
		return normalizeOpenAIImageCapability(nextAPIFamily, current)
	}
	return nil, nil
}

func normalizeOpenAIImageCapability(apiFamily string, value *string) (*string, error) {
	if !providerauth.IsOpenAI(apiFamily) {
		if value != nil && strings.TrimSpace(*value) != "" {
			return nil, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "openai_image_capability is only supported for OpenAI-family connections"}
		}
		return nil, nil
	}
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	capability := strings.ToLower(strings.TrimSpace(*value))
	if !providerauth.IsSupportedOpenAIImageCapability(capability) {
		return nil, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "openai_image_capability is invalid"}
	}
	return &capability, nil
}

// ensureOpenAIConnectionDimensionsPresent rejects an OpenAI Terminal Target
// that declares neither dimension. Such a target could serve no operation at
// all, and the database constraint rejects it as well.
func ensureOpenAIConnectionDimensionsPresent(apiFamily string, textCapability *string, imageCapability *string) error {
	if !providerauth.IsOpenAI(apiFamily) {
		return nil
	}
	if textCapability == nil && imageCapability == nil {
		return &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "at least one of openai_text_capability or openai_image_capability is required for OpenAI-family connections"}
	}
	return nil
}

// defaultOpenAIImageCapabilityFromOwner fills an omitted image capability from
// the owner model's own image dimension. Authors who never mention images get a
// Terminal Target that exactly serves what the owner accepts; an explicit value
// may still widen it, and the coverage check below rejects a narrowing one.
func defaultOpenAIImageCapabilityFromOwner(ownerAPIFamily string, ownerImageOperations *string, resolved *string) *string {
	if !providerauth.IsOpenAI(ownerAPIFamily) || resolved != nil {
		return resolved
	}
	if ownerImageOperations == nil || strings.TrimSpace(*ownerImageOperations) == "" {
		return nil
	}
	inherited := strings.TrimSpace(*ownerImageOperations)
	return &inherited
}

// ensureOpenAIImageCapabilityCoversOwnerOperations rejects a Terminal Target
// that would leave one of the owner model's accepted image operations unserved.
// An owner that accepts no image operation imposes no requirement.
func ensureOpenAIImageCapabilityCoversOwnerOperations(ownerAPIFamily string, ownerImageOperations *string, capability *string) error {
	if !providerauth.IsOpenAI(ownerAPIFamily) {
		return nil
	}
	if ownerImageOperations == nil || strings.TrimSpace(*ownerImageOperations) == "" {
		return nil
	}
	if !providerauth.OpenAIImageCapabilitiesCover(ownerImageOperations, capability) {
		return &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "openai_image_capability must serve every image operation the owner model accepts"}
	}
	return nil
}

// openAIImageOperationsServedByCapability reports whether a destination model's
// accepted image operations are all served by a capability being copied onto
// it. It is the copy-time counterpart of the create/update check.
func openAIImageOperationsServedByCapability(destinationImageOperations string, capability string) bool {
	return providerauth.OpenAIImageCapabilityCovers(destinationImageOperations, capability)
}

// openAIImageUncoveredIssueCode re-exports the routing issue code so HTTP
// handlers in this package can report image coverage failures with the same
// identity the routing domain uses.
const openAIImageUncoveredIssueCode = modelrouting.OpenAIImageUncoveredIssueCode

func ensureOpenAITextCapabilityMatchesOwnerModes(ownerAPIFamily string, ownerMode *string, capability *string) error {
	if !providerauth.IsOpenAI(ownerAPIFamily) {
		return nil
	}
	if !providerauth.OpenAITextModesMatch(ownerMode, capability) {
		return &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "openai_text_capability must equal the owner model openai_accepted_format"}
	}
	return nil
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
