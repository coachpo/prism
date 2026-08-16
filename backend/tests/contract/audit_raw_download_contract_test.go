package contracttest

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAuditRawBodyDownloadRoundTrip verifies the raw-download routes return
// the exact stored BYTEA prefix byte-for-byte with safe attachment headers
// and private no-store cache control (Requests SPEC §5.4).
func TestAuditRawBodyDownloadRoundTrip(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := time.Now().UTC()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("audit_logs", now))

	// Seed an audit log with a binary-ish request body (invalid UTF-8/NUL).
	requestBody := []byte{0x7b, 0x22, 0x6d, 0x73, 0x67, 0x22, 0x3a, 0x22, 0x68, 0x69, 0x00, 0xff, 0x22, 0x7d}
	responseBody := []byte("event: response.completed\ndata: {\"ok\":true}\n\n")
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO audit_logs (id, profile_id, model_id, request_method, request_url, request_headers, request_headers_scrub_provenance, request_headers_capture_status, request_body, request_body_capture_provenance, request_body_capture_status, request_body_bytes_observed, request_body_bytes_stored, response_body, response_body_capture_provenance, response_body_capture_status, response_body_bytes_observed, response_body_bytes_stored, upstream_status_code, row_kind, url_scrub_provenance, attempt_duration_ms, is_stream, audit_enabled_at_request, audit_capture_bodies_at_request, created_at)
		VALUES (1, $1, 'audit-model', 'POST', 'https://audit.invalid/v1/chat/completions', '{}', 'runtime_scrubbed', 'captured', $2, 'runtime_bytes', 'captured', $3, $3, $4, 'runtime_bytes', 'captured', $5, $5, 200, 'upstream', 'runtime_scrubbed', 100, FALSE, TRUE, TRUE, $6)`,
		profileID, requestBody, len(requestBody), responseBody, len(responseBody), now); err != nil {
		t.Fatalf("seed audit raw body row: %v", err)
	}

	// Request raw download.
	reqResponse := harness.requestRaw(t, harness.client, http.MethodGet, "/api/audit/logs/1/body/request", bytes.NewReader(nil), false, modelHeader(profileID))
	if reqResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected request raw download 200, got %d", reqResponse.StatusCode)
	}
	if reqResponse.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("expected octet-stream content type, got %q", reqResponse.Header.Get("Content-Type"))
	}
	if reqResponse.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("expected nosniff, got %q", reqResponse.Header.Get("X-Content-Type-Options"))
	}
	if reqResponse.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("expected private no-store, got %q", reqResponse.Header.Get("Cache-Control"))
	}
	if !strings.Contains(reqResponse.Header.Get("Content-Disposition"), "attachment; filename=") {
		t.Fatalf("expected attachment disposition, got %q", reqResponse.Header.Get("Content-Disposition"))
	}
	if reqResponse.Header.Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("expected sandbox CSP, got %q", reqResponse.Header.Get("Content-Security-Policy"))
	}
	rawBody, err := ioReadAll(reqResponse)
	if err != nil {
		t.Fatalf("read raw request body: %v", err)
	}
	if !bytes.Equal(rawBody, requestBody) {
		t.Fatalf("raw request body round trip mismatch: got %v want %v", rawBody, requestBody)
	}

	// Response raw download.
	respResponse := harness.requestRaw(t, harness.client, http.MethodGet, "/api/audit/logs/1/body/response", bytes.NewReader(nil), false, modelHeader(profileID))
	if respResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected response raw download 200, got %d", respResponse.StatusCode)
	}
	rawResponseBody, err := ioReadAll(respResponse)
	if err != nil {
		t.Fatalf("read raw response body: %v", err)
	}
	if !bytes.Equal(rawResponseBody, responseBody) {
		t.Fatalf("raw response body round trip mismatch")
	}

	// Audit detail is also private no-store.
	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs/1", nil, modelHeader(profileID))
	if detailResponse.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("expected audit detail private no-store, got %q", detailResponse.Header.Get("Cache-Control"))
	}
}
