package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

func TestRequestGenerationParamsByOperation(t *testing.T) {
	tests := []struct {
		name, requestPath string
		rawBody           []byte
		want              requestGenerationParamsSnapshot
	}{
		{"openai chat completions", "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hidden"}],"temperature":0.7,"top_p":0.9,"max_completion_tokens":1024,"reasoning_effort":"low"}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "openai", Temperature: floatPtr(0.7), TopP: floatPtr(0.9), MaxOutputTokens: intPtr(1024), MaxOutputTokensSource: stringPtr("max_completion_tokens"), Reasoning: &requestGenerationReasoningParams{Effort: stringPtr("low"), SourceField: stringPtr("reasoning_effort")}}}},
		{"openai responses", "/v1/responses", []byte(`{"model":"gpt-4o","input":"hidden","temperature":0.4,"top_p":0.8,"max_output_tokens":256,"reasoning":{"effort":"medium"}}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "openai", Temperature: floatPtr(0.4), TopP: floatPtr(0.8), MaxOutputTokens: intPtr(256), MaxOutputTokensSource: stringPtr("max_output_tokens"), Reasoning: &requestGenerationReasoningParams{Effort: stringPtr("medium"), SourceField: stringPtr("reasoning.effort")}}}},
		{"openai responses chat token alias", "/v1/responses", []byte(`{"model":"gpt-4o","input":"hidden","max_tokens":333}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "openai", MaxOutputTokens: intPtr(333), MaxOutputTokensSource: stringPtr("max_tokens")}}},
		{"openai image operation skipped", "/v1/images/generations", []byte(`{"model":"gpt-image-1","temperature":0.7,"top_p":0.9,"max_tokens":64}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusMissing}},
		{"anthropic messages", "/v1/messages", []byte(`{"model":"claude","messages":[{"role":"user","content":"hidden"}],"temperature":0.6,"top_p":0.95,"max_tokens":512,"thinking":{"type":"enabled","budget_tokens":2048},"output_config":{"effort":"high"}}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "anthropic", Temperature: floatPtr(0.6), TopP: floatPtr(0.95), MaxOutputTokens: intPtr(512), MaxOutputTokensSource: stringPtr("max_tokens"), Reasoning: &requestGenerationReasoningParams{Effort: stringPtr("high"), Mode: stringPtr("enabled"), BudgetTokens: intPtr(2048), SourceField: stringPtr("output_config.effort")}}}},
		{"gemini generate content", "/v1beta/models/gemini:generateContent", []byte(`{"contents":[{"parts":[{"text":"hidden"}]}],"generationConfig":{"temperature":0.3,"topP":0.7,"topK":40,"maxOutputTokens":777,"thinkingConfig":{"thinkingBudget":123,"thinkingLevel":"high","includeThoughts":true}}}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "gemini", Temperature: floatPtr(0.3), TopP: floatPtr(0.7), TopK: intPtr(40), MaxOutputTokens: intPtr(777), MaxOutputTokensSource: stringPtr("generationConfig.maxOutputTokens"), Reasoning: &requestGenerationReasoningParams{Effort: stringPtr("high"), BudgetTokens: intPtr(123), IncludeThoughts: boolPtr(true), SourceField: stringPtr("generationConfig.thinkingConfig")}}}},
		{"gemini malformed", "/v1beta/models/gemini:generateContent", []byte(`{"generationConfig":`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusMalformed}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operationMatch := mustResolveRequestGenerationOperation(t, test.requestPath)
			original := append([]byte(nil), test.rawBody...)
			got := extractBufferedRequestGenerationParams(operationMatch.Operation, test.rawBody)
			if !reflect.DeepEqual(got, test.want) {
				gotJSON, _ := json.Marshal(got)
				wantJSON, _ := json.Marshal(test.want)
				t.Fatalf("expected %s, got %s", wantJSON, gotJSON)
			}
			if !bytes.Equal(test.rawBody, original) {
				t.Fatal("buffered extraction mutated raw request bytes")
			}
		})
	}
}

func TestOpenAIChatTokenizerGPT5DependencyLoadsOfflineModelsAndEncodings(t *testing.T) {
	roundTripper := &tokenizerNetworkBlocker{}
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripper
	defer func() { http.DefaultTransport = oldTransport }()

	for _, encoding := range []tokenizer.Encoding{tokenizer.O200kBase, tokenizer.Cl100kBase} {
		codec, err := tokenizer.Get(encoding)
		if err != nil {
			t.Fatalf("load encoding %s: %v", encoding, err)
		}
		if count, err := codec.Count("offline tokenizer smoke"); err != nil || count <= 0 {
			t.Fatalf("expected %s to count offline, count=%d err=%v", encoding, count, err)
		}
	}
	modelEncodings := map[string]string{"gpt-5": "o200k_base", "gpt-5.5": "o200k_base", "gpt-5.4": "o200k_base", "gpt-5.4-mini": "o200k_base", "gpt-5.4-nano": "o200k_base", "gpt-5.3-codex": "o200k_base", "gpt-5.3-codex-spark": "o200k_base", "gpt-5-2025-08-07": "o200k_base", "gpt-4.1": "o200k_base", "gpt-4.1-2025-04-14": "o200k_base", "gpt-4o": "o200k_base", "gpt-4o-2024-08-06": "o200k_base", "gpt-4": "cl100k_base"}
	for modelID, wantEncoding := range modelEncodings {
		codec, err := openAITokenizerForModel(modelID)
		if err != nil {
			t.Fatalf("load tokenizer for %s: %v", modelID, err)
		}
		if codec.GetName() != wantEncoding {
			t.Fatalf("expected %s to use %s, got %s", modelID, wantEncoding, codec.GetName())
		}
	}
	if calls := roundTripper.calls.Load(); calls != 0 {
		t.Fatalf("expected tokenizer construction/counting to avoid runtime network fetches, got %d HTTP calls", calls)
	}
}

func TestOpenAIChatTokenizerCountsMessagesToolsAndResponseFormat(t *testing.T) {
	contextWindowTokens := 10_000
	rawBody := []byte(`{"model":"gpt-4o","messages":[{"role":"system","content":"You are terse."},{"role":"user","name":"tester","content":[{"type":"text","text":"Summarize this request."}]},{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"summarize","arguments":"{\"topic\":\"routing\"}"}}],"function_call":{"name":"legacy","arguments":"{}"}},{"role":"tool","tool_call_id":"call_1","content":"done"}],"tools":[{"type":"function","function":{"name":"summarize","parameters":{"type":"object","properties":{"topic":{"type":"string"}}}}},{"function":{"name":"implicit","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"summarize"}},"functions":[{"name":"legacy","parameters":{"type":"object"}}],"function_call":{"name":"legacy"},"response_format":{"type":"json_schema","json_schema":{"name":"summary","schema":{"type":"object"}}},"max_completion_tokens":512}`)
	estimation, err := estimateOpenAIChatCompletionsRequestTokens(rawBody, requestContextEstimationOptions{ModelID: "gpt-4o", ContextWindowTokens: &contextWindowTokens})
	if err != nil {
		t.Fatalf("estimate chat request tokens: %v", err)
	}
	if estimation == nil {
		t.Fatal("expected chat estimation metadata")
	}
	if estimation.Method != openAIChatContextEstimationMethod {
		t.Fatalf("expected method %q, got %+v", openAIChatContextEstimationMethod, estimation)
	}
	if estimation.ReservedOutputTokens != 512 {
		t.Fatalf("expected explicit chat output reserve 512, got %+v", estimation)
	}
	if estimation.UsableContextWindowTokens == nil || *estimation.UsableContextWindowTokens != 9000 {
		t.Fatalf("expected default usable chat context 9000, got %+v", estimation)
	}
	if estimation.EstimatedInputTokens <= 0 {
		t.Fatalf("expected positive chat input estimate, got %+v", estimation)
	}
	if estimation.EstimatedTotalContextTokens != estimation.EstimatedInputTokens+512 {
		t.Fatalf("expected chat total context to include explicit reserve, got %+v", estimation)
	}
}

func TestOpenAIChatTokenizerUnknownModelUnavailableForStreamingPromotion(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
	}{
		{name: "unknown future model", modelID: "gpt-unknown-future"},
		{name: "broad gpt-5 prefix guard", modelID: "gpt-50"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`{"model":"` + test.modelID + `","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":32}`)
			_, err := estimateOpenAIChatCompletionsRequestTokens(body, requestContextEstimationOptions{ModelID: test.modelID})
			assertContextEstimationUnavailableReason(t, err, requestContextEstimationUnavailableReasonUnknownTokenizerModel)
		})
	}
}

