package managementjobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
)

const (
	WorkerName = background.WorkerName("management_audit_delete_jobs")

	TypeAuditDelete  = "audit_delete"
	TypeLogRetention = "log_retention"

	defaultBatchSize       = 500
	defaultPollInterval    = 5 * time.Second
	defaultLeaseDuration   = 2 * time.Minute
	defaultShutdownTimeout = 30 * time.Second

	scheduledLogRetentionRequestedBy = "scheduled-log-retention"
	scheduledLogRetentionReason      = "scheduled global log retention cleanup"
)

type Store struct {
	pool         *pgxpool.Pool
	scheduler    *background.Scheduler
	logRetention *logretention.Store
	workerID     string
	now          func() time.Time
	batchSize    int
}

type Options struct {
	Pool         *pgxpool.Pool
	Scheduler    *background.Scheduler
	LogRetention *logretention.Store
	WorkerID     string
	Now          func() time.Time
}

type LogRetentionScope struct {
	Before    *time.Time `json:"before,omitempty"`
	Table     string     `json:"table,omitempty"`
	Cutoff    *time.Time `json:"cutoff,omitempty"`
	DeleteAll bool       `json:"delete_all,omitempty"`
}

type AuditDeleteScope = LogRetentionScope

type CreateAuditDeleteJobRequest struct {
	ProfileID      int
	RequestedBy    string
	IdempotencyKey string
	Reason         string
	Scope          AuditDeleteScope
}

type CreateLogRetentionJobRequest struct {
	RequestedBy    string
	IdempotencyKey string
	Reason         string
	Scope          LogRetentionScope
}

type JobProgress struct {
	RowsMatchedEstimate int64  `json:"rows_matched_estimate"`
	RowsDeleted         int64  `json:"rows_deleted"`
	BatchesCompleted    int64  `json:"batches_completed"`
	LastCursor          string `json:"last_cursor"`
}

type Job struct {
	ID              string            `json:"id"`
	Type            string            `json:"type"`
	State           string            `json:"state"`
	RequestedBy     string            `json:"requested_by"`
	RequestedAt     time.Time         `json:"requested_at"`
	ProfileID       int               `json:"-"`
	StartedAt       *time.Time        `json:"started_at"`
	FinishedAt      *time.Time        `json:"finished_at"`
	Scope           LogRetentionScope `json:"scope"`
	Reason          string            `json:"reason"`
	Progress        JobProgress       `json:"progress"`
	AttemptCount    int               `json:"attempt_count"`
	LastHeartbeatAt *time.Time        `json:"last_heartbeat_at"`
	CancelRequested bool              `json:"cancel_requested"`
	ErrorCode       *string           `json:"error_code"`
	ErrorMessage    *string           `json:"error_message"`
}

