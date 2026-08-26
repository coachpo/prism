package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestStatsRetentionJobs(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertRequestLogSummaryRow(t, harness, 500, profileID, "retention-model", "openai", 12, 81, 200, 100, 0, 0, 0, fixedS15Now.Add(-48*time.Hour))
	insertRequestLogSummaryRow(t, harness, 501, profileID, "retention-model", "openai", 12, 81, 200, 100, 0, 0, 0, fixedS15Now.Add(-30*time.Minute))
	insertUsageEvent(t, harness, usageEventSeed{ID: 40, ProfileID: profileID, IngressRequestID: "stats-retention-old", ModelID: "retention-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-48 * time.Hour)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 41, ProfileID: profileID, IngressRequestID: "stats-retention-new", ModelID: "retention-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-30 * time.Minute)})
	refreshS15ActualCoverage(t, harness, "request_logs", "usage_request_events")

	cutoff := fixedS15Now.Add(-24 * time.Hour).Format(time.RFC3339)
	requestLogJob := createS15LogRetentionJob(t, harness, "request_logs", map[string]any{"cutoff": cutoff}, "request-logs")
	usageJob := createS15LogRetentionJob(t, harness, "usage_request_events", map[string]any{"cutoff": cutoff}, "usage-events")
	if requestLogJob == usageJob || s15CountRows(t, harness, `SELECT COUNT(*) FROM request_logs WHERE profile_id = $1`, profileID) != 2 || s15CountRows(t, harness, `SELECT COUNT(*) FROM usage_request_events WHERE profile_id = $1`, profileID) != 2 {
		t.Fatalf("expected global retention jobs to enqueue without inline stats deletion")
	}
}

func TestAuditDetailMissingRequestLogWeakReference(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	requestCreatedAt := fixedS15Now.AddDate(0, 0, -2).Add(2 * time.Hour)
	auditCreatedAt := fixedS15Now.Add(-5 * time.Minute)
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 9200, profileID, "audit-missing-request", "openai", 12, 91, 200, 100, 0, 0, 0, requestCreatedAt, true)
	insertAuditLog(t, harness, auditLogSeed{ID: 9201, ProfileID: profileID, RequestLogID: intPtr(9200), RequestLogCreatedAt: timePtr(requestCreatedAt), IngressRequestID: stringPtr("weak-request-9200"), ModelID: "audit-missing-request", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, AuditCaptureBodiesAtRequest: false, CreatedAt: auditCreatedAt})
	dropS15RequestLogPartition(t, harness, requestCreatedAt)

	detailPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs/9201", http.StatusOK)
	if detailPayload["request_log_id"] != "9200" || detailPayload["ingress_request_id"] != "weak-request-9200" || detailPayload["request_log_created_at"] == nil || detailPayload["request_log_missing"] != true {
		t.Fatalf("expected audit detail weak request link with missing state, got %+v", detailPayload)
	}
}

func TestAuditPartitionProfileScopedWeakRequestLinkList(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	otherProfileID := s15InsertProfile(t, harness, "S15 Other Audit")
	requestCreatedAt := fixedS15Now.Add(-20 * time.Minute)
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 9300, profileID, "audit-partition", "openai", 12, 91, 200, 100, 0, 0, 0, requestCreatedAt, true)
	insertAuditLog(t, harness, auditLogSeed{ID: 9301, ProfileID: profileID, RequestLogID: intPtr(9300), RequestLogCreatedAt: timePtr(requestCreatedAt), IngressRequestID: stringPtr("weak-request-9300"), ModelID: "audit-partition", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-10 * time.Minute)})
	insertAuditLog(t, harness, auditLogSeed{ID: 9302, ProfileID: otherProfileID, RequestLogID: intPtr(9300), RequestLogCreatedAt: timePtr(requestCreatedAt), IngressRequestID: stringPtr("weak-request-other"), ModelID: "audit-partition", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-9 * time.Minute)})

	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=20", http.StatusOK)
	items := listPayload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected profile-scoped audit partition list, got %+v", listPayload)
	}
	item := asMap(t, items[0])
	if item["request_log_id"] != "9300" || item["ingress_request_id"] != "weak-request-9300" || item["request_log_created_at"] == nil || item["request_log_missing"] != false {
		t.Fatalf("expected audit list weak request fields, got %+v", item)
	}
}

