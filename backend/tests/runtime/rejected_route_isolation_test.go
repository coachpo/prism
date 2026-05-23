package runtime_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

type runtimeRejectedRoutePersistenceCounts struct {
	RequestLogs int
	AuditLogs   int
	UsageEvents int
	OutboxRows  int
}

func TestRuntimeRejectedRoutesStayOutsideTransportAdmissionSideEffectsAndPersistence(t *testing.T) {
	var sideEffectSubmits atomic.Int32
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{
			SideEffects: runtimeapi.RuntimeSideEffectOptions{
				Hooks: &runtimeapi.RuntimeSideEffectHooks{
					AfterSubmit: func(runtimeapi.RuntimeSideEffectSubmitResult) {
						sideEffectSubmits.Add(1)
					},
				},
			},
		},
	})
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()

	openAIVendorID := enableRuntimeRejectedRouteAudit(t, harness, "openai")
	anthropicVendorID := enableRuntimeRejectedRouteAudit(t, harness, "anthropic")
	geminiVendorID := enableRuntimeRejectedRouteAudit(t, harness, "gemini")
	openAIRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "rejected-openai-public-" + suffix,
		TargetModelID:   "rejected-openai-target-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/rejected/openai"),
		EndpointAPIKey:  "rejected-openai-key",
	})
	anthropicRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "anthropic",
		PublicModelID:   "rejected-anthropic-public-" + suffix,
		TargetModelID:   "rejected-anthropic-target-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/rejected/anthropic"),
		EndpointAPIKey:  "rejected-anthropic-key",
	})
	geminiRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "gemini",
		PublicModelID:   "rejected-gemini-public-" + suffix,
		TargetModelID:   "rejected-gemini-target-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/rejected/gemini"),
		EndpointAPIKey:  "rejected-gemini-key",
	})
	attachRuntimeRejectedRouteVendor(t, harness, profileID, openAIVendorID, openAIRoute)
	attachRuntimeRejectedRouteVendor(t, harness, profileID, anthropicVendorID, anthropicRoute)
	attachRuntimeRejectedRouteVendor(t, harness, profileID, geminiVendorID, geminiRoute)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	assertRuntimeRejectedRouteNoAdmissionState(t, harness, profileID, openAIRoute.ConnectionID, anthropicRoute.ConnectionID, geminiRoute.ConnectionID)
	baseline := loadRuntimeRejectedRoutePersistenceCounts(t, harness.conn, profileID)
	if baseline != (runtimeRejectedRoutePersistenceCounts{}) {
		t.Fatalf("expected fresh rejected-route test profile to start without runtime persistence rows, got %+v", baseline)
	}

	tests := []struct {
		name       string
		method     string
		path       string
		body       map[string]any
		wantStatus int
		wantDetail string
		wantAllow  string
	}{
		{
			name:   "unsupported OpenAI route",
			method: http.MethodPost,
			path:   "/v1/models",
			body: map[string]any{
				"model":    openAIRoute.PublicModelID,
				"messages": []map[string]any{},
			},
			wantStatus: http.StatusNotFound,
			wantDetail: "Runtime operation not found",
		},
		{
			name:   "unsupported Anthropic route",
			method: http.MethodPost,
			path:   "/v1/messages/batches",
			body: map[string]any{
				"model":    anthropicRoute.PublicModelID,
				"messages": []map[string]any{},
			},
			wantStatus: http.StatusNotFound,
			wantDetail: "Runtime operation not found",
		},
		{
			name:   "unsupported Gemini action",
			method: http.MethodPost,
			path:   "/v1beta/models/" + geminiRoute.PublicModelID + ":embedContent",
			body: map[string]any{
				"contents": []map[string]any{{"parts": []map[string]any{{"text": "rejected route"}}}},
			},
			wantStatus: http.StatusNotFound,
			wantDetail: "Runtime operation not found",
		},
		{
			name:   "wrong method OpenAI route",
			method: http.MethodGet,
			path:   "/v1/chat/completions",
			body: map[string]any{
				"model":    openAIRoute.PublicModelID,
				"messages": []map[string]any{},
			},
			wantStatus: http.StatusMethodNotAllowed,
			wantDetail: "Method not allowed for runtime operation",
			wantAllow:  http.MethodPost,
		},
		{
			name:   "wrong method Anthropic route",
			method: http.MethodGet,
			path:   "/v1/messages",
			body: map[string]any{
				"model":    anthropicRoute.PublicModelID,
				"messages": []map[string]any{},
			},
			wantStatus: http.StatusMethodNotAllowed,
			wantDetail: "Method not allowed for runtime operation",
			wantAllow:  http.MethodPost,
		},
		{
			name:   "wrong method Gemini route",
			method: http.MethodGet,
			path:   "/v1beta/models/" + geminiRoute.PublicModelID + ":generateContent",
			body: map[string]any{
				"contents": []map[string]any{{"parts": []map[string]any{{"text": "wrong method"}}}},
			},
			wantStatus: http.StatusMethodNotAllowed,
			wantDetail: "Method not allowed for runtime operation",
			wantAllow:  http.MethodPost,
		},
	}

	harness.upstream.clear()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := harness.requestJSON(t, test.method, test.path, test.body, nil)

			assertStatus(t, response, test.wantStatus)
			assertRuntimeRejectedRouteError(t, response, test.wantDetail, test.wantAllow)
			assertRuntimeRejectedRouteNoDownstreamEffects(t, harness, profileID, &sideEffectSubmits, openAIRoute.ConnectionID, anthropicRoute.ConnectionID, geminiRoute.ConnectionID)
		})
	}
	assertRuntimeRejectedRoutePersistenceCountsRemain(t, harness.conn, profileID, baseline, 500*time.Millisecond)
}

