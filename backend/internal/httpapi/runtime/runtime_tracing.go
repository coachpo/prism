package runtime

import (
	"context"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	gatewayaccounting "github.com/coachpo/prism/backend/internal/gateway/accounting"
)

const (
	runtimeTraceAttrOperationName                                         = "prism.operation_name"
	runtimeTraceAttrUpstreamOperationName                                 = "prism.upstream_operation_name"
	runtimeTraceAttrOperationTranslationMode                              = "prism.operation_translation_mode"
	runtimeTraceAttrUpstreamRequestPath                                   = "prism.upstream_request_path"
	runtimeTraceAttrPreferredContextBand                                  = "prism.preferred_context_band"
	runtimeTraceAttrSelectedTerminalTargetID                              = "prism.selected_terminal_target_id"
	runtimeTraceAttrContextOverflowPromotion                              = "prism.context_overflow_promotion"
	runtimeTraceAttrContextOverflowPromotionFromModelID                   = "prism.context_overflow_promotion.from_model_id"
	runtimeTraceAttrContextOverflowPromotionToModelID                     = "prism.context_overflow_promotion.to_model_id"
	runtimeTraceAttrContextOverflowPromotionTriggerStatus                 = "prism.context_overflow_promotion.trigger_status"
	runtimeTraceAttrContextOverflowPromotionTriggerPhase                  = "prism.context_overflow_promotion.trigger_phase"
	runtimeTraceAttrContextOverflowPromotionTriggerCode                   = "prism.context_overflow_promotion.trigger_code"
	runtimeTraceAttrContextOverflowPromotionTriggerClassifier             = "prism.context_overflow_promotion.trigger_classifier"
	runtimeTraceAttrContextOverflowPromotionEstimationMode                = "prism.context_overflow_promotion.estimation_mode"
	runtimeTraceAttrContextOverflowPromotionEstimationMethod              = "prism.context_overflow_promotion.estimation_method"
	runtimeTraceAttrContextOverflowPromotionEstimatedInputTokens          = "prism.context_overflow_promotion.estimated_input_tokens"
	runtimeTraceAttrContextOverflowPromotionReservedOutputTokens          = "prism.context_overflow_promotion.reserved_output_tokens"
	runtimeTraceAttrContextOverflowPromotionEstimatedTotalContextTokens   = "prism.context_overflow_promotion.estimated_total_context_tokens"
	runtimeTraceAttrContextOverflowPromotionFromSelectedTerminalTargetID  = "prism.context_overflow_promotion.from_selected_terminal_target_id"
	runtimeTraceAttrContextOverflowPromotionToSelectedTerminalTargetID    = "prism.context_overflow_promotion.to_selected_terminal_target_id"
	runtimeTraceAttrContextOverflowPromotionFromUsableContextWindowTokens = "prism.context_overflow_promotion.from_usable_context_window_tokens"
	runtimeTraceAttrContextOverflowPromotionToUsableContextWindowTokens   = "prism.context_overflow_promotion.to_usable_context_window_tokens"
	runtimeTraceAttrContextOverflowPromotionSourceAttemptCount            = "prism.context_overflow_promotion.source_attempt_count"
	runtimeTraceAttrContextOverflowPromotionFinalAttemptCount             = "prism.context_overflow_promotion.final_attempt_count"
	runtimeTraceAttrContextOverflowPromotionResult                        = "prism.context_overflow_promotion.result"
	runtimeTraceAttrContextOverflowAffinityState                          = "prism.context_overflow_affinity.state"
	runtimeTraceAttrContextOverflowAffinityHashPrefix                     = "prism.context_overflow_affinity.affinity_hash_prefix"
	runtimeTraceAttrContextOverflowAffinityParentHashPrefix               = "prism.context_overflow_affinity.parent_hash_prefix"
	runtimeTraceAttrContextOverflowAffinityContextBucket                  = "prism.context_overflow_affinity.context_bucket"
	runtimeTraceAttrContextOverflowAffinitySourceModelID                  = "prism.context_overflow_affinity.source_model_id"
	runtimeTraceAttrContextOverflowAffinityPromotionTargetModelID         = "prism.context_overflow_affinity.promotion_target_model_id"
	runtimeTraceAttrContextOverflowAffinityRejectionReason                = "prism.context_overflow_affinity.rejection_reason"
	runtimeTraceAttrFacadeModelID                                         = "prism.facade_model_id"
	runtimeTraceAttrFacadeSelectedTargetModel                             = "prism.facade_selected_target_model_id"
	runtimeTraceAttrFacadeSelectedWeight                                  = "prism.facade_selected_weight"
	runtimeTraceAttrFacadeEligibleTotalWeight                             = "prism.facade_eligible_total_weight"
	runtimeTraceAttrFacadeExclusionSummary                                = "prism.facade_exclusion_summary"
	runtimeTraceAttrPlannerVersion                                        = "prism.planner_version"
	runtimeTraceAttrPlannerDecision                                       = "prism.planner_decision"
	runtimeTraceAttrPlannerPolicy                                         = "prism.planner_policy"
	runtimeTraceAttrPlannerSelectedTier                                   = "prism.planner_selected_tier_priority"
	runtimeTraceAttrPlannerSkippedTargets                                 = "prism.planner_skipped_terminal_targets"
	runtimeTraceAttrAPIFamily                                             = "prism.api_family"
	runtimeTraceAttrStreaming                                             = "prism.streaming"
	runtimeTraceAttrStatusClass                                           = "prism.status_class"
	runtimeTraceAttrStreamOutcome                                         = "prism.stream_outcome"
	runtimeTraceAttrRouteReason                                           = "prism.route_reason"
	runtimeTraceAttrUsageSource                                           = "prism.usage_source"
	runtimeTraceAttrPricingConfigVersionUsed                              = "prism.pricing_config_version_used"
	runtimeTraceAttrBodyMode                                              = "prism.body_mode"
	runtimeTraceAttrAttemptResult                                         = "prism.attempt_result"
	runtimeTraceAttrFeedbackKind                                          = "prism.feedback_kind"
	runtimeTraceAttrEnqueueStatus                                         = "prism.enqueue_status"
	runtimeTraceAttrHTTPMethod                                            = "http.request.method"
	runtimeTraceAttrHTTPStatus                                            = "http.response.status_code"

	runtimeTraceValueUnknown  = "unknown"
	runtimeTraceBodyStreaming = "streaming"
	runtimeTraceBodyBuffered  = "buffered"
	runtimeTraceBodyEmpty     = "empty"
)

