package runtime

import (
	"context"
	"strings"
)

type runtimeTraceContext struct {
	TraceParent string `json:"trace_parent,omitempty"`
	TraceState  string `json:"trace_state,omitempty"`
}

func runtimeTraceContextFromContext(context.Context) runtimeTraceContext {
	return runtimeTraceContext{}
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
