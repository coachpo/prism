package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
)

func TestAdapterBuildsGenerateStreamAndTokenCountRequests(t *testing.T) {
	adapter := New(nil)
	tests := []struct {
		name          string
		operationName string
		wantPath      string
	}{
		{name: "generate content", operationName: OperationGenerateContent, wantPath: "/v1beta/models/target-gemini:generateContent"},
		{name: "stream generate content", operationName: OperationStreamGenerateContent, wantPath: "/v1beta/models/target-gemini:streamGenerateContent"},
		{name: "count tokens", operationName: OperationCountTokens, wantPath: "/v1beta/models/target-gemini:countTokens"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := adapter.BuildGenerateContentUpstreamRequest(context.Background(), GenerateContentUpstreamRequest{
				Operation:     provider.Operation{Name: test.operationName},
				RawBody:       []byte(`{"model":"body-should-not-change","contents":[]}`),
				TargetModelID: "target-gemini",
			})
			if err != nil {
				t.Fatalf("build upstream request: %v", err)
			}
			if request.Method != http.MethodPost || request.Path != test.wantPath {
				t.Fatalf("expected POST %s, got %+v", test.wantPath, request)
			}
			if got := adapterTestBodyModel(t, request.Body); got != "body-should-not-change" {
				t.Fatalf("expected path-bound Gemini body model to remain unchanged, got %q", got)
			}
		})
	}
}

func TestAdapterRejectsUnsupportedOperationWithTypedError(t *testing.T) {
	adapter := New(nil)
	_, err := adapter.BuildGenerateContentUpstreamRequest(context.Background(), GenerateContentUpstreamRequest{Operation: provider.Operation{Name: "gemini.unknown"}})
	var adapterErr *provider.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("expected adapter error, got %v", err)
	}
	if adapterErr.HTTPStatus != http.StatusBadRequest || adapterErr.Code != "gemini_operation_unsupported" {
		t.Fatalf("unexpected adapter error: %+v", adapterErr)
	}
}

func TestExtractGenerateContentUsagePreservesGeminiSplits(t *testing.T) {
	usage := ExtractGenerateContentUsage([]byte(`{"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":17,"totalTokenCount":99,"cachedContentTokenCount":4,"thoughtsTokenCount":6}}`))
	want := provider.UsageEnvelope{
		InputTokens:          intPtr(7),
		OutputTokens:         intPtr(11),
		TotalTokens:          intPtr(99),
		CacheReadInputTokens: intPtr(4),
		ReasoningTokens:      intPtr(6),
		NormalizationRule:    OperationGenerateContent,
	}
	if !reflect.DeepEqual(usage, want) {
		t.Fatalf("expected usage %+v, got %+v", want, usage)
	}
}

func TestExtractCountTokensUsageIgnoresGenerationUsageMetadata(t *testing.T) {
	usage := ExtractCountTokensUsage([]byte(`{"totalTokens":41,"cachedContentTokenCount":3,"usageMetadata":{"promptTokenCount":999,"candidatesTokenCount":999,"totalTokenCount":1998}}`))
	want := provider.UsageEnvelope{InputTokens: intPtr(41), TotalTokens: intPtr(41), CacheReadInputTokens: intPtr(3), NormalizationRule: OperationCountTokens}
	if !reflect.DeepEqual(usage, want) {
		t.Fatalf("expected count-token usage %+v, got %+v", want, usage)
	}
}

func TestParseStreamGenerateContentUsesTerminalUsageMetadata(t *testing.T) {
	stream := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\n" +
		"data: {\"usageMetadata\":{\"promptTokenCount\":13,\"candidatesTokenCount\":21,\"totalTokenCount\":34,\"cachedContentTokenCount\":5,\"thoughtsTokenCount\":8}}\n\n"
	usage, completed, terminal := ParseStreamGenerateContent([]byte(stream))
	want := provider.UsageEnvelope{
		InputTokens:          intPtr(8),
		OutputTokens:         intPtr(13),
		TotalTokens:          intPtr(34),
		CacheReadInputTokens: intPtr(5),
		ReasoningTokens:      intPtr(8),
		NormalizationRule:    OperationStreamGenerateContent,
	}
	if !completed || terminal != StreamTerminalCompleted || !reflect.DeepEqual(usage, want) {
		t.Fatalf("unexpected stream result completed=%v terminal=%q usage=%+v", completed, terminal, usage)
	}
}

func TestAdapterStreamPreservesBodyAndReturnsUsage(t *testing.T) {
	adapter := New(nil)
	stream := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":4,\"totalTokenCount\":7}}\n\n"
	var forwarded bytes.Buffer
	result, err := adapter.AdaptStream(context.Background(), provider.StreamRequest{Reader: bytes.NewReader([]byte(stream)), Writer: &forwarded})
	if err != nil {
		t.Fatalf("adapt stream: %v", err)
	}
	if forwarded.String() != stream || !result.Completed || result.TerminalSignal != StreamTerminalCompleted {
		t.Fatalf("unexpected stream forwarding/result forwarded=%q result=%+v", forwarded.String(), result)
	}
	wantUsage := provider.UsageEnvelope{InputTokens: intPtr(3), OutputTokens: intPtr(4), TotalTokens: intPtr(7), NormalizationRule: OperationStreamGenerateContent}
	if !reflect.DeepEqual(result.Usage, wantUsage) {
		t.Fatalf("expected stream usage %+v, got %+v", wantUsage, result.Usage)
	}
}

func TestClassifyProviderErrorKeepsProviderFields(t *testing.T) {
	adapterErr := ClassifyProviderError(http.StatusBadRequest, []byte(`{"error":{"status":"INVALID_ARGUMENT","code":"400","message":"bad prompt"}}`))
	if adapterErr == nil {
		t.Fatal("expected provider error classification")
	}
	if adapterErr.HTTPStatus != http.StatusBadRequest || adapterErr.Code != "gemini_provider_error" || adapterErr.Detail != "bad prompt" {
		t.Fatalf("unexpected adapter error: %+v", adapterErr)
	}
	if adapterErr.Fields["provider_error_status"] != "INVALID_ARGUMENT" || adapterErr.Fields["provider_error_code"] != "400" {
		t.Fatalf("expected provider fields to be preserved, got %+v", adapterErr.Fields)
	}
}

func adapterTestBodyModel(t *testing.T, body []byte) string {
	t.Helper()
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	model, _ := payload["model"].(string)
	return model
}

func intPtr(value int) *int {
	values := []int{value}
	return &values[0]
}
