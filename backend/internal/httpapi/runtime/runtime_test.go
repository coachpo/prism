package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestModelResolutionAndRewriteHelpers(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	if got, err := resolveModelID(rawBody, "/v1/chat/completions"); err != nil || got != "gpt-4o" {
		t.Fatalf("expected body model id, got model=%q err=%v", got, err)
	}
	responsesBody := []byte(`{"model":"gpt-4o","input":"hello"}`)
	if got, err := resolveModelID(responsesBody, "/v1/responses"); err != nil || got != "gpt-4o" {
		t.Fatalf("expected Responses body model id, got model=%q err=%v", got, err)
	}
	if got, err := resolveModelID(nil, "/v1beta/models/gemini-2.5-pro:generateContent"); err != nil || got != "gemini-2.5-pro" {
		t.Fatalf("expected path model id, got model=%q err=%v", got, err)
	}
	if got := extractModelFromPath("/v1beta/models/gemini-2.5-pro:generateContent"); got != "gemini-2.5-pro" {
		t.Fatalf("expected path extraction to return Gemini model id, got %q", got)
	}

	rewrittenBody := rewriteModelInBody(rawBody, "gpt-4o-mini")
	if got := extractModelFromBody(rewrittenBody); got != "gpt-4o-mini" {
		t.Fatalf("expected rewritten model id in body, got %q", got)
	}
	if got := rewriteModelInPath("/v1beta/models/gemini-1.5-pro:generateContent", "gemini-1.5-pro", "gemini-2.5-pro"); got != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("expected rewritten Gemini path, got %q", got)
	}
	if _, err := resolveModelID([]byte(`{"messages":[]}`), "/v1/chat/completions"); err == nil {
		t.Fatal("expected missing model id to fail")
	}
	if _, err := resolveModelID([]byte(`{"input":"hello"}`), "/v1/responses"); err == nil {
		t.Fatal("expected missing Responses model id to fail")
	}
}

func TestValidatePathCompatibilityAndHeaderHelpers(t *testing.T) {
	if err := validatePathCompatibility("openai", "/v1/chat/completions"); err != nil {
		t.Fatalf("expected OpenAI generic path to be valid, got %v", err)
	}
	if err := validatePathCompatibility("openai", "/v1/responses"); err != nil {
		t.Fatalf("expected OpenAI Responses path to be valid, got %v", err)
	}
	if err := validatePathCompatibility("anthropic", "/v1/messages"); err != nil {
		t.Fatalf("expected Anthropic messages path to be valid, got %v", err)
	}
	if err := validatePathCompatibility("gemini", "/v1beta/models/gemini-2.5-pro:generateContent"); err != nil {
		t.Fatalf("expected Gemini native path to be valid, got %v", err)
	}

	err := validatePathCompatibility("openai", "/v1beta/models/gemini-2.5-pro:generateContent")
	var domainErr *domainError
	if !errors.As(err, &domainErr) || domainErr.StatusCode != http.StatusBadRequest || !strings.Contains(domainErr.Detail, "incompatible") {
		t.Fatalf("expected incompatibility domain error, got %v", err)
	}
	err = validatePathCompatibility("anthropic", "/v1/responses")
	if !errors.As(err, &domainErr) || domainErr.StatusCode != http.StatusBadRequest || !strings.Contains(domainErr.Detail, "api_family 'anthropic'") {
		t.Fatalf("expected Anthropic Responses incompatibility domain error, got %v", err)
	}

	if got, ok := normalizeHeaderValue("  keep  "); !ok || got != "keep" {
		t.Fatalf("expected normalized header value, got value=%q ok=%v", got, ok)
	}
	if _, ok := normalizeHeaderValue("bad\nvalue"); ok {
		t.Fatal("expected control-character header value to be rejected")
	}

	rules := []headerBlocklistRule{{MatchType: "exact", Pattern: "x-remove"}, {MatchType: "prefix", Pattern: "x-secret-"}}
	sanitized := sanitizeHeaders(map[string]string{"X-Trace-Id": "1", "x-secret-token": "blocked", "X-Remove": "gone"}, rules)
	if !reflect.DeepEqual(sanitized, map[string]string{"X-Trace-Id": "1"}) {
		t.Fatalf("expected blocklisted headers to be removed, got %v", sanitized)
	}

	filtered := filterResponseHeaders(http.Header{"Connection": []string{"keep-alive"}, "X-Request-Id": []string{"abc"}})
	if filtered.Get("Connection") != "" || filtered.Get("X-Request-Id") != "abc" {
		t.Fatalf("expected hop-by-hop response headers to be filtered, got %v", filtered)
	}
}

func TestRequestWantsStreamUsesGeminiStreamingPath(t *testing.T) {
	if !requestWantsStream(nil, "/v1beta/models/gemini-2.5-pro:streamGenerateContent") {
		t.Fatal("expected Gemini streamGenerateContent path to force streaming")
	}
	if requestWantsStream(nil, "/v1beta/models/gemini-2.5-pro:generateContent?alt=sse") {
		t.Fatal("expected generateContent path without stream body flag to remain non-streaming")
	}
	if !requestWantsStream([]byte(`{"stream":true}`), "/v1/chat/completions") {
		t.Fatal("expected explicit stream body flag to remain streaming for generic routes")
	}
}

