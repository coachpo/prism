package runtime

import (
	"bytes"
	"context"
	"errors"
	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/coachpo/prism/backend/internal/domain/safediag"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

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
	pricingTemplateSnapshot := runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
		card := snapshot.Cards[pricingkind.RoleStandard]
		card.CachedInputPrice, card.CacheCreationPrice, card.ReasoningPrice = "0", "0", "0"
		snapshot.Cards[pricingkind.RoleStandard] = card
	})
	reportCurrencySnapshot := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}

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

		pricing := buildRuntimePricingResultForTest(reportCurrencySnapshot, pricingTemplateSnapshot, nil, capture.Usage, capture.StreamOutcome)
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

	pricingTemplateSnapshot := runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
		card := snapshot.Cards[pricingkind.RoleStandard]
		card.CachedInputPrice, card.CacheCreationPrice, card.ReasoningPrice = "0", "0", "0"
		snapshot.Cards[pricingkind.RoleStandard] = card
	})
	pricing := buildRuntimePricingResultForTest(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, pricingTemplateSnapshot, nil, capture.Usage, capture.StreamOutcome)
	if !pricing.Billable || !pricing.Priced || pricing.UnpricedReason != nil {
		t.Fatalf("expected observed usage before [DONE] to price normally, got %+v", pricing)
	}
}

