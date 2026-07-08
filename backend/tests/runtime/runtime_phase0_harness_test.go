package runtime_test

import (
	"context"
	"fmt"
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
	managementconfigrules "github.com/coachpo/prism/backend/internal/httpapi/management/configrules"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

type runtimeSQLCategory string

const (
	runtimeSQLCategoryRuntimeStateTables   runtimeSQLCategory = "runtime_state_tables"
	runtimeSQLCategoryRoundRobinState      runtimeSQLCategory = "round_robin_state"
	runtimeSQLCategoryProxyKeyUsageWrite   runtimeSQLCategory = "proxy_key_usage_write"
	runtimeSQLCategoryPlanningSnapshotWarm runtimeSQLCategory = "planning_snapshot_warm"
	runtimeSQLCategoryAuthWarm             runtimeSQLCategory = "auth_warm"
)

type runtimeCapturedStatement struct {
	Raw        string
	Normalized string
}

type runtimeSQLSnapshot struct {
	statements []runtimeCapturedStatement
}

type runtimeSQLProbe struct {
	mu     sync.Mutex
	active *runtimeSQLSnapshot
}

type runtimePhase0Harness struct {
	*runtimeHarness
	settings     config.Settings
	statsService *managementstats.Service
	runtimeProbe *runtimeSQLProbe
}

type runtimePhase0HarnessOptions struct {
	SettingsMutator     func(*config.Settings)
	RuntimeOptions      runtimeapi.Options
	IncludeStatsService bool
}

func newRuntimePhase0Harness(tb testing.TB) *runtimePhase0Harness {
	tb.Helper()
	return newRuntimePhase0HarnessWithOptions(tb, runtimePhase0HarnessOptions{})
}

func newRuntimePhase0HarnessWithOptions(tb testing.TB, options runtimePhase0HarnessOptions) *runtimePhase0Harness {
	tb.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	databaseName := "runtime_phase0_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(tb, testContext, databaseName)
	tb.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	return newRuntimePhase0HarnessForDatabaseWithOptions(tb, databaseName, conn, options)
}

func restartRuntimePhase0Harness(tb testing.TB, databaseName string) *runtimePhase0Harness {
	tb.Helper()
	return restartRuntimePhase0HarnessWithOptions(tb, databaseName, runtimePhase0HarnessOptions{})
}

func restartRuntimePhase0HarnessWithOptions(tb testing.TB, databaseName string, options runtimePhase0HarnessOptions) *runtimePhase0Harness {
	tb.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn := connectDatabase(tb, testContext, sharedPostgresHarness.connectionString(databaseName))
	tb.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	return newRuntimePhase0HarnessForDatabaseWithOptions(tb, databaseName, conn, options)
}

func newRuntimePhase0HarnessForDatabaseWithOptions(tb testing.TB, databaseName string, conn *pgx.Conn, options runtimePhase0HarnessOptions) *runtimePhase0Harness {
	tb.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	startupService, err := startup.New(startup.Options{
		DatabaseURL:         sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey: "runtime-phase0-secret",
	})
	if err != nil {
		tb.Fatalf("build runtime phase-0 startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		tb.Fatalf("run runtime phase-0 startup service: %v", err)
	}

	settings := phase0RuntimeHarnessSettings(databaseName)
	if options.SettingsMutator != nil {
		options.SettingsMutator(&settings)
	}

	managementPool := newRuntimePhase0Pool(tb, settings.DatabaseURL, settings.ManagementDatabaseBudget(), nil)
	tb.Cleanup(managementPool.Close)

	runtimeProbe := &runtimeSQLProbe{}

	runtimePool := newRuntimePhase0Pool(tb, settings.DatabaseURL, settings.RuntimeDatabaseBudget(), runtimeProbe)
	tb.Cleanup(runtimePool.Close)
	runtimeTelemetryPool := newRuntimePhase0Pool(tb, settings.DatabaseURL, settings.RuntimeDatabaseBudget(), nil)
	tb.Cleanup(runtimeTelemetryPool.Close)
	runtimeFeedbackPool := newRuntimePhase0Pool(tb, settings.DatabaseURL, config.DefaultPostgresPoolsBudget().RuntimeFeedback, nil)
	tb.Cleanup(runtimeFeedbackPool.Close)
	cacheRefreshPool := newRuntimePhase0Pool(tb, settings.DatabaseURL, config.DefaultPostgresPoolsBudget().CacheRefresh, nil)
	tb.Cleanup(cacheRefreshPool.Close)

	managementAuthService, err := managementauth.NewService(settings, managementauth.Options{Pool: managementPool})
	if err != nil {
		tb.Fatalf("build phase-0 management auth service: %v", err)
	}
	tb.Cleanup(managementAuthService.Close)

	runtimePlanningCache := options.RuntimeOptions.Cache
	if runtimePlanningCache == nil {
		runtimePlanningCache = runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: cacheRefreshPool, SecretEncryptionKey: settings.SecretEncryptionKey})
	} else {
		runtimePlanningCache.Configure(runtimeapi.SharedCacheOptions{RefreshPool: cacheRefreshPool, SecretEncryptionKey: settings.SecretEncryptionKey})
	}
	if err := runtimePlanningCache.Bootstrap(testContext); err != nil {
		tb.Fatalf("bootstrap phase-0 published runtime snapshot: %v", err)
	}
	runtimeAuthCache := managementauth.NewRuntimeCacheFromShared(runtimePlanningCache)
	runtimeAuthService, err := managementauth.NewService(settings, managementauth.Options{Pool: runtimePool, RuntimeCache: runtimeAuthCache})
	if err != nil {
		tb.Fatalf("build phase-0 runtime auth service: %v", err)
	}
	tb.Cleanup(runtimeAuthService.Close)

	configRulesService, err := managementconfigrules.NewService(settings, managementconfigrules.Options{Pool: managementPool})
	if err != nil {
		tb.Fatalf("build phase-0 config rules service: %v", err)
	}
	tb.Cleanup(configRulesService.Close)

	var statsService *managementstats.Service
	if options.IncludeStatsService {
		statsService, err = managementstats.NewService(settings, managementstats.Options{Pool: managementPool})
		if err != nil {
			tb.Fatalf("build phase-0 stats service: %v", err)
		}
		tb.Cleanup(statsService.Close)
	}

	runtimeOptions := options.RuntimeOptions
	runtimeOptions.ExecutionPool = runtimePool
	runtimeOptions.TelemetryPool = runtimeTelemetryPool
	runtimeOptions.FeedbackPool = runtimeFeedbackPool
	runtimeOptions.Cache = runtimePlanningCache

	runtimeService, err := runtimeapi.NewService(settings, runtimeOptions)
	if err != nil {
		tb.Fatalf("build phase-0 runtime service: %v", err)
	}
	tb.Cleanup(runtimeService.Close)

	upstream := newUpstreamRecorder(tb)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{
		Version:            "runtime-phase0-test",
		AuthService:        managementAuthService,
		RuntimeAuthService: runtimeAuthService,
		ConfigRulesService: configRulesService,
		RuntimeService:     runtimeService,
		RuntimeCache:       runtimePlanningCache,
		StatsService:       statsService,
	})
	if err != nil {
		tb.Fatalf("build phase-0 runtime handler: %v", err)
	}

	server := httptest.NewServer(handler)
	tb.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		tb.Fatalf("create phase-0 runtime cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar

	return &runtimePhase0Harness{
		runtimeHarness: &runtimeHarness{
			databaseName:   databaseName,
			client:         client,
			conn:           conn,
			authService:    managementAuthService,
			runtimeService: runtimeService,
			runtimeCache:   runtimePlanningCache,
			server:         server,
			url:            server.URL,
			upstream:       upstream,
		},
		settings:     settings,
		statsService: statsService,
		runtimeProbe: runtimeProbe,
	}
}

