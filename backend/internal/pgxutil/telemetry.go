package pgxutil

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const unknownDBLane = "unknown"

var (
	dbTransactionMeter  = otel.Meter("github.com/coachpo/prism/backend/internal/pgxutil")
	dbTransactionTracer = otel.Tracer("github.com/coachpo/prism/backend/internal/pgxutil")

	dbLaneRegistry        sync.Map
	dbTransactionInitOnce sync.Once
	dbTransactionInitErr  error

	dbTransactionAcquireDuration otelmetric.Float64Histogram
	dbTransactionDuration        otelmetric.Float64Histogram
)

func RegisterPoolLane(pool *pgxpool.Pool, lane string) {
	if pool == nil {
		return
	}
	dbLaneRegistry.Store(pool, normalizeDBLane(lane))
}

func UnregisterPoolLane(pool *pgxpool.Pool) {
	if pool == nil {
		return
	}
	dbLaneRegistry.Delete(pool)
}

func initDBTransactionTelemetryInstruments() error {
	dbTransactionInitOnce.Do(func() {
		var err error
		dbTransactionAcquireDuration, err = dbTransactionMeter.Float64Histogram(
			"prism.db.transaction.acquire.duration",
			otelmetric.WithDescription("Transaction begin/acquire duration by database lane."),
			otelmetric.WithUnit("s"),
		)
		if err != nil {
			dbTransactionInitErr = err
			return
		}
		dbTransactionDuration, err = dbTransactionMeter.Float64Histogram(
			"prism.db.transaction.duration",
			otelmetric.WithDescription("Transaction duration by database lane."),
			otelmetric.WithUnit("s"),
		)
		if err != nil {
			dbTransactionInitErr = err
		}
	})
	return dbTransactionInitErr
}

func startTransactionTelemetry(ctx context.Context, beginner Beginner) (context.Context, func(error)) {
	lane := poolLaneForTelemetry(beginner)
	startedAt := time.Now()
	ctx, span := dbTransactionTracer.Start(
		ctx,
		"db.transaction",
		trace.WithAttributes(attribute.String("db.pool.lane", lane)),
	)
	return ctx, func(err error) {
		if initDBTransactionTelemetryInstruments() == nil {
			dbTransactionDuration.Record(ctx, time.Since(startedAt).Seconds(), otelmetric.WithAttributes(attribute.String("lane", lane)))
		}
		if err != nil {
			span.SetStatus(codes.Error, "transaction_failed")
		}
		span.End()
	}
}

func recordTransactionAcquire(ctx context.Context, beginner Beginner, elapsed time.Duration, err error) {
	lane := poolLaneForTelemetry(beginner)
	trace.SpanFromContext(ctx).AddEvent("db.transaction.acquire", trace.WithAttributes(attribute.String("db.pool.lane", lane)))
	if initDBTransactionTelemetryInstruments() == nil {
		dbTransactionAcquireDuration.Record(ctx, elapsed.Seconds(), otelmetric.WithAttributes(attribute.String("lane", lane)))
	}
	if err != nil {
		trace.SpanFromContext(ctx).SetStatus(codes.Error, "transaction_begin_failed")
	}
}

func poolLaneForTelemetry(beginner Beginner) string {
	pool, ok := beginner.(*pgxpool.Pool)
	if !ok || pool == nil {
		return unknownDBLane
	}
	value, ok := dbLaneRegistry.Load(pool)
	if !ok {
		return unknownDBLane
	}
	lane, ok := value.(string)
	if !ok {
		return unknownDBLane
	}
	return normalizeDBLane(lane)
}

func normalizeDBLane(lane string) string {
	switch lane {
	case "management", "runtime_execution", "runtime_telemetry":
		return lane
	case "runtime_feedback", "realtime", "cache_refresh", "background_jobs":
		return lane
	default:
		return unknownDBLane
	}
}
