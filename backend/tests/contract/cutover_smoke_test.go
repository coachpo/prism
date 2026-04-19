package contract_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/migrate"
	"github.com/coachpo/prism/backend/internal/platform/startup"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

const cutoverLegacyAlembicTip = "0026_request_log_audit_enabled_at_request"

func TestCutoverSmoke(t *testing.T) {
	harness := newCutoverSmokeHarness(t)
	expectedVersion := readBackendVersion(t)

	healthResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/health", nil, nil)
	assertStatus(t, healthResponse, http.StatusOK)
	var healthPayload struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	decodeJSONResponse(t, healthResponse, &healthPayload)
	if healthPayload.Status != "ok" {
		t.Fatalf("expected /health status ok, got %+v", healthPayload)
	}
	if healthPayload.Version != expectedVersion {
		t.Fatalf("expected /health version %q, got %q", expectedVersion, healthPayload.Version)
	}

	bootstrapResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/profiles/bootstrap", nil, nil)
	assertStatus(t, bootstrapResponse, http.StatusOK)

	var bootstrapPayload map[string]any
	decodeJSONResponse(t, bootstrapResponse, &bootstrapPayload)
	activeProfile, ok := bootstrapPayload["active_profile"].(map[string]any)
	if !ok || activeProfile == nil {
		t.Fatalf("expected bootstrap active_profile to be present on cutover smoke path, got %+v", bootstrapPayload)
	}
	profileLimits := asMap(t, bootstrapPayload["profile_limits"])
	if got := jsonInt(t, profileLimits["max_profiles"]); got != profiledomain.MaxNonDeletedProfiles {
		t.Fatalf("expected bootstrap max_profiles %d, got %+v", profiledomain.MaxNonDeletedProfiles, profileLimits)
	}
	profilesList, ok := bootstrapPayload["profiles"].([]any)
	if !ok || len(profilesList) == 0 {
		t.Fatalf("expected bootstrap profiles list on cutover smoke path, got %+v", bootstrapPayload)
	}
	activeID := jsonInt(t, activeProfile["id"])
	if !profileListContainsID(t, profilesList, activeID) {
		t.Fatalf("expected bootstrap profiles to include active profile id %d", activeID)
	}

	vendorsResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/vendors", nil, nil)
	assertStatus(t, vendorsResponse, http.StatusOK)

	var vendorsPayload []map[string]any
	decodeJSONResponse(t, vendorsResponse, &vendorsPayload)
	vendorsByKey := make(map[string]map[string]any, len(vendorsPayload))
	for _, vendor := range vendorsPayload {
		key, _ := vendor["key"].(string)
		if key != "" {
			vendorsByKey[key] = vendor
		}
	}
	for _, expected := range startup.DefaultVendors {
		vendor, ok := vendorsByKey[expected.Key]
		if !ok {
			t.Fatalf("expected seeded vendor %q in cutover smoke payload, got %+v", expected.Key, vendorsPayload)
		}
		if vendor["name"] != expected.Name || vendor["description"] != expected.Description || vendor["icon_key"] != expected.IconKey {
			t.Fatalf("expected seeded vendor payload for %q to match startup defaults, got %+v", expected.Key, vendor)
		}
		if vendor["is_readonly"] != true {
			t.Fatalf("expected seeded vendor %q to remain readonly after cutover, got %+v", expected.Key, vendor)
		}
		if _, ok := vendor["audit_enabled"]; !ok {
			t.Fatalf("expected seeded vendor %q to expose audit_enabled, got %+v", expected.Key, vendor)
		}
		if _, ok := vendor["audit_capture_bodies"]; !ok {
			t.Fatalf("expected seeded vendor %q to expose audit_capture_bodies, got %+v", expected.Key, vendor)
		}
	}
}

