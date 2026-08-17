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
	assertStatus(t, logoutResponse, http.StatusNoContent)
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
		assertAuthProblemResponse(t, unknownResponse, http.StatusUnauthorized, "auth_invalid_credentials")
	}
	unknownLockout := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/api/auth/login", map[string]any{"username": "missing-admin", "password": "wrong-password", "session_duration": "7_days"}, nil)
	assertLoginLockedProblem(t, unknownLockout)

	for attempt := 1; attempt < 5; attempt++ {
		knownResponse := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/api/auth/login", map[string]any{"username": "throttle-admin", "password": "wrong-password", "session_duration": "7_days"}, nil)
		assertAuthProblemResponse(t, knownResponse, http.StatusUnauthorized, "auth_invalid_credentials")
	}
	knownLockout := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/api/auth/login", map[string]any{"username": "throttle-admin", "password": "wrong-password", "session_duration": "7_days"}, nil)
	assertLoginLockedProblem(t, knownLockout)

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
		assertAuthProblemResponse(t, response, http.StatusUnauthorized, "auth_invalid_credentials")
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
	assertAuthProblemResponse(t, postResetFailure, http.StatusUnauthorized, "auth_invalid_credentials")
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
	assertLoginLockedProblem(t, lockedAfterRestart)
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

	disableResponse := putAuthSettingsV2(t, harness, false, map[string]any{"kind": "preserve"}, map[string]any{"disable_to_permissive_access": true})
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

	updateResponse := putAuthSettingsV2(t, harness, true, map[string]any{"kind": "update", "username": "rename-admin-v2", "new_password": nil}, map[string]any{"invalidate_operator_sessions": true})
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
	assertAuthProblemResponse(t, staleSession, http.StatusUnauthorized, "auth_not_authenticated")

	oldUsernameLogin := harness.requestJSON(
		t,
		harness.newClient(t),
		http.MethodPost,
		"/api/auth/login",
		map[string]any{"username": "rename-admin", "password": "rename-password-123", "session_duration": "7_days"},
		nil,
	)
	assertAuthProblemResponse(t, oldUsernameLogin, http.StatusUnauthorized, "auth_invalid_credentials")

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
		map[string]any{"name": "Rotatable key", "notes": "rotation metadata"},
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
	priorUse := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE proxy_api_keys SET expires_at = $2, updated_at = $2, last_used_at = $3, last_used_ip = $4 WHERE id = $1`,
		keyID,
		futureExpiry,
		priorUse,
		"203.0.113.7",
	); err != nil {
		t.Fatalf("set proxy key expiry and usage trace before rotation: %v", err)
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
	if rotatedID != keyID {
		t.Fatalf("expected proxy key rotation to keep the same row id, got %d want %d", rotatedID, keyID)
	}
	if rotatedItem["key_prefix"] == originalPrefix {
		t.Fatal("expected proxy key rotation to update the lookup prefix")
	}
	if _, present := rotatedItem["rotated_from_id"]; present {
		t.Fatalf("expected in-place rotation to drop the lineage field, got %+v", rotatedItem)
	}
	rotationCount, ok := rotatedItem["rotation_count"].(float64)
	if !ok || int(rotationCount) != 1 {
		t.Fatalf("expected rotated proxy key response to report one rotation, got %+v", rotatedItem)
	}
	if rotatedItem["rotated_at"] == nil {
		t.Fatalf("expected rotated proxy key response to carry the rotation instant, got %+v", rotatedItem)
	}
	if rotatedItem["expires_at"] == nil {
		t.Fatalf("expected rotated proxy key response to preserve the expiry, got %+v", rotatedItem)
	}
	if rotatedItem["last_used_at"] != nil || rotatedItem["last_used_ip"] != nil {
		t.Fatalf("expected rotation to clear the usage trace of the retired secret, got %+v", rotatedItem)
	}

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth/proxy-keys", nil, nil)
	assertStatus(t, listResponse, http.StatusOK)
	var listedPayload struct {
		Items []map[string]any `json:"items"`
	}
	decodeJSONResponse(t, listResponse, &listedPayload)
	if len(listedPayload.Items) != 1 {
		t.Fatalf("expected in-place rotation to leave a single ledger row, got %+v", listedPayload.Items)
	}

	snapshots := loadProxyKeys(t, harness)
	if len(snapshots) != 1 {
		t.Fatalf("expected in-place rotation to leave exactly one proxy key row, got %+v", snapshots)
	}
	rotated := snapshots[0]
	if rotated.ID != keyID {
		t.Fatalf("expected the rotated row to keep its identity, got %+v", rotated)
	}
	if rotated.KeyPrefix == originalPrefix {
		t.Fatalf("expected the rotated row to carry a new prefix, got %+v", rotated)
	}
	if !rotated.IsActive {
		t.Fatalf("expected the rotated row to stay active, got %+v", rotated)
	}
	if rotated.ExpiresAt == nil || !rotated.ExpiresAt.Equal(futureExpiry) {
		t.Fatalf("expected the rotated row to keep its expiry, got %+v", rotated)
	}
	if !rotated.CreatedAt.Equal(originalSnapshot.CreatedAt) {
		t.Fatalf("expected the rotated row to keep its creation instant, got %+v want %+v", rotated, originalSnapshot)
	}
	if rotated.LastUsedAt != nil || rotated.LastUsedIP != nil {
		t.Fatalf("expected rotation to clear the persisted usage trace, got %+v", rotated)
	}
	if rotated.RotationCount != 1 {
		t.Fatalf("expected the rotated row to count one rotation, got %+v", rotated)
	}
	if rotated.RotatedAt == nil || rotated.RotatedAt.Before(originalSnapshot.CreatedAt) {
		t.Fatalf("expected the rotated row to record the rotation instant, got %+v", rotated)
	}
	if rotated.Name != originalSnapshot.Name {
		t.Fatalf("expected rotation to preserve the key name, got %+v want %+v", rotated, originalSnapshot)
	}
	if originalSnapshot.CreatedByID == nil || rotated.CreatedByID == nil || *originalSnapshot.CreatedByID != *rotated.CreatedByID {
		t.Fatalf("expected rotation to preserve the creator, got %+v want %+v", rotated, originalSnapshot)
	}
	if originalSnapshot.Notes == nil || rotated.Notes == nil || *originalSnapshot.Notes != *rotated.Notes {
		t.Fatalf("expected rotation to preserve notes, got %+v want %+v", rotated, originalSnapshot)
	}

	// A second rotation accumulates on the same row rather than extending a chain.
	// It runs before any harness.newClient call: newClient swaps the cookie jar of
	// the shared httptest client, which drops the management session on harness.client.
	secondRotateResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/settings/auth/proxy-keys/%d/rotate", keyID), nil, nil)
	assertStatus(t, secondRotateResponse, http.StatusOK)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{Auth: true})
	var secondRotatedPayload map[string]any
	decodeJSONResponse(t, secondRotateResponse, &secondRotatedPayload)
	secondRotatedItem := secondRotatedPayload["item"].(map[string]any)
	secondRotatedKey := secondRotatedPayload["key"].(string)
	if int(secondRotatedItem["id"].(float64)) != keyID {
		t.Fatalf("expected the second rotation to stay on the same row, got %+v", secondRotatedItem)
	}
	if count, ok := secondRotatedItem["rotation_count"].(float64); !ok || int(count) != 2 {
		t.Fatalf("expected the second rotation to increment the counter, got %+v", secondRotatedItem)
	}
	if secondRotatedKey == rotatedKey {
		t.Fatal("expected the second rotation to issue another distinct raw key")
	}
	if repeated := loadProxyKeys(t, harness); len(repeated) != 1 {
		t.Fatalf("expected repeated rotation to leave exactly one proxy key row, got %+v", repeated)
	}

	// Every superseded secret stops authorizing; only the newest one is live.
	originalKeyRuntime := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/v1/chat/completions", map[string]any{"model": "gpt-4o"}, map[string]string{"Authorization": "Bearer " + originalKey})
	assertErrorResponse(t, originalKeyRuntime, http.StatusUnauthorized, "Invalid proxy API key")

	firstRotatedKeyRuntime := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/v1/chat/completions", map[string]any{"model": "gpt-4o"}, map[string]string{"Authorization": "Bearer " + rotatedKey})
	assertErrorResponse(t, firstRotatedKeyRuntime, http.StatusUnauthorized, "Invalid proxy API key")

	newKeyRuntime := harness.requestJSON(t, harness.newClient(t), http.MethodPost, "/v1/chat/completions", map[string]any{"model": "gpt-4o"}, map[string]string{"Authorization": "Bearer " + secondRotatedKey})
	assertErrorResponse(t, newKeyRuntime, http.StatusNotImplemented, "Runtime proxy unavailable without a runtime service")

	// Expiry is still a hard gate: an expired key cannot be rotated back to life.
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
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE proxy_api_keys SET expires_at = $2 WHERE id = $1`,
		keyID,
		time.Now().UTC().Add(-time.Minute),
	); err != nil {
		t.Fatalf("expire proxy key before the negative rotation case: %v", err)
	}
	expiredRotateResponse := harness.requestJSON(t, reauthenticatedClient, http.MethodPost, fmt.Sprintf("/api/settings/auth/proxy-keys/%d/rotate", keyID), nil, nil)
	assertErrorResponse(t, expiredRotateResponse, http.StatusConflict, "Expired proxy API keys cannot be rotated")
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

