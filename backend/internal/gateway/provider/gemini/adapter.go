package gemini

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

const APIFamily = provider.APIFamilyGemini

const (
	OperationGenerateContent       = "gemini.generate_content"
	OperationStreamGenerateContent = "gemini.stream_generate_content"
	OperationCountTokens           = "gemini.count_tokens"
)

const (
	StreamTerminalNone      = ""
	StreamTerminalCompleted = "completed"
)

type Adapter struct {
	provider.DefaultAdapter
}

type OperationMetadata struct {
	Name       string
	Suffix     string
	TokenCount bool
	Streaming  bool
}

type GenerateContentUpstreamRequest struct {
	Operation     provider.Operation
	RawBody       []byte
	ContentType   string
	RequestPath   string
	TargetModelID string
	Header        http.Header
}

func New(_ ...any) Adapter {
	return Adapter{DefaultAdapter: provider.DefaultAdapter{APIFamilyName: APIFamily}}
}

var _ provider.ProviderAdapter = Adapter{}

func Operation(operation provider.Operation) (OperationMetadata, bool) {
	switch strings.TrimSpace(operation.Name) {
	case OperationGenerateContent:
		return OperationMetadata{Name: OperationGenerateContent, Suffix: ":generateContent"}, true
	case OperationStreamGenerateContent:
		return OperationMetadata{Name: OperationStreamGenerateContent, Suffix: ":streamGenerateContent", Streaming: true}, true
	case OperationCountTokens:
		return OperationMetadata{Name: OperationCountTokens, Suffix: ":countTokens", TokenCount: true}, true
	default:
		return OperationMetadata{}, false
	}
}

func IsOperation(operation provider.Operation) bool {
	_, ok := Operation(operation)
	return ok
}

func (adapter Adapter) BuildUpstreamRequest(ctx context.Context, request provider.ProviderRequest, target provider.UpstreamTarget) (provider.UpstreamRequest, error) {
	return adapter.BuildGenerateContentUpstreamRequest(ctx, GenerateContentUpstreamRequest{
		Operation:     request.Operation,
		RawBody:       request.Body,
		ContentType:   request.ContentType,
		RequestPath:   request.NativePath,
		TargetModelID: target.ModelID,
		Header:        target.Header,
	})
}

func (adapter Adapter) BuildGenerateContentUpstreamRequest(_ context.Context, request GenerateContentUpstreamRequest) (provider.UpstreamRequest, error) {
	metadata, ok := Operation(request.Operation)
	if !ok {
		return provider.UpstreamRequest{}, &provider.AdapterError{HTTPStatus: http.StatusBadRequest, Code: "gemini_operation_unsupported", Detail: "Gemini operation is unsupported by this adapter."}
	}
	path := "/v1beta/models/" + strings.TrimSpace(request.TargetModelID) + metadata.Suffix
	if strings.TrimSpace(request.TargetModelID) == "" {
		path = strings.TrimSpace(request.RequestPath)
	}
	return provider.UpstreamRequest{Method: http.MethodPost, Path: path, Header: request.Header.Clone(), Body: append([]byte(nil), request.RawBody...)}, nil
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
	case OperationGenerateContent, OperationStreamGenerateContent:
		return ExtractGenerateContentUsage(response.Body), nil
	case OperationCountTokens:
		return ExtractCountTokensUsage(response.Body), nil
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
	usage, completed, terminal := ParseStreamGenerateContent(streamBody)
	return provider.StreamResult{Usage: usage, Completed: completed, TerminalSignal: terminal}, nil
}

func ExtractGenerateContentUsage(body []byte) provider.UsageEnvelope {
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return provider.UsageEnvelope{}
	}
	usagePayload, ok := payload["usageMetadata"].(map[string]any)
	if !ok {
		usagePayload, ok = payload["usage_metadata"].(map[string]any)
	}
	if !ok {
		return provider.UsageEnvelope{}
	}
	return ParseUsageMetadata(usagePayload, OperationGenerateContent)
}

