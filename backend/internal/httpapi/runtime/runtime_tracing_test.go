package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type runtimeTracingRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper runtimeTracingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func TestRuntimeTracingCoversStreamingAndNonStreaming(t *testing.T) {
	recorder := installRuntimeTraceTestProvider(t)
	client := &http.Client{Transport: runtimeTracingRoundTripper(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, ":streamGenerateContent") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\ndata: {\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":2,\"totalTokenCount\":3}}\n\n")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`)),
		}, nil
	})}
	service := newRuntimeTracingExecutionService(client)

	service.handleStreamingProxy(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"missing-cache","messages":[]}`)))
	executeRuntimeTracingPlan(t, service, client, "/v1/chat/completions", "openai", "trace-openai-public", `{"model":"trace-openai-public","messages":[{"role":"user","content":"hello"}]}`)
	executeRuntimeTracingPlan(t, service, client, "/v1beta/models/trace-gemini-public:streamGenerateContent", "gemini", "trace-gemini-public", `{"contents":[{"parts":[{"text":"hello"}]}]}`)

	spans := recorder.Ended()
	for _, name := range []string{
		"runtime.request",
		"runtime.operation.resolve",
		"runtime.request.plan",
		"runtime.request.execute",
		"runtime.connection.attempt",
		"runtime.provider.http",
		"runtime.response.handle",
		"runtime.activity.record",
		"runtime.side_effect.submit",
	} {
		if !runtimeTraceSpanExists(spans, name) {
			t.Fatalf("expected runtime tracing span %q; got %v", name, runtimeTraceSpanNames(spans))
		}
	}
	assertRuntimeTraceSpanWithAttribute(t, spans, "runtime.response.handle", runtimeTraceAttrStreamOutcome, runtimeStreamOutcomeCompleted)
	assertRuntimeTraceSpanWithAttribute(t, spans, "runtime.response.handle", runtimeTraceAttrStreamOutcome, runtimeStreamOutcomeNotStreaming)
}

