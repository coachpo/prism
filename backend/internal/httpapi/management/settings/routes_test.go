package settings

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/migrate"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

var auditSettingsRoutePostgres struct {
	once     sync.Once
	hostPort string
	err      error
}

func TestRetentionDaysForTableLoadbalanceEvents(t *testing.T) {
	value := 8
	settingsRow := logRetentionSettingsRow{LoadbalanceEventsRetentionDays: &value}

	got := retentionDaysForTable(settingsRow, "loadbalance_events")
	if got == nil || *got != 8 {
		t.Fatalf("expected loadbalance events retention days to resolve to 8, got %+v", got)
	}
}

func TestAuditSettingsDefaultsStableFamilyOrder(t *testing.T) {
	response := buildAuditSettingsResponse(7, []auditSettingsRow{{APIFamily: "gemini", AuditEnabled: true}})

	if response.ProfileID != 7 || len(response.Settings) != 3 {
		t.Fatalf("expected three audit settings for profile 7, got %+v", response)
	}
	wantFamilies := []string{"openai", "anthropic", "gemini"}
	for index, family := range wantFamilies {
		setting := response.Settings[index]
		if setting.APIFamily != family {
			t.Fatalf("expected family order %v, got %+v", wantFamilies, response.Settings)
		}
		if family != "gemini" && (setting.AuditEnabled || setting.AuditCaptureBodies) {
			t.Fatalf("expected missing family %s to default false/false, got %+v", family, setting)
		}
	}
	if !response.Settings[2].AuditEnabled || response.Settings[2].AuditCaptureBodies {
		t.Fatalf("expected existing gemini row to preserve booleans, got %+v", response.Settings[2])
	}
}

func TestAuditSettingsValidationNormalizesAndOrdersFamilies(t *testing.T) {
	request := auditSettingsUpdateRequest{Settings: []auditSetting{
		{APIFamily: " Gemini ", AuditEnabled: true, AuditCaptureBodies: true},
		{APIFamily: "openai", AuditEnabled: false},
		{APIFamily: "ANTHROPIC", AuditEnabled: true},
	}}

	if err := normalizeAndValidateAuditSettingsRequest(&request); err != nil {
		t.Fatalf("expected valid audit settings, got %v", err)
	}
	wantFamilies := []string{"openai", "anthropic", "gemini"}
	for index, family := range wantFamilies {
		if request.Settings[index].APIFamily != family {
			t.Fatalf("expected canonical family order %v, got %+v", wantFamilies, request.Settings)
		}
	}
}

func TestAuditSettingsValidationRejectsInvalidPayloads(t *testing.T) {
	testCases := []struct {
		name    string
		request auditSettingsUpdateRequest
		detail  string
	}{
		{
			name: "unknown family",
			request: auditSettingsUpdateRequest{Settings: []auditSetting{
				{APIFamily: "openai"},
				{APIFamily: "anthropic"},
				{APIFamily: "mistral"},
			}},
			detail: "not supported",
		},
		{
			name: "duplicate family",
			request: auditSettingsUpdateRequest{Settings: []auditSetting{
				{APIFamily: "openai"},
				{APIFamily: "openai"},
				{APIFamily: "gemini"},
			}},
			detail: "Duplicate audit setting for api_family=openai",
		},
		{
			name: "missing family",
			request: auditSettingsUpdateRequest{Settings: []auditSetting{
				{APIFamily: "openai"},
				{APIFamily: "anthropic"},
			}},
			detail: "settings must include exactly openai, anthropic, and gemini",
		},
		{
			name: "capture requires enabled",
			request: auditSettingsUpdateRequest{Settings: []auditSetting{
				{APIFamily: "openai", AuditCaptureBodies: true},
				{APIFamily: "anthropic"},
				{APIFamily: "gemini"},
			}},
			detail: "audit_capture_bodies requires audit_enabled",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := normalizeAndValidateAuditSettingsRequest(&testCase.request)
			if err == nil || !strings.Contains(err.Error(), testCase.detail) {
				t.Fatalf("expected error containing %q, got %v", testCase.detail, err)
			}
		})
	}
}

