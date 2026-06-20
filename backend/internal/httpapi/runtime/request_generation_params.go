package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/tiktoken-go/tokenizer"
)

const (
	requestGenerationParamsStatusComplete        = "complete"
	requestGenerationParamsStatusMissing         = "missing"
	requestGenerationParamsStatusMalformed       = "malformed"
	requestGenerationParamsStatusIncomplete      = "incomplete"
	requestGenerationParamsStatusSkippedOversize = "skipped_oversize"

	geminiGenerationParamsStreamingCaptureLimit = 64 * 1024
)

type requestGenerationParamsSnapshot struct {
	Params *requestGenerationParams
	Status string
}

type requestGenerationParams struct {
	Provider              string                            `json:"provider"`
	Temperature           *float64                          `json:"temperature,omitempty"`
	TopP                  *float64                          `json:"top_p,omitempty"`
	TopK                  *int                              `json:"top_k,omitempty"`
	MaxOutputTokens       *int                              `json:"max_output_tokens,omitempty"`
	MaxOutputTokensSource *string                           `json:"max_output_tokens_source,omitempty"`
	Reasoning             *requestGenerationReasoningParams `json:"reasoning,omitempty"`
}

type requestGenerationReasoningParams struct {
	Effort          *string `json:"effort,omitempty"`
	Mode            *string `json:"mode,omitempty"`
	BudgetTokens    *int    `json:"budget_tokens,omitempty"`
	IncludeThoughts *bool   `json:"include_thoughts,omitempty"`
	SourceField     *string `json:"source_field,omitempty"`
}

func extractBufferedRequestGenerationParams(operation RuntimeOperation, rawBody []byte) requestGenerationParamsSnapshot {
	hooks, ok := requestHooksForOperation(operation)
	if !ok || hooks.ExtractBufferedGenerationParams == nil {
		return requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusMissing}
	}
	provider := strings.ToLower(strings.TrimSpace(hooks.Provider))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(operation.APIFamily))
	}
	if provider == "" {
		return requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusMissing}
	}
	params := &requestGenerationParams{Provider: provider}
	if len(bytes.TrimSpace(rawBody)) == 0 {
		return requestGenerationParamsSnapshot{Params: params, Status: requestGenerationParamsStatusMissing}
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusMalformed}
	}
	hooks.ExtractBufferedGenerationParams(payload, params)
	status := requestGenerationParamsStatusComplete
	if !params.hasGenerationFields() {
		status = requestGenerationParamsStatusMissing
	}
	return requestGenerationParamsSnapshot{Params: params, Status: status}
}

func extractOpenAIChatGenerationParams(payload map[string]any, params *requestGenerationParams) {
	params.Temperature = floatPointerFromAny(payload["temperature"])
	params.TopP = floatPointerFromAny(payload["top_p"])
	if maxCompletionTokens := intPointerFromAny(payload["max_completion_tokens"]); maxCompletionTokens != nil {
		params.MaxOutputTokens = maxCompletionTokens
		params.MaxOutputTokensSource = stringPtr("max_completion_tokens")
	} else if maxTokens := intPointerFromAny(payload["max_tokens"]); maxTokens != nil {
		params.MaxOutputTokens = maxTokens
		params.MaxOutputTokensSource = stringPtr("max_tokens")
	}
	if effort := trimmedStringFromAny(payload["reasoning_effort"]); effort != nil {
		reasoning := params.ensureReasoning()
		reasoning.Effort = effort
		reasoning.SourceField = stringPtr("reasoning_effort")
	}
}

func extractOpenAIResponsesGenerationParams(payload map[string]any, params *requestGenerationParams) {
	params.Temperature = floatPointerFromAny(payload["temperature"])
	params.TopP = floatPointerFromAny(payload["top_p"])
	if maxOutputTokens := intPointerFromAny(payload["max_output_tokens"]); maxOutputTokens != nil {
		params.MaxOutputTokens = maxOutputTokens
		params.MaxOutputTokensSource = stringPtr("max_output_tokens")
	} else if maxCompletionTokens := intPointerFromAny(payload["max_completion_tokens"]); maxCompletionTokens != nil {
		params.MaxOutputTokens = maxCompletionTokens
		params.MaxOutputTokensSource = stringPtr("max_completion_tokens")
	} else if maxTokens := intPointerFromAny(payload["max_tokens"]); maxTokens != nil {
		params.MaxOutputTokens = maxTokens
		params.MaxOutputTokensSource = stringPtr("max_tokens")
	}
	if effort := trimmedStringFromAny(nestedValue(payload, "reasoning", "effort")); effort != nil {
		reasoning := params.ensureReasoning()
		reasoning.Effort = effort
		reasoning.SourceField = stringPtr("reasoning.effort")
	}
}

func extractAnthropicGenerationParams(payload map[string]any, params *requestGenerationParams) {
	params.Temperature = floatPointerFromAny(payload["temperature"])
	params.TopP = floatPointerFromAny(payload["top_p"])
	if maxTokens := intPointerFromAny(payload["max_tokens"]); maxTokens != nil {
		params.MaxOutputTokens = maxTokens
		params.MaxOutputTokensSource = stringPtr("max_tokens")
	}
	if mode := trimmedStringFromAny(nestedValue(payload, "thinking", "type")); mode != nil {
		reasoning := params.ensureReasoning()
		reasoning.Mode = mode
		reasoning.SourceField = stringPtr("thinking")
	}
	if budgetTokens := intPointerFromAny(nestedValue(payload, "thinking", "budget_tokens")); budgetTokens != nil {
		reasoning := params.ensureReasoning()
		reasoning.BudgetTokens = budgetTokens
		reasoning.SourceField = stringPtr("thinking")
	}
	if effort := trimmedStringFromAny(nestedValue(payload, "output_config", "effort")); effort != nil {
		reasoning := params.ensureReasoning()
		reasoning.Effort = effort
		reasoning.SourceField = stringPtr("output_config.effort")
	}
}

