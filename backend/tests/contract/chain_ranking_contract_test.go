package contracttest

import (
	"context"
	"encoding/csv"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestChainRankingRetainedCohortAndCSV(t *testing.T) {
	h := newS15ContractHarness(t)
	profile := modelLoadDefaultProfileID(t, h)
	at := fixedS15Now.Add(-5 * time.Minute)
	ensureContractTestLogPartitions(t, h, contractTestLogPartitionFor("request_logs", at), contractTestLogPartitionFor("usage_request_events", at))
	for _, id := range []string{"rank-a", "rank-b", "rank-c", "rank-orphan", "rank-no-clock", "rank-eur", "rank-no-currency"} {
		seedChainIngress(t, h, profile, id, at, 200, 2, true, "not_streaming")
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := h.conn.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE usage_request_events SET ingress_completed_at=ingress_started_at+ interval '10 seconds' WHERE profile_id=$1`, profile)
	exec(`UPDATE usage_request_events SET ingress_completed_at=ingress_started_at+ interval '2 seconds' WHERE profile_id=$1 AND ingress_request_id='rank-c'`, profile)
	exec(`UPDATE usage_request_events SET ingress_started_at=NULL,ingress_completed_at=NULL WHERE profile_id=$1 AND ingress_request_id='rank-no-clock'`, profile)
	exec(`DELETE FROM usage_request_events WHERE profile_id=$1 AND ingress_request_id='rank-orphan'`, profile)
	for _, id := range []string{"rank-a", "rank-b", "rank-c", "rank-eur", "rank-no-currency"} {
		code := "USD"
		if id == "rank-eur" {
			code = "EUR"
		}
		if id == "rank-no-currency" {
			code = ""
		}
		amount := int64(100)
		if id == "rank-c" {
			amount = 0
		}
		if id == "rank-eur" {
			amount = 9999
		}
		exec(`UPDATE usage_request_events SET pricing_status='priced',pricing_evidence_trust='trusted', report_currency_code=NULLIF($3,''), input_cost_micros=0,output_cost_micros=0,reasoning_cost_micros=0,cache_read_input_cost_micros=0,cache_creation_input_cost_micros=0,total_cost_original_micros=$4,total_cost_user_currency_micros=$4 WHERE profile_id=$1 AND ingress_request_id=$2`, profile, id, code, amount)
	}
	for _, tc := range []struct {
		metric, order string
		want          []string
	}{
		{"elapsed_ms", "desc", []string{"rank-no-currency", "rank-eur", "rank-b", "rank-a", "rank-c", "rank-orphan", "rank-no-clock"}},
		{"elapsed_ms", "asc", []string{"rank-c", "rank-a", "rank-b", "rank-eur", "rank-no-currency", "rank-no-clock", "rank-orphan"}},
		{"total_cost_user_currency_micros", "desc", []string{"rank-eur", "rank-b", "rank-a", "rank-c", "rank-orphan", "rank-no-currency", "rank-no-clock"}},
	} {
		t.Run(tc.metric+tc.order, func(t *testing.T) {
			query := url.Values{"view": {"ingress_chains"}, "sort_by": {tc.metric}, "sort_order": {tc.order}, "chain_limit": {"1"}, "from_time": {at.Add(-time.Hour).Format(time.RFC3339)}, "to_time": {at.Add(time.Hour).Format(time.RFC3339)}}
			got := []string{}
			for page := 0; page < len(tc.want); page++ {
				payload := s15GET[map[string]any](t, h, profile, "/api/stats/requests?"+query.Encode(), http.StatusOK)
				if jsonInt(t, payload["retained_ingress_total"]) != 7 || jsonInt(t, payload["retained_request_log_row_total"]) != 14 {
					t.Fatalf("cohort: %+v", payload)
				}
				coverage := asMap(t, payload["ranking"])
				wantRankable, wantMissing := 5, 2
				if tc.metric == "total_cost_user_currency_micros" {
					wantRankable, wantMissing = 4, 3
				}
				if jsonInt(t, coverage["rankable_ingress_count"]) != wantRankable || jsonInt(t, coverage["unrankable_ingress_count"]) != wantMissing {
					t.Fatalf("whole-cohort ranking counts: %+v", coverage)
				}
				reasons := asMap(t, coverage["unrankable_reasons"])
				if jsonInt(t, reasons["missing_finalized"]) != 1 {
					t.Fatalf("orphan count: %+v", reasons)
				}
				if tc.metric == "elapsed_ms" && jsonInt(t, reasons["missing_elapsed"]) != 1 {
					t.Fatalf("missing clock count: %+v", reasons)
				}
				if tc.metric == "total_cost_user_currency_micros" && (jsonInt(t, reasons["unknown_currency"]) != 1 || jsonInt(t, reasons["untrusted_cost"]) != 1) {
					t.Fatalf("cost reason counts: %+v", reasons)
				}
				items := payload["items"].([]any)
				if len(items) != 1 {
					t.Fatalf("page %d: %+v", page, items)
				}
				item := asMap(t, items[0])
				got = append(got, item["ingress_request_id"].(string))
				if len(item["retained_rows"].([]any)) != 2 {
					t.Fatal("rank split a retry chain")
				}
				ranking := asMap(t, item["ranking"])
				if ranking["metric"] != tc.metric {
					t.Fatalf("missing metric: %+v", ranking)
				}
				id := item["ingress_request_id"].(string)
				if id == "rank-orphan" && (ranking["state"] != "missing_finalized" || ranking["value"] != nil) {
					t.Fatalf("orphan must not fabricate metric: %+v", ranking)
				}
				if tc.metric == "total_cost_user_currency_micros" {
					if id == "rank-no-currency" && ranking["state"] != "unknown_currency" {
						t.Fatalf("currency evidence: %+v", ranking)
					}
					if id == "rank-c" && (ranking["state"] != "ranked" || jsonInt(t, ranking["value"]) != 0) {
						t.Fatalf("trusted zero lost: %+v", ranking)
					}
					if id == "rank-a" && jsonInt(t, ranking["value"]) != 100 {
						t.Fatalf("parent/attempt cost added: %+v", ranking)
					}
				}
				cursor, _ := payload["next_chain_cursor"].(string)
				if page == len(tc.want)-1 && cursor != "" {
					t.Fatal("extra cursor")
				}
				if cursor != "" {
					query.Set("chain_cursor", cursor)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("want %v got %v", tc.want, got)
			}
			query.Del("chain_cursor")
			query.Del("chain_limit")
			resp := h.requestJSONRaw(t, h.client, http.MethodGet, "/api/stats/requests/export?"+query.Encode(), "", modelHeader(profile))
			body, err := ioReadAll(resp)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 200 {
				t.Fatalf("CSV %d: %s", resp.StatusCode, body)
			}
			records, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != 15 {
				t.Fatalf("CSV cohort rows: %d", len(records))
			}
			exportIDs := []string{}
			for i := 1; i < len(records); i += 2 {
				exportIDs = append(exportIDs, records[i][2])
				if records[i][2] != records[i+1][2] {
					t.Fatal("CSV split chain")
				}
			}
			if !reflect.DeepEqual(exportIDs, tc.want) {
				t.Fatalf("CSV order want %v got %v", tc.want, exportIDs)
			}
		})
	}
}

func TestChainRankingWindowBoundaryAndCursorBinding(t *testing.T) {
	h := newS15ContractHarness(t)
	profile := modelLoadDefaultProfileID(t, h)
	at := fixedS15Now.Add(-10 * time.Minute)
	ensureContractTestLogPartitions(t, h, contractTestLogPartitionFor("request_logs", at), contractTestLogPartitionFor("usage_request_events", at))
	for _, id := range []string{"edge-a", "edge-b"} {
		seedChainIngress(t, h, profile, id, at, 200, 2, true, "not_streaming")
	}
	if _, err := h.conn.Exec(context.Background(), `UPDATE usage_request_events SET created_at=$2,ingress_completed_at=$2 WHERE profile_id=$1`, profile, at.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	q := url.Values{"view": {"ingress_chains"}, "sort_by": {"elapsed_ms"}, "chain_limit": {"1"}, "from_time": {at.Format(time.RFC3339)}, "to_time": {at.Add(3 * time.Minute).Format(time.RFC3339)}}
	first := s15GET[map[string]any](t, h, profile, "/api/stats/requests?"+q.Encode(), 200)
	if jsonInt(t, first["retained_ingress_total"]) != 2 || len(first["items"].([]any)) != 1 {
		t.Fatalf("late finalization disappeared: %+v", first)
	}
	q.Set("chain_cursor", first["next_chain_cursor"].(string))
	q.Set("sort_by", "total_cost_user_currency_micros")
	s15GET[map[string]any](t, h, profile, "/api/stats/requests?"+q.Encode(), 422)
	q.Del("chain_cursor")
	q.Set("sort_by", "elapsed_ms")
	q.Set("ingress_request_id", "edge-a")
	detail := s15GET[map[string]any](t, h, profile, "/api/stats/requests?"+q.Encode(), 200)
	item := asMap(t, detail["items"].([]any)[0])
	if jsonInt(t, item["elapsed_ms"]) != 300000 || len(item["retained_rows"].([]any)) != 2 {
		t.Fatalf("detail mismatch: %+v", item)
	}
	q.Del("ingress_request_id")
	q.Del("chain_limit")
	resp := h.requestJSONRaw(t, h.client, http.MethodGet, "/api/stats/requests/export?"+q.Encode(), "", modelHeader(profile))
	body, err := ioReadAll(resp)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("CSV status %d: %s", resp.StatusCode, body)
	}
	records, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
	if err != nil || len(records) != 5 {
		t.Fatalf("boundary CSV mismatch: %d %v", len(records), err)
	}
}

func TestChainRankingNewestFinalizedEvidenceAndEmptyCounts(t *testing.T) {
	h := newS15ContractHarness(t)
	profile := modelLoadDefaultProfileID(t, h)
	at := fixedS15Now.Add(-5 * time.Minute)
	ensureContractTestLogPartitions(t, h, contractTestLogPartitionFor("request_logs", at), contractTestLogPartitionFor("usage_request_events", at))
	seedChainIngress(t, h, profile, "latest-rank", at, 200, 2, true, "not_streaming")
	if _, err := h.conn.Exec(context.Background(), `INSERT INTO usage_request_events (profile_id,ingress_request_id,model_id,api_family,endpoint_label_snapshot,status_code,success_flag,attempt_count,request_path,pricing_status,pricing_evidence_trust,stream_outcome,created_at,ingress_started_at,ingress_completed_at,proxy_api_key_attribution_state)
 VALUES ($1,'latest-rank','chain-model','openai','Chain Endpoint',200,TRUE,2,'/v1/chat/completions','ineligible','trusted','not_streaming',$2,$2,$2::timestamptz+interval '30 seconds','none')`, profile, at); err != nil {
		t.Fatal(err)
	}
	q := url.Values{"view": {"ingress_chains"}, "sort_by": {"elapsed_ms"}, "ingress_request_id": {"latest-rank"}}
	page := s15GET[map[string]any](t, h, profile, "/api/stats/requests?"+q.Encode(), 200)
	if jsonInt(t, page["retained_ingress_total"]) != 1 || len(page["items"].([]any)) != 1 {
		t.Fatalf("finalized duplicates counted as ingress: %+v", page)
	}
	item := asMap(t, page["items"].([]any)[0])
	if jsonInt(t, item["elapsed_ms"]) != 30000 || jsonInt(t, asMap(t, item["ranking"])["value"]) != 30000 {
		t.Fatalf("latest finalized evidence differs: %+v", item)
	}
	q.Set("ingress_request_id", "absent-rank")
	empty := s15GET[map[string]any](t, h, profile, "/api/stats/requests?"+q.Encode(), 200)
	counts := asMap(t, empty["ranking"])
	if len(empty["items"].([]any)) != 0 || jsonInt(t, counts["rankable_ingress_count"]) != 0 || jsonInt(t, counts["unrankable_ingress_count"]) != 0 {
		t.Fatalf("empty ranking counts: %+v", empty)
	}
	for _, value := range asMap(t, counts["unrankable_reasons"]) {
		if jsonInt(t, value) != 0 {
			t.Fatalf("empty reasons not zero: %+v", counts)
		}
	}
}

func TestChainRankingHistoricalCurrencyGroups(t *testing.T) {
	h := newS15ContractHarness(t)
	profile := modelLoadDefaultProfileID(t, h)
	at := fixedS15Now.Add(-5 * time.Minute)
	ensureContractTestLogPartitions(t, h, contractTestLogPartitionFor("request_logs", at), contractTestLogPartitionFor("usage_request_events", at))
	for index, id := range []string{"epoch-a", "epoch-b", "epoch-c", "epoch-d"} {
		seedChainIngress(t, h, profile, id, at, 200, 1, false, "not_streaming")
		epoch := 1
		code := "USD"
		amount := index + 1
		if index >= 2 {
			epoch = 2
			amount = 999 - index
		}
		if index == 3 {
			code = "EUR"
		}
		if _, err := h.conn.Exec(context.Background(), `UPDATE usage_request_events SET pricing_status='priced',pricing_evidence_trust='trusted',report_currency_code=$3,reporting_currency_epoch=$4,input_cost_micros=0,output_cost_micros=0,reasoning_cost_micros=0,cache_read_input_cost_micros=0,cache_creation_input_cost_micros=0,total_cost_original_micros=$5,total_cost_user_currency_micros=$5 WHERE profile_id=$1 AND ingress_request_id=$2`, profile, id, code, epoch, amount); err != nil {
			t.Fatal(err)
		}
	}
	q := url.Values{"view": {"ingress_chains"}, "sort_by": {"total_cost_user_currency_micros"}}
	page := s15GET[map[string]any](t, h, profile, "/api/stats/requests?"+q.Encode(), 200)
	items := page["items"].([]any)
	want := []string{"epoch-b", "epoch-a", "epoch-d", "epoch-c"}
	for index, id := range want {
		if asMap(t, items[index])["ingress_request_id"] != id {
			t.Fatalf("cross-group amounts were compared: %+v", items)
		}
	}
	q.Set("cost_segment_key", "e.2")
	selected := s15GET[map[string]any](t, h, profile, "/api/stats/requests?"+q.Encode(), 200)
	if jsonInt(t, selected["retained_ingress_total"]) != 2 || jsonInt(t, asMap(t, selected["ranking"])["rankable_ingress_count"]) != 2 {
		t.Fatalf("selected currency cohort: %+v", selected)
	}
}
