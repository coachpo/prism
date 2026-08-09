package integrationtest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/openaimodecheck"
)

// TestOpenAIModePreflightAndStartupFailFast verifies the read-only openai-mode
// preflight contract (deterministic report, zero writes) and the startup
// fail-fast gate against persisted mode-equality violations, including
// disabled relations.
func TestOpenAIModePreflightAndStartupFailFast(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	databaseName := "openai_mode_preflight"
	conn := harness.openEmptyDatabase(t, testContext, databaseName)
	defer func() { _ = conn.Close(testContext) }()

	service := newStartupService(t, harness.connectionString(databaseName), nil)
	if _, err := service.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup sequence: %v", err)
	}
	profileID := integrationLoadDefaultProfileID(t, testContext, conn)

	strategyID := preflightInsertStrategy(t, testContext, conn, profileID)
	modelAID := preflightInsertModel(t, testContext, conn, profileID, "preflight-source", "dual_native", strategyID)
	modelBID := preflightInsertModel(t, testContext, conn, profileID, "preflight-target", "dual_native", strategyID)
	preflightInsertModelTarget(t, testContext, conn, profileID, modelAID, modelBID, 0, true)
	endpointID := preflightInsertEndpoint(t, testContext, conn, profileID, "preflight-endpoint")
	connectionID := preflightInsertConnection(t, testContext, conn, profileID, modelAID, endpointID, "dual_native", "preflight-endpoint")
	preflightInsertConnectionTarget(t, testContext, conn, profileID, modelAID, connectionID, 1, true)

	// Compliant state: deterministic report with zero violations.
	before := preflightCaptureCounts(t, testContext, conn, profileID)
	report, err := openaimodecheck.Check(testContext, conn, profileID)
	if err != nil {
		t.Fatalf("run openai mode preflight check on compliant state: %v", err)
	}
	preflightAssertCountsUnchanged(t, testContext, conn, profileID, before)
	if len(report.Violations) != 0 {
		t.Fatalf("expected compliant preflight report, got %+v", report)
	}
	if !strings.Contains(report.String(), "openai_mode_preflight profile="+fmt.Sprint(profileID)+" violations=0") {
		t.Fatalf("unexpected compliant report text: %q", report.String())
	}

	// Startup fail-fast passes on a compliant state.
	if _, err := service.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("expected compliant startup to pass fail-fast, got %v", err)
	}

	// Introduce cross-mode relations directly (including a disabled relation).
	modelCID := preflightInsertModel(t, testContext, conn, profileID, "preflight-chat-target", "chat_completions_only", strategyID)
	modelDID := preflightInsertModel(t, testContext, conn, profileID, "preflight-chat-target-disabled", "chat_completions_only", strategyID)
	preflightInsertModelTarget(t, testContext, conn, profileID, modelAID, modelCID, 2, true)
	preflightInsertModelTarget(t, testContext, conn, profileID, modelAID, modelDID, 3, false)
	mismatchedConnectionID := preflightInsertConnection(t, testContext, conn, profileID, modelAID, endpointID, "responses_only", "preflight-endpoint")
	preflightInsertConnectionTarget(t, testContext, conn, profileID, modelAID, mismatchedConnectionID, 4, true)

	before = preflightCaptureCounts(t, testContext, conn, profileID)
	report, err = openaimodecheck.Check(testContext, conn, profileID)
	if err != nil {
		t.Fatalf("run openai mode preflight check on violating state: %v", err)
	}
	preflightAssertCountsUnchanged(t, testContext, conn, profileID, before)
	if len(report.Violations) != 3 {
		t.Fatalf("expected 3 violations (model x2 including disabled, connection x1), got %+v", report)
	}
	text := report.String()
	for _, expected := range []string{
		"openai_mode_preflight profile=" + fmt.Sprint(profileID) + " violations=3",
		"model_target source=preflight-source target=preflight-chat-target source_mode=dual_native target_mode=chat_completions_only",
		"model_target source=preflight-source target=preflight-chat-target-disabled source_mode=dual_native target_mode=chat_completions_only",
		"connection_target source=preflight-source target=preflight-endpoint source_mode=dual_native target_mode=responses_only",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected preflight report to contain %q, got:\n%s", expected, text)
		}
	}

	// Startup fail-fast blocks the violating state with a stable error.
	_, err = service.RunWithConn(testContext, conn)
	if err == nil {
		t.Fatal("expected startup fail-fast on openai mode violations, got nil error")
	}
	if !strings.Contains(err.Error(), "openai text mode equality check failed") {
		t.Fatalf("expected startup fail-fast error mentioning the check, got %v", err)
	}
}

