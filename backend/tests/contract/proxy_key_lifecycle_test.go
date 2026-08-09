package contracttest

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

// TestProxyKeyCapacitySemantics verifies the authoritative capacity predicate:
// inactive-but-unexpired rows count, expired rows do not, and the snapshot
// comes from the server in every mutation response.
func TestProxyKeyCapacitySemantics(t *testing.T) {
	harness := newContractHarness(t)
	loginWithVerifiedAuth(t, harness, "capacity-admin", "capacity-password-123", "capacity@example.com")

	create := func(name string) map[string]any {
		t.Helper()
		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/settings/auth/proxy-keys", map[string]any{"name": name}, nil)
		assertStatus(t, response, http.StatusCreated)
		var payload map[string]any
		decodeJSONResponse(t, response, &payload)
		return payload
	}

	first := create("capacity-one")
	assertProxyKeyCapacityPayload(t, decodeCapacityFromPayload(t, first), 1, 100)

	// Inactive-but-unexpired rows still occupy capacity.
	firstItem := first["item"].(map[string]any)
	retireResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/settings/auth/proxy-keys/%d", int(firstItem["id"].(float64))), map[string]any{"name": "capacity-one", "is_active": false}, nil)
	assertStatus(t, retireResponse, http.StatusOK)
	assertProxyKeyCapacityPayload(t, decodeCapacityField(t, retireResponse, "capacity"), 1, 100)

	// Expiring a row releases capacity.
	if _, err := harness.conn.Exec(context.Background(), `UPDATE proxy_api_keys SET expires_at = $2, updated_at = $2 WHERE id = $1`, int(firstItem["id"].(float64)), time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatalf("expire proxy key: %v", err)
	}
	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth/proxy-keys", nil, nil)
	assertStatus(t, listResponse, http.StatusOK)
	var listed struct {
		Items    []map[string]any        `json:"items"`
		Capacity proxyKeyCapacityPayload `json:"capacity"`
	}
	decodeJSONResponse(t, listResponse, &listed)
	if len(listed.Items) != 1 {
		t.Fatalf("expected expired row to remain visible in the ledger, got %d items", len(listed.Items))
	}
	assertProxyKeyCapacityPayload(t, listed.Capacity, 0, 100)

	// Delete releases capacity.
	second := create("capacity-two")
	secondItem := second["item"].(map[string]any)
	deleteResponse := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/settings/auth/proxy-keys/%d", int(secondItem["id"].(float64))), nil, nil)
	assertStatus(t, deleteResponse, http.StatusOK)
	assertProxyKeyCapacityPayload(t, decodeCapacityField(t, deleteResponse, "capacity"), 0, 100)
}

// TestProxyKeyAtomicCapacityConcurrency verifies concurrent creates at the
// limit commit at most one and the final used never exceeds the limit.
func TestProxyKeyAtomicCapacityConcurrency(t *testing.T) {
	harness := newContractHarness(t)
	loginWithVerifiedAuth(t, harness, "concurrent-admin", "concurrent-password-123", "concurrent@example.com")

	// Fill to 99.
	for index := 0; index < 99; index++ {
		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/settings/auth/proxy-keys", map[string]any{"name": fmt.Sprintf("fill-%d", index)}, nil)
		assertStatus(t, response, http.StatusCreated)
	}
	fillList := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth/proxy-keys", nil, nil)
	assertStatus(t, fillList, http.StatusOK)
	var filled struct {
		Capacity proxyKeyCapacityPayload `json:"capacity"`
	}
	decodeJSONResponse(t, fillList, &filled)
	assertProxyKeyCapacityPayload(t, filled.Capacity, 99, 100)

	// 2 concurrent creates below the M2 management admission budget:
	// exactly one must succeed. Without the capacity serialization lock both
	// would commit and push used past the limit.
	const workers = 2
	type createOutcome struct {
		status int
		detail string
	}
	results := make(chan createOutcome, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/settings/auth/proxy-keys", map[string]any{"name": fmt.Sprintf("race-%d", worker)}, nil)
			outcome := createOutcome{status: response.StatusCode}
			if response.StatusCode != http.StatusCreated {
				var payload map[string]any
				decodeJSONResponse(t, response, &payload)
				outcome.detail = fmt.Sprintf("%v", payload)
			}
			results <- outcome
		}(index)
	}
	wait.Wait()
	close(results)
	created := 0
	conflicts := 0
	for outcome := range results {
		switch outcome.status {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected concurrent create status %d (detail %q)", outcome.status, outcome.detail)
		}
	}
	if created != 1 || conflicts != workers-1 {
		t.Fatalf("expected exactly one concurrent create to commit at the limit, got created=%d conflicts=%d", created, conflicts)
	}

	finalList := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/auth/proxy-keys", nil, nil)
	assertStatus(t, finalList, http.StatusOK)
	var final struct {
		Capacity proxyKeyCapacityPayload `json:"capacity"`
	}
	decodeJSONResponse(t, finalList, &final)
	assertProxyKeyCapacityPayload(t, final.Capacity, 100, 100)

	// Typed capacity error on one more create.
	exhausted := harness.requestJSON(t, harness.client, http.MethodPost, "/api/settings/auth/proxy-keys", map[string]any{"name": "overflow"}, nil)
	assertErrorResponseCode(t, exhausted, http.StatusConflict, "proxy_key_capacity_exhausted", "Maximum 100 proxy API keys reached")
}

