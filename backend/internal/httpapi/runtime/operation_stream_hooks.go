package runtime

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
	case "response.incomplete":
		return sseTerminalSignalProviderIncomplete
	case "response.completed":
		return sseTerminalSignalCompleted
	}
	switch payloadType, _ := payload["type"].(string); payloadType {
	case "response.incomplete":
		return sseTerminalSignalProviderIncomplete
	case "response.completed":
		return sseTerminalSignalCompleted
	default:
		return sseTerminalSignalNone
	}
}

func classifyAnthropicMessagesStreamTerminal(event string, payload map[string]any) sseTerminalSignal {
	if event == "message_stop" {
		return sseTerminalSignalCompleted
	}
	if payloadType, _ := payload["type"].(string); payloadType == "message_stop" {
		return sseTerminalSignalCompleted
	}
	return sseTerminalSignalNone
}

func classifyGeminiStreamGenerateContentTerminal(_ string, payload map[string]any) sseTerminalSignal {
	if done, _ := payload["done"].(bool); done {
		return sseTerminalSignalCompleted
	}
	if _, hasUsageMetadata := payload["usageMetadata"].(map[string]any); hasUsageMetadata {
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

func mergeAnthropicMessagesStreamUsage(rule runtimeUsageNormalizationRule, usage *responseUsage, event string, payload map[string]any) {
	if isAnthropicMessagesStreamEvent(event, payload, "message_start") {
		if usagePayload, ok := runtimeUsagePayloadFromCarrier(payload, runtimeUsageCarrierMessageUsage); ok {
			usage.mergeRuntimeUsagePayload(rule, runtimeUsageCarrierMessageUsage, usagePayload)
		}
		return
	}
	if isAnthropicMessagesStreamEvent(event, payload, "message_delta") {
		if usagePayload, ok := runtimeUsagePayloadFromCarrier(payload, runtimeUsageCarrierRootUsage); ok {
			usage.mergeAnthropicMessageDeltaUsagePayload(rule, usagePayload)
		}
	}
}

func isAnthropicMessagesStreamEvent(event string, payload map[string]any, eventType string) bool {
	if event == eventType {
		return true
	}
	payloadType, _ := payload["type"].(string)
	return payloadType == eventType
}

func (usage *responseUsage) mergeAnthropicMessageDeltaUsagePayload(rule runtimeUsageNormalizationRule, usagePayload map[string]any) {
	if usage == nil || usage.discarded || !rule.allowsCarrier(runtimeUsageCarrierRootUsage) || usagePayload == nil {
		return
	}
	parsed, ok := parseRuntimeUsagePayload(rule.PayloadShape, usagePayload)
	if !ok {
		return
	}
	parsed = responseUsage{OutputTokens: parsed.OutputTokens, TotalTokens: parsed.TotalTokens}
	if !parsed.hasValues() {
		return
	}
	merged := *usage
	merged.merge(parsed)
	if parsed.OutputTokens != nil && parsed.TotalTokens == nil {
		merged.TotalTokens = nil
	}
	if !merged.validForRuntimeUsage(rule) {
		*usage = responseUsage{discarded: true}
		return
	}
	*usage = merged
}

func mergeGeminiStreamGenerateContentUsage(rule runtimeUsageNormalizationRule, usage *responseUsage, _ string, payload map[string]any) {
	if usageMetadata, ok := runtimeUsagePayloadFromCarrier(payload, runtimeUsageCarrierRootUsageMetadata); ok {
		usage.mergeRuntimeUsagePayload(rule, runtimeUsageCarrierRootUsageMetadata, usageMetadata)
	}
}
