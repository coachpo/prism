package runtime_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementprofiles "github.com/coachpo/prism/backend/internal/httpapi/management/profiles"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

var sharedPostgresHarness testPostgresHarness

type testPostgresHarness struct {
	containerName string
	hostPort      string
}

type runtimeHarness struct {
	client          *http.Client
	conn            *pgx.Conn
	authService     *managementauth.Service
	profilesService *managementprofiles.Service
	runtimeService  *runtimeapi.Service
	server          *httptest.Server
	url             string
	upstream        *upstreamRecorder
}

type seededRuntimeRoute struct {
	PublicModelID   string
	TargetModelID   string
	EndpointBaseURL string
	EndpointAPIKey  string
	ConnectionID    int
}

type runtimeRouteSeed struct {
	ProfileID       int
	APIFamily       string
	PublicModelID   string
	TargetModelID   string
	EndpointBaseURL string
	EndpointAPIKey  string
	CustomHeaders   map[string]any
}

type upstreamRequestSnapshot struct {
	Method  string
	URL     string
	Path    string
	Query   string
	Headers http.Header
	Body    []byte
}

type upstreamRecorder struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []upstreamRequestSnapshot
}

func TestMain(m *testing.M) {
	harness, err := startSharedPostgresHarness()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sharedPostgresHarness = harness
	code := m.Run()
	cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if harness.containerName != "" {
		_ = exec.CommandContext(cleanupContext, "docker", "rm", "-f", harness.containerName).Run()
	}
	os.Exit(code)
}

func TestRuntimeIgnoresXProfileId(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	inactiveProfileID := harness.createProfile(t, "Runtime Ignore Override")
	suffix := randomSuffix()
	publicModelID := "runtime-ignore-" + suffix
	activeRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       activeProfileID,
		APIFamily:       "openai",
		PublicModelID:   publicModelID,
		TargetModelID:   "runtime-active-target-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/active"),
		EndpointAPIKey:  "active-upstream-key",
	})
	inactiveRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       inactiveProfileID,
		APIFamily:       "openai",
		PublicModelID:   publicModelID,
		TargetModelID:   "runtime-inactive-target-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/inactive"),
		EndpointAPIKey:  "inactive-upstream-key",
	})

	runtimePayload := map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "ignore override"}},
		"model":    publicModelID,
	}
	harness.upstream.clear()
	firstResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		runtimePayload,
		map[string]string{"X-Profile-Id": fmt.Sprintf("%d", inactiveProfileID)},
	)
	assertStatus(t, firstResponse, http.StatusOK)
	firstRequest := harness.upstream.lastRequest(t)
	if firstRequest.Path != "/active/v1/chat/completions" {
		t.Fatalf("expected first upstream path to use active profile route, got %s", firstRequest.Path)
	}
	if firstRequest.Headers.Get("Authorization") != "Bearer "+activeRoute.EndpointAPIKey {
		t.Fatalf("expected first upstream authorization header, got %q", firstRequest.Headers.Get("Authorization"))
	}
	if requestModelID(t, firstRequest.Body) != activeRoute.TargetModelID {
		t.Fatalf("expected first upstream body model %q, got %q", activeRoute.TargetModelID, requestModelID(t, firstRequest.Body))
	}

	harness.activateProfile(t, inactiveProfileID, activeProfileID)
	harness.upstream.clear()

	secondResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		runtimePayload,
		map[string]string{"X-Profile-Id": fmt.Sprintf("%d", activeProfileID)},
	)
	assertStatus(t, secondResponse, http.StatusOK)
	secondRequest := harness.upstream.lastRequest(t)
	if secondRequest.Path != "/inactive/v1/chat/completions" {
		t.Fatalf("expected second upstream path to follow new active profile, got %s", secondRequest.Path)
	}
	if secondRequest.Headers.Get("Authorization") != "Bearer "+inactiveRoute.EndpointAPIKey {
		t.Fatalf("expected second upstream authorization header, got %q", secondRequest.Headers.Get("Authorization"))
	}
	if requestModelID(t, secondRequest.Body) != inactiveRoute.TargetModelID {
		t.Fatalf("expected second upstream body model %q, got %q", inactiveRoute.TargetModelID, requestModelID(t, secondRequest.Body))
	}
}

