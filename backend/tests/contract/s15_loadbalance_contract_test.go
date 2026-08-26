package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
)

func TestLoadbalanceCurrentState(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Loadbalance Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "lb-model", stringPtr("Loadbalance Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Endpoint")
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	insertRuntimeState(t, harness, runtimeStateSeed{ProfileID: profileID, ConnectionID: connectionID, ConsecutiveFailures: 2, LastFailureKind: stringPtr("transient_http"), LastCooldownSeconds: 60.0, BanMode: "off", BlockedUntilAt: timePtr(fixedS15Now.Add(30 * time.Minute)), LastSuccessResponseHeadersLatencyMS: intPtr(540), CreatedAt: fixedS15Now.Add(-1 * time.Hour), UpdatedAt: fixedS15Now})

	// Global view: no internal row-id input; the configured-target union is the
	// row set and completeness describes the process-local observation.
	payload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/current-state", http.StatusOK)
	if payload["scope"] != "process" || fmt.Sprint(payload["instance_id"]) == "" || fmt.Sprint(payload["configuration_revision"]) == "" {
		t.Fatalf("expected global current-state envelope, got %+v", payload)
	}
	completeness := asMap(t, payload["completeness"])
	if completeness["state"] != "ready" || completeness["complete"] != true || jsonInt(t, completeness["configured_target_count"]) != 1 || jsonInt(t, completeness["observed_target_count"]) != 1 || jsonInt(t, completeness["unobserved_target_count"]) != 0 {
		t.Fatalf("expected ready completeness with one observed target, got %+v", completeness)
	}
	item := s15CurrentStateItemByConnectionID(t, payload, connectionID)
	if item["observation_state"] != "observed" || item["state"] != "retry_wait" || item["next_retry_at"] == nil || jsonInt(t, item["cycle_retry_attempts"]) != 2 || jsonInt(t, item["last_success_response_headers_latency_ms"]) != 540 {
		t.Fatalf("unexpected loadbalance global current-state item: %+v", item)
	}
	model := asMap(t, item["model"])
	target := asMap(t, item["terminal_target"])
	if jsonInt(t, model["model_config_id"]) != modelConfigID || model["id"] != "lb-model" || jsonInt(t, target["id"]) != connectionID || item["available"] != false {
		t.Fatalf("unexpected global current-state identities: %+v", item)
	}
	assertS15NoPolicyThresholdFields(t, item)
}

func TestObservabilityLoadbalanceRetryWindowStateAndSummaryRemainCoherent(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Retry Window Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "lb-retry-window-model", stringPtr("Loadbalance Retry Window Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Retry Window Endpoint")
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	nextRetryAt := fixedS15Now.Add(1 * time.Minute)
	failureKind := "transient_http"
	insertRuntimeState(t, harness, runtimeStateSeed{
		ProfileID:           profileID,
		ConnectionID:        connectionID,
		ConsecutiveFailures: 1,
		LastFailureKind:     &failureKind,
		LastCooldownSeconds: 60.0,
		BanMode:             "off",
		BlockedUntilAt:      timePtr(nextRetryAt),
		CreatedAt:           fixedS15Now.Add(-10 * time.Minute),
		UpdatedAt:           fixedS15Now,
	})

	currentStatePayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/current-state", http.StatusOK)
	item := s15CurrentStateItemByConnectionID(t, currentStatePayload, connectionID)
	if item["observation_state"] != "observed" || item["state"] != "retry_wait" || item["next_retry_at"] == nil || jsonInt(t, item["cycle_retry_attempts"]) != 1 || jsonInt(t, item["last_retry_delay_ms"]) != 60000 {
		t.Fatalf("expected retry-window current-state payload, got %+v", item)
	}
	assertS15NoPolicyThresholdFields(t, item)

	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1150, ProfileID: profileID, ConnectionID: connectionID, EventType: "retry_scheduled", FailureKind: &failureKind, ConsecutiveFailures: 1, CooldownSeconds: 60.0, ModelID: stringPtr("lb-retry-window-model"), EndpointID: &endpointID, BanMode: stringPtr("off"), CreatedAt: fixedS15Now.Add(-1 * time.Second)})

	queryContext := s15IssueEventsContext(t, harness, profileID, "24h")
	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&model_id=lb-retry-window-model&limit=20", http.StatusOK)
	event := s15LoadbalanceEventByConnectionID(t, listPayload, connectionID)
	summary := asMap(t, event["summary"])
	params := asMap(t, summary["params"])
	if event["event_type"] != "retry_scheduled" || summary["code"] != "loadbalance.retry_scheduled" || jsonInt(t, summary["version"]) != 1 || params["evidence_state"] != "complete" || params["failure_kind"] != "transient_http" || jsonInt(t, params["cycle_retry_attempts"]) != 1 {
		t.Fatalf("expected retry-scheduled V1 summary payload, got %+v", event)
	}

	detailPayload := s15GET[map[string]any](t, harness, profileID, fmt.Sprintf("/api/loadbalance/events/%s?query_context=%s", event["event_id"], url.QueryEscape(queryContext)), http.StatusOK)
	detailSummary := asMap(t, detailPayload["summary"])
	detailParams := asMap(t, detailSummary["params"])
	if detailPayload["event_type"] != "retry_scheduled" || detailSummary["code"] != "loadbalance.retry_scheduled" || detailParams["evidence_state"] != "complete" || jsonInt(t, detailParams["cycle_retry_attempts"]) != 1 {
		t.Fatalf("expected retry-scheduled event detail payload, got %+v", detailPayload)
	}
}

