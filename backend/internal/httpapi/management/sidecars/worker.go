package sidecars

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/background"
)

const (
	SidecarSyncWorkerName     = background.WorkerName("sidecar_snapshot_sync")
	SidecarWatchdogWorkerName = background.WorkerName("sidecar_watchdog_reconcile")

	sidecarSyncWorkerInterval     = 30 * time.Second
	sidecarSyncWorkerInitialDelay = 15 * time.Second
	sidecarSyncWorkerTimeout      = 30 * time.Second

	sidecarWatchdogWorkerInterval     = 30 * time.Second
	sidecarWatchdogWorkerInitialDelay = 20 * time.Second
	sidecarWatchdogWorkerTimeout      = 30 * time.Second
)

func (s *Service) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if s == nil || scheduler == nil {
		return nil
	}
	if err := scheduler.Register(background.WorkerSpec{
		Name:             SidecarSyncWorkerName,
		Priority:         background.PriorityLowBackground,
		MaxPriority:      background.PriorityLowBackground,
		QueueLimit:       1,
		ConcurrencyLimit: 1,
		DrainPolicy:      background.DrainBestEffort,
		CoalescePolicy:   background.CoalesceDropNew,
		PeriodicTrigger:  &background.PeriodicTrigger{Interval: sidecarSyncWorkerInterval, InitialDelay: sidecarSyncWorkerInitialDelay},
		Timeout:          sidecarSyncWorkerTimeout,
	}, s.handleScheduledSidecarSync); err != nil {
		return err
	}
	return scheduler.Register(background.WorkerSpec{
		Name:             SidecarWatchdogWorkerName,
		Priority:         background.PriorityLowBackground,
		MaxPriority:      background.PriorityLowBackground,
		QueueLimit:       1,
		ConcurrencyLimit: 1,
		DrainPolicy:      background.DrainBestEffort,
		CoalescePolicy:   background.CoalesceDropNew,
		PeriodicTrigger:  &background.PeriodicTrigger{Interval: sidecarWatchdogWorkerInterval, InitialDelay: sidecarWatchdogWorkerInitialDelay},
		Timeout:          sidecarWatchdogWorkerTimeout,
	}, s.handleScheduledSidecarWatchdog)
}

func (s *Service) handleScheduledSidecarSync(ctx context.Context, _ background.Job) background.JobResult {
	summary, err := s.SyncDueSidecars(ctx)
	if err != nil {
		err = fmt.Errorf("sync due sidecars: %w", err)
		slog.Error("sidecar sync worker failed", "error", err)
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	if summary.Failed > 0 {
		slog.Warn("sidecar sync worker completed with sidecar failures", "checked", summary.Checked, "synced", summary.Synced, "skipped", summary.Skipped, "failed", summary.Failed)
	} else {
		slog.Debug("sidecar sync worker completed", "checked", summary.Checked, "synced", summary.Synced, "skipped", summary.Skipped)
	}
	return background.JobResult{Status: background.JobSucceeded}
}

func (s *Service) handleScheduledSidecarWatchdog(ctx context.Context, _ background.Job) background.JobResult {
	summary, err := s.ReconcileWatchdogDueSidecars(ctx)
	if err != nil {
		err = fmt.Errorf("reconcile sidecar watchdog: %w", err)
		slog.Error("sidecar watchdog worker failed", "error", err)
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	if summary.Failed > 0 {
		slog.Warn("sidecar watchdog worker completed with sidecar failures", "checked", summary.Checked, "reconciled", summary.Reconciled, "skipped", summary.Skipped, "failed", summary.Failed)
	} else {
		slog.Debug("sidecar watchdog worker completed", "checked", summary.Checked, "reconciled", summary.Reconciled, "skipped", summary.Skipped)
	}
	return background.JobResult{Status: background.JobSucceeded}
}
