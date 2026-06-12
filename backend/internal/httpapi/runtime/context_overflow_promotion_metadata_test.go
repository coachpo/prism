package runtime

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestProviderOverflowPromotionMetadataIncludesTriggerPhase(t *testing.T) {
	sourceResolvedTargetModelID := "source-model"
	promotedResolvedTargetModelID := "promoted-model"
	sourceSelectedTerminalTargetID := intPtr(101)
	promotedSelectedTerminalTargetID := intPtr(202)
	sourcePlan := requestPlan{
		RequestedModelID:         "public-model",
		ResolvedTargetModelID:    &sourceResolvedTargetModelID,
		RequestContextEstimation: &requestContextEstimation{Method: openAIChatContextEstimationMethod, EstimatedInputTokens: 900, ReservedOutputTokens: 2048, EstimatedTotalContextTokens: 2948, UsableContextWindowTokens: intPtr(250000)},
		SelectedTerminalTargetID: sourceSelectedTerminalTargetID,
		ContextRouting:           &runtimeContextRoutingDecision{Policy: "cheapest_eligible_context", SelectedTerminalTargetID: sourceSelectedTerminalTargetID, SelectedUsableContextWindowTokens: intPtr(250000), UsableContextWindowTokens: intPtr(250000)},
	}
	promotedPlan := requestPlan{
		ResolvedTargetModelID:    &promotedResolvedTargetModelID,
		SelectedTerminalTargetID: promotedSelectedTerminalTargetID,
		ContextRouting:           &runtimeContextRoutingDecision{Policy: "cheapest_eligible_context", SelectedTerminalTargetID: promotedSelectedTerminalTargetID, SelectedUsableContextWindowTokens: intPtr(500000), UsableContextWindowTokens: intPtr(500000)},
	}
	sourceExecution := executionResult{Response: &http.Response{StatusCode: http.StatusBadRequest}, ResolvedTargetModelID: &sourceResolvedTargetModelID, AttemptCount: 1, Attempts: []executionAttempt{{ResolvedTargetModelID: sourceResolvedTargetModelID}}}
	promotedExecution := executionResult{Response: &http.Response{StatusCode: http.StatusOK}, ResolvedTargetModelID: &promotedResolvedTargetModelID, AttemptCount: 1, Attempts: []executionAttempt{{ResolvedTargetModelID: promotedResolvedTargetModelID}}}
	promotion := buildContextOverflowPromotionDecision(sourcePlan, sourceExecution, promotedPlan, promotedExecution, cliProxyAPIOverflowClassification{Promotable: true, ErrorCode: "context_length_exceeded", Classifier: cliProxyAPIOverflowClassifierErrorCode})
	if promotion == nil {
		t.Fatal("expected promotion decision, got nil")
	}
	if promotion.TriggerPhase != runtimeContextOverflowPromotionTriggerPhaseProviderOverflow {
		t.Fatalf("expected trigger_phase=%q, got %+v", runtimeContextOverflowPromotionTriggerPhaseProviderOverflow, promotion)
	}
	assertRuntimeContextOverflowPromotionDecision(t, promotion, http.StatusBadRequest, "context_length_exceeded", cliProxyAPIOverflowClassifierErrorCode, runtimeContextOverflowPromotionTriggerPhaseProviderOverflow, runtimeContextOverflowPromotionEstimationModeEstimated, openAIChatContextEstimationMethod, 900, 2048, 2948, sourceResolvedTargetModelID, 101, promotedResolvedTargetModelID, 202, 250000, 500000, 1, 2, runtimeContextOverflowPromotionResultPromotedSuccess)
}

func TestPreDispatchPromotionMetadataIncludesEstimateFields(t *testing.T) {
	promotion := &runtimeContextOverflowPromotionDecision{
		TriggerStatus:               http.StatusBadRequest,
		TriggerPhase:                runtimeContextOverflowPromotionTriggerPhasePreDispatchEstimate,
		TriggerClassifier:           cliProxyAPIOverflowClassifierErrorCode,
		EstimationMode:              runtimeContextOverflowPromotionEstimationModeEstimated,
		EstimationMethod:            stringPtr(openAIChatContextEstimationMethod),
		EstimatedInputTokens:        intPtr(1200),
		ReservedOutputTokens:        intPtr(4000),
		EstimatedTotalContextTokens: intPtr(5200),
		SourceAttemptCount:          0,
		FinalAttemptCount:           1,
		Result:                      runtimeContextOverflowPromotionResultPromotedSuccess,
	}
	encoded, err := json.Marshal(promotion)
	if err != nil {
		t.Fatalf("marshal promotion: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal promotion: %v", err)
	}
	if payload["trigger_phase"] != runtimeContextOverflowPromotionTriggerPhasePreDispatchEstimate || payload["estimation_method"] != openAIChatContextEstimationMethod || int(payload["estimated_input_tokens"].(float64)) != 1200 || int(payload["reserved_output_tokens"].(float64)) != 4000 || int(payload["estimated_total_context_tokens"].(float64)) != 5200 || int(payload["source_attempt_count"].(float64)) != 0 || int(payload["final_attempt_count"].(float64)) != 1 {
		t.Fatalf("expected serialized pre-dispatch promotion estimate fields, got %s", encoded)
	}
}

func TestRuntimeTraceContextOverflowPromotion(t *testing.T) {
	promotion := &runtimeContextOverflowPromotionDecision{
		TriggerStatus:               http.StatusBadRequest,
		TriggerPhase:                runtimeContextOverflowPromotionTriggerPhaseProviderOverflow,
		TriggerClassifier:           cliProxyAPIOverflowClassifierErrorCode,
		EstimationMode:              runtimeContextOverflowPromotionEstimationModeEstimated,
		EstimationMethod:            stringPtr(openAIChatContextEstimationMethod),
		EstimatedInputTokens:        intPtr(900),
		ReservedOutputTokens:        intPtr(2048),
		EstimatedTotalContextTokens: intPtr(2948),
		SourceAttemptCount:          1,
		FinalAttemptCount:           2,
		Result:                      runtimeContextOverflowPromotionResultPromotedSuccess,
	}
	attrs := attributesByKey(runtimeTraceContextOverflowPromotionAttributes(&runtimeContextRoutingDecision{ContextOverflowPromotion: promotion}))
	if !attrs[runtimeTraceAttrContextOverflowPromotion].AsBool() || attrs[runtimeTraceAttrContextOverflowPromotionTriggerPhase].AsString() != runtimeContextOverflowPromotionTriggerPhaseProviderOverflow || attrs[runtimeTraceAttrContextOverflowPromotionEstimationMode].AsString() != runtimeContextOverflowPromotionEstimationModeEstimated || attrs[runtimeTraceAttrContextOverflowPromotionEstimationMethod].AsString() != openAIChatContextEstimationMethod || attrs[runtimeTraceAttrContextOverflowPromotionEstimatedInputTokens].AsInt64() != 900 || attrs[runtimeTraceAttrContextOverflowPromotionReservedOutputTokens].AsInt64() != 2048 || attrs[runtimeTraceAttrContextOverflowPromotionEstimatedTotalContextTokens].AsInt64() != 2948 {
		t.Fatalf("expected promotion trace attrs with phase and estimates, got %+v", attrs)
	}
}
