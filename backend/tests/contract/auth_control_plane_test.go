package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

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

func TestAuthLoginExactMatchProtection(t *testing.T) {
	harness := newContractHarness(t)
	seedVerifiedAuthSettings(t, harness, "exact-admin", "exact-password-123", "exact@example.com")

	for _, path := range []string{"/api/auth/login/", "/api/auth/logins"} {
		response := harness.requestJSON(
			t,
			harness.client,
			http.MethodPost,
			path,
			map[string]any{
				"username":         "exact-admin",
				"password":         "exact-password-123",
				"session_duration": "7_days",
			},
			nil,
		)
		assertStatus(t, response, http.StatusUnauthorized)
	}
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

func TestAuthLoginThrottleLocksUnknownAndKnownSubjectsGenerically(t *testing.T) {
	harness := newContractHarness(t)
	seedVerifiedAuthSettings(t, harness, "throttle-admin", "throttle-password-123", "throttle@example.com")

	for attempt := 1; attempt < 5; attempt++ {
		unknownResponse := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/api/auth/login", map[string]any{"username": "missing-admin", "password": "wrong-password", "session_duration": "7_days"}, nil)
		assertErrorResponse(t, unknownResponse, http.StatusUnauthorized, "Invalid credentials")
	}
	unknownLockout := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/api/auth/login", map[string]any{"username": "missing-admin", "password": "wrong-password", "session_duration": "7_days"}, nil)
	assertErrorResponse(t, unknownLockout, http.StatusTooManyRequests, "Too many login attempts. Please try again later.")

	for attempt := 1; attempt < 5; attempt++ {
		knownResponse := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/api/auth/login", map[string]any{"username": "throttle-admin", "password": "wrong-password", "session_duration": "7_days"}, nil)
		assertErrorResponse(t, knownResponse, http.StatusUnauthorized, "Invalid credentials")
	}
	knownLockout := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/api/auth/login", map[string]any{"username": "throttle-admin", "password": "wrong-password", "session_duration": "7_days"}, nil)
	assertErrorResponse(t, knownLockout, http.StatusTooManyRequests, "Too many login attempts. Please try again later.")

	entries := loadLoginThrottleEntries(t, harness)
	if len(entries) != 2 {
		t.Fatalf("expected separate throttle ledger entries for known and unknown subjects, got %+v", entries)
	}
	for _, entry := range entries {
		if entry.FailureCount != 5 || entry.LockedUntil == nil {
			t.Fatalf("expected locked throttle entry after five failures, got %+v", entry)
		}
	}
}

func TestAuthLoginThrottleSuccessClearsCounter(t *testing.T) {
	harness := newContractHarness(t)
	seedVerifiedAuthSettings(t, harness, "reset-throttle-admin", "reset-throttle-password-123", "reset-throttle@example.com")

	for attempt := 1; attempt < 5; attempt++ {
		response := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/api/auth/login", map[string]any{"username": "reset-throttle-admin", "password": "wrong-password", "session_duration": "7_days"}, nil)
		assertErrorResponse(t, response, http.StatusUnauthorized, "Invalid credentials")
	}
	if entries := loadLoginThrottleEntries(t, harness); len(entries) != 1 || entries[0].FailureCount != 4 {
		t.Fatalf("expected four recorded failures before success, got %+v", entries)
	}

	success := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/api/auth/login", map[string]any{"username": "reset-throttle-admin", "password": "reset-throttle-password-123", "session_duration": "7_days"}, nil)
	assertStatus(t, success, http.StatusOK)
	if entries := loadLoginThrottleEntries(t, harness); len(entries) != 0 {
		t.Fatalf("expected successful login to clear throttle ledger, got %+v", entries)
	}

	postResetFailure := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/api/auth/login", map[string]any{"username": "reset-throttle-admin", "password": "wrong-password", "session_duration": "7_days"}, nil)
	assertErrorResponse(t, postResetFailure, http.StatusUnauthorized, "Invalid credentials")
}

func TestAuthLoginThrottleConcurrentFailuresPersistAndLock(t *testing.T) {
	harness := newContractHarness(t)
	seedVerifiedAuthSettings(t, harness, "concurrent-admin", "concurrent-password-123", "concurrent@example.com")

	clients := make([]*http.Client, 0, 5)
	for range 5 {
		clients = append(clients, harness.newClient(t))
	}
	var waitGroup sync.WaitGroup
	results := make(chan loginAttemptResult, 5)
	for _, client := range clients {
		waitGroup.Add(1)
		go func(client *http.Client) {
			defer waitGroup.Done()
			results <- performLoginAttempt(harness, client, "concurrent-admin", "wrong-password")
		}(client)
	}
	waitGroup.Wait()
	close(results)

	tooManyRequests := 0
	unauthorized := 0
	for result := range results {
		if result.Err != nil {
			t.Fatalf("concurrent login attempt failed: %v", result.Err)
		}
		switch result.Status {
		case http.StatusUnauthorized:
			unauthorized++
		case http.StatusTooManyRequests:
			tooManyRequests++
		default:
			t.Fatalf("expected concurrent failure to return 401 or 429, got %d body=%s", result.Status, result.Body)
		}
	}
	if unauthorized+tooManyRequests != 5 || tooManyRequests == 0 {
		t.Fatalf("expected five serialized failures with at least one lockout response, got unauthorized=%d lockout=%d", unauthorized, tooManyRequests)
	}
	entries := loadLoginThrottleEntries(t, harness)
	if len(entries) != 1 || entries[0].FailureCount != 5 || entries[0].LockedUntil == nil {
		t.Fatalf("expected concurrent failures to persist one locked ledger row, got %+v", entries)
	}
}

func TestAuthLoginThrottlePersistsAcrossServiceRestart(t *testing.T) {
	harness := newContractHarness(t)
	seedVerifiedAuthSettings(t, harness, "persist-throttle-admin", "persist-throttle-password-123", "persist-throttle@example.com")

	for range 5 {
		response := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/api/auth/login", map[string]any{"username": "persist-throttle-admin", "password": "wrong-password", "session_duration": "7_days"}, nil)
		if response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("expected pre-restart failure to return 401 or 429, got %d", response.StatusCode)
		}
	}
	harness.server.Close()

	restarted := newContractHarnessForExistingDatabase(t, harness.dsn)
	lockedAfterRestart := restarted.requestJSON(t, restarted.newClient(t), http.MethodPost, "/api/auth/login", map[string]any{"username": "persist-throttle-admin", "password": "persist-throttle-password-123", "session_duration": "7_days"}, nil)
	assertErrorResponse(t, lockedAfterRestart, http.StatusTooManyRequests, "Too many login attempts. Please try again later.")
}

