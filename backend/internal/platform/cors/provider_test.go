package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplyAllowOriginHeadersExposesIngressRequestID(t *testing.T) {
	snapshot := NewSnapshot([]string{"http://dashboard.local:5173"})

	request := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	request.Header.Set("Origin", "http://dashboard.local:5173")
	recorder := httptest.NewRecorder()

	if !ApplyAllowOriginHeaders(recorder, request, snapshot) {
		t.Fatal("expected allowed origin to be accepted")
	}
	if got := recorder.Header().Get("Access-Control-Expose-Headers"); got != IngressRequestIDHeader {
		t.Fatalf("expected Access-Control-Expose-Headers to expose %q, got %q", IngressRequestIDHeader, got)
	}
}

func TestApplyAllowOriginHeadersSkipsDisallowedOrigins(t *testing.T) {
	snapshot := NewSnapshot([]string{"http://allowed.local"})

	request := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	request.Header.Set("Origin", "http://evil.local")
	recorder := httptest.NewRecorder()

	if ApplyAllowOriginHeaders(recorder, request, snapshot) {
		t.Fatal("expected disallowed origin to be rejected")
	}
	if recorder.Header().Get("Access-Control-Expose-Headers") != "" {
		t.Fatal("expected no expose headers for disallowed origin")
	}
}
