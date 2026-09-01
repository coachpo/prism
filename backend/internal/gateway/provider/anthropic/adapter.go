package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
)

const APIFamily = provider.APIFamilyAnthropic

const (
	OperationMessages    = "anthropic.messages"
	OperationCountTokens = "anthropic.count_tokens"
)

type Adapter struct {
	provider.DefaultAdapter
}

type OperationMetadata struct {
	Name       string
	NativePath string
	TokenCount bool
}

type MessagesUpstreamRequest struct {
	Operation       provider.Operation
	RawBody         []byte
	ContentType     string
	RequestPath     string
	UpstreamModelID string
	Header          http.Header
}

const (
	StreamTerminalNone      = ""
	StreamTerminalCompleted = "completed"
)

func New(_ ...any) Adapter {
	return Adapter{DefaultAdapter: provider.DefaultAdapter{APIFamilyName: APIFamily}}
}

var _ provider.ProviderAdapter = Adapter{}

func Operation(operation provider.Operation) (OperationMetadata, bool) {
	switch strings.TrimSpace(operation.Name) {
	case OperationMessages:
		return OperationMetadata{Name: OperationMessages, NativePath: "/v1/messages"}, true
	case OperationCountTokens:
		return OperationMetadata{Name: OperationCountTokens, NativePath: "/v1/messages/count_tokens", TokenCount: true}, true
	default:
		return OperationMetadata{}, false
	}
}

func IsOperation(operation provider.Operation) bool {
	_, ok := Operation(operation)
	return ok
}

func (adapter Adapter) BuildUpstreamRequest(ctx context.Context, request provider.ProviderRequest, target provider.UpstreamTarget) (provider.UpstreamRequest, error) {
	return adapter.BuildMessagesUpstreamRequest(ctx, MessagesUpstreamRequest{
		Operation:       request.Operation,
		RawBody:         request.Body,
		ContentType:     request.ContentType,
		RequestPath:     request.NativePath,
		UpstreamModelID: target.ModelID,
		Header:          target.Header,
	})
}

func (adapter Adapter) BuildMessagesUpstreamRequest(_ context.Context, request MessagesUpstreamRequest) (provider.UpstreamRequest, error) {
	metadata, ok := Operation(request.Operation)
	if !ok {
		return provider.UpstreamRequest{}, &provider.AdapterError{HTTPStatus: http.StatusBadRequest, Code: "anthropic_operation_unsupported", Detail: "Anthropic operation is unsupported by this adapter."}
	}
	body := rewriteJSONModel(request.RawBody, request.UpstreamModelID)
	return provider.UpstreamRequest{Method: http.MethodPost, Path: metadata.NativePath, Header: request.Header.Clone(), Body: body}, nil
}

func (adapter Adapter) AdaptNonStreamResponse(ctx context.Context, response provider.UpstreamResponse) (provider.ClientResponse, error) {
	clientResponse, err := adapter.DefaultAdapter.AdaptNonStreamResponse(ctx, response)
	if err != nil {
		return provider.ClientResponse{}, err
	}
	usage, err := adapter.ExtractUsage(ctx, response)
	if err != nil {
		return provider.ClientResponse{}, err
	}
	clientResponse.Usage = usage
	return clientResponse, nil
}

func (adapter Adapter) ExtractUsage(_ context.Context, response provider.UpstreamResponse) (provider.UsageEnvelope, error) {
	switch strings.TrimSpace(response.Operation.Name) {
	case OperationMessages:
		return ExtractMessagesUsage(response.Body), nil
	case OperationCountTokens:
		return ExtractTokenCountUsage(response.Body), nil
	default:
		return provider.UsageEnvelope{}, nil
	}
}

func (adapter Adapter) AdaptStream(_ context.Context, request provider.StreamRequest) (provider.StreamResult, error) {
	streamBody, err := io.ReadAll(request.Reader)
	if err != nil {
		return provider.StreamResult{}, err
	}
	if request.Writer != nil {
		if _, err := request.Writer.Write(streamBody); err != nil {
			return provider.StreamResult{}, err
		}
	}
	usage, completed, terminal := ParseMessagesStream(streamBody)
	return provider.StreamResult{Usage: usage, Completed: completed, TerminalSignal: terminal}, nil
}

func ExtractMessagesUsage(body []byte) provider.UsageEnvelope {
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return provider.UsageEnvelope{}
	}
	usagePayload, ok := payload["usage"].(map[string]any)
	if !ok {
		return provider.UsageEnvelope{}
	}
	return ParseMessagesUsagePayload(usagePayload)
}

func ExtractTokenCountUsage(body []byte) provider.UsageEnvelope {
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return provider.UsageEnvelope{}
	}
	usage := provider.UsageEnvelope{}
	if inputTokens := intPointerFromAny(payload["input_tokens"]); inputTokens != nil {
		usage.InputTokens = inputTokens
	}
	if totalTokens := intPointerFromAny(firstValue(payload, "total_tokens", "totalTokens")); totalTokens != nil {
		assignTokenCountTotal(&usage, *totalTokens)
	}
	if cacheReadTokens := intPointerFromAny(firstValue(payload, "cache_read_input_tokens", "cachedContentTokenCount")); cacheReadTokens != nil {
		usage.CacheReadInputTokens = cacheReadTokens
	}
	if cacheCreationTokens := intPointerFromAny(payload["cache_creation_input_tokens"]); cacheCreationTokens != nil {
		usage.CacheCreationInputTokens = cacheCreationTokens
	}
	return normalizeTokenCountUsage(usage)
}

