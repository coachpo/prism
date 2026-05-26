package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestModelResolutionAndRewriteHelpers(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	chatOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions")
	if got, err := resolveModelIDForOperation(rawBody, "application/json", chatOperation); err != nil || got != "gpt-4o" {
		t.Fatalf("expected body model id, got model=%q err=%v", got, err)
	}
	responsesBody := []byte(`{"model":"gpt-4o","input":"hello"}`)
	responsesOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses")
	if got, err := resolveModelIDForOperation(responsesBody, "application/json", responsesOperation); err != nil || got != "gpt-4o" {
		t.Fatalf("expected Responses body model id, got model=%q err=%v", got, err)
	}
	geminiOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent")
	if got, err := resolveModelIDForOperation(nil, "", geminiOperation); err != nil || got != "gemini-2.5-pro" {
		t.Fatalf("expected path model id, got model=%q err=%v", got, err)
	}
	rewrittenBody := rewriteModelInBody(rawBody, "gpt-4o-mini")
	if got := extractModelFromBody(rewrittenBody); got != "gpt-4o-mini" {
		t.Fatalf("expected rewritten model id in body, got %q", got)
	}
	if got := rewriteModelInPath("/v1beta/models/gemini-1.5-pro:generateContent", "gemini-1.5-pro", "gemini-2.5-pro"); got != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("expected rewritten Gemini path, got %q", got)
	}
	if _, err := resolveModelIDForOperation([]byte(`{"messages":[]}`), "application/json", chatOperation); err == nil {
		t.Fatal("expected missing model id to fail")
	}
	if _, err := resolveModelIDForOperation([]byte(`{"input":"hello"}`), "application/json", responsesOperation); err == nil {
		t.Fatal("expected missing Responses model id to fail")
	}
}

func TestBuildRequestPlanCarriesOperation(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "gemini", ModelID: "gemini-public", ModelType: "native"})
	request := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-public:generateContent", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)

	plan, err := service.buildRequestPlanFromSnapshot(request, nil, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build request plan: %v", err)
	}
	operationMatch.PathParams["model"] = "mutated"

	if plan.RuntimeOperation.Name != "gemini.generate_content" {
		t.Fatalf("expected plan operation gemini.generate_content, got %+v", plan.RuntimeOperation)
	}
	if plan.RuntimeOperation.HookCollectionID != "gemini.generate_content" {
		t.Fatalf("expected hook collection id to travel with operation, got %q", plan.RuntimeOperation.HookCollectionID)
	}
	if got := plan.RuntimeOperationPathParams["model"]; got != "gemini-public" {
		t.Fatalf("expected path model param gemini-public, got %q", got)
	}
}

func TestBuildRequestPlanClassifiesGeminiStreamingByOperation(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "gemini", ModelID: "gemini-public", ModelType: "native"})

	tests := []struct {
		name       string
		path       string
		rawBody    []byte
		wantStream bool
	}{
		{
			name:       "generateContent ignores body stream flag",
			path:       "/v1beta/models/gemini-public:generateContent",
			rawBody:    []byte(`{"contents":[],"stream":true}`),
			wantStream: false,
		},
		{
			name:       "streamGenerateContent is path authoritative",
			path:       "/v1beta/models/gemini-public:streamGenerateContent",
			rawBody:    []byte(`{"contents":[],"stream":false}`),
			wantStream: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, nil)
			operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)

			plan, err := service.buildRequestPlanFromSnapshot(request, test.rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
			if err != nil {
				t.Fatalf("build request plan: %v", err)
			}
			if plan.IsStreamingRequest != test.wantStream {
				t.Fatalf("expected IsStreamingRequest=%v for %s, got %v", test.wantStream, plan.RuntimeOperation.Name, plan.IsStreamingRequest)
			}
		})
	}
}

