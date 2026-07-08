package alerting

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

var alertingTestPostgres struct {
	once     sync.Once
	hostPort string
	name     string
	err      error
}

type fakeWebhookURLProvider struct {
	mu  sync.Mutex
	url string
}

func TestMain(m *testing.M) {
	code := m.Run()
	if alertingTestPostgres.name != "" {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_, _ = alertingDockerCommand(cleanupCtx, "rm", "-f", alertingTestPostgres.name)
		cancel()
	}
	os.Exit(code)
}

func TestEnqueueIdempotent(t *testing.T) {
	ctx, pool := openAlertingTestPool(t)
	store := NewStore(Options{Pool: pool, WebhookURLProvider: &fakeWebhookURLProvider{url: "https://alerts.example.test/webhook"}})
	payload := testIncidentPayload("banned")
	for range 2 {
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return store.EnqueueTx(ctx, tx, payload)
		})
		if err != nil {
			t.Fatalf("enqueue alert: %v", err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alert_webhook_outbox WHERE event_type = 'banned'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one idempotent alert row, got %d", count)
	}
}

func TestWorkerPostsQueuedPayload(t *testing.T) {
	ctx, pool := openAlertingTestPool(t)
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected webhook request method=%s content-type=%s", r.Method, r.Header.Get("Content-Type"))
		}
		raw := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(raw)
		received <- string(raw)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	store := NewStore(Options{Pool: pool, WebhookURLProvider: &fakeWebhookURLProvider{url: server.URL}})
	enqueueAlertingTestPayload(t, ctx, pool, store, testIncidentPayload("banned"))
	store.handleScheduledPost(ctx, background.Job{})
	assertAlertingStatus(t, ctx, pool, "banned", "sent", 0)
	body := <-received
	if !strings.Contains(body, `"event_type":"banned"`) || !strings.Contains(body, `"connection_id":3`) {
		t.Fatalf("unexpected webhook payload: %s", body)
	}
}

func TestWorkerUsesCurrentWebhookURLProviderSnapshot(t *testing.T) {
	ctx, pool := openAlertingTestPool(t)
	first := make(chan struct{}, 1)
	second := make(chan struct{}, 1)
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer firstServer.Close()
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		second <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer secondServer.Close()

	provider := &fakeWebhookURLProvider{url: firstServer.URL}
	store := NewStore(Options{Pool: pool, WebhookURLProvider: provider})
	enqueueAlertingTestPayload(t, ctx, pool, store, testIncidentPayload("banned"))
	store.handleScheduledPost(ctx, background.Job{})
	<-first

	provider.publish(secondServer.URL)
	payload := testIncidentPayload("recovered")
	payload.OccurredAt = payload.OccurredAt.Add(time.Second)
	enqueueAlertingTestPayload(t, ctx, pool, store, payload)
	store.handleScheduledPost(ctx, background.Job{})
	<-second
}

func TestWorkerRetriesTransientFailure(t *testing.T) {
	ctx, pool := openAlertingTestPool(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	store := NewStore(Options{Pool: pool, WebhookURLProvider: &fakeWebhookURLProvider{url: server.URL}})
	enqueueAlertingTestPayload(t, ctx, pool, store, testIncidentPayload("banned"))
	store.handleScheduledPost(ctx, background.Job{})
	assertAlertingStatus(t, ctx, pool, "banned", "queued", 1)
	var backedOff bool
	if err := pool.QueryRow(ctx, `SELECT next_attempt_at > now() FROM alert_webhook_outbox WHERE event_type = 'banned'`).Scan(&backedOff); err != nil {
		t.Fatal(err)
	}
	if !backedOff {
		t.Fatal("expected failed webhook to back off")
	}
}

func openAlertingTestPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	harness := alertingPostgresHarness(t)
	databaseName := "alerting_" + strings.ReplaceAll(t.Name(), "/", "_") + "_" + fmt.Sprintf("%d", time.Now().UnixNano())
	admin, err := pgx.Connect(ctx, harness.connectionString("postgres"))
	if err != nil {
		t.Fatalf("connect admin database: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+quoteAlertingIdentifier(databaseName)); err != nil {
		t.Fatalf("create database: %v", err)
	}
	runner, err := migrate.New(migrate.Options{})
	if err != nil {
		t.Fatalf("build migration runner: %v", err)
	}
	migrateConn, err := pgx.Connect(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("connect migration database: %v", err)
	}
	defer func() { _ = migrateConn.Close(ctx) }()
	if _, err := runner.Run(ctx, migrateConn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}

func enqueueAlertingTestPayload(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, payload IncidentPayload) {
	t.Helper()
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return store.EnqueueTx(ctx, tx, payload) }); err != nil {
		t.Fatalf("enqueue alerting payload: %v", err)
	}
}

func testIncidentPayload(eventType string) IncidentPayload {
	bannedUntil := time.Date(2026, time.June, 7, 15, 45, 0, 0, time.UTC)
	return IncidentPayload{
		EventType:     eventType,
		ConnectionID:  3,
		EndpointID:    1,
		ModelID:       "gpt-test",
		BannedUntilAt: &bannedUntil,
		OccurredAt:    time.Date(2026, time.June, 7, 15, 30, 0, 0, time.UTC),
	}
}

func assertAlertingStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventType string, wantStatus string, wantAttempts int) {
	t.Helper()
	var status string
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT status, attempt_count FROM alert_webhook_outbox WHERE event_type = $1`, eventType).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || attempts != wantAttempts {
		t.Fatalf("expected %s/%d, got %s/%d", wantStatus, wantAttempts, status, attempts)
	}
}

func (p *fakeWebhookURLProvider) AlertingWebhookURL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.url
}

func (p *fakeWebhookURLProvider) publish(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.url = url
}

func alertingPostgresHarness(t *testing.T) alertingHarness {
	t.Helper()
	alertingTestPostgres.once.Do(func() {
		containerName := "prism-alerting-" + fmt.Sprintf("%d", time.Now().UnixNano())
		if _, err := alertingDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-p", "127.0.0.1::5432", "postgres:16-alpine"); err != nil {
			alertingTestPostgres.err = err
			return
		}
		hostPort, err := alertingDockerPort(containerName)
		if err != nil {
			alertingTestPostgres.err = err
			return
		}
		if err := alertingWaitForPostgres(hostPort); err != nil {
			alertingTestPostgres.err = err
			return
		}
		alertingTestPostgres.hostPort = hostPort
		alertingTestPostgres.name = containerName
	})
	if alertingTestPostgres.err != nil {
		t.Fatalf("start postgres harness: %v", alertingTestPostgres.err)
	}
	return alertingHarness{hostPort: alertingTestPostgres.hostPort}
}

type alertingHarness struct{ hostPort string }

func (h alertingHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func alertingDockerPort(containerName string) (string, error) {
	output, err := alertingDockerCommand(context.Background(), "port", containerName, "5432/tcp")
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.TrimSpace(output), ":")
	if len(parts) < 2 {
		return "", fmt.Errorf("unexpected docker port output %q", output)
	}
	return strings.TrimSpace(parts[len(parts)-1]), nil
}

func alertingWaitForPostgres(hostPort string) error {
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/postgres?sslmode=disable", hostPort))
		if err == nil {
			_ = conn.Close(ctx)
			cancel()
			return nil
		}
		cancel()
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("postgres container on port %s did not become ready", hostPort)
}

func alertingDockerCommand(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func quoteAlertingIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
