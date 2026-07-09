package runtimetest

import (
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/bodylimits"
)

type oversizedRuntimeBodyReader struct {
	remaining int64
}

func (reader *oversizedRuntimeBodyReader) Read(payload []byte) (int, error) {
	if reader.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(payload)) > reader.remaining {
		payload = payload[:reader.remaining]
	}
	for index := range payload {
		payload[index] = 'x'
	}
	reader.remaining -= int64(len(payload))
	return len(payload), nil
}

func TestRuntimeOversizedBodiesRejectBeforeProviderAndTelemetry(t *testing.T) {
	var sideEffectSubmits atomic.Int32
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{
			SideEffects: runtimeapi.RuntimeSideEffectOptions{
				Hooks: &runtimeapi.RuntimeSideEffectHooks{
					AfterSubmit: func(runtimeapi.RuntimeSideEffectSubmitResult) {
						sideEffectSubmits.Add(1)
					},
				},
			},
		},
	})
	profileID := harness.activeProfileID(t)
	baseline := loadRuntimeRejectedRoutePersistenceCounts(t, harness.conn, profileID)
	tests := []struct {
		name        string
		path        string
		contentType string
		limitBytes  int64
	}{
		{name: "OpenAIChatCompletions", path: "/v1/chat/completions", contentType: "application/json", limitBytes: bodylimits.RuntimeJSONRequestBodyLimitBytes},
		{name: "OpenAIResponses", path: "/v1/responses", contentType: "application/json", limitBytes: bodylimits.RuntimeJSONRequestBodyLimitBytes},
		{name: "GeminiGenerateContent", path: "/v1beta/models/body-limit-gemini:generateContent", contentType: "application/json", limitBytes: bodylimits.RuntimeJSONRequestBodyLimitBytes},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performOversizedRuntimeRequest(t, harness, test.path, test.contentType, test.limitBytes)
			assertOversizedRuntimeResponse(t, response, test.limitBytes)
			if got := len(harness.upstream.requestsSnapshot()); got != 0 {
				t.Fatalf("expected oversized body not to reach provider transport, got %d upstream requests", got)
			}
			if got := sideEffectSubmits.Load(); got != 0 {
				t.Fatalf("expected oversized body not to submit runtime activity, got %d submissions", got)
			}
		})
	}
	assertRuntimeRejectedRoutePersistenceCountsRemain(t, harness.conn, profileID, baseline, 500*time.Millisecond)
}

func performOversizedRuntimeRequest(t *testing.T, harness *runtimeHarness, path string, contentType string, limitBytes int64) *http.Response {
	t.Helper()
	body := &oversizedRuntimeBodyReader{remaining: limitBytes + 1}
	request, err := http.NewRequest(http.MethodPost, harness.url+path, body)
	if err != nil {
		t.Fatalf("build oversized runtime request: %v", err)
	}
	request.Header.Set("Content-Type", contentType)
	request.ContentLength = limitBytes + 1
	response, err := harness.client.Do(request)
	if err != nil {
		t.Fatalf("perform oversized runtime request: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func assertOversizedRuntimeResponse(t *testing.T, response *http.Response, wantLimitBytes int64) {
	t.Helper()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("expected oversized status %d, got %d with body %s", http.StatusRequestEntityTooLarge, response.StatusCode, string(body))
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}
	var payload struct {
		Error      string `json:"error"`
		LimitBytes int64  `json:"limit_bytes"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode oversized runtime response: %v", err)
	}
	if payload.Error != bodylimits.RequestBodyTooLargeCode {
		t.Fatalf("expected error code %q, got %+v", bodylimits.RequestBodyTooLargeCode, payload)
	}
	if payload.LimitBytes != wantLimitBytes {
		t.Fatalf("expected limit_bytes %d, got %+v", wantLimitBytes, payload)
	}
}