func TestLoadbalanceReset(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Reset Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "lb-reset-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Reset Endpoint")
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	insertRuntimeState(t, harness, runtimeStateSeed{ProfileID: profileID, ConnectionID: connectionID, ConsecutiveFailures: 3, LastFailureKind: stringPtr("timeout"), LastCooldownSeconds: 90.0, BanMode: "until_reset", CreatedAt: fixedS15Now.Add(-1 * time.Hour), UpdatedAt: fixedS15Now})
	for range 3 {
		harness.runtimeService.RuntimeState().ClaimRoundRobinCursor(profileID, modelConfigID, 4)
	}

	payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/loadbalance/current-state/%d/reset", connectionID), nil, http.StatusOK)
	if jsonInt(t, payload["connection_id"]) != connectionID || payload["cleared"] != true {
		t.Fatalf("unexpected loadbalance reset payload: %+v", payload)
	}
	resetState := asMap(t, payload["state"])
	if resetState == nil {
		t.Fatalf("expected reset payload to include state DTO, got %+v", payload)
	}
	if resetState["state"] != "available" || jsonInt(t, resetState["cycle_retry_attempts"]) != 0 || jsonInt(t, resetState["cumulative_retry_attempts"]) != 0 || resetState["next_retry_at"] != nil || jsonInt(t, resetState["last_retry_delay_ms"]) != 0 || resetState["ban_mode"] != "off" || resetState["banned_until_at"] != nil || resetState["last_failure_kind"] != nil {
		t.Fatalf("expected reset to clear cooldown fields in state DTO, got %+v", resetState)
	}
	if _, ok := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, connectionID); !ok {
		t.Fatalf("expected loadbalance reset to preserve local runtime state row")
	}
	if cursor := harness.runtimeService.RuntimeState().PeekRoundRobinCursor(profileID, modelConfigID, 4); cursor == 0 {
		t.Fatalf("expected loadbalance reset to preserve round-robin cursor, got %d", cursor)
	}
	if _, exists := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, connectionID); !exists {
		t.Fatalf("expected narrow reset to preserve the connection state key")
	}
	if cursor := harness.runtimeService.RuntimeState().PeekRoundRobinCursor(profileID, modelConfigID, 4); cursor == 0 {
		t.Fatalf("expected narrow reset to preserve the round-robin cursor, got 0")
	}
	if cursor := harness.runtimeService.RuntimeState().PeekRoundRobinCursor(profileID, modelConfigID, 4); cursor != 3 {
		t.Fatalf("expected loadbalance reset to preserve round-robin cursor, got %d", cursor)
	}

	// Second reset has no cooldown fields left to clear: cleared=false with snapshot.
	secondPayload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/loadbalance/current-state/%d/reset", connectionID), nil, http.StatusOK)
	if jsonInt(t, secondPayload["connection_id"]) != connectionID || secondPayload["cleared"] != false || secondPayload["state"] == nil {
		t.Fatalf("expected no-op reset payload with cleared=false and state snapshot, got %+v", secondPayload)
	}

	// Unknown or cross-profile connection id returns 404.
	s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/loadbalance/current-state/999999/reset", nil, http.StatusNotFound)
}

