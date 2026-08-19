package runtimetest

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func testEndpointCredential(label string) string {
	return label + "-" + strings.Repeat("x", 24)
}

func TestRuntimePhase1Snapshot_HotPathUsesPublishedPlanningSnapshotOnly(t *testing.T) {
	harness := newRuntimePhase0Harness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-phase1-planning-hot-path"})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "phase1-planning-public-" + suffix,
		TargetModelID:   "phase1-planning-target-" + suffix,
		EndpointBaseURL: upstream.baseURL("/phase1/planning"),
		EndpointAPIKey:  testEndpointCredential("phase1-planning"),
	})

	response, observed := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 planning hot path"}},
		"model":    route.PublicModelID,
	}, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected published planning runtime request status 200, got %d with body %s", response.StatusCode, readResponseBody(t, response))
	}
	observed.assertExcludesCategory(t, runtimeSQLCategoryPlanningSnapshotWarm)
	observed.assertExcludesCategory(t, runtimeSQLCategoryAuthWarm)
	observed.assertExcludesCategory(t, runtimeSQLCategoryRuntimeStateTables)

	request := upstream.lastRequest(t)
	if request.Path != "/phase1/planning/v1/chat/completions" {
		t.Fatalf("expected published planning upstream path %q, got %q", "/phase1/planning/v1/chat/completions", request.Path)
	}
	if got := requestModelID(t, request.Body); got != route.TargetModelID {
		t.Fatalf("expected published planning upstream model %q, got %q", route.TargetModelID, got)
	}
}

func TestRuntimePhase1Snapshot_PinsPlanningToDefaultProfile(t *testing.T) {
	harness := newRuntimePhase0Harness(t)
	defaultProfileID := harness.activeProfileID(t)
	shadowProfileID := harness.createProfile(t, "Phase1 Shadow Profile")
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-phase1-default-profile"})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       defaultProfileID,
		APIFamily:       "openai",
		PublicModelID:   "phase1-default-public-" + suffix,
		TargetModelID:   "phase1-default-target-" + suffix,
		EndpointBaseURL: upstream.baseURL("/phase1/default-profile"),
		EndpointAPIKey:  testEndpointCredential("phase1-default"),
	})

	harness.forceActiveProfile(t, shadowProfileID)

	defaultProfile, planning, err := harness.runtimeCache.LoadFreshDefaultRuntimePlan(context.Background())
	if err != nil {
		t.Fatalf("load fresh default runtime plan: %v", err)
	}
	if defaultProfile.ID != defaultProfileID {
		t.Fatalf("expected frozen Default profile id %d, got %d", defaultProfileID, defaultProfile.ID)
	}
	if _, ok := planning.ModelsByID[route.PublicModelID]; !ok {
		t.Fatalf("expected default profile planning snapshot to contain %q, got %+v", route.PublicModelID, planning.ModelsByID)
	}

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "phase-1 default profile planning"}},
			"model":    route.PublicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)

	request := upstream.lastRequest(t)
	if request.Path != "/phase1/default-profile/v1/chat/completions" {
		t.Fatalf("expected default-profile upstream path %q, got %q", "/phase1/default-profile/v1/chat/completions", request.Path)
	}
	if got := requestModelID(t, request.Body); got != route.TargetModelID {
		t.Fatalf("expected default-profile upstream model %q, got %q", route.TargetModelID, got)
	}
}

func TestRuntimeHotPathUsesPublishedAuthStateOnly(t *testing.T) {
	harness := newRuntimePhase0Harness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-phase1-auth-hot-path"})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "phase1-auth-public-" + suffix,
		TargetModelID:   "phase1-auth-target-" + suffix,
		EndpointBaseURL: upstream.baseURL("/phase1/auth"),
		EndpointAPIKey:  testEndpointCredential("phase1-auth"),
	})
	proxyAPIKey := harness.enableRuntimeProxyAPIKeyAuth(t)

	response, observed := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 auth hot path"}},
		"model":    route.PublicModelID,
	}, map[string]string{"Authorization": "Bearer " + proxyAPIKey})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected published auth runtime request status 200, got %d with body %s", response.StatusCode, readResponseBody(t, response))
	}
	observed.assertExcludesCategory(t, runtimeSQLCategoryPlanningSnapshotWarm)
	observed.assertExcludesCategory(t, runtimeSQLCategoryAuthWarm)
	observed.assertExcludesCategory(t, runtimeSQLCategoryProxyKeyUsageWrite)

	request := upstream.lastRequest(t)
	if request.Path != "/phase1/auth/v1/chat/completions" {
		t.Fatalf("expected published auth upstream path %q, got %q", "/phase1/auth/v1/chat/completions", request.Path)
	}
	if got := request.Headers.Get("Authorization"); got != "Bearer "+route.EndpointAPIKey {
		t.Fatalf("expected upstream authorization header %q, got %q", "Bearer "+route.EndpointAPIKey, got)
	}
	if got := requestModelID(t, request.Body); got != route.TargetModelID {
		t.Fatalf("expected published auth upstream model %q, got %q", route.TargetModelID, got)
	}
}