type runtimeTraceContext struct {
	TraceParent string `json:"trace_parent,omitempty"`
	TraceState  string `json:"trace_state,omitempty"`
}

type runtimeTraceAttributePolicy struct{}

var runtimeTracePolicy runtimeTraceAttributePolicy

func startRuntimeSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	return otel.Tracer(runtimeMetricScopeName).Start(ctx, name, trace.WithAttributes(attrs...))
}

func startRuntimeClientSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	return otel.Tracer(runtimeMetricScopeName).Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
}

func runtimeTraceMarkError(span trace.Span, reason string) {
	if span == nil {
		return
	}
	span.SetStatus(codes.Error, runtimeTracePolicy.errorReason(reason))
}

func runtimeTraceContextFromContext(ctx context.Context) runtimeTraceContext {
	if ctx == nil || !trace.SpanContextFromContext(ctx).IsValid() {
		return runtimeTraceContext{}
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return runtimeTraceContext{TraceParent: carrier["traceparent"], TraceState: carrier["tracestate"]}
}

func (traceContext runtimeTraceContext) empty() bool {
	return strings.TrimSpace(traceContext.TraceParent) == "" && strings.TrimSpace(traceContext.TraceState) == ""
}

func (traceContext runtimeTraceContext) context(parent context.Context) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	if traceContext.empty() {
		return parent
	}
	carrier := propagation.MapCarrier{}
	if strings.TrimSpace(traceContext.TraceParent) != "" {
		carrier["traceparent"] = strings.TrimSpace(traceContext.TraceParent)
	}
	if strings.TrimSpace(traceContext.TraceState) != "" {
		carrier["tracestate"] = strings.TrimSpace(traceContext.TraceState)
	}
	return otel.GetTextMapPropagator().Extract(parent, carrier)
}

func runtimeTraceDetachedContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func runtimeTraceOperationAttributes(operation RuntimeOperation) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(runtimeTraceAttrOperationName, runtimeMetricPolicy.operationName(operation.Name)),
		attribute.String(runtimeTraceAttrAPIFamily, runtimeTracePolicy.apiFamily(operation.APIFamily)),
	}
}

