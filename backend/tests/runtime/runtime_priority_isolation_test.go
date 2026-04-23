package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementprofiles "github.com/coachpo/prism/backend/internal/httpapi/management/profiles"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

type runtimeStatsHarness struct {
	*runtimeHarness
	statsService *managementstats.Service
	settings     config.Settings
}

type runtimeStatsHarnessOptions struct {
	runtimeDatabasePoolBudget        config.DatabasePoolBudget
	managementDatabasePoolBudget     config.DatabasePoolBudget
	managementAdmissionControlBudget config.ManagementAdmissionBudget
}

type runtimeStatsTablesLock struct {
	conn *pgx.Conn
	tx   pgx.Tx
	once sync.Once
}

const managementM3FastFailDeadline = 500 * time.Millisecond

func TestRuntimePriorityIsolation_NonStream(t *testing.T) {
	harness := newRuntimeStatsHarnessWithOptions(t, runtimeStatsHarnessOptions{
		runtimeDatabasePoolBudget:        config.DatabasePoolBudget{MaxConns: 1, MinIdleConns: 0},
		managementDatabasePoolBudget:     config.DatabasePoolBudget{MaxConns: 1, MinIdleConns: 0},
		managementAdmissionControlBudget: config.ManagementAdmissionBudget{M2MaxConcurrent: 1, M3MaxConcurrent: 1},
	})
	profileID := harness.activeProfileID(t)
	harness.seedStatsPressureHistory(t, profileID, "priority-non-stream")

	statsLock := holdRuntimeStatsTablesLock(t, harness.databaseName)
	defer statsLock.release(t)

	pressureResults := []<-chan concurrentRuntimeRequestResult{
		startAsyncPriorityRequest(t, harness.client, http.MethodGet, harness.url+"/api/stats/requests?limit=50&offset=0", nil, runtimeModelHeader(profileID)),
		startAsyncPriorityRequest(t, harness.client, http.MethodGet, harness.url+"/api/stats/summary", nil, runtimeModelHeader(profileID)),
		startAsyncPriorityRequest(t, harness.client, http.MethodGet, harness.url+"/api/stats/throughput", nil, runtimeModelHeader(profileID)),
	}
	expectedPendingPressure := expectedPendingFirstShedRequests(len(pressureResults), harness.settings)
	waitForStatsLockWaiters(t, harness.conn, expectedPendingPressure, 5*time.Second)

	blockingUpstream := newBlockingScriptedUpstream(t, 1, http.StatusOK, map[string]any{"id": "chatcmpl-priority-non-stream"})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "priority-public-" + randomSuffix(),
		TargetModelID:   "priority-target-" + randomSuffix(),
		EndpointBaseURL: blockingUpstream.baseURL("/priority/non-stream"),
		EndpointAPIKey:  "priority-non-stream-key",
	})

	runtimeResultCh := startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "non-stream priority isolation"}},
		"model":    route.PublicModelID,
	}, nil)

	blockingUpstream.waitUntilReady(t, 5*time.Second)
	pendingPressureResults := assertAsyncRequestsPendingOrRejected(t, pressureResults, expectedPendingPressure, len(pressureResults)-expectedPendingPressure)

	requests := blockingUpstream.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("expected runtime request to reach the upstream once under stats pressure, got %d requests", len(requests))
	}
	if requests[0].Path != "/priority/non-stream/v1/chat/completions" {
		t.Fatalf("expected runtime request path %q, got %q", "/priority/non-stream/v1/chat/completions", requests[0].Path)
	}
	if got := requestModelID(t, requests[0].Body); got != route.TargetModelID {
		t.Fatalf("expected runtime request model %q, got %q", route.TargetModelID, got)
	}

	statsLock.release(t)
	assertAsyncRequestsStatus(t, pendingPressureResults, http.StatusOK)

	blockingUpstream.releaseRequests()
	runtimeResult := awaitAsyncRequest(t, runtimeResultCh, 5*time.Second)
	if runtimeResult.Err != nil {
		t.Fatalf("expected non-stream runtime request to succeed under mixed load, got error: %v", runtimeResult.Err)
	}
	if runtimeResult.StatusCode != http.StatusOK {
		t.Fatalf("expected non-stream runtime request status 200, got %d with body %s", runtimeResult.StatusCode, runtimeResult.Body)
	}

	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
}