func TestRuntimeTracingPropagatesToOutboxAndFeedback(t *testing.T) {
	recorder := installRuntimeTraceTestProvider(t)
	rootCtx, rootSpan := otel.Tracer(runtimeMetricScopeName).Start(context.Background(), "runtime.trace.root")
	terminalFailures := make(chan error, 1)
	sideEffectScheduler := backgroundSchedulerForRuntimeTracing(t)
	manager := NewRuntimeSideEffectManager(nil, RuntimeSideEffectOptions{
		AttemptTimeout:  25 * time.Millisecond,
		ShutdownTimeout: time.Second,
		RetryDelay:      time.Millisecond,
		MaxAttempts:     2,
		Hooks: &RuntimeSideEffectHooks{TerminalFailure: func(_ RuntimeActivityIntent, err error) {
			terminalFailures <- err
		}},
	})
	if err := manager.RegisterBackgroundWorker(sideEffectScheduler); err != nil {
		t.Fatalf("register side-effect worker: %v", err)
	}
	if err := sideEffectScheduler.Start(context.Background()); err != nil {
		t.Fatalf("start side-effect scheduler: %v", err)
	}
	defer func() { _ = sideEffectScheduler.Stop(context.Background(), time.Now().Add(time.Second)) }()

	intent := validRuntimeActivityIntent()
	intent.Envelope.UsageEvent.OperationName = "openai.chat_completions"
	if result := manager.SubmitRuntimeActivityContext(rootCtx, intent); result.Status != RuntimeSideEffectAccepted {
		t.Fatalf("expected side-effect intent accepted, got %+v", result)
	}
	select {
	case <-terminalFailures:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for side-effect terminal failure")
	}

	writeResults := make(chan RuntimeFeedbackWriteResult, 1)
	feedbackScheduler := backgroundSchedulerForRuntimeTracing(t)
	pipeline := newRuntimeFeedbackPipeline(nil, nil, nil, RuntimeFeedbackPipelineOptions{
		QueueCapacity: 1,
		WorkerCount:   1,
		WriteTimeout:  25 * time.Millisecond,
		Hooks: &RuntimeFeedbackPipelineHooks{AfterWrite: func(result RuntimeFeedbackWriteResult) {
			writeResults <- result
		}},
	})
	if err := pipeline.RegisterBackgroundWorker(feedbackScheduler); err != nil {
		t.Fatalf("register feedback worker: %v", err)
	}
	if err := feedbackScheduler.Start(context.Background()); err != nil {
		t.Fatalf("start feedback scheduler: %v", err)
	}
	defer func() { _ = feedbackScheduler.Stop(context.Background(), time.Now().Add(time.Second)) }()
	feedbackEvent := runtimeFeedbackEvent{Kind: runtimeFeedbackAdmissionRejected, ProfileID: 1, ConnectionID: 2, ModelConfigID: 3, APIFamily: "openai", ObservedAt: time.Now().UTC()}
	if result := pipeline.TryEnqueueContext(rootCtx, feedbackEvent); result.Status != RuntimeFeedbackAccepted {
		t.Fatalf("expected feedback accepted, got %+v", result)
	}
	select {
	case <-writeResults:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for feedback write")
	}
	rootSpan.End()

	spans := waitForRuntimeTraceSpans(t, recorder, "runtime.side_effect.submit", "runtime.side_effect.commit", "runtime.outbox.enqueue", "runtime.feedback.enqueue", "runtime.feedback.write", "runtime.trace.root")
	root := runtimeTraceSpanByName(spans, "runtime.trace.root")
	if root == nil {
		t.Fatalf("expected root span; got %v", runtimeTraceSpanNames(spans))
	}
	for _, name := range []string{"runtime.side_effect.submit", "runtime.side_effect.commit", "runtime.outbox.enqueue", "runtime.feedback.enqueue", "runtime.feedback.write"} {
		span := runtimeTraceSpanByName(spans, name)
		if span == nil {
			t.Fatalf("expected propagated async span %q; got %v", name, runtimeTraceSpanNames(spans))
		}
		if span.SpanContext().TraceID() != root.SpanContext().TraceID() {
			t.Fatalf("expected %q to preserve trace id %s, got %s", name, root.SpanContext().TraceID(), span.SpanContext().TraceID())
		}
	}
}

func TestRuntimeTracingRedactsSensitiveAttributes(t *testing.T) {
	recorder := installRuntimeTraceTestProvider(t)
	client := &http.Client{Transport: runtimeTracingRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
		}, nil
	})}
	service := newRuntimeTracingExecutionService(client)
	executeRuntimeTracingPlan(t, service, client, "/v1/chat/completions", "openai", "secret-model-id", `{"model":"secret-model-id","messages":[{"role":"user","content":"prompt body leaked super-secret"}]}`)
	_, synthetic := startRuntimeSpan(context.Background(), "runtime.redaction.synthetic",
		runtimeTraceEnvelopeAttributes(runtimeTelemetryEnvelope{UsageEvent: usageEventInsert{OperationName: "https://upstream.example/v1/chat/completions?api_key=secret", APIFamily: "raw-url", StatusCode: 599, StreamOutcome: "prompt body leaked", UpstreamOperationName: stringPtr("https://upstream.example/v1/chat/completions?api_key=secret"), OperationTranslationMode: stringPtr("super-secret"), UpstreamRequestPath: stringPtr("/v1beta/models/secret-model-id:generateContent")}})...,
	)
	synthetic.SetAttributes(runtimeTraceFeedbackAttributes(runtimeFeedbackEvent{Kind: runtimeFeedbackKind("provider error text"), APIFamily: "Bearer secret-token"})...)
	synthetic.End()

	for _, span := range recorder.Ended() {
		for _, attr := range span.Attributes() {
			value := attr.Value.AsString()
			for _, fragment := range []string{
				"secret-model-id",
				"prompt body leaked",
				"super-secret",
				"api_key=secret",
				"upstream.example",
				"provider error text",
				"secret-token",
			} {
				if strings.Contains(value, fragment) {
					t.Fatalf("span %q leaked sensitive fragment %q in %s=%q", span.Name(), fragment, attr.Key, value)
				}
			}
		}
	}
}

