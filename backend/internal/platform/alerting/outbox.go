package alerting

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/background"
)

const (
	// ponytail: single URL, plain POST, no channel framework — add templating only if a second sink appears.
	WorkerName = background.WorkerName("alert_webhook_worker")

	defaultBatchSize       = 25
	defaultPollInterval    = 10 * time.Second
	defaultClaimTimeout    = 2 * time.Second
	defaultPostTimeout     = 10 * time.Second
	defaultLockDuration    = 2 * time.Minute
	defaultRetryCap        = 8
	defaultBackoffBase     = 5 * time.Second
	defaultBackoffCap      = 15 * time.Minute
	defaultShutdownTimeout = 30 * time.Second
)

type WebhookURLProvider interface {
	AlertingWebhookURL() string
}

type Options struct {
	Pool               *pgxpool.Pool
	Scheduler          *background.Scheduler
	WebhookURLProvider WebhookURLProvider
	Now                func() time.Time
	HTTPClient         *http.Client
	LockedBy           string
}

type Store struct {
	pool               *pgxpool.Pool
	scheduler          *background.Scheduler
	webhookURLProvider WebhookURLProvider
	now                func() time.Time
	httpClient         *http.Client
	lockedBy           string
	batchSize          int
	maxAttempts        int
}

type IncidentPayload struct {
	EventType     string     `json:"event_type"`
	ConnectionID  int        `json:"connection_id"`
	EndpointID    int        `json:"endpoint_id"`
	ModelID       string     `json:"model_id"`
	BannedUntilAt *time.Time `json:"banned_until_at"`
	OccurredAt    time.Time  `json:"occurred_at"`
}

type outboxRow struct {
	ID           string
	EventType    string
	Payload      json.RawMessage
	AttemptCount int
	MaxAttempts  int
}

func NewStore(options Options) *Store {
	if options.Pool == nil {
		return nil
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultPostTimeout}
	}
	lockedBy := strings.TrimSpace(options.LockedBy)
	if lockedBy == "" {
		lockedBy = defaultLockedBy()
	}
	return &Store{
		pool:               options.Pool,
		scheduler:          options.Scheduler,
		webhookURLProvider: options.WebhookURLProvider,
		now:                now,
		httpClient:         client,
		lockedBy:           lockedBy,
		batchSize:          defaultBatchSize,
		maxAttempts:        defaultRetryCap,
	}
}

func (s *Store) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if s == nil || scheduler == nil {
		return nil
	}
	s.scheduler = scheduler
	return scheduler.Register(background.WorkerSpec{
		Name:             WorkerName,
		Priority:         background.PriorityNormalBackground,
		MaxPriority:      background.PriorityNormalBackground,
		QueueLimit:       defaultBatchSize,
		ConcurrencyLimit: 1,
		DrainPolicy:      background.DrainBestEffort,
		CoalescePolicy:   background.CoalesceDropNew,
		RetryPolicy:      &background.RetryPolicy{MaxAttempts: 3, Delay: defaultPollInterval},
		PeriodicTrigger:  &background.PeriodicTrigger{Interval: defaultPollInterval},
		Timeout:          defaultShutdownTimeout,
	}, s.handleScheduledPost)
}

func (s *Store) Wake(ctx context.Context) error {
	if s == nil || s.scheduler == nil {
		return nil
	}
	result := s.scheduler.Submit(ctx, background.JobRequest{Worker: WorkerName, CoalesceKey: string(WorkerName)})
	switch result.Status {
	case background.SubmitAccepted, background.SubmitCoalesced:
		return nil
	default:
		return fmt.Errorf("wake alert webhook worker: %s", result.Status)
	}
}