func TestLoadbalanceEvents(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1000, ProfileID: profileID, ConnectionID: 1, EventType: "retry_scheduled", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 2, CooldownSeconds: 60.0, ModelID: stringPtr("lb-events-model"), EndpointID: intPtr(12), BanMode: stringPtr("off"), CreatedAt: fixedS15Now.Add(-2 * time.Minute)})
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1001, ProfileID: profileID, ConnectionID: 1, EventType: "banned", FailureKind: stringPtr("transient_http"), ConsecutiveFailures: 3, CooldownSeconds: 120.0, ModelID: stringPtr("lb-events-model"), EndpointID: intPtr(12), BanMode: stringPtr("temporary"), PolicyCycleRetryAttemptLimit: intPtr(2), PolicyBanCumulativeRetryAttemptThreshold: intPtr(3), BannedUntilAt: timePtr(fixedS15Now.Add(1 * time.Hour)), CreatedAt: fixedS15Now.Add(-1 * time.Minute)})

	queryContext := s15IssueEventsContext(t, harness, profileID, "24h")
	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&model_id=lb-events-model&limit=50", http.StatusOK)
	items := listPayload["items"].([]any)
	if len(items) != 2 || listPayload["has_more"] != false {
		t.Fatalf("expected loadbalance events list payload, got %+v", listPayload)
	}
	first := asMap(t, items[0])
	firstSummary := asMap(t, first["summary"])
	firstParams := asMap(t, firstSummary["params"])
	if first["event_id"] != "1001" || firstSummary["code"] != "loadbalance.banned" || jsonInt(t, firstParams["policy_ban_cumulative_retry_attempt_threshold"]) != 3 || jsonInt(t, first["policy_cycle_retry_attempt_limit"]) != 2 || jsonInt(t, first["policy_ban_cumulative_retry_attempt_threshold"]) != 3 || first["ban_mode"] != "temporary" || first["banned_until_at"] == nil {
		t.Fatalf("expected newest banned event with public policy snapshots and V1 summary, got %+v", first)
	}
	if firstParams["failure_kind"] != "transient_http" || firstParams["evidence_state"] != "complete" {
		t.Fatalf("expected complete banned summary params, got %+v", firstParams)
	}
	// The list DTO carries the policy snapshots as its own top-level typed
	// fields (SPEC §7.2); the summary params reconcile with them.
	if jsonInt(t, firstParams["cycle_retry_attempts"]) != jsonInt(t, first["cycle_retry_attempts"]) {
		t.Fatalf("summary params must reconcile with top-level facts: %+v vs %+v", firstParams, first)
	}

	// Bidirectional stable sort: desc is newest-first; asc restores the causal
	// order (older event first).
	ascPayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&sort_order=asc&limit=50", http.StatusOK)
	ascItems := ascPayload["items"].([]any)
	if len(ascItems) != 2 || asMap(t, ascItems[0])["event_id"] != "1000" || asMap(t, ascItems[1])["event_id"] != "1001" {
		t.Fatalf("expected asc order 1000 -> 1001, got %+v", ascPayload)
	}

	detailPayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/events/1001?query_context="+url.QueryEscape(queryContext), http.StatusOK)
	detailSummary := asMap(t, detailPayload["summary"])
	detailParams := asMap(t, detailSummary["params"])
	if jsonInt(t, detailPayload["cycle_retry_attempts"]) != 3 || jsonInt(t, detailPayload["cumulative_retry_attempts"]) != 3 || jsonInt(t, detailPayload["last_retry_delay_ms"]) != 120000 || detailPayload["ban_mode"] != "temporary" || jsonInt(t, detailPayload["policy_cycle_retry_attempt_limit"]) != 2 || jsonInt(t, detailPayload["policy_ban_cumulative_retry_attempt_threshold"]) != 3 || detailSummary["code"] != "loadbalance.banned" || detailParams["ban_mode"] != "temporary" || detailParams["banned_until_at"] == nil {
		t.Fatalf("expected loadbalance event detail payload with policy snapshot and V1 summary, got %+v", detailPayload)
	}
}

