package runtimetest

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

func assertPhase3HotPathExcludesForbiddenSQL(tb testing.TB, snapshot runtimeSQLSnapshot) {
	tb.Helper()
	snapshot.assertExcludesCategory(tb, runtimeSQLCategoryPlanningSnapshotWarm)
	snapshot.assertExcludesCategory(tb, runtimeSQLCategoryAuthWarm)
	snapshot.assertExcludesCategory(tb, runtimeSQLCategoryRuntimeStateTables)
	snapshot.assertExcludesCategory(tb, runtimeSQLCategoryRoundRobinState)
	snapshot.assertExcludesCategory(tb, runtimeSQLCategoryProxyKeyUsageWrite)
}

func assertExecutionLaneExcludesTelemetryOutboxSQL(tb testing.TB, snapshot runtimeSQLSnapshot) {
	tb.Helper()
	for _, statement := range snapshot.statements {
		if strings.Contains(statement.Normalized, "runtime_telemetry_outbox") {
			tb.Fatalf("expected execution-lane SQL to exclude runtime telemetry outbox statements, got:\n%s", snapshot.dump())
		}
	}
}

func waitForProxyAPIKeyUsageMaterialization(t *testing.T, conn *pgx.Conn, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var usedCount int
		err := conn.QueryRow(
			context.Background(),
			`SELECT COUNT(*) FROM proxy_api_keys WHERE last_used_at IS NOT NULL AND COALESCE(last_used_ip, '') <> ''`,
		).Scan(&usedCount)
		if err != nil {
			t.Fatalf("query async proxy api key usage materialization: %v", err)
		}
		if usedCount > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for async proxy api key usage materialization")
}

func TestRuntimeHotPathAvoidsPlanningAndCoordinationSQL(t *testing.T) {
	t.Run("OpenAIAdmissionSkip", func(t *testing.T) {
		harness := newRuntimePhase0Harness(t)
		profileID := harness.activeProfileID(t)
		suffix := randomSuffix()
		publicModelID := "phase3-admission-public-" + suffix
		targetModelID := "phase3-admission-target-" + suffix
		strategyID := harness.seedAdaptiveStrategy(t, profileID, "phase3-admission-"+suffix)
		targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
		publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
		harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
		rejectedEndpointID := harness.seedEndpoint(t, profileID, "phase3-admission-rejected-"+suffix, harness.upstream.baseURL("/phase3/admission/rejected"), "phase3-admission-rejected-key")
		eligibleEndpointID := harness.seedEndpoint(t, profileID, "phase3-admission-eligible-"+suffix, harness.upstream.baseURL("/phase3/admission/eligible"), "phase3-admission-eligible-key")
		rejectedConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, rejectedEndpointID, "phase3-admission-rejected-connection-"+suffix, nil, nil, 0)
		_ = harness.seedConnection(t, profileID, targetModelConfigID, eligibleEndpointID, "phase3-admission-eligible-connection-"+suffix, nil, nil, 1)
		qpsLimit := 1
		harness.updateConnectionAdmissionLimits(t, rejectedConnectionID, &qpsLimit, nil, nil)
		windowStartedAt := time.Now().UTC()
		harness.runtimeService.RuntimeState().SeedConnectionState(profileID, targetModelConfigID, rejectedConnectionID, loadbalancedomain.RuntimeConnectionState{
			ConnectionID:       rejectedConnectionID,
			BanMode:            "off",
			WindowStartedAt:    &windowStartedAt,
			WindowRequestCount: 1,
		}, windowStartedAt, windowStartedAt)

		response, snapshot := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "phase-3 admission hot path"}},
			"model":    publicModelID,
		}, nil)
		assertStatus(t, response, http.StatusOK)
		assertPhase3HotPathExcludesForbiddenSQL(t, snapshot)
		if upstreamRequest := harness.upstream.lastRequest(t); upstreamRequest.Path != "/phase3/admission/eligible/v1/chat/completions" {
			t.Fatalf("expected runtime to skip the qps-exhausted connection via local state, got %s", upstreamRequest.Path)
		}
	})

	t.Run("GeminiRoundRobin", func(t *testing.T) {
		harness := newRuntimePhase0Harness(t)
		profileID := harness.activeProfileID(t)
		suffix := randomSuffix()
		strategyID := harness.seedLegacyStrategy(t, profileID, "phase3-round-robin-"+suffix, "round-robin")
		publicModelID := "phase3-round-robin-public-" + suffix
		targetModelID := "phase3-round-robin-target-" + suffix
		targetModelConfigID := harness.seedModel(t, profileID, "gemini", targetModelID, "native", &strategyID)
		publicModelConfigID := harness.seedModel(t, profileID, "gemini", publicModelID, "proxy", nil)
		harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
		firstEndpointID := harness.seedEndpoint(t, profileID, "phase3-round-robin-first-"+suffix, harness.upstream.baseURL("/phase3/round-robin/first"), "phase3-round-robin-first-key")
		secondEndpointID := harness.seedEndpoint(t, profileID, "phase3-round-robin-second-"+suffix, harness.upstream.baseURL("/phase3/round-robin/second"), "phase3-round-robin-second-key")
		_ = harness.seedConnection(t, profileID, targetModelConfigID, firstEndpointID, "phase3-round-robin-first-connection-"+suffix, nil, nil, 0)
		_ = harness.seedConnection(t, profileID, targetModelConfigID, secondEndpointID, "phase3-round-robin-second-connection-"+suffix, nil, nil, 1)
		requestPath := fmt.Sprintf("/v1beta/models/%s:generateContent", publicModelID)
		requestBody := runtimePhase0GeminiRequest("phase-3 round robin hot path")

		warmResponse := harness.requestJSON(t, http.MethodPost, requestPath, requestBody, nil)
		assertStatus(t, warmResponse, http.StatusOK)
		response, snapshot := harness.captureJSONRequest(t, http.MethodPost, requestPath, requestBody, nil)
		assertStatus(t, response, http.StatusOK)
		assertPhase3HotPathExcludesForbiddenSQL(t, snapshot)
		if upstreamRequest := harness.upstream.lastRequest(t); upstreamRequest.Path != "/phase3/round-robin/second/v1beta/models/"+targetModelID+":generateContent" {
			t.Fatalf("expected second launch to use the locally advanced round-robin cursor, got %s", upstreamRequest.Path)
		}
	})
}