func assertContextEstimationUnavailableReason(t *testing.T, err error, want requestContextEstimationUnavailableReason) {
	t.Helper()
	got := contextEstimationUnavailableReasonFromError(err)
	if got == nil || *got != string(want) {
		t.Fatalf("expected estimation unavailable reason %q, got reason=%v err=%v", want, got, err)
	}
}

func TestOpenAIChatTokenizerUnsupportedShapesUnavailable(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "messages not array", body: `{"model":"gpt-4o","messages":{"role":"user","content":"hello"}}`},
		{name: "message not object", body: `{"model":"gpt-4o","messages":["hello"]}`},
		{name: "numeric message content", body: `{"model":"gpt-4o","messages":[{"role":"user","content":42}]}`},
		{name: "content part not object", body: `{"model":"gpt-4o","messages":[{"role":"user","content":["hello"]}]}`},
		{name: "content part text not string", body: `{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"text","text":{"nested":true}}]}]}`},
		{name: "image content part", body: `{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/image.png"}}]}]}`},
		{name: "tools not array", body: `{"model":"gpt-4o","messages":[{"role":"user","content":"search"}],"tools":{"type":"function"}}`},
		{name: "tool not object", body: `{"model":"gpt-4o","messages":[{"role":"user","content":"search"}],"tools":["function"]}`},
		{name: "function payload not object", body: `{"model":"gpt-4o","messages":[{"role":"user","content":"search"}],"tools":[{"type":"function","function":"lookup"}]}`},
		{name: "unsafe tool type", body: `{"model":"gpt-4o","messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search_preview"}]}`},
		{name: "mcp tool type", body: `{"model":"gpt-4o","messages":[{"role":"user","content":"search"}],"tools":[{"type":"mcp","function":{"name":"search"}}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := estimateOpenAIChatCompletionsRequestTokens([]byte(test.body), requestContextEstimationOptions{ModelID: "gpt-4o"})
			if !isContextEstimationUnavailableError(err) {
				t.Fatalf("expected estimation unavailable, got %v", err)
			}
		})
	}
}

func TestOpenAIChatTokenizerReservedOutputOrder(t *testing.T) {
	baseBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]%s}`
	tests := []struct {
		name           string
		suffix         string
		defaultReserve *int
		wantReserve    int
	}{
		{name: "max completion wins", suffix: `,"max_completion_tokens":111,"max_tokens":222`, defaultReserve: intPtr(333), wantReserve: 111},
		{name: "max tokens second", suffix: `,"max_tokens":222`, defaultReserve: intPtr(333), wantReserve: 222},
		{name: "model default third", suffix: ``, defaultReserve: intPtr(333), wantReserve: 333},
		{name: "hard fallback last", suffix: ``, wantReserve: defaultOutputTokenReserve},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			estimation, err := estimateOpenAIChatCompletionsRequestTokens([]byte(fmt.Sprintf(baseBody, test.suffix)), requestContextEstimationOptions{ModelID: "gpt-4o", DefaultOutputTokenReserve: test.defaultReserve})
			if err != nil {
				t.Fatalf("estimate chat request tokens: %v", err)
			}
			if estimation.ReservedOutputTokens != test.wantReserve {
				t.Fatalf("expected reserve %d, got %+v", test.wantReserve, estimation)
			}
		})
	}
}

