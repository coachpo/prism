package managementjobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

const (
	defaultJobEvidencePageLimit = 20
	maxJobEvidencePageLimit     = 100
)

type globalJobListResult struct {
	Items       []retentionJobRow
	HasMore     bool
	NextCursor  *string
	GeneratedAt string
}

// ListGlobalRetentionJobs lists global log-retention jobs with keyset
// pagination and origin/state filters (SPEC §7.1).
func (s *Store) ListGlobalRetentionJobs(ctx context.Context, origin string, state []string, limit int, cursor *string) (globalJobListResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	origin = strings.TrimSpace(origin)
	state = canonicalJobStates(state)
	query := `SELECT ` + retentionSelectColumns() + `
		FROM management_jobs
		WHERE type = 'log_retention' AND profile_id = 0`
	args := []any{}
	if origin != "" {
		args = append(args, origin)
		query += fmt.Sprintf(` AND origin = $%d`, len(args))
	}
	if len(state) > 0 {
		query += ` AND state = ANY($` + fmt.Sprintf("%d", len(args)+1) + `::text[])`
		args = append(args, state)
	}
	if cursor != nil && strings.TrimSpace(*cursor) != "" {
		value, ok := s.decodeJobsCursor(*cursor)
		if !ok {
			return globalJobListResult{}, errInvalidJobsCursor
		}
		if value.Origin != origin || !sameJobStates(value.States, state) || value.Limit != limit || value.Sort != jobsCursorSort {
			return globalJobListResult{}, errInvalidJobsCursor
		}
		upperAt, err := time.Parse(time.RFC3339Nano, value.UpperAt)
		if err != nil {
			return globalJobListResult{}, errInvalidJobsCursor
		}
		upperArgs := []any{upperAt, value.UpperID}
		query += fmt.Sprintf(` AND (requested_at, id) <= ($%d, $%d)`, len(args)+1, len(args)+2)
		args = append(args, upperArgs...)
		positionAt, err := time.Parse(time.RFC3339Nano, value.PositionAt)
		if err != nil {
			return globalJobListResult{}, errInvalidJobsCursor
		}
		args = append(args, positionAt, value.PositionID)
		query += fmt.Sprintf(` AND (requested_at, id) < ($%d, $%d)`, len(args)-1, len(args))
	}
	query += ` ORDER BY requested_at DESC, id DESC LIMIT $` + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return globalJobListResult{}, fmt.Errorf("list global retention jobs: %w", err)
	}
	defer rows.Close()
	items := []retentionJobRow{}
	for rows.Next() {
		item, err := scanRetentionRow(rows)
		if err != nil {
			return globalJobListResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return globalJobListResult{}, err
	}
	result := globalJobListResult{GeneratedAt: s.now().UTC().Format(time.RFC3339)}
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		upper := items[0]
		payload := jobsCursorPayload{
			Version:    2,
			Origin:     origin,
			States:     append([]string(nil), state...),
			Limit:      limit,
			Sort:       jobsCursorSort,
			UpperAt:    upper.RequestedAt.UTC().Format(time.RFC3339Nano),
			UpperID:    upper.ID,
			PositionAt: last.RequestedAt.UTC().Format(time.RFC3339Nano),
			PositionID: last.ID,
		}
		encoded := s.encodeJobsCursor(payload)
		result.NextCursor = &encoded
		result.HasMore = true
	}
	result.Items = items
	return result, nil
}

func (s *Store) GetGlobalRetentionJob(ctx context.Context, id string) (retentionJobRow, error) {
	return s.loadGlobalRetentionRow(ctx, s.pool, id)
}

