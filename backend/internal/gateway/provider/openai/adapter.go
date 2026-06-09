package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
)

const APIFamily = provider.APIFamilyOpenAI

const (
	OperationChatCompletions      = "openai.chat_completions"
	OperationResponses            = "openai.responses"
	OperationResponsesInputTokens = "openai.responses.input_tokens"
	OperationResponsesCompact     = "openai.responses.compact"
)

type Adapter struct {
	provider.DefaultAdapter
}

type TextOperationMetadata struct {
	Name       string
	NativePath string
	TokenCount bool
}

type TextUpstreamRequest struct {
	Operation       provider.Operation
	RawBody         []byte
	ContentType     string
	RequestPath     string
	TargetModelID   string
	TranslationMode provider.TranslationMode
}

func New(_ ...any) Adapter {
	return Adapter{DefaultAdapter: provider.DefaultAdapter{APIFamilyName: APIFamily}}
}

var _ provider.ProviderAdapter = Adapter{}

func TextOperation(operation provider.Operation) (TextOperationMetadata, bool) {
	switch strings.TrimSpace(operation.Name) {
	case OperationChatCompletions:
		return TextOperationMetadata{Name: OperationChatCompletions, NativePath: "/v1/chat/completions"}, true
	case OperationResponses:
		return TextOperationMetadata{Name: OperationResponses, NativePath: "/v1/responses"}, true
	case OperationResponsesInputTokens:
		return TextOperationMetadata{Name: OperationResponsesInputTokens, NativePath: "/v1/responses/input_tokens", TokenCount: true}, true
	case OperationResponsesCompact:
		return TextOperationMetadata{Name: OperationResponsesCompact, NativePath: "/v1/responses/compact"}, true
	default:
		return TextOperationMetadata{}, false
	}
}

func IsTextOperation(operation provider.Operation) bool {
	_, ok := TextOperation(operation)
	return ok
}

func (adapter Adapter) BuildTextUpstreamRequest(ctx context.Context, request TextUpstreamRequest) (provider.UpstreamRequest, error) {
	metadata, ok := TextOperation(request.Operation)
	if !ok {
		return provider.UpstreamRequest{}, &provider.AdapterError{HTTPStatus: http.StatusBadRequest, Code: "openai_text_operation_unsupported", Detail: "OpenAI text operation is unsupported by this adapter."}
	}
	mode := normalizedTranslationMode(request.TranslationMode)
	if mode != provider.TranslationModeNone {
		if !isConvertibleTextOperation(metadata.Name) {
			return provider.UpstreamRequest{}, unsupportedConversionError(provider.ConversionCapability{Mode: mode, UnsupportedReason: "operation_translation_unsupported", HTTPStatus: http.StatusBadRequest})
		}
		conversion := provider.ConversionRequest{Operation: request.Operation, RawBody: request.RawBody, Mode: mode, TargetModelID: request.TargetModelID}
		capability, err := adapter.ConversionCapability(ctx, conversion)
		if err != nil {
			return provider.UpstreamRequest{}, err
		}
		if !capability.RequestSupported || !capability.ResponseSupported || !capability.StreamSupported {
			return provider.UpstreamRequest{}, unsupportedConversionError(capability)
		}
		translated, err := adapter.TranslateRequest(ctx, conversion)
		if err != nil {
			return provider.UpstreamRequest{}, err
		}
		return provider.UpstreamRequest{Method: http.MethodPost, Path: translated.Path, Body: translated.Body}, nil
	}
	body := rewriteJSONModel(request.RawBody, request.TargetModelID)
	return provider.UpstreamRequest{Method: http.MethodPost, Path: metadata.NativePath, Body: body}, nil
}

func (adapter Adapter) AdaptNonStreamResponse(ctx context.Context, response provider.UpstreamResponse) (provider.ClientResponse, error) {
	mode := normalizedTranslationMode(response.TranslationMode)
	if mode == provider.TranslationModeNone {
		return adapter.DefaultAdapter.AdaptNonStreamResponse(ctx, response)
	}
	translated, err := adapter.TranslateResponse(ctx, provider.ConversionRequest{
		Operation:        response.Operation,
		RawBody:          response.Body,
		Mode:             mode,
		RequestedModelID: response.RequestedModelID,
	})
	if err != nil {
		return provider.ClientResponse{}, err
	}
	return provider.ClientResponse{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: translated.Body, Usage: translated.Usage}, nil
}

func normalizedTranslationMode(mode provider.TranslationMode) provider.TranslationMode {
	if strings.TrimSpace(string(mode)) == "" {
		return provider.TranslationModeNone
	}
	return mode
}

func isConvertibleTextOperation(operationName string) bool {
	switch strings.TrimSpace(operationName) {
	case OperationChatCompletions, OperationResponses:
		return true
	default:
		return false
	}
}