func runtimeTraceAttemptAttributionAttributes(operation RuntimeOperation, translationMode TranslationMode, contextRouting *runtimeContextRoutingDecision) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(runtimeTraceAttrUpstreamOperationName, runtimeMetricPolicy.operationName(runtimeUpstreamOperationName(operation, translationMode))),
		attribute.String(runtimeTraceAttrOperationTranslationMode, runtimeTracePolicy.translationMode(string(normalizedRuntimeTranslationMode(translationMode)))),
		attribute.String(runtimeTraceAttrUpstreamRequestPath, runtimeTracePolicy.requestPath(runtimeUpstreamRequestPathTemplate(operation, translationMode))),
	}
	if contextRouting != nil {
		if contextRouting.SelectedContextBand != nil {
			attrs = append(attrs, attribute.String(runtimeTraceAttrPreferredContextBand, runtimeTracePolicy.preferredContextBand(*contextRouting.SelectedContextBand)))
		}
		if contextRouting.SelectedTerminalTargetID != nil {
			attrs = append(attrs, attribute.Int(runtimeTraceAttrSelectedTerminalTargetID, *contextRouting.SelectedTerminalTargetID))
		}
		attrs = append(attrs, runtimeTraceContextOverflowPromotionAttributes(contextRouting)...)
		attrs = append(attrs, runtimeTraceContextOverflowAffinityAttributes(contextRouting)...)
		attrs = append(attrs, runtimeTraceFacadeSelectionAttributes(contextRouting)...)
		attrs = append(attrs, runtimeTracePlannerTraceAttributes(contextRouting.PlannerTrace)...)
	}
	return attrs
}

func runtimeTraceFacadeSelectionAttributes(contextRouting *runtimeContextRoutingDecision) []attribute.KeyValue {
	if contextRouting == nil || contextRouting.FacadeSelection == nil {
		return nil
	}
	facadeSelection := contextRouting.FacadeSelection
	attrs := []attribute.KeyValue{attribute.String(runtimeTraceAttrFacadeModelID, strings.TrimSpace(facadeSelection.FacadeModelID))}
	if facadeSelection.SelectedTargetModelID != nil && strings.TrimSpace(*facadeSelection.SelectedTargetModelID) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrFacadeSelectedTargetModel, strings.TrimSpace(*facadeSelection.SelectedTargetModelID)))
	}
	if facadeSelection.SelectedWeight != nil {
		attrs = append(attrs, attribute.Int(runtimeTraceAttrFacadeSelectedWeight, *facadeSelection.SelectedWeight))
	}
	if facadeSelection.EligibleTotalWeight != nil {
		attrs = append(attrs, attribute.Int(runtimeTraceAttrFacadeEligibleTotalWeight, *facadeSelection.EligibleTotalWeight))
	}
	if facadeSelection.ExclusionSummary != nil && strings.TrimSpace(*facadeSelection.ExclusionSummary) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrFacadeExclusionSummary, strings.TrimSpace(*facadeSelection.ExclusionSummary)))
	}
	return attrs
}

