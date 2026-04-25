package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

const runtimePhase0AdmissionContentionConcurrency = 8

func BenchmarkRuntimeHotPathBaseline(b *testing.B) {
	harness := newRuntimePhase0HarnessWithOptions(b, runtimePhase0HarnessOptions{SettingsMutator: useBenchmarkRuntimeTransportOverrides})
	profileID := harness.activeProfileID(b)
	upstream := newRuntimeBenchmarkUpstream(b, http.StatusOK, runtimeBenchmarkHotPathResponse())
	route := harness.seedProxyRoute(b, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "benchmark-phase0-hot-path-public-" + randomSuffix(),
		TargetModelID:   "benchmark-phase0-hot-path-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/benchmark/phase0/hot-path"),
		EndpointAPIKey:  "benchmark-phase0-hot-path-key",
	})
	rawBody := runtimeBenchmarkRequestBody(b, route.PublicModelID, "phase-0 hot path baseline")

	statusCode, _, err := performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
	if err != nil {
		b.Fatalf("warm runtime phase-0 hot-path benchmark request: %v", err)
	}
	if statusCode != http.StatusOK {
		b.Fatalf("expected warm runtime phase-0 hot-path status 200, got %d", statusCode)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		statusCode, _, err = performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
		if err != nil {
			b.Fatalf("run runtime phase-0 hot-path benchmark request: %v", err)
		}
		if statusCode != http.StatusOK {
			b.Fatalf("expected runtime phase-0 hot-path status 200, got %d", statusCode)
		}
	}
}

func BenchmarkRuntimeAdmissionContentionBaseline(b *testing.B) {
	harness := newRuntimePhase0HarnessWithOptions(b, runtimePhase0HarnessOptions{SettingsMutator: useBenchmarkRuntimeTransportOverrides})
	profileID := harness.activeProfileID(b)
	upstream := newRuntimeBenchmarkUpstream(b, http.StatusOK, runtimeBenchmarkHotPathResponse())
	route := harness.seedProxyRoute(b, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "benchmark-phase0-contention-public-" + randomSuffix(),
		TargetModelID:   "benchmark-phase0-contention-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/benchmark/phase0/admission-contention"),
		EndpointAPIKey:  "benchmark-phase0-contention-key",
	})
	rawBody := runtimeBenchmarkRequestBody(b, route.PublicModelID, "phase-0 admission contention baseline")

	statusCode, _, err := performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
	if err != nil {
		b.Fatalf("warm runtime phase-0 admission contention benchmark request: %v", err)
	}
	if statusCode != http.StatusOK {
		b.Fatalf("expected warm runtime phase-0 admission contention status 200, got %d", statusCode)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := runRuntimeBenchmarkStorm(harness.client, harness.url+"/v1/chat/completions", rawBody, runtimePhase0AdmissionContentionConcurrency); err != nil {
			b.Fatalf("run runtime phase-0 admission contention benchmark: %v", err)
		}
	}
}