func ExtractCountTokensUsage(body []byte) provider.UsageEnvelope {
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return provider.UsageEnvelope{}
	}
	usage := provider.UsageEnvelope{}
	if totalTokens := intPointerFromAny(firstValue(payload, "totalTokens", "total_tokens")); totalTokens != nil {
		assignTokenCountTotal(&usage, *totalTokens)
	}
	if cacheReadTokens := intPointerFromAny(firstValue(payload, "cachedContentTokenCount", "cache_read_input_tokens")); cacheReadTokens != nil {
		usage.CacheReadInputTokens = cacheReadTokens
	}
	return normalizeTokenCountUsage(usage)
}

// ParseUsageMetadata maps Gemini usageMetadata to Prism's canonical disjoint
// components. Google defines totalTokenCount as "prompt + thoughts + response
// candidates" (three parallel terms), and cachedContentTokenCount as a subset
// of promptTokenCount. Therefore only the input side subtracts its child:
// InputTokens = prompt - cachedContent, OutputTokens = candidates as reported,
// ReasoningTokens = thoughts as reported.
func ParseUsageMetadata(usagePayload map[string]any, normalizationRule string) provider.UsageEnvelope {
	inputTokens := intPointerFromAny(firstValue(usagePayload, "promptTokenCount", "prompt_token_count"))
	cacheReadTokens := intPointerFromAny(firstValue(usagePayload, "cachedContentTokenCount", "cached_content_token_count"))
	outputTokens := intPointerFromAny(firstValue(usagePayload, "candidatesTokenCount", "candidates_token_count"))
	reasoningTokens := intPointerFromAny(firstValue(usagePayload, "thoughtsTokenCount", "thoughts_token_count"))
	totalTokens := intPointerFromAny(firstValue(usagePayload, "totalTokenCount", "total_token_count"))
	usage := provider.UsageEnvelope{TotalTokens: totalTokens, CacheReadInputTokens: cacheReadTokens, ReasoningTokens: reasoningTokens}
	if inputTokens != nil {
		baseInputTokens := *inputTokens - intValue(cacheReadTokens)
		usage.InputTokens = &baseInputTokens
	}
	usage.OutputTokens = outputTokens
	return normalizeGenerationUsage(usage, normalizationRule)
}

func ParseStreamGenerateContent(streamBody []byte) (provider.UsageEnvelope, bool, string) {
	usage := provider.UsageEnvelope{}
	completed := false
	terminal := StreamTerminalNone
	scanner := bufio.NewScanner(bytes.NewReader(streamBody))
	dataLines := make([]string, 0, 1)
	flush := func() {
		if len(dataLines) == 0 {
			return
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &payload); err == nil {
			usage = MergeStreamGenerateContentUsage(usage, payload)
			if IsStreamGenerateContentTerminal(payload) {
				completed = true
				terminal = StreamTerminalCompleted
			}
		}
		dataLines = dataLines[:0]
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			dataLines = append(dataLines, strings.TrimSpace(value))
		}
	}
	flush()
	return usage, completed, terminal
}

func MergeStreamGenerateContentUsage(current provider.UsageEnvelope, payload map[string]any) provider.UsageEnvelope {
	if current.NormalizationRejected {
		return current
	}
	usagePayload, ok := payload["usageMetadata"].(map[string]any)
	if !ok {
		usagePayload, ok = payload["usage_metadata"].(map[string]any)
	}
	if !ok {
		return current
	}
	return mergeUsage(current, ParseUsageMetadata(usagePayload, OperationStreamGenerateContent))
}

func IsStreamGenerateContentTerminal(payload map[string]any) bool {
	if done, _ := payload["done"].(bool); done {
		return true
	}
	if _, ok := payload["usageMetadata"].(map[string]any); ok {
		return true
	}
	_, ok := payload["usage_metadata"].(map[string]any)
	return ok
}

func mergeUsage(current provider.UsageEnvelope, parsed provider.UsageEnvelope) provider.UsageEnvelope {
	if current.NormalizationRejected || parsed.NormalizationRejected {
		return provider.UsageEnvelope{NormalizationRejected: true}
	}
	if parsed.InputTokens != nil {
		current.InputTokens = parsed.InputTokens
	}
	if parsed.OutputTokens != nil {
		current.OutputTokens = parsed.OutputTokens
	}
	if parsed.TotalTokens != nil {
		current.TotalTokens = parsed.TotalTokens
	}
	if parsed.CacheReadInputTokens != nil {
		current.CacheReadInputTokens = parsed.CacheReadInputTokens
	}
	if parsed.ReasoningTokens != nil {
		current.ReasoningTokens = parsed.ReasoningTokens
	}
	return normalizeGenerationUsage(current, OperationStreamGenerateContent)
}

