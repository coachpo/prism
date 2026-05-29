package platformhttp

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	httpTelemetryBranchManagement = "management"
	httpTelemetryBranchRuntime    = "runtime"
	httpTelemetryBranchOther      = "other"
	admissionTelemetryTierNone    = "none"
)

type routeTemplateFunc func(method string, rawPath string) string

var (
	httpTelemetryMeter  = otel.Meter("github.com/coachpo/prism/backend/internal/platform/http")
	httpTelemetryTracer = otel.Tracer("github.com/coachpo/prism/backend/internal/platform/http")

	httpTelemetryInitOnce sync.Once
	httpTelemetryInitErr  error

	httpIngressRequests          otelmetric.Int64Counter
	httpIngressDuration          otelmetric.Float64Histogram
	httpAdmissionDecisionCounter otelmetric.Int64Counter
)

func initHTTPTelemetryInstruments() error {
	httpTelemetryInitOnce.Do(func() {
		var err error
		httpIngressRequests, err = httpTelemetryMeter.Int64Counter(
			"prism.http.ingress.requests",
			otelmetric.WithDescription("HTTP ingress requests by bounded branch and route template."),
			otelmetric.WithUnit("1"),
		)
		if err != nil {
			httpTelemetryInitErr = err
			return
		}
		httpIngressDuration, err = httpTelemetryMeter.Float64Histogram(
			"prism.http.ingress.duration",
			otelmetric.WithDescription("HTTP ingress duration by bounded branch and route template."),
			otelmetric.WithUnit("s"),
		)
		if err != nil {
			httpTelemetryInitErr = err
			return
		}
		httpAdmissionDecisionCounter, err = httpTelemetryMeter.Int64Counter(
			"prism.http.admission.decisions",
			otelmetric.WithDescription("HTTP admission decisions by bounded outcome."),
			otelmetric.WithUnit("1"),
		)
		if err != nil {
			httpTelemetryInitErr = err
		}
	})
	return httpTelemetryInitErr
}

func managementIngressTelemetryMiddleware(next http.Handler) http.Handler {
	return httpIngressTelemetryMiddleware(httpTelemetryBranchManagement, managementTelemetryRouteTemplate, next)
}

func runtimeIngressTelemetryMiddleware(next http.Handler) http.Handler {
	return httpIngressTelemetryMiddleware(httpTelemetryBranchRuntime, runtimeTelemetryRouteTemplate, next)
}

func httpIngressTelemetryMiddleware(branch string, routeTemplate routeTemplateFunc, next http.Handler) http.Handler {
	if next == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routeTemplateValue := routeTemplate(r.Method, r.URL.Path)
		method := normalizedHTTPMethod(r.Method)
		spanContext := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		spanContext, span := httpTelemetryTracer.Start(
			spanContext,
			"http.ingress."+normalizedTelemetryBranch(branch),
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(httpIngressTraceAttributes(branch, method, routeTemplateValue)...),
		)

		startedAt := time.Now()
		response := &telemetryResponseWriter{ResponseWriter: w}
		defer func() {
			statusCode := response.status()
			statusClass := httpStatusClass(statusCode)
			span.SetAttributes(
				attribute.Int("http.response.status_code", statusCode),
				attribute.String("prism.http.status_class", statusClass),
			)
			if statusCode >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, "http_server_error")
			}
			recordHTTPIngressMetrics(spanContext, branch, method, routeTemplateValue, statusClass, time.Since(startedAt))
			span.End()
		}()

		next.ServeHTTP(response, r.WithContext(spanContext))
	})
}

func recordHTTPIngressMetrics(ctx context.Context, branch string, method string, routeTemplate string, statusClass string, elapsed time.Duration) {
	if initHTTPTelemetryInstruments() != nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("branch", normalizedTelemetryBranch(branch)),
		attribute.String("method", method),
		attribute.String("route", routeTemplate),
		attribute.String("status_class", statusClass),
	}
	httpIngressRequests.Add(ctx, 1, otelmetric.WithAttributes(attrs...))
	httpIngressDuration.Record(ctx, elapsed.Seconds(), otelmetric.WithAttributes(attrs...))
}