func TestBuildRequestPlanAppliesOperationRewriteRules(t *testing.T) {
	t.Run("path-bound operation rewrites only path model", func(t *testing.T) {
		service := newRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(
			runtimeModelRecord{ID: 1, APIFamily: "gemini", ModelID: "public-gemini", ModelType: "proxy", ProxySelectionStrategy: proxySelectionStrategyOrderedFallback},
			runtimeModelRecord{ID: 2, APIFamily: "gemini", ModelID: "target-gemini", ModelType: "native"},
		)
		addRequestPlanProxyTarget(snapshot, "public-gemini", "target-gemini")
		rawBody := []byte(`{"model":"body-should-not-change","contents":[]}`)
		request := httptest.NewRequest(http.MethodPost, "/v1beta/models/public-gemini:generateContent", nil)
		operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)

		plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
		if err != nil {
			t.Fatalf("build request plan: %v", err)
		}

		if plan.EffectiveRequestPath != "/v1beta/models/target-gemini:generateContent" {
			t.Fatalf("expected rewritten Gemini path, got %q", plan.EffectiveRequestPath)
		}
		if !bytes.Equal(plan.UpstreamBody, rawBody) {
			t.Fatalf("expected path-bound operation to leave body unchanged, got %s", string(plan.UpstreamBody))
		}
		if got := extractModelFromBody(plan.UpstreamBody); got != "body-should-not-change" {
			t.Fatalf("expected body model to remain untouched, got %q", got)
		}
	})

	t.Run("body-bound operation rewrites only body model", func(t *testing.T) {
		service := newRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(
			runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "public-openai", ModelType: "proxy", ProxySelectionStrategy: proxySelectionStrategyOrderedFallback},
			runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "target-openai", ModelType: "native"},
		)
		addRequestPlanProxyTarget(snapshot, "public-openai", "target-openai")
		rawBody := []byte(`{"model":"public-openai","messages":[]}`)
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)

		plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
		if err != nil {
			t.Fatalf("build request plan: %v", err)
		}

		if plan.EffectiveRequestPath != "/v1/chat/completions" {
			t.Fatalf("expected body-bound operation to leave path unchanged, got %q", plan.EffectiveRequestPath)
		}
		if got := extractModelFromBody(plan.UpstreamBody); got != "target-openai" {
			t.Fatalf("expected rewritten body model target-openai, got %q in %s", got, string(plan.UpstreamBody))
		}
	})

	t.Run("operation api family must match resolved target", func(t *testing.T) {
		service := newRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "gemini", ModelID: "gemini-native", ModelType: "native"})
		rawBody := []byte(`{"model":"gemini-native","messages":[]}`)
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)

		_, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
		assertPlanDomainError(t, err, http.StatusBadRequest, "incompatible")
	})

	t.Run("unsupported model-binding source fails before model fallback", func(t *testing.T) {
		service := newRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-4o", ModelType: "native"})
		rawBody := []byte(`{"model":"gpt-4o","messages":[]}`)
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		operationMatch := RuntimeOperationMatch{Operation: RuntimeOperation{
			Name:               "test.header_model",
			Method:             http.MethodPost,
			APIFamily:          "openai",
			PathTemplate:       "/v1/chat/completions",
			PathMatcher:        staticRuntimeOperationPath("/v1/chat/completions"),
			ModelBindingSource: RuntimeOperationModelBindingSource("header"),
		}}

		_, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
		assertPlanDomainError(t, err, http.StatusBadRequest, "unsupported model binding source")
	})

	t.Run("generic OpenAI v1 path is not a planning fallback", func(t *testing.T) {
		service := newRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-4o", ModelType: "native"})
		rawBody := []byte(`{"model":"gpt-4o"}`)
		request := httptest.NewRequest(http.MethodPost, "/v1/models", nil)
		operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions")

		_, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
		assertPlanDomainError(t, err, http.StatusNotFound, runtimeOperationNotFoundDetail)
	})
}

func TestHeaderHelpers(t *testing.T) {
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
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	writeFailure := errors.New("write failed\nwith\tcontrol")
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), failingWriter{err: writeFailure}, strings.NewReader("event: response.created\ndata: {\"type\":\"response.created\"}\n\n"), time.Now, false)
	if !errors.Is(err, writeFailure) {
		t.Fatalf("expected write failure, got %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeClientDisconnected || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindClientWriteFailed || capture.StreamErrorDetail == nil || *capture.StreamErrorDetail != "write failed with control" {
		t.Fatalf("expected sanitized client-disconnected capture, got %+v", capture)
	}

	readFailure := errors.New("upstream read failed")
	capture, err = proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, &errorAfterReader{payload: []byte("event: response.created\ndata: {\"type\":\"response.created\"}\n\n"), err: readFailure}, time.Now, false)
	if !errors.Is(err, readFailure) {
		t.Fatalf("expected upstream read failure, got %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeUpstreamReadError || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindUpstreamReadFailed || capture.StreamErrorDetail == nil || *capture.StreamErrorDetail != "upstream read failed" {
		t.Fatalf("expected upstream read error capture, got %+v", capture)
	}
}

