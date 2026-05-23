package runtime

type bufferedRequestGenerationParamsExtractor func(map[string]any, *requestGenerationParams)
type operationRequestStreamDetector func([]byte, string) bool

type operationRequestHooks struct {
	Provider                             string
	ExtractBufferedGenerationParams      bufferedRequestGenerationParamsExtractor
	NewGenerationParamsStreamingObserver func() *geminiGenerationParamsStreamingObserver
	RequestWantsStream                   operationRequestStreamDetector
}

var operationRequestHooksByCollectionID = map[string]operationRequestHooks{
	"openai.chat_completions": {
		Provider:                        "openai",
		ExtractBufferedGenerationParams: extractOpenAIChatGenerationParams,
		RequestWantsStream:              requestBodyStreamDetector,
	},
	"openai.responses": {
		Provider:                        "openai",
		ExtractBufferedGenerationParams: extractOpenAIResponsesGenerationParams,
		RequestWantsStream:              requestBodyStreamDetector,
	},
	runtimeHookCollectionOpenAIImagesGeneration: {
		Provider:           "openai",
		RequestWantsStream: neverStreamRequest,
	},
	runtimeHookCollectionOpenAIImagesEdit: {
		Provider:           "openai",
		RequestWantsStream: neverStreamRequest,
	},
	"anthropic.messages": {
		Provider:                        "anthropic",
		ExtractBufferedGenerationParams: extractAnthropicGenerationParams,
		RequestWantsStream:              requestBodyStreamDetector,
	},
	"gemini.generate_content": {
		Provider:                        "gemini",
		ExtractBufferedGenerationParams: extractGeminiGenerationParams,
		RequestWantsStream:              neverStreamRequest,
	},
	"gemini.stream_generate_content": {
		Provider:                        "gemini",
		ExtractBufferedGenerationParams: extractGeminiGenerationParams,
		NewGenerationParamsStreamingObserver: func() *geminiGenerationParamsStreamingObserver {
			return newGeminiGenerationParamsStreamingObserver()
		},
		RequestWantsStream: alwaysStreamRequest,
	},
	runtimeHookCollectionAnthropicCountTokens: {
		Provider:           "anthropic",
		RequestWantsStream: neverStreamRequest,
	},
	runtimeHookCollectionGeminiCountTokens: {
		Provider:           "gemini",
		RequestWantsStream: neverStreamRequest,
	},
}

func requestHooksForOperation(operation RuntimeOperation) (operationRequestHooks, bool) {
	hookCollectionID := operation.HookCollectionID
	if hookCollectionID == "" {
		hookCollectionID = operation.Name
	}
	hooks, ok := operationRequestHooksByCollectionID[hookCollectionID]
	return hooks, ok
}

func requestWantsStreamForOperation(operation RuntimeOperation, rawBody []byte, requestPath string) bool {
	hooks, ok := requestHooksForOperation(operation)
	if !ok || hooks.RequestWantsStream == nil {
		return requestWantsStream(rawBody, requestPath)
	}
	return hooks.RequestWantsStream(rawBody, requestPath)
}

func requestBodyStreamDetector(rawBody []byte, _ string) bool {
	return requestBodyWantsStream(rawBody)
}

func alwaysStreamRequest(_ []byte, _ string) bool {
	return true
}

func neverStreamRequest(_ []byte, _ string) bool {
	return false
}

func newRequestGenerationParamsStreamingObserver(operation RuntimeOperation) (*geminiGenerationParamsStreamingObserver, bool) {
	hooks, ok := requestHooksForOperation(operation)
	if !ok || hooks.NewGenerationParamsStreamingObserver == nil {
		return nil, false
	}
	return hooks.NewGenerationParamsStreamingObserver(), true
}
