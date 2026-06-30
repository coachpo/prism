package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
)

type integrationRuntimePersistenceCounts struct {
	RequestLogs int
	AuditLogs   int
	UsageEvents int
	OutboxRows  int
}

type integrationRuntimeTransportRecorder struct {
	calls atomic.Int32
}

func (recorder *integrationRuntimeTransportRecorder) RoundTrip(_ *http.Request) (*http.Response, error) {
	recorder.calls.Add(1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
	}, nil
}

func TestRuntimeNegativeRouteMatrixMountedThroughPlatform(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	postgres := newPostgresHarness(t)
	databaseName := "runtime_negative_routes_" + randomSuffix(t)
	conn := postgres.openDatabase(t, testContext, databaseName)
	defer func() { _ = conn.Close(testContext) }()
	if _, err := newRunner(t).Run(testContext, conn); err != nil {
		t.Fatalf("apply runtime negative-route baseline: %v", err)
	}

	databaseURL := postgres.connectionString(databaseName)
	executionPool := openIntegrationRuntimePool(t, testContext, databaseURL, "execution")
	telemetryPool := openIntegrationRuntimePool(t, testContext, databaseURL, "telemetry")
	feedbackPool := openIntegrationRuntimePool(t, testContext, databaseURL, "feedback")
	transport := &integrationRuntimeTransportRecorder{}
	sideEffectSubmits := &atomic.Int32{}
	settings := config.Settings{
		Host:                "127.0.0.1",
		Port:                8000,
		AppEnv:              config.EnvironmentProduction,
		DatabaseURL:         databaseURL,
		SecretEncryptionKey: "integration-runtime-secret",
		AuthJWTSecret:       "integration-runtime-jwt-secret",
	}
	runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{
		ExecutionPool: executionPool,
		TelemetryPool: telemetryPool,
		FeedbackPool:  feedbackPool,
		HTTPClient:    &http.Client{Transport: transport},
		Scheduler:     background.NewScheduler(background.Config{}),
		SideEffects: runtimeapi.RuntimeSideEffectOptions{Hooks: &runtimeapi.RuntimeSideEffectHooks{
			AfterSubmit: func(runtimeapi.RuntimeSideEffectSubmitResult) {
				sideEffectSubmits.Add(1)
			},
		}},
	})
	if err != nil {
		t.Fatalf("build integration runtime route service: %v", err)
	}
	t.Cleanup(runtimeService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{
		Version:        "integration-test",
		RuntimeService: runtimeService,
	})
	if err != nil {
		t.Fatalf("build integration platform handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	baseline := loadIntegrationRuntimePersistenceCounts(t, testContext, conn)
	if baseline != (integrationRuntimePersistenceCounts{}) {
		t.Fatalf("expected fresh runtime negative-route persistence counts to be empty, got %+v", baseline)
	}

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantDetail string
		wantAllow  string
	}{
		{name: "wrong method OpenAI models route", method: http.MethodPost, path: "/v1/models", wantStatus: http.StatusMethodNotAllowed, wantDetail: "Method not allowed for runtime operation", wantAllow: http.MethodGet},
		{name: "unsupported Anthropic route", method: http.MethodPost, path: "/v1/messages/batches", wantStatus: http.StatusNotFound, wantDetail: "Runtime operation not found"},
		{name: "unsupported Gemini action", method: http.MethodPost, path: "/v1beta/models/gemini-negative:embedContent", wantStatus: http.StatusNotFound, wantDetail: "Runtime operation not found"},
		{name: "wrong method OpenAI route", method: http.MethodGet, path: "/v1/chat/completions", wantStatus: http.StatusMethodNotAllowed, wantDetail: "Method not allowed for runtime operation", wantAllow: http.MethodPost},
		{name: "wrong method Anthropic route", method: http.MethodGet, path: "/v1/messages", wantStatus: http.StatusMethodNotAllowed, wantDetail: "Method not allowed for runtime operation", wantAllow: http.MethodPost},
		{name: "wrong method Gemini route", method: http.MethodGet, path: "/v1beta/models/gemini-negative:generateContent", wantStatus: http.StatusMethodNotAllowed, wantDetail: "Method not allowed for runtime operation", wantAllow: http.MethodPost},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(test.method, server.URL+test.path, bytes.NewBufferString(`{"model":"negative-route"}`))
			if err != nil {
				t.Fatalf("build negative-route request: %v", err)
			}
			request.Header.Set("Content-Type", "application/json")
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatalf("send negative-route request: %v", err)
			}
			assertIntegrationRuntimeJSONError(t, response, test.wantStatus, test.wantDetail, test.wantAllow)
			if got := transport.calls.Load(); got != 0 {
				t.Fatalf("expected mounted rejected route not to reach provider transport, got %d calls", got)
			}
			if got := sideEffectSubmits.Load(); got != 0 {
				t.Fatalf("expected mounted rejected route not to submit runtime side effects, got %d submissions", got)
			}
		})
	}
	assertIntegrationRuntimePersistenceCountsRemain(t, testContext, conn, baseline, 500*time.Millisecond)
}

func openIntegrationRuntimePool(t *testing.T, ctx context.Context, databaseURL string, name string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open integration runtime %s pool: %v", name, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func assertIntegrationRuntimeJSONError(t *testing.T, response *http.Response, wantStatus int, wantDetail string, wantAllow string) {
	t.Helper()
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != wantStatus {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("expected status %d, got %d with body %s", wantStatus, response.StatusCode, strings.TrimSpace(string(body)))
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("expected runtime JSON content type, got %q", contentType)
	}
	var payload map[string]string
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode runtime JSON error: %v", err)
	}
	if payload["detail"] != wantDetail {
		t.Fatalf("expected detail %q, got %+v", wantDetail, payload)
	}
	if allow := response.Header.Get("Allow"); allow != wantAllow {
		t.Fatalf("expected Allow header %q, got %q", wantAllow, allow)
	}
}

func assertIntegrationRuntimePersistenceCountsRemain(t *testing.T, ctx context.Context, conn *pgx.Conn, want integrationRuntimePersistenceCounts, duration time.Duration) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for {
		got := loadIntegrationRuntimePersistenceCounts(t, ctx, conn)
		if got != want {
			t.Fatalf("expected mounted rejected routes to leave runtime persistence counts %+v, got %+v", want, got)
		}
		if !time.Now().Before(deadline) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func loadIntegrationRuntimePersistenceCounts(t *testing.T, ctx context.Context, conn *pgx.Conn) integrationRuntimePersistenceCounts {
	t.Helper()
	var counts integrationRuntimePersistenceCounts
	if err := conn.QueryRow(ctx, `SELECT
			(SELECT COUNT(*) FROM request_logs),
			(SELECT COUNT(*) FROM audit_logs),
			(SELECT COUNT(*) FROM usage_request_events),
			(SELECT COUNT(*) FROM runtime_telemetry_outbox)`).Scan(
		&counts.RequestLogs,
		&counts.AuditLogs,
		&counts.UsageEvents,
		&counts.OutboxRows,
	); err != nil {
		t.Fatalf("load integration runtime persistence counts: %v", err)
	}
	return counts
}