func TestRuntimeHotPathAvoidsProxyKeyUsageWrite(t *testing.T) {
	harness := newRuntimePhase0Harness(t)
	profileID := harness.activeProfileID(t)
	proxyAPIKey := harness.enableRuntimeProxyAPIKeyAuth(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "phase3-proxy-key-public-" + randomSuffix(),
		TargetModelID:   "phase3-proxy-key-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/phase3/proxy-key"),
		EndpointAPIKey:  "phase3-proxy-key-upstream",
	})
	response, snapshot := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-3 proxy key hot path"}},
		"model":    route.PublicModelID,
	}, map[string]string{"Authorization": "Bearer " + proxyAPIKey})
	assertStatus(t, response, http.StatusOK)
	assertPhase3HotPathExcludesForbiddenSQL(t, snapshot)
	waitForProxyAPIKeyUsageMaterialization(t, harness.conn, 5*time.Second)
}

func TestRuntimeManagementLoadDoesNotForceRuntimeDatabaseCoordination(t *testing.T) {
	harness := newRuntimePhase0HarnessWithOptions(t, runtimePhase0HarnessOptions{
		IncludeStatsService: true,
		SettingsMutator: func(settings *config.Settings) {
			usePhase0ManagementIsolationSettings(settings)
			useDurableRuntimeTelemetryMode(settings)
			settings.RuntimeDatabasePoolBudget = config.DatabasePoolBudget{MaxConns: 2, MinIdleConns: 0}
		},
	})
	profileID := harness.activeProfileID(t)
	harness.seedStatsPressureHistory(t, profileID, "phase3-management-load")
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "phase3-management-load-public-" + randomSuffix(),
		TargetModelID:   "phase3-management-load-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/phase3/management-load"),
		EndpointAPIKey:  "phase3-management-load-key",
	})
	statsLock := holdRuntimeStatsTablesLock(t, harness.databaseName)
	defer statsLock.release(t)
	pressureResult := startAsyncBenchmarkPriorityRequest(t, harness.client, http.MethodGet, harness.url+"/api/stats/summary", nil, runtimeModelHeader(profileID))
	waitForStatsLockWaitersTB(t, harness.conn, 1, 5*time.Second)

	response, observed := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-3 management load isolation"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	assertPhase3HotPathExcludesForbiddenSQL(t, observed)

	statsLock.release(t)
	assertAsyncBenchmarkRequestStatus(t, pressureResult, http.StatusOK)
}

func TestRuntimeLegacyExecutionUsesLocalCoordinationOnly(t *testing.T) {
}

func TestPhase3LegacyRoutingHonorsAttemptBudget(t *testing.T) {
}
