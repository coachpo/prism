package runtime

import (
	anthropicprovider "github.com/coachpo/prism/backend/internal/gateway/provider/anthropic"
	geminiprovider "github.com/coachpo/prism/backend/internal/gateway/provider/gemini"
)

type operationStreamTerminalClassifier func(string, map[string]any) sseTerminalSignal
type operationStreamUsageMerger func(runtimeUsageNormalizationRule, *responseUsage, string, map[string]any)

type operationStreamHooks struct {
	Provider               string
	Kind                   operationResponseKind
	UsageRule              runtimeUsageNormalizationRule
	CompleteOnDoneSentinel bool
	ClassifyTerminalSignal operationStreamTerminalClassifier
	MergeUsage             operationStreamUsageMerger
}

var operationStreamHooksByCollectionID = map[string]operationStreamHooks{
	"openai.chat_completions": {
		Provider:               "openai",
		Kind:                   operationResponseKindTextGeneration,
		UsageRule:              runtimeUsageRuleOpenAIChatCompletions,
		CompleteOnDoneSentinel: true,
		MergeUsage:             mergeOpenAIChatCompletionsStreamUsage,
	},
	"openai.responses": {
		Provider:               "openai",
		Kind:                   operationResponseKindTextGeneration,
		UsageRule:              runtimeUsageRuleOpenAIResponses,
		ClassifyTerminalSignal: classifyOpenAIResponsesStreamTerminal,
		MergeUsage:             mergeOpenAIResponsesStreamUsage,
	},
	"anthropic.messages": {
		Provider:               "anthropic",
		Kind:                   operationResponseKindTextGeneration,
		UsageRule:              runtimeUsageRuleAnthropicMessages,
		ClassifyTerminalSignal: classifyAnthropicMessagesStreamTerminal,
		MergeUsage:             mergeAnthropicMessagesStreamUsage,
	},
	"gemini.stream_generate_content": {
		Provider:               "gemini",
		Kind:                   operationResponseKindTextGeneration,
		UsageRule:              runtimeUsageRuleGeminiStreamGenerateContent,
		ClassifyTerminalSignal: classifyGeminiStreamGenerateContentTerminal,
		MergeUsage:             mergeGeminiStreamGenerateContentUsage,
	},
}

func streamHooksForOperation(operation RuntimeOperation) (operationStreamHooks, bool) {
	hookCollectionID := operation.HookCollectionID
	if hookCollectionID == "" {
		hookCollectionID = operation.Name
	}
	hooks, ok := operationStreamHooksByCollectionID[hookCollectionID]
	return hooks, ok
}

func streamHooksForProxyResponse(operation RuntimeOperation, isStreamingRequest bool) (operationStreamHooks, bool) {
	if !isStreamingRequest {
		return operationStreamHooks{}, false
	}
	return streamHooksForOperation(operation)
}

func (hooks operationStreamHooks) terminalSignal(event string, payload map[string]any) sseTerminalSignal {
	if hooks.ClassifyTerminalSignal == nil {
		return sseTerminalSignalNone
	}
	return hooks.ClassifyTerminalSignal(event, payload)
}

func (hooks operationStreamHooks) mergeUsage(usage *responseUsage, event string, payload map[string]any) {
	if hooks.MergeUsage == nil || usage == nil {
		return
	}
	hooks.MergeUsage(hooks.UsageRule, usage, event, payload)
}

func classifyOpenAIResponsesStreamTerminal(event string, payload map[string]any) sseTerminalSignal {
	switch event {
	case "response.incomplete", "response.failed":
		return sseTerminalSignalProviderIncomplete
	case "response.completed":
		return sseTerminalSignalCompleted
	}
	switch payloadType, _ := payload["type"].(string); payloadType {
	case "response.incomplete", "response.failed":
		return sseTerminalSignalProviderIncomplete
	case "response.completed":
		return sseTerminalSignalCompleted
	default:
		return sseTerminalSignalNone
	}
}

func classifyAnthropicMessagesStreamTerminal(event string, payload map[string]any) sseTerminalSignal {
	if anthropicprovider.IsMessagesStreamTerminal(event, payload) {
		return sseTerminalSignalCompleted
	}
	return sseTerminalSignalNone
}

func classifyGeminiStreamGenerateContentTerminal(_ string, payload map[string]any) sseTerminalSignal {
	if geminiprovider.IsStreamGenerateContentTerminal(payload) {
		return sseTerminalSignalCompleted
	}
	return sseTerminalSignalNone
}

func mergeOpenAIChatCompletionsStreamUsage(rule runtimeUsageNormalizationRule, usage *responseUsage, _ string, payload map[string]any) {
	if !isOpenAIChatCompletionsFinalUsageChunk(payload) {
		return
	}
	if usagePayload, ok := runtimeUsagePayloadFromCarrier(payload, runtimeUsageCarrierRootUsage); ok {
		usage.mergeRuntimeUsagePayload(rule, runtimeUsageCarrierRootUsage, usagePayload)
	}
}

func isOpenAIChatCompletionsFinalUsageChunk(payload map[string]any) bool {
	if _, ok := runtimeUsagePayloadFromCarrier(payload, runtimeUsageCarrierRootUsage); !ok {
		return false
	}
	choices, ok := payload["choices"].([]any)
	return ok && len(choices) == 0
}

func mergeOpenAIResponsesStreamUsage(rule runtimeUsageNormalizationRule, usage *responseUsage, event string, payload map[string]any) {
	if !isOpenAIResponsesCompletedStreamEvent(event, payload) {
		return
	}
	if usagePayload, ok := runtimeUsagePayloadFromCarrier(payload, runtimeUsageCarrierRootUsage); ok {
		usage.mergeRuntimeUsagePayload(rule, runtimeUsageCarrierRootUsage, usagePayload)
	}
	if usagePayload, ok := runtimeUsagePayloadFromCarrier(payload, runtimeUsageCarrierResponseUsage); ok {
		usage.mergeRuntimeUsagePayload(rule, runtimeUsageCarrierResponseUsage, usagePayload)
	}
}

func isOpenAIResponsesCompletedStreamEvent(event string, payload map[string]any) bool {
	if event == "response.completed" {
		return true
	}
	payloadType, _ := payload["type"].(string)
	return payloadType == "response.completed"
}

func mergeAnthropicMessagesStreamUsage(_ runtimeUsageNormalizationRule, usage *responseUsage, event string, payload map[string]any) {
	if usage == nil {
		return
	}
	merged := anthropicprovider.MergeMessagesStreamUsage(providerUsageEnvelope(*usage), event, payload)
	*usage = responseUsageFromProviderUsageEnvelope(merged)
}

func mergeGeminiStreamGenerateContentUsage(_ runtimeUsageNormalizationRule, usage *responseUsage, _ string, payload map[string]any) {
	if usage == nil {
		return
	}
	merged := geminiprovider.MergeStreamGenerateContentUsage(providerUsageEnvelope(*usage), payload)
	*usage = responseUsageFromProviderUsageEnvelope(merged)
}