func TestAuditSettingsRouteDefaultsReplacementValidationRollback(t *testing.T) {
	ctx, conn, dsn := auditSettingsRouteMigratedDatabase(t, "audit_settings_route")
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	profileID := auditSettingsRouteInsertProfile(t, ctx, conn, "Audit Settings Route", now)
	handler := auditSettingsRouteRouter(t, ctx, dsn, now)

	initial := auditSettingsRouteRequest(t, handler, http.MethodGet, "/settings/audit", profileID, nil)
	auditSettingsRouteRequireStatus(t, initial, http.StatusOK)
	var payload auditSettingsResponse
	auditSettingsRouteDecode(t, initial, &payload)
	auditSettingsRouteAssertPayload(t, payload, profileID, [][2]bool{{false, false}, {false, false}, {false, false}})

	updated := auditSettingsRouteRequest(t, handler, http.MethodPut, "/settings/audit", profileID, map[string]any{"settings": []map[string]any{
		{"api_family": "gemini", "audit_enabled": true, "audit_capture_bodies": true},
		{"api_family": "openai", "audit_enabled": true, "audit_capture_bodies": false},
		{"api_family": "anthropic", "audit_enabled": false, "audit_capture_bodies": false},
	}})
	auditSettingsRouteRequireStatus(t, updated, http.StatusOK)
	auditSettingsRouteDecode(t, updated, &payload)
	auditSettingsRouteAssertPayload(t, payload, profileID, [][2]bool{{true, false}, {false, false}, {true, true}})
	auditSettingsRouteAssertRows(t, ctx, conn, profileID, map[string][2]bool{"openai": {true, false}, "anthropic": {false, false}, "gemini": {true, true}})

	invalid := auditSettingsRouteRequest(t, handler, http.MethodPut, "/settings/audit", profileID, map[string]any{"settings": []map[string]any{
		{"api_family": "openai", "audit_enabled": false, "audit_capture_bodies": true},
		{"api_family": "anthropic", "audit_enabled": false, "audit_capture_bodies": false},
		{"api_family": "gemini", "audit_enabled": false, "audit_capture_bodies": false},
	}})
	auditSettingsRouteRequireStatus(t, invalid, http.StatusBadRequest)
	auditSettingsRouteAssertRows(t, ctx, conn, profileID, map[string][2]bool{"openai": {true, false}, "anthropic": {false, false}, "gemini": {true, true}})

	otherProfileID := auditSettingsRouteInsertProfile(t, ctx, conn, "Audit Settings Route Other", now)
	other := auditSettingsRouteRequest(t, handler, http.MethodGet, "/settings/audit", otherProfileID, nil)
	auditSettingsRouteRequireStatus(t, other, http.StatusOK)
	auditSettingsRouteDecode(t, other, &payload)
	auditSettingsRouteAssertPayload(t, payload, otherProfileID, [][2]bool{{false, false}, {false, false}, {false, false}})
}