// TestProxyKeyRotateAtFullCapacityIsNetNeutral verifies rotation at the limit
// succeeds atomically (predecessor becomes non-counting in the same
// transaction as successor creation).
func TestProxyKeyRotateAtFullCapacityIsNetNeutral(t *testing.T) {
	harness := newContractHarness(t)
	loginWithVerifiedAuth(t, harness, "rotate-full-admin", "rotate-full-password-123", "rotate-full@example.com")

	var keyID int
	for index := 0; index < 100; index++ {
		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/settings/auth/proxy-keys", map[string]any{"name": fmt.Sprintf("rotate-fill-%d", index)}, nil)
		assertStatus(t, response, http.StatusCreated)
		if index == 0 {
			var payload map[string]any
			decodeJSONResponse(t, response, &payload)
			keyID = int(payload["item"].(map[string]any)["id"].(float64))
		}
	}

	rotateResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/settings/auth/proxy-keys/%d/rotate", keyID), nil, nil)
	assertStatus(t, rotateResponse, http.StatusOK)
	assertProxyKeyCapacityPayload(t, decodeCapacityField(t, rotateResponse, "capacity"), 100, 100)
	if rotateResponse.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("expected rotate response to carry private, no-store header, got %q", rotateResponse.Header.Get("Cache-Control"))
	}

	// Predecessor is inactive and expired; successor is active and inherits.
	predecessor := loadProxyKeyByID(t, harness, keyID)
	if predecessor.IsActive {
		t.Fatal("expected rotated predecessor to be inactive")
	}
	if predecessor.ExpiresAt == nil {
		t.Fatal("expected rotated predecessor to be expired at rotation time")
	}
}