func phase0RuntimeHarnessSettings(databaseName string) config.Settings {
	return config.Settings{
		Host:        "127.0.0.1",
		Port:        8000,
		AppEnv:      config.EnvironmentProduction,
		DatabaseURL: sharedPostgresHarness.connectionString(databaseName),

		RuntimeTelemetryMode:       config.RuntimeTelemetryModeSynchronous,
		SecretEncryptionKey:        "runtime-phase0-secret",
		CORSAllowedOrigins:         "http://localhost:5173,http://127.0.0.1:5173",
		AuthJWTSecret:              "runtime-phase0-jwt-secret",
		AuthAccessTokenTTLSeconds:  900,
		AuthRefreshTokenTTLSeconds: 604800,
		AuthResetCodeTTLSeconds:    600,
		AuthCookieName:             "prism_access_token",
		AuthRefreshCookieName:      "prism_refresh_token",
		AuthCookieSecure:           false,
	}
}

func usePhase0ManagementIsolationSettings(settings *config.Settings) {
	settings.RuntimeDatabasePoolBudget = config.DatabasePoolBudget{MaxConns: 1, MinIdleConns: 0}
	settings.ManagementDatabasePoolBudget = config.DatabasePoolBudget{MaxConns: 1, MinIdleConns: 0}
	settings.ManagementAdmissionControlBudget = config.ManagementAdmissionBudget{M2MaxConcurrent: 1, M3MaxConcurrent: 1}
}

