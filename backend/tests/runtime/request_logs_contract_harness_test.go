package runtimetest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

type requestLogContractHarness struct {
	databaseName string
	client       *http.Client
	conn         *pgx.Conn
	server       *httptest.Server
	url          string
}

func newRequestLogContractHarness(t *testing.T) *requestLogContractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	databaseName := "s15_runtime_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	startupService, err := startup.New(startup.Options{DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "s15-runtime-secret"})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup service: %v", err)
	}
	settings := config.Settings{Host: "127.0.0.1", Port: 8000, AppEnv: config.EnvironmentProduction, DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "s15-runtime-secret", CORSAllowedOrigins: "http://localhost:5173,http://127.0.0.1:5173"}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	statsService, err := managementstats.NewService(settings, managementstats.Options{Pool: pool, Now: func() time.Time { return time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatalf("build stats service: %v", err)
	}
	t.Cleanup(statsService.Close)
	auditService, err := managementaudit.NewService(settings, managementaudit.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build audit service: %v", err)
	}
	t.Cleanup(auditService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "s15-runtime-test", AuditService: auditService, StatsService: statsService})
	if err != nil {
		t.Fatalf("build runtime request-log handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar
	return &requestLogContractHarness{databaseName: databaseName, client: client, conn: conn, server: server, url: server.URL}
}

func (h *requestLogContractHarness) requestJSON(t *testing.T, method string, path string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		requestBody = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, h.url+path, requestBody)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := h.client.Do(request)
	if err != nil {
		t.Fatalf("perform request %s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func loadRuntimeDefaultProfileID(t *testing.T, harness *requestLogContractHarness) int {
	t.Helper()
	var profileID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM profiles WHERE is_default = TRUE ORDER BY id ASC LIMIT 1`).Scan(&profileID); err != nil {
		t.Fatalf("load default profile id: %v", err)
	}
	return profileID
}

func runtimeModelHeader(profileID int) map[string]string {
	return map[string]string{"X-Profile-Id": fmt.Sprintf("%d", profileID)}
}

func seedRuntimeAuditFamilySetting(t *testing.T, harness *runtimeHarness, profileID int, apiFamily string, auditEnabled bool, auditCaptureBodies bool) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(
		context.Background(),
		`INSERT INTO profile_api_family_audit_settings (profile_id, api_family, audit_enabled, audit_capture_bodies, migration_provenance, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'explicit', $5, $5)
		 ON CONFLICT (profile_id, api_family) DO UPDATE SET audit_enabled = EXCLUDED.audit_enabled, audit_capture_bodies = EXCLUDED.audit_capture_bodies, migration_provenance = 'explicit', updated_at = EXCLUDED.updated_at`,
		profileID,
		apiFamily,
		auditEnabled,
		auditCaptureBodies,
		now,
	); err != nil {
		t.Fatalf("seed runtime audit setting %s: %v", apiFamily, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
}

func assertRuntimeRequestLogAuditSnapshot(t *testing.T, harness *runtimeHarness, profileID int, modelID string, wantAuditEnabled bool, wantAuditCaptureBodies bool) {
	t.Helper()
	var auditEnabledAtRequest bool
	var auditCaptureBodiesAtRequest bool
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT audit_enabled_at_request, audit_capture_bodies_at_request FROM request_logs WHERE profile_id = $1 AND model_id = $2 ORDER BY id DESC LIMIT 1`,
		profileID,
		modelID,
	).Scan(&auditEnabledAtRequest, &auditCaptureBodiesAtRequest); err != nil {
		t.Fatalf("load request-log audit snapshot for %s: %v", modelID, err)
	}
	if auditEnabledAtRequest != wantAuditEnabled || auditCaptureBodiesAtRequest != wantAuditCaptureBodies {
		t.Fatalf("expected request-log audit snapshot for %s to be enabled=%v capture=%v, got enabled=%v capture=%v", modelID, wantAuditEnabled, wantAuditCaptureBodies, auditEnabledAtRequest, auditCaptureBodiesAtRequest)
	}
}

func assertRuntimeAuditLogSnapshot(t *testing.T, harness *runtimeHarness, profileID int, modelID string, wantAuditEnabled bool, wantAuditCaptureBodies bool) {
	t.Helper()
	var auditEnabledAtRequest bool
	var auditCaptureBodiesAtRequest bool
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT audit_enabled_at_request, audit_capture_bodies_at_request FROM audit_logs WHERE profile_id = $1 AND model_id = $2 ORDER BY id DESC LIMIT 1`,
		profileID,
		modelID,
	).Scan(&auditEnabledAtRequest, &auditCaptureBodiesAtRequest); err != nil {
		t.Fatalf("load audit-log audit snapshot for %s: %v", modelID, err)
	}
	if auditEnabledAtRequest != wantAuditEnabled || auditCaptureBodiesAtRequest != wantAuditCaptureBodies {
		t.Fatalf("expected audit-log audit snapshot for %s to be enabled=%v capture=%v, got enabled=%v capture=%v", modelID, wantAuditEnabled, wantAuditCaptureBodies, auditEnabledAtRequest, auditCaptureBodiesAtRequest)
	}
}

func seedRequestLogEndpoints(t *testing.T, harness *requestLogContractHarness, profileID int) {
	t.Helper()
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO endpoints (id, profile_id, name, base_url, api_key, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6), ($7, $2, $8, $9, $10, $6, $6)`, 12, profileID, "Primary OpenAI", "https://api.openai.com", "fixture-key", now, 13, "Primary Anthropic", "https://api.anthropic.com", "fixture-key"); err != nil {
		t.Fatalf("seed request-log endpoints: %v", err)
	}
}

func seedRequestLogUserAgentRules(t *testing.T, harness *requestLogContractHarness, profileID int) {
	t.Helper()
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO user_agent_client_rules (profile_id, name, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, $2, $3, TRUE, FALSE, $4, $4), ($1, $5, $6, TRUE, FALSE, $4, $4)`, profileID, "Codex", "codex", now, "OpenAI SDK", "openai/python"); err != nil {
		t.Fatalf("seed request-log user-agent rules: %v", err)
	}
}

func insertRequestLogUserAgentRule(t *testing.T, harness *requestLogContractHarness, profileID *int, name string, pattern string, enabled bool, isSystem bool) int {
	t.Helper()
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	var profileValue any
	if profileID != nil {
		profileValue = *profileID
	}
	var ruleID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO user_agent_client_rules (profile_id, name, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6) RETURNING id`, profileValue, name, pattern, enabled, isSystem, now).Scan(&ruleID); err != nil {
		t.Fatalf("insert request-log user-agent rule %q: %v", name, err)
	}
	return ruleID
}

