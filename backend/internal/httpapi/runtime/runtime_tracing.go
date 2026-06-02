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
)

const (
	runtimeTraceAttrOperationName             = "prism.runtime.operation_name"
	runtimeTraceAttrUpstreamOperationName     = "prism.runtime.upstream_operation_name"
	runtimeTraceAttrOperationTranslationMode  = "prism.runtime.operation_translation_mode"
	runtimeTraceAttrUpstreamRequestPath       = "prism.runtime.upstream_request_path"
	runtimeTraceAttrPreferredContextBand      = "prism.runtime.preferred_context_band"
	runtimeTraceAttrSelectedTerminalTargetID  = "prism.runtime.selected_terminal_target_id"
	runtimeTraceAttrFacadeModelID             = "prism.runtime.facade_model_id"
	runtimeTraceAttrFacadeSelectedTargetModel = "prism.runtime.facade_selected_target_model_id"
	runtimeTraceAttrFacadeSelectedWeight      = "prism.runtime.facade_selected_weight"
	runtimeTraceAttrFacadeEligibleTotalWeight = "prism.runtime.facade_eligible_total_weight"
	runtimeTraceAttrFacadeExclusionSummary    = "prism.runtime.facade_exclusion_summary"
	runtimeTraceAttrAPIFamily                 = "prism.runtime.api_family"
	runtimeTraceAttrStreaming                 = "prism.runtime.streaming"
	runtimeTraceAttrStatusClass               = "prism.runtime.status_class"
	runtimeTraceAttrStreamOutcome             = "prism.runtime.stream_outcome"
	runtimeTraceAttrBodyMode                  = "prism.runtime.body_mode"
	runtimeTraceAttrAttemptResult             = "prism.runtime.attempt_result"
	runtimeTraceAttrFeedbackKind              = "prism.runtime.feedback_kind"
	runtimeTraceAttrEnqueueStatus             = "prism.runtime.enqueue_status"
	runtimeTraceAttrHTTPMethod                = "http.request.method"
	runtimeTraceAttrHTTPStatus                = "http.response.status_code"

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
		attrs = append(attrs, runtimeTraceFacadeSelectionAttributes(contextRouting)...)
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
		attrs = append(attrs, runtimeTraceFacadeSelectionAttributes(failure.ContextRouting)...)
	}
	return attrs
}

func runtimeTraceEnvelopeAttributes(envelope runtimeTelemetryEnvelope) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(runtimeTraceAttrOperationName, runtimeMetricPolicy.operationName(envelope.UsageEvent.OperationName)),
		attribute.String(runtimeTraceAttrAPIFamily, runtimeTracePolicy.apiFamily(envelope.UsageEvent.APIFamily)),
		attribute.String(runtimeTraceAttrStatusClass, runtimeMetricPolicy.statusClass(envelope.UsageEvent.StatusCode)),
		attribute.String(runtimeTraceAttrStreamOutcome, runtimeMetricPolicy.streamOutcome(envelope.UsageEvent.StreamOutcome)),
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
		attrs = append(attrs, runtimeTraceFacadeSelectionAttributes(envelope.UsageEvent.ContextRouting)...)
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

func (policy runtimeTraceAttributePolicy) preferredContextBand(value string) string {
	switch strings.TrimSpace(value) {
	case runtimeContextBandPreferred, runtimeContextBandDiscretionary:
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
		span.SetAttributes(attribute.Int("prism.runtime.attempt_count", count))
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
