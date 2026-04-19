package runtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementprofiles "github.com/coachpo/prism/backend/internal/httpapi/management/profiles"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	realtimeapi "github.com/coachpo/prism/backend/internal/httpapi/realtime"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

type realtimeHarness struct {
	*runtimeHarness
	realtimeService *realtimeapi.Service
	statsService    *managementstats.Service
	fixedNow        time.Time
}

func TestRealtimeProtocolOrder(t *testing.T) {
	harness := newRealtimeHarness(t)

	openConn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, openConn, "authenticated")
	assertRealtimeMessage(t, openConn, map[string]any{"type": "heartbeat"})
	_ = openConn.Close()

	seedRealtimeVerifiedAuthSettings(t, harness, "realtime-admin", "realtime-password-123", "realtime@example.com")
	loginResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/api/auth/login",
		map[string]any{"username": "realtime-admin", "password": "realtime-password-123", "session_duration": "7_days"},
		nil,
	)
	assertStatus(t, loginResponse, http.StatusOK)

	unauthorizedConn := harness.dialWebSocket(t, false)
	assertWebSocketClosedWithCode(t, unauthorizedConn, websocket.ClosePolicyViolation)

	authorizedConn := harness.dialWebSocket(t, true)
	assertRealtimeMessage(t, authorizedConn, map[string]any{"type": "authenticated", "username": "realtime-admin"})
	assertRealtimeMessage(t, authorizedConn, map[string]any{"type": "heartbeat"})
	_ = authorizedConn.Close()
}

func TestRealtimeSubscriptions(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileOneID := harness.activeProfileID(t)
	profileTwoID := harness.createProfile(t, "Realtime Secondary Profile")
	routeOne := harness.seedRealtimeDashboardRoute(t, profileOneID, "profile-one")
	routeTwo := harness.seedRealtimeDashboardRoute(t, profileTwoID, "profile-two")
	requestLogOne := harness.insertDashboardActivity(t, routeOne, profileOneID, 8101, 9101, harness.fixedNow.Add(-2*time.Minute))
	requestLogTwo := harness.insertDashboardActivity(t, routeTwo, profileTwoID, 8102, 9102, harness.fixedNow.Add(-90*time.Second))

	replacementConn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, replacementConn, "authenticated")
	assertRealtimeMessage(t, replacementConn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, replacementConn, map[string]any{"type": "subscribe", "profile_id": profileOneID, "channel": "dashboard"})
	assertRealtimeMessage(t, replacementConn, map[string]any{"type": "subscribed", "profile_id": float64(profileOneID), "channel": "dashboard"})
	delivered, err := harness.realtimeService.PublishDashboardUpdate(context.Background(), requestLogOne, profileOneID)
	if err != nil {
		t.Fatalf("publish profile-one dashboard update: %v", err)
	}
	if !delivered {
		t.Fatal("expected profile-one dashboard update delivery while subscribed")
	}
	assertNestedRequestLogProfileID(t, readWebSocketJSON(t, replacementConn), profileOneID)
	writeWebSocketJSON(t, replacementConn, map[string]any{"type": "subscribe", "profile_id": profileTwoID, "channel": "dashboard"})
	assertRealtimeMessage(t, replacementConn, map[string]any{"type": "subscribed", "profile_id": float64(profileTwoID), "channel": "dashboard"})
	delivered, err = harness.realtimeService.PublishDashboardUpdate(context.Background(), requestLogOne, profileOneID)
	if err != nil {
		t.Fatalf("publish replaced profile-one dashboard update: %v", err)
	}
	if delivered {
		t.Fatal("expected no delivery after profile replacement removed profile-one membership")
	}
	_ = replacementConn.Close()

	delivered, err = harness.realtimeService.PublishDashboardUpdate(context.Background(), requestLogTwo, profileTwoID)
	if err != nil {
		t.Fatalf("prime profile-two pending dashboard update: %v", err)
	}
	if delivered {
		t.Fatal("expected no immediate profile-two delivery without subscribers")
	}

	channelConn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, channelConn, "authenticated")
	assertRealtimeMessage(t, channelConn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, channelConn, map[string]any{"type": "subscribe", "profile_id": profileTwoID, "channel": "dashboard"})
	assertRealtimeMessage(t, channelConn, map[string]any{"type": "subscribed", "profile_id": float64(profileTwoID), "channel": "dashboard"})
	assertNestedRequestLogProfileID(t, readWebSocketJSON(t, channelConn), profileTwoID)
	writeWebSocketJSON(t, channelConn, map[string]any{"type": "unsubscribe_channel", "channel": "dashboard"})
	assertRealtimeMessage(t, channelConn, map[string]any{"type": "unsubscribed", "channel": "dashboard"})
	delivered, err = harness.realtimeService.PublishDashboardUpdate(context.Background(), requestLogTwo, profileTwoID)
	if err != nil {
		t.Fatalf("publish after unsubscribe_channel: %v", err)
	}
	if delivered {
		t.Fatal("expected no delivery after unsubscribe_channel cleanup")
	}
	_ = channelConn.Close()

	unsubscribeConn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, unsubscribeConn, "authenticated")
	assertRealtimeMessage(t, unsubscribeConn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, unsubscribeConn, map[string]any{"type": "subscribe", "profile_id": profileTwoID, "channel": "dashboard"})
	assertRealtimeMessage(t, unsubscribeConn, map[string]any{"type": "subscribed", "profile_id": float64(profileTwoID), "channel": "dashboard"})
	assertNestedRequestLogProfileID(t, readWebSocketJSON(t, unsubscribeConn), profileTwoID)
	writeWebSocketJSON(t, unsubscribeConn, map[string]any{"type": "unsubscribe"})
	assertRealtimeMessage(t, unsubscribeConn, map[string]any{"type": "unsubscribed"})
	delivered, err = harness.realtimeService.PublishDashboardUpdate(context.Background(), requestLogTwo, profileTwoID)
	if err != nil {
		t.Fatalf("publish after unsubscribe: %v", err)
	}
	if delivered {
		t.Fatal("expected no delivery after unsubscribe cleanup")
	}
	_ = unsubscribeConn.Close()
}