func extractGeminiGenerationParams(payload map[string]any, params *requestGenerationParams) {
	generationConfig, ok := payload["generationConfig"].(map[string]any)
	if !ok {
		return
	}
	params.Temperature = floatPointerFromAny(generationConfig["temperature"])
	params.TopP = floatPointerFromAny(generationConfig["topP"])
	params.TopK = intPointerFromAny(generationConfig["topK"])
	if maxOutputTokens := intPointerFromAny(generationConfig["maxOutputTokens"]); maxOutputTokens != nil {
		params.MaxOutputTokens = maxOutputTokens
		params.MaxOutputTokensSource = stringPtr("generationConfig.maxOutputTokens")
	}
	thinkingConfig, ok := generationConfig["thinkingConfig"].(map[string]any)
	if !ok {
		return
	}
	if budget := intPointerFromAny(thinkingConfig["thinkingBudget"]); budget != nil {
		reasoning := params.ensureReasoning()
		reasoning.BudgetTokens = budget
		reasoning.SourceField = stringPtr("generationConfig.thinkingConfig")
	}
	if level := trimmedStringFromAny(thinkingConfig["thinkingLevel"]); level != nil {
		reasoning := params.ensureReasoning()
		reasoning.Effort = level
		reasoning.SourceField = stringPtr("generationConfig.thinkingConfig")
	}
	if includeThoughts, ok := boolPointerFromAny(thinkingConfig["includeThoughts"]); ok {
		reasoning := params.ensureReasoning()
		reasoning.IncludeThoughts = includeThoughts
		reasoning.SourceField = stringPtr("generationConfig.thinkingConfig")
	}
}

func (params *requestGenerationParams) ensureReasoning() *requestGenerationReasoningParams {
	if params.Reasoning == nil {
		params.Reasoning = &requestGenerationReasoningParams{}
	}
	return params.Reasoning
}
func (params requestGenerationParams) hasGenerationFields() bool {
	return params.Temperature != nil || params.TopP != nil || params.TopK != nil || params.MaxOutputTokens != nil || params.Reasoning != nil
}
func (snapshot requestGenerationParamsSnapshot) clone() requestGenerationParamsSnapshot {
	cloned := requestGenerationParamsSnapshot{Status: snapshot.Status}
	if snapshot.Params == nil {
		return cloned
	}
	params := *snapshot.Params
	if snapshot.Params.Reasoning != nil {
		reasoning := *snapshot.Params.Reasoning
		params.Reasoning = &reasoning
	}
	cloned.Params = &params
	return cloned
}

const (
	openAIChatContextEstimationMethod      = "openai_chat_tokenizer_v1"
	openAIResponsesContextEstimationMethod = "openai_responses_tokenizer_v1"
	contextEstimationUnavailableErrorCode  = "context_estimation_unavailable"
	contextEstimationUnavailableDetail     = "Preflight context estimation is unavailable for this request shape."
	contextEstimationUnavailableReasonKey  = "estimation_unavailable_reason"
	defaultOutputTokenReserve              = 4096
	defaultMaxContextUtilization           = 0.90
	openAIChatTokensPerMessage             = 3
	openAIChatTokensPerName                = 1
	openAIChatReplyPrimingTokens           = 3
)

type requestContextEstimationUnavailableReason string

const (
	requestContextEstimationUnavailableReasonUnknownTokenizerModel    requestContextEstimationUnavailableReason = "unknown_tokenizer_model"
	requestContextEstimationUnavailableReasonMalformedRequestJSON     requestContextEstimationUnavailableReason = "malformed_request_json"
	requestContextEstimationUnavailableReasonChatNonTextContent       requestContextEstimationUnavailableReason = "chat_non_text_content"
	requestContextEstimationUnavailableReasonChatUnsafeToolShape      requestContextEstimationUnavailableReason = "chat_unsafe_tool_shape"
	requestContextEstimationUnavailableReasonResponsesStatefulRef     requestContextEstimationUnavailableReason = "responses_stateful_reference"
	requestContextEstimationUnavailableReasonResponsesNonTextInput    requestContextEstimationUnavailableReason = "responses_non_text_input"
	requestContextEstimationUnavailableReasonResponsesUnsafeToolShape requestContextEstimationUnavailableReason = "responses_unsafe_tool_shape"
)

type requestContextEstimation struct {
	Method                      string `json:"method"`
	EstimatedInputTokens        int    `json:"estimated_input_tokens"`
	ReservedOutputTokens        int    `json:"reserved_output_tokens"`
	EstimatedTotalContextTokens int    `json:"estimated_total_context_tokens"`
	UsableContextWindowTokens   *int   `json:"usable_context_window_tokens,omitempty"`
}

type requestContextEstimationOptions struct {
	ModelID                   string
	DefaultOutputTokenReserve *int
	ContextWindowTokens       *int
	MaxContextUtilization     *float64
}

