package integrationtest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/alerting"
	"github.com/coachpo/prism/backend/internal/platform/background"
)

func TestAlertWebhookOutboxEnqueueIdempotent(t *testing.T) {
	ctx, pool := newAlertingIntegrationPool(t)
	store := alerting.NewStore(alerting.Options{
		Pool:               pool,
		WebhookURLProvider: &integrationAlertWebhookURLProvider{url: "https://alerts.example.test/webhook"},
	})

	payload := integrationAlertIncidentPayload("banned")
	for range 2 {
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return store.EnqueueTx(ctx, tx, payload)
		}); err != nil {
			t.Fatalf("enqueue alert: %v", err)
		}
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alert_webhook_outbox WHERE event_type = 'banned'`).Scan(&count); err != nil {
		t.Fatalf("count queued alerts: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one idempotent alert row, got %d", count)
	}
}

func TestAlertWebhookOutboxWorkerPostsQueuedPayload(t *testing.T) {
	ctx, pool := newAlertingIntegrationPool(t)
	scheduler := background.NewScheduler(background.Config{})
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected webhook request method=%s content-type=%s", r.Method, r.Header.Get("Content-Type"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read webhook body: %v", err)
		}
		received <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	store := newAlertingIntegrationWorkerStore(t, scheduler, pool, &integrationAlertWebhookURLProvider{url: server.URL})
	enqueueAlertingIntegrationPayload(t, ctx, pool, store, integrationAlertIncidentPayload("banned"))
	if err := store.Wake(ctx); err != nil {
		t.Fatalf("wake alert worker: %v", err)
	}

	select {
	case body := <-received:
		if !strings.Contains(body, `"event_type":"banned"`) || !strings.Contains(body, `"connection_id":3`) {
			t.Fatalf("unexpected webhook payload: %s", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for alert webhook delivery")
	}
	waitForAlertingIntegrationStatus(t, ctx, pool, "banned", "sent", 0)
}

func TestAlertWebhookOutboxWorkerUsesCurrentWebhookURLProviderSnapshot(t *testing.T) {
	ctx, pool := newAlertingIntegrationPool(t)
	scheduler := background.NewScheduler(background.Config{})
	first := make(chan struct{}, 1)
	second := make(chan struct{}, 1)
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(firstServer.Close)
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		second <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(secondServer.Close)

	provider := &integrationAlertWebhookURLProvider{url: firstServer.URL}
	store := newAlertingIntegrationWorkerStore(t, scheduler, pool, provider)

	enqueueAlertingIntegrationPayload(t, ctx, pool, store, integrationAlertIncidentPayload("banned"))
	if err := store.Wake(ctx); err != nil {
		t.Fatalf("wake first alert worker run: %v", err)
	}
	select {
	case <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first webhook delivery")
	}
	// Wait for the first worker run to fully finish (outbox row sent) so the
	// second Wake is not dropped by the coalesce policy while the first job is
	// still held in the scheduler queue.
	waitForAlertingIntegrationStatus(t, ctx, pool, "banned", "sent", 0)

	provider.publish(secondServer.URL)
	payload := integrationAlertIncidentPayload("recovered")
	payload.OccurredAt = payload.OccurredAt.Add(time.Second)
	enqueueAlertingIntegrationPayload(t, ctx, pool, store, payload)
	if err := store.Wake(ctx); err != nil {
		t.Fatalf("wake second alert worker run: %v", err)
	}
	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for second webhook delivery")
	}
}

func TestAlertWebhookOutboxWorkerRetriesTransientFailure(t *testing.T) {
	ctx, pool := newAlertingIntegrationPool(t)
	scheduler := background.NewScheduler(background.Config{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	store := newAlertingIntegrationWorkerStore(t, scheduler, pool, &integrationAlertWebhookURLProvider{url: server.URL})
	enqueueAlertingIntegrationPayload(t, ctx, pool, store, integrationAlertIncidentPayload("banned"))
	if err := store.Wake(ctx); err != nil {
		t.Fatalf("wake alert worker: %v", err)
	}

	waitForAlertingIntegrationStatus(t, ctx, pool, "banned", "queued", 1)
	var backedOff bool
	if err := pool.QueryRow(ctx, `SELECT next_attempt_at > now() FROM alert_webhook_outbox WHERE event_type = 'banned'`).Scan(&backedOff); err != nil {
		t.Fatalf("load alert backoff state: %v", err)
	}
	if !backedOff {
		t.Fatal("expected failed webhook to back off")
	}
}

type integrationAlertWebhookURLProvider struct {
	mu  sync.Mutex
	url string
}

func (p *integrationAlertWebhookURLProvider) AlertingWebhookURL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.url
}

func (p *integrationAlertWebhookURLProvider) publish(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.url = url
}

func newAlertingIntegrationPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	harness := newPostgresHarness(t)
	databaseName := "alerting_outbox_" + randomSuffix(t)
	conn := harness.openDatabase(t, ctx, databaseName)
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup connection: %v", err)
	}

	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}

func newAlertingIntegrationWorkerStore(t *testing.T, scheduler *background.Scheduler, pool *pgxpool.Pool, provider alerting.WebhookURLProvider) *alerting.Store {
	t.Helper()
	store := alerting.NewStore(alerting.Options{Pool: pool, Scheduler: scheduler, WebhookURLProvider: provider})
	if err := store.RegisterBackgroundWorker(scheduler); err != nil {
		t.Fatalf("register alert worker: %v", err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start alert scheduler: %v", err)
	}
	t.Cleanup(func() {
		_ = scheduler.Stop(context.Background(), time.Now().Add(5*time.Second))
	})
	return store
}

func enqueueAlertingIntegrationPayload(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *alerting.Store, payload alerting.IncidentPayload) {
	t.Helper()
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return store.EnqueueTx(ctx, tx, payload)
	}); err != nil {
		t.Fatalf("enqueue alerting payload: %v", err)
	}
}

func integrationAlertIncidentPayload(eventType string) alerting.IncidentPayload {
	bannedUntil := time.Date(2026, time.June, 7, 15, 45, 0, 0, time.UTC)
	return alerting.IncidentPayload{
		EventType:     eventType,
		ConnectionID:  3,
		EndpointID:    1,
		ModelID:       "gpt-test",
		BannedUntilAt: &bannedUntil,
		OccurredAt:    time.Date(2026, time.June, 7, 15, 30, 0, 0, time.UTC),
	}
}

func waitForAlertingIntegrationStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventType string, wantStatus string, wantAttempts int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var status string
		var attempts int
		err := pool.QueryRow(ctx, `SELECT status, attempt_count FROM alert_webhook_outbox WHERE event_type = $1`, eventType).Scan(&status, &attempts)
		if err == nil && status == wantStatus && attempts == wantAttempts {
			return
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("load alerting status for %s: %v", eventType, err)
			}
			t.Fatalf("expected %s/%d, got %s/%d", wantStatus, wantAttempts, status, attempts)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
