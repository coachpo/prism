package contract_test

import (
	"bytes"
	"context"
	"crypto/rand"
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
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
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
	client  *http.Client
	conn    *pgx.Conn
	dsn     string
	mailer  *captureMailer
	server  *httptest.Server
	service *managementauth.Service
	url     string
}

type captureMailer struct {
	mu                sync.Mutex
	emailVerification []sentOTP
	passwordReset     []sentOTP
}

type sentOTP struct {
	Recipient string
	OTPCode   string
}

type refreshTokenSnapshot struct {
	ID            int
	RotatedFromID *int
	RevokedAt     *time.Time
	LastUsedAt    *time.Time
}

type passwordResetSnapshot struct {
	AttemptCount int
	ConsumedAt   *time.Time
	OTPHash      string
}

type appAuthSnapshot struct {
	ID                            int
	AuthEnabled                   bool
	Username                      *string
	Email                         *string
	PendingEmail                  *string
	PasswordHash                  *string
	EmailBoundAt                  *time.Time
	EmailVerificationAttemptCount int
	TokenVersion                  int
}

type proxyKeySnapshot struct {
	ID          int
	Name        string
	KeyPrefix   string
	IsActive    bool
	LastUsedAt  *time.Time
	LastUsedIP  *string
	CreatedByID *int
	Notes       *string
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

func TestAuthBootstrap(t *testing.T) {
	harness := newContractHarness(t)

	bootstrapResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/public-bootstrap", nil, nil)
	assertStatus(t, bootstrapResponse, http.StatusOK)
	assertSessionPayload(t, bootstrapResponse, false, false, nil)

	enableVerifiedAuth(t, harness, "bootstrap-admin", "bootstrap-password-123", "bootstrap@example.com")

	bootstrapAfterEnable := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/public-bootstrap", nil, nil)
	assertStatus(t, bootstrapAfterEnable, http.StatusOK)
	assertSessionPayload(t, bootstrapAfterEnable, false, true, nil)

	loginResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/auth/login",
		map[string]any{
			"username":         "bootstrap-admin",
			"password":         "bootstrap-password-123",
			"session_duration": "7_days",
		},
		nil,
	)
	assertStatus(t, loginResponse, http.StatusOK)
	assertCookiePresent(t, loginResponse, "prism_access_token")
	assertCookiePresent(t, loginResponse, "prism_refresh_token")

	bootstrapAuthed := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/public-bootstrap", nil, nil)
	assertStatus(t, bootstrapAuthed, http.StatusOK)
	assertSessionPayload(t, bootstrapAuthed, true, true, stringPtr("bootstrap-admin"))
}

func TestAuthLoginRefreshLogout(t *testing.T) {
	harness := newContractHarness(t)
	seedVerifiedAuthSettings(t, harness, "session-admin", "session-password-123", "session@example.com")

	loginResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/auth/login",
		map[string]any{
			"username":         "session-admin",
			"password":         "session-password-123",
			"session_duration": "7_days",
		},
		nil,
	)
	assertStatus(t, loginResponse, http.StatusOK)
	assertSessionPayload(t, loginResponse, true, true, stringPtr("session-admin"))
	firstRefreshCookie := cookieValue(t, harness.client, harness.url, "prism_refresh_token")
	if firstRefreshCookie == "" {
		t.Fatal("expected refresh cookie after login")
	}

	loginTokens := loadRefreshTokens(t, harness)
	if len(loginTokens) != 1 {
		t.Fatalf("expected 1 refresh token after login, got %d", len(loginTokens))
	}
	if loginTokens[0].RevokedAt != nil || loginTokens[0].LastUsedAt != nil {
		t.Fatalf("expected fresh refresh token to be active and unused, got %+v", loginTokens[0])
	}

	refreshResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/auth/refresh", nil, nil)
	assertStatus(t, refreshResponse, http.StatusOK)
	assertSessionPayload(t, refreshResponse, true, true, stringPtr("session-admin"))
	refreshedCookie := cookieValue(t, harness.client, harness.url, "prism_refresh_token")
	if refreshedCookie == "" || refreshedCookie == firstRefreshCookie {
		t.Fatalf("expected refresh rotation to replace the refresh cookie")
	}

	rotatedTokens := loadRefreshTokens(t, harness)
	if len(rotatedTokens) != 2 {
		t.Fatalf("expected 2 refresh tokens after rotation, got %d", len(rotatedTokens))
	}
	if rotatedTokens[0].RevokedAt == nil || rotatedTokens[0].LastUsedAt == nil {
		t.Fatalf("expected original refresh token to be revoked and marked used, got %+v", rotatedTokens[0])
	}
	if rotatedTokens[1].RotatedFromID == nil || *rotatedTokens[1].RotatedFromID != rotatedTokens[0].ID {
		t.Fatalf("expected rotated refresh token to point to original token, got %+v", rotatedTokens[1])
	}

	logoutResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/auth/logout", nil, nil)
	assertStatus(t, logoutResponse, http.StatusOK)
	assertSessionPayload(t, logoutResponse, false, true, nil)
	if cookieValue(t, harness.client, harness.url, "prism_access_token") != "" {
		t.Fatal("expected access cookie to be cleared after logout")
	}
	if cookieValue(t, harness.client, harness.url, "prism_refresh_token") != "" {
		t.Fatal("expected refresh cookie to be cleared after logout")
	}

	revokedTokens := loadRefreshTokens(t, harness)
	if len(revokedTokens) != 2 {
		t.Fatalf("expected 2 refresh tokens after logout, got %d", len(revokedTokens))
	}
	for _, token := range revokedTokens {
		if token.RevokedAt == nil {
			t.Fatalf("expected refresh token %d to be revoked after logout", token.ID)
		}
	}
}