func TestClassifySSEStreamOutcome(t *testing.T) {
	writeErr := errors.New("client write failed")
	upstreamErr := errors.New("upstream read failed")
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name        string
		ctx         context.Context
		terminal    sseTerminalSignal
		upstreamErr error
		writeErr    error
		wantOutcome string
		wantKind    *string
	}{
		{name: "client write failure", terminal: sseTerminalSignalNone, writeErr: writeErr, wantOutcome: runtimeStreamOutcomeClientDisconnected, wantKind: stringPtr(runtimeStreamErrorKindClientWriteFailed)},
		{name: "client write failure beats canceled context", ctx: canceledContext, terminal: sseTerminalSignalCompleted, upstreamErr: upstreamErr, writeErr: writeErr, wantOutcome: runtimeStreamOutcomeClientDisconnected, wantKind: stringPtr(runtimeStreamErrorKindClientWriteFailed)},
		{name: "request context canceled", ctx: canceledContext, terminal: sseTerminalSignalNone, wantOutcome: runtimeStreamOutcomeClientDisconnected, wantKind: stringPtr(runtimeStreamErrorKindRequestContextCanceled)},
		{name: "canceled context beats completed", ctx: canceledContext, terminal: sseTerminalSignalCompleted, wantOutcome: runtimeStreamOutcomeClientDisconnected, wantKind: stringPtr(runtimeStreamErrorKindRequestContextCanceled)},
		{name: "canceled context beats provider incomplete", ctx: canceledContext, terminal: sseTerminalSignalProviderIncomplete, wantOutcome: runtimeStreamOutcomeClientDisconnected, wantKind: stringPtr(runtimeStreamErrorKindRequestContextCanceled)},
		{name: "upstream read error", terminal: sseTerminalSignalNone, upstreamErr: upstreamErr, wantOutcome: runtimeStreamOutcomeUpstreamReadError, wantKind: stringPtr(runtimeStreamErrorKindUpstreamReadFailed)},
		{name: "upstream read error beats completed", terminal: sseTerminalSignalCompleted, upstreamErr: upstreamErr, wantOutcome: runtimeStreamOutcomeUpstreamReadError, wantKind: stringPtr(runtimeStreamErrorKindUpstreamReadFailed)},
		{name: "upstream read error beats provider incomplete", terminal: sseTerminalSignalProviderIncomplete, upstreamErr: upstreamErr, wantOutcome: runtimeStreamOutcomeUpstreamReadError, wantKind: stringPtr(runtimeStreamErrorKindUpstreamReadFailed)},
		{name: "completed", terminal: sseTerminalSignalCompleted, wantOutcome: runtimeStreamOutcomeCompleted},
		{name: "provider incomplete", terminal: sseTerminalSignalProviderIncomplete, wantOutcome: runtimeStreamOutcomeProviderIncomplete},
		{name: "missing terminal", terminal: sseTerminalSignalNone, wantOutcome: runtimeStreamOutcomeUpstreamEndedWithoutTerminal, wantKind: stringPtr(runtimeStreamErrorKindMissingTerminalEvent)},
		{name: "eof without terminal", terminal: sseTerminalSignalNone, upstreamErr: io.EOF, wantOutcome: runtimeStreamOutcomeUpstreamEndedWithoutTerminal, wantKind: stringPtr(runtimeStreamErrorKindMissingTerminalEvent)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifySSEStreamOutcome(test.ctx, test.terminal, test.upstreamErr, test.writeErr)
			if got.outcome != test.wantOutcome {
				t.Fatalf("expected outcome %q, got %+v", test.wantOutcome, got)
			}
			if !reflect.DeepEqual(got.kind, test.wantKind) {
				t.Fatalf("expected error kind %+v, got %+v", test.wantKind, got.kind)
			}
		})
	}
}

