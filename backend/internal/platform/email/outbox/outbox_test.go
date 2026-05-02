package outbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/background"
	platformemail "github.com/coachpo/prism/backend/internal/platform/email"
)

var testHarness postgresHarness

type postgresHarness struct {
	containerName string
	hostPort      string
}

type fakeMailer struct {
	mu   sync.Mutex
	sent []string
	err  error
}

type fakeMailerProvider struct {
	mu     sync.Mutex
	mailer Mailer
}

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	harness, err := startPostgres(ctx)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres: %v\n", err)
		os.Exit(1)
	}
	testHarness = harness
	code := m.Run()
	cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	_ = exec.CommandContext(cleanupContext, "docker", "rm", "-f", harness.containerName).Run()
	cleanupCancel()
	os.Exit(code)
}

func TestEnqueueIdempotent(t *testing.T) {
	ctx, pool := openTestPool(t)
	store := NewStore(Options{Pool: pool, SecretEncryptionKey: "test-secret"})
	for i := range 2 {
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			_, err := store.EnqueueTx(ctx, tx, testJob("idempotent", "123456"))
			return err
		})
		if err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM email_outbox WHERE idempotency_key = 'idempotent'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one idempotent row, got %d", count)
	}
}

func TestEnqueuePayloadDoesNotExposePlaintextSecrets(t *testing.T) {
	ctx, pool := openTestPool(t)
	store := NewStore(Options{Pool: pool, SecretEncryptionKey: "test-secret"})
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := store.EnqueueTx(ctx, tx, testJob("secret-safe", "123456"))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload, ciphertext string
	if err := pool.QueryRow(ctx, `SELECT payload_json::text, email_secret_ciphertext FROM email_outbox WHERE idempotency_key = 'secret-safe'`).Scan(&payload, &ciphertext); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "123456") || strings.Contains(ciphertext, "123456") || ciphertext == "123456" {
		t.Fatalf("outbox exposed plaintext secret payload=%q ciphertext=%q", payload, ciphertext)
	}
}

func TestWorkerSendsQueuedJob(t *testing.T) {
	ctx, pool := openTestPool(t)
	mailer := &fakeMailer{}
	store := NewStore(Options{Pool: pool, Mailer: mailer, SecretEncryptionKey: "test-secret"})
	enqueueTestJob(t, ctx, store, "send-ok", "123456")
	store.handleScheduledSend(ctx, zeroJob())
	assertStatus(t, ctx, pool, "send-ok", "sent", 0)
	if got := mailer.count(); got != 1 {
		t.Fatalf("expected one send, got %d", got)
	}
}

func TestWorkerUsesCurrentMailerProviderSnapshot(t *testing.T) {
	ctx, pool := openTestPool(t)
	firstMailer := &fakeMailer{}
	secondMailer := &fakeMailer{}
	provider := &fakeMailerProvider{mailer: firstMailer}
	store := NewStore(Options{Pool: pool, MailerProvider: provider, SecretEncryptionKey: "test-secret"})
	enqueueTestJob(t, ctx, store, "provider-first", "111111")
	store.handleScheduledSend(ctx, zeroJob())
	assertStatus(t, ctx, pool, "provider-first", "sent", 0)
	if got := firstMailer.count(); got != 1 {
		t.Fatalf("expected first provider mailer to send once, got %d", got)
	}

	provider.publish(secondMailer)
	enqueueTestJob(t, ctx, store, "provider-second", "222222")
	store.handleScheduledSend(ctx, zeroJob())
	assertStatus(t, ctx, pool, "provider-second", "sent", 0)
	if got := secondMailer.count(); got != 1 {
		t.Fatalf("expected second provider mailer to send once, got %d", got)
	}
	if got := firstMailer.count(); got != 1 {
		t.Fatalf("expected first provider mailer not to be reused, got %d", got)
	}
}

func TestWorkerRetriesTransientFailure(t *testing.T) {
	ctx, pool := openTestPool(t)
	store := NewStore(Options{Pool: pool, Mailer: &fakeMailer{err: TransientError{Err: errors.New("temporary provider failure code=123456")}}, SecretEncryptionKey: "test-secret"})
	enqueueTestJob(t, ctx, store, "retry", "123456")
	store.handleScheduledSend(ctx, zeroJob())
	assertStatus(t, ctx, pool, "retry", "queued", 1)
	var backedOff bool
	var lastError string
	if err := pool.QueryRow(ctx, `SELECT next_attempt_at > now(), last_error FROM email_outbox WHERE idempotency_key = 'retry'`).Scan(&backedOff, &lastError); err != nil {
		t.Fatal(err)
	}
	if !backedOff || strings.Contains(lastError, "123456") {
		t.Fatalf("expected sanitized backed off retry, backedOff=%v error=%q", backedOff, lastError)
	}
}

func TestWorkerDeadLettersPermanentFailure(t *testing.T) {
	ctx, pool := openTestPool(t)
	store := NewStore(Options{Pool: pool, Mailer: &fakeMailer{err: PermanentError{Err: errors.New("bad template")}}, SecretEncryptionKey: "test-secret"})
	enqueueTestJob(t, ctx, store, "dead", "123456")
	store.handleScheduledSend(ctx, zeroJob())
	assertStatus(t, ctx, pool, "dead", "dead", 1)
}