func TestRuntimeManagementMutationPublishesNewSnapshotGeneration(t *testing.T) {
	harness := newRuntimePhase0Harness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	blockedHeaderName := "X-Phase1-Blocked"
	allowedHeaderName := "X-Phase1-Allowed"
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "phase1-mutation-public-" + suffix,
		TargetModelID:   "phase1-mutation-target-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/phase1/mutation"),
		EndpointAPIKey:  testEndpointCredential("phase1-mutation"),
		// Probes ride on connection custom headers: client headers outside the
		// protocol allowlist never reach an upstream, so they cannot observe a
		// snapshot refresh. Custom headers cross and stay subject to the
		// blocklist, which is the signal these tests need.
		CustomHeaders: map[string]any{
			"X-Phase1-Blocked": "before-mutation",
			"X-Phase1-Allowed": "still-allowed",
		},
	})

	baselineResponse, baselineObserved := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 baseline before mutation"}},
		"model":    route.PublicModelID,
	}, nil)
	if baselineResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected baseline mutation runtime request status 200, got %d with body %s", baselineResponse.StatusCode, readResponseBody(t, baselineResponse))
	}
	baselineObserved.assertExcludesCategory(t, runtimeSQLCategoryPlanningSnapshotWarm)
	baselineObserved.assertExcludesCategory(t, runtimeSQLCategoryAuthWarm)
	if got := harness.upstream.lastRequest(t).Headers.Get(blockedHeaderName); got != "before-mutation" {
		t.Fatalf("expected blocked header to pass before mutation, got %q", got)
	}

	generation := harness.runtimeCache.PublishedGeneration()
	createRuntimeHeaderBlocklistRule(t, harness.runtimeHarness, profileID, "Phase 1 published mutation "+suffix, "exact", "x-phase1-blocked")
	nextGeneration := harness.runtimeCache.PublishedGeneration()
	if nextGeneration <= generation {
		t.Fatalf("expected management mutation to publish a new generation beyond %d, got %d", generation, nextGeneration)
	}

	harness.upstream.clear()
	response, observed := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 published mutation"}},
		"model":    route.PublicModelID,
	}, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected post-mutation runtime request status 200, got %d with body %s", response.StatusCode, readResponseBody(t, response))
	}
	observed.assertExcludesCategory(t, runtimeSQLCategoryPlanningSnapshotWarm)
	observed.assertExcludesCategory(t, runtimeSQLCategoryAuthWarm)

	request := harness.upstream.lastRequest(t)
	if request.Headers.Get(blockedHeaderName) != "" {
		t.Fatalf("expected blocked header %s to be removed after published mutation, got %q", blockedHeaderName, request.Headers.Get(blockedHeaderName))
	}
	if request.Headers.Get(allowedHeaderName) != "still-allowed" {
		t.Fatalf("expected allowed header %s to survive mutation refresh, got %q", allowedHeaderName, request.Headers.Get(allowedHeaderName))
	}
}

func TestRuntimeRefreshFailureKeepsLastGoodSnapshot(t *testing.T) {
	injectedErr := errors.New("injected published snapshot refresh failure")
	var failRefresh atomic.Bool
	refreshAttempts := make(chan runtimeapi.RefreshRequest, 1)
	cache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{
		BeforePublish: func(request runtimeapi.RefreshRequest) error {
			if failRefresh.Load() {
				select {
				case refreshAttempts <- request:
				default:
				}
				return injectedErr
			}
			return nil
		},
	})
	harness := newRuntimePhase0HarnessWithOptions(t, runtimePhase0HarnessOptions{RuntimeOptions: runtimeapi.Options{Cache: cache}})
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	blockedHeaderName := "X-Phase1-Failure-Blocked"
	allowedHeaderName := "X-Phase1-Failure-Allowed"
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "phase1-failure-public-" + suffix,
		TargetModelID:   "phase1-failure-target-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/phase1/failure"),
		EndpointAPIKey:  testEndpointCredential("phase1-failure"),
		// Probes ride on connection custom headers: client headers outside the
		// protocol allowlist never reach an upstream, so they cannot observe a
		// snapshot refresh. Custom headers cross and stay subject to the
		// blocklist, which is the signal these tests need.
		CustomHeaders: map[string]any{
			"X-Phase1-Failure-Blocked": "still-present-before-failure",
			"X-Phase1-Failure-Allowed": "still-allowed",
		},
	})

	baselineResponse, baselineObserved := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 refresh failure baseline"}},
		"model":    route.PublicModelID,
	}, nil)
	if baselineResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected refresh-failure baseline status 200, got %d with body %s", baselineResponse.StatusCode, readResponseBody(t, baselineResponse))
	}
	baselineObserved.assertExcludesCategory(t, runtimeSQLCategoryPlanningSnapshotWarm)
	baselineObserved.assertExcludesCategory(t, runtimeSQLCategoryAuthWarm)
	if got := harness.upstream.lastRequest(t).Headers.Get(blockedHeaderName); got != "still-present-before-failure" {
		t.Fatalf("expected blocked header to still pass before injected refresh failure, got %q", got)
	}

	generation := harness.runtimeCache.PublishedGeneration()
	failRefresh.Store(true)
	response := harness.requestJSON(t, http.MethodPost, "/api/config/header-blocklist-rules", map[string]any{
		"name":       "Phase 1 failed mutation " + suffix,
		"match_type": "exact",
		"pattern":    "x-phase1-failure-blocked",
	}, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusCreated)

	select {
	case request := <-refreshAttempts:
		if !request.PlanningAll && len(request.PlanningProfileIDs) == 0 {
			t.Fatalf("expected failed refresh request to target planning data, got %+v", request)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for injected published snapshot refresh failure")
	}
	if got := harness.runtimeCache.PublishedGeneration(); got != generation {
		t.Fatalf("expected failed refresh to keep generation %d, got %d", generation, got)
	}

	harness.upstream.clear()
	failedResponse, _ := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 refresh failure fallback"}},
		"model":    route.PublicModelID,
	}, map[string]string{blockedHeaderName: "still-present-after-failure", allowedHeaderName: "still-allowed"})
	if failedResponse.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected stale runtime request to reject after refresh failure, got %d with body %s", failedResponse.StatusCode, readResponseBody(t, failedResponse))
	}
	if got := len(harness.upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected stale runtime request to stop before upstream after refresh failure, got %d upstream requests", got)
	}
}