func newRuntimePhase0Pool(tb testing.TB, databaseURL string, budget config.DatabasePoolBudget, tracer pgx.QueryTracer) *pgxpool.Pool {
	tb.Helper()
	parsedConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		tb.Fatalf("parse phase-0 pgx pool config: %v", err)
	}
	parsedConfig.MaxConns = budget.MaxConns
	parsedConfig.MinIdleConns = budget.MinIdleConns

	if tracer != nil {
		parsedConfig.ConnConfig.Tracer = tracer
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), parsedConfig)
	if err != nil {
		tb.Fatalf("create phase-0 pgx pool: %v", err)
	}
	return pool
}

func (probe *runtimeSQLProbe) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	probe.mu.Lock()
	if probe.active != nil {
		probe.active.statements = append(probe.active.statements, runtimeCapturedStatement{
			Raw:        strings.TrimSpace(data.SQL),
			Normalized: normalizeCapturedSQL(data.SQL),
		})
	}
	probe.mu.Unlock()
	return ctx
}

func (*runtimeSQLProbe) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (probe *runtimeSQLProbe) Begin() {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	probe.active = &runtimeSQLSnapshot{}
}

func (probe *runtimeSQLProbe) Finish() runtimeSQLSnapshot {

	probe.mu.Lock()
	defer probe.mu.Unlock()
	if probe.active == nil {
		return runtimeSQLSnapshot{}
	}
	snapshot := runtimeSQLSnapshot{statements: append([]runtimeCapturedStatement(nil), probe.active.statements...)}
	probe.active = nil
	return snapshot
}

func normalizeCapturedSQL(raw string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(raw))), " ")
}

func runtimePhase0GeminiRequest(prompt string) map[string]any {
	return map[string]any{
		"contents": []map[string]any{{
			"role":  "user",
			"parts": []map[string]any{{"text": prompt}},
		}},
	}
}

func (snapshot runtimeSQLSnapshot) statementsForCategory(category runtimeSQLCategory) []string {
	matches := make([]string, 0)
	for _, statement := range snapshot.statements {
		if matchesRuntimeSQLCategory(statement.Normalized, category) {
			matches = append(matches, statement.Raw)
		}
	}
	return matches
}