func TestManagementOverload_FastFailsM3WhileKeepingM1Reachable(t *testing.T) {
	harness := newRuntimeStatsHarnessWithOptions(t, runtimeStatsHarnessOptions{
		runtimeDatabasePoolBudget:        config.DatabasePoolBudget{MaxConns: 1, MinIdleConns: 0},
		managementDatabasePoolBudget:     config.DatabasePoolBudget{MaxConns: 3, MinIdleConns: 0},
		managementAdmissionControlBudget: config.ManagementAdmissionBudget{M2MaxConcurrent: 2, M3MaxConcurrent: 2},
	})
	profileID := harness.activeProfileID(t)
	harness.seedStatsPressureHistory(t, profileID, "priority-m1-m3")

	statsLock := holdRuntimeStatsTablesLock(t, harness.databaseName)
	defer statsLock.release(t)

	pressureResults := []<-chan concurrentRuntimeRequestResult{
		startAsyncPriorityRequest(t, harness.client, http.MethodGet, harness.url+"/api/stats/requests?limit=50&offset=0", nil, runtimeModelHeader(profileID)),
		startAsyncPriorityRequest(t, harness.client, http.MethodGet, harness.url+"/api/stats/summary", nil, runtimeModelHeader(profileID)),
	}
	expectedPendingPressure := expectedPendingFirstShedRequests(len(pressureResults), harness.settings)
	waitForStatsLockWaiters(t, harness.conn, expectedPendingPressure, 5*time.Second)
	pendingPressureResults := assertAsyncRequestsPendingOrRejected(t, pressureResults, expectedPendingPressure, 0)

	blockingUpstream := newBlockingScriptedUpstream(t, 1, http.StatusOK, map[string]any{"id": "chatcmpl-priority-m1-m3"})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "priority-m1-m3-public-" + randomSuffix(),
		TargetModelID:   "priority-m1-m3-target-" + randomSuffix(),
		EndpointBaseURL: blockingUpstream.baseURL("/priority/m1-m3"),
		EndpointAPIKey:  "priority-m1-m3-key",
	})

	runtimeResultCh := startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "runtime should stay isolated while management overload sheds M3"}},
		"model":    route.PublicModelID,
	}, nil)

	blockingUpstream.waitUntilReady(t, 5*time.Second)
	requests := blockingUpstream.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("expected runtime request to reach the upstream once while management overload was active, got %d requests", len(requests))
	}
	if requests[0].Path != "/priority/m1-m3/v1/chat/completions" {
		t.Fatalf("expected runtime request path %q, got %q", "/priority/m1-m3/v1/chat/completions", requests[0].Path)
	}
	if got := requestModelID(t, requests[0].Body); got != route.TargetModelID {
		t.Fatalf("expected runtime request model %q, got %q", route.TargetModelID, got)
	}

	overloadStartedAt := time.Now()
	overloadResponse := performPriorityRequest(t, harness.client, time.Second, http.MethodGet, harness.url+"/api/stats/throughput", nil, runtimeModelHeader(profileID))
	if elapsed := time.Since(overloadStartedAt); elapsed > managementM3FastFailDeadline {
		t.Fatalf("expected saturated M3 route to fast-fail within %s, got %s", managementM3FastFailDeadline, elapsed)
	}
	assertManagementOverloadResponse(t, overloadResponse)

	protectedResponse := performPriorityRequest(t, harness.client, 2*time.Second, http.MethodGet, harness.url+"/api/profiles/active", nil, nil)
	assertStatus(t, protectedResponse, http.StatusOK)
	var protectedPayload map[string]any
	decodeJSONResponse(t, protectedResponse, &protectedPayload)
	if got := jsonInt(t, protectedPayload["id"]); got != profileID {
		t.Fatalf("expected protected M1 route to keep profile id %d during overload, got %+v", profileID, protectedPayload)
	}

	assertAsyncRequestsPending(t, pendingPressureResults)

	statsLock.release(t)
	assertAsyncRequestsStatus(t, pendingPressureResults, http.StatusOK)

	blockingUpstream.releaseRequests()
	runtimeResult := awaitAsyncRequest(t, runtimeResultCh, 5*time.Second)
	if runtimeResult.Err != nil {
		t.Fatalf("expected runtime request to succeed while management overload shed M3, got error: %v", runtimeResult.Err)
	}
	if runtimeResult.StatusCode != http.StatusOK {
		t.Fatalf("expected runtime request status 200, got %d with body %s", runtimeResult.StatusCode, runtimeResult.Body)
	}

	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
}

