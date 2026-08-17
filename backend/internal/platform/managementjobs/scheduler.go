package managementjobs

import (
	"context"
	"fmt"

	"github.com/coachpo/prism/backend/internal/platform/background"
)

const WorkerName = background.WorkerName("management_audit_delete_jobs")

func (s *Store) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if s == nil || scheduler == nil {
		return nil
	}
	s.scheduler = scheduler
	return scheduler.Register(background.WorkerSpec{Name: WorkerName, Priority: background.PriorityLowBackground, MaxPriority: background.PriorityLowBackground, QueueLimit: 32, ConcurrencyLimit: 1, DrainPolicy: background.DrainBestEffort, CoalescePolicy: background.CoalesceDropNew, RetryPolicy: &background.RetryPolicy{MaxAttempts: 3, Delay: defaultPollInterval}, PeriodicTrigger: &background.PeriodicTrigger{Interval: defaultPollInterval}, Timeout: defaultShutdownTimeout}, s.handleScheduledJobs)
}

func (s *Store) Wake(ctx context.Context) error {
	if s == nil || s.scheduler == nil {
		return nil
	}
	result := s.scheduler.Submit(ctx, background.JobRequest{Worker: WorkerName, CoalesceKey: string(WorkerName)})
	if result.Status == background.SubmitAccepted || result.Status == background.SubmitCoalesced {
		return nil
	}
	return fmt.Errorf("wake management jobs worker: %s", result.Status)
}

func (s *Store) handleScheduledJobs(ctx context.Context, _ background.Job) background.JobResult {
	// Planning: UTC day-aligned logical cutoffs and durable desired work.
	if err := s.planScheduledRetention(ctx); err != nil {
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	// Frozen v1 drain for previously accepted legacy rows, then current-contract execution.
	if err := s.ProcessDue(ctx); err != nil {
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	return background.JobResult{Status: background.JobSucceeded}
}

func (s *Store) ProcessDue(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return nil
	}
	// audit_delete jobs keep the v1 execution path.
	if auditJob, ok, err := s.claimOne(ctx); err != nil {
		return err
	} else if ok {
		return s.processAuditDelete(ctx, auditJob)
	}
	// Legacy v1 log-retention rows drain first through the frozen executor;
	// current-contract jobs follow.
	legacy, legacyOK, err := s.claimLegacyRetentionJob(ctx)
	if err != nil {
		return err
	}
	if legacyOK {
		return s.drainLegacyRetentionJob(ctx, legacy)
	}
	job, ok, err := s.claimRetentionJob(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	var processErr error
	switch {
	case job.ContractVersion == 2:
		processErr = s.processRetentionJob(ctx, job)
	default:
		processErr = s.drainLegacyRetentionJob(ctx, job)
	}
	return processErr
}
