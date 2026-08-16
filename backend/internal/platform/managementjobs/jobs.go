package managementjobs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	cursorKey    []byte
}

type Options struct {
	Pool         *pgxpool.Pool
	Scheduler    *background.Scheduler
	LogRetention *logretention.Store
	WorkerID     string
	Now          func() time.Time
	// CursorSigningKey is the stable bootstrap secret used for global job
	// cursors. It is domain-separated before use and is never returned to a
	// caller. Tests and small isolated stores may omit it; those stores get a
	// process-local key derived from their worker id.
	CursorSigningKey string
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
	return &Store{pool: options.Pool, scheduler: options.Scheduler, logRetention: options.LogRetention, workerID: workerID, now: now, batchSize: defaultBatchSize, cursorKey: deriveCursorSigningKey(options.CursorSigningKey, workerID)}
}

// SetCursorSigningKey supplies the process-wide stable bootstrap secret after
// the database background services have been assembled. Production calls this
// during startup before the HTTP server accepts requests.
func (s *Store) SetCursorSigningKey(secret string) {
	if s == nil || strings.TrimSpace(secret) == "" {
		return
	}
	s.cursorKey = deriveCursorSigningKey(secret, s.workerID)
}

func deriveCursorSigningKey(secret, workerID string) []byte {
	seed := strings.TrimSpace(secret)
	if seed == "" {
		seed = "process:" + strings.TrimSpace(workerID)
	}
	sum := sha256.Sum256([]byte("prism.management.jobs.cursor.v2|" + seed))
	return append([]byte(nil), sum[:]...)
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

func (s *Store) GetJob(ctx context.Context, id string, profileID int) (Job, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, type, state, requested_by, requested_at, profile_id, started_at, finished_at, scope_json, reason, COALESCE(rows_matched_estimate, 0), rows_deleted, batches_completed, COALESCE(progress_json->>'last_cursor', ''), attempt_count, last_heartbeat_at, cancel_requested, error_code, error_message FROM management_jobs WHERE id = $1 AND profile_id = $2`, id, profileID)
	return scanJob(row)
}

func (s *Store) GetGlobalJob(ctx context.Context, id string) (Job, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, type, state, requested_by, requested_at, profile_id, started_at, finished_at, scope_json, reason, COALESCE(rows_matched_estimate, 0), rows_deleted, batches_completed, COALESCE(progress_json->>'last_cursor', ''), attempt_count, last_heartbeat_at, cancel_requested, error_code, error_message FROM management_jobs WHERE id = $1 AND profile_id = 0`, id)
	return scanJob(row)
}

func (s *Store) getGlobalJobByType(ctx context.Context, id string, jobType string) (Job, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, type, state, requested_by, requested_at, profile_id, started_at, finished_at, scope_json, reason, COALESCE(rows_matched_estimate, 0), rows_deleted, batches_completed, COALESCE(progress_json->>'last_cursor', ''), attempt_count, last_heartbeat_at, cancel_requested, error_code, error_message FROM management_jobs WHERE id = $1 AND profile_id = 0 AND type = $2`, id, jobType)
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
	job, err := s.GetJob(ctx, id, profileID)
	if err != nil {
		return Job{}, err
	}
	_ = s.appendEvent(ctx, id, "cancel_requested", "operator requested cancellation", 0)
	return job, nil
}

func (s *Store) CancelGlobalLogRetentionJob(ctx context.Context, id string) (Job, error) {
	_, err := s.pool.Exec(ctx, `UPDATE management_jobs SET cancel_requested = TRUE, state = CASE WHEN state = 'queued' THEN 'cancelled' ELSE 'cancel_requested' END, finished_at = CASE WHEN state = 'queued' THEN now() ELSE finished_at END, updated_at = now() WHERE id = $1 AND profile_id = 0 AND type = $2 AND state IN ('queued', 'running')`, id, TypeLogRetention)
	if err != nil {
		return Job{}, err
	}
	job, err := s.getGlobalJobByType(ctx, id, TypeLogRetention)
	if err != nil {
		return Job{}, err
	}
	_ = s.appendEvent(ctx, id, "cancel_requested", "operator requested cancellation", 0)
	return job, nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func (s *Store) handleScheduledJobs(ctx context.Context, _ background.Job) background.JobResult {
	// v2 planning: UTC day-aligned logical cutoffs and durable desired work.
	if err := s.planScheduledRetentionV2(ctx); err != nil {
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	// Frozen v1 drain for previously accepted legacy rows, then v2 execution.
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
	// v2 jobs follow.
	legacy, legacyOK, err := s.claimLegacyRetentionJob(ctx)
	if err != nil {
		return err
	}
	if legacyOK {
		return s.drainLegacyRetentionJob(ctx, legacy)
	}
	job, ok, err := s.claimV2RetentionJob(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	var processErr error
	switch {
	case job.ContractVersion == 2:
		processErr = s.processV2RetentionJob(ctx, job)
	default:
		processErr = s.drainLegacyRetentionJob(ctx, job)
	}
	return processErr
}

func (s *Store) claimOne(ctx context.Context) (Job, bool, error) {
	result, err := pgxutil.InTxValue(ctx, s.pool, "management_jobs_claim", func(tx pgx.Tx) (struct {
		Job Job
		OK  bool
	}, error) {
		row := tx.QueryRow(ctx, `WITH claimable AS (SELECT id FROM management_jobs WHERE type = 'audit_delete' AND cancel_requested = FALSE AND next_attempt_at <= now() AND (state = 'queued' OR (state = 'running' AND locked_until < now())) ORDER BY created_at ASC, id ASC LIMIT 1 FOR UPDATE SKIP LOCKED) UPDATE management_jobs j SET state = 'running', started_at = COALESCE(started_at, now()), locked_by = $1, locked_until = now() + $2::interval, last_heartbeat_at = now(), updated_at = now() FROM claimable WHERE j.id = claimable.id RETURNING j.id, j.type, j.state, j.requested_by, j.requested_at, j.profile_id, j.started_at, j.finished_at, j.scope_json, j.reason, COALESCE(j.rows_matched_estimate, 0), j.rows_deleted, j.batches_completed, COALESCE(j.progress_json->>'last_cursor', ''), j.attempt_count, j.last_heartbeat_at, j.cancel_requested, j.error_code, j.error_message`, s.workerID, intervalLiteral(defaultLeaseDuration))
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