func TestProxyEventStreamClassifiesWriteAndReadFailures(t *testing.T) {
	writeFailure := errors.New("write failed\nwith\tcontrol")
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(context.Background(), failingWriter{err: writeFailure}, strings.NewReader("event: response.created\ndata: {\"type\":\"response.created\"}\n\n"), time.Now, false)
	if !errors.Is(err, writeFailure) {
		t.Fatalf("expected write failure, got %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeClientDisconnected || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindClientWriteFailed || capture.StreamErrorDetail == nil || *capture.StreamErrorDetail != "write failed with control" {
		t.Fatalf("expected sanitized client-disconnected capture, got %+v", capture)
	}

	readFailure := errors.New("upstream read failed")
	capture, err = proxyEventStreamAndCaptureCompletedResponse(context.Background(), &forwarded, &errorAfterReader{payload: []byte("event: response.created\ndata: {\"type\":\"response.created\"}\n\n"), err: readFailure}, time.Now, false)
	if !errors.Is(err, readFailure) {
		t.Fatalf("expected upstream read failure, got %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeUpstreamReadError || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindUpstreamReadFailed || capture.StreamErrorDetail == nil || *capture.StreamErrorDetail != "upstream read failed" {
		t.Fatalf("expected upstream read error capture, got %+v", capture)
	}
}

func TestProxyEventStreamClassifiesEOFWithoutTerminal(t *testing.T) {
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(context.Background(), &forwarded, strings.NewReader("event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"usage\":null}}\n\n"), time.Now, false)
	if err != nil {
		t.Fatalf("proxy SSE without terminal: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeUpstreamEndedWithoutTerminal || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindMissingTerminalEvent || capture.StreamErrorDetail != nil || capture.CompletedAt != nil || capture.Usage.hasValues() {
		t.Fatalf("expected missing-terminal EOF capture without usage, got %+v", capture)
	}
}

func TestProxyEventStreamPreservesTerminalTimingWhenTransportOutranksTerminal(t *testing.T) {
	terminalStream := "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n"

	canceledContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	var forwarded bytes.Buffer
	cancelingWriter := cancelOnBlankLineWriter{dst: &forwarded, cancel: cancel}
	capture, err := proxyEventStreamAndCaptureCompletedResponse(canceledContext, cancelingWriter, strings.NewReader(terminalStream), time.Now, false)
	if err != nil {
		t.Fatalf("proxy SSE with canceled context after terminal: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeClientDisconnected || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindRequestContextCanceled || capture.CompletedAt == nil {
		t.Fatalf("expected context cancellation to outrank terminal while preserving completion timing, got %+v", capture)
	}

	readFailure := errors.New("upstream read failed after terminal")
	forwarded.Reset()
	capture, err = proxyEventStreamAndCaptureCompletedResponse(context.Background(), &forwarded, &errorAfterReader{payload: []byte(terminalStream), err: readFailure}, time.Now, false)
	if !errors.Is(err, readFailure) {
		t.Fatalf("expected upstream read failure, got %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeUpstreamReadError || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindUpstreamReadFailed || capture.CompletedAt == nil {
		t.Fatalf("expected upstream read error to outrank terminal while preserving completion timing, got %+v", capture)
	}
}

func TestProxyEventStreamRecognizesOpenAIDONESentinel(t *testing.T) {
	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{PricingUnit: runtimePricingUnitPerMillion, PricingCurrencyCode: "USD", InputPrice: "2", OutputPrice: "5", Version: 1}
	reportCurrencySnapshot := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}

	t.Run("completed without usage", func(t *testing.T) {
		stream := "data: {\"id\":\"chatcmpl-done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n" +
			"data:   [DONE]  \n\n"
		var forwarded bytes.Buffer
		capture, err := proxyEventStreamAndCaptureCompletedResponse(context.Background(), &forwarded, strings.NewReader(stream), time.Now, false)
		if err != nil {
			t.Fatalf("proxy SSE with [DONE]: %v", err)
		}
		if forwarded.String() != stream {
			t.Fatalf("expected SSE stream to pass through unchanged, got %q", forwarded.String())
		}
		if capture.StreamOutcome != runtimeStreamOutcomeCompleted || capture.CompletedAt == nil || capture.StreamErrorKind != nil || capture.StreamErrorDetail != nil || capture.Usage.hasValues() {
			t.Fatalf("expected completed [DONE] capture without usage, got %+v", capture)
		}

		pricing := buildRuntimePricingResult(reportCurrencySnapshot, pricingTemplateSnapshot, nil, capture.Usage, capture.StreamOutcome)
		if pricing.UnpricedReason == nil || *pricing.UnpricedReason != runtimeUnpricedReasonMissingUsage {
			t.Fatalf("expected completed [DONE] stream without usage to keep missing-token reason, got %+v", pricing)
		}
	})

	t.Run("other non JSON remains missing terminal", func(t *testing.T) {
		var forwarded bytes.Buffer
		capture, err := proxyEventStreamAndCaptureCompletedResponse(context.Background(), &forwarded, strings.NewReader("data: not-json\n\n"), time.Now, false)
		if err != nil {
			t.Fatalf("proxy SSE with non-JSON payload: %v", err)
		}
		if capture.StreamOutcome != runtimeStreamOutcomeUpstreamEndedWithoutTerminal || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindMissingTerminalEvent || capture.CompletedAt != nil {
			t.Fatalf("expected non-[DONE] non-JSON payload to remain missing terminal, got %+v", capture)
		}
	})
}

func TestProxyEventStreamMergesUsageBeforeOpenAIDONESentinel(t *testing.T) {
	stream := "data: {\"id\":\"chatcmpl-usage\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":13,\"total_tokens\":20}}\n\n" +
		"data: [DONE]\n\n"
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(context.Background(), &forwarded, strings.NewReader(stream), time.Now, false)
	if err != nil {
		t.Fatalf("proxy SSE with usage and [DONE]: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted || capture.CompletedAt == nil || capture.StreamErrorKind != nil || capture.StreamErrorDetail != nil {
		t.Fatalf("expected usage stream ending with [DONE] to complete, got %+v", capture)
	}
	inputTokens := 7
	outputTokens := 13
	totalTokens := 20
	wantUsage := responseUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens, TotalTokens: &totalTokens}
	if !reflect.DeepEqual(capture.Usage, wantUsage) {
		t.Fatalf("expected usage before [DONE] to be preserved: want %+v got %+v", wantUsage, capture.Usage)
	}

	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{PricingUnit: runtimePricingUnitPerMillion, PricingCurrencyCode: "USD", InputPrice: "2", OutputPrice: "5", Version: 1}
	pricing := buildRuntimePricingResult(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}, pricingTemplateSnapshot, nil, capture.Usage, capture.StreamOutcome)
	if !pricing.Billable || !pricing.Priced || pricing.UnpricedReason != nil {
		t.Fatalf("expected observed usage before [DONE] to price normally, got %+v", pricing)
	}
}

func TestProxyEventStreamCapturesRawAuditBodyWhenEnabled(t *testing.T) {
	stream := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"usage\":null}}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":13,\"total_tokens\":20}}}\n\n"
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(context.Background(), &forwarded, strings.NewReader(stream), time.Now, true)
	if err != nil {
		t.Fatalf("proxy SSE with audit capture: %v", err)
	}
	if forwarded.String() != stream {
		t.Fatalf("expected SSE stream to pass through unchanged, got %q", forwarded.String())
	}
	if string(capture.AuditBody) != stream || !strings.Contains(string(capture.AuditBody), "event: response.completed") {
		t.Fatalf("expected raw framed SSE audit body, got %q", string(capture.AuditBody))
	}
	if bytes.Equal(capture.AuditBody, capture.Body) {
		t.Fatalf("expected raw audit body to differ from parsed completion body, got %q", string(capture.AuditBody))
	}
	inputTokens := 7
	outputTokens := 13
	totalTokens := 20
	wantUsage := responseUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens, TotalTokens: &totalTokens}
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, wantUsage) {
		t.Fatalf("expected usage extraction to survive audit capture: want %+v got %+v", wantUsage, got)
	}
}

func TestStreamPayloadTerminalSignalRecognizesJSONTerminals(t *testing.T) {
	tests := []struct {
		name  string
		event string
		body  map[string]any
		want  sseTerminalSignal
	}{
		{name: "openai responses completed event", event: "response.completed", body: map[string]any{"type": "response.created"}, want: sseTerminalSignalCompleted},
		{name: "openai responses completed type", body: map[string]any{"type": "response.completed"}, want: sseTerminalSignalCompleted},
		{name: "openai responses incomplete type", body: map[string]any{"type": "response.incomplete"}, want: sseTerminalSignalProviderIncomplete},
		{name: "anthropic stop event", event: "message_stop", body: map[string]any{}, want: sseTerminalSignalCompleted},
		{name: "anthropic stop type", body: map[string]any{"type": "message_stop"}, want: sseTerminalSignalCompleted},
		{name: "gemini usage metadata", body: map[string]any{"usageMetadata": map[string]any{"promptTokenCount": float64(1)}}, want: sseTerminalSignalCompleted},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := payloadTerminalSignal(test.event, test.body); got != test.want {
				t.Fatalf("expected terminal signal %d, got %d", test.want, got)
			}
		})
	}
}

