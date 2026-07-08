package connections

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
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

var connectionsRoutePostgres struct {
	once     sync.Once
	hostPort string
	err      error
}

func connectionStringRef(value string) *string {
	return &value
}

func connectionIntRef(value int) *int {
	return &value
}

func requireConnectionDomainError(t *testing.T, err error, status int, detail string) {
	t.Helper()

	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domainError, got %T", err)
	}
	if domainErr.StatusCode != status || domainErr.Detail != detail {
		t.Fatalf("expected domainError (%d, %q), got (%d, %q)", status, detail, domainErr.StatusCode, domainErr.Detail)
	}
}

func TestNormalizeConnectionPriorities(t *testing.T) {
	now := time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC)
	items := []connectionResponse{{Priority: 5}, {Priority: 1}}
	if changed := normalizeConnectionPriorities(items, now); !changed {
		t.Fatal("expected mismatched priorities to be normalized")
	}
	if items[0].Priority != 0 || items[1].Priority != 1 {
		t.Fatalf("expected normalized priorities [0 1], got [%d %d]", items[0].Priority, items[1].Priority)
	}
	if !items[0].UpdatedAt.Equal(now) {
		t.Fatalf("expected normalized item updated_at to be set, got %v", items[0].UpdatedAt)
	}

	stable := []connectionResponse{{Priority: 0}, {Priority: 1}}
	if changed := normalizeConnectionPriorities(stable, now); changed {
		t.Fatal("expected already-normalized priorities not to report a change")
	}
}

