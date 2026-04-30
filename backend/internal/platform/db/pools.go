package db

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

type LanePool struct {
	lane config.PostgresPoolLane
	pool *pgxpool.Pool
}

type DatabasePools struct {
	Management       LanePool
	RuntimeExecution LanePool
	RuntimeTelemetry LanePool
	RuntimeFeedback  LanePool
	Realtime         LanePool
	CacheRefresh     LanePool
	BackgroundJobs   LanePool
	closeOnce        sync.Once
}

type PoolMetricSnapshot struct {
	Lane                   config.PostgresPoolLane
	AcquiredConnections    int32
	IdleConnections        int32
	TotalConnections       int32
	MaxConnections         int32
	AcquireCount           int64
	AcquireDurationSeconds float64
	AcquireTimeoutCount    int64
	EmptyAcquireCount      int64
}

func OpenDatabasePools(ctx context.Context, databaseURL string, budget config.PostgresPoolsBudget) (*DatabasePools, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("database URL is required")
	}
	if err := budget.Validate(); err != nil {
		return nil, err
	}
	slog.Info(
		fmt.Sprintf(
			"postgres pool budget: total_max_conns=%d management=%d runtime_execution=%d runtime_telemetry=%d runtime_feedback=%d realtime=%d cache_refresh=%d background_jobs=%d",
			budget.TotalMaxConns,
			budget.Management.MaxConns,
			budget.RuntimeExecution.MaxConns,
			budget.RuntimeTelemetry.MaxConns,
			budget.RuntimeFeedback.MaxConns,
			budget.Realtime.MaxConns,
			budget.CacheRefresh.MaxConns,
			budget.BackgroundJobs.MaxConns,
		),
		"total_max_conns", budget.TotalMaxConns,
		"management", budget.Management.MaxConns,
		"runtime_execution", budget.RuntimeExecution.MaxConns,
		"runtime_telemetry", budget.RuntimeTelemetry.MaxConns,
		"runtime_feedback", budget.RuntimeFeedback.MaxConns,
		"realtime", budget.Realtime.MaxConns,
		"cache_refresh", budget.CacheRefresh.MaxConns,
		"background_jobs", budget.BackgroundJobs.MaxConns,
	)
	pools := &DatabasePools{}
	created := make([]LanePool, 0, 7)
	create := func(lane config.PostgresPoolLane, laneBudget config.DatabasePoolBudget) (LanePool, error) {
		pool, err := openLanePool(ctx, databaseURL, lane, laneBudget)
		if err != nil {
			return LanePool{}, err
		}
		handle := LanePool{lane: lane, pool: pool}
		created = append(created, handle)
		return handle, nil
	}
	var err error
	if pools.Management, err = create(config.PostgresLaneManagement, budget.Management); err != nil {
		closeCreatedLanePools(created)
		return nil, err
	}
	if pools.RuntimeExecution, err = create(config.PostgresLaneRuntimeExecution, budget.RuntimeExecution); err != nil {
		closeCreatedLanePools(created)
		return nil, err
	}
	if pools.RuntimeTelemetry, err = create(config.PostgresLaneRuntimeTelemetry, budget.RuntimeTelemetry); err != nil {
		closeCreatedLanePools(created)
		return nil, err
	}
	if pools.RuntimeFeedback, err = create(config.PostgresLaneRuntimeFeedback, budget.RuntimeFeedback); err != nil {
		closeCreatedLanePools(created)
		return nil, err
	}
	if pools.Realtime, err = create(config.PostgresLaneRealtime, budget.Realtime); err != nil {
		closeCreatedLanePools(created)
		return nil, err
	}
	if pools.CacheRefresh, err = create(config.PostgresLaneCacheRefresh, budget.CacheRefresh); err != nil {
		closeCreatedLanePools(created)
		return nil, err
	}
	if pools.BackgroundJobs, err = create(config.PostgresLaneBackgroundJobs, budget.BackgroundJobs); err != nil {
		closeCreatedLanePools(created)
		return nil, err
	}
	return pools, nil
}

func (p LanePool) Lane() config.PostgresPoolLane {
	return p.lane
}

func (p LanePool) Raw() *pgxpool.Pool {
	return p.pool
}

func (p *DatabasePools) Close() {
	if p == nil {
		return
	}
	p.closeOnce.Do(func() {
		for _, lanePool := range []LanePool{p.BackgroundJobs, p.CacheRefresh, p.Realtime, p.RuntimeFeedback, p.RuntimeTelemetry, p.RuntimeExecution, p.Management} {
			closeLanePool(lanePool)
		}
	})
}

