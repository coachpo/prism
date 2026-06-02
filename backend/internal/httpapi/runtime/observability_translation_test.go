package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestObservability_FacadeTranslatedRejectionPersistsDecisionMetadataAndTraceAttributes(t *testing.T) {
	startedAt := time.Date(2026, 6, 2, 9, 45, 0, 0, time.UTC)
	completedAt := startedAt.Add(350 * time.Millisecond)
	service := &Service{now: func() time.Time { return completedAt }}
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	selectedTerminalTargetID := intPtr(101)
	facadeSelection := &runtimeFacadeSelectionDecision{
		FacadeModelID:         "facade-translated-public",
		SelectedTargetModelID: stringPtr("facade-translated-target"),
		SelectedWeight:        intPtr(1),
		EligibleTotalWeight:   intPtr(0),
		ExclusionReasons:      []runtimeFacadeExclusionReason{{Reason: runtimeFacadeExclusionReasonTranslationRejection, Count: 1}},
		ExclusionSummary:      stringPtr("translation_rejection=1"),
	}
	contextRouting := &runtimeContextRoutingDecision{
		Policy:                      runtimeFacadeSelectionPolicyWeightedEligibleContext,
		SelectedTerminalTargetID:    selectedTerminalTargetID,
		SelectedEndpointID:          intPtr(201),
		SelectedContextBand:         stringPtr(runtimeContextBandPreferred),
		EstimatedInputTokens:        intPtr(6),
		ReservedOutputTokens:        intPtr(64),
		EstimatedTotalContextTokens: intPtr(70),
		FacadeSelection:             facadeSelection,
	}
	failure := runtimePlanningFailureTelemetry{
		ProfileID:                10,
		RequestedModelID:         "facade-translated-public",
		RequestedVendorID:        intPtr(1),
		RequestedVendorKey:       stringPtr("openai"),
		RequestedVendorName:      stringPtr("OpenAI"),
		APIFamily:                "openai",
		RuntimeOperation:         RuntimeOperation{Name: "openai.responses", PathTemplate: "/v1/responses"},
		UpstreamOperationName:    stringPtr(openAIUpstreamOperationChatCompletions),
		RequestPath:              "/v1/responses",
		UpstreamRequestPath:      stringPtr("/v1/chat/completions"),
		OperationTranslationMode: stringPtr(string(TranslationModeOpenAIResponsesToChatCompletions)),
		ReportCurrencySnapshot:   runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"},
		ContextRouting:           contextRouting,
	}
	runtimeErr := &domainError{StatusCode: http.StatusBadRequest, ErrorCode: openAIRequestTranslationUnsupportedErrorCode, Detail: "translated rejection", PlanningFailure: &failure, SelectedTerminalTargetID: selectedTerminalTargetID, ContextRouting: contextRouting}

	envelope := service.buildRuntimePlanningFailureTelemetryEnvelope(failure, request, startedAt, runtimeErr)
	assertRuntimeFacadeSelectionDecision(t, envelope.RequestLogs[0].ContextRouting.FacadeSelection, "facade-translated-public", stringPtr("facade-translated-target"), intPtr(1), intPtr(0), stringPtr("translation_rejection=1"))
	assertRuntimeFacadeSelectionDecision(t, envelope.UsageEvent.ContextRouting.FacadeSelection, "facade-translated-public", stringPtr("facade-translated-target"), intPtr(1), intPtr(0), stringPtr("translation_rejection=1"))

	planningFailureAttrs := attributesByKey(runtimeTracePlanningFailureAttributes(failure))
	if planningFailureAttrs[runtimeTraceAttrFacadeModelID].AsString() != "facade-translated-public" || planningFailureAttrs[runtimeTraceAttrFacadeSelectedTargetModel].AsString() != "facade-translated-target" || planningFailureAttrs[runtimeTraceAttrFacadeSelectedWeight].AsInt64() != 1 || planningFailureAttrs[runtimeTraceAttrFacadeEligibleTotalWeight].AsInt64() != 0 || planningFailureAttrs[runtimeTraceAttrFacadeExclusionSummary].AsString() != "translation_rejection=1" {
		t.Fatalf("expected facade translated rejection trace attrs, got %+v", planningFailureAttrs)
	}
}