func TestRuntimeTracingTranslationAttributes(t *testing.T) {
	translatedContextRouting := &runtimeContextRoutingDecision{
		Policy:                            "cheapest_eligible_context",
		SelectedTerminalTargetID:          intPtr(34),
		SelectedContextBand:               stringPtr(runtimeContextBandPreferred),
		SelectedUsableContextWindowTokens: intPtr(8192),
		PlannerTrace: &runtimePlannerTraceDecision{
			PlannerVersion:         runtimePlannerTraceVersion,
			PlannerMode:            string(config.RuntimeRoutingPlannerModeShadow),
			Decision:               runtimePlannerTraceDecisionSelected,
			Policy:                 "cheapest_eligible_context",
			SelectedTierPriority:   intPtr(4),
			SkippedTerminalTargets: 2,
			ShadowComparisonResult: &runtimeShadowComparisonResult{Result: runtimeShadowComparisonResultMismatch, MismatchReasons: []string{"resolved_model", "selected_connection"}},
		},
	}
	translatedPlan := requestPlan{
		APIFamily:          "openai",
		RuntimeOperation:   RuntimeOperation{Name: "openai.responses", APIFamily: "openai", PathTemplate: "/v1/responses"},
		IsStreamingRequest: false,
		ContextRouting:     translatedContextRouting,
		TerminalAttempts: []runtimeTerminalAttempt{{
			Connection:           runtimeConnection{ID: 34},
			TranslationMode:      TranslationModeOpenAIResponsesToChatCompletions,
			EffectiveRequestPath: "/v1/chat/completions",
		}},
	}
	translatedAttrs := attributesByKey(runtimeTracePlanAttributes(translatedPlan))
	if translatedAttrs[runtimeTraceAttrOperationName].AsString() != "openai.responses" || translatedAttrs[runtimeTraceAttrUpstreamOperationName].AsString() != "openai.chat_completions" || translatedAttrs[runtimeTraceAttrOperationTranslationMode].AsString() != string(TranslationModeOpenAIResponsesToChatCompletions) || translatedAttrs[runtimeTraceAttrUpstreamRequestPath].AsString() != "/v1/chat/completions" || translatedAttrs[runtimeTraceAttrPreferredContextBand].AsString() != runtimeContextBandPreferred || translatedAttrs[runtimeTraceAttrSelectedTerminalTargetID].AsInt64() != 34 || translatedAttrs[runtimeTraceAttrPlannerVersion].AsString() != runtimePlannerTraceVersion || translatedAttrs[runtimeTraceAttrPlannerMode].AsString() != string(config.RuntimeRoutingPlannerModeShadow) || translatedAttrs[runtimeTraceAttrPlannerDecision].AsString() != runtimePlannerTraceDecisionSelected || translatedAttrs[runtimeTraceAttrPlannerPolicy].AsString() != "cheapest_eligible_context" || translatedAttrs[runtimeTraceAttrPlannerSelectedTier].AsInt64() != 4 || translatedAttrs[runtimeTraceAttrPlannerSkippedTargets].AsInt64() != 2 || translatedAttrs[runtimeTraceAttrShadowComparisonResult].AsString() != runtimeShadowComparisonResultMismatch || translatedAttrs[runtimeTraceAttrShadowMismatchReasons].AsString() != "resolved_model,selected_connection" {
		t.Fatalf("expected translated plan trace attributes, got %+v", translatedAttrs)
	}

	translatedEnvelopeAttrs := attributesByKey(runtimeTraceEnvelopeAttributes(runtimeTelemetryEnvelope{UsageEvent: usageEventInsert{
		OperationName:            "openai.responses",
		UpstreamOperationName:    stringPtr("openai.chat_completions"),
		OperationTranslationMode: stringPtr(string(TranslationModeOpenAIResponsesToChatCompletions)),
		UpstreamRequestPath:      stringPtr("/v1/chat/completions"),
		APIFamily:                "openai",
		StatusCode:               http.StatusOK,
		StreamOutcome:            runtimeStreamOutcomeNotStreaming,
		ContextRouting:           translatedContextRouting,
	}}))
	if translatedEnvelopeAttrs[runtimeTraceAttrOperationName].AsString() != "openai.responses" || translatedEnvelopeAttrs[runtimeTraceAttrUpstreamOperationName].AsString() != "openai.chat_completions" || translatedEnvelopeAttrs[runtimeTraceAttrOperationTranslationMode].AsString() != string(TranslationModeOpenAIResponsesToChatCompletions) || translatedEnvelopeAttrs[runtimeTraceAttrUpstreamRequestPath].AsString() != "/v1/chat/completions" || translatedEnvelopeAttrs[runtimeTraceAttrPreferredContextBand].AsString() != runtimeContextBandPreferred || translatedEnvelopeAttrs[runtimeTraceAttrSelectedTerminalTargetID].AsInt64() != 34 {
		t.Fatalf("expected translated envelope trace attributes, got %+v", translatedEnvelopeAttrs)
	}

	nativePlan := requestPlan{
		APIFamily:          "openai",
		RuntimeOperation:   RuntimeOperation{Name: "openai.chat_completions", APIFamily: "openai", PathTemplate: "/v1/chat/completions"},
		IsStreamingRequest: false,
		TerminalAttempts: []runtimeTerminalAttempt{{
			Connection:           runtimeConnection{ID: 35},
			TranslationMode:      TranslationModeNone,
			EffectiveRequestPath: "/v1/chat/completions",
		}},
	}
	nativeAttrs := attributesByKey(runtimeTracePlanAttributes(nativePlan))
	if nativeAttrs[runtimeTraceAttrOperationName].AsString() != "openai.chat_completions" || nativeAttrs[runtimeTraceAttrUpstreamOperationName].AsString() != "openai.chat_completions" || nativeAttrs[runtimeTraceAttrOperationTranslationMode].AsString() != string(TranslationModeNone) || nativeAttrs[runtimeTraceAttrUpstreamRequestPath].AsString() != "/v1/chat/completions" {
		t.Fatalf("expected native plan trace attributes, got %+v", nativeAttrs)
	}

	planningFailureAttrs := attributesByKey(runtimeTracePlanningFailureAttributes(runtimePlanningFailureTelemetry{
		APIFamily:                "openai",
		RuntimeOperation:         RuntimeOperation{Name: "openai.responses", APIFamily: "openai", PathTemplate: "/v1/responses"},
		UpstreamOperationName:    stringPtr("openai.chat_completions"),
		UpstreamRequestPath:      stringPtr("/v1/chat/completions"),
		OperationTranslationMode: stringPtr(string(TranslationModeOpenAIResponsesToChatCompletions)),
		IsStreamingRequest:       false,
		ContextRouting:           translatedContextRouting,
	}))
	if planningFailureAttrs[runtimeTraceAttrOperationName].AsString() != "openai.responses" || planningFailureAttrs[runtimeTraceAttrUpstreamOperationName].AsString() != "openai.chat_completions" || planningFailureAttrs[runtimeTraceAttrOperationTranslationMode].AsString() != string(TranslationModeOpenAIResponsesToChatCompletions) || planningFailureAttrs[runtimeTraceAttrUpstreamRequestPath].AsString() != "/v1/chat/completions" || planningFailureAttrs[runtimeTraceAttrPreferredContextBand].AsString() != runtimeContextBandPreferred || planningFailureAttrs[runtimeTraceAttrSelectedTerminalTargetID].AsInt64() != 34 {
		t.Fatalf("expected translated planning-failure trace attributes, got %+v", planningFailureAttrs)
	}
}

