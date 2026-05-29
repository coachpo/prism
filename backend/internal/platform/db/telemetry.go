package db

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

	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

var (
	dbPoolTelemetryMeter    = otel.Meter("github.com/coachpo/prism/backend/internal/platform/db")
	dbPoolTelemetryTracer   = otel.Tracer("github.com/coachpo/prism/backend/internal/platform/db")
	dbPoolTelemetryInitOnce sync.Once
	dbPoolTelemetryInitErr  error
	dbPoolTelemetryHandles  dbPoolTelemetryInstruments
	dbPoolRegistrations     sync.Map
)

type dbPoolTelemetryInstruments struct {
	poolCreateDuration otelmetric.Float64Histogram
	acquired           otelmetric.Int64ObservableGauge
	idle               otelmetric.Int64ObservableGauge
	total              otelmetric.Int64ObservableGauge
	max                otelmetric.Int64ObservableGauge
	saturation         otelmetric.Float64ObservableGauge
	acquireCount       otelmetric.Int64ObservableCounter
	acquireDuration    otelmetric.Float64ObservableCounter
	acquireTimeout     otelmetric.Int64ObservableCounter
	emptyAcquire       otelmetric.Int64ObservableCounter
}

func initDBPoolTelemetryInstruments() (dbPoolTelemetryInstruments, error) {
	dbPoolTelemetryInitOnce.Do(func() {
		var err error
		dbPoolTelemetryHandles.poolCreateDuration, err = dbPoolTelemetryMeter.Float64Histogram(
			"prism.db.pool.create.duration",
			otelmetric.WithDescription("Postgres pool creation duration by lane."),
			otelmetric.WithUnit("s"),
		)
		if err != nil {
			dbPoolTelemetryInitErr = err
			return
		}
		dbPoolTelemetryHandles.acquired, err = dbPoolTelemetryMeter.Int64ObservableGauge(
			"prism.db.pool.acquired_connections",
			otelmetric.WithDescription("Acquired Postgres pool connections by lane."),
			otelmetric.WithUnit("1"),
		)
		if err != nil {
			dbPoolTelemetryInitErr = err
			return
		}
		dbPoolTelemetryHandles.idle, err = dbPoolTelemetryMeter.Int64ObservableGauge(
			"prism.db.pool.idle_connections",
			otelmetric.WithDescription("Idle Postgres pool connections by lane."),
			otelmetric.WithUnit("1"),
		)
		if err != nil {
			dbPoolTelemetryInitErr = err
			return
		}
		dbPoolTelemetryHandles.total, err = dbPoolTelemetryMeter.Int64ObservableGauge(
			"prism.db.pool.total_connections",
			otelmetric.WithDescription("Total Postgres pool connections by lane."),
			otelmetric.WithUnit("1"),
		)
		if err != nil {
			dbPoolTelemetryInitErr = err
			return
		}
		dbPoolTelemetryHandles.max, err = dbPoolTelemetryMeter.Int64ObservableGauge(
			"prism.db.pool.max_connections",
			otelmetric.WithDescription("Maximum Postgres pool connections by lane."),
			otelmetric.WithUnit("1"),
		)
		if err != nil {
			dbPoolTelemetryInitErr = err
			return
		}
		dbPoolTelemetryHandles.saturation, err = dbPoolTelemetryMeter.Float64ObservableGauge(
			"prism.db.pool.saturation",
			otelmetric.WithDescription("Acquired divided by maximum Postgres pool connections by lane."),
			otelmetric.WithUnit("1"),
		)
		if err != nil {
			dbPoolTelemetryInitErr = err
			return
		}
		dbPoolTelemetryHandles.acquireCount, err = dbPoolTelemetryMeter.Int64ObservableCounter(
			"prism.db.pool.acquire.count",
			otelmetric.WithDescription("Cumulative Postgres pool acquire count by lane."),
			otelmetric.WithUnit("1"),
		)
		if err != nil {
			dbPoolTelemetryInitErr = err
			return
		}
		dbPoolTelemetryHandles.acquireDuration, err = dbPoolTelemetryMeter.Float64ObservableCounter(
			"prism.db.pool.acquire.duration",
			otelmetric.WithDescription("Cumulative Postgres pool acquire duration by lane."),
			otelmetric.WithUnit("s"),
		)
		if err != nil {
			dbPoolTelemetryInitErr = err
			return
		}
		dbPoolTelemetryHandles.acquireTimeout, err = dbPoolTelemetryMeter.Int64ObservableCounter(
			"prism.db.pool.acquire.timeout.count",
			otelmetric.WithDescription("Cumulative Postgres pool canceled acquire count by lane."),
			otelmetric.WithUnit("1"),
		)
		if err != nil {
			dbPoolTelemetryInitErr = err
			return
		}
		dbPoolTelemetryHandles.emptyAcquire, err = dbPoolTelemetryMeter.Int64ObservableCounter(
			"prism.db.pool.empty_acquire.count",
			otelmetric.WithDescription("Cumulative Postgres pool empty acquire count by lane."),
			otelmetric.WithUnit("1"),
		)
		if err != nil {
			dbPoolTelemetryInitErr = err
		}
	})
	return dbPoolTelemetryHandles, dbPoolTelemetryInitErr
}

