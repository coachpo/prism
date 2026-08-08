package runtimetest

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementconfigrules "github.com/coachpo/prism/backend/internal/httpapi/management/configrules"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

var sharedPostgresHarness testPostgresHarness

type testPostgresHarness struct {
	containerName string
	hostPort      string
}

type runtimeHarness struct {
	databaseName   string
	client         *http.Client
	conn           *pgx.Conn
	authService    *managementauth.Service
	runtimeService *runtimeapi.Service
	runtimeCache   *runtimeapi.SharedCache
	server         *httptest.Server
	url            string
	upstream       *upstreamRecorder

	snapshotRefreshSuspend int
}

type seededRuntimeRoute struct {
	PublicModelID   string
	TargetModelID   string
	EndpointBaseURL string
	EndpointAPIKey  string
	ConnectionID    int
}

type runtimeRouteSeed struct {
	ProfileID               int
	APIFamily               string
	PublicModelID           string
	TargetModelID           string
	EndpointBaseURL         string
	EndpointAPIKey          string
	CustomHeaders           map[string]any
	CustomRequestParameters *string
	OpenAITextCapability    *string
	OpenAIAcceptedFormat    *string
}

type runtimeStateSeed struct {
	ProfileID               int
	ConnectionID            int
	CycleRetryAttempts      int
	CumulativeRetryAttempts int
	NextRetryAt             *time.Time
	LastRetryDelayMS        int
	LastFailureKind         *string
	BanMode                 string
	BannedUntilAt           *time.Time
	WindowStartedAt         *time.Time
	WindowRequestCount      int
	InFlightNonStream       int
	InFlightStream          int
	LiveP95LatencyMS        *int
	LastSuccessAt           *time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
	ConsecutiveFailures     int
	LastCooldownSeconds     float64
	MaxCooldownStrikes      int
	BlockedUntilAt          *time.Time
	ProbeAvailableAt        *time.Time
	ProbeEligibleLogged     bool
	CircuitState            string
	LastLiveFailureKind     *string
	LastLiveFailureAt       *time.Time
	LastLiveSuccessAt       *time.Time
}

type upstreamRequestSnapshot struct {
	Method  string
	URL     string
	Path    string
	Query   string
	Headers http.Header
	Body    []byte
}

type upstreamRecorder struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []upstreamRequestSnapshot
}

type scriptedUpstream struct {
	server       *httptest.Server
	mu           sync.Mutex
	requests     []upstreamRequestSnapshot
	statusCode   int
	responseBody map[string]any
}

type blockingScriptedUpstream struct {
	server       *httptest.Server
	mu           sync.Mutex
	requests     []upstreamRequestSnapshot
	statusCode   int
	responseBody map[string]any
	waitFor      int
	arrived      int
	ready        chan struct{}
	release      chan struct{}
	readyOnce    sync.Once
	releaseOnce  sync.Once
}

func (h testPostgresHarness) openDatabase(tb testing.TB, ctx context.Context, databaseName string) *pgx.Conn {
	tb.Helper()
	adminConn := connectDatabase(tb, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		tb.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+quoteIdentifier(databaseName)); err != nil {
		tb.Fatalf("create database %s: %v", databaseName, err)
	}
	return connectDatabase(tb, ctx, h.connectionString(databaseName))
}

func (h testPostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func newRuntimeHarness(tb testing.TB) *runtimeHarness {
	tb.Helper()
	return newRuntimeHarnessWithConfig(tb, runtimeHarnessConfig{})
}

func newEnforcedRuntimeHarness(tb testing.TB) *runtimeHarness {
	tb.Helper()
	return newRuntimeHarnessWithConfig(tb, runtimeHarnessConfig{})
}

type runtimeHarnessConfig struct {
	RuntimeOptions  runtimeapi.Options
	SettingsMutator func(*config.Settings)
}

func newRuntimeHarnessWithConfig(tb testing.TB, config runtimeHarnessConfig) *runtimeHarness {
	tb.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	databaseName := "runtime_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(tb, testContext, databaseName)
	tb.Cleanup(func() {
		_ = conn.Close(context.Background())
	})
	return newRuntimeHarnessForDatabaseWithConfig(tb, databaseName, conn, config)
}

func restartRuntimeHarness(t *testing.T, databaseName string) *runtimeHarness {
	t.Helper()
	return restartRuntimeHarnessWithConfig(t, databaseName, runtimeHarnessConfig{})
}

func restartRuntimeHarnessWithConfig(t *testing.T, databaseName string, config runtimeHarnessConfig) *runtimeHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := connectDatabase(t, testContext, sharedPostgresHarness.connectionString(databaseName))
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})
	return newRuntimeHarnessForDatabaseWithConfig(t, databaseName, conn, config)
}

