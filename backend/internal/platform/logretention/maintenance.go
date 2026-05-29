package logretention

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/asyncmetrics"
	"github.com/coachpo/prism/backend/internal/platform/background"
)

const (
	WorkerName = background.WorkerName("log_partition_maintenance")

	maintenanceInterval = time.Hour
	maintenanceTimeout  = 2 * time.Minute
)

func (s *Store) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if s == nil || scheduler == nil {
		return nil
	}
	return scheduler.Register(background.WorkerSpec{
		Name:             WorkerName,
		Priority:         background.PriorityLowBackground,
		MaxPriority:      background.PriorityLowBackground,
		QueueLimit:       1,
		ConcurrencyLimit: 1,
		DrainPolicy:      background.DrainBestEffort,
		CoalescePolicy:   background.CoalesceDropNew,
		RetryPolicy:      &background.RetryPolicy{MaxAttempts: 3, Delay: time.Minute},
		PeriodicTrigger:  &background.PeriodicTrigger{Interval: maintenanceInterval},
		Timeout:          maintenanceTimeout,
	}, s.handlePartitionMaintenance)
}

func (s *Store) handlePartitionMaintenance(ctx context.Context, _ background.Job) background.JobResult {
	startedAt := time.Now()
	if err := s.EnsurePartitionHorizon(ctx); err != nil {
		err = fmt.Errorf("refresh log partition horizon: %w", err)
		asyncmetrics.RecordDuration(ctx, "log_retention_worker", "partition_horizon", asyncmetrics.OutcomeFailure, time.Since(startedAt))
		slog.Error("log partition maintenance failed", "error", err)
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	asyncmetrics.RecordDuration(ctx, "log_retention_worker", "partition_horizon", asyncmetrics.OutcomeSuccess, time.Since(startedAt))
	slog.Info("log partition maintenance completed")
	return background.JobResult{Status: background.JobSucceeded}
}
