package runtime_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

const runtimeTelemetryOutboxBackpressureRequests = 8

func TestRuntimeDurableTelemetryIsDefault(t *testing.T) {
	gate := newRuntimeTelemetryMaterializeGate()
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
			WorkerCount:     1,
			PollInterval:    25 * time.Millisecond,
			ShutdownTimeout: time.Second,
			WakeupBuffer:    1,
			Hooks: &runtimeapi.TelemetryOutboxHooks{
				BeforeMaterialize: gate.Wait,
			},
		}},
	})
	profileID := harness.activeProfileID(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "telemetry-default-public-" + randomSuffix(),
		TargetModelID:   "telemetry-default-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/telemetry/default"),
		EndpointAPIKey:  "telemetry-default-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "durable telemetry should be the default runtime path"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 0, UsageEvents: 0, OutboxRows: 1}, 5*time.Second)

	gate.Release()
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
}

func TestRuntimeProxyKeyUsageDurablyEnqueues(t *testing.T) {
	shutdownTimeout := 150 * time.Millisecond
	closeResults := make(chan runtimeapi.TelemetryOutboxCloseResult, 1)
	gate := newRuntimeTelemetryMaterializeGate()
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
			WorkerCount:     1,
			PollInterval:    25 * time.Millisecond,
			ShutdownTimeout: shutdownTimeout,
			WakeupBuffer:    1,
			Hooks: &runtimeapi.TelemetryOutboxHooks{
				BeforeMaterialize: gate.Wait,
				AfterClose: func(result runtimeapi.TelemetryOutboxCloseResult) {
					closeResults <- result
				},
			},
		}},
	})
	profileID := harness.activeProfileID(t)
	proxyAPIKey := harness.enableRuntimeProxyAPIKeyAuth(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "telemetry-proxy-key-public-" + randomSuffix(),
		TargetModelID:   "telemetry-proxy-key-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/telemetry/proxy-key"),
		EndpointAPIKey:  "telemetry-proxy-key-upstream",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "proxy key usage should survive durable enqueue"}},
		"model":    route.PublicModelID,
	}, map[string]string{"Authorization": "Bearer " + proxyAPIKey})
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 0, UsageEvents: 0, OutboxRows: 1}, 5*time.Second)
	if got := loadProxyAPIKeyUsageMaterializationCount(t, harness.conn); got != 0 {
		t.Fatalf("expected proxy key usage to remain pending before replay, got %d materialized rows", got)
	}

	startedAt := time.Now()
	harness.runtimeService.Close()
	if elapsed := time.Since(startedAt); elapsed < shutdownTimeout || elapsed > time.Second {
		t.Fatalf("expected durable runtime close to respect the configured timeout around %s, got %s", shutdownTimeout, elapsed)
	}
	closeResult := <-closeResults
	if closeResult.Drained || !closeResult.TimedOut || closeResult.PendingRows != 1 {
		t.Fatalf("expected bounded shutdown timeout to leave one pending durable signal for replay, got %+v", closeResult)
	}
	countsAfterClose := loadRuntimeTelemetryCounts(t, harness.conn, profileID)
	if countsAfterClose.RequestLogs != 0 || countsAfterClose.UsageEvents != 0 || countsAfterClose.OutboxRows != 1 {
		t.Fatalf("expected shutdown-timeout close to leave one durable outbox row for restart replay, got %+v", countsAfterClose)
	}

	restartedHarness := restartRuntimeHarnessWithConfig(t, harness.databaseName, runtimeHarnessConfig{})
	waitForRuntimeTelemetryCounts(t, restartedHarness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	waitForProxyAPIKeyUsageMaterialization(t, restartedHarness.conn, 5*time.Second)
	assertLatestRuntimeAttemptCounts(t, restartedHarness.conn, profileID, 1, 1)
}