func runtimeTraceContextOverflowPromotionAttributes(contextRouting *runtimeContextRoutingDecision) []attribute.KeyValue {
	if contextRouting == nil || contextRouting.ContextOverflowPromotion == nil {
		return nil
	}
	promotion := contextRouting.ContextOverflowPromotion
	attrs := []attribute.KeyValue{
		attribute.Bool(runtimeTraceAttrContextOverflowPromotion, true),
		attribute.Int(runtimeTraceAttrContextOverflowPromotionTriggerStatus, promotion.TriggerStatus),
		attribute.String(runtimeTraceAttrContextOverflowPromotionTriggerPhase, runtimeTracePolicy.contextOverflowPromotionTriggerPhase(promotion.TriggerPhase)),
		attribute.Int(runtimeTraceAttrContextOverflowPromotionSourceAttemptCount, promotion.SourceAttemptCount),
		attribute.Int(runtimeTraceAttrContextOverflowPromotionFinalAttemptCount, promotion.FinalAttemptCount),
		attribute.String(runtimeTraceAttrContextOverflowPromotionResult, runtimeTracePolicy.contextOverflowPromotionResult(promotion.Result)),
	}
	if strings.TrimSpace(promotion.TriggerClassifier) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrContextOverflowPromotionTriggerClassifier, runtimeTracePolicy.contextOverflowPromotionClassifier(promotion.TriggerClassifier)))
	}
	if promotion.TriggerErrorCode != nil && strings.TrimSpace(*promotion.TriggerErrorCode) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrContextOverflowPromotionTriggerCode, runtimeTracePolicy.contextOverflowPromotionTriggerCode(*promotion.TriggerErrorCode)))
	}
	if strings.TrimSpace(promotion.EstimationMode) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrContextOverflowPromotionEstimationMode, runtimeTracePolicy.contextOverflowPromotionEstimationMode(promotion.EstimationMode)))
	}
	if promotion.EstimationMethod != nil && strings.TrimSpace(*promotion.EstimationMethod) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrContextOverflowPromotionEstimationMethod, strings.TrimSpace(*promotion.EstimationMethod)))
	}
	if promotion.EstimatedInputTokens != nil {
		attrs = append(attrs, attribute.Int(runtimeTraceAttrContextOverflowPromotionEstimatedInputTokens, *promotion.EstimatedInputTokens))
	}
	if promotion.ReservedOutputTokens != nil {
		attrs = append(attrs, attribute.Int(runtimeTraceAttrContextOverflowPromotionReservedOutputTokens, *promotion.ReservedOutputTokens))
	}
	if promotion.EstimatedTotalContextTokens != nil {
		attrs = append(attrs, attribute.Int(runtimeTraceAttrContextOverflowPromotionEstimatedTotalContextTokens, *promotion.EstimatedTotalContextTokens))
	}
	if promotion.FromResolvedTargetModelID != nil && strings.TrimSpace(*promotion.FromResolvedTargetModelID) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrContextOverflowPromotionFromModelID, strings.TrimSpace(*promotion.FromResolvedTargetModelID)))
	}
	if promotion.ToResolvedTargetModelID != nil && strings.TrimSpace(*promotion.ToResolvedTargetModelID) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrContextOverflowPromotionToModelID, strings.TrimSpace(*promotion.ToResolvedTargetModelID)))
	}
	if promotion.FromSelectedTerminalTargetID != nil {
		attrs = append(attrs, attribute.Int(runtimeTraceAttrContextOverflowPromotionFromSelectedTerminalTargetID, *promotion.FromSelectedTerminalTargetID))
	}
	if promotion.ToSelectedTerminalTargetID != nil {
		attrs = append(attrs, attribute.Int(runtimeTraceAttrContextOverflowPromotionToSelectedTerminalTargetID, *promotion.ToSelectedTerminalTargetID))
	}
	if promotion.FromUsableContextWindowTokens != nil {
		attrs = append(attrs, attribute.Int(runtimeTraceAttrContextOverflowPromotionFromUsableContextWindowTokens, *promotion.FromUsableContextWindowTokens))
	}
	if promotion.ToUsableContextWindowTokens != nil {
		attrs = append(attrs, attribute.Int(runtimeTraceAttrContextOverflowPromotionToUsableContextWindowTokens, *promotion.ToUsableContextWindowTokens))
	}
	return attrs
}

func runtimeTraceContextOverflowAffinityAttributes(contextRouting *runtimeContextRoutingDecision) []attribute.KeyValue {
	if contextRouting == nil || contextRouting.ContextOverflowAffinity == nil {
		return nil
	}
	affinity := contextRouting.ContextOverflowAffinity
	attrs := []attribute.KeyValue{attribute.String(runtimeTraceAttrContextOverflowAffinityState, strings.TrimSpace(affinity.State))}
	if strings.TrimSpace(affinity.AffinityHashPrefix) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrContextOverflowAffinityHashPrefix, strings.TrimSpace(affinity.AffinityHashPrefix)))
	}
	if affinity.ParentHashPrefix != nil && strings.TrimSpace(*affinity.ParentHashPrefix) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrContextOverflowAffinityParentHashPrefix, strings.TrimSpace(*affinity.ParentHashPrefix)))
	}
	if strings.TrimSpace(affinity.ContextBucket) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrContextOverflowAffinityContextBucket, strings.TrimSpace(affinity.ContextBucket)))
	}
	if strings.TrimSpace(affinity.SourceModelID) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrContextOverflowAffinitySourceModelID, strings.TrimSpace(affinity.SourceModelID)))
	}
	if strings.TrimSpace(affinity.PromotionTargetModelID) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrContextOverflowAffinityPromotionTargetModelID, strings.TrimSpace(affinity.PromotionTargetModelID)))
	}
	if affinity.RejectionReason != nil && strings.TrimSpace(*affinity.RejectionReason) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrContextOverflowAffinityRejectionReason, strings.TrimSpace(*affinity.RejectionReason)))
	}
	return attrs
}

