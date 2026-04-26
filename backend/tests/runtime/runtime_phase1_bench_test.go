package runtime_test

import (
	"net/http"
	"testing"
	"time"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

var runtimeCompiledSnapshotPlanningSink any
var runtimeCompiledSnapshotAuthSettingsSink any
var runtimeCompiledSnapshotProxyKeyDecisionSink any

func BenchmarkRuntimePublishedSnapshotHit(b *testing.B) {
	harness := newRuntimeHarnessWithConfig(b, runtimeHarnessConfig{SettingsMutator: useBenchmarkRuntimeTransportOverrides})
	profileID := harness.activeProfileID(b)
	upstream := newRuntimeBenchmarkUpstream(b, http.StatusOK, runtimeBenchmarkHotPathResponse())
	route := harness.seedProxyRoute(b, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "benchmark-phase1-hit-public-" + randomSuffix(),
		TargetModelID:   "benchmark-phase1-hit-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/benchmark/phase1/hit"),
		EndpointAPIKey:  "benchmark-phase1-hit-key",
	})
	rawBody := runtimeBenchmarkRequestBody(b, route.PublicModelID, "phase-1 published snapshot hit benchmark")

	statusCode, _, err := performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
	if err != nil {
		b.Fatalf("warm published snapshot hit request: %v", err)
	}
	if statusCode != http.StatusOK {
		b.Fatalf("expected warm published snapshot hit status 200, got %d", statusCode)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		statusCode, _, err = performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
		if err != nil {
			b.Fatalf("run published snapshot hit request: %v", err)
		}
		if statusCode != http.StatusOK {
			b.Fatalf("expected published snapshot hit status 200, got %d", statusCode)
		}
	}
}

func BenchmarkRuntimePublishedSnapshotRefreshStorm(b *testing.B) {
	cache := runtimeapi.NewSharedCache(0)
	harness := newRuntimeHarnessWithConfig(b, runtimeHarnessConfig{
		RuntimeOptions:  runtimeapi.Options{Cache: cache},
		SettingsMutator: useBenchmarkRuntimeTransportOverrides,
	})
	profileID := harness.activeProfileID(b)
	upstream := newRuntimeBenchmarkUpstream(b, http.StatusOK, runtimeBenchmarkHotPathResponse())
	route := harness.seedProxyRoute(b, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "benchmark-phase1-refresh-public-" + randomSuffix(),
		TargetModelID:   "benchmark-phase1-refresh-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/benchmark/phase1/refresh-storm"),
		EndpointAPIKey:  "benchmark-phase1-refresh-key",
	})
	rawBody := runtimeBenchmarkRequestBody(b, route.PublicModelID, "phase-1 published snapshot refresh storm benchmark")

	statusCode, _, err := performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
	if err != nil {
		b.Fatalf("warm published snapshot refresh storm request: %v", err)
	}
	if statusCode != http.StatusOK {
		b.Fatalf("expected warm published snapshot refresh storm status 200, got %d", statusCode)
	}

	generation := harness.runtimeCache.PublishedGeneration()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		harness.runtimeCache.ScheduleRefresh(runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
		generation = harness.waitForRuntimeSnapshotGeneration(b, generation)
		if err := runRuntimeBenchmarkStorm(harness.client, harness.url+"/v1/chat/completions", rawBody, runtimeCacheMissStormConcurrency); err != nil {
			b.Fatalf("run published snapshot refresh storm benchmark: %v", err)
		}
	}
}

func BenchmarkRuntimeCompiledSnapshotReadAllocs(b *testing.B) {
	harness := newRuntimePhase0HarnessWithOptions(b, runtimePhase0HarnessOptions{SettingsMutator: useBenchmarkRuntimeTransportOverrides})
	profileID := harness.activeProfileID(b)
	harness.seedProxyRoute(b, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "benchmark-phase1-read-public-" + randomSuffix(),
		TargetModelID:   "benchmark-phase1-read-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/benchmark/phase1/read-allocs"),
		EndpointAPIKey:  "benchmark-phase1-read-key",
	})
	proxyAPIKey := harness.enableRuntimeProxyAPIKeyAuth(b)
	authCache := managementauth.NewRuntimeCacheFromShared(harness.runtimeCache)
	referenceNow := time.Unix(100, 0).UTC()

	planning, err := harness.runtimeCache.LoadPublishedPlanningSnapshot(profileID)
	if err != nil {
		b.Fatalf("load published planning snapshot: %v", err)
	}
	if planning == nil {
		b.Fatal("expected published planning snapshot")
	}
	authSettings, err := authCache.LoadRuntimeAuthSettings()
	if err != nil {
		b.Fatalf("load published runtime auth settings: %v", err)
	}
	if !authSettings.AuthEnabled {
		b.Fatalf("expected published runtime auth settings to be enabled, got %+v", authSettings)
	}
	decision, err := authCache.LoadRuntimeProxyKeyDecision(referenceNow, proxyAPIKey)
	if err != nil {
		b.Fatalf("load published runtime proxy key decision: %v", err)
	}
	if !decision.Allowed {
		b.Fatalf("expected published runtime proxy key decision to allow the benchmark key, got %+v", decision)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		planning, err = harness.runtimeCache.LoadPublishedPlanningSnapshot(profileID)
		if err != nil {
			b.Fatalf("load published planning snapshot: %v", err)
		}
		authSettings, err = authCache.LoadRuntimeAuthSettings()
		if err != nil {
			b.Fatalf("load published runtime auth settings: %v", err)
		}
		decision, err = authCache.LoadRuntimeProxyKeyDecision(referenceNow, proxyAPIKey)
		if err != nil {
			b.Fatalf("load published runtime proxy key decision: %v", err)
		}
		if planning == nil || !authSettings.AuthEnabled || !decision.Allowed {
			b.Fatalf("expected compiled snapshot reads to stay valid, got planning=%v auth=%+v decision=%+v", planning, authSettings, decision)
		}
		runtimeCompiledSnapshotPlanningSink = planning
		runtimeCompiledSnapshotAuthSettingsSink = authSettings
		runtimeCompiledSnapshotProxyKeyDecisionSink = decision
	}
}
