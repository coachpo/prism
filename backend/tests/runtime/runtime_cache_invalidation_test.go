package runtime_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"golang.org/x/crypto/bcrypt"
)

const runtimeCacheInvalidationVisibilityDeadline = 2 * time.Second

func TestRuntimeCacheInvalidation(t *testing.T) {
	t.Run("AuthCacheInvalidationAfterProxyKeyRotation", runtimeAuthCacheInvalidationAfterProxyKeyRotation)
	t.Run("AuthCacheInvalidationAfterProxyKeyRetire", runtimeAuthCacheInvalidationAfterProxyKeyRetire)
	t.Run("AuthCacheInvalidationAfterProxyKeyExpiryMutation", runtimeAuthCacheInvalidationAfterProxyKeyExpiryMutation)
	t.Run("AuthCacheInvalidationAfterAuthDisable", runtimeAuthCacheInvalidationAfterAuthDisable)
	t.Run("AfterActiveProfileActivation", runtimeCacheInvalidationAfterActiveProfileActivation)
	t.Run("PlanningCacheInvalidationAfterHeaderBlocklistWrite", runtimePlanningCacheInvalidationAfterHeaderBlocklistWrite)
	t.Run("PlanningCacheInvalidationAfterOwnerScopedConnectionAndTargetMutations", runtimePlanningCacheInvalidationAfterOwnerScopedConnectionAndTargetMutations)
	t.Run("DashboardTopologyRepublishAfterModelMutation", TestRuntimeDashboardTopologyRepublishAfterModelMutation)
}

func TestRuntimeCacheInvalidationAfterContextOverflowPromotionTargetMutation(t *testing.T) {
	t.Run("target mutation starts at source and promotes to new target", func(t *testing.T) {
		fixture := newOverflowAffinityCacheRuntimeFixture(t, "target-mutation", http.StatusBadRequest, runtimeOverflowErrorPayload("source overflow refreshes target"), http.StatusOK, map[string]any{
			"id":     "chatcmpl-target-a-before-mutation",
			"object": "chat.completion",
			"usage":  map[string]any{"prompt_tokens": 6, "completion_tokens": 4, "total_tokens": 10},
		})
		targetBModelID := "overflow-affinity-target-mutation-target-b-" + randomSuffix()
		targetBUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
			"id":     "chatcmpl-target-b-after-mutation",
			"object": "chat.completion",
			"usage":  map[string]any{"prompt_tokens": 7, "completion_tokens": 5, "total_tokens": 12},
		})
		seedRuntimePromotionNativeModel(t, fixture.harness, fixture.profileID, targetBModelID, targetBUpstream.baseURL("/overflow/affinity/target-mutation/target-b"), "overflow-affinity-target-mutation-target-b-key", nil, 32_768)

		firstResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "first overflowing request", overflowAffinityCacheHeaders())
		assertStatus(t, firstResponse, http.StatusOK)
		beforeMutationGeneration := fixture.harness.runtimeCache.PublishedGeneration()
		setRuntimeHarnessPromotionTarget(t, fixture.harness, fixture.profileID, fixture.route.TargetModelID, targetBModelID)
		if got := fixture.harness.runtimeCache.PublishedGeneration(); got <= beforeMutationGeneration {
			t.Fatalf("expected promotion-target mutation to publish a newer runtime snapshot generation, got %d after %d", got, beforeMutationGeneration)
		}
		secondResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "second matching request", overflowAffinityCacheHeaders())
		assertStatus(t, secondResponse, http.StatusOK)

		assertOverflowAffinityRequestSequence(t, fixture.sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
			Path:    "/overflow/affinity/target-mutation/source/v1/chat/completions",
			ModelID: fixture.route.TargetModelID,
		}, {
			Path:    "/overflow/affinity/target-mutation/source/v1/chat/completions",
			ModelID: fixture.route.TargetModelID,
		}})
		assertOverflowAffinityRequestSequence(t, fixture.promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
			Path:    "/overflow/affinity/target-mutation/promoted/v1/chat/completions",
			ModelID: fixture.promotedModelID,
		}})
		assertOverflowAffinityRequestSequence(t, targetBUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
			Path:    "/overflow/affinity/target-mutation/target-b/v1/chat/completions",
			ModelID: targetBModelID,
		}})
	})

	t.Run("same terminal target starts at source and stops before promoted transport", func(t *testing.T) {
		fixture := newOverflowAffinityCacheRuntimeFixture(t, "same-terminal", http.StatusBadRequest, runtimeOverflowErrorPayload("source overflow stays final after same terminal"), http.StatusOK, map[string]any{
			"id":     "chatcmpl-same-terminal-before-mutation",
			"object": "chat.completion",
			"usage":  map[string]any{"prompt_tokens": 6, "completion_tokens": 4, "total_tokens": 10},
		})

		firstResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "first overflowing request", overflowAffinityCacheHeaders())
		assertStatus(t, firstResponse, http.StatusOK)
		beforeMutationGeneration := fixture.harness.runtimeCache.PublishedGeneration()
		setRuntimeHarnessPromotionTarget(t, fixture.harness, fixture.profileID, fixture.route.TargetModelID, fixture.route.TargetModelID)
		if got := fixture.harness.runtimeCache.PublishedGeneration(); got <= beforeMutationGeneration {
			t.Fatalf("expected same-terminal mutation to publish a newer runtime snapshot generation, got %d after %d", got, beforeMutationGeneration)
		}
		secondResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "second matching request", overflowAffinityCacheHeaders())
		assertStatus(t, secondResponse, http.StatusBadRequest)

		assertOverflowAffinityRequestSequence(t, fixture.sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
			Path:    "/overflow/affinity/same-terminal/source/v1/chat/completions",
			ModelID: fixture.route.TargetModelID,
		}, {
			Path:    "/overflow/affinity/same-terminal/source/v1/chat/completions",
			ModelID: fixture.route.TargetModelID,
		}})
		assertOverflowAffinityRequestSequence(t, fixture.promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
			Path:    "/overflow/affinity/same-terminal/promoted/v1/chat/completions",
			ModelID: fixture.promotedModelID,
		}})
	})
}

