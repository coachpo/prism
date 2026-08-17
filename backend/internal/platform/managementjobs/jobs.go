// Package managementjobs owns low-priority durable management work.
//
// Two job types share the management_jobs table:
//
//   - audit_delete is profile-scoped and runs the original single-shot
//     executor (audit_delete.go).
//   - log_retention is global (profile_id = 0) and runs the current retention
//     contract: UTC day-aligned logical cutoffs, durable per-dataset policy
//     resources, protection-gated physical reclaim (24h token TTL + 24h grace
//     for the three Observe domains; audit uses its own fence projection),
//     manual purge with execution-fence purge_to_time, and final epoch/coverage
//     publication.
//
// Rows under the current contract carry contract_version = 2, a durable
// operation_id/request_hash, an origin and worker_generation >= the fenced
// minimum. Rows left at contract_version = 1 are legacy: they drain only
// through the frozen executor in retention_legacy.go, and only after the
// startup cutover authorizes legacy claim/delete.
package managementjobs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
)

const (
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

// LogRetentionScope is the durable scope_json payload shared by both job types.
type LogRetentionScope struct {
	Before    *time.Time `json:"before,omitempty"`
	Table     string     `json:"table,omitempty"`
	Cutoff    *time.Time `json:"cutoff,omitempty"`
	DeleteAll bool       `json:"delete_all,omitempty"`
}

type AuditDeleteScope = LogRetentionScope

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
	// The domain separator is part of the derived key: changing it invalidates
	// every cursor already handed out.
	sum := sha256.Sum256([]byte("prism.management.jobs.cursor.v2|" + seed))
	return append([]byte(nil), sum[:]...)
}

func (s *Store) appendEvent(ctx context.Context, jobID string, eventType string, message string, rowsDeleted int64) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO management_job_events (job_id, event_type, message, rows_deleted, created_at) VALUES ($1, $2, $3, $4, now())`, jobID, eventType, message, rowsDeleted)
	return err
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

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return raw
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