func TestRuntimeCompiledSnapshotPublishesAuthAndRoutingTogether(t *testing.T) {
	harness := newRuntimePhase0Harness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	blockedHeaderName := "X-Phase1-Compiled-Blocked"
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "phase1-compiled-public-" + suffix,
		TargetModelID:   "phase1-compiled-target-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/phase1/compiled"),
		EndpointAPIKey:  testEndpointCredential("phase1-compiled"),
		// Probe rides on a connection custom header: client headers outside the
		// protocol allowlist never reach an upstream, so they cannot observe a
		// compiled-snapshot publish. Custom headers cross and stay subject to
		// the blocklist.
		CustomHeaders: map[string]any{
			"X-Phase1-Compiled-Blocked": "before-compiled-publish",
		},
	})

	baselineResponse, baselineObserved := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 compiled snapshot baseline"}},
		"model":    route.PublicModelID,
	}, nil)
	if baselineResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected compiled-snapshot baseline status 200, got %d with body %s", baselineResponse.StatusCode, readResponseBody(t, baselineResponse))
	}
	baselineObserved.assertExcludesCategory(t, runtimeSQLCategoryPlanningSnapshotWarm)
	baselineObserved.assertExcludesCategory(t, runtimeSQLCategoryAuthWarm)
	if got := harness.upstream.lastRequest(t).Headers.Get(blockedHeaderName); got != "before-compiled-publish" {
		t.Fatalf("expected blocked header to pass before compiled publish, got %q", got)
	}

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	proxyAPIKey := harness.enableRuntimeProxyAPIKeyAuth(t)
	harness.seedProfileHeaderBlocklistRule(t, profileID, "Phase 1 compiled snapshot "+suffix, "exact", "x-phase1-compiled-blocked")
	releaseRefresh()

	generation := harness.runtimeCache.PublishedGeneration()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{Auth: true, PlanningProfileIDs: []int{profileID}})
	if got := harness.runtimeCache.PublishedGeneration(); got != generation+1 {
		t.Fatalf("expected combined compiled snapshot publish to advance generation from %d to %d, got %d", generation, generation+1, got)
	}

	unauthorized := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 compiled snapshot unauthorized"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, unauthorized, http.StatusUnauthorized)

	harness.upstream.clear()
	response, observed := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 compiled snapshot authorized"}},
		"model":    route.PublicModelID,
	}, map[string]string{
		"Authorization":   "Bearer " + proxyAPIKey,
		blockedHeaderName: "removed-after-compiled-publish",
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected compiled-snapshot runtime request status 200, got %d with body %s", response.StatusCode, readResponseBody(t, response))
	}
	observed.assertExcludesCategory(t, runtimeSQLCategoryPlanningSnapshotWarm)
	observed.assertExcludesCategory(t, runtimeSQLCategoryAuthWarm)
	observed.assertExcludesCategory(t, runtimeSQLCategoryProxyKeyUsageWrite)

	request := harness.upstream.lastRequest(t)
	if request.Headers.Get(blockedHeaderName) != "" {
		t.Fatalf("expected blocked header %s to be removed after compiled publish, got %q", blockedHeaderName, request.Headers.Get(blockedHeaderName))
	}
	if got := request.Headers.Get("Authorization"); got != "Bearer "+route.EndpointAPIKey {
		t.Fatalf("expected compiled upstream authorization header %q, got %q", "Bearer "+route.EndpointAPIKey, got)
	}
	if got := requestModelID(t, request.Body); got != route.TargetModelID {
		t.Fatalf("expected compiled upstream model %q, got %q", route.TargetModelID, got)
	}
}
