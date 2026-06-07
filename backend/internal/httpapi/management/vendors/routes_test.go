package vendors

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

var vendorsRoutePostgres struct {
	once     sync.Once
	hostPort string
	err      error
}

func TestVendorRoutesReadonlyGlobalUsageAndDeleteContracts(t *testing.T) {
	ctx, conn, dsn := vendorsRouteMigratedDatabase(t, "vendors_routes_contract")
	now := time.Date(2026, time.June, 7, 15, 40, 0, 0, time.UTC)
	router := vendorsRouteRouter(t, ctx, dsn, now)
	readonlyID := vendorsRouteInsertVendor(t, ctx, conn, "openai", "OpenAI", "System OpenAI", "openai", true, false, now)

	readonlyCreate := vendorsRouteRequest(t, router, http.MethodPost, "/vendors", 0, map[string]any{"key": " OpenAI ", "name": "Duplicate OpenAI"})
	vendorsRouteRequireStatus(t, readonlyCreate, http.StatusForbidden)
	vendorsRouteRequireDetail(t, readonlyCreate, "Vendor 'openai' is readonly and cannot be modified here")

	createdResponse := vendorsRouteRequest(t, router, http.MethodPost, "/vendors", 0, map[string]any{"key": " Custom-Key ", "name": " Custom Vendor ", "description": " ", "icon_key": " Anthropic "})
	vendorsRouteRequireStatus(t, createdResponse, http.StatusCreated)
	var created vendorResponse
	vendorsRouteDecode(t, createdResponse, &created)
	if created.Key != "custom-key" || created.Name != "Custom Vendor" || created.Description != nil || created.IconKey == nil || *created.IconKey != "anthropic" || created.IsReadonly || created.AuditEnabled || !created.AuditCaptureBodies {
		t.Fatalf("unexpected created vendor response: %+v", created)
	}

	duplicateName := vendorsRouteRequest(t, router, http.MethodPost, "/vendors", 0, map[string]any{"key": "another-custom", "name": "Custom Vendor"})
	vendorsRouteRequireStatus(t, duplicateName, http.StatusConflict)
	vendorsRouteRequireDetail(t, duplicateName, "Vendor name 'Custom Vendor' already exists")

	readonlyAuditUpdate := vendorsRouteRequest(t, router, http.MethodPatch, fmt.Sprintf("/vendors/%d", readonlyID), 0, map[string]any{"audit_enabled": false, "audit_capture_bodies": true})
	vendorsRouteRequireStatus(t, readonlyAuditUpdate, http.StatusOK)
	var readonlyUpdated vendorResponse
	vendorsRouteDecode(t, readonlyAuditUpdate, &readonlyUpdated)
	if !readonlyUpdated.IsReadonly || readonlyUpdated.Key != "openai" || readonlyUpdated.AuditEnabled || !readonlyUpdated.AuditCaptureBodies {
		t.Fatalf("unexpected readonly audit update response: %+v", readonlyUpdated)
	}

	readonlyIdentityUpdate := vendorsRouteRequest(t, router, http.MethodPatch, fmt.Sprintf("/vendors/%d", readonlyID), 0, map[string]any{"name": "OpenAI Renamed"})
	vendorsRouteRequireStatus(t, readonlyIdentityUpdate, http.StatusForbidden)
	vendorsRouteRequireDetail(t, readonlyIdentityUpdate, "Vendor 'openai' is readonly and cannot be modified here")

	readonlyKeyUpdate := vendorsRouteRequest(t, router, http.MethodPatch, fmt.Sprintf("/vendors/%d", created.ID), 0, map[string]any{"key": "gemini"})
	vendorsRouteRequireStatus(t, readonlyKeyUpdate, http.StatusForbidden)
	vendorsRouteRequireDetail(t, readonlyKeyUpdate, "Vendor 'gemini' is readonly and cannot be modified here")

	customUpdate := vendorsRouteRequest(t, router, http.MethodPatch, fmt.Sprintf("/vendors/%d", created.ID), 0, map[string]any{"description": " Custom description ", "audit_enabled": true, "audit_capture_bodies": false})
	vendorsRouteRequireStatus(t, customUpdate, http.StatusOK)
	var updated vendorResponse
	vendorsRouteDecode(t, customUpdate, &updated)
	if updated.Description == nil || *updated.Description != "Custom description" || !updated.AuditEnabled || updated.AuditCaptureBodies {
		t.Fatalf("unexpected custom update response: %+v", updated)
	}

	firstProfile := vendorsRouteInsertProfile(t, ctx, conn, "Vendor usage first", now)
	secondProfile := vendorsRouteInsertProfile(t, ctx, conn, "Vendor usage second", now)
	firstModelID := vendorsRouteInsertModel(t, ctx, conn, firstProfile, created.ID, "openai", "vendor-model-a", "Vendor Model A", true, now)
	secondModelID := vendorsRouteInsertModel(t, ctx, conn, secondProfile, created.ID, "anthropic", "vendor-model-b", "Vendor Model B", false, now)

	usage := vendorsRouteRequest(t, router, http.MethodGet, fmt.Sprintf("/vendors/%d/models", created.ID), secondProfile, nil)
	vendorsRouteRequireStatus(t, usage, http.StatusOK)
	var usageItems []vendorModelUsageItem
	vendorsRouteDecode(t, usage, &usageItems)
	if len(usageItems) != 2 || usageItems[0].ModelConfigID != firstModelID || usageItems[0].ProfileID != firstProfile || usageItems[1].ModelConfigID != secondModelID || usageItems[1].ProfileID != secondProfile || usageItems[1].IsEnabled {
		t.Fatalf("expected global vendor model usage ordered by profile/model, got %+v", usageItems)
	}

	readonlyDelete := vendorsRouteRequest(t, router, http.MethodDelete, fmt.Sprintf("/vendors/%d", readonlyID), 0, nil)
	vendorsRouteRequireStatus(t, readonlyDelete, http.StatusForbidden)
	vendorsRouteRequireDetail(t, readonlyDelete, "Vendor 'openai' is readonly and cannot be modified here")

	deleted := vendorsRouteRequest(t, router, http.MethodDelete, fmt.Sprintf("/vendors/%d", created.ID), 0, nil)
	vendorsRouteRequireStatus(t, deleted, http.StatusNoContent)
	if deleted.Body.Len() != 0 {
		t.Fatalf("expected empty delete body, got %s", deleted.Body.String())
	}
	var nulledVendorCount int
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM model_configs WHERE id IN ($1, $2) AND vendor_id IS NULL`, firstModelID, secondModelID).Scan(&nulledVendorCount); err != nil {
		t.Fatalf("query nulled model vendors: %v", err)
	}
	if nulledVendorCount != 2 {
		t.Fatalf("expected vendor delete to preserve models with NULL vendor_id, got %d", nulledVendorCount)
	}
}

func vendorsRouteRouter(t *testing.T, ctx context.Context, dsn string, now time.Time) http.Handler {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	service, err := NewService(config.Settings{}, Options{Pool: pool, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("build vendor service: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return router
}

func vendorsRouteRequest(t *testing.T, handler http.Handler, method string, path string, profileID int, body any) *httptest.ResponseRecorder {
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
	if profileID > 0 {
		request.Header.Set(profiledomain.ProfileIDHeader, fmt.Sprintf("%d", profileID))
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func vendorsRouteDecode(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode JSON response: %v\nbody=%s", err, response.Body.String())
	}
}

func vendorsRouteRequireStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("expected status %d, got %d body=%s", status, response.Code, response.Body.String())
	}
}

func vendorsRouteRequireDetail(t *testing.T, response *httptest.ResponseRecorder, detail string) {
	t.Helper()
	var body map[string]string
	vendorsRouteDecode(t, response, &body)
	if body["detail"] != detail {
		t.Fatalf("expected detail %q, got %+v", detail, body)
	}
}

func vendorsRouteInsertVendor(t *testing.T, ctx context.Context, conn *pgx.Conn, key string, name string, description string, iconKey string, auditEnabled bool, auditCaptureBodies bool, now time.Time) int {
	t.Helper()
	var vendorID int
	if err := conn.QueryRow(ctx, `INSERT INTO vendors (key, name, description, icon_key, audit_enabled, audit_capture_bodies, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $7) RETURNING id`, key, name, description, iconKey, auditEnabled, auditCaptureBodies, now).Scan(&vendorID); err != nil {
		t.Fatalf("insert vendor %s: %v", key, err)
	}
	return vendorID
}

func vendorsRouteInsertProfile(t *testing.T, ctx context.Context, conn *pgx.Conn, name string, now time.Time) int {
	t.Helper()
	var profileID int
	if err := conn.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, NULL, FALSE, FALSE, TRUE, 1, $2, $2) RETURNING id`, name, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	return profileID
}