func ParseMessagesUsagePayload(usagePayload map[string]any) provider.UsageEnvelope {
	usage := provider.UsageEnvelope{}
	if inputTokens := intPointerFromAny(usagePayload["input_tokens"]); inputTokens != nil {
		usage.InputTokens = inputTokens
	}
	if outputTokens := intPointerFromAny(usagePayload["output_tokens"]); outputTokens != nil {
		usage.OutputTokens = outputTokens
	}
	if totalTokens := intPointerFromAny(usagePayload["total_tokens"]); totalTokens != nil {
		usage.TotalTokens = totalTokens
	}
	if cacheReadTokens := intPointerFromAny(usagePayload["cache_read_input_tokens"]); cacheReadTokens != nil {
		usage.CacheReadInputTokens = cacheReadTokens
	}
	if cacheCreationTokens := intPointerFromAny(usagePayload["cache_creation_input_tokens"]); cacheCreationTokens != nil {
		usage.CacheCreationInputTokens = cacheCreationTokens
	}
	return normalizeMessagesUsage(usage)
}

func ParseMessagesStream(streamBody []byte) (provider.UsageEnvelope, bool, string) {
	usage := provider.UsageEnvelope{}
	completed := false
	terminal := StreamTerminalNone
	scanner := bufio.NewScanner(bytes.NewReader(streamBody))
	event := ""
	dataLines := make([]string, 0, 1)
	flush := func() {
		if len(dataLines) == 0 {
			event = ""
			return
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &payload); err == nil {
			usage = MergeMessagesStreamUsage(usage, event, payload)
			if IsMessagesStreamTerminal(event, payload) {
				completed = true
				terminal = StreamTerminalCompleted
			}
		}
		event = ""
		dataLines = dataLines[:0]
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if value, ok := strings.CutPrefix(line, "event:"); ok {
			event = strings.TrimSpace(value)
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			dataLines = append(dataLines, strings.TrimSpace(value))
		}
	}
	flush()
	return usage, completed, terminal
}

func MergeMessagesStreamUsage(current provider.UsageEnvelope, event string, payload map[string]any) provider.UsageEnvelope {
	if IsMessagesStreamEvent(event, payload, "message_start") {
		messagePayload, ok := payload["message"].(map[string]any)
		if !ok {
			return current
		}
		usagePayload, ok := messagePayload["usage"].(map[string]any)
		if !ok {
			return current
		}
		return mergeUsage(current, ParseMessagesUsagePayload(usagePayload))
	}
	if IsMessagesStreamEvent(event, payload, "message_delta") {
		usagePayload, ok := payload["usage"].(map[string]any)
		if !ok {
			return current
		}
		deltaUsage := provider.UsageEnvelope{}
		if outputTokens := intPointerFromAny(usagePayload["output_tokens"]); outputTokens != nil {
			deltaUsage.OutputTokens = outputTokens
		}
		if totalTokens := intPointerFromAny(usagePayload["total_tokens"]); totalTokens != nil {
			deltaUsage.TotalTokens = totalTokens
		}
		return mergeUsage(current, deltaUsage)
	}
	return current
}

func IsMessagesStreamTerminal(event string, payload map[string]any) bool {
	return IsMessagesStreamEvent(event, payload, "message_stop")
}

func IsMessagesStreamEvent(event string, payload map[string]any, eventType string) bool {
	if event == eventType {
		return true
	}
	payloadType, _ := payload["type"].(string)
	return payloadType == eventType
}

func mergeUsage(current provider.UsageEnvelope, parsed provider.UsageEnvelope) provider.UsageEnvelope {
	if parsed.InputTokens != nil {
		current.InputTokens = parsed.InputTokens
	}
	if parsed.OutputTokens != nil {
		current.OutputTokens = parsed.OutputTokens
	}
	if parsed.TotalTokens != nil {
		current.TotalTokens = parsed.TotalTokens
	} else if parsed.OutputTokens != nil {
		current.TotalTokens = nil
	}
	if parsed.CacheReadInputTokens != nil {
		current.CacheReadInputTokens = parsed.CacheReadInputTokens
	}
	if parsed.CacheCreationInputTokens != nil {
		current.CacheCreationInputTokens = parsed.CacheCreationInputTokens
	}
	return normalizeMessagesUsage(current)
}

func normalizeMessagesUsage(usage provider.UsageEnvelope) provider.UsageEnvelope {
	if !usageHasValues(usage) || hasNegativeTokens(usage) {
		return provider.UsageEnvelope{}
	}
	if usage.TotalTokens == nil && usageHasComponents(usage) {
		total := componentTotal(usage)
		usage.TotalTokens = &total
	}
	if usage.TotalTokens != nil && *usage.TotalTokens < componentTotal(usage) {
		return provider.UsageEnvelope{}
	}
	usage.NormalizationRule = OperationMessages
	return usage
}

