package anthropic

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

func TestAdapterBuildsMessagesAndTokenCountRequests(t *testing.T) {
	adapter := New(nil)
	tests := []struct {
		name          string
		operationName string
		wantPath      string
	}{
		{name: "messages", operationName: OperationMessages, wantPath: "/v1/messages"},
		{name: "count tokens", operationName: OperationCountTokens, wantPath: "/v1/messages/count_tokens"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := adapter.BuildMessagesUpstreamRequest(context.Background(), MessagesUpstreamRequest{
				Operation:     provider.Operation{Name: test.operationName},
				RawBody:       []byte(`{"model":"public-claude","messages":[],"stream":true}`),
				TargetModelID: "target-claude",
			})
			if err != nil {
				t.Fatalf("build upstream request: %v", err)
			}
			if request.Method != http.MethodPost || request.Path != test.wantPath {
				t.Fatalf("expected POST %s, got %+v", test.wantPath, request)
			}
			if got := adapterTestBodyModel(t, request.Body); got != "target-claude" {
				t.Fatalf("expected rewritten model, got %q", got)
			}
		})
	}
}

func TestAdapterRejectsUnsupportedOperationWithTypedError(t *testing.T) {
	adapter := New(nil)
	_, err := adapter.BuildMessagesUpstreamRequest(context.Background(), MessagesUpstreamRequest{Operation: provider.Operation{Name: "anthropic.unknown"}})
	var adapterErr *provider.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("expected adapter error, got %v", err)
	}
	if adapterErr.HTTPStatus != http.StatusBadRequest || adapterErr.Code != "anthropic_operation_unsupported" {
		t.Fatalf("unexpected adapter error: %+v", adapterErr)
	}
}

func TestExtractMessagesUsagePreservesAnthropicSplits(t *testing.T) {
	usage := ExtractMessagesUsage([]byte(`{"usage":{"input_tokens":7,"cache_read_input_tokens":2,"cache_creation_input_tokens":3,"output_tokens":13}}`))
	want := provider.UsageEnvelope{
		InputTokens:              intPtr(7),
		OutputTokens:             intPtr(13),
		TotalTokens:              intPtr(25),
		CacheReadInputTokens:     intPtr(2),
		CacheCreationInputTokens: intPtr(3),
		NormalizationRule:        OperationMessages,
	}
	if !reflect.DeepEqual(usage, want) {
		t.Fatalf("expected usage %+v, got %+v", want, usage)
	}
}

func TestExtractTokenCountUsageIgnoresGenerationUsage(t *testing.T) {
	usage := ExtractTokenCountUsage([]byte(`{"input_tokens":11,"usage":{"input_tokens":999,"output_tokens":999,"total_tokens":1998}}`))
	want := provider.UsageEnvelope{InputTokens: intPtr(11), NormalizationRule: OperationCountTokens}
	if !reflect.DeepEqual(usage, want) {
		t.Fatalf("expected count-token usage %+v, got %+v", want, usage)
	}
}

func TestParseMessagesStreamUsesCumulativeDeltaUsage(t *testing.T) {
	stream := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7,\"cache_read_input_tokens\":2,\"cache_creation_input_tokens\":3,\"output_tokens\":1}}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":13}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	usage, completed, terminal := ParseMessagesStream([]byte(stream))
	want := provider.UsageEnvelope{
		InputTokens:              intPtr(7),
		OutputTokens:             intPtr(13),
		TotalTokens:              intPtr(25),
		CacheReadInputTokens:     intPtr(2),
		CacheCreationInputTokens: intPtr(3),
		NormalizationRule:        OperationMessages,
	}
	if !completed || terminal != StreamTerminalCompleted || !reflect.DeepEqual(usage, want) {
		t.Fatalf("unexpected stream result completed=%v terminal=%q usage=%+v", completed, terminal, usage)
	}
}

func TestAdapterStreamPreservesBodyAndReturnsUsage(t *testing.T) {
	adapter := New(nil)
	stream := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":4}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	var forwarded bytes.Buffer
	result, err := adapter.AdaptStream(context.Background(), provider.StreamRequest{Reader: bytes.NewReader([]byte(stream)), Writer: &forwarded})
	if err != nil {
		t.Fatalf("adapt stream: %v", err)
	}
	if forwarded.String() != stream || !result.Completed || result.TerminalSignal != StreamTerminalCompleted {
		t.Fatalf("unexpected stream forwarding/result forwarded=%q result=%+v", forwarded.String(), result)
	}
	wantUsage := provider.UsageEnvelope{InputTokens: intPtr(3), OutputTokens: intPtr(4), TotalTokens: intPtr(7), NormalizationRule: OperationMessages}
	if !reflect.DeepEqual(result.Usage, wantUsage) {
		t.Fatalf("expected stream usage %+v, got %+v", wantUsage, result.Usage)
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
	return &value
}

func TestClassifyProviderErrorKeepsProviderFields(t *testing.T) {
	adapterErr := ClassifyProviderError(http.StatusTooManyRequests, []byte(`{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"slow down"}}`))
	if adapterErr == nil {
		t.Fatal("expected provider error classification")
	}
	if adapterErr.HTTPStatus != http.StatusTooManyRequests || adapterErr.Code != "anthropic_provider_error" || adapterErr.Detail != "slow down" {
		t.Fatalf("unexpected adapter error: %+v", adapterErr)
	}
	if adapterErr.Fields["provider_error_type"] != "rate_limit_error" || adapterErr.Fields["provider_error_code"] != "rate_limit_exceeded" {
		t.Fatalf("expected provider fields to be preserved, got %+v", adapterErr.Fields)
	}
}