func estimatePreflightRequestContext(operation RuntimeOperation, rawBody []byte, requestedModel runtimeModelRecord) (*requestContextEstimation, error) {
	options := requestContextEstimationOptions{
		ModelID: requestedModel.ModelID,
	}
	switch strings.TrimSpace(operation.Name) {
	case "openai.chat_completions":
		return estimateOpenAIChatCompletionsRequestTokens(rawBody, options)
	case "openai.responses":
		return estimateOpenAIResponsesRequestTokens(rawBody, options)
	default:
		return nil, nil
	}
}

func estimateOpenAIChatCompletionsRequestTokens(rawBody []byte, options requestContextEstimationOptions) (*requestContextEstimation, error) {
	payload, err := decodeRequestContextEstimationPayload(rawBody)
	if err != nil {
		return nil, err
	}
	modelID := strings.TrimSpace(options.ModelID)
	if modelID == "" {
		modelID = strings.TrimSpace(stringValue(payload["model"]))
	}
	codec, err := openAITokenizerForModel(modelID)
	if err != nil {
		return nil, err
	}
	if err := validateOpenAIChatMessagesShape(payload["messages"]); err != nil {
		return nil, err
	}
	if err := validateOpenAIChatToolsShape(payload["tools"]); err != nil {
		return nil, err
	}
	params := &requestGenerationParams{Provider: "openai"}
	extractOpenAIChatGenerationParams(payload, params)
	estimatedInputTokens, err := estimateOpenAIChatTokenizerInputTokens(payload, codec)
	if err != nil {
		return nil, err
	}
	return buildRequestContextEstimation(openAIChatContextEstimationMethod, estimatedInputTokens, params.MaxOutputTokens, options), nil
}

func openAITokenizerForModel(modelID string) (tokenizer.Codec, error) {
	normalized := strings.ToLower(strings.TrimSpace(modelID))
	if normalized == "" {
		return nil, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonUnknownTokenizerModel)
	}
	if normalized == "gpt-5" || strings.HasPrefix(normalized, "gpt-5-") || strings.HasPrefix(normalized, "gpt-5.") {
		codec, err := tokenizer.Get(tokenizer.O200kBase)
		if err != nil {
			return nil, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonUnknownTokenizerModel)
		}
		return codec, nil
	}
	codec, err := tokenizer.ForModel(tokenizer.Model(normalized))
	if err != nil {
		return nil, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonUnknownTokenizerModel)
	}
	return codec, nil
}

func estimateOpenAIChatTokenizerInputTokens(payload map[string]any, codec tokenizer.Codec) (int, error) {
	messages, ok := payload["messages"].([]any)
	if !ok {
		if payload["messages"] == nil {
			return openAIChatReplyPrimingTokens, nil
		}
		return 0, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatNonTextContent)
	}
	total := openAIChatReplyPrimingTokens
	for _, message := range messages {
		messageTokens, err := countOpenAIChatMessageTokens(message, codec)
		if err != nil {
			return 0, err
		}
		total += messageTokens
	}
	for _, field := range []string{"tools", "tool_choice", "functions", "function_call", "response_format"} {
		count, err := tokenizerTokensForSerializedJSON(codec, payload[field])
		if err != nil {
			return 0, err
		}
		total += count
	}
	return total, nil
}

func countOpenAIChatMessageTokens(message any, codec tokenizer.Codec) (int, error) {
	messageMap, ok := message.(map[string]any)
	if !ok {
		return 0, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatNonTextContent)
	}
	total := openAIChatTokensPerMessage
	for _, field := range []string{"role", "tool_call_id"} {
		count, err := tokenizerTokensForString(codec, messageMap[field])
		if err != nil {
			return 0, err
		}
		total += count
	}
	if name := stringValue(messageMap["name"]); strings.TrimSpace(name) != "" {
		count, err := codec.Count(name)
		if err != nil {
			return 0, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatNonTextContent)
		}
		total += openAIChatTokensPerName + count
	}
	contentTokens, err := countOpenAIChatContentTokens(messageMap["content"], codec)
	if err != nil {
		return 0, err
	}
	total += contentTokens
	for _, field := range []string{"tool_calls", "function_call"} {
		count, err := tokenizerTokensForSerializedJSON(codec, messageMap[field])
		if err != nil {
			return 0, err
		}
		total += count
	}
	return total, nil
}

func countOpenAIChatContentTokens(value any, codec tokenizer.Codec) (int, error) {
	switch typed := value.(type) {
	case nil:
		return 0, nil
	case string:
		return tokenizerTokensForString(codec, typed)
	case []any:
		return countOpenAIChatContentPartTokens(typed, codec)
	default:
		return 0, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatNonTextContent)
	}
}

func countOpenAIChatContentPartTokens(parts []any, codec tokenizer.Codec) (int, error) {
	total := 0
	for _, part := range parts {
		partMap, ok := part.(map[string]any)
		if !ok {
			return 0, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatNonTextContent)
		}
		partType := strings.ToLower(strings.TrimSpace(stringValue(partMap["type"])))
		switch partType {
		case "", "text":
			count, err := tokenizerTokensForString(codec, partMap["text"])
			if err != nil {
				return 0, err
			}
			total += count
		default:
			return 0, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatNonTextContent)
		}
	}
	return total, nil
}

func tokenizerTokensForString(codec tokenizer.Codec, value any) (int, error) {
	return tokenizerTokensForStringWithReason(codec, value, requestContextEstimationUnavailableReasonChatNonTextContent)
}

