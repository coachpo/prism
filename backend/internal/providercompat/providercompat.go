package providercompat

import (
	"errors"
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
	DefaultOpenAIProbeEndpointVariant                      = OpenAIProbeEndpointVariantResponsesMinimal
	OpenAIProbeEndpointVariantResponsesMinimal             = "responses_minimal"
	OpenAIProbeEndpointVariantResponsesReasoningNone       = "responses_reasoning_none"
	OpenAIProbeEndpointVariantChatCompletionsMinimal       = "chat_completions_minimal"
	OpenAIProbeEndpointVariantChatCompletionsReasoningNone = "chat_completions_reasoning_none"
)

const (
	OpenAIUpstreamOperationResponses       = "openai.responses"
	OpenAIUpstreamOperationChatCompletions = "openai.chat_completions"
)

var (
	ErrOpenAIProbeEndpointVariantUnsupported = errors.New("openai_probe_endpoint_variant unsupported for api family")
	ErrOpenAIProbeEndpointVariantInvalid     = errors.New("openai_probe_endpoint_variant invalid")
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

type HealthProbeRequest struct {
	Path string
	Body map[string]any
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

func NormalizeConnectionOpenAIProbeEndpointVariant(apiFamily string, value *string) (*string, error) {
	if !IsOpenAI(apiFamily) {
		if value != nil && strings.TrimSpace(*value) != "" {
			return nil, ErrOpenAIProbeEndpointVariantUnsupported
		}
		return nil, nil
	}
	if value == nil || strings.TrimSpace(*value) == "" {
		variant := DefaultOpenAIProbeEndpointVariant
		return &variant, nil
	}
	variant, ok := normalizeOpenAIProbeEndpointVariantValue(*value, false)
	if !ok {
		return nil, ErrOpenAIProbeEndpointVariantInvalid
	}
	return &variant, nil
}

func NormalizeImportedOpenAIProbeEndpointVariant(apiFamily string, value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if !IsOpenAI(apiFamily) {
		return nil, ErrOpenAIProbeEndpointVariantUnsupported
	}
	variant, ok := normalizeOpenAIProbeEndpointVariantValue(*value, true)
	if !ok {
		return nil, ErrOpenAIProbeEndpointVariantInvalid
	}
	return &variant, nil
}

func OpenAIProbeEndpointVariantOrDefault(value *string) string {
	if value != nil && strings.TrimSpace(*value) != "" {
		return strings.TrimSpace(*value)
	}
	return DefaultOpenAIProbeEndpointVariant
}

func DeriveOpenAIUpstreamOperation(apiFamily string, probeEndpointVariant *string) *string {
	if !IsOpenAI(apiFamily) {
		return nil
	}
	operation := OpenAIUpstreamOperationResponses
	switch OpenAIProbeEndpointVariantOrDefault(probeEndpointVariant) {
	case OpenAIProbeEndpointVariantChatCompletionsMinimal, OpenAIProbeEndpointVariantChatCompletionsReasoningNone:
		operation = OpenAIUpstreamOperationChatCompletions
	}
	return &operation
}

func IsSupportedOpenAIUpstreamOperation(value string) bool {
	switch strings.TrimSpace(value) {
	case "", OpenAIUpstreamOperationResponses, OpenAIUpstreamOperationChatCompletions:
		return true
	default:
		return false
	}
}

func BuildHealthProbeRequest(apiFamily string, modelID string, openAIProbeEndpointVariant *string) (HealthProbeRequest, error) {
	switch NormalizeAPIFamily(apiFamily) {
	case APIFamilyOpenAI:
		return buildOpenAIHealthProbeRequest(modelID, OpenAIProbeEndpointVariantOrDefault(openAIProbeEndpointVariant)), nil
	case APIFamilyAnthropic:
		return HealthProbeRequest{Path: "/v1/messages", Body: map[string]any{
			"model":      modelID,
			"max_tokens": 1,
			"messages":   []map[string]any{{"role": "user", "content": "."}},
		}}, nil
	case APIFamilyGemini:
		return HealthProbeRequest{Path: fmt.Sprintf("/v1beta/models/%s:generateContent", modelID), Body: map[string]any{
			"contents":         []map[string]any{{"role": "user", "parts": []map[string]any{{"text": "."}}}},
			"generationConfig": map[string]any{"maxOutputTokens": 1},
		}}, nil
	default:
		return HealthProbeRequest{}, fmt.Errorf("unsupported api_family %q for health check", apiFamily)
	}
}

func buildOpenAIHealthProbeRequest(modelID string, variant string) HealthProbeRequest {
	switch variant {
	case OpenAIProbeEndpointVariantChatCompletionsMinimal, OpenAIProbeEndpointVariantChatCompletionsReasoningNone:
		body := map[string]any{
			"model":      modelID,
			"messages":   []map[string]any{{"role": "user", "content": "."}},
			"max_tokens": 1,
		}
		if variant == OpenAIProbeEndpointVariantChatCompletionsReasoningNone {
			body["reasoning_effort"] = "none"
		}
		return HealthProbeRequest{Path: "/v1/chat/completions", Body: body}
	default:
		body := map[string]any{
			"model":             modelID,
			"input":             []map[string]any{{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "."}}}},
			"max_output_tokens": 1,
		}
		if variant == OpenAIProbeEndpointVariantResponsesReasoningNone {
			body["reasoning"] = map[string]any{"effort": "none"}
		}
		return HealthProbeRequest{Path: "/v1/responses", Body: body}
	}
}

func normalizeOpenAIProbeEndpointVariantValue(value string, caseInsensitive bool) (string, bool) {
	variant := strings.TrimSpace(value)
	if caseInsensitive {
		variant = strings.ToLower(variant)
	}
	switch variant {
	case OpenAIProbeEndpointVariantResponsesMinimal,
		OpenAIProbeEndpointVariantResponsesReasoningNone,
		OpenAIProbeEndpointVariantChatCompletionsMinimal,
		OpenAIProbeEndpointVariantChatCompletionsReasoningNone:
		return variant, true
	default:
		return "", false
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
