package runtime_test

import (
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func TestRuntimeHotPathUsesPublishedPlanningSnapshotOnly(t *testing.T) {
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
		EndpointAPIKey:  "phase1-planning-key",
	})

	response, observed := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 planning hot path"}},
		"model":    route.PublicModelID,
	}, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected published planning runtime request status 200, got %d with body %s", response.StatusCode, readResponseBody(t, response))
	}
	observed.assertExcludesCategory(t, runtimeSQLCategoryPlanningSnapshotWarm)
	observed.assertExcludesCategory(t, runtimeSQLCategoryAuthSnapshotWarm)
	observed.assertExcludesCategory(t, runtimeSQLCategoryRuntimeStateTables)

	request := upstream.lastRequest(t)
	if request.Path != "/phase1/planning/v1/chat/completions" {
		t.Fatalf("expected published planning upstream path %q, got %q", "/phase1/planning/v1/chat/completions", request.Path)
	}
	if got := requestModelID(t, request.Body); got != route.TargetModelID {
		t.Fatalf("expected published planning upstream model %q, got %q", route.TargetModelID, got)
	}
}

func TestRuntimeHotPathUsesPublishedAuthSnapshotOnly(t *testing.T) {
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
		EndpointAPIKey:  "phase1-auth-key",
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
	observed.assertExcludesCategory(t, runtimeSQLCategoryAuthSnapshotWarm)
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
		EndpointAPIKey:  "phase1-mutation-key",
	})

	baselineResponse, baselineObserved := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 baseline before mutation"}},
		"model":    route.PublicModelID,
	}, map[string]string{blockedHeaderName: "before-mutation", allowedHeaderName: "still-allowed"})
	if baselineResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected baseline mutation runtime request status 200, got %d with body %s", baselineResponse.StatusCode, readResponseBody(t, baselineResponse))
	}
	baselineObserved.assertExcludesCategory(t, runtimeSQLCategoryPlanningSnapshotWarm)
	baselineObserved.assertExcludesCategory(t, runtimeSQLCategoryAuthSnapshotWarm)
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
	}, map[string]string{blockedHeaderName: "removed-after-publish", allowedHeaderName: "still-allowed"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected post-mutation runtime request status 200, got %d with body %s", response.StatusCode, readResponseBody(t, response))
	}
	observed.assertExcludesCategory(t, runtimeSQLCategoryPlanningSnapshotWarm)
	observed.assertExcludesCategory(t, runtimeSQLCategoryAuthSnapshotWarm)

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
		EndpointAPIKey:  "phase1-failure-key",
	})

	baselineResponse, baselineObserved := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 refresh failure baseline"}},
		"model":    route.PublicModelID,
	}, map[string]string{blockedHeaderName: "still-present-before-failure", allowedHeaderName: "still-allowed"})
	if baselineResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected refresh-failure baseline status 200, got %d with body %s", baselineResponse.StatusCode, readResponseBody(t, baselineResponse))
	}
	baselineObserved.assertExcludesCategory(t, runtimeSQLCategoryPlanningSnapshotWarm)
	baselineObserved.assertExcludesCategory(t, runtimeSQLCategoryAuthSnapshotWarm)
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
	failedResponse, failedObserved := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 refresh failure fallback"}},
		"model":    route.PublicModelID,
	}, map[string]string{blockedHeaderName: "still-present-after-failure", allowedHeaderName: "still-allowed"})
	if failedResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected runtime request to keep serving last good snapshot after refresh failure, got %d with body %s", failedResponse.StatusCode, readResponseBody(t, failedResponse))
	}
	failedObserved.assertExcludesCategory(t, runtimeSQLCategoryPlanningSnapshotWarm)
	failedObserved.assertExcludesCategory(t, runtimeSQLCategoryAuthSnapshotWarm)

	request := harness.upstream.lastRequest(t)
	if request.Headers.Get(blockedHeaderName) != "still-present-after-failure" {
		t.Fatalf("expected failed refresh to retain last good snapshot header state, got %q", request.Headers.Get(blockedHeaderName))
	}
	if request.Headers.Get(allowedHeaderName) != "still-allowed" {
		t.Fatalf("expected allowed header %s to survive failed refresh, got %q", allowedHeaderName, request.Headers.Get(allowedHeaderName))
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
		EndpointAPIKey:  "phase1-compiled-key",
	})

	baselineResponse, baselineObserved := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-1 compiled snapshot baseline"}},
		"model":    route.PublicModelID,
	}, map[string]string{blockedHeaderName: "before-compiled-publish"})
	if baselineResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected compiled-snapshot baseline status 200, got %d with body %s", baselineResponse.StatusCode, readResponseBody(t, baselineResponse))
	}
	baselineObserved.assertExcludesCategory(t, runtimeSQLCategoryPlanningSnapshotWarm)
	baselineObserved.assertExcludesCategory(t, runtimeSQLCategoryAuthSnapshotWarm)
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
	observed.assertExcludesCategory(t, runtimeSQLCategoryAuthSnapshotWarm)
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
