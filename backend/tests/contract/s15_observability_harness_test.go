package contracttest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
)

var fixedS15Now = time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC)

func s15AuditWindowQuery() string {
	values := url.Values{}
	values.Set("from", fixedS15Now.Add(-24*time.Hour).Format(time.RFC3339))
	values.Set("to", fixedS15Now.Add(time.Minute).Format(time.RFC3339))
	return values.Encode()
}

func assertErrorCode(t *testing.T, payload map[string]any, want string) {
	t.Helper()
	// The audit/stats surfaces use the flat management problem envelope:
	// {code, detail, params, details, request_id}.
	if payload["code"] != want {
		t.Fatalf("expected error code %q, got %+v", want, payload)
	}
}

func assertS15NoPolicyThresholdFields(t *testing.T, item map[string]any) {
	t.Helper()
	for _, key := range []string{"policy_cycle_retry_attempt_limit", "policy_ban_cumulative_retry_attempt_threshold", "cycle_retry_attempt_limit", "ban_cumulative_retry_attempt_threshold"} {
		if _, ok := item[key]; ok {
			t.Fatalf("current-state payload must stay threshold-free; found %q in %+v", key, item)
		}
	}
}

func assertJSONIntFields(t *testing.T, payload map[string]any, want map[string]int) {
	t.Helper()
	for key, expected := range want {
		if got := jsonInt(t, payload[key]); got != expected {
			t.Fatalf("expected %s=%d, got %+v", key, expected, payload)
		}
	}
}

func s15SumJSONInts(t *testing.T, items []any, keys ...string) map[string]any {
	t.Helper()
	totals := map[string]any{}
	for _, raw := range items {
		item := asMap(t, raw)
		for _, key := range keys {
			totals[key] = float64(intFromJSONNumber(totals[key]) + jsonInt(t, item[key]))
		}
	}
	return totals
}

func s15TokenTrendTotals(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	for _, raw := range asMap(t, payload["token_usage_trends"])["hourly"].([]any) {
		series := asMap(t, raw)
		if series["key"] == "all" {
			return s15SumJSONInts(t, series["points"].([]any), "input_tokens", "output_tokens", "total_tokens", "cached_tokens", "reasoning_tokens")
		}
	}
	t.Fatalf("expected aggregate token trend series, got %+v", payload["token_usage_trends"])
	return nil
}

func assertS15DashboardShape(t *testing.T, payload map[string]any) {
	t.Helper()
	for _, key := range []string{"api_family_rows", "coverage_24h", "coverage_30d", "generated_at", "health", "metric_snapshot", "routing_health_map", "snapshot_revision", "source_watermark", "top_spending_models", "caliber", "dataset_coverage", "samples", "routing_health_caliber"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected dashboard snapshot key %q, got %+v", key, payload)
		}
	}
	if len(payload) != 14 {
		t.Fatalf("expected canonical dashboard snapshot shape, got %+v", payload)
	}
	for _, key := range []string{"window", "covers", "freshness", "metrics", "recent_requests", "strategy_family_summary"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("expected dashboard snapshot to omit legacy key %q, got %+v", key, payload)
		}
	}
}

func assertS15EmptyRoutingHealthMap(t *testing.T, payload map[string]any) {
	t.Helper()
	routingHealthMap := asMap(t, payload["routing_health_map"])
	if len(routingHealthMap["nodes"].([]any)) != 0 || len(routingHealthMap["links"].([]any)) != 0 || jsonInt(t, routingHealthMap["endpointCount"]) != 0 || jsonInt(t, routingHealthMap["modelCount"]) != 0 {
		t.Fatalf("expected empty routing health map shell, got %+v", routingHealthMap)
	}
}

func s15LabelsByID(t *testing.T, items []any, idKey string, labelKey string) map[int]string {
	t.Helper()
	labels := make(map[int]string, len(items))
	for _, raw := range items {
		item := asMap(t, raw)
		labels[jsonInt(t, item[idKey])] = item[labelKey].(string)
	}
	return labels
}

