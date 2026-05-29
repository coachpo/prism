package telemetry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelmetric "go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc/credentials"
)

const (
	fallbackExporterTimeout  = 10 * time.Second
	fallbackServiceNamespace = "prism"
	fallbackServiceName      = "prism-backend"
	traceExportPath          = "/v1/traces"
	metricExportPath         = "/v1/metrics"
	authorizationHeaderKey   = "Authorization"
)

// Providers owns process-wide OpenTelemetry providers built from startup config.
type Providers struct {
	traceProvider *sdktrace.TracerProvider
	meterProvider *metric.MeterProvider

	shutdownOnce sync.Once
	shutdownErr  error
}

// BuildProviders builds tracer and meter providers once from typed bootstrap settings.
func BuildProviders(ctx context.Context, telemetryConfig config.TelemetryConfig) (*Providers, error) {
	providers := &Providers{}
	if !telemetryConfig.Enabled {
		providers.installGlobals()
		return providers, nil
	}

	telemetryResource := resourceFor(telemetryConfig.Service)
	if telemetryConfig.Traces.Enabled {
		traceExporter, err := newTraceExporter(ctx, telemetryConfig.Exporter)
		if err != nil {
			return nil, err
		}
		providers.traceProvider = sdktrace.NewTracerProvider(
			sdktrace.WithResource(telemetryResource),
			sdktrace.WithSampler(traceSampler(telemetryConfig.Traces)),
			sdktrace.WithBatcher(traceExporter),
		)
	}

	if telemetryConfig.Metrics.Enabled {
		metricExporter, err := newMetricExporter(ctx, telemetryConfig.Exporter)
		if err != nil {
			return nil, errors.Join(err, providers.Shutdown(context.WithoutCancel(ctx)))
		}
		providers.meterProvider = metric.NewMeterProvider(
			metric.WithResource(telemetryResource),
			metric.WithReader(metric.NewPeriodicReader(metricExporter)),
		)
	}

	providers.installGlobals()
	return providers, nil
}

// TracerProvider returns the startup-built tracer provider or a no-op provider.
func (providers *Providers) TracerProvider() oteltrace.TracerProvider {
	if providers != nil && providers.traceProvider != nil {
		return providers.traceProvider
	}
	return tracenoop.NewTracerProvider()
}

// MeterProvider returns the startup-built meter provider or a no-op provider.
func (providers *Providers) MeterProvider() otelmetric.MeterProvider {
	if providers != nil && providers.meterProvider != nil {
		return providers.meterProvider
	}
	return metricnoop.NewMeterProvider()
}

// Shutdown force-flushes and shuts down startup-owned telemetry providers once.
func (providers *Providers) Shutdown(ctx context.Context) error {
	if providers == nil {
		return nil
	}
	providers.shutdownOnce.Do(func() {
		var errs []error
		if providers.traceProvider != nil {
			errs = appendError(errs, providers.traceProvider.ForceFlush(ctx))
		}
		if providers.meterProvider != nil {
			errs = appendError(errs, providers.meterProvider.ForceFlush(ctx))
		}
		if providers.traceProvider != nil {
			errs = appendError(errs, providers.traceProvider.Shutdown(ctx))
		}
		if providers.meterProvider != nil {
			errs = appendError(errs, providers.meterProvider.Shutdown(ctx))
		}
		installNoopGlobals()
		providers.shutdownErr = errors.Join(errs...)
	})
	return providers.shutdownErr
}

func (providers *Providers) installGlobals() {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	if providers.traceProvider != nil {
		otel.SetTracerProvider(providers.traceProvider)
	} else {
		otel.SetTracerProvider(tracenoop.NewTracerProvider())
	}
	if providers.meterProvider != nil {
		otel.SetMeterProvider(providers.meterProvider)
	} else {
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
	}
}

func installNoopGlobals() {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	otel.SetMeterProvider(metricnoop.NewMeterProvider())
}

func resourceFor(service config.TelemetryServiceConfig) *resource.Resource {
	serviceNamespace := strings.TrimSpace(service.Namespace)
	if serviceNamespace == "" {
		serviceNamespace = fallbackServiceNamespace
	}
	serviceName := strings.TrimSpace(service.Name)
	if serviceName == "" {
		serviceName = fallbackServiceName
	}
	return resource.NewWithAttributes(
		"",
		attribute.String("service.namespace", serviceNamespace),
		attribute.String("service.name", serviceName),
	)
}

