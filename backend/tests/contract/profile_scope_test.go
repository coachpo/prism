package contract_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	managementprofiles "github.com/coachpo/prism/backend/internal/httpapi/management/profiles"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestProfileBootstrap(t *testing.T) {
	t.Run("bootstrap payload and nullable schema", func(t *testing.T) {
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

		assertBootstrapSchemaAllowsNullActiveProfile(t)
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
				_ = json.NewEncoder(w).Encode(map[string]string{"detail": httpErr.Detail})
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
	assertErrorResponse(t, missingHeader, http.StatusBadRequest, fmt.Sprintf("%s header is required", profiledomain.ProfileIDHeader))

	invalidHeaderValue := "not-an-integer"
	invalidHeader := scopeProbeRequest(t, probeServer.URL, &invalidHeaderValue)
	assertErrorResponse(t, invalidHeader, http.StatusBadRequest, fmt.Sprintf("%s must be an integer", profiledomain.ProfileIDHeader))

	negativeHeaderValue := "-1"
	negativeHeader := scopeProbeRequest(t, probeServer.URL, &negativeHeaderValue)
	assertErrorResponse(t, negativeHeader, http.StatusBadRequest, fmt.Sprintf("%s must be a positive integer", profiledomain.ProfileIDHeader))

	missingProfileValue := "999999"
	missingProfile := scopeProbeRequest(t, probeServer.URL, &missingProfileValue)
	assertErrorResponse(t, missingProfile, http.StatusNotFound, "Profile 999999 not found")

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

func newProfileContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "profile_contract_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() {
		conn.Close(context.Background())
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
		Host:                       "127.0.0.1",
		Port:                       8000,
		AppEnv:                     config.EnvironmentProduction,
		DatabaseURL:                sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey:        "profile-contract-secret",
		CORSAllowedOrigins:         "http://localhost:5173,http://127.0.0.1:5173",
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
	profilesService, err := managementprofiles.NewService(settings, managementprofiles.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build profiles service: %v", err)
	}
	t.Cleanup(profilesService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{
		Version:         "profile-contract-test",
		ProfilesService: profilesService,
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
	return &contractHarness{client: client, conn: conn, dsn: settings.DatabaseURL, mailer: nil, server: server, service: nil, url: server.URL}
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

func assertBootstrapSchemaAllowsNullActiveProfile(t *testing.T) {
	t.Helper()
	spec := loadOpenAPISpec(t)
	paths := asMap(t, spec["paths"])
	bootstrapPath := asMap(t, paths["/api/profiles/bootstrap"])
	getOperation := asMap(t, bootstrapPath["get"])
	responses := asMap(t, getOperation["responses"])
	okResponse := asMap(t, responses["200"])
	content := asMap(t, okResponse["content"])
	appJSON := asMap(t, content["application/json"])
	schema := resolveSchema(t, spec, appJSON["schema"])
	properties := asMap(t, schema["properties"])
	activeProfile := asMap(t, properties["active_profile"])
	if !schemaAllowsNull(activeProfile) {
		t.Fatalf("expected bootstrap active_profile schema to allow null, got %+v", activeProfile)
	}
}

func loadOpenAPISpec(t *testing.T) map[string]any {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve contract test file path")
	}
	openAPIPath := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", "openapi.json"))
	raw, err := os.ReadFile(openAPIPath)
	if err != nil {
		t.Fatalf("read docs/openapi.json: %v", err)
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decode docs/openapi.json: %v", err)
	}
	return spec
}

func resolveSchema(t *testing.T, spec map[string]any, value any) map[string]any {
	t.Helper()
	schema := asMap(t, value)
	ref, ok := schema["$ref"].(string)
	if !ok {
		return schema
	}
	if !strings.HasPrefix(ref, "#/") {
		t.Fatalf("unsupported schema ref %q", ref)
	}
	current := any(spec)
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		current = asMap(t, current)[part]
	}
	return asMap(t, current)
}

func schemaAllowsNull(schema map[string]any) bool {
	if nullable, ok := schema["nullable"].(bool); ok && nullable {
		return true
	}
	for _, key := range []string{"anyOf", "oneOf"} {
		rawOptions, ok := schema[key].([]any)
		if !ok {
			continue
		}
		for _, rawOption := range rawOptions {
			option, ok := rawOption.(map[string]any)
			if !ok {
				continue
			}
			if optionType, ok := option["type"].(string); ok && optionType == "null" {
				return true
			}
		}
	}
	return false
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
