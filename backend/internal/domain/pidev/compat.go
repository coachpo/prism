package pidev

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	APIOpenAICompletions = "openai-completions"
	APIOpenAIResponses   = "openai-responses"
	APIAnthropicMessages = "anthropic-messages"
	APIGoogleGenerative  = "google-generative-ai"
)

// CompatValidationError reports one Pi 0.84.3 compat leaf that is invalid for
// the model's concrete API. Path is relative to compat.
type CompatValidationError struct {
	Path   string
	Reason string
}

func (e *CompatValidationError) Error() string {
	return fmt.Sprintf("compat.%s %s", e.Path, e.Reason)
}

type compatKind uint8

const (
	compatBool compatKind = iota + 1
	compatMaxTokensField
	compatThinkingFormat
	compatThinkingTokenBudgetField
	compatCacheControlFormat
	compatDeferredToolsMode
	compatSessionAffinityFormat
	compatChatTemplateRecord
)

// compatSchemas is deliberately API-specific. It mirrors the safe, request-
// shape compatibility fields Pi 0.84.3 consumes while excluding every field
// that can select an upstream provider or fallback model.
var compatSchemas = map[string]map[string]compatKind{
	APIOpenAICompletions: {
		"supportsStore": compatBool, "supportsDeveloperRole": compatBool,
		"supportsReasoningEffort": compatBool, "supportsUsageInStreaming": compatBool,
		"supportsFinishReason": compatBool, "requiresToolResultName": compatBool,
		"requiresAssistantAfterToolResult": compatBool, "requiresThinkingAsText": compatBool,
		"requiresReasoningContentOnAssistantMessages": compatBool, "zaiToolStream": compatBool,
		"supportsThinkingTokenBudget": compatBool, "supportsOpenAIGrammarTools": compatBool,
		"supportsStrictMode": compatBool, "sendSessionAffinityHeaders": compatBool,
		"supportsLongCacheRetention": compatBool,
		"maxTokensField":             compatMaxTokensField, "thinkingFormat": compatThinkingFormat,
		"thinkingTokenBudgetField": compatThinkingTokenBudgetField,
		"cacheControlFormat":       compatCacheControlFormat, "deferredToolsMode": compatDeferredToolsMode,
		"sessionAffinityFormat": compatSessionAffinityFormat,
		"chatTemplateKwargs":    compatChatTemplateRecord, "chatTemplateArgs": compatChatTemplateRecord,
	},
	APIOpenAIResponses: {
		"supportsDeveloperRole": compatBool, "supportsLongCacheRetention": compatBool,
		"supportsStrictMode": compatBool, "supportsOpenAIGrammarTools": compatBool,
		"supportsAdditionalTools": compatBool, "supportsToolSearch": compatBool,
		"supportsExplicitPromptCacheMode": compatBool,
		"sessionAffinityFormat":           compatSessionAffinityFormat,
	},
	APIAnthropicMessages: {
		"supportsEagerToolInputStreaming": compatBool, "supportsLongCacheRetention": compatBool,
		"sendSessionAffinityHeaders": compatBool, "supportsCacheControlOnTools": compatBool,
		"supportsTemperature": compatBool, "forceAdaptiveThinking": compatBool,
		"allowEmptySignature": compatBool, "supportsStrictTools": compatBool,
		"supportsToolReferences": compatBool,
	},
	APIGoogleGenerative: {},
}

var unsafeCompatFields = map[string]struct{}{
	"allowedFallbackModels": {},
	"openRouterRouting":     {},
	"vercelGatewayRouting":  {},
}

var sensitiveKeySubstrings = []string{
	"apikey", "authorization", "authtoken", "proxykey", "secret", "password",
	"passwd", "credential", "cookie", "sessionkey", "accesskey", "privatekey",
	"bearer", "signature", "satoken", "clientsecret", "accesstoken",
	"sessiontoken", "refreshtoken", "idtoken",
}

// KeyLooksSensitive reports whether a JSON object key is credential-shaped.
// Catalog ingest and the final renderer share this exact guard so an allowed
// compat container can never persist a value that render later rejects.
func KeyLooksSensitive(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return true
	}
	var compactBuilder strings.Builder
	compactBuilder.Grow(len(lower))
	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			compactBuilder.WriteRune(r)
		}
	}
	compact := compactBuilder.String()
	for _, needle := range sensitiveKeySubstrings {
		if strings.Contains(lower, needle) || strings.Contains(compact, needle) {
			return true
		}
	}
	return false
}