type JobListResponse struct {
	Items      []Job   `json:"items"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

func NewStore(options Options) *Store {
	if options.Pool == nil {
		return nil
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	workerID := strings.TrimSpace(options.WorkerID)
	if workerID == "" {
		workerID = defaultWorkerID()
	}
	return &Store{pool: options.Pool, scheduler: options.Scheduler, logRetention: options.LogRetention, workerID: workerID, now: now, batchSize: defaultBatchSize}
}

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

func (s *Store) CreateAuditDeleteJob(ctx context.Context, req CreateAuditDeleteJobRequest) (Job, error) {
	if s == nil || s.pool == nil {
		return Job{}, fmt.Errorf("management job store is required")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return Job{}, fmt.Errorf("delete_reason_required")
	}
	if !req.Scope.DeleteAll && req.Scope.Before == nil {
		return Job{}, fmt.Errorf("delete_scope_required")
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return Job{}, fmt.Errorf("delete_idempotency_required")
	}
	requestedBy := strings.TrimSpace(req.RequestedBy)
	if requestedBy == "" {
		requestedBy = fmt.Sprintf("profile:%d", req.ProfileID)
	}
	jobID := "job_" + randomHex(12)
	scopeJSON, err := json.Marshal(req.Scope)
	if err != nil {
		return Job{}, err
	}
	var id string
	err = s.pool.QueryRow(ctx, `INSERT INTO management_jobs (id, type, state, requested_by, requested_at, priority, idempotency_key, profile_id, scope_json, reason, created_at, updated_at) VALUES ($1, 'audit_delete', 'queued', $2, $3, 'maintenance', $4, $5, $6, $7, $3, $3) ON CONFLICT (type, requested_by, idempotency_key) WHERE idempotency_key IS NOT NULL DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key RETURNING id`, jobID, requestedBy, s.now().UTC(), req.IdempotencyKey, req.ProfileID, scopeJSON, strings.TrimSpace(req.Reason)).Scan(&id)
	if err != nil {
		return Job{}, fmt.Errorf("insert management job: %w", err)
	}
	_ = s.appendEvent(ctx, id, "created", "audit delete job accepted", 0)
	_ = s.Wake(context.Background())
	return s.GetJob(ctx, id, req.ProfileID)
}

func (s *Store) CreateLogRetentionJob(ctx context.Context, req CreateLogRetentionJobRequest) (Job, error) {
	if s == nil || s.pool == nil {
		return Job{}, fmt.Errorf("management job store is required")
	}
	req.Scope.Table = strings.TrimSpace(req.Scope.Table)
	if req.Scope.Table == "" {
		return Job{}, fmt.Errorf("retention_table_required")
	}
	if !isManagedRetentionTable(req.Scope.Table) {
		return Job{}, fmt.Errorf("retention_table_unknown")
	}
	if !req.Scope.DeleteAll && req.Scope.Cutoff == nil {
		return Job{}, fmt.Errorf("retention_scope_required")
	}
	requestedBy := strings.TrimSpace(req.RequestedBy)
	if requestedBy == "" {
		requestedBy = "global"
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "log retention cleanup"
	}
	jobID := "job_" + randomHex(12)
	scopeJSON, err := json.Marshal(req.Scope)
	if err != nil {
		return Job{}, err
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	var id string
	if idempotencyKey == "" {
		err = s.pool.QueryRow(ctx, `INSERT INTO management_jobs (id, type, state, requested_by, requested_at, priority, profile_id, scope_json, reason, created_at, updated_at) VALUES ($1, $2, 'queued', $3, $4, 'maintenance', 0, $5, $6, $4, $4) RETURNING id`, jobID, TypeLogRetention, requestedBy, s.now().UTC(), scopeJSON, reason).Scan(&id)
	} else {
		err = s.pool.QueryRow(ctx, `INSERT INTO management_jobs (id, type, state, requested_by, requested_at, priority, idempotency_key, profile_id, scope_json, reason, created_at, updated_at) VALUES ($1, $2, 'queued', $3, $4, 'maintenance', $5, 0, $6, $7, $4, $4) ON CONFLICT (type, requested_by, idempotency_key) WHERE idempotency_key IS NOT NULL DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key RETURNING id`, jobID, TypeLogRetention, requestedBy, s.now().UTC(), idempotencyKey, scopeJSON, reason).Scan(&id)
	}
	if err != nil {
		return Job{}, fmt.Errorf("insert log retention job: %w", err)
	}
	_ = s.appendEvent(ctx, id, "created", "log retention job accepted", 0)
	_ = s.Wake(context.Background())
	return s.GetGlobalJob(ctx, id)
}

func (s *Store) GetJob(ctx context.Context, id string, profileID int) (Job, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, type, state, requested_by, requested_at, profile_id, started_at, finished_at, scope_json, reason, COALESCE(rows_matched_estimate, 0), rows_deleted, batches_completed, COALESCE(progress_json->>'last_cursor', ''), attempt_count, last_heartbeat_at, cancel_requested, error_code, error_message FROM management_jobs WHERE id = $1 AND profile_id = $2`, id, profileID)
	return scanJob(row)
}