func TestProxyEventStreamClassifiesEOFWithoutTerminal(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader("event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"usage\":null}}\n\n"), time.Now, false)
	if err != nil {
		t.Fatalf("proxy SSE without terminal: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeUpstreamEndedWithoutTerminal || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindMissingTerminalEvent || capture.StreamErrorDetail != nil || capture.CompletedAt != nil || capture.Usage.hasValues() {
		t.Fatalf("expected missing-terminal EOF capture without usage, got %+v", capture)
	}
}

func TestProxyEventStreamPreservesTerminalTimingWhenTransportOutranksTerminal(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	terminalStream := "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n"

	canceledContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	var forwarded bytes.Buffer
	cancelingWriter := cancelOnBlankLineWriter{dst: &forwarded, cancel: cancel}
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, canceledContext, cancelingWriter, strings.NewReader(terminalStream), time.Now, false)
	if err != nil {
		t.Fatalf("proxy SSE with canceled context after terminal: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeClientDisconnected || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindRequestContextCanceled || capture.CompletedAt == nil {
		t.Fatalf("expected context cancellation to outrank terminal while preserving completion timing, got %+v", capture)
	}

	readFailure := errors.New("upstream read failed after terminal")
	forwarded.Reset()
	capture, err = proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, &errorAfterReader{payload: []byte(terminalStream), err: readFailure}, time.Now, false)
	if !errors.Is(err, readFailure) {
		t.Fatalf("expected upstream read failure, got %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeUpstreamReadError || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindUpstreamReadFailed || capture.CompletedAt == nil {
		t.Fatalf("expected upstream read error to outrank terminal while preserving completion timing, got %+v", capture)
	}
}

func TestProxyEventStreamRecognizesOpenAIDONESentinel(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{PricingUnit: runtimePricingUnitPerMillion, PricingCurrencyCode: "USD", InputPrice: "2", OutputPrice: "5", CachedInputPrice: "0", CacheCreationPrice: "0", ReasoningPrice: "0", Version: 1}
	reportCurrencySnapshot := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}

	t.Run("completed without usage", func(t *testing.T) {
		stream := "data: {\"id\":\"chatcmpl-done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n" +
			"data:   [DONE]  \n\n"
		var forwarded bytes.Buffer
		capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(stream), time.Now, false)
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
		capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader("data: not-json\n\n"), time.Now, false)
		if err != nil {
			t.Fatalf("proxy SSE with non-JSON payload: %v", err)
		}
		if capture.StreamOutcome != runtimeStreamOutcomeUpstreamEndedWithoutTerminal || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindMissingTerminalEvent || capture.CompletedAt != nil {
			t.Fatalf("expected non-[DONE] non-JSON payload to remain missing terminal, got %+v", capture)
		}
	})
}

func TestProxyEventStreamMergesUsageBeforeOpenAIDONESentinel(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	stream := "data: {\"id\":\"chatcmpl-usage\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}],\"usage\":{\"prompt_tokens\":999,\"completion_tokens\":999,\"total_tokens\":1998}}\n\n" +
		"data: {\"id\":\"chatcmpl-usage\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens_details\":{\"reasoning_tokens\":3}}}\n\n" +
		"data: [DONE]\n\n"
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(stream), time.Now, false)
	if err != nil {
		t.Fatalf("proxy SSE with usage and [DONE]: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted || capture.CompletedAt == nil || capture.StreamErrorKind != nil || capture.StreamErrorDetail != nil {
		t.Fatalf("expected usage stream ending with [DONE] to complete, got %+v", capture)
	}
	wantUsage := responseUsage{InputTokens: intPtr(6), OutputTokens: intPtr(3), TotalTokens: intPtr(16), CacheReadInputTokens: intPtr(4), ReasoningTokens: intPtr(3)}
	if !reflect.DeepEqual(capture.Usage, wantUsage) {
		t.Fatalf("expected final include_usage chunk to be preserved: want %+v got %+v", wantUsage, capture.Usage)
	}

	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{PricingUnit: runtimePricingUnitPerMillion, PricingCurrencyCode: "USD", InputPrice: "2", OutputPrice: "5", CachedInputPrice: "0", CacheCreationPrice: "0", ReasoningPrice: "0", Version: 1}
	pricing := buildRuntimePricingResult(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}, pricingTemplateSnapshot, nil, capture.Usage, capture.StreamOutcome)
	if !pricing.Billable || !pricing.Priced || pricing.UnpricedReason != nil {
		t.Fatalf("expected observed usage before [DONE] to price normally, got %+v", pricing)
	}
}