func TestSanitizedStreamErrorDetailNormalizesRedactsAndTruncates(t *testing.T) {
	detail := sanitizedStreamErrorDetail(errors.New("  first\nsecond\tthird  "))
	if detail == nil || *detail != "first second third" {
		t.Fatalf("expected whitespace-normalized detail, got %+v", detail)
	}

	redacted := sanitizedStreamErrorDetail(errors.New("upstream read failed; Authorization: Bearer secret-token-123; x-api-key=abc123; body={\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}; Cookie: session=abc"))
	if redacted == nil {
		t.Fatal("expected redacted stream error detail")
	}
	if strings.Contains(*redacted, "secret-token-123") || strings.Contains(*redacted, "abc123") || strings.Contains(*redacted, "hello") || strings.Contains(*redacted, "session=abc") || strings.Contains(strings.ToLower(*redacted), "authorization:") || strings.Contains(strings.ToLower(*redacted), "cookie:") {
		t.Fatalf("expected secret-bearing fragments to be redacted, got %q", *redacted)
	}
	if !strings.Contains(*redacted, "upstream read failed") {
		t.Fatalf("expected useful operator detail to survive, got %q", *redacted)
	}

	longDetail := sanitizedStreamErrorDetail(errors.New(strings.Repeat("x", runtimeStreamErrorDetailMaxLength+20)))
	if longDetail == nil || len(*longDetail) != runtimeStreamErrorDetailMaxLength {
		t.Fatalf("expected truncated stream error detail, got length %d", len(*longDetail))
	}
}

