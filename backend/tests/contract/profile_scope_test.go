package contract_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementprofiles "github.com/coachpo/prism/backend/internal/httpapi/management/profiles"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestProfileBootstrap(t *testing.T) {
	t.Run("bootstrap payload", func(t *testing.T) {
		harness := newProfileContractHarness(t)

		bootstrapResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/profiles/bootstrap", nil, nil)
		assertStatus(t, bootstrapResponse, http.StatusOK)

		var bootstrapPayload map[string]any
		decodeJSONResponse(t, bootstrapResponse, &bootstrapPayload)

		activeProfile, ok := bootstrapPayload["active_profile"].(map[string]any)
		if !ok || activeProfile == nil {
			t.Fatalf("expected bootstrap active_profile to be present on the startup-backed happy path, got %+v", bootstrapPayload)
		}
		profileLimits := asMap(t, bootstrapPayload["profile_limits"])
		if got := jsonInt(t, profileLimits["max_profiles"]); got != profiledomain.MaxNonDeletedProfiles {
			t.Fatalf("expected max_profiles %d, got %+v", profiledomain.MaxNonDeletedProfiles, profileLimits)
		}
		profilesList, ok := bootstrapPayload["profiles"].([]any)
		if !ok || len(profilesList) == 0 {
			t.Fatalf("expected bootstrap profiles list to be non-empty, got %+v", bootstrapPayload)
		}
		activeID := jsonInt(t, activeProfile["id"])
		if !profileListContainsID(t, profilesList, activeID) {
			t.Fatalf("expected bootstrap profiles to include active profile id %d", activeID)
		}

		activeResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/profiles/active", nil, nil)
		assertStatus(t, activeResponse, http.StatusOK)
		var activePayload map[string]any
		decodeJSONResponse(t, activeResponse, &activePayload)
		if got := jsonInt(t, activePayload["id"]); got != activeID {
			t.Fatalf("expected /api/profiles/active id %d, got %+v", activeID, activePayload)
		}
	})

	t.Run("lifecycle capacity cas and delete guardrails", func(t *testing.T) {
		harness := newProfileContractHarness(t)

		initialActiveResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/profiles/active", nil, nil)
		assertStatus(t, initialActiveResponse, http.StatusOK)
		var initialActivePayload map[string]any
		decodeJSONResponse(t, initialActiveResponse, &initialActivePayload)
		initialActiveID := jsonInt(t, initialActivePayload["id"])

		firstCreateResponse := harness.requestJSON(
			t,
			harness.client,
			http.MethodPost,
			"/api/profiles",
			map[string]any{"name": "Lifecycle Alpha", "description": "alpha"},
			nil,
		)
		assertStatus(t, firstCreateResponse, http.StatusCreated)
		var firstCreated map[string]any
		decodeJSONResponse(t, firstCreateResponse, &firstCreated)
		firstCreatedID := jsonInt(t, firstCreated["id"])
		if firstCreated["is_active"] != false || firstCreated["is_default"] != false || firstCreated["is_editable"] != true {
			t.Fatalf("expected newly created profile to stay inactive/non-default/editable, got %+v", firstCreated)
		}

		activateResponse := harness.requestJSON(
			t,
			harness.client,
			http.MethodPost,
			fmt.Sprintf("/api/profiles/%d/activate", firstCreatedID),
			map[string]any{"expected_active_profile_id": initialActiveID},
			nil,
		)
		assertStatus(t, activateResponse, http.StatusOK)
		var activatedPayload map[string]any
		decodeJSONResponse(t, activateResponse, &activatedPayload)
		if jsonInt(t, activatedPayload["id"]) != firstCreatedID || activatedPayload["is_active"] != true {
			t.Fatalf("expected activate response to return active profile %d, got %+v", firstCreatedID, activatedPayload)
		}

		activeAfterActivate := harness.requestJSON(t, harness.client, http.MethodGet, "/api/profiles/active", nil, nil)
		assertStatus(t, activeAfterActivate, http.StatusOK)
		var activeAfterPayload map[string]any
		decodeJSONResponse(t, activeAfterActivate, &activeAfterPayload)
		if got := jsonInt(t, activeAfterPayload["id"]); got != firstCreatedID {
			t.Fatalf("expected activated profile %d to become active, got %+v", firstCreatedID, activeAfterPayload)
		}

		staleActivate := harness.requestJSON(
			t,
			harness.client,
			http.MethodPost,
			fmt.Sprintf("/api/profiles/%d/activate", firstCreatedID),
			map[string]any{"expected_active_profile_id": initialActiveID},
			nil,
		)
		assertErrorResponse(
			t,
			staleActivate,
			http.StatusConflict,
			fmt.Sprintf("Active profile mismatch: expected %d, got %d", initialActiveID, firstCreatedID),
		)

		inactiveProfileIDs := make([]int, 0, profiledomain.MaxNonDeletedProfiles-2)
		for index := 0; index < profiledomain.MaxNonDeletedProfiles-2; index++ {
			createResponse := harness.requestJSON(
				t,
				harness.client,
				http.MethodPost,
				"/api/profiles",
				map[string]any{
					"name":        fmt.Sprintf("Lifecycle Extra %02d", index),
					"description": fmt.Sprintf("extra-%02d", index),
				},
				nil,
			)
			assertStatus(t, createResponse, http.StatusCreated)
			var created map[string]any
			decodeJSONResponse(t, createResponse, &created)
			inactiveProfileIDs = append(inactiveProfileIDs, jsonInt(t, created["id"]))
		}

		overflowCreate := harness.requestJSON(
			t,
			harness.client,
			http.MethodPost,
			"/api/profiles",
			map[string]any{"name": "Lifecycle Overflow", "description": "overflow"},
			nil,
		)
		assertErrorResponse(
			t,
			overflowCreate,
			http.StatusConflict,
			fmt.Sprintf("Maximum %d profiles reached. Delete a profile to create a new one.", profiledomain.MaxNonDeletedProfiles),
		)

		deleteActive := harness.requestJSON(
			t,
			harness.client,
			http.MethodDelete,
			fmt.Sprintf("/api/profiles/%d", firstCreatedID),
			nil,
			nil,
		)
		assertErrorResponse(t, deleteActive, http.StatusBadRequest, "Cannot delete active profile. Activate another profile first.")

		deleteDefault := harness.requestJSON(
			t,
			harness.client,
			http.MethodDelete,
			fmt.Sprintf("/api/profiles/%d", initialActiveID),
			nil,
			nil,
		)
		assertErrorResponse(t, deleteDefault, http.StatusBadRequest, "Default profile cannot be deleted.")

		deleteInactiveID := inactiveProfileIDs[len(inactiveProfileIDs)-1]
		deleteInactive := harness.requestJSON(
			t,
			harness.client,
			http.MethodDelete,
			fmt.Sprintf("/api/profiles/%d", deleteInactiveID),
			nil,
			nil,
		)
		assertStatus(t, deleteInactive, http.StatusOK)
		var deletedPayload map[string]any
		decodeJSONResponse(t, deleteInactive, &deletedPayload)
		if deletedPayload["deleted"] != true {
			t.Fatalf("expected delete payload to confirm soft-delete, got %+v", deletedPayload)
		}

		listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/profiles", nil, nil)
		assertStatus(t, listResponse, http.StatusOK)
		var listedProfiles []map[string]any
		decodeJSONResponse(t, listResponse, &listedProfiles)
		if profileSliceContainsID(t, listedProfiles, deleteInactiveID) {
			t.Fatalf("expected soft-deleted profile %d to be omitted from list", deleteInactiveID)
		}

		replacementCreate := harness.requestJSON(
			t,
			harness.client,
			http.MethodPost,
			"/api/profiles",
			map[string]any{"name": "Lifecycle Replacement", "description": "replacement"},
			nil,
		)
		assertStatus(t, replacementCreate, http.StatusCreated)
	})
}

