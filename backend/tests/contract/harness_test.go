package contract_test

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
	RotatedFromID *int
}

func TestMain(m *testing.M) {
	harness, err := startSharedPostgresHarness()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sharedPostgresHarness = harness
	code := m.Run()
	cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if harness.containerName != "" {
		_ = exec.CommandContext(cleanupContext, "docker", "rm", "-f", harness.containerName).Run()
	}
	os.Exit(code)
}

func startSharedPostgresHarness() (testPostgresHarness, error) {
	containerName := "prism-s5-" + randomSuffix()
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
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	if dsn == "" {
		databaseName := "contract_" + randomSuffix()
		conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
		t.Cleanup(func() {
			_ = conn.Close(context.Background())
		})
		startupService, err := startup.New(startup.Options{
			DatabaseURL:         sharedPostgresHarness.connectionString(databaseName),
			SecretEncryptionKey: "contract-secret",
		})
		if err != nil {
			t.Fatalf("build startup service: %v", err)
		}
		if _, err := startupService.RunWithConn(testContext, conn); err != nil {
			t.Fatalf("run startup service: %v", err)
		}
		dsn = sharedPostgresHarness.connectionString(databaseName)
		return buildContractHarness(t, testContext, conn, dsn, "contract-test")
	}
	conn := connectDatabase(t, testContext, dsn)
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})
	return buildContractHarness(t, testContext, conn, dsn, "contract-restart-test")
}

func newContractHarnessForExistingDatabase(t *testing.T, dsn string) *contractHarness {
	t.Helper()
	return newContractHarnessWithDatabase(t, dsn)
}

func buildContractHarness(t *testing.T, testContext context.Context, conn *pgx.Conn, dsn string, version string) *contractHarness {
	t.Helper()
	settings := contractAuthSettings()
	settings.DatabaseURL = dsn
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	runtimeCache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
	if err := runtimeCache.Bootstrap(testContext); err != nil {
		t.Fatalf("bootstrap published runtime snapshot: %v", err)
	}
	runtimeAuthCache := managementauth.NewRuntimeCacheFromShared(runtimeCache)
	hotRuntime, err := platformhttp.NewHotBootstrapConfigRuntime(settings)
	if err != nil {
		t.Fatalf("build hot bootstrap runtime: %v", err)
	}
	authService, err := managementauth.NewService(settings, managementauth.Options{CORSOriginProvider: hotRuntime, AuthRuntimeConfigProvider: hotRuntime, Pool: pool, RuntimeCache: runtimeAuthCache})
	if err != nil {
		t.Fatalf("build auth service: %v", err)
	}
	t.Cleanup(authService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{
		Version:                   version,
		AuthService:               authService,
		RuntimeAuthService:        authService,
		RuntimeCache:              runtimeCache,
		HotBootstrapConfigRuntime: hotRuntime,
	})
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
	return &contractHarness{client: client, conn: conn, dsn: dsn, server: server, service: authService, runtimeService: nil, runtimeCache: runtimeCache, hotRuntime: hotRuntime, url: server.URL}
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
	request, err := http.NewRequest(method, h.url+path, requestBody)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != nil {
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

func enableVerifiedAuth(t *testing.T, harness *contractHarness, username string, password string, _ string) {
	t.Helper()
	enableResponse := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/auth", map[string]any{"auth_enabled": true, "username": username, "password": password}, nil)
	assertStatus(t, enableResponse, http.StatusOK)
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
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE app_auth_settings
		SET auth_enabled = TRUE,
			username = $1,
			password_hash = $2,
			token_version = 0,
			updated_at = $3
		WHERE singleton_key = 'app'`,
		username,
		string(hash),
		now,
	); err != nil {
		t.Fatalf("seed verified auth settings: %v", err)
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
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT id, auth_enabled, username, password_hash, token_version FROM app_auth_settings WHERE singleton_key = 'app'`,
	).Scan(
		&snapshot.ID,
		&snapshot.AuthEnabled,
		&username,
		&passwordHash,
		&snapshot.TokenVersion,
	); err != nil {
		t.Fatalf("query app auth settings: %v", err)
	}
	snapshot.Username = username.ptr()
	snapshot.PasswordHash = passwordHash.ptr()
	return snapshot
}

func loadProxyKeys(t *testing.T, harness *contractHarness) []proxyKeySnapshot {
	t.Helper()
	rows, err := harness.conn.Query(
		context.Background(),
		`SELECT id, name, key_prefix, is_active, expires_at, last_used_at, last_used_ip, created_by_auth_subject_id, notes, rotated_from_id FROM proxy_api_keys ORDER BY id ASC`,
	)
	if err != nil {
		t.Fatalf("query proxy keys: %v", err)
	}
	defer rows.Close()
	var snapshots []proxyKeySnapshot
	for rows.Next() {
		var expiresAt, lastUsedAt sqlNullTime
		var lastUsedIP, notes sqlNullString
		var createdByID, rotatedFromID sqlNullInt32
		var snapshot proxyKeySnapshot
		if err := rows.Scan(&snapshot.ID, &snapshot.Name, &snapshot.KeyPrefix, &snapshot.IsActive, &expiresAt, &lastUsedAt, &lastUsedIP, &createdByID, &notes, &rotatedFromID); err != nil {
			t.Fatalf("scan proxy key: %v", err)
		}
		snapshot.ExpiresAt = expiresAt.ptr()
		snapshot.LastUsedAt = lastUsedAt.ptr()
		snapshot.LastUsedIP = lastUsedIP.ptr()
		snapshot.CreatedByID = createdByID.ptr()
		snapshot.Notes = notes.ptr()
		snapshot.RotatedFromID = rotatedFromID.ptr()
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

func stringPtr(value string) *string {
	result := value
	return &result
}