func runtimeTracePlannerTraceAttributes(plannerTrace *runtimePlannerTraceDecision) []attribute.KeyValue {
	if plannerTrace == nil {
		return nil
	}
	attrs := []attribute.KeyValue{
		attribute.String(runtimeTraceAttrPlannerVersion, runtimeTracePolicy.plannerVersion(plannerTrace.PlannerVersion)),
		attribute.String(runtimeTraceAttrPlannerDecision, runtimeTracePolicy.plannerDecision(plannerTrace.Decision)),
	}
	if strings.TrimSpace(plannerTrace.Policy) != "" {
		attrs = append(attrs, attribute.String(runtimeTraceAttrPlannerPolicy, runtimeTracePolicy.plannerPolicy(plannerTrace.Policy)))
	}
	if plannerTrace.SelectedTierPriority != nil {
		attrs = append(attrs, attribute.Int(runtimeTraceAttrPlannerSelectedTier, *plannerTrace.SelectedTierPriority))
	}
	if plannerTrace.SkippedTerminalTargets > 0 {
		attrs = append(attrs, attribute.Int(runtimeTraceAttrPlannerSkippedTargets, plannerTrace.SkippedTerminalTargets))
	}
	return attrs
}

func runtimeTracePlanAttributes(plan requestPlan) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(runtimeTraceAttrOperationName, runtimeMetricPolicy.operationName(plan.RuntimeOperation.Name)),
		attribute.String(runtimeTraceAttrAPIFamily, runtimeTracePolicy.apiFamily(plan.APIFamily)),
		attribute.Bool(runtimeTraceAttrStreaming, plan.IsStreamingRequest),
	}
	attrs = append(attrs, runtimeTraceAttemptAttributionAttributes(plan.RuntimeOperation, firstTerminalAttempt(plan).TranslationMode, plan.ContextRouting)...)
	return attrs
}

func runtimeTraceAttemptAttributes(plan requestPlan, attempt runtimeTerminalAttempt) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(runtimeTraceAttrOperationName, runtimeMetricPolicy.operationName(plan.RuntimeOperation.Name)),
		attribute.String(runtimeTraceAttrAPIFamily, runtimeTracePolicy.apiFamily(plan.APIFamily)),
		attribute.Bool(runtimeTraceAttrStreaming, plan.IsStreamingRequest),
	}
	attrs = append(attrs, runtimeTraceAttemptAttributionAttributes(plan.RuntimeOperation, attempt.TranslationMode, plan.ContextRouting)...)
	return attrs
}

func runtimeTracePlanningFailureAttributes(failure runtimePlanningFailureTelemetry) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(runtimeTraceAttrOperationName, runtimeMetricPolicy.operationName(failure.RuntimeOperation.Name)),
		attribute.String(runtimeTraceAttrAPIFamily, runtimeTracePolicy.apiFamily(failure.APIFamily)),
		attribute.Bool(runtimeTraceAttrStreaming, failure.IsStreamingRequest),
	}
	if failure.UpstreamOperationName != nil {
		attrs = append(attrs, attribute.String(runtimeTraceAttrUpstreamOperationName, runtimeMetricPolicy.operationName(*failure.UpstreamOperationName)))
	}
	if failure.OperationTranslationMode != nil {
		attrs = append(attrs, attribute.String(runtimeTraceAttrOperationTranslationMode, runtimeTracePolicy.translationMode(*failure.OperationTranslationMode)))
	}
	if failure.UpstreamRequestPath != nil {
		attrs = append(attrs, attribute.String(runtimeTraceAttrUpstreamRequestPath, runtimeTracePolicy.requestPath(*failure.UpstreamRequestPath)))
	}
	if failure.ContextRouting != nil {
		if failure.ContextRouting.SelectedContextBand != nil {
			attrs = append(attrs, attribute.String(runtimeTraceAttrPreferredContextBand, runtimeTracePolicy.preferredContextBand(*failure.ContextRouting.SelectedContextBand)))
		}
		if failure.ContextRouting.SelectedTerminalTargetID != nil {
			attrs = append(attrs, attribute.Int(runtimeTraceAttrSelectedTerminalTargetID, *failure.ContextRouting.SelectedTerminalTargetID))
		}
		attrs = append(attrs, runtimeTraceContextOverflowPromotionAttributes(failure.ContextRouting)...)
		attrs = append(attrs, runtimeTraceContextOverflowAffinityAttributes(failure.ContextRouting)...)
		attrs = append(attrs, runtimeTraceFacadeSelectionAttributes(failure.ContextRouting)...)
		attrs = append(attrs, runtimeTracePlannerTraceAttributes(failure.ContextRouting.PlannerTrace)...)
	}
	return attrs
}