func s15JSON[T any](t *testing.T, harness *contractHarness, profileID int, method string, path string, body any, want int) T {
	t.Helper()
	response := harness.requestJSON(t, harness.client, method, path, body, modelHeader(profileID))
	assertStatus(t, response, want)
	var payload T
	decodeJSONResponse(t, response, &payload)
	return payload
}

func s15GET[T any](t *testing.T, harness *contractHarness, profileID int, path string, want int) T {
	t.Helper()
	return s15JSON[T](t, harness, profileID, http.MethodGet, path, nil, want)
}

func newS15StatsService(t *testing.T, harness *contractHarness, snapshots *statsdomain.DashboardAggregateStore) *managementstats.Service {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), harness.dsn)
	if err != nil {
		t.Fatalf("create stats pool: %v", err)
	}
	t.Cleanup(pool.Close)
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }, DashboardSnapshots: snapshots})
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}
	t.Cleanup(service.Close)
	return service
}

func s15StatsServiceGET(t *testing.T, service *managementstats.Service, profileID int, path string) map[string]any {
	t.Helper()
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode %s response: %v", path, err)
	}
	return payload
}

func newS15ContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	return newContractHarnessFor(t, "s15_contract", contractHarnessOptions{
		SecretEncryptionKey: "s15-contract-secret",
		Version:             "s15-contract-test",
		DependenciesBuilder: func(t *testing.T, testContext context.Context, harness *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies {
			t.Helper()
			ensureContractTestLogPartitions(t, harness,
				contractTestLogPartitionFor("request_logs", fixedS15Now.AddDate(0, 0, -2)),
				contractTestLogPartitionFor("request_logs", fixedS15Now.AddDate(0, 0, -1)),
				contractTestLogPartitionFor("request_logs", fixedS15Now),
				contractTestLogPartitionFor("audit_logs", fixedS15Now.AddDate(0, 0, -2)),
				contractTestLogPartitionFor("audit_logs", fixedS15Now.AddDate(0, 0, -1)),
				contractTestLogPartitionFor("audit_logs", fixedS15Now),
				contractTestLogPartitionFor("usage_request_events", fixedS15Now.AddDate(0, 0, -2)),
				contractTestLogPartitionFor("usage_request_events", fixedS15Now.AddDate(0, 0, -1)),
				contractTestLogPartitionFor("usage_request_events", fixedS15Now),
				contractTestLogPartitionFor("loadbalance_events", fixedS15Now.AddDate(0, 0, -2)),
				contractTestLogPartitionFor("loadbalance_events", fixedS15Now.AddDate(0, 0, -1)),
				contractTestLogPartitionFor("loadbalance_events", fixedS15Now),
			)
			telemetryPool, err := pgxpool.New(testContext, settings.DatabaseURL)
			if err != nil {
				t.Fatalf("create runtime telemetry pgx pool: %v", err)
			}
			t.Cleanup(telemetryPool.Close)
			feedbackPool, err := pgxpool.New(testContext, settings.DatabaseURL)
			if err != nil {
				t.Fatalf("create runtime feedback pgx pool: %v", err)
			}
			t.Cleanup(feedbackPool.Close)
			runtimeCache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
			if err := runtimeCache.Bootstrap(testContext); err != nil {
				t.Fatalf("bootstrap published runtime snapshot: %v", err)
			}
			runtimeState := loadbalancedomain.NewLocalRuntimeStateStore()
			auditService, err := managementaudit.NewService(settings, managementaudit.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }})
			if err != nil {
				t.Fatalf("build audit service: %v", err)
			}
			t.Cleanup(auditService.Close)
			settingsService, err := managementsettings.NewService(settings, managementsettings.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }})
			if err != nil {
				t.Fatalf("build settings service: %v", err)
			}
			t.Cleanup(settingsService.Close)
			loadbalanceService, err := managementloadbalance.NewService(settings, managementloadbalance.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }, RuntimeState: runtimeState})
			if err != nil {
				t.Fatalf("build loadbalance service: %v", err)
			}
			t.Cleanup(loadbalanceService.Close)
			statsService, err := managementstats.NewService(settings, managementstats.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }, SecretEncryptionKey: settings.SecretEncryptionKey})
			if err != nil {
				t.Fatalf("build stats service: %v", err)
			}
			t.Cleanup(statsService.Close)
			runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{ExecutionPool: pool, TelemetryPool: telemetryPool, FeedbackPool: feedbackPool, Now: func() time.Time { return fixedS15Now }, Cache: runtimeCache, RuntimeState: runtimeState})
			if err != nil {
				t.Fatalf("build runtime service: %v", err)
			}
			t.Cleanup(runtimeService.Close)
			harness.runtimeService = runtimeService
			harness.runtimeCache = runtimeCache
			return platformhttp.Dependencies{
				AuditService:       auditService,
				LoadbalanceService: loadbalanceService,
				RuntimeService:     runtimeService,
				RuntimeCache:       runtimeCache,
				RuntimeState:       runtimeState,
				SettingsService:    settingsService,
				StatsService:       statsService,
			}
		},
	})
}