func (s *Store) GetGlobalJob(ctx context.Context, id string) (Job, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, type, state, requested_by, requested_at, profile_id, started_at, finished_at, scope_json, reason, COALESCE(rows_matched_estimate, 0), rows_deleted, batches_completed, COALESCE(progress_json->>'last_cursor', ''), attempt_count, last_heartbeat_at, cancel_requested, error_code, error_message FROM management_jobs WHERE id = $1 AND profile_id = 0`, id)
	return scanJob(row)
}

func (s *Store) ListJobs(ctx context.Context, profileID int, limit int) (JobListResponse, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT id, type, state, requested_by, requested_at, profile_id, started_at, finished_at, scope_json, reason, COALESCE(rows_matched_estimate, 0), rows_deleted, batches_completed, COALESCE(progress_json->>'last_cursor', ''), attempt_count, last_heartbeat_at, cancel_requested, error_code, error_message FROM management_jobs WHERE profile_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, profileID, limit+1)
	if err != nil {
		return JobListResponse{}, err
	}
	defer rows.Close()
	items := []Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return JobListResponse{}, err
		}
		items = append(items, job)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return JobListResponse{Items: items, HasMore: hasMore}, rows.Err()
}

func (s *Store) CancelJob(ctx context.Context, id string, profileID int) (Job, error) {
	_, err := s.pool.Exec(ctx, `UPDATE management_jobs SET cancel_requested = TRUE, state = CASE WHEN state = 'queued' THEN 'cancelled' ELSE 'cancel_requested' END, finished_at = CASE WHEN state = 'queued' THEN now() ELSE finished_at END, updated_at = now() WHERE id = $1 AND profile_id = $2 AND state IN ('queued', 'running')`, id, profileID)
	if err != nil {
		return Job{}, err
	}
	_ = s.appendEvent(ctx, id, "cancel_requested", "operator requested cancellation", 0)
	return s.GetJob(ctx, id, profileID)
}

func (s *Store) handleScheduledJobs(ctx context.Context, _ background.Job) background.JobResult {
	if err := s.ScheduleGlobalLogRetention(ctx); err != nil {
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	if err := s.ProcessDue(ctx); err != nil {
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	return background.JobResult{Status: background.JobSucceeded}
}

func (s *Store) ScheduleGlobalLogRetention(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return nil
	}
	settings, err := s.loadGlobalLogRetentionSettings(ctx)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	for _, policy := range settings.policies() {
		if policy.RetentionDays == nil {
			continue
		}
		cutoff := now.Add(-time.Duration(*policy.RetentionDays) * 24 * time.Hour)
		_, err := s.CreateLogRetentionJob(ctx, CreateLogRetentionJobRequest{
			RequestedBy:    scheduledLogRetentionRequestedBy,
			IdempotencyKey: scheduledLogRetentionIdempotencyKey(policy.Table, now),
			Reason:         scheduledLogRetentionReason,
			Scope:          LogRetentionScope{Table: policy.Table, Cutoff: &cutoff},
		})
		if err != nil {
			return fmt.Errorf("schedule global log retention for %s: %w", policy.Table, err)
		}
	}
	return nil
}

type globalLogRetentionSettings struct {
	RequestLogsRetentionDays          *int
	AuditLogsRetentionDays            *int
	StatisticsRetentionDays           *int
	LoadbalanceEventsRetentionDays    *int
	SidecarActionHistoryRetentionDays *int
}

type globalLogRetentionPolicy struct {
	Table         string
	RetentionDays *int
}

func (settings globalLogRetentionSettings) policies() []globalLogRetentionPolicy {
	return []globalLogRetentionPolicy{
		{Table: "request_logs", RetentionDays: settings.RequestLogsRetentionDays},
		{Table: "audit_logs", RetentionDays: settings.AuditLogsRetentionDays},
		{Table: "usage_request_events", RetentionDays: settings.StatisticsRetentionDays},
		{Table: "loadbalance_events", RetentionDays: settings.LoadbalanceEventsRetentionDays},
		{Table: "sidecar_watchdog_actions", RetentionDays: settings.SidecarActionHistoryRetentionDays},
	}
}

func (s *Store) loadGlobalLogRetentionSettings(ctx context.Context) (globalLogRetentionSettings, error) {
	var settings globalLogRetentionSettings
	var requestLogsRetentionDays *int32
	var auditLogsRetentionDays *int32
	var statisticsRetentionDays *int32
	var loadbalanceEventsRetentionDays *int32
	var sidecarActionHistoryRetentionDays *int32
	err := s.pool.QueryRow(ctx, `SELECT request_logs_retention_days, audit_logs_retention_days, statistics_retention_days, loadbalance_events_retention_days, sidecar_action_history_retention_days FROM log_retention_settings WHERE singleton_key = 'global'`).Scan(&requestLogsRetentionDays, &auditLogsRetentionDays, &statisticsRetentionDays, &loadbalanceEventsRetentionDays, &sidecarActionHistoryRetentionDays)
	if err != nil {
		return globalLogRetentionSettings{}, fmt.Errorf("load global log retention settings: %w", err)
	}
	settings.RequestLogsRetentionDays = int32PtrToIntPtr(requestLogsRetentionDays)
	settings.AuditLogsRetentionDays = int32PtrToIntPtr(auditLogsRetentionDays)
	settings.StatisticsRetentionDays = int32PtrToIntPtr(statisticsRetentionDays)
	settings.LoadbalanceEventsRetentionDays = int32PtrToIntPtr(loadbalanceEventsRetentionDays)
	settings.SidecarActionHistoryRetentionDays = int32PtrToIntPtr(sidecarActionHistoryRetentionDays)
	return settings, nil
}

func int32PtrToIntPtr(value *int32) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}

func scheduledLogRetentionIdempotencyKey(tableName string, now time.Time) string {
	return fmt.Sprintf("%s:%s", tableName, now.UTC().Format("2006-01-02"))
}

func (s *Store) ProcessDue(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return nil
	}
	job, ok, err := s.claimOne(ctx)
	if err != nil || !ok {
		return err
	}
	switch job.Type {
	case TypeAuditDelete:
		return s.processAuditDelete(ctx, job)
	case TypeLogRetention:
		return s.processLogRetention(ctx, job)
	default:
		return fmt.Errorf("unknown management job type %s", job.Type)
	}
}

func (s *Store) claimOne(ctx context.Context) (Job, bool, error) {
	result, err := pgxutil.InTxValue(ctx, s.pool, "management_jobs_claim", func(tx pgx.Tx) (struct {
		Job Job
		OK  bool
	}, error) {
		row := tx.QueryRow(ctx, `WITH claimable AS (SELECT id FROM management_jobs WHERE type IN ('audit_delete', 'log_retention') AND cancel_requested = FALSE AND next_attempt_at <= now() AND (state = 'queued' OR (state = 'running' AND locked_until < now())) ORDER BY created_at ASC, id ASC LIMIT 1 FOR UPDATE SKIP LOCKED) UPDATE management_jobs j SET state = 'running', started_at = COALESCE(started_at, now()), locked_by = $1, locked_until = now() + $2::interval, last_heartbeat_at = now(), updated_at = now() FROM claimable WHERE j.id = claimable.id RETURNING j.id, j.type, j.state, j.requested_by, j.requested_at, j.profile_id, j.started_at, j.finished_at, j.scope_json, j.reason, COALESCE(j.rows_matched_estimate, 0), j.rows_deleted, j.batches_completed, COALESCE(j.progress_json->>'last_cursor', ''), j.attempt_count, j.last_heartbeat_at, j.cancel_requested, j.error_code, j.error_message`, s.workerID, intervalLiteral(defaultLeaseDuration))
		job, err := scanJob(row)
		if err == pgx.ErrNoRows {
			return struct {
				Job Job
				OK  bool
			}{}, nil
		}
		return struct {
			Job Job
			OK  bool
		}{Job: job, OK: true}, err
	})
	return result.Job, result.OK, err
}

func (s *Store) processAuditDelete(ctx context.Context, job Job) error {
	job.Scope.Table = "audit_logs"
	job.Scope.Cutoff = job.Scope.Before
	return s.processLogRetention(ctx, job)
}

func (s *Store) processLogRetention(ctx context.Context, job Job) error {
	if s.logRetention == nil {
		return s.failJob(ctx, job, "retention_store_missing", "log retention store is required")
	}
	summary, err := s.logRetention.RunRetention(ctx, job.Scope.Table, job.Scope.Cutoff, job.Scope.DeleteAll)
	if err != nil {
		_, _ = s.pool.Exec(ctx, `UPDATE management_jobs SET attempt_count = LEAST(attempt_count + 1, max_attempts), state = CASE WHEN attempt_count + 1 >= max_attempts THEN 'failed' ELSE 'queued' END, error_code = 'retention_error', error_message = $2, next_attempt_at = CASE WHEN attempt_count + 1 >= max_attempts THEN next_attempt_at ELSE now() + interval '5 seconds' END, finished_at = CASE WHEN attempt_count + 1 >= max_attempts THEN now() ELSE finished_at END, locked_by = NULL, locked_until = NULL, updated_at = now() WHERE id = $1`, job.ID, err.Error())
		return err
	}
	progressJSON, err := json.Marshal(map[string]any{"dropped_partitions": summary.DroppedPartitions, "boundary_partition": summary.BoundaryPartition})
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE management_jobs SET state = CASE WHEN cancel_requested THEN 'cancelled' ELSE 'succeeded' END, finished_at = now(), locked_by = NULL, locked_until = NULL, rows_deleted = rows_deleted + $2, batches_completed = batches_completed + 1, progress_json = $3::jsonb, last_heartbeat_at = now(), updated_at = now() WHERE id = $1`, job.ID, summary.BoundaryRowsDeleted, progressJSON)
	_ = s.appendEvent(ctx, job.ID, "finished", "log retention job finished", summary.BoundaryRowsDeleted)
	return err
}