func TestCurrentSession(t *testing.T) {
	harness := newContractHarness(t)
	seedVerifiedAuthSettings(t, harness, "current-admin", "current-password-123", "current@example.com")

	unauthenticatedResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/session", nil, nil)
	assertErrorResponse(t, unauthenticatedResponse, http.StatusUnauthorized, "Authentication required")

	loginResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/auth/login",
		map[string]any{
			"username":         "current-admin",
			"password":         "current-password-123",
			"session_duration": "7_days",
		},
		nil,
	)
	assertStatus(t, loginResponse, http.StatusOK)

	sessionResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/session", nil, nil)
	assertStatus(t, sessionResponse, http.StatusOK)
	assertSessionPayload(t, sessionResponse, true, true, stringPtr("current-admin"))
}

func TestPasswordReset(t *testing.T) {
	harness := newContractHarness(t)
	seedVerifiedAuthSettings(t, harness, "reset-admin", "reset-password-123", "reset@example.com")

	loginResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/auth/login",
		map[string]any{
			"username":         "reset-admin",
			"password":         "reset-password-123",
			"session_duration": "7_days",
		},
		nil,
	)
	assertStatus(t, loginResponse, http.StatusOK)

	requestResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/auth/password-reset/request",
		map[string]any{"username_or_email": "reset@example.com"},
		nil,
	)
	assertStatus(t, requestResponse, http.StatusOK)
	assertSuccessPayload(t, requestResponse)
	otpCode := harness.mailer.lastPasswordResetOTP(t)
	challenge := loadLatestPasswordResetChallenge(t, harness)
	if challenge.ConsumedAt != nil {
		t.Fatalf("expected password reset challenge to remain active before confirmation")
	}
	if challenge.OTPHash == "" {
		t.Fatal("expected password reset challenge hash to be stored")
	}

	confirmResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/auth/password-reset/confirm",
		map[string]any{
			"otp_code":     otpCode,
			"new_password": "reset-password-updated-456",
		},
		nil,
	)
	assertStatus(t, confirmResponse, http.StatusOK)
	assertSuccessPayload(t, confirmResponse)
	if cookieValue(t, harness.client, harness.url, "prism_access_token") != "" {
		t.Fatal("expected access cookie to be cleared after password reset confirmation")
	}

	updatedChallenge := loadLatestPasswordResetChallenge(t, harness)
	if updatedChallenge.ConsumedAt == nil {
		t.Fatal("expected password reset challenge to be consumed after confirmation")
	}
	settings := loadAppAuthSettings(t, harness)
	if settings.TokenVersion != 1 {
		t.Fatalf("expected password reset to increment token version to 1, got %d", settings.TokenVersion)
	}
	for _, token := range loadRefreshTokens(t, harness) {
		if token.RevokedAt == nil {
			t.Fatalf("expected refresh token %d to be revoked during password reset", token.ID)
		}
	}

	oldPasswordLogin := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/auth/login",
		map[string]any{
			"username":         "reset-admin",
			"password":         "reset-password-123",
			"session_duration": "7_days",
		},
		nil,
	)
	assertErrorResponse(t, oldPasswordLogin, http.StatusUnauthorized, "Invalid credentials")

	newPasswordLogin := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/auth/login",
		map[string]any{
			"username":         "reset-admin",
			"password":         "reset-password-updated-456",
			"session_duration": "7_days",
		},
		nil,
	)
	assertStatus(t, newPasswordLogin, http.StatusOK)
}