func TestOverflowAffinityCacheInvalidPromotionTargetUsesSourceFirst(t *testing.T) {
	fixture := newOverflowAffinityCacheRuntimeFixture(t, "disabled-target", http.StatusBadRequest, runtimeOverflowErrorPayload("source overflow stays final after disable"), http.StatusOK, map[string]any{
		"id":     "chatcmpl-disabled-target-before-disable",
		"object": "chat.completion",
		"usage":  map[string]any{"prompt_tokens": 6, "completion_tokens": 4, "total_tokens": 10},
	})

	firstResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "first overflowing request", overflowAffinityCacheHeaders())
	assertStatus(t, firstResponse, http.StatusOK)
	beforeDisableGeneration := fixture.harness.runtimeCache.PublishedGeneration()
	setRuntimeHarnessModelEnabled(t, fixture.harness, fixture.profileID, fixture.promotedModelID, false)
	if got := fixture.harness.runtimeCache.PublishedGeneration(); got <= beforeDisableGeneration {
		t.Fatalf("expected target disable to publish a newer runtime snapshot generation, got %d after %d", got, beforeDisableGeneration)
	}
	secondResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "second matching request", overflowAffinityCacheHeaders())
	assertStatus(t, secondResponse, http.StatusBadRequest)

	assertOverflowAffinityRequestSequence(t, fixture.sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/affinity/disabled-target/source/v1/chat/completions",
		ModelID: fixture.route.TargetModelID,
	}, {
		Path:    "/overflow/affinity/disabled-target/source/v1/chat/completions",
		ModelID: fixture.route.TargetModelID,
	}})
	assertOverflowAffinityRequestSequence(t, fixture.promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/affinity/disabled-target/promoted/v1/chat/completions",
		ModelID: fixture.promotedModelID,
	}})
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