func TestLoadbalancePartitionProfileScopedEvents(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	otherProfileID := s15InsertProfile(t, harness, "S15 Other Loadbalance")
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1200, ProfileID: profileID, ConnectionID: 1, EventType: "retry_scheduled", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 1, CooldownSeconds: 30.0, ModelID: stringPtr("lb-partition-model"), CreatedAt: fixedS15Now.Add(-2 * time.Minute)})
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1201, ProfileID: otherProfileID, ConnectionID: 1, EventType: "banned", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 2, CooldownSeconds: 60.0, ModelID: stringPtr("lb-partition-model"), CreatedAt: fixedS15Now.Add(-1 * time.Minute)})

	queryContext := s15IssueEventsContext(t, harness, profileID, "24h")
	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&limit=20", http.StatusOK)
	items := listPayload["items"].([]any)
	if len(items) != 1 || asMap(t, items[0])["event_id"] != "1200" {
		t.Fatalf("expected profile-scoped loadbalance event list over partitions, got %+v", listPayload)
	}

	detailPayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/events/1200?query_context="+url.QueryEscape(queryContext), http.StatusOK)
	if detailPayload["event_type"] != "retry_scheduled" || detailPayload["event_id"] != "1200" {
		t.Fatalf("expected loadbalance partition detail for Default profile, got %+v", detailPayload)
	}
}

func TestLoadbalanceEventRetentionJob(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1100, ProfileID: profileID, ConnectionID: 1, EventType: "retry_scheduled", ConsecutiveFailures: 1, CooldownSeconds: 60.0, ModelID: stringPtr("lb-retention-model"), CreatedAt: fixedS15Now.Add(-48 * time.Hour)})
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1101, ProfileID: profileID, ConnectionID: 1, EventType: "retry_scheduled", ConsecutiveFailures: 1, CooldownSeconds: 60.0, ModelID: stringPtr("lb-retention-model"), CreatedAt: fixedS15Now.Add(-30 * time.Minute)})
	refreshS15ActualCoverage(t, harness, "loadbalance_events")

	jobID := createS15LogRetentionJob(t, harness, "loadbalance_events", map[string]any{"cutoff": fixedS15Now.Add(-24 * time.Hour).Format(time.RFC3339)}, "loadbalance-events")
	if jobID == "" || s15CountRows(t, harness, `SELECT COUNT(*) FROM loadbalance_events WHERE profile_id = $1`, profileID) != 2 {
		t.Fatalf("expected loadbalance event retention job to enqueue without inline delete")
	}
}

