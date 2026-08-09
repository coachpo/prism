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
// group; Chat Completions is its own group.
const (
	OpenAIOperationGroupChatCompletions = "chat_completions"
	OpenAIOperationGroupResponses       = "responses"
)

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

// OpenAIOperationGroup maps an OpenAI operation name to its user-visible group.
func OpenAIOperationGroup(operation string) string {
	switch strings.TrimSpace(operation) {
	case providerauth.OpenAIUpstreamOperationChatCompletions:
		return OpenAIOperationGroupChatCompletions
	case providerauth.OpenAIUpstreamOperationResponses, providerauth.OpenAIUpstreamOperationResponsesInputTokens, providerauth.OpenAIUpstreamOperationResponsesCompact:
		return OpenAIOperationGroupResponses
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