func runtimeTraceEnvelopeAttributes(envelope runtimeTelemetryEnvelope) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(runtimeTraceAttrOperationName, runtimeMetricPolicy.operationName(envelope.UsageEvent.OperationName)),
		attribute.String(runtimeTraceAttrAPIFamily, runtimeTracePolicy.apiFamily(envelope.UsageEvent.APIFamily)),
		attribute.String(runtimeTraceAttrStatusClass, runtimeMetricPolicy.statusClass(envelope.UsageEvent.StatusCode)),
		attribute.String(runtimeTraceAttrStreamOutcome, runtimeMetricPolicy.streamOutcome(envelope.UsageEvent.StreamOutcome)),
		attribute.String(runtimeTraceAttrRouteReason, string(gatewayaccounting.NormalizeRouteReason(envelope.AccountingEvent.RouteReason))),
		attribute.String(runtimeTraceAttrUsageSource, string(gatewayaccounting.NormalizeUsageSource(envelope.AccountingEvent.UsageSource))),
	}
	if envelope.AccountingEvent.PricingConfigVersionUsed != nil {
		attrs = append(attrs, attribute.Int(runtimeTraceAttrPricingConfigVersionUsed, *envelope.AccountingEvent.PricingConfigVersionUsed))
	}
	if envelope.UsageEvent.UpstreamOperationName != nil {
		attrs = append(attrs, attribute.String(runtimeTraceAttrUpstreamOperationName, runtimeMetricPolicy.operationName(*envelope.UsageEvent.UpstreamOperationName)))
	}
	if envelope.UsageEvent.OperationTranslationMode != nil {
		attrs = append(attrs, attribute.String(runtimeTraceAttrOperationTranslationMode, runtimeTracePolicy.translationMode(*envelope.UsageEvent.OperationTranslationMode)))
	}
	if envelope.UsageEvent.UpstreamRequestPath != nil {
		attrs = append(attrs, attribute.String(runtimeTraceAttrUpstreamRequestPath, runtimeTracePolicy.requestPath(*envelope.UsageEvent.UpstreamRequestPath)))
	}
	if envelope.UsageEvent.ContextRouting != nil {
		if envelope.UsageEvent.ContextRouting.SelectedContextBand != nil {
			attrs = append(attrs, attribute.String(runtimeTraceAttrPreferredContextBand, runtimeTracePolicy.preferredContextBand(*envelope.UsageEvent.ContextRouting.SelectedContextBand)))
		}
		if envelope.UsageEvent.ContextRouting.SelectedTerminalTargetID != nil {
			attrs = append(attrs, attribute.Int(runtimeTraceAttrSelectedTerminalTargetID, *envelope.UsageEvent.ContextRouting.SelectedTerminalTargetID))
		}
		attrs = append(attrs, runtimeTraceContextOverflowPromotionAttributes(envelope.UsageEvent.ContextRouting)...)
		attrs = append(attrs, runtimeTraceContextOverflowAffinityAttributes(envelope.UsageEvent.ContextRouting)...)
		attrs = append(attrs, runtimeTraceFacadeSelectionAttributes(envelope.UsageEvent.ContextRouting)...)
		attrs = append(attrs, runtimeTracePlannerTraceAttributes(envelope.UsageEvent.ContextRouting.PlannerTrace)...)
	}
	if envelope.UsageEvent.StatusCode >= 100 && envelope.UsageEvent.StatusCode <= 599 {
		attrs = append(attrs, attribute.Int(runtimeTraceAttrHTTPStatus, envelope.UsageEvent.StatusCode))
	}
	return attrs
}

func runtimeTraceFeedbackAttributes(event runtimeFeedbackEvent) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(runtimeTraceAttrAPIFamily, runtimeTracePolicy.apiFamily(event.APIFamily)),
		attribute.String(runtimeTraceAttrFeedbackKind, runtimeMetricPolicy.feedbackKind(event.Kind)),
	}
}

