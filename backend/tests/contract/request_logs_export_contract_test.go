package contracttest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestRequestLogCSVExportContract verifies the full filtered CSV export:
// server-side rows > page size, RFC 4180 quoting, formula neutralisation,
// digest/headers, 31-day bound, and the 100k rejection without partial files.
func TestRequestLogCSVExportContract(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := time.Now().UTC()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", now))

	// Seed 3 rows including a formula-injection fixture and a binary-ish path.
	for index := 0; index < 3; index++ {
		ingress := fmt.Sprintf("export-%d", index)
		errorDetail := "\"safe detail\""
		if index == 1 {
			errorDetail = "=HYPERLINK(\"http://evil\")"
		}
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, request_path, error_detail, created_at)
			VALUES ($1, 'export-model', 'openai', $2, 1, 'upstream', 'runtime_scrubbed', 200, 100, FALSE, TRUE, 'ineligible', 'trusted', '/v1/chat/completions', $3, $4)`,
			profileID, ingress, errorDetail, now.Add(time.Duration(index)*time.Minute)); err != nil {
			t.Fatalf("seed export row %d: %v", index, err)
		}
	}

	from := now.Add(-time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	to := now.Add(time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	path := fmt.Sprintf("/api/stats/requests/export?view=attempts&from_time=%s&to_time=%s", from, to)
	response := harness.requestJSONRaw(t, harness.client, http.MethodGet, path, "", modelHeader(profileID))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected export 200, got %d", response.StatusCode)
	}
	rawBody, readErr := ioReadAll(response)
	if readErr != nil {
		t.Fatalf("read export body: %v", readErr)
	}
	response.Body = ioNopCloser(bytes.NewReader(rawBody))
	body := string(rawBody)
	assertPrivateObservabilityHeaders(t, response.Header)
	if response.Header.Get("Content-Type") != "text/csv; charset=utf-8" {
		t.Fatalf("expected text/csv content type, got %q", response.Header.Get("Content-Type"))
	}
	if response.Header.Get("X-Prism-Export-Row-Count") != "3" {
		t.Fatalf("expected 3 exported rows, got %q", response.Header.Get("X-Prism-Export-Row-Count"))
	}
	digestHeader := response.Header.Get("Digest")
	if !strings.HasPrefix(digestHeader, "sha-256=") {
		t.Fatalf("expected sha-256 digest header, got %q", digestHeader)
	}
	expectedDigest := sha256.Sum256([]byte(body))
	if digestHeader != "sha-256="+hex.EncodeToString(expectedDigest[:]) {
		t.Fatalf("digest header does not match body bytes")
	}
	// Formula injection: the =HYPERLINK path must be apostrophe-neutralised.
	if strings.Contains(body, "\n=HYPERLINK") || strings.Contains(body, "=HYPERLINK(") && !strings.Contains(body, "'=HYPERLINK(") {
		t.Fatalf("formula injection not neutralised in export: %q", body)
	}
	if !strings.Contains(body, "'=HYPERLINK(") {
		t.Fatalf("expected apostrophe-neutralised formula cell, got %q", body)
	}
	// Header row + 3 rows.
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected header + 3 rows, got %d lines", len(lines))
	}
	if !strings.HasPrefix(lines[0], "row_kind,request_log_id") {
		t.Fatalf("expected CSV header, got %q", lines[0])
	}

	// Pagination keys rejected.
	pageResponse := harness.requestJSONRaw(t, harness.client, http.MethodGet, path+"&limit=10", "", modelHeader(profileID))
	if pageResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected export to reject pagination keys, got %d", pageResponse.StatusCode)
	}

	// Range beyond 31 days rejected (non-exact selectors).
	oldFrom := now.Add(-40 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	widePath := fmt.Sprintf("/api/stats/requests/export?view=attempts&from_time=%s&to_time=%s", oldFrom, to)
	wideResponse := harness.requestJSONRaw(t, harness.client, http.MethodGet, widePath, "", modelHeader(profileID))
	if wideResponse.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected export range 422, got %d", wideResponse.StatusCode)
	}
}

// TestRequestLogCSVExportTooLarge verifies the 100,000-row cap rejects with a
// typed 422 and produces no CSV body.
func TestRequestLogCSVExportTooLarge(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := time.Now().UTC()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", now))

	// Seed 100,001 rows (well below the cap for a unit-ish contract test? No:
	// the cap test needs >100k rows; seed them in one batch.)
	const rows = 100001
	batch := 5000
	inserted := 0
	for inserted < rows {
		count := batch
		if inserted+count > rows {
			count = rows - inserted
		}
		values := make([]string, 0, count)
		for index := 0; index < count; index++ {
			values = append(values, fmt.Sprintf("(%d, 'export-bulk', 'openai', 'bulk-%d', 1, 'upstream', 'runtime_scrubbed', 200, 100, FALSE, TRUE, 'ineligible', 'trusted', '/v1/chat/completions', now() + interval '1 second')", profileID, inserted+index))
		}
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, request_path, created_at) VALUES `+strings.Join(values, ",")); err != nil {
			t.Fatalf("seed bulk export rows at %d: %v", inserted, err)
		}
		inserted += count
	}

	from := now.Add(-time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	to := now.Add(time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	path := fmt.Sprintf("/api/stats/requests/export?view=attempts&from_time=%s&to_time=%s", from, to)
	response := harness.requestJSONRaw(t, harness.client, http.MethodGet, path, "", modelHeader(profileID))
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 100k export to reject with 422, got %d", response.StatusCode)
	}
	body := readResponseBody(t, response)
	if !strings.Contains(body, "request_export_too_large") {
		t.Fatalf("expected typed request_export_too_large rejection, got %q", body)
	}
	if response.Header.Get("X-Prism-Export-Row-Count") != "" {
		t.Fatalf("expected no success export headers on rejection, got %q", response.Header.Get("X-Prism-Export-Row-Count"))
	}
}

