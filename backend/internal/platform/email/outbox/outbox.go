package outbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/email"
)

const (
	WorkerName = background.WorkerName("email_outbox_worker")

	KindEmailVerificationOTP = "email_verification_otp"
	KindPasswordReset        = "password_reset"

	TemplateEmailVerificationOTP = "email_verification_otp"
	TemplatePasswordReset        = "password_reset"

	defaultBatchSize       = 25
	defaultConcurrency     = 2
	defaultPollInterval    = 5 * time.Second
	defaultClaimTimeout    = 2 * time.Second
	defaultSendTimeout     = 30 * time.Second
	defaultLeaseDuration   = 2 * time.Minute
	defaultMaxAttempts     = 8
	defaultBackoffBase     = 30 * time.Second
	defaultBackoffCap      = time.Hour
	defaultShutdownTimeout = 30 * time.Second
)

type Mailer interface {
	SendEmailVerificationOTP(context.Context, string, string) error
	SendPasswordResetEmail(context.Context, string, string) error
}

type MailerProvider interface {
	Mailer() email.Mailer
}

type Store struct {
	pool                *pgxpool.Pool
	mailer              Mailer
	mailerProvider      MailerProvider
	secretEncryptionKey string
	scheduler           *background.Scheduler
	workerID            string
	now                 func() time.Time
	batchSize           int
	maxAttempts         int
	leaseDuration       time.Duration
}

type Options struct {
	Pool                *pgxpool.Pool
	Mailer              Mailer
	MailerProvider      MailerProvider
	SecretEncryptionKey string
	Scheduler           *background.Scheduler
	WorkerID            string
	Now                 func() time.Time
}

type Job struct {
	Kind           string
	RecipientEmail string
	Template       string
	Payload        any
	Secret         string
	IdempotencyKey string
	MaxAttempts    int
}

type Row struct {
	ID                    string
	Kind                  string
	RecipientEmail        string
	Template              string
	Payload               json.RawMessage
	EmailSecretCiphertext string
	AttemptCount          int
	MaxAttempts           int
	CreatedAt             time.Time
}

type PermanentError struct{ Err error }
type TransientError struct{ Err error }

func (e PermanentError) Error() string {
	if e.Err == nil {
		return "permanent email outbox failure"
	}
	return e.Err.Error()
}
func (e PermanentError) Unwrap() error { return e.Err }
func (e TransientError) Error() string {
	if e.Err == nil {
		return "transient email outbox failure"
	}
	return e.Err.Error()
}
func (e TransientError) Unwrap() error { return e.Err }

func NewStore(options Options) *Store {
	if options.Pool == nil {
		return nil
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	mailer := options.Mailer
	if mailer == nil {
		mailer = email.DisabledMailer{}
	}
	workerID := strings.TrimSpace(options.WorkerID)
	if workerID == "" {
		workerID = defaultWorkerID()
	}
	return &Store{pool: options.Pool, mailer: mailer, mailerProvider: options.MailerProvider, secretEncryptionKey: strings.TrimSpace(options.SecretEncryptionKey), scheduler: options.Scheduler, workerID: workerID, now: now, batchSize: defaultBatchSize, maxAttempts: defaultMaxAttempts, leaseDuration: defaultLeaseDuration}
}

func (s *Store) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if s == nil || scheduler == nil {
		return nil
	}
	s.scheduler = scheduler
	return scheduler.Register(background.WorkerSpec{
		Name: WorkerName, Priority: background.PriorityNormalBackground, MaxPriority: background.PriorityNormalBackground,
		QueueLimit: defaultBatchSize, ConcurrencyLimit: defaultConcurrency, DrainPolicy: background.DrainBestEffort,
		CoalescePolicy: background.CoalesceDropNew, RetryPolicy: &background.RetryPolicy{MaxAttempts: 3, Delay: defaultPollInterval},
		PeriodicTrigger: &background.PeriodicTrigger{Interval: defaultPollInterval}, Timeout: defaultShutdownTimeout,
	}, s.handleScheduledSend)
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
		return fmt.Errorf("wake email outbox worker: %s", result.Status)
	}
}

func (s *Store) EnqueueTx(ctx context.Context, tx pgx.Tx, job Job) (string, error) {
	if s == nil {
		return "", fmt.Errorf("email outbox store is required")
	}
	return InsertTx(ctx, tx, s.secretEncryptionKey, s.now, job)
}