func TestWorkerRecoversStaleLock(t *testing.T) {
	ctx, pool := openTestPool(t)
	store := NewStore(Options{Pool: pool, SecretEncryptionKey: "test-secret"})
	_, err := pool.Exec(ctx, `INSERT INTO email_outbox (id, kind, recipient_email, template, payload_json, idempotency_key, status, locked_by, locked_until) VALUES ($1, $2, $3, $4, '{}'::jsonb, $5, 'sending', 'dead-worker', now() - interval '5 minutes')`, mustUUID(t), KindEmailVerificationOTP, "operator@example.com", TemplateEmailVerificationOTP, "stale")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverStaleLocks(ctx); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, ctx, pool, "stale", "queued", 0)
}

func TestWorkerConcurrentClaimsUseSkipLocked(t *testing.T) {
	ctx, pool := openTestPool(t)
	storeA := NewStore(Options{Pool: pool, SecretEncryptionKey: "test-secret", WorkerID: "a"})
	storeB := NewStore(Options{Pool: pool, SecretEncryptionKey: "test-secret", WorkerID: "b"})
	storeA.batchSize = 1
	storeB.batchSize = 1
	enqueueTestJob(t, ctx, storeA, "claim-a", "111111")
	enqueueTestJob(t, ctx, storeA, "claim-b", "222222")
	rowsA, err := storeA.ClaimBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rowsB, err := storeB.ClaimBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rowsA) != 1 || len(rowsB) != 1 || rowsA[0].ID == rowsB[0].ID {
		t.Fatalf("expected distinct claims, got %+v and %+v", rowsA, rowsB)
	}
}

func TestWorkerDoesNotLogSecrets(t *testing.T) {
	ctx, pool := openTestPool(t)
	var buffer bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buffer, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	store := NewStore(Options{Pool: pool, Mailer: &fakeMailer{err: TransientError{Err: errors.New("temporary failure otp=123456 token=abcdefghijklmnopqrstuvwxyz")}}, SecretEncryptionKey: "test-secret"})
	enqueueTestJob(t, ctx, store, "log-safe", "123456")
	store.handleScheduledSend(ctx, zeroJob())
	logs := buffer.String()
	if strings.Contains(logs, "123456") || strings.Contains(logs, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("logs exposed secret: %s", logs)
	}
}

func openTestPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "outbox_" + strings.ReplaceAll(t.Name(), "/", "_") + "_" + fmt.Sprintf("%d", time.Now().UnixNano())
	admin, err := pgx.Connect(ctx, testHarness.connectionString("postgres"))
	if err != nil {
		t.Fatalf("connect admin database: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+quoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database: %v", err)
	}
	pool, err := pgxpool.New(ctx, testHarness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	migration, err := os.ReadFile("../../../../migrations/000008_email_outbox.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(migration)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	return ctx, pool
}

func enqueueTestJob(t *testing.T, ctx context.Context, store *Store, key string, secret string) {
	t.Helper()
	err := pgx.BeginFunc(ctx, store.pool, func(tx pgx.Tx) error { _, err := store.EnqueueTx(ctx, tx, testJob(key, secret)); return err })
	if err != nil {
		t.Fatalf("enqueue test job: %v", err)
	}
}

func testJob(key string, secret string) Job {
	return Job{Kind: KindEmailVerificationOTP, RecipientEmail: "operator@example.com", Template: TemplateEmailVerificationOTP, Secret: secret, IdempotencyKey: key, Payload: map[string]any{"purpose": "test"}}
}

func assertStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, key string, wantStatus string, wantAttempts int) {
	t.Helper()
	var status string
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT status, attempt_count FROM email_outbox WHERE idempotency_key = $1`, key).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || attempts != wantAttempts {
		t.Fatalf("expected %s/%d, got %s/%d", wantStatus, wantAttempts, status, attempts)
	}
}

func (m *fakeMailer) SendEmailVerificationOTP(_ context.Context, recipient string, otpCode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, recipient+":"+otpCode)
	return m.err
}

func (m *fakeMailer) SendPasswordResetEmail(ctx context.Context, recipient string, otpCode string) error {
	return m.SendEmailVerificationOTP(ctx, recipient, otpCode)
}

func (m *fakeMailer) count() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.sent) }

func (p *fakeMailerProvider) Mailer() platformemail.Mailer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mailer
}

func (p *fakeMailerProvider) publish(mailer Mailer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mailer = mailer
}

func zeroJob() background.Job { return background.Job{} }

func startPostgres(ctx context.Context) (postgresHarness, error) {
	containerName := "prism-email-outbox-test-" + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := runDocker(ctx, "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-p", "127.0.0.1::5432", "postgres:16-alpine"); err != nil {
		return postgresHarness{}, err
	}
	deadline := time.Now().Add(45 * time.Second)
	var port string
	for time.Now().Before(deadline) {
		var err error
		port, err = dockerPort(containerName)
		if err == nil && port != "" {
			conn, connErr := pgx.Connect(ctx, fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/postgres?sslmode=disable", port))
			if connErr == nil {
				_ = conn.Close(ctx)
				return postgresHarness{containerName: containerName, hostPort: port}, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return postgresHarness{}, fmt.Errorf("postgres container did not become ready")
}

func (h postgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func runDocker(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func dockerPort(containerName string) (string, error) {
	output, err := exec.Command("docker", "port", containerName, "5432/tcp").CombinedOutput()
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.TrimSpace(string(output)), ":")
	if len(parts) < 2 {
		return "", fmt.Errorf("unexpected docker port output %q", string(output))
	}
	return strings.TrimSpace(parts[len(parts)-1]), nil
}

func quoteIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }

func mustUUID(t *testing.T) string {
	t.Helper()
	id, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