func (p *DatabasePools) Metrics() []PoolMetricSnapshot {
	if p == nil {
		return nil
	}
	snapshots := make([]PoolMetricSnapshot, 0, 7)
	for _, lanePool := range []LanePool{p.Management, p.RuntimeExecution, p.RuntimeTelemetry, p.RuntimeFeedback, p.Realtime, p.CacheRefresh, p.BackgroundJobs} {
		if lanePool.pool == nil {
			continue
		}
		stat := lanePool.pool.Stat()
		snapshots = append(snapshots, PoolMetricSnapshot{
			Lane:                   lanePool.lane,
			AcquiredConnections:    stat.AcquiredConns(),
			IdleConnections:        stat.IdleConns(),
			TotalConnections:       stat.TotalConns(),
			MaxConnections:         stat.MaxConns(),
			AcquireCount:           stat.AcquireCount(),
			AcquireDurationSeconds: stat.AcquireDuration().Seconds(),
			AcquireTimeoutCount:    stat.CanceledAcquireCount(),
			EmptyAcquireCount:      stat.EmptyAcquireCount(),
		})
	}
	return snapshots
}

func MetricsHandler(pools *DatabasePools) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		for _, snapshot := range pools.Metrics() {
			lane := string(snapshot.Lane)
			_, _ = fmt.Fprintf(w, "prism_db_pool_acquired_connections{lane=%q} %d\n", lane, snapshot.AcquiredConnections)
			_, _ = fmt.Fprintf(w, "prism_db_pool_idle_connections{lane=%q} %d\n", lane, snapshot.IdleConnections)
			_, _ = fmt.Fprintf(w, "prism_db_pool_total_connections{lane=%q} %d\n", lane, snapshot.TotalConnections)
			_, _ = fmt.Fprintf(w, "prism_db_pool_max_connections{lane=%q} %d\n", lane, snapshot.MaxConnections)
			_, _ = fmt.Fprintf(w, "prism_db_pool_acquire_count{lane=%q} %d\n", lane, snapshot.AcquireCount)
			_, _ = fmt.Fprintf(w, "prism_db_pool_acquire_duration_seconds{lane=%q} %.9f\n", lane, snapshot.AcquireDurationSeconds)
			_, _ = fmt.Fprintf(w, "prism_db_pool_acquire_timeout_count{lane=%q} %d\n", lane, snapshot.AcquireTimeoutCount)
			_, _ = fmt.Fprintf(w, "prism_db_pool_empty_acquire_count{lane=%q} %d\n", lane, snapshot.EmptyAcquireCount)
		}
	}
}

func openLanePool(ctx context.Context, databaseURL string, lane config.PostgresPoolLane, budget config.DatabasePoolBudget) (*pgxpool.Pool, error) {
	parsedConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse postgres pool config lane=%s: %w", lane, err)
	}
	parsedConfig.MaxConns = budget.MaxConns
	parsedConfig.MinIdleConns = budget.MinIdleConns
	pool, err := pgxpool.NewWithConfig(ctx, parsedConfig)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool lane=%s: %w", lane, err)
	}
	slog.Info("postgres pool created", "lane", lane, "max_conns", budget.MaxConns, "min_idle_conns", budget.MinIdleConns)
	return pool, nil
}

func closeCreatedLanePools(pools []LanePool) {
	for index := len(pools) - 1; index >= 0; index-- {
		closeLanePool(pools[index])
	}
}

func closeLanePool(pool LanePool) {
	if pool.pool == nil {
		return
	}
	startedAt := time.Now()
	pool.pool.Close()
	slog.Info("closed postgres pool", "lane", pool.lane, "elapsed", time.Since(startedAt))
}

func ComponentLaneAssignments() map[string]config.PostgresPoolLane {
	assignments := map[string]config.PostgresPoolLane{
		"runtime_execution": config.PostgresLaneRuntimeExecution,
		"runtime_telemetry": config.PostgresLaneRuntimeTelemetry,
		"runtime_feedback":  config.PostgresLaneRuntimeFeedback,
		"management":        config.PostgresLaneManagement,
		"realtime":          config.PostgresLaneRealtime,
		"cache_refresh":     config.PostgresLaneCacheRefresh,
		"background_jobs":   config.PostgresLaneBackgroundJobs,
	}
	return assignments
}

func SortedLaneNames() []string {
	lanes := make([]string, 0, len(ComponentLaneAssignments()))
	for _, lane := range ComponentLaneAssignments() {
		lanes = append(lanes, string(lane))
	}
	sort.Strings(lanes)
	return lanes
}