func TestProxyEventStreamCapturesRawAuditBodyWhenEnabled(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	stream := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"usage\":{\"input_tokens\":999,\"output_tokens\":999,\"total_tokens\":1998}}}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":9,\"output_tokens\":7,\"total_tokens\":16,\"input_tokens_details\":{\"cached_tokens\":2},\"output_tokens_details\":{\"reasoning_tokens\":5}}}}\n\n"
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(stream), time.Now, true)
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
	wantUsage := responseUsage{InputTokens: intPtr(7), OutputTokens: intPtr(2), TotalTokens: intPtr(16), CacheReadInputTokens: intPtr(2), ReasoningTokens: intPtr(5)}
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, wantUsage) {
		t.Fatalf("expected usage extraction to survive audit capture: want %+v got %+v", wantUsage, got)
	}
}

func TestAnthropicMessagesStreamUsageUsesFinalCumulativeOutput(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/messages").Operation
	stream := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7,\"cache_read_input_tokens\":2,\"cache_creation_input_tokens\":3,\"output_tokens\":1}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"do not synthesize reasoning\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":13}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(stream), time.Now, false)
	if err != nil {
		t.Fatalf("proxy Anthropic SSE stream: %v", err)
	}
	if forwarded.String() != stream {
		t.Fatalf("expected SSE stream to pass through unchanged, got %q", forwarded.String())
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted || capture.CompletedAt == nil || capture.StreamErrorKind != nil || capture.StreamErrorDetail != nil {
		t.Fatalf("expected Anthropic stream to complete cleanly, got %+v", capture)
	}
	wantUsage := responseUsage{InputTokens: intPtr(7), OutputTokens: intPtr(13), TotalTokens: intPtr(25), CacheReadInputTokens: intPtr(2), CacheCreationInputTokens: intPtr(3)}
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, wantUsage) {
		t.Fatalf("expected final cumulative output without summing deltas: want %+v got %+v", wantUsage, got)
	}
}