func unsupportedConversionError(capability provider.ConversionCapability) error {
	reason := strings.TrimSpace(capability.UnsupportedReason)
	if reason == "" {
		reason = "unsupported_request_shape"
	}
	status := capability.HTTPStatus
	if status == 0 {
		status = http.StatusBadRequest
	}
	return &provider.AdapterError{HTTPStatus: status, Code: "openai_request_translation_unsupported", Detail: "Prism cannot translate this OpenAI request shape for the selected target.", Fields: map[string]any{"translation_mode": string(capability.Mode), "unsupported_reason": reason}}
}

func rewriteJSONModel(rawBody []byte, targetModelID string) []byte {
	if len(rawBody) == 0 || strings.TrimSpace(targetModelID) == "" {
		return append([]byte(nil), rawBody...)
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return append([]byte(nil), rawBody...)
	}
	payload["model"] = targetModelID
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return append([]byte(nil), rawBody...)
	}
	return rewritten
}

func (adapter Adapter) ParseRequest(_ context.Context, envelope provider.RequestEnvelope) (provider.ProviderRequest, error) {
	return provider.ProviderRequest{
		Operation:   envelope.Operation,
		Body:        append([]byte(nil), envelope.RawBody...),
		ContentType: envelope.ContentType,
		NativePath:  envelope.RequestPath,
		WantsStream: RequestWantsStream(envelope.Operation, envelope.RawBody, envelope.RequestPath),
		Metadata:    map[string]string{},
	}, nil
}

func RequestWantsStream(operation provider.Operation, rawBody []byte, requestPath string) bool {
	if _, ok := ImageOperation(operation); ok {
		return false
	}
	if metadata, ok := TextOperation(operation); ok && metadata.TokenCount {
		return false
	}
	if operation.Streaming {
		return true
	}
	return requestBodyWantsStream(rawBody, requestPath)
}

func requestBodyWantsStream(rawBody []byte, _ string) bool {
	payload := map[string]any{}
	decoder := json.NewDecoder(strings.NewReader(string(rawBody)))
	if err := decoder.Decode(&payload); err != nil {
		return false
	}
	stream, _ := payload["stream"].(bool)
	return stream
}

func (adapter Adapter) BuildUpstreamRequest(ctx context.Context, request provider.ProviderRequest, target provider.UpstreamTarget) (provider.UpstreamRequest, error) {
	if IsImageOperation(request.Operation) {
		return adapter.BuildImageUpstreamRequest(ctx, ImageUpstreamRequest{Operation: request.Operation, RawBody: request.Body, ContentType: request.ContentType, TargetModelID: target.ModelID})
	}
	if IsTextOperation(request.Operation) {
		return adapter.BuildTextUpstreamRequest(ctx, TextUpstreamRequest{Operation: request.Operation, RawBody: request.Body, ContentType: request.ContentType, RequestPath: request.NativePath, TargetModelID: target.ModelID})
	}
	return provider.UpstreamRequest{}, &provider.AdapterError{HTTPStatus: http.StatusBadRequest, Code: "openai_operation_unsupported", Detail: "OpenAI operation is unsupported by this adapter."}
}

func (adapter Adapter) ExtractUsage(_ context.Context, response provider.UpstreamResponse) (provider.UsageEnvelope, error) {
	usage, _ := extractResponseUsageByOperation(response.Operation, response.Body)
	return usage, nil
}

func (adapter Adapter) EstimateTokens(context.Context, provider.ProviderRequest) (provider.TokenEstimate, error) {
	return provider.TokenEstimate{}, nil
}

func (adapter Adapter) ConversionCapability(_ context.Context, request provider.ConversionRequest) (provider.ConversionCapability, error) {
	mode := normalizedTranslationMode(request.Mode)
	capability := provider.ConversionCapability{Mode: mode, RequestSupported: true, ResponseSupported: true, StreamSupported: true, HTTPStatus: http.StatusBadRequest, UpstreamOperationName: upstreamOperationName(request.Operation, mode)}
	if mode == provider.TranslationModeNone {
		return capability, nil
	}
	payload, err := decodeOpenAITranslationPayload(request.RawBody, mode)
	if err != nil {
		capability.RequestSupported = false
		capability.UnsupportedReason = requestTranslationUnsupportedReason(err, "invalid_translation_payload")
		return capability, nil
	}
	if RequestWantsStream(request.Operation, request.RawBody, "") {
		switch mode {
		case provider.TranslationModeOpenAIResponsesToChatCompletions:
			if responsesTranslationStreamUsesTools(payload) {
				capability.StreamSupported = false
				capability.UnsupportedReason = "responses_stream_tools"
				return capability, nil
			}
		case provider.TranslationModeOpenAIChatCompletionsToResponses:
			if chatTranslationStreamUsesTools(payload) {
				capability.StreamSupported = false
				capability.UnsupportedReason = "chat_stream_tools"
				return capability, nil
			}
		}
	}
	if _, _, err := translateRequest(request.RawBody, mode, "translation-preview-model"); err != nil {
		capability.RequestSupported = false
		capability.UnsupportedReason = requestTranslationUnsupportedReason(err, "invalid_translation_payload")
	}
	return capability, nil
}