func runtimeAuthCacheInvalidationAfterProxyKeyRetire(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	seedRuntimeVerifiedAuthSettings(t, harness, "retire-admin", "retire-password-123", "retire@example.com")
	loginRuntimeHarness(t, harness, "retire-admin", "retire-password-123")

	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-runtime-cache-retire"})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "retire-public-" + randomSuffix(),
		TargetModelID:   "retire-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/cache-invalidation/retire"),
		EndpointAPIKey:  "cache-retire-upstream-key",
	})

	keyName := "Phase 2 runtime key retirement"
	keyID, rawKey := createRuntimeProxyKey(t, harness, keyName)

	baselineResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "pre-retire request"}}, "model": route.PublicModelID},
		map[string]string{"Authorization": "Bearer " + rawKey},
	)
	assertStatus(t, baselineResponse, http.StatusOK)

	retireRuntimeProxyKey(t, harness, keyID, keyName)
	retireVisibleAt := time.Now()
	retiredResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "post-retire request"}}, "model": route.PublicModelID},
		map[string]string{"Authorization": "Bearer " + rawKey},
	)
	assertStatus(t, retiredResponse, http.StatusUnauthorized)
	assertRuntimeCacheInvalidationVisibleWithin(t, retireVisibleAt, "proxy-key retirement stale-key rejection")
	if got := len(upstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected retired key request to stop before the upstream, got %d upstream requests", got)
	}
}

func runtimeAuthCacheInvalidationAfterProxyKeyExpiryMutation(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	seedRuntimeVerifiedAuthSettings(t, harness, "expire-admin", "expire-password-123", "expire@example.com")
	loginRuntimeHarness(t, harness, "expire-admin", "expire-password-123")

	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-runtime-cache-expire"})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "expire-public-" + randomSuffix(),
		TargetModelID:   "expire-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/cache-invalidation/expire"),
		EndpointAPIKey:  "cache-expire-upstream-key",
	})

	keyName := "Phase 2 runtime key expiry"
	keyID, rawKey := createRuntimeProxyKey(t, harness, keyName)

	baselineResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "pre-expiry request"}}, "model": route.PublicModelID},
		map[string]string{"Authorization": "Bearer " + rawKey},
	)
	assertStatus(t, baselineResponse, http.StatusOK)

	expireRuntimeProxyKey(t, harness, keyID, keyName, time.Now().UTC().Add(-1*time.Minute).Truncate(time.Microsecond))
	expiryVisibleAt := time.Now()
	expiredResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "post-expiry request"}}, "model": route.PublicModelID},
		map[string]string{"Authorization": "Bearer " + rawKey},
	)
	assertStatus(t, expiredResponse, http.StatusUnauthorized)
	assertRuntimeCacheInvalidationVisibleWithin(t, expiryVisibleAt, "proxy-key expiry stale-key rejection")
	if got := len(upstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected expired key request to stop before the upstream, got %d upstream requests", got)
	}
}

func runtimeAuthCacheInvalidationAfterAuthDisable(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	seedRuntimeVerifiedAuthSettings(t, harness, "disable-admin", "disable-password-123", "disable@example.com")
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{Auth: true})
	loginRuntimeHarness(t, harness, "disable-admin", "disable-password-123")

	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-runtime-cache-auth-disable"})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "disable-public-" + randomSuffix(),
		TargetModelID:   "disable-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/cache-invalidation/auth-disable"),
		EndpointAPIKey:  "cache-disable-upstream-key",
	})

	baselineUnauthorized := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "auth-required baseline"}}, "model": route.PublicModelID},
		nil,
	)
	assertStatus(t, baselineUnauthorized, http.StatusUnauthorized)

	disableResponse := harness.requestJSON(
		t,
		http.MethodPut,
		"/api/settings/auth",
		map[string]any{"auth_enabled": false},
		nil,
	)
	assertStatus(t, disableResponse, http.StatusOK)

	disableVisibleAt := time.Now()
	postDisableResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "post-disable runtime request"}}, "model": route.PublicModelID},
		nil,
	)
	assertStatus(t, postDisableResponse, http.StatusOK)
	assertRuntimeCacheInvalidationVisibleWithin(t, disableVisibleAt, "auth disable runtime publication")
	if got := len(upstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected runtime request without proxy key to reach upstream immediately after auth disable, got %d upstream requests", got)
	}
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

func runtimePlanningCacheInvalidationAfterOwnerScopedConnectionAndTargetMutations(t *testing.T) {
	t.Run("OwnerScopedConnectionRoutes", runtimePlanningCacheInvalidationAfterOwnerScopedConnectionRoutes)
	t.Run("ModelTargetRoutes", runtimePlanningCacheInvalidationAfterModelTargetRoutes)
}