func TestSSEStreamHooksByOperation(t *testing.T) {
	tests := []struct {
		name         string
		requestPath  string
		stream       string
		wantProvider string
		wantKind     operationResponseKind
		wantOutcome  string
		wantUsage    responseUsage
	}{
		{
			name:         "openai responses completed owns response terminal",
			requestPath:  "/v1/responses",
			stream:       "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response\":{\"usage\":{\"input_tokens\":999,\"output_tokens\":999,\"total_tokens\":1998}},\"delta\":\"partial\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":8,\"total_tokens\":13,\"input_tokens_details\":{\"cached_tokens\":2},\"output_tokens_details\":{\"reasoning_tokens\":3}}}}\n\n",
			wantProvider: "openai",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeCompleted,
			wantUsage:    responseUsage{InputTokens: intPtr(3), OutputTokens: intPtr(5), TotalTokens: intPtr(13), CacheReadInputTokens: intPtr(2), ReasoningTokens: intPtr(3)},
		},
		{
			name:         "openai responses incomplete owns provider incomplete terminal",
			requestPath:  "/v1/responses",
			stream:       "event: response.incomplete\ndata: {\"type\":\"response.incomplete\"}\n\n",
			wantProvider: "openai",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeProviderIncomplete,
		},
		{
			name:         "anthropic messages owns message stop",
			requestPath:  "/v1/messages",
			stream:       "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7,\"total_tokens\":18}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
			wantProvider: "anthropic",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeCompleted,
			wantUsage:    responseUsage{InputTokens: intPtr(11), OutputTokens: intPtr(7), TotalTokens: intPtr(18)},
		},
		{
			name:         "gemini stream generate owns usage metadata terminal",
			requestPath:  "/v1beta/models/gemini-2.5-pro:streamGenerateContent",
			stream:       "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":13,\"totalTokenCount\":20,\"cachedContentTokenCount\":3,\"thoughtsTokenCount\":5}}\n\n",
			wantProvider: "gemini",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeCompleted,
			wantUsage:    responseUsage{InputTokens: intPtr(4), OutputTokens: intPtr(8), TotalTokens: intPtr(20), CacheReadInputTokens: intPtr(3), ReasoningTokens: intPtr(5)},
		},
		{
			name:         "gemini stream generate owns done terminal",
			requestPath:  "/v1beta/models/gemini-2.5-pro:streamGenerateContent",
			stream:       "data: {\"done\":true}\n\n",
			wantProvider: "gemini",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeCompleted,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			hooks, ok := streamHooksForProxyResponse(operation, true)
			if !ok {
				t.Fatalf("expected stream hooks for %s", operation.Name)
			}
			if hooks.Provider != test.wantProvider || hooks.Kind != test.wantKind {
				t.Fatalf("expected %s/%s stream hooks, got %+v", test.wantProvider, test.wantKind, hooks)
			}
			var forwarded bytes.Buffer
			capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(test.stream), time.Now, false)
			if err != nil {
				t.Fatalf("proxy SSE stream: %v", err)
			}
			if forwarded.String() != test.stream {
				t.Fatalf("expected SSE stream to pass through unchanged, got %q", forwarded.String())
			}
			if capture.StreamOutcome != test.wantOutcome {
				t.Fatalf("expected outcome %q, got %+v", test.wantOutcome, capture)
			}
			if got := capture.extractedUsage(); !reflect.DeepEqual(got, test.wantUsage) {
				t.Fatalf("expected usage %+v, got %+v", test.wantUsage, got)
			}
		})
	}

	responsesHooks, _ := streamHooksForOperation(mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation)
	if got := responsesHooks.terminalSignal("message_stop", map[string]any{"type": "message_stop"}); got != sseTerminalSignalNone {
		t.Fatalf("expected OpenAI responses hook to ignore Anthropic message_stop, got %d", got)
	}
	anthropicHooks, _ := streamHooksForOperation(mustResolveRuntimeOperation(t, http.MethodPost, "/v1/messages").Operation)
	if got := anthropicHooks.terminalSignal("response.completed", map[string]any{"type": "response.completed"}); got != sseTerminalSignalNone {
		t.Fatalf("expected Anthropic hook to ignore OpenAI response.completed, got %d", got)
	}
	geminiHooks, _ := streamHooksForOperation(mustResolveRuntimeOperation(t, http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent").Operation)
	if got := geminiHooks.terminalSignal("response.completed", map[string]any{"type": "response.completed"}); got != sseTerminalSignalNone {
		t.Fatalf("expected Gemini hook to ignore OpenAI response.completed, got %d", got)
	}
}

func TestNonStreamOperationsCannotUseSSEHooks(t *testing.T) {
	responsesOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	if _, ok := streamHooksForProxyResponse(responsesOperation, false); ok {
		t.Fatal("expected non-stream OpenAI responses request to skip SSE hook selection")
	}

	geminiStreamPath := "/v1beta/models/gemini-2.5-pro:streamGenerateContent"
	geminiStreamOperation := mustResolveRuntimeOperation(t, http.MethodPost, geminiStreamPath).Operation
	if !requestWantsStreamForOperation(geminiStreamOperation, nil, geminiStreamPath) {
		t.Fatal("expected Gemini streamGenerateContent path to imply streaming")
	}
	if _, ok := streamHooksForProxyResponse(geminiStreamOperation, true); !ok {
		t.Fatal("expected Gemini streamGenerateContent to select SSE hooks")
	}

	tests := []struct {
		name        string
		requestPath string
	}{
		{name: "openai image generations", requestPath: "/v1/images/generations"},
		{name: "openai image edits", requestPath: "/v1/images/edits"},
		{name: "anthropic count tokens", requestPath: "/v1/messages/count_tokens"},
		{name: "gemini generate content", requestPath: "/v1beta/models/gemini-2.5-pro:generateContent"},
		{name: "gemini count tokens", requestPath: "/v1beta/models/gemini-2.5-pro:countTokens"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			if _, ok := streamHooksForOperation(operation); ok {
				t.Fatalf("expected no SSE hooks for %s", operation.Name)
			}
			if _, ok := streamHooksForProxyResponse(operation, true); ok {
				t.Fatalf("expected %s to skip SSE parser dispatch even when forced streaming", operation.Name)
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
	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{PricingUnit: runtimePricingUnitPerMillion, PricingCurrencyCode: "USD", InputPrice: "2", OutputPrice: "5", CachedInputPrice: "0", CacheCreationPrice: "0", ReasoningPrice: "0", Version: 1}
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

func TestBuildRuntimePricingResultRequiresUsageBeforePriceData(t *testing.T) {
	validPricingTemplateSnapshot := &runtimePricingTemplateSnapshot{PricingUnit: runtimePricingUnitPerMillion, PricingCurrencyCode: "USD", InputPrice: "2", OutputPrice: "5", CachedInputPrice: "0", CacheCreationPrice: "0", ReasoningPrice: "0", Version: 1}

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
			name:                   "interrupted missing usage beats invalid input price",
			reportCurrencySnapshot: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
			pricingTemplateSnapshot: &runtimePricingTemplateSnapshot{
				PricingUnit:         runtimePricingUnitPerMillion,
				PricingCurrencyCode: "USD",
				InputPrice:          "not-a-decimal",
				OutputPrice:         "5",
				CachedInputPrice:    "0",
				CacheCreationPrice:  "0",
				ReasoningPrice:      "0",
				Version:             1,
			},
			streamOutcome: runtimeStreamOutcomeUpstreamReadError,
			wantReason:    runtimeUnpricedReasonStreamUsageUnavailable,
		},
		{
			name:                   "completed missing usage beats invalid output price",
			reportCurrencySnapshot: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
			pricingTemplateSnapshot: &runtimePricingTemplateSnapshot{
				PricingUnit:         runtimePricingUnitPerMillion,
				PricingCurrencyCode: "USD",
				InputPrice:          "2",
				OutputPrice:         "not-a-decimal",
				CachedInputPrice:    "0",
				CacheCreationPrice:  "0",
				ReasoningPrice:      "0",
				Version:             1,
			},
			streamOutcome: runtimeStreamOutcomeCompleted,
			wantReason:    runtimeUnpricedReasonMissingUsage,
		},
		{
			name:                    "interrupted missing usage beats missing fx",
			reportCurrencySnapshot:  runtimeReportCurrencySnapshot{Code: "EUR", Symbol: "EUR"},
			pricingTemplateSnapshot: validPricingTemplateSnapshot,
			streamOutcome:           runtimeStreamOutcomeUpstreamEndedWithoutTerminal,
			wantReason:              runtimeUnpricedReasonStreamUsageUnavailable,
		},
		{
			name:                    "completed missing usage beats invalid fx",
			reportCurrencySnapshot:  runtimeReportCurrencySnapshot{Code: "EUR", Symbol: "EUR"},
			pricingTemplateSnapshot: validPricingTemplateSnapshot,
			endpointFXSnapshot:      &runtimeEndpointFXSnapshot{FXRate: "not-a-decimal"},
			streamOutcome:           runtimeStreamOutcomeCompleted,
			wantReason:              runtimeUnpricedReasonMissingUsage,
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

func TestNewRuntimeHTTPClientUsesTransportDefaultsAndOverrides(t *testing.T) {
	defaultClient := newRuntimeHTTPClient(config.Load())
	if defaultClient.Timeout != 300*time.Second {
		t.Fatalf("expected canonical runtime HTTP client timeout 300s, got %v", defaultClient.Timeout)
	}
	defaultTransport, ok := defaultClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected canonical runtime HTTP transport, got %T", defaultClient.Transport)
	}
	if defaultTransport.MaxIdleConns != 100 || defaultTransport.MaxIdleConnsPerHost != 16 || defaultTransport.MaxConnsPerHost != 16 {
		t.Fatalf("expected canonical runtime transport caps 100/16/16, got %+v", defaultTransport)
	}
	if defaultTransport.IdleConnTimeout != 90*time.Second || defaultTransport.ResponseHeaderTimeout != 0 || defaultTransport.TLSHandshakeTimeout != 10*time.Second || defaultTransport.ExpectContinueTimeout != time.Second {
		t.Fatalf("unexpected canonical runtime transport timeouts: %+v", defaultTransport)
	}

	zeroSettings := config.Load()
	zeroSettings.RuntimeTransportConfig.MaxConnsPerHost = 0
	zeroClient := newRuntimeHTTPClient(zeroSettings)
	zeroTransport, ok := zeroClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected explicit-zero runtime HTTP transport, got %T", zeroClient.Transport)
	}
	if zeroTransport.MaxConnsPerHost != 0 {
		t.Fatalf("expected explicit maxConnsPerHost=0 to remain unlimited, got %+v", zeroTransport)
	}

	settings := config.Load()
	settings.RuntimeTransportConfig.RequestTimeout = 17 * time.Second
	configuredClient := newRuntimeHTTPClient(settings)
	if configuredClient.Timeout != 17*time.Second {
		t.Fatalf("expected configured runtime HTTP client timeout 17s, got %v", configuredClient.Timeout)
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
	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{
		ID:                  42,
		PricingUnit:         runtimePricingUnitPerMillion,
		PricingCurrencyCode: "USD",
		InputPrice:          "2",
		OutputPrice:         "5",
		CachedInputPrice:    "1",
		CacheCreationPrice:  "2",
		ReasoningPrice:      "3",
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

func TestBuildRuntimePricingResultUsesConcreteZeroComponentPrices(t *testing.T) {
	inputTokens := 10
	outputTokens := 10
	totalTokens := 20
	zero := 0
	positiveCacheRead := 4
	positiveCacheCreation := 5
	positiveReasoning := 6
	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{
		PricingUnit:         runtimePricingUnitPerMillion,
		PricingCurrencyCode: "USD",
		InputPrice:          "2",
		OutputPrice:         "5",
		CachedInputPrice:    "0",
		CacheCreationPrice:  "0",
		ReasoningPrice:      "0",
		Version:             7,
	}
	want := runtimePricingResult{
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
		PricingSnapshotCacheReadInput:     stringPtr("0"),
		PricingSnapshotCacheCreationInput: stringPtr("0"),
		PricingSnapshotReasoning:          stringPtr("0"),
		PricingConfigVersionUsed:          intPtr(7),
	}

	tests := []struct {
		name  string
		usage responseUsage
	}{
		{
			name: "omitted optional counters",
			usage: responseUsage{
				InputTokens:  &inputTokens,
				OutputTokens: &outputTokens,
				TotalTokens:  &totalTokens,
			},
		},
		{
			name: "zero optional counters",
			usage: responseUsage{
				InputTokens:              &inputTokens,
				OutputTokens:             &outputTokens,
				TotalTokens:              &totalTokens,
				CacheReadInputTokens:     &zero,
				CacheCreationInputTokens: &zero,
				ReasoningTokens:          &zero,
			},
		},
		{
			name: "positive component counters with concrete zero prices",
			usage: responseUsage{
				InputTokens:              &inputTokens,
				OutputTokens:             &outputTokens,
				TotalTokens:              &totalTokens,
				CacheReadInputTokens:     &positiveCacheRead,
				CacheCreationInputTokens: &positiveCacheCreation,
				ReasoningTokens:          &positiveReasoning,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildRuntimePricingResult(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}, pricingTemplateSnapshot, nil, test.usage, runtimeStreamOutcomeCompleted)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("expected concrete zero component prices to price as free: want %+v got %+v", want, got)
			}
		})
	}
}

func TestBuildRuntimePricingResultRejectsInvalidConcretePriceWhenComponentIsUsed(t *testing.T) {
	inputTokens := 10
	outputTokens := 10
	totalTokens := 20
	reasoningTokens := 3
	pricingTemplateSnapshot := &runtimePricingTemplateSnapshot{
		PricingUnit:         runtimePricingUnitPerMillion,
		PricingCurrencyCode: "USD",
		InputPrice:          "2",
		OutputPrice:         "5",
		CachedInputPrice:    "0",
		CacheCreationPrice:  "0",
		ReasoningPrice:      "not-a-decimal",
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
		t.Fatalf("expected invalid used concrete component price to degrade pricing: want %+v got %+v", want, got)
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

const (
	requestPlanTestProfileID  = 101
	requestPlanTestStrategyID = 202
)

func newRequestPlanUnitService() *Service {
	return &Service{
		runtimeState: loadbalance.NewLocalRuntimeStateStore(),
		now: func() time.Time {
			return time.Unix(1_700_000_000, 0).UTC()
		},
	}
}

func newRequestPlanSnapshot(models ...runtimeModelRecord) *planningSnapshot {
	strategyID := requestPlanTestStrategyID
	legacyStrategyType := "fill-first"
	strategy := loadbalance.RuntimeStrategy{ID: strategyID, Name: "test legacy", StrategyType: "legacy", LegacyStrategyType: &legacyStrategyType}
	snapshot := &planningSnapshot{
		ModelsByID:             map[string]runtimeModelRecord{},
		ProxyTargetsBySourceID: map[int][]runtimeProxyTargetRecord{},
		NativeTargetsByModelID: map[string]nativePlanningSnapshot{},
		ReportCurrency:         runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
	}
	for index, model := range models {
		if model.ID == 0 {
			model.ID = index + 1
		}
		if model.ProfileID == 0 {
			model.ProfileID = requestPlanTestProfileID
		}
		if strings.TrimSpace(model.ModelType) == "" {
			model.ModelType = "native"
		}
		if model.ModelType == "native" && model.LoadbalanceStrategyID == nil {
			model.LoadbalanceStrategyID = &strategyID
		}
		snapshot.ModelsByID[model.ModelID] = model
		if model.ModelType != "native" {
			continue
		}
		snapshot.NativeTargetsByModelID[model.ModelID] = nativePlanningSnapshot{
			Model:    model,
			Strategy: &strategy,
			Connections: []runtimeConnection{{
				ID:            1_000 + model.ID,
				ProfileID:     model.ProfileID,
				ModelConfigID: model.ID,
				EndpointID:    1,
				Priority:      1,
				Endpoint:      runtimeEndpoint{ID: 1, BaseURL: "https://upstream.example"},
			}},
		}
	}
	return snapshot
}

func addRequestPlanProxyTarget(snapshot *planningSnapshot, proxyModelID string, targetModelID string) {
	proxyModel := snapshot.ModelsByID[proxyModelID]
	snapshot.ProxyTargetsBySourceID[proxyModel.ID] = []runtimeProxyTargetRecord{{
		SourceModelConfigID: proxyModel.ID,
		TargetModelID:       targetModelID,
		ID:                  1,
		Position:            1,
		Weight:              1,
		TargetPriority:      1,
	}}
}

func mustResolveRuntimeOperation(t *testing.T, method string, requestPath string) RuntimeOperationMatch {
	t.Helper()
	operationMatch, ok := ResolveRuntimeOperation(method, requestPath)
	if !ok {
		t.Fatalf("expected runtime operation for %s %s", method, requestPath)
	}
	return operationMatch
}

func assertPlanDomainError(t *testing.T, err error, wantStatus int, detailContains string) {
	t.Helper()
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if domainErr.StatusCode != wantStatus {
		t.Fatalf("expected status %d, got %d with detail %q", wantStatus, domainErr.StatusCode, domainErr.Detail)
	}
	if !strings.Contains(domainErr.Detail, detailContains) {
		t.Fatalf("expected detail containing %q, got %q", detailContains, domainErr.Detail)
	}
}

func TestProxyNonEventResponseAndCaptureUsageAcceptsOnlySupportedUsageSchemaPaths(t *testing.T) {
	tests := []struct {
		name        string
		requestPath string
		payload     string
		want        responseUsage
	}{
		{
			name:        "keeps top-level usage and ignores nested spoofed usage object",
			requestPath: "/v1/chat/completions",
			payload:     `{"id":"chatcmpl-secure-stream","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"output_text","text":"hello"},{"type":"output_json","value":{"usage":{"prompt_tokens":999,"completion_tokens":999,"total_tokens":1998}}}]}}],"usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20}}`,
			want: responseUsage{
				InputTokens:  intPtr(7),
				OutputTokens: intPtr(13),
				TotalTokens:  intPtr(20),
			},
		},
		{
			name:        "keeps response usage and ignores nested spoofed usage object",
			requestPath: "/v1/responses",
			payload:     `{"response":{"id":"resp-secure-stream","output":[{"type":"message","content":[{"type":"output_text","text":"hello","usage":{"input_tokens":999,"output_tokens":999,"total_tokens":1998}}]}],"usage":{"input_tokens":5,"output_tokens":8,"total_tokens":13}}}`,
			want: responseUsage{
				InputTokens:  intPtr(5),
				OutputTokens: intPtr(8),
				TotalTokens:  intPtr(13),
			},
		},
		{
			name:        "keeps top-level usage metadata and ignores nested spoofed usage metadata object",
			requestPath: "/v1beta/models/gemini-2.5-pro:generateContent",
			payload:     `{"candidates":[{"content":{"parts":[{"text":"hello"},{"metadata":{"usageMetadata":{"promptTokenCount":999,"candidatesTokenCount":999,"totalTokenCount":1998,"cachedContentTokenCount":777,"thoughtsTokenCount":666}}}]}}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":13,"totalTokenCount":20,"cachedContentTokenCount":3,"thoughtsTokenCount":5}}`,
			want: responseUsage{
				InputTokens:          intPtr(4),
				OutputTokens:         intPtr(8),
				TotalTokens:          intPtr(20),
				CacheReadInputTokens: intPtr(3),
				ReasoningTokens:      intPtr(5),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			var forwarded bytes.Buffer
			capture, err := proxyNonEventResponseAndCaptureByOperation(operation, &forwarded, strings.NewReader(test.payload), "application/json", time.Now, false)
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