type usageEventSeed struct {
	ID, ProfileID, StatusCode, AttemptCount int
	IngressRequestID, ModelID, APIFamily    string
	SuccessFlag                             bool
	EndpointID, ConnectionID                *int
	ProxyAPIKeyID                           *int
	InputTokens, OutputTokens, TotalTokens  *int
	CacheReadInputTokens                    *int
	CacheCreationInputTokens                *int
	ReasoningTokens                         *int
	ResponseTimeMS, TTFTMS                  *int
	CompletionDurationMS                    *int
	// Output-rate evidence: when state is set the fixture row is a measured
	// sample; otherwise the row keeps NULL evidence and reads as unknown.
	OutputRateState             string
	OutputDeliveryEventCount    *int
	OutputDeliverySpanMS        *int
	EndpointLabelSnapshot       *string
	ProxyAPIKeyNameSnapshot     *string
	UnpricedReason              *string
	BillableFlag, PricedFlag    *bool
	TotalCostUserCurrencyMicros *int64
	RequestPath                 string
	CreatedAt                   time.Time
}

type auditLogSeed struct {
	ID, ProfileID, ResponseStatus                  int
	ModelID, RequestHeaders                        string
	RequestLogID                                   *int
	RequestLogCreatedAt                            *time.Time
	IngressRequestID, RequestBody, ResponseHeaders *string
	ResponseBody                                   *string
	RequestBodyStored, ResponseBodyStored          *bool
	IsStream, AuditEnabledAtRequest                bool
	AuditCaptureBodiesAtRequest                    bool
	CreatedAt                                      time.Time
}