func tokenizerTokensForStringWithReason(codec tokenizer.Codec, value any, reason requestContextEstimationUnavailableReason) (int, error) {
	if value == nil {
		return 0, nil
	}
	text, ok := value.(string)
	if !ok {
		return 0, contextEstimationUnavailableDomainError(reason)
	}
	count, err := codec.Count(text)
	if err != nil {
		return 0, contextEstimationUnavailableDomainError(reason)
	}
	return count, nil
}

func tokenizerTokensForSerializedJSON(codec tokenizer.Codec, value any) (int, error) {
	return tokenizerTokensForSerializedJSONWithReason(codec, value, requestContextEstimationUnavailableReasonChatNonTextContent)
}

func tokenizerTokensForSerializedJSONWithReason(codec tokenizer.Codec, value any, reason requestContextEstimationUnavailableReason) (int, error) {
	if value == nil {
		return 0, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, contextEstimationUnavailableDomainError(reason)
	}
	count, err := codec.Count(string(raw))
	if err != nil {
		return 0, contextEstimationUnavailableDomainError(reason)
	}
	return count, nil
}

func estimateOpenAIResponsesRequestTokens(rawBody []byte, options requestContextEstimationOptions) (*requestContextEstimation, error) {
	payload, err := decodeRequestContextEstimationPayload(rawBody)
	if err != nil {
		return nil, err
	}
	if fieldPresent(payload, "previous_response_id") || fieldPresent(payload, "conversation") {
		return nil, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesStatefulRef)
	}
	if err := validateOpenAIResponsesInputShape(payload["input"]); err != nil {
		return nil, err
	}
	if err := validateOpenAIToolsShape(payload["tools"]); err != nil {
		return nil, err
	}
	modelID := strings.TrimSpace(options.ModelID)
	if modelID == "" {
		modelID = strings.TrimSpace(stringValue(payload["model"]))
	}
	codec, err := openAITokenizerForModel(modelID)
	if err != nil {
		return nil, err
	}
	params := &requestGenerationParams{Provider: "openai"}
	extractOpenAIResponsesGenerationParams(payload, params)
	estimatedInputTokens, err := estimateOpenAIResponsesTokenizerInputTokens(payload, codec)
	if err != nil {
		return nil, err
	}
	return buildRequestContextEstimation(openAIResponsesContextEstimationMethod, estimatedInputTokens, params.MaxOutputTokens, options), nil
}

func estimateOpenAIResponsesTokenizerInputTokens(payload map[string]any, codec tokenizer.Codec) (int, error) {
	total, err := countOpenAIResponsesInputTokens(payload["input"], codec)
	if err != nil {
		return 0, err
	}
	instructionsTokens, err := tokenizerTokensForStringWithReason(codec, payload["instructions"], requestContextEstimationUnavailableReasonResponsesNonTextInput)
	if err != nil {
		return 0, err
	}
	total += instructionsTokens
	for _, field := range []string{"tools", "tool_choice", "text"} {
		count, err := tokenizerTokensForSerializedJSONWithReason(codec, payload[field], requestContextEstimationUnavailableReasonResponsesNonTextInput)
		if err != nil {
			return 0, err
		}
		total += count
	}
	return total, nil
}

func countOpenAIResponsesInputTokens(value any, codec tokenizer.Codec) (int, error) {
	switch typed := value.(type) {
	case string:
		return tokenizerTokensForStringWithReason(codec, typed, requestContextEstimationUnavailableReasonResponsesNonTextInput)
	case map[string]any:
		return countOpenAIResponsesTopLevelInputItemTokens(typed, codec)
	case []any:
		total := 0
		for _, item := range typed {
			itemMap, ok := item.(map[string]any)
			if !ok {
				return 0, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesNonTextInput)
			}
			count, err := countOpenAIResponsesTopLevelInputItemTokens(itemMap, codec)
			if err != nil {
				return 0, err
			}
			total += count
		}
		return total, nil
	default:
		return 0, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesNonTextInput)
	}
}

func countOpenAIResponsesTopLevelInputItemTokens(item map[string]any, codec tokenizer.Codec) (int, error) {
	switch strings.ToLower(strings.TrimSpace(stringValue(item["type"]))) {
	case "input_text":
		return tokenizerTokensForStringWithReason(codec, item["text"], requestContextEstimationUnavailableReasonResponsesNonTextInput)
	case "message":
		return countOpenAIResponsesMessageItemTokens(item, codec)
	default:
		return 0, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesNonTextInput)
	}
}

func countOpenAIResponsesMessageItemTokens(item map[string]any, codec tokenizer.Codec) (int, error) {
	switch typed := item["content"].(type) {
	case nil:
		return 0, nil
	case string:
		return tokenizerTokensForStringWithReason(codec, typed, requestContextEstimationUnavailableReasonResponsesNonTextInput)
	case []any:
		total := 0
		for _, part := range typed {
			partMap, ok := part.(map[string]any)
			if !ok {
				return 0, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesNonTextInput)
			}
			if strings.ToLower(strings.TrimSpace(stringValue(partMap["type"]))) != "input_text" {
				return 0, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesNonTextInput)
			}
			count, err := tokenizerTokensForStringWithReason(codec, partMap["text"], requestContextEstimationUnavailableReasonResponsesNonTextInput)
			if err != nil {
				return 0, err
			}
			total += count
		}
		return total, nil
	default:
		return 0, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesNonTextInput)
	}
}

