package runtimetest

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func TestRuntimePartitionedRequestAuditUsageMaterialization(t *testing.T) {
	fixedNow := time.Date(2026, 6, 30, 23, 30, 0, 0, time.FixedZone("late-west", -7*60*60))
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{Now: func() time.Time { return fixedNow }},
	})
	profileID := harness.activeProfileID(t)
	route := seedPartitionedLogFailoverRoute(t, harness, profileID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "partitioned runtime telemetry"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	events := loadLoadbalanceEvents(t, harness.conn, profileID, route.PrimaryConnectionID)
	if len(events) != 1 {
		t.Fatalf("expected one loadbalance feedback event, got %+v", events)
	}

	utcDay := fixedNow.UTC()
	assertPartitionRowCount(t, harness.conn, "request_logs", utcDay, profileID, 2)
	assertPartitionRowCount(t, harness.conn, "usage_request_events", utcDay, profileID, 1)
	assertPartitionRowCount(t, harness.conn, "loadbalance_events", utcDay, profileID, 1)
}

func TestRuntimeTelemetryWritesPartitionedLogs(t *testing.T) {
	TestRuntimePartitionedRequestAuditUsageMaterialization(t)
}

type partitionedLogRoute struct {
	PublicModelID         string
	TargetModelID         string
	PrimaryConnectionID   int
	PrimaryEndpointID     int
	SecondaryConnectionID int
}

func seedPartitionedLogFailoverRoute(t *testing.T, harness *runtimeHarness, profileID int) partitionedLogRoute {
	t.Helper()
	suffix := randomSuffix()
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "partitioned primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-partitioned-secondary"})
	autoRecovery := `{"mode":"enabled","status_codes":[503],"cooldown":{"base_seconds":60,"failure_threshold":1,"backoff_multiplier":2.0,"max_cooldown_seconds":900},"ban":{"mode":"off","max_cooldown_strikes_before_ban":0,"ban_duration_seconds":0}}`
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategyWithAutoRecovery(t, profileID, "partitioned-runtime-"+suffix, "fill-first", autoRecovery)
	publicModelID := "partitioned-public-" + suffix
	targetModelID := "partitioned-target-" + suffix
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "partitioned-primary-"+suffix, primaryUpstream.baseURL("/partitioned/primary"), "partitioned-primary-key")
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "partitioned-secondary-"+suffix, secondaryUpstream.baseURL("/partitioned/secondary"), "partitioned-secondary-key")
	primaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, primaryEndpointID, "partitioned-primary-connection-"+suffix, nil, nil, 0)
	secondaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, secondaryEndpointID, "partitioned-secondary-connection-"+suffix, nil, nil, 1)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return partitionedLogRoute{
		PublicModelID:         publicModelID,
		TargetModelID:         targetModelID,
		PrimaryConnectionID:   primaryConnectionID,
		PrimaryEndpointID:     primaryEndpointID,
		SecondaryConnectionID: secondaryConnectionID,
	}
}

func assertPartitionRowCount(t *testing.T, conn *pgx.Conn, tableName string, timestamp time.Time, profileID int, want int) {
	t.Helper()
	partitionName := logPartitionName(tableName, timestamp)
	var exists bool
	if err := conn.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, "public."+partitionName).Scan(&exists); err != nil {
		t.Fatalf("check partition %s: %v", partitionName, err)
	}
	if !exists {
		t.Fatalf("expected partition %s to exist", partitionName)
	}
	query := fmt.Sprintf(`SELECT COUNT(*) FROM public.%s WHERE profile_id = $1`, quoteIdentifier(partitionName))
	var count int
	if err := conn.QueryRow(context.Background(), query, profileID).Scan(&count); err != nil {
		t.Fatalf("count rows in partition %s: %v", partitionName, err)
	}
	if count != want {
		t.Fatalf("expected %d %s rows in %s, got %d", want, tableName, partitionName, count)
	}
}

func logPartitionName(tableName string, timestamp time.Time) string {
	return tableName + "_p" + timestamp.UTC().Format("20060102")
}
