package core

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRequestContextTrimsIDAndOmitTraceMetadata(t *testing.T) {
	receivedAt := time.Unix(1_700_000_000, 0).UTC()
	requestContext := NewRequestContext(context.Background(), " request-123 ", receivedAt)
	envelope := NewRequestEnvelope(RequestEnvelopeInput{
		Context:   requestContext,
		Operation: OperationDescriptor{Name: "openai.responses", Method: http.MethodPost, APIFamily: APIFamilyOpenAI, PathTemplate: "/v1/responses", Shape: EndpointShapeTextGeneration, ModelBindingSource: ModelBindingSourceBody},
		Method:    http.MethodPost,
		Path:      "/v1/responses",
		Body:      []byte(`{"model":"model-a"}`),
	})

	if envelope.Context.RequestID != "request-123" {
		t.Fatalf("expected trimmed request id, got %q", envelope.Context.RequestID)
	}
	if envelope.Context.TraceID != "" || !envelope.Context.TraceContext.Empty() {
		t.Fatalf("expected trace metadata to be omitted, got %+v", envelope.Context)
	}
	restored := envelope.Context.TraceContext.Context(context.Background())
	if restored == nil {
		t.Fatal("expected empty trace context restore to return parent context")
	}
}
