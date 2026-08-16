package contracttest

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
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

type contractHarness struct {
	client         *http.Client
	conn           *pgx.Conn
	dsn            string
	server         *httptest.Server
	service        *managementauth.Service
	runtimeService *runtimeapi.Service
	runtimeCache   *runtimeapi.SharedCache
	hotRuntime     *platformhttp.HotBootstrapConfigRuntime
	url            string
}

type contractHarnessOptions struct {
	DatabaseURL         string
	SecretEncryptionKey string
	Version             string
	SettingsMutator     func(*config.Settings)
	DependenciesBuilder func(t *testing.T, ctx context.Context, harness *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies
}

type refreshTokenSnapshot struct {
	ID            int
	RotatedFromID *int
	RevokedAt     *time.Time
	LastUsedAt    *time.Time
}

type loginThrottleSnapshot struct {
	SubjectKey    string
	RemoteAddress string
	FailureCount  int
	LockedUntil   *time.Time
}

type loginAttemptResult struct {
	Status int
	Body   string
	Err    error
}

type appAuthSettingsRecord struct {
	ID           int
	AuthEnabled  bool
	Username     *string
	Email        *string
	PendingEmail *string
	PasswordHash *string
	EmailBoundAt *time.Time
	TokenVersion int
}

type proxyKeySnapshot struct {
	ID            int
	Name          string
	KeyPrefix     string
	IsActive      bool
	ExpiresAt     *time.Time
	LastUsedAt    *time.Time
	LastUsedIP    *string
	CreatedByID   *int
	Notes         *string
	RotatedAt     *time.Time
	RotationCount int
	CreatedAt     time.Time
}

func startSharedPostgresHarness() (testPostgresHarness, error) {
	containerName := testDockerContainerName("postgres")
	if err := runDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
		return testPostgresHarness{}, err
	}
	hostPort, err := dockerPort(containerName)
	if err != nil {
		return testPostgresHarness{}, err
	}
	if err := waitForPostgres(hostPort); err != nil {
		return testPostgresHarness{}, err
	}
	return testPostgresHarness{containerName: containerName, hostPort: hostPort}, nil
}

// testDockerContainerPrefix returns the branch-scoped prefix used for shared
// Docker resources. On shared machines, tests from different worktrees/branches
// must not collide on container names, so the current git branch (sanitized for
// Docker name rules) is embedded in every container name. An explicit
// PRISM_TEST_DOCKER_PREFIX environment variable overrides the branch.
func testDockerContainerPrefix() string {
	if explicit := strings.TrimSpace(os.Getenv("PRISM_TEST_DOCKER_PREFIX")); explicit != "" {
		return sanitizeDockerNameComponent(explicit)
	}
	command := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	output, err := command.Output()
	if err != nil {
		return "unknown"
	}
	branch := strings.TrimSpace(string(output))
	return sanitizeDockerNameComponent(branch)
}

func sanitizeDockerNameComponent(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-_")
}

// testDockerContainerName builds a Docker container name scoped by the
// branch prefix (or PRISM_TEST_DOCKER_PREFIX) plus a random suffix.
func testDockerContainerName(role string) string {
	return fmt.Sprintf("prism-%s-%s-%s", sanitizeDockerNameComponent(role), testDockerContainerPrefix(), randomSuffix())
}