func TestProxyExecutionParity(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	openAIRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       activeProfileID,
		APIFamily:       "openai",
		PublicModelID:   "proxy-openai-" + suffix,
		TargetModelID:   "native-openai-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/parity/openai"),
		EndpointAPIKey:  "openai-upstream-key",
	})
	geminiRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       activeProfileID,
		APIFamily:       "gemini",
		PublicModelID:   "proxy-gemini-" + suffix,
		TargetModelID:   "native-gemini-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/parity/gemini"),
		EndpointAPIKey:  "gemini-upstream-key",
	})

	harness.upstream.clear()
	openAIResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions?trace=1",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "proxy parity"}},
			"model":    openAIRoute.PublicModelID,
		},
		nil,
	)
	assertStatus(t, openAIResponse, http.StatusOK)
	assertResponseField(t, openAIResponse, "id", "chatcmpl-smoke")

	geminiResponse := harness.requestJSON(
		t,
		http.MethodPost,
		fmt.Sprintf("/v1beta/models/%s:generateContent?alt=sse", geminiRoute.PublicModelID),
		map[string]any{
			"contents": []map[string]any{{
				"role":  "user",
				"parts": []map[string]any{{"text": "proxy parity"}},
			}},
		},
		nil,
	)
	assertStatus(t, geminiResponse, http.StatusOK)
	assertResponseField(t, geminiResponse, "responseId", "gemini-smoke")

	requests := harness.upstream.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", len(requests))
	}
	if requests[0].Path != "/parity/openai/v1/chat/completions" || requests[0].Query != "trace=1" {
		t.Fatalf("unexpected OpenAI upstream target: %+v", requests[0])
	}
	if requestModelID(t, requests[0].Body) != openAIRoute.TargetModelID {
		t.Fatalf("expected OpenAI upstream model rewrite to %q, got %q", openAIRoute.TargetModelID, requestModelID(t, requests[0].Body))
	}
	if requests[0].Headers.Get("Authorization") != "Bearer "+openAIRoute.EndpointAPIKey {
		t.Fatalf("expected OpenAI auth header, got %q", requests[0].Headers.Get("Authorization"))
	}
	wantGeminiPath := fmt.Sprintf("/parity/gemini/v1beta/models/%s:generateContent", geminiRoute.TargetModelID)
	if requests[1].Path != wantGeminiPath || requests[1].Query != "alt=sse" {
		t.Fatalf("unexpected Gemini upstream target: %+v", requests[1])
	}
	if requests[1].Headers.Get("Authorization") != "Bearer "+geminiRoute.EndpointAPIKey {
		t.Fatalf("expected Gemini auth header, got %q", requests[1].Headers.Get("Authorization"))
	}
}