func TestProxyEventStreamMergesUsageFromOpenAIChatTerminalChoiceChunk(t *testing.T) {
	tests := []struct {
		name        string
		finishField string
	}{
		{name: "snake_case_finish_reason", finishField: `"finish_reason":"stop"`},
		{name: "camelCase_finishReason", finishField: `"finishReason":"stop"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
			stream := "data: {\"id\":\"chatcmpl-usage\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}],\"usage\":{\"prompt_tokens\":999,\"completion_tokens\":999,\"total_tokens\":1998}}\n\n" +
				"data: {\"id\":\"chatcmpl-usage\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"final\"}," + test.finishField + "}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens_details\":{\"reasoning_tokens\":3}}}\n\n" +
				"data: [DONE]\n\n"
			var forwarded bytes.Buffer
			capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(stream), time.Now, false)
			if err != nil {
				t.Fatalf("proxy SSE with terminal choice usage and [DONE]: %v", err)
			}
			if capture.StreamOutcome != runtimeStreamOutcomeCompleted || capture.CompletedAt == nil || capture.StreamErrorKind != nil || capture.StreamErrorDetail != nil {
				t.Fatalf("expected usage stream ending with [DONE] to complete, got %+v", capture)
			}
			wantUsage := responseUsage{InputTokens: intPtr(6), OutputTokens: intPtr(3), TotalTokens: intPtr(16), CacheReadInputTokens: intPtr(4), ReasoningTokens: intPtr(3)}
			if !reflect.DeepEqual(capture.Usage, wantUsage) {
				t.Fatalf("expected terminal choice usage to win over earlier bogus root usage: want %+v got %+v", wantUsage, capture.Usage)
			}
		})
	}
}

func TestProxyEventStreamIgnoresOpenAIChatUsageWhenAnyChoiceIsNonTerminal(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	stream := "data: {\"id\":\"chatcmpl-usage\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"stop\"},{\"index\":1,\"delta\":{\"content\":\"still-running\"}}],\"usage\":{\"prompt_tokens\":999,\"completion_tokens\":999,\"total_tokens\":1998}}\n\n" +
		"data: [DONE]\n\n"
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(stream), time.Now, false)
	if err != nil {
		t.Fatalf("proxy SSE with mixed choice usage and [DONE]: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted || capture.CompletedAt == nil || capture.StreamErrorKind != nil || capture.StreamErrorDetail != nil {
		t.Fatalf("expected usage stream ending with [DONE] to complete, got %+v", capture)
	}
	if capture.Usage.hasValues() {
		t.Fatalf("expected mixed terminal/non-terminal choice chunk usage to be ignored, got %+v", capture.Usage)
	}
}

func TestInsertedAuditTimesExcludeSuppressedRows(t *testing.T) {
	times := appendAuditTimeIfInserted(nil, time.Unix(0, 0), false)
	times = appendAuditTimeIfInserted(times, time.Unix(1, 0), true)
	if len(times) != 1 || !times[0].Equal(time.Unix(1, 0)) {
		t.Fatalf("expected only the inserted audit timestamp, got %v", times)
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

func TestRuntimeDiagnosticFailureFieldsAlwaysWriteSafeReadableDetail(t *testing.T) {
	tests := []struct {
		name      string
		rowKind   string
		errorCode string
		wantStage string
		wantText  string
	}{
		{name: "routing fallback", rowKind: requestLogRowKindPlanning, wantStage: failureStageRouting, wantText: "routing"},
		{name: "admission reason", rowKind: requestLogRowKindAdmission, errorCode: "admission_exhausted", wantStage: failureStageAdmission, wantText: "admission_exhausted"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestLog := requestLogInsert{RowKind: test.rowKind}
			runtimeErr := &domainError{
				ErrorCode: test.errorCode,
				Detail:    "",
			}
			applyRuntimeDiagnosticFailureFields(&requestLog, runtimeErr)

			if requestLog.ErrorDetail == nil || strings.TrimSpace(*requestLog.ErrorDetail) == "" {
				t.Fatal("expected non-empty error detail")
			}
			if len(*requestLog.ErrorDetail) > safediag.MaxErrorDetailBytes {
				t.Fatalf("expected error detail cap, got %d bytes", len(*requestLog.ErrorDetail))
			}
			if strings.Contains(*requestLog.ErrorDetail, "secret-token") {
				t.Fatalf("expected credential redaction, got %q", *requestLog.ErrorDetail)
			}
			if *requestLog.FailureStage != test.wantStage || !strings.Contains(*requestLog.ErrorDetail, test.wantText) {
				t.Fatalf("expected stage %q and readable reason containing %q, got stage=%q detail=%q", test.wantStage, test.wantText, *requestLog.FailureStage, *requestLog.ErrorDetail)
			}
		})
	}

	requestLog := requestLogInsert{RowKind: requestLogRowKindPlanning}
	applyRuntimeDiagnosticFailureFields(&requestLog, &domainError{Detail: "Authorization: Bearer secret-token; route rejected after policy evaluation"})
	if requestLog.ErrorDetail == nil || strings.Contains(*requestLog.ErrorDetail, "secret-token") || !requestLog.ErrorDetailRedacted {
		t.Fatalf("expected redacted diagnostic detail, got %+v", requestLog.ErrorDetail)
	}
}

func TestSyntheticFailureMetadataScrubPreservesCallerRequestID(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request = request.WithContext(withRuntimeIngressContext(request.Context(), runtimeIngressContext{
		ingressRequestID: newRuntimeUUIDv4(),
		callerRequestID:  "syntheticcalleralpha",
	}))
	requestLog := requestLogInsert{
		OperationName:   mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation.Name,
		CallerUserAgent: stringPtr("matrix-test-agent"),
	}

	applyRuntimePlanningFailureMetadataScrub(&requestLog, request)

	if requestLog.CallerRequestID == nil || *requestLog.CallerRequestID != "syntheticcalleralpha" {
		t.Fatalf("expected synthetic failure caller correlation, got %+v", requestLog.CallerRequestID)
	}
	if requestLog.RequestPath != "/v1/chat/completions" {
		t.Fatalf("expected scrubbed request path, got %q", requestLog.RequestPath)
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

func TestGeminiProxyEventStreamClassifiesReadFailureAfterPartialChunk(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent").Operation
	readFailure := errors.New("gemini upstream read failed after partial event")
	partialStream := []byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}]}\n\n")
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, &errorAfterReader{payload: partialStream, err: readFailure}, time.Now, false)
	if !errors.Is(err, readFailure) {
		t.Fatalf("expected upstream read failure, got %v", err)
	}
	if forwarded.String() != string(partialStream) {
		t.Fatalf("expected partial Gemini chunk to be forwarded before failure, got %q", forwarded.String())
	}
	if capture.StreamOutcome != runtimeStreamOutcomeUpstreamReadError || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindUpstreamReadFailed {
		t.Fatalf("expected Gemini upstream read error capture, got %+v", capture)
	}
	if capture.Usage.hasValues() || capture.CompletedAt != nil {
		t.Fatalf("expected no completed usage after partial read failure, got %+v", capture)
	}
}