func insertRequestLogProfile(t *testing.T, harness *requestLogContractHarness) int {
	t.Helper()
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	var profileID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COALESCE(MAX(id), 0) + 1 FROM profiles`).Scan(&profileID); err != nil {
		t.Fatalf("choose request-log profile id: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO profiles (id, name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, $2, NULL, FALSE, FALSE, TRUE, 1, $3, $3)`, profileID, "request-log-other-profile-"+randomSuffix(), now); err != nil {
		t.Fatalf("insert request-log profile: %v", err)
	}
	return profileID
}

func updateRequestLogUserAgents(t *testing.T, harness *requestLogContractHarness, profileID int, requestLogID int, callerUserAgent string, upstreamUserAgent string) {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET caller_user_agent = $1, upstream_user_agent = $2 WHERE profile_id = $3 AND id = $4`, callerUserAgent, upstreamUserAgent, profileID, requestLogID); err != nil {
		t.Fatalf("update request-log user agents for %d: %v", requestLogID, err)
	}
}

func updateRequestLogModels(t *testing.T, harness *requestLogContractHarness, profileID int, requestLogID int, modelID string, resolvedTargetModelID string) {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET model_id = $1, resolved_target_model_id = $2 WHERE profile_id = $3 AND id = $4`, modelID, resolvedTargetModelID, profileID, requestLogID); err != nil {
		t.Fatalf("update request-log models for %d: %v", requestLogID, err)
	}
}