func newCutoverSmokeHarness(t *testing.T) *contractHarness {
	t.Helper()

	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	databaseName := "cutover_smoke_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() {
		conn.Close(context.Background())
	})

	applyCutoverArtifact(t, testContext, conn)
	seedCutoverLegacyAlembicVersion(t, testContext, conn)

	startupService, err := startup.New(startup.Options{
		DatabaseURL:         sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey: "cutover-contract-secret",
	})
	if err != nil {
		t.Fatalf("build cutover startup service: %v", err)
	}
	startupResult, err := startupService.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("run cutover startup service: %v", err)
	}
	if startupResult.Migration.Outcome != migrate.OutcomeStamp {
		t.Fatalf("expected cutover smoke startup to stamp baseline, got %q", startupResult.Migration.Outcome)
	}
	if got, want := startupResult.Migration.Versions, []string{migrate.DefaultBaselineVersion}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("expected cutover smoke stamped versions %v, got %v", want, got)
	}

	runner, err := migrate.New(migrate.Options{})
	if err != nil {
		t.Fatalf("build cutover migration runner: %v", err)
	}
	match, actual, expected, err := runner.ApplicationSchemaMatchesCutover(testContext, conn)
	if err != nil {
		t.Fatalf("compare stamped cutover clone to artifact: %v", err)
	}
	if !match {
		t.Fatalf("expected stamped cutover clone to match artifact\n--- actual ---\n%s\n--- expected ---\n%s", actual, expected)
	}
	assertCutoverSmokeHistory(t, testContext, conn, []string{migrate.DefaultBaselineVersion})
	assertCutoverLegacyAlembicVersion(t, testContext, conn, cutoverLegacyAlembicTip)

	settings := config.Settings{
		Host:                       "127.0.0.1",
		Port:                       8000,
		AppEnv:                     config.EnvironmentProduction,
		DatabaseURL:                sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey:        "cutover-contract-secret",
		CORSAllowedOrigins:         "http://localhost:5173,http://127.0.0.1:5173",
		AuthJWTSecret:              "cutover-contract-jwt-secret",
		AuthAccessTokenTTLSeconds:  900,
		AuthRefreshTokenTTLSeconds: 604800,
		AuthResetCodeTTLSeconds:    600,
		AuthCookieName:             "prism_access_token",
		AuthRefreshCookieName:      "prism_refresh_token",
		AuthCookieSecure:           false,
	}
	httpServer, err := platformhttp.NewServer(settings)
	if err != nil {
		t.Fatalf("build cutover smoke server: %v", err)
	}
	t.Cleanup(func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownContext)
	})
	server := httptest.NewServer(httpServer.Handler)
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar

	return &contractHarness{client: client, conn: conn, dsn: settings.DatabaseURL, server: server, url: server.URL}
}

func applyCutoverArtifact(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()

	path := migrate.DefaultCutoverSchemaPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cutover artifact %s: %v", path, err)
	}

	for _, statement := range splitCutoverSQLStatements(string(raw)) {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("apply cutover artifact %s: %v\nstatement:\n%s", path, err, statement)
		}
	}
}

func splitCutoverSQLStatements(sql string) []string {
	rawStatements := strings.Split(sql, ";")
	statements := make([]string, 0, len(rawStatements))
	for _, rawStatement := range rawStatements {
		statement := strings.TrimSpace(rawStatement)
		if statement == "" {
			continue
		}
		statements = append(statements, statement)
	}
	return statements
}

func seedCutoverLegacyAlembicVersion(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()

	statements := []string{
		`CREATE TABLE IF NOT EXISTS alembic_version (version_num VARCHAR(128) NOT NULL PRIMARY KEY)`,
		`INSERT INTO alembic_version (version_num) VALUES ('` + cutoverLegacyAlembicTip + `')`,
	}
	for _, statement := range statements {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("seed cutover legacy alembic version: %v", err)
		}
	}
}

func assertCutoverSmokeHistory(t *testing.T, ctx context.Context, conn *pgx.Conn, expected []string) {
	t.Helper()

	rows, err := conn.Query(ctx, `SELECT version FROM prism_schema_migrations ORDER BY version ASC`)
	if err != nil {
		t.Fatalf("query prism migration history: %v", err)
	}
	defer rows.Close()

	versions := []string{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			t.Fatalf("scan prism migration history: %v", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate prism migration history: %v", err)
	}

	if len(versions) != len(expected) {
		t.Fatalf("expected prism migration history %v, got %v", expected, versions)
	}
	for index := range expected {
		if versions[index] != expected[index] {
			t.Fatalf("expected prism migration history %v, got %v", expected, versions)
		}
	}
}

func assertCutoverLegacyAlembicVersion(t *testing.T, ctx context.Context, conn *pgx.Conn, expected string) {
	t.Helper()

	var version string
	if err := conn.QueryRow(ctx, `SELECT version_num FROM alembic_version`).Scan(&version); err != nil {
		t.Fatalf("read legacy alembic version row: %v", err)
	}
	if version != expected {
		t.Fatalf("expected legacy alembic version %q, got %q", expected, version)
	}
}