func buildRequestContextEstimation(method string, estimatedInputTokens int, explicitMaxOutputTokens *int, options requestContextEstimationOptions) *requestContextEstimation {
	if estimatedInputTokens < 0 {
		estimatedInputTokens = 0
	}
	reservedOutputTokens := resolveReservedOutputTokens(explicitMaxOutputTokens, options.DefaultOutputTokenReserve)
	return &requestContextEstimation{
		Method:                      method,
		EstimatedInputTokens:        estimatedInputTokens,
		ReservedOutputTokens:        reservedOutputTokens,
		EstimatedTotalContextTokens: estimatedInputTokens + reservedOutputTokens,
		UsableContextWindowTokens:   computeUsableContextWindowTokens(options.ContextWindowTokens, options.MaxContextUtilization),
	}
}

func (estimation *requestContextEstimation) fitsUsableContextWindowTokens(usableContextWindowTokens int) bool {
	if estimation == nil || usableContextWindowTokens <= 0 {
		return false
	}
	return estimation.EstimatedTotalContextTokens <= usableContextWindowTokens
}

func decodeRequestContextEstimationPayload(rawBody []byte) (map[string]any, error) {
	payload := map[string]any{}
	if len(bytes.TrimSpace(rawBody)) == 0 {
		return payload, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonMalformedRequestJSON)
	}
	return payload, nil
}

func fieldPresent(payload map[string]any, key string) bool {
	_, ok := payload[key]
	return ok
}

func contextEstimationUnavailableDomainError(reasons ...requestContextEstimationUnavailableReason) error {
	fields := map[string]any(nil)
	if len(reasons) > 0 {
		reason := strings.TrimSpace(string(reasons[0]))
		if reason != "" {
			fields = map[string]any{contextEstimationUnavailableReasonKey: reason}
		}
	}
	return &domainError{StatusCode: http.StatusBadRequest, ErrorCode: contextEstimationUnavailableErrorCode, Detail: contextEstimationUnavailableDetail, Fields: fields}
}

func contextEstimationUnavailableReasonFromError(err error) *string {
	var domainErr *domainError
	if !errors.As(err, &domainErr) || domainErr == nil || strings.TrimSpace(domainErr.ErrorCode) != contextEstimationUnavailableErrorCode {
		return nil
	}
	if reason, ok := domainErr.Fields[contextEstimationUnavailableReasonKey].(string); ok && strings.TrimSpace(reason) != "" {
		trimmed := strings.TrimSpace(reason)
		return &trimmed
	}
	return nil
}

func allowContextEstimationUnavailablePassThrough(operation RuntimeOperation, err error) bool {
	if !isContextEstimationUnavailableError(err) {
		return false
	}
	switch strings.TrimSpace(operation.Name) {
	case "openai.chat_completions", "openai.responses":
		return true
	default:
		return false
	}
}

func isContextEstimationUnavailableError(err error) bool {
	var domainErr *domainError
	if !errors.As(err, &domainErr) || domainErr == nil {
		return false
	}
	return strings.TrimSpace(domainErr.ErrorCode) == contextEstimationUnavailableErrorCode
}

func resolveReservedOutputTokens(explicitMaxOutputTokens *int, defaultOutputReserve *int) int {
	if explicitMaxOutputTokens != nil && *explicitMaxOutputTokens > 0 {
		return *explicitMaxOutputTokens
	}
	if defaultOutputReserve != nil && *defaultOutputReserve > 0 {
		return *defaultOutputReserve
	}
	return defaultOutputTokenReserve
}

func computeUsableContextWindowTokens(contextWindowTokens *int, maxContextUtilization *float64) *int {
	if contextWindowTokens == nil || *contextWindowTokens <= 0 {
		return nil
	}
	utilization := defaultMaxContextUtilization
	if maxContextUtilization != nil && *maxContextUtilization > 0 && *maxContextUtilization <= 1 {
		utilization = *maxContextUtilization
	}
	usable := int(math.Floor(float64(*contextWindowTokens) * utilization))
	return &usable
}

func validateOpenAIChatMessagesShape(value any) error {
	if value == nil {
		return nil
	}
	messages, ok := value.([]any)
	if !ok {
		return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatNonTextContent)
	}
	for _, message := range messages {
		messageMap, ok := message.(map[string]any)
		if !ok {
			return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatNonTextContent)
		}
		if err := validateOpenAIChatMessageContent(messageMap["content"]); err != nil {
			return err
		}
	}
	return nil
}

func validateOpenAIChatMessageContent(value any) error {
	switch typed := value.(type) {
	case nil, string:
		return nil
	case []any:
		for _, part := range typed {
			partMap, ok := part.(map[string]any)
			if !ok {
				return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatNonTextContent)
			}
			switch strings.ToLower(strings.TrimSpace(stringValue(partMap["type"]))) {
			case "", "text":
				if _, ok := partMap["text"].(string); !ok && partMap["text"] != nil {
					return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatNonTextContent)
				}
			default:
				return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatNonTextContent)
			}
		}
		return nil
	default:
		return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatNonTextContent)
	}
}

func validateOpenAIResponsesInputShape(value any) error {
	switch typed := value.(type) {
	case string:
		return nil
	case map[string]any:
		return validateOpenAIResponsesTopLevelInputItem(typed)
	case []any:
		for _, item := range typed {
			itemMap, ok := item.(map[string]any)
			if !ok {
				return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesNonTextInput)
			}
			if err := validateOpenAIResponsesTopLevelInputItem(itemMap); err != nil {
				return err
			}
		}
		return nil
	default:
		return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesNonTextInput)
	}
}