func auditSettingsRouteRouter(t *testing.T, ctx context.Context, dsn string, now time.Time) http.Handler {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	service, err := NewService(config.Settings{}, Options{Pool: pool, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("build settings service: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return router
}

func auditSettingsRouteRequest(t *testing.T, handler http.Handler, method string, path string, profileID int, body any) *httptest.ResponseRecorder {
	t.Helper()
	reader := bytes.NewReader(nil)
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set(profiledomain.ProfileIDHeader, fmt.Sprintf("%d", profileID))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func auditSettingsRouteDecode(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode JSON response: %v\nbody=%s", err, response.Body.String())
	}
}

func auditSettingsRouteRequireStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("expected status %d, got %d body=%s", status, response.Code, response.Body.String())
	}
}

func auditSettingsRouteAssertPayload(t *testing.T, payload auditSettingsResponse, profileID int, want [][2]bool) {
	t.Helper()
	wantFamilies := []string{"openai", "anthropic", "gemini"}
	if payload.ProfileID != profileID || len(payload.Settings) != len(wantFamilies) {
		t.Fatalf("expected profile %d with three settings, got %+v", profileID, payload)
	}
	for index, family := range wantFamilies {
		setting := payload.Settings[index]
		if setting.APIFamily != family || setting.AuditEnabled != want[index][0] || setting.AuditCaptureBodies != want[index][1] {
			t.Fatalf("unexpected audit payload %+v, want families %v values %+v", payload, wantFamilies, want)
		}
	}
}

func auditSettingsRouteAssertRows(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, want map[string][2]bool) {
	t.Helper()
	rows, err := conn.Query(ctx, `SELECT api_family, audit_enabled, audit_capture_bodies FROM profile_api_family_audit_settings WHERE profile_id = $1`, profileID)
	if err != nil {
		t.Fatalf("query audit settings: %v", err)
	}
	defer rows.Close()
	got := map[string][2]bool{}
	for rows.Next() {
		var family string
		var enabled bool
		var capture bool
		if err := rows.Scan(&family, &enabled, &capture); err != nil {
			t.Fatalf("scan audit setting: %v", err)
		}
		got[family] = [2]bool{enabled, capture}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit settings: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected audit settings rows %+v, got %+v", want, got)
	}
	for family, values := range want {
		if got[family] != values {
			t.Fatalf("expected audit settings rows %+v, got %+v", want, got)
		}
	}
}

func auditSettingsRouteInsertProfile(t *testing.T, ctx context.Context, conn *pgx.Conn, name string, now time.Time) int {
	t.Helper()
	var profileID int
	if err := conn.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, NULL, FALSE, FALSE, TRUE, 1, $2, $2) RETURNING id`, name, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	return profileID
}

func auditSettingsRouteMigratedDatabase(t *testing.T, name string) (context.Context, *pgx.Conn, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	harness := auditSettingsRouteHarness(t)
	databaseName := fmt.Sprintf("%s_%s", name, auditSettingsRouteRandomSuffix(t))
	dsn := harness.connectionString(databaseName)
	conn := harness.openDatabase(t, ctx, databaseName)
	runner, err := migrate.New(migrate.Options{})
	if err != nil {
		t.Fatalf("build migration runner: %v", err)
	}
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations for %s: %v", databaseName, err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return ctx, conn, dsn
}

type auditSettingsRoutePostgresHarness struct{ hostPort string }

func auditSettingsRouteHarness(t *testing.T) auditSettingsRoutePostgresHarness {
	t.Helper()
	auditSettingsRoutePostgres.once.Do(func() {
		containerName := "prism-settings-audit-" + auditSettingsRouteRandomSuffix(t)
		if _, err := auditSettingsRouteDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
			auditSettingsRoutePostgres.err = err
			return
		}
		hostPort, err := auditSettingsRouteDockerPort(containerName)
		if err != nil {
			auditSettingsRoutePostgres.err = err
			return
		}
		if err := auditSettingsRouteWaitForPostgres(hostPort); err != nil {
			auditSettingsRoutePostgres.err = err
			return
		}
		auditSettingsRoutePostgres.hostPort = hostPort
	})
	if auditSettingsRoutePostgres.err != nil {
		t.Fatalf("start postgres harness: %v", auditSettingsRoutePostgres.err)
	}
	return auditSettingsRoutePostgresHarness{hostPort: auditSettingsRoutePostgres.hostPort}
}

func (h auditSettingsRoutePostgresHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := auditSettingsRouteConnect(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+auditSettingsRouteQuoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+auditSettingsRouteQuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return auditSettingsRouteConnect(t, ctx, h.connectionString(databaseName))
}

func (h auditSettingsRoutePostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func auditSettingsRouteConnect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres %s: %v", dsn, err)
	}
	return conn
}

func auditSettingsRouteDockerPort(containerName string) (string, error) {
	output, err := auditSettingsRouteDockerCommand(context.Background(), "port", containerName, "5432/tcp")
	if err != nil {
		return "", err
	}
	_, port, err := net.SplitHostPort(strings.TrimSpace(strings.Split(output, "\n")[0]))
	return port, err
}

func auditSettingsRouteWaitForPostgres(hostPort string) error {
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/postgres?sslmode=disable", hostPort))
		if err == nil {
			_ = conn.Close(ctx)
			cancel()
			return nil
		}
		cancel()
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("postgres container on port %s did not become ready in time", hostPort)
}

func auditSettingsRouteDockerCommand(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func auditSettingsRouteRandomSuffix(t *testing.T) string {
	t.Helper()
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}
	return hex.EncodeToString(raw[:])
}

func auditSettingsRouteQuoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