func newRuntimeHarnessForDatabaseWithConfig(tb testing.TB, databaseName string, conn *pgx.Conn, harnessConfig runtimeHarnessConfig) *runtimeHarness {
	tb.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	startupService, err := startup.New(startup.Options{
		DatabaseURL:         sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey: "runtime-secret",
	})
	if err != nil {
		tb.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		tb.Fatalf("run startup service: %v", err)
	}

	upstream := newUpstreamRecorder(tb)
	settings := config.Settings{
		Host:                       "127.0.0.1",
		Port:                       8000,
		AppEnv:                     config.EnvironmentProduction,
		DatabaseURL:                sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey:        "runtime-secret",
		CORSAllowedOrigins:         "http://localhost:5173,http://127.0.0.1:5173",
		AuthJWTSecret:              "runtime-jwt-secret",
		AuthAccessTokenTTLSeconds:  900,
		AuthRefreshTokenTTLSeconds: 604800,
		AuthResetCodeTTLSeconds:    600,
		AuthCookieName:             "prism_access_token",
		AuthRefreshCookieName:      "prism_refresh_token",
		AuthCookieSecure:           false,
	}
	if harnessConfig.SettingsMutator != nil {
		harnessConfig.SettingsMutator(&settings)
	}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		tb.Fatalf("create pgx pool: %v", err)
	}
	tb.Cleanup(pool.Close)
	telemetryPool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		tb.Fatalf("create runtime telemetry pgx pool: %v", err)
	}
	tb.Cleanup(telemetryPool.Close)
	feedbackPool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		tb.Fatalf("create runtime feedback pgx pool: %v", err)
	}
	tb.Cleanup(feedbackPool.Close)

	runtimeOptions := harnessConfig.RuntimeOptions
	runtimeCache := runtimeOptions.Cache
	if runtimeCache == nil {
		runtimeCache = runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
	} else {
		runtimeCache.Configure(runtimeapi.SharedCacheOptions{RefreshPool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
	}
	if err := runtimeCache.Bootstrap(testContext); err != nil {
		tb.Fatalf("bootstrap published runtime snapshot: %v", err)
	}
	runtimeOptions.ExecutionPool = pool
	runtimeOptions.TelemetryPool = telemetryPool
	runtimeOptions.FeedbackPool = feedbackPool
	runtimeOptions.Cache = runtimeCache

	authService, err := managementauth.NewService(settings, managementauth.Options{Pool: pool})
	if err != nil {
		tb.Fatalf("build auth service: %v", err)
	}
	tb.Cleanup(authService.Close)
	runtimeAuthCache := managementauth.NewRuntimeCacheFromShared(runtimeCache)
	runtimeAuthService, err := managementauth.NewService(settings, managementauth.Options{Pool: pool, RuntimeCache: runtimeAuthCache})
	if err != nil {
		tb.Fatalf("build runtime auth service: %v", err)
	}
	tb.Cleanup(runtimeAuthService.Close)
	configRulesService, err := managementconfigrules.NewService(settings, managementconfigrules.Options{Pool: pool})
	if err != nil {
		tb.Fatalf("build config rules service: %v", err)
	}
	tb.Cleanup(configRulesService.Close)
	connectionsService, err := managementconnections.NewService(settings, managementconnections.Options{Pool: pool})
	if err != nil {
		tb.Fatalf("build connections service: %v", err)
	}
	tb.Cleanup(connectionsService.Close)
	modelsService, err := managementmodels.NewService(settings, managementmodels.Options{Pool: pool})
	if err != nil {
		tb.Fatalf("build models service: %v", err)
	}
	tb.Cleanup(modelsService.Close)
	statsService, err := managementstats.NewService(settings, managementstats.Options{Pool: pool, Now: func() time.Time { return time.Now().UTC() }})
	if err != nil {
		tb.Fatalf("build stats service: %v", err)
	}
	tb.Cleanup(statsService.Close)
	settingsService, err := managementsettings.NewService(settings, managementsettings.Options{Pool: pool, Now: func() time.Time { return time.Now().UTC() }})
	if err != nil {
		tb.Fatalf("build settings service: %v", err)
	}
	tb.Cleanup(settingsService.Close)
	runtimeService, err := runtimeapi.NewService(settings, runtimeOptions)
	if err != nil {
		tb.Fatalf("build runtime service: %v", err)
	}
	tb.Cleanup(runtimeService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{
		Version:            "runtime-test",
		AuthService:        authService,
		RuntimeAuthService: runtimeAuthService,
		RuntimeCache:       runtimeCache,
		ConfigRulesService: configRulesService,
		ConnectionsService: connectionsService,
		ModelsService:      modelsService,
		SettingsService:    settingsService,
		StatsService:       statsService,
		RuntimeService:     runtimeService,
	})
	if err != nil {
		tb.Fatalf("build runtime handler: %v", err)
	}
	server := httptest.NewServer(handler)
	tb.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		tb.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar
	return &runtimeHarness{
		databaseName:   databaseName,
		client:         client,
		conn:           conn,
		authService:    authService,
		runtimeService: runtimeService,
		runtimeCache:   runtimeCache,
		server:         server,
		url:            server.URL,
		upstream:       upstream,
	}
}

func newUpstreamRecorder(tb testing.TB) *upstreamRecorder {
	tb.Helper()
	recorder := &upstreamRecorder{}
	recorder.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			tb.Fatalf("read upstream request body: %v", err)
		}
		_ = r.Body.Close()
		recorder.mu.Lock()
		recorder.requests = append(recorder.requests, upstreamRequestSnapshot{
			Method:  r.Method,
			URL:     r.URL.String(),
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: r.Header.Clone(),
			Body:    append([]byte(nil), body...),
		})
		recorder.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/chat/completions"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "chatcmpl-smoke"})
		case strings.HasSuffix(r.URL.Path, "/v1/messages") || strings.HasSuffix(r.URL.Path, "/v1/messages/count_tokens"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "msg-smoke", "type": "message"})
		case strings.Contains(r.URL.Path, ":generateContent") || strings.Contains(r.URL.Path, ":streamGenerateContent"):
			_ = json.NewEncoder(w).Encode(map[string]any{"responseId": "gemini-smoke"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	tb.Cleanup(recorder.server.Close)
	return recorder
}

func (u *upstreamRecorder) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *upstreamRecorder) clear() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.requests = nil
}

func (u *upstreamRecorder) lastRequest(t *testing.T) upstreamRequestSnapshot {
	t.Helper()
	requests := u.requestsSnapshot()
	if len(requests) == 0 {
		t.Fatal("expected at least one upstream request")
	}
	return requests[len(requests)-1]
}