func TestAuditListAndDetailPreserveBigIntRequestLogIDAsDecimalString(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	requestLogID := int(9007199254740997)
	requestCreatedAt := fixedS15Now.Add(-20 * time.Minute)
	auditCreatedAt := fixedS15Now.Add(-10 * time.Minute)
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, requestLogID, profileID, "audit-bigint", "openai", 12, 91, 200, 100, 0, 0, 0, requestCreatedAt, true)
	insertAuditLog(t, harness, auditLogSeed{
		ID:                          9401,
		ProfileID:                   profileID,
		RequestLogID:                intPtr(requestLogID),
		RequestLogCreatedAt:         timePtr(requestCreatedAt),
		IngressRequestID:            stringPtr("audit-bigint-ingress"),
		ModelID:                     "audit-bigint",
		RequestHeaders:              `{}`,
		ResponseStatus:              200,
		AuditEnabledAtRequest:       true,
		AuditCaptureBodiesAtRequest: false,
		CreatedAt:                   auditCreatedAt,
	})
	want := "9007199254740997"

	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&request_log_id="+want+"&limit=20", http.StatusOK)
	items := listPayload["items"].([]any)
	if len(items) != 1 || asMap(t, items[0])["request_log_id"] != want {
		t.Fatalf("expected audit list request_log_id %q as a decimal string, got %+v", want, listPayload)
	}

	detailPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs/9401", http.StatusOK)
	if detailPayload["request_log_id"] != want {
		t.Fatalf("expected audit detail request_log_id %q as a decimal string, got %T(%v)", want, detailPayload["request_log_id"], detailPayload["request_log_id"])
	}
}