func runtimePlanningCacheInvalidationAfterOwnerScopedConnectionRoutes(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	strategyID := harness.seedLegacyStrategy(t, profileID, "cache-owner-connections-"+suffix, "fill-first")
	publicModelID := "cache-owner-public-" + suffix
	ownerModelID := "cache-owner-target-" + suffix
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
	ownerModelConfigID := harness.seedModel(t, profileID, "openai", ownerModelID, "native", &strategyID)
	harness.seedProxyTarget(t, publicModelConfigID, ownerModelConfigID)

	createUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-owner-connection-create"})
	createEndpointID := harness.seedEndpoint(t, profileID, "cache-owner-create-endpoint-"+suffix, createUpstream.baseURL("/cache-invalidation/owner-connection-create"), "owner-create-key", 0)
	baselineResponse := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"messages": []map[string]any{{"role": "user", "content": "before owner connection create"}}, "model": publicModelID}, nil)
	if baselineResponse.StatusCode == http.StatusOK {
		t.Fatalf("expected runtime request before owner connection create to fail, got %d", baselineResponse.StatusCode)
	}

	generation := harness.runtimeCache.PublishedGeneration()
	createResponse := harness.requestJSON(t, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelConfigID), map[string]any{"endpoint_id": createEndpointID, "name": "cache owner created", "is_active": true, "openai_probe_endpoint_variant": "chat_completions_reasoning_none"}, runtimeModelHeader(profileID))
	assertStatus(t, createResponse, http.StatusCreated)
	harness.waitForRuntimeSnapshotGeneration(t, generation)
	var createPayload map[string]any
	decodeJSONResponse(t, createResponse, &createPayload)
	createdConnectionID := jsonInt(t, createPayload["id"])
	assertRuntimeRequestRoutesToScriptedUpstream(t, harness, publicModelID, ownerModelID, createUpstream, "/cache-invalidation/owner-connection-create/v1/chat/completions")

	updateUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-owner-connection-update"})
	updateEndpointID := harness.seedEndpoint(t, profileID, "cache-owner-update-endpoint-"+suffix, updateUpstream.baseURL("/cache-invalidation/owner-connection-update"), "owner-update-key", 1)
	generation = harness.runtimeCache.PublishedGeneration()
	updateResponse := harness.requestJSON(t, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", ownerModelConfigID, createdConnectionID), map[string]any{"endpoint_id": updateEndpointID}, runtimeModelHeader(profileID))
	assertStatus(t, updateResponse, http.StatusOK)
	harness.waitForRuntimeSnapshotGeneration(t, generation)
	assertRuntimeRequestRoutesToScriptedUpstream(t, harness, publicModelID, ownerModelID, updateUpstream, "/cache-invalidation/owner-connection-update/v1/chat/completions")

	remainingUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-owner-connection-delete"})
	remainingEndpointID := harness.seedEndpoint(t, profileID, "cache-owner-delete-endpoint-"+suffix, remainingUpstream.baseURL("/cache-invalidation/owner-connection-delete"), "owner-delete-key", 2)
	generation = harness.runtimeCache.PublishedGeneration()
	secondCreateResponse := harness.requestJSON(t, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelConfigID), map[string]any{"endpoint_id": remainingEndpointID, "name": "cache owner remaining", "is_active": true, "openai_probe_endpoint_variant": "chat_completions_reasoning_none"}, runtimeModelHeader(profileID))
	assertStatus(t, secondCreateResponse, http.StatusCreated)
	harness.waitForRuntimeSnapshotGeneration(t, generation)

	generation = harness.runtimeCache.PublishedGeneration()
	deleteResponse := harness.requestJSON(t, http.MethodDelete, fmt.Sprintf("/api/models/%d/connections/%d", ownerModelConfigID, createdConnectionID), nil, runtimeModelHeader(profileID))
	assertStatus(t, deleteResponse, http.StatusOK)
	harness.waitForRuntimeSnapshotGeneration(t, generation)
	assertRuntimeRequestRoutesToScriptedUpstream(t, harness, publicModelID, ownerModelID, remainingUpstream, "/cache-invalidation/owner-connection-delete/v1/chat/completions")
}