func TestObservability_TranslatedResponseAuditUsesUpstreamBody(t *testing.T) {
	startedAt := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(900 * time.Millisecond)
	service := &Service{now: func() time.Time { return completedAt }}
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	endpointName := "translated endpoint"
	resolvedModelID := "chat-target-model"
	connection := runtimeConnection{ID: 11, Endpoint: runtimeEndpoint{ID: 101, BaseURL: "https://translated.example", Name: &endpointName}, MaxContextUtilization: 1.0}
	selectedTerminalTargetID := intPtr(connection.ID)
	plan := requestPlan{
		ProfileID:                   7,
		RequestedModelID:            "public-model",
		ResolvedTargetModelID:       &resolvedModelID,
		RequestedVendorID:           intPtr(1),
		RequestedVendorKey:          stringPtr("openai"),
		RequestedVendorName:         stringPtr("OpenAI"),
		APIFamily:                   "openai",
		RuntimeOperation:            RuntimeOperation{Name: "openai.responses", PathTemplate: "/v1/responses"},
		AuditEnabledAtRequest:       true,
		AuditCaptureBodiesAtRequest: true,
		SelectedTerminalTargetID:    selectedTerminalTargetID,
		ContextRouting: &runtimeContextRoutingDecision{
			Policy:                            "cheapest_eligible_context",
			SelectedTerminalTargetID:          selectedTerminalTargetID,
			SelectedEndpointID:                intPtr(connection.Endpoint.ID),
			SelectedContextBand:               stringPtr(runtimeContextBandPreferred),
			SelectedUsableContextWindowTokens: intPtr(8192),
			EstimationMethod:                  stringPtr(openAIResponsesContextEstimationMethod),
			EstimatedInputTokens:              intPtr(6),
			ReservedOutputTokens:              intPtr(64),
			EstimatedTotalContextTokens:       intPtr(70),
			UsableContextWindowTokens:         intPtr(8192),
			SkippedTerminalTargets: []runtimeContextRoutingSkippedTerminalTarget{{
				TerminalTargetID:            intPtr(12),
				EndpointID:                  intPtr(102),
				ContextBand:                 stringPtr(runtimeContextBandIneligible),
				Reason:                      runtimeContextRoutingSkipReasonEstimatedContextExceedsUsableWindow,
				UsableContextWindowTokens:   intPtr(32),
				EstimatedTotalContextTokens: intPtr(70),
			}},
		},
		UpstreamBody: []byte(`{"model":"chat-target-model","messages":[{"role":"user","content":"hello"}]}`),
	}
	result := executionResult{
		Response:                    &http.Response{StatusCode: http.StatusOK},
		Connection:                  connection,
		ResolvedTargetModelID:       &resolvedModelID,
		AuditEnabledAtRequest:       true,
		AuditCaptureBodiesAtRequest: true,
		Attempts: []executionAttempt{{
			Connection:                  connection,
			ResolvedTargetModelID:       resolvedModelID,
			RequestURL:                  "https://translated.example/v1/chat/completions",
			RequestHeaders:              map[string]string{"User-Agent": "translated-upstream"},
			RequestBody:                 plan.UpstreamBody,
			ResponseHeaders:             http.Header{"X-Request-Id": []string{"translated-upstream-id"}},
			StatusCode:                  http.StatusOK,
			ResponseTimeMS:              900,
			CompletedAt:                 completedAt,
			AuditEnabledAtRequest:       true,
			AuditCaptureBodiesAtRequest: true,
			UpstreamOperationName:       openAIUpstreamOperationChatCompletions,
			UpstreamRequestPath:         "/v1/chat/completions",
			OperationTranslationMode:    TranslationModeOpenAIResponsesToChatCompletions,
		}},
	}
	capture := runtimeResponseCapture{
		Body:          []byte(`{"id":"resp_client","object":"response","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}]}`),
		AuditBody:     []byte(`{"id":"chatcmpl_upstream","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`),
		Usage:         generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
		CompletedAt:   &completedAt,
		StreamOutcome: runtimeStreamOutcomeNotStreaming,
	}

	envelope := service.buildRuntimeTelemetryEnvelope(plan, result, request, startedAt, capture)
	if len(envelope.RequestLogs) != 1 || len(envelope.AuditLogs) != 1 {
		t.Fatalf("expected one translated request-log and audit row, got request=%d audit=%d", len(envelope.RequestLogs), len(envelope.AuditLogs))
	}
	requestLog := envelope.RequestLogs[0]
	assertRuntimeStringPtr(t, requestLog.UpstreamOperationName, openAIUpstreamOperationChatCompletions, "translated request-log upstream operation")
	assertRuntimeStringPtr(t, requestLog.OperationTranslationMode, string(TranslationModeOpenAIResponsesToChatCompletions), "translated request-log translation mode")
	assertRuntimeStringPtr(t, requestLog.UpstreamRequestPath, "/v1/chat/completions", "translated request-log upstream request path")
	if requestLog.ContextRouting == nil || requestLog.ContextRouting.SelectedContextBand == nil || *requestLog.ContextRouting.SelectedContextBand != runtimeContextBandPreferred {
		t.Fatalf("expected translated request-log selected context band to persist, got %+v", requestLog.ContextRouting)
	}
	if len(requestLog.ContextRouting.SkippedTerminalTargets) != 1 || requestLog.ContextRouting.SkippedTerminalTargets[0].ContextBand == nil || *requestLog.ContextRouting.SkippedTerminalTargets[0].ContextBand != runtimeContextBandIneligible {
		t.Fatalf("expected translated request-log skipped target context band to persist, got %+v", requestLog.ContextRouting)
	}
	assertRuntimeStringPtr(t, envelope.UsageEvent.UpstreamOperationName, openAIUpstreamOperationChatCompletions, "translated usage-event upstream operation")
	assertRuntimeStringPtr(t, envelope.UsageEvent.OperationTranslationMode, string(TranslationModeOpenAIResponsesToChatCompletions), "translated usage-event translation mode")
	assertRuntimeStringPtr(t, envelope.UsageEvent.UpstreamRequestPath, "/v1/chat/completions", "translated usage-event upstream request path")
	finalAudit := envelope.AuditLogs[0]
	assertRuntimeStringPtr(t, finalAudit.ResponseBody, string(capture.AuditBody), "translated audit response body")
	if finalAudit.ResponseBody != nil && *finalAudit.ResponseBody == string(capture.Body) {
		t.Fatalf("expected translated client body to stay out of audit storage, got %+v", finalAudit)
	}
}