func TestAuthHotBootstrapRuntimeConfigAppliesToNewOperations(t *testing.T) {
	harness := newContractHarness(t)
	seedVerifiedAuthSettings(t, harness, "hot-admin", "hot-password-123", "hot@example.com")

	updated := contractAuthSettings()
	updated.AuthJWTSecret = "hot-published-jwt-secret"
	updated.AuthAccessTokenTTLSeconds = 37
	updated.AuthRefreshTokenTTLSeconds = 43
	updated.AuthCookieName = "hot_access_token"
	updated.AuthRefreshCookieName = "hot_refresh_token"
	updated.AuthCookieSecure = true
	retired, err := harness.hotRuntime.Publish(updated)
	if err != nil {
		t.Fatalf("publish hot auth runtime config: %v", err)
	}
	retired.CloseIdleConnections()

	loginResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/auth/login",
		map[string]any{
			"username":         "hot-admin",
			"password":         "hot-password-123",
			"session_duration": "session",
		},
		nil,
	)
	assertStatus(t, loginResponse, http.StatusOK)
	accessCookie := responseCookie(t, loginResponse, "hot_access_token")
	refreshCookie := responseCookie(t, loginResponse, "hot_refresh_token")
	if !accessCookie.Secure || !refreshCookie.Secure {
		t.Fatalf("expected hot cookie secure flag, got access=%v refresh=%v", accessCookie.Secure, refreshCookie.Secure)
	}
	assertNoResponseCookie(t, loginResponse, "prism_access_token")
	assertNoResponseCookie(t, loginResponse, "prism_refresh_token")
	claims := decodeAccessTokenClaims(t, accessCookie.Value)
	if got := int(claims["exp"].(float64) - claims["iat"].(float64)); got != 37 {
		t.Fatalf("expected hot access token TTL 37s, got %ds", got)
	}
	assertJWTSignature(t, accessCookie.Value, "contract-jwt-secret", true)
	assertJWTSignature(t, accessCookie.Value, "hot-published-jwt-secret", false)

	var refreshTTL int
	if err := harness.conn.QueryRow(context.Background(), `SELECT ROUND(EXTRACT(EPOCH FROM expires_at - created_at))::int FROM refresh_tokens ORDER BY id DESC LIMIT 1`).Scan(&refreshTTL); err != nil {
		t.Fatalf("query hot refresh token TTL: %v", err)
	}
	if refreshTTL != 43 {
		t.Fatalf("expected hot refresh token TTL 43s, got %ds", refreshTTL)
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

func TestAuthDisableInvalidatesCurrentSession(t *testing.T) {
	harness := newContractHarness(t)
	loginWithVerifiedAuth(t, harness, "disable-admin", "disable-password-123", "disable@example.com")

	disableResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/auth",
		map[string]any{"auth_enabled": false},
		nil,
	)
	assertStatus(t, disableResponse, http.StatusOK)
	if cookieValue(t, harness.client, harness.url, "prism_access_token") != "" {
		t.Fatal("expected access cookie to be cleared after disabling auth")
	}
	if cookieValue(t, harness.client, harness.url, "prism_refresh_token") != "" {
		t.Fatal("expected refresh cookie to be cleared after disabling auth")
	}

	bootstrapResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/public-bootstrap", nil, nil)
	assertStatus(t, bootstrapResponse, http.StatusOK)
	assertSessionPayload(t, bootstrapResponse, false, false, nil)

	for _, token := range loadRefreshTokens(t, harness) {
		if token.RevokedAt == nil {
			t.Fatalf("expected refresh token %d to be revoked after disabling auth", token.ID)
		}
	}
}