func TestSelectedEffectiveProfileScope(t *testing.T) {
	harness := newProfileContractHarness(t)

	globalProfiles := harness.requestJSON(t, harness.client, http.MethodGet, "/api/profiles", nil, nil)
	assertStatus(t, globalProfiles, http.StatusOK)
	globalBootstrap := harness.requestJSON(t, harness.client, http.MethodGet, "/api/profiles/bootstrap", nil, nil)
	assertStatus(t, globalBootstrap, http.StatusOK)
	strayGlobalHeader := map[string]string{profiledomain.ProfileIDHeader: "999999"}
	globalProfilesWithStrayHeader := harness.requestJSON(t, harness.client, http.MethodGet, "/api/profiles", nil, strayGlobalHeader)
	assertStatus(t, globalProfilesWithStrayHeader, http.StatusOK)
	globalBootstrapWithStrayHeader := harness.requestJSON(t, harness.client, http.MethodGet, "/api/profiles/bootstrap", nil, strayGlobalHeader)
	assertStatus(t, globalBootstrapWithStrayHeader, http.StatusOK)

	activeResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/profiles/active", nil, nil)
	assertStatus(t, activeResponse, http.StatusOK)
	var activePayload map[string]any
	decodeJSONResponse(t, activeResponse, &activePayload)
	activeID := jsonInt(t, activePayload["id"])
	globalActiveWithStrayHeader := harness.requestJSON(t, harness.client, http.MethodGet, "/api/profiles/active", nil, strayGlobalHeader)
	assertStatus(t, globalActiveWithStrayHeader, http.StatusOK)
	var globalActiveWithStrayHeaderPayload map[string]any
	decodeJSONResponse(t, globalActiveWithStrayHeader, &globalActiveWithStrayHeaderPayload)
	if got := jsonInt(t, globalActiveWithStrayHeaderPayload["id"]); got != activeID {
		t.Fatalf("expected global /api/profiles/active to ignore stray %s and keep active id %d, got %+v", profiledomain.ProfileIDHeader, activeID, globalActiveWithStrayHeaderPayload)
	}

	selectedProfileResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/profiles",
		map[string]any{"name": "Selected Scope Profile", "description": "selected-profile"},
		nil,
	)
	assertStatus(t, selectedProfileResponse, http.StatusCreated)
	var selectedPayload map[string]any
	decodeJSONResponse(t, selectedProfileResponse, &selectedPayload)
	selectedID := jsonInt(t, selectedPayload["id"])
	if selectedID == activeID {
		t.Fatalf("expected selected profile id %d to differ from active profile id %d", selectedID, activeID)
	}

	probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		effectiveProfile, err := profiledomain.ResolveEffectiveProfile(r.Context(), harness.conn, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			var httpErr *profiledomain.HTTPError
			if errors.As(err, &httpErr) {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(httpErr.StatusCode)
				_ = json.NewEncoder(w).Encode(httpErr.ResponseBody())
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"detail": "Internal server error"})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]int{"effective_profile_id": effectiveProfile.ID})
	}))
	defer probeServer.Close()

	missingHeader := scopeProbeRequest(t, probeServer.URL, nil)
	assertErrorResponseCode(t, missingHeader, http.StatusBadRequest, profiledomain.ScopeErrorCodeHeaderMissing, fmt.Sprintf("%s header is required", profiledomain.ProfileIDHeader))

	invalidHeaderValue := "not-an-integer"
	invalidHeader := scopeProbeRequest(t, probeServer.URL, &invalidHeaderValue)
	assertErrorResponseCode(t, invalidHeader, http.StatusBadRequest, profiledomain.ScopeErrorCodeHeaderInvalid, fmt.Sprintf("%s must be an integer", profiledomain.ProfileIDHeader))

	nonPositiveHeaderValue := "0"
	nonPositiveHeader := scopeProbeRequest(t, probeServer.URL, &nonPositiveHeaderValue)
	assertErrorResponseCode(t, nonPositiveHeader, http.StatusBadRequest, profiledomain.ScopeErrorCodeHeaderNonPositive, fmt.Sprintf("%s must be a positive integer", profiledomain.ProfileIDHeader))

	missingProfileValue := "999999"
	missingProfile := scopeProbeRequest(t, probeServer.URL, &missingProfileValue)
	assertErrorResponseCode(t, missingProfile, http.StatusNotFound, profiledomain.ScopeErrorCodeProfileNotFound, "Profile 999999 not found")

	selectedHeaderValue := fmt.Sprintf("%d", selectedID)
	selectedProbe := scopeProbeRequest(t, probeServer.URL, &selectedHeaderValue)
	assertStatus(t, selectedProbe, http.StatusOK)
	var selectedProbePayload map[string]any
	decodeJSONResponse(t, selectedProbe, &selectedProbePayload)
	if got := jsonInt(t, selectedProbePayload["effective_profile_id"]); got != selectedID {
		t.Fatalf("expected effective profile id %d, got %+v", selectedID, selectedProbePayload)
	}
	if got := jsonInt(t, selectedProbePayload["effective_profile_id"]); got == activeID {
		t.Fatalf("expected selected/effective profile %d to remain distinct from active profile %d", got, activeID)
	}
}