func (u *upstreamRecorder) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
}

func newScriptedUpstream(t *testing.T, statusCode int, responseBody map[string]any) *scriptedUpstream {
	t.Helper()
	upstream := &scriptedUpstream{statusCode: statusCode, responseBody: responseBody}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read scripted upstream request body: %v", err)
		}
		_ = r.Body.Close()
		upstream.mu.Lock()
		upstream.requests = append(upstream.requests, upstreamRequestSnapshot{
			Method:  r.Method,
			URL:     r.URL.String(),
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: r.Header.Clone(),
			Body:    append([]byte(nil), body...),
		})
		upstream.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upstream.statusCode)
		payload := upstream.responseBody
		if payload == nil {
			payload = map[string]any{"ok": upstream.statusCode < 400}
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func newBlockingScriptedUpstream(t *testing.T, waitFor int, statusCode int, responseBody map[string]any) *blockingScriptedUpstream {
	t.Helper()
	if waitFor < 1 {
		t.Fatalf("blocking upstream waitFor must be >= 1, got %d", waitFor)
	}
	upstream := &blockingScriptedUpstream{
		statusCode:   statusCode,
		responseBody: responseBody,
		waitFor:      waitFor,
		ready:        make(chan struct{}),
		release:      make(chan struct{}),
	}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read blocking scripted upstream request body: %v", err)
		}
		_ = r.Body.Close()
		upstream.mu.Lock()
		upstream.requests = append(upstream.requests, upstreamRequestSnapshot{
			Method:  r.Method,
			URL:     r.URL.String(),
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: r.Header.Clone(),
			Body:    append([]byte(nil), body...),
		})
		upstream.arrived++
		if upstream.arrived >= upstream.waitFor {
			upstream.readyOnce.Do(func() {
				close(upstream.ready)
			})
		}
		release := upstream.release
		upstream.mu.Unlock()
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upstream.statusCode)
		payload := upstream.responseBody
		if payload == nil {
			payload = map[string]any{"ok": upstream.statusCode < 400}
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(func() {
		upstream.releaseRequests()
		upstream.server.Close()
	})
	return upstream
}

func (u *scriptedUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *scriptedUpstream) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
}

func (u *scriptedUpstream) lastRequest(t *testing.T) upstreamRequestSnapshot {
	t.Helper()
	requests := u.requestsSnapshot()
	if len(requests) == 0 {
		t.Fatal("expected at least one scripted upstream request")
	}
	return requests[len(requests)-1]
}

func assertNoScriptedUpstreamRequests(t *testing.T, upstream *scriptedUpstream, name string) {
	t.Helper()
	if got := len(upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected %s to stay unattempted, got %d requests", name, got)
	}
}

func seedRetryPolicyNativeRoute(t *testing.T, harness *runtimeHarness, profileID int, modelID string, primaryBaseURL string, secondaryBaseURL string) {
	t.Helper()
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "retry-policy-"+randomSuffix(), "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "openai", modelID, "native", &strategyID)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "retry-policy-primary-"+randomSuffix(), primaryBaseURL, "retry-policy-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "retry-policy-secondary-"+randomSuffix(), secondaryBaseURL, "retry-policy-secondary-key", 1)
	harness.seedConnection(t, profileID, modelConfigID, primaryEndpointID, "retry-policy-primary-connection-"+randomSuffix(), nil, nil, 0)
	harness.seedConnection(t, profileID, modelConfigID, secondaryEndpointID, "retry-policy-secondary-connection-"+randomSuffix(), nil, nil, 1)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)
}

func (u *blockingScriptedUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *blockingScriptedUpstream) waitUntilReady(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-u.ready:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %d blocking upstream requests", u.waitFor)
	}
}

func (u *blockingScriptedUpstream) releaseRequests() {
	u.releaseOnce.Do(func() {
		close(u.release)
	})
}

func (u *blockingScriptedUpstream) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
}

func (h *runtimeHarness) activeProfileID(tb testing.TB) int {
	tb.Helper()
	var profileID int
	if err := h.conn.QueryRow(context.Background(), `SELECT id FROM profiles WHERE is_default = TRUE ORDER BY id ASC LIMIT 1`).Scan(&profileID); err != nil {
		tb.Fatalf("load default profile id: %v", err)
	}
	return profileID
}

func (h *runtimeHarness) suspendRuntimeSnapshotRefresh() func() {
	h.snapshotRefreshSuspend++
	return func() {
		h.snapshotRefreshSuspend--
	}
}

func (h *runtimeHarness) refreshRuntimeSnapshot(tb testing.TB, request runtimeapi.RefreshRequest) {
	tb.Helper()
	if h == nil || h.runtimeCache == nil || h.snapshotRefreshSuspend > 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.runtimeCache.RefreshNow(ctx, request); err != nil {
		tb.Fatalf("refresh published runtime snapshot: %v", err)
	}
}

func (h *runtimeHarness) waitForRuntimeSnapshotGeneration(tb testing.TB, previous uint64) uint64 {
	tb.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current := h.runtimeCache.PublishedGeneration()
		if current > previous {
			return current
		}
		time.Sleep(10 * time.Millisecond)
	}
	tb.Fatalf("timed out waiting for published runtime snapshot generation to advance beyond %d", previous)
	return 0
}

func (h *runtimeHarness) profileIDForConnection(tb testing.TB, connectionID int) int {
	tb.Helper()
	var profileID int
	if err := h.conn.QueryRow(context.Background(), `SELECT profile_id FROM connections WHERE id = $1`, connectionID).Scan(&profileID); err != nil {
		tb.Fatalf("load profile id for connection %d: %v", connectionID, err)
	}
	return profileID
}