func runtimeTraceHTTPAttributes(method string, operation RuntimeOperation, isStreaming bool, bodyMode string) []attribute.KeyValue {
	attrs := runtimeTraceOperationAttributes(operation)
	attrs = append(attrs,
		attribute.String(runtimeTraceAttrHTTPMethod, runtimeTracePolicy.httpMethod(method)),
		attribute.Bool(runtimeTraceAttrStreaming, isStreaming),
		attribute.String(runtimeTraceAttrBodyMode, runtimeTracePolicy.bodyMode(bodyMode)),
	)
	return attrs
}

func runtimeTraceSetStatusCode(span trace.Span, statusCode int) {
	if span == nil {
		return
	}
	span.SetAttributes(attribute.String(runtimeTraceAttrStatusClass, runtimeMetricPolicy.statusClass(statusCode)))
	if statusCode >= 100 && statusCode <= 599 {
		span.SetAttributes(attribute.Int(runtimeTraceAttrHTTPStatus, statusCode))
	}
	if statusCode >= 500 {
		span.SetStatus(codes.Error, "provider_http_5xx")
	}
}

func runtimeTraceSetEnqueueStatus(span trace.Span, status string) {
	if span != nil {
		span.SetAttributes(attribute.String(runtimeTraceAttrEnqueueStatus, runtimeMetricPolicy.outboxEnqueueStatus(status)))
	}
}

func runtimeTraceSetFeedbackStatus(span trace.Span, status RuntimeFeedbackEnqueueStatus) {
	if span != nil {
		span.SetAttributes(attribute.String(runtimeTraceAttrEnqueueStatus, runtimeMetricPolicy.feedbackEnqueueStatus(status)))
	}
}

func runtimeTraceBodyMode(source *runtimeRequestBodySource) string {
	if source == nil {
		return runtimeTraceBodyEmpty
	}
	if source.useStreamingBody {
		return runtimeTraceBodyStreaming
	}
	if len(source.bufferedBody) == 0 {
		return runtimeTraceBodyEmpty
	}
	return runtimeTraceBodyBuffered
}

func (policy runtimeTraceAttributePolicy) apiFamily(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "openai", "anthropic", "gemini":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return runtimeTraceValueUnknown
	}
}

func (policy runtimeTraceAttributePolicy) httpMethod(method string) string {
	normalized := strings.ToUpper(strings.TrimSpace(method))
	switch normalized {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return normalized
	default:
		return runtimeTraceValueUnknown
	}
}

func (policy runtimeTraceAttributePolicy) bodyMode(value string) string {
	switch strings.TrimSpace(value) {
	case runtimeTraceBodyStreaming, runtimeTraceBodyBuffered, runtimeTraceBodyEmpty:
		return strings.TrimSpace(value)
	default:
		return runtimeTraceValueUnknown
	}
}

func (policy runtimeTraceAttributePolicy) translationMode(value string) string {
	switch strings.TrimSpace(value) {
	case string(TranslationModeNone), string(TranslationModeOpenAIResponsesToChatCompletions), string(TranslationModeOpenAIChatCompletionsToResponses):
		return strings.TrimSpace(value)
	default:
		return runtimeTraceValueUnknown
	}
}

func (policy runtimeTraceAttributePolicy) requestPath(value string) string {
	trimmed := strings.TrimSpace(value)
	for _, operation := range runtimeOperationCatalog {
		if trimmed == operation.PathTemplate {
			return operation.PathTemplate
		}
		if _, ok := operation.PathMatcher.Match(trimmed); ok {
			return operation.PathTemplate
		}
	}
	return runtimeTraceValueUnknown
}

func (policy runtimeTraceAttributePolicy) plannerVersion(value string) string {
	if strings.TrimSpace(value) == runtimePlannerTraceVersion {
		return runtimePlannerTraceVersion
	}
	return runtimeTraceValueUnknown
}

func (policy runtimeTraceAttributePolicy) plannerDecision(value string) string {
	switch strings.TrimSpace(value) {
	case runtimePlannerTraceDecisionSelected, runtimePlannerTraceDecisionNoFit, runtimePlannerTraceDecisionNoTarget:
		return strings.TrimSpace(value)
	default:
		return runtimeTraceValueUnknown
	}
}