type tokenizerNetworkBlocker struct{ calls atomic.Int64 }

func (blocker *tokenizerNetworkBlocker) RoundTrip(*http.Request) (*http.Response, error) {
	blocker.calls.Add(1)
	return nil, errors.New("tokenizer test blocked runtime HTTP fetch")
}

func TestEstimateOpenAIResponsesRequestTokens(t *testing.T) {
	defaultOutputReserve := 2048
	contextWindowTokens := 20_000
	maxContextUtilization := 0.75
	estimation, err := estimateOpenAIResponsesRequestTokens([]byte(`{"model":"gpt-4o","instructions":"Be concise.","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Summarize this request."}]}],"text":{"format":{"type":"text"}}}`), requestContextEstimationOptions{DefaultOutputTokenReserve: &defaultOutputReserve, ContextWindowTokens: &contextWindowTokens, MaxContextUtilization: &maxContextUtilization})
	if err != nil {
		t.Fatalf("estimate responses request tokens: %v", err)
	}
	if estimation == nil {
		t.Fatal("expected responses estimation metadata")
	}
	if estimation.Method != openAIResponsesContextEstimationMethod {
		t.Fatalf("expected method %q, got %+v", openAIResponsesContextEstimationMethod, estimation)
	}
	if estimation.ReservedOutputTokens != defaultOutputReserve {
		t.Fatalf("expected default output reserve %d, got %+v", defaultOutputReserve, estimation)
	}
	if estimation.UsableContextWindowTokens == nil || *estimation.UsableContextWindowTokens != 15_000 {
		t.Fatalf("expected override usable context 15000, got %+v", estimation)
	}
	if estimation.EstimatedInputTokens <= 0 {
		t.Fatalf("expected positive responses input estimate, got %+v", estimation)
	}
	if estimation.EstimatedTotalContextTokens != estimation.EstimatedInputTokens+defaultOutputReserve {
		t.Fatalf("expected responses total context to include default reserve, got %+v", estimation)
	}
}

