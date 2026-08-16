package openai

import (
	"context"
	"encoding/json"
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
	Operation     provider.Operation
	RawBody       []byte
	TargetModelID string
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

func (adapter Adapter) BuildTextUpstreamRequest(_ context.Context, request TextUpstreamRequest) (provider.UpstreamRequest, error) {
	metadata, ok := TextOperation(request.Operation)
	if !ok {
		return provider.UpstreamRequest{}, &provider.AdapterError{HTTPStatus: http.StatusBadRequest, Code: "openai_text_operation_unsupported", Detail: "OpenAI text operation is unsupported by this adapter."}
	}
	body := rewriteJSONModel(request.RawBody, request.TargetModelID)
	if metadata.Name == OperationChatCompletions && RequestWantsStream(request.Operation, request.RawBody, metadata.NativePath) {
		body = ensureChatCompletionsStreamUsage(body)
	}
	return provider.UpstreamRequest{Method: http.MethodPost, Path: metadata.NativePath, Body: body}, nil
}

func (adapter Adapter) AdaptNonStreamResponse(ctx context.Context, response provider.UpstreamResponse) (provider.ClientResponse, error) {
	return adapter.DefaultAdapter.AdaptNonStreamResponse(ctx, response)
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

// streamOptionsIncludeUsageDefault is the stream_options object Prism injects
// on streaming Chat Completions requests. OpenAI-compatible upstreams emit the
// final usage chunk only when the caller asks for it, so without this every
// streaming request persists NULL tokens and unpriced/MISSING_TOKEN_USAGE.
var streamOptionsIncludeUsageDefault = json.RawMessage(`{"include_usage":true}`)

// ensureChatCompletionsStreamUsage adds stream_options.include_usage=true to a
// streaming Chat Completions body. Caller intent wins: a stream_options object
// that already declares include_usage (any value, including false) is left
// untouched. A missing key and an explicit JSON null are both treated as "no
// opinion" - null is the documented OpenAI default and clients emit it for an
// unset field - so both receive the injected object. A non-object, non-null
// stream_options is passed through for the upstream to reject.
func ensureChatCompletionsStreamUsage(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return body
	}
	existing, present := payload["stream_options"]
	if !present || string(existing) == "null" {
		payload["stream_options"] = streamOptionsIncludeUsageDefault
	} else {
		var options map[string]json.RawMessage
		if err := json.Unmarshal(existing, &options); err != nil || options == nil {
			return body
		}
		if _, declared := options["include_usage"]; declared {
			return body
		}
		options["include_usage"] = json.RawMessage(`true`)
		merged, err := json.Marshal(options)
		if err != nil {
			return body
		}
		payload["stream_options"] = merged
	}
	injected, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return injected
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
	if IsTextOperation(request.Operation) {
		return adapter.BuildTextUpstreamRequest(ctx, TextUpstreamRequest{Operation: request.Operation, RawBody: request.Body, TargetModelID: target.ModelID})
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

func (adapter Adapter) ClassifyOverflow(_ context.Context, response provider.UpstreamResponse) provider.OverflowClassification {
	return ClassifyOverflowResponse(response.StatusCode, response.Body)
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
	return provider.CurrentOperationBehavior{}, false
}