func TestBuildRuntimePricingResultUsesStreamUsageUnavailableOnlyForInterruptedStreams(t *testing.T) {
	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{PricingUnit: runtimePricingUnitPerMillion, PricingCurrencyCode: "USD", InputPrice: "2", OutputPrice: "5", Version: 1}
	reportCurrencySnapshot := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}
	inputTokens := 7
	outputTokens := 13

	priced := buildRuntimePricingResult(reportCurrencySnapshot, pricingTemplateSnapshot, nil, responseUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens}, runtimeStreamOutcomeCompleted)
	if !priced.Billable || !priced.Priced || priced.UnpricedReason != nil || priced.TotalCostUserCurrencyMicros == nil || *priced.TotalCostUserCurrencyMicros != 79 {
		t.Fatalf("expected completed stream with observed usage to price normally, got %+v", priced)
	}

	tests := []struct {
		name       string
		outcome    string
		wantReason string
	}{
		{name: "completed", outcome: runtimeStreamOutcomeCompleted, wantReason: runtimeUnpricedReasonMissingUsage},
		{name: "not streaming", outcome: runtimeStreamOutcomeNotStreaming, wantReason: runtimeUnpricedReasonMissingUsage},
		{name: "provider incomplete", outcome: runtimeStreamOutcomeProviderIncomplete, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
		{name: "client disconnected", outcome: runtimeStreamOutcomeClientDisconnected, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
		{name: "upstream read error", outcome: runtimeStreamOutcomeUpstreamReadError, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
		{name: "missing terminal", outcome: runtimeStreamOutcomeUpstreamEndedWithoutTerminal, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
		{name: "unknown", outcome: runtimeStreamOutcomeUnknown, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildRuntimePricingResult(reportCurrencySnapshot, pricingTemplateSnapshot, nil, responseUsage{}, test.outcome)
			if got.UnpricedReason == nil || *got.UnpricedReason != test.wantReason {
				t.Fatalf("expected missing usage with outcome %q to use %q, got %+v", test.outcome, test.wantReason, got)
			}
		})
	}
}

func TestBuildRuntimePricingResultPrioritizesPriceDataBeforeMissingUsage(t *testing.T) {
	validPricingTemplateSnapshot := &runtimePricingTemplateSnapshot{PricingUnit: runtimePricingUnitPerMillion, PricingCurrencyCode: "USD", InputPrice: "2", OutputPrice: "5", Version: 1}

	tests := []struct {
		name                    string
		reportCurrencySnapshot  runtimeReportCurrencySnapshot
		pricingTemplateSnapshot *runtimePricingTemplateSnapshot
		endpointFXSnapshot      *runtimeEndpointFXSnapshot
		streamOutcome           string
		wantReason              string
	}{
		{
			name:                   "pricing disabled beats interrupted missing usage",
			reportCurrencySnapshot: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
			streamOutcome:          runtimeStreamOutcomeUpstreamReadError,
			wantReason:             runtimeUnpricedReasonPricingOff,
		},
		{
			name:                   "invalid input price beats interrupted missing usage",
			reportCurrencySnapshot: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
			pricingTemplateSnapshot: &runtimePricingTemplateSnapshot{
				PricingUnit:         runtimePricingUnitPerMillion,
				PricingCurrencyCode: "USD",
				InputPrice:          "not-a-decimal",
				OutputPrice:         "5",
				Version:             1,
			},
			streamOutcome: runtimeStreamOutcomeUpstreamReadError,
			wantReason:    runtimeUnpricedReasonMissingData,
		},
		{
			name:                   "invalid output price beats completed missing usage",
			reportCurrencySnapshot: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
			pricingTemplateSnapshot: &runtimePricingTemplateSnapshot{
				PricingUnit:         runtimePricingUnitPerMillion,
				PricingCurrencyCode: "USD",
				InputPrice:          "2",
				OutputPrice:         "not-a-decimal",
				Version:             1,
			},
			streamOutcome: runtimeStreamOutcomeCompleted,
			wantReason:    runtimeUnpricedReasonMissingData,
		},
		{
			name:                    "missing fx beats interrupted missing usage",
			reportCurrencySnapshot:  runtimeReportCurrencySnapshot{Code: "EUR", Symbol: "EUR"},
			pricingTemplateSnapshot: validPricingTemplateSnapshot,
			streamOutcome:           runtimeStreamOutcomeUpstreamEndedWithoutTerminal,
			wantReason:              runtimeUnpricedReasonMissingData,
		},
		{
			name:                    "invalid fx beats completed missing usage",
			reportCurrencySnapshot:  runtimeReportCurrencySnapshot{Code: "EUR", Symbol: "EUR"},
			pricingTemplateSnapshot: validPricingTemplateSnapshot,
			endpointFXSnapshot:      &runtimeEndpointFXSnapshot{FXRate: "not-a-decimal"},
			streamOutcome:           runtimeStreamOutcomeCompleted,
			wantReason:              runtimeUnpricedReasonMissingData,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildRuntimePricingResult(test.reportCurrencySnapshot, test.pricingTemplateSnapshot, test.endpointFXSnapshot, responseUsage{}, test.streamOutcome)
			if got.UnpricedReason == nil || *got.UnpricedReason != test.wantReason {
				t.Fatalf("expected reason %q, got %+v", test.wantReason, got)
			}
		})
	}
}

func TestNewRuntimeHTTPClientUsesConfiguredRequestTimeout(t *testing.T) {
	settings := config.Settings{
		RuntimeTransportConfig: config.RuntimeTransportConfig{
			RequestTimeout: 17 * time.Second,
		},
	}

	client := newRuntimeHTTPClient(settings)
	if client.Timeout != 17*time.Second {
		t.Fatalf("expected runtime HTTP client timeout 17s, got %v", client.Timeout)
	}
}

func TestRuntimeProxyConfigProviderUpdatesNewPlansAndKeepsExistingPlanClient(t *testing.T) {
	oldClient := &http.Client{Timeout: 17 * time.Second}
	newClient := &http.Client{Timeout: 23 * time.Second}
	provider := &mutableRuntimeProxyConfigProvider{snapshot: RuntimeProxyConfigSnapshot{BufferingMode: config.RuntimeBufferingModeBuffered, HTTPClient: oldClient}}
	service := &Service{runtimeProxyConfigProvider: provider}

	oldSnapshot := service.runtimeProxyConfigSnapshot()
	oldPlan := requestPlan{HTTPClient: oldSnapshot.HTTPClient}
	provider.snapshot = RuntimeProxyConfigSnapshot{BufferingMode: config.RuntimeBufferingModeBuffered, HTTPClient: newClient}
	newSnapshot := service.runtimeProxyConfigSnapshot()
	newPlan := requestPlan{HTTPClient: newSnapshot.HTTPClient}

	if oldPlan.HTTPClient != oldClient || oldPlan.HTTPClient.Timeout != 17*time.Second {
		t.Fatalf("expected existing plan to keep old client snapshot, got %+v", oldPlan.HTTPClient)
	}
	if newPlan.HTTPClient != newClient || newPlan.HTTPClient.Timeout != 23*time.Second {
		t.Fatalf("expected new plan to use updated client snapshot, got %+v", newPlan.HTTPClient)
	}
}

func TestBuildRuntimePricingResult(t *testing.T) {
	cachedInputPrice := "1"
	cacheCreationPrice := "2"
	reasoningPrice := "3"
	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{
		ID:                  42,
		PricingUnit:         runtimePricingUnitPerMillion,
		PricingCurrencyCode: "USD",
		InputPrice:          "2",
		OutputPrice:         "5",
		CachedInputPrice:    &cachedInputPrice,
		CacheCreationPrice:  &cacheCreationPrice,
		ReasoningPrice:      &reasoningPrice,
		Version:             7,
	}
	reportCurrencySnapshot := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}
	zero := 0
	positiveCacheRead := 4
	positiveCacheCreation := 5
	positiveReasoning := 6
	inputTokens := 10
	outputTokens := 10
	totalTokens := 20

	tests := []struct {
		name  string
		usage responseUsage
		want  runtimePricingResult
	}{
		{
			name: "prices base usage when optional counters are omitted",
			usage: responseUsage{
				InputTokens:  &inputTokens,
				OutputTokens: &outputTokens,
				TotalTokens:  &totalTokens,
			},
			want: runtimePricingResult{
				Billable:                          true,
				Priced:                            true,
				InputCostMicros:                   int64Ptr(20),
				OutputCostMicros:                  int64Ptr(50),
				CacheReadInputCostMicros:          int64Ptr(0),
				CacheCreationInputCostMicros:      int64Ptr(0),
				ReasoningCostMicros:               int64Ptr(0),
				TotalCostOriginalMicros:           int64Ptr(70),
				TotalCostUserCurrencyMicros:       int64Ptr(70),
				CurrencyCodeOriginal:              stringPtr("USD"),
				ReportCurrencyCode:                stringPtr("USD"),
				ReportCurrencySymbol:              stringPtr("$"),
				FXRateUsed:                        stringPtr("1"),
				FXRateSource:                      stringPtr(runtimeFXSourceDefaultOneToOne),
				PricingSnapshotUnit:               stringPtr(runtimePricingUnitPerMillion),
				PricingSnapshotInput:              stringPtr("2"),
				PricingSnapshotOutput:             stringPtr("5"),
				PricingSnapshotCacheReadInput:     stringPtr("1"),
				PricingSnapshotCacheCreationInput: stringPtr("2"),
				PricingSnapshotReasoning:          stringPtr("3"),
				PricingConfigVersionUsed:          intPtr(7),
			},
		},
		{
			name: "keeps missing token usage for missing required base usage",
			usage: responseUsage{
				OutputTokens: &outputTokens,
				TotalTokens:  &outputTokens,
			},
			want: runtimePricingResult{
				Billable:       true,
				UnpricedReason: stringPtr(runtimeUnpricedReasonMissingUsage),
			},
		},
		{
			name: "prices optional counters explicitly set to zero",
			usage: responseUsage{
				InputTokens:              &inputTokens,
				OutputTokens:             &outputTokens,
				TotalTokens:              &totalTokens,
				CacheReadInputTokens:     &zero,
				CacheCreationInputTokens: &zero,
				ReasoningTokens:          &zero,
			},
			want: runtimePricingResult{
				Billable:                          true,
				Priced:                            true,
				InputCostMicros:                   int64Ptr(20),
				OutputCostMicros:                  int64Ptr(50),
				CacheReadInputCostMicros:          int64Ptr(0),
				CacheCreationInputCostMicros:      int64Ptr(0),
				ReasoningCostMicros:               int64Ptr(0),
				TotalCostOriginalMicros:           int64Ptr(70),
				TotalCostUserCurrencyMicros:       int64Ptr(70),
				CurrencyCodeOriginal:              stringPtr("USD"),
				ReportCurrencyCode:                stringPtr("USD"),
				ReportCurrencySymbol:              stringPtr("$"),
				FXRateUsed:                        stringPtr("1"),
				FXRateSource:                      stringPtr(runtimeFXSourceDefaultOneToOne),
				PricingSnapshotUnit:               stringPtr(runtimePricingUnitPerMillion),
				PricingSnapshotInput:              stringPtr("2"),
				PricingSnapshotOutput:             stringPtr("5"),
				PricingSnapshotCacheReadInput:     stringPtr("1"),
				PricingSnapshotCacheCreationInput: stringPtr("2"),
				PricingSnapshotReasoning:          stringPtr("3"),
				PricingConfigVersionUsed:          intPtr(7),
			},
		},
		{
			name: "prices positive optional counters independently",
			usage: responseUsage{
				InputTokens:              &inputTokens,
				OutputTokens:             &outputTokens,
				TotalTokens:              &totalTokens,
				CacheReadInputTokens:     &positiveCacheRead,
				CacheCreationInputTokens: &positiveCacheCreation,
				ReasoningTokens:          &positiveReasoning,
			},
			want: runtimePricingResult{
				Billable:                          true,
				Priced:                            true,
				InputCostMicros:                   int64Ptr(20),
				OutputCostMicros:                  int64Ptr(50),
				CacheReadInputCostMicros:          int64Ptr(4),
				CacheCreationInputCostMicros:      int64Ptr(10),
				ReasoningCostMicros:               int64Ptr(18),
				TotalCostOriginalMicros:           int64Ptr(102),
				TotalCostUserCurrencyMicros:       int64Ptr(102),
				CurrencyCodeOriginal:              stringPtr("USD"),
				ReportCurrencyCode:                stringPtr("USD"),
				ReportCurrencySymbol:              stringPtr("$"),
				FXRateUsed:                        stringPtr("1"),
				FXRateSource:                      stringPtr(runtimeFXSourceDefaultOneToOne),
				PricingSnapshotUnit:               stringPtr(runtimePricingUnitPerMillion),
				PricingSnapshotInput:              stringPtr("2"),
				PricingSnapshotOutput:             stringPtr("5"),
				PricingSnapshotCacheReadInput:     stringPtr("1"),
				PricingSnapshotCacheCreationInput: stringPtr("2"),
				PricingSnapshotReasoning:          stringPtr("3"),
				PricingConfigVersionUsed:          intPtr(7),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildRuntimePricingResult(reportCurrencySnapshot, pricingTemplateSnapshot, nil, test.usage, runtimeStreamOutcomeCompleted)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("expected pricing result %+v, got %+v", test.want, got)
			}
		})
	}
}