func installRuntimeTraceTestProvider(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
	return recorder
}

func newRuntimeTracingExecutionService(client *http.Client) *Service {
	return &Service{
		httpClient:               client,
		staticRuntimeProxyConfig: RuntimeProxyConfigSnapshot{HTTPClient: client},
		runtimeState:             loadbalance.NewLocalRuntimeStateStore(),
		runtimeMetrics:           newRuntimeMetrics(),
		runtimeSideEffects:       NewRuntimeSideEffectManager(nil, RuntimeSideEffectOptions{}),
		now: func() time.Time {
			return time.Unix(1_700_000_000, 0).UTC()
		},
	}
}

func executeRuntimeTracingPlan(t *testing.T, service *Service, client *http.Client, path string, apiFamily string, modelID string, rawBody string) {
	t.Helper()
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, path)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(rawBody))
	request.Header.Set("Content-Type", "application/json")
	plan, err := service.buildRequestPlanFromSnapshot(request, []byte(rawBody), RuntimeProxyConfigSnapshot{HTTPClient: client}, operationMatch, requestPlanTestProfileID, runtimeTracingSnapshot(apiFamily, modelID))
	if err != nil {
		t.Fatalf("build tracing plan: %v", err)
	}
	response := httptest.NewRecorder()
	service.handlePlannedProxy(response, request, plan, newBufferedRuntimeRequestBodySource(plan.UpstreamBody))
	if response.Code != http.StatusOK {
		t.Fatalf("expected runtime response 200 for %s, got %d body=%s", path, response.Code, response.Body.String())
	}
}

