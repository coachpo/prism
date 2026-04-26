package runtime_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const runtimeCacheInvalidationVisibilityDeadline = 2 * time.Second

func TestRuntimeCacheInvalidation(t *testing.T) {
	t.Run("AuthCacheInvalidationAfterProxyKeyRotation", runtimeAuthCacheInvalidationAfterProxyKeyRotation)
	t.Run("AfterActiveProfileActivation", runtimeCacheInvalidationAfterActiveProfileActivation)
	t.Run("PlanningCacheInvalidationAfterHeaderBlocklistWrite", runtimePlanningCacheInvalidationAfterHeaderBlocklistWrite)
}

func runtimeAuthCacheInvalidationAfterProxyKeyRotation(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	seedRuntimeVerifiedAuthSettings(t, harness, "cache-admin", "cache-password-123", "cache@example.com")
	loginRuntimeHarness(t, harness, "cache-admin", "cache-password-123")

	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-runtime-cache-invalidation"})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "cache-public-" + randomSuffix(),
		TargetModelID:   "cache-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/cache-invalidation/runtime"),
		EndpointAPIKey:  "cache-invalidation-upstream-key",
	})

	keyID, originalKey := createRuntimeProxyKey(t, harness, "Phase 3.3 auth cache invalidation")

	initialResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "pre-rotation request"}},
			"model":    route.PublicModelID,
		},
		map[string]string{"Authorization": "Bearer " + originalKey},
	)
	assertStatus(t, initialResponse, http.StatusOK)

	requests := upstream.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("expected one upstream request before rotation, got %d", len(requests))
	}
	if requests[0].Path != "/cache-invalidation/runtime/v1/chat/completions" {
		t.Fatalf("expected pre-rotation upstream path %q, got %q", "/cache-invalidation/runtime/v1/chat/completions", requests[0].Path)
	}
	if got := requestModelID(t, requests[0].Body); got != route.TargetModelID {
		t.Fatalf("expected pre-rotation upstream model %q, got %q", route.TargetModelID, got)
	}

	rotatedKey := rotateRuntimeProxyKey(t, harness, keyID)
	if rotatedKey == originalKey {
		t.Fatal("expected rotated proxy key to differ from the original key")
	}

	rotationVisibleAt := time.Now()
	oldKeyResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "stale key request"}},
			"model":    route.PublicModelID,
		},
		map[string]string{"Authorization": "Bearer " + originalKey},
	)
	assertStatus(t, oldKeyResponse, http.StatusUnauthorized)
	assertRuntimeCacheInvalidationVisibleWithin(t, rotationVisibleAt, "proxy-key rotation stale-key rejection")
	var oldKeyPayload map[string]any
	decodeJSONResponse(t, oldKeyResponse, &oldKeyPayload)
	if oldKeyPayload["detail"] != "Invalid proxy API key" {
		t.Fatalf("expected stale key request to fail with invalid proxy key detail, got %+v", oldKeyPayload)
	}
	if got := len(upstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected stale key request to stop before the upstream, got %d upstream requests", got)
	}

	newKeyResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "post-rotation request"}},
			"model":    route.PublicModelID,
		},
		map[string]string{"Authorization": "Bearer " + rotatedKey},
	)
	assertStatus(t, newKeyResponse, http.StatusOK)

	requests = upstream.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("expected rotated key request to reach the upstream after rotation, got %d upstream requests", len(requests))
	}
	if requests[1].Path != "/cache-invalidation/runtime/v1/chat/completions" {
		t.Fatalf("expected post-rotation upstream path %q, got %q", "/cache-invalidation/runtime/v1/chat/completions", requests[1].Path)
	}
	if got := requestModelID(t, requests[1].Body); got != route.TargetModelID {
		t.Fatalf("expected post-rotation upstream model %q, got %q", route.TargetModelID, got)
	}

	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
}