func insertUsageEvent(t *testing.T, harness *contractHarness, seed usageEventSeed) {
	t.Helper()
	seed = coherentUsageEventSeed(seed)
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("usage_request_events", seed.CreatedAt))
	attributionState := "unknown"
	if seed.ProxyAPIKeyID != nil {
		attributionState = "identified"
	}
	if _, err := harness.conn.Exec(
		context.Background(),
		`INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, api_family, endpoint_id, endpoint_label_snapshot, connection_id, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot, proxy_api_key_attribution_state, proxy_api_key_auth_enforced_at_request, status_code, success_flag, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, attempt_count, request_path, created_at, response_time_ms, completion_duration_ms, ttft_ms, output_rate_state, output_delivery_event_count, output_delivery_span_ms, pricing_status, pricing_evidence_trust, unpriced_reason, pricing_resolution_kind) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40)`,
		seed.ID,
		seed.ProfileID,
		seed.IngressRequestID,
		seed.ModelID,
		seed.APIFamily,
		nullableTestInt(seed.EndpointID),
		usageEventEndpointLabel(t, harness, seed),
		nullableTestInt(seed.ConnectionID),
		nullableTestInt(seed.ProxyAPIKeyID),
		nullableTestString(seed.ProxyAPIKeyNameSnapshot),
		attributionState,
		nil,
		seed.StatusCode,
		seed.SuccessFlag,
		nullableTestInt(seed.InputTokens),
		nullableTestInt(seed.OutputTokens),
		nullableTestInt(seed.TotalTokens),
		nullableTestInt(seed.CacheReadInputTokens),
		nullableTestInt(seed.CacheCreationInputTokens),
		nullableTestInt(seed.ReasoningTokens),
		nullableTestInt64(usageEventComponentCost(seed)),
		nullableTestInt64(usageEventComponentCost(seed)),
		nullableTestInt64(usageEventComponentCost(seed)),
		nullableTestInt64(usageEventComponentCost(seed)),
		nullableTestInt64(usageEventComponentCost(seed)),
		nullableTestInt64(seed.TotalCostUserCurrencyMicros),
		nullableTestInt64(seed.TotalCostUserCurrencyMicros),
		seed.AttemptCount,
		seed.RequestPath,
		seed.CreatedAt,
		nullableTestInt(seed.ResponseTimeMS),
		nullableTestInt(seed.CompletionDurationMS),
		nullableTestInt(seed.TTFTMS),
		nullableTestStringIfSet(seed.OutputRateState),
		nullableTestInt(seed.OutputDeliveryEventCount),
		nullableTestInt(seed.OutputDeliverySpanMS),
		usageEventPricingStatus(seed),
		usageEventPricingTrust(seed),
		usageEventUnpricedReason(seed),
		usageEventResolutionKind(seed),
	); err != nil {
		t.Fatalf("insert usage event %d: %v", seed.ID, err)
	}
}

// usageEventPricingStatus projects the four-state status for fixture rows
// (SPEC 3.4): known non-2xx -> ineligible; 2xx priced seeds -> priced; 2xx
// with a typed usage reason -> unpriced; everything else stays unknown.
func usageEventPricingStatus(seed usageEventSeed) string {
	if seed.StatusCode != 0 && (seed.StatusCode < 200 || seed.StatusCode > 299) {
		return "ineligible"
	}
	if seed.PricedFlag != nil && *seed.PricedFlag {
		return "priced"
	}
	if seed.UnpricedReason != nil {
		return "unpriced"
	}
	return "unknown"
}

func usageEventPricingTrust(seed usageEventSeed) string {
	switch usageEventPricingStatus(seed) {
	case "priced":
		return "trusted"
	case "unpriced":
		// An unpriced row carrying any cost value is not canonical (the
		// all-or-none coherence predicate requires all costs absent for
		// unpriced); fixture rows keep legacy_untrusted instead of faking a
		// trusted snapshot.
		if seed.TotalCostUserCurrencyMicros != nil {
			return "legacy_untrusted"
		}
		return "trusted"
	default:
		return "legacy_untrusted"
	}
}

func usageEventUnpricedReason(seed usageEventSeed) *string {
	if usageEventPricingStatus(seed) != "unpriced" {
		return nil
	}
	return seed.UnpricedReason
}

// usageEventResolutionKind gives MISSING_PRICE_DATA fixture rows a typed
// resolution kind so the DB CHECK stays satisfied (SPEC 3.3).
func usageEventResolutionKind(seed usageEventSeed) *string {
	if seed.UnpricedReason != nil && *seed.UnpricedReason == "MISSING_PRICE_DATA" {
		return stringPtr("snapshot_incoherent")
	}
	return nil
}

// usageEventComponentCost gives priced fixtures an all-or-none cost set
// (SPEC 6.5): every component is present (0 when not seeded) so trusted
// priced rows satisfy the DB coherence CHECK.
func usageEventComponentCost(seed usageEventSeed) *int64 {
	if seed.PricedFlag == nil || !*seed.PricedFlag {
		return nil
	}
	if seed.TotalCostUserCurrencyMicros == nil {
		return int64Ptr(0)
	}
	return int64Ptr(0)
}

