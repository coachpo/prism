package runtime

import "github.com/coachpo/prism/backend/internal/gateway/provider"

func responseUsageFromProviderUsageEnvelope(usage provider.UsageEnvelope) responseUsage {
	return responseUsage{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		TotalTokens:              usage.TotalTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		ReasoningTokens:          usage.ReasoningTokens,
		discarded:                usage.NormalizationRejected,
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
		NormalizationRejected:    usage.discarded,
	}
}