func TestRealtimePong(t *testing.T) {
	harness := newRealtimeHarness(t)
	conn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, conn, "authenticated")
	assertRealtimeMessage(t, conn, map[string]any{"type": "heartbeat"})

	writeWebSocketJSON(t, conn, map[string]any{"type": "pong"})
	writeWebSocketJSON(t, conn, map[string]any{"type": "ping"})
	assertRealtimeMessage(t, conn, map[string]any{"type": "pong"})
	_ = conn.Close()
}

func TestDashboardUpdatePayload(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := harness.seedRealtimeDashboardRoute(t, profileID, "payload")
	requestLogID := harness.insertDashboardActivity(t, route, profileID, 8201, 9201, harness.fixedNow.Add(-30*time.Second))

	message, err := harness.realtimeService.BuildDashboardUpdate(context.Background(), requestLogID, profileID)
	if err != nil {
		t.Fatalf("build dashboard update payload: %v", err)
	}
	fixture := loadRealtimeFixture(t, "dashboard-update.json")
	assertJSONShapeMatchesFixture(t, message, fixture)
	if message.Type != "dashboard.update" {
		t.Fatalf("expected dashboard.update message type, got %+v", message)
	}
	if message.RequestLog.ID != requestLogID || message.RequestLog.ProfileID != profileID || message.RoutingRoute24H == nil {
		t.Fatalf("expected built payload to include request log and route snapshot, got %+v", message)
	}
}