func TestValidateLimiterAndAuthType(t *testing.T) {
	if err := validateLimiter("qps_limit", nil); err != nil {
		t.Fatalf("expected nil limiter to pass, got %v", err)
	}
	if err := validateLimiter("qps_limit", connectionIntRef(1)); err != nil {
		t.Fatalf("expected positive limiter to pass, got %v", err)
	}
	if err := validateLimiter("qps_limit", connectionIntRef(0)); err == nil {
		t.Fatal("expected zero limiter to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "qps_limit must be >= 1 when provided")
	}
	if got, err := validateAuthType(connectionStringRef(" Anthropic ")); err != nil || got == nil || *got != "anthropic" {
		t.Fatalf("expected normalized auth type, got value=%#v err=%v", got, err)
	}
	if _, err := validateAuthType(connectionStringRef(" ")); err == nil {
		t.Fatal("expected blank auth type to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "auth_type must be one of 'openai', 'anthropic', or 'gemini'")
	}
}

func TestConnectionRequestsRejectOpenAIProbeEndpointVariant(t *testing.T) {
	for _, test := range []struct {
		name   string
		target any
	}{
		{name: "create", target: &connectionCreateRequest{}},
		{name: "update", target: &connectionUpdateRequest{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/models/1/connections", strings.NewReader(`{"openai_probe_endpoint_variant":"responses_minimal"}`))
			if err := decodeJSONBody(request, test.target); err == nil {
				t.Fatal("expected removed probe variant field to be rejected")
			}
		})
	}
}

func TestConnectionHelpers(t *testing.T) {
	if got := normalizeHeaders(map[string]string{}); got != nil {
		t.Fatalf("expected empty headers to normalize to nil, got %#v", got)
	}
	headers := map[string]string{"X-Test": "1"}
	if got := normalizeHeaders(headers); got == nil || got["X-Test"] != "1" {
		t.Fatalf("expected headers to pass through, got %#v", got)
	}
	if got := normalizeOptionalString(connectionStringRef(" value ")); got == nil || *got != " value " {
		t.Fatalf("expected optional string to preserve its raw value, got %#v", got)
	}
	if !reflect.DeepEqual(dedupeIntValues([]int{3, 1, 3, 2, 1}), []int{3, 1, 2}) {
		t.Fatalf("expected deduped ints to preserve first-seen order")
	}
}

func TestResolveOpenAITextCapability(t *testing.T) {
	if got, err := resolveOpenAITextCapabilityCreate("openai", connectionStringRef(" dual_native ")); err != nil || got == nil || *got != "dual_native" {
		t.Fatalf("expected normalized OpenAI text capability, got value=%#v err=%v", got, err)
	}
	if _, err := resolveOpenAITextCapabilityCreate("openai", nil); err == nil {
		t.Fatal("expected missing OpenAI text capability to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "openai_text_capability is required for OpenAI-family connections")
	}
	if _, err := resolveOpenAITextCapabilityCreate("openai", connectionStringRef("bogus")); err == nil {
		t.Fatal("expected invalid OpenAI text capability to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "openai_text_capability is invalid")
	}
	if _, err := resolveOpenAITextCapabilityCreate("anthropic", connectionStringRef("responses_only")); err == nil {
		t.Fatal("expected non-OpenAI text capability to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "openai_text_capability is only supported for OpenAI-family connections")
	}
	if got, err := resolveOpenAITextCapabilityUpdate("openai", "openai", connectionStringRef("responses_only"), optionalString{}); err != nil || got == nil || *got != "responses_only" {
		t.Fatalf("expected update to preserve existing OpenAI text capability, got value=%#v err=%v", got, err)
	}
	if _, err := resolveOpenAITextCapabilityUpdate("anthropic", "openai", nil, optionalString{}); err == nil {
		t.Fatal("expected changing to OpenAI without text capability to fail")
	} else {
		requireConnectionDomainError(t, err, http.StatusUnprocessableEntity, "openai_text_capability is required for OpenAI-family connections")
	}
}

func TestRouteInt(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/connections/42", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("connection_id", "42")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))

	if got, err := routeInt(request, "connection_id"); err != nil || got != 42 {
		t.Fatalf("expected route param 42, got value=%d err=%v", got, err)
	}

	routeContext.URLParams.Values[0] = "bad"
	if _, err := routeInt(request, "connection_id"); err == nil {
		t.Fatal("expected invalid route param to fail")
	}
}

func TestTerminalTargetRecordAdapterPreservesConnectionResponseShape(t *testing.T) {
	modelConfigID := 7
	name := "primary"
	authType := "openai"
	textCapability := "chat_completions_only"
	pricingTemplateID := 11
	qpsLimit := 12
	maxNonStream := 3
	maxStream := 4
	now := time.Date(2026, time.June, 7, 12, 0, 0, 0, time.UTC)

	connection := connectionResponse{
		ID: 42, ProfileID: 5, ModelConfigID: &modelConfigID, APIFamily: "openai", EndpointID: 9,
		IsActive: true, Priority: 2, Name: &name, AuthType: &authType,
		CustomHeaders:        map[string]string{"X-Test": "1"},
		OpenAITextCapability: &textCapability, PricingTemplateID: &pricingTemplateID,
		QPSLimit: &qpsLimit, MaxInFlightNonStream: &maxNonStream, MaxInFlightStream: &maxStream,
		PricingTemplate: &connectionPricingTemplateSummary{ID: 11, Name: "standard", PricingUnit: "tokens", PricingCurrencyCode: "USD", Version: 1},
		CreatedAt:       now, UpdatedAt: now,
	}
	converted := connectionResponseFromTerminalTargetRecord(terminalTargetRecordFromConnectionResponse(connection))
	if !reflect.DeepEqual(converted, connection) {
		t.Fatalf("expected terminal-target adapter to preserve connection response\nwant: %#v\ngot:  %#v", connection, converted)
	}
}

func TestPricingTemplateImportRouteUpsertValidationAndUnknownFields(t *testing.T) {
	ctx, conn, dsn := connectionsRouteMigratedDatabase(t, "pricing_template_import")
	now := time.Date(2026, time.July, 8, 12, 0, 0, 0, time.UTC)
	profileID := connectionsRouteInsertProfile(t, ctx, conn, "Pricing import profile", now)
	router := connectionsRouteRouter(t, ctx, dsn, now)

	payload := map[string]any{
		"mode": "upsert_by_name",
		"templates": []map[string]any{
			{"name": " gpt-4o ", "pricing_unit": "PER_1M", "pricing_currency_code": "usd", "input_price": "2.5", "output_price": "10", "cached_input_price": "1.25", "cache_creation_price": "0", "reasoning_price": "0", "description": " flagship "},
			{"name": "gpt-4o-mini", "pricing_unit": "PER_1M", "pricing_currency_code": "USD", "input_price": "0.15", "output_price": "0.60"},
		},
	}

	createdResponse := connectionsRouteRequest(t, router, http.MethodPost, "/pricing-templates/import", profileID, payload)
	connectionsRouteRequireStatus(t, createdResponse, http.StatusOK)
	var imported pricingTemplateImportResponse
	connectionsRouteDecode(t, createdResponse, &imported)
	if imported.Created != 2 || imported.Updated != 0 || len(imported.Skipped) != 0 || len(imported.Errors) != 0 {
		t.Fatalf("unexpected created import response: %+v", imported)
	}
	connectionsRouteRequireTemplateCount(t, ctx, conn, profileID, 2)

	updatedResponse := connectionsRouteRequest(t, router, http.MethodPost, "/pricing-templates/import", profileID, payload)
	connectionsRouteRequireStatus(t, updatedResponse, http.StatusOK)
	connectionsRouteDecode(t, updatedResponse, &imported)
	if imported.Created != 0 || imported.Updated != 2 || len(imported.Skipped) != 0 || len(imported.Errors) != 0 {
		t.Fatalf("unexpected upsert import response: %+v", imported)
	}
	connectionsRouteRequireTemplateCount(t, ctx, conn, profileID, 2)

	invalid := connectionsRouteRequest(t, router, http.MethodPost, "/pricing-templates/import", profileID, map[string]any{
		"mode": "upsert_by_name",
		"templates": []map[string]any{
			{"name": "bad-row-kept-out", "pricing_currency_code": "USD", "input_price": "1", "output_price": "2"},
			{"name": "bad-price", "pricing_currency_code": "USD", "input_price": "-1", "output_price": "2"},
		},
	})
	connectionsRouteRequireStatus(t, invalid, http.StatusBadRequest)
	connectionsRouteRequireTemplateCount(t, ctx, conn, profileID, 2)

	unknown := connectionsRouteRequest(t, router, http.MethodPost, "/pricing-templates/import", profileID, map[string]any{
		"mode":      "upsert_by_name",
		"templates": []map[string]any{},
		"surprise":  true,
	})
	connectionsRouteRequireStatus(t, unknown, http.StatusBadRequest)
}

func connectionsRouteRouter(t *testing.T, ctx context.Context, dsn string, now time.Time) http.Handler {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	service, err := NewService(config.Settings{}, Options{Pool: pool, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("build connections service: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return router
}

func connectionsRouteRequest(t *testing.T, handler http.Handler, method string, path string, profileID int, body any) *httptest.ResponseRecorder {
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

func connectionsRouteDecode(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode JSON response: %v\nbody=%s", err, response.Body.String())
	}
}

func connectionsRouteRequireStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("expected status %d, got %d body=%s", status, response.Code, response.Body.String())
	}
}

func connectionsRouteRequireTemplateCount(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, want int) {
	t.Helper()
	var got int
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM pricing_templates WHERE profile_id = $1`, profileID).Scan(&got); err != nil {
		t.Fatalf("count pricing templates: %v", err)
	}
	if got != want {
		t.Fatalf("expected %d pricing templates, got %d", want, got)
	}
}

func connectionsRouteInsertProfile(t *testing.T, ctx context.Context, conn *pgx.Conn, name string, now time.Time) int {
	t.Helper()
	var profileID int
	if err := conn.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, NULL, TRUE, TRUE, TRUE, 1, $2, $2) RETURNING id`, name, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	return profileID
}

func connectionsRouteMigratedDatabase(t *testing.T, name string) (context.Context, *pgx.Conn, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	harness := connectionsRouteHarness(t)
	databaseName := fmt.Sprintf("%s_%s", name, connectionsRouteRandomSuffix(t))
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

type connectionsRoutePostgresHarness struct{ hostPort string }

func connectionsRouteHarness(t *testing.T) connectionsRoutePostgresHarness {
	t.Helper()
	connectionsRoutePostgres.once.Do(func() {
		containerName := "prism-connections-" + connectionsRouteRandomSuffix(t)
		if _, err := connectionsRouteDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
			connectionsRoutePostgres.err = err
			return
		}
		hostPort, err := connectionsRouteDockerPort(containerName)
		if err != nil {
			connectionsRoutePostgres.err = err
			return
		}
		if err := connectionsRouteWaitForPostgres(hostPort); err != nil {
			connectionsRoutePostgres.err = err
			return
		}
		connectionsRoutePostgres.hostPort = hostPort
	})
	if connectionsRoutePostgres.err != nil {
		t.Fatalf("start postgres harness: %v", connectionsRoutePostgres.err)
	}
	return connectionsRoutePostgresHarness{hostPort: connectionsRoutePostgres.hostPort}
}

func (h connectionsRoutePostgresHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := connectionsRouteConnect(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+connectionsRouteQuoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+connectionsRouteQuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return connectionsRouteConnect(t, ctx, h.connectionString(databaseName))
}

func (h connectionsRoutePostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func connectionsRouteConnect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres %s: %v", dsn, err)
	}
	return conn
}

func connectionsRouteDockerPort(containerName string) (string, error) {
	output, err := connectionsRouteDockerCommand(context.Background(), "port", containerName, "5432/tcp")
	if err != nil {
		return "", err
	}
	_, port, err := net.SplitHostPort(strings.TrimSpace(strings.Split(output, "\n")[0]))
	return port, err
}

func connectionsRouteWaitForPostgres(hostPort string) error {
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

func connectionsRouteDockerCommand(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func connectionsRouteRandomSuffix(t *testing.T) string {
	t.Helper()
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}
	return hex.EncodeToString(raw[:])
}

func connectionsRouteQuoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
