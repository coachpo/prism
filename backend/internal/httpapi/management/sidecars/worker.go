package sidecars

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/asyncmetrics"
	"github.com/coachpo/prism/backend/internal/platform/background"
)

const (
	SidecarSyncWorkerName = background.WorkerName("sidecar_snapshot_sync")

	sidecarSyncWorkerInterval     = 30 * time.Second
	sidecarSyncWorkerInitialDelay = 15 * time.Second
	sidecarSyncWorkerTimeout      = 30 * time.Second
)

func (s *Service) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if s == nil || scheduler == nil {
		return nil
	}
	return scheduler.Register(background.WorkerSpec{
		Name:             SidecarSyncWorkerName,
		Priority:         background.PriorityLowBackground,
		MaxPriority:      background.PriorityLowBackground,
		QueueLimit:       1,
		ConcurrencyLimit: 1,
		DrainPolicy:      background.DrainBestEffort,
		CoalescePolicy:   background.CoalesceDropNew,
		PeriodicTrigger:  &background.PeriodicTrigger{Interval: sidecarSyncWorkerInterval, InitialDelay: sidecarSyncWorkerInitialDelay},
		Timeout:          sidecarSyncWorkerTimeout,
	}, s.handleScheduledSidecarSync)
}

func (s *Service) handleScheduledSidecarSync(ctx context.Context, _ background.Job) background.JobResult {
	startedAt := time.Now()
	summary, err := s.SyncDueSidecars(ctx)
	if err != nil {
		err = fmt.Errorf("sync due sidecar providers: %w", err)
		asyncmetrics.RecordDuration(ctx, "sidecar_worker", "due_sync", asyncmetrics.OutcomeFailure, time.Since(startedAt))
		slog.Error("sidecar provider sync worker failed", "error", err)
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	outcome := sidecarSyncSummaryTelemetryOutcome(summary, nil)
	asyncmetrics.RecordBatchSize(ctx, "sidecar_worker", "due_sync_checked", int64(summary.Checked))
	asyncmetrics.RecordDuration(ctx, "sidecar_worker", "due_sync", outcome, time.Since(startedAt))
	if summary.Failed > 0 {
		slog.Warn("sidecar provider sync worker completed with sidecar failures", "checked", summary.Checked, "synced", summary.Synced, "skipped", summary.Skipped, "failed", summary.Failed)
	} else {
		slog.Debug("sidecar provider sync worker completed", "checked", summary.Checked, "synced", summary.Synced, "skipped", summary.Skipped)
	}
	return background.JobResult{Status: background.JobSucceeded}
}