func seedFixtureRequestLog(t *testing.T, harness *requestLogContractHarness, profileID int) {
	t.Helper()
	createdAt := time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC)
	ensureRuntimeTestLogPartitions(t, harness.databaseName, runtimeTestLogPartitionFor("request_logs", createdAt))
	if _, err := harness.conn.Exec(context.Background(), `
		INSERT INTO request_logs (
			id, profile_id, model_id, api_family, resolved_target_model_id, upstream_model_id, endpoint_id, connection_id,
			proxy_api_key_id_snapshot, proxy_api_key_name_snapshot, ingress_request_id, attempt_number,
			provider_correlation_id, endpoint_base_url, row_kind, url_scrub_provenance, upstream_status_code,
			attempt_duration_ms, is_stream, input_tokens, output_tokens, total_tokens, success_flag,
			pricing_status, pricing_evidence_trust, unpriced_reason, reasoning_tokens, input_cost_micros,
			output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros,
			reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros,
			currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source,
			pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_reasoning,
			cache_read_input_tokens, cache_creation_input_tokens, pricing_snapshot_cache_read_input,
			pricing_snapshot_cache_creation_input, pricing_config_version_used, request_path, error_detail,
			endpoint_description, created_at, caller_user_agent, upstream_user_agent, completion_duration_ms,
			ttft_ms, audit_enabled_at_request, audit_capture_bodies_at_request
		) VALUES (
			$1, $2, 'gpt-4o', 'openai', 'gpt-4o-native', 'vendor/gpt-4o-native', 12, 34, NULL, NULL, 'ingress_req_42', 2,
			'req_upstream_abc123', 'https://api.openai.com', 'upstream', 'runtime_scrubbed', 200,
			1234, FALSE, 15, 42, 57, TRUE, 'priced', 'trusted', NULL, 3, 20, 30, 0, 0, 0, 1250, 1250,
			'USD', 'USD', '$', '1', 'DEFAULT_1_TO_1', '1M tokens', '2.500000', '10.000000', '0.000000',
			0, 0, '1.250000', '0.000000', 1, '/v1/chat/completions', 'Primary production key', NULL,
			$3, 'codex/1.0', 'OpenAI/Python 1.0', 914, 320, FALSE, FALSE
		)`, 101, profileID, createdAt); err != nil {
		t.Fatalf("seed fixture request log: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET operation_name = 'openai.chat_completions', upstream_operation_name = 'openai.responses', operation_translation_mode = 'openai_chat_completions_to_responses', upstream_request_path = '/v1/responses', request_generation_params = $1::jsonb, request_generation_params_status = 'complete', selected_terminal_target_id = 34 WHERE profile_id = $2 AND id = 101`, `{"provider":"openai","temperature":0.7,"top_p":0.9,"max_output_tokens":1024,"max_output_tokens_source":"max_completion_tokens","reasoning":{"effort":"low","source_field":"reasoning_effort"}}`, profileID); err != nil {
		t.Fatalf("seed fixture request generation params: %v", err)
	}
}