func TestRuntimeHeaderBlocklistMerge(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	allowedHeaderValue := "allowed-custom-header"
	anthropicRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       activeProfileID,
		APIFamily:       "anthropic",
		PublicModelID:   "proxy-anthropic-" + suffix,
		TargetModelID:   "native-anthropic-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/headers/anthropic"),
		EndpointAPIKey:  "anthropic-upstream-key",
		CustomHeaders: map[string]any{
			"anthropic-version": "bad-version",
			"x-api-key":         "bad-upstream-key",
			"x-request-id":      "blocked-after-merge",
			"x-allow-smoke":     allowedHeaderValue,
		},
	})
	harness.seedProfileHeaderBlocklistRule(t, activeProfileID, "Block anthropic version", "exact", "anthropic-version")
	harness.seedProfileHeaderBlocklistRule(t, activeProfileID, "Block anthropic auth", "exact", "x-api-key")

	harness.upstream.clear()
	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/messages",
		map[string]any{
			"messages":   []map[string]any{{"role": "user", "content": "header merge"}},
			"max_tokens": 1,
			"model":      anthropicRoute.PublicModelID,
		},
		map[string]string{
			"User-Agent":    "claude-cli/2.1.109 (external, cli)",
			"X-Client-Kept": "runtime-ok",
			"X-Request-Id":  "blocked-before-merge",
		},
	)
	assertStatus(t, response, http.StatusOK)
	upstreamRequest := harness.upstream.lastRequest(t)
	if upstreamRequest.Path != "/headers/anthropic/v1/messages" {
		t.Fatalf("expected anthropic upstream path, got %s", upstreamRequest.Path)
	}
	if upstreamRequest.Headers.Get("x-api-key") != anthropicRoute.EndpointAPIKey {
		t.Fatalf("expected protected upstream x-api-key header, got %q", upstreamRequest.Headers.Get("x-api-key"))
	}
	if upstreamRequest.Headers.Get("anthropic-version") != "2023-06-01" {
		t.Fatalf("expected protected upstream anthropic-version header, got %q", upstreamRequest.Headers.Get("anthropic-version"))
	}
	if upstreamRequest.Headers.Get("X-Allow-Smoke") != allowedHeaderValue {
		t.Fatalf("expected allowed custom header, got %q", upstreamRequest.Headers.Get("X-Allow-Smoke"))
	}
	if upstreamRequest.Headers.Get("X-Client-Kept") != "runtime-ok" {
		t.Fatalf("expected non-blocked client header to survive, got %q", upstreamRequest.Headers.Get("X-Client-Kept"))
	}
	if upstreamRequest.Headers.Get("X-Request-Id") != "" {
		t.Fatalf("expected blocked request id header to be removed, got %q", upstreamRequest.Headers.Get("X-Request-Id"))
	}
}

func TestRuntimeUserAgentRuleMerge(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	callerUserAgent := "claude-cli/2.1.109 (external, cli)"
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       activeProfileID,
		APIFamily:       "openai",
		PublicModelID:   "proxy-ua-" + suffix,
		TargetModelID:   "native-ua-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/user-agent/openai"),
		EndpointAPIKey:  "user-agent-upstream-key",
	})

	harness.upstream.clear()
	firstResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "caller user agent"}},
			"model":    route.PublicModelID,
		},
		map[string]string{"User-Agent": callerUserAgent},
	)
	assertStatus(t, firstResponse, http.StatusOK)
	if firstUpstreamUA := harness.upstream.lastRequest(t).Headers.Get("User-Agent"); firstUpstreamUA != callerUserAgent {
		t.Fatalf("expected caller user-agent to flow upstream, got %q", firstUpstreamUA)
	}

	harness.updateConnectionCustomHeaders(t, route.ConnectionID, map[string]any{"User-Agent": "Prism Custom Agent/1.0"})
	harness.upstream.clear()
	secondResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "custom user agent"}},
			"model":    route.PublicModelID,
		},
		map[string]string{"User-Agent": callerUserAgent},
	)
	assertStatus(t, secondResponse, http.StatusOK)
	if secondUpstreamUA := harness.upstream.lastRequest(t).Headers.Get("User-Agent"); secondUpstreamUA != "Prism Custom Agent/1.0" {
		t.Fatalf("expected custom user-agent override, got %q", secondUpstreamUA)
	}

	harness.seedProfileHeaderBlocklistRule(t, activeProfileID, "Block user agent", "exact", "user-agent")
	harness.upstream.clear()
	thirdResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "blocked user agent"}},
			"model":    route.PublicModelID,
		},
		map[string]string{"User-Agent": callerUserAgent},
	)
	assertStatus(t, thirdResponse, http.StatusOK)
	if blockedUA := harness.upstream.lastRequest(t).Headers.Get("User-Agent"); blockedUA != "" {
		t.Fatalf("expected blocklisted user-agent to be removed, got %q", blockedUA)
	}
}