func TestBuildRuntimePricingResultAllowsMissingOptionalPriceWhenDimensionIsUnused(t *testing.T) {
	inputTokens := 10
	outputTokens := 10
	totalTokens := 20
	zero := 0
	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{
		PricingUnit:         runtimePricingUnitPerMillion,
		PricingCurrencyCode: "USD",
		InputPrice:          "2",
		OutputPrice:         "5",
		Version:             7,
	}

	got := buildRuntimePricingResult(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}, pricingTemplateSnapshot, nil, responseUsage{
		InputTokens:     &inputTokens,
		OutputTokens:    &outputTokens,
		TotalTokens:     &totalTokens,
		ReasoningTokens: &zero,
	}, runtimeStreamOutcomeCompleted)

	want := runtimePricingResult{
		Billable:                     true,
		Priced:                       true,
		InputCostMicros:              int64Ptr(20),
		OutputCostMicros:             int64Ptr(50),
		CacheReadInputCostMicros:     int64Ptr(0),
		CacheCreationInputCostMicros: int64Ptr(0),
		ReasoningCostMicros:          int64Ptr(0),
		TotalCostOriginalMicros:      int64Ptr(70),
		TotalCostUserCurrencyMicros:  int64Ptr(70),
		CurrencyCodeOriginal:         stringPtr("USD"),
		ReportCurrencyCode:           stringPtr("USD"),
		ReportCurrencySymbol:         stringPtr("$"),
		FXRateUsed:                   stringPtr("1"),
		FXRateSource:                 stringPtr(runtimeFXSourceDefaultOneToOne),
		PricingSnapshotUnit:          stringPtr(runtimePricingUnitPerMillion),
		PricingSnapshotInput:         stringPtr("2"),
		PricingSnapshotOutput:        stringPtr("5"),
		PricingConfigVersionUsed:     intPtr(7),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected missing unused optional dimension to remain priced: want %+v got %+v", want, got)
	}
}