func TestEmailVerification(t *testing.T) {
	harness := newContractHarness(t)

	settingsResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth", nil, nil)
	assertStatus(t, settingsResponse, http.StatusOK)
	var initialPayload map[string]any
	decodeJSONResponse(t, settingsResponse, &initialPayload)
	if initialPayload["auth_enabled"] != false || initialPayload["email"] != nil {
		t.Fatalf("expected initial auth settings to be disabled and unbound, got %+v", initialPayload)
	}

	missingEmailResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/auth",
		map[string]any{
			"auth_enabled": true,
			"username":     "email-admin",
			"password":     "email-password-123",
		},
		nil,
	)
	assertErrorResponse(t, missingEmailResponse, http.StatusBadRequest, "A verified email is required")

	verificationRequest := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/settings/auth/email-verification/request",
		map[string]any{"email": "email-admin@example.com"},
		nil,
	)
	assertStatus(t, verificationRequest, http.StatusOK)
	assertEmailVerificationPayload(t, verificationRequest, stringPtr("email-admin@example.com"), nil)
	otpCode := harness.mailer.lastEmailVerificationOTP(t)

	pendingSettingsResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth", nil, nil)
	assertStatus(t, pendingSettingsResponse, http.StatusOK)
	var pendingPayload map[string]any
	decodeJSONResponse(t, pendingSettingsResponse, &pendingPayload)
	if pendingPayload["pending_email"] != "email-admin@example.com" || pendingPayload["email_verification_required"] != true {
		t.Fatalf("expected pending email verification in settings response, got %+v", pendingPayload)
	}

	verificationConfirm := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/settings/auth/email-verification/confirm",
		map[string]any{"otp_code": otpCode},
		nil,
	)
	assertStatus(t, verificationConfirm, http.StatusOK)
	assertEmailVerificationPayload(t, verificationConfirm, nil, stringPtr("email-admin@example.com"))

	enableResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/auth",
		map[string]any{
			"auth_enabled": true,
			"username":     "email-admin",
			"password":     "email-password-123",
		},
		nil,
	)
	assertStatus(t, enableResponse, http.StatusOK)
	var enabledPayload map[string]any
	decodeJSONResponse(t, enableResponse, &enabledPayload)
	if enabledPayload["auth_enabled"] != true || enabledPayload["username"] != "email-admin" || enabledPayload["email"] != "email-admin@example.com" || enabledPayload["has_password"] != true {
		t.Fatalf("expected enabled auth settings payload, got %+v", enabledPayload)
	}
}

func TestProxyKeyCRUD(t *testing.T) {
	harness := newContractHarness(t)
	loginWithVerifiedAuth(t, harness, "proxy-admin", "proxy-password-123", "proxy@example.com")

	initialList := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth/proxy-keys", nil, nil)
	assertStatus(t, initialList, http.StatusOK)
	var emptyList []map[string]any
	decodeJSONResponse(t, initialList, &emptyList)
	if len(emptyList) != 0 {
		t.Fatalf("expected no proxy API keys at test start, got %+v", emptyList)
	}

	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/settings/auth/proxy-keys",
		map[string]any{"name": "Primary runtime key", "notes": "created in contract test"},
		nil,
	)
	assertStatus(t, createResponse, http.StatusCreated)
	var createdPayload map[string]any
	decodeJSONResponse(t, createResponse, &createdPayload)
	item := createdPayload["item"].(map[string]any)
	createdID := int(item["id"].(float64))
	if createdPayload["key"] == "" || item["name"] != "Primary runtime key" || item["notes"] != "created in contract test" || item["is_active"] != true {
		t.Fatalf("expected created proxy key payload, got %+v", createdPayload)
	}

	updatedResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPatch,
		fmt.Sprintf("/api/settings/auth/proxy-keys/%d", createdID),
		map[string]any{"name": "Primary runtime key v2", "notes": "rotatable", "is_active": false},
		nil,
	)
	assertStatus(t, updatedResponse, http.StatusOK)
	var updatedPayload map[string]any
	decodeJSONResponse(t, updatedResponse, &updatedPayload)
	if updatedPayload["name"] != "Primary runtime key v2" || updatedPayload["notes"] != "rotatable" || updatedPayload["is_active"] != false {
		t.Fatalf("expected updated proxy key payload, got %+v", updatedPayload)
	}

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth/proxy-keys", nil, nil)
	assertStatus(t, listResponse, http.StatusOK)
	var listed []map[string]any
	decodeJSONResponse(t, listResponse, &listed)
	if len(listed) != 1 || listed[0]["name"] != "Primary runtime key v2" {
		t.Fatalf("expected updated proxy key in list response, got %+v", listed)
	}

	deleteResponse := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/settings/auth/proxy-keys/%d", createdID), nil, nil)
	assertStatus(t, deleteResponse, http.StatusOK)
	var deletedPayload map[string]any
	decodeJSONResponse(t, deleteResponse, &deletedPayload)
	if deletedPayload["deleted"] != true {
		t.Fatalf("expected delete confirmation payload, got %+v", deletedPayload)
	}

	finalList := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth/proxy-keys", nil, nil)
	assertStatus(t, finalList, http.StatusOK)
	decodeJSONResponse(t, finalList, &emptyList)
	if len(emptyList) != 0 {
		t.Fatalf("expected proxy API key list to be empty after delete, got %+v", emptyList)
	}
}

