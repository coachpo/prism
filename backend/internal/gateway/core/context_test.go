package core

import (
	"context"
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestRequestTraceIDPropagationInEnvelope(t *testing.T) {
	oldProvider := otel.GetTracerProvider()
	oldPropagator := otel.GetTextMapPropagator()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer func() {
		otel.SetTracerProvider(oldProvider)
		otel.SetTextMapPropagator(oldPropagator)
		_ = provider.Shutdown(context.Background())
	}()

	rootCtx, rootSpan := otel.Tracer("gateway-core-test").Start(context.Background(), "root")
	defer rootSpan.End()
	receivedAt := time.Unix(1_700_000_000, 0).UTC()
	requestContext := NewRequestContext(rootCtx, " request-123 ", receivedAt)
	envelope := NewRequestEnvelope(RequestEnvelopeInput{
		Context:   requestContext,
		Operation: OperationDescriptor{Name: "openai.responses", Method: http.MethodPost, APIFamily: APIFamilyOpenAI, PathTemplate: "/v1/responses", Shape: EndpointShapeTextGeneration, ModelBindingSource: ModelBindingSourceBody},
		Method:    http.MethodPost,
		Path:      "/v1/responses",
		Body:      []byte(`{"model":"model-a"}`),
	})

	rootTraceID := rootSpan.SpanContext().TraceID().String()
	if envelope.Context.RequestID != "request-123" {
		t.Fatalf("expected trimmed request id, got %q", envelope.Context.RequestID)
	}
	if envelope.Context.TraceID != rootTraceID || envelope.Context.TraceContext.Empty() {
		t.Fatalf("expected trace %s to propagate through envelope, got %+v", rootTraceID, envelope.Context)
	}
	restored := envelope.Context.TraceContext.Context(context.Background())
	if trace.SpanContextFromContext(restored).TraceID().String() != rootTraceID {
		t.Fatalf("expected trace context restore to preserve %s, got %s", rootTraceID, trace.SpanContextFromContext(restored).TraceID())
	}
}