func validateOpenAIResponsesTopLevelInputItem(item map[string]any) error {
	switch strings.ToLower(strings.TrimSpace(stringValue(item["type"]))) {
	case "input_text":
		return validateOpenAIResponsesInputTextItem(item)
	case "message":
		return validateOpenAIResponsesMessageItem(item)
	default:
		return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesNonTextInput)
	}
}

func validateOpenAIResponsesInputTextItem(item map[string]any) error {
	if _, ok := item["text"].(string); !ok && item["text"] != nil {
		return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesNonTextInput)
	}
	return nil
}

func validateOpenAIResponsesMessageItem(item map[string]any) error {
	switch typed := item["content"].(type) {
	case nil, string:
		return nil
	case []any:
		for _, part := range typed {
			partMap, ok := part.(map[string]any)
			if !ok {
				return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesNonTextInput)
			}
			if strings.ToLower(strings.TrimSpace(stringValue(partMap["type"]))) != "input_text" {
				return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesNonTextInput)
			}
			if err := validateOpenAIResponsesInputTextItem(partMap); err != nil {
				return err
			}
		}
		return nil
	default:
		return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesNonTextInput)
	}
}

func validateOpenAIChatToolsShape(value any) error {
	if value == nil {
		return nil
	}
	tools, ok := value.([]any)
	if !ok {
		return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatUnsafeToolShape)
	}
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if !ok {
			return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatUnsafeToolShape)
		}
		switch strings.ToLower(strings.TrimSpace(stringValue(toolMap["type"]))) {
		case "", "function":
			if functionValue := toolMap["function"]; functionValue != nil {
				if _, ok := functionValue.(map[string]any); !ok {
					return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatUnsafeToolShape)
				}
			}
		default:
			return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonChatUnsafeToolShape)
		}
	}
	return nil
}

func validateOpenAIToolsShape(value any) error {
	if value == nil {
		return nil
	}
	tools, ok := value.([]any)
	if !ok {
		return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesUnsafeToolShape)
	}
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if !ok {
			return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesUnsafeToolShape)
		}
		if isContextUnsafeToolType(stringValue(toolMap["type"])) {
			return contextEstimationUnavailableDomainError(requestContextEstimationUnavailableReasonResponsesUnsafeToolShape)
		}
	}
	return nil
}

func isContextUnsafeToolType(toolType string) bool {
	switch strings.ToLower(strings.TrimSpace(toolType)) {
	case "web_search", "web_search_preview", "file_search", "mcp", "code_interpreter", "computer_use", "computer_use_preview":
		return true
	default:
		return false
	}
}

type geminiGenerationParamsStreamingObserver struct {
	mu       sync.Mutex
	parser   geminiGenerationParamsStreamingParser
	finished bool
}

func newGeminiGenerationParamsStreamingObserver(captureLimit ...int) *geminiGenerationParamsStreamingObserver {
	limit := geminiGenerationParamsStreamingCaptureLimit
	if len(captureLimit) > 0 && captureLimit[0] > 0 {
		limit = captureLimit[0]
	}
	return &geminiGenerationParamsStreamingObserver{parser: newGeminiGenerationParamsStreamingParser(limit)}
}
func (observer *geminiGenerationParamsStreamingObserver) Observe(payload []byte) {
	if observer == nil || len(payload) == 0 {
		return
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if observer.finished || observer.parser.terminal() {
		return
	}
	observer.parser.observe(payload)
}
func (observer *geminiGenerationParamsStreamingObserver) Write(payload []byte) (int, error) {
	observer.Observe(payload)
	return len(payload), nil
}
func (observer *geminiGenerationParamsStreamingObserver) Finish() {
	if observer == nil {
		return
	}
	observer.mu.Lock()
	observer.finished = true
	observer.parser.finish()
	observer.mu.Unlock()
}
func (observer *geminiGenerationParamsStreamingObserver) Snapshot() requestGenerationParamsSnapshot {
	if observer == nil {
		return requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusMissing}
	}
	observer.mu.Lock()
	finished := observer.finished
	snapshot := observer.parser.snapshot()
	observer.mu.Unlock()
	if !finished {
		return requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusIncomplete}
	}
	return snapshot
}

type geminiGenerationParamsStreamingParser struct {
	limit       int
	stack       []geminiJSONFrame
	params      requestGenerationParams
	sawPayload  bool
	malformed   bool
	oversize    bool
	state       geminiJSONScanState
	stringRole  geminiJSONStringRole
	stringPath  []string
	tokenPath   []string
	token       []byte
	stringToken []byte
	escaped     bool
	escapeNext  bool
}

type geminiJSONFrame struct {
	kind         byte
	key          string
	expectingKey bool
	pendingKey   string
}

type geminiJSONScanState int

const (
	geminiJSONScanNormal geminiJSONScanState = iota
	geminiJSONScanString
	geminiJSONScanNumber
	geminiJSONScanLiteral
)

type geminiJSONStringRole int

const (
	geminiJSONStringSkipped geminiJSONStringRole = iota
	geminiJSONStringKey
	geminiJSONStringScalar
)

func newGeminiGenerationParamsStreamingParser(limit int) geminiGenerationParamsStreamingParser {
	return geminiGenerationParamsStreamingParser{limit: limit, params: requestGenerationParams{Provider: "gemini"}}
}

func (parser *geminiGenerationParamsStreamingParser) terminal() bool {
	return parser.malformed || parser.oversize
}

