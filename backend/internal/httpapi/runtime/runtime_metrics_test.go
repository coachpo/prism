package runtime

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRuntimeMetricsUseBoundedAttributes(t *testing.T) {
	reader := installRuntimeMetricTestProvider(t)
	service := &Service{runtimeMetrics: newRuntimeMetrics()}
	ctx := context.Background()

	service.recordRuntimeMetricsForEnvelope(ctx, runtimeTelemetryEnvelope{UsageEvent: runtimeMetricUsageEvent("openai.chat_completions", http.StatusOK, runtimeStreamOutcomeCompleted)})
	service.runtimeMetrics.recordFailover(ctx, "openai.chat_completions", "http")
	service.runtimeMetrics.recordHedge(ctx, "openai.chat_completions", 2)
	service.recordRuntimeFeedbackEnqueue(ctx, "openai.chat_completions", runtimeFeedbackFailoverHTTP, RuntimeFeedbackAccepted)
	service.recordRuntimeOutboxEnqueue(ctx, "openai.chat_completions", runtimeOutboxEnqueueAccepted)

	points := collectRuntimeMetricPoints(t, reader)
	if len(points) == 0 {
		t.Fatal("expected runtime metric datapoints")
	}
	allowedKeys := map[string]struct{}{
		runtimeMetricAttrOperationName:  {},
		runtimeMetricAttrStatusClass:    {},
		runtimeMetricAttrStreamOutcome:  {},
		runtimeMetricAttrFailoverReason: {},
		runtimeMetricAttrFeedbackKind:   {},
		runtimeMetricAttrEnqueueStatus:  {},
	}
	allowedValues := map[string]struct{}{
		"openai.chat_completions":           {},
		"2xx":                               {},
		runtimeStreamOutcomeCompleted:       {},
		"http":                              {},
		string(runtimeFeedbackFailoverHTTP): {},
		runtimeOutboxEnqueueAccepted:        {},
	}
	for _, point := range points {
		for _, attr := range point.attrs {
			key := string(attr.Key)
			if _, ok := allowedKeys[key]; !ok {
				t.Fatalf("metric %s emitted unapproved attribute key %q", point.name, key)
			}
			value := attr.Value.AsString()
			if _, ok := allowedValues[value]; !ok {
				t.Fatalf("metric %s emitted unbounded attribute %s=%q", point.name, key, value)
			}
		}
	}
}

func TestRuntimeMetricsDoNotLeakSensitiveFields(t *testing.T) {
	reader := installRuntimeMetricTestProvider(t)
	service := &Service{runtimeMetrics: newRuntimeMetrics()}
	ctx := context.Background()
	sensitiveProxyKeyID := 77
	sensitiveProxyKeyName := "proxy-key-name-secret"
	sensitiveStreamError := "provider said prompt body leaked"
	event := runtimeMetricUsageEvent("https://upstream.example/v1/chat/completions?prompt=super-secret", http.StatusTooManyRequests, sensitiveStreamError)
	event.IngressRequestID = "ingress-request-secret"
	event.ProxyAPIKeyID = &sensitiveProxyKeyID
	event.ProxyAPIKeyNameSnapshot = &sensitiveProxyKeyName
	event.EndpointID = 42
	event.RequestPath = "/v1/chat/completions"
	event.StreamErrorKind = &sensitiveStreamError

	service.recordRuntimeMetricsForEnvelope(ctx, runtimeTelemetryEnvelope{UsageEvent: event})
	service.runtimeMetrics.recordFailover(ctx, "endpoint-42 /v1/chat/completions", "https://upstream.example")
	service.recordRuntimeFeedbackEnqueue(ctx, "proxy-key-name-secret", runtimeFeedbackKind("provider error text"), RuntimeFeedbackEnqueueStatus("ingress-request-secret"))
	service.recordRuntimeOutboxEnqueue(ctx, "raw-body-super-secret", "proxy-key-id-77")

	leakyFragments := []string{
		"/v1/chat/completions",
		"https://upstream.example",
		"ingress-request-secret",
		"proxy-key-name-secret",
		"proxy-key-id-77",
		"endpoint-42",
		"provider error text",
		"prompt body leaked",
		"super-secret",
		"raw-body-super-secret",
	}
	points := collectRuntimeMetricPoints(t, reader)
	if len(points) == 0 {
		t.Fatal("expected runtime metric datapoints")
	}
	for _, point := range points {
		for _, attr := range point.attrs {
			value := attr.Value.AsString()
			for _, fragment := range leakyFragments {
				if strings.Contains(value, fragment) {
					t.Fatalf("metric %s leaked sensitive fragment %q in %s=%q", point.name, fragment, attr.Key, value)
				}
			}
		}
	}
}

