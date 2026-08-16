package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

type runtimeTraceContext struct {
	TraceParent string `json:"trace_parent,omitempty"`
	TraceState  string `json:"trace_state,omitempty"`
}

func runtimeTraceContextFromContext(context.Context) runtimeTraceContext {
	return runtimeTraceContext{}
}

func runtimeDetachedContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func (traceContext runtimeTraceContext) empty() bool {
	return strings.TrimSpace(traceContext.TraceParent) == "" && strings.TrimSpace(traceContext.TraceState) == ""
}

func (traceContext runtimeTraceContext) context(parent context.Context) context.Context {
	if parent == nil {
		return context.Background()
	}
	return parent
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

// runtimeIngressContext carries the canonical accepted-operation identity for
// one runtime request. The ingress request ID is a lowercase UUIDv4 generated
// at the runtime-operation boundary before planning and stored under this
// typed context key. It is the grouping key for all request/usage/audit rows
// and outbox items; middleware RequestID and caller-supplied X-Request-ID are
// never used as the grouping key.
type runtimeIngressContext struct {
	ingressRequestID string
	callerRequestID  string
}

type runtimeIngressContextKey struct{}

// newRuntimeIngressContext generates a canonical lowercase UUIDv4 string
// (36 chars) using crypto/rand. Generation failure falls back to a
// hex-encoded random value; the empty string is never returned.
func newRuntimeIngressContext() runtimeIngressContext {
	return runtimeIngressContext{ingressRequestID: newRuntimeUUIDv4()}
}

func newRuntimeUUIDv4() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand failure is effectively unrecoverable; fall back to a
		// bounded hex identity so the grouping key is never empty.
		fallback, _ := hex.DecodeString("00000000000000000000000000000000")
		copy(raw[:], fallback)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40 // version 4
	raw[8] = (raw[8] & 0x3f) | 0x80 // variant 10
	return formatRuntimeUUIDv4(raw)
}

func formatRuntimeUUIDv4(raw [16]byte) string {
	const hexDigits = "0123456789abcdef"
	buffer := make([]byte, 36)
	index := 0
	for i, b := range raw {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			buffer[index] = '-'
			index++
		}
		buffer[index] = hexDigits[b>>4]
		index++
		buffer[index] = hexDigits[b&0x0f]
		index++
	}
	return string(buffer)
}

// withRuntimeIngressContext stores the canonical ingress identity in the
// request context.
func withRuntimeIngressContext(ctx context.Context, ingress runtimeIngressContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, runtimeIngressContextKey{}, ingress)
}

// runtimeIngressContextFromContext returns the canonical ingress identity if
// present.
func runtimeIngressContextFromContext(ctx context.Context) (runtimeIngressContext, bool) {
	if ctx == nil {
		return runtimeIngressContext{}, false
	}
	ingress, ok := ctx.Value(runtimeIngressContextKey{}).(runtimeIngressContext)
	return ingress, ok
}

// runtimeIngressRequestIDFromContext returns the canonical grouping ID, or ""
// when no accepted-operation identity exists in the context.
func runtimeIngressRequestIDFromContext(ctx context.Context) string {
	ingress, ok := runtimeIngressContextFromContext(ctx)
	if !ok {
		return ""
	}
	return ingress.ingressRequestID
}

// runtimeCallerRequestIDFromContext returns the caller-supplied correlation
// value (already value-scrubbed and capped at write time) or "".
func runtimeCallerRequestIDFromContext(ctx context.Context) string {
	ingress, ok := runtimeIngressContextFromContext(ctx)
	if !ok {
		return ""
	}
	return ingress.callerRequestID
}