func (parser *geminiGenerationParamsStreamingParser) observe(payload []byte) {
	for index := 0; index < len(payload); index++ {
		if parser.terminal() {
			return
		}
		current := payload[index]
		if parser.state == geminiJSONScanString {
			parser.observeStringByte(current)
			continue
		}
		if parser.state == geminiJSONScanNumber || parser.state == geminiJSONScanLiteral {
			if isGeminiJSONTokenByte(current) {
				parser.appendTokenByte(current)
				continue
			}
			parser.finishToken()
			index--
			continue
		}
		if isGeminiJSONWhitespace(current) {
			continue
		}
		parser.sawPayload = true
		switch current {
		case '{':
			parser.startContainer('{')
		case '[':
			parser.startContainer('[')
		case '}':
			parser.endContainer('{')
		case ']':
			parser.endContainer('[')
		case ',':
			parser.afterComma()
		case ':':
		case '"':
			parser.startString()
		case 't', 'f', 'n':
			parser.startToken(geminiJSONScanLiteral, current)
		default:
			if current == '-' || (current >= '0' && current <= '9') {
				parser.startToken(geminiJSONScanNumber, current)
			} else {
				parser.malformed = true
			}
		}
	}
}

func (parser *geminiGenerationParamsStreamingParser) finish() {
	if parser.state != geminiJSONScanNormal || len(parser.stack) != 0 {
		parser.malformed = true
	}
}

func (parser *geminiGenerationParamsStreamingParser) snapshot() requestGenerationParamsSnapshot {
	if parser.oversize {
		return requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusSkippedOversize}
	}
	if parser.malformed {
		return requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusMalformed}
	}
	if !parser.sawPayload {
		return requestGenerationParamsSnapshot{Params: &requestGenerationParams{Provider: "gemini"}, Status: requestGenerationParamsStatusMissing}
	}
	params := parser.params
	status := requestGenerationParamsStatusComplete
	if !params.hasGenerationFields() {
		status = requestGenerationParamsStatusMissing
	}
	return requestGenerationParamsSnapshot{Params: &params, Status: status}
}

func (parser *geminiGenerationParamsStreamingParser) startContainer(kind byte) {
	key := parser.consumePendingValueKey()
	frame := geminiJSONFrame{kind: kind, key: key}
	if kind == '{' {
		frame.expectingKey = true
	}
	parser.stack = append(parser.stack, frame)
}

func (parser *geminiGenerationParamsStreamingParser) endContainer(kind byte) {
	if len(parser.stack) == 0 || parser.stack[len(parser.stack)-1].kind != kind {
		parser.malformed = true
		return
	}
	parser.stack = parser.stack[:len(parser.stack)-1]
}

func (parser *geminiGenerationParamsStreamingParser) afterComma() {
	if len(parser.stack) == 0 {
		parser.malformed = true
		return
	}
	frame := &parser.stack[len(parser.stack)-1]
	if frame.kind == '{' {
		frame.expectingKey = true
		frame.pendingKey = ""
	}
}

func (parser *geminiGenerationParamsStreamingParser) startString() {
	parser.state = geminiJSONScanString
	parser.stringToken = parser.stringToken[:0]
	parser.escaped = false
	parser.escapeNext = false
	parser.stringPath = parser.stringPath[:0]
	parser.stringRole = geminiJSONStringSkipped
	if parser.insideObjectExpectingKey() {
		parser.stringRole = geminiJSONStringKey
		return
	}
	path := parser.currentValuePath()
	if isGeminiTargetStringPath(path) {
		parser.stringRole = geminiJSONStringScalar
		parser.stringPath = append(parser.stringPath, path...)
	}
}

func (parser *geminiGenerationParamsStreamingParser) observeStringByte(current byte) {
	if parser.escapeNext {
		parser.escapeNext = false
		parser.escaped = true
		parser.appendStringByte(current)
		return
	}
	if current == '\\' {
		parser.escapeNext = true
		parser.escaped = true
		parser.appendStringByte(current)
		return
	}
	if current == '"' {
		parser.finishString()
		return
	}
	parser.appendStringByte(current)
}

func (parser *geminiGenerationParamsStreamingParser) appendStringByte(current byte) {
	if parser.stringRole == geminiJSONStringSkipped {
		return
	}
	if len(parser.stringToken)+1 > parser.limit {
		parser.oversize = true
		return
	}
	parser.stringToken = append(parser.stringToken, current)
}

func (parser *geminiGenerationParamsStreamingParser) finishString() {
	value := string(parser.stringToken)
	if parser.escaped {
		unquoted, err := strconv.Unquote("\"" + value + "\"")
		if err != nil {
			parser.malformed = true
			return
		}
		value = unquoted
	}
	switch parser.stringRole {
	case geminiJSONStringKey:
		if len(parser.stack) == 0 || parser.stack[len(parser.stack)-1].kind != '{' {
			parser.malformed = true
			return
		}
		frame := &parser.stack[len(parser.stack)-1]
		frame.pendingKey = value
		frame.expectingKey = false
	case geminiJSONStringScalar:
		parser.applyGeminiScalar(parser.stringPath, value)
		parser.consumePendingValueKey()
	}
	parser.state = geminiJSONScanNormal
	parser.stringToken = parser.stringToken[:0]
	parser.stringPath = parser.stringPath[:0]
}

func (parser *geminiGenerationParamsStreamingParser) startToken(state geminiJSONScanState, first byte) {
	parser.state = state
	parser.token = parser.token[:0]
	parser.tokenPath = parser.tokenPath[:0]
	parser.tokenPath = append(parser.tokenPath, parser.currentValuePath()...)
	parser.appendTokenByte(first)
}

