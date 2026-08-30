package runtimetest

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestRuntimePermissiveAttributionIdentifiedOnValidKey verifies that with
// auth disabled, a valid active unexpired proxy key is still recognized and
// attributed: request/usage rows carry identified + auth_enforced=false plus
// the immutable ID/name snapshot.
func TestRuntimePermissiveAttributionIdentifiedOnValidKey(t *testing.T) {
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{})
	profileID := harness.activeProfileID(t)
	proxyAPIKey := harness.insertProxyAPIKey(t, "permissive-identified")
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "permissive-id-public-" + randomSuffix(),
		TargetModelID:   "permissive-id-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/permissive/identified"),
		EndpointAPIKey:  "permissive-upstream-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "permissive identified attribution"}},
		"model":    route.PublicModelID,
	}, map[string]string{"Authorization": "Bearer " + proxyAPIKey.RawKey})
	assertStatus(t, response, http.StatusOK)

	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeAttribution(t, harness.conn, profileID, runtimeAttributionExpectation{
		State:        "identified",
		AuthEnforced: false,
		KeyID:        proxyAPIKey.ID,
		Name:         proxyAPIKey.Name,
	})
	waitForProxyAPIKeyUsageMaterialization(t, harness.conn, 5*time.Second)
}

// TestRuntimePermissiveAttributionNoneOnMissingOrInvalidKey verifies that with
// auth disabled, missing and unrecognized credentials continue and are
// recorded as none (never identified, never rejected).
func TestRuntimePermissiveAttributionNoneOnMissingOrInvalidKey(t *testing.T) {
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{})
	profileID := harness.activeProfileID(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "permissive-none-public-" + randomSuffix(),
		TargetModelID:   "permissive-none-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/permissive/none"),
		EndpointAPIKey:  "permissive-none-key",
	})

	missingResponse := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "permissive none missing"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, missingResponse, http.StatusOK)

	invalidResponse := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "permissive none invalid"}},
		"model":    route.PublicModelID,
	}, map[string]string{"Authorization": "Bearer pm-ffffffffffffffffffffffffffffffff"})
	assertStatus(t, invalidResponse, http.StatusOK)

	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 2, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeAttribution(t, harness.conn, profileID, runtimeAttributionExpectation{
		State:        "none",
		AuthEnforced: false,
	})
}

// TestRuntimeEnforcedAttributionIdentifiedAndRejected verifies auth-on
// behavior: valid key writes identified with auth_enforced=true; missing key
// returns 401 and produces no ordinary telemetry rows.
func TestRuntimeEnforcedAttributionIdentifiedAndRejected(t *testing.T) {
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{})
	profileID := harness.activeProfileID(t)
	proxyAPIKey := harness.enableRuntimeProxyAPIKeyAuth(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "enforced-id-public-" + randomSuffix(),
		TargetModelID:   "enforced-id-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/enforced/identified"),
		EndpointAPIKey:  "enforced-upstream-key",
	})

	missingResponse := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "enforced missing"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, missingResponse, http.StatusUnauthorized)

	validResponse := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "enforced identified"}},
		"model":    route.PublicModelID,
	}, map[string]string{"Authorization": "Bearer " + proxyAPIKey})
	assertStatus(t, validResponse, http.StatusOK)

	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	keyRecord := harness.latestProxyAPIKey(t, "runtime-branch-key-")
	assertLatestRuntimeAttribution(t, harness.conn, profileID, runtimeAttributionExpectation{
		State:        "identified",
		AuthEnforced: true,
		KeyID:        keyRecord.ID,
		Name:         keyRecord.Name,
	})
}

type runtimeAttributionExpectation struct {
	State        string
	AuthEnforced bool
	KeyID        int
	Name         string
}

func assertLatestRuntimeAttribution(t *testing.T, conn *pgx.Conn, profileID int, want runtimeAttributionExpectation) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var state sql.NullString
	var enforced sql.NullBool
	var keyID sql.NullInt64
	var name sql.NullString
	if err := conn.QueryRow(ctx, `
		SELECT proxy_api_key_attribution_state, proxy_api_key_auth_enforced_at_request,
			proxy_api_key_id_snapshot, proxy_api_key_name_snapshot
		FROM request_logs
		WHERE profile_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, profileID).Scan(&state, &enforced, &keyID, &name); err != nil {
		t.Fatalf("load latest request_log attribution: %v", err)
	}
	if !state.Valid || state.String != want.State {
		t.Fatalf("attribution state = %v, want %q", state, want.State)
	}
	if !enforced.Valid || enforced.Bool != want.AuthEnforced {
		t.Fatalf("auth enforced = %v, want %v", enforced, want.AuthEnforced)
	}
	if want.State == "identified" {
		if !keyID.Valid || int(keyID.Int64) != want.KeyID {
			t.Fatalf("key id snapshot = %v, want %d", keyID, want.KeyID)
		}
		if !name.Valid || name.String != want.Name {
			t.Fatalf("key name snapshot = %v, want %q", name, want.Name)
		}
	} else {
		if keyID.Valid {
			t.Fatalf("expected nil key id snapshot for %q, got %d", want.State, keyID.Int64)
		}
	}

	// The usage event must carry the same frozen attribution.
	var usageState sql.NullString
	var usageEnforced sql.NullBool
	var usageKeyID sql.NullInt64
	if err := conn.QueryRow(ctx, `
		SELECT proxy_api_key_attribution_state, proxy_api_key_auth_enforced_at_request,
			proxy_api_key_id_snapshot
		FROM usage_request_events
		WHERE profile_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, profileID).Scan(&usageState, &usageEnforced, &usageKeyID); err != nil {
		t.Fatalf("load latest usage attribution: %v", err)
	}
	if !usageState.Valid || usageState.String != want.State {
		t.Fatalf("usage attribution state = %v, want %q", usageState, want.State)
	}
	if !usageEnforced.Valid || usageEnforced.Bool != want.AuthEnforced {
		t.Fatalf("usage auth enforced = %v, want %v", usageEnforced, want.AuthEnforced)
	}
	if want.State == "identified" && (!usageKeyID.Valid || int(usageKeyID.Int64) != want.KeyID) {
		t.Fatalf("usage key id snapshot = %v, want %d", usageKeyID, want.KeyID)
	}
}