func TestRuntimeResponsesProxyKeyAuthAndUsage(t *testing.T) {
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{})
	profileID := harness.activeProfileID(t)
	proxyAPIKey := harness.enableRuntimeProxyAPIKeyAuth(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "responses-proxy-key-public-" + randomSuffix(),
		TargetModelID:   "responses-proxy-key-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/responses/proxy-key"),
		EndpointAPIKey:  "responses-proxy-key-upstream",
	})
	payload := map[string]any{
		"input": "responses proxy key parity",
		"model": route.PublicModelID,
	}

	missingKeyResponse := harness.requestJSON(t, http.MethodPost, "/v1/responses", payload, nil)
	assertStatus(t, missingKeyResponse, http.StatusUnauthorized)
	assertResponseField(t, missingKeyResponse, "detail", "Proxy API key required")
	if got := len(harness.upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected missing Responses proxy key to stop before upstream, got %d upstream requests", got)
	}

	successResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/responses",
		payload,
		map[string]string{"Authorization": "Bearer " + proxyAPIKey},
	)
	assertStatus(t, successResponse, http.StatusOK)
	requests := harness.upstream.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("expected valid Responses proxy key request to reach upstream once, got %d upstream requests", len(requests))
	}
	if requests[0].Path != "/responses/proxy-key/v1/responses" {
		t.Fatalf("expected Responses upstream path %q, got %q", "/responses/proxy-key/v1/responses", requests[0].Path)
	}
	if requests[0].Headers.Get("Authorization") != "Bearer "+route.EndpointAPIKey {
		t.Fatalf("expected Responses upstream authorization header, got %q", requests[0].Headers.Get("Authorization"))
	}
	if got := requestModelID(t, requests[0].Body); got != route.TargetModelID {
		t.Fatalf("expected Responses upstream body model %q, got %q", route.TargetModelID, got)
	}
	waitForProxyAPIKeyUsageMaterialization(t, harness.conn, 5*time.Second)
}