func (policy runtimeTraceAttributePolicy) plannerPolicy(value string) string {
	switch strings.TrimSpace(value) {
	case "single", "fill-first", "round-robin", "cheapest_eligible_context", runtimeFacadeSelectionPolicyWeightedEligibleContext:
		return strings.TrimSpace(value)
	default:
		return runtimeTraceValueUnknown
	}
}

func (policy runtimeTraceAttributePolicy) preferredContextBand(value string) string {
	switch strings.TrimSpace(value) {
	case runtimeContextBandPreferred, runtimeContextBandDiscretionary:
		return strings.TrimSpace(value)
	default:
		return runtimeTraceValueUnknown
	}
}

func (policy runtimeTraceAttributePolicy) contextOverflowPromotionResult(value string) string {
	switch strings.TrimSpace(value) {
	case runtimeContextOverflowPromotionResultPromotedSuccess:
		return strings.TrimSpace(value)
	default:
		return runtimeTraceValueUnknown
	}
}

func (policy runtimeTraceAttributePolicy) contextOverflowPromotionClassifier(value string) string {
	switch strings.TrimSpace(value) {
	case cliProxyAPIOverflowClassifierErrorCode, cliProxyAPIOverflowClassifierMessageText:
		return strings.TrimSpace(value)
	default:
		return runtimeTraceValueUnknown
	}
}

func (policy runtimeTraceAttributePolicy) contextOverflowPromotionTriggerCode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "context_length_exceeded", "context_too_large":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return runtimeTraceValueUnknown
	}
}

func (policy runtimeTraceAttributePolicy) contextOverflowPromotionTriggerPhase(value string) string {
	switch strings.TrimSpace(value) {
	case runtimeContextOverflowPromotionTriggerPhasePreDispatchEstimate, runtimeContextOverflowPromotionTriggerPhaseProviderOverflow:
		return strings.TrimSpace(value)
	default:
		return runtimeTraceValueUnknown
	}
}

func (policy runtimeTraceAttributePolicy) contextOverflowPromotionEstimationMode(value string) string {
	switch strings.TrimSpace(value) {
	case runtimeContextOverflowPromotionEstimationModeEstimated, runtimeContextOverflowPromotionEstimationModePassThrough:
		return strings.TrimSpace(value)
	default:
		return runtimeTraceValueUnknown
	}
}

func (policy runtimeTraceAttributePolicy) attemptResult(value string) string {
	switch strings.TrimSpace(value) {
	case "success", "failover_http", "transport_error", "admission_rejected", "skipped", "fatal_error", "hedge_canceled":
		return strings.TrimSpace(value)
	default:
		return runtimeTraceValueUnknown
	}
}

func (policy runtimeTraceAttributePolicy) errorReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "operation_not_found", "method_not_allowed", "request_plan_failed", "runtime_execute_failed", "connection_attempt_failed", "provider_http_failed", "response_handle_failed", "runtime_activity_submit_rejected", "side_effect_submit_rejected", "side_effect_commit_failed", "outbox_enqueue_failed", "outbox_materialize_failed", "feedback_enqueue_failed", "feedback_write_failed", "invalid_payload":
		return strings.TrimSpace(reason)
	default:
		return "runtime_error"
	}
}

func runtimeTraceSetAttemptResult(span trace.Span, result string) {
	if span != nil {
		span.SetAttributes(attribute.String(runtimeTraceAttrAttemptResult, runtimeTracePolicy.attemptResult(result)))
	}
}

func runtimeTraceSetStreamOutcome(span trace.Span, outcome string) {
	if span != nil {
		span.SetAttributes(attribute.String(runtimeTraceAttrStreamOutcome, runtimeMetricPolicy.streamOutcome(outcome)))
	}
}

func runtimeTraceSetAttemptCount(span trace.Span, count int) {
	if span != nil && count > 0 {
		span.SetAttributes(attribute.Int("prism.attempt_count", count))
	}
}

func runtimeTraceMarkContextError(ctx context.Context, reason string) {
	runtimeTraceMarkError(trace.SpanFromContext(ctx), reason)
}

func eventAPIFamily(existing string, operationName string) string {
	if strings.TrimSpace(existing) != "" {
		return existing
	}
	trimmedOperation := strings.TrimSpace(operationName)
	for _, operation := range runtimeOperationCatalog {
		if operation.Name == trimmedOperation {
			return operation.APIFamily
		}
	}
	return ""
}