func coherentUsageEventSeed(seed usageEventSeed) usageEventSeed {
	if seed.APIFamily == "" {
		seed.APIFamily = "openai"
	}
	if seed.StatusCode == 0 {
		seed.StatusCode = http.StatusOK
	}
	if seed.StatusCode >= 200 && seed.StatusCode < 300 {
		seed.SuccessFlag = true
	}
	if seed.AttemptCount == 0 {
		seed.AttemptCount = 1
	}
	if seed.RequestPath == "" {
		seed.RequestPath = "/v1/chat/completions"
	}
	if seed.UnpricedReason != nil {
		trimmed := strings.TrimSpace(*seed.UnpricedReason)
		if trimmed == "" {
			seed.UnpricedReason = nil
		} else {
			seed.UnpricedReason = &trimmed
		}
	}
	if seed.SuccessFlag && seed.BillableFlag == nil {
		seed.BillableFlag = boolPtr(true)
	}
	if seed.SuccessFlag && seed.PricedFlag == nil {
		seed.PricedFlag = boolPtr(true)
	}
	if !seed.SuccessFlag || seed.BillableFlag == nil || !*seed.BillableFlag {
		return seed
	}
	if seed.UnpricedReason != nil {
		// Unpriced rows must be canonical: no cost values at all (the
		// all-or-none coherence predicate and trusted-requires-known checks
		// forbid unpriced rows carrying costs). The fixture drops them so the
		// insert is a coherent trusted snapshot; callers that need a cost
		// projection set it via explicit updates after seeding.
		seed.PricedFlag = boolPtr(false)
		seed.TotalCostUserCurrencyMicros = nil
		return seed
	}
	if seed.TotalCostUserCurrencyMicros != nil {
		seed.PricedFlag = boolPtr(true)
		return seed
	}
	if seed.PricedFlag != nil && *seed.PricedFlag {
		seed.PricedFlag = boolPtr(false)
		seed.UnpricedReason = stringPtr("MISSING_PRICE_DATA")
	}
	return seed
}

func usageEventEndpointLabel(t *testing.T, harness *contractHarness, seed usageEventSeed) string {
	t.Helper()
	if seed.EndpointLabelSnapshot != nil && strings.TrimSpace(*seed.EndpointLabelSnapshot) != "" {
		return strings.TrimSpace(*seed.EndpointLabelSnapshot)
	}
	if seed.EndpointID == nil {
		return "Unknown Endpoint"
	}
	var label string
	if err := harness.conn.QueryRow(context.Background(), `SELECT name FROM endpoints WHERE id = $1`, *seed.EndpointID).Scan(&label); err == nil && strings.TrimSpace(label) != "" {
		return label
	}
	return fmt.Sprintf("Endpoint %d", *seed.EndpointID)
}

func intFromJSONNumber(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}

func insertContractProxyAPIKey(t *testing.T, harness *contractHarness, name string) int {
	t.Helper()
	var keyID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO proxy_api_keys (name, key_prefix, key_hash, last_four, is_active, created_at, updated_at) VALUES ($1, $2, $3, $4, TRUE, $5, $5) RETURNING id`, name, "sk_test", strings.Repeat("a", 64), "1234", fixedS15Now).Scan(&keyID); err != nil {
		t.Fatalf("insert proxy api key %q: %v", name, err)
	}
	return keyID
}

func s15CountRows(t *testing.T, harness *contractHarness, query string, args ...any) int {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count rows with %q: %v", query, err)
	}
	return count
}

func s15InsertProfile(t *testing.T, harness *contractHarness, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var profileID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ($1, NULL, FALSE, FALSE, TRUE, 0, NULL, $2, $2) RETURNING id`, name, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile %q: %v", name, err)
	}
	return profileID
}

