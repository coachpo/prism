package runtime

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type ingressTrackingBody struct {
	reader     *strings.Reader
	readCount  int
	closeCount int
}

func newIngressTrackingBody(payload string) *ingressTrackingBody {
	return &ingressTrackingBody{reader: strings.NewReader(payload)}
}

func (body *ingressTrackingBody) Read(payload []byte) (int, error) {
	body.readCount++
	return body.reader.Read(payload)
}

func (body *ingressTrackingBody) Close() error {
	body.closeCount++
	return nil
}

type ingressRoundTripRecorder struct {
	calls atomic.Int32
}

func (recorder *ingressRoundTripRecorder) RoundTrip(_ *http.Request) (*http.Response, error) {
	recorder.calls.Add(1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
	}, nil
}

func newIngressTestService(transport *ingressRoundTripRecorder, sideEffectSubmits *atomic.Int32) *Service {
	return &Service{
		staticRuntimeProxyConfig: RuntimeProxyConfigSnapshot{HTTPClient: &http.Client{Transport: transport}},
		now: func() time.Time {
			return time.Unix(1_700_000_000, 0).UTC()
		},
		runtimeSideEffects: NewRuntimeSideEffectManager(nil, RuntimeSideEffectOptions{Hooks: &RuntimeSideEffectHooks{
			AfterSubmit: func(RuntimeSideEffectSubmitResult) {
				sideEffectSubmits.Add(1)
			},
		}}),
	}
}

func TestHandleStreamingProxyRejectsUnsupportedRoutes(t *testing.T) {
	t.Run("supported route continues into planning path", func(t *testing.T) {
		transport := &ingressRoundTripRecorder{}
		sideEffectSubmits := &atomic.Int32{}
		service := newIngressTestService(transport, sideEffectSubmits)
		body := newIngressTrackingBody(`{"model":"gpt-4o","messages":[]}`)
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		request.Body = body

		response := performIngressRequest(service, request)

		assertRuntimeJSONError(t, response, http.StatusServiceUnavailable, "Runtime snapshot is unavailable. Retry later.")
		if body.readCount == 0 {
			t.Fatal("expected supported body-bound route to read the body before planning")
		}
		assertIngressNoProviderOrSideEffects(t, transport, sideEffectSubmits)
	})

	tests := []struct {
		name string
		path string
	}{
		{name: "unknown OpenAI path", path: "/v1/models"},
		{name: "unsupported Gemini action", path: "/v1beta/models/gemini-2.5-pro:embedContent"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := &ingressRoundTripRecorder{}
			sideEffectSubmits := &atomic.Int32{}
			service := newIngressTestService(transport, sideEffectSubmits)
			body := newIngressTrackingBody(`{"model":"gpt-4o"}`)
			request := httptest.NewRequest(http.MethodPost, test.path, nil)
			request.Body = body

			response := performIngressRequest(service, request)

			assertRuntimeJSONError(t, response, http.StatusNotFound, runtimeOperationNotFoundDetail)
			assertIngressRejectedBeforeBodyRead(t, body)
			assertIngressNoProviderOrSideEffects(t, transport, sideEffectSubmits)
		})
	}
}

func TestHandleStreamingProxyRejectsWrongMethod(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "OpenAI chat completions GET", path: "/v1/chat/completions"},
		{name: "Gemini generateContent GET", path: "/v1beta/models/gemini-2.5-pro:generateContent"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := &ingressRoundTripRecorder{}
			sideEffectSubmits := &atomic.Int32{}
			service := newIngressTestService(transport, sideEffectSubmits)
			body := newIngressTrackingBody(`{"model":"gpt-4o"}`)
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Body = body

			response := performIngressRequest(service, request)

			assertRuntimeJSONError(t, response, http.StatusMethodNotAllowed, runtimeOperationMethodNotAllowedDetail)
			if allow := response.Header.Get("Allow"); allow != http.MethodPost {
				t.Fatalf("expected Allow header %q, got %q", http.MethodPost, allow)
			}
			assertIngressRejectedBeforeBodyRead(t, body)
			assertIngressNoProviderOrSideEffects(t, transport, sideEffectSubmits)
		})
	}
}

func performIngressRequest(service *Service, request *http.Request) *http.Response {
	responseRecorder := httptest.NewRecorder()
	service.handleStreamingProxy(responseRecorder, request)
	return responseRecorder.Result()
}

func assertRuntimeJSONError(t *testing.T, response *http.Response, wantStatus int, wantDetail string) {
	t.Helper()
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != wantStatus {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("expected status %d, got %d with body %s", wantStatus, response.StatusCode, string(body))
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}
	var payload struct {
		Detail string `json:"detail"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode runtime JSON error: %v", err)
	}
	if payload.Detail != wantDetail {
		t.Fatalf("expected detail %q, got %q", wantDetail, payload.Detail)
	}
}

func assertIngressRejectedBeforeBodyRead(t *testing.T, body *ingressTrackingBody) {
	t.Helper()
	if body.readCount != 0 {
		t.Fatalf("expected rejected ingress not to read request body, got %d reads", body.readCount)
	}
}

func assertIngressNoProviderOrSideEffects(t *testing.T, transport *ingressRoundTripRecorder, sideEffectSubmits *atomic.Int32) {
	t.Helper()
	if got := transport.calls.Load(); got != 0 {
		t.Fatalf("expected ingress not to call provider transport, got %d calls", got)
	}
	if got := sideEffectSubmits.Load(); got != 0 {
		t.Fatalf("expected ingress not to submit runtime side effects, got %d submissions", got)
	}
}