func TestDashboardUpdateDelivery(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := harness.seedRealtimeDashboardRoute(t, profileID, "delivery")
	conn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, conn, "authenticated")
	assertRealtimeMessage(t, conn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, conn, map[string]any{"type": "subscribe", "profile_id": profileID, "channel": "dashboard"})
	assertRealtimeMessage(t, conn, map[string]any{"type": "subscribed", "profile_id": float64(profileID), "channel": "dashboard"})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "deliver dashboard update"}}, "model": route.PublicModelID},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	message := readWebSocketJSON(t, conn)
	if message["type"] != "dashboard.update" {
		t.Fatalf("expected dashboard.update websocket message, got %+v", message)
	}
	requestLog := message["request_log"].(map[string]any)
	if requestLog["profile_id"] != float64(profileID) || requestLog["model_id"] != route.PublicModelID || requestLog["request_path"] != "/v1/chat/completions" {
		t.Fatalf("unexpected delivered request_log payload: %+v", requestLog)
	}
	statsSummary := message["stats_summary_24h"].(map[string]any)
	if statsSummary["total_requests"] != float64(1) || statsSummary["success_count"] != float64(1) {
		t.Fatalf("unexpected delivered stats summary payload: %+v", statsSummary)
	}
	throughput := message["throughput_24h"].(map[string]any)
	if throughput["total_requests"] != float64(1) {
		t.Fatalf("unexpected delivered throughput payload: %+v", throughput)
	}
	if message["routing_route_24h"] == nil {
		t.Fatalf("expected delivered routing snapshot, got %+v", message)
	}
	_ = conn.Close()
}

func newRealtimeHarness(t *testing.T) *realtimeHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	databaseName := "s16_runtime_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { conn.Close(context.Background()) })
	startupService, err := startup.New(startup.Options{DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "s16-runtime-secret"})
	if err != nil {
		t.Fatalf("build S16 startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run S16 startup service: %v", err)
	}

	fixedNow := time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC)
	upstream := newUpstreamRecorder(t)
	settings := config.Settings{Host: "127.0.0.1", Port: 8000, AppEnv: config.EnvironmentProduction, DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "s16-runtime-secret", CORSAllowedOrigins: "http://localhost:5173,http://127.0.0.1:5173", AuthJWTSecret: "s16-runtime-jwt-secret", AuthAccessTokenTTLSeconds: 900, AuthRefreshTokenTTLSeconds: 604800, AuthResetCodeTTLSeconds: 600, AuthCookieName: "prism_access_token", AuthRefreshCookieName: "prism_refresh_token", AuthCookieSecure: false}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create S16 pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	authService, err := managementauth.NewService(settings, managementauth.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build S16 auth service: %v", err)
	}
	t.Cleanup(authService.Close)
	profilesService, err := managementprofiles.NewService(settings, managementprofiles.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build S16 profiles service: %v", err)
	}
	t.Cleanup(profilesService.Close)
	statsService, err := managementstats.NewService(settings, managementstats.Options{Pool: pool, Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatalf("build S16 stats service: %v", err)
	}
	t.Cleanup(statsService.Close)
	realtimeService, err := realtimeapi.NewService(settings, realtimeapi.Options{Pool: pool, AuthService: authService, Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatalf("build S16 realtime service: %v", err)
	}
	t.Cleanup(realtimeService.Close)
	runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{Pool: pool, Now: func() time.Time { return fixedNow }, DashboardUpdates: realtimeService})
	if err != nil {
		t.Fatalf("build S16 runtime service: %v", err)
	}
	t.Cleanup(runtimeService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "s16-runtime-test", AuthService: authService, ProfilesService: profilesService, RealtimeService: realtimeService, RuntimeService: runtimeService, StatsService: statsService})
	if err != nil {
		t.Fatalf("build S16 handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create S16 cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar
	baseHarness := &runtimeHarness{client: client, conn: conn, authService: authService, profilesService: profilesService, runtimeService: runtimeService, server: server, url: server.URL, upstream: upstream}
	return &realtimeHarness{runtimeHarness: baseHarness, realtimeService: realtimeService, statsService: statsService, fixedNow: fixedNow}
}

func (h *realtimeHarness) dialWebSocket(t *testing.T, includeCookies bool) *websocket.Conn {
	t.Helper()
	requestURL, err := url.Parse(h.url)
	if err != nil {
		t.Fatalf("parse harness URL: %v", err)
	}
	headers := http.Header{}
	if includeCookies {
		cookies := h.client.Jar.Cookies(requestURL)
		cookiePairs := make([]string, 0, len(cookies))
		for _, cookie := range cookies {
			cookiePairs = append(cookiePairs, cookie.Name+"="+cookie.Value)
		}
		if len(cookiePairs) > 0 {
			headers.Set("Cookie", strings.Join(cookiePairs, "; "))
		}
	}
	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(h.url, "http://", "ws://", 1)+"/api/realtime/ws", headers)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	return conn
}

