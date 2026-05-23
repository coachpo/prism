package runtime

type operationStreamTerminalClassifier func(string, map[string]any) sseTerminalSignal
type operationStreamUsageMerger func(*responseUsage, map[string]any)

type operationStreamHooks struct {
	Provider               string
	Kind                   operationResponseKind
	CompleteOnDoneSentinel bool
	ClassifyTerminalSignal operationStreamTerminalClassifier
	MergeUsage             operationStreamUsageMerger
}

var operationStreamHooksByCollectionID = map[string]operationStreamHooks{
	"openai.chat_completions": {
		Provider:               "openai",
		Kind:                   operationResponseKindTextGeneration,
		CompleteOnDoneSentinel: true,
		MergeUsage:             mergeOpenAIChatCompletionsStreamUsage,
	},
	"openai.responses": {
		Provider:               "openai",
		Kind:                   operationResponseKindTextGeneration,
		ClassifyTerminalSignal: classifyOpenAIResponsesStreamTerminal,
		MergeUsage:             mergeOpenAIResponsesStreamUsage,
	},
	"anthropic.messages": {
		Provider:               "anthropic",
		Kind:                   operationResponseKindTextGeneration,
		ClassifyTerminalSignal: classifyAnthropicMessagesStreamTerminal,
		MergeUsage:             mergeAnthropicMessagesStreamUsage,
	},
	"gemini.stream_generate_content": {
		Provider:               "gemini",
		Kind:                   operationResponseKindTextGeneration,
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

func (hooks operationStreamHooks) mergeUsage(usage *responseUsage, payload map[string]any) {
	if hooks.MergeUsage == nil || usage == nil {
		return
	}
	hooks.MergeUsage(usage, payload)
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

func mergeOpenAIChatCompletionsStreamUsage(usage *responseUsage, payload map[string]any) {
	if usagePayload, ok := payload["usage"].(map[string]any); ok {
		usage.mergeStandardUsagePayload(usagePayload)
	}
}

func mergeOpenAIResponsesStreamUsage(usage *responseUsage, payload map[string]any) {
	if usagePayload, ok := responseUsagePayload(payload); ok {
		usage.mergeStandardUsagePayload(usagePayload)
	}
}

func mergeAnthropicMessagesStreamUsage(usage *responseUsage, payload map[string]any) {
	if usagePayload, ok := payload["usage"].(map[string]any); ok {
		usage.mergeStandardUsagePayload(usagePayload)
	}
	if messagePayload, ok := payload["message"].(map[string]any); ok {
		if usagePayload, ok := messagePayload["usage"].(map[string]any); ok {
			usage.mergeStandardUsagePayload(usagePayload)
		}
	}
}

func mergeGeminiStreamGenerateContentUsage(usage *responseUsage, payload map[string]any) {
	if usageMetadata, ok := payload["usageMetadata"].(map[string]any); ok {
		usage.mergeGeminiUsagePayload(usageMetadata)
	}
}
