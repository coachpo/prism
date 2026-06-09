package runtime

import (
	"strings"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
)

func responseUsageFromProviderUsageEnvelope(usage provider.UsageEnvelope) responseUsage {
	return responseUsage{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		TotalTokens:              usage.TotalTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		ReasoningTokens:          usage.ReasoningTokens,
	}
}

func responseUsageFromProviderEnvelope(envelope provider.UsageEnvelope) (responseUsage, runtimeUsageNormalizationRule) {
	return responseUsage{
		InputTokens:              envelope.InputTokens,
		OutputTokens:             envelope.OutputTokens,
		TotalTokens:              envelope.TotalTokens,
		CacheReadInputTokens:     envelope.CacheReadInputTokens,
		CacheCreationInputTokens: envelope.CacheCreationInputTokens,
		ReasoningTokens:          envelope.ReasoningTokens,
	}, runtimeUsageRuleByName(envelope.NormalizationRule)
}

func runtimeUsageRuleByName(name string) runtimeUsageNormalizationRule {
	switch strings.TrimSpace(name) {
	case runtimeUsageRuleName(runtimeUsageRuleOpenAIChatCompletions):
		return runtimeUsageRuleOpenAIChatCompletions
	case runtimeUsageRuleName(runtimeUsageRuleOpenAIResponses):
		return runtimeUsageRuleOpenAIResponses
	case runtimeUsageRuleName(runtimeUsageRuleAnthropicMessages):
		return runtimeUsageRuleAnthropicMessages
	default:
		return runtimeUsageNormalizationRule{}
	}
}

func providerUsageEnvelope(usage responseUsage) provider.UsageEnvelope {
	return provider.UsageEnvelope{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		TotalTokens:              usage.TotalTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		ReasoningTokens:          usage.ReasoningTokens,
	}
}

func runtimeUsageRuleName(rule runtimeUsageNormalizationRule) string {
	if strings.TrimSpace(rule.OperationName) != "" {
		return rule.OperationName
	}
	if strings.TrimSpace(rule.Provider) != "" && strings.TrimSpace(string(rule.PayloadShape)) != "" {
		return rule.Provider + ":" + string(rule.PayloadShape)
	}
	return ""
}

func providerTranslationMode(mode TranslationMode) provider.TranslationMode {
	switch mode {
	case TranslationModeOpenAIResponsesToChatCompletions:
		return provider.TranslationModeOpenAIResponsesToChatCompletions
	case TranslationModeOpenAIChatCompletionsToResponses:
		return provider.TranslationModeOpenAIChatCompletionsToResponses
	case TranslationModeNone, "":
		return provider.TranslationModeNone
	default:
		return provider.TranslationMode(mode)
	}
}
