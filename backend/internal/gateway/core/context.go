package core

import (
	"context"
	"strings"
	"time"
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
	return RequestContext{
		RequestID:    strings.TrimSpace(requestID),
		TraceID:      "",
		TraceContext: TraceContextFromContext(ctx),
		ReceivedAt:   receivedAt.UTC(),
	}
}

func TraceContextFromContext(ctx context.Context) TraceContext {
	return TraceContext{}
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
	return parent
}
