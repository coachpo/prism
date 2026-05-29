package asyncmetrics

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestAsyncMetricsSanitizeOutboundAttributes(t *testing.T) {
	reader := installMetricTestProvider(t)
	ctx := context.Background()

	RecordOutbound(ctx, "sidecar_client", "https://admin:secret@sidecar.example/v0/management/auth-files?token=abc", "request failed with token", 10*time.Millisecond)
	RecordDuration(ctx, "runtime_side_effect_manager", "commit_runtime_activity", OutcomeSuccess, 5*time.Millisecond)
	RecordQueueDepth(ctx, "proxy_key_usage_writer", "pending", 3)

	points := collectMetricPoints(t, reader)
	if len(points) == 0 {
		t.Fatal("expected async metric datapoints")
	}
	leakyFragments := []string{"sidecar.example", "admin", "secret", "token", "auth-files?", "request failed"}
	for _, point := range points {
		for _, attr := range point.attrs {
			key := string(attr.Key)
			if key != "component" && key != "operation" && key != "outcome" {
				t.Fatalf("metric %s emitted unexpected attribute key %q", point.name, key)
			}
			value := attr.Value.AsString()
			for _, fragment := range leakyFragments {
				if strings.Contains(value, fragment) {
					t.Fatalf("metric %s leaked %q in %s=%q", point.name, fragment, key, value)
				}
			}
		}
	}
}

type metricPoint struct {
	name  string
	attrs []attribute.KeyValue
}

func installMetricTestProvider(t *testing.T) *sdkmetric.ManualReader {
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

func collectMetricPoints(t *testing.T, reader *sdkmetric.ManualReader) []metricPoint {
	t.Helper()
	var resourceMetrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resourceMetrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	points := make([]metricPoint, 0)
	for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
		for _, sample := range scopeMetrics.Metrics {
			switch data := sample.Data.(type) {
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					points = append(points, metricPoint{name: sample.Name, attrs: point.Attributes.ToSlice()})
				}
			case metricdata.Histogram[int64]:
				for _, point := range data.DataPoints {
					points = append(points, metricPoint{name: sample.Name, attrs: point.Attributes.ToSlice()})
				}
			case metricdata.Histogram[float64]:
				for _, point := range data.DataPoints {
					points = append(points, metricPoint{name: sample.Name, attrs: point.Attributes.ToSlice()})
				}
			}
		}
	}
	return points
}