// TestAuthRefreshThreeStateMatrix covers the SPEC §5.2 refresh classifier:
// invalid token stays 200 {true,false}, auth-disabled is a live 200
// {false,false} (never a stale {true,false}), and a persisted fail-closed
// transition is the registered typed 503 (never false,false or a generic 5xx).
func TestAuthRefreshThreeStateMatrix(t *testing.T) {
	harness := newContractHarness(t)

	// Disabled instance: refresh (with no cookie) returns live false,false.
	disabledRefresh := harness.requestJSON(t, harness.client, http.MethodPost, "/api/auth/refresh", nil, nil)
	assertStatus(t, disabledRefresh, http.StatusOK)
	assertSessionPayload(t, disabledRefresh, false, false, nil)

	// Enabled instance with an invalid refresh cookie: 200 {true,false}.
	enableVerifiedAuth(t, harness, "refresh-admin", "refresh-password-123", "refresh@example.com")
	invalidRefresh := harness.requestJSON(t, harness.client, http.MethodPost, "/api/auth/refresh", nil, map[string]string{"Cookie": "prism_refresh_token=invalid-token"})
	assertStatus(t, invalidRefresh, http.StatusOK)
	assertSessionPayload(t, invalidRefresh, false, true, nil)

	// Enabled instance with a valid session: refresh rotates and returns
	// authenticated session with subject_key.
	loginResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/auth/login", map[string]any{"username": "refresh-admin", "password": "refresh-password-123", "session_duration": "7_days"}, nil)
	assertStatus(t, loginResponse, http.StatusOK)
	validRefresh := harness.requestJSON(t, harness.client, http.MethodPost, "/api/auth/refresh", nil, nil)
	assertStatus(t, validRefresh, http.StatusOK)
	var sessionPayload map[string]any
	decodeJSONResponse(t, validRefresh, &sessionPayload)
	if sessionPayload["authenticated"] != true || sessionPayload["subject_key"] == nil {
		t.Fatalf("expected authenticated refresh with subject_key, got %+v", sessionPayload)
	}

	// Persisted enabling_fail_closed transition: typed 503, never false,false.
	seedAuthTransition(t, harness, "enabling_fail_closed", "retrying", "enable", "11111111-1111-4111-8111-111111111111")
	harness.service.InvalidateAppAuthSettingsSnapshot()
	transitionRefresh := harness.requestJSON(t, harness.client, http.MethodPost, "/api/auth/refresh", nil, nil)
	assertAuthProblemResponse(t, transitionRefresh, http.StatusServiceUnavailable, "auth_transition_in_progress")
	var transitionBody map[string]any
	decodeJSONResponse(t, transitionRefresh, &transitionBody)
	details := transitionBody["details"].(map[string]any)
	if details["transition_state"] != "enabling_fail_closed" || details["recovery"] != "confirm_public_status" {
		t.Fatalf("expected transition details, got %+v", details)
	}
	// Public status reports the same fail-closed union.
	statusResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/status", nil, nil)
	assertStatus(t, statusResponse, http.StatusOK)
	var statusPayload map[string]any
	decodeJSONResponse(t, statusResponse, &statusPayload)
	if statusPayload["state"] != "transition_fail_closed" || statusPayload["transition_state"] != "enabling_fail_closed" {
		t.Fatalf("expected tagged transition_fail_closed status, got %+v", statusPayload)
	}
	// Operation status is public, bounded, and identifies the seeded
	// operation; unknown operations use the same safe 404.
	operationStatus := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/operations/11111111-1111-4111-8111-111111111111/status", nil, nil)
	assertStatus(t, operationStatus, http.StatusOK)
	unknownOperation := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/operations/00000000-0000-4000-8000-000000000000/status", nil, nil)
	assertStatus(t, unknownOperation, http.StatusNotFound)
	// Wrong method on the operation route is not exempt (verified after the
	// transition is cleared below).

	// A second mutation cannot be used as an implicit recovery path. The
	// existing transition remains the only operation identity until its
	// publisher/rollback worker proves a terminal state.
	recovery := putAuthSettingsV2(t, harness, true, map[string]any{"kind": "preserve"}, nil)
	assertAuthProblemResponse(t, recovery, http.StatusConflict, "auth_transition_in_progress")
	clearAuthTransition(t, harness)
	harness.service.InvalidateAppAuthSettingsSnapshot()
	recovered := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/status", nil, nil)
	assertStatus(t, recovered, http.StatusOK)
	var recoveredPayload map[string]any
	decodeJSONResponse(t, recovered, &recoveredPayload)
	if recoveredPayload["state"] != "enabled" {
		t.Fatalf("expected enabled state after transition recovery, got %+v", recoveredPayload)
	}
	wrongMethod := harness.requestJSON(t, harness.client, http.MethodPost, "/api/auth/operations/11111111-1111-4111-8111-111111111111/status", nil, nil)
	assertStatus(t, wrongMethod, http.StatusMethodNotAllowed)
}