func normalizeTokenCountUsage(usage provider.UsageEnvelope) provider.UsageEnvelope {
	if !usageHasValues(usage) || hasNegativeTokens(usage) {
		return provider.UsageEnvelope{}
	}
	if usage.TotalTokens != nil && usage.InputTokens != nil && *usage.TotalTokens < *usage.InputTokens {
		return provider.UsageEnvelope{}
	}
	usage.NormalizationRule = OperationCountTokens
	return usage
}

func assignTokenCountTotal(usage *provider.UsageEnvelope, count int) {
	if usage.InputTokens == nil {
		inputTokens := count
		usage.InputTokens = &inputTokens
	}
	if usage.TotalTokens == nil {
		totalTokens := count
		usage.TotalTokens = &totalTokens
	}
}

func usageHasValues(usage provider.UsageEnvelope) bool {
	return usage.InputTokens != nil || usage.OutputTokens != nil || usage.TotalTokens != nil || usage.CacheReadInputTokens != nil || usage.CacheCreationInputTokens != nil
}

func usageHasComponents(usage provider.UsageEnvelope) bool {
	return usage.InputTokens != nil || usage.OutputTokens != nil || usage.CacheReadInputTokens != nil || usage.CacheCreationInputTokens != nil
}

func componentTotal(usage provider.UsageEnvelope) int {
	return intValue(usage.InputTokens) + intValue(usage.OutputTokens) + intValue(usage.CacheReadInputTokens) + intValue(usage.CacheCreationInputTokens)
}

func hasNegativeTokens(usage provider.UsageEnvelope) bool {
	for _, tokenCount := range []*int{usage.InputTokens, usage.OutputTokens, usage.TotalTokens, usage.CacheReadInputTokens, usage.CacheCreationInputTokens} {
		if tokenCount != nil && *tokenCount < 0 {
			return true
		}
	}
	return false
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func intPointerFromAny(value any) *int {
	switch typed := value.(type) {
	case int:
		return &typed
	case int64:
		converted := int(typed)
		return &converted
	case float64:
		if typed != float64(int(typed)) {
			return nil
		}
		converted := int(typed)
		return &converted
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return nil
		}
		converted := int(parsed)
		return &converted
	default:
		return nil
	}
}

func firstValue(payload map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			return value
		}
	}
	return nil
}

func rewriteJSONModel(rawBody []byte, targetModelID string) []byte {
	if len(rawBody) == 0 || strings.TrimSpace(targetModelID) == "" {
		return append([]byte(nil), rawBody...)
	}
	payload := map[string]any{}
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

func ClassifyProviderError(statusCode int, body []byte) *provider.AdapterError {
	if statusCode < http.StatusBadRequest {
		return nil
	}
	providerType, providerCode, message := extractProviderErrorFields(body)
	if strings.TrimSpace(message) == "" {
		message = http.StatusText(statusCode)
	}
	fields := map[string]any{}
	if strings.TrimSpace(providerType) != "" {
		fields["provider_error_type"] = providerType
	}
	if strings.TrimSpace(providerCode) != "" {
		fields["provider_error_code"] = providerCode
	}
	return &provider.AdapterError{HTTPStatus: statusCode, Code: "anthropic_provider_error", Detail: message, Fields: fields}
}

func extractProviderErrorFields(body []byte) (string, string, string) {
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", ""
	}
	errorPayload, _ := payload["error"].(map[string]any)
	if errorPayload == nil {
		return stringFromAny(payload["type"]), stringFromAny(payload["code"]), stringFromAny(payload["message"])
	}
	return stringFromAny(errorPayload["type"]), stringFromAny(errorPayload["code"]), stringFromAny(errorPayload["message"])
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func (adapter Adapter) CurrentBehavior(_ context.Context, operation provider.Operation) (provider.CurrentOperationBehavior, bool) {
	metadata, ok := Operation(operation)
	if !ok {
		return provider.CurrentOperationBehavior{}, false
	}
	kind := "text_generation"
	if metadata.TokenCount {
		kind = "token_count"
	}
	behavior := provider.CurrentOperationBehavior{
		OperationName:    operation.Name,
		APIFamily:        APIFamily,
		HookCollectionID: operation.HookCollectionID,
		HasRequest:       true,
		Request:          provider.RequestHookBehavior{Provider: APIFamily, HasStreamDetector: true},
		HasResponse:      true,
		Response:         provider.ResponseHookBehavior{Provider: APIFamily, Kind: kind, HasNonStreamParser: true, UsageRule: metadata.Name},
		HasStream:        !metadata.TokenCount,
	}
	if behavior.HasStream {
		behavior.Stream = provider.StreamHookBehavior{Provider: APIFamily, Kind: kind, UsageRule: metadata.Name, HasTerminalClassifier: true, HasUsageMerger: true}
	}
	return behavior, true
}