func startSharedPostgresHarness() (testPostgresHarness, error) {
	containerName := "prism-s14-runtime-" + randomSuffix()
	if err := runDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
		return testPostgresHarness{}, err
	}
	hostPort, err := dockerPort(containerName)
	if err != nil {
		return testPostgresHarness{}, err
	}
	if err := waitForPostgres(hostPort); err != nil {
		return testPostgresHarness{}, err
	}
	return testPostgresHarness{containerName: containerName, hostPort: hostPort}, nil
}

func (h testPostgresHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := connectDatabase(t, ctx, h.connectionString("postgres"))
	defer adminConn.Close(ctx)
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+quoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return connectDatabase(t, ctx, h.connectionString(databaseName))
}

func (h testPostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func newRuntimeHarness(t *testing.T) *runtimeHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	databaseName := "runtime_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() {
		conn.Close(context.Background())
	})

	startupService, err := startup.New(startup.Options{
		DatabaseURL:         sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey: "runtime-secret",
	})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup service: %v", err)
	}

	upstream := newUpstreamRecorder(t)
	settings := config.Settings{
		Host:                       "127.0.0.1",
		Port:                       8000,
		AppEnv:                     config.EnvironmentProduction,
		DatabaseURL:                sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey:        "runtime-secret",
		CORSAllowedOrigins:         "http://localhost:5173,http://127.0.0.1:5173",
		AuthJWTSecret:              "runtime-jwt-secret",
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
	runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build runtime service: %v", err)
	}
	t.Cleanup(runtimeService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{
		Version:         "runtime-test",
		AuthService:     authService,
		ProfilesService: profilesService,
		RuntimeService:  runtimeService,
	})
	if err != nil {
		t.Fatalf("build runtime handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar
	return &runtimeHarness{
		client:          client,
		conn:            conn,
		authService:     authService,
		profilesService: profilesService,
		runtimeService:  runtimeService,
		server:          server,
		url:             server.URL,
		upstream:        upstream,
	}
}

func newUpstreamRecorder(t *testing.T) *upstreamRecorder {
	t.Helper()
	recorder := &upstreamRecorder{}
	recorder.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream request body: %v", err)
		}
		_ = r.Body.Close()
		recorder.mu.Lock()
		recorder.requests = append(recorder.requests, upstreamRequestSnapshot{
			Method:  r.Method,
			URL:     r.URL.String(),
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: r.Header.Clone(),
			Body:    append([]byte(nil), body...),
		})
		recorder.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/chat/completions"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "chatcmpl-smoke"})
		case strings.HasSuffix(r.URL.Path, "/v1/messages") || strings.HasSuffix(r.URL.Path, "/v1/messages/count_tokens"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "msg-smoke", "type": "message"})
		case strings.Contains(r.URL.Path, ":generateContent") || strings.Contains(r.URL.Path, ":streamGenerateContent"):
			_ = json.NewEncoder(w).Encode(map[string]any{"responseId": "gemini-smoke"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(recorder.server.Close)
	return recorder
}

func (u *upstreamRecorder) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *upstreamRecorder) clear() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.requests = nil
}

func (u *upstreamRecorder) lastRequest(t *testing.T) upstreamRequestSnapshot {
	t.Helper()
	requests := u.requestsSnapshot()
	if len(requests) == 0 {
		t.Fatal("expected at least one upstream request")
	}
	return requests[len(requests)-1]
}

func (u *upstreamRecorder) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
}

func (h *runtimeHarness) activeProfileID(t *testing.T) int {
	t.Helper()
	response := h.requestJSON(t, http.MethodGet, "/api/profiles/active", nil, nil)
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return jsonInt(t, payload["id"])
}

func (h *runtimeHarness) createProfile(t *testing.T, name string) int {
	t.Helper()
	response := h.requestJSON(t, http.MethodPost, "/api/profiles", map[string]any{"name": name, "description": name}, nil)
	assertStatus(t, response, http.StatusCreated)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return jsonInt(t, payload["id"])
}