func vendorsRouteInsertModel(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, vendorID int, apiFamily string, modelID string, displayName string, enabled bool, now time.Time) int {
	t.Helper()
	var modelConfigID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, NULL, $6, $7, $7) RETURNING id`, profileID, vendorID, apiFamily, modelID, displayName, enabled, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert model: %v", err)
	}
	return modelConfigID
}

func vendorsRouteMigratedDatabase(t *testing.T, name string) (context.Context, *pgx.Conn, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	harness := vendorsRouteHarness(t)
	databaseName := fmt.Sprintf("%s_%s", name, vendorsRouteRandomSuffix(t))
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

type vendorsRoutePostgresHarness struct{ hostPort string }

func vendorsRouteHarness(t *testing.T) vendorsRoutePostgresHarness {
	t.Helper()
	vendorsRoutePostgres.once.Do(func() {
		containerName := "prism-vendors-" + vendorsRouteRandomSuffix(t)
		if _, err := vendorsRouteDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
			vendorsRoutePostgres.err = err
			return
		}
		hostPort, err := vendorsRouteDockerPort(containerName)
		if err != nil {
			vendorsRoutePostgres.err = err
			return
		}
		if err := vendorsRouteWaitForPostgres(hostPort); err != nil {
			vendorsRoutePostgres.err = err
			return
		}
		vendorsRoutePostgres.hostPort = hostPort
	})
	if vendorsRoutePostgres.err != nil {
		t.Fatalf("start postgres harness: %v", vendorsRoutePostgres.err)
	}
	return vendorsRoutePostgresHarness{hostPort: vendorsRoutePostgres.hostPort}
}

func (h vendorsRoutePostgresHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := vendorsRouteConnect(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+vendorsRouteQuoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+vendorsRouteQuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return vendorsRouteConnect(t, ctx, h.connectionString(databaseName))
}

func (h vendorsRoutePostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func vendorsRouteConnect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres %s: %v", dsn, err)
	}
	return conn
}

func vendorsRouteDockerPort(containerName string) (string, error) {
	output, err := vendorsRouteDockerCommand(context.Background(), "port", containerName, "5432/tcp")
	if err != nil {
		return "", err
	}
	_, port, err := net.SplitHostPort(strings.TrimSpace(strings.Split(output, "\n")[0]))
	return port, err
}

func vendorsRouteWaitForPostgres(hostPort string) error {
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

func vendorsRouteDockerCommand(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func vendorsRouteRandomSuffix(t *testing.T) string {
	t.Helper()
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}
	return hex.EncodeToString(raw[:])
}

func vendorsRouteQuoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
