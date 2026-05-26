package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
)

func TestRuntimeResponseTelemetryEnvelopeCharacterizesFinalAttemptRows(t *testing.T) {
	startedAt := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(1500 * time.Millisecond)
	firstMeaningfulPayloadAt := startedAt.Add(225 * time.Millisecond)
	primaryCompletedAt := startedAt.Add(333 * time.Millisecond)
	proxyKeyLastUsedAt := startedAt.Add(2 * time.Second)

	service := &Service{now: func() time.Time { return completedAt.Add(time.Second) }}
	request := newRuntimeTelemetryEnvelopeRequest(proxyKeyLastUsedAt)

	vendorID := 7
	vendorKey := "openai"
	vendorName := "OpenAI"
	resolvedModelID := "target-model"
	primaryEndpointName := "primary endpoint"
	secondaryEndpointName := "secondary endpoint"
	primaryConnection := runtimeConnection{
		ID:       11,
		Endpoint: runtimeEndpoint{ID: 101, BaseURL: "https://primary.example", Name: &primaryEndpointName},
	}
	secondaryConnection := runtimeConnection{
		ID:                      22,
		PricingTemplateSnapshot: &runtimePricingTemplateSnapshot{PricingUnit: runtimePricingUnitPerMillion, PricingCurrencyCode: "USD", InputPrice: "2", OutputPrice: "5", CachedInputPrice: "0", CacheCreationPrice: "0", ReasoningPrice: "0", Version: 3},
		Endpoint:                runtimeEndpoint{ID: 202, BaseURL: "https://secondary.example", Name: &secondaryEndpointName},
	}
	plan := requestPlan{
		ProfileID:                   42,
		RequestedModelID:            "public-model",
		ResolvedTargetModelID:       &resolvedModelID,
		RequestedVendorID:           &vendorID,
		RequestedVendorKey:          &vendorKey,
		RequestedVendorName:         &vendorName,
		APIFamily:                   "openai",
		RuntimeOperation:            RuntimeOperation{Name: " openai.chat_completions "},
		AuditEnabledAtRequest:       true,
		AuditCaptureBodiesAtRequest: true,
		ReportCurrencySnapshot:      runtimeReportCurrencySnapshot{Code: " USD ", Symbol: " $ "},
		UpstreamBody:                []byte(`{"model":"target-model","messages":[{"role":"user","content":"hello"}]}`),
	}
	usage := responseUsage{
		InputTokens:          intPtr(10),
		OutputTokens:         intPtr(6),
		TotalTokens:          intPtr(16),
		CacheReadInputTokens: intPtr(4),
		ReasoningTokens:      intPtr(3),
	}
	result := executionResult{
		Response:   &http.Response{StatusCode: http.StatusOK},
		Connection: secondaryConnection,
		Attempts: []executionAttempt{
			{
				Connection:      primaryConnection,
				RequestURL:      "https://primary.example/v1/chat/completions",
				RequestHeaders:  map[string]string{"User-Agent": "primary-upstream"},
				ResponseHeaders: http.Header{"Request-Id": []string{"primary-correlation"}},
				StatusCode:      http.StatusServiceUnavailable,
				ResponseTimeMS:  333,
				CompletedAt:     primaryCompletedAt,
			},
			{
				Connection:      secondaryConnection,
				RequestURL:      "https://secondary.example/v1/chat/completions",
				RequestHeaders:  map[string]string{"User-Agent": "secondary-upstream"},
				ResponseHeaders: http.Header{"X-Request-Id": []string{"secondary-correlation"}},
				StatusCode:      http.StatusOK,
				ResponseTimeMS:  88,
				CompletedAt:     startedAt.Add(time.Second),
			},
		},
	}
	capture := runtimeResponseCapture{
		Usage:                    usage,
		AuditBody:                []byte(`{"id":"chatcmpl-final","usage":{"total_tokens":16}}`),
		FirstMeaningfulPayloadAt: &firstMeaningfulPayloadAt,
		CompletedAt:              &completedAt,
		StreamOutcome:            runtimeStreamOutcomeCompleted,
	}

	envelope := service.buildRuntimeTelemetryEnvelope(plan, result, request, startedAt, capture)

	if len(envelope.RequestLogs) != 2 || len(envelope.AuditLogs) != 2 {
		t.Fatalf("expected two request and audit rows, got request=%d audit=%d", len(envelope.RequestLogs), len(envelope.AuditLogs))
	}
	firstLog := envelope.RequestLogs[0]
	finalLog := envelope.RequestLogs[1]
	if firstLog.AttemptNumber != 1 || firstLog.ConnectionID != primaryConnection.ID || firstLog.EndpointID != primaryConnection.Endpoint.ID || firstLog.StatusCode != http.StatusServiceUnavailable || firstLog.ResponseTimeMS != 333 || !firstLog.CreatedAt.Equal(primaryCompletedAt) {
		t.Fatalf("unexpected first attempt request log: %+v", firstLog)
	}
	if firstLog.InputTokens != nil || firstLog.OutputTokens != nil || firstLog.TotalTokens != nil || firstLog.TotalCostUserCurrencyMicros != nil || firstLog.CompletionDurationMS != nil || firstLog.TTFTMS != nil || firstLog.StreamErrorKind != nil || firstLog.StreamErrorDetail != nil {
		t.Fatalf("expected non-final request log to omit final usage, pricing, and timing attribution, got %+v", firstLog)
	}
	assertRuntimeBoolPtr(t, firstLog.BillableFlag, false, "first billable")
	assertRuntimeBoolPtr(t, firstLog.PricedFlag, false, "first priced")
	if firstLog.UnpricedReason != nil || firstLog.StreamOutcome != runtimeStreamOutcomeNotStreaming || !firstLog.IsStream {
		t.Fatalf("expected non-final stream row to keep attempt-local failure state, got %+v", firstLog)
	}
	if finalLog.AttemptNumber != 2 || finalLog.ConnectionID != secondaryConnection.ID || finalLog.EndpointID != secondaryConnection.Endpoint.ID || finalLog.StatusCode != http.StatusOK || finalLog.ResponseTimeMS != 1500 || !finalLog.CreatedAt.Equal(completedAt) {
		t.Fatalf("unexpected final attempt request log: %+v", finalLog)
	}
	assertRuntimeIntPtr(t, finalLog.InputTokens, 10, "final input tokens")
	assertRuntimeIntPtr(t, finalLog.OutputTokens, 6, "final output tokens")
	assertRuntimeIntPtr(t, finalLog.TotalTokens, 16, "final total tokens")
	assertRuntimeIntPtr(t, finalLog.CacheReadInputTokens, 4, "final cache-read tokens")
	assertRuntimeIntPtr(t, finalLog.ReasoningTokens, 3, "final reasoning tokens")
	assertRuntimeBoolPtr(t, finalLog.BillableFlag, true, "final billable")
	assertRuntimeBoolPtr(t, finalLog.PricedFlag, true, "final priced")
	assertRuntimeInt64Ptr(t, finalLog.TotalCostUserCurrencyMicros, 50, "final user-currency total cost")
	assertRuntimeIntPtr(t, finalLog.PricingConfigVersionUsed, 3, "final pricing version")
	assertRuntimeIntPtr(t, finalLog.CompletionDurationMS, 1500, "final completion duration")
	assertRuntimeIntPtr(t, finalLog.TTFTMS, 225, "final ttft")
	assertRuntimeStringPtr(t, finalLog.ProviderCorrelationID, "secondary-correlation", "final correlation")
	assertRuntimeStringPtr(t, finalLog.CallerUserAgent, "telemetry-characterization-client", "caller user agent")
	assertRuntimeStringPtr(t, finalLog.UpstreamUserAgent, "secondary-upstream", "upstream user agent")
	if finalLog.OperationName != "openai.chat_completions" || finalLog.RequestPath != "/v1/chat/completions" || finalLog.StreamOutcome != runtimeStreamOutcomeCompleted || finalLog.UnpricedReason != nil {
		t.Fatalf("expected final request log metadata to stay canonical, got %+v", finalLog)
	}

	firstAudit := envelope.AuditLogs[0]
	finalAudit := envelope.AuditLogs[1]
	assertRuntimeStringPtr(t, firstAudit.RequestBody, string(plan.UpstreamBody), "first audit request body")
	if !firstAudit.RequestBodyStored || firstAudit.ResponseBody != nil || firstAudit.ResponseBodyStored || firstAudit.DurationMS != 333 || !firstAudit.IsStream || !firstAudit.CreatedAt.Equal(primaryCompletedAt) {
		t.Fatalf("expected first audit row to keep request body only and attempt timing, got %+v", firstAudit)
	}
	assertRuntimeStringPtr(t, finalAudit.RequestBody, string(plan.UpstreamBody), "final audit request body")
	assertRuntimeStringPtr(t, finalAudit.ResponseBody, string(capture.AuditBody), "final audit response body")
	if !finalAudit.RequestBodyStored || !finalAudit.ResponseBodyStored || finalAudit.DurationMS != 1500 || !finalAudit.CreatedAt.Equal(completedAt) || !finalAudit.AuditCaptureBodiesAtRequest {
		t.Fatalf("expected final audit row to store gated response body and final timing, got %+v", finalAudit)
	}

	usageEvent := envelope.UsageEvent
	if usageEvent.AttemptCount != 2 || usageEvent.ConnectionID != secondaryConnection.ID || usageEvent.EndpointID != secondaryConnection.Endpoint.ID || usageEvent.StatusCode != http.StatusOK || usageEvent.OperationName != "openai.chat_completions" || usageEvent.RequestPath != "/v1/chat/completions" {
		t.Fatalf("unexpected usage event identity: %+v", usageEvent)
	}
	assertRuntimeIntPtr(t, usageEvent.InputTokens, 10, "usage event input tokens")
	assertRuntimeIntPtr(t, usageEvent.OutputTokens, 6, "usage event output tokens")
	assertRuntimeInt64Ptr(t, usageEvent.TotalCostUserCurrencyMicros, 50, "usage event cost")
	assertRuntimeBoolPtr(t, usageEvent.BillableFlag, true, "usage event billable")
	assertRuntimeBoolPtr(t, usageEvent.PricedFlag, true, "usage event priced")
	assertRuntimeIntPtr(t, usageEvent.ResponseTimeMS, 1500, "usage event response time")
	assertRuntimeIntPtr(t, usageEvent.CompletionDurationMS, 1500, "usage event completion duration")
	assertRuntimeIntPtr(t, usageEvent.TTFTMS, 225, "usage event ttft")
	if usageEvent.StreamOutcome != runtimeStreamOutcomeCompleted || usageEvent.UnpricedReason != nil || !usageEvent.CreatedAt.Equal(completedAt) {
		t.Fatalf("expected final usage event telemetry, got %+v", usageEvent)
	}
	if envelope.ProxyKeyUsage == nil || envelope.ProxyKeyUsage.KeyID != 77 || envelope.ProxyKeyUsage.LastUsedIP != "198.51.100.10" || !envelope.ProxyKeyUsage.LastUsedAt.Equal(proxyKeyLastUsedAt) {
		t.Fatalf("expected proxy key usage signal to survive durable envelope construction, got %+v", envelope.ProxyKeyUsage)
	}
}

func newRuntimeTelemetryEnvelopeRequest(proxyKeyLastUsedAt time.Time) *http.Request {
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "runtime-envelope-characterization")
	ctx = requestcontext.WithRuntimeProxyKey(ctx, requestcontext.RuntimeProxyKeySnapshot{
		ID:         77,
		Name:       "runtime telemetry key",
		LastUsedAt: proxyKeyLastUsedAt,
		LastUsedIP: " 198.51.100.10 ",
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?characterization=true", nil).WithContext(ctx)
	request.Header.Set("User-Agent", " telemetry-characterization-client ")
	return request
}

func assertRuntimeIntPtr(t *testing.T, got *int, want int, label string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("expected %s=%d, got %+v", label, want, got)
	}
}

func assertRuntimeInt64Ptr(t *testing.T, got *int64, want int64, label string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("expected %s=%d, got %+v", label, want, got)
	}
}

func assertRuntimeBoolPtr(t *testing.T, got *bool, want bool, label string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("expected %s=%v, got %+v", label, want, got)
	}
}

func assertRuntimeStringPtr(t *testing.T, got *string, want string, label string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("expected %s=%q, got %+v", label, want, got)
	}
}
