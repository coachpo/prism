package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

const runtimeTelemetryOutboxBackpressureRequests = 8

func TestRuntimeTelemetryOutboxDurability(t *testing.T) {
	shutdownTimeout := 150 * time.Millisecond
	gate := newRuntimeTelemetryMaterializeGate()
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		SettingsMutator: useDurableRuntimeTelemetryMode,
		RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
			WorkerCount:     1,
			PollInterval:    25 * time.Millisecond,
			ShutdownTimeout: shutdownTimeout,
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
		PublicModelID:   "telemetry-durable-public-" + randomSuffix(),
		TargetModelID:   "telemetry-durable-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/telemetry/durable"),
		EndpointAPIKey:  "telemetry-durable-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "persist telemetry before worker drain"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 0, UsageEvents: 0, OutboxRows: 1}, 5*time.Second)

	startedAt := time.Now()
	harness.runtimeService.Close()
	if elapsed := time.Since(startedAt); elapsed < shutdownTimeout || elapsed > time.Second {
		t.Fatalf("expected durable runtime close to respect the configured timeout around %s, got %s", shutdownTimeout, elapsed)
	}
	countsAfterClose := loadRuntimeTelemetryCounts(t, harness.conn, profileID)
	if countsAfterClose.RequestLogs != 0 || countsAfterClose.UsageEvents != 0 || countsAfterClose.OutboxRows != 1 {
		t.Fatalf("expected shutdown-timeout close to leave one durable outbox row for restart replay, got %+v", countsAfterClose)
	}

	restartedHarness := restartRuntimeHarnessWithConfig(t, harness.databaseName, runtimeHarnessConfig{SettingsMutator: useDurableRuntimeTelemetryMode})
	waitForRuntimeTelemetryCounts(t, restartedHarness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeAttemptCounts(t, restartedHarness.conn, profileID, 1, 1)
}

func TestRuntimeTelemetryOutboxDrainOnShutdown(t *testing.T) {
	shutdownTimeout := time.Second
	gate := newRuntimeTelemetryMaterializeGate()
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		SettingsMutator: useDurableRuntimeTelemetryMode,
		RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
			WorkerCount:     1,
			PollInterval:    25 * time.Millisecond,
			ShutdownTimeout: shutdownTimeout,
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
		PublicModelID:   "telemetry-drain-public-" + randomSuffix(),
		TargetModelID:   "telemetry-drain-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/telemetry/drain"),
		EndpointAPIKey:  "telemetry-drain-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "drain runtime telemetry on shutdown"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 0, UsageEvents: 0, OutboxRows: 1}, 5*time.Second)

	closeResult := make(chan time.Duration, 1)
	go func() {
		startedAt := time.Now()
		harness.runtimeService.Close()
		closeResult <- time.Since(startedAt)
	}()
	assertRuntimeClosePending(t, closeResult, 150*time.Millisecond)
	gate.Release()

	select {
	case elapsed := <-closeResult:
		if elapsed > shutdownTimeout {
			t.Fatalf("expected runtime close to finish within configured shutdown timeout %s, got %s", shutdownTimeout, elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for durable runtime close to drain pending telemetry")
	}
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
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

func TestRuntimeTelemetryOutboxFallback(t *testing.T) {
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		SettingsMutator: useDurableRuntimeTelemetryMode,
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
		PublicModelID:   "telemetry-fallback-public-" + randomSuffix(),
		TargetModelID:   "telemetry-fallback-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/telemetry/fallback"),
		EndpointAPIKey:  "telemetry-fallback-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "fallback to synchronous telemetry materialization"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
}

func BenchmarkRuntimeTelemetryHotPath(b *testing.B) {
	harness := newRuntimeHarnessWithConfig(b, runtimeHarnessConfig{
		SettingsMutator: useDurableRuntimeTelemetryMode,
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
	rawBody := runtimeBenchmarkRequestBody(b, route.PublicModelID, "phase-1 telemetry hot path benchmark")

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

func assertRuntimeClosePending(t *testing.T, closeResult <-chan time.Duration, timeout time.Duration) {
	t.Helper()
	select {
	case elapsed := <-closeResult:
		t.Fatalf("expected runtime close to still be draining pending telemetry, completed in %s", elapsed)
	case <-time.After(timeout):
	}
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