func (h *realtimeHarness) seedRealtimeDashboardRoute(t *testing.T, profileID int, suffix string) seededDashboardRoute {
	t.Helper()
	strategyID := h.seedLegacyStrategy(t, profileID, "s16-strategy-"+suffix+"-"+randomSuffix(), "round-robin")
	targetModelConfigID := h.seedModel(t, profileID, "openai", "native-"+suffix+"-"+randomSuffix(), "native", &strategyID)
	publicModelID := "public-" + suffix + "-" + randomSuffix()
	publicModelConfigID := h.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	h.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointName := "Endpoint " + suffix + " " + randomSuffix()
	endpointID := h.seedEndpoint(t, profileID, endpointName, h.upstream.baseURL("/"+suffix), "endpoint-key-"+suffix, 0)
	connectionID := h.seedConnection(t, profileID, targetModelConfigID, endpointID, "connection-"+suffix+"-"+randomSuffix(), nil, nil, 0)
	return seededDashboardRoute{PublicModelID: publicModelID, EndpointID: endpointID, EndpointName: endpointName, EndpointBaseURL: h.upstream.baseURL("/" + suffix), ConnectionID: connectionID}
}

func (h *realtimeHarness) insertDashboardActivity(t *testing.T, route seededDashboardRoute, profileID int, requestLogID int, usageEventID int, createdAt time.Time) int {
	t.Helper()
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO request_logs (id, profile_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, ingress_request_id, attempt_number, provider_correlation_id, endpoint_base_url, status_code, response_time_ms, is_stream, input_tokens, output_tokens, total_tokens, success_flag, billable_flag, priced_flag, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, request_path, endpoint_description, created_at) VALUES ($1, $2, $3, $4, 'openai', $5, $6, $7, 1, $8, $9, 200, 1200, FALSE, 11, 7, 18, TRUE, TRUE, TRUE, 1250, 'USD', '$', '/v1/chat/completions', $10, $11)`,
		requestLogID,
		profileID,
		route.PublicModelID,
		"native-target",
		route.EndpointID,
		route.ConnectionID,
		fmt.Sprintf("ingress-%d", requestLogID),
		fmt.Sprintf("provider-%d", requestLogID),
		route.EndpointBaseURL,
		route.EndpointName,
		createdAt,
	); err != nil {
		t.Fatalf("insert dashboard request log %d: %v", requestLogID, err)
	}
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, status_code, success_flag, billable_flag, priced_flag, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, attempt_count, request_path, created_at, response_time_ms) VALUES ($1, $2, $3, $4, $5, 'openai', $6, $7, 200, TRUE, TRUE, TRUE, 11, 7, 18, 1250, 'USD', '$', 1, '/v1/chat/completions', $8, 1200)`,
		usageEventID,
		profileID,
		fmt.Sprintf("ingress-%d", requestLogID),
		route.PublicModelID,
		"native-target",
		route.EndpointID,
		route.ConnectionID,
		createdAt,
	); err != nil {
		t.Fatalf("insert dashboard usage event %d: %v", usageEventID, err)
	}
	return requestLogID
}

