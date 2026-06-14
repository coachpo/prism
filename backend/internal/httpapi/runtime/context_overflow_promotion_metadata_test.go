package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
		EstimationStatus:            runtimeContextEstimationStatusPresent,
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

func TestPreDispatchPromotionMetadataIncludesRecursiveChain(t *testing.T) {
	requestedModelID := "client-requested-model"
	finalModelID := "final-promoted-model"
	finalTerminalTargetID := intPtr(303)
	promotion := &runtimeContextOverflowPromotionDecision{
		TriggerPhase:                runtimeContextOverflowPromotionTriggerPhasePreDispatchEstimate,
		TriggerClassifier:           runtimeContextRoutingSkipReasonEstimatedContextExceedsUsableWindow,
		EstimationMode:              runtimeContextOverflowPromotionEstimationModeEstimated,
		EstimationStatus:            runtimeContextEstimationStatusPresent,
		EstimatedInputTokens:        intPtr(1200),
		ReservedOutputTokens:        intPtr(4000),
		EstimatedTotalContextTokens: intPtr(5200),
		ToResolvedTargetModelID:     stringPtr(finalModelID),
		ToSelectedTerminalTargetID:  finalTerminalTargetID,
		SourceAttemptCount:          0,
		FinalAttemptCount:           1,
		Result:                      runtimeContextOverflowPromotionResultPromotedSuccess,
	}
	contextRouting := runtimeContextRoutingWithRecursivePlannerMetadata(&runtimeContextRoutingDecision{ContextOverflowPromotion: promotion}, &runtimeRecursiveContextOverflowPlannerResult{PromotionChain: []string{requestedModelID, "middle-promoted-model", finalModelID}, Depth: 2, Promoted: true}, stringPtr(finalModelID), finalTerminalTargetID, &requestContextEstimation{Method: openAIChatContextEstimationMethod, EstimatedInputTokens: 1200, ReservedOutputTokens: 4000, EstimatedTotalContextTokens: 5200}, nil)
	plan := requestPlan{RequestedModelID: requestedModelID, ContextRouting: contextRouting}
	if plan.RequestedModelID != requestedModelID {
		t.Fatalf("expected requested model id to remain %q, got %q", requestedModelID, plan.RequestedModelID)
	}
	got := plan.ContextRouting.ContextOverflowPromotion
	if got == nil {
		t.Fatal("expected recursive pre-dispatch promotion metadata")
	}
	if got.TriggerPhase != runtimeContextOverflowPromotionTriggerPhasePreDispatchEstimate || got.EstimationMode != runtimeContextOverflowPromotionEstimationModeEstimated {
		t.Fatalf("expected pre-dispatch estimate metadata, got %+v", got)
	}
	if len(got.PromotionChain) != 3 || got.PromotionChain[0] != requestedModelID || got.PromotionChain[2] != finalModelID || got.PromotionDepth == nil || *got.PromotionDepth != 2 {
		t.Fatalf("expected recursive promotion chain/depth, got %+v", got)
	}
	if dereferenceString(got.FinalResolvedTargetModelID) != finalModelID || intValue(got.FinalSelectedTerminalTargetID) != 303 {
		t.Fatalf("expected additive final selected model/terminal metadata, got %+v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal recursive promotion metadata: %v", err)
	}
	if string(encoded) == "" || containsAnyRuntimeSensitiveFragment(string(encoded), []string{"secret prompt", "raw request body"}) {
		t.Fatalf("recursive promotion metadata leaked sensitive payload content: %s", encoded)
	}
}

func containsAnyRuntimeSensitiveFragment(payload string, fragments []string) bool {
	for _, fragment := range fragments {
		if strings.Contains(payload, fragment) {
			return true
		}
	}
	return false
}