func TestBuildRuntimePricingResultMarksMissingOptionalPriceDataUnpricedWhenDimensionIsUsed(t *testing.T) {
	inputTokens := 10
	outputTokens := 10
	totalTokens := 20
	reasoningTokens := 3
	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{
		PricingUnit:         runtimePricingUnitPerMillion,
		PricingCurrencyCode: "USD",
		InputPrice:          "2",
		OutputPrice:         "5",
		Version:             7,
	}

	got := buildRuntimePricingResult(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}, pricingTemplateSnapshot, nil, responseUsage{
		InputTokens:     &inputTokens,
		OutputTokens:    &outputTokens,
		TotalTokens:     &totalTokens,
		ReasoningTokens: &reasoningTokens,
	}, runtimeStreamOutcomeCompleted)

	want := runtimePricingResult{
		Billable:       true,
		UnpricedReason: stringPtr(runtimeUnpricedReasonMissingData),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected missing used optional dimension to degrade pricing: want %+v got %+v", want, got)
	}
}

type failingWriter struct {
	err error
}

func (writer failingWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

type cancelOnBlankLineWriter struct {
	dst    io.Writer
	cancel context.CancelFunc
}

func (writer cancelOnBlankLineWriter) Write(payload []byte) (int, error) {
	n, err := writer.dst.Write(payload)
	if err == nil && len(bytes.TrimSpace(payload)) == 0 {
		writer.cancel()
	}
	return n, err
}

type errorAfterReader struct {
	payload []byte
	err     error
	sent    bool
}

func (reader *errorAfterReader) Read(payload []byte) (int, error) {
	if reader.sent {
		return 0, reader.err
	}
	reader.sent = true
	return copy(payload, reader.payload), reader.err
}

var _ io.Reader = (*errorAfterReader)(nil)

type mutableRuntimeProxyConfigProvider struct {
	snapshot RuntimeProxyConfigSnapshot
}

func (p *mutableRuntimeProxyConfigProvider) RuntimeProxyConfigSnapshot() RuntimeProxyConfigSnapshot {
	return p.snapshot
}

func TestProxyNonEventResponseAndCaptureUsageAcceptsOnlySupportedUsageSchemaPaths(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    responseUsage
	}{
		{
			name:    "keeps top-level usage and ignores nested spoofed usage object",
			payload: `{"id":"chatcmpl-secure-stream","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"output_text","text":"hello"},{"type":"output_json","value":{"usage":{"prompt_tokens":999,"completion_tokens":999,"total_tokens":1998}}}]}}],"usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20}}`,
			want: responseUsage{
				InputTokens:  intPtr(7),
				OutputTokens: intPtr(13),
				TotalTokens:  intPtr(20),
			},
		},
		{
			name:    "keeps response usage and ignores nested spoofed usage object",
			payload: `{"response":{"id":"resp-secure-stream","output":[{"type":"message","content":[{"type":"output_text","text":"hello","usage":{"input_tokens":999,"output_tokens":999,"total_tokens":1998}}]}],"usage":{"input_tokens":5,"output_tokens":8,"total_tokens":13}}}`,
			want: responseUsage{
				InputTokens:  intPtr(5),
				OutputTokens: intPtr(8),
				TotalTokens:  intPtr(13),
			},
		},
		{
			name:    "keeps top-level usage metadata and ignores nested spoofed usage metadata object",
			payload: `{"candidates":[{"content":{"parts":[{"text":"hello"},{"metadata":{"usageMetadata":{"promptTokenCount":999,"candidatesTokenCount":999,"totalTokenCount":1998}}}]}}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":13,"totalTokenCount":20}}`,
			want: responseUsage{
				InputTokens:  intPtr(7),
				OutputTokens: intPtr(13),
				TotalTokens:  intPtr(20),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var forwarded bytes.Buffer
			capture, err := proxyNonEventResponseAndCaptureUsage(&forwarded, strings.NewReader(test.payload), "application/json", time.Now, false)
			if err != nil {
				t.Fatalf("capture streamed non-sse usage: %v", err)
			}
			if forwarded.String() != test.payload {
				t.Fatalf("expected streamed response body to pass through unchanged, got %q", forwarded.String())
			}
			if got := capture.extractedUsage(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("expected extracted usage %+v, got %+v", test.want, got)
			}
		})
	}
}