func (snapshot runtimeSQLSnapshot) dump() string {
	if len(snapshot.statements) == 0 {
		return "<no captured runtime SQL>"
	}
	var builder strings.Builder
	for index, statement := range snapshot.statements {
		fmt.Fprintf(&builder, "\n[%d] %s", index+1, statement.Raw)
	}
	return strings.TrimPrefix(builder.String(), "\n")
}

func (snapshot runtimeSQLSnapshot) assertContainsCategory(tb testing.TB, category runtimeSQLCategory) {
	tb.Helper()
	matches := snapshot.statementsForCategory(category)
	if len(matches) == 0 {
		tb.Fatalf("expected captured runtime SQL to include %s, got:\n%s", category, snapshot.dump())
	}
}

func (snapshot runtimeSQLSnapshot) assertExcludesCategory(tb testing.TB, category runtimeSQLCategory) {
	tb.Helper()
	matches := snapshot.statementsForCategory(category)
	if len(matches) != 0 {
		tb.Fatalf("expected captured runtime SQL to exclude %s, got matches %+v\nfull capture:\n%s", category, matches, snapshot.dump())
	}
}

func matchesRuntimeSQLCategory(normalized string, category runtimeSQLCategory) bool {
	switch category {

	case runtimeSQLCategoryRuntimeStateTables:
		return containsAnyTable(normalized, "routing_connection_runtime_state", "routing_connection_runtime_leases")
	case runtimeSQLCategoryRoundRobinState:
		return containsAnyTable(normalized, "loadbalance_round_robin_state")
	case runtimeSQLCategoryProxyKeyUsageWrite:
		return strings.Contains(normalized, "update proxy_api_keys set") && strings.Contains(normalized, "last_used_at")
	case runtimeSQLCategoryPlanningSnapshotWarm:
		return containsAnyTable(
			normalized,
			"profiles",
			"model_configs",
			"model_proxy_targets",
			"loadbalance_strategies",
			"connections",
			"header_blocklist_rules",
			"user_settings",
			"vendors",
			"pricing_templates",
			"endpoint_fx_rate_settings",
			"endpoints",
		)
	case runtimeSQLCategoryAuthWarm:
		return strings.Contains(normalized, "from app_auth_settings") ||
			(strings.Contains(normalized, "from proxy_api_keys") && strings.Contains(normalized, "select"))
	default:
		return false
	}
}

func containsAnyTable(normalized string, tables ...string) bool {
	for _, table := range tables {
		if strings.Contains(normalized, table) {
			return true
		}
	}

	return false
}

func (h *runtimePhase0Harness) captureJSONRequest(tb testing.TB, method string, path string, body any, headers map[string]string) (*http.Response, runtimeSQLSnapshot) {
	tb.Helper()
	h.runtimeProbe.Begin()
	response := h.requestJSON(tb, method, path, body, headers)
	_ = readResponseBody(tb, response)
	snapshot := h.runtimeProbe.Finish()
	return response, snapshot
}

func (h *runtimePhase0Harness) seedStatsPressureHistory(t *testing.T, profileID int, suffix string) {
	t.Helper()
	if h.statsService == nil {
		t.Fatal("phase-0 stats service is required for pressure history seeding")
	}
	route := h.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "stats-pressure-public-" + suffix + "-" + randomSuffix(),
		TargetModelID:   "stats-pressure-target-" + suffix + "-" + randomSuffix(),
		EndpointBaseURL: h.upstream.baseURL("/stats-pressure/" + suffix),
		EndpointAPIKey:  "stats-pressure-key",
	})
	for index := range 6 {
		response := h.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"messages": []map[string]any{{"role": "user", "content": fmt.Sprintf("stats pressure %d", index)}},
			"model":    route.PublicModelID,
		}, nil)
		assertStatus(t, response, http.StatusOK)
	}
	waitForRuntimeTelemetryCounts(t, h.conn, profileID, runtimeTelemetryCounts{RequestLogs: 6, UsageEvents: 6, OutboxRows: 0}, 5*time.Second)
}
