package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
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
		Response:                    &http.Response{StatusCode: http.StatusOK},
		Connection:                  secondaryConnection,
		ResolvedTargetModelID:       &resolvedModelID,
		AuditEnabledAtRequest:       true,
		AuditCaptureBodiesAtRequest: true,
		Attempts: []executionAttempt{
			{
				Connection:                  primaryConnection,
				ResolvedTargetModelID:       resolvedModelID,
				RequestURL:                  "https://primary.example/v1/chat/completions",
				RequestHeaders:              map[string]string{"User-Agent": "primary-upstream"},
				RequestBody:                 plan.UpstreamBody,
				ResponseHeaders:             http.Header{"Request-Id": []string{"primary-correlation"}},
				StatusCode:                  http.StatusServiceUnavailable,
				ResponseTimeMS:              333,
				CompletedAt:                 primaryCompletedAt,
				AuditEnabledAtRequest:       true,
				AuditCaptureBodiesAtRequest: true,
			},
			{
				Connection:                  secondaryConnection,
				ResolvedTargetModelID:       resolvedModelID,
				RequestURL:                  "https://secondary.example/v1/chat/completions",
				RequestHeaders:              map[string]string{"User-Agent": "secondary-upstream"},
				RequestBody:                 plan.UpstreamBody,
				ResponseHeaders:             http.Header{"X-Request-Id": []string{"secondary-correlation"}},
				StatusCode:                  http.StatusOK,
				ResponseTimeMS:              88,
				CompletedAt:                 startedAt.Add(time.Second),
				AuditEnabledAtRequest:       true,
				AuditCaptureBodiesAtRequest: true,
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
	if firstLog.AttemptNumber != 1 || firstLog.StatusCode != http.StatusServiceUnavailable || firstLog.ResponseTimeMS != 333 || !firstLog.CreatedAt.Equal(primaryCompletedAt) {
		t.Fatalf("unexpected first attempt request log: %+v", firstLog)
	}
	assertRuntimeIntPtr(t, firstLog.ConnectionID, primaryConnection.ID, "first attempt connection id")
	assertRuntimeIntPtr(t, firstLog.EndpointID, primaryConnection.Endpoint.ID, "first attempt endpoint id")
	if firstLog.InputTokens != nil || firstLog.OutputTokens != nil || firstLog.TotalTokens != nil || firstLog.TotalCostUserCurrencyMicros != nil || firstLog.CompletionDurationMS != nil || firstLog.TTFTMS != nil || firstLog.StreamErrorKind != nil || firstLog.StreamErrorDetail != nil {
		t.Fatalf("expected non-final request log to omit final usage, pricing, and timing attribution, got %+v", firstLog)
	}
	assertRuntimeBoolPtr(t, firstLog.BillableFlag, false, "first billable")
	assertRuntimeBoolPtr(t, firstLog.PricedFlag, false, "first priced")
	if firstLog.UnpricedReason != nil || firstLog.StreamOutcome != runtimeStreamOutcomeNotStreaming || !firstLog.IsStream {
		t.Fatalf("expected non-final stream row to keep attempt-local failure state, got %+v", firstLog)
	}
	if finalLog.AttemptNumber != 2 || finalLog.StatusCode != http.StatusOK || finalLog.ResponseTimeMS != 1500 || !finalLog.CreatedAt.Equal(completedAt) {
		t.Fatalf("unexpected final attempt request log: %+v", finalLog)
	}
	assertRuntimeIntPtr(t, finalLog.ConnectionID, secondaryConnection.ID, "final attempt connection id")
	assertRuntimeIntPtr(t, finalLog.EndpointID, secondaryConnection.Endpoint.ID, "final attempt endpoint id")
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
	if usageEvent.AttemptCount != 2 || usageEvent.StatusCode != http.StatusOK || usageEvent.OperationName != "openai.chat_completions" || usageEvent.RequestPath != "/v1/chat/completions" {
		t.Fatalf("unexpected usage event identity: %+v", usageEvent)
	}
	assertRuntimeIntPtr(t, usageEvent.ConnectionID, secondaryConnection.ID, "usage event connection id")
	assertRuntimeIntPtr(t, usageEvent.EndpointID, secondaryConnection.Endpoint.ID, "usage event endpoint id")
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
	if len(envelope.AccountingAttempts) != 2 {
		t.Fatalf("expected two accounting attempts, got %d", len(envelope.AccountingAttempts))
	}
	accountingEvent := envelope.AccountingEvent
	if accountingEvent.RequestID != usageEvent.IngressRequestID || accountingEvent.OperationName != usageEvent.OperationName || accountingEvent.RequestedModelID != usageEvent.ModelID || accountingEvent.StatusCode != usageEvent.StatusCode || !accountingEvent.Final || accountingEvent.AttemptNumber != usageEvent.AttemptCount {
		t.Fatalf("expected final accounting event to mirror usage provenance, got %+v usage=%+v", accountingEvent, usageEvent)
	}
	assertRuntimeStringPtr(t, accountingEvent.EffectiveModelID, resolvedModelID, "accounting effective model")
	assertRuntimeIntPtr(t, accountingEvent.ConnectionID, secondaryConnection.ID, "accounting final connection")
	assertRuntimeIntPtr(t, accountingEvent.EndpointID, secondaryConnection.Endpoint.ID, "accounting final endpoint")
	assertRuntimeIntPtr(t, accountingEvent.PricingConfigVersionUsed, 3, "accounting pricing version")
	if accountingEvent.RouteReason != gatewaycore.RouteReasonDirectMatch || accountingEvent.UsageSource != gatewaycore.UsageSourceProviderStreamTerminal || accountingEvent.StreamOutcome != runtimeStreamOutcomeCompleted || !accountingEvent.AuditEnabled || !accountingEvent.AuditCaptureBodies || !accountingEvent.ObservedAt.Equal(completedAt) {
		t.Fatalf("unexpected final accounting provenance: %+v", accountingEvent)
	}
	firstAccountingAttempt := envelope.AccountingAttempts[0]
	finalAccountingAttempt := envelope.AccountingAttempts[1]
	if firstAccountingAttempt.AttemptNumber != 1 || firstAccountingAttempt.StatusCode != http.StatusServiceUnavailable || firstAccountingAttempt.UsageSource != gatewaycore.UsageSourceMissing || firstAccountingAttempt.Final || !firstAccountingAttempt.AuditEnabled || !firstAccountingAttempt.AuditCaptureBodies || !firstAccountingAttempt.ObservedAt.Equal(primaryCompletedAt) {
		t.Fatalf("unexpected first accounting attempt: %+v", firstAccountingAttempt)
	}
	if finalAccountingAttempt.AttemptNumber != 2 || finalAccountingAttempt.StatusCode != http.StatusOK || finalAccountingAttempt.UsageSource != gatewaycore.UsageSourceProviderStreamTerminal || !finalAccountingAttempt.Final || finalAccountingAttempt.StreamOutcome != runtimeStreamOutcomeCompleted || !finalAccountingAttempt.ObservedAt.Equal(completedAt) {
		t.Fatalf("unexpected final accounting attempt: %+v", finalAccountingAttempt)
	}
	if envelope.ProxyKeyUsage == nil || envelope.ProxyKeyUsage.KeyID != 77 || envelope.ProxyKeyUsage.LastUsedIP != "198.51.100.10" || !envelope.ProxyKeyUsage.LastUsedAt.Equal(proxyKeyLastUsedAt) {
		t.Fatalf("expected proxy key usage signal to survive durable envelope construction, got %+v", envelope.ProxyKeyUsage)
	}
}

func TestObservability_PreservesProviderUsageTruthWithContextRoutingMetadata(t *testing.T) {
	startedAt := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(2 * time.Second)
	service := &Service{now: func() time.Time { return completedAt }}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	selectedEndpointName := "selected endpoint"
	finalEndpointName := "final endpoint"
	selectedConnection := runtimeConnection{ID: 11, Endpoint: runtimeEndpoint{ID: 101, BaseURL: "https://selected.example", Name: &selectedEndpointName}}
	finalConnection := runtimeConnection{ID: 22, Endpoint: runtimeEndpoint{ID: 202, BaseURL: "https://final.example", Name: &finalEndpointName}}
	selectedTerminalTargetID := intPtr(selectedConnection.ID)
	contextRouting := &runtimeContextRoutingDecision{
		Policy:                      "cheapest_eligible_context",
		SelectedTerminalTargetID:    selectedTerminalTargetID,
		EstimationMethod:            stringPtr("openai_chat_tokenizer_v1"),
		EstimatedInputTokens:        intPtr(12),
		ReservedOutputTokens:        intPtr(256),
		EstimatedTotalContextTokens: intPtr(268),
		UsableContextWindowTokens:   intPtr(4096),
		CostRankingMethod:           stringPtr(runtimeContextRoutingCostRankingMethod),
		SkippedTerminalTargets: []runtimeContextRoutingSkippedTerminalTarget{{
			TerminalTargetID:            intPtr(33),
			EndpointID:                  intPtr(303),
			Reason:                      runtimeContextRoutingSkipReasonEstimatedContextExceedsUsableWindow,
			UsableContextWindowTokens:   intPtr(128),
			EstimatedTotalContextTokens: intPtr(268),
		}},
	}
	plan := requestPlan{
		ProfileID:                7,
		RequestedModelID:         "public-model",
		ResolvedTargetModelID:    stringPtr("native-model"),
		RequestedVendorID:        intPtr(1),
		RequestedVendorKey:       stringPtr("openai"),
		RequestedVendorName:      stringPtr("OpenAI"),
		APIFamily:                "openai",
		RuntimeOperation:         RuntimeOperation{Name: "openai.chat_completions"},
		ReportCurrencySnapshot:   runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
		UpstreamBody:             []byte(`{"model":"native-model"}`),
		SelectedTerminalTargetID: selectedTerminalTargetID,
		ContextRouting:           contextRouting,
	}
	result := executionResult{
		Response:              &http.Response{StatusCode: http.StatusOK},
		Connection:            finalConnection,
		ResolvedTargetModelID: stringPtr("native-model"),
		Attempts: []executionAttempt{
			{Connection: selectedConnection, ResolvedTargetModelID: "native-model", RequestHeaders: map[string]string{"User-Agent": "selected-upstream"}, ResponseHeaders: http.Header{"X-Request-Id": []string{"selected-correlation"}}, StatusCode: http.StatusServiceUnavailable, ResponseTimeMS: 300, CompletedAt: startedAt.Add(300 * time.Millisecond)},
			{Connection: finalConnection, ResolvedTargetModelID: "native-model", RequestHeaders: map[string]string{"User-Agent": "final-upstream"}, ResponseHeaders: http.Header{"X-Request-Id": []string{"final-correlation"}}, StatusCode: http.StatusOK, ResponseTimeMS: 1200, CompletedAt: completedAt},
		},
	}
	capture := runtimeResponseCapture{Usage: responseUsage{InputTokens: intPtr(10), OutputTokens: intPtr(20), TotalTokens: intPtr(30)}, CompletedAt: &completedAt, StreamOutcome: runtimeStreamOutcomeNotStreaming}

	envelope := service.buildRuntimeTelemetryEnvelope(plan, result, request, startedAt, capture)
	if len(envelope.RequestLogs) != 2 {
		t.Fatalf("expected two request-log rows, got %d", len(envelope.RequestLogs))
	}
	firstLog := envelope.RequestLogs[0]
	finalLog := envelope.RequestLogs[1]
	assertRuntimeIntPtr(t, firstLog.SelectedTerminalTargetID, selectedConnection.ID, "first selected terminal target")
	assertRuntimeIntPtr(t, finalLog.SelectedTerminalTargetID, selectedConnection.ID, "final selected terminal target")
	assertRuntimeIntPtr(t, firstLog.ConnectionID, selectedConnection.ID, "first connection")
	assertRuntimeIntPtr(t, finalLog.ConnectionID, finalConnection.ID, "final connection")
	if finalLog.ContextRouting == nil || finalLog.ContextRouting.EstimationMethod == nil || *finalLog.ContextRouting.EstimationMethod != "openai_chat_tokenizer_v1" {
		t.Fatalf("expected final request log context routing metadata, got %+v", finalLog.ContextRouting)
	}
	assertRuntimeIntPtr(t, finalLog.InputTokens, 10, "final provider input tokens")
	assertRuntimeIntPtr(t, finalLog.OutputTokens, 20, "final provider output tokens")
	assertRuntimeIntPtr(t, finalLog.TotalTokens, 30, "final provider total tokens")
	assertRuntimeIntPtr(t, finalLog.ContextRouting.EstimatedInputTokens, 12, "estimated input tokens")
	assertRuntimeIntPtr(t, finalLog.ContextRouting.ReservedOutputTokens, 256, "reserved output tokens")
	assertRuntimeIntPtr(t, finalLog.ContextRouting.EstimatedTotalContextTokens, 268, "estimated total context tokens")
	assertRuntimeIntPtr(t, envelope.UsageEvent.SelectedTerminalTargetID, selectedConnection.ID, "usage event selected terminal target")
	assertRuntimeIntPtr(t, envelope.UsageEvent.ConnectionID, finalConnection.ID, "usage event final connection")
	assertRuntimeIntPtr(t, envelope.UsageEvent.InputTokens, 10, "usage event provider input tokens")
	if envelope.UsageEvent.ContextRouting == nil || len(envelope.UsageEvent.ContextRouting.SkippedTerminalTargets) != 1 {
		t.Fatalf("expected usage event context routing metadata, got %+v", envelope.UsageEvent.ContextRouting)
	}
}

func TestObservability_OverflowAffinityCacheMetadataPersistsSafely(t *testing.T) {
	const rawAffinity = "raw-observability-affinity"
	const rawParent = "raw-observability-parent"
	startedAt := time.Date(2026, 6, 7, 11, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(500 * time.Millisecond)
	service := &Service{now: func() time.Time { return completedAt }}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	contextRouting := &runtimeContextRoutingDecision{
		Policy: "cheapest_eligible_context",
		ContextOverflowAffinity: &runtimeContextOverflowAffinityDecision{
			State:                  runtimeContextOverflowAffinityStatePopulated,
			AffinityHashPrefix:     "1111222233334444",
			ParentHashPrefix:       stringPtr("aaaabbbbccccdddd"),
			ContextBucket:          "0-16383",
			SourceModelID:          "source-model",
			PromotionTargetModelID: "promotion-target-model",
		},
	}
	connection := runtimeConnection{ID: 22, Endpoint: runtimeEndpoint{ID: 202}}
	plan := requestPlan{
		ProfileID:             7,
		RequestedModelID:      "public-model",
		ResolvedTargetModelID: stringPtr("promotion-target-model"),
		APIFamily:             "openai",
		RuntimeOperation:      RuntimeOperation{Name: "openai.chat_completions"},
		ContextRouting:        contextRouting,
		TerminalAttempts:      []runtimeTerminalAttempt{{Connection: connection}},
	}
	result := executionResult{
		Response:              &http.Response{StatusCode: http.StatusOK},
		Connection:            connection,
		ResolvedTargetModelID: stringPtr("promotion-target-model"),
		Attempts: []executionAttempt{{
			Connection:               connection,
			ResolvedTargetModelID:    "promotion-target-model",
			StatusCode:               http.StatusOK,
			ResponseTimeMS:           500,
			CompletedAt:              completedAt,
			UpstreamOperationName:    "openai.chat_completions",
			UpstreamRequestPath:      "/v1/chat/completions",
			OperationTranslationMode: TranslationModeNone,
		}},
	}
	capture := runtimeResponseCapture{Usage: responseUsage{InputTokens: intPtr(1), OutputTokens: intPtr(2), TotalTokens: intPtr(3)}, CompletedAt: &completedAt, StreamOutcome: runtimeStreamOutcomeNotStreaming}

	envelope := service.buildRuntimeTelemetryEnvelope(plan, result, request, startedAt, capture)
	if len(envelope.RequestLogs) != 1 || envelope.RequestLogs[0].ContextRouting == nil || envelope.RequestLogs[0].ContextRouting.ContextOverflowAffinity == nil {
		t.Fatalf("expected request-log overflow affinity metadata, got %+v", envelope.RequestLogs)
	}
	if envelope.UsageEvent.ContextRouting == nil || envelope.UsageEvent.ContextRouting.ContextOverflowAffinity == nil {
		t.Fatalf("expected usage-event overflow affinity metadata, got %+v", envelope.UsageEvent.ContextRouting)
	}
	affinity := envelope.RequestLogs[0].ContextRouting.ContextOverflowAffinity
	if affinity.State != runtimeContextOverflowAffinityStatePopulated || affinity.AffinityHashPrefix != "1111222233334444" || affinity.ParentHashPrefix == nil || *affinity.ParentHashPrefix != "aaaabbbbccccdddd" || affinity.ContextBucket != "0-16383" || affinity.SourceModelID != "source-model" || affinity.PromotionTargetModelID != "promotion-target-model" {
		t.Fatalf("unexpected overflow affinity metadata: %+v", affinity)
	}
	serialized, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if strings.Contains(string(serialized), rawAffinity) || strings.Contains(string(serialized), rawParent) {
		t.Fatalf("serialized telemetry envelope leaked raw affinity material: %s", serialized)
	}
}

func TestObservability_NoFitRoutingLeavesSelectedTerminalTargetNil(t *testing.T) {
	startedAt := time.Date(2026, 5, 29, 11, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(250 * time.Millisecond)
	service := &Service{now: func() time.Time { return completedAt }}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	contextRouting := &runtimeContextRoutingDecision{
		Policy:                      "cheapest_eligible_context",
		SelectedTerminalTargetID:    nil,
		EstimationMethod:            stringPtr("openai_chat_tokenizer_v1"),
		EstimatedInputTokens:        intPtr(14),
		ReservedOutputTokens:        intPtr(600),
		EstimatedTotalContextTokens: intPtr(614),
		UsableContextWindowTokens:   intPtr(400),
		CostRankingMethod:           stringPtr(runtimeContextRoutingCostRankingMethod),
		SkippedTerminalTargets: []runtimeContextRoutingSkippedTerminalTarget{{
			TerminalTargetID:            intPtr(44),
			EndpointID:                  intPtr(404),
			Reason:                      runtimeContextRoutingSkipReasonEstimatedContextExceedsUsableWindow,
			UsableContextWindowTokens:   intPtr(400),
			EstimatedTotalContextTokens: intPtr(614),
		}},
	}
	failure := runtimePlanningFailureTelemetry{
		ProfileID:               8,
		RequestedModelID:        "no-fit-model",
		RequestedVendorID:       intPtr(1),
		RequestedVendorKey:      stringPtr("openai"),
		RequestedVendorName:     stringPtr("OpenAI"),
		APIFamily:               "openai",
		RuntimeOperation:        RuntimeOperation{Name: "openai.chat_completions"},
		RequestPath:             "/v1/chat/completions",
		ReportCurrencySnapshot:  runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
		RequestGenerationParams: requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusMissing},
		ContextRouting:          contextRouting,
	}
	runtimeErr := &domainError{StatusCode: http.StatusRequestEntityTooLarge, ErrorCode: contextWindowExceededErrorCode, Detail: contextWindowExceededDetail, PlanningFailure: &failure, ContextRouting: contextRouting}

	envelope := service.buildRuntimePlanningFailureTelemetryEnvelope(failure, request, startedAt, runtimeErr)
	if len(envelope.RequestLogs) != 1 {
		t.Fatalf("expected one planning-failure request log row, got %d", len(envelope.RequestLogs))
	}
	requestLog := envelope.RequestLogs[0]
	if requestLog.ConnectionID != nil || requestLog.EndpointID != nil || requestLog.SelectedTerminalTargetID != nil {
		t.Fatalf("expected planning-failure row to keep connection and selected target nil, got %+v", requestLog)
	}
	if requestLog.ContextRouting == nil || len(requestLog.ContextRouting.SkippedTerminalTargets) != 1 {
		t.Fatalf("expected skipped-target context routing metadata, got %+v", requestLog.ContextRouting)
	}
	assertRuntimeBoolPtr(t, requestLog.BillableFlag, false, "planning failure billable")
	assertRuntimeBoolPtr(t, requestLog.PricedFlag, false, "planning failure priced")
	if envelope.UsageEvent.SelectedTerminalTargetID != nil || envelope.UsageEvent.ConnectionID != nil {
		t.Fatalf("expected usage event selected/final terminal target ids to stay nil on no-fit rejection, got %+v", envelope.UsageEvent)
	}
}

func TestObservability_FacadeSuccessPersistsDecisionMetadataAndTraceAttributes(t *testing.T) {
	startedAt := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(1200 * time.Millisecond)
	service := &Service{now: func() time.Time { return completedAt }}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	selectedTerminalTargetID := intPtr(11)
	facadeSelection := &runtimeFacadeSelectionDecision{
		FacadeModelID:         "facade-public-model",
		SelectedTargetModelID: stringPtr("native-target-model"),
		ExclusionReasons:      []runtimeFacadeExclusionReason{{Reason: runtimeContextRoutingSkipReasonEstimatedContextExceedsUsableWindow, Count: 1}},
		ExclusionSummary:      stringPtr("estimated_context_exceeds_usable_window=1"),
	}
	contextRouting := &runtimeContextRoutingDecision{
		Policy:                   runtimeFacadeSelectionPolicyOrderedEligibleContext,
		SelectedTerminalTargetID: selectedTerminalTargetID,
		FacadeSelection:          facadeSelection,
	}
	resolvedTargetModelID := "native-target-model"
	connection := runtimeConnection{ID: 11, Endpoint: runtimeEndpoint{ID: 101, BaseURL: "https://facade-success.example"}}
	plan := requestPlan{
		ProfileID:                7,
		RequestedModelID:         "facade-public-model",
		ResolvedTargetModelID:    &resolvedTargetModelID,
		RequestedVendorID:        intPtr(1),
		RequestedVendorKey:       stringPtr("openai"),
		RequestedVendorName:      stringPtr("OpenAI"),
		APIFamily:                "openai",
		RuntimeOperation:         RuntimeOperation{Name: "openai.chat_completions", PathTemplate: "/v1/chat/completions"},
		ReportCurrencySnapshot:   runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
		SelectedTerminalTargetID: selectedTerminalTargetID,
		ContextRouting:           contextRouting,
	}
	result := executionResult{
		Response:              &http.Response{StatusCode: http.StatusOK},
		Connection:            connection,
		ResolvedTargetModelID: &resolvedTargetModelID,
		Attempts: []executionAttempt{{
			Connection:            connection,
			ResolvedTargetModelID: resolvedTargetModelID,
			ResponseHeaders:       http.Header{"X-Request-Id": []string{"facade-success-request"}},
			StatusCode:            http.StatusOK,
			ResponseTimeMS:        1200,
			CompletedAt:           completedAt,
			UpstreamOperationName: openAIUpstreamOperationChatCompletions,
			UpstreamRequestPath:   "/v1/chat/completions",
		}},
	}
	capture := runtimeResponseCapture{Usage: responseUsage{InputTokens: intPtr(8), OutputTokens: intPtr(4), TotalTokens: intPtr(12)}, CompletedAt: &completedAt, StreamOutcome: runtimeStreamOutcomeNotStreaming}

	envelope := service.buildRuntimeTelemetryEnvelope(plan, result, request, startedAt, capture)
	assertRuntimeFacadeSelectionDecision(t, envelope.RequestLogs[0].ContextRouting.FacadeSelection, "facade-public-model", stringPtr("native-target-model"), intPtr(1), intPtr(2), stringPtr("estimated_context_exceeds_usable_window=1"))
	assertRuntimeFacadeSelectionDecision(t, envelope.UsageEvent.ContextRouting.FacadeSelection, "facade-public-model", stringPtr("native-target-model"), intPtr(1), intPtr(2), stringPtr("estimated_context_exceeds_usable_window=1"))

	planAttrs := attributesByKey(runtimeTracePlanAttributes(plan))
	if planAttrs[runtimeTraceAttrFacadeModelID].AsString() != "facade-public-model" || planAttrs[runtimeTraceAttrFacadeSelectedTargetModel].AsString() != "native-target-model" || planAttrs[runtimeTraceAttrFacadeExclusionSummary].AsString() != "estimated_context_exceeds_usable_window=1" {
		t.Fatalf("expected facade plan trace attrs, got %+v", planAttrs)
	}
	envelopeAttrs := attributesByKey(runtimeTraceEnvelopeAttributes(envelope))
	if envelopeAttrs[runtimeTraceAttrFacadeModelID].AsString() != "facade-public-model" || envelopeAttrs[runtimeTraceAttrFacadeSelectedTargetModel].AsString() != "native-target-model" || envelopeAttrs[runtimeTraceAttrFacadeExclusionSummary].AsString() != "estimated_context_exceeds_usable_window=1" {
		t.Fatalf("expected facade envelope trace attrs, got %+v", envelopeAttrs)
	}
}

func TestObservability_FacadeNoFitPlanningFailurePersistsDecisionMetadataAndTraceAttributes(t *testing.T) {
	startedAt := time.Date(2026, 6, 2, 12, 15, 0, 0, time.UTC)
	completedAt := startedAt.Add(250 * time.Millisecond)
	service := &Service{now: func() time.Time { return completedAt }}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	facadeSelection := &runtimeFacadeSelectionDecision{
		FacadeModelID:    "facade-no-fit-model",
		ExclusionReasons: []runtimeFacadeExclusionReason{{Reason: runtimeContextRoutingSkipReasonEstimatedContextExceedsUsableWindow, Count: 2}},
		ExclusionSummary: stringPtr("estimated_context_exceeds_usable_window=2"),
	}
	contextRouting := &runtimeContextRoutingDecision{
		Policy:                      runtimeFacadeSelectionPolicyOrderedEligibleContext,
		EstimatedInputTokens:        intPtr(14),
		ReservedOutputTokens:        intPtr(600),
		EstimatedTotalContextTokens: intPtr(614),
		FacadeSelection:             facadeSelection,
	}
	failure := runtimePlanningFailureTelemetry{
		ProfileID:              8,
		RequestedModelID:       "facade-no-fit-model",
		RequestedVendorID:      intPtr(1),
		RequestedVendorKey:     stringPtr("openai"),
		RequestedVendorName:    stringPtr("OpenAI"),
		APIFamily:              "openai",
		RuntimeOperation:       RuntimeOperation{Name: "openai.chat_completions", PathTemplate: "/v1/chat/completions"},
		RequestPath:            "/v1/chat/completions",
		ReportCurrencySnapshot: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
		ContextRouting:         contextRouting,
	}
	runtimeErr := &domainError{StatusCode: http.StatusRequestEntityTooLarge, ErrorCode: contextWindowExceededErrorCode, Detail: contextWindowExceededDetail, PlanningFailure: &failure, ContextRouting: contextRouting}

	envelope := service.buildRuntimePlanningFailureTelemetryEnvelope(failure, request, startedAt, runtimeErr)
	assertRuntimeFacadeSelectionDecision(t, envelope.RequestLogs[0].ContextRouting.FacadeSelection, "facade-no-fit-model", nil, nil, intPtr(0), stringPtr("estimated_context_exceeds_usable_window=2"))
	assertRuntimeFacadeSelectionDecision(t, envelope.UsageEvent.ContextRouting.FacadeSelection, "facade-no-fit-model", nil, nil, intPtr(0), stringPtr("estimated_context_exceeds_usable_window=2"))

	planningFailureAttrs := attributesByKey(runtimeTracePlanningFailureAttributes(failure))
	if planningFailureAttrs[runtimeTraceAttrFacadeModelID].AsString() != "facade-no-fit-model" || planningFailureAttrs[runtimeTraceAttrFacadeExclusionSummary].AsString() != "estimated_context_exceeds_usable_window=2" {
		t.Fatalf("expected facade no-fit planning failure trace attrs, got %+v", planningFailureAttrs)
	}
}

func TestObservability_FacadeNoEligiblePlanningFailurePersistsDecisionMetadataAndTraceAttributes(t *testing.T) {
	startedAt := time.Date(2026, 6, 2, 12, 30, 0, 0, time.UTC)
	completedAt := startedAt.Add(150 * time.Millisecond)
	service := &Service{now: func() time.Time { return completedAt }}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	facadeSelection := &runtimeFacadeSelectionDecision{FacadeModelID: "facade-no-eligible-model"}
	contextRouting := &runtimeContextRoutingDecision{Policy: runtimeFacadeSelectionPolicyOrderedEligibleContext, FacadeSelection: facadeSelection}
	failure := runtimePlanningFailureTelemetry{
		ProfileID:              9,
		RequestedModelID:       "facade-no-eligible-model",
		RequestedVendorID:      intPtr(1),
		RequestedVendorKey:     stringPtr("openai"),
		RequestedVendorName:    stringPtr("OpenAI"),
		APIFamily:              "openai",
		RuntimeOperation:       RuntimeOperation{Name: "openai.chat_completions", PathTemplate: "/v1/chat/completions"},
		RequestPath:            "/v1/chat/completions",
		ReportCurrencySnapshot: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
		ContextRouting:         contextRouting,
	}
	runtimeErr := &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "No eligible targets available for model 'facade-no-eligible-model'.", PlanningFailure: &failure, ContextRouting: contextRouting}

	envelope := service.buildRuntimePlanningFailureTelemetryEnvelope(failure, request, startedAt, runtimeErr)
	assertRuntimeFacadeSelectionDecision(t, envelope.RequestLogs[0].ContextRouting.FacadeSelection, "facade-no-eligible-model", nil, nil, intPtr(0), nil)
	assertRuntimeFacadeSelectionDecision(t, envelope.UsageEvent.ContextRouting.FacadeSelection, "facade-no-eligible-model", nil, nil, intPtr(0), nil)

	planningFailureAttrs := attributesByKey(runtimeTracePlanningFailureAttributes(failure))
	if planningFailureAttrs[runtimeTraceAttrFacadeModelID].AsString() != "facade-no-eligible-model" {
		t.Fatalf("expected facade no-eligible planning failure trace attrs, got %+v", planningFailureAttrs)
	}
	if _, ok := planningFailureAttrs[runtimeTraceAttrFacadeExclusionSummary]; ok {
		t.Fatalf("expected no facade exclusion summary attr on no-eligible planning failure, got %+v", planningFailureAttrs)
	}
}

func TestObservabilityRecordsSourceAndPromotedAttempts(t *testing.T) {
	startedAt := time.Date(2026, 6, 5, 9, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(1200 * time.Millisecond)
	service := &Service{now: func() time.Time { return completedAt }}
	request := newRuntimeTelemetryEnvelopeRequest(completedAt)

	sourceResolvedTargetModelID := "source-model"
	promotedResolvedTargetModelID := "promoted-model"
	sourceSelectedTerminalTargetID := intPtr(11)
	promotedSelectedTerminalTargetID := intPtr(22)
	sourceConnectionOne := runtimeConnection{ID: 11, Endpoint: runtimeEndpoint{ID: 111, BaseURL: "https://source-one.example"}}
	sourceConnectionTwo := runtimeConnection{ID: 12, Endpoint: runtimeEndpoint{ID: 112, BaseURL: "https://source-two.example"}}
	promotedConnection := runtimeConnection{ID: 22, Endpoint: runtimeEndpoint{ID: 222, BaseURL: "https://promoted.example"}}
	sourcePlan := requestPlan{
		ProfileID:                42,
		RequestedModelID:         "public-model",
		ResolvedTargetModelID:    &sourceResolvedTargetModelID,
		RequestedVendorID:        intPtr(1),
		RequestedVendorKey:       stringPtr("openai"),
		RequestedVendorName:      stringPtr("OpenAI"),
		APIFamily:                "openai",
		RuntimeOperation:         RuntimeOperation{Name: "openai.chat_completions", PathTemplate: "/v1/chat/completions"},
		ReportCurrencySnapshot:   runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
		RequestContextEstimation: &requestContextEstimation{Method: openAIChatContextEstimationMethod, EstimatedInputTokens: 1200, ReservedOutputTokens: 4000, EstimatedTotalContextTokens: 5200, UsableContextWindowTokens: intPtr(243000)},
		SelectedTerminalTargetID: sourceSelectedTerminalTargetID,
		ContextRouting: &runtimeContextRoutingDecision{
			Policy:                            "cheapest_eligible_context",
			SelectedTerminalTargetID:          sourceSelectedTerminalTargetID,
			SelectedUsableContextWindowTokens: intPtr(243000),
			UsableContextWindowTokens:         intPtr(243000),
		},
	}
	promotedPlan := requestPlan{
		RequestedModelID:         "promoted-model",
		ResolvedTargetModelID:    &promotedResolvedTargetModelID,
		SelectedTerminalTargetID: promotedSelectedTerminalTargetID,
		ContextRouting: &runtimeContextRoutingDecision{
			Policy:                            "cheapest_eligible_context",
			SelectedTerminalTargetID:          promotedSelectedTerminalTargetID,
			SelectedUsableContextWindowTokens: intPtr(900000),
			UsableContextWindowTokens:         intPtr(900000),
		},
		TerminalAttempts: []runtimeTerminalAttempt{{Connection: promotedConnection}},
	}
	sourceExecution := executionResult{
		Response:              &http.Response{StatusCode: http.StatusBadRequest},
		Connection:            sourceConnectionTwo,
		ResolvedTargetModelID: &sourceResolvedTargetModelID,
		AttemptCount:          2,
		Attempts: []executionAttempt{
			{Connection: sourceConnectionOne, ResolvedTargetModelID: sourceResolvedTargetModelID, RequestHeaders: map[string]string{"User-Agent": "source-upstream-1"}, StatusCode: http.StatusServiceUnavailable, ResponseTimeMS: 150, CompletedAt: startedAt.Add(150 * time.Millisecond)},
			{Connection: sourceConnectionTwo, ResolvedTargetModelID: sourceResolvedTargetModelID, RequestHeaders: map[string]string{"User-Agent": "source-upstream-2"}, StatusCode: http.StatusBadRequest, ResponseTimeMS: 350, CompletedAt: startedAt.Add(350 * time.Millisecond)},
		},
	}
	promotedExecution := executionResult{
		Response:              &http.Response{StatusCode: http.StatusOK},
		Connection:            promotedConnection,
		ResolvedTargetModelID: &promotedResolvedTargetModelID,
		AttemptCount:          1,
		Attempts:              []executionAttempt{{Connection: promotedConnection, ResolvedTargetModelID: promotedResolvedTargetModelID, RequestHeaders: map[string]string{"User-Agent": "promoted-upstream"}, StatusCode: http.StatusOK, ResponseTimeMS: 600, CompletedAt: completedAt}},
	}

	finalPlan := mergeContextOverflowPromotedPlan(sourcePlan, promotedPlan, sourceExecution, promotedExecution, cliProxyAPIOverflowClassification{Promotable: true, ErrorCode: "context_length_exceeded", Classifier: cliProxyAPIOverflowClassifierErrorCode})
	finalExecution := mergeContextOverflowPromotedExecution(finalPlan, sourceExecution, promotedExecution)
	envelope := service.buildRuntimeTelemetryEnvelope(finalPlan, finalExecution, request, startedAt, runtimeResponseCapture{Usage: responseUsage{InputTokens: intPtr(9), OutputTokens: intPtr(5), TotalTokens: intPtr(14)}, CompletedAt: &completedAt, StreamOutcome: runtimeStreamOutcomeNotStreaming})

	if len(envelope.RequestLogs) != 3 {
		t.Fatalf("expected three request-log rows, got %d", len(envelope.RequestLogs))
	}
	for index, requestLog := range envelope.RequestLogs {
		if requestLog.AttemptNumber != index+1 {
			t.Fatalf("expected attempt_number=%d, got %+v", index+1, requestLog)
		}
		if requestLog.IngressRequestID != envelope.UsageEvent.IngressRequestID {
			t.Fatalf("expected shared ingress_request_id, got request_log=%q usage_event=%q", requestLog.IngressRequestID, envelope.UsageEvent.IngressRequestID)
		}
	}
	assertRuntimeIntPtr(t, envelope.RequestLogs[0].SelectedTerminalTargetID, 11, "first source selected terminal target")
	assertRuntimeIntPtr(t, envelope.RequestLogs[1].SelectedTerminalTargetID, 11, "overflow source selected terminal target")
	assertRuntimeIntPtr(t, envelope.RequestLogs[2].SelectedTerminalTargetID, 22, "promoted selected terminal target")
	assertRuntimeStringPtr(t, envelope.RequestLogs[0].ResolvedTargetModelID, sourceResolvedTargetModelID, "first source resolved target model")
	assertRuntimeStringPtr(t, envelope.RequestLogs[1].ResolvedTargetModelID, sourceResolvedTargetModelID, "overflow source resolved target model")
	assertRuntimeStringPtr(t, envelope.RequestLogs[2].ResolvedTargetModelID, promotedResolvedTargetModelID, "promoted resolved target model")
	assertRuntimeContextOverflowPromotionDecision(t, envelope.RequestLogs[1].ContextRouting.ContextOverflowPromotion, 400, "context_length_exceeded", cliProxyAPIOverflowClassifierErrorCode, runtimeContextOverflowPromotionTriggerPhaseProviderOverflow, runtimeContextOverflowPromotionEstimationModeEstimated, openAIChatContextEstimationMethod, 1200, 4000, 5200, sourceResolvedTargetModelID, 11, promotedResolvedTargetModelID, 22, 243000, 900000, 2, 3, runtimeContextOverflowPromotionResultPromotedSuccess)
	assertRuntimeContextOverflowPromotionDecision(t, envelope.RequestLogs[2].ContextRouting.ContextOverflowPromotion, 400, "context_length_exceeded", cliProxyAPIOverflowClassifierErrorCode, runtimeContextOverflowPromotionTriggerPhaseProviderOverflow, runtimeContextOverflowPromotionEstimationModeEstimated, openAIChatContextEstimationMethod, 1200, 4000, 5200, sourceResolvedTargetModelID, 11, promotedResolvedTargetModelID, 22, 243000, 900000, 2, 3, runtimeContextOverflowPromotionResultPromotedSuccess)
	assertRuntimeIntPtr(t, envelope.UsageEvent.SelectedTerminalTargetID, 22, "usage event promoted selected terminal target")
	assertRuntimeStringPtr(t, envelope.UsageEvent.ResolvedTargetModelID, promotedResolvedTargetModelID, "usage event promoted resolved target model")
	if envelope.UsageEvent.AttemptCount != 3 {
		t.Fatalf("expected usage event attempt_count=3, got %+v", envelope.UsageEvent)
	}
	assertRuntimeContextOverflowPromotionDecision(t, envelope.UsageEvent.ContextRouting.ContextOverflowPromotion, 400, "context_length_exceeded", cliProxyAPIOverflowClassifierErrorCode, runtimeContextOverflowPromotionTriggerPhaseProviderOverflow, runtimeContextOverflowPromotionEstimationModeEstimated, openAIChatContextEstimationMethod, 1200, 4000, 5200, sourceResolvedTargetModelID, 11, promotedResolvedTargetModelID, 22, 243000, 900000, 2, 3, runtimeContextOverflowPromotionResultPromotedSuccess)
}

func TestUsageEventUsesFinalPromotedResponse(t *testing.T) {
	startedAt := time.Date(2026, 6, 5, 9, 30, 0, 0, time.UTC)
	completedAt := startedAt.Add(1600 * time.Millisecond)
	service := &Service{now: func() time.Time { return completedAt }}
	request := newRuntimeTelemetryEnvelopeRequest(completedAt)

	sourceResolvedTargetModelID := "source-model"
	promotedResolvedTargetModelID := "promoted-model"
	sourceSelectedTerminalTargetID := intPtr(31)
	promotedSelectedTerminalTargetID := intPtr(41)
	sourceConnection := runtimeConnection{ID: 31, Endpoint: runtimeEndpoint{ID: 311, BaseURL: "https://source.example"}}
	promotedConnectionOne := runtimeConnection{ID: 41, Endpoint: runtimeEndpoint{ID: 411, BaseURL: "https://promoted-one.example"}}
	promotedConnectionTwo := runtimeConnection{ID: 42, Endpoint: runtimeEndpoint{ID: 412, BaseURL: "https://promoted-two.example"}}
	sourcePlan := requestPlan{
		ProfileID:                7,
		RequestedModelID:         "public-model",
		ResolvedTargetModelID:    &sourceResolvedTargetModelID,
		RequestedVendorID:        intPtr(1),
		RequestedVendorKey:       stringPtr("openai"),
		RequestedVendorName:      stringPtr("OpenAI"),
		APIFamily:                "openai",
		RuntimeOperation:         RuntimeOperation{Name: "openai.chat_completions", PathTemplate: "/v1/chat/completions"},
		ReportCurrencySnapshot:   runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
		SelectedTerminalTargetID: sourceSelectedTerminalTargetID,
		ContextRouting: &runtimeContextRoutingDecision{
			Policy:                            "cheapest_eligible_context",
			SelectedTerminalTargetID:          sourceSelectedTerminalTargetID,
			SelectedUsableContextWindowTokens: intPtr(200000),
			UsableContextWindowTokens:         intPtr(200000),
		},
	}
	promotedPlan := requestPlan{
		RequestedModelID:         "promoted-model",
		ResolvedTargetModelID:    &promotedResolvedTargetModelID,
		SelectedTerminalTargetID: promotedSelectedTerminalTargetID,
		ContextRouting: &runtimeContextRoutingDecision{
			Policy:                            "cheapest_eligible_context",
			SelectedTerminalTargetID:          promotedSelectedTerminalTargetID,
			SelectedUsableContextWindowTokens: intPtr(400000),
			UsableContextWindowTokens:         intPtr(400000),
		},
		TerminalAttempts: []runtimeTerminalAttempt{{Connection: promotedConnectionOne}, {Connection: promotedConnectionTwo}},
	}
	sourceExecution := executionResult{
		Response:              &http.Response{StatusCode: http.StatusUnprocessableEntity},
		Connection:            sourceConnection,
		ResolvedTargetModelID: &sourceResolvedTargetModelID,
		AttemptCount:          1,
		Attempts:              []executionAttempt{{Connection: sourceConnection, ResolvedTargetModelID: sourceResolvedTargetModelID, StatusCode: http.StatusUnprocessableEntity, ResponseTimeMS: 200, CompletedAt: startedAt.Add(200 * time.Millisecond)}},
	}
	promotedExecution := executionResult{
		Response:              &http.Response{StatusCode: http.StatusOK},
		Connection:            promotedConnectionTwo,
		ResolvedTargetModelID: &promotedResolvedTargetModelID,
		AttemptCount:          2,
		Attempts: []executionAttempt{
			{Connection: promotedConnectionOne, ResolvedTargetModelID: promotedResolvedTargetModelID, StatusCode: http.StatusServiceUnavailable, ResponseTimeMS: 500, CompletedAt: startedAt.Add(500 * time.Millisecond)},
			{Connection: promotedConnectionTwo, ResolvedTargetModelID: promotedResolvedTargetModelID, StatusCode: http.StatusOK, ResponseTimeMS: 900, CompletedAt: completedAt},
		},
	}

	finalPlan := mergeContextOverflowPromotedPlan(sourcePlan, promotedPlan, sourceExecution, promotedExecution, cliProxyAPIOverflowClassification{Promotable: true, ErrorCode: "context_too_large", Classifier: cliProxyAPIOverflowClassifierErrorCode})
	finalExecution := mergeContextOverflowPromotedExecution(finalPlan, sourceExecution, promotedExecution)
	envelope := service.buildRuntimeTelemetryEnvelope(finalPlan, finalExecution, request, startedAt, runtimeResponseCapture{Usage: responseUsage{InputTokens: intPtr(12), OutputTokens: intPtr(8), TotalTokens: intPtr(20)}, CompletedAt: &completedAt, StreamOutcome: runtimeStreamOutcomeNotStreaming})

	if len(envelope.RequestLogs) != 3 {
		t.Fatalf("expected three request-log rows, got %d", len(envelope.RequestLogs))
	}
	if envelope.RequestLogs[0].TotalTokens != nil || envelope.RequestLogs[1].TotalTokens != nil {
		t.Fatalf("expected pre-final attempts to omit final usage, got %+v %+v", envelope.RequestLogs[0], envelope.RequestLogs[1])
	}
	assertRuntimeIntPtr(t, envelope.RequestLogs[2].TotalTokens, 20, "final promoted request-log total tokens")
	assertRuntimeStringPtr(t, envelope.UsageEvent.ResolvedTargetModelID, promotedResolvedTargetModelID, "usage event final resolved target model")
	assertRuntimeIntPtr(t, envelope.UsageEvent.ConnectionID, 42, "usage event final connection")
	assertRuntimeIntPtr(t, envelope.UsageEvent.SelectedTerminalTargetID, 41, "usage event promoted selected terminal target")
	assertRuntimeIntPtr(t, envelope.UsageEvent.InputTokens, 12, "usage event final input tokens")
	assertRuntimeIntPtr(t, envelope.UsageEvent.OutputTokens, 8, "usage event final output tokens")
	assertRuntimeIntPtr(t, envelope.UsageEvent.TotalTokens, 20, "usage event final total tokens")
	if envelope.UsageEvent.StatusCode != http.StatusOK || envelope.UsageEvent.AttemptCount != 3 {
		t.Fatalf("expected final usage event status/attempt_count from promoted response, got %+v", envelope.UsageEvent)
	}
}

func TestPromotionObservabilityPreservesFacadeSelection(t *testing.T) {
	startedAt := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(1400 * time.Millisecond)
	service := &Service{now: func() time.Time { return completedAt }}
	request := newRuntimeTelemetryEnvelopeRequest(completedAt)

	sourceResolvedTargetModelID := "facade-child-model"
	promotedResolvedTargetModelID := "promoted-model"
	sourceSelectedTerminalTargetID := intPtr(51)
	promotedSelectedTerminalTargetID := intPtr(61)
	sourceConnection := runtimeConnection{ID: 51, Endpoint: runtimeEndpoint{ID: 511, BaseURL: "https://facade-source.example"}}
	promotedConnection := runtimeConnection{ID: 61, Endpoint: runtimeEndpoint{ID: 611, BaseURL: "https://facade-promoted.example"}}
	facadeSelection := &runtimeFacadeSelectionDecision{
		FacadeModelID:         "facade-public-model",
		SelectedTargetModelID: stringPtr(sourceResolvedTargetModelID),
		ExclusionReasons:      []runtimeFacadeExclusionReason{{Reason: runtimeContextRoutingSkipReasonEstimatedContextExceedsUsableWindow, Count: 1}},
		ExclusionSummary:      stringPtr("estimated_context_exceeds_usable_window=1"),
	}
	sourcePlan := requestPlan{
		ProfileID:                9,
		RequestedModelID:         "facade-public-model",
		ResolvedTargetModelID:    &sourceResolvedTargetModelID,
		RequestedVendorID:        intPtr(1),
		RequestedVendorKey:       stringPtr("openai"),
		RequestedVendorName:      stringPtr("OpenAI"),
		APIFamily:                "openai",
		RuntimeOperation:         RuntimeOperation{Name: "openai.chat_completions", PathTemplate: "/v1/chat/completions"},
		ReportCurrencySnapshot:   runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
		RequestContextEstimation: &requestContextEstimation{Method: openAIChatContextEstimationMethod, EstimatedInputTokens: 900, ReservedOutputTokens: 2048, EstimatedTotalContextTokens: 2948, UsableContextWindowTokens: intPtr(250000)},
		SelectedTerminalTargetID: sourceSelectedTerminalTargetID,
		ContextRouting: &runtimeContextRoutingDecision{
			Policy:                            runtimeFacadeSelectionPolicyOrderedEligibleContext,
			SelectedTerminalTargetID:          sourceSelectedTerminalTargetID,
			SelectedUsableContextWindowTokens: intPtr(250000),
			UsableContextWindowTokens:         intPtr(250000),
			FacadeSelection:                   facadeSelection,
		},
	}
	promotedPlan := requestPlan{
		RequestedModelID:         "promoted-model",
		ResolvedTargetModelID:    &promotedResolvedTargetModelID,
		SelectedTerminalTargetID: promotedSelectedTerminalTargetID,
		ContextRouting: &runtimeContextRoutingDecision{
			Policy:                            "cheapest_eligible_context",
			SelectedTerminalTargetID:          promotedSelectedTerminalTargetID,
			SelectedUsableContextWindowTokens: intPtr(700000),
			UsableContextWindowTokens:         intPtr(700000),
		},
		TerminalAttempts: []runtimeTerminalAttempt{{Connection: promotedConnection}},
	}
	sourceExecution := executionResult{
		Response:              &http.Response{StatusCode: http.StatusBadRequest},
		Connection:            sourceConnection,
		ResolvedTargetModelID: &sourceResolvedTargetModelID,
		AttemptCount:          1,
		Attempts:              []executionAttempt{{Connection: sourceConnection, ResolvedTargetModelID: sourceResolvedTargetModelID, StatusCode: http.StatusBadRequest, ResponseTimeMS: 300, CompletedAt: startedAt.Add(300 * time.Millisecond)}},
	}
	promotedExecution := executionResult{
		Response:              &http.Response{StatusCode: http.StatusOK},
		Connection:            promotedConnection,
		ResolvedTargetModelID: &promotedResolvedTargetModelID,
		AttemptCount:          1,
		Attempts:              []executionAttempt{{Connection: promotedConnection, ResolvedTargetModelID: promotedResolvedTargetModelID, StatusCode: http.StatusOK, ResponseTimeMS: 700, CompletedAt: completedAt}},
	}

	finalPlan := mergeContextOverflowPromotedPlan(sourcePlan, promotedPlan, sourceExecution, promotedExecution, cliProxyAPIOverflowClassification{Promotable: true, ErrorCode: "context_length_exceeded", Classifier: cliProxyAPIOverflowClassifierErrorCode})
	finalExecution := mergeContextOverflowPromotedExecution(finalPlan, sourceExecution, promotedExecution)
	envelope := service.buildRuntimeTelemetryEnvelope(finalPlan, finalExecution, request, startedAt, runtimeResponseCapture{Usage: responseUsage{InputTokens: intPtr(10), OutputTokens: intPtr(4), TotalTokens: intPtr(14)}, CompletedAt: &completedAt, StreamOutcome: runtimeStreamOutcomeNotStreaming})

	assertRuntimeFacadeSelectionDecision(t, envelope.RequestLogs[1].ContextRouting.FacadeSelection, "facade-public-model", stringPtr(sourceResolvedTargetModelID), intPtr(1), intPtr(2), stringPtr("estimated_context_exceeds_usable_window=1"))
	assertRuntimeFacadeSelectionDecision(t, envelope.UsageEvent.ContextRouting.FacadeSelection, "facade-public-model", stringPtr(sourceResolvedTargetModelID), intPtr(1), intPtr(2), stringPtr("estimated_context_exceeds_usable_window=1"))
	assertRuntimeContextOverflowPromotionDecision(t, envelope.UsageEvent.ContextRouting.ContextOverflowPromotion, 400, "context_length_exceeded", cliProxyAPIOverflowClassifierErrorCode, runtimeContextOverflowPromotionTriggerPhaseProviderOverflow, runtimeContextOverflowPromotionEstimationModeEstimated, openAIChatContextEstimationMethod, 900, 2048, 2948, sourceResolvedTargetModelID, 51, promotedResolvedTargetModelID, 61, 250000, 700000, 1, 2, runtimeContextOverflowPromotionResultPromotedSuccess)

	planAttrs := attributesByKey(runtimeTracePlanAttributes(finalPlan))
	if !planAttrs[runtimeTraceAttrContextOverflowPromotion].AsBool() || planAttrs[runtimeTraceAttrFacadeModelID].AsString() != "facade-public-model" || planAttrs[runtimeTraceAttrFacadeSelectedTargetModel].AsString() != sourceResolvedTargetModelID || planAttrs[runtimeTraceAttrFacadeExclusionSummary].AsString() != "estimated_context_exceeds_usable_window=1" || planAttrs[runtimeTraceAttrContextOverflowPromotionFromModelID].AsString() != "redacted_model" || planAttrs[runtimeTraceAttrContextOverflowPromotionToModelID].AsString() != "redacted_model" || planAttrs[runtimeTraceAttrContextOverflowPromotionTriggerStatus].AsInt64() != 400 || planAttrs[runtimeTraceAttrContextOverflowPromotionTriggerCode].AsString() != "context_length_exceeded" || planAttrs[runtimeTraceAttrContextOverflowPromotionTriggerClassifier].AsString() != cliProxyAPIOverflowClassifierErrorCode || planAttrs[runtimeTraceAttrContextOverflowPromotionResult].AsString() != runtimeContextOverflowPromotionResultPromotedSuccess {
		t.Fatalf("expected additive facade + promotion plan trace attrs, got %+v", planAttrs)
	}
	envelopeAttrs := attributesByKey(runtimeTraceEnvelopeAttributes(envelope))
	if !envelopeAttrs[runtimeTraceAttrContextOverflowPromotion].AsBool() || envelopeAttrs[runtimeTraceAttrFacadeModelID].AsString() != "facade-public-model" || envelopeAttrs[runtimeTraceAttrFacadeSelectedTargetModel].AsString() != sourceResolvedTargetModelID || envelopeAttrs[runtimeTraceAttrContextOverflowPromotionFromModelID].AsString() != "redacted_model" || envelopeAttrs[runtimeTraceAttrContextOverflowPromotionToModelID].AsString() != "redacted_model" || envelopeAttrs[runtimeTraceAttrContextOverflowPromotionTriggerCode].AsString() != "context_length_exceeded" || envelopeAttrs[runtimeTraceAttrContextOverflowPromotionResult].AsString() != runtimeContextOverflowPromotionResultPromotedSuccess {
		t.Fatalf("expected additive facade + promotion envelope trace attrs, got %+v", envelopeAttrs)
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

func assertRuntimeFacadeSelectionDecision(t *testing.T, got *runtimeFacadeSelectionDecision, wantFacadeModelID string, wantSelectedTargetModelID *string, _ *int, _ *int, wantExclusionSummary *string) {
	t.Helper()
	if got == nil {
		t.Fatal("expected facade selection decision, got nil")
	}
	if got.FacadeModelID != wantFacadeModelID {
		t.Fatalf("expected facade_model_id=%q, got %+v", wantFacadeModelID, got)
	}
	if dereferenceString(got.SelectedTargetModelID) != dereferenceString(wantSelectedTargetModelID) {
		t.Fatalf("expected selected_target_model_id=%q, got %+v", dereferenceString(wantSelectedTargetModelID), got)
	}
	if dereferenceString(got.ExclusionSummary) != dereferenceString(wantExclusionSummary) {
		t.Fatalf("expected exclusion_summary=%q, got %+v", dereferenceString(wantExclusionSummary), got)
	}
	if wantExclusionSummary != nil && len(got.ExclusionReasons) == 0 {
		t.Fatalf("expected exclusion reasons for summary %q, got %+v", *wantExclusionSummary, got)
	}
}

func assertRuntimeContextOverflowPromotionDecision(t *testing.T, got *runtimeContextOverflowPromotionDecision, wantTriggerStatus int, wantTriggerErrorCode string, wantTriggerClassifier string, wantTriggerPhase string, wantEstimationMode string, wantEstimationMethod string, wantEstimatedInputTokens int, wantReservedOutputTokens int, wantEstimatedTotalContextTokens int, wantFromResolvedTargetModelID string, wantFromSelectedTerminalTargetID int, wantToResolvedTargetModelID string, wantToSelectedTerminalTargetID int, wantFromUsableContextWindowTokens int, wantToUsableContextWindowTokens int, wantSourceAttemptCount int, wantFinalAttemptCount int, wantResult string) {
	t.Helper()
	if got == nil {
		t.Fatal("expected context overflow promotion decision, got nil")
	}
	if got.TriggerStatus != wantTriggerStatus || dereferenceString(got.TriggerErrorCode) != wantTriggerErrorCode || got.TriggerClassifier != wantTriggerClassifier || got.TriggerPhase != wantTriggerPhase || got.EstimationMode != wantEstimationMode || dereferenceString(got.EstimationMethod) != wantEstimationMethod || intValue(got.EstimatedInputTokens) != wantEstimatedInputTokens || intValue(got.ReservedOutputTokens) != wantReservedOutputTokens || intValue(got.EstimatedTotalContextTokens) != wantEstimatedTotalContextTokens || dereferenceString(got.FromResolvedTargetModelID) != wantFromResolvedTargetModelID || dereferenceString(got.ToResolvedTargetModelID) != wantToResolvedTargetModelID || got.SourceAttemptCount != wantSourceAttemptCount || got.FinalAttemptCount != wantFinalAttemptCount || got.Result != wantResult {
		t.Fatalf("unexpected context overflow promotion decision: %+v", got)
	}
	assertRuntimeIntPtr(t, got.FromSelectedTerminalTargetID, wantFromSelectedTerminalTargetID, "promotion from selected terminal target")
	assertRuntimeIntPtr(t, got.ToSelectedTerminalTargetID, wantToSelectedTerminalTargetID, "promotion to selected terminal target")
	assertRuntimeIntPtr(t, got.FromUsableContextWindowTokens, wantFromUsableContextWindowTokens, "promotion from usable context window")
	assertRuntimeIntPtr(t, got.ToUsableContextWindowTokens, wantToUsableContextWindowTokens, "promotion to usable context window")
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