func (h testPostgresHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := connectDatabase(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+quoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return connectDatabase(t, ctx, h.connectionString(databaseName))
}

func (h testPostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
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
			hotRuntime, err := platformhttp.NewHotBootstrapConfigRuntime(settings)
			if err != nil {
				t.Fatalf("build hot bootstrap runtime: %v", err)
			}
			authService, err := managementauth.NewService(settings, managementauth.Options{
				CORSOriginProvider:        hotRuntime,
				AuthRuntimeConfigProvider: hotRuntime,
				Pool:                      pool,
				RuntimeCache:              runtimeAuthCache,
			})
			if err != nil {
				t.Fatalf("build auth service: %v", err)
			}
			t.Cleanup(authService.Close)
			harness.service = authService
			harness.runtimeCache = runtimeCache
			harness.hotRuntime = hotRuntime
			return platformhttp.Dependencies{
				AuthService:               authService,
				RuntimeAuthService:        authService,
				RuntimeCache:              runtimeCache,
				HotBootstrapConfigRuntime: hotRuntime,
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

func performLoginAttempt(harness *contractHarness, client *http.Client, username string, password string) loginAttemptResult {
	payload, err := json.Marshal(map[string]any{"username": username, "password": password, "session_duration": "7_days"})
	if err != nil {
		return loginAttemptResult{Err: fmt.Errorf("marshal login attempt: %w", err)}
	}
	request, err := http.NewRequest(http.MethodPost, harness.url+"/api/auth/login", bytes.NewReader(payload))
	if err != nil {
		return loginAttemptResult{Err: fmt.Errorf("build login attempt: %w", err)}
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return loginAttemptResult{Err: fmt.Errorf("perform login attempt: %w", err)}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return loginAttemptResult{Err: fmt.Errorf("read login attempt response: %w", err)}
	}
	return loginAttemptResult{Status: response.StatusCode, Body: strings.TrimSpace(string(body))}
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
		if apiFamily == "openai" {
			body["openai_accepted_format"] = "dual_native"
		}
		return
	}
	if method != http.MethodPut && method != http.MethodPatch {
		return
	}
	const modelPathPrefix = "/api/models/"
	if !strings.HasPrefix(path, modelPathPrefix) || strings.Contains(path, "/targets") || strings.Contains(path, "/connections") {
		return
	}
	var modelID int
	if _, err := fmt.Sscanf(strings.TrimPrefix(path, modelPathPrefix), "%d", &modelID); err != nil {
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

func putAuthSettingsV2(t *testing.T, harness *contractHarness, desiredEnabled bool, accountChange map[string]any, acknowledgements map[string]any) *http.Response {
	t.Helper()
	getResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth", nil, nil)
	assertStatus(t, getResponse, http.StatusOK)
	var parsed struct {
		Revision string `json:"revision"`
		AuthMode struct {
			Effective string `json:"effective"`
		} `json:"auth_mode"`
		ProxyKeyReadiness struct {
			ReadinessGeneration string `json:"readiness_generation"`
		} `json:"proxy_key_readiness"`
	}
	if err := json.Unmarshal([]byte(readResponseBody(t, getResponse)), &parsed); err != nil {
		t.Fatalf("decode auth settings response: %v", err)
	}
	body := map[string]any{
		"operation_id":         "contract-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"expected_revision":    parsed.Revision,
		"desired_auth_enabled": desiredEnabled,
		"account_change":       accountChange,
	}
	if desiredEnabled && parsed.AuthMode.Effective != "enabled" {
		body["expected_proxy_key_readiness_generation"] = parsed.ProxyKeyReadiness.ReadinessGeneration
	}
	if acknowledgements != nil {
		body["acknowledgements"] = acknowledgements
	}
	return harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/auth", body, nil)
}

func enableVerifiedAuth(t *testing.T, harness *contractHarness, username string, password string, _ string) {
	t.Helper()
	// New auth contract (Settings SPEC §8.2): staged immutable config version
	// + acknowledgements; enabling without activation-safe keys requires the
	// zero-key acknowledgement.
	getResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth", nil, nil)
	assertStatus(t, getResponse, http.StatusOK)
	getPayload := readResponseBody(t, getResponse)
	var parsed struct {
		Revision          string `json:"revision"`
		ProxyKeyReadiness struct {
			ReadinessGeneration string `json:"readiness_generation"`
		} `json:"proxy_key_readiness"`
	}
	if err := json.Unmarshal([]byte(getPayload), &parsed); err != nil {
		t.Fatalf("decode auth settings response: %v", err)
	}
	revision := parsed.Revision
	readinessGeneration := parsed.ProxyKeyReadiness.ReadinessGeneration
	body := map[string]any{
		"operation_id":      "contract-enable-auth",
		"expected_revision": revision,
		"expected_proxy_key_readiness_generation": readinessGeneration,
		"desired_auth_enabled":                    true,
		"account_change": map[string]any{
			"kind":         "update",
			"username":     username,
			"new_password": password,
		},
		"acknowledgements": map[string]any{
			"enable_without_active_proxy_keys": true,
		},
	}
	enableResponse := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/auth", body, nil)
	if enableResponse.StatusCode != http.StatusOK {
		t.Logf("enable PUT status=%d body=%s", enableResponse.StatusCode, readResponseBody(t, enableResponse))
	}
	assertStatus(t, enableResponse, http.StatusOK)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{Auth: true})
}

func loginWithVerifiedAuth(t *testing.T, harness *contractHarness, username string, password string, email string) {
	t.Helper()
	seedVerifiedAuthSettings(t, harness, username, password, email)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{Auth: true})
	loginResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/auth/login", map[string]any{"username": username, "password": password, "session_duration": "7_days"}, nil)
	assertStatus(t, loginResponse, http.StatusOK)
}

func seedVerifiedAuthSettings(t *testing.T, harness *contractHarness, username string, password string, _ string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password for test seed: %v", err)
	}
	now := time.Now().UTC()
	// The finalizer removed the in-place credential columns; seeding uses the
	// immutable auth_config_versions pointer (Settings SPEC §14.1 item 11).
	var configID int64
	if err := harness.conn.QueryRow(
		context.Background(),
		`INSERT INTO auth_config_versions (
			subject_key, generation, desired_mode, username, password_hash,
			session_version, state, created_operation_id, created_at, updated_at
		) VALUES ('app', 'contract-seed', 'enabled', $1, $2, 0, 'effective', NULL, $3, $3)
		RETURNING id`,
		username,
		string(hash),
		now,
	).Scan(&configID); err != nil {
		t.Fatalf("insert seeded auth config version: %v", err)
	}
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE app_auth_settings
		SET desired_config_version_id = $1,
			effective_config_version_id = $1,
			desired_generation = 'contract-seed',
			effective_generation = 'contract-seed',
			auth_revision = auth_revision + 1,
			updated_at = $2
		WHERE singleton_key = 'app'`,
		configID,
		now,
	); err != nil {
		t.Fatalf("seed verified auth settings pointer: %v", err)
	}
}

func loadRefreshTokens(t *testing.T, harness *contractHarness) []refreshTokenSnapshot {
	t.Helper()
	rows, err := harness.conn.Query(context.Background(), `SELECT id, rotated_from_id, revoked_at, last_used_at FROM refresh_tokens ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("query refresh tokens: %v", err)
	}
	defer rows.Close()
	var snapshots []refreshTokenSnapshot
	for rows.Next() {
		var rotatedFromID sqlNullInt32
		var revokedAt sqlNullTime
		var lastUsedAt sqlNullTime
		var snapshot refreshTokenSnapshot
		if err := rows.Scan(&snapshot.ID, &rotatedFromID, &revokedAt, &lastUsedAt); err != nil {
			t.Fatalf("scan refresh token: %v", err)
		}
		snapshot.RotatedFromID = rotatedFromID.ptr()
		snapshot.RevokedAt = revokedAt.ptr()
		snapshot.LastUsedAt = lastUsedAt.ptr()
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate refresh tokens: %v", err)
	}
	return snapshots
}