func BenchmarkRuntimeRoundRobinBaseline(b *testing.B) {
	harness := newRuntimePhase0HarnessWithOptions(b, runtimePhase0HarnessOptions{SettingsMutator: useBenchmarkRuntimeTransportOverrides})
	profileID := harness.activeProfileID(b)
	suffix := randomSuffix()

	strategyID := harness.seedLegacyStrategy(b, profileID, "benchmark-phase0-round-robin-"+suffix, "round-robin")
	publicModelID := "benchmark-phase0-round-robin-public-" + suffix
	targetModelID := "benchmark-phase0-round-robin-target-" + suffix
	targetModelConfigID := harness.seedModel(b, profileID, "gemini", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(b, profileID, "gemini", publicModelID, "proxy", nil)
	harness.seedProxyTarget(b, publicModelConfigID, targetModelConfigID)

	firstEndpointID := harness.seedEndpoint(b, profileID, "benchmark-phase0-round-robin-first-"+suffix, harness.upstream.baseURL("/benchmark/phase0/round-robin/first"), "benchmark-phase0-round-robin-first-key", 0)
	secondEndpointID := harness.seedEndpoint(b, profileID, "benchmark-phase0-round-robin-second-"+suffix, harness.upstream.baseURL("/benchmark/phase0/round-robin/second"), "benchmark-phase0-round-robin-second-key", 1)
	harness.seedConnection(b, profileID, targetModelConfigID, firstEndpointID, "benchmark-phase0-round-robin-connection-a-"+suffix, nil, nil, 0)
	harness.seedConnection(b, profileID, targetModelConfigID, secondEndpointID, "benchmark-phase0-round-robin-connection-b-"+suffix, nil, nil, 1)

	requestURL := fmt.Sprintf("%s/v1beta/models/%s:generateContent", harness.url, publicModelID)
	rawBody := runtimePhase0GeminiBenchmarkRequestBody(b, "phase-0 round-robin baseline")

	statusCode, _, err := performRuntimeBenchmarkRequest(harness.client, requestURL, rawBody)
	if err != nil {
		b.Fatalf("warm runtime phase-0 round-robin benchmark request: %v", err)
	}
	if statusCode != http.StatusOK {
		b.Fatalf("expected warm runtime phase-0 round-robin status 200, got %d", statusCode)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		statusCode, _, err = performRuntimeBenchmarkRequest(harness.client, requestURL, rawBody)
		if err != nil {
			b.Fatalf("run runtime phase-0 round-robin benchmark request: %v", err)
		}
		if statusCode != http.StatusOK {
			b.Fatalf("expected runtime phase-0 round-robin status 200, got %d", statusCode)
		}
	}
}

func BenchmarkRuntimeVsManagementLoad(b *testing.B) {
	harness := newRuntimePhase0HarnessWithOptions(b, runtimePhase0HarnessOptions{
		IncludeStatsService: true,
		SettingsMutator: func(settings *config.Settings) {
			useBenchmarkRuntimeTransportOverrides(settings)
			useDurableRuntimeTelemetryMode(settings)
			usePhase0ManagementIsolationSettings(settings)
			settings.RuntimeDatabasePoolBudget = config.DatabasePoolBudget{MaxConns: 2, MinIdleConns: 0}
		},
	})
	profileID := harness.activeProfileID(b)
	route := harness.seedProxyRoute(b, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "benchmark-phase0-management-load-public-" + randomSuffix(),
		TargetModelID:   "benchmark-phase0-management-load-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/benchmark/phase0/management-load"),
		EndpointAPIKey:  "benchmark-phase0-management-load-key",
	})
	rawBody := runtimeBenchmarkRequestBody(b, route.PublicModelID, "phase-0 runtime vs management load")

	statusCode, _, err := performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
	if err != nil {
		b.Fatalf("warm runtime phase-0 runtime-vs-management benchmark request: %v", err)
	}
	if statusCode != http.StatusOK {
		b.Fatalf("expected warm runtime phase-0 runtime-vs-management status 200, got %d", statusCode)
	}

	statsLock := holdRuntimeStatsTablesLockTB(b, harness.databaseName)
	defer statsLock.release(b)

	pressureResult := startAsyncBenchmarkPriorityRequest(b, harness.client, http.MethodGet, harness.url+"/api/stats/summary", nil, runtimeModelHeader(profileID))
	waitForStatsLockWaitersTB(b, harness.conn, 1, 5*time.Second)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		statusCode, _, err = performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
		if err != nil {
			b.Fatalf("run runtime phase-0 runtime-vs-management benchmark request: %v", err)
		}
		if statusCode != http.StatusOK {
			b.Fatalf("expected runtime phase-0 runtime-vs-management status 200, got %d", statusCode)
		}
	}
	b.StopTimer()

	statsLock.release(b)
	assertAsyncBenchmarkRequestStatus(b, pressureResult, http.StatusOK)
}

func runtimePhase0GeminiBenchmarkRequestBody(tb testing.TB, prompt string) []byte {
	tb.Helper()
	return mustMarshalBenchmarkJSON(tb, map[string]any{
		"contents": []map[string]any{{
			"role":  "user",
			"parts": []map[string]any{{"text": prompt}},
		}},
	})
}

type benchmarkAsyncRequestResult struct {
	StatusCode int
	Body       string
	Err        error
}

type benchmarkRuntimeStatsTablesLock struct {
	conn *pgx.Conn
	tx   pgx.Tx
	once sync.Once
}