func TestAuditLogs(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	streamBody := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"usage\":null}}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":13,\"total_tokens\":20}}}\n\n"
	insertRequestLogSummaryRow(t, harness, 700, profileID, "audit-model", "openai", 12, 91, 200, 100, 0, 0, 0, fixedS15Now.Add(-20*time.Minute))
	insertAuditLog(t, harness, auditLogSeed{ID: 800, ProfileID: profileID, RequestLogID: intPtr(700), ModelID: "audit-model", RequestHeaders: `{"authorization":"Bearer [REDACTED]"}`, RequestBody: stringPtr(strings.Repeat("a", 210)), ResponseHeaders: stringPtr(`{"x-request-id":"req_1"}`), ResponseBody: stringPtr(`{"ok":true}`), ResponseStatus: 200, AuditEnabledAtRequest: false, AuditCaptureBodiesAtRequest: true, CreatedAt: fixedS15Now.Add(-10 * time.Minute)})
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 701, profileID, "audit-model", "openai", 12, 91, 200, 100, 0, 0, 0, fixedS15Now.Add(-8*time.Minute), true)
	insertAuditLog(t, harness, auditLogSeed{ID: 801, ProfileID: profileID, RequestLogID: intPtr(701), ModelID: "audit-model", RequestHeaders: `{"authorization":"Bearer [REDACTED]"}`, RequestBody: stringPtr(strings.Repeat("b", 210)), ResponseHeaders: stringPtr(`{"x-request-id":"req_2"}`), ResponseBody: stringPtr(`{"ok":true}`), ResponseStatus: 200, AuditEnabledAtRequest: true, AuditCaptureBodiesAtRequest: true, CreatedAt: fixedS15Now.Add(-5 * time.Minute)})
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 702, profileID, "audit-model", "openai", 12, 91, 200, 100, 0, 0, 0, fixedS15Now.Add(-6*time.Minute), true)
	insertAuditLog(t, harness, auditLogSeed{ID: 802, ProfileID: profileID, RequestLogID: intPtr(702), ModelID: "audit-model", RequestHeaders: `{"authorization":"Bearer [REDACTED]"}`, ResponseStatus: 200, AuditEnabledAtRequest: true, AuditCaptureBodiesAtRequest: false, CreatedAt: fixedS15Now.Add(-4 * time.Minute)})
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 703, profileID, "audit-model", "openai", 12, 91, 200, 100, 7, 13, 20, fixedS15Now.Add(-3*time.Minute), true)
	insertAuditLog(t, harness, auditLogSeed{ID: 803, ProfileID: profileID, RequestLogID: intPtr(703), ModelID: "audit-model", RequestHeaders: `{"authorization":"Bearer [REDACTED]"}`, RequestBody: stringPtr(`{"model":"audit-model","stream":true}`), ResponseHeaders: stringPtr(`{"content-type":"text/event-stream"}`), ResponseBody: &streamBody, ResponseStatus: 200, IsStream: true, AuditEnabledAtRequest: true, AuditCaptureBodiesAtRequest: true, CreatedAt: fixedS15Now.Add(-3 * time.Minute)})

	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&request_log_id=700&limit=20", http.StatusConflict)
	if listPayload["detail"] != "Audit capture unavailable for this request" {
		t.Fatalf("expected disabled request snapshot to reject audit list, got %+v", listPayload)
	}

	detailPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs/800", http.StatusConflict)
	if detailPayload["detail"] != "Audit capture unavailable for this request" {
		t.Fatalf("expected disabled request snapshot to reject audit detail, got %+v", detailPayload)
	}

	visibleListPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=20", http.StatusOK)
	items := visibleListPayload["items"].([]any)
	if visibleListPayload["has_more"] != false || visibleListPayload["next_cursor"] != nil || len(items) != 3 {
		t.Fatalf("expected audit list to show only enabled rows, got %+v", visibleListPayload)
	}
	if jsonInt(t, asMap(t, items[0])["id"]) != 803 || jsonInt(t, asMap(t, items[1])["id"]) != 802 || jsonInt(t, asMap(t, items[2])["id"]) != 801 {
		t.Fatalf("expected streaming row to sort ahead of metadata-only and full-capture rows, got %+v", visibleListPayload)
	}
	streamListItem := asMap(t, items[0])
	if streamListItem["is_stream"] != true || streamListItem["response_body_stored"] != true || streamListItem["audit_capture_bodies_at_request"] != true {
		t.Fatalf("expected audit list streaming row to expose stored-body metadata, got %+v", streamListItem)
	}

	metadataDetailPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs/802", http.StatusOK)
	if metadataDetailPayload["request_body"] != nil || metadataDetailPayload["response_body"] != nil || metadataDetailPayload["request_body_stored"] != false || metadataDetailPayload["response_body_stored"] != false || metadataDetailPayload["audit_capture_bodies_at_request"] != false {
		t.Fatalf("expected metadata-only audit detail to be a first-class nil-body state, got %+v", metadataDetailPayload)
	}

	enabledDetailPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs/801", http.StatusOK)
	if enabledDetailPayload["request_body_base64"] == nil || enabledDetailPayload["response_body_base64"] == nil || enabledDetailPayload["request_body_stored"] != true || enabledDetailPayload["response_body_stored"] != true || enabledDetailPayload["audit_capture_bodies_at_request"] != true {
		t.Fatalf("expected audit detail to return full captured bodies for enabled requests, got %+v", enabledDetailPayload)
	}

	streamDetailPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs/803", http.StatusOK)
	if streamDetailPayload["is_stream"] != true || streamDetailPayload["response_body_stored"] != true || streamDetailPayload["audit_capture_bodies_at_request"] != true || streamDetailPayload["response_body_base64"] == nil {
		t.Fatalf("expected streaming audit detail to return raw stored SSE body, got %+v", streamDetailPayload)
	}
	streamResponseBody := mustDecodeBase64String(t, streamDetailPayload["response_body_base64"])
	if streamResponseBody != streamBody {
		t.Fatalf("expected streaming audit detail to return raw stored SSE body, got %q", streamResponseBody)
	}
	if !strings.Contains(streamResponseBody, "event: response.created") || !strings.Contains(streamResponseBody, "event: response.completed") || strings.HasPrefix(strings.TrimSpace(streamResponseBody), "{") {
		t.Fatalf("expected streaming audit detail response body to preserve raw SSE framing, got %q", streamResponseBody)
	}
}