func TestAuthUsernameChangeInvalidatesCurrentSession(t *testing.T) {
	harness := newContractHarness(t)
	loginWithVerifiedAuth(t, harness, "rename-admin", "rename-password-123", "rename@example.com")

	updateResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/auth",
		map[string]any{"auth_enabled": true, "username": "rename-admin-v2"},
		nil,
	)
	assertStatus(t, updateResponse, http.StatusOK)
	if cookieValue(t, harness.client, harness.url, "prism_access_token") != "" {
		t.Fatal("expected access cookie to be cleared after username change")
	}
	if cookieValue(t, harness.client, harness.url, "prism_refresh_token") != "" {
		t.Fatal("expected refresh cookie to be cleared after username change")
	}

	settings := loadAppAuthSettings(t, harness)
	if settings.TokenVersion != 1 {
		t.Fatalf("expected username change to increment token version to 1, got %d", settings.TokenVersion)
	}
	for _, token := range loadRefreshTokens(t, harness) {
		if token.RevokedAt == nil {
			t.Fatalf("expected refresh token %d to be revoked after username change", token.ID)
		}
	}

	staleSession := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/session", nil, nil)
	assertErrorResponse(t, staleSession, http.StatusUnauthorized, "Authentication required")

	oldUsernameLogin := harness.requestJSON(
		t,
		harness.newClient(t),
		http.MethodPost,
		"/api/auth/login",
		map[string]any{"username": "rename-admin", "password": "rename-password-123", "session_duration": "7_days"},
		nil,
	)
	assertErrorResponse(t, oldUsernameLogin, http.StatusUnauthorized, "Invalid credentials")

	newUsernameLogin := harness.requestJSON(
		t,
		harness.newClient(t),
		http.MethodPost,
		"/api/auth/login",
		map[string]any{"username": "rename-admin-v2", "password": "rename-password-123", "session_duration": "7_days"},
		nil,
	)
	assertStatus(t, newUsernameLogin, http.StatusOK)
}