func traceSampler(traces config.TelemetryTracesConfig) sdktrace.Sampler {
	ratio := traces.SamplingRatio
	if ratio < 0 || ratio > 1 {
		ratio = 1
	}
	return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
}

func newTraceExporter(ctx context.Context, exporter config.TelemetryExporterConfig) (sdktrace.SpanExporter, error) {
	switch exporter.Protocol {
	case config.TelemetryExporterProtocolGRPC:
		return newGRPCTraceExporter(ctx, exporter)
	case config.TelemetryExporterProtocolHTTPProtobuf, "":
		return newHTTPTraceExporter(ctx, exporter)
	default:
		return nil, fmt.Errorf("unsupported telemetry trace exporter protocol %q", exporter.Protocol)
	}
}

func newMetricExporter(ctx context.Context, exporter config.TelemetryExporterConfig) (metric.Exporter, error) {
	switch exporter.Protocol {
	case config.TelemetryExporterProtocolGRPC:
		return newGRPCMetricExporter(ctx, exporter)
	case config.TelemetryExporterProtocolHTTPProtobuf, "":
		return newHTTPMetricExporter(ctx, exporter)
	default:
		return nil, fmt.Errorf("unsupported telemetry metric exporter protocol %q", exporter.Protocol)
	}
}

func newHTTPTraceExporter(ctx context.Context, exporter config.TelemetryExporterConfig) (sdktrace.SpanExporter, error) {
	endpoint, err := exporterEndpoint(exporter)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := tlsConfigForExporter(exporter.TLS)
	if err != nil {
		return nil, err
	}
	options := []otlptracehttp.Option{
		otlptracehttp.WithTimeout(exporterTimeout(exporter)),
		otlptracehttp.WithHeaders(exporterHeaders(exporter.Auth)),
		otlptracehttp.WithCompression(traceHTTPCompression(exporter.Compression)),
	}
	options = appendHTTPEndpointOptions(options, endpoint, traceExportPath)
	if !endpointUsesPlaintext(endpoint) {
		options = append(options, otlptracehttp.WithTLSClientConfig(tlsConfig))
	}
	return otlptracehttp.New(ctx, options...)
}

func newHTTPMetricExporter(ctx context.Context, exporter config.TelemetryExporterConfig) (metric.Exporter, error) {
	endpoint, err := exporterEndpoint(exporter)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := tlsConfigForExporter(exporter.TLS)
	if err != nil {
		return nil, err
	}
	options := []otlpmetrichttp.Option{
		otlpmetrichttp.WithTimeout(exporterTimeout(exporter)),
		otlpmetrichttp.WithHeaders(exporterHeaders(exporter.Auth)),
		otlpmetrichttp.WithCompression(metricHTTPCompression(exporter.Compression)),
	}
	options = appendMetricHTTPEndpointOptions(options, endpoint, metricExportPath)
	if !endpointUsesPlaintext(endpoint) {
		options = append(options, otlpmetrichttp.WithTLSClientConfig(tlsConfig))
	}
	return otlpmetrichttp.New(ctx, options...)
}

func newGRPCTraceExporter(ctx context.Context, exporter config.TelemetryExporterConfig) (sdktrace.SpanExporter, error) {
	endpoint, err := exporterEndpoint(exporter)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := tlsConfigForExporter(exporter.TLS)
	if err != nil {
		return nil, err
	}
	options := []otlptracegrpc.Option{
		otlptracegrpc.WithTimeout(exporterTimeout(exporter)),
		otlptracegrpc.WithHeaders(exporterHeaders(exporter.Auth)),
		otlptracegrpc.WithCompressor(grpcCompressor(exporter.Compression)),
	}
	options = appendGRPCEndpointOptions(options, endpoint)
	if endpointUsesPlaintext(endpoint) {
		options = append(options, otlptracegrpc.WithInsecure())
	} else {
		options = append(options, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(tlsConfig)))
	}
	return otlptracegrpc.New(ctx, options...)
}