func TestAuditLogRetentionJob(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertAuditLog(t, harness, auditLogSeed{ID: 900, ProfileID: profileID, ModelID: "audit-retention", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-48 * time.Hour)})
	insertAuditLog(t, harness, auditLogSeed{ID: 901, ProfileID: profileID, ModelID: "audit-retention", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-30 * time.Minute)})
	refreshS15ActualCoverage(t, harness, "audit_logs")

	// A manual audit purge job queues without inline deletion; the scheduled
	// policy flow is covered by the v2 planner tests.
	jobID := createS15LogRetentionJob(t, harness, "audit_logs", map[string]any{}, "audit-policy")
	if jobID == "" || s15CountRows(t, harness, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1`, profileID) != 2 {
		t.Fatalf("expected audit log-retention job to enqueue without inline delete")
	}
}

func TestManagementLogRetentionJobCreateContract(t *testing.T) {
	harness := newS15ContractHarness(t)
	jobID := createS15LogRetentionJob(t, harness, "loadbalance_events", map[string]any{"delete_all": true}, "create")
	if jobID == "" {
		t.Fatal("expected log-retention job id")
	}
}

func TestManagementLogRetentionJobIdempotency(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	// First acceptance creates the durable job; a lost-response retry with the
	// same operation id and request hash replays the recorded job (SPEC §6.4).
	operationID := "s15-idem-" + fmt.Sprintf("%d", time.Now().UnixNano())
	preflight := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/maintenance/log-retention/preflights", map[string]any{
		"kind":                 "manual_cleanup",
		"operation_id":         operationID,
		"preflight_attempt_id": "s15-idem-attempt",
		"dataset":              "audit_logs",
		"selection":            map[string]any{"mode": "delete_all"},
	}, map[string]string{}, http.StatusCreated)
	token := preflight["preflight_token"].(string)
	body := map[string]any{"operation_id": operationID, "preflight_token": token, "confirmation": map[string]any{"keyword": "DELETE"}}
	first := harness.requestJSON(t, harness.client, http.MethodPost, "/api/maintenance/log-retention/jobs", body, modelHeader(profileID))
	assertStatus(t, first, http.StatusAccepted)
	var firstPayload struct {
		Job map[string]any `json:"job"`
	}
	decodeJSONResponse(t, first, &firstPayload)
	firstID, _ := firstPayload.Job["id"].(string)
	second := harness.requestJSON(t, harness.client, http.MethodPost, "/api/maintenance/log-retention/jobs", body, modelHeader(profileID))
	assertStatus(t, second, http.StatusOK)
	var secondPayload struct {
		Replayed bool           `json:"replayed"`
		Job      map[string]any `json:"job"`
	}
	decodeJSONResponse(t, second, &secondPayload)
	secondID, _ := secondPayload.Job["id"].(string)
	if !secondPayload.Replayed || firstID == "" || secondID != firstID {
		t.Fatalf("expected idempotent replay to return the same job, got replayed=%v first=%s second=%s", secondPayload.Replayed, firstID, secondID)
	}
}

func TestRequestLogDeletionDoesNotWidenAuditVisibility(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertRequestLogSummaryRow(t, harness, 710, profileID, "audit-model", "openai", 12, 91, 200, 100, 0, 0, 0, fixedS15Now.Add(-12*time.Minute))
	insertAuditLog(t, harness, auditLogSeed{ID: 810, ProfileID: profileID, RequestLogID: intPtr(710), ModelID: "audit-model", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: false, CreatedAt: fixedS15Now.Add(-11 * time.Minute)})
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 711, profileID, "audit-model", "openai", 12, 91, 200, 100, 0, 0, 0, fixedS15Now.Add(-10*time.Minute), true)
	insertAuditLog(t, harness, auditLogSeed{ID: 811, ProfileID: profileID, RequestLogID: intPtr(711), ModelID: "audit-model", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-9 * time.Minute)})
	refreshS15ActualCoverage(t, harness, "request_logs", "audit_logs")

	beforeDeletePayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=20", http.StatusOK)
	if len(beforeDeletePayload["items"].([]any)) != 1 || jsonInt(t, asMap(t, beforeDeletePayload["items"].([]any)[0])["id"]) != 811 {
		t.Fatalf("expected only enabled audit row visible before request-log deletion, got %+v", beforeDeletePayload)
	}

	jobID := createS15LogRetentionJob(t, harness, "request_logs", map[string]any{"delete_all": true}, "request-log-visibility")
	if jobID == "" {
		t.Fatal("expected request-log retention job id")
	}
	if s15CountRows(t, harness, `SELECT COUNT(*) FROM request_logs WHERE profile_id = $1`, profileID) != 2 {
		t.Fatalf("expected global request-log retention job to avoid inline parent request deletion")
	}
	if s15CountRows(t, harness, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1 AND request_log_id IS NULL`, profileID) != 0 {
		t.Fatalf("expected global request-log retention job to avoid inline audit orphaning")
	}

	afterJobPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=20", http.StatusOK)
	if len(afterJobPayload["items"].([]any)) != 1 || jsonInt(t, asMap(t, afterJobPayload["items"].([]any)[0])["id"]) != 811 {
		t.Fatalf("expected queued request-log retention job not to widen audit visibility inline, got %+v", afterJobPayload)
	}
}