func TestProxyKeyCRUD(t *testing.T) {
	harness := newContractHarness(t)
	loginWithVerifiedAuth(t, harness, "proxy-admin", "proxy-password-123", "proxy@example.com")

	initialList := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth/proxy-keys", nil, nil)
	assertStatus(t, initialList, http.StatusOK)
	var initialListPayload struct {
		Items    []map[string]any        `json:"items"`
		Capacity proxyKeyCapacityPayload `json:"capacity"`
	}
	decodeJSONResponse(t, initialList, &initialListPayload)
	if len(initialListPayload.Items) != 0 {
		t.Fatalf("expected no proxy API keys at test start, got %+v", initialListPayload.Items)
	}
	assertProxyKeyCapacityPayload(t, initialListPayload.Capacity, 0, 100)

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
	assertProxyKeyCapacityPayload(t, decodeCapacityField(t, createResponse, "capacity"), 1, 100)
	if createResponse.Header.Get("Cache-Control") != "private, no-store" || createResponse.Header.Get("Pragma") != "no-cache" {
		t.Fatalf("expected create response to carry private, no-store headers, got Cache-Control=%q Pragma=%q", createResponse.Header.Get("Cache-Control"), createResponse.Header.Get("Pragma"))
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
	updatedItem := updatedPayload["item"].(map[string]any)
	if updatedItem["name"] != "Primary runtime key v2" || updatedItem["notes"] != "rotatable" || updatedItem["is_active"] != false {
		t.Fatalf("expected updated proxy key payload, got %+v", updatedPayload)
	}
	assertProxyKeyCapacityPayload(t, decodeCapacityField(t, updatedResponse, "capacity"), 1, 100)

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth/proxy-keys", nil, nil)
	assertStatus(t, listResponse, http.StatusOK)
	var listedPayload struct {
		Items    []map[string]any        `json:"items"`
		Capacity proxyKeyCapacityPayload `json:"capacity"`
	}
	decodeJSONResponse(t, listResponse, &listedPayload)
	if len(listedPayload.Items) != 1 || listedPayload.Items[0]["name"] != "Primary runtime key v2" {
		t.Fatalf("expected updated proxy key in list response, got %+v", listedPayload.Items)
	}
	assertProxyKeyCapacityPayload(t, listedPayload.Capacity, 1, 100)

	deleteResponse := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/settings/auth/proxy-keys/%d", createdID), nil, nil)
	assertStatus(t, deleteResponse, http.StatusOK)
	var deletedPayload map[string]any
	decodeJSONResponse(t, deleteResponse, &deletedPayload)
	if deletedPayload["deleted_id"] != float64(createdID) {
		t.Fatalf("expected delete confirmation payload with deleted_id, got %+v", deletedPayload)
	}
	assertProxyKeyCapacityPayload(t, decodeCapacityField(t, deleteResponse, "capacity"), 0, 100)
}

func TestProxyKeyExpiryMutation(t *testing.T) {
	harness := newContractHarness(t)
	loginWithVerifiedAuth(t, harness, "expiry-admin", "expiry-password-123", "expiry@example.com")

	futureExpiry := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Microsecond)
	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/settings/auth/proxy-keys",
		map[string]any{"name": "Expiring key", "expires_at": futureExpiry},
		nil,
	)
	assertStatus(t, createResponse, http.StatusCreated)
	var createdPayload map[string]any
	decodeJSONResponse(t, createResponse, &createdPayload)
	createdItem := createdPayload["item"].(map[string]any)
	createdSnapshot := loadProxyKeyByPrefix(t, harness, createdItem["key_prefix"].(string))
	if createdSnapshot.ExpiresAt == nil || !createdSnapshot.ExpiresAt.Equal(futureExpiry) {
		t.Fatalf("expected proxy key create payload to persist expires_at %s, got %+v", futureExpiry.Format(time.RFC3339Nano), createdSnapshot)
	}

	// Non-future expiry is rejected with a typed locatable error.
	pastExpiry := time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Microsecond)
	pastUpdateResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPatch,
		fmt.Sprintf("/api/settings/auth/proxy-keys/%d", createdSnapshot.ID),
		map[string]any{"name": "Expiring key", "expires_at": pastExpiry},
		nil,
	)
	assertErrorResponseCode(t, pastUpdateResponse, http.StatusUnprocessableEntity, "proxy_key_expiry_invalid", "Expiry must be a future time")
	unchangedSnapshot := loadProxyKeyByPrefix(t, harness, createdItem["key_prefix"].(string))
	if unchangedSnapshot.ExpiresAt == nil || !unchangedSnapshot.ExpiresAt.Equal(futureExpiry) {
		t.Fatalf("expected rejected non-future expiry to preserve the previous instant, got %+v", unchangedSnapshot)
	}

	// Presence-aware update: explicit null clears the expiry.
	clearResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPatch,
		fmt.Sprintf("/api/settings/auth/proxy-keys/%d", createdSnapshot.ID),
		map[string]any{"name": "Expiring key", "expires_at": nil},
		nil,
	)
	assertStatus(t, clearResponse, http.StatusOK)
	clearedSnapshot := loadProxyKeyByPrefix(t, harness, createdItem["key_prefix"].(string))
	if clearedSnapshot.ExpiresAt != nil {
		t.Fatalf("expected explicit null expiry update to clear expires_at, got %+v", clearedSnapshot)
	}
	assertProxyKeyCapacityPayload(t, decodeCapacityField(t, clearResponse, "capacity"), 1, 100)

	// Presence-aware update: setting a new future instant applies it.
	newExpiry := time.Now().UTC().Add(3 * time.Hour).Truncate(time.Microsecond)
	setResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPatch,
		fmt.Sprintf("/api/settings/auth/proxy-keys/%d", createdSnapshot.ID),
		map[string]any{"name": "Expiring key", "expires_at": newExpiry},
		nil,
	)
	assertStatus(t, setResponse, http.StatusOK)
	setSnapshot := loadProxyKeyByPrefix(t, harness, createdItem["key_prefix"].(string))
	if setSnapshot.ExpiresAt == nil || !setSnapshot.ExpiresAt.Equal(newExpiry) {
		t.Fatalf("expected proxy key update payload to persist expires_at %s, got %+v", newExpiry.Format(time.RFC3339Nano), setSnapshot)
	}

	// Presence-aware update: omitted field preserves the current instant.
	preserveResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPatch,
		fmt.Sprintf("/api/settings/auth/proxy-keys/%d", createdSnapshot.ID),
		map[string]any{"name": "Expiring key renamed"},
		nil,
	)
	assertStatus(t, preserveResponse, http.StatusOK)
	preservedSnapshot := loadProxyKeyByPrefix(t, harness, createdItem["key_prefix"].(string))
	if preservedSnapshot.ExpiresAt == nil || !preservedSnapshot.ExpiresAt.Equal(newExpiry) {
		t.Fatalf("expected omitted expiry update to preserve the current instant, got %+v", preservedSnapshot)
	}
	if preservedSnapshot.Name != "Expiring key renamed" {
		t.Fatalf("expected rename to apply alongside preserved expiry, got %+v", preservedSnapshot)
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
		map[string]any{"name": "Rotatable key", "notes": "rotation lineage"},
		nil,
	)
	assertStatus(t, createResponse, http.StatusCreated)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{Auth: true})
	var createdPayload map[string]any
	decodeJSONResponse(t, createResponse, &createdPayload)
	item := createdPayload["item"].(map[string]any)
	keyID := int(item["id"].(float64))
	originalKey := createdPayload["key"].(string)
	originalPrefix := item["key_prefix"].(string)
	futureExpiry := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Microsecond)
	if _, err := harness.conn.Exec(context.Background(), `UPDATE proxy_api_keys SET expires_at = $2, updated_at = $2 WHERE id = $1`, keyID, futureExpiry); err != nil {
		t.Fatalf("set proxy key expiry before rotation: %v", err)
	}
	originalSnapshot := loadProxyKeyByPrefix(t, harness, originalPrefix)

	rotateResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/settings/auth/proxy-keys/%d/rotate", keyID), nil, nil)
	assertStatus(t, rotateResponse, http.StatusOK)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{Auth: true})
	var rotatedPayload map[string]any
	decodeJSONResponse(t, rotateResponse, &rotatedPayload)
	rotatedItem := rotatedPayload["item"].(map[string]any)
	rotatedID := int(rotatedItem["id"].(float64))
	rotatedKey := rotatedPayload["key"].(string)
	if rotatedKey == "" || rotatedKey == originalKey {
		t.Fatal("expected proxy key rotation to issue a new raw key")
	}
	if rotatedID == keyID {
		t.Fatal("expected proxy key rotation to create a successor row")
	}
	if rotatedItem["key_prefix"] == originalPrefix {
		t.Fatal("expected proxy key rotation to update the lookup prefix")
	}
	rotatedFromID, ok := rotatedItem["rotated_from_id"].(float64)
	if !ok || int(rotatedFromID) != keyID {
		t.Fatalf("expected rotated proxy key response to point to the previous key, got %+v", rotatedItem)
	}
	if rotatedItem["expires_at"] == nil {
		t.Fatalf("expected rotated proxy key response to preserve the inherited expiry, got %+v", rotatedItem)
	}

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth/proxy-keys", nil, nil)
	assertStatus(t, listResponse, http.StatusOK)
	var listedPayload struct {
		Items []map[string]any `json:"items"`
	}
	decodeJSONResponse(t, listResponse, &listedPayload)
	if len(listedPayload.Items) != 2 {
		t.Fatalf("expected proxy key rotation to preserve history in list responses, got %+v", listedPayload.Items)
	}

	snapshots := loadProxyKeys(t, harness)
	if len(snapshots) != 2 {
		t.Fatalf("expected proxy key rotation to preserve exactly two proxy key rows, got %+v", snapshots)
	}
	keysByID := make(map[int]proxyKeySnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		keysByID[snapshot.ID] = snapshot
	}
	historical := keysByID[keyID]
	successor := keysByID[rotatedID]
	if historical.KeyPrefix != originalPrefix {
		t.Fatalf("expected historical proxy key to keep its original prefix, got %+v", historical)
	}
	if historical.IsActive {
		t.Fatalf("expected historical proxy key to be deactivated on rotation, got %+v", historical)
	}
	if historical.ExpiresAt == nil || !historical.ExpiresAt.Before(futureExpiry) {
		t.Fatalf("expected historical proxy key to expire immediately on rotation, got %+v", historical)
	}
	if historical.RotatedFromID != nil {
		t.Fatalf("expected historical proxy key to remain the root of the chain, got %+v", historical)
	}
	if successor.RotatedFromID == nil || *successor.RotatedFromID != keyID {
		t.Fatalf("expected successor proxy key to point back to the historical row, got %+v", successor)
	}
	if !successor.IsActive {
		t.Fatalf("expected successor proxy key to preserve the current active state, got %+v", successor)
	}
	if successor.ExpiresAt == nil || !successor.ExpiresAt.Equal(futureExpiry) {
		t.Fatalf("expected successor proxy key to inherit the prior expiry, got %+v", successor)
	}
	if successor.Name != originalSnapshot.Name {
		t.Fatalf("expected successor proxy key to preserve the key name, got historical %+v successor %+v", originalSnapshot, successor)
	}
	if originalSnapshot.CreatedByID == nil || successor.CreatedByID == nil || *originalSnapshot.CreatedByID != *successor.CreatedByID {
		t.Fatalf("expected successor proxy key to preserve the creator, got historical %+v successor %+v", originalSnapshot, successor)
	}
	if originalSnapshot.Notes == nil || successor.Notes == nil || *originalSnapshot.Notes != *successor.Notes {
		t.Fatalf("expected successor proxy key to preserve notes, got historical %+v successor %+v", originalSnapshot, successor)
	}

	oldKeyRuntime := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/v1/chat/completions", map[string]any{"model": "gpt-4o"}, map[string]string{"Authorization": "Bearer " + originalKey})
	assertErrorResponse(t, oldKeyRuntime, http.StatusUnauthorized, "Invalid proxy API key")

	reauthenticatedClient := harness.newClient(t)
	reauthenticatedResponse := harness.requestJSON(
		t,
		reauthenticatedClient,
		http.MethodPost,
		"/api/auth/login",
		map[string]any{"username": "rotate-admin", "password": "rotate-password-123", "session_duration": "7_days"},
		nil,
	)
	assertStatus(t, reauthenticatedResponse, http.StatusOK)

	historicalRotateResponse := harness.requestJSON(t, reauthenticatedClient, http.MethodPost, fmt.Sprintf("/api/settings/auth/proxy-keys/%d/rotate", keyID), nil, nil)
	assertErrorResponse(t, historicalRotateResponse, http.StatusConflict, "Expired proxy API keys cannot be rotated")

	newKeyRuntime := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/v1/chat/completions", map[string]any{"model": "gpt-4o"}, map[string]string{"Authorization": "Bearer " + rotatedKey})
	assertErrorResponse(t, newKeyRuntime, http.StatusNotImplemented, "Runtime proxy unavailable without a runtime service")
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
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{Auth: true})
	var createdPayload map[string]any
	decodeJSONResponse(t, createResponse, &createdPayload)
	rawKey := createdPayload["key"].(string)

	proxyKeyAgainstManagement := harness.requestJSON(t, harness.newClient(t), http.MethodGet, "/api/settings/auth/proxy-keys", nil, map[string]string{"Authorization": "Bearer " + rawKey})
	assertErrorResponse(t, proxyKeyAgainstManagement, http.StatusUnauthorized, "Authentication required")

	sessionCookieAgainstRuntime := harness.requestJSON(t, harness.client, http.MethodPost, "/v1/chat/completions", map[string]any{"model": "gpt-4o"}, nil)
	assertErrorResponse(t, sessionCookieAgainstRuntime, http.StatusUnauthorized, "Proxy API key required")

	proxyKeyRuntime := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/v1/chat/completions", map[string]any{"model": "gpt-4o"}, map[string]string{"Authorization": "Bearer " + rawKey})
	assertErrorResponse(t, proxyKeyRuntime, http.StatusNotImplemented, "Runtime proxy unavailable without a runtime service")

	proxyKeyRuntimeBeta := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/v1beta/models/example:generateContent", map[string]any{"model": "gemini-pro"}, map[string]string{"X-API-Key": rawKey})
	assertErrorResponse(t, proxyKeyRuntimeBeta, http.StatusNotImplemented, "Runtime proxy unavailable without a runtime service")

	proxyKey := loadProxyKeyByPrefix(t, harness, createdPayload["item"].(map[string]any)["key_prefix"].(string))
	if proxyKey.LastUsedAt != nil {
		t.Fatal("expected runtime probe path to avoid last_used_at materialization without a real runtime request")
	}
}

