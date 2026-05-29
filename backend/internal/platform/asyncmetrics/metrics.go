package asyncmetrics

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

const (
	OutcomeSuccess          = "success"
	OutcomeFailure          = "failure"
	OutcomeTimeout          = "timeout"
	OutcomeCanceled         = "canceled"
	OutcomeAccepted         = "accepted"
	OutcomeCoalesced        = "coalesced"
	OutcomeBackpressure     = "backpressure"
	OutcomeRejected         = "rejected"
	OutcomeSkipped          = "skipped"
	OutcomeRetryScheduled   = "retry_scheduled"
	OutcomeRetryExhausted   = "retry_exhausted"
	OutcomePermanentFailure = "permanent_failure"
	OutcomeTransientFailure = "transient_failure"
	OutcomeUnavailable      = "unavailable"
	OutcomeInvalid          = "invalid"
	OutcomeOther            = "other"
)

var (
	meter = otel.Meter("github.com/coachpo/prism/backend/internal/platform/asyncmetrics")

	initOnce sync.Once
	initErr  error

	queueDepth       otelmetric.Int64Histogram
	batchSize        otelmetric.Int64Histogram
	inflight         otelmetric.Int64UpDownCounter
	workDuration     otelmetric.Float64Histogram
	workOutcomes     otelmetric.Int64Counter
	workRetries      otelmetric.Int64Counter
	outboundDuration otelmetric.Float64Histogram
	outboundOutcomes otelmetric.Int64Counter
)

func RecordQueueDepth(ctx context.Context, component string, operation string, depth int64) {
	if depth < 0 {
		depth = 0
	}
	if initInstruments() != nil || queueDepth == nil {
		return
	}
	queueDepth.Record(contextOrBackground(ctx), depth, otelmetric.WithAttributes(baseAttrs(component, operation)...))
}

func RecordBatchSize(ctx context.Context, component string, operation string, size int64) {
	if size < 0 {
		size = 0
	}
	if initInstruments() != nil || batchSize == nil {
		return
	}
	batchSize.Record(contextOrBackground(ctx), size, otelmetric.WithAttributes(baseAttrs(component, operation)...))
}

func AddInflight(ctx context.Context, component string, operation string, delta int64) {
	if delta == 0 || initInstruments() != nil || inflight == nil {
		return
	}
	inflight.Add(contextOrBackground(ctx), delta, otelmetric.WithAttributes(baseAttrs(component, operation)...))
}

func RecordDuration(ctx context.Context, component string, operation string, outcome string, elapsed time.Duration) {
	if elapsed < 0 {
		elapsed = 0
	}
	if initInstruments() != nil || workDuration == nil {
		return
	}
	workDuration.Record(contextOrBackground(ctx), elapsed.Seconds(), otelmetric.WithAttributes(outcomeAttrs(component, operation, outcome)...))
}

func RecordOutcome(ctx context.Context, component string, operation string, outcome string) {
	if initInstruments() != nil || workOutcomes == nil {
		return
	}
	workOutcomes.Add(contextOrBackground(ctx), 1, otelmetric.WithAttributes(outcomeAttrs(component, operation, outcome)...))
}

func RecordRetry(ctx context.Context, component string, operation string, outcome string) {
	if initInstruments() != nil || workRetries == nil {
		return
	}
	workRetries.Add(contextOrBackground(ctx), 1, otelmetric.WithAttributes(outcomeAttrs(component, operation, outcome)...))
}

func RecordOutbound(ctx context.Context, component string, operation string, outcome string, elapsed time.Duration) {
	if elapsed < 0 {
		elapsed = 0
	}
	if initInstruments() != nil {
		return
	}
	if outboundOutcomes != nil {
		outboundOutcomes.Add(contextOrBackground(ctx), 1, otelmetric.WithAttributes(outcomeAttrs(component, operation, outcome)...))
	}
	if outboundDuration != nil {
		outboundDuration.Record(contextOrBackground(ctx), elapsed.Seconds(), otelmetric.WithAttributes(outcomeAttrs(component, operation, outcome)...))
	}
}