func TestProxyKeyRotate(t *testing.T) {
	harness := newContractHarness(t)
	loginWithVerifiedAuth(t, harness, "rotate-admin", "rotate-password-123", "rotate@example.com")

	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/settings/auth/proxy-keys",
		map[string]any{"name": "Rotatable key"},
		nil,
	)
	assertStatus(t, createResponse, http.StatusCreated)
	var createdPayload map[string]any
	decodeJSONResponse(t, createResponse, &createdPayload)
	item := createdPayload["item"].(map[string]any)
	keyID := int(item["id"].(float64))
	originalKey := createdPayload["key"].(string)
	originalPrefix := item["key_prefix"].(string)

	rotateResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/settings/auth/proxy-keys/%d/rotate", keyID), nil, nil)
	assertStatus(t, rotateResponse, http.StatusOK)
	var rotatedPayload map[string]any
	decodeJSONResponse(t, rotateResponse, &rotatedPayload)
	rotatedItem := rotatedPayload["item"].(map[string]any)
	rotatedKey := rotatedPayload["key"].(string)
	if rotatedKey == "" || rotatedKey == originalKey {
		t.Fatal("expected proxy key rotation to issue a new raw key")
	}
	if rotatedItem["key_prefix"] == originalPrefix {
		t.Fatal("expected proxy key rotation to update the lookup prefix")
	}

	oldKeyRuntime := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/v1/chat/completions", map[string]any{"model": "gpt-4o"}, map[string]string{"Authorization": "Bearer " + originalKey})
	assertErrorResponse(t, oldKeyRuntime, http.StatusUnauthorized, "Invalid proxy API key")

	newKeyRuntime := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/v1/chat/completions", map[string]any{"model": "gpt-4o"}, map[string]string{"Authorization": "Bearer " + rotatedKey})
	assertErrorResponse(t, newKeyRuntime, http.StatusNotImplemented, "Runtime proxy not implemented in S5")
}

