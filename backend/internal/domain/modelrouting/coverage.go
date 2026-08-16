package modelrouting

import (
	"strings"

	"github.com/coachpo/prism/backend/internal/providerauth"
)

// Coverage classifies the relationship between a model's accepted operation
// set and a terminal target's supported operation set.
type Coverage string

const (
	CoverageFull          Coverage = "full"
	CoveragePartial       Coverage = "partial"
	CoverageNone          Coverage = "none"
	CoverageNotApplicable Coverage = "not_applicable"
)

// OpenAI operation groups used by diagnostics and the UI. The Responses group
// merges the three Responses-family registered operations into one visible
// group; Chat Completions is its own group. The two image groups map one to one
// onto their operations because generations and edits are authorized
// independently of each other.
const (
	OpenAIOperationGroupChatCompletions   = "chat_completions"
	OpenAIOperationGroupResponses         = "responses"
	OpenAIOperationGroupImagesGenerations = "images_generations"
	OpenAIOperationGroupImagesEdits       = "images_edits"
)

// OpenAIOperationGroupOrder is the stable display order of every OpenAI
// operation group. Summaries iterate it so text groups keep their existing
// position and image groups append after them.
func OpenAIOperationGroupOrder() []string {
	return []string{
		OpenAIOperationGroupChatCompletions,
		OpenAIOperationGroupResponses,
		OpenAIOperationGroupImagesGenerations,
		OpenAIOperationGroupImagesEdits,
	}
}

// OpenAIAcceptedOperationSet returns the canonical model-bound operation set
// for an OpenAI accepted format, using the runtime operation registry names.
// The empty/unknown format falls back to dual_native (the canonical default).
func OpenAIAcceptedOperationSet(acceptedFormat string) []string {
	switch strings.TrimSpace(acceptedFormat) {
	case providerauth.OpenAITextCapabilityChatCompletionsOnly:
		return []string{providerauth.OpenAIUpstreamOperationChatCompletions}
	case providerauth.OpenAITextCapabilityResponsesOnly:
		return responsesOperationSet()
	default:
		return append([]string{providerauth.OpenAIUpstreamOperationChatCompletions}, responsesOperationSet()...)
	}
}

// OpenAITargetSupportedOperationSet returns the operations a Terminal Target
// capability can natively serve. Responses-only and dual both serve the whole
// Responses family group; chat-only serves Chat Completions alone.
func OpenAITargetSupportedOperationSet(targetCapability string) []string {
	switch strings.TrimSpace(targetCapability) {
	case providerauth.OpenAITextCapabilityChatCompletionsOnly:
		return []string{providerauth.OpenAIUpstreamOperationChatCompletions}
	case providerauth.OpenAITextCapabilityResponsesOnly:
		return responsesOperationSet()
	default:
		return append([]string{providerauth.OpenAIUpstreamOperationChatCompletions}, responsesOperationSet()...)
	}
}

func responsesOperationSet() []string {
	return []string{
		providerauth.OpenAIUpstreamOperationResponses,
		providerauth.OpenAIUpstreamOperationResponsesInputTokens,
		providerauth.OpenAIUpstreamOperationResponsesCompact,
	}
}

// OpenAIImageAcceptedOperationSet returns the model-bound image operation set
// for an authored image dimension value. Unlike the text set it has no
// dual_native fallback: an absent or unrecognized image dimension accepts no
// image operation, because a model that never declared image support must not
// be reported as accepting one.
func OpenAIImageAcceptedOperationSet(imageOperations string) []string {
	return providerauth.OpenAIImageCapabilityOperations(imageOperations)
}

// OpenAIImageTargetSupportedOperationSet returns the image operations a
// Terminal Target capability can serve.
func OpenAIImageTargetSupportedOperationSet(targetImageCapability string) []string {
	return providerauth.OpenAIImageCapabilityOperations(targetImageCapability)
}

// OpenAIAcceptedOperationSetForDimensions is the optional-value composite used
// by every model call site. It is the only correct way to read a model's
// accepted set now that openai_accepted_format is nullable: an absent text mode
// contributes no text operation instead of falling back to dual_native, which
// would otherwise report a pure image model as accepting Chat Completions and
// Responses.
func OpenAIAcceptedOperationSetForDimensions(acceptedFormat *string, imageOperations *string) []string {
	operations := make([]string, 0, 6)
	if acceptedFormat != nil && strings.TrimSpace(*acceptedFormat) != "" {
		operations = append(operations, OpenAIAcceptedOperationSet(*acceptedFormat)...)
	}
	if imageOperations != nil && strings.TrimSpace(*imageOperations) != "" {
		operations = append(operations, OpenAIImageAcceptedOperationSet(*imageOperations)...)
	}
	return operations
}