func newGRPCMetricExporter(ctx context.Context, exporter config.TelemetryExporterConfig) (metric.Exporter, error) {
	endpoint, err := exporterEndpoint(exporter)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := tlsConfigForExporter(exporter.TLS)
	if err != nil {
		return nil, err
	}
	options := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithTimeout(exporterTimeout(exporter)),
		otlpmetricgrpc.WithHeaders(exporterHeaders(exporter.Auth)),
		otlpmetricgrpc.WithCompressor(grpcCompressor(exporter.Compression)),
	}
	options = appendMetricGRPCEndpointOptions(options, endpoint)
	if endpointUsesPlaintext(endpoint) {
		options = append(options, otlpmetricgrpc.WithInsecure())
	} else {
		options = append(options, otlpmetricgrpc.WithTLSCredentials(credentials.NewTLS(tlsConfig)))
	}
	return otlpmetricgrpc.New(ctx, options...)
}

func appendHTTPEndpointOptions(options []otlptracehttp.Option, endpoint string, defaultPath string) []otlptracehttp.Option {
	if endpointHasScheme(endpoint) {
		return append(options, otlptracehttp.WithEndpointURL(endpoint))
	}
	return append(options, otlptracehttp.WithEndpoint(endpoint), otlptracehttp.WithURLPath(defaultPath))
}

func appendMetricHTTPEndpointOptions(options []otlpmetrichttp.Option, endpoint string, defaultPath string) []otlpmetrichttp.Option {
	if endpointHasScheme(endpoint) {
		return append(options, otlpmetrichttp.WithEndpointURL(endpoint))
	}
	return append(options, otlpmetrichttp.WithEndpoint(endpoint), otlpmetrichttp.WithURLPath(defaultPath))
}

func appendGRPCEndpointOptions(options []otlptracegrpc.Option, endpoint string) []otlptracegrpc.Option {
	if endpointHasScheme(endpoint) {
		return append(options, otlptracegrpc.WithEndpointURL(endpoint))
	}
	return append(options, otlptracegrpc.WithEndpoint(endpoint))
}

func appendMetricGRPCEndpointOptions(options []otlpmetricgrpc.Option, endpoint string) []otlpmetricgrpc.Option {
	if endpointHasScheme(endpoint) {
		return append(options, otlpmetricgrpc.WithEndpointURL(endpoint))
	}
	return append(options, otlpmetricgrpc.WithEndpoint(endpoint))
}

func traceHTTPCompression(compression config.TelemetryExporterCompression) otlptracehttp.Compression {
	if compression == config.TelemetryExporterCompressionGzip {
		return otlptracehttp.GzipCompression
	}
	return otlptracehttp.NoCompression
}

func metricHTTPCompression(compression config.TelemetryExporterCompression) otlpmetrichttp.Compression {
	if compression == config.TelemetryExporterCompressionGzip {
		return otlpmetrichttp.GzipCompression
	}
	return otlpmetrichttp.NoCompression
}

func grpcCompressor(compression config.TelemetryExporterCompression) string {
	if compression == config.TelemetryExporterCompressionGzip {
		return "gzip"
	}
	return ""
}

func exporterEndpoint(exporter config.TelemetryExporterConfig) (string, error) {
	endpoint := strings.TrimSpace(exporter.Endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("telemetry exporter endpoint is required")
	}
	return endpoint, nil
}

func exporterTimeout(exporter config.TelemetryExporterConfig) time.Duration {
	if exporter.Timeout <= 0 {
		return fallbackExporterTimeout
	}
	return exporter.Timeout
}

func exporterHeaders(auth config.TelemetryExporterAuthConfig) map[string]string {
	if auth.Mode != config.TelemetryExporterAuthModeAuthorizationHeader {
		return map[string]string{}
	}
	header := strings.TrimSpace(auth.AuthorizationHeader)
	if header == "" {
		return map[string]string{}
	}
	return map[string]string{authorizationHeaderKey: header}
}

func endpointHasScheme(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}

func endpointUsesPlaintext(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	return err == nil && strings.EqualFold(parsed.Scheme, "http")
}

func tlsConfigForExporter(settings config.TelemetryExporterTLSConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: settings.InsecureSkipVerify, // configured by startup telemetry settings
	}
	caFile := strings.TrimSpace(settings.CAFile)
	if caFile == "" {
		return tlsConfig, nil
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	pemBytes, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read telemetry exporter CA file: %w", err)
	}
	if ok := roots.AppendCertsFromPEM(pemBytes); !ok {
		return nil, fmt.Errorf("telemetry exporter CA file did not contain PEM certificates")
	}
	tlsConfig.RootCAs = roots
	return tlsConfig, nil
}

func appendError(errs []error, err error) []error {
	if err == nil {
		return errs
	}
	return append(errs, err)
}