func TestProxyKeyRuntimeSeparation(t *testing.T) {
	harness := newContractHarness(t)
	loginWithVerifiedAuth(t, harness, "runtime-admin", "runtime-password-123", "runtime@example.com")

	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/settings/auth/proxy-keys",
		map[string]any{"name": "Runtime separation key"},
		nil,
	)
	assertStatus(t, createResponse, http.StatusCreated)
	var createdPayload map[string]any
	decodeJSONResponse(t, createResponse, &createdPayload)
	rawKey := createdPayload["key"].(string)

	proxyKeyAgainstManagement := harness.requestJSON(t, harness.newClient(t), http.MethodGet, "/api/settings/auth/proxy-keys", nil, map[string]string{"Authorization": "Bearer " + rawKey})
	assertErrorResponse(t, proxyKeyAgainstManagement, http.StatusUnauthorized, "Authentication required")

	sessionCookieAgainstRuntime := harness.requestJSON(t, harness.client, http.MethodPost, "/v1/chat/completions", map[string]any{"model": "gpt-4o"}, nil)
	assertErrorResponse(t, sessionCookieAgainstRuntime, http.StatusUnauthorized, "Proxy API key required")

	proxyKeyRuntime := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/v1/chat/completions", map[string]any{"model": "gpt-4o"}, map[string]string{"Authorization": "Bearer " + rawKey})
	assertErrorResponse(t, proxyKeyRuntime, http.StatusNotImplemented, "Runtime proxy not implemented in S5")

	proxyKeyRuntimeBeta := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/v1beta/models/example:generateContent", map[string]any{"model": "gemini-pro"}, map[string]string{"X-API-Key": rawKey})
	assertErrorResponse(t, proxyKeyRuntimeBeta, http.StatusNotImplemented, "Runtime proxy not implemented in S5")

	proxyKey := loadProxyKeyByPrefix(t, harness, createdPayload["item"].(map[string]any)["key_prefix"].(string))
	if proxyKey.LastUsedAt == nil {
		t.Fatal("expected runtime proxy key use to record last_used_at")
	}
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
	defer adminConn.Close(ctx)
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
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "contract_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() {
		conn.Close(context.Background())
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

	settings := config.Settings{
		Host:                       "127.0.0.1",
		Port:                       8000,
		AppEnv:                     config.EnvironmentProduction,
		DatabaseURL:                sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey:        "contract-secret",
		CORSAllowedOrigins:         "http://localhost:5173,http://127.0.0.1:5173",
		AuthJWTSecret:              "contract-jwt-secret",
		AuthAccessTokenTTLSeconds:  900,
		AuthRefreshTokenTTLSeconds: 604800,
		AuthResetCodeTTLSeconds:    600,
		AuthCookieName:             "prism_access_token",
		AuthRefreshCookieName:      "prism_refresh_token",
		AuthCookieSecure:           false,
	}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	mailer := &captureMailer{}
	authService, err := managementauth.NewService(settings, managementauth.Options{Pool: pool, Mailer: mailer})
	if err != nil {
		t.Fatalf("build auth service: %v", err)
	}
	t.Cleanup(authService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{
		Version:     "contract-test",
		AuthService: authService,
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
	return &contractHarness{client: client, conn: conn, dsn: settings.DatabaseURL, mailer: mailer, server: server, service: authService, url: server.URL}
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

func (m *captureMailer) SendEmailVerificationOTP(_ context.Context, recipient string, otpCode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emailVerification = append(m.emailVerification, sentOTP{Recipient: recipient, OTPCode: otpCode})
	return nil
}

func (m *captureMailer) SendPasswordResetEmail(_ context.Context, recipient string, otpCode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.passwordReset = append(m.passwordReset, sentOTP{Recipient: recipient, OTPCode: otpCode})
	return nil
}

func (m *captureMailer) lastEmailVerificationOTP(t *testing.T) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.emailVerification) == 0 {
		t.Fatal("expected captured email verification OTP")
	}
	return m.emailVerification[len(m.emailVerification)-1].OTPCode
}

func (m *captureMailer) lastPasswordResetOTP(t *testing.T) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.passwordReset) == 0 {
		t.Fatal("expected captured password reset OTP")
	}
	return m.passwordReset[len(m.passwordReset)-1].OTPCode
}

func enableVerifiedAuth(t *testing.T, harness *contractHarness, username string, password string, email string) {
	t.Helper()
	verificationRequest := harness.requestJSON(t, harness.client, http.MethodPost, "/api/settings/auth/email-verification/request", map[string]any{"email": email}, nil)
	assertStatus(t, verificationRequest, http.StatusOK)
	verificationConfirm := harness.requestJSON(t, harness.client, http.MethodPost, "/api/settings/auth/email-verification/confirm", map[string]any{"otp_code": harness.mailer.lastEmailVerificationOTP(t)}, nil)
	assertStatus(t, verificationConfirm, http.StatusOK)
	enableResponse := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/auth", map[string]any{"auth_enabled": true, "username": username, "password": password}, nil)
	assertStatus(t, enableResponse, http.StatusOK)
}

func loginWithVerifiedAuth(t *testing.T, harness *contractHarness, username string, password string, email string) {
	t.Helper()
	seedVerifiedAuthSettings(t, harness, username, password, email)
	loginResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/auth/login", map[string]any{"username": username, "password": password, "session_duration": "7_days"}, nil)
	assertStatus(t, loginResponse, http.StatusOK)
}