func TestRuntimeShutdownDrainsDurableRuntimeSignals(t *testing.T) {
	shutdownTimeout := time.Second
	closeResults := make(chan runtimeapi.TelemetryOutboxCloseResult, 1)
	gate := newRuntimeTelemetryMaterializeGate()
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
			WorkerCount:     1,
			PollInterval:    25 * time.Millisecond,
			ShutdownTimeout: shutdownTimeout,
			WakeupBuffer:    1,
			Hooks: &runtimeapi.TelemetryOutboxHooks{
				BeforeMaterialize: gate.Wait,
				AfterClose: func(result runtimeapi.TelemetryOutboxCloseResult) {
					closeResults <- result
				},
			},
		}},
	})
	profileID := harness.activeProfileID(t)
	proxyAPIKey := harness.enableRuntimeProxyAPIKeyAuth(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "telemetry-drain-public-" + randomSuffix(),
		TargetModelID:   "telemetry-drain-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/telemetry/drain"),
		EndpointAPIKey:  "telemetry-drain-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "drain durable runtime telemetry and auth usage on shutdown"}},
		"model":    route.PublicModelID,
	}, map[string]string{"Authorization": "Bearer " + proxyAPIKey})
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 0, UsageEvents: 0, OutboxRows: 1}, 5*time.Second)

	closeElapsed := make(chan time.Duration, 1)
	go func() {
		startedAt := time.Now()
		harness.runtimeService.Close()
		closeElapsed <- time.Since(startedAt)
	}()
	assertRuntimeClosePending(t, closeElapsed, 150*time.Millisecond)
	gate.Release()

	select {
	case elapsed := <-closeElapsed:
		if elapsed > shutdownTimeout {
			t.Fatalf("expected runtime close to finish within configured shutdown timeout %s, got %s", shutdownTimeout, elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for durable runtime close to drain pending telemetry")
	}
	closeResult := <-closeResults
	if !closeResult.Drained || closeResult.TimedOut || closeResult.PendingRows != 0 || closeResult.Inflight != 0 {
		t.Fatalf("expected shutdown drain to fully flush durable runtime signals, got %+v", closeResult)
	}
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	waitForProxyAPIKeyUsageMaterialization(t, harness.conn, 5*time.Second)
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
}

func TestRuntimeTelemetryOutboxBackpressure(t *testing.T) {
	gate := newRuntimeTelemetryMaterializeGate()
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		SettingsMutator: useDurableRuntimeTelemetryMode,
		RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
			WorkerCount:     1,
			PollInterval:    25 * time.Millisecond,
			ShutdownTimeout: time.Second,
			WakeupBuffer:    1,
			Hooks: &runtimeapi.TelemetryOutboxHooks{
				BeforeMaterialize: gate.Wait,
			},
		}},
	})
	profileID := harness.activeProfileID(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "telemetry-pressure-public-" + randomSuffix(),
		TargetModelID:   "telemetry-pressure-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/telemetry/backpressure"),
		EndpointAPIKey:  "telemetry-pressure-key",
	})

	results := make([]<-chan concurrentRuntimeRequestResult, 0, runtimeTelemetryOutboxBackpressureRequests)
	for i := 0; i < runtimeTelemetryOutboxBackpressureRequests; i++ {
		results = append(results, startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/chat/completions", map[string]any{
			"messages": []map[string]any{{"role": "user", "content": fmt.Sprintf("telemetry pressure request %d", i)}},
			"model":    route.PublicModelID,
		}, nil))
	}
	for _, resultCh := range results {
		result := awaitAsyncRequest(t, resultCh, 5*time.Second)
		if result.Err != nil {
			t.Fatalf("expected durable outbox pressure request to complete without worker unblock, got error: %v", result.Err)
		}
		if result.StatusCode != http.StatusOK {
			t.Fatalf("expected durable outbox pressure request status 200, got %d with body %s", result.StatusCode, result.Body)
		}
	}
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 0, UsageEvents: 0, OutboxRows: runtimeTelemetryOutboxBackpressureRequests}, 5*time.Second)
	if got := len(harness.upstream.requestsSnapshot()); got != runtimeTelemetryOutboxBackpressureRequests {
		t.Fatalf("expected all durable outbox pressure requests to reach upstream without deadlock, got %d", got)
	}

	gate.Release()
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: runtimeTelemetryOutboxBackpressureRequests, UsageEvents: runtimeTelemetryOutboxBackpressureRequests, OutboxRows: 0}, 5*time.Second)
}

func TestRuntimeTelemetryBacklogDoesNotConsumeExecutionPool(t *testing.T) {
	gate := newRuntimeTelemetryMaterializeGate()
	harness := newRuntimePhase0HarnessWithOptions(t, runtimePhase0HarnessOptions{
		SettingsMutator: useDurableRuntimeTelemetryMode,
		RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
			WorkerCount:     1,
			PollInterval:    25 * time.Millisecond,
			ShutdownTimeout: time.Second,
			WakeupBuffer:    1,
			Hooks: &runtimeapi.TelemetryOutboxHooks{
				BeforeMaterialize: gate.Wait,
			},
		}},
	})
	profileID := harness.activeProfileID(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "telemetry-isolation-public-" + randomSuffix(),
		TargetModelID:   "telemetry-isolation-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/telemetry/isolation"),
		EndpointAPIKey:  "telemetry-isolation-key",
	})

	warmResponse := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "seed one durable telemetry backlog row"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, warmResponse, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 0, UsageEvents: 0, OutboxRows: 1}, 5*time.Second)

	response, snapshot := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "execution lane must stay free while telemetry replay backlogs"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	assertPhase3HotPathExcludesForbiddenSQL(t, snapshot)
	assertExecutionLaneExcludesTelemetryOutboxSQL(t, snapshot)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 0, UsageEvents: 0, OutboxRows: 2}, 5*time.Second)

	gate.Release()
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 2, OutboxRows: 0}, 5*time.Second)
}