func attachRequestLogCurrentPricingTemplate(t *testing.T, harness *requestLogContractHarness, profileID int, requestLogID int, templateID int, effectiveAt time.Time) {
	t.Helper()
	tx, err := harness.conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin attach pricing template: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var oldRevisionID, revisionID int64
	if err := tx.QueryRow(context.Background(), `SELECT current_revision_id FROM pricing_templates WHERE profile_id = $1 AND id = $2 FOR UPDATE`, profileID, templateID).Scan(&oldRevisionID); err != nil {
		t.Fatalf("load pricing revision: %v", err)
	}
	if err := tx.QueryRow(context.Background(), `INSERT INTO pricing_template_revisions (template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, template_kind, effective_at, created_at, created_by_kind, created_by_operation_id) SELECT template_id, version + 1, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, 'standard', $2, $2, 'legacy_backfill', NULL FROM pricing_template_revisions WHERE id = $1 RETURNING id`, oldRevisionID, effectiveAt).Scan(&revisionID); err != nil {
		t.Fatalf("insert pricing revision: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO pricing_template_cards (revision_id, template_kind, card_role, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price) SELECT $1, 'standard', 'standard', input_price, output_price, cached_input_price, cache_creation_price, reasoning_price FROM pricing_template_cards WHERE revision_id = $2`, revisionID, oldRevisionID); err != nil {
		t.Fatalf("copy pricing card: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `UPDATE pricing_templates SET current_revision_id = $1, updated_at = $2 WHERE id = $3`, revisionID, effectiveAt, templateID); err != nil {
		t.Fatalf("update pricing pointer: %v", err)
	}
	result, err := tx.Exec(context.Background(), `UPDATE request_logs SET pricing_template_id_used = $1, pricing_template_revision_id_used = $2 WHERE profile_id = $3 AND id = $4`, templateID, revisionID, profileID, requestLogID)
	if err != nil {
		t.Fatalf("attach current pricing template to request log %d: %v", requestLogID, err)
	}
	if result.RowsAffected() != 1 {
		t.Fatalf("expected to attach one current pricing template to request log %d, got %d", requestLogID, result.RowsAffected())
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit attached pricing template: %v", err)
	}
}

func seedSimpleRequestLog(t *testing.T, harness *requestLogContractHarness, profileID int, id int, endpointID int, endpointBaseURL *string, createdAt time.Time, auditEnabledAtRequest bool) {
	t.Helper()
	ensureRuntimeTestLogPartitions(t, harness.databaseName, runtimeTestLogPartitionFor("request_logs", createdAt))
	var historicalBaseURL any
	if endpointBaseURL != nil {
		historicalBaseURL = *endpointBaseURL
	}
	requestID := fmt.Sprintf("req-%d", id)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, ingress_request_id, attempt_number, endpoint_base_url, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, unpriced_reason, pricing_resolution_kind, request_path, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, 'gpt-4o', 'gpt-4o', 'openai', $3, NULL, $4, 1, $5, 'upstream', 'runtime_scrubbed', 200, 120, FALSE, TRUE, 'unpriced', 'trusted', 'MISSING_PRICE_DATA', 'unsupported_unit', '/v1/chat/completions', $6, $7, FALSE)`, id, profileID, endpointID, requestID, historicalBaseURL, createdAt, auditEnabledAtRequest); err != nil {
		t.Fatalf("seed simple request log %d: %v", id, err)
	}
}

func seedFilteredRequestLog(t *testing.T, harness *requestLogContractHarness, profileID int, id int, statusCode int, errorDetail *string, createdAt time.Time) {
	t.Helper()
	ensureRuntimeTestLogPartitions(t, harness.databaseName, runtimeTestLogPartitionFor("request_logs", createdAt))
	success := statusCode >= 200 && statusCode < 300
	requestID := fmt.Sprintf("filtered-req-%d", id)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, ingress_request_id, attempt_number, endpoint_base_url, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, request_path, error_detail, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, 'gpt-4o', 'gpt-4o', 'openai', 12, NULL, $3, 1, 'https://api.openai.com', 'upstream', 'runtime_scrubbed', $4, 120, FALSE, $5, 'unknown', 'legacy_untrusted', '/v1/chat/completions', $6, $7, FALSE, FALSE)`, id, profileID, requestID, statusCode, success, nullableTestString(errorDetail), createdAt); err != nil {
		t.Fatalf("seed filtered request log %d: %v", id, err)
	}
}