func normalizeGenerationUsage(usage provider.UsageEnvelope, normalizationRule string) provider.UsageEnvelope {
	if !usageHasValues(usage) {
		return provider.UsageEnvelope{}
	}
	if hasNegativeTokens(usage) {
		return provider.UsageEnvelope{NormalizationRejected: true}
	}
	if usage.TotalTokens == nil && usageHasComponents(usage) {
		total := componentTotal(usage)
		usage.TotalTokens = &total
	}
	if usage.TotalTokens != nil && *usage.TotalTokens < componentTotal(usage) {
		return provider.UsageEnvelope{NormalizationRejected: true}
	}
	usage.NormalizationRule = normalizationRule
	return usage
}

func normalizeTokenCountUsage(usage provider.UsageEnvelope) provider.UsageEnvelope {
	if !usageHasValues(usage) {
		return provider.UsageEnvelope{}
	}
	if hasNegativeTokens(usage) {
		return provider.UsageEnvelope{NormalizationRejected: true}
	}
	if usage.TotalTokens != nil && usage.InputTokens != nil && *usage.TotalTokens < *usage.InputTokens {
		return provider.UsageEnvelope{NormalizationRejected: true}
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
	return usage.InputTokens != nil || usage.OutputTokens != nil || usage.TotalTokens != nil || usage.CacheReadInputTokens != nil || usage.ReasoningTokens != nil
}

func usageHasComponents(usage provider.UsageEnvelope) bool {
	return usage.InputTokens != nil || usage.OutputTokens != nil || usage.CacheReadInputTokens != nil || usage.ReasoningTokens != nil
}

func componentTotal(usage provider.UsageEnvelope) int {
	return intValue(usage.InputTokens) + intValue(usage.OutputTokens) + intValue(usage.CacheReadInputTokens) + intValue(usage.ReasoningTokens)
}

func hasNegativeTokens(usage provider.UsageEnvelope) bool {
	for _, tokenCount := range []*int{usage.InputTokens, usage.OutputTokens, usage.TotalTokens, usage.CacheReadInputTokens, usage.ReasoningTokens} {
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

func ClassifyProviderError(statusCode int, body []byte) *provider.AdapterError {
	if statusCode < http.StatusBadRequest {
		return nil
	}
	providerStatus, providerCode, message := extractProviderErrorFields(body)
	if strings.TrimSpace(message) == "" {
		message = http.StatusText(statusCode)
	}
	fields := map[string]any{}
	if strings.TrimSpace(providerStatus) != "" {
		fields["provider_error_status"] = providerStatus
	}
	if strings.TrimSpace(providerCode) != "" {
		fields["provider_error_code"] = providerCode
	}
	return &provider.AdapterError{HTTPStatus: statusCode, Code: "gemini_provider_error", Detail: message, Fields: fields}
}

func extractProviderErrorFields(body []byte) (string, string, string) {
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", ""
	}
	errorPayload, _ := payload["error"].(map[string]any)
	if errorPayload == nil {
		return stringFromAny(payload["status"]), stringFromAny(payload["code"]), stringFromAny(payload["message"])
	}
	return stringFromAny(errorPayload["status"]), stringFromAny(errorPayload["code"]), stringFromAny(errorPayload["message"])
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
		Request:          provider.RequestHookBehavior{Provider: APIFamily, HasGenerationParamsExtractor: true, HasStreamDetector: true},
		HasResponse:      true,
		Response:         provider.ResponseHookBehavior{Provider: APIFamily, Kind: kind, HasNonStreamParser: true, UsageRule: metadata.Name},
		HasStream:        metadata.Streaming,
	}
	if behavior.HasStream {
		behavior.Stream = provider.StreamHookBehavior{Provider: APIFamily, Kind: kind, UsageRule: metadata.Name, HasTerminalClassifier: true, HasUsageMerger: true}
	}
	return behavior, true
}
