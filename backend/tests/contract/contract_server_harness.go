package contracttest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

type contractHarness struct {
	client         *http.Client
	conn           *pgx.Conn
	dsn            string
	server         *httptest.Server
	service        *managementauth.Service
	runtimeService *runtimeapi.Service
	runtimeCache   *runtimeapi.SharedCache
	url            string
}

type contractHarnessOptions struct {
	DatabaseURL         string
	SecretEncryptionKey string
	Version             string
	SettingsMutator     func(*config.Settings)
	DependenciesBuilder func(t *testing.T, ctx context.Context, harness *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies
}

func newContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	return newContractHarnessWithDatabase(t, "")
}

func contractAuthSettings() config.Settings {
	return config.Settings{
		Host:                       "127.0.0.1",
		Port:                       8000,
		AppEnv:                     config.EnvironmentProduction,
		SecretEncryptionKey:        "contract-secret",
		CORSAllowedOrigins:         "http://localhost:5173,http://127.0.0.1:5173",
		AuthJWTSecret:              "contract-jwt-secret",
		AuthAccessTokenTTLSeconds:  900,
		AuthRefreshTokenTTLSeconds: 604800,
		AuthCookieName:             "prism_access_token",
		AuthRefreshCookieName:      "prism_refresh_token",
		AuthCookieSecure:           false,
		ManagementDatabasePoolBudget: config.DatabasePoolBudget{
			MaxConns: 12,
		},
		ManagementAdmissionControlBudget: config.ManagementAdmissionBudget{
			M2MaxConcurrent: 3,
			M3MaxConcurrent: 2,
		},
	}
}

func newContractHarnessWithDatabase(t *testing.T, dsn string) *contractHarness {
	t.Helper()
	version := "contract-test"
	if dsn != "" {
		version = "contract-restart-test"
	}
	return newContractHarnessFor(t, "contract", contractHarnessOptions{
		DatabaseURL:         dsn,
		SecretEncryptionKey: "contract-secret",
		Version:             version,
		SettingsMutator: func(settings *config.Settings) {
			authSettings := contractAuthSettings()
			authSettings.DatabaseURL = settings.DatabaseURL
			*settings = authSettings
		},
		DependenciesBuilder: func(t *testing.T, testContext context.Context, harness *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies {
			t.Helper()
			runtimeCache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
			if err := runtimeCache.Bootstrap(testContext); err != nil {
				t.Fatalf("bootstrap published runtime snapshot: %v", err)
			}
			runtimeAuthCache := managementauth.NewRuntimeCacheFromShared(runtimeCache)
			startupRuntime, err := platformhttp.NewStartupConfigRuntime(settings)
			if err != nil {
				t.Fatalf("build startup config runtime: %v", err)
			}
			authService, err := managementauth.NewService(settings, managementauth.Options{
				CORSOriginProvider:        startupRuntime,
				AuthRuntimeConfigProvider: startupRuntime,
				Pool:                      pool,
				RuntimeCache:              runtimeAuthCache,
			})
			if err != nil {
				t.Fatalf("build auth service: %v", err)
			}
			t.Cleanup(authService.Close)
			harness.service = authService
			harness.runtimeCache = runtimeCache
			return platformhttp.Dependencies{
				AuthService:          authService,
				RuntimeAuthService:   authService,
				RuntimeCache:         runtimeCache,
				StartupConfigRuntime: startupRuntime,
			}
		},
	})
}

func newContractHarnessForExistingDatabase(t *testing.T, dsn string) *contractHarness {
	t.Helper()
	return newContractHarnessWithDatabase(t, dsn)
}

func newContractHarnessFor(t *testing.T, prefix string, opts contractHarnessOptions) *contractHarness {
	t.Helper()
	if opts.DependenciesBuilder == nil {
		t.Fatal("contract harness requires dependencies builder")
	}
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	dsn := opts.DatabaseURL
	databaseName := ""
	if dsn == "" {
		databaseName = prefix + "_" + randomSuffix()
		dsn = sharedPostgresHarness.connectionString(databaseName)
	}
	var conn *pgx.Conn
	if opts.DatabaseURL == "" {
		conn = sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	} else {
		conn = connectDatabase(t, testContext, dsn)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})
	if opts.DatabaseURL == "" {
		startupService, err := startup.New(startup.Options{
			DatabaseURL:         dsn,
			SecretEncryptionKey: opts.SecretEncryptionKey,
		})
		if err != nil {
			t.Fatalf("build startup service: %v", err)
		}
		if _, err := startupService.RunWithConn(testContext, conn); err != nil {
			t.Fatalf("run startup service: %v", err)
		}
	}
	harness := &contractHarness{conn: conn, dsn: dsn}
	settings := config.Settings{
		Host:                "127.0.0.1",
		Port:                8000,
		AppEnv:              config.EnvironmentProduction,
		DatabaseURL:         dsn,
		SecretEncryptionKey: opts.SecretEncryptionKey,
		CORSAllowedOrigins:  "http://localhost:5173,http://127.0.0.1:5173",
	}
	if opts.SettingsMutator != nil {
		opts.SettingsMutator(&settings)
	}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	dependencies := opts.DependenciesBuilder(t, testContext, harness, settings, pool)
	if dependencies.Version == "" {
		dependencies.Version = opts.Version
	}
	handler, err := platformhttp.NewHandlerWithDependencies(settings, dependencies)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar
	harness.client = client
	harness.server = server
	harness.url = server.URL
	return harness
}

