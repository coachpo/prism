package runtime_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
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
	assertPartitionRowCount(t, harness.conn, "audit_logs", utcDay, profileID, 2)
	assertPartitionRowCount(t, harness.conn, "usage_request_events", utcDay, profileID, 1)
	assertPartitionRowCount(t, harness.conn, "loadbalance_events", utcDay, profileID, 1)
}

func TestRuntimeTelemetryWritesPartitionedLogs(t *testing.T) {
	TestRuntimePartitionedRequestAuditUsageMaterialization(t)
}

func TestAuditWeakReferenceSurvivesRequestLogExpiry(t *testing.T) {
	fixedNow := time.Date(2026, 7, 2, 10, 15, 0, 0, time.UTC)
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{Now: func() time.Time { return fixedNow }},
	})
	profileID := harness.activeProfileID(t)
	vendorID := loadVendorIDByKey(t, harness.conn, "openai")
	if _, err := harness.conn.Exec(context.Background(), `UPDATE vendors SET audit_enabled = TRUE, audit_capture_bodies = FALSE WHERE id = $1`, vendorID); err != nil {
		t.Fatalf("enable audit for weak reference test: %v", err)
	}
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "audit-weak-public-" + randomSuffix(),
		TargetModelID:   "audit-weak-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/audit/weak-reference"),
		EndpointAPIKey:  "audit-weak-key",
	})
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET vendor_id = $1 WHERE profile_id = $2 AND model_id = ANY($3::text[])`, vendorID, profileID, []string{route.PublicModelID, route.TargetModelID}); err != nil {
		t.Fatalf("attach weak-reference models to audit vendor: %v", err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "audit weak reference"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	link := loadLatestAuditWeakLink(t, harness.conn, profileID)
	requestCreatedAt, requestIngressID := loadRequestWeakLinkSource(t, harness.conn, int(link.RequestLogID.Int64))
	if !link.RequestLogCreatedAt.Valid || !link.RequestLogCreatedAt.Time.UTC().Equal(requestCreatedAt.UTC()) {
		t.Fatalf("expected audit request_log_created_at %s, got %+v", requestCreatedAt, link.RequestLogCreatedAt)
	}
	if !link.IngressRequestID.Valid || link.IngressRequestID.String != requestIngressID {
		t.Fatalf("expected audit ingress_request_id %q, got %+v", requestIngressID, link.IngressRequestID)
	}
	assertAuditRequestLogForeignKeyAbsent(t, harness.conn)

	dropLogPartition(t, harness.conn, "request_logs", requestCreatedAt)
	if count := countParentRowsByID(t, harness.conn, "request_logs", int(link.RequestLogID.Int64)); count != 0 {
		t.Fatalf("expected dropped request partition to remove request log %d, got %d rows", link.RequestLogID.Int64, count)
	}

	detail, err := auditdomain.GetLog(context.Background(), harness.conn, profileID, link.AuditID)
	if err != nil {
		t.Fatalf("load audit detail after request partition drop: %v", err)
	}
	if detail == nil || detail.RequestLogID == nil || *detail.RequestLogID != int(link.RequestLogID.Int64) {
		t.Fatalf("expected audit detail to keep weak request_log_id %d after request expiry, got %+v", link.RequestLogID.Int64, detail)
	}
	persisted := loadAuditWeakLinkByID(t, harness.conn, link.AuditID)
	if !persisted.RequestLogCreatedAt.Valid || !persisted.IngressRequestID.Valid {
		t.Fatalf("expected weak audit request fields to survive request partition drop, got %+v", persisted)
	}
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
	vendorID := loadVendorIDByKey(t, harness.conn, "openai")
	if _, err := harness.conn.Exec(context.Background(), `UPDATE vendors SET audit_enabled = TRUE, audit_capture_bodies = FALSE WHERE id = $1`, vendorID); err != nil {
		t.Fatalf("enable audit for partitioned telemetry test: %v", err)
	}
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
	primaryEndpointID := harness.seedEndpoint(t, profileID, "partitioned-primary-"+suffix, primaryUpstream.baseURL("/partitioned/primary"), "partitioned-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "partitioned-secondary-"+suffix, secondaryUpstream.baseURL("/partitioned/secondary"), "partitioned-secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, primaryEndpointID, "partitioned-primary-connection-"+suffix, nil, nil, 0)
	secondaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, secondaryEndpointID, "partitioned-secondary-connection-"+suffix, nil, nil, 1)
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET vendor_id = $1 WHERE profile_id = $2 AND model_id = ANY($3::text[])`, vendorID, profileID, []string{publicModelID, targetModelID}); err != nil {
		t.Fatalf("attach partitioned models to audit vendor: %v", err)
	}
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