func runtimePlanningCacheInvalidationAfterModelTargetRoutes(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	strategyID := harness.seedLegacyStrategy(t, profileID, "cache-model-targets-"+suffix, "fill-first")
	publicModelID := "cache-target-public-" + suffix
	targetAModelID := "cache-target-a-" + suffix
	targetBModelID := "cache-target-b-" + suffix
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
	targetAModelConfigID := harness.seedModel(t, profileID, "openai", targetAModelID, "native", &strategyID)
	targetBModelConfigID := harness.seedModel(t, profileID, "openai", targetBModelID, "native", &strategyID)
	targetAUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-model-target-a"})
	targetBUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-model-target-b"})
	targetAEndpointID := harness.seedEndpoint(t, profileID, "cache-target-a-endpoint-"+suffix, targetAUpstream.baseURL("/cache-invalidation/model-target-a"), "target-a-key", 0)
	targetBEndpointID := harness.seedEndpoint(t, profileID, "cache-target-b-endpoint-"+suffix, targetBUpstream.baseURL("/cache-invalidation/model-target-b"), "target-b-key", 1)
	harness.seedConnection(t, profileID, targetAModelConfigID, targetAEndpointID, "cache-target-a-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, targetBModelConfigID, targetBEndpointID, "cache-target-b-connection-"+suffix, nil, nil, 0)

	baselineResponse := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"messages": []map[string]any{{"role": "user", "content": "before model target create"}}, "model": publicModelID}, nil)
	if baselineResponse.StatusCode == http.StatusOK {
		t.Fatalf("expected runtime request before model target create to fail, got %d", baselineResponse.StatusCode)
	}

	generation := harness.runtimeCache.PublishedGeneration()
	createResponse := harness.requestJSON(t, http.MethodPost, fmt.Sprintf("/api/models/%d/targets", publicModelConfigID), map[string]any{"target_type": "model", "target_model_id": targetAModelID, "position": 0, "is_enabled": true}, runtimeModelHeader(profileID))
	assertStatus(t, createResponse, http.StatusCreated)
	harness.waitForRuntimeSnapshotGeneration(t, generation)
	assertRuntimeRequestRoutesToScriptedUpstream(t, harness, publicModelID, targetAModelID, targetAUpstream, "/cache-invalidation/model-target-a/v1/chat/completions")

	targetID := modelAccessTargetID(t, harness, publicModelConfigID, targetAModelConfigID)
	generation = harness.runtimeCache.PublishedGeneration()
	updateResponse := harness.requestJSON(t, http.MethodPut, fmt.Sprintf("/api/models/%d/targets/%d", publicModelConfigID, targetID), map[string]any{"target_type": "model", "target_model_id": targetBModelID, "position": 0, "is_enabled": true}, runtimeModelHeader(profileID))
	assertStatus(t, updateResponse, http.StatusOK)
	harness.waitForRuntimeSnapshotGeneration(t, generation)
	assertRuntimeRequestRoutesToScriptedUpstream(t, harness, publicModelID, targetBModelID, targetBUpstream, "/cache-invalidation/model-target-b/v1/chat/completions")

	generation = harness.runtimeCache.PublishedGeneration()
	secondCreateResponse := harness.requestJSON(t, http.MethodPost, fmt.Sprintf("/api/models/%d/targets", publicModelConfigID), map[string]any{"target_type": "model", "target_model_id": targetAModelID, "position": 1, "is_enabled": true}, runtimeModelHeader(profileID))
	assertStatus(t, secondCreateResponse, http.StatusCreated)
	harness.waitForRuntimeSnapshotGeneration(t, generation)
	targetATargetID := modelAccessTargetID(t, harness, publicModelConfigID, targetAModelConfigID)

	generation = harness.runtimeCache.PublishedGeneration()
	moveResponse := harness.requestJSON(t, http.MethodPatch, fmt.Sprintf("/api/models/%d/targets/%d/position", publicModelConfigID, targetATargetID), map[string]any{"to_index": 0}, runtimeModelHeader(profileID))
	assertStatus(t, moveResponse, http.StatusOK)
	harness.waitForRuntimeSnapshotGeneration(t, generation)
	assertRuntimeRequestRoutesToScriptedUpstream(t, harness, publicModelID, targetAModelID, targetAUpstream, "/cache-invalidation/model-target-a/v1/chat/completions")

	generation = harness.runtimeCache.PublishedGeneration()
	deleteResponse := harness.requestJSON(t, http.MethodDelete, fmt.Sprintf("/api/models/%d/targets/%d", publicModelConfigID, targetATargetID), nil, runtimeModelHeader(profileID))
	assertStatus(t, deleteResponse, http.StatusOK)
	harness.waitForRuntimeSnapshotGeneration(t, generation)
	assertRuntimeRequestRoutesToScriptedUpstream(t, harness, publicModelID, targetBModelID, targetBUpstream, "/cache-invalidation/model-target-b/v1/chat/completions")
}