// TestProxyKeyImmutableHistoryAfterDeleteRenameRotate verifies request/usage
// snapshot identity is never rewritten by lifecycle mutations.
func TestProxyKeyImmutableHistoryAfterDeleteRenameRotate(t *testing.T) {
	harness := newContractHarness(t)
	loginWithVerifiedAuth(t, harness, "history-admin", "history-password-123", "history@example.com")

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/settings/auth/proxy-keys", map[string]any{"name": "history-key"}, nil)
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	item := created["item"].(map[string]any)
	keyID := int(item["id"].(float64))

	// Insert retained request/usage rows referencing the snapshot identity.
	now := time.Now().UTC()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", now))
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("usage_request_events", now))
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, api_family, status_code, response_time_ms, is_stream, success_flag, request_path, created_at, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot, proxy_api_key_attribution_state, proxy_api_key_auth_enforced_at_request, ingress_request_id, attempt_number) VALUES (1, $1, 'model-a', 'openai', 200, 10, FALSE, TRUE, '/v1/chat/completions', $2, $3, 'history-key', 'identified', TRUE, 'ingress-hist-1', 1)`, modelLoadDefaultProfileID(t, harness), now, keyID); err != nil {
		t.Fatalf("seed historical request log: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, api_family, status_code, success_flag, attempt_count, request_path, created_at, endpoint_label_snapshot, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot, proxy_api_key_attribution_state, proxy_api_key_auth_enforced_at_request) VALUES (1, $1, 'ingress-hist-1', 'model-a', 'openai', 200, TRUE, 1, '/v1/chat/completions', $2, 'ep', $3, 'history-key', 'identified', TRUE)`, modelLoadDefaultProfileID(t, harness), now, keyID); err != nil {
		t.Fatalf("seed historical usage event: %v", err)
	}

	// Rename the key: history name snapshot must stay unchanged.
	renameResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/settings/auth/proxy-keys/%d", keyID), map[string]any{"name": "history-key-renamed"}, nil)
	assertStatus(t, renameResponse, http.StatusOK)
	assertHistoricalSnapshotIdentity(t, harness, keyID, "history-key")

	// Rotate: predecessor identity must stay unchanged in history.
	rotateResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/settings/auth/proxy-keys/%d/rotate", keyID), nil, nil)
	assertStatus(t, rotateResponse, http.StatusOK)
	assertHistoricalSnapshotIdentity(t, harness, keyID, "history-key")

	// Delete the key row: retained snapshots survive without the management row.
	deleteResponse := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/settings/auth/proxy-keys/%d", keyID), nil, nil)
	assertStatus(t, deleteResponse, http.StatusOK)
	assertHistoricalSnapshotIdentity(t, harness, keyID, "history-key")

	var managementRowCount int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM proxy_api_keys WHERE id = $1`, keyID).Scan(&managementRowCount); err != nil {
		t.Fatalf("count deleted proxy key row: %v", err)
	}
	if managementRowCount != 0 {
		t.Fatalf("expected proxy key management row to be deleted, found %d", managementRowCount)
	}
}

func assertHistoricalSnapshotIdentity(t *testing.T, harness *contractHarness, keyID int, wantName string) {
	t.Helper()
	var requestID sql.NullInt64
	var requestName sql.NullString
	if err := harness.conn.QueryRow(context.Background(), `SELECT proxy_api_key_id_snapshot, proxy_api_key_name_snapshot FROM request_logs WHERE ingress_request_id = 'ingress-hist-1'`).Scan(&requestID, &requestName); err != nil {
		t.Fatalf("load historical request snapshot: %v", err)
	}
	if !requestID.Valid || int(requestID.Int64) != keyID || !requestName.Valid || requestName.String != wantName {
		t.Fatalf("request snapshot identity changed: id=%v name=%v want id=%d name=%q", requestID, requestName, keyID, wantName)
	}
	var usageID sql.NullInt64
	var usageName sql.NullString
	if err := harness.conn.QueryRow(context.Background(), `SELECT proxy_api_key_id_snapshot, proxy_api_key_name_snapshot FROM usage_request_events WHERE ingress_request_id = 'ingress-hist-1'`).Scan(&usageID, &usageName); err != nil {
		t.Fatalf("load historical usage snapshot: %v", err)
	}
	if !usageID.Valid || int(usageID.Int64) != keyID || !usageName.Valid || usageName.String != wantName {
		t.Fatalf("usage snapshot identity changed: id=%v name=%v want id=%d name=%q", usageID, usageName, keyID, wantName)
	}
}

func decodeCapacityFromPayload(t *testing.T, payload map[string]any) proxyKeyCapacityPayload {
	t.Helper()
	raw, ok := payload["capacity"].(map[string]any)
	if !ok {
		t.Fatalf("expected capacity in payload, got %+v", payload)
	}
	return proxyKeyCapacityPayload{
		Limit:     int(raw["limit"].(float64)),
		Used:      int(raw["used"].(float64)),
		Remaining: int(raw["remaining"].(float64)),
		CountedAt: raw["counted_at"].(string),
	}
}

func loadProxyKeyByID(t *testing.T, harness *contractHarness, keyID int) proxyKeySnapshot {
	t.Helper()
	snapshots := loadProxyKeys(t, harness)
	for _, snapshot := range snapshots {
		if snapshot.ID == keyID {
			return snapshot
		}
	}
	t.Fatalf("proxy key %d not found in %+v", keyID, snapshots)
	return proxyKeySnapshot{}
}