func startPoolCreateTelemetry(ctx context.Context, lane config.PostgresPoolLane) (context.Context, func(error)) {
	laneValue := string(lane)
	startedAt := time.Now()
	ctx, span := dbPoolTelemetryTracer.Start(
		ctx,
		"db.pool.create",
		trace.WithAttributes(attribute.String("db.pool.lane", laneValue)),
	)
	return ctx, func(err error) {
		if instruments, instrumentErr := initDBPoolTelemetryInstruments(); instrumentErr == nil {
			instruments.poolCreateDuration.Record(ctx, time.Since(startedAt).Seconds(), otelmetric.WithAttributes(attribute.String("lane", laneValue)))
		}
		if err != nil {
			span.SetStatus(codes.Error, "pool_create_failed")
		}
		span.End()
	}
}

func (p *DatabasePools) registerTelemetry() {
	if p == nil {
		return
	}
	instruments, err := initDBPoolTelemetryInstruments()
	if err != nil {
		return
	}
	registration, err := dbPoolTelemetryMeter.RegisterCallback(
		func(ctx context.Context, observer otelmetric.Observer) error {
			for _, snapshot := range p.Metrics() {
				attrs := otelmetric.WithAttributes(attribute.String("lane", string(snapshot.Lane)))
				observer.ObserveInt64(instruments.acquired, int64(snapshot.AcquiredConnections), attrs)
				observer.ObserveInt64(instruments.idle, int64(snapshot.IdleConnections), attrs)
				observer.ObserveInt64(instruments.total, int64(snapshot.TotalConnections), attrs)
				observer.ObserveInt64(instruments.max, int64(snapshot.MaxConnections), attrs)
				observer.ObserveFloat64(instruments.saturation, poolSaturation(snapshot), attrs)
				observer.ObserveInt64(instruments.acquireCount, snapshot.AcquireCount, attrs)
				observer.ObserveFloat64(instruments.acquireDuration, snapshot.AcquireDurationSeconds, attrs)
				observer.ObserveInt64(instruments.acquireTimeout, snapshot.AcquireTimeoutCount, attrs)
				observer.ObserveInt64(instruments.emptyAcquire, snapshot.EmptyAcquireCount, attrs)
			}
			return nil
		},
		instruments.acquired,
		instruments.idle,
		instruments.total,
		instruments.max,
		instruments.saturation,
		instruments.acquireCount,
		instruments.acquireDuration,
		instruments.acquireTimeout,
		instruments.emptyAcquire,
	)
	if err != nil {
		return
	}
	dbPoolRegistrations.Store(p, registration)
}

func (p *DatabasePools) unregisterTelemetry() {
	if p == nil {
		return
	}
	registrationValue, ok := dbPoolRegistrations.LoadAndDelete(p)
	if !ok {
		return
	}
	registration, ok := registrationValue.(otelmetric.Registration)
	if !ok {
		return
	}
	_ = registration.Unregister()
}

func registerLanePoolTelemetry(pool *pgxpool.Pool, lane config.PostgresPoolLane) {
	pgxutil.RegisterPoolLane(pool, string(lane))
}

func unregisterLanePoolTelemetry(pool *pgxpool.Pool) {
	pgxutil.UnregisterPoolLane(pool)
}

func poolSaturation(snapshot PoolMetricSnapshot) float64 {
	if snapshot.MaxConnections <= 0 {
		return 0
	}
	return float64(snapshot.AcquiredConnections) / float64(snapshot.MaxConnections)
}