func TestRuntimeTraceContextOverflowPromotionRecursiveChain(t *testing.T) {
	promotion := &runtimeContextOverflowPromotionDecision{
		TriggerStatus:                 http.StatusBadRequest,
		TriggerPhase:                  runtimeContextOverflowPromotionTriggerPhasePreDispatchEstimate,
		TriggerClassifier:             runtimeContextRoutingSkipReasonEstimatedContextExceedsUsableWindow,
		EstimationMode:                runtimeContextOverflowPromotionEstimationModeEstimated,
		EstimationStatus:              runtimeContextEstimationStatusPresent,
		EstimationMethod:              stringPtr(openAIChatContextEstimationMethod),
		FromResolvedTargetModelID:     stringPtr("recursive-source-model"),
		FromSelectedTerminalTargetID:  intPtr(101),
		ToResolvedTargetModelID:       stringPtr("recursive-final-model"),
		ToSelectedTerminalTargetID:    intPtr(202),
		FinalResolvedTargetModelID:    stringPtr("recursive-final-model"),
		FinalSelectedTerminalTargetID: intPtr(202),
		PromotionChain:                []string{"recursive-source-model", "recursive-middle-model", "recursive-final-model"},
		PromotionDepth:                intPtr(2),
		FromUsableContextWindowTokens: intPtr(250000),
		ToUsableContextWindowTokens:   intPtr(500000),
		SourceAttemptCount:            0,
		FinalAttemptCount:             1,
		Result:                        runtimeContextOverflowPromotionResultPromotedSuccess,
	}
	attrs := attributesByKey(runtimeTraceContextOverflowPromotionAttributes(&runtimeContextRoutingDecision{ContextOverflowPromotion: promotion}))
	if !attrs[runtimeTraceAttrContextOverflowPromotion].AsBool() || attrs[runtimeTraceAttrContextOverflowPromotionPromotionDepth].AsInt64() != 2 || attrs[runtimeTraceAttrContextOverflowPromotionFinalSelectedTerminalTargetID].AsInt64() != 202 || attrs[runtimeTraceAttrContextOverflowPromotionFromSelectedTerminalTargetID].AsInt64() != 101 || attrs[runtimeTraceAttrContextOverflowPromotionToSelectedTerminalTargetID].AsInt64() != 202 || attrs[runtimeTraceAttrContextOverflowPromotionFromUsableContextWindowTokens].AsInt64() != 250000 || attrs[runtimeTraceAttrContextOverflowPromotionToUsableContextWindowTokens].AsInt64() != 500000 {
		t.Fatalf("expected recursive promotion trace attrs, got %+v", attrs)
	}
	if attrs[runtimeTraceAttrContextOverflowPromotionFromModelID].AsString() != "redacted_model" || attrs[runtimeTraceAttrContextOverflowPromotionToModelID].AsString() != "redacted_model" || attrs[runtimeTraceAttrContextOverflowPromotionFinalResolvedTargetModelID].AsString() != "redacted_model" {
		t.Fatalf("expected redacted recursive trace model attrs, got %+v", attrs)
	}
	chain := attrs[runtimeTraceAttrContextOverflowPromotionPromotionChain].AsStringSlice()
	if len(chain) != 3 || chain[0] != "redacted_model" || chain[1] != "redacted_model" || chain[2] != "redacted_model" {
		t.Fatalf("expected redacted recursive trace promotion_chain, got %+v", attrs)
	}
	attrPayload := fmt.Sprint(attrs)
	if containsAnyRuntimeSensitiveFragment(attrPayload, []string{"recursive-source-model", "recursive-middle-model", "recursive-final-model", "secret prompt", "raw request body"}) {
		t.Fatalf("recursive trace attrs leaked sensitive payload content: %+v", attrs)
	}
}

func TestRuntimeTraceContextOverflowPromotion(t *testing.T) {
	promotion := &runtimeContextOverflowPromotionDecision{
		TriggerStatus:               http.StatusBadRequest,
		TriggerPhase:                runtimeContextOverflowPromotionTriggerPhaseProviderOverflow,
		TriggerClassifier:           cliProxyAPIOverflowClassifierErrorCode,
		EstimationMode:              runtimeContextOverflowPromotionEstimationModeEstimated,
		EstimationStatus:            runtimeContextEstimationStatusPresent,
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