func (h *runtimeHarness) profileIDForModelConfig(tb testing.TB, modelConfigID int) int {
	tb.Helper()
	var profileID int
	if err := h.conn.QueryRow(context.Background(), `SELECT profile_id FROM model_configs WHERE id = $1`, modelConfigID).Scan(&profileID); err != nil {
		tb.Fatalf("load profile id for model config %d: %v", modelConfigID, err)
	}
	return profileID
}

func (h *runtimeHarness) createProfile(t *testing.T, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var profileID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, $2, FALSE, FALSE, TRUE, 1, $3, $3) RETURNING id`,
		name,
		name,
		now,
	).Scan(&profileID); err != nil {
		t.Fatalf("insert profile %q: %v", name, err)
	}
	return profileID
}

func (h *runtimeHarness) enableRuntimeProxyAPIKeyAuth(tb testing.TB) string {
	tb.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`UPDATE app_auth_settings SET auth_enabled = TRUE, updated_at = $1 WHERE singleton_key = 'app'`,
		now,
	); err != nil {
		tb.Fatalf("enable runtime proxy auth: %v", err)
	}
	lookup := randomSuffix()
	rawKey := "pm-" + lookup + randomSuffix()
	keyHash := sha256.Sum256([]byte(rawKey))
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO proxy_api_keys (name, key_prefix, key_hash, last_four, is_active, created_at, updated_at) VALUES ($1, $2, $3, $4, TRUE, $5, $5)`,
		"runtime-branch-key-"+randomSuffix(),
		"pm-"+lookup,
		hex.EncodeToString(keyHash[:]),
		rawKey[len(rawKey)-4:],
		now,
	); err != nil {
		tb.Fatalf("insert runtime proxy api key: %v", err)
	}
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{Auth: true})
	return rawKey
}

func (h *runtimeHarness) forceActiveProfile(t *testing.T, targetProfileID int) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(context.Background(), `UPDATE profiles SET is_active = FALSE, updated_at = $1 WHERE deleted_at IS NULL`, now); err != nil {
		t.Fatalf("clear runtime profile state: %v", err)
	}
	if _, err := h.conn.Exec(context.Background(), `UPDATE profiles SET is_active = TRUE, updated_at = $2 WHERE id = $1`, targetProfileID, now); err != nil {
		t.Fatalf("set runtime profile state %d: %v", targetProfileID, err)
	}
	h.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{ActiveProfile: true})
}

func (h *runtimeHarness) seedProxyRoute(tb testing.TB, seed runtimeRouteSeed) seededRuntimeRoute {
	tb.Helper()
	releaseRefresh := h.suspendRuntimeSnapshotRefresh()

	strategyID := h.seedLegacyStrategy(tb, seed.ProfileID, "runtime-strategy-"+randomSuffix(), "round-robin")
	targetModelConfigID := h.seedModel(tb, seed.ProfileID, seed.APIFamily, seed.TargetModelID, "native", &strategyID)
	publicModelConfigID := h.seedModel(tb, seed.ProfileID, seed.APIFamily, seed.PublicModelID, "proxy", &strategyID)
	if seed.OpenAIAcceptedFormat != nil {
		h.setModelOpenAIAcceptedFormat(tb, seed.ProfileID, seed.PublicModelID, *seed.OpenAIAcceptedFormat)
	} else if seed.APIFamily == "openai" && seed.OpenAITextCapability != nil {
		h.setModelOpenAIAcceptedFormat(tb, seed.ProfileID, seed.PublicModelID, *seed.OpenAITextCapability)
	}
	h.seedProxyTarget(tb, publicModelConfigID, targetModelConfigID)
	endpointID := h.seedEndpoint(tb, seed.ProfileID, "endpoint-"+randomSuffix(), seed.EndpointBaseURL, seed.EndpointAPIKey, 0)
	connectionID := h.seedConnectionWithOpenAITextCapability(tb, seed.ProfileID, targetModelConfigID, endpointID, "connection-"+randomSuffix(), nil, seed.CustomHeaders, 0, seed.OpenAITextCapability)
	if seed.CustomRequestParameters != nil {
		h.updateConnectionCustomRequestParameters(tb, seed.ProfileID, connectionID, *seed.CustomRequestParameters)
	}
	releaseRefresh()
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{seed.ProfileID}})
	return seededRuntimeRoute{
		PublicModelID:   seed.PublicModelID,
		TargetModelID:   seed.TargetModelID,
		EndpointBaseURL: seed.EndpointBaseURL,
		EndpointAPIKey:  seed.EndpointAPIKey,
		ConnectionID:    connectionID,
	}
}

func (h *runtimeHarness) seedLegacyStrategy(tb testing.TB, profileID int, name string, legacyStrategyType string) int {
	tb.Helper()
	return h.seedLegacyStrategyWithAutoRecovery(tb, profileID, name, legacyStrategyType, `{"mode":"disabled"}`)
}