func (s *Store) failJob(ctx context.Context, job Job, code string, message string) error {
	_, _ = s.pool.Exec(ctx, `UPDATE management_jobs SET attempt_count = LEAST(attempt_count + 1, max_attempts), state = 'failed', error_code = $2, error_message = $3, finished_at = now(), locked_by = NULL, locked_until = NULL, updated_at = now() WHERE id = $1`, job.ID, code, message)
	return fmt.Errorf("%s", message)
}

func (s *Store) appendEvent(ctx context.Context, jobID string, eventType string, message string, rowsDeleted int64) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO management_job_events (job_id, event_type, message, rows_deleted, created_at) VALUES ($1, $2, $3, $4, now())`, jobID, eventType, message, rowsDeleted)
	return err
}

func scanJob(scanner interface{ Scan(...any) error }) (Job, error) {
	var scopeRaw []byte
	var startedAt *time.Time
	var finishedAt *time.Time
	var lastHeartbeatAt *time.Time
	job := Job{}
	if err := scanner.Scan(&job.ID, &job.Type, &job.State, &job.RequestedBy, &job.RequestedAt, &job.ProfileID, &startedAt, &finishedAt, &scopeRaw, &job.Reason, &job.Progress.RowsMatchedEstimate, &job.Progress.RowsDeleted, &job.Progress.BatchesCompleted, &job.Progress.LastCursor, &job.AttemptCount, &lastHeartbeatAt, &job.CancelRequested, &job.ErrorCode, &job.ErrorMessage); err != nil {
		return Job{}, err
	}
	_ = json.Unmarshal(scopeRaw, &job.Scope)
	job.StartedAt = startedAt
	job.FinishedAt = finishedAt
	job.LastHeartbeatAt = lastHeartbeatAt
	return job, nil
}

func isManagedRetentionTable(tableName string) bool {
	for _, managedTable := range logretention.ManagedTables() {
		if tableName == managedTable {
			return true
		}
	}
	return false
}

func randomHex(bytes int) string {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}

func intervalLiteral(duration time.Duration) string {
	seconds := int64(duration.Seconds())
	if seconds <= 0 {
		seconds = 1
	}
	return fmt.Sprintf("%d seconds", seconds)
}

func defaultWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return "prism-management-jobs"
	}
	return "prism-management-jobs@" + hostname
}

func LogTransition(jobID string, state string) {
	slog.Info("management.job.transition", "job_id", jobID, "state", state)
}
