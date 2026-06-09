package core

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type RequestContext struct {
	RequestID    string       `json:"request_id"`
	TraceID      string       `json:"trace_id,omitempty"`
	TraceContext TraceContext `json:"trace_context"`
	ReceivedAt   time.Time    `json:"received_at"`
}

type TraceContext struct {
	TraceParent string `json:"trace_parent,omitempty"`
	TraceState  string `json:"trace_state,omitempty"`
}

func NewRequestContext(ctx context.Context, requestID string, receivedAt time.Time) RequestContext {
	spanContext := trace.SpanContextFromContext(ctx)
	traceID := ""
	if spanContext.IsValid() {
		traceID = spanContext.TraceID().String()
	}
	return RequestContext{
		RequestID:    strings.TrimSpace(requestID),
		TraceID:      traceID,
		TraceContext: TraceContextFromContext(ctx),
		ReceivedAt:   receivedAt.UTC(),
	}
}

func TraceContextFromContext(ctx context.Context) TraceContext {
	if ctx == nil || !trace.SpanContextFromContext(ctx).IsValid() {
		return TraceContext{}
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return TraceContext{TraceParent: carrier["traceparent"], TraceState: carrier["tracestate"]}
}

func (traceContext TraceContext) Empty() bool {
	return strings.TrimSpace(traceContext.TraceParent) == "" && strings.TrimSpace(traceContext.TraceState) == ""
}

func (traceContext TraceContext) Context(parent context.Context) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	if traceContext.Empty() {
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