func TestManagementAuditListRequiresWindow(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	payload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?limit=20", http.StatusBadRequest)
	assertErrorCode(t, payload, "audit_window_required")
}

func TestManagementAuditListRejectsOversizedWindow(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	from := fixedS15Now.Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	to := fixedS15Now.Format(time.RFC3339)
	payload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?from="+from+"&to="+to, http.StatusBadRequest)
	assertErrorCode(t, payload, "audit_window_too_large")
}

func TestManagementAuditRejectsUnsupportedFilters(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	cases := []string{
		"actor_id=operator",
		"from_time=" + fixedS15Now.Add(-time.Hour).Format(time.RFC3339),
		"to_time=" + fixedS15Now.Format(time.RFC3339),
	}
	for _, query := range cases {
		payload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&"+query, http.StatusBadRequest)
		assertErrorCode(t, payload, "audit_filter_unsupported")
	}
}

func TestManagementAuditCursorIntegrity(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertAuditLog(t, harness, auditLogSeed{ID: 820, ProfileID: profileID, ModelID: "audit-cursor", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-3 * time.Minute)})
	insertAuditLog(t, harness, auditLogSeed{ID: 821, ProfileID: profileID, ModelID: "audit-cursor", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-2 * time.Minute)})

	firstPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=1", http.StatusOK)
	if firstPayload["has_more"] != true || firstPayload["next_cursor"] == nil {
		t.Fatalf("expected first audit cursor page to include next_cursor, got %+v", firstPayload)
	}

	cursor := firstPayload["next_cursor"].(string)
	secondPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=1&cursor="+cursor, http.StatusOK)
	if jsonInt(t, asMap(t, secondPayload["items"].([]any)[0])["id"]) != 820 {
		t.Fatalf("expected keyset cursor page to continue after newest row, got %+v", secondPayload)
	}

	// Tamper with the first signature character, not the last one: a 32-byte
	// HMAC encodes to 43 base64 characters whose final character carries only
	// four significant bits, so replacing it decodes to the same signature
	// roughly one run in sixteen and the forgery would be accepted.
	signatureStart := strings.LastIndex(cursor, ".") + 1
	replacement := byte('x')
	if cursor[signatureStart] == replacement {
		replacement = 'y'
	}
	tamperedCursor := cursor[:signatureStart] + string(replacement) + cursor[signatureStart+1:]
	tamperedPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=1&cursor="+tamperedCursor, http.StatusBadRequest)
	assertErrorCode(t, tamperedPayload, "audit_cursor_invalid")
}