func runtimeTracingSnapshot(apiFamily string, modelID string) *planningSnapshot {
	model := runtimeModelRecord{ID: 1, ProfileID: requestPlanTestProfileID, APIFamily: apiFamily, ModelID: modelID}
	snapshot := newRequestPlanSnapshot(model)
	for connectionID, connection := range snapshot.ConnectionsByID {
		connection.Endpoint.BaseURL = "https://upstream.example/base"
		connection.UpstreamAuth = &runtimeConnectionUpstreamAuthSnapshot{
			AuthHeader:            "Authorization",
			AuthValue:             "Bearer redacted",
			ControlledHeaderNames: map[string]struct{}{"authorization": {}},
		}
		snapshot.ConnectionsByID[connectionID] = connection
	}
	return snapshot
}

func runtimeTraceSpanExists(spans []sdktrace.ReadOnlySpan, name string) bool {
	return runtimeTraceSpanByName(spans, name) != nil
}

func waitForRuntimeTraceSpans(t *testing.T, recorder *tracetest.SpanRecorder, names ...string) []sdktrace.ReadOnlySpan {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		spans := recorder.Ended()
		if runtimeTraceSpansInclude(spans, names) || time.Now().After(deadline) {
			return spans
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runtimeTraceSpansInclude(spans []sdktrace.ReadOnlySpan, names []string) bool {
	for _, name := range names {
		if !runtimeTraceSpanExists(spans, name) {
			return false
		}
	}
	return true
}

func runtimeTraceSpanByName(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, span := range spans {
		if span.Name() == name {
			return span
		}
	}
	return nil
}

func runtimeTraceSpanNames(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name())
	}
	return names
}

func assertRuntimeTraceSpanWithAttribute(t *testing.T, spans []sdktrace.ReadOnlySpan, spanName string, key string, value string) {
	t.Helper()
	for _, span := range spans {
		if span.Name() != spanName {
			continue
		}
		for _, attr := range span.Attributes() {
			if string(attr.Key) == key && attr.Value.AsString() == value {
				return
			}
		}
	}
	t.Fatalf("expected span %q with %s=%q; got %v", spanName, key, value, runtimeTraceSpanAttributeSummary(spans))
}

func runtimeTraceSpanAttributeSummary(spans []sdktrace.ReadOnlySpan) map[string][]attribute.KeyValue {
	summary := make(map[string][]attribute.KeyValue, len(spans))
	for _, span := range spans {
		summary[span.Name()] = append(summary[span.Name()], span.Attributes()...)
	}
	return summary
}

func attributesByKey(attrs []attribute.KeyValue) map[string]attribute.Value {
	items := make(map[string]attribute.Value, len(attrs))
	for _, attr := range attrs {
		items[string(attr.Key)] = attr.Value
	}
	return items
}

func backgroundSchedulerForRuntimeTracing(t *testing.T) *background.Scheduler {
	t.Helper()
	return background.NewScheduler(background.Config{})
}
