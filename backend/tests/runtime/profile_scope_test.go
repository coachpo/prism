package runtimetest

import (
	"context"
	"net/http"
	"testing"

	"github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestManagementProfileScopePinnedToDefault(t *testing.T) {
	harness := newRuntimeHarness(t)

	for _, testCase := range []struct {
		name    string
		headers map[string]string
	}{
		{name: "missing header"},
		{name: "ignored non-default header", headers: map[string]string{profiledomain.ProfileIDHeader: "999"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			getResponse := harness.requestJSON(t, http.MethodGet, "/api/settings/costing", nil, testCase.headers)
			assertStatus(t, getResponse, http.StatusOK)
			var current map[string]any
			decodeJSONResponse(t, getResponse, &current)
			response := harness.requestJSON(
				t,
				http.MethodPut,
				"/api/settings/costing",
				map[string]any{"expected_updated_at": current["updated_at"], "timezone_preference": "UTC"},
				testCase.headers,
			)
			assertStatus(t, response, http.StatusOK)

			var payload map[string]any
			decodeJSONResponse(t, response, &payload)
			if got := jsonInt(t, payload["profile_id"]); got != 1 {
				t.Fatalf("expected pinned profile_id 1, got %+v", payload)
			}
		})
	}

	var count int
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM user_settings WHERE profile_id = 1 AND timezone_preference = 'UTC'`,
	).Scan(&count); err != nil {
		t.Fatalf("count default profile timezone settings: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected timezone write to land on Default profile id=1, got %d rows", count)
	}
}