func TestRuntimeAcceptedResponseDoesNotMaterializeSyncTelemetry(t *testing.T) {
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
			Hooks: &runtimeapi.TelemetryOutboxHooks{
				EnqueueError: func() error {
					return errors.New("forced durable telemetry enqueue failure")
				},
			},
		}},
	})
	profileID := harness.activeProfileID(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "telemetry-no-sync-public-" + randomSuffix(),
		TargetModelID:   "telemetry-no-sync-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/telemetry/no-sync"),
		EndpointAPIKey:  "telemetry-no-sync-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "accepted responses must not fall back to inline sync telemetry materialization"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	time.Sleep(100 * time.Millisecond)
	counts := loadRuntimeTelemetryCounts(t, harness.conn, profileID)
	if counts != (runtimeTelemetryCounts{}) {
		t.Fatalf("expected accepted response to avoid inline sync telemetry materialization after enqueue failure, got %+v", counts)
	}
}

func BenchmarkRuntimeDurableTelemetryHotPath(b *testing.B) {
	benchmarkRuntimeDurableTelemetryHotPath(b)
}

func BenchmarkRuntimeTelemetryHotPath(b *testing.B) {
	benchmarkRuntimeDurableTelemetryHotPath(b)
}

func benchmarkRuntimeDurableTelemetryHotPath(b *testing.B) {
	harness := newRuntimeHarnessWithConfig(b, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
			ShutdownTimeout: 100 * time.Millisecond,
		}},
	})
	profileID := harness.activeProfileID(b)
	upstream := newRuntimeBenchmarkUpstream(b, http.StatusOK, runtimeBenchmarkHotPathResponse())
	route := harness.seedProxyRoute(b, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "benchmark-telemetry-hot-path-public-" + randomSuffix(),
		TargetModelID:   "benchmark-telemetry-hot-path-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/benchmark/telemetry-hot-path"),
		EndpointAPIKey:  "benchmark-telemetry-hot-path-key",
	})
	rawBody := runtimeBenchmarkRequestBody(b, route.PublicModelID, "phase-4 durable telemetry hot path benchmark")

	statusCode, _, err := performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
	if err != nil {
		b.Fatalf("warm runtime telemetry hot-path benchmark request: %v", err)
	}
	if statusCode != http.StatusOK {
		b.Fatalf("expected warm runtime telemetry hot-path status 200, got %d", statusCode)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		statusCode, _, err = performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
		if err != nil {
			b.Fatalf("run runtime telemetry hot-path benchmark request: %v", err)
		}
		if statusCode != http.StatusOK {
			b.Fatalf("expected runtime telemetry hot-path status 200, got %d", statusCode)
		}
	}
}

func BenchmarkRuntimeAuthUsageAsyncHotPath(b *testing.B) {
	harness := newRuntimeHarnessWithConfig(b, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
			ShutdownTimeout: 100 * time.Millisecond,
		}},
	})
	profileID := harness.activeProfileID(b)
	proxyAPIKey := harness.enableRuntimeProxyAPIKeyAuth(b)
	upstream := newRuntimeBenchmarkUpstream(b, http.StatusOK, runtimeBenchmarkHotPathResponse())
	route := harness.seedProxyRoute(b, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "benchmark-auth-usage-public-" + randomSuffix(),
		TargetModelID:   "benchmark-auth-usage-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/benchmark/auth-usage"),
		EndpointAPIKey:  "benchmark-auth-usage-key",
	})
	rawBody := runtimeBenchmarkRequestBody(b, route.PublicModelID, "phase-4 auth usage async hot path benchmark")
	headers := map[string]string{"Authorization": "Bearer " + proxyAPIKey}

	statusCode, _, err := performRuntimeBenchmarkRequestWithHeaders(harness.client, harness.url+"/v1/chat/completions", rawBody, headers)
	if err != nil {
		b.Fatalf("warm runtime auth usage async benchmark request: %v", err)
	}
	if statusCode != http.StatusOK {
		b.Fatalf("expected warm runtime auth usage async benchmark status 200, got %d", statusCode)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		statusCode, _, err = performRuntimeBenchmarkRequestWithHeaders(harness.client, harness.url+"/v1/chat/completions", rawBody, headers)
		if err != nil {
			b.Fatalf("run runtime auth usage async benchmark request: %v", err)
		}
		if statusCode != http.StatusOK {
			b.Fatalf("expected runtime auth usage async benchmark status 200, got %d", statusCode)
		}
	}
}