// AllowedCompatFields returns a stable copy of the exact safe field allowlist
// for one Pi API. An unknown API has no allowed compat fields.
func AllowedCompatFields(api string) []string {
	schema := compatSchemas[api]
	fields := make([]string, 0, len(schema))
	for field := range schema {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

// SanitizeCompat validates and retains only fields allowed for api. Unknown,
// cross-API, routing, and fallback fields are dropped and returned as sorted
// full paths so callers can persist/display honest source evidence.
func SanitizeCompat(api string, value map[string]any) (map[string]any, []string, error) {
	if value == nil {
		return nil, nil, nil
	}
	clean := make(map[string]any, len(value))
	dropped := make([]string, 0)
	for _, field := range sortedMapKeys(value) {
		kind, allowed := compatSchemas[api][field]
		if !allowed {
			dropped = append(dropped, "compat."+field)
			continue
		}
		if err := validateCompatValue(field, value[field], kind); err != nil {
			return nil, nil, err
		}
		clean[field] = value[field]
	}
	if len(clean) == 0 {
		clean = nil
	}
	return clean, dropped, nil
}

// ValidateCompat applies the same API-specific schema as SanitizeCompat but
// fails closed on every unknown, cross-API, routing, or fallback key. Binding
// overrides use this path, so they cannot persist a value ingest would drop.
func ValidateCompat(api string, value map[string]any) error {
	for _, field := range sortedMapKeys(value) {
		kind, allowed := compatSchemas[api][field]
		if !allowed {
			reason := fmt.Sprintf("is not allowed for Pi API %q", api)
			if _, unsafe := unsafeCompatFields[field]; unsafe {
				reason = "is a provider routing or fallback directive and is intentionally excluded"
			}
			return &CompatValidationError{Path: field, Reason: reason}
		}
		if err := validateCompatValue(field, value[field], kind); err != nil {
			return err
		}
	}
	return nil
}

func validateCompatValue(field string, value any, kind compatKind) error {
	invalid := func(reason string) error { return &CompatValidationError{Path: field, Reason: reason} }
	switch kind {
	case compatBool:
		if _, ok := value.(bool); !ok {
			return invalid("must be a boolean")
		}
	case compatMaxTokensField:
		if !oneOfString(value, "max_completion_tokens", "max_tokens") {
			return invalid("has an unsupported value")
		}
	case compatThinkingFormat:
		if !oneOfString(value, "openai", "openrouter", "together", "baseten", "deepseek", "zai", "qwen", "chat-template", "qwen-chat-template", "string-thinking", "ant-ling") {
			return invalid("has an unsupported value")
		}
	case compatThinkingTokenBudgetField:
		if !oneOfString(value, "thinking_token_budget", "thinking_budget", "thinking_budget_tokens") {
			return invalid("has an unsupported value")
		}
	case compatCacheControlFormat:
		if !oneOfString(value, "anthropic") {
			return invalid("has an unsupported value")
		}
	case compatDeferredToolsMode:
		if !oneOfString(value, "kimi") {
			return invalid("has an unsupported value")
		}
	case compatSessionAffinityFormat:
		if !oneOfString(value, "openai", "openai-nosession", "openrouter") {
			return invalid("has an unsupported value")
		}
	case compatChatTemplateRecord:
		if err := validateChatTemplateRecord(field, value); err != nil {
			return err
		}
	default:
		return invalid("has no validator")
	}
	return nil
}

func validateChatTemplateRecord(field string, value any) error {
	record, ok := value.(map[string]any)
	if !ok {
		return &CompatValidationError{Path: field, Reason: "must be an object"}
	}
	for _, key := range sortedMapKeys(record) {
		path := field + "." + key
		if KeyLooksSensitive(key) {
			return &CompatValidationError{Path: path, Reason: "looks like credential material and is not allowed"}
		}
		item := record[key]
		switch typed := item.(type) {
		case nil, string, bool:
			continue
		case json.Number, float64, float32, int, int32, int64, uint, uint32, uint64:
			if !finiteNumber(typed) {
				return &CompatValidationError{Path: path, Reason: "must be a finite number"}
			}
		case map[string]any:
			for _, objectKey := range sortedMapKeys(typed) {
				if objectKey != "$var" && objectKey != "omitWhenOff" {
					return &CompatValidationError{Path: path + "." + objectKey, Reason: "is not supported"}
				}
			}
			if !oneOfString(typed["$var"], "thinking.enabled", "thinking.effort", "thinking.budget") {
				return &CompatValidationError{Path: path + ".$var", Reason: "has an unsupported value"}
			}
			if omit, present := typed["omitWhenOff"]; present {
				if _, ok := omit.(bool); !ok {
					return &CompatValidationError{Path: path + ".omitWhenOff", Reason: "must be a boolean"}
				}
			}
		default:
			return &CompatValidationError{Path: path, Reason: "must be a scalar or Pi thinking variable"}
		}
	}
	return nil
}

func finiteNumber(value any) bool {
	var number float64
	var err error
	switch typed := value.(type) {
	case json.Number:
		number, err = typed.Float64()
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	default:
		return true
	}
	return err == nil && !math.IsInf(number, 0) && !math.IsNaN(number)
}

func oneOfString(value any, allowed ...string) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, candidate := range allowed {
		if text == candidate {
			return true
		}
	}
	return false
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