func TestLoadbalanceEventsPersistPolicySnapshotsFromRuntimeFailure(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Runtime Event Snapshot Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "lb-runtime-snapshot-model", stringPtr("Runtime Snapshot Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Runtime Snapshot Endpoint")
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	strategy, ok, err := loadbalancedomain.LoadRuntimeStrategy(context.Background(), harness.conn, profileID, strategyID)
	if err != nil || !ok {
		t.Fatalf("load runtime strategy for snapshot test: ok=%t err=%v", ok, err)
	}
	var transition loadbalancedomain.RuntimeStateTransition
	for attempt := 0; attempt < 4; attempt++ {
		transition = harness.runtimeService.RuntimeState().RecordRuntimeTransportFailure(profileID, modelConfigID, connectionID, strategy, fixedS15Now.Add(time.Duration(attempt)*time.Second))
	}
	if transition.CurrentState.BanMode != "until_reset" || transition.CurrentState.CumulativeRetryAttempts != 4 {
		t.Fatalf("expected runtime policy evaluation to ban at threshold, got %+v", transition.CurrentState)
	}
	if _, _, err := loadbalancedomain.InsertRuntimeFailureEvent(context.Background(), harness.conn, s15LoadbalancePartitionEnsurer{harness: harness}, profileID, modelConfigID, connectionID, transition, strategy, "connect_error", fixedS15Now.Add(4*time.Second)); err != nil {
		t.Fatalf("insert runtime failure loadbalance event: %v", err)
	}

	var storedCycleLimit, storedBanThreshold int
	if err := harness.conn.QueryRow(context.Background(), `SELECT policy_cycle_retry_attempt_limit, policy_ban_cumulative_retry_attempt_threshold FROM loadbalance_events WHERE profile_id = $1 AND connection_id = $2`, profileID, connectionID).Scan(&storedCycleLimit, &storedBanThreshold); err != nil {
		t.Fatalf("load stored policy snapshots: %v", err)
	}
	if storedCycleLimit != 2 || storedBanThreshold != 4 {
		t.Fatalf("expected stored immutable policy snapshots 2/4, got %d/%d", storedCycleLimit, storedBanThreshold)
	}

	// A custom event window must cover events written at fixedS15Now+4s (the
	// s15 harness freezes the service clock at fixedS15Now).
	contextPayload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/loadbalance/events/query-context", map[string]any{
		"requested_preset": "custom",
		"custom_from_time": fixedS15Now.Add(-1 * time.Hour).Format(time.RFC3339),
		"custom_to_time":   fixedS15Now.Add(1 * time.Hour).Format(time.RFC3339),
	}, http.StatusOK)
	queryContext := fmt.Sprint(contextPayload["query_context"])

	listPath := "/api/loadbalance/events?query_context=" + url.QueryEscape(queryContext) + "&model_id=lb-runtime-snapshot-model&limit=20"
	listPayload := requestS15LoadbalanceEventsUntil(t, harness, profileID, listPath, connectionID, "banned")
	event := s15LoadbalanceEventByConnectionIDAndType(t, listPayload, connectionID, "banned")
	summary := asMap(t, event["summary"])
	params := asMap(t, summary["params"])
	if jsonInt(t, event["policy_cycle_retry_attempt_limit"]) != 2 || jsonInt(t, event["policy_ban_cumulative_retry_attempt_threshold"]) != 4 || jsonInt(t, params["policy_ban_cumulative_retry_attempt_threshold"]) != 4 || params["failure_kind"] != "connect_error" || params["evidence_state"] != "complete" {
		t.Fatalf("expected runtime event list to expose public policy snapshots and V1 summary, got %+v", event)
	}
	if summary["code"] != "loadbalance.banned" {
		t.Fatalf("expected banned V1 summary code, got %+v", summary)
	}

	detailPayload := s15GET[map[string]any](t, harness, profileID, fmt.Sprintf("/api/loadbalance/events/%s?query_context=%s", event["event_id"], url.QueryEscape(queryContext)), http.StatusOK)
	detailSummary := asMap(t, detailPayload["summary"])
	detailParams := asMap(t, detailSummary["params"])
	if jsonInt(t, detailPayload["policy_cycle_retry_attempt_limit"]) != 2 || jsonInt(t, detailPayload["policy_ban_cumulative_retry_attempt_threshold"]) != 4 || detailSummary["code"] != "loadbalance.banned" || detailParams["ban_mode"] != "until_reset" {
		t.Fatalf("expected runtime event detail to expose public policy snapshots and V1 summary, got %+v", detailPayload)
	}
	if detailParams["banned_until_at"] != nil {
		t.Fatalf("expected until_reset ban summary to carry null banned_until_at, got %+v", detailParams)
	}

	currentStatePayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/current-state", http.StatusOK)
	currentState := s15CurrentStateItemByConnectionID(t, currentStatePayload, connectionID)
	if currentState["state"] != "banned" || currentState["ban_mode"] != "until_reset" || currentState["observation_state"] != "observed" {
		t.Fatalf("expected current-state to remain connection-global while banned, got %+v", currentState)
	}
	assertS15NoPolicyThresholdFields(t, currentState)
}

type runtimeStateSeed struct {
	ProfileID, ConnectionID, ConsecutiveFailures int
	LastFailureKind                              *string
	LastCooldownSeconds                          float64
	BanMode                                      string
	BlockedUntilAt                               *time.Time
	LastSuccessResponseHeadersLatencyMS          *int
	CreatedAt, UpdatedAt                         time.Time
}

type loadbalanceEventSeed struct {
	ID                                       int64
	ProfileID, ConnectionID                  int
	EventType                                string
	FailureKind, ModelID                     *string
	EndpointID                               *int
	ConsecutiveFailures                      int
	CooldownSeconds                          float64
	BanMode                                  *string
	PolicyCycleRetryAttemptLimit             *int
	PolicyBanCumulativeRetryAttemptThreshold *int
	BannedUntilAt                            *time.Time
	CreatedAt                                time.Time
}

type s15LoadbalancePartitionEnsurer struct {
	harness *contractHarness
}