func enableRuntimeRejectedRouteAudit(t *testing.T, harness *runtimeHarness, vendorKey string) int {
	t.Helper()
	vendorID := loadVendorIDByKey(t, harness.conn, vendorKey)
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE vendors SET audit_enabled = TRUE, audit_capture_bodies = TRUE WHERE id = $1`,
		vendorID,
	); err != nil {
		t.Fatalf("enable rejected-route audit for vendor %q: %v", vendorKey, err)
	}
	return vendorID
}

func attachRuntimeRejectedRouteVendor(t *testing.T, harness *runtimeHarness, profileID int, vendorID int, route seededRuntimeRoute) {
	t.Helper()
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE model_configs SET vendor_id = $1 WHERE profile_id = $2 AND model_id = ANY($3::text[])`,
		vendorID,
		profileID,
		[]string{route.PublicModelID, route.TargetModelID},
	); err != nil {
		t.Fatalf("attach rejected-route vendor metadata: %v", err)
	}
}

func assertRuntimeRejectedRouteError(t *testing.T, response *http.Response, wantDetail string, wantAllow string) {
	t.Helper()
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("expected rejected-route JSON content type, got %q", contentType)
	}
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["detail"] != wantDetail {
		t.Fatalf("expected rejected-route detail %q, got %+v", wantDetail, payload)
	}
	if allow := response.Header.Get("Allow"); allow != wantAllow {
		t.Fatalf("expected Allow header %q, got %q", wantAllow, allow)
	}
}

func assertRuntimeRejectedRouteNoDownstreamEffects(t *testing.T, harness *runtimeHarness, profileID int, sideEffectSubmits *atomic.Int32, connectionIDs ...int) {
	t.Helper()
	if got := len(harness.upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected rejected route not to reach provider transport, got %d upstream requests", got)
	}
	if got := sideEffectSubmits.Load(); got != 0 {
		t.Fatalf("expected rejected route not to submit runtime activity, got %d side-effect submissions", got)
	}
	assertRuntimeRejectedRouteNoAdmissionState(t, harness, profileID, connectionIDs...)
}

func assertRuntimeRejectedRouteNoAdmissionState(t *testing.T, harness *runtimeHarness, profileID int, connectionIDs ...int) {
	t.Helper()
	for _, connectionID := range connectionIDs {
		if runtimeStateExists(t, harness, profileID, connectionID) {
			t.Fatalf("expected rejected route not to create runtime admission state for connection %d", connectionID)
		}
	}
}

func assertRuntimeRejectedRoutePersistenceCountsRemain(t *testing.T, conn *pgx.Conn, profileID int, want runtimeRejectedRoutePersistenceCounts, duration time.Duration) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for {
		got := loadRuntimeRejectedRoutePersistenceCounts(t, conn, profileID)
		if got != want {
			t.Fatalf("expected rejected routes to leave runtime persistence counts %+v, got %+v", want, got)
		}
		if !time.Now().Before(deadline) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func loadRuntimeRejectedRoutePersistenceCounts(t *testing.T, conn *pgx.Conn, profileID int) runtimeRejectedRoutePersistenceCounts {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var counts runtimeRejectedRoutePersistenceCounts
	if err := conn.QueryRow(
		ctx,
		`SELECT
			(SELECT COUNT(*) FROM request_logs WHERE profile_id = $1),
			(SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1),
			(SELECT COUNT(*) FROM usage_request_events WHERE profile_id = $1),
			(SELECT COUNT(*) FROM runtime_telemetry_outbox WHERE profile_id = $1)`,
		profileID,
	).Scan(
		&counts.RequestLogs,
		&counts.AuditLogs,
		&counts.UsageEvents,
		&counts.OutboxRows,
	); err != nil {
		t.Fatalf("load rejected-route runtime persistence counts: %v", err)
	}
	return counts
}