func startAsyncBenchmarkPriorityRequest(tb testing.TB, client *http.Client, method string, url string, body any, headers map[string]string) <-chan benchmarkAsyncRequestResult {
	tb.Helper()
	var rawBody []byte
	if body != nil {
		encodedBody, err := json.Marshal(body)
		if err != nil {
			tb.Fatalf("marshal benchmark async request body for %s %s: %v", method, url, err)
		}
		rawBody = encodedBody
	}

	resultCh := make(chan benchmarkAsyncRequestResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		var requestBody io.Reader
		if rawBody != nil {
			requestBody = bytes.NewReader(rawBody)
		}
		request, err := http.NewRequestWithContext(ctx, method, url, requestBody)
		if err != nil {
			resultCh <- benchmarkAsyncRequestResult{Err: fmt.Errorf("build benchmark request %s %s: %w", method, url, err)}
			return
		}
		if rawBody != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		response, err := client.Do(request)
		if err != nil {
			resultCh <- benchmarkAsyncRequestResult{Err: fmt.Errorf("perform benchmark request %s %s: %w", method, url, err)}
			return
		}
		defer func() { _ = response.Body.Close() }()
		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			resultCh <- benchmarkAsyncRequestResult{Err: fmt.Errorf("read benchmark response body for %s %s: %w", method, url, err)}
			return
		}
		resultCh <- benchmarkAsyncRequestResult{StatusCode: response.StatusCode, Body: string(bytes.TrimSpace(responseBody))}
	}()
	return resultCh
}

func assertAsyncBenchmarkRequestStatus(tb testing.TB, resultCh <-chan benchmarkAsyncRequestResult, wantStatus int) {
	tb.Helper()
	result := awaitAsyncBenchmarkRequest(tb, resultCh, 5*time.Second)
	if result.Err != nil {
		tb.Fatalf("expected benchmark async request to succeed, got error: %v", result.Err)
	}
	if result.StatusCode != wantStatus {
		tb.Fatalf("expected benchmark async request status %d, got %d with body %s", wantStatus, result.StatusCode, result.Body)
	}
}

func awaitAsyncBenchmarkRequest(tb testing.TB, resultCh <-chan benchmarkAsyncRequestResult, timeout time.Duration) benchmarkAsyncRequestResult {
	tb.Helper()
	select {
	case result := <-resultCh:
		return result
	case <-time.After(timeout):
		tb.Fatalf("timed out waiting for benchmark async request result after %s", timeout)
		return benchmarkAsyncRequestResult{}
	}
}

func holdRuntimeStatsTablesLockTB(tb testing.TB, databaseName string) *benchmarkRuntimeStatsTablesLock {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := connectDatabase(tb, ctx, sharedPostgresHarness.connectionString(databaseName))
	tx, err := conn.Begin(ctx)
	if err != nil {
		_ = conn.Close(ctx)
		tb.Fatalf("begin benchmark stats lock transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE request_logs IN ACCESS EXCLUSIVE MODE`); err != nil {
		_ = tx.Rollback(ctx)
		_ = conn.Close(ctx)
		tb.Fatalf("lock benchmark request_logs table: %v", err)
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE usage_request_events IN ACCESS EXCLUSIVE MODE`); err != nil {
		_ = tx.Rollback(ctx)
		_ = conn.Close(ctx)
		tb.Fatalf("lock benchmark usage_request_events table: %v", err)
	}
	return &benchmarkRuntimeStatsTablesLock{conn: conn, tx: tx}
}

func (lock *benchmarkRuntimeStatsTablesLock) release(tb testing.TB) {
	tb.Helper()
	if lock == nil {
		return
	}
	lock.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = lock.tx.Rollback(ctx)
		_ = lock.conn.Close(ctx)
	})
}

func waitForStatsLockWaitersTB(tb testing.TB, conn *pgx.Conn, want int, timeout time.Duration) {
	tb.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var waiters int
		if err := conn.QueryRow(
			context.Background(),
			`SELECT COUNT(*)
            FROM pg_stat_activity
            WHERE pid <> pg_backend_pid()
              AND datname = current_database()
              AND usename = current_user
              AND state = 'active'
              AND wait_event_type = 'Lock'`,
		).Scan(&waiters); err != nil {
			tb.Fatalf("count benchmark blocked stats waiters: %v", err)
		}
		if waiters >= want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	tb.Fatalf("timed out waiting for %d blocked benchmark stats requests", want)
}