func seedPricingFilteredRequestLog(t *testing.T, harness *requestLogContractHarness, profileID int, id int, priced bool, unpricedReason *string, _ bool, createdAt time.Time) {
	t.Helper()
	ensureRuntimeTestLogPartitions(t, harness.databaseName, runtimeTestLogPartitionFor("request_logs", createdAt))
	var componentCost, originalCost, userCost any
	pricingTrust := "trusted"
	if priced {
		componentCost = int64(0)
		originalCost = int64(1000)
		userCost = int64(1000)
	}
	requestID := fmt.Sprintf("pricing-filtered-req-%d", id)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, ingress_request_id, attempt_number, endpoint_base_url, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, unpriced_reason, pricing_resolution_kind, input_cost_micros, output_cost_micros, reasoning_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, request_path, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, 'gpt-4o', 'gpt-4o', 'openai', 12, NULL, $3, 1, 'https://api.openai.com', 'upstream', 'runtime_scrubbed', 200, 120, FALSE, TRUE, $4, $5, $6, $7, $8, $8, $8, $8, $8, $9, $10, '/v1/chat/completions', $11, FALSE, FALSE)`, id, profileID, requestID, runtimePricingStatusForSeed(priced), pricingTrust, nullableTestString(unpricedReason), runtimeResolutionKindForSeed(unpricedReason), componentCost, originalCost, userCost, createdAt); err != nil {
		t.Fatalf("seed pricing-filtered request log %d: %v", id, err)
	}
}

func seedRuntimeAuditLog(t *testing.T, harness *requestLogContractHarness, auditLogID int, profileID int, requestLogID int, createdAt time.Time) {
	t.Helper()
	ensureRuntimeTestLogPartitions(t, harness.databaseName, runtimeTestLogPartitionFor("audit_logs", createdAt))
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO audit_logs (id, profile_id, request_log_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_headers_scrub_provenance, request_headers_capture_status, request_body, request_body_capture_provenance, request_body_capture_status, upstream_status_code, response_headers, response_headers_scrub_provenance, response_headers_capture_status, response_body, response_body_capture_provenance, response_body_capture_status, is_stream, row_kind, url_scrub_provenance, attempt_duration_ms, audit_enabled_at_request, audit_capture_bodies_at_request, created_at) VALUES ($1, $2, $3, 'gpt-4o', NULL, NULL, 'https://audit.invalid', 'Audit endpoint', 'POST', 'https://audit.invalid/v1/chat/completions', '{"authorization":"Bearer [REDACTED]"}', 'runtime_scrubbed', 'captured', '{"messages":[{"role":"user","content":"hidden"}]}', 'runtime_bytes', 'captured', 200, '{"x-request-id":"req-hidden"}', 'runtime_scrubbed', 'captured', '{"ok":true}', 'runtime_bytes', 'captured', FALSE, 'upstream', 'runtime_scrubbed', 1234, FALSE, TRUE, $4)`, auditLogID, profileID, requestLogID, createdAt); err != nil {
		t.Fatalf("seed runtime audit log %d: %v", auditLogID, err)
	}
}

func runtimeResolutionKindForSeed(unpricedReason *string) any {
	if unpricedReason != nil && *unpricedReason == "MISSING_PRICE_DATA" {
		return "unsupported_unit"
	}
	return nil
}

func runtimePricingStatusForSeed(priced bool) string {
	if priced {
		return "priced"
	}
	return "unpriced"
}

func seedRequestLogModels(t *testing.T, harness *requestLogContractHarness, profileID int) {
	t.Helper()
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	var strategyID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, $2, 'round-robin', ARRAY[403,422,429,500,502,503,504,529], 'off', 60000, 2.0, 0.2, 900000, 3, 0, 0, $3, $3) RETURNING id`, profileID, "request-log-current-models", now).Scan(&strategyID); err != nil {
		t.Fatalf("insert current request-log strategy: %v", err)
	}
	var nativeModelID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', $2, $3, $4, 'dual_native', TRUE, $5, $5) RETURNING id`, profileID, "gpt-4o-native", "GPT-4o Native", strategyID, now).Scan(&nativeModelID); err != nil {
		t.Fatalf("insert current native request-log model: %v", err)
	}
	var proxyModelID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', $2, $3, $4, 'dual_native', TRUE, $5, $5) RETURNING id`, profileID, "gpt-4o", "GPT-4o Proxy", strategyID, now).Scan(&proxyModelID); err != nil {
		t.Fatalf("insert current proxy request-log model: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, 0, TRUE, $4, $4)`, profileID, proxyModelID, nativeModelID, now); err != nil {
		t.Fatalf("insert request-log access target: %v", err)
	}
}