func (h *runtimeHarness) activateProfile(t *testing.T, targetProfileID int, expectedActiveProfileID int) {
	t.Helper()
	response := h.requestJSON(
		t,
		http.MethodPost,
		fmt.Sprintf("/api/profiles/%d/activate", targetProfileID),
		map[string]any{"expected_active_profile_id": expectedActiveProfileID},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
}

func (h *runtimeHarness) seedProxyRoute(t *testing.T, seed runtimeRouteSeed) seededRuntimeRoute {
	t.Helper()
	strategyID := h.seedLegacyStrategy(t, seed.ProfileID, "runtime-strategy-"+randomSuffix(), "round-robin")
	targetModelConfigID := h.seedModel(t, seed.ProfileID, seed.APIFamily, seed.TargetModelID, "native", &strategyID)
	publicModelConfigID := h.seedModel(t, seed.ProfileID, seed.APIFamily, seed.PublicModelID, "proxy", nil)
	h.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := h.seedEndpoint(t, seed.ProfileID, "endpoint-"+randomSuffix(), seed.EndpointBaseURL, seed.EndpointAPIKey, 0)
	connectionID := h.seedConnection(t, seed.ProfileID, targetModelConfigID, endpointID, "connection-"+randomSuffix(), nil, seed.CustomHeaders, 0)
	return seededRuntimeRoute{
		PublicModelID:   seed.PublicModelID,
		TargetModelID:   seed.TargetModelID,
		EndpointBaseURL: seed.EndpointBaseURL,
		EndpointAPIKey:  seed.EndpointAPIKey,
		ConnectionID:    connectionID,
	}
}

func (h *runtimeHarness) seedLegacyStrategy(t *testing.T, profileID int, name string, legacyStrategyType string) int {
	t.Helper()
	now := time.Now().UTC()
	autoRecovery := `{"mode":"disabled"}`
	var strategyID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO loadbalance_strategies (profile_id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy, created_at, updated_at)
		 VALUES ($1, $2, 'legacy', $3, $4::jsonb, NULL, $5, $5)
		 RETURNING id`,
		profileID,
		name,
		legacyStrategyType,
		autoRecovery,
		now,
	).Scan(&strategyID); err != nil {
		t.Fatalf("insert runtime strategy %q: %v", name, err)
	}
	return strategyID
}

func (h *runtimeHarness) seedModel(t *testing.T, profileID int, apiFamily string, modelID string, modelType string, strategyID *int) int {
	t.Helper()
	now := time.Now().UTC()
	var modelConfigID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO model_configs (
			profile_id,
			vendor_id,
			api_family,
			model_id,
			display_name,
			model_type,
			loadbalance_strategy_id,
			is_enabled,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, TRUE, $8, $8)
		RETURNING id`,
		profileID,
		nil,
		apiFamily,
		modelID,
		nil,
		modelType,
		nullableTestInt(strategyID),
		now,
	).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert runtime model %q: %v", modelID, err)
	}
	return modelConfigID
}

func (h *runtimeHarness) seedProxyTarget(t *testing.T, sourceModelConfigID int, targetModelConfigID int) {
	t.Helper()
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO model_proxy_targets (source_model_config_id, target_model_config_id, position) VALUES ($1, $2, 0)`,
		sourceModelConfigID,
		targetModelConfigID,
	); err != nil {
		t.Fatalf("insert runtime proxy target: %v", err)
	}
}

func (h *runtimeHarness) seedEndpoint(t *testing.T, profileID int, name string, baseURL string, apiKey string, position int) int {
	t.Helper()
	now := time.Now().UTC()
	var endpointID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $6)
		 RETURNING id`,
		profileID,
		name,
		baseURL,
		apiKey,
		position,
		now,
	).Scan(&endpointID); err != nil {
		t.Fatalf("insert runtime endpoint %q: %v", name, err)
	}
	return endpointID
}