func (s *Store) CancelRetentionJob(ctx context.Context, id string, operationID string) (retentionJobRow, bool, error) {
	var row retentionJobRow
	replayed := false
	err := pgxutil.InTx(ctx, s.pool, "retention_cancel", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(ctx, tx); err != nil {
			return fmt.Errorf("retention cancel owner admission: %w", err)
		}
		requestHash := canonicalRequestHash("retention_job_cancel", id, operationID)
		var recordedHash string
		var recordedResult []byte
		operationErr := tx.QueryRow(ctx, `SELECT request_hash, result_json FROM settings_mutation_operations
			WHERE resource_kind = 'retention_job_cancel' AND operation_id = $1`, operationID).
			Scan(&recordedHash, &recordedResult)
		if operationErr == nil {
			if recordedHash != requestHash || len(recordedResult) == 0 {
				return errRetentionCancelOperationConflict
			}
			replayed = true
			var loadErr error
			row, loadErr = s.loadGlobalRetentionRow(ctx, tx, id)
			return loadErr
		}
		if operationErr != pgx.ErrNoRows {
			return operationErr
		}
		existing, err := scanRetentionRow(tx.QueryRow(ctx, retentionSelect+` WHERE id = $1 AND type = 'log_retention' AND profile_id = 0 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if existing.ContractVersion != 2 {
			return errLegacyJobNotCancellable
		}
		if existing.State == "succeeded" || existing.State == "failed" || existing.State == "superseded" {
			return errJobTerminal
		}
		// Manual purge jobs are cancellable only while queued and before the
		// purge fence; once running they must complete or recover (SPEC §7.3).
		isManual := existing.Origin != nil && *existing.Origin == "manual"
		if isManual && existing.State == "running" {
			return errPurgeNotCancellable
		}
		if existing.State == "cancel_requested" || existing.State == "cancelled" {
			row = existing
			return recordRetentionCancelOperation(ctx, tx, operationID, requestHash, RetentionJobSummary(existing))
		}
		// Queued manual/automatic cancels commit directly; running automatic
		// enters cancel_requested at the next safe checkpoint.
		newState := "cancelled"
		if existing.State == "running" {
			newState = "cancel_requested"
		}
		if _, err := tx.Exec(ctx, `UPDATE management_jobs SET
			state = $2, terminal_disposition = CASE WHEN $2 = 'cancelled' THEN 'cancelled' ELSE NULL END, cancel_requested = TRUE,
			finished_at = CASE WHEN $2 = 'cancelled' THEN now() ELSE finished_at END,
			updated_at = now() WHERE id = $1 AND type = 'log_retention' AND profile_id = 0`, id, newState); err != nil {
			return err
		}
		row, err = s.loadGlobalRetentionRow(ctx, tx, id)
		if err != nil {
			return err
		}
		return recordRetentionCancelOperation(ctx, tx, operationID, requestHash, RetentionJobSummary(row))
	})
	if err != nil {
		return retentionJobRow{}, false, err
	}
	return row, replayed, nil
}

func recordRetentionCancelOperation(ctx context.Context, tx pgx.Tx, operationID, requestHash string, summary RetentionJobSummaryDTO) error {
	resultJSON, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO settings_mutation_operations (
		resource_kind, operation_id, request_hash, state, result_json, created_at, updated_at
	) VALUES ('retention_job_cancel', $1, $2, 'completed', $3, now(), now())
	ON CONFLICT (resource_kind, operation_id) DO NOTHING`, operationID, requestHash, resultJSON)
	return err
}

// ---- settings-facing DTO queries (audit service consumes these) ----

// ListGlobalRetentionJobsDTO lists global log-retention jobs with keyset
// pagination and origin/state filters (SPEC §7.1).
func (s *Store) ListGlobalRetentionJobsDTO(ctx context.Context, origin string, states []string, limit int, cursor *string) (JobListDTO, error) {
	result, err := s.ListGlobalRetentionJobs(ctx, origin, states, limit, cursor)
	if err != nil {
		return JobListDTO{}, err
	}
	dto := JobListDTO{HasMore: result.HasMore, NextCursor: result.NextCursor, GeneratedAt: result.GeneratedAt}
	dto.Items = make([]RetentionJobSummaryDTO, 0, len(result.Items))
	for _, item := range result.Items {
		dto.Items = append(dto.Items, RetentionJobSummary(item))
	}
	return dto, nil
}

// GetGlobalRetentionJobDetailDTO returns the exact detail with embedded
// bounded checkpoint/partition pages (SPEC §7.2).
func (s *Store) GetGlobalRetentionJobDetailDTO(ctx context.Context, id string) (JobDetailDTO, error) {
	row, err := s.loadGlobalRetentionRow(ctx, s.pool, id)
	if err != nil {
		return JobDetailDTO{}, err
	}
	checkpoints, err := s.checkpointPage(ctx, id, defaultJobEvidencePageLimit, "")
	if err != nil {
		return JobDetailDTO{}, err
	}
	partitions, err := s.partitionPage(ctx, row, defaultJobEvidencePageLimit, "")
	if err != nil {
		return JobDetailDTO{}, err
	}
	detail := JobDetailDTO{
		Job:         RetentionJobSummary(row),
		Checkpoints: checkpoints,
		Partitions:  partitions,
	}
	detail.TerminalResult = terminalResultFor(row)
	return detail, nil
}

// GetGlobalRetentionJobCheckpointsDTO returns the bounded checkpoint page.
func (s *Store) GetGlobalRetentionJobCheckpointsDTO(ctx context.Context, id string, limit int, cursor string) (JobCheckpointPageDTO, error) {
	// The row load is the existence and global-scope check; the page itself is
	// keyed by job id alone.
	if _, err := s.loadGlobalRetentionRow(ctx, s.pool, id); err != nil {
		return JobCheckpointPageDTO{}, err
	}
	return s.checkpointPage(ctx, id, limit, cursor)
}

// GetGlobalRetentionJobPartitionsDTO returns the bounded partition evidence page.
func (s *Store) GetGlobalRetentionJobPartitionsDTO(ctx context.Context, id string, limit int, cursor string) (JobPartitionPageDTO, error) {
	row, err := s.loadGlobalRetentionRow(ctx, s.pool, id)
	if err != nil {
		return JobPartitionPageDTO{}, err
	}
	return s.partitionPage(ctx, row, limit, cursor)
}

// CancelGlobalRetentionJobDTO cancels a global log-retention job with
// operation identity and durable replay.
func (s *Store) CancelGlobalRetentionJobDTO(ctx context.Context, id string, operationID string) (RetentionJobSummaryDTO, bool, error) {
	row, replayed, err := s.CancelRetentionJob(ctx, id, operationID)
	if err != nil {
		return RetentionJobSummaryDTO{}, false, err
	}
	return RetentionJobSummary(row), replayed, nil
}

func (s *Store) checkpointPage(ctx context.Context, jobID string, limit int, cursor string) (JobCheckpointPageDTO, error) {
	if limit <= 0 || limit > maxJobEvidencePageLimit {
		limit = defaultJobEvidencePageLimit
	}
	page := JobCheckpointPageDTO{GeneratedAt: s.now().UTC().Format(time.RFC3339)}
	positionID := int64(0)
	upperID := int64(0)
	if strings.TrimSpace(cursor) != "" {
		decoded, ok := s.decodeEvidenceCursor(cursor, "checkpoints", jobID, limit)
		if !ok {
			return page, errInvalidJobsCursor
		}
		positionID = decoded.PositionID
		upperID = decoded.UpperID
	}
	// The upper bound is resolved once, on the first page only: deriving it
	// from MAX(id) OVER () inside the paged query forces the planner to scan
	// the whole job even though LIMIT bounds the page.
	if upperID == 0 {
		var maxEventID sql.NullInt64
		if err := s.pool.QueryRow(ctx, `SELECT MAX(id) FROM management_job_events WHERE job_id = $1`, jobID).Scan(&maxEventID); err != nil {
			return page, fmt.Errorf("resolve job checkpoint upper bound: %w", err)
		}
		if maxEventID.Valid {
			upperID = maxEventID.Int64
		}
	}
	args := []any{jobID}
	where := "job_id = $1"
	if upperID > 0 {
		args = append(args, upperID)
		where += fmt.Sprintf(" AND id <= $%d", len(args))
	}
	if positionID > 0 {
		args = append(args, positionID)
		where += fmt.Sprintf(" AND id > $%d", len(args))
	}
	args = append(args, limit+1)
	rows, err := s.pool.Query(ctx, `SELECT id, event_type, rows_deleted, created_at
		FROM management_job_events WHERE `+where+` ORDER BY id ASC LIMIT $`+fmt.Sprintf("%d", len(args)), args...)
	if err != nil {
		return page, fmt.Errorf("list job checkpoints: %w", err)
	}
	defer rows.Close()
	type checkpointEvidence struct {
		id          int64
		eventType   string
		rowsDeleted int64
		createdAt   time.Time
	}
	items := make([]checkpointEvidence, 0, limit+1)
	for rows.Next() {
		var eventID int64
		var eventType string
		var rowsDeleted int64
		var createdAt time.Time
		if err := rows.Scan(&eventID, &eventType, &rowsDeleted, &createdAt); err != nil {
			return page, fmt.Errorf("scan job checkpoint: %w", err)
		}
		items = append(items, checkpointEvidence{id: eventID, eventType: eventType, rowsDeleted: rowsDeleted, createdAt: createdAt})
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("iterate job checkpoints: %w", err)
	}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
	}
	for _, item := range items {
		page.Items = append(page.Items, JobCheckpointDTO{
			Sequence:              fmt.Sprintf("%d", item.id),
			RecordedAt:            item.createdAt.UTC().Format(time.RFC3339),
			Stage:                 mapEventStage(item.eventType),
			Kind:                  mapEventKind(item.eventType),
			BoundaryRowsDelta:     fmt.Sprintf("%d", item.rowsDeleted),
			DroppedPartitionDelta: "0",
		})
	}
	if page.HasMore && len(items) > 0 {
		encoded := s.encodeEvidenceCursor(evidenceCursorPayload{Version: 2, Kind: "checkpoints", JobID: jobID, Limit: limit, Sort: evidenceCursorSort, UpperID: upperID, PositionID: items[len(items)-1].id})
		page.NextCursor = &encoded
	}
	return page, nil
}

func (s *Store) partitionPage(ctx context.Context, row retentionJobRow, limit int, cursor string) (JobPartitionPageDTO, error) {
	if limit <= 0 || limit > maxJobEvidencePageLimit {
		limit = defaultJobEvidencePageLimit
	}
	page := JobPartitionPageDTO{GeneratedAt: s.now().UTC().Format(time.RFC3339)}
	var progress struct {
		DroppedPartitions []string `json:"dropped_partitions"`
	}
	_ = json.Unmarshal(row.ProgressJSON, &progress)
	evidenceAt := "unknown"
	if row.LastHeartbeatAt != nil {
		evidenceAt = row.LastHeartbeatAt.UTC().Format(time.RFC3339)
	}
	positionID := int64(0)
	upperID := int64(len(progress.DroppedPartitions))
	if strings.TrimSpace(cursor) != "" {
		decoded, ok := s.decodeEvidenceCursor(cursor, "partitions", row.ID, limit)
		if !ok {
			return page, errInvalidJobsCursor
		}
		if decoded.UpperID > 0 && decoded.UpperID < upperID {
			upperID = decoded.UpperID
		}
		positionID = decoded.PositionID
	}
	end := int64(len(progress.DroppedPartitions))
	if upperID > 0 && end > upperID {
		end = upperID
	}
	if positionID < 0 {
		positionID = 0
	}
	for index := positionID; index < end && int64(len(page.Items)) < int64(limit); index++ {
		name := progress.DroppedPartitions[index]
		page.Items = append(page.Items, JobPartitionEvidenceDTO{
			Sequence:            fmt.Sprintf("%d", index+1),
			PartitionName:       name,
			Action:              "dropped",
			EvidenceAt:          evidenceAt,
			BoundaryRowsDeleted: "0",
			DroppedRowsAccuracy: "unavailable",
		})
	}
	if end > positionID+int64(len(page.Items)) {
		page.HasMore = true
	}
	if page.HasMore && len(page.Items) > 0 {
		last := positionID + int64(len(page.Items))
		encoded := s.encodeEvidenceCursor(evidenceCursorPayload{Version: 2, Kind: "partitions", JobID: row.ID, Limit: limit, Sort: evidenceCursorSort, UpperID: upperID, PositionID: last})
		page.NextCursor = &encoded
	}
	return page, nil
}

func mapEventKind(eventType string) string {
	switch eventType {
	case "created":
		return "claimed"
	case "partitions_dropped":
		return "partition_dropped"
	case "boundary_rows_deleted":
		return "boundary_batch_committed"
	case "purge_started", "purge_to_time_frozen":
		return "purge_state_changed"
	case "purge_published", "superseded", "succeeded", "failed", "cancelled", "finished":
		return "coverage_published"
	default:
		return "coverage_published"
	}
}

func mapEventStage(eventType string) string {
	switch eventType {
	case "partitions_dropped":
		return "dropping_partitions"
	case "boundary_rows_deleted":
		return "deleting_boundary_rows"
	case "purge_started":
		return "purge_running"
	case "purge_published":
		return "publishing_epoch_coverage"
	case "succeeded":
		return "finished"
	default:
		return "queued"
	}
}
