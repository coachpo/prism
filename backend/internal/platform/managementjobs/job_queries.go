package managementjobs

import (
	"context"
	"encoding/json"
	"time"
)

// Job is the v1 job projection served by the profile-scoped job endpoints.
// Global log-retention work is served through the retention DTO contract instead
// (see retention_dto.go).
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

type JobProgress struct {
	RowsMatchedEstimate int64  `json:"rows_matched_estimate"`
	RowsDeleted         int64  `json:"rows_deleted"`
	BatchesCompleted    int64  `json:"batches_completed"`
	LastCursor          string `json:"last_cursor"`
}

type JobListResponse struct {
	Items      []Job   `json:"items"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

// jobColumns and jobColumnsQualified are the same projection in scan order.
// The qualified form is required where a CTE joins the target table and a bare
// column name would be ambiguous. Keep both in sync with scanJob.
const (
	jobColumns = `id, type, state, requested_by, requested_at, profile_id, started_at, finished_at, ` +
		`scope_json, reason, COALESCE(rows_matched_estimate, 0), rows_deleted, batches_completed, ` +
		`COALESCE(progress_json->>'last_cursor', ''), attempt_count, last_heartbeat_at, ` +
		`cancel_requested, error_code, error_message`

	jobColumnsQualified = `j.id, j.type, j.state, j.requested_by, j.requested_at, j.profile_id, j.started_at, j.finished_at, ` +
		`j.scope_json, j.reason, COALESCE(j.rows_matched_estimate, 0), j.rows_deleted, j.batches_completed, ` +
		`COALESCE(j.progress_json->>'last_cursor', ''), j.attempt_count, j.last_heartbeat_at, ` +
		`j.cancel_requested, j.error_code, j.error_message`
)

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

func (s *Store) GetJob(ctx context.Context, id string, profileID int) (Job, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM management_jobs WHERE id = $1 AND profile_id = $2`, id, profileID)
	return scanJob(row)
}

func (s *Store) GetGlobalJob(ctx context.Context, id string) (Job, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM management_jobs WHERE id = $1 AND profile_id = 0`, id)
	return scanJob(row)
}

func (s *Store) getGlobalJobByType(ctx context.Context, id string, jobType string) (Job, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM management_jobs WHERE id = $1 AND profile_id = 0 AND type = $2`, id, jobType)
	return scanJob(row)
}

func (s *Store) ListJobs(ctx context.Context, profileID int, limit int) (JobListResponse, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT `+jobColumns+` FROM management_jobs WHERE profile_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, profileID, limit+1)
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
