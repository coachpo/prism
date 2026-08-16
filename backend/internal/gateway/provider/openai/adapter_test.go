package openai

import (
	"encoding/json"
	"testing"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
)

func TestBuildTextUpstreamRequestStreamUsageInjection(t *testing.T) {
	adapter := New()

	chatCompletions := provider.Operation{Name: OperationChatCompletions}
	responses := provider.Operation{Name: OperationResponses}
	responsesInputTokens := provider.Operation{Name: OperationResponsesInputTokens}

	cases := []struct {
		name               string
		operation          provider.Operation
		rawBody            string
		targetModelID      string
		wantStreamOptions  json.RawMessage // nil means the key must be absent
		wantRawPassthrough bool
	}{
		{
			name:              "stream without stream_options receives include_usage",
			operation:         chatCompletions,
			rawBody:           `{"model":"m","stream":true}`,
			wantStreamOptions: json.RawMessage(`{"include_usage":true}`),
		},
		{
			name:              "explicit null stream_options is treated as unset",
			operation:         chatCompletions,
			rawBody:           `{"model":"m","stream":true,"stream_options":null}`,
			wantStreamOptions: json.RawMessage(`{"include_usage":true}`),
		},
		{
			name:              "client include_usage false is preserved",
			operation:         chatCompletions,
			rawBody:           `{"model":"m","stream":true,"stream_options":{"include_usage":false}}`,
			wantStreamOptions: json.RawMessage(`{"include_usage":false}`),
		},
		{
			name:              "client include_usage true is not re-injected",
			operation:         chatCompletions,
			rawBody:           `{"model":"m","stream":true,"stream_options":{"include_usage":true}}`,
			wantStreamOptions: json.RawMessage(`{"include_usage":true}`),
		},
		{
			name:              "other stream_options keys are kept alongside include_usage",
			operation:         chatCompletions,
			rawBody:           `{"model":"m","stream":true,"stream_options":{"include_obfuscation":false}}`,
			wantStreamOptions: json.RawMessage(`{"include_obfuscation":false,"include_usage":true}`),
		},
		{
			name:      "non-stream chat completions stays untouched",
			operation: chatCompletions,
			rawBody:   `{"model":"m"}`,
		},
		{
			name:      "explicit stream false stays untouched",
			operation: chatCompletions,
			rawBody:   `{"model":"m","stream":false}`,
		},
		{
			name:      "responses stream stays untouched",
			operation: responses,
			rawBody:   `{"model":"m","stream":true}`,
		},
		{
			name:      "responses input_tokens stream stays untouched",
			operation: responsesInputTokens,
			rawBody:   `{"model":"m","stream":true}`,
		},
		{
			name:               "non-JSON body passes through byte for byte",
			operation:          chatCompletions,
			rawBody:            `not json`,
			wantRawPassthrough: true,
		},
		{
			name:              "model rewrite still applies alongside injection",
			operation:         chatCompletions,
			rawBody:           `{"model":"client","stream":true}`,
			targetModelID:     "target",
			wantStreamOptions: json.RawMessage(`{"include_usage":true}`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream, err := adapter.BuildTextUpstreamRequest(t.Context(), TextUpstreamRequest{
				Operation:     tc.operation,
				RawBody:       []byte(tc.rawBody),
				TargetModelID: tc.targetModelID,
			})
			if err != nil {
				t.Fatalf("BuildTextUpstreamRequest: %v", err)
			}
			if tc.wantRawPassthrough {
				if string(upstream.Body) != tc.rawBody {
					t.Fatalf("expected body to pass through unchanged, got %q", upstream.Body)
				}
				return
			}
			var payload map[string]any
			if err := json.Unmarshal(upstream.Body, &payload); err != nil {
				t.Fatalf("outbound body is not valid JSON: %v", err)
			}
			got, present := payload["stream_options"]
			if tc.wantStreamOptions == nil {
				if present {
					t.Fatalf("expected no stream_options key, got %v", got)
				}
				return
			}
			if !present {
				t.Fatalf("expected stream_options %s, key missing", tc.wantStreamOptions)
			}
			var want map[string]any
			if err := json.Unmarshal(tc.wantStreamOptions, &want); err != nil {
				t.Fatalf("decode expected stream_options: %v", err)
			}
			gotObject, ok := got.(map[string]any)
			if !ok {
				t.Fatalf("expected stream_options object, got %T %v", got, got)
			}
			if len(gotObject) != len(want) {
				t.Fatalf("expected stream_options keys %v, got %v", want, gotObject)
			}
			for key, wantValue := range want {
				if gotObject[key] != wantValue {
					t.Fatalf("expected stream_options.%s=%v, got %v", key, wantValue, gotObject[key])
				}
			}
			if tc.name == "model rewrite still applies alongside injection" {
				if payload["model"] != "target" {
					t.Fatalf("expected model to be rewritten to target, got %v", payload["model"])
				}
			}
		})
	}
}