func newRuntimeStatsHarness(t *testing.T) *runtimeStatsHarness {
	t.Helper()
	return newRuntimeStatsHarnessWithOptions(t, runtimeStatsHarnessOptions{})
}

func newRuntimeStatsHarnessWithOptions(t *testing.T, options runtimeStatsHarnessOptions) *runtimeStatsHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	databaseName := "runtime_priority_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	startupService, err := startup.New(startup.Options{DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "runtime-priority-secret"})
	if err != nil {
		t.Fatalf("build runtime priority startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run runtime priority startup service: %v", err)
	}

	upstream := newUpstreamRecorder(t)
	settings := config.Settings{
		Host:                             "127.0.0.1",
		Port:                             8000,
		AppEnv:                           config.EnvironmentProduction,
		DatabaseURL:                      sharedPostgresHarness.connectionString(databaseName),
		RuntimeDatabasePoolBudget:        options.runtimeDatabasePoolBudget,
		ManagementDatabasePoolBudget:     options.managementDatabasePoolBudget,
		ManagementAdmissionControlBudget: options.managementAdmissionControlBudget,
		SecretEncryptionKey:              "runtime-priority-secret",
		CORSAllowedOrigins:               "http://localhost:5173,http://127.0.0.1:5173",
		AuthJWTSecret:                    "runtime-priority-jwt-secret",
		AuthAccessTokenTTLSeconds:        900,
		AuthRefreshTokenTTLSeconds:       604800,
		AuthResetCodeTTLSeconds:          600,
		AuthCookieName:                   "prism_access_token",
		AuthRefreshCookieName:            "prism_refresh_token",
		AuthCookieSecure:                 false,
	}

	managementPool := newRuntimePriorityPool(t, settings.DatabaseURL, settings.ManagementDatabaseBudget(), "management")
	t.Cleanup(managementPool.Close)
	runtimePool := newRuntimePriorityPool(t, settings.DatabaseURL, settings.RuntimeDatabaseBudget(), "runtime")
	t.Cleanup(runtimePool.Close)

	managementAuthService, err := managementauth.NewService(settings, managementauth.Options{Pool: managementPool})
	if err != nil {
		t.Fatalf("build runtime priority management auth service: %v", err)
	}
	t.Cleanup(managementAuthService.Close)
	runtimeAuthService, err := managementauth.NewService(settings, managementauth.Options{Pool: runtimePool})
	if err != nil {
		t.Fatalf("build runtime priority runtime auth service: %v", err)
	}
	t.Cleanup(runtimeAuthService.Close)
	profilesService, err := managementprofiles.NewService(settings, managementprofiles.Options{Pool: managementPool})
	if err != nil {
		t.Fatalf("build runtime priority profiles service: %v", err)
	}
	t.Cleanup(profilesService.Close)
	statsService, err := managementstats.NewService(settings, managementstats.Options{Pool: managementPool})
	if err != nil {
		t.Fatalf("build runtime priority stats service: %v", err)
	}
	t.Cleanup(statsService.Close)
	runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{Pool: runtimePool})
	if err != nil {
		t.Fatalf("build runtime priority runtime service: %v", err)
	}
	t.Cleanup(runtimeService.Close)

	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{
		Version:            "runtime-priority-test",
		AuthService:        managementAuthService,
		RuntimeAuthService: runtimeAuthService,
		ProfilesService:    profilesService,
		RuntimeService:     runtimeService,
		StatsService:       statsService,
	})
	if err != nil {
		t.Fatalf("build runtime priority handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create runtime priority cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar

	return &runtimeStatsHarness{
		runtimeHarness: &runtimeHarness{
			databaseName:    databaseName,
			client:          client,
			conn:            conn,
			authService:     managementAuthService,
			profilesService: profilesService,
			runtimeService:  runtimeService,
			server:          server,
			url:             server.URL,
			upstream:        upstream,
		},
		statsService: statsService,
		settings:     settings,
	}
}

func newRuntimePriorityPool(t *testing.T, databaseURL string, budget config.DatabasePoolBudget, lane string) *pgxpool.Pool {
	t.Helper()
	parsedConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse %s runtime priority pgx pool config: %v", lane, err)
	}
	parsedConfig.MaxConns = budget.MaxConns
	parsedConfig.MinIdleConns = budget.MinIdleConns
	pool, err := pgxpool.NewWithConfig(context.Background(), parsedConfig)
	if err != nil {
		t.Fatalf("create %s runtime priority pgx pool: %v", lane, err)
	}
	return pool
}