func (h *contractHarness) refreshRuntimeSnapshot(t *testing.T, request runtimeapi.RefreshRequest) {
	t.Helper()
	if h == nil || h.runtimeCache == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.runtimeCache.RefreshNow(ctx, request); err != nil {
		t.Fatalf("refresh published runtime snapshot: %v", err)
	}
}

func (h *contractHarness) newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := h.server.Client()
	client.Jar = jar
	return client
}

func (h *contractHarness) requestJSON(t *testing.T, client *http.Client, method string, path string, body any, headers map[string]string) *http.Response {
	t.Helper()
	if bodyMap, ok := body.(map[string]any); ok {
		h.ensureOpenAIAcceptedFormat(t, method, path, bodyMap)
	}
	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		requestBody = bytes.NewReader(raw)
	}
	return h.requestRaw(t, client, method, path, requestBody, body != nil, headers)
}

func (h *contractHarness) requestJSONRaw(t *testing.T, client *http.Client, method string, path string, rawBody string, headers map[string]string) *http.Response {
	t.Helper()
	return h.requestRaw(t, client, method, path, bytes.NewReader([]byte(rawBody)), true, headers)
}

func (h *contractHarness) requestRaw(t *testing.T, client *http.Client, method string, path string, requestBody *bytes.Reader, contentType bool, headers map[string]string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, h.url+path, requestBody)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if contentType {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("perform request %s %s: %v", method, path, err)
	}
	t.Cleanup(func() {
		_ = response.Body.Close()
	})
	return response
}

func (h *contractHarness) ensureOpenAIAcceptedFormat(t *testing.T, method string, path string, body map[string]any) {
	t.Helper()
	if _, ok := body["openai_accepted_format"]; ok {
		return
	}
	if apiFamily, ok := body["api_family"].(string); ok {
		if apiFamily == "openai" && !strings.Contains(path, "/verify") {
			body["openai_accepted_format"] = "dual_native"
		}
		return
	}
	if method != http.MethodPut && method != http.MethodPatch {
		return
	}
	const modelPathPrefix = "/api/models/"
	if !strings.HasPrefix(path, modelPathPrefix) {
		return
	}
	// Only the bare PUT/PATCH /api/models/{id} model-update endpoint wants
	// this injection. Every sub-route (targets, connections, catalog, pi,
	// ...) has its own request shape and must never have an unrelated field
	// silently added to its body: Sscanf("%d", ...) on a longer path like
	// "5/pi/override" would otherwise happily parse "5" and stop, ignoring
	// the trailing "/pi/override" it never actually matched.
	remainder := strings.TrimPrefix(path, modelPathPrefix)
	if strings.Contains(remainder, "/") {
		return
	}
	var modelID int
	if _, err := fmt.Sscanf(remainder, "%d", &modelID); err != nil {
		return
	}
	var apiFamily string
	if err := h.conn.QueryRow(context.Background(), `SELECT api_family FROM model_configs WHERE id = $1`, modelID).Scan(&apiFamily); err != nil {
		return
	}
	if apiFamily == "openai" {
		body["openai_accepted_format"] = "dual_native"
	}
}