func insertRuntimePricingTemplate(t *testing.T, conn *pgx.Conn, profileID int, name string, pricingCurrencyCode string, inputPrice string, outputPrice string, cachedInputPrice string, cacheCreationPrice string, reasoningPrice string) int {
	t.Helper()
	now := time.Now().UTC()
	/*
		// The merged pricing schema keeps prices on the revision table; the
		// template row only carries the canonical name identity and points at its
		// current revision. The pointer guard is a deferred constraint trigger, so
		// the whole shape must commit atomically in one transaction.
		tx, err := conn.Begin(context.Background())
		if err != nil {
			t.Fatalf("begin runtime pricing template tx %q: %v", name, err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		var templateID int
		if err := tx.QueryRow(
			context.Background(),
			`INSERT INTO pricing_templates (profile_id, name, description, created_at, updated_at) VALUES ($1, $2, NULL, $3, $3) RETURNING id`,
			profileID,
			name,
			now,
		).Scan(&templateID); err != nil {
			t.Fatalf("insert runtime pricing template %q: %v", name, err)
		}
		var revisionID int64
		if err := tx.QueryRow(
			context.Background(),
			`INSERT INTO pricing_template_revisions (template_id, version, pricing_unit, currency_code, currency_attribution, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, effective_at, created_at, created_by_kind)
			VALUES ($1, 1, 'PER_1M', $2, 'legacy_foreign', $3, $4, $5, $6, $7, $8, $8, 'legacy_backfill') RETURNING id`,
			templateID,
			pricingCurrencyCode,
			inputPrice,
			outputPrice,
			nullableRuntimePrice(cachedInputPrice),
			nullableRuntimePrice(cacheCreationPrice),
			nullableRuntimePrice(reasoningPrice),
			now,
		).Scan(&revisionID); err != nil {
			t.Fatalf("insert runtime pricing template revision %q: %v", name, err)
		}
		if _, err := tx.Exec(
			context.Background(),
			`UPDATE pricing_templates SET current_revision_id = $2, updated_at = $3 WHERE id = $1`,
			templateID,
			revisionID,
			now,
		); err != nil {
			t.Fatalf("attach runtime pricing template revision %q: %v", name, err)
		}
		if err := tx.Commit(context.Background()); err != nil {
			t.Fatalf("commit runtime pricing template %q: %v", name, err)
	*/
	var activeEpochID int64
	var activeEpoch int
	var activeCurrency string
	if err := conn.QueryRow(context.Background(), `
		SELECT epochs.id, epochs.epoch, epochs.currency_code
		FROM user_settings AS settings
		JOIN reporting_currency_epochs AS epochs ON epochs.id = settings.current_reporting_currency_epoch_id
		WHERE settings.profile_id = $1`, profileID).Scan(&activeEpochID, &activeEpoch, &activeCurrency); err != nil {
		t.Fatalf("load runtime active reporting currency epoch: %v", err)
	}
	if strings.TrimSpace(pricingCurrencyCode) == "" {
		pricingCurrencyCode = activeCurrency
	}
	attribution := "active_epoch"
	var epochID any = activeEpochID
	var epoch any = activeEpoch
	if !strings.EqualFold(strings.TrimSpace(pricingCurrencyCode), activeCurrency) {
		// Direct fixture insertion models a legacy foreign-currency revision;
		// current authoring rejects this path, while runtime must retain and
		// classify it as missing FX rather than inventing a zero conversion.
		attribution = "legacy_foreign"
		epochID = nil
		epoch = nil
	}
	nilPrice := func(value string) any {
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return value
	}
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin runtime pricing template insert: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var templateID int
	if err := tx.QueryRow(context.Background(), `INSERT INTO pricing_templates (profile_id, name, description, current_revision_id, created_at, updated_at) VALUES ($1, $2, NULL, NULL, $3, $3) RETURNING id`, profileID, name, now).Scan(&templateID); err != nil {
		t.Fatalf("insert runtime pricing template %q: %v", name, err)
	}
	var revisionID int64
	if err := tx.QueryRow(context.Background(), `INSERT INTO pricing_template_revisions (template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, template_kind, effective_at, created_at, created_by_kind, created_by_operation_id) VALUES ($1, 1, 'PER_1M', $2, $3, $4, $5, 'standard', NULL, $6, 'legacy_backfill', NULL) RETURNING id`, templateID, pricingCurrencyCode, epochID, epoch, attribution, now).Scan(&revisionID); err != nil {
		t.Fatalf("insert runtime pricing revision %q: %v", name, err)
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO pricing_template_cards (revision_id, template_kind, card_role, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price) VALUES ($1, 'standard', 'standard', $2, $3, $4, $5, $6)`, revisionID, inputPrice, outputPrice, nilPrice(cachedInputPrice), nilPrice(cacheCreationPrice), nilPrice(reasoningPrice)); err != nil {
		t.Fatalf("insert runtime pricing card %q: %v", name, err)
	}
	if _, err := tx.Exec(context.Background(), `UPDATE pricing_templates SET current_revision_id = $1, updated_at = $2 WHERE id = $3`, revisionID, now, templateID); err != nil {
		t.Fatalf("close runtime pricing template pointer %q: %v", name, err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit runtime pricing template %q: %v", name, err)
	}
	return templateID
}

func advanceRuntimePricingTemplateRevisionWithTier(t *testing.T, conn *pgx.Conn, templateID int) int64 {
	t.Helper()
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tier revision: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	now := time.Now().UTC()
	var oldRevisionID, revisionID int64
	if err := tx.QueryRow(context.Background(), `SELECT current_revision_id FROM pricing_templates WHERE id = $1 FOR UPDATE`, templateID).Scan(&oldRevisionID); err != nil {
		t.Fatalf("load old revision: %v", err)
	}
	if err := tx.QueryRow(context.Background(), `INSERT INTO pricing_template_revisions (template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, template_kind, tier_input_tokens_above, effective_at, created_at, created_by_kind, created_by_operation_id) SELECT template_id, version + 1, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, 'tiered', 272000, $2, $2, 'legacy_backfill', NULL FROM pricing_template_revisions WHERE id = $1 RETURNING id`, oldRevisionID, now).Scan(&revisionID); err != nil {
		t.Fatalf("insert tier revision: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO pricing_template_cards (revision_id, template_kind, card_role, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price) SELECT $1, 'tiered', 'tier_base', input_price, output_price, cached_input_price, cache_creation_price, reasoning_price FROM pricing_template_cards WHERE revision_id = $2`, revisionID, oldRevisionID); err != nil {
		t.Fatalf("copy tier base card: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO pricing_template_cards (revision_id, template_kind, card_role, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price) VALUES ($1, 'tiered', 'tier_above', '4', '18', '2', '5', '20')`, revisionID); err != nil {
		t.Fatalf("insert tier above card: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `UPDATE pricing_templates SET current_revision_id = $1, updated_at = $2 WHERE id = $3`, revisionID, now, templateID); err != nil {
		t.Fatalf("update tier template pointer: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit tier revision: %v", err)
	}
	return revisionID
}

func advanceRuntimePricingTemplateRevision(t *testing.T, conn *pgx.Conn, templateID int) int64 {
	t.Helper()
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin standard revision: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	now := time.Now().UTC()
	var oldRevisionID, revisionID int64
	if err := tx.QueryRow(context.Background(), `SELECT current_revision_id FROM pricing_templates WHERE id = $1 FOR UPDATE`, templateID).Scan(&oldRevisionID); err != nil {
		t.Fatalf("load old revision: %v", err)
	}
	if err := tx.QueryRow(context.Background(), `INSERT INTO pricing_template_revisions (template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, template_kind, effective_at, created_at, created_by_kind, created_by_operation_id) SELECT template_id, version + 1, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, 'standard', $2, $2, 'legacy_backfill', NULL FROM pricing_template_revisions WHERE id = $1 RETURNING id`, oldRevisionID, now).Scan(&revisionID); err != nil {
		t.Fatalf("insert standard revision: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO pricing_template_cards (revision_id, template_kind, card_role, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price) SELECT $1, 'standard', 'standard', input_price, output_price, cached_input_price, cache_creation_price, reasoning_price FROM pricing_template_cards WHERE revision_id = $2`, revisionID, oldRevisionID); err != nil {
		t.Fatalf("copy standard card: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `UPDATE pricing_templates SET current_revision_id = $1, updated_at = $2 WHERE id = $3`, revisionID, now, templateID); err != nil {
		t.Fatalf("update standard pointer: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit standard revision: %v", err)
	}
	return revisionID
}

func attachRuntimeConnectionPricingTemplate(t *testing.T, harness *runtimeHarness, connectionID int, pricingTemplateID int) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE connections SET pricing_template_id = $2, updated_at = $3 WHERE id = $1`,
		connectionID,
		pricingTemplateID,
		now,
	); err != nil {
		t.Fatalf("attach pricing template %d to runtime connection %d: %v", pricingTemplateID, connectionID, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{harness.profileIDForConnection(t, connectionID)}})
}

func requestLogItemsByID(t *testing.T, rawItems []any) map[int]map[string]any {
	t.Helper()
	itemsByID := make(map[int]map[string]any, len(rawItems))
	for _, rawItem := range rawItems {
		item := asMapRuntime(t, rawItem)
		rawID, ok := item["request_log_id"].(string)
		if !ok {
			t.Fatalf("expected request-log item request_log_id string, got %+v", item)
		}
		parsed, err := strconv.Atoi(rawID)
		if err != nil {
			t.Fatalf("expected decimal request_log_id, got %+v", item)
		}
		itemsByID[parsed] = item
	}
	return itemsByID
}

func loadRequestFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve runtime request-log test file path")
	}
	fixturePath := filepath.Join(filepath.Dir(currentFile), "..", "..", "testdata", "requests", name)
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixturePath, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode fixture %s: %v", fixturePath, err)
	}
	return payload
}

const runtimeFixtureUpdateEnv = "PRISM_UPDATE_REQUEST_FIXTURES"

// writeRequestFixtureIfRequested persists the actual payload over the fixture
// when the update env is set (mirrors the migration golden update flow).
func writeRequestFixtureIfRequested(t *testing.T, name string, payload any) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(runtimeFixtureUpdateEnv)) == "" {
		return
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve runtime request-log test file path")
	}
	fixturePath := filepath.Join(filepath.Dir(currentFile), "..", "..", "testdata", "requests", name)
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture update %s: %v", name, err)
	}
	if err := os.WriteFile(fixturePath, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write fixture update %s: %v", name, err)
	}
}

func jsonBytesEqual(t *testing.T, left any, right any) bool {
	t.Helper()
	leftRaw, err := json.Marshal(left)
	if err != nil {
		t.Fatalf("marshal left payload: %v", err)
	}
	rightRaw, err := json.Marshal(right)
	if err != nil {
		t.Fatalf("marshal right payload: %v", err)
	}
	return bytes.Equal(leftRaw, rightRaw)
}

func asMapRuntime(t *testing.T, raw any) map[string]any {
	t.Helper()
	item, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T %+v", raw, raw)
	}
	return item
}

func loadLatestRuntimeRequestLogDetailPayload(t *testing.T, harness *runtimeHarness, profileID int) map[string]any {
	t.Helper()
	var requestLogID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM request_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`, profileID).Scan(&requestLogID); err != nil {
		t.Fatalf("load latest facade request log id: %v", err)
	}
	response := harness.requestJSON(t, http.MethodGet, fmt.Sprintf("/api/stats/requests/%d", requestLogID), nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return payload
}

type runtimeReportCurrencySnapshot struct {
	Code   string
	Symbol string
	Epoch  int
}

func loadRuntimeReportCurrencySnapshot(t *testing.T, conn *pgx.Conn, profileID int) runtimeReportCurrencySnapshot {
	t.Helper()
	var snapshot runtimeReportCurrencySnapshot
	if err := conn.QueryRow(
		context.Background(),
		`SELECT epochs.currency_code, settings.report_currency_symbol, epochs.epoch FROM user_settings AS settings JOIN reporting_currency_epochs AS epochs ON epochs.id = settings.current_reporting_currency_epoch_id WHERE settings.profile_id = $1 ORDER BY settings.id ASC LIMIT 1`,
		profileID,
	).Scan(&snapshot.Code, &snapshot.Symbol, &snapshot.Epoch); err != nil {
		t.Fatalf("load runtime report currency snapshot: %v", err)
	}
	return snapshot
}

func loadRuntimeReportCurrencyCode(t *testing.T, conn *pgx.Conn, profileID int) string {
	return loadRuntimeReportCurrencySnapshot(t, conn, profileID).Code
}

func runtimeNullInt64(value int64) sql.NullInt64 {
	return sql.NullInt64{Int64: value, Valid: true}
}

func runtimeNullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: true}
}

func loadLatestRuntimeIngressRequestID(t *testing.T, conn *pgx.Conn, profileID int) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var ingressRequestID string
		err := conn.QueryRow(
			context.Background(),
			`SELECT ingress_request_id FROM request_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`,
			profileID,
		).Scan(&ingressRequestID)
		if err == nil {
			return ingressRequestID
		}
		time.Sleep(25 * time.Millisecond)
	}
	var ingressRequestID string
	if err := conn.QueryRow(
		context.Background(),
		`SELECT ingress_request_id FROM request_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`,
		profileID,
	).Scan(&ingressRequestID); err != nil {
		t.Fatalf("load latest runtime ingress request id: %v", err)
	}
	return ingressRequestID
}