func (e s15LoadbalancePartitionEnsurer) EnsurePartitionForTime(ctx context.Context, tableName string, timestamp time.Time) error {
	return ensureContractTestLogPartition(ctx, e.harness, tableName, utcContractPartitionDay(timestamp))
}

func insertRuntimeState(t *testing.T, harness *contractHarness, seed runtimeStateSeed) {
	t.Helper()
	if harness.runtimeService == nil {
		t.Fatal("runtime service is required for local runtime state seeding")
	}
	var modelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT source_model_config_id FROM model_access_targets WHERE target_connection_id = $1 ORDER BY position ASC, id ASC LIMIT 1`, seed.ConnectionID).Scan(&modelConfigID); err != nil {
		t.Fatalf("load model config for connection %d: %v", seed.ConnectionID, err)
	}
	banMode := seed.BanMode
	if strings.TrimSpace(banMode) == "" {
		banMode = "off"
	}
	harness.runtimeService.RuntimeState().SeedConnectionState(seed.ProfileID, modelConfigID, seed.ConnectionID, loadbalancedomain.RuntimeConnectionState{
		ConnectionID:                        seed.ConnectionID,
		BanMode:                             banMode,
		NextRetryAt:                         seed.BlockedUntilAt,
		WindowRequestCount:                  4,
		InFlightNonStream:                   1,
		CycleRetryAttempts:                  seed.ConsecutiveFailures,
		CumulativeRetryAttempts:             seed.ConsecutiveFailures,
		LastRetryDelayMS:                    int(seed.LastCooldownSeconds * 1000),
		LastFailureKind:                     seed.LastFailureKind,
		LastSuccessResponseHeadersLatencyMS: seed.LastSuccessResponseHeadersLatencyMS,
	}, seed.CreatedAt, seed.UpdatedAt)
}

func insertLoadbalanceEvent(t *testing.T, harness *contractHarness, seed loadbalanceEventSeed) {
	t.Helper()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("loadbalance_events", seed.CreatedAt))
	nextRetryAt := (*time.Time)(nil)
	if seed.CooldownSeconds > 0 {
		resolved := seed.CreatedAt.Add(time.Duration(seed.CooldownSeconds * float64(time.Second)))
		nextRetryAt = &resolved
	}
	lastRetryDelayMS := int(seed.CooldownSeconds * 1000)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO loadbalance_events (id, profile_id, connection_id, event_type, failure_kind, cycle_retry_attempts, cumulative_retry_attempts, next_retry_at, last_retry_delay_ms, model_id, endpoint_id, ban_mode, policy_cycle_retry_attempt_limit, policy_ban_cumulative_retry_attempt_threshold, banned_until_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`, seed.ID, seed.ProfileID, seed.ConnectionID, seed.EventType, nullableTestString(seed.FailureKind), seed.ConsecutiveFailures, nullableTestTime(nextRetryAt), lastRetryDelayMS, nullableTestString(seed.ModelID), nullableTestInt(seed.EndpointID), nullableTestString(seed.BanMode), nullableTestInt(seed.PolicyCycleRetryAttemptLimit), nullableTestInt(seed.PolicyBanCumulativeRetryAttemptThreshold), nullableTestTime(seed.BannedUntilAt), seed.CreatedAt); err != nil {
		t.Fatalf("insert loadbalance event %d: %v", seed.ID, err)
	}
}

func s15CurrentStateItemByConnectionID(t *testing.T, payload map[string]any, connectionID int) map[string]any {
	t.Helper()
	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("expected current-state items array, got %+v", payload)
	}
	for _, raw := range items {
		item := asMap(t, raw)
		target := asMap(t, item["terminal_target"])
		if jsonInt(t, target["id"]) == connectionID {
			return item
		}
	}
	t.Fatalf("expected current-state payload for connection %d, got %+v", connectionID, payload)
	return nil
}

func s15IssueEventsContext(t *testing.T, harness *contractHarness, profileID int, preset string) string {
	t.Helper()
	payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/loadbalance/events/query-context", map[string]any{"requested_preset": preset}, http.StatusOK)
	token, ok := payload["query_context"].(string)
	if !ok || token == "" {
		t.Fatalf("expected issued events query context, got %+v", payload)
	}
	return token
}