func seedVerifiedAuthSettings(t *testing.T, harness *contractHarness, username string, password string, email string) {
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
			email = $2,
			pending_email = NULL,
			email_bound_at = $3,
			password_hash = $4,
			email_verification_code_hash = NULL,
			email_verification_expires_at = NULL,
			email_verification_attempt_count = 0,
			token_version = 0,
			updated_at = $3
		WHERE singleton_key = 'app'`,
		username,
		email,
		now,
		string(hash),
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

func loadLatestPasswordResetChallenge(t *testing.T, harness *contractHarness) passwordResetSnapshot {
	t.Helper()
	var consumedAt sqlNullTime
	var snapshot passwordResetSnapshot
	if err := harness.conn.QueryRow(context.Background(), `SELECT attempt_count, consumed_at, otp_hash FROM password_reset_challenges ORDER BY id DESC LIMIT 1`).Scan(&snapshot.AttemptCount, &consumedAt, &snapshot.OTPHash); err != nil {
		t.Fatalf("query latest password reset challenge: %v", err)
	}
	snapshot.ConsumedAt = consumedAt.ptr()
	return snapshot
}

func loadAppAuthSettings(t *testing.T, harness *contractHarness) appAuthSnapshot {
	t.Helper()
	var username, email, pendingEmail, passwordHash sqlNullString
	var emailBoundAt sqlNullTime
	var snapshot appAuthSnapshot
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT id, auth_enabled, username, email, pending_email, password_hash, email_bound_at, email_verification_attempt_count, token_version FROM app_auth_settings WHERE singleton_key = 'app'`,
	).Scan(
		&snapshot.ID,
		&snapshot.AuthEnabled,
		&username,
		&email,
		&pendingEmail,
		&passwordHash,
		&emailBoundAt,
		&snapshot.EmailVerificationAttemptCount,
		&snapshot.TokenVersion,
	); err != nil {
		t.Fatalf("query app auth settings: %v", err)
	}
	snapshot.Username = username.ptr()
	snapshot.Email = email.ptr()
	snapshot.PendingEmail = pendingEmail.ptr()
	snapshot.PasswordHash = passwordHash.ptr()
	snapshot.EmailBoundAt = emailBoundAt.ptr()
	return snapshot
}

func loadProxyKeyByPrefix(t *testing.T, harness *contractHarness, keyPrefix string) proxyKeySnapshot {
	t.Helper()
	var lastUsedAt sqlNullTime
	var lastUsedIP, notes sqlNullString
	var createdByID sqlNullInt32
	var snapshot proxyKeySnapshot
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT id, name, key_prefix, is_active, last_used_at, last_used_ip, created_by_auth_subject_id, notes FROM proxy_api_keys WHERE key_prefix = $1 LIMIT 1`,
		keyPrefix,
	).Scan(&snapshot.ID, &snapshot.Name, &snapshot.KeyPrefix, &snapshot.IsActive, &lastUsedAt, &lastUsedIP, &createdByID, &notes); err != nil {
		t.Fatalf("query proxy key by prefix: %v", err)
	}
	snapshot.LastUsedAt = lastUsedAt.ptr()
	snapshot.LastUsedIP = lastUsedIP.ptr()
	snapshot.CreatedByID = createdByID.ptr()
	snapshot.Notes = notes.ptr()
	return snapshot
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
	var payload map[string]string
	decodeJSONResponse(t, response, &payload)
	if payload["detail"] != wantDetail {
		t.Fatalf("expected error detail %q, got %+v", wantDetail, payload)
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

func assertEmailVerificationPayload(t *testing.T, response *http.Response, pendingEmail *string, email *string) {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["success"] != true {
		t.Fatalf("expected email verification success payload, got %+v", payload)
	}
	assertOptionalString(t, payload["pending_email"], pendingEmail, "pending_email")
	assertOptionalString(t, payload["email"], email, "email")
}

func assertOptionalString(t *testing.T, actual any, expected *string, field string) {
	t.Helper()
	if expected == nil {
		if actual != nil {
			t.Fatalf("expected %s to be null, got %v", field, actual)
		}
		return
	}
	if actual != *expected {
		t.Fatalf("expected %s %q, got %v", field, *expected, actual)
	}
}

func assertCookiePresent(t *testing.T, response *http.Response, name string) {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			return
		}
	}
	t.Fatalf("expected response to set cookie %q", name)
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