func TestOpenAIResponsesTokenizerCountsTextInputsInstructionsToolsAndTextConfig(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-4o","instructions":"Follow the rubric.","input":[{"type":"input_text","text":"Direct text."},{"type":"message","role":"user","content":[{"type":"input_text","text":"Nested text."}]},{"type":"message","role":"assistant","content":"Message string."}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"query":{"type":"string"}}}}],"tool_choice":{"type":"function","name":"lookup"},"text":{"format":{"type":"json_schema","name":"answer","schema":{"type":"object","properties":{"answer":{"type":"string"}}}}},"max_output_tokens":123}`)
	codec, err := openAITokenizerForModel("gpt-4o")
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}
	payload, err := decodeRequestContextEstimationPayload(rawBody)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	expectedInputTokens := 0
	for _, text := range []string{"Direct text.", "Nested text.", "Message string.", "Follow the rubric."} {
		count, err := tokenizerTokensForString(codec, text)
		if err != nil {
			t.Fatalf("count %q: %v", text, err)
		}
		expectedInputTokens += count
	}
	for _, field := range []string{"tools", "tool_choice", "text"} {
		count, err := tokenizerTokensForSerializedJSON(codec, payload[field])
		if err != nil {
			t.Fatalf("count serialized %s: %v", field, err)
		}
		expectedInputTokens += count
	}
	estimation, err := estimateOpenAIResponsesRequestTokens(rawBody, requestContextEstimationOptions{ModelID: "gpt-4o"})
	if err != nil {
		t.Fatalf("estimate responses request tokens: %v", err)
	}
	if estimation == nil {
		t.Fatal("expected responses estimation metadata")
	}
	if estimation.Method != openAIResponsesContextEstimationMethod {
		t.Fatalf("expected method %q, got %+v", openAIResponsesContextEstimationMethod, estimation)
	}
	if estimation.ReservedOutputTokens != 123 {
		t.Fatalf("expected explicit output reserve 123, got %+v", estimation)
	}
	if estimation.EstimatedInputTokens != expectedInputTokens {
		t.Fatalf("expected tokenizer input estimate %d, got %+v", expectedInputTokens, estimation)
	}
	if estimation.EstimatedTotalContextTokens != expectedInputTokens+123 {
		t.Fatalf("expected total context to include tokenizer input and reserve, got %+v", estimation)
	}
}