func seedRealtimeVerifiedAuthSettings(t *testing.T, harness *realtimeHarness, username string, password string, email string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash realtime auth password: %v", err)
	}
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE app_auth_settings SET auth_enabled = TRUE, username = $1, email = $2, pending_email = NULL, email_bound_at = $3, password_hash = $4, email_verification_code_hash = NULL, email_verification_expires_at = NULL, email_verification_attempt_count = 0, token_version = 0, updated_at = $3 WHERE singleton_key = 'app'`,
		username,
		email,
		harness.fixedNow,
		string(hash),
	); err != nil {
		t.Fatalf("seed realtime auth settings: %v", err)
	}
}

type seededDashboardRoute struct {
	PublicModelID   string
	EndpointID      int
	EndpointName    string
	EndpointBaseURL string
	ConnectionID    int
}

func assertRealtimeMessageType(t *testing.T, conn *websocket.Conn, wantType string) {
	t.Helper()
	message := readWebSocketJSON(t, conn)
	if message["type"] != wantType {
		t.Fatalf("expected realtime message type %q, got %+v", wantType, message)
	}
}

func assertRealtimeMessage(t *testing.T, conn *websocket.Conn, expected map[string]any) {
	t.Helper()
	message := readWebSocketJSON(t, conn)
	for key, value := range expected {
		if message[key] != value {
			t.Fatalf("expected realtime message %s=%v, got %+v", key, value, message)
		}
	}
}

func assertNestedRequestLogProfileID(t *testing.T, message map[string]any, profileID int) {
	t.Helper()
	requestLog, ok := message["request_log"].(map[string]any)
	if !ok {
		t.Fatalf("expected realtime request_log payload, got %+v", message)
	}
	if requestLog["profile_id"] != float64(profileID) {
		t.Fatalf("expected realtime request_log.profile_id=%d, got %+v", profileID, requestLog)
	}
}

func readWebSocketJSON(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var message map[string]any
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatalf("read websocket JSON: %v", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	return message
}

func writeWebSocketJSON(t *testing.T, conn *websocket.Conn, payload map[string]any) {
	t.Helper()
	if err := conn.WriteJSON(payload); err != nil {
		t.Fatalf("write websocket JSON %+v: %v", payload, err)
	}
}

func assertWebSocketClosedWithCode(t *testing.T, conn *websocket.Conn, code int) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn.ReadMessage()
	if err == nil {
		t.Fatalf("expected websocket to close with code %d", code)
	}
	if !websocket.IsCloseError(err, code) {
		t.Fatalf("expected websocket close code %d, got %v", code, err)
	}
}

func loadRealtimeFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	_, currentFile, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("resolve realtime test file path")
	}
	fixturePath := filepath.Join(filepath.Dir(currentFile), "..", "..", "testdata", "realtime", name)
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read realtime fixture %s: %v", fixturePath, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode realtime fixture %s: %v", fixturePath, err)
	}
	return payload
}

func assertJSONShapeMatchesFixture(t *testing.T, actual any, fixture map[string]any) {
	t.Helper()
	actualRaw, err := json.Marshal(actual)
	if err != nil {
		t.Fatalf("marshal realtime payload: %v", err)
	}
	var actualPayload map[string]any
	if err := json.Unmarshal(actualRaw, &actualPayload); err != nil {
		t.Fatalf("decode realtime payload: %v", err)
	}
	assertShapeRecursive(t, actualPayload, fixture)
}

func assertShapeRecursive(t *testing.T, actual any, fixture any) {
	t.Helper()
	switch typedFixture := fixture.(type) {
	case map[string]any:
		actualMap, ok := actual.(map[string]any)
		if !ok {
			t.Fatalf("expected map shape, got %T %+v", actual, actual)
		}
		if len(actualMap) == 0 {
			return
		}
		for key, fixtureValue := range typedFixture {
			actualValue, ok := actualMap[key]
			if !ok {
				t.Fatalf("expected realtime payload key %q to exist, got %+v", key, actualMap)
			}
			assertShapeRecursive(t, actualValue, fixtureValue)
		}
	case []any:
		actualSlice, ok := actual.([]any)
		if !ok {
			t.Fatalf("expected slice shape, got %T %+v", actual, actual)
		}
		if len(typedFixture) == 0 || len(actualSlice) == 0 {
			return
		}
		assertShapeRecursive(t, actualSlice[0], typedFixture[0])
	default:
		return
	}
}