type runtimeTelemetryCounts struct {
	RequestLogs int
	UsageEvents int
	OutboxRows  int
}

type runtimeTelemetryMaterializeGate struct {
	release chan struct{}
}

func newRuntimeTelemetryMaterializeGate() *runtimeTelemetryMaterializeGate {
	return &runtimeTelemetryMaterializeGate{release: make(chan struct{})}
}

func (g *runtimeTelemetryMaterializeGate) Wait(ctx context.Context) error {
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *runtimeTelemetryMaterializeGate) Release() {
	select {
	case <-g.release:
	default:
		close(g.release)
	}
}

func useDurableRuntimeTelemetryMode(settings *config.Settings) {
	settings.RuntimeTelemetryMode = config.RuntimeTelemetryModeDurableOutbox
}

func performRuntimeBenchmarkRequestWithHeaders(client *http.Client, url string, rawBody []byte, headers map[string]string) (int, int, error) {
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(rawBody))
	if err != nil {
		return 0, 0, fmt.Errorf("build benchmark request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, 0, fmt.Errorf("perform benchmark request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return response.StatusCode, 0, fmt.Errorf("read benchmark response body: %w", err)
	}
	return response.StatusCode, len(responseBody), nil
}

func assertRuntimeClosePending(t *testing.T, closeResult <-chan time.Duration, timeout time.Duration) {
	t.Helper()
	select {
	case elapsed := <-closeResult:
		t.Fatalf("expected runtime close to still be draining pending telemetry, completed in %s", elapsed)
	case <-time.After(timeout):
	}
}

func loadProxyAPIKeyUsageMaterializationCount(t *testing.T, conn *pgx.Conn) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var usedCount int
	if err := conn.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM proxy_api_keys WHERE last_used_at IS NOT NULL AND COALESCE(last_used_ip, '') <> ''`,
	).Scan(&usedCount); err != nil {
		t.Fatalf("load proxy api key usage materialization count: %v", err)
	}
	return usedCount
}

func waitForRuntimeTelemetryCounts(t *testing.T, conn *pgx.Conn, profileID int, want runtimeTelemetryCounts, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := loadRuntimeTelemetryCounts(t, conn, profileID)
	for time.Now().Before(deadline) {
		last = loadRuntimeTelemetryCounts(t, conn, profileID)
		if last == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for runtime telemetry counts %+v, last %+v", want, last)
}

func loadRuntimeTelemetryCounts(t *testing.T, conn *pgx.Conn, profileID int) runtimeTelemetryCounts {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var counts runtimeTelemetryCounts
	if err := conn.QueryRow(
		ctx,
		`SELECT
			(SELECT COUNT(*) FROM request_logs WHERE profile_id = $1),
			(SELECT COUNT(*) FROM usage_request_events WHERE profile_id = $1),
			(SELECT COUNT(*) FROM runtime_telemetry_outbox WHERE profile_id = $1)`,
		profileID,
	).Scan(&counts.RequestLogs, &counts.UsageEvents, &counts.OutboxRows); err != nil {
		t.Fatalf("load runtime telemetry counts for profile %d: %v", profileID, err)
	}
	return counts
}