func TestManagementRouteSeamKeepsEffectiveProfileScopeAfterRouterAuthSplit(t *testing.T) {
	harness := newProfileContractHarness(t)

	activeResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/profiles/active", nil, nil)
	assertStatus(t, activeResponse, http.StatusOK)
	var activePayload map[string]any
	decodeJSONResponse(t, activeResponse, &activePayload)
	activeID := jsonInt(t, activePayload["id"])

	selectedProfileResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/profiles",
		map[string]any{"name": "Management Route Seam Profile", "description": "management-route-seam"},
		nil,
	)
	assertStatus(t, selectedProfileResponse, http.StatusCreated)
	var selectedPayload map[string]any
	decodeJSONResponse(t, selectedProfileResponse, &selectedPayload)
	selectedID := jsonInt(t, selectedPayload["id"])
	if selectedID == activeID {
		t.Fatalf("expected seam-selected profile id %d to differ from active profile id %d", selectedID, activeID)
	}

	insertRequestLogSummaryRow(t, harness, 6100, activeID, "management-seam-active", "openai", 12, 41, http.StatusOK, 120, 10, 20, 30, fixedS15Now.Add(-2*time.Minute))
	insertRequestLogSummaryRow(t, harness, 6101, selectedID, "management-seam-selected", "anthropic", 13, 42, http.StatusServiceUnavailable, 260, 5, 8, 13, fixedS15Now.Add(-1*time.Minute))
	loginWithVerifiedAuth(t, harness, "profile-seam-admin", "profile-seam-password-123", "profile-seam@example.com")

	missingHeader := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/summary?group_by=api_family", nil, nil)
	assertErrorResponseCode(t, missingHeader, http.StatusBadRequest, profiledomain.ScopeErrorCodeHeaderMissing, fmt.Sprintf("%s header is required", profiledomain.ProfileIDHeader))
	invalidHeader := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/summary?group_by=api_family", nil, map[string]string{profiledomain.ProfileIDHeader: "not-an-integer"})
	assertErrorResponseCode(t, invalidHeader, http.StatusBadRequest, profiledomain.ScopeErrorCodeHeaderInvalid, fmt.Sprintf("%s must be an integer", profiledomain.ProfileIDHeader))
	nonPositiveHeader := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/summary?group_by=api_family", nil, map[string]string{profiledomain.ProfileIDHeader: "0"})
	assertErrorResponseCode(t, nonPositiveHeader, http.StatusBadRequest, profiledomain.ScopeErrorCodeHeaderNonPositive, fmt.Sprintf("%s must be a positive integer", profiledomain.ProfileIDHeader))
	missingProfile := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/summary?group_by=api_family", nil, modelHeader(999999))
	assertErrorResponseCode(t, missingProfile, http.StatusNotFound, profiledomain.ScopeErrorCodeProfileNotFound, "Profile 999999 not found")

	activeSummary := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/summary?group_by=api_family", nil, modelHeader(activeID))
	assertStatus(t, activeSummary, http.StatusOK)
	var activeSummaryPayload map[string]any
	decodeJSONResponse(t, activeSummary, &activeSummaryPayload)
	if jsonInt(t, activeSummaryPayload["total_requests"]) != 1 || jsonInt(t, activeSummaryPayload["success_count"]) != 1 || jsonInt(t, activeSummaryPayload["error_count"]) != 0 {
		t.Fatalf("expected active-profile management summary to stay scoped to the active profile row, got %+v", activeSummaryPayload)
	}
	activeGroups := activeSummaryPayload["groups"].([]any)
	if len(activeGroups) != 1 || asMap(t, activeGroups[0])["key"] != "openai" {
		t.Fatalf("expected active-profile management summary to keep the openai group, got %+v", activeSummaryPayload)
	}

	selectedSummary := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/summary?group_by=api_family", nil, modelHeader(selectedID))
	assertStatus(t, selectedSummary, http.StatusOK)
	var selectedSummaryPayload map[string]any
	decodeJSONResponse(t, selectedSummary, &selectedSummaryPayload)
	if jsonInt(t, selectedSummaryPayload["total_requests"]) != 1 || jsonInt(t, selectedSummaryPayload["success_count"]) != 0 || jsonInt(t, selectedSummaryPayload["error_count"]) != 1 {
		t.Fatalf("expected selected-profile management summary to keep effective-profile scoping after the split, got %+v", selectedSummaryPayload)
	}
	selectedGroups := selectedSummaryPayload["groups"].([]any)
	if len(selectedGroups) != 1 || asMap(t, selectedGroups[0])["key"] != "anthropic" {
		t.Fatalf("expected selected-profile management summary to keep the anthropic group, got %+v", selectedSummaryPayload)
	}
}

func newProfileContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "profile_contract_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})

	startupService, err := startup.New(startup.Options{
		DatabaseURL:         sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey: "profile-contract-secret",
	})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup service: %v", err)
	}

	settings := config.Settings{
		Host:                "127.0.0.1",
		Port:                8000,
		AppEnv:              config.EnvironmentProduction,
		DatabaseURL:         sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey: "profile-contract-secret",
		CORSAllowedOrigins:  "http://localhost:5173,http://127.0.0.1:5173",
		AuthJWTSecret:              "profile-contract-jwt-secret",
		AuthAccessTokenTTLSeconds:  900,
		AuthRefreshTokenTTLSeconds: 604800,
		AuthResetCodeTTLSeconds:    600,
		AuthCookieName:             "prism_access_token",
		AuthRefreshCookieName:      "prism_refresh_token",
		AuthCookieSecure:           false,
	}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	authService, err := managementauth.NewService(settings, managementauth.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build auth service: %v", err)
	}
	t.Cleanup(authService.Close)
	profilesService, err := managementprofiles.NewService(settings, managementprofiles.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build profiles service: %v", err)
	}
	t.Cleanup(profilesService.Close)
	statsService, err := managementstats.NewService(settings, managementstats.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build stats service: %v", err)
	}
	t.Cleanup(statsService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{
		Version:         "profile-contract-test",
		AuthService:     authService,
		ProfilesService: profilesService,
		StatsService:    statsService,
	})
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar
	return &contractHarness{client: client, conn: conn, dsn: settings.DatabaseURL, mailer: nil, server: server, service: authService, url: server.URL}
}

func scopeProbeRequest(t *testing.T, rawURL string, headerValue *string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("build scope probe request: %v", err)
	}
	if headerValue != nil {
		request.Header.Set(profiledomain.ProfileIDHeader, *headerValue)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("perform scope probe request: %v", err)
	}
	t.Cleanup(func() {
		_ = response.Body.Close()
	})
	return response
}

func asMap(t *testing.T, value any) map[string]any {
	t.Helper()
	typed, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", value)
	}
	return typed
}

func jsonInt(t *testing.T, value any) int {
	t.Helper()
	floatValue, ok := value.(float64)
	if !ok {
		t.Fatalf("expected JSON number, got %T", value)
	}
	return int(floatValue)
}

func profileListContainsID(t *testing.T, profiles []any, wantID int) bool {
	t.Helper()
	for _, rawProfile := range profiles {
		profile := asMap(t, rawProfile)
		if jsonInt(t, profile["id"]) == wantID {
			return true
		}
	}
	return false
}

func profileSliceContainsID(t *testing.T, profiles []map[string]any, wantID int) bool {
	t.Helper()
	for _, profile := range profiles {
		if jsonInt(t, profile["id"]) == wantID {
			return true
		}
	}
	return false
}
