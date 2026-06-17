package runtime

import (
	"context"
	"strings"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
)

func translateOpenAIRequest(rawBody []byte, mode TranslationMode, targetModelID string) (string, []byte, error) {
	path, body, _, err := translateOpenAIRequestWithLoss(rawBody, mode, targetModelID)
	return path, body, err
}

func translateOpenAIRequestWithLoss(rawBody []byte, mode TranslationMode, targetModelID string) (string, []byte, *runtimeTranslationLossDecision, error) {
	if mode == TranslationModeNone {
		return "", nil, nil, nil
	}
	adapter := openai.New()
	translated, err := adapter.TranslateRequest(context.Background(), provider.ConversionRequest{RawBody: rawBody, Mode: providerTranslationMode(mode), TargetModelID: targetModelID})
	if err != nil {
		if domainErr := domainErrorFromProviderAdapterError(err); domainErr != nil {
			return "", nil, nil, domainErr
		}
		return "", nil, nil, err
	}
	return translated.Path, translated.Body, runtimeTranslationLossDecisionFromProvider(translated.TranslationLoss, mode), nil
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(stringValue(value)); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func fieldHasValue(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	return valueHasMeaning(value)
}

func valueHasMeaning(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}