// OpenAITargetSupportedOperationSetForDimensions is the Terminal Target
// counterpart of OpenAIAcceptedOperationSetForDimensions.
func OpenAITargetSupportedOperationSetForDimensions(textCapability *string, imageCapability *string) []string {
	operations := make([]string, 0, 6)
	if textCapability != nil && strings.TrimSpace(*textCapability) != "" {
		operations = append(operations, OpenAITargetSupportedOperationSet(*textCapability)...)
	}
	if imageCapability != nil && strings.TrimSpace(*imageCapability) != "" {
		operations = append(operations, OpenAIImageTargetSupportedOperationSet(*imageCapability)...)
	}
	return operations
}

// OpenAIRegisteredOperationList returns every registered OpenAI model-bound
// operation across both dimensions. Diagnostics analyze this full list so
// root-unaccepted rows stay visible; ClassifyOpenAICoverage is then run against
// the model's actual accepted subset.
func OpenAIRegisteredOperationList() []string {
	operations := OpenAIAcceptedOperationSet(providerauth.OpenAITextCapabilityDualNative)
	return append(operations, OpenAIImageAcceptedOperationSet(providerauth.OpenAIImageCapabilityGenerationsAndEdits)...)
}

// ClassifyOpenAICoverage computes the directional coverage of a terminal
// target against a model accepted operation set:
//
//	FULL    iff accepted ⊆ supported
//	PARTIAL iff accepted ∩ supported ≠ ∅ and accepted ⊄ supported
//	NONE    iff accepted ∩ supported = ∅
//
// It returns the coverage class, the accepted operations the target supports,
// and the accepted operations the target does not support.
func ClassifyOpenAICoverage(acceptedOperations []string, targetSupportedOperations []string) (Coverage, []string, []string) {
	acceptedSet := operationSet(acceptedOperations)
	supportedSet := operationSet(targetSupportedOperations)
	supportedAccepted := make([]string, 0, len(acceptedSet))
	unsupportedAccepted := make([]string, 0, len(acceptedSet))
	for _, operation := range acceptedOperations {
		trimmed := strings.TrimSpace(operation)
		if trimmed == "" {
			continue
		}
		if _, ok := supportedSet[trimmed]; ok {
			supportedAccepted = append(supportedAccepted, trimmed)
		} else {
			unsupportedAccepted = append(unsupportedAccepted, trimmed)
		}
	}
	if len(acceptedSet) == 0 {
		return CoverageNotApplicable, supportedAccepted, unsupportedAccepted
	}
	if len(supportedAccepted) == 0 {
		return CoverageNone, supportedAccepted, unsupportedAccepted
	}
	if len(unsupportedAccepted) == 0 {
		return CoverageFull, supportedAccepted, unsupportedAccepted
	}
	return CoveragePartial, supportedAccepted, unsupportedAccepted
}

// OpenAIFormatSupportsOperation reports whether an accepted format natively
// supports an operation (reuses the shared providerauth helper).
func OpenAIFormatSupportsOperation(acceptedFormat string, operation string) bool {
	return providerauth.OpenAITextCapabilitySupportsNativeOperation(acceptedFormat, operation)
}

// IsOpenAIImageOperation reports whether an operation belongs to the image
// dimension and must therefore be judged against the image capability rather
// than the text one.
func IsOpenAIImageOperation(operation string) bool {
	return providerauth.IsOpenAIImageOperation(operation)
}

// OpenAIImageSupportsOperation reports whether an image dimension value serves
// an image operation (reuses the shared providerauth helper).
func OpenAIImageSupportsOperation(imageCapability string, operation string) bool {
	return providerauth.OpenAIImageCapabilitySupportsOperation(imageCapability, operation)
}

// OpenAIOperationGroup maps an OpenAI operation name to its user-visible group.
func OpenAIOperationGroup(operation string) string {
	switch strings.TrimSpace(operation) {
	case providerauth.OpenAIUpstreamOperationChatCompletions:
		return OpenAIOperationGroupChatCompletions
	case providerauth.OpenAIUpstreamOperationResponses, providerauth.OpenAIUpstreamOperationResponsesInputTokens, providerauth.OpenAIUpstreamOperationResponsesCompact:
		return OpenAIOperationGroupResponses
	case providerauth.OpenAIUpstreamOperationImagesGenerations:
		return OpenAIOperationGroupImagesGenerations
	case providerauth.OpenAIUpstreamOperationImagesEdits:
		return OpenAIOperationGroupImagesEdits
	default:
		return ""
	}
}

func operationSet(operations []string) map[string]struct{} {
	set := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		if trimmed := strings.TrimSpace(operation); trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	return set
}