func dropS15RequestLogPartition(t *testing.T, harness *contractHarness, createdAt time.Time) {
	t.Helper()
	day := utcContractPartitionDay(createdAt)
	partitionName := fmt.Sprintf("request_logs_p%s", day.Format("20060102"))
	// SAFETY: DDL identifiers cannot be query parameters. The only dynamic
	// piece is the partition name assembled from a formatted date and quoted
	// through quoteIdentifier, which neutralizes quotes; no caller input
	// reaches this statement.
	if _, err := harness.conn.Exec(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS public.%s`, quoteIdentifier(partitionName))); err != nil {
		t.Fatalf("drop request log partition %s: %v", partitionName, err)
	}
}

func withHeader(headers map[string]string, key string, value string) map[string]string {
	merged := make(map[string]string, len(headers)+1)
	maps.Copy(merged, headers)
	merged[key] = value
	return merged
}

func createS15LogRetentionJob(t *testing.T, harness *contractHarness, tableName string, scope map[string]any, suffix string) string {
	t.Helper()
	// Manual cleanup requires the fresh-preflight -> sealed-job flow (SPEC §6);
	// the old table/cutoff create route was removed.
	selection := map[string]any{}
	if cutoff, ok := scope["cutoff"].(string); ok {
		selection["mode"] = "cutoff"
		selection["cutoff"] = cutoff
	} else if deleteAll, ok := scope["delete_all"].(bool); ok && deleteAll {
		selection["mode"] = "delete_all"
	} else {
		selection["mode"] = "keep_days"
		selection["days"] = 7
	}
	operationID := "s15-purge-" + suffix + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
	preflight := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/maintenance/log-retention/preflights", map[string]any{
		"kind":                 "manual_cleanup",
		"operation_id":         operationID,
		"preflight_attempt_id": "s15-attempt-" + suffix,
		"dataset":              tableName,
		"selection":            selection,
	}, map[string]string{}, http.StatusCreated)
	token, ok := preflight["preflight_token"].(string)
	if !ok || token == "" {
		t.Fatalf("expected retention preflight token, got %+v", preflight)
	}
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/maintenance/log-retention/jobs", map[string]any{
		"operation_id":    operationID,
		"preflight_token": token,
		"confirmation":    map[string]any{"keyword": "DELETE"},
	}, withHeader(map[string]string{}, "Idempotency-Key", "log-retention-"+suffix))
	assertStatus(t, response, http.StatusAccepted)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	job, ok := payload["job"].(map[string]any)
	jobID, hasID := job["id"].(string)
	if !ok || !hasID || jobID == "" || job["state"] != "queued" || job["dataset"] != tableName {
		t.Fatalf("expected log-retention job response, got %+v", payload)
	}
	return jobID
}

func refreshS15ActualCoverage(t *testing.T, harness *contractHarness, datasets ...string) {
	t.Helper()
	for _, dataset := range datasets {
		source, err := statsdomain.LoadRetentionSourceProjection(context.Background(), harness.conn, dataset, fixedS15Now)
		if err != nil {
			t.Fatalf("load %s retention source for fixture refresh: %v", dataset, err)
		}
		if err := statsdomain.RefreshActualCoverageProjection(context.Background(), harness.conn, source, fixedS15Now); err != nil {
			t.Fatalf("refresh %s actual coverage for fixture: %v", dataset, err)
		}
	}
}

func nullableTestInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTestStringBytes(value *string) any {
	if value == nil {
		return nil
	}
	return []byte(*value)
}

// nullableTestStringIfSet maps an empty fixture string to SQL NULL so an
// unset output-rate evidence state keeps the evidence columns NULL.
func nullableTestStringIfSet(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func mustDecodeBase64String(t *testing.T, value any) string {
	t.Helper()
	encoded, ok := value.(string)
	if !ok {
		t.Fatalf("expected base64 string, got %T", value)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode base64 body: %v", err)
	}
	return string(decoded)
}

func nullableTestTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func intPtr(value int) *int {
	resolved := value
	return &resolved
}

func boolPtr(value bool) *bool {
	resolved := value
	return &resolved
}

func int64Ptr(value int64) *int64 {
	resolved := value
	return &resolved
}

func timePtr(value time.Time) *time.Time {
	resolved := value.UTC()
	return &resolved
}