func assertRuntimeRequestRoutesToScriptedUpstream(t *testing.T, harness *runtimeHarness, publicModelID string, targetModelID string, upstream *scriptedUpstream, wantPath string) {
	t.Helper()
	before := len(upstream.requestsSnapshot())
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"messages": []map[string]any{{"role": "user", "content": "assert runtime planning invalidation"}}, "model": publicModelID}, nil)
	assertStatus(t, response, http.StatusOK)
	requests := upstream.requestsSnapshot()
	if len(requests) != before+1 {
		t.Fatalf("expected one new upstream request to %s, got %d new requests", wantPath, len(requests)-before)
	}
	request := requests[len(requests)-1]
	if request.Path != wantPath {
		t.Fatalf("expected upstream path %q, got %q", wantPath, request.Path)
	}
	if got := requestModelID(t, request.Body); got != targetModelID {
		t.Fatalf("expected upstream model %q, got %q", targetModelID, got)
	}
}

func modelAccessTargetID(t *testing.T, harness *runtimeHarness, sourceModelConfigID int, targetModelConfigID int) int {
	t.Helper()
	var targetID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM model_access_targets WHERE source_model_config_id = $1 AND target_model_config_id = $2`, sourceModelConfigID, targetModelConfigID).Scan(&targetID); err != nil {
		t.Fatalf("load model access target %d -> %d: %v", sourceModelConfigID, targetModelConfigID, err)
	}
	return targetID
}

func assertRuntimeCacheInvalidationVisibleWithin(t *testing.T, startedAt time.Time, action string) {
	t.Helper()
	if elapsed := time.Since(startedAt); elapsed > runtimeCacheInvalidationVisibilityDeadline {
		t.Fatalf("expected %s to become visible to /v1 within %s, got %s", action, runtimeCacheInvalidationVisibilityDeadline, elapsed)
	}
}

func createRuntimeProxyKey(t *testing.T, harness *runtimeHarness, name string) (int, string) {
	t.Helper()
	response := harness.requestJSON(t, http.MethodPost, "/api/settings/auth/proxy-keys", map[string]any{"name": name}, nil)
	assertStatus(t, response, http.StatusCreated)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	item := payload["item"].(map[string]any)
	return jsonInt(t, item["id"]), itemlessString(t, payload["key"])
}

func rotateRuntimeProxyKey(t *testing.T, harness *runtimeHarness, keyID int) string {
	t.Helper()
	response := harness.requestJSON(t, http.MethodPost, fmt.Sprintf("/api/settings/auth/proxy-keys/%d/rotate", keyID), nil, nil)
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return itemlessString(t, payload["key"])
}

func retireRuntimeProxyKey(t *testing.T, harness *runtimeHarness, keyID int, name string) {
	t.Helper()
	response := harness.requestJSON(t, http.MethodPatch, fmt.Sprintf("/api/settings/auth/proxy-keys/%d", keyID), map[string]any{"name": name, "is_active": false}, nil)
	assertStatus(t, response, http.StatusOK)
}

func expireRuntimeProxyKey(t *testing.T, harness *runtimeHarness, keyID int, name string, expiresAt time.Time) {
	t.Helper()
	response := harness.requestJSON(t, http.MethodPatch, fmt.Sprintf("/api/settings/auth/proxy-keys/%d", keyID), map[string]any{"name": name, "expires_at": expiresAt}, nil)
	assertStatus(t, response, http.StatusOK)
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