func OutcomeFromError(err error) string {
	if err == nil {
		return OutcomeSuccess
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return OutcomeTimeout
	}
	if errors.Is(err, context.Canceled) {
		return OutcomeCanceled
	}
	return OutcomeFailure
}

func initInstruments() error {
	initOnce.Do(func() {
		var err error
		queueDepth, err = meter.Int64Histogram("prism.async.queue.depth", otelmetric.WithDescription("Observed async queue depth samples by bounded component and operation."), otelmetric.WithUnit("1"))
		if err != nil {
			initErr = err
			return
		}
		batchSize, err = meter.Int64Histogram("prism.async.batch.size", otelmetric.WithDescription("Async worker batch sizes by bounded component and operation."), otelmetric.WithUnit("1"))
		if err != nil {
			initErr = err
			return
		}
		inflight, err = meter.Int64UpDownCounter("prism.async.work.inflight", otelmetric.WithDescription("Inflight async work by bounded component and operation."), otelmetric.WithUnit("1"))
		if err != nil {
			initErr = err
			return
		}
		workDuration, err = meter.Float64Histogram("prism.async.work.duration", otelmetric.WithDescription("Async work duration by bounded component, operation, and outcome."), otelmetric.WithUnit("s"))
		if err != nil {
			initErr = err
			return
		}
		workOutcomes, err = meter.Int64Counter("prism.async.work.outcomes", otelmetric.WithDescription("Async work outcomes by bounded component and operation."), otelmetric.WithUnit("1"))
		if err != nil {
			initErr = err
			return
		}
		workRetries, err = meter.Int64Counter("prism.async.work.retries", otelmetric.WithDescription("Async retry decisions by bounded component, operation, and outcome."), otelmetric.WithUnit("1"))
		if err != nil {
			initErr = err
			return
		}
		outboundDuration, err = meter.Float64Histogram("prism.outbound.duration", otelmetric.WithDescription("Outbound side-effect duration by bounded component, operation, and outcome."), otelmetric.WithUnit("s"))
		if err != nil {
			initErr = err
			return
		}
		outboundOutcomes, err = meter.Int64Counter("prism.outbound.outcomes", otelmetric.WithDescription("Outbound side-effect outcomes by bounded component and operation."), otelmetric.WithUnit("1"))
		if err != nil {
			initErr = err
		}
	})
	return initErr
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func baseAttrs(component string, operation string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("component", safeLabel(component)),
		attribute.String("operation", safeLabel(operation)),
	}
}

func outcomeAttrs(component string, operation string, outcome string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("component", safeLabel(component)),
		attribute.String("operation", safeLabel(operation)),
		attribute.String("outcome", safeOutcome(outcome)),
	}
}

func safeOutcome(value string) string {
	switch safeLabel(value) {
	case OutcomeSuccess, OutcomeFailure, OutcomeTimeout, OutcomeCanceled, OutcomeAccepted, OutcomeCoalesced:
		return safeLabel(value)
	case OutcomeBackpressure, OutcomeRejected, OutcomeSkipped, OutcomeRetryScheduled, OutcomeRetryExhausted:
		return safeLabel(value)
	case OutcomePermanentFailure, OutcomeTransientFailure, OutcomeUnavailable, OutcomeInvalid:
		return safeLabel(value)
	default:
		return OutcomeOther
	}
}

func safeLabel(value string) string {
	label := strings.ToLower(strings.TrimSpace(value))
	if label == "" || len(label) > 96 || strings.Contains(label, "://") || strings.Contains(label, "@") {
		return OutcomeOther
	}
	if containsSensitiveFragment(label) {
		return OutcomeOther
	}
	for _, r := range label {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '_', '-', '.', ':', '/':
			continue
		default:
			return OutcomeOther
		}
	}
	return label
}

func containsSensitiveFragment(value string) bool {
	for _, fragment := range []string{"password", "passwd", "secret", "token", "credential", "authorization", "api_key", "apikey"} {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
