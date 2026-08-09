package providerauth

import (
	"fmt"
	"maps"
	"strings"
)

const (
	APIFamilyOpenAI    = "openai"
	APIFamilyAnthropic = "anthropic"
	APIFamilyGemini    = "gemini"
)

const (
	OpenAIUpstreamOperationResponses            = "openai.responses"
	OpenAIUpstreamOperationChatCompletions      = "openai.chat_completions"
	OpenAIUpstreamOperationResponsesInputTokens = "openai.responses.input_tokens"
	OpenAIUpstreamOperationResponsesCompact     = "openai.responses.compact"
	OpenAITextCapabilityResponsesOnly           = "responses_only"
	OpenAITextCapabilityChatCompletionsOnly     = "chat_completions_only"
	OpenAITextCapabilityDualNative              = "dual_native"
	OpenAITextTranslationModeNone               = "none"
	OpenAIWireFormatChatCompletions             = "chat_completions"
	OpenAIWireFormatResponses                   = "responses"
)

var supportedAPIFamilies = map[string]struct{}{
	APIFamilyOpenAI:    {},
	APIFamilyAnthropic: {},
	APIFamilyGemini:    {},
}

type AuthProfile struct {
	AuthHeader   string
	AuthPrefix   string
	ExtraHeaders map[string]string
}

var authProfiles = map[string]AuthProfile{
	APIFamilyOpenAI: {
		AuthHeader:   "Authorization",
		AuthPrefix:   "Bearer ",
		ExtraHeaders: map[string]string{},
	},
	APIFamilyAnthropic: {
		AuthHeader: "x-api-key",
		AuthPrefix: "",
		ExtraHeaders: map[string]string{
			"anthropic-version": "2023-06-01",
		},
	},
	APIFamilyGemini: {
		AuthHeader:   "Authorization",
		AuthPrefix:   "Bearer ",
		ExtraHeaders: map[string]string{},
	},
}

func NormalizeAPIFamily(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func IsSupportedAPIFamily(value string) bool {
	_, ok := supportedAPIFamilies[NormalizeAPIFamily(value)]
	return ok
}

func IsOpenAI(value string) bool {
	return NormalizeAPIFamily(value) == APIFamilyOpenAI
}

func SameAPIFamily(left string, right string) bool {
	normalizedLeft := NormalizeAPIFamily(left)
	return normalizedLeft != "" && normalizedLeft == NormalizeAPIFamily(right)
}

func IsSupportedAuthType(value string) bool {
	return IsSupportedAPIFamily(value)
}

func ResolveAuthProfile(authType *string, apiFamily string) (AuthProfile, error) {
	resolvedKey := NormalizeAPIFamily(apiFamily)
	if authType != nil && strings.TrimSpace(*authType) != "" {
		resolvedKey = NormalizeAPIFamily(*authType)
	}
	profile, ok := authProfiles[resolvedKey]
	if !ok {
		return AuthProfile{}, fmt.Errorf("unsupported auth_type: %s", resolvedKey)
	}
	return profile.clone(), nil
}

func (profile AuthProfile) ControlledHeaderNames() map[string]struct{} {
	controlled := map[string]struct{}{}
	if strings.TrimSpace(profile.AuthHeader) != "" {
		controlled[strings.ToLower(profile.AuthHeader)] = struct{}{}
	}
	for key := range profile.ExtraHeaders {
		if strings.TrimSpace(key) != "" {
			controlled[strings.ToLower(key)] = struct{}{}
		}
	}
	return controlled
}

func (profile AuthProfile) clone() AuthProfile {
	return AuthProfile{
		AuthHeader:   profile.AuthHeader,
		AuthPrefix:   profile.AuthPrefix,
		ExtraHeaders: cloneStringMap(profile.ExtraHeaders),
	}
}

func IsSupportedOpenAITextCapability(value string) bool {
	switch strings.TrimSpace(value) {
	case OpenAITextCapabilityResponsesOnly, OpenAITextCapabilityChatCompletionsOnly, OpenAITextCapabilityDualNative:
		return true
	default:
		return false
	}
}

func OpenAITextCapabilitySupportsNativeOperation(capability string, ingressOperation string) bool {
	switch strings.TrimSpace(ingressOperation) {
	case OpenAIUpstreamOperationChatCompletions:
		return capabilitySupportsChatCompletions(capability)
	case OpenAIUpstreamOperationResponses:
		return capabilitySupportsResponses(capability)
	case OpenAIUpstreamOperationResponsesInputTokens, OpenAIUpstreamOperationResponsesCompact:
		return OpenAITextCapabilitySupportsResponsesAdjunct(capability)
	default:
		return false
	}
}

func OpenAITextCapabilitySupportsResponsesAdjunct(capability string) bool {
	return capabilitySupportsResponses(capability)
}

func OpenAICallerWireFormat(ingressOperation string) (string, bool) {
	switch strings.TrimSpace(ingressOperation) {
	case OpenAIUpstreamOperationChatCompletions:
		return OpenAIWireFormatChatCompletions, true
	case OpenAIUpstreamOperationResponses, OpenAIUpstreamOperationResponsesInputTokens, OpenAIUpstreamOperationResponsesCompact:
		return OpenAIWireFormatResponses, true
	default:
		return "", false
	}
}

func capabilitySupportsResponses(capability string) bool {
	switch strings.TrimSpace(capability) {
	case OpenAITextCapabilityResponsesOnly, OpenAITextCapabilityDualNative:
		return true
	default:
		return false
	}
}

func capabilitySupportsChatCompletions(capability string) bool {
	switch strings.TrimSpace(capability) {
	case OpenAITextCapabilityChatCompletionsOnly, OpenAITextCapabilityDualNative:
		return true
	default:
		return false
	}
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(source))
	maps.Copy(cloned, source)
	return cloned
}