func (adapter Adapter) TranslateRequest(_ context.Context, request provider.ConversionRequest) (provider.TranslatedRequest, error) {
	path, body, err := translateRequest(request.RawBody, normalizedTranslationMode(request.Mode), request.TargetModelID)
	if err != nil {
		return provider.TranslatedRequest{}, err
	}
	return provider.TranslatedRequest{Path: path, Body: body}, nil
}

func (adapter Adapter) TranslateResponse(_ context.Context, request provider.ConversionRequest) (provider.TranslatedResponse, error) {
	body, usage, rule, err := translateResponse(request.RawBody, normalizedTranslationMode(request.Mode), request.RequestedModelID)
	if err != nil {
		return provider.TranslatedResponse{}, err
	}
	usage.NormalizationRule = rule
	return provider.TranslatedResponse{Body: body, Usage: usage}, nil
}

func (adapter Adapter) ClassifyOverflow(_ context.Context, response provider.UpstreamResponse) provider.OverflowClassification {
	return ClassifyOverflowResponse(response.StatusCode, response.Body, response.TranslationMode)
}

func (adapter Adapter) CurrentBehavior(_ context.Context, operation provider.Operation) (provider.CurrentOperationBehavior, bool) {
	behavior := provider.CurrentOperationBehavior{OperationName: operation.Name, APIFamily: APIFamily, HookCollectionID: operation.HookCollectionID, HasRequest: true, HasResponse: true}
	if metadata, ok := TextOperation(operation); ok {
		behavior.Request = provider.RequestHookBehavior{Provider: APIFamily, HasStreamDetector: true}
		kind := "text_generation"
		if metadata.TokenCount {
			kind = "token_count"
		}
		behavior.Response = provider.ResponseHookBehavior{Provider: APIFamily, Kind: kind, HasNonStreamParser: true, UsageRule: metadata.Name}
		behavior.HasStream = metadata.Name == OperationChatCompletions || metadata.Name == OperationResponses
		if behavior.HasStream {
			behavior.Stream = provider.StreamHookBehavior{Provider: APIFamily, Kind: kind, UsageRule: metadata.Name, CompleteOnDoneSentinel: true, HasTerminalClassifier: true, HasUsageMerger: true}
		}
		return behavior, true
	}
	if _, ok := ImageOperation(operation); ok {
		behavior.Response = provider.ResponseHookBehavior{Provider: APIFamily, Kind: "media", HasNonStreamParser: true}
		behavior.Media = provider.MediaHookBehavior{Provider: APIFamily, Kind: "media", HasModelExtractor: true, HasModelRewriter: true}
		behavior.HasMedia = true
		return behavior, true
	}
	return provider.CurrentOperationBehavior{}, false
}

func upstreamOperationName(operation provider.Operation, mode provider.TranslationMode) string {
	switch mode {
	case provider.TranslationModeOpenAIResponsesToChatCompletions:
		return OperationChatCompletions
	case provider.TranslationModeOpenAIChatCompletionsToResponses:
		return OperationResponses
	default:
		return operation.Name
	}
}

func requestTranslationUnsupportedReason(err error, fallback string) string {
	var adapterErr *provider.AdapterError
	if errors.As(err, &adapterErr) && adapterErr != nil {
		if reason := stringValue(adapterErr.Fields["unsupported_reason"]); strings.TrimSpace(reason) != "" {
			return reason
		}
	}
	return fallback
}

func chatTranslationStreamUsesTools(payload map[string]any) bool {
	if fieldHasValue(payload, "tools") || fieldHasValue(payload, "tool_choice") {
		return true
	}
	messages, _ := payload["messages"].([]any)
	for _, rawMessage := range messages {
		message, _ := rawMessage.(map[string]any)
		if fieldHasValue(message, "tool_calls") || fieldHasValue(message, "function_call") || fieldHasValue(message, "tool_call_id") {
			return true
		}
		if strings.TrimSpace(stringValue(message["role"])) == "tool" {
			return true
		}
	}
	return false
}

func responsesTranslationStreamUsesTools(payload map[string]any) bool {
	if fieldHasValue(payload, "tools") || fieldHasValue(payload, "tool_choice") {
		return true
	}
	return responsesInputContainsFunctionItems(payload["input"])
}

func responsesInputContainsFunctionItems(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		switch strings.TrimSpace(stringValue(item["type"])) {
		case "function_call", "function_call_output":
			return true
		}
	}
	return false
}
