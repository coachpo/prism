package runtime

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestReadStreamStartDetectsProviderErrorAndReplaysUnchanged(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		want   bool
	}{
		{"sse error event", "event: error\ndata: {\"error\":{\"code\":\"client_error\"}}\n\nevent: done\ndata: [DONE]\n\n", true},
		{"top-level error after comment", ": keepalive\n\ndata: {\"error\":{\"code\":400,\"status\":\"INVALID_ARGUMENT\"}}\n\n", true},
		{"typed error with crlf", "data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\"}}\r\n\r\n", true},
		{"error event ended by eof", "event: error\ndata: {}", true},
		{"content chunk with null error", "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}],\"error\":null}\n\ndata: [DONE]\n\n", false},
		{"responses created", "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"error\":null}}\n\n", false},
		{"done sentinel", "data: [DONE]\n\n", false},
		{"first event beyond limit", "data: " + strings.Repeat("x", streamStartInspectionLimit) + "\n\nevent: error\ndata: {}\n\n", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := io.NopCloser(strings.NewReader(test.stream))
			start := readStreamStart(upstream)
			if got := start.isError(); got != test.want {
				t.Fatalf("isError() = %v, want %v", got, test.want)
			}
			replayed, err := io.ReadAll(start.replay(upstream))
			if err != nil || string(replayed) != test.stream {
				t.Fatalf("replay changed the stream: err=%v, got %q", err, replayed)
			}
		})
	}
}

func TestStreamStartReplayResurfacesReadError(t *testing.T) {
	readErr := errors.New("upstream reset")
	upstream := io.NopCloser(io.MultiReader(strings.NewReader("data: {\"choices\":[]}\n"), iotest.ErrReader(readErr)))
	start := readStreamStart(upstream)
	if start.isError() {
		t.Fatal("a content payload interrupted by a read error is not a provider error event")
	}
	replayed, err := io.ReadAll(start.replay(upstream))
	if !errors.Is(err, readErr) || string(replayed) != "data: {\"choices\":[]}\n" {
		t.Fatalf("replay must yield the head and then the read error: err=%v, got %q", err, replayed)
	}
}

func TestStreamStartDiagnosticsPreferProviderCode(t *testing.T) {
	recognized := readStreamStart(strings.NewReader("event: error\ndata: {\"error\":{\"message\":\"unknown variant `max`\",\"type\":\"client_error\",\"code\":\"client_error\"}}\n\n")).diagnostics(nil)
	if recognized.Source != errorSourceUpstream || recognized.Stage != failureStageStream || recognized.Code != "client_error" || !strings.Contains(recognized.Detail, "unknown variant") {
		t.Fatalf("unexpected recognized diagnostics: %+v", recognized)
	}
	unrecognized := readStreamStart(strings.NewReader("event: error\ndata: overloaded\n\n")).diagnostics(nil)
	if unrecognized.Code != "stream_provider_incomplete" || unrecognized.Detail != streamStartErrorFallbackDetail {
		t.Fatalf("unexpected fallback diagnostics: %+v", unrecognized)
	}
}