func (parser *geminiGenerationParamsStreamingParser) appendTokenByte(current byte) {
	if len(parser.token)+1 > parser.limit {
		parser.oversize = true
		return
	}
	parser.token = append(parser.token, current)
}

func (parser *geminiGenerationParamsStreamingParser) finishToken() {
	token := string(parser.token)
	if parser.state == geminiJSONScanNumber {
		parser.applyGeminiScalar(parser.tokenPath, json.Number(token))
	} else if token == "true" || token == "false" {
		parser.applyGeminiScalar(parser.tokenPath, token == "true")
	} else if token != "null" {
		parser.malformed = true
	}
	parser.consumePendingValueKey()
	parser.state = geminiJSONScanNormal
	parser.token = parser.token[:0]
	parser.tokenPath = parser.tokenPath[:0]
}

func (parser *geminiGenerationParamsStreamingParser) insideObjectExpectingKey() bool {
	if len(parser.stack) == 0 {
		return false
	}
	frame := parser.stack[len(parser.stack)-1]
	return frame.kind == '{' && frame.expectingKey
}

func (parser *geminiGenerationParamsStreamingParser) currentValuePath() []string {
	path := make([]string, 0, len(parser.stack)+1)
	for _, frame := range parser.stack {
		if frame.key != "" {
			path = append(path, frame.key)
		}
	}
	if len(parser.stack) > 0 {
		frame := parser.stack[len(parser.stack)-1]
		if frame.kind == '{' && frame.pendingKey != "" {
			path = append(path, frame.pendingKey)
		}
	}
	return path
}

func (parser *geminiGenerationParamsStreamingParser) consumePendingValueKey() string {
	if len(parser.stack) == 0 {
		return ""
	}
	frame := &parser.stack[len(parser.stack)-1]
	if frame.kind != '{' {
		return ""
	}
	key := frame.pendingKey
	frame.pendingKey = ""
	return key
}

func (parser *geminiGenerationParamsStreamingParser) applyGeminiScalar(path []string, value any) {
	if len(path) == 2 && path[0] == "generationConfig" {
		switch path[1] {
		case "temperature":
			parser.params.Temperature = floatPointerFromAny(value)
		case "topP":
			parser.params.TopP = floatPointerFromAny(value)
		case "topK":
			parser.params.TopK = intPointerFromAny(value)
		case "maxOutputTokens":
			if maxOutputTokens := intPointerFromAny(value); maxOutputTokens != nil {
				parser.params.MaxOutputTokens = maxOutputTokens
				parser.params.MaxOutputTokensSource = stringPtr("generationConfig.maxOutputTokens")
			}
		}
		return
	}
	if len(path) != 3 || path[0] != "generationConfig" || path[1] != "thinkingConfig" {
		return
	}
	switch path[2] {
	case "thinkingBudget":
		if budget := intPointerFromAny(value); budget != nil {
			reasoning := parser.params.ensureReasoning()
			reasoning.BudgetTokens = budget
			reasoning.SourceField = stringPtr("generationConfig.thinkingConfig")
		}
	case "thinkingLevel":
		if level := trimmedStringFromAny(value); level != nil {
			reasoning := parser.params.ensureReasoning()
			reasoning.Effort = level
			reasoning.SourceField = stringPtr("generationConfig.thinkingConfig")
		}
	case "includeThoughts":
		if includeThoughts, ok := boolPointerFromAny(value); ok {
			reasoning := parser.params.ensureReasoning()
			reasoning.IncludeThoughts = includeThoughts
			reasoning.SourceField = stringPtr("generationConfig.thinkingConfig")
		}
	}
}

func isGeminiTargetStringPath(path []string) bool {
	return len(path) == 3 && path[0] == "generationConfig" && path[1] == "thinkingConfig" && path[2] == "thinkingLevel"
}

func isGeminiJSONWhitespace(current byte) bool {
	return current == ' ' || current == '\n' || current == '\r' || current == '\t'
}

func isGeminiJSONTokenByte(current byte) bool {
	return !isGeminiJSONWhitespace(current) && current != ',' && current != '}' && current != ']'
}

type requestGenerationParamsObservingReadCloser struct {
	source   io.ReadCloser
	observer *geminiGenerationParamsStreamingObserver
}

func (reader *requestGenerationParamsObservingReadCloser) Read(payload []byte) (int, error) {
	n, err := reader.source.Read(payload)
	if n > 0 {
		reader.observer.Observe(payload[:n])
	}
	if err == io.EOF {
		reader.observer.Finish()
	}
	return n, err
}
func (reader *requestGenerationParamsObservingReadCloser) Close() error { return reader.source.Close() }

func floatPointerFromAny(value any) *float64 {
	switch typed := value.(type) {
	case json.Number:
		resolved, err := typed.Float64()
		if err != nil {
			return nil
		}
		return &resolved
	case float64:
		resolved := typed
		return &resolved
	case int:
		resolved := float64(typed)
		return &resolved
	case int64:
		resolved := float64(typed)
		return &resolved
	default:
		return nil
	}
}
func boolPointerFromAny(value any) (*bool, bool) {
	resolved, ok := value.(bool)
	if !ok {
		return nil, false
	}
	return &resolved, true
}
func trimmedStringFromAny(value any) *string {
	resolved, ok := value.(string)
	if !ok {
		return nil
	}
	trimmed := strings.TrimSpace(resolved)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