func s15LoadbalanceEventByConnectionID(t *testing.T, payload map[string]any, connectionID int) map[string]any {
	t.Helper()
	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("expected loadbalance event items array, got %+v", payload)
	}
	for _, raw := range items {
		item := asMap(t, raw)
		target := asMap(t, item["terminal_target"])
		if jsonInt(t, target["id"]) == connectionID {
			return item
		}
	}
	t.Fatalf("expected loadbalance event for connection %d, got %+v", connectionID, payload)
	return nil
}

func s15LoadbalanceEventByConnectionIDAndType(t *testing.T, payload map[string]any, connectionID int, eventType string) map[string]any {
	t.Helper()
	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("expected loadbalance event items array, got %+v", payload)
	}
	for _, raw := range items {
		item := asMap(t, raw)
		target := asMap(t, item["terminal_target"])
		if jsonInt(t, target["id"]) == connectionID && item["event_type"] == eventType {
			return item
		}
	}
	t.Fatalf("expected loadbalance event for connection %d with type %q, got %+v", connectionID, eventType, payload)
	return nil
}

func requestS15LoadbalanceEventsUntil(t *testing.T, harness *contractHarness, profileID int, path string, connectionID int, eventType string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var payload map[string]any
	for {
		payload = s15GET[map[string]any](t, harness, profileID, path, http.StatusOK)
		if s15PayloadHasLoadbalanceEvent(payload, connectionID, eventType) {
			return payload
		}
		if time.Now().After(deadline) {
			return payload
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func s15PayloadHasLoadbalanceEvent(payload map[string]any, connectionID int, eventType string) bool {
	items, ok := payload["items"].([]any)
	if !ok {
		return false
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		target, ok := item["terminal_target"].(map[string]any)
		if !ok || intFromJSONNumber(target["id"]) != connectionID {
			continue
		}
		if eventType == "" || item["event_type"] == eventType {
			return true
		}
	}
	return false
}

func TestLoadbalanceEventsBidirectionalKeyset(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := fixedS15Now.Add(-time.Minute)
	for index := 0; index < 5; index++ {
		insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{
			ID: int64(9800 + index), ProfileID: profileID, ConnectionID: 1,
			EventType: "retry_scheduled", FailureKind: stringPtr("timeout"),
			ConsecutiveFailures: 1, CooldownSeconds: 60.0,
			ModelID: stringPtr("keyset-model"), EndpointID: intPtr(12), BanMode: stringPtr("off"),
			CreatedAt: now.Add(-time.Duration(index+1) * time.Minute),
		})
	}
	contextPayload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/loadbalance/events/query-context", map[string]any{"requested_preset": "24h"}, http.StatusOK)
	queryContext := url.QueryEscape(fmt.Sprint(contextPayload["query_context"]))
	base := "/api/loadbalance/events?query_context=" + queryContext + "&model_id=keyset-model&sort_order=desc&limit=2"

	// The current registered event surface uses a signed forward cursor and
	// exposes event IDs as decimal strings.
	first := s15GET[map[string]any](t, harness, profileID, base, http.StatusOK)
	firstItems := first["items"].([]any)
	if len(firstItems) != 2 || asMap(t, firstItems[0])["event_id"] != "9800" || asMap(t, firstItems[1])["event_id"] != "9801" {
		t.Fatalf("expected newest-first page 9800/9801, got %+v", first)
	}
	if first["has_more"] != true || first["next_cursor"] == nil {
		t.Fatalf("expected a signed next cursor on the first page, got %+v", first)
	}

	second := s15GET[map[string]any](t, harness, profileID, base+"&cursor="+url.QueryEscape(fmt.Sprint(first["next_cursor"])), http.StatusOK)
	secondItems := second["items"].([]any)
	if len(secondItems) != 2 || asMap(t, secondItems[0])["event_id"] != "9802" || asMap(t, secondItems[1])["event_id"] != "9803" {
		t.Fatalf("expected cursor page 9802/9803, got %+v", second)
	}

	third := s15GET[map[string]any](t, harness, profileID, base+"&cursor="+url.QueryEscape(fmt.Sprint(second["next_cursor"])), http.StatusOK)
	thirdItems := third["items"].([]any)
	if len(thirdItems) != 1 || asMap(t, thirdItems[0])["event_id"] != "9804" || third["has_more"] != false {
		t.Fatalf("expected final cursor page 9804, got %+v", third)
	}
}
