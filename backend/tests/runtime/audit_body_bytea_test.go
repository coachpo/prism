package runtimetest

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"
)

// Audit bodies land in bytea columns, so they must be bound as raw bytes.
// Binding them as text makes PostgreSQL parse the body as a bytea input
// literal: a backslash starts an escape sequence, so any JSON carrying \n,
// \" or \uXXXX fails with 22P02 and head-of-line blocks the telemetry outbox,
// while a body that happens to contain no backslash is stored
// escape-interpreted rather than verbatim.
func TestAuditBodiesWithBackslashesRoundTripVerbatim(t *testing.T) {
	// Every backslash form a real upstream emits: an escaped quote, a newline
	// escape, a unicode escape, and a literal backslash pair.
	upstreamBody := []byte(`{"created":1713833628,"choices":[{"message":{"role":"assistant",` +
		`"content":"line one\nline two \"quoted\" \u4e2d\u6587 path C:\\tmp\\file"}}],` +
		`"usage":{"prompt_tokens":11,"completion_tokens":41,"total_tokens":52}}`)

	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	seedRuntimeAuditFamilySetting(t, harness, profileID, "openai", true, true)

	upstream := newRouteMatrixUpstream(t, "application/json", upstreamBody)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "audit-bytea-public-" + randomSuffix(),
		TargetModelID:   "audit-bytea-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/audit-bytea"),
		EndpointAPIKey:  "audit-bytea-key",
	})

	requestPayload := map[string]any{
		"model":    route.PublicModelID,
		"messages": []map[string]any{{"role": "user", "content": "tab\tand \"quotes\" and \\ backslash"}},
	}
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", requestPayload, nil)
	assertStatus(t, response, http.StatusOK)

	// The audit row only exists once materialization succeeded. Before the
	// fix the outbox retries forever, so this is where the bug surfaces.
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{
		RequestLogs: 1,
		UsageEvents: 1,
		OutboxRows:  0,
	}, 15*time.Second)

	var storedRequestBody, storedResponseBody []byte
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT COALESCE(request_body, ''::bytea), COALESCE(response_body, ''::bytea)
		   FROM audit_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`,
		profileID,
	).Scan(&storedRequestBody, &storedResponseBody); err != nil {
		t.Fatalf("read audited bodies: %v", err)
	}

	if !bytes.Equal(storedResponseBody, upstreamBody) {
		t.Fatalf("audited response body was not stored verbatim\n want: %s\n  got: %s", upstreamBody, storedResponseBody)
	}

	// The request body is re-serialized on the way out, so assert the escape
	// that actually distinguishes verbatim bytes from escape-interpreted ones.
	if !bytes.Contains(storedRequestBody, []byte(`\\`)) {
		t.Fatalf("audited request body lost its literal backslash escape, got: %s", storedRequestBody)
	}
	if !bytes.Contains(storedRequestBody, []byte(`\"quotes\"`)) {
		t.Fatalf("audited request body lost its escaped quotes, got: %s", storedRequestBody)
	}
}