func runtimeCacheInvalidationAfterActiveProfileActivation(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	standbyProfileID := harness.createProfile(t, "Cache Activation Standby")
	suffix := randomSuffix()
	publicModelID := "cache-active-profile-" + suffix
	activeRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       activeProfileID,
		APIFamily:       "openai",
		PublicModelID:   publicModelID,
		TargetModelID:   "cache-active-target-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/cache-invalidation/active-profile"),
		EndpointAPIKey:  "cache-active-profile-key",
	})
	standbyRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       standbyProfileID,
		APIFamily:       "openai",
		PublicModelID:   publicModelID,
		TargetModelID:   "cache-standby-target-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/cache-invalidation/standby-profile"),
		EndpointAPIKey:  "cache-standby-profile-key",
	})

	initialResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "prime active profile cache"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, initialResponse, http.StatusOK)

	firstRequest := harness.upstream.lastRequest(t)
	if firstRequest.Path != "/cache-invalidation/active-profile/v1/chat/completions" {
		t.Fatalf("expected cached active profile to route through %q before activation, got %q", "/cache-invalidation/active-profile/v1/chat/completions", firstRequest.Path)
	}
	if firstRequest.Headers.Get("Authorization") != "Bearer "+activeRoute.EndpointAPIKey {
		t.Fatalf("expected active profile upstream authorization header, got %q", firstRequest.Headers.Get("Authorization"))
	}
	if got := requestModelID(t, firstRequest.Body); got != activeRoute.TargetModelID {
		t.Fatalf("expected active profile upstream model %q before activation, got %q", activeRoute.TargetModelID, got)
	}

	harness.activateProfile(t, standbyProfileID, activeProfileID)
	harness.upstream.clear()

	activationVisibleAt := time.Now()
	activatedResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "read newly activated profile"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, activatedResponse, http.StatusOK)
	assertRuntimeCacheInvalidationVisibleWithin(t, activationVisibleAt, "active-profile activation")

	secondRequest := harness.upstream.lastRequest(t)
	if secondRequest.Path != "/cache-invalidation/standby-profile/v1/chat/completions" {
		t.Fatalf("expected activation write to invalidate cached active profile and route through %q, got %q", "/cache-invalidation/standby-profile/v1/chat/completions", secondRequest.Path)
	}
	if secondRequest.Headers.Get("Authorization") != "Bearer "+standbyRoute.EndpointAPIKey {
		t.Fatalf("expected newly active profile upstream authorization header, got %q", secondRequest.Headers.Get("Authorization"))
	}
	if got := requestModelID(t, secondRequest.Body); got != standbyRoute.TargetModelID {
		t.Fatalf("expected newly active profile upstream model %q after activation, got %q", standbyRoute.TargetModelID, got)
	}
}

func runtimePlanningCacheInvalidationAfterHeaderBlocklistWrite(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	blockedHeaderName := "X-Cache-Blocked"
	allowedHeaderName := "X-Cache-Allowed"
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "cache-header-public-" + suffix,
		TargetModelID:   "cache-header-target-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/cache-invalidation/header-blocklist"),
		EndpointAPIKey:  "cache-header-upstream-key",
	})

	initialResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "prime planning snapshot cache"}},
			"model":    route.PublicModelID,
		},
		map[string]string{
			blockedHeaderName: "before-invalidation",
			allowedHeaderName: "allowed-before-and-after",
		},
	)
	assertStatus(t, initialResponse, http.StatusOK)

	firstRequest := harness.upstream.lastRequest(t)
	if firstRequest.Path != "/cache-invalidation/header-blocklist/v1/chat/completions" {
		t.Fatalf("expected planning-cache baseline upstream path %q, got %q", "/cache-invalidation/header-blocklist/v1/chat/completions", firstRequest.Path)
	}
	if firstRequest.Headers.Get(blockedHeaderName) != "before-invalidation" {
		t.Fatalf("expected %s to survive before blocklist write, got %q", blockedHeaderName, firstRequest.Headers.Get(blockedHeaderName))
	}
	if firstRequest.Headers.Get(allowedHeaderName) != "allowed-before-and-after" {
		t.Fatalf("expected %s to survive before blocklist write, got %q", allowedHeaderName, firstRequest.Headers.Get(allowedHeaderName))
	}

	createRuntimeHeaderBlocklistRule(t, harness, profileID, "Block cache invalidation header", "exact", "x-cache-blocked")
	harness.upstream.clear()

	blocklistVisibleAt := time.Now()
	invalidatedResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "use post-write planning snapshot"}},
			"model":    route.PublicModelID,
		},
		map[string]string{
			blockedHeaderName: "should-be-removed",
			allowedHeaderName: "allowed-before-and-after",
		},
	)
	assertStatus(t, invalidatedResponse, http.StatusOK)
	assertRuntimeCacheInvalidationVisibleWithin(t, blocklistVisibleAt, "header-blocklist write")

	secondRequest := harness.upstream.lastRequest(t)
	if secondRequest.Headers.Get(blockedHeaderName) != "" {
		t.Fatalf("expected header-blocklist write to invalidate the cached planning snapshot and remove %s, got %q", blockedHeaderName, secondRequest.Headers.Get(blockedHeaderName))
	}
	if secondRequest.Headers.Get(allowedHeaderName) != "allowed-before-and-after" {
		t.Fatalf("expected non-blocked header %s to survive after invalidation, got %q", allowedHeaderName, secondRequest.Headers.Get(allowedHeaderName))
	}
}

