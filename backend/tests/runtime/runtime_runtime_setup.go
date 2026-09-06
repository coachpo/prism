package runtimetest

import (
	"context"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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

const runtimeHarnessSecretEncryptionKey = "runtime-secret"

func newRuntimeHarnessForDatabaseWithConfig(tb testing.TB, databaseName string, conn *pgx.Conn, harnessConfig runtimeHarnessConfig) *runtimeHarness {
	tb.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	startupService, err := startup.New(startup.Options{
		DatabaseURL:         sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey: runtimeHarnessSecretEncryptionKey,
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
		SecretEncryptionKey:        runtimeHarnessSecretEncryptionKey,
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
	modelsService, err := managementmodels.NewService(settings, managementmodels.Options{Pool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
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