func (s *Store) EnqueueTx(ctx context.Context, tx pgx.Tx, payload IncidentPayload) error {
	if s == nil || tx == nil {
		return nil
	}
	if strings.TrimSpace(s.alertingWebhookURL()) == "" {
		return nil
	}
	normalized, err := normalizeIncidentPayload(payload)
	if err != nil {
		return err
	}
	rawPayload, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshal alert webhook payload: %w", err)
	}
	id, err := newUUIDString()
	if err != nil {
		return fmt.Errorf("generate alert webhook outbox id: %w", err)
	}
	nowAt := s.now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO alert_webhook_outbox (
			id, event_type, payload_json, idempotency_key, status,
			attempt_count, max_attempts, next_attempt_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'queued', 0, $5, $6, $6, $6)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		id,
		normalized.EventType,
		rawPayload,
		incidentIdempotencyKey(normalized),
		s.maxAttempts,
		nowAt,
	)
	if err != nil {
		return fmt.Errorf("insert alert webhook outbox row: %w", err)
	}
	return nil
}

func (s *Store) handleScheduledPost(ctx context.Context, _ background.Job) background.JobResult {
	if s == nil || s.pool == nil {
		return background.JobResult{Status: background.JobSucceeded}
	}
	webhookURL := strings.TrimSpace(s.alertingWebhookURL())
	if webhookURL == "" {
		return background.JobResult{Status: background.JobSucceeded}
	}
	if err := s.recoverStaleLocks(ctx); err != nil {
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	rows, err := s.claimBatch(ctx)
	if err != nil {
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	for _, row := range rows {
		s.processRow(ctx, webhookURL, row)
	}
	return background.JobResult{Status: background.JobSucceeded}
}

func (s *Store) recoverStaleLocks(ctx context.Context) error {
	claimCtx, cancel := context.WithTimeout(ctx, defaultClaimTimeout)
	defer cancel()
	_, err := s.pool.Exec(claimCtx, `
		UPDATE alert_webhook_outbox
		SET status = 'queued', locked_by = NULL, locked_until = NULL, next_attempt_at = now(), updated_at = now()
		WHERE status = 'sending' AND locked_until < now()`)
	if err != nil {
		return fmt.Errorf("recover stale alert webhook outbox locks: %w", err)
	}
	return nil
}

func (s *Store) claimBatch(ctx context.Context) ([]outboxRow, error) {
	claimCtx, cancel := context.WithTimeout(ctx, defaultClaimTimeout)
	defer cancel()
	return pgxutil.InTxValue(claimCtx, s.pool, "alert_webhook_claim", func(tx pgx.Tx) ([]outboxRow, error) {
		rows, err := tx.Query(claimCtx, `
			WITH claimable AS (
				SELECT id FROM alert_webhook_outbox
				WHERE status = 'queued' AND next_attempt_at <= now()
				ORDER BY next_attempt_at ASC, created_at ASC, id ASC
				LIMIT $1
				FOR UPDATE SKIP LOCKED
			)
			UPDATE alert_webhook_outbox outbox
			SET status = 'sending',
				attempt_count = outbox.attempt_count + 1,
				locked_by = $2,
				locked_until = $3,
				last_error = NULL,
				updated_at = now()
			FROM claimable
			WHERE outbox.id = claimable.id
			RETURNING outbox.id::text, outbox.event_type, outbox.payload_json, outbox.attempt_count, outbox.max_attempts`,
			s.batchSize,
			s.lockedBy,
			s.now().UTC().Add(defaultLockDuration),
		)
		if err != nil {
			return nil, fmt.Errorf("claim alert webhook outbox rows: %w", err)
		}
		defer rows.Close()
		claimed := []outboxRow{}
		for rows.Next() {
			var row outboxRow
			if err := rows.Scan(&row.ID, &row.EventType, &row.Payload, &row.AttemptCount, &row.MaxAttempts); err != nil {
				return nil, fmt.Errorf("scan alert webhook outbox row: %w", err)
			}
			claimed = append(claimed, row)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate alert webhook outbox rows: %w", err)
		}
		return claimed, nil
	})
}

func (s *Store) processRow(ctx context.Context, webhookURL string, row outboxRow) {
	postCtx, cancel := context.WithTimeout(ctx, defaultPostTimeout)
	err := s.postWebhook(postCtx, webhookURL, row.Payload)
	cancel()
	if err == nil {
		s.finalizeSuccess(context.Background(), row.ID)
		return
	}
	s.finalizeFailure(context.Background(), row, err)
}

func (s *Store) postWebhook(ctx context.Context, webhookURL string, payload json.RawMessage) error {
	var compact bytes.Buffer
	if err := json.Compact(&compact, payload); err != nil {
		return fmt.Errorf("compact alert webhook payload: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(compact.Bytes()))
	if err != nil {
		return fmt.Errorf("build alert webhook request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("post alert webhook: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("post alert webhook returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (s *Store) finalizeSuccess(ctx context.Context, id string) {
	_, err := s.pool.Exec(ctx, `
		UPDATE alert_webhook_outbox
		SET status = 'sent',
			attempt_count = GREATEST(attempt_count - 1, 0),
			sent_at = now(),
			locked_by = NULL,
			locked_until = NULL,
			last_error = NULL,
			updated_at = now()
		WHERE id = $1`, id)
	if err != nil {
		slog.Error("failed to finalize alert webhook success", "error", err, "outbox_id", id)
	}
}

func (s *Store) finalizeFailure(ctx context.Context, row outboxRow, err error) {
	status := "queued"
	nextAttemptAt := s.now().UTC().Add(backoffForAttempt(row.AttemptCount))
	deadLetteredAt := any(nil)
	if row.AttemptCount >= row.MaxAttempts {
		status = "dead"
		nextAttemptAt = s.now().UTC()
		deadLetteredAt = nextAttemptAt
	}
	_, execErr := s.pool.Exec(ctx, `
		UPDATE alert_webhook_outbox
		SET status = $2,
			next_attempt_at = $3,
			locked_by = NULL,
			locked_until = NULL,
			dead_lettered_at = $4,
			last_error = $5,
			updated_at = now()
		WHERE id = $1`,
		row.ID,
		status,
		nextAttemptAt,
		deadLetteredAt,
		truncateLastError(err),
	)
	if execErr != nil {
		slog.Error("failed to finalize alert webhook failure", "error", execErr, "outbox_id", row.ID, "handler_error", err)
		return
	}
	slog.Warn("alert webhook post failed", "error", err, "outbox_id", row.ID, "event_type", row.EventType, "status", status, "attempt", row.AttemptCount)
}

func (s *Store) alertingWebhookURL() string {
	if s == nil || s.webhookURLProvider == nil {
		return ""
	}
	return strings.TrimSpace(s.webhookURLProvider.AlertingWebhookURL())
}

func normalizeIncidentPayload(payload IncidentPayload) (IncidentPayload, error) {
	eventType := strings.TrimSpace(payload.EventType)
	if eventType != "banned" && eventType != "unbanned" && eventType != "recovered" {
		return IncidentPayload{}, fmt.Errorf("unsupported alert incident event_type %q", eventType)
	}
	modelID := strings.TrimSpace(payload.ModelID)
	if payload.ConnectionID <= 0 || payload.EndpointID <= 0 || modelID == "" || payload.OccurredAt.IsZero() {
		return IncidentPayload{}, fmt.Errorf("alert incident payload missing required identity")
	}
	payload.EventType = eventType
	payload.ModelID = modelID
	payload.OccurredAt = payload.OccurredAt.UTC()
	if payload.BannedUntilAt != nil {
		bannedUntilAt := payload.BannedUntilAt.UTC()
		payload.BannedUntilAt = &bannedUntilAt
	}
	return payload, nil
}

func incidentIdempotencyKey(payload IncidentPayload) string {
	parts := []string{
		payload.EventType,
		fmt.Sprintf("%d", payload.ConnectionID),
		fmt.Sprintf("%d", payload.EndpointID),
		payload.ModelID,
		payload.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
	return strings.Join(parts, ":")
}

func backoffForAttempt(attempt int) time.Duration {
	if attempt <= 1 {
		return defaultBackoffBase
	}
	multiplier := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(defaultBackoffBase) * multiplier)
	if delay > defaultBackoffCap || delay <= 0 {
		return defaultBackoffCap
	}
	return delay
}

func newUUIDString() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], raw[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], raw[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], raw[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], raw[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], raw[10:16])
	return string(dst), nil
}

func truncateLastError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) <= 2048 {
		return message
	}
	return message[:2048]
}

func defaultLockedBy() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s:%d", hostname, os.Getpid())
}