func integrationLoadDefaultProfileID(t *testing.T, ctx context.Context, conn *pgx.Conn) int {
	t.Helper()
	var profileID int
	if err := conn.QueryRow(ctx, `SELECT id FROM profiles WHERE name = 'Default' ORDER BY id ASC LIMIT 1`).Scan(&profileID); err != nil {
		t.Fatalf("load default profile id: %v", err)
	}
	return profileID
}

func preflightInsertStrategy(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int) int {
	t.Helper()
	now := time.Now().UTC()
	var strategyID int
	if err := conn.QueryRow(ctx, `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, $2, 'single', ARRAY[429,500,502,503,504], 'off', 60000, 2.0, 0.2, 900000, 2, 0, 0, $3, $3) RETURNING id`, profileID, "preflight-strategy", now).Scan(&strategyID); err != nil {
		t.Fatalf("insert preflight strategy: %v", err)
	}
	return strategyID
}

func preflightInsertModel(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, modelID string, mode string, strategyID int) int {
	t.Helper()
	now := time.Now().UTC()
	var modelConfigID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', $2, $2, $3, $4, TRUE, $5, $5) RETURNING id`, profileID, modelID, strategyID, mode, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert preflight model %q: %v", modelID, err)
	}
	return modelConfigID
}

func preflightInsertModelTarget(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, sourceModelConfigID int, targetModelConfigID int, position int, isEnabled bool) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := conn.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, $4, $5, $6, $6)`, profileID, sourceModelConfigID, targetModelConfigID, position, isEnabled, now); err != nil {
		t.Fatalf("insert preflight model target: %v", err)
	}
}

func preflightInsertEndpoint(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var endpointID int
	if err := conn.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, config_revision, created_at, updated_at) VALUES ($1, $2, $3, 'key', 1, $4, $4) RETURNING id`, profileID, name, "https://"+strings.ToLower(name)+".invalid", now).Scan(&endpointID); err != nil {
		t.Fatalf("insert preflight endpoint: %v", err)
	}
	return endpointID
}

func preflightInsertConnection(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, ownerModelConfigID int, endpointID int, capability string, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var connectionID int
	if err := conn.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, openai_text_capability, is_active, priority, name, health_status, created_at, updated_at) VALUES ($1, 'openai', $2, $3, TRUE, 0, $4, 'healthy', $5, $5) RETURNING id`, profileID, endpointID, capability, name, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert preflight connection: %v", err)
	}
	return connectionID
}

func preflightInsertConnectionTarget(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, sourceModelConfigID int, connectionID int, position int, isEnabled bool) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := conn.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, $5, $6, $6)`, profileID, sourceModelConfigID, connectionID, position, isEnabled, now); err != nil {
		t.Fatalf("insert preflight connection target: %v", err)
	}
}

type preflightCounts struct {
	models      int
	targets     int
	connections int
	requestLogs int
}

func preflightCaptureCounts(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int) preflightCounts {
	t.Helper()
	counts := preflightCounts{}
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM model_configs WHERE profile_id = $1`, profileID).Scan(&counts.models); err != nil {
		t.Fatalf("count preflight models: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM model_access_targets WHERE profile_id = $1`, profileID).Scan(&counts.targets); err != nil {
		t.Fatalf("count preflight targets: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM connections WHERE profile_id = $1`, profileID).Scan(&counts.connections); err != nil {
		t.Fatalf("count preflight connections: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM request_logs WHERE profile_id = $1`, profileID).Scan(&counts.requestLogs); err != nil {
		t.Fatalf("count preflight request logs: %v", err)
	}
	return counts
}

func preflightAssertCountsUnchanged(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, before preflightCounts) {
	t.Helper()
	after := preflightCaptureCounts(t, ctx, conn, profileID)
	if after != before {
		t.Fatalf("preflight must be read-only: counts changed from %+v to %+v", before, after)
	}
}