type auditWeakLink struct {
	AuditID             int
	RequestLogID        sql.NullInt64
	RequestLogCreatedAt sql.NullTime
	IngressRequestID    sql.NullString
	CreatedAt           time.Time
}

func loadLatestAuditWeakLink(t *testing.T, conn *pgx.Conn, profileID int) auditWeakLink {
	t.Helper()
	var link auditWeakLink
	if err := conn.QueryRow(context.Background(), `SELECT id, request_log_id, request_log_created_at, ingress_request_id, created_at FROM audit_logs WHERE profile_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, profileID).Scan(&link.AuditID, &link.RequestLogID, &link.RequestLogCreatedAt, &link.IngressRequestID, &link.CreatedAt); err != nil {
		t.Fatalf("load latest audit weak link: %v", err)
	}
	if !link.RequestLogID.Valid {
		t.Fatalf("expected audit row to keep request_log_id, got %+v", link)
	}
	return link
}

func loadAuditWeakLinkByID(t *testing.T, conn *pgx.Conn, auditID int) auditWeakLink {
	t.Helper()
	var link auditWeakLink
	if err := conn.QueryRow(context.Background(), `SELECT id, request_log_id, request_log_created_at, ingress_request_id, created_at FROM audit_logs WHERE id = $1 LIMIT 1`, auditID).Scan(&link.AuditID, &link.RequestLogID, &link.RequestLogCreatedAt, &link.IngressRequestID, &link.CreatedAt); err != nil {
		t.Fatalf("load audit weak link %d: %v", auditID, err)
	}
	return link
}

func loadRequestWeakLinkSource(t *testing.T, conn *pgx.Conn, requestLogID int) (time.Time, string) {
	t.Helper()
	var createdAt time.Time
	var ingressRequestID sql.NullString
	if err := conn.QueryRow(context.Background(), `SELECT created_at, ingress_request_id FROM request_logs WHERE id = $1 LIMIT 1`, requestLogID).Scan(&createdAt, &ingressRequestID); err != nil {
		t.Fatalf("load request weak link source %d: %v", requestLogID, err)
	}
	if !ingressRequestID.Valid || ingressRequestID.String == "" {
		t.Fatalf("expected request log %d to have ingress_request_id, got %+v", requestLogID, ingressRequestID)
	}
	return createdAt.UTC(), ingressRequestID.String
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

func dropLogPartition(t *testing.T, conn *pgx.Conn, tableName string, timestamp time.Time) {
	t.Helper()
	partitionName := logPartitionName(tableName, timestamp)
	if _, err := conn.Exec(context.Background(), `DROP TABLE public.`+quoteIdentifier(partitionName)); err != nil {
		t.Fatalf("drop partition %s: %v", partitionName, err)
	}
}

func countParentRowsByID(t *testing.T, conn *pgx.Conn, tableName string, id int) int {
	t.Helper()
	query := fmt.Sprintf(`SELECT COUNT(*) FROM public.%s WHERE id = $1`, quoteIdentifier(tableName))
	var count int
	if err := conn.QueryRow(context.Background(), query, id).Scan(&count); err != nil {
		t.Fatalf("count %s rows for id %d: %v", tableName, id, err)
	}
	return count
}

func assertAuditRequestLogForeignKeyAbsent(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	var exists bool
	if err := conn.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conrelid = 'public.audit_logs'::regclass
			  AND conname = 'audit_logs_request_log_id_fkey'
		)`).Scan(&exists); err != nil {
		t.Fatalf("check audit request log foreign key: %v", err)
	}
	if exists {
		t.Fatal("expected audit_logs_request_log_id_fkey to be absent")
	}
}

func logPartitionName(tableName string, timestamp time.Time) string {
	return tableName + "_p" + timestamp.UTC().Format("20060102")
}