type proxyKeyCapacityPayload struct {
	Limit     int    `json:"limit"`
	Used      int    `json:"used"`
	Remaining int    `json:"remaining"`
	CountedAt string `json:"counted_at"`
}

func decodeCapacityField(t *testing.T, response *http.Response, field string) proxyKeyCapacityPayload {
	t.Helper()
	var payload struct {
		Capacity proxyKeyCapacityPayload `json:"capacity"`
	}
	decodeJSONResponse(t, response, &payload)
	return payload.Capacity
}

func assertProxyKeyCapacityPayload(t *testing.T, payload proxyKeyCapacityPayload, used int, limit int) {
	t.Helper()
	if payload.Limit != limit {
		t.Fatalf("capacity limit = %d, want %d", payload.Limit, limit)
	}
	if payload.Used != used {
		t.Fatalf("capacity used = %d, want %d", payload.Used, used)
	}
	expectedRemaining := limit - used
	if expectedRemaining < 0 {
		expectedRemaining = 0
	}
	if payload.Remaining != expectedRemaining {
		t.Fatalf("capacity remaining = %d, want %d", payload.Remaining, expectedRemaining)
	}
	if strings.TrimSpace(payload.CountedAt) == "" {
		t.Fatal("capacity counted_at must be an RFC3339 string")
	}
}