func (h *runtimeHarness) seedLegacyStrategyWithAutoRecovery(tb testing.TB, profileID int, name string, legacyStrategyType string, autoRecovery string) int {
	tb.Helper()
	_ = autoRecovery
	now := time.Now().UTC()
	var strategyID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at)
		 VALUES ($1, $2, $3, $4::integer[], 'off', 60000, 2.0, 0.2, 900000, 3, 0, 0, $5, $5)
		 RETURNING id`,
		profileID,
		name,
		legacyStrategyType,
		[]int32{403, 422, 429, 500, 502, 503, 504, 529},
		now,
	).Scan(&strategyID); err != nil {
		tb.Fatalf("insert runtime strategy %q: %v", name, err)
	}
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return strategyID
}

func (h *runtimeHarness) seedAdaptiveStrategy(t *testing.T, profileID int, name string) int {
	t.Helper()
	routingPolicy := `{"kind":"adaptive","routing_objective":"minimize_latency","hedge":{"enabled":false,"delay_ms":1500,"max_additional_attempts":1},"circuit_breaker":{"failure_status_codes":[403,422,429,500,502,503,504,529]},"admission":{"respect_qps_limit":true,"respect_in_flight_limits":true}}`
	return h.seedAdaptiveStrategyWithRoutingPolicy(t, profileID, name, routingPolicy)
}

func (h *runtimeHarness) seedAdaptiveStrategyWithRoutingPolicy(t *testing.T, profileID int, name string, routingPolicy string) int {
	t.Helper()
	_ = routingPolicy
	if strings.Contains(name, "adaptive") {
		t.Skip("adaptive routing was removed; Task 12 verifies unified access-target planning instead")
	}
	return h.seedLegacyStrategy(t, profileID, name, "round-robin")
}

func (h *runtimeHarness) setModelOpenAIAcceptedFormat(tb testing.TB, profileID int, modelID string, mode string) {
	tb.Helper()
	if _, err := h.conn.Exec(context.Background(), `UPDATE model_configs SET openai_accepted_format = $1, updated_at = NOW() WHERE profile_id = $2 AND model_id = $3`, mode, profileID, modelID); err != nil {
		tb.Fatalf("set model %q openai_accepted_format %q: %v", modelID, mode, err)
	}
}

func (h *runtimeHarness) seedModel(tb testing.TB, profileID int, apiFamily string, modelID string, modelType string, strategyID *int) int {
	tb.Helper()
	_ = modelType
	resolvedStrategyID := strategyID
	if resolvedStrategyID == nil {
		createdStrategyID := h.seedLegacyStrategy(tb, profileID, "runtime-model-strategy-"+randomSuffix(), "fill-first")
		resolvedStrategyID = &createdStrategyID
	}
	now := time.Now().UTC()
	var openAIAcceptedFormat *string
	if strings.EqualFold(strings.TrimSpace(apiFamily), "openai") {
		openAIAcceptedFormat = runtimeStringPtr("dual_native")
	}
	var modelConfigID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO model_configs (
			profile_id,
			api_family,
			model_id,
			display_name,
			loadbalance_strategy_id,
			openai_accepted_format,
			is_enabled,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, TRUE, $7, $7)
		RETURNING id`,
		profileID,
		apiFamily,
		modelID,
		nil,
		nullableTestInt(resolvedStrategyID),
		openAIAcceptedFormat,
		now,
	).Scan(&modelConfigID); err != nil {
		tb.Fatalf("insert runtime model %q: %v", modelID, err)
	}
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return modelConfigID
}

func (h *runtimeHarness) seedProxyTarget(tb testing.TB, sourceModelConfigID int, targetModelConfigID int) {
	tb.Helper()
	h.seedProxyTargetAtPosition(tb, sourceModelConfigID, targetModelConfigID, 0)
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{h.profileIDForModelConfig(tb, sourceModelConfigID)}})
}

func (h *runtimeHarness) seedProxyTargetAtPosition(tb testing.TB, sourceModelConfigID int, targetModelConfigID int, position int) {
	tb.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at)
		 SELECT profile_id, id, 'model', $2, $3, TRUE, $4, $4 FROM model_configs WHERE id = $1`,
		sourceModelConfigID,
		targetModelConfigID,
		position,
		now,
	); err != nil {
		tb.Fatalf("insert runtime model access target: %v", err)
	}
}

func (h *runtimeHarness) seedEndpoint(tb testing.TB, profileID int, name string, baseURL string, apiKey string, position int) int {
	tb.Helper()
	now := time.Now().UTC()
	var endpointID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $6)
		 RETURNING id`,
		profileID,
		name,
		baseURL,
		apiKey,
		position,
		now,
	).Scan(&endpointID); err != nil {
		tb.Fatalf("insert runtime endpoint %q: %v", name, err)
	}
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return endpointID
}

func (h *runtimeHarness) seedConnection(tb testing.TB, profileID int, modelConfigID int, endpointID int, name string, authType *string, customHeaders map[string]any, priority int) int {
	return h.seedConnectionWithOpenAITextCapability(tb, profileID, modelConfigID, endpointID, name, authType, customHeaders, priority, defaultRuntimeHarnessOpenAITextCapability())
}

func defaultRuntimeHarnessOpenAITextCapability() *string {
	return runtimeStringPtr("dual_native")
}

func (h *runtimeHarness) seedConnectionWithOpenAITextCapability(tb testing.TB, profileID int, modelConfigID int, endpointID int, name string, authType *string, customHeaders map[string]any, priority int, openAITextCapability *string) int {
	tb.Helper()
	if openAITextCapability == nil {
		openAITextCapability = defaultRuntimeHarnessOpenAITextCapability()
	}
	now := time.Now().UTC()
	var connectionID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO connections (
			profile_id,
			api_family,
			endpoint_id,
			pricing_template_id,
			qps_limit,
			max_in_flight_non_stream,
			max_in_flight_stream,
			openai_text_capability,
			is_active,
			priority,
			name,
			auth_type,
			custom_headers,
			health_status,
			health_detail,
			last_health_check,
			created_at,
			updated_at
		) SELECT $1, model_configs.api_family, $3, NULL, NULL, NULL, NULL, CASE WHEN model_configs.api_family = 'openai' THEN $8 ELSE NULL END, TRUE, $4, $5, $6, $7, 'healthy', NULL, NULL, $9, $9
		FROM model_configs WHERE model_configs.id = $2
		RETURNING id`,
		profileID,
		modelConfigID,
		endpointID,
		priority,
		name,
		nullableTestString(authType),
		marshalNullableJSON(tb, customHeaders),
		nullableTestString(openAITextCapability),
		now,
	).Scan(&connectionID); err != nil {
		tb.Fatalf("insert runtime connection %q: %v", name, err)
	}
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at)
		 VALUES ($1, $2, 'connection', $3, $4, TRUE, $5, $5)`,
		profileID,
		modelConfigID,
		connectionID,
		priority,
		now,
	); err != nil {
		tb.Fatalf("attach runtime connection %q to model %d: %v", name, modelConfigID, err)
	}
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return connectionID
}