func InsertTx(ctx context.Context, tx pgx.Tx, secretEncryptionKey string, now func() time.Time, job Job) (string, error) {
	if strings.TrimSpace(job.Kind) == "" || strings.TrimSpace(job.RecipientEmail) == "" || strings.TrimSpace(job.Template) == "" || strings.TrimSpace(job.IdempotencyKey) == "" {
		return "", fmt.Errorf("email outbox job missing required identity")
	}
	secretCiphertext := ""
	if strings.TrimSpace(job.Secret) != "" {
		encrypted, err := endpointdomain.EncryptSecret(job.Secret, secretEncryptionKey, now)
		if err != nil {
			return "", fmt.Errorf("encrypt email outbox secret: %w", err)
		}
		secretCiphertext = encrypted
	}
	payload := job.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal email outbox payload: %w", err)
	}
	maxAttempts := job.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	id, err := newUUID()
	if err != nil {
		return "", err
	}
	var rowID string
	err = tx.QueryRow(ctx, `
		INSERT INTO email_outbox (
			id, kind, recipient_email, template, payload_json, email_secret_ciphertext,
			idempotency_key, status, attempt_count, max_attempts, next_attempt_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, 'queued', 0, $8, now(), now(), now())
		ON CONFLICT (idempotency_key) DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
		RETURNING id::text`, id, strings.TrimSpace(job.Kind), strings.TrimSpace(job.RecipientEmail), strings.TrimSpace(job.Template), rawPayload, secretCiphertext, strings.TrimSpace(job.IdempotencyKey), maxAttempts).Scan(&rowID)
	if err != nil {
		return "", fmt.Errorf("insert email outbox row: %w", err)
	}
	return rowID, nil
}

func (s *Store) handleScheduledSend(ctx context.Context, _ background.Job) background.JobResult {
	if err := s.ProcessDue(ctx); err != nil {
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	return background.JobResult{Status: background.JobSucceeded}
}

func (s *Store) ProcessDue(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return nil
	}
	if err := s.RecoverStaleLocks(ctx); err != nil {
		return err
	}
	rows, err := s.ClaimBatch(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		s.processRow(ctx, row)
	}
	return nil
}

func (s *Store) RecoverStaleLocks(ctx context.Context) error {
	claimCtx, cancel := context.WithTimeout(ctx, defaultClaimTimeout)
	defer cancel()
	_, err := s.pool.Exec(claimCtx, `
		UPDATE email_outbox
		SET status = 'queued', locked_by = NULL, locked_until = NULL, updated_at = now()
		WHERE status = 'sending' AND locked_until < now()`)
	if err != nil {
		return fmt.Errorf("recover stale email outbox locks: %w", err)
	}
	return nil
}

func (s *Store) ClaimBatch(ctx context.Context) ([]Row, error) {
	claimCtx, cancel := context.WithTimeout(ctx, defaultClaimTimeout)
	defer cancel()
	return pgxutil.InTxValue(claimCtx, s.pool, "email_outbox_claim", func(tx pgx.Tx) ([]Row, error) {
		rows, err := tx.Query(claimCtx, `
			WITH claimable AS (
				SELECT id FROM email_outbox
				WHERE status = 'queued' AND next_attempt_at <= now()
				ORDER BY next_attempt_at ASC, created_at ASC, id ASC
				LIMIT $1
				FOR UPDATE SKIP LOCKED
			)
			UPDATE email_outbox outbox
			SET status = 'sending', locked_by = $2, locked_until = now() + $3::interval, updated_at = now(), last_error = NULL
			FROM claimable
			WHERE outbox.id = claimable.id
			RETURNING outbox.id::text, outbox.kind, outbox.recipient_email, outbox.template, outbox.payload_json,
				COALESCE(outbox.email_secret_ciphertext, ''), outbox.attempt_count, outbox.max_attempts, outbox.created_at`, s.batchSize, s.workerID, intervalLiteral(s.leaseDuration))
		if err != nil {
			return nil, fmt.Errorf("claim email outbox rows: %w", err)
		}
		defer rows.Close()
		claimed := []Row{}
		for rows.Next() {
			var row Row
			if err := rows.Scan(&row.ID, &row.Kind, &row.RecipientEmail, &row.Template, &row.Payload, &row.EmailSecretCiphertext, &row.AttemptCount, &row.MaxAttempts, &row.CreatedAt); err != nil {
				return nil, fmt.Errorf("scan email outbox row: %w", err)
			}
			claimed = append(claimed, row)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate email outbox rows: %w", err)
		}
		return claimed, nil
	})
}