// TestAuthTaggedPublicStatusUnion covers the tagged PublicAuthStatus wire:
// the only legal branches are enabled+null|disabling_enforced,
// disabled+null and transition_fail_closed+enabling_fail_closed|rollback_required,
// each carrying the canonical positive decimal effective_generation.
func TestAuthTaggedPublicStatusUnion(t *testing.T) {
	harness := newContractHarness(t)

	disabled := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/status", nil, nil)
	assertStatus(t, disabled, http.StatusOK)
	var disabledPayload map[string]any
	decodeJSONResponse(t, disabled, &disabledPayload)
	if disabledPayload["state"] != "disabled" || disabledPayload["transition_state"] != nil || disabledPayload["login_available"] != false {
		t.Fatalf("expected disabled union, got %+v", disabledPayload)
	}
	if disabledPayload["effective_generation"] != "1" {
		t.Fatalf("expected default effective_generation 1, got %+v", disabledPayload)
	}

	enableVerifiedAuth(t, harness, "status-admin", "status-password-123", "status@example.com")
	enabled := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/status", nil, nil)
	assertStatus(t, enabled, http.StatusOK)
	var enabledPayload map[string]any
	decodeJSONResponse(t, enabled, &enabledPayload)
	if enabledPayload["state"] != "enabled" || enabledPayload["transition_state"] != nil || enabledPayload["login_available"] != true {
		t.Fatalf("expected enabled union, got %+v", enabledPayload)
	}

	// Persisted rollback_required maps to transition_fail_closed with the
	// recovery-required problem code on ordinary management.
	seedAuthTransition(t, harness, "rollback_required", "rollback_required", "enable", "22222222-2222-4222-8222-222222222222")
	harness.service.InvalidateAppAuthSettingsSnapshot()
	statusRollback := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/status", nil, nil)
	assertStatus(t, statusRollback, http.StatusOK)
	var rollbackPayload map[string]any
	decodeJSONResponse(t, statusRollback, &rollbackPayload)
	if rollbackPayload["state"] != "transition_fail_closed" || rollbackPayload["transition_state"] != "rollback_required" {
		t.Fatalf("expected rollback_required union, got %+v", rollbackPayload)
	}
	// Ordinary management (models list) gets the typed recovery-required 503.
	models := harness.requestJSON(t, harness.client, http.MethodGet, "/api/models", nil, nil)
	assertAuthProblemResponse(t, models, http.StatusServiceUnavailable, "auth_transition_recovery_required")
	// Login also gets the typed 503, never a credentials error.
	login := harness.requestJSON(t, harness.client, http.MethodPost, "/api/auth/login", map[string]any{"username": "status-admin", "password": "status-password-123", "session_duration": "7_days"}, nil)
	assertAuthProblemResponse(t, login, http.StatusServiceUnavailable, "auth_transition_recovery_required")

	// Disabling-enforced context: enabled mode stays enforced with the
	// transition context attached, and the tagged union reports it.
	seedAuthTransition(t, harness, "disabling_enforced", "staged", "disable", "33333333-3333-4333-8333-333333333333")
	harness.service.InvalidateAppAuthSettingsSnapshot()
	disabling := harness.requestJSON(t, harness.client, http.MethodGet, "/api/auth/status", nil, nil)
	assertStatus(t, disabling, http.StatusOK)
	var disablingPayload map[string]any
	decodeJSONResponse(t, disabling, &disablingPayload)
	if disablingPayload["state"] != "enabled" || disablingPayload["transition_state"] != "disabling_enforced" {
		t.Fatalf("expected disabling_enforced context on enabled state, got %+v", disablingPayload)
	}
	// Clean up: clear the transition for the harness teardown.
	clearAuthTransition(t, harness)
	harness.service.InvalidateAppAuthSettingsSnapshot()
}

func seedAuthTransition(t *testing.T, harness *contractHarness, legacyState string, pointerState string, kind string, operationID string) {
	t.Helper()
	if _, err := harness.conn.Exec(t.Context(), `UPDATE app_auth_settings SET
		auth_transition_state = $1,
		auth_transition_operation_id = $2::uuid,
		transition_state = $3,
		transition_kind = $4,
		transition_operation_id = $5
		WHERE singleton_key = 'app'`, legacyState, operationID, pointerState, kind, operationID); err != nil {
		t.Fatalf("seed auth transition: %v", err)
	}
}

func clearAuthTransition(t *testing.T, harness *contractHarness) {
	t.Helper()
	if _, err := harness.conn.Exec(t.Context(), `UPDATE app_auth_settings SET
		auth_transition_state = NULL,
		auth_transition_operation_id = NULL,
		transition_state = NULL,
		transition_kind = NULL,
		transition_operation_id = NULL
		WHERE singleton_key = 'app'`); err != nil {
		t.Fatalf("clear auth transition: %v", err)
	}
}
