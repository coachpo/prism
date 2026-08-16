package contracttest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

var privateObservabilityHeaderCases = []struct {
	name string
	path string
	want int
}{
	{"request attempts", "/api/stats/requests?view=attempts&time_range=24h&limit=1", http.StatusOK},
	{"request chains", "/api/stats/requests?view=ingress_chains&time_range=24h&chain_limit=1", http.StatusOK},
	{"request list rejection", "/api/stats/requests?view=invalid", http.StatusBadRequest},
	{"request detail", "/api/stats/requests/9910", http.StatusOK},
	{"request detail rejection", "/api/stats/requests/not-a-number", http.StatusBadRequest},
	{"request export rejection", "/api/stats/requests/export", http.StatusUnprocessableEntity},
	{"audit list", "/api/audit/logs?from=2026-04-18T12:00:00Z&to=2026-04-19T12:00:00Z&limit=1", http.StatusOK},
	{"audit list conflict", "/api/audit/logs?from=2026-04-18T12:00:00Z&to=2026-04-19T12:00:00Z&request_log_id=9910", http.StatusConflict},
	{"audit detail rejection", "/api/audit/logs/not-a-number", http.StatusBadRequest},
	{"audit request body rejection", "/api/audit/logs/not-a-number/body/request", http.StatusBadRequest},
	{"audit response body rejection", "/api/audit/logs/not-a-number/body/response", http.StatusBadRequest},
}

func TestPrivateObservabilityResponseHeaders(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedPrivateObservabilityRequestLog(t, harness, profileID)
	headers := modelHeader(profileID)
	headers["Origin"] = "http://localhost:5173"

	for _, test := range privateObservabilityHeaderCases {
		t.Run(test.name, func(t *testing.T) {
			response := harness.requestJSON(t, harness.client, http.MethodGet, test.path, nil, headers)
			assertStatus(t, response, test.want)
			assertPrivateObservabilityHeaders(t, response.Header)
			assertVaryContains(t, response.Header, "Origin")
			if got := response.Header.Get("Access-Control-Allow-Origin"); got != headers["Origin"] {
				t.Fatalf("expected allowed CORS origin %q, got %q", headers["Origin"], got)
			}
		})
	}
}

func seedPrivateObservabilityRequestLog(t *testing.T, harness *contractHarness, profileID int) {
	t.Helper()
	_, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, request_path, audit_enabled_at_request, created_at)
		VALUES (9910, $1, 'private-headers-model', 'openai', 'private-headers-ingress', 1, 'upstream', 'runtime_scrubbed', 200, 10, FALSE, TRUE, 'ineligible', 'trusted', '/v1/chat/completions', FALSE, $2)`, profileID, fixedS15Now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("seed private observability request log: %v", err)
	}
}

func assertPrivateObservabilityHeaders(t *testing.T, header http.Header) {
	t.Helper()
	if got := header.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("expected private no-store cache control, got %q", got)
	}
	for _, field := range []string{"Authorization", "Cookie", "X-Profile-Id"} {
		assertVaryContains(t, header, field)
	}
}

func assertVaryContains(t *testing.T, header http.Header, want string) {
	t.Helper()
	matches := 0
	for _, line := range header.Values("Vary") {
		for _, field := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(field), want) {
				matches++
			}
		}
	}
	if matches != 1 {
		t.Fatalf("expected Vary to contain %q exactly once, got %q", want, header.Values("Vary"))
	}
}