func (h *runtimeStatsHarness) seedStatsPressureHistory(t *testing.T, profileID int, suffix string) {
	t.Helper()
	realtimeView := &realtimeHarness{runtimeHarness: h.runtimeHarness, statsService: h.statsService, fixedNow: time.Now().UTC()}
	route := realtimeView.seedRealtimeDashboardRoute(t, profileID, suffix)
	idBase := -200000 - int(time.Now().UnixNano()%10000)
	createdAtBase := time.Now().UTC()
	for index := 0; index < 6; index++ {
		realtimeView.insertDashboardActivity(t, route, profileID, idBase-index, idBase-100-index, createdAtBase.Add(-time.Duration(index+1)*time.Minute))
	}
}

func holdRuntimeStatsTablesLock(t *testing.T, databaseName string) *runtimeStatsTablesLock {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := connectDatabase(t, ctx, sharedPostgresHarness.connectionString(databaseName))
	tx, err := conn.Begin(ctx)
	if err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("begin stats lock transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE request_logs IN ACCESS EXCLUSIVE MODE`); err != nil {
		_ = tx.Rollback(ctx)
		_ = conn.Close(ctx)
		t.Fatalf("lock request_logs table: %v", err)
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE usage_request_events IN ACCESS EXCLUSIVE MODE`); err != nil {
		_ = tx.Rollback(ctx)

		_ = conn.Close(ctx)
		t.Fatalf("lock usage_request_events table: %v", err)
	}
	return &runtimeStatsTablesLock{conn: conn, tx: tx}
}

func (lock *runtimeStatsTablesLock) release(t *testing.T) {
	t.Helper()
	lock.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = lock.tx.Rollback(ctx)
		_ = lock.conn.Close(ctx)
	})
}

func startAsyncPriorityRequest(t *testing.T, client *http.Client, method string, url string, body any, headers map[string]string) <-chan concurrentRuntimeRequestResult {
	t.Helper()
	var rawBody []byte
	if body != nil {
		encodedBody, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal async request body for %s %s: %v", method, url, err)
		}
		rawBody = encodedBody
	}

	resultCh := make(chan concurrentRuntimeRequestResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var requestBody io.Reader
		if rawBody != nil {

			requestBody = bytes.NewReader(rawBody)
		}
		request, err := http.NewRequestWithContext(ctx, method, url, requestBody)
		if err != nil {
			resultCh <- concurrentRuntimeRequestResult{Err: fmt.Errorf("build request %s %s: %w", method, url, err)}
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
			resultCh <- concurrentRuntimeRequestResult{Err: fmt.Errorf("perform request %s %s: %w", method, url, err)}
			return
		}
		defer func() { _ = response.Body.Close() }()
		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			resultCh <- concurrentRuntimeRequestResult{Err: fmt.Errorf("read response body for %s %s: %w", method, url, err)}
			return
		}
		resultCh <- concurrentRuntimeRequestResult{StatusCode: response.StatusCode, Body: strings.TrimSpace(string(responseBody))}
	}()
	return resultCh
}

func performPriorityRequest(t *testing.T, client *http.Client, timeout time.Duration, method string, url string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var rawBody []byte
	if body != nil {
		encodedBody, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body for %s %s: %v", method, url, err)
		}
		rawBody = encodedBody
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	var requestBody io.Reader
	if rawBody != nil {
		requestBody = bytes.NewReader(rawBody)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, requestBody)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, url, err)
	}
	if rawBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("perform request %s %s: %v", method, url, err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func expectedPendingFirstShedRequests(total int, settings config.Settings) int {
	return min(total, int(settings.ManagementAdmissionBudget().M3MaxConcurrent))
}

func assertManagementOverloadResponse(t *testing.T, response *http.Response) {
	t.Helper()
	assertStatus(t, response, http.StatusServiceUnavailable)
	if retryAfter := response.Header.Get("Retry-After"); retryAfter != "1" {
		t.Fatalf("expected Retry-After header to be 1, got %q", retryAfter)
	}
	assertManagementOverloadBodyString(t, readResponseBody(t, response))
}

func assertManagementOverloadBodyString(t *testing.T, body string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode management overload response %q: %v", body, err)
	}
	if got, _ := payload["detail"].(string); got != "Management route temporarily overloaded. Retry later." {
		t.Fatalf("expected management overload detail %q, got %+v", "Management route temporarily overloaded. Retry later.", payload)
	}
}

func waitForStatsLockWaiters(t *testing.T, conn *pgx.Conn, want int, timeout time.Duration) {
	t.Helper()
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
			t.Fatalf("count blocked stats waiters: %v", err)
		}
		if waiters >= want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d blocked stats requests", want)
}

func assertAsyncRequestsPending(t *testing.T, results []<-chan concurrentRuntimeRequestResult) {
	t.Helper()
	for index, resultCh := range results {
		select {
		case result := <-resultCh:
			t.Fatalf("expected pressure request %d to remain pending, got %+v", index, result)
		default:
		}
	}
}

func assertAsyncRequestsPendingOrRejected(t *testing.T, results []<-chan concurrentRuntimeRequestResult, wantPending int, wantRejected int) []<-chan concurrentRuntimeRequestResult {
	t.Helper()
	pending := make([]<-chan concurrentRuntimeRequestResult, 0, len(results))
	rejected := 0
	for index, resultCh := range results {
		select {
		case result := <-resultCh:
			if result.Err != nil {
				t.Fatalf("expected pressure request %d to fast-fail cleanly, got error: %v", index, result.Err)
			}
			if result.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("expected pressure request %d to fast-fail with 503, got %d with body %s", index, result.StatusCode, result.Body)
			}
			assertManagementOverloadBodyString(t, result.Body)
			rejected++
		default:
			pending = append(pending, resultCh)
		}
	}
	if len(pending) != wantPending {
		t.Fatalf("expected %d pressure requests to remain pending, got %d", wantPending, len(pending))
	}
	if rejected != wantRejected {
		t.Fatalf("expected %d pressure requests to fast-fail, got %d", wantRejected, rejected)
	}
	return pending
}

func assertAsyncRequestsStatus(t *testing.T, results []<-chan concurrentRuntimeRequestResult, wantStatus int) {
	t.Helper()
	for index, resultCh := range results {

		result := awaitAsyncRequest(t, resultCh, 5*time.Second)
		if result.Err != nil {
			t.Fatalf("expected pressure request %d to succeed, got error: %v", index, result.Err)
		}
		if result.StatusCode != wantStatus {
			t.Fatalf("expected pressure request %d status %d, got %d with body %s", index, wantStatus, result.StatusCode, result.Body)
		}
	}
}

func awaitAsyncRequest(t *testing.T, resultCh <-chan concurrentRuntimeRequestResult, timeout time.Duration) concurrentRuntimeRequestResult {
	t.Helper()
	select {
	case result := <-resultCh:
		return result
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for async request result after %s", timeout)
		return concurrentRuntimeRequestResult{}
	}
}