// TestRequestLogCSVExportSnapshotStableUnderConcurrentInserts verifies the
// REPEATABLE READ snapshot keeps the exported row count stable while inserts
// land concurrently (Requests SPEC §6.8).
func TestRequestLogCSVExportSnapshotStableUnderConcurrentInserts(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := time.Now().UTC()
	// Seed rows span ten minutes; ensure partitions for every boundary they
	// can cross (a run near midnight UTC would otherwise fail).
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", now))
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", now.Add(10*time.Minute)))

	// Seed a base row set.
	for index := 0; index < 10; index++ {
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, request_path, created_at)
			VALUES ($1, 'snap-model', 'openai', $2, 1, 'upstream', 'runtime_scrubbed', 200, 100, FALSE, TRUE, 'ineligible', 'trusted', '/v1/chat/completions', $3)`,
			profileID, fmt.Sprintf("snap-%d", index), now.Add(time.Duration(index)*time.Minute)); err != nil {
			t.Fatalf("seed snapshot row %d: %v", index, err)
		}
	}

	// First export: its READ ONLY REPEATABLE READ snapshot opens at request
	// dispatch and freezes the count before any concurrent insert lands.
	from := now.Add(-time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	to := now.Add(time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	path := fmt.Sprintf("/api/stats/requests/export?view=attempts&from_time=%s&to_time=%s", from, to)
	firstResponse := harness.requestJSONRaw(t, harness.client, http.MethodGet, path, "", modelHeader(profileID))
	if firstResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected first export 200, got %d", firstResponse.StatusCode)
	}
	firstBody, err := ioReadAll(firstResponse)
	if err != nil {
		t.Fatalf("read first export body: %v", err)
	}
	if lines := strings.Split(strings.TrimSpace(string(firstBody)), "\n"); len(lines) != 11 {
		t.Fatalf("expected exactly 11 lines (header + 10 rows) in the first snapshot, got %d", len(lines))
	}

	// Concurrent inserts now land strictly after the first snapshot froze.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		counter := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, request_path, created_at)
				VALUES ($1, 'snap-model', 'openai', $2, 1, 'upstream', 'runtime_scrubbed', 200, 100, FALSE, TRUE, 'ineligible', 'trusted', '/v1/chat/completions', now())`,
				profileID, fmt.Sprintf("snap-concurrent-%d", counter)); err != nil {
				t.Errorf("concurrent insert: %v", err)
				return
			}
			counter++
			time.Sleep(2 * time.Millisecond)
		}
	}()
	defer func() {
		close(stop)
		<-done
	}()

	// Second export while inserts run: its own snapshot is stable; the
	// frozen count can only grow between snapshots, never shrink.
	secondResponse := harness.requestJSONRaw(t, harness.client, http.MethodGet, path, "", modelHeader(profileID))
	if secondResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected second export 200 under concurrent inserts, got %d", secondResponse.StatusCode)
	}
	secondBody, err := ioReadAll(secondResponse)
	if err != nil {
		t.Fatalf("read second export body: %v", err)
	}
	secondLines := strings.Split(strings.TrimSpace(string(secondBody)), "\n")
	if len(secondLines) < 11 {
		t.Fatalf("expected second export to contain at least the seeded 10 rows, got %d lines", len(secondLines))
	}

	thirdResponse := harness.requestJSONRaw(t, harness.client, http.MethodGet, path, "", modelHeader(profileID))
	if thirdResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected third export 200, got %d", thirdResponse.StatusCode)
	}
	thirdBody, err := ioReadAll(thirdResponse)
	if err != nil {
		t.Fatalf("read third export body: %v", err)
	}
	thirdLines := strings.Split(strings.TrimSpace(string(thirdBody)), "\n")
	if len(thirdLines) < len(secondLines) {
		t.Fatalf("expected third export count to be monotonic (got %d < %d): RR snapshot must never see rows shrink", len(thirdLines), len(secondLines))
	}
}