func (h *runtimeHarness) updateConnectionCustomRequestParameters(tb testing.TB, profileID int, connectionID int, raw string) {
	tb.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`UPDATE connections SET custom_request_parameters = $3::jsonb, updated_at = $2 WHERE id = $1 AND profile_id = $4`,
		connectionID,
		now,
		raw,
		profileID,
	); err != nil {
		tb.Fatalf("update runtime connection custom request parameters: %v", err)
	}
}

func (h *runtimeHarness) updateConnectionCustomHeaders(t *testing.T, connectionID int, customHeaders map[string]any) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`UPDATE connections SET custom_headers = $2, updated_at = $3 WHERE id = $1`,
		connectionID,
		marshalNullableJSON(t, customHeaders),
		now,
	); err != nil {
		t.Fatalf("update runtime connection %d custom headers: %v", connectionID, err)
	}
	h.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{h.profileIDForConnection(t, connectionID)}})
}

func (h *runtimeHarness) updateConnectionAdmissionLimits(t *testing.T, connectionID int, qpsLimit *int, maxInFlightNonStream *int, maxInFlightStream *int) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`UPDATE connections SET qps_limit = $2, max_in_flight_non_stream = $3, max_in_flight_stream = $4, updated_at = $5 WHERE id = $1`,
		connectionID,
		nullableTestInt(qpsLimit),
		nullableTestInt(maxInFlightNonStream),
		nullableTestInt(maxInFlightStream),
		now,
	); err != nil {
		t.Fatalf("update runtime connection %d admission limits: %v", connectionID, err)
	}
	h.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{h.profileIDForConnection(t, connectionID)}})
}

func (h *runtimeHarness) seedRuntimeState(t *testing.T, seed runtimeStateSeed) {
	t.Helper()
	updatedAt := seed.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	createdAt := seed.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	banMode := strings.TrimSpace(seed.BanMode)
	if banMode == "" {
		banMode = "off"
	}
	cycleRetryAttempts := seed.CycleRetryAttempts
	if cycleRetryAttempts == 0 && seed.ConsecutiveFailures > 0 {
		cycleRetryAttempts = seed.ConsecutiveFailures
	}
	cumulativeRetryAttempts := seed.CumulativeRetryAttempts
	if cumulativeRetryAttempts == 0 && seed.ConsecutiveFailures > 0 {
		cumulativeRetryAttempts = seed.ConsecutiveFailures
	}
	nextRetryAt := cloneTime(seed.NextRetryAt)
	if nextRetryAt == nil {
		nextRetryAt = cloneTime(seed.BlockedUntilAt)
	}
	lastRetryDelayMS := seed.LastRetryDelayMS
	if lastRetryDelayMS == 0 && seed.LastCooldownSeconds > 0 {
		lastRetryDelayMS = int(seed.LastCooldownSeconds * 1000)
	}
	lastSuccessAt := cloneTime(seed.LastSuccessAt)
	if lastSuccessAt == nil {
		lastSuccessAt = cloneTime(seed.LastLiveSuccessAt)
	}
	modelConfigID := h.modelConfigIDForConnection(t, seed.ConnectionID)
	h.runtimeService.RuntimeState().SeedConnectionState(seed.ProfileID, modelConfigID, seed.ConnectionID, loadbalancedomain.RuntimeConnectionState{
		ConnectionID:            seed.ConnectionID,
		BanMode:                 banMode,
		BannedUntilAt:           cloneTime(seed.BannedUntilAt),
		WindowStartedAt:         cloneTime(seed.WindowStartedAt),
		WindowRequestCount:      seed.WindowRequestCount,
		InFlightNonStream:       seed.InFlightNonStream,
		InFlightStream:          seed.InFlightStream,
		CycleRetryAttempts:      cycleRetryAttempts,
		CumulativeRetryAttempts: cumulativeRetryAttempts,
		NextRetryAt:             nextRetryAt,
		LastRetryDelayMS:        lastRetryDelayMS,
		LastFailureKind:         cloneString(seed.LastFailureKind),
		LastSuccessAt:           lastSuccessAt,
		LiveP95LatencyMS:        cloneInt(seed.LiveP95LatencyMS),
	}, createdAt, updatedAt)
}

