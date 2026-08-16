package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestClassifySSEStreamOutcomeSeparatesGatewayDeadlineFromClientCancel(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	classification := classifySSEStreamOutcome(canceled, sseTerminalSignalNone, nil, nil)
	if classification.outcome != runtimeStreamOutcomeClientDisconnected {
		t.Fatalf("cancelled client context: got outcome %q, want %q", classification.outcome, runtimeStreamOutcomeClientDisconnected)
	}
	if classification.kind == nil || *classification.kind != runtimeStreamErrorKindRequestContextCanceled {
		t.Fatalf("cancelled client context: got kind %v, want %q", classification.kind, runtimeStreamErrorKindRequestContextCanceled)
	}

	expired, cancelExpired := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancelExpired()
	<-expired.Done()
	classification = classifySSEStreamOutcome(expired, sseTerminalSignalNone, nil, nil)
	if classification.outcome != runtimeStreamOutcomeGatewayTimeout {
		t.Fatalf("expired gateway budget: got outcome %q, want %q", classification.outcome, runtimeStreamOutcomeGatewayTimeout)
	}
	if classification.kind == nil || *classification.kind != runtimeStreamErrorKindGatewayStreamDeadline {
		t.Fatalf("expired gateway budget: got kind %v, want %q", classification.kind, runtimeStreamErrorKindGatewayStreamDeadline)
	}
}

func TestRuntimeStreamAbortReasonOnlyCoversTruncatedStreams(t *testing.T) {
	aborting := []string{runtimeStreamOutcomeUpstreamReadError, runtimeStreamOutcomeGatewayTimeout}
	for _, outcome := range aborting {
		if _, ok := runtimeStreamAbortReasonFor(outcome); !ok {
			t.Fatalf("outcome %q must produce an abort reason", outcome)
		}
	}
	// upstream_ended_without_terminal is the important negative: Prism relayed
	// every byte it received, so fabricating an error would break providers that
	// legitimately never send a [DONE] sentinel.
	passthrough := []string{
		runtimeStreamOutcomeUpstreamEndedWithoutTerminal,
		runtimeStreamOutcomeCompleted,
		runtimeStreamOutcomeProviderIncomplete,
		runtimeStreamOutcomeClientDisconnected,
		runtimeStreamOutcomeNotStreaming,
		runtimeStreamOutcomeUnknown,
	}
	for _, outcome := range passthrough {
		if _, ok := runtimeStreamAbortReasonFor(outcome); ok {
			t.Fatalf("outcome %q must not produce an abort reason", outcome)
		}
	}
}

func TestWriteRuntimeStreamAbortFrameUsesIngressNativeShape(t *testing.T) {
	reason, ok := runtimeStreamAbortReasonFor(runtimeStreamOutcomeUpstreamReadError)
	if !ok {
		t.Fatal("expected an abort reason for upstream_read_error")
	}
	testCases := []struct {
		name         string
		collectionID string
		namedEvent   bool
		assert       func(t *testing.T, payload map[string]any)
	}{
		{
			name:         "openai chat completions",
			collectionID: "openai.chat_completions",
			assert: func(t *testing.T, payload map[string]any) {
				errObject, ok := payload["error"].(map[string]any)
				if !ok {
					t.Fatalf("openai frame must carry an error object, got %v", payload)
				}
				if errObject["type"] != "upstream_error" {
					t.Fatalf("openai frame error.type = %v, want upstream_error", errObject["type"])
				}
			},
		},
		{
			name:         "openai responses",
			collectionID: "openai.responses",
			namedEvent:   true,
			assert: func(t *testing.T, payload map[string]any) {
				if payload["error"] == nil {
					t.Fatalf("responses frame must carry an error object, got %v", payload)
				}
			},
		},
		{
			name:         "anthropic messages",
			collectionID: "anthropic.messages",
			namedEvent:   true,
			assert: func(t *testing.T, payload map[string]any) {
				if payload["type"] != "error" {
					t.Fatalf("anthropic frame type = %v, want error", payload["type"])
				}
			},
		},
		{
			name:         "gemini stream generate content",
			collectionID: "gemini.stream_generate_content",
			assert: func(t *testing.T, payload map[string]any) {
				errObject, ok := payload["error"].(map[string]any)
				if !ok {
					t.Fatalf("gemini frame must carry an error object, got %v", payload)
				}
				if errObject["status"] != "UNAVAILABLE" {
					t.Fatalf("gemini frame error.status = %v, want UNAVAILABLE", errObject["status"])
				}
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var buffer bytes.Buffer
			operation := RuntimeOperation{Name: testCase.collectionID, HookCollectionID: testCase.collectionID, Streaming: true}
			writeRuntimeStreamAbortFrame(&buffer, operation, reason)
			frame := buffer.String()
			if frame == "" {
				t.Fatal("expected an abort frame to be written")
			}
			// A truncated stream must never be closed with the sentinel that
			// means "the model finished", nor with a synthesised stop reason.
			if strings.Contains(frame, "[DONE]") {
				t.Fatalf("abort frame must not emit a [DONE] sentinel: %q", frame)
			}
			if strings.Contains(frame, "finish_reason") {
				t.Fatalf("abort frame must not synthesise a finish_reason: %q", frame)
			}
			if hasNamedEvent := strings.HasPrefix(frame, "event: error\n"); hasNamedEvent != testCase.namedEvent {
				t.Fatalf("named event prefix = %v, want %v (frame %q)", hasNamedEvent, testCase.namedEvent, frame)
			}
			if !strings.HasSuffix(frame, "\n\n") {
				t.Fatalf("abort frame must terminate the SSE event: %q", frame)
			}
			_, data, found := strings.Cut(frame, "data: ")
			if !found {
				t.Fatalf("abort frame must carry a data line: %q", frame)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &payload); err != nil {
				t.Fatalf("abort frame data must be JSON: %v (frame %q)", err, frame)
			}
			testCase.assert(t, payload)
		})
	}
}

func TestWriteRuntimeStreamAbortFrameSkipsUnknownOperations(t *testing.T) {
	reason, _ := runtimeStreamAbortReasonFor(runtimeStreamOutcomeGatewayTimeout)
	var buffer bytes.Buffer
	writeRuntimeStreamAbortFrame(&buffer, RuntimeOperation{Name: "not.a.stream_collection"}, reason)
	if buffer.Len() != 0 {
		t.Fatalf("unknown operation must not receive a frame, got %q", buffer.String())
	}
}