func TestOpenAIResponsesTokenizerUnknownModelUnavailable(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		options requestContextEstimationOptions
	}{
		{name: "unknown request model", body: `{"model":"gpt-unknown-future","input":"hello"}`},
		{name: "unknown resolved model", body: `{"model":"gpt-4o","input":"hello"}`, options: requestContextEstimationOptions{ModelID: "gpt-unknown-future"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := estimateOpenAIResponsesRequestTokens([]byte(test.body), test.options)
			assertContextEstimationUnavailableReason(t, err, requestContextEstimationUnavailableReasonUnknownTokenizerModel)
		})
	}
}

func TestOpenAIResponsesTokenizerEligibleTextShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "plain string input", body: `{"model":"gpt-4o","input":"hello"}`},
		{name: "top level input text item", body: `{"model":"gpt-4o","input":{"type":"input_text","text":"hello"}}`},
		{name: "top level input text nil text", body: `{"model":"gpt-4o","input":{"type":"input_text","text":null}}`},
		{name: "array input text items", body: `{"model":"gpt-4o","input":[{"type":"input_text","text":"hello"},{"type":"input_text","text":null}]}`},
		{name: "message nil content", body: `{"model":"gpt-4o","input":[{"type":"message","role":"user","content":null}]}`},
		{name: "message string content", body: `{"model":"gpt-4o","input":[{"type":"message","role":"user","content":"hello"}]}`},
		{name: "message input text content array", body: `{"model":"gpt-4o","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_text","text":null}]}]}`},
		{name: "instructions tool choice text config", body: `{"model":"gpt-4o","instructions":"Be concise.","input":"hello","tool_choice":{"type":"function","name":"lookup"},"text":{"format":{"type":"json_schema","name":"answer","schema":{"type":"object"}}}}`},
		{name: "safe tools", body: `{"model":"gpt-4o","input":"hello","tools":[{}, {"type":"function","name":"lookup","parameters":{"type":"object"}}, {"type":"custom","name":"local"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			estimation, err := estimateOpenAIResponsesRequestTokens([]byte(test.body), requestContextEstimationOptions{ModelID: "gpt-4o"})
			if err != nil {
				t.Fatalf("estimate responses request tokens: %v", err)
			}
			if estimation == nil || estimation.Method != openAIResponsesContextEstimationMethod {
				t.Fatalf("expected responses estimation metadata, got %+v", estimation)
			}
		})
	}
}

func TestOpenAIResponsesTokenizerUnavailableStatefulAndMultimodalShapes(t *testing.T) {
	unsafeToolTypes := []string{"web_search", "web_search_preview", "file_search", "mcp", "code_interpreter", "computer_use", "computer_use_preview"}
	tests := []struct {
		name string
		body string
	}{
		{name: "previous response id", body: `{"model":"gpt-4o","previous_response_id":"resp_123","input":"hello"}`},
		{name: "previous response id null still present", body: `{"model":"gpt-4o","previous_response_id":null,"input":"hello"}`},
		{name: "conversation", body: `{"model":"gpt-4o","conversation":"conv_123","input":"hello"}`},
		{name: "conversation null still present", body: `{"model":"gpt-4o","conversation":null,"input":"hello"}`},
		{name: "input absent", body: `{"model":"gpt-4o","instructions":"hello"}`},
		{name: "input null", body: `{"model":"gpt-4o","input":null}`},
		{name: "top level input image inline", body: `{"model":"gpt-4o","input":[{"type":"input_image","image_url":"data:image/png;base64,AAAA","detail":"low"}]}`},
		{name: "top level input image remote", body: `{"model":"gpt-4o","input":[{"type":"input_image","image_url":"https://example.invalid/image.png"}]}`},
		{name: "top level input image file id", body: `{"model":"gpt-4o","input":[{"type":"input_image","file_id":"file_123"}]}`},
		{name: "top level input file inline", body: `{"model":"gpt-4o","input":[{"type":"input_file","file_data":"data:application/pdf;base64,AAAA"}]}`},
		{name: "top level input file remote", body: `{"model":"gpt-4o","input":[{"type":"input_file","file_url":"https://example.invalid/file.pdf"}]}`},
		{name: "top level input file id", body: `{"model":"gpt-4o","input":[{"type":"input_file","file_id":"file_123"}]}`},
		{name: "function call", body: `{"model":"gpt-4o","input":[{"type":"function_call","name":"lookup","arguments":"{}"}]}`},
		{name: "function call output", body: `{"model":"gpt-4o","input":[{"type":"function_call_output","call_id":"call_1","output":"done"}]}`},
		{name: "other non text item", body: `{"model":"gpt-4o","input":[{"type":"output_text","text":"hello"}]}`},
		{name: "top level input text object text", body: `{"model":"gpt-4o","input":{"type":"input_text","text":{"value":"hello"}}}`},
		{name: "message content part not object", body: `{"model":"gpt-4o","input":[{"type":"message","role":"user","content":["hello"]}]}`},
		{name: "message content output text", body: `{"model":"gpt-4o","input":[{"type":"message","role":"user","content":[{"type":"output_text","text":"hello"}]}]}`},
		{name: "message content input image", body: `{"model":"gpt-4o","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,AAAA","detail":"low"}]}]}`},
		{name: "message content input file", body: `{"model":"gpt-4o","input":[{"type":"message","role":"user","content":[{"type":"input_file","file_data":"data:application/pdf;base64,AAAA"}]}]}`},
		{name: "message content function call", body: `{"model":"gpt-4o","input":[{"type":"message","role":"assistant","content":[{"type":"function_call","name":"lookup","arguments":"{}"}]}]}`},
		{name: "message content function call output", body: `{"model":"gpt-4o","input":[{"type":"message","role":"tool","content":[{"type":"function_call_output","call_id":"call_1","output":"done"}]}]}`},
		{name: "message content input text number", body: `{"model":"gpt-4o","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":42}]}]}`},
		{name: "tools not array", body: `{"model":"gpt-4o","input":"hello","tools":{"type":"function"}}`},
		{name: "tool not object", body: `{"model":"gpt-4o","input":"hello","tools":["function"]}`},
	}
	for _, toolType := range unsafeToolTypes {
		tests = append(tests, struct {
			name string
			body string
		}{
			name: "unsafe tool " + toolType,
			body: fmt.Sprintf(`{"model":"gpt-4o","input":"hello","tools":[{"type":%q}]}`, toolType),
		})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := estimateOpenAIResponsesRequestTokens([]byte(test.body), requestContextEstimationOptions{ModelID: "gpt-4o"})
			if !isContextEstimationUnavailableError(err) {
				t.Fatalf("expected estimation unavailable, got %v", err)
			}
		})
	}
}

func TestRequestGenerationParamsContextFitHelper(t *testing.T) {
	estimation := &requestContextEstimation{EstimatedTotalContextTokens: 600}
	if !estimation.fitsUsableContextWindowTokens(600) {
		t.Fatal("expected equal estimated and usable context tokens to fit")
	}
	if estimation.fitsUsableContextWindowTokens(599) {
		t.Fatal("expected estimated tokens above usable context window to be rejected")
	}
	var missing *requestContextEstimation
	if missing.fitsUsableContextWindowTokens(600) {
		t.Fatal("expected missing estimation to never fit")
	}
	if estimation.fitsUsableContextWindowTokens(0) {
		t.Fatal("expected unavailable usable context metadata to never fit")
	}
}

func TestGeminiStreamingObserverByOperation(t *testing.T) {
	streamOperation := mustResolveRequestGenerationOperation(t, "/v1beta/models/gemini:streamGenerateContent").Operation
	generateOperation := mustResolveRequestGenerationOperation(t, "/v1beta/models/gemini:generateContent").Operation
	streamFlagBody := []byte(`{"stream":true}`)
	if !canStreamIncomingRequestBody(requestPlan{APIFamily: "gemini"}, streamOperation) {
		t.Fatal("expected streamGenerateContent operation to allow streaming request-generation observation")
	}
	if canStreamIncomingRequestBody(requestPlan{APIFamily: "gemini"}, generateOperation) {
		t.Fatal("expected non-stream generateContent operation to use buffered request-generation extraction")
	}
	if requestWantsStreamForOperation(generateOperation, streamFlagBody, "/v1beta/models/gemini:generateContent") {
		t.Fatal("expected generateContent to ignore body stream:true for runtime stream classification")
	}
	if !requestWantsStreamForOperation(streamOperation, nil, "/v1beta/models/gemini:streamGenerateContent") {
		t.Fatal("expected streamGenerateContent path to force runtime stream classification")
	}
	if _, ok := newRequestGenerationParamsStreamingObserver(generateOperation); ok {
		t.Fatal("expected generateContent to have no streaming request-generation observer hook")
	}
	observer, ok := newRequestGenerationParamsStreamingObserver(streamOperation)
	if !ok {
		t.Fatal("expected streamGenerateContent to provide a streaming request-generation observer hook")
	}
	for _, chunk := range []string{`{"contents":[`, `{"parts":[{"text":"hidden"}]}`, `],"generationConfig":{"temperature":0.5,"topP":0.8,"topK":32,"maxOutputTokens":99,"thinkingConfig":{"thinkingBudget":11,"thinkingLevel":"low","includeThoughts":false}}}`} {
		observer.Observe([]byte(chunk))
	}
	observer.Finish()
	snapshot := observer.Snapshot()
	if snapshot.Status != requestGenerationParamsStatusComplete || snapshot.Params == nil || snapshot.Params.MaxOutputTokens == nil || *snapshot.Params.MaxOutputTokens != 99 {
		t.Fatalf("expected operation-selected Gemini streaming params, got %+v", snapshot)
	}
}

func TestCountTokenHooksDoNotUseGenerationParsers(t *testing.T) {
	tests := []struct {
		name             string
		requestPath      string
		rawBody          []byte
		hookCollectionID string
		provider         string
	}{
		{
			name:             "anthropic count tokens",
			requestPath:      "/v1/messages/count_tokens",
			rawBody:          []byte(`{"model":"claude-3-5-sonnet","messages":[],"temperature":0.7,"top_p":0.8,"max_tokens":123,"thinking":{"type":"enabled","budget_tokens":456},"stream":true}`),
			hookCollectionID: runtimeHookCollectionAnthropicCountTokens,
			provider:         "anthropic",
		},
		{
			name:             "gemini count tokens",
			requestPath:      "/v1beta/models/gemini-2.5-pro:countTokens",
			rawBody:          []byte(`{"contents":[],"generationConfig":{"temperature":0.2,"topP":0.3,"topK":4,"maxOutputTokens":5,"thinkingConfig":{"thinkingBudget":6,"thinkingLevel":"low","includeThoughts":true}},"stream":true}`),
			hookCollectionID: runtimeHookCollectionGeminiCountTokens,
			provider:         "gemini",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRequestGenerationOperation(t, test.requestPath).Operation
			if operation.HookCollectionID != test.hookCollectionID {
				t.Fatalf("expected hook collection %q, got %q", test.hookCollectionID, operation.HookCollectionID)
			}
			hooks, ok := requestHooksForOperation(operation)
			if !ok {
				t.Fatalf("expected dedicated request hooks for %s", operation.Name)
			}
			if hooks.Provider != test.provider {
				t.Fatalf("expected provider %q, got %q", test.provider, hooks.Provider)
			}
			responseHooks, ok := responseHooksForOperation(operation)
			if !ok {
				t.Fatalf("expected dedicated response hooks for %s", operation.Name)
			}
			if responseHooks.Provider != test.provider || responseHooks.Kind != operationResponseKindTokenCount {
				t.Fatalf("expected %s token-count response hooks, got %+v", test.provider, responseHooks)
			}
			if hooks.ExtractBufferedGenerationParams != nil {
				t.Fatal("expected count-token hook to omit generation-param extraction")
			}
			if hooks.NewGenerationParamsStreamingObserver != nil {
				t.Fatal("expected count-token hook to omit generation streaming observer")
			}
			if requestWantsStreamForOperation(operation, test.rawBody, test.requestPath) {
				t.Fatal("expected count-token hook to ignore generation-style stream:true")
			}
			if canStreamIncomingRequestBody(requestPlan{APIFamily: operation.APIFamily}, operation) {
				t.Fatal("expected count-token operation to reject streaming request-body semantics")
			}
			snapshot := extractBufferedRequestGenerationParams(operation, test.rawBody)
			if snapshot.Status != requestGenerationParamsStatusMissing || snapshot.Params != nil {
				t.Fatalf("expected count-token generation snapshot to stay missing, got %+v", snapshot)
			}
		})
	}
}

func mustResolveRequestGenerationOperation(t *testing.T, requestPath string) RuntimeOperationMatch {
	t.Helper()
	operationMatch, ok := ResolveRuntimeOperation(http.MethodPost, requestPath)
	if !ok {
		t.Fatalf("expected runtime operation for %s", requestPath)
	}
	return operationMatch
}

func TestGeminiGenerationParamsStreamingObserverExtractsAcrossSmallChunks(t *testing.T) {
	observer := newGeminiGenerationParamsStreamingObserver()
	for _, chunk := range []string{`{"contents":[`, `{"parts":[{"text":"hidden"}]}`, `],"generationConfig":{"temperature":0.5,"topP":0.8,"topK":32,"maxOutputTokens":99,"thinkingConfig":{"thinkingBudget":11,"thinkingLevel":"low","includeThoughts":false}}}`} {
		observer.Observe([]byte(chunk))
	}
	observer.Finish()
	snapshot := observer.Snapshot()
	if snapshot.Status != requestGenerationParamsStatusComplete || snapshot.Params == nil || snapshot.Params.MaxOutputTokens == nil || *snapshot.Params.MaxOutputTokens != 99 {
		t.Fatalf("expected complete Gemini streaming params, got %+v", snapshot)
	}
	if snapshot.Params.MaxOutputTokensSource == nil || *snapshot.Params.MaxOutputTokensSource != "generationConfig.maxOutputTokens" {
		t.Fatalf("expected max token source, got %+v", snapshot.Params)
	}
	if snapshot.Params.Reasoning == nil || snapshot.Params.Reasoning.SourceField == nil || *snapshot.Params.Reasoning.SourceField != "generationConfig.thinkingConfig" {
		t.Fatalf("expected reasoning source, got %+v", snapshot.Params)
	}
	raw, _ := json.Marshal(snapshot.Params)
	for _, forbidden := range []string{"hidden", "contents", "parts", "text"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("retained forbidden payload %q in %s", forbidden, raw)
		}
	}
}

func TestGeminiGenerationParamsStreamingObserverHandlesConfigBeforeAndAfterContents(t *testing.T) {
	for _, body := range []string{`{"generationConfig":{"temperature":0.1},"contents":[{"parts":[{"text":"hidden"}]}]}`, `{"contents":[{"parts":[{"text":"hidden"}]}],"generationConfig":{"temperature":0.1}}`} {
		observer := newGeminiGenerationParamsStreamingObserver()
		_, _ = observer.Write([]byte(body))
		observer.Finish()
		snapshot := observer.Snapshot()
		if snapshot.Status != requestGenerationParamsStatusComplete || snapshot.Params.Temperature == nil || *snapshot.Params.Temperature != 0.1 {
			t.Fatalf("expected order-independent extraction, got %+v", snapshot)
		}
	}
}

func TestGeminiGenerationParamsStreamingObserverReportsTerminalStatuses(t *testing.T) {
	tests := []struct {
		name, body string
		limit      int
		finish     bool
		want       string
	}{{"incomplete", `{"generationConfig":{"temperature":0.1}}`, 0, false, requestGenerationParamsStatusIncomplete}, {"malformed", `{"generationConfig":`, 0, true, requestGenerationParamsStatusMalformed}, {"missing", `{"contents":[{"parts":[{"text":"hidden"}]}]}`, 0, true, requestGenerationParamsStatusMissing}, {"large skipped content still parses", `{"contents":"` + strings.Repeat("x", 128) + `","generationConfig":{"temperature":0.1}}`, 32, true, requestGenerationParamsStatusComplete}, {"oversize captured scalar", `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"` + strings.Repeat("x", 128) + `"}}}`, 32, true, requestGenerationParamsStatusSkippedOversize}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observer := newGeminiGenerationParamsStreamingObserver(test.limit)
			observer.Observe([]byte(test.body))
			if test.finish {
				observer.Finish()
			}
			if got := observer.Snapshot().Status; got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func floatPtr(value float64) *float64 { return &value }