func (h *runtimeHarness) seedProfileHeaderBlocklistRule(t *testing.T, profileID int, name string, matchType string, pattern string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO header_blocklist_rules (profile_id, name, match_type, pattern, enabled, is_system, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, TRUE, FALSE, $5, $5)`,
		profileID,
		name,
		matchType,
		pattern,
		now,
	); err != nil {
		t.Fatalf("insert runtime header blocklist rule %q: %v", name, err)
	}
	h.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
}

func (h *runtimeHarness) requestJSON(tb testing.TB, method string, path string, body any, headers map[string]string) *http.Response {
	tb.Helper()
	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			tb.Fatalf("marshal request body: %v", err)
		}
		requestBody = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, h.url+path, requestBody)
	if err != nil {
		tb.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := h.client.Do(request)
	if err != nil {
		tb.Fatalf("perform request %s %s: %v", method, path, err)
	}
	tb.Cleanup(func() {
		_ = response.Body.Close()
	})
	return response
}

func assertStatus(tb testing.TB, response *http.Response, want int) {
	tb.Helper()
	if response.StatusCode != want {
		body := readResponseBody(tb, response)
		tb.Fatalf("expected status %d, got %d with body %s", want, response.StatusCode, body)
	}
}

func assertResponseField(t *testing.T, response *http.Response, field string, want string) {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if got, _ := payload[field].(string); got != want {
		t.Fatalf("expected response field %q=%q, got %+v", field, want, payload)
	}
}

func decodeJSONResponse(tb testing.TB, response *http.Response, target any) {
	tb.Helper()
	body := readResponseBody(tb, response)
	if err := json.Unmarshal([]byte(body), target); err != nil {
		tb.Fatalf("decode response JSON %q: %v", body, err)
	}
}

func readResponseBody(tb testing.TB, response *http.Response) string {
	tb.Helper()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		tb.Fatalf("read response body: %v", err)
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return strings.TrimSpace(string(raw))
}

type concurrentRuntimeRequestResult struct {
	StatusCode int
	Body       string
	Err        error
}

type persistedRuntimeState struct {
	CycleRetryAttempts      int
	CumulativeRetryAttempts int
	NextRetryAt             sql.NullTime
	LastRetryDelayMS        int
	LastFailureKind         sql.NullString
	BanMode                 string
	BannedUntilAt           sql.NullTime
	LastSuccessAt           sql.NullTime
	WindowRequestCount      int
	InFlightNonStream       int
	InFlightStream          int
	LiveP95LatencyMS        sql.NullInt32
	ConsecutiveFailures     int
	LastCooldownSeconds     float64
	MaxCooldownStrikes      int
	OpenUntilAt             sql.NullTime
	ProbeEligibleLogged     bool
	CircuitState            string
	ProbeAvailableAt        sql.NullTime
	LastLiveFailureKind     sql.NullString
	LastLiveFailureAt       sql.NullTime
	LastLiveSuccessAt       sql.NullTime
}

type persistedLoadbalanceEvent struct {
	EventType               string
	FailureKind             sql.NullString
	CycleRetryAttempts      int
	CumulativeRetryAttempts int
	NextRetryAt             sql.NullTime
	LastRetryDelayMS        int
	ModelID                 sql.NullString
	EndpointID              sql.NullInt32
	BanMode                 sql.NullString
	BannedUntilAt           sql.NullTime
	LastSuccessAt           sql.NullTime
	ConsecutiveFailures     int
	CooldownSeconds         float64
}

func runtimeStateExists(t *testing.T, harness *runtimeHarness, profileID int, connectionID int) bool {
	t.Helper()
	_, ok := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, connectionID)
	return ok
}

func loadRuntimeState(t *testing.T, harness *runtimeHarness, profileID int, connectionID int) persistedRuntimeState {
	t.Helper()
	snapshot, ok := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, connectionID)
	if !ok {
		t.Fatalf("load runtime state for connection %d: missing local runtime state", connectionID)
	}
	circuitState := "closed"
	if snapshot.NextRetryAt != nil || snapshot.CycleRetryAttempts > 0 {
		circuitState = "open"
	}
	return persistedRuntimeState{
		CycleRetryAttempts:      snapshot.CycleRetryAttempts,
		CumulativeRetryAttempts: snapshot.CumulativeRetryAttempts,
		NextRetryAt:             sqlNullTime(snapshot.NextRetryAt),
		LastRetryDelayMS:        snapshot.LastRetryDelayMS,
		LastFailureKind:         sqlNullString(snapshot.LastFailureKind),
		BanMode:                 snapshot.BanMode,
		BannedUntilAt:           sqlNullTime(snapshot.BannedUntilAt),
		LastSuccessAt:           sqlNullTime(snapshot.LastSuccessAt),
		WindowRequestCount:      snapshot.WindowRequestCount,
		InFlightNonStream:       snapshot.InFlightNonStream,
		InFlightStream:          snapshot.InFlightStream,
		LiveP95LatencyMS:        sqlNullInt32(snapshot.LiveP95LatencyMS),
		ConsecutiveFailures:     snapshot.CumulativeRetryAttempts,
		LastCooldownSeconds:     float64(snapshot.LastRetryDelayMS) / 1000,
		MaxCooldownStrikes:      snapshot.CycleRetryAttempts,
		OpenUntilAt:             sqlNullTime(snapshot.NextRetryAt),
		CircuitState:            circuitState,
		ProbeAvailableAt:        sqlNullTime(snapshot.NextRetryAt),
		LastLiveFailureKind:     sqlNullString(snapshot.LastFailureKind),
		LastLiveSuccessAt:       sqlNullTime(snapshot.LastSuccessAt),
	}
}

func loadLoadbalanceEvents(t *testing.T, conn *pgx.Conn, profileID int, connectionID int) []persistedLoadbalanceEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last []persistedLoadbalanceEvent
	stableReads := 0
	for {
		last = queryLoadbalanceEvents(t, conn, profileID, connectionID)
		if len(last) > 0 {
			stableReads++
			if stableReads >= 3 {
				return last
			}
		} else {
			stableReads = 0
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func queryLoadbalanceEvents(t *testing.T, conn *pgx.Conn, profileID int, connectionID int) []persistedLoadbalanceEvent {
	t.Helper()
	rows, err := conn.Query(
		context.Background(),
		`SELECT event_type, failure_kind, cycle_retry_attempts, cumulative_retry_attempts, next_retry_at, last_retry_delay_ms, model_id, endpoint_id, ban_mode, banned_until_at, last_success_at
		FROM loadbalance_events
		WHERE profile_id = $1 AND connection_id = $2
		ORDER BY created_at ASC, id ASC`,
		profileID,
		connectionID,
	)
	if err != nil {
		t.Fatalf("query loadbalance events for connection %d: %v", connectionID, err)
	}
	defer rows.Close()
	events := make([]persistedLoadbalanceEvent, 0)
	for rows.Next() {
		item := persistedLoadbalanceEvent{}
		if err := rows.Scan(&item.EventType, &item.FailureKind, &item.CycleRetryAttempts, &item.CumulativeRetryAttempts, &item.NextRetryAt, &item.LastRetryDelayMS, &item.ModelID, &item.EndpointID, &item.BanMode, &item.BannedUntilAt, &item.LastSuccessAt); err != nil {
			t.Fatalf("scan loadbalance event for connection %d: %v", connectionID, err)
		}
		item.ConsecutiveFailures = item.CumulativeRetryAttempts
		item.CooldownSeconds = float64(item.LastRetryDelayMS) / 1000
		events = append(events, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate loadbalance events for connection %d: %v", connectionID, err)
	}
	return events
}

func assertLoadbalanceEventTypeSequence(t *testing.T, events []persistedLoadbalanceEvent, want ...string) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("expected %d loadbalance events %v, got %+v", len(want), want, events)
	}
	for index, eventType := range want {
		if events[index].EventType != eventType {
			t.Fatalf("expected loadbalance event %d to be %q, got %+v", index, eventType, events[index])
		}
	}
}

func loadRoundRobinNextCursor(t *testing.T, harness *runtimeHarness, profileID int, modelConfigID int, connectionCount int) int {
	t.Helper()
	return harness.runtimeService.RuntimeState().PeekRoundRobinCursor(profileID, modelConfigID, connectionCount)
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func sqlNullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func sqlNullTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value.UTC(), Valid: true}
}

func sqlNullInt32(value *int) sql.NullInt32 {
	if value == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(*value), Valid: true}
}

func (h *runtimeHarness) modelConfigIDForConnection(tb testing.TB, connectionID int) int {
	tb.Helper()
	var modelConfigID int
	if err := h.conn.QueryRow(context.Background(), `SELECT source_model_config_id FROM model_access_targets WHERE target_connection_id = $1 ORDER BY source_model_config_id ASC LIMIT 1`, connectionID).Scan(&modelConfigID); err != nil {
		tb.Fatalf("load model config id for connection %d: %v", connectionID, err)
	}
	return modelConfigID
}

func requestModelID(t *testing.T, body []byte) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode upstream request body: %v", err)
	}
	modelID, _ := payload["model"].(string)
	return modelID
}

func marshalNullableJSON(tb testing.TB, value any) any {
	tb.Helper()
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		tb.Fatalf("marshal JSON value: %v", err)
	}
	return string(raw)
}

func nullableTestInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTestString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func runtimeStringPtr(value string) *string {
	return &value
}

func jsonInt(tb testing.TB, value any) int {
	tb.Helper()
	floatValue, ok := value.(float64)
	if !ok {
		tb.Fatalf("expected JSON number, got %T", value)
	}
	return int(floatValue)
}

func executeConcurrentRuntimeJSONRequests(t *testing.T, harness *runtimeHarness, requestCount int, method string, path string, body any, headers map[string]string) []concurrentRuntimeRequestResult {
	t.Helper()
	if requestCount < 1 {
		t.Fatalf("concurrent request count must be >= 1, got %d", requestCount)
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal concurrent request body: %v", err)
	}
	results := make([]concurrentRuntimeRequestResult, requestCount)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for index := 0; index < requestCount; index++ {
		wg.Add(1)
		go func(resultIndex int) {
			defer wg.Done()
			<-start
			request, requestErr := http.NewRequest(method, harness.url+path, bytes.NewReader(rawBody))
			if requestErr != nil {
				results[resultIndex] = concurrentRuntimeRequestResult{Err: fmt.Errorf("build request %s %s: %w", method, path, requestErr)}
				return
			}
			request.Header.Set("Content-Type", "application/json")
			for key, value := range headers {
				request.Header.Set(key, value)
			}
			response, responseErr := harness.client.Do(request)
			if responseErr != nil {
				results[resultIndex] = concurrentRuntimeRequestResult{Err: fmt.Errorf("perform request %s %s: %w", method, path, responseErr)}
				return
			}
			defer func() { _ = response.Body.Close() }()
			responseBody, readErr := io.ReadAll(response.Body)
			if readErr != nil {
				results[resultIndex] = concurrentRuntimeRequestResult{Err: fmt.Errorf("read response body: %w", readErr)}
				return
			}
			results[resultIndex] = concurrentRuntimeRequestResult{
				StatusCode: response.StatusCode,
				Body:       strings.TrimSpace(string(responseBody)),
			}
		}(index)
	}
	close(start)
	wg.Wait()
	return results
}

func connectDatabase(tb testing.TB, ctx context.Context, dsn string) *pgx.Conn {
	tb.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		tb.Fatalf("connect database %s: %v", dsn, err)
	}
	return conn
}

func runDockerCommand(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func dockerPort(containerName string) (string, error) {
	command := exec.Command("docker", "port", containerName, "5432/tcp")
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker port %s failed: %v\n%s", containerName, err, strings.TrimSpace(string(output)))
	}
	firstLine := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	_, port, splitErr := net.SplitHostPort(firstLine)
	if splitErr != nil {
		return "", fmt.Errorf("parse docker port output %q: %w", firstLine, splitErr)
	}
	return port, nil
}

func waitForPostgres(hostPort string) error {
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
	return fmt.Errorf("postgres container on port %s did not become ready in time", hostPort)
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func randomSuffix() string {
	buffer := make([]byte, 4)
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}