func insertAuditLog(t *testing.T, harness *contractHarness, seed auditLogSeed) {
	t.Helper()
	auditCaptureBodiesAtRequest := seed.AuditCaptureBodiesAtRequest
	if !auditCaptureBodiesAtRequest && (seed.RequestBody != nil || seed.ResponseBody != nil) {
		auditCaptureBodiesAtRequest = true
	}
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("audit_logs", seed.CreatedAt))
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO audit_logs (id, profile_id, request_log_id, request_log_created_at, ingress_request_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_headers_scrub_provenance, request_headers_capture_status, request_body, request_body_encoding, request_body_capture_provenance, request_body_capture_end_state, request_body_capture_status, request_body_bytes_observed, request_body_bytes_stored, legacy_status_code, legacy_duration_ms, response_headers, response_headers_scrub_provenance, response_headers_capture_status, response_body, response_body_encoding, response_body_capture_provenance, response_body_capture_end_state, response_body_capture_status, response_body_bytes_observed, response_body_bytes_stored, row_kind, url_scrub_provenance, is_stream, audit_enabled_at_request, audit_capture_bodies_at_request, created_at) VALUES ($1, $2, $3, $4, $5, $6, NULL, NULL, 'https://audit.invalid', 'Audit endpoint', 'POST', 'https://audit.invalid/v1/chat/completions', $7::jsonb, 'legacy_unknown', CASE WHEN $7::jsonb IS NULL THEN 'not_requested' ELSE 'captured' END, $8::bytea, CASE WHEN $8::bytea IS NULL THEN NULL ELSE 'utf8' END, CASE WHEN $8::bytea IS NULL THEN 'not_applicable' ELSE 'legacy_text_transcoded' END, CASE WHEN $8::bytea IS NULL THEN NULL ELSE 'unknown' END, CASE WHEN $8::bytea IS NULL THEN 'not_requested' ELSE 'captured' END, CASE WHEN $8::bytea IS NULL THEN NULL ELSE octet_length($8::bytea) END, CASE WHEN $8::bytea IS NULL THEN NULL ELSE octet_length($8::bytea) END, $9, 1234, $10::jsonb, 'legacy_unknown', CASE WHEN $10::jsonb IS NULL THEN 'not_requested' ELSE 'captured' END, $11::bytea, CASE WHEN $11::bytea IS NULL THEN NULL ELSE 'utf8' END, CASE WHEN $11::bytea IS NULL THEN 'not_applicable' ELSE 'legacy_text_transcoded' END, CASE WHEN $11::bytea IS NULL THEN NULL ELSE 'unknown' END, CASE WHEN $11::bytea IS NULL THEN 'not_requested' ELSE 'captured' END, CASE WHEN $11::bytea IS NULL THEN NULL ELSE octet_length($11::bytea) END, CASE WHEN $11::bytea IS NULL THEN NULL ELSE octet_length($11::bytea) END, 'legacy_unknown', 'legacy_unknown', $12, $13, $14, $15)`, seed.ID, seed.ProfileID, nullableTestInt(seed.RequestLogID), nullableTestTime(seed.RequestLogCreatedAt), nullableTestString(seed.IngressRequestID), seed.ModelID, seed.RequestHeaders, nullableTestStringBytes(seed.RequestBody), seed.ResponseStatus, seed.ResponseHeaders, nullableTestStringBytes(seed.ResponseBody), seed.IsStream, seed.AuditEnabledAtRequest, auditCaptureBodiesAtRequest, seed.CreatedAt); err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok {
			t.Fatalf("insert audit log %d at %s: %s (%s)", seed.ID, seed.CreatedAt.UTC().Format(time.RFC3339), pgErr.Message, pgErr.Detail)
		}
		t.Fatalf("insert audit log %d at %s: %v", seed.ID, seed.CreatedAt.UTC().Format(time.RFC3339), err)
	}
}

func TestAuditAnchorItemOutsideFirstPageIncludedExactlyOnce(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	requestCreatedAt := fixedS15Now.Add(-30 * time.Minute)
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 9400, profileID, "audit-anchor", "openai", 12, 91, 200, 100, 0, 0, 0, requestCreatedAt, true)
	// 25 audit rows for the same ingress: the anchored row (id 9401, oldest)
	// falls outside the first page of limit=5.
	for index := 0; index < 25; index++ {
		insertAuditLog(t, harness, auditLogSeed{
			ID:                    9401 + index,
			ProfileID:             profileID,
			RequestLogID:          intPtr(9400),
			RequestLogCreatedAt:   timePtr(requestCreatedAt),
			IngressRequestID:      stringPtr("anchor-ingress-9400"),
			ModelID:               "audit-anchor",
			RequestHeaders:        `{}`,
			ResponseStatus:        200,
			AuditEnabledAtRequest: true,
			CreatedAt:             fixedS15Now.Add(-time.Duration(25-index) * time.Minute),
		})
	}

	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=5&anchor_id=9401", http.StatusOK)
	items := listPayload["items"].([]any)
	if len(items) != 5 {
		t.Fatalf("expected first page of 5, got %d: %+v", len(items), listPayload)
	}
	anchorPayload, ok := listPayload["anchor_item"].(map[string]any)
	if !ok {
		t.Fatalf("expected anchor_item present in first response, got %+v", listPayload)
	}
	if jsonInt(t, anchorPayload["id"]) != 9401 {
		t.Fatalf("expected anchor_item id 9401, got %+v", anchorPayload)
	}
	// Exactly once: anchor is not duplicated inside items.
	for _, raw := range items {
		item := asMap(t, raw)
		if jsonInt(t, item["id"]) == 9401 {
			t.Fatalf("anchor id 9401 duplicated inside items: %+v", listPayload)
		}
	}

	// In-page anchor: no anchor_item, no duplication.
	inPagePayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=5&anchor_id=9425", http.StatusOK)
	if _, exists := inPagePayload["anchor_item"]; exists {
		t.Fatalf("in-page anchor must not emit anchor_item: %+v", inPagePayload)
	}

	// Unknown anchor id: no anchor_item, normal list.
	unknownPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=5&anchor_id=999999", http.StatusOK)
	if _, exists := unknownPayload["anchor_item"]; exists {
		t.Fatalf("unknown anchor must not emit anchor_item: %+v", unknownPayload)
	}
}