func (h *runtimeHarness) seedConnection(t *testing.T, profileID int, modelConfigID int, endpointID int, name string, authType *string, customHeaders map[string]any, priority int) int {
	t.Helper()
	now := time.Now().UTC()
	var connectionID int
	if err := h.conn.QueryRow(
		context.Background(),
		`INSERT INTO connections (
			profile_id,
			model_config_id,
			endpoint_id,
			pricing_template_id,
			qps_limit,
			max_in_flight_non_stream,
			max_in_flight_stream,
			openai_probe_endpoint_variant,
			is_active,
			priority,
			name,
			auth_type,
			custom_headers,
			health_status,
			health_detail,
			last_health_check,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, NULL, NULL, NULL, NULL, NULL, TRUE, $4, $5, $6, $7, 'healthy', NULL, NULL, $8, $8)
		RETURNING id`,
		profileID,
		modelConfigID,
		endpointID,
		priority,
		name,
		nullableTestString(authType),
		marshalNullableJSON(t, customHeaders),
		now,
	).Scan(&connectionID); err != nil {
		t.Fatalf("insert runtime connection %q: %v", name, err)
	}
	return connectionID
}

func (h *runtimeHarness) updateConnectionCustomHeaders(t *testing.T, connectionID int, customHeaders map[string]any) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`UPDATE connections SET custom_headers = $2, updated_at = $3 WHERE id = $1`,
		connectionID,
		marshalNullableJSON(t, customHeaders),
		now,
	); err != nil {
		t.Fatalf("update runtime connection %d custom headers: %v", connectionID, err)
	}
}

func (h *runtimeHarness) seedProfileHeaderBlocklistRule(t *testing.T, profileID int, name string, matchType string, pattern string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO header_blocklist_rules (profile_id, name, match_type, pattern, enabled, is_system, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, TRUE, FALSE, $5, $5)`,
		profileID,
		name,
		matchType,
		pattern,
		now,
	); err != nil {
		t.Fatalf("insert runtime header blocklist rule %q: %v", name, err)
	}
}

func (h *runtimeHarness) requestJSON(t *testing.T, method string, path string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		requestBody = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, h.url+path, requestBody)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := h.client.Do(request)
	if err != nil {
		t.Fatalf("perform request %s %s: %v", method, path, err)
	}
	t.Cleanup(func() {
		_ = response.Body.Close()
	})
	return response
}

func assertStatus(t *testing.T, response *http.Response, want int) {
	t.Helper()
	if response.StatusCode != want {
		body := readResponseBody(t, response)
		t.Fatalf("expected status %d, got %d with body %s", want, response.StatusCode, body)
	}
}

func assertResponseField(t *testing.T, response *http.Response, field string, want string) {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if got, _ := payload[field].(string); got != want {
		t.Fatalf("expected response field %q=%q, got %+v", field, want, payload)
	}
}

func decodeJSONResponse(t *testing.T, response *http.Response, target any) {
	t.Helper()
	body := readResponseBody(t, response)
	if err := json.Unmarshal([]byte(body), target); err != nil {
		t.Fatalf("decode response JSON %q: %v", body, err)
	}
}

func readResponseBody(t *testing.T, response *http.Response) string {
	t.Helper()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return strings.TrimSpace(string(raw))
}

func requestModelID(t *testing.T, body []byte) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode upstream request body: %v", err)
	}
	modelID, _ := payload["model"].(string)
	return modelID
}

func marshalNullableJSON(t *testing.T, value any) any {
	t.Helper()
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON value: %v", err)
	}
	return string(raw)
}

func nullableTestInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTestString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func jsonInt(t *testing.T, value any) int {
	t.Helper()
	floatValue, ok := value.(float64)
	if !ok {
		t.Fatalf("expected JSON number, got %T", value)
	}
	return int(floatValue)
}

func connectDatabase(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect database %s: %v", dsn, err)
	}
	return conn
}

func runDockerCommand(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func dockerPort(containerName string) (string, error) {
	command := exec.Command("docker", "port", containerName, "5432/tcp")
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker port %s failed: %v\n%s", containerName, err, strings.TrimSpace(string(output)))
	}
	firstLine := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	_, port, splitErr := net.SplitHostPort(firstLine)
	if splitErr != nil {
		return "", fmt.Errorf("parse docker port output %q: %w", firstLine, splitErr)
	}
	return port, nil
}

func waitForPostgres(hostPort string) error {
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

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func randomSuffix() string {
	buffer := make([]byte, 4)
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}
