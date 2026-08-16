package contracttest

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestCostSegmentsCatalogue verifies the canonical cost-segments catalogue:
// server-generated keys (e.N / l.AAA / l.__unknown__), coverage states, and
// bounded pagination.
func TestCostSegmentsCatalogue(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := time.Now().UTC()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("usage_request_events", now))

	// Segment e.2: 1 priced trusted cost, then 2 unpriced rows whose symbols
	// are $, US$, $ so full-symbol ordering must deduplicate the later repeat.
	for index, symbol := range []string{"$", "US$", "$"} {
		status := "priced"
		reason := "NULL"
		cost := int64(2500)
		if index > 0 {
			status = "unpriced"
			reason = "'PRICING_DISABLED'"
			cost = 0
		}
		var costArg any
		if cost > 0 {
			costArg = cost
		}
		query := fmt.Sprintf(`INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, attempt_count, request_path, pricing_status, pricing_evidence_trust, unpriced_reason, report_currency_code, report_currency_symbol, reporting_currency_epoch, input_cost_micros, output_cost_micros, reasoning_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, created_at, proxy_api_key_attribution_state)
			VALUES ($1, 'seg-e2-%d', 'seg-model', 'openai', 'Seg', 200, TRUE, 1, '/v1/chat/completions', '%s', 'trusted', %s, 'USD', $5, 2, $4, $4, $4, $4, $4, $2, $2, $3, 'none')`, index, status, reason)
		if _, err := harness.conn.Exec(context.Background(), query, profileID, costArg, now.Add(time.Duration(index)*time.Minute), costArg, symbol); err != nil {
			t.Fatalf("seed e.2 segment row %d: %v", index, err)
		}
	}
	// Legacy segment l.EUR: unknown trust, canonical cost null.
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, attempt_count, request_path, pricing_status, pricing_evidence_trust, report_currency_code, report_currency_symbol, created_at, proxy_api_key_attribution_state)
		VALUES ($1, 'seg-leur-1', 'seg-model', 'openai', 'Seg', 200, TRUE, 1, '/v1/chat/completions', 'unknown', 'legacy_untrusted', 'EUR', '€', $2, 'none')`,
		profileID, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("seed l.EUR segment row: %v", err)
	}
	// Legacy l.__unknown__ row.
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, attempt_count, request_path, pricing_status, pricing_evidence_trust, created_at, proxy_api_key_attribution_state)
		VALUES ($1, 'seg-unk-1', 'seg-model', 'openai', 'Seg', 200, TRUE, 1, '/v1/chat/completions', 'unknown', 'legacy_untrusted', $2, 'none')`,
		profileID, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("seed l.__unknown__ segment row: %v", err)
	}

	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/cost-segments?limit=50", http.StatusOK)
	segments := payload["cost_segments"].([]any)
	if len(segments) != 3 {
		t.Fatalf("expected 3 cost segments, got %d", len(segments))
	}
	first := asMap(t, segments[0])
	if first["segment_key"] != "e.2" {
		t.Fatalf("expected identified epoch segment first, got %v", first["segment_key"])
	}
	if first["priced_request_count"] != float64(1) || first["unpriced_request_count"] != float64(2) {
		t.Fatalf("expected e.2 counts priced=1 unpriced=2, got %+v", first)
	}
	if first["pricing_coverage_state"] != "partial" {
		t.Fatalf("expected partial coverage for e.2, got %v", first["pricing_coverage_state"])
	}
	if first["known_cost_micros"] != "2500" {
		t.Fatalf("expected trusted known cost 2500, got %v", first["known_cost_micros"])
	}
	if first["request_count"] != float64(3) || first["display_symbol"] != "$" {
		t.Fatalf("expected one aggregate e.2 segment with latest symbol, got %+v", first)
	}
	observedSymbols := first["observed_symbols"].([]any)
	if len(observedSymbols) != 2 || observedSymbols[0] != "$" || observedSymbols[1] != "US$" {
		t.Fatalf("expected deterministic observed symbols [$ US$], got %+v", observedSymbols)
	}
	if first["observed_symbol_count"] != float64(2) || first["observed_symbols_truncated"] != false {
		t.Fatalf("expected complete observed-symbol metadata, got %+v", first)
	}
	second := asMap(t, segments[1])
	if second["segment_key"] != "l.EUR" {
		t.Fatalf("expected legacy EUR segment second, got %v", second["segment_key"])
	}
	if second["known_cost_micros"] != nil {
		t.Fatalf("expected null known cost for legacy untrusted segment, got %v", second["known_cost_micros"])
	}
	third := asMap(t, segments[2])
	if third["segment_key"] != "l.__unknown__" {
		t.Fatalf("expected unknown segment last, got %v", third["segment_key"])
	}
	if third["known_cost_micros"] != nil {
		t.Fatalf("expected null known cost for unknown segment, got %v", third["known_cost_micros"])
	}
	if jsonInt(t, payload["cost_segments_total_count"]) != 3 || payload["cost_segments_next_cursor"] != nil {
		t.Fatalf("expected complete page metadata, got %+v", payload)
	}

	for _, test := range []struct {
		name, path, key      string
		symbols              []string
		total, limit, offset int
	}{
		{"first page", "/api/stats/cost-segments/e.2/symbols?limit=1", "e.2", []string{"$"}, 2, 1, 0},
		{"second page", "/api/stats/cost-segments/e.2/symbols?limit=1&offset=1", "e.2", []string{"US$"}, 2, 1, 1},
		{"out of range", "/api/stats/cost-segments/e.2/symbols?limit=1&offset=2", "e.2", []string{}, 2, 1, 2},
		{"legacy", "/api/stats/cost-segments/l.EUR/symbols", "l.EUR", []string{"€"}, 1, 50, 0},
		{"symbol-less", "/api/stats/cost-segments/l.__unknown__/symbols", "l.__unknown__", []string{}, 0, 50, 0},
		{"limit cap", "/api/stats/cost-segments/e.2/symbols?limit=999", "e.2", []string{"$", "US$"}, 2, 100, 0},
	} {
		t.Run("symbols "+test.name, func(t *testing.T) {
			page := s15GET[map[string]any](t, harness, profileID, test.path, http.StatusOK)
			gotSymbols := page["symbols"].([]any)
			if len(page) != 5 || page["segment_key"] != test.key || jsonInt(t, page["total"]) != test.total || jsonInt(t, page["limit"]) != test.limit || jsonInt(t, page["offset"]) != test.offset || len(gotSymbols) != len(test.symbols) {
				t.Fatalf("unexpected symbol page: %+v", page)
			}
			for index, symbol := range test.symbols {
				if gotSymbols[index] != symbol {
					t.Fatalf("expected symbols %+v, got %+v", test.symbols, gotSymbols)
				}
			}
		})
	}
	for _, path := range []string{"/api/stats/cost-segments/e.2/symbols?limit=0", "/api/stats/cost-segments/e.2/symbols?offset=-1"} {
		_ = s15GET[map[string]any](t, harness, profileID, path, http.StatusBadRequest)
	}
	missing := s15GET[map[string]any](t, harness, profileID, "/api/stats/cost-segments/e.999/symbols", http.StatusNotFound)
	assertErrorCode(t, missing, "cost_segment_not_found")

	expectedKeys := []string{"e.2", "l.EUR", "l.__unknown__"}
	cursor := ""
	seenKeys := make(map[string]struct{}, len(expectedKeys))
	snapshotHash := ""
	for index, expectedKey := range expectedKeys {
		path := "/api/stats/cost-segments?limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		page := s15GET[map[string]any](t, harness, profileID, path, http.StatusOK)
		pageSegments := page["cost_segments"].([]any)
		if len(pageSegments) != 1 || asMap(t, pageSegments[0])["segment_key"] != expectedKey {
			t.Fatalf("expected page %d key %s, got %+v", index+1, expectedKey, pageSegments)
		}
		if _, duplicate := seenKeys[expectedKey]; duplicate {
			t.Fatalf("cost segment key %s repeated across pages", expectedKey)
		}
		seenKeys[expectedKey] = struct{}{}
		if jsonInt(t, page["cost_segments_total_count"]) != len(expectedKeys) || jsonInt(t, page["cost_segments_consumed_count"]) != index+1 {
			t.Fatalf("expected page %d total=%d consumed=%d, got %+v", index+1, len(expectedKeys), index+1, page)
		}
		pageSnapshotHash, ok := page["cost_segments_snapshot_hash"].(string)
		if !ok || pageSnapshotHash == "" {
			t.Fatalf("expected page %d to carry a snapshot hash, got %+v", index+1, page)
		}
		if snapshotHash == "" {
			snapshotHash = pageSnapshotHash
		} else if pageSnapshotHash != snapshotHash {
			t.Fatalf("expected stable snapshot hash %s, got %s", snapshotHash, pageSnapshotHash)
		}
		nextCursor, hasNext := page["cost_segments_next_cursor"].(string)
		if index < len(expectedKeys)-1 {
			if !hasNext || nextCursor == "" {
				t.Fatalf("expected page %d to carry a next cursor, got %+v", index+1, page)
			}
			cursor = nextCursor
			continue
		}
		if page["cost_segments_next_cursor"] != nil {
			t.Fatalf("expected final page to terminate pagination, got %+v", page)
		}
	}

	parts := strings.Split(cursor, ".")
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode cursor payload: %v", err)
	}
	publicDigest := sha256.Sum256(raw)
	forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString(publicDigest[:])
	rejected := s15GET[map[string]any](t, harness, profileID, "/api/stats/cost-segments?limit=1&cursor="+url.QueryEscape(forged), http.StatusBadRequest)
	assertErrorCode(t, rejected, "cost_segment_cursor_invalid")
}