func assertRuntimeCacheInvalidationVisibleWithin(t *testing.T, startedAt time.Time, action string) {
	t.Helper()
	if elapsed := time.Since(startedAt); elapsed > runtimeCacheInvalidationVisibilityDeadline {
		t.Fatalf("expected %s to become visible to /v1 within %s, got %s", action, runtimeCacheInvalidationVisibilityDeadline, elapsed)
	}
}

func createRuntimeProxyKey(t *testing.T, harness *runtimeHarness, name string) (int, string) {
	t.Helper()
	generation := harness.runtimeCache.PublishedGeneration()
	response := harness.requestJSON(t, http.MethodPost, "/api/settings/auth/proxy-keys", map[string]any{"name": name}, nil)
	assertStatus(t, response, http.StatusCreated)
	harness.waitForRuntimeSnapshotGeneration(t, generation)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	item := payload["item"].(map[string]any)
	return jsonInt(t, item["id"]), itemlessString(t, payload["key"])
}

func rotateRuntimeProxyKey(t *testing.T, harness *runtimeHarness, keyID int) string {
	t.Helper()
	generation := harness.runtimeCache.PublishedGeneration()
	response := harness.requestJSON(t, http.MethodPost, fmt.Sprintf("/api/settings/auth/proxy-keys/%d/rotate", keyID), nil, nil)
	assertStatus(t, response, http.StatusOK)
	harness.waitForRuntimeSnapshotGeneration(t, generation)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return itemlessString(t, payload["key"])
}

func createRuntimeHeaderBlocklistRule(t *testing.T, harness *runtimeHarness, profileID int, name string, matchType string, pattern string) {
	t.Helper()
	generation := harness.runtimeCache.PublishedGeneration()
	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/api/config/header-blocklist-rules",
		map[string]any{"name": name, "match_type": matchType, "pattern": pattern},
		runtimeModelHeader(profileID),
	)
	assertStatus(t, response, http.StatusCreated)
	harness.waitForRuntimeSnapshotGeneration(t, generation)
}

func itemlessString(t *testing.T, value any) string {
	t.Helper()
	stringValue, ok := value.(string)
	if !ok || stringValue == "" {
		t.Fatalf("expected non-empty string value, got %+v", value)
	}
	return stringValue
}

func loginRuntimeHarness(t *testing.T, harness *runtimeHarness, username string, password string) {
	t.Helper()
	response := harness.requestJSON(t, http.MethodPost, "/api/auth/login", map[string]any{
		"username":         username,
		"password":         password,
		"session_duration": "7_days",
	}, nil)
	assertStatus(t, response, http.StatusOK)
}

func seedRuntimeVerifiedAuthSettings(t *testing.T, harness *runtimeHarness, username string, password string, email string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password for runtime auth seed: %v", err)
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
		t.Fatalf("seed runtime auth settings: %v", err)
	}
	harness.authService.InvalidateAppAuthSettingsSnapshot()
}
