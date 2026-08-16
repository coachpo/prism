package platformhttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/platform/bodylimits"
)

type bodyLimitTestPayload struct {
	Value string `json:"value"`
}

type bodyLimitErrorPayload struct {
	Error      string `json:"error"`
	Message    string `json:"message"`
	LimitBytes int64  `json:"limit_bytes"`
}

func TestLimitRequestBodyDetectsOversizedValidJSON(t *testing.T) {
	limitBytes := int64(32)
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"`+strings.Repeat("a", 64)+`"}`))
	recorder := httptest.NewRecorder()

	bodylimits.LimitRequestBody(recorder, request, limitBytes)

	var payload bodyLimitTestPayload
	err := json.NewDecoder(request.Body).Decode(&payload)
	if err == nil {
		t.Fatalf("expected oversized valid JSON to fail")
	}
	maxBytesErr, ok := bodylimits.MaxBytesError(err)
	if !ok {
		t.Fatalf("expected MaxBytesError, got %T: %v", err, err)
	}
	if maxBytesErr.Limit != limitBytes {
		t.Fatalf("expected limit %d, got %d", limitBytes, maxBytesErr.Limit)
	}
}

func TestLimitRequestBodyDetectsOversizedInvalidJSON(t *testing.T) {
	limitBytes := int64(24)
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"`+strings.Repeat("a", 64)))
	recorder := httptest.NewRecorder()

	bodylimits.LimitRequestBody(recorder, request, limitBytes)

	var payload bodyLimitTestPayload
	err := json.NewDecoder(request.Body).Decode(&payload)
	if err == nil {
		t.Fatalf("expected oversized invalid JSON to fail")
	}
	maxBytesErr, ok := bodylimits.MaxBytesError(err)
	if !ok {
		t.Fatalf("expected MaxBytesError, got %T: %v", err, err)
	}
	if maxBytesErr.Limit != limitBytes {
		t.Fatalf("expected limit %d, got %d", limitBytes, maxBytesErr.Limit)
	}
}

func TestWriteMaxBytesErrorRespondsWithStableJSON(t *testing.T) {
	limitBytes := int64(4096)
	err := fmt.Errorf("wrapped: %w", &http.MaxBytesError{Limit: limitBytes})
	recorder := httptest.NewRecorder()

	if !bodylimits.WriteMaxBytesError(recorder, err, 0) {
		t.Fatalf("expected wrapped MaxBytesError to be handled")
	}

	response := recorder.Result()
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}

	var payload bodyLimitErrorPayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error != bodylimits.RequestBodyTooLargeCode {
		t.Fatalf("expected error code %q, got %q", bodylimits.RequestBodyTooLargeCode, payload.Error)
	}
	if payload.Message == "" {
		t.Fatalf("expected human-readable message")
	}
	if payload.LimitBytes != limitBytes {
		t.Fatalf("expected limit_bytes %d, got %d", limitBytes, payload.LimitBytes)
	}
}