func recordAdmissionDecision(ctx context.Context, branch string, tier string, err error) {
	outcome := admissionTelemetryOutcome(err)
	attrs := []attribute.KeyValue{
		attribute.String("branch", normalizedTelemetryBranch(branch)),
		attribute.String("tier", normalizedAdmissionTelemetryTier(tier)),
		attribute.String("outcome", outcome),
	}
	trace.SpanFromContext(ctx).AddEvent("http.admission.decision", trace.WithAttributes(attrs...))
	if initHTTPTelemetryInstruments() != nil {
		return
	}
	httpAdmissionDecisionCounter.Add(ctx, 1, otelmetric.WithAttributes(attrs...))
}

func admissionTelemetryOutcome(err error) string {
	if err == nil {
		return "admitted"
	}
	if _, ok := errors.AsType[*admission.OverloadError](err); ok {
		return "overloaded"
	}
	return "error"
}

func managementTelemetryRouteTemplate(method string, rawPath string) string {
	if spec, ok := matchManagementRouteSpec(method, rawPath); ok {
		return apiRouteTemplate(spec.pattern)
	}
	return "/api/*"
}

func apiRouteTemplate(pattern string) string {
	normalized := normalizeManagementRoutePath(pattern)
	if normalized == "" || normalized == "/" {
		return "/api"
	}
	return "/api" + normalized
}

func runtimeTelemetryRouteTemplate(method string, rawPath string) string {
	if match, ok := runtimeapi.ResolveRuntimeOperation(method, rawPath); ok {
		return match.Operation.PathTemplate
	}
	for _, operation := range runtimeapi.RuntimeOperationCatalog() {
		if _, ok := operation.PathMatcher.Match(rawPath); ok {
			return operation.PathTemplate
		}
	}
	if rawPath == "/v1" || strings.HasPrefix(rawPath, "/v1/") {
		return "/v1/*"
	}
	if rawPath == "/v1beta" || strings.HasPrefix(rawPath, "/v1beta/") {
		return "/v1beta/*"
	}
	return "unmatched"
}

func httpIngressTraceAttributes(branch string, method string, routeTemplate string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("prism.http.branch", normalizedTelemetryBranch(branch)),
		attribute.String("http.request.method", method),
		attribute.String("http.route", routeTemplate),
	}
}

func normalizedHTTPMethod(method string) string {
	normalized := strings.ToUpper(strings.TrimSpace(method))
	if normalized == "" {
		return "UNKNOWN"
	}
	switch normalized {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return normalized
	default:
		return "OTHER"
	}
}

func normalizedTelemetryBranch(branch string) string {
	switch branch {
	case httpTelemetryBranchManagement, httpTelemetryBranchRuntime:
		return branch
	default:
		return httpTelemetryBranchOther
	}
}

func normalizedAdmissionTelemetryTier(tier string) string {
	switch tier {
	case "M1", "M2", "M3":
		return tier
	case "", admissionTelemetryTierNone:
		return admissionTelemetryTierNone
	default:
		return "other"
	}
}

func httpStatusClass(statusCode int) string {
	if statusCode < 100 || statusCode > 999 {
		return "unknown"
	}
	return strconv.Itoa(statusCode/100) + "xx"
}

type telemetryResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *telemetryResponseWriter) WriteHeader(statusCode int) {
	if w.statusCode != 0 {
		return
	}
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *telemetryResponseWriter) Write(body []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func (w *telemetryResponseWriter) status() int {
	if w.statusCode == 0 {
		return http.StatusOK
	}
	return w.statusCode
}

func (w *telemetryResponseWriter) Flush() {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *telemetryResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *telemetryResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (w *telemetryResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