func TestRuntimeMetricsRecordOperationName(t *testing.T) {
	reader := installRuntimeMetricTestProvider(t)
	service := &Service{runtimeMetrics: newRuntimeMetrics()}
	ctx := context.Background()

	service.recordRuntimeMetricsForEnvelope(ctx, runtimeTelemetryEnvelope{UsageEvent: runtimeMetricUsageEvent("openai.chat_completions", http.StatusOK, runtimeStreamOutcomeCompleted)})
	service.runtimeMetrics.recordFailover(ctx, "openai.chat_completions", "http")
	service.runtimeMetrics.recordHedge(ctx, "openai.chat_completions", 1)
	service.recordRuntimeFeedbackEnqueue(ctx, "openai.chat_completions", runtimeFeedbackFailoverHTTP, RuntimeFeedbackAccepted)
	service.recordRuntimeOutboxEnqueue(ctx, "openai.chat_completions", runtimeOutboxEnqueueAccepted)

	points := collectRuntimeMetricPoints(t, reader)
	if len(points) == 0 {
		t.Fatal("expected runtime metric datapoints")
	}
	for _, point := range points {
		attrs := runtimeMetricAttributesByKey(point.attrs)
		operationName, ok := attrs[runtimeMetricAttrOperationName]
		if !ok {
			t.Fatalf("metric %s omitted operation_name attribute: %+v", point.name, point.attrs)
		}
		if operationName != "openai.chat_completions" {
			t.Fatalf("metric %s recorded operation_name=%q", point.name, operationName)
		}
	}
}

type runtimeMetricPoint struct {
	name  string
	attrs []attribute.KeyValue
}

func installRuntimeMetricTestProvider(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	previous := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetMeterProvider(previous)
	})
	return reader
}

func collectRuntimeMetricPoints(t *testing.T, reader *sdkmetric.ManualReader) []runtimeMetricPoint {
	t.Helper()
	var resourceMetrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resourceMetrics); err != nil {
		t.Fatalf("collect runtime metrics: %v", err)
	}
	points := make([]runtimeMetricPoint, 0)
	for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
		for _, sample := range scopeMetrics.Metrics {
			switch data := sample.Data.(type) {
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					points = append(points, runtimeMetricPoint{name: sample.Name, attrs: point.Attributes.ToSlice()})
				}
			case metricdata.Histogram[int64]:
				for _, point := range data.DataPoints {
					points = append(points, runtimeMetricPoint{name: sample.Name, attrs: point.Attributes.ToSlice()})
				}
			case metricdata.Histogram[float64]:
				for _, point := range data.DataPoints {
					points = append(points, runtimeMetricPoint{name: sample.Name, attrs: point.Attributes.ToSlice()})
				}
			}
		}
	}
	return points
}

func runtimeMetricUsageEvent(operationName string, statusCode int, streamOutcome string) usageEventInsert {
	responseTimeMS := 321
	completionDurationMS := 300
	ttftMS := 42
	return usageEventInsert{
		ProfileID:            1,
		IngressRequestID:     "ingress-request-redacted",
		ModelID:              "public-model",
		APIFamily:            "openai",
		OperationName:        operationName,
		EndpointID:           7,
		ConnectionID:         8,
		StatusCode:           statusCode,
		SuccessFlag:          statusCode >= 200 && statusCode <= 299,
		AttemptCount:         3,
		RequestPath:          "/v1/chat/completions",
		CreatedAt:            time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
		ResponseTimeMS:       &responseTimeMS,
		CompletionDurationMS: &completionDurationMS,
		TTFTMS:               &ttftMS,
		StreamOutcome:        streamOutcome,
	}
}

func runtimeMetricAttributesByKey(attrs []attribute.KeyValue) map[string]string {
	byKey := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		byKey[string(attr.Key)] = attr.Value.AsString()
	}
	return byKey
}

func formatRuntimeMetricAttributes(attrs []attribute.KeyValue) string {
	parts := make([]string, 0, len(attrs))
	for _, attr := range attrs {
		parts = append(parts, fmt.Sprintf("%s=%s", attr.Key, attr.Value.AsString()))
	}
	return strings.Join(parts, ",")
}