func (s *Store) processRow(ctx context.Context, row Row) {
	secret, err := endpointdomain.DecryptSecret(row.EmailSecretCiphertext, s.secretEncryptionKey)
	if err == nil && strings.TrimSpace(secret) == "" {
		err = PermanentError{Err: fmt.Errorf("email secret is missing")}
	}
	if err == nil {
		sendCtx, cancel := context.WithTimeout(ctx, defaultSendTimeout)
		err = s.send(sendCtx, row, secret)
		cancel()
	}
	if err == nil {
		s.finalizeSuccess(context.Background(), row.ID)
		return
	}
	s.finalizeFailure(context.Background(), row, err)
}

func (s *Store) send(ctx context.Context, row Row, secret string) error {
	mailer := s.currentMailer()
	switch row.Template {
	case TemplateEmailVerificationOTP:
		return mailer.SendEmailVerificationOTP(ctx, row.RecipientEmail, secret)
	case TemplatePasswordReset:
		return mailer.SendPasswordResetEmail(ctx, row.RecipientEmail, secret)
	default:
		return PermanentError{Err: fmt.Errorf("unknown email template")}
	}
}

func (s *Store) currentMailer() Mailer {
	if s == nil {
		return email.DisabledMailer{}
	}
	if s.mailerProvider != nil {
		if mailer := s.mailerProvider.Mailer(); mailer != nil {
			return mailer
		}
	}
	if s.mailer != nil {
		return s.mailer
	}
	return email.DisabledMailer{}
}

func (s *Store) finalizeSuccess(ctx context.Context, id string) {
	_, err := s.pool.Exec(ctx, `UPDATE email_outbox SET status = 'sent', sent_at = now(), locked_by = NULL, locked_until = NULL, email_secret_ciphertext = NULL, last_error = NULL, updated_at = now() WHERE id = $1`, id)
	if err != nil {
		slog.Error("failed to finalize email outbox success", "error", sanitizeError(err), "outbox_id", id)
	}
}

func (s *Store) finalizeFailure(ctx context.Context, row Row, err error) {
	sanitized := sanitizeError(err)
	status := "queued"
	nextAttemptAt := s.now().UTC().Add(backoffForAttempt(row.ID, row.AttemptCount+1))
	newAttemptCount := row.AttemptCount + 1
	var permanent PermanentError
	if errors.As(err, &permanent) || newAttemptCount >= row.MaxAttempts {
		status = "dead"
		nextAttemptAt = s.now().UTC()
	}
	_, execErr := s.pool.Exec(ctx, `
		UPDATE email_outbox
		SET status = $2, attempt_count = $3, next_attempt_at = $4, locked_by = NULL, locked_until = NULL,
			dead_lettered_at = CASE WHEN $2 = 'dead' THEN now() ELSE dead_lettered_at END,
			last_error = $5, updated_at = now()
		WHERE id = $1`, row.ID, status, newAttemptCount, nextAttemptAt, sanitized)
	if execErr != nil {
		slog.Error("failed to finalize email outbox failure", "error", sanitizeError(execErr), "outbox_id", row.ID, "send_error", sanitized)
		return
	}
	slog.Error("email outbox send failed", "error", sanitized, "outbox_id", row.ID, "kind", row.Kind, "status", status, "attempt", newAttemptCount)
}

func backoffForAttempt(id string, attempt int) time.Duration {
	if attempt <= 1 {
		attempt = 1
	}
	delay := time.Duration(float64(defaultBackoffBase) * math.Pow(2, float64(attempt-1)))
	if delay > defaultBackoffCap || delay <= 0 {
		delay = defaultBackoffCap
	}
	factor := 0.8 + (float64(jitterSeed(id, attempt)%41) / 100.0)
	return time.Duration(float64(delay) * factor)
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|otp|code)=([^\s&]+)`),
		regexp.MustCompile(`[A-Za-z0-9._~+\-]{24,}`),
		regexp.MustCompile(`\b[0-9]{6}\b`),
	}
	for _, pattern := range patterns {
		value = pattern.ReplaceAllString(value, "$1=[redacted]")
	}
	if len(value) > 300 {
		value = value[:300]
	}
	return value
}

func newUUID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate email outbox id: %w", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func jitterSeed(id string, attempt int) uint32 {
	h := fnv.New32a()
	_, _ = fmt.Fprintf(h, "%s:%d", id, attempt)
	return h.Sum32()
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
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s:%d", hostname, os.Getpid())
}