func loadLoginThrottleEntries(t *testing.T, harness *contractHarness) []loginThrottleSnapshot {
	t.Helper()
	rows, err := harness.conn.Query(context.Background(), `SELECT subject_key, remote_address, failure_count, locked_until FROM login_throttle_ledger ORDER BY subject_key, remote_address`)
	if err != nil {
		t.Fatalf("query login throttle ledger: %v", err)
	}
	defer rows.Close()
	var snapshots []loginThrottleSnapshot
	for rows.Next() {
		var lockedUntil sqlNullTime
		var snapshot loginThrottleSnapshot
		if err := rows.Scan(&snapshot.SubjectKey, &snapshot.RemoteAddress, &snapshot.FailureCount, &lockedUntil); err != nil {
			t.Fatalf("scan login throttle ledger: %v", err)
		}
		snapshot.LockedUntil = lockedUntil.ptr()
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate login throttle ledger: %v", err)
	}
	return snapshots
}

func loadAppAuthSettings(t *testing.T, harness *contractHarness) appAuthSettingsRecord {
	t.Helper()
	var username, passwordHash sqlNullString
	var snapshot appAuthSettingsRecord
	var configMode *string
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT a.id, v.desired_mode, v.username, v.password_hash, v.session_version FROM app_auth_settings AS a
		 LEFT JOIN auth_config_versions AS v ON v.id = a.effective_config_version_id
		 WHERE a.singleton_key = 'app'`,
	).Scan(
		&snapshot.ID,
		&configMode,
		&username,
		&passwordHash,
		&snapshot.TokenVersion,
	); err != nil {
		t.Fatalf("query app auth settings: %v", err)
	}
	if configMode != nil {
		snapshot.AuthEnabled = *configMode == "enabled"
	}
	snapshot.Username = username.ptr()
	snapshot.PasswordHash = passwordHash.ptr()
	return snapshot
}

func loadProxyKeys(t *testing.T, harness *contractHarness) []proxyKeySnapshot {
	t.Helper()
	rows, err := harness.conn.Query(
		context.Background(),
		`SELECT id, name, key_prefix, is_active, expires_at, last_used_at, last_used_ip, created_by_auth_subject_id, notes, rotated_at, rotation_count, created_at FROM proxy_api_keys ORDER BY id ASC`,
	)
	if err != nil {
		t.Fatalf("query proxy keys: %v", err)
	}
	defer rows.Close()
	var snapshots []proxyKeySnapshot
	for rows.Next() {
		var expiresAt, lastUsedAt, rotatedAt sqlNullTime
		var lastUsedIP, notes sqlNullString
		var createdByID sqlNullInt32
		var snapshot proxyKeySnapshot
		if err := rows.Scan(&snapshot.ID, &snapshot.Name, &snapshot.KeyPrefix, &snapshot.IsActive, &expiresAt, &lastUsedAt, &lastUsedIP, &createdByID, &notes, &rotatedAt, &snapshot.RotationCount, &snapshot.CreatedAt); err != nil {
			t.Fatalf("scan proxy key: %v", err)
		}
		snapshot.ExpiresAt = expiresAt.ptr()
		snapshot.LastUsedAt = lastUsedAt.ptr()
		snapshot.LastUsedIP = lastUsedIP.ptr()
		snapshot.CreatedByID = createdByID.ptr()
		snapshot.Notes = notes.ptr()
		snapshot.RotatedAt = rotatedAt.ptr()
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate proxy keys: %v", err)
	}
	return snapshots
}

func loadProxyKeyByPrefix(t *testing.T, harness *contractHarness, keyPrefix string) proxyKeySnapshot {
	t.Helper()
	for _, snapshot := range loadProxyKeys(t, harness) {
		if snapshot.KeyPrefix == keyPrefix {
			return snapshot
		}
	}
	t.Fatalf("query proxy key by prefix: %s not found", keyPrefix)
	return proxyKeySnapshot{}
}

type sqlNullString struct{ sql *string }
type sqlNullTime struct{ time *time.Time }
type sqlNullInt32 struct{ value *int }

func (value *sqlNullString) Scan(src any) error {
	if src == nil {
		value.sql = nil
		return nil
	}
	switch typed := src.(type) {
	case string:
		value.sql = stringPtr(typed)
		return nil
	case []byte:
		value.sql = stringPtr(string(typed))
		return nil
	default:
		return fmt.Errorf("unsupported string scan type %T", src)
	}
}

func (value sqlNullString) ptr() *string { return value.sql }

func (value *sqlNullTime) Scan(src any) error {
	if src == nil {
		value.time = nil
		return nil
	}
	typed, ok := src.(time.Time)
	if !ok {
		return fmt.Errorf("unsupported time scan type %T", src)
	}
	utc := typed.UTC()
	value.time = &utc
	return nil
}

func (value sqlNullTime) ptr() *time.Time { return value.time }

func (value *sqlNullInt32) Scan(src any) error {
	if src == nil {
		value.value = nil
		return nil
	}
	switch typed := src.(type) {
	case int32:
		converted := int(typed)
		value.value = &converted
		return nil
	case int64:
		converted := int(typed)
		value.value = &converted
		return nil
	case int:
		converted := typed
		value.value = &converted
		return nil
	default:
		return fmt.Errorf("unsupported int scan type %T", src)
	}
}

func (value sqlNullInt32) ptr() *int { return value.value }

func assertStatus(t *testing.T, response *http.Response, want int) {
	t.Helper()
	if response.StatusCode != want {
		body := readResponseBody(t, response)
		t.Fatalf("expected status %d, got %d with body %s", want, response.StatusCode, body)
	}
}

func assertErrorResponse(t *testing.T, response *http.Response, wantStatus int, wantDetail string) {
	t.Helper()
	assertStatus(t, response, wantStatus)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	detail, ok := payload["detail"].(string)
	if !ok {
		t.Fatalf("expected error detail string, got %+v", payload)
	}
	if detail != wantDetail {
		t.Fatalf("expected error detail %q, got %+v", wantDetail, payload)
	}
}

func assertErrorResponseCode(t *testing.T, response *http.Response, wantStatus int, wantCode string, wantDetail string) {
	t.Helper()
	assertStatus(t, response, wantStatus)
	var payload map[string]string
	decodeJSONResponse(t, response, &payload)
	if payload["code"] != wantCode || payload["detail"] != wantDetail {
		t.Fatalf("expected error code/detail %q/%q, got %+v", wantCode, wantDetail, payload)
	}
}

// assertAuthProblemResponse asserts the flat management problem envelope for
// registered auth codes: exact status, exact code, exact empty params and a
// request_id present.
func assertAuthProblemResponse(t *testing.T, response *http.Response, wantStatus int, wantCode string) {
	t.Helper()
	assertStatus(t, response, wantStatus)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["code"] != wantCode {
		t.Fatalf("expected auth problem code %q, got %+v", wantCode, payload)
	}
	params, ok := payload["params"].(map[string]any)
	if !ok || len(params) != 0 {
		t.Fatalf("expected auth problem params to be exact empty object, got %+v", payload["params"])
	}
	requestID, ok := payload["request_id"].(string)
	if !ok || requestID == "" {
		t.Fatalf("expected auth problem request_id, got %+v", payload)
	}
}

// assertLoginLockedProblem asserts the auth_login_locked envelope: Retry-After
// header and details.retry_at/retry_after_seconds must be present and the
// header delta must equal the body delta.
func assertLoginLockedProblem(t *testing.T, response *http.Response) {
	t.Helper()
	assertAuthProblemResponse(t, response, http.StatusTooManyRequests, "auth_login_locked")
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	details, ok := payload["details"].(map[string]any)
	if !ok {
		t.Fatalf("expected auth_login_locked details object, got %+v", payload["details"])
	}
	retryAt, ok := details["retry_at"].(string)
	if !ok || retryAt == "" {
		t.Fatalf("expected auth_login_locked details.retry_at, got %+v", details)
	}
	retryAfter, ok := details["retry_after_seconds"].(float64)
	if !ok || retryAfter < 0 {
		t.Fatalf("expected auth_login_locked details.retry_after_seconds, got %+v", details)
	}
	headerValue := response.Header.Get("Retry-After")
	if headerValue == "" {
		t.Fatalf("expected Retry-After header on auth_login_locked, headers=%v", response.Header)
	}
	parsed, err := strconv.ParseInt(headerValue, 10, 64)
	if err != nil || int64(retryAfter) != parsed {
		t.Fatalf("expected Retry-After header to equal details.retry_after_seconds (%v), got %q", retryAfter, headerValue)
	}
}

func assertSessionPayload(t *testing.T, response *http.Response, authenticated bool, authEnabled bool, username *string) {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["authenticated"] != authenticated || payload["auth_enabled"] != authEnabled {
		t.Fatalf("expected session payload authenticated=%v auth_enabled=%v, got %+v", authenticated, authEnabled, payload)
	}
	if username == nil {
		if payload["username"] != nil {
			t.Fatalf("expected null username, got %+v", payload)
		}
		return
	}
	if payload["username"] != *username {
		t.Fatalf("expected username %q, got %+v", *username, payload)
	}
}

func assertSuccessPayload(t *testing.T, response *http.Response) {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["success"] != true {
		t.Fatalf("expected success payload, got %+v", payload)
	}
}

func assertCookiePresent(t *testing.T, response *http.Response, name string) {
	t.Helper()
	_ = responseCookie(t, response, name)
}

func responseCookie(t *testing.T, response *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("expected response to set cookie %q", name)
	return nil
}

func assertNoResponseCookie(t *testing.T, response *http.Response, name string) {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			t.Fatalf("expected response not to set cookie %q", name)
		}
	}
}

func decodeAccessTokenClaims(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected JWT token with 3 parts, got %q", token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal JWT claims: %v", err)
	}
	return claims
}

func assertJWTSignature(t *testing.T, token string, secret string, wantValid bool) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected JWT token with 3 parts, got %q", token)
	}
	signer := hmac.New(sha256.New, []byte(secret))
	_, _ = signer.Write([]byte(parts[0] + "." + parts[1]))
	got := hmac.Equal([]byte(base64.RawURLEncoding.EncodeToString(signer.Sum(nil))), []byte(parts[2]))
	if got != wantValid {
		t.Fatalf("expected JWT signature validity for secret %q to be %v, got %v", secret, wantValid, got)
	}
}

func cookieValue(t *testing.T, client *http.Client, rawURL string, name string) string {
	t.Helper()
	urlValue, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("build cookie request: %v", err)
	}
	for _, cookie := range client.Jar.Cookies(urlValue.URL) {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

func decodeJSONResponse(t *testing.T, response *http.Response, target any) {
	t.Helper()
	body := readResponseBody(t, response)
	if err := json.Unmarshal([]byte(body), target); err != nil {
		t.Fatalf("decode response JSON %q: %v", body, err)
	}
}

func readResponseBody(t *testing.T, response *http.Response) string {
	t.Helper()
	raw, err := ioReadAll(response)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	response.Body = ioNopCloser(bytes.NewReader(raw))
	return strings.TrimSpace(string(raw))
}

func ioReadAll(response *http.Response) ([]byte, error) {
	return io.ReadAll(response.Body)
}

func ioNopCloser(reader *bytes.Reader) io.ReadCloser {
	return io.NopCloser(reader)
}

func connectDatabase(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect database %s: %v", dsn, err)
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

// branchContainerPrefix returns a sanitized current-git-branch label used as
// a container-name prefix so shared Colima instances can attribute and clean
// up harness containers per branch. Falls back to "unknown" when git is not
// available (e.g. CI tarball checkouts).
func branchContainerPrefix() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	branch := strings.TrimSpace(string(output))
	if branch == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, char := range strings.ToLower(branch) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			builder.WriteRune(char)
		} else {
			builder.WriteByte('-')
		}
	}
	label := builder.String()
	if len(label) > 32 {
		label = label[:32]
	}
	return strings.Trim(label, "-.")
}

func stringPtr(value string) *string {
	result := value
	return &result
}

func modelLoadVendorIDByKey(t *testing.T, _ *contractHarness, _ string) int {
	t.Helper()
	return 1
}
