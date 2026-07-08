package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	realtimeapi "github.com/coachpo/prism/backend/internal/httpapi/realtime"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

type realtimeHarness struct {
	*runtimeHarness
	realtimeService         *realtimeapi.Service
	statsService            *managementstats.Service
	asyncDashboardPublisher *realtimeapi.AsyncDashboardPublisher
	fixedNow                time.Time
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
	profileOneID := profiledomain.DefaultProfileID
	profileTwoID := harness.createProfile(t, "Realtime Secondary Profile")
	routeOne := harness.seedRealtimeDashboardRoute(t, profileOneID, "profile-one")
	routeTwo := harness.seedRealtimeDashboardRoute(t, profileTwoID, "profile-two")
	requestLogOne := harness.insertDashboardActivity(t, routeOne, profileOneID, 8101, 9101, harness.fixedNow.Add(-2*time.Minute))
	requestLogTwo := harness.insertDashboardActivity(t, routeTwo, profileTwoID, 8102, 9102, harness.fixedNow.Add(-90*time.Second))

	replacementConn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, replacementConn, "authenticated")
	assertRealtimeMessage(t, replacementConn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, replacementConn, map[string]any{"type": "subscribe", "profile_id": profileTwoID, "channel": "dashboard"})
	assertRealtimeMessage(t, replacementConn, map[string]any{"type": "subscribed", "profile_id": float64(profileOneID), "channel": "dashboard"})
	delivered, err := harness.realtimeService.PublishDashboardActivity(context.Background(), requestLogOne, profileOneID)
	if err != nil {
		t.Fatalf("publish profile-one dashboard activity: %v", err)
	}
	if !delivered {
		t.Fatal("expected profile-one dashboard activity delivery while subscribed")
	}
	assertDashboardActivityProfileID(t, readWebSocketJSON(t, replacementConn), profileOneID)
	delivered, err = harness.realtimeService.PublishDashboardActivity(context.Background(), requestLogTwo, profileTwoID)
	if err != nil {
		t.Fatalf("publish secondary profile dashboard activity without subscriber: %v", err)
	}
	if delivered {
		t.Fatal("expected no delivery after dashboard subscribe normalized to Default profile")
	}
	writeWebSocketJSON(t, replacementConn, map[string]any{"type": "unsubscribe"})
	assertRealtimeMessage(t, replacementConn, map[string]any{"type": "unsubscribed"})
	_ = replacementConn.Close()

	delivered, err = harness.realtimeService.PublishDashboardActivity(context.Background(), requestLogOne, profileOneID)
	if err != nil {
		t.Fatalf("publish profile-one activity without subscribers: %v", err)
	}
	if delivered {
		t.Fatal("expected no immediate delivery after unsubscribe cleanup")
	}

	channelConn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, channelConn, "authenticated")
	assertRealtimeMessage(t, channelConn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, channelConn, map[string]any{"type": "subscribe", "profile_id": profileTwoID, "channel": "dashboard"})
	assertRealtimeMessage(t, channelConn, map[string]any{"type": "subscribed", "profile_id": float64(profileOneID), "channel": "dashboard"})
	delivered, err = harness.realtimeService.PublishDashboardActivity(context.Background(), requestLogOne, profileOneID)
	if err != nil || !delivered {
		t.Fatalf("publish default profile dashboard activity: delivered=%v err=%v", delivered, err)
	}
	assertDashboardActivityProfileID(t, readWebSocketJSON(t, channelConn), profileOneID)
	writeWebSocketJSON(t, channelConn, map[string]any{"type": "unsubscribe_channel", "channel": "dashboard"})
	assertRealtimeMessage(t, channelConn, map[string]any{"type": "unsubscribed", "channel": "dashboard"})
	delivered, err = harness.realtimeService.PublishDashboardActivity(context.Background(), requestLogOne, profileOneID)
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
	assertRealtimeMessage(t, unsubscribeConn, map[string]any{"type": "subscribed", "profile_id": float64(profileOneID), "channel": "dashboard"})
	delivered, err = harness.realtimeService.PublishDashboardActivity(context.Background(), requestLogOne, profileOneID)
	if err != nil || !delivered {
		t.Fatalf("publish subscribed default profile dashboard activity: delivered=%v err=%v", delivered, err)
	}
	assertDashboardActivityProfileID(t, readWebSocketJSON(t, unsubscribeConn), profileOneID)
	writeWebSocketJSON(t, unsubscribeConn, map[string]any{"type": "unsubscribe"})
	assertRealtimeMessage(t, unsubscribeConn, map[string]any{"type": "unsubscribed"})
	delivered, err = harness.realtimeService.PublishDashboardActivity(context.Background(), requestLogOne, profileOneID)
	if err != nil {
		t.Fatalf("publish after unsubscribe: %v", err)
	}
	if delivered {
		t.Fatal("expected no delivery after unsubscribe cleanup")
	}
	_ = unsubscribeConn.Close()
}

func TestRealtimeSameOriginHandshakeAllowed(t *testing.T) {
	harness := newRealtimeHarness(t)
	conn, response, err := harness.dialWebSocketWithOrigin(t, false, harness.url)
	if err != nil {
		statusCode := 0
		if response != nil {
			statusCode = response.StatusCode
		}
		t.Fatalf("expected same-origin websocket handshake success, got err=%v status=%d", err, statusCode)
	}
	assertRealtimeMessageType(t, conn, "authenticated")
	assertRealtimeMessage(t, conn, map[string]any{"type": "heartbeat"})
	_ = conn.Close()
}

func TestRealtimeRejectsDisallowedCrossOriginHandshake(t *testing.T) {
	harness := newRealtimeHarness(t)
	conn, response, err := harness.dialWebSocketWithOrigin(t, false, "http://example.invalid")
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected cross-origin websocket handshake failure")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		statusCode := 0
		if response != nil {
			statusCode = response.StatusCode
		}
		t.Fatalf("expected forbidden websocket handshake response, got %d with err=%v", statusCode, err)
	}
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

func TestRealtimeWebSocketReleasesManagementAdmissionAfterHandshake(t *testing.T) {
	harness := newRealtimeHarnessWithConfig(t, realtimeHarnessConfig{
		ManagementDatabasePoolBudget: config.DatabasePoolBudget{MaxConns: 3},
		ManagementAdmissionBudget:    config.ManagementAdmissionBudget{M2MaxConcurrent: 1, M3MaxConcurrent: 1},
	})
	conn := harness.dialWebSocket(t, false)
	defer func() { _ = conn.Close() }()
	assertRealtimeMessageType(t, conn, "authenticated")
	assertRealtimeMessage(t, conn, map[string]any{"type": "heartbeat"})

	harness.loadDashboardSnapshot(t, harness.activeProfileID(t))
}

func TestRealtimeLogoutClosesOpenAuthenticatedWebSocket(t *testing.T) {
	harness := newRealtimeHarness(t)
	seedRealtimeVerifiedAuthSettings(t, harness, "logout-admin", "logout-password-123", "logout@example.com")
	loginResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/api/auth/login",
		map[string]any{"username": "logout-admin", "password": "logout-password-123", "session_duration": "7_days"},
		nil,
	)
	assertStatus(t, loginResponse, http.StatusOK)

	conn := harness.dialWebSocket(t, true)
	defer func() { _ = conn.Close() }()
	assertRealtimeMessage(t, conn, map[string]any{"type": "authenticated", "username": "logout-admin"})
	assertRealtimeMessage(t, conn, map[string]any{"type": "heartbeat"})

	logoutResponse := harness.requestJSON(t, http.MethodPost, "/api/auth/logout", nil, nil)
	assertStatus(t, logoutResponse, http.StatusOK)
	assertWebSocketClosedWithCode(t, conn, websocket.ClosePolicyViolation)
}

func TestRealtimeSubjectLimiterRejectsExcessAuthenticatedSockets(t *testing.T) {
	harness := newRealtimeHarness(t)
	seedRealtimeVerifiedAuthSettings(t, harness, "limited-admin", "limited-password-123", "limited@example.com")
	loginResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/api/auth/login",
		map[string]any{"username": "limited-admin", "password": "limited-password-123", "session_duration": "7_days"},
		nil,
	)
	assertStatus(t, loginResponse, http.StatusOK)

	connections := make([]*websocket.Conn, 0, 16)
	defer func() {
		for _, conn := range connections {
			_ = conn.Close()
		}
	}()
	for range 16 {
		conn := harness.dialWebSocket(t, true)
		assertRealtimeMessage(t, conn, map[string]any{"type": "authenticated", "username": "limited-admin"})
		assertRealtimeMessage(t, conn, map[string]any{"type": "heartbeat"})
		connections = append(connections, conn)
	}
	overflowConn := harness.dialWebSocket(t, true)
	assertWebSocketClosedWithCode(t, overflowConn, websocket.CloseTryAgainLater)
	_ = overflowConn.Close()

	_ = connections[0].Close()
	connections = connections[1:]
	var replacementConn *websocket.Conn
	deadline := time.Now().Add(2 * time.Second)
	for {
		replacementConn = harness.dialWebSocket(t, true)
		_ = replacementConn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		var message map[string]any
		err := replacementConn.ReadJSON(&message)
		_ = replacementConn.SetReadDeadline(time.Time{})
		if err == nil {
			if message["type"] != "authenticated" || message["username"] != "limited-admin" {
				t.Fatalf("expected replacement realtime authentication, got %+v", message)
			}
			break
		}
		_ = replacementConn.Close()
		if !websocket.IsCloseError(err, websocket.CloseTryAgainLater) {
			t.Fatalf("read replacement websocket JSON: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for released realtime subject slot, last error: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	assertRealtimeMessage(t, replacementConn, map[string]any{"type": "heartbeat"})
	connections = append(connections, replacementConn)
}

func TestDashboardSnapshotRealtimeContract(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := harness.seedRealtimeDashboardRoute(t, profileID, "snapshot-contract")
	harness.insertDashboardActivity(t, route, profileID, 8201, 9201, harness.fixedNow.Add(-30*time.Second))

	message, err := harness.realtimeService.BuildDashboardSnapshot(context.Background(), profileID)
	if err != nil {
		t.Fatalf("build dashboard snapshot payload: %v", err)
	}
	fixture := loadRealtimeFixture(t, "dashboard-snapshot.json")
	assertJSONShapeMatchesFixture(t, message, fixture)
	if message.Type != "dashboard.snapshot" || message.ProfileID != profileID {
		t.Fatalf("expected dashboard.snapshot profile envelope, got %+v", message)
	}
	assertDashboardSnapshotEnvelopeShape(t, message)
	if len(message.Snapshot.RoutingHealthMap.Links) == 0 || message.Snapshot.SnapshotRevision == "" {
		t.Fatalf("expected built snapshot to include routing and revision, got %+v", message.Snapshot)
	}
}

func TestDashboardActivityRealtimeContract(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := harness.seedRealtimeDashboardRoute(t, profileID, "activity-contract")
	requestLogID := harness.insertDashboardActivity(t, route, profileID, 8202, 9202, harness.fixedNow.Add(-30*time.Second))

	message, err := harness.realtimeService.BuildDashboardActivity(context.Background(), requestLogID, profileID)
	if err != nil {
		t.Fatalf("build dashboard activity payload: %v", err)
	}
	fixture := loadRealtimeFixture(t, "dashboard-activity.json")
	assertJSONShapeMatchesFixture(t, message, fixture)
	if message.Type != "dashboard.activity" || message.ProfileID != profileID {
		t.Fatalf("expected dashboard.activity profile envelope, got %+v", message)
	}
	assertDashboardActivityEnvelopeShape(t, message)
	if message.Activity.RequestLogID != requestLogID {
		t.Fatalf("expected single activity item for request log %d, got %+v", requestLogID, message.Activity)
	}
	if message.Activity.ModelLabel != route.PublicModelLabel || message.Activity.ResolvedTargetModelLabel == nil || *message.Activity.ResolvedTargetModelLabel != route.TargetModelLabel {
		t.Fatalf("expected activity labels to reuse recent-activity DTO semantics, got %+v", message.Activity)
	}
	if message.ActivityWatermark.LatestRequestLogID == nil || *message.ActivityWatermark.LatestRequestLogID != requestLogID {
		t.Fatalf("expected activity watermark to track the activity item, got %+v", message.ActivityWatermark)
	}
}

func TestRequestLogFailoverAttemptDetailUnaffectedByDashboardSplit(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "dashboard-split-public-" + suffix
	targetModelID := "dashboard-split-target-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-dashboard-split-secondary"})
	strategyID := harness.seedLegacyStrategy(t, profileID, "dashboard-split-fill-first-"+suffix, "fill-first")
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "dashboard-split-primary-"+suffix, primaryUpstream.baseURL("/dashboard-split/primary"), "dashboard-split-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "dashboard-split-secondary-"+suffix, secondaryUpstream.baseURL("/dashboard-split/secondary"), "dashboard-split-secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, primaryEndpointID, "dashboard-split-primary-connection-"+suffix, nil, nil, 0)
	secondaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, secondaryEndpointID, "dashboard-split-secondary-connection-"+suffix, nil, nil, 1)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "preserve failover attempts while splitting dashboard metrics"}},
		"model":    publicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeAttemptSequence(t, harness.conn, profileID, []runtimeRequestLogAttempt{{
		AttemptNumber: 1,
		ConnectionID:  primaryConnectionID,
		EndpointID:    primaryEndpointID,
		StatusCode:    http.StatusServiceUnavailable,
		SuccessFlag:   false,
	}, {
		AttemptNumber: 2,
		ConnectionID:  secondaryConnectionID,
		EndpointID:    secondaryEndpointID,
		StatusCode:    http.StatusOK,
		SuccessFlag:   true,
	}})

	ingressRequestID := loadLatestRuntimeIngressRequestID(t, harness.conn, profileID)
	var primaryRequestLogID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 AND attempt_number = 1`, profileID, ingressRequestID).Scan(&primaryRequestLogID); err != nil {
		t.Fatalf("load primary failover request-log id: %v", err)
	}
	detailResponse := harness.requestJSON(t, http.MethodGet, fmt.Sprintf("/api/stats/requests/%d", primaryRequestLogID), nil, runtimeModelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	requestDetail := detailPayload["request"].(map[string]any)
	routingDetail := detailPayload["routing"].(map[string]any)
	summaryDetail := detailPayload["summary"].(map[string]any)
	if requestDetail["attempt_number"] != float64(1) || summaryDetail["status_code"] != float64(http.StatusServiceUnavailable) || routingDetail["terminal_target_id"] != float64(primaryConnectionID) {
		t.Fatalf("expected request-log detail to preserve failed primary attempt, got %+v", detailPayload)
	}

	snapshot := harness.loadDashboardSnapshot(t, profileID)
	primaryLink := realtimeDashboardRoutingLinkForEndpoint(t, snapshot.RoutingHealthMap, primaryEndpointID)
	secondaryLink := realtimeDashboardRoutingLinkForEndpoint(t, snapshot.RoutingHealthMap, secondaryEndpointID)
	if primaryLink.RequestCount24H != 0 || primaryLink.SuccessCount24H != 0 || primaryLink.ErrorCount24H != 0 {
		t.Fatalf("expected dashboard routing health to ignore failed request-log-only attempt, got %+v", primaryLink)
	}
	if secondaryLink.RequestCount24H != 1 || secondaryLink.SuccessCount24H != 1 || secondaryLink.ErrorCount24H != 0 {
		t.Fatalf("expected dashboard routing health to use final usage event only, got %+v", secondaryLink)
	}
}

func TestAnalyticsRealtimeProtocolFixture(t *testing.T) {
	generatedAt := time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC)
	successCount := 40
	failedCount := 2
	pricedRequestCount := 40
	unpricedRequestCount := 2
	inputTokens := 500
	outputTokens := 900
	cachedTokens := 20
	reasoningTokens := 10
	p50TTFTMS := 320
	p95TTFTMS := 900
	avgOutputRateTPS := 48.5
	endpointID := 12
	profileID := 2
	preset := "1h"
	startAt := generatedAt.Add(-time.Hour)
	dayStart := time.Date(2026, time.April, 19, 0, 0, 0, 0, time.UTC)
	message := realtimeapi.AnalyticsSnapshotMessage{
		Type:        "analytics.snapshot",
		Channel:     "analytics",
		ProfileID:   profileID,
		Preset:      preset,
		Sequence:    7,
		GeneratedAt: generatedAt,
		Snapshot: statsdomain.UsageSnapshotResponse{
			GeneratedAt: generatedAt,
			TimeRange:   statsdomain.UsageSnapshotTimeRange{Preset: preset, StartAt: &startAt, EndAt: generatedAt},
			Currency:    statsdomain.UsageSnapshotCurrency{Code: "USD", Symbol: "$"},
			Overview: statsdomain.UsageSnapshotOverview{
				TotalRequests:        42,
				SuccessRequests:      40,
				FailedRequests:       2,
				SuccessRate:          95.24,
				TotalTokens:          1400,
				InputTokens:          500,
				OutputTokens:         900,
				CachedTokens:         20,
				ReasoningTokens:      10,
				AverageRPM:           0.7,
				AverageTPM:           23.333,
				TotalCostMicros:      1250000,
				RollingWindowMinutes: 5,
				RollingRequestCount:  4,
				RollingTokenCount:    120,
				RollingRPM:           0.8,
				RollingTPM:           24,
			},
			RequestTrends:         statsdomain.UsageRequestTrends{Hourly: []statsdomain.UsageRequestTrendSeries{{Key: "openai", Label: "OpenAI", TotalRequests: 42, Points: []statsdomain.UsageRequestTrendPoint{{BucketStart: startAt, RequestCount: 42, SuccessCount: 40, FailedCount: 2, RPM: 0.7}}}}, Daily: []statsdomain.UsageRequestTrendSeries{{Key: "openai", Label: "OpenAI", TotalRequests: 42, Points: []statsdomain.UsageRequestTrendPoint{{BucketStart: dayStart, RequestCount: 42, SuccessCount: 40, FailedCount: 2, RPM: 0.029}}}}},
			TokenUsageTrends:      statsdomain.UsageTokenUsageTrends{Hourly: []statsdomain.UsageTokenTrendSeries{{Key: "gpt-4o", Label: "GPT-4o Proxy", TotalTokens: 1400, Points: []statsdomain.UsageTokenTrendPoint{{BucketStart: startAt, TotalTokens: 1400, InputTokens: 500, OutputTokens: 900, CachedTokens: 20, ReasoningTokens: 10, TPM: 23.333}}}}, Daily: []statsdomain.UsageTokenTrendSeries{{Key: "gpt-4o", Label: "GPT-4o Proxy", TotalTokens: 1400, Points: []statsdomain.UsageTokenTrendPoint{{BucketStart: dayStart, TotalTokens: 1400, InputTokens: 500, OutputTokens: 900, CachedTokens: 20, ReasoningTokens: 10, TPM: 0.972}}}}},
			TokenTypeBreakdown:    statsdomain.UsageTokenTypeBreakdown{Hourly: []statsdomain.UsageTokenTypeBreakdownPoint{{BucketStart: startAt, InputTokens: 500, OutputTokens: 900, CachedTokens: 20, ReasoningTokens: 10}}, Daily: []statsdomain.UsageTokenTypeBreakdownPoint{{BucketStart: dayStart, InputTokens: 500, OutputTokens: 900, CachedTokens: 20, ReasoningTokens: 10}}},
			CostOverview:          statsdomain.UsageCostOverview{TotalCostMicros: 1250000, PricedRequestCount: 40, UnpricedRequestCount: 2, Hourly: []statsdomain.UsageCostOverviewPoint{{BucketStart: startAt, TotalCostMicros: 1250000}}, Daily: []statsdomain.UsageCostOverviewPoint{{BucketStart: dayStart, TotalCostMicros: 1250000}}},
			EndpointStatistics:    []statsdomain.UsageEndpointStatistic{{EndpointID: &endpointID, EndpointLabel: "Primary OpenAI", RequestCount: 42, SuccessRate: 95.24, P50TTFTMS: &p50TTFTMS, P95TTFTMS: &p95TTFTMS, AvgOutputRateTPS: &avgOutputRateTPS, TotalTokens: 1400, TotalCostMicros: 1250000}},
			ModelStatistics:       []statsdomain.UsageModelStatistic{{ModelID: "gpt-4o", ModelLabel: "GPT-4o Proxy", RequestCount: 42, SuccessCount: &successCount, FailedCount: &failedCount, PricedRequestCount: &pricedRequestCount, UnpricedRequestCount: &unpricedRequestCount, SuccessRate: 95.24, P50TTFTMS: &p50TTFTMS, P95TTFTMS: &p95TTFTMS, InputTokens: &inputTokens, OutputTokens: &outputTokens, CachedTokens: &cachedTokens, ReasoningTokens: &reasoningTokens, TotalTokens: 1400, TotalCostMicros: 1250000, AvgOutputRateTPS: &avgOutputRateTPS}},
			ProxyAPIKeyStatistics: []statsdomain.UsageProxyAPIKeyStatistic{{ProxyAPIKeyID: nil, ProxyAPIKeyLabel: "Direct / unauthenticated", RequestCount: 42, SuccessRate: 95.24, TotalTokens: 1400, TotalCostMicros: 1250000}},
		},
		EndpointModelStatisticsByEndpointID: map[string][]statsdomain.UsageModelStatistic{"12": {{ModelID: "gpt-4o", ModelLabel: "GPT-4o Proxy", RequestCount: 42, SuccessCount: &successCount, FailedCount: &failedCount, PricedRequestCount: &pricedRequestCount, UnpricedRequestCount: &unpricedRequestCount, SuccessRate: 95.24, P50TTFTMS: &p50TTFTMS, P95TTFTMS: &p95TTFTMS, TotalTokens: 1400, TotalCostMicros: 1250000, AvgOutputRateTPS: &avgOutputRateTPS}}},
	}
	fixture := loadRealtimeFixture(t, "analytics-snapshot.json")
	assertJSONShapeMatchesFixture(t, message, fixture)
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal analytics snapshot: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode analytics snapshot: %v", err)
	}
	snapshotPayload, ok := payload["snapshot"].(map[string]any)
	if !ok {
		t.Fatalf("expected analytics snapshot object, got %+v", payload["snapshot"])
	}
	if _, ok := snapshotPayload["service_health"]; ok {
		t.Fatalf("expected analytics snapshot to omit service_health, got %+v", snapshotPayload["service_health"])
	}
	if message.Type != "analytics.snapshot" || message.Channel != "analytics" || message.ProfileID != profileID || message.Preset != preset {
		t.Fatalf("unexpected analytics snapshot envelope: %+v", message)
	}
	errorMessage := realtimeapi.AnalyticsErrorMessage{Type: "analytics.error", Channel: "analytics", ProfileID: &profileID, Preset: &preset, Code: "snapshot_failed", Message: "failed to build analytics snapshot"}
	errorRaw, err := json.Marshal(errorMessage)
	if err != nil {
		t.Fatalf("marshal analytics error: %v", err)
	}
	var errorPayload map[string]any
	if err := json.Unmarshal(errorRaw, &errorPayload); err != nil {
		t.Fatalf("decode analytics error: %v", err)
	}
	for _, key := range []string{"type", "channel", "profile_id", "preset", "code", "message"} {
		if _, ok := errorPayload[key]; !ok {
			t.Fatalf("expected analytics error key %q, got %+v", key, errorPayload)
		}
	}
}

func TestRealtimeCancellation(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := harness.activeProfileID(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := harness.realtimeService.BuildAnalyticsSnapshot(ctx, profileID, "1h", harness.fixedNow); err == nil {
		t.Fatal("expected canceled context to abort realtime analytics snapshot build")
	}
}

func TestRealtimeAnalyticsSnapshotParity(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := harness.seedRealtimeDashboardRoute(t, profileID, "analytics-parity")
	harness.insertDashboardActivity(t, route, profileID, 8400, 9400, harness.fixedNow.Add(-10*time.Minute))

	usageResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/usage-snapshot?preset=1h", nil, runtimeModelHeader(profileID))
	assertStatus(t, usageResponse, http.StatusOK)
	var restUsage statsdomain.UsageSnapshotResponse
	decodeJSONResponse(t, usageResponse, &restUsage)
	assertUsageSnapshotMergedTokenSemantics(t, restUsage, 11, 7, 25, 6, 1)

	fromTime := url.QueryEscape(restUsage.TimeRange.StartAt.Format(time.RFC3339Nano))
	toTime := url.QueryEscape(restUsage.TimeRange.EndAt.Format(time.RFC3339Nano))
	modelsResponse := harness.requestJSON(t, http.MethodGet, fmt.Sprintf("/api/stats/endpoints/%d/models?from_time=%s&to_time=%s", route.EndpointID, fromTime, toTime), nil, runtimeModelHeader(profileID))
	assertStatus(t, modelsResponse, http.StatusOK)
	var restModels []statsdomain.EndpointModelStatistic
	decodeJSONResponse(t, modelsResponse, &restModels)

	message, err := harness.realtimeService.BuildAnalyticsSnapshot(context.Background(), profileID, "1h", harness.fixedNow)
	if err != nil {
		t.Fatalf("build analytics snapshot: %v", err)
	}
	if !message.GeneratedAt.Equal(harness.fixedNow) {
		t.Fatalf("expected analytics message generated_at to use referenceNow %s, got %s", harness.fixedNow, message.GeneratedAt)
	}
	if !reflect.DeepEqual(restUsage, message.Snapshot) {
		t.Fatalf("expected analytics snapshot to match REST usage snapshot, got rest=%+v realtime=%+v", restUsage, message.Snapshot)
	}
	assertUsageSnapshotMergedTokenSemantics(t, message.Snapshot, 11, 7, 25, 6, 1)
	if message.Snapshot.TimeRange.StartAt == nil || !message.Snapshot.TimeRange.StartAt.Equal(*restUsage.TimeRange.StartAt) || !message.Snapshot.TimeRange.EndAt.Equal(restUsage.TimeRange.EndAt) {
		t.Fatalf("expected realtime snapshot window to match REST window, got rest=%+v realtime=%+v", restUsage.TimeRange, message.Snapshot.TimeRange)
	}
	realtimeModels := message.EndpointModelStatisticsByEndpointID[fmt.Sprint(route.EndpointID)]
	if !reflect.DeepEqual(usageModelStatisticsFromEndpointTest(restModels), realtimeModels) {
		t.Fatalf("expected endpoint model stats to match REST custom-window stats, got rest=%+v realtime=%+v", restModels, realtimeModels)
	}
	if _, ok := message.EndpointModelStatisticsByEndpointID[fmt.Sprint(route.EndpointID)]; !ok {
		t.Fatalf("expected endpoint model stats keyed by endpoint ID string, got %+v", message.EndpointModelStatisticsByEndpointID)
	}
}

func TestRealtimeAnalyticsSubscribeInitialSnapshot(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := profiledomain.DefaultProfileID
	route := harness.seedRealtimeDashboardRoute(t, profileID, "analytics-initial")
	harness.insertDashboardActivity(t, route, profileID, 8401, 9401, harness.fixedNow.Add(-10*time.Minute))

	conn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, conn, "authenticated")
	assertRealtimeMessage(t, conn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, conn, map[string]any{"type": "subscribe", "profile_id": 999999, "channel": "analytics", "preset": "1h"})
	assertRealtimeMessage(t, conn, map[string]any{"type": "subscribed", "profile_id": float64(profileID), "channel": "analytics", "preset": "1h"})
	snapshot := readWebSocketJSON(t, conn)
	assertAnalyticsSnapshot(t, snapshot, profileID, "1h", float64(1))
	if snapshotPayload := snapshot["snapshot"].(map[string]any); snapshotPayload["overview"].(map[string]any)["total_requests"] != float64(1) {
		t.Fatalf("expected initial analytics snapshot to include seeded usage, got %+v", snapshotPayload["overview"])
	}
	endpointStats := snapshot["endpoint_model_statistics_by_endpoint_id"].(map[string]any)
	if _, ok := endpointStats[fmt.Sprint(route.EndpointID)]; !ok {
		t.Fatalf("expected endpoint model statistics for endpoint %d, got %+v", route.EndpointID, endpointStats)
	}

	harness.insertDashboardActivity(t, route, profileID, 8403, 9403, harness.fixedNow.Add(-2*time.Minute))
	writeWebSocketJSON(t, conn, map[string]any{"type": "refresh", "profile_id": 999999, "channel": "analytics", "preset": "1h"})
	refreshedSnapshot := readWebSocketJSON(t, conn)
	assertAnalyticsSnapshot(t, refreshedSnapshot, profileID, "1h", float64(2))
	if snapshotPayload := refreshedSnapshot["snapshot"].(map[string]any); snapshotPayload["overview"].(map[string]any)["total_requests"] != float64(2) {
		t.Fatalf("expected refreshed analytics snapshot to include newly seeded usage, got %+v", snapshotPayload["overview"])
	}
	writeWebSocketJSON(t, conn, map[string]any{"type": "ping"})
	assertRealtimeMessage(t, conn, map[string]any{"type": "pong"})
	writeWebSocketJSON(t, conn, map[string]any{"type": "unsubscribe_channel", "channel": "analytics", "preset": "1h"})
	assertRealtimeMessage(t, conn, map[string]any{"type": "unsubscribed", "channel": "analytics", "preset": "1h"})
	writeWebSocketJSON(t, conn, map[string]any{"type": "refresh", "profile_id": 999999, "channel": "analytics", "preset": "1h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "scope_not_subscribed")
	_ = conn.Close()
}

func TestRealtimeAnalyticsSubscribeValidationErrors(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := profiledomain.DefaultProfileID
	route := harness.seedRealtimeDashboardRoute(t, profileID, "analytics-validation")
	harness.insertDashboardActivity(t, route, profileID, 8402, 9402, harness.fixedNow.Add(-5*time.Minute))

	conn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, conn, "authenticated")
	assertRealtimeMessage(t, conn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, conn, map[string]any{"type": "subscribe", "channel": "analytics", "preset": "2h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "invalid_preset")
	writeWebSocketJSON(t, conn, map[string]any{"type": "subscribe", "channel": "analytics", "preset": "1h"})
	assertRealtimeMessage(t, conn, map[string]any{"type": "subscribed", "profile_id": float64(profileID), "channel": "analytics", "preset": "1h"})
	assertAnalyticsSnapshot(t, readWebSocketJSON(t, conn), profileID, "1h", float64(1))
	writeWebSocketJSON(t, conn, map[string]any{"type": "refresh", "profile_id": 999999, "channel": "analytics", "preset": "1h"})
	assertAnalyticsSnapshot(t, readWebSocketJSON(t, conn), profileID, "1h", float64(2))
	writeWebSocketJSON(t, conn, map[string]any{"type": "unsubscribe_channel", "channel": "analytics", "preset": "1h"})
	assertRealtimeMessage(t, conn, map[string]any{"type": "unsubscribed", "channel": "analytics", "preset": "1h"})
	writeWebSocketJSON(t, conn, map[string]any{"type": "refresh", "profile_id": 999999, "channel": "analytics", "preset": "1h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "scope_not_subscribed")
	writeWebSocketJSON(t, conn, map[string]any{"type": "unsubscribe_channel", "channel": "analytics"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "invalid_preset")
	writeWebSocketJSON(t, conn, map[string]any{"type": "unsubscribe_channel", "channel": "analytics", "preset": "2h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "invalid_preset")
	writeWebSocketJSON(t, conn, map[string]any{"type": "unsubscribe_channel", "channel": "analytics", "preset": "1h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "scope_not_subscribed")
	writeWebSocketJSON(t, conn, map[string]any{"type": 12, "profile_id": profileID, "channel": "analytics", "preset": "1h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "malformed_message")
	_ = conn.Close()
}

func TestDashboardRealtimeDelivery(t *testing.T) {
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
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "deliver dashboard activity"}}, "model": route.PublicModelID},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	activity := readDashboardMessageByType(t, conn, "dashboard.activity")
	assertNoMixedDashboardUpdateEnvelope(t, activity)
	item, ok := activity["activity"].(map[string]any)
	if !ok {
		t.Fatalf("expected single activity object, got %+v", activity)
	}
	if item["model_id"] != route.PublicModelID || item["model_label"] != route.PublicModelLabel || item["resolved_target_model_label"] != route.TargetModelLabel {
		t.Fatalf("unexpected delivered activity payload: %+v", item)
	}
	if _, ok := activity["snapshot"]; ok {
		t.Fatalf("did not expect dashboard.activity to include snapshot, got %+v", activity)
	}
	delivered, err := harness.realtimeService.PublishDashboardSnapshot(context.Background(), profileID)
	if err != nil {
		t.Fatalf("publish explicit dashboard snapshot after activity: %v", err)
	}
	if !delivered {
		t.Fatal("expected explicit dashboard snapshot publish to deliver while subscribed")
	}
	snapshot := readDashboardMessageByType(t, conn, "dashboard.snapshot")
	assertNoMixedDashboardUpdateEnvelope(t, snapshot)
	metricSnapshot := snapshot["snapshot"].(map[string]any)["metric_snapshot"].(map[string]any)
	if metricSnapshot["total_requests"] != float64(1) || metricSnapshot["success_rate"] != float64(100) {
		t.Fatalf("unexpected delivered metric_snapshot payload: %+v", metricSnapshot)
	}
	_ = conn.Close()
}

func TestManagementAuthSettingsSnapshotInvalidation(t *testing.T) {
	harness := newRealtimeHarness(t)
	seedRealtimeVerifiedAuthSettings(t, harness, "snapshot-admin", "snapshot-password-123", "snapshot@example.com")

	loginResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/api/auth/login",
		map[string]any{"username": "snapshot-admin", "password": "snapshot-password-123", "session_duration": "7_days"},
		nil,
	)
	assertStatus(t, loginResponse, http.StatusOK)

	baselineSession := harness.requestJSON(t, http.MethodGet, "/api/auth/session", nil, nil)
	assertStatus(t, baselineSession, http.StatusOK)

	baselineConn := harness.dialWebSocket(t, true)
	assertRealtimeMessage(t, baselineConn, map[string]any{"type": "authenticated", "username": "snapshot-admin"})
	assertRealtimeMessage(t, baselineConn, map[string]any{"type": "heartbeat"})
	defer func() { _ = baselineConn.Close() }()

	updatedPassword := "snapshot-password-456"
	updateResponse := harness.requestJSON(
		t,
		http.MethodPut,
		"/api/settings/auth",
		map[string]any{"auth_enabled": true, "username": "snapshot-admin", "password": updatedPassword},
		nil,
	)
	assertStatus(t, updateResponse, http.StatusOK)
	assertWebSocketClosedWithCode(t, baselineConn, websocket.ClosePolicyViolation)

	staleSession := harness.requestJSON(t, http.MethodGet, "/api/auth/session", nil, nil)
	assertStatus(t, staleSession, http.StatusUnauthorized)

	staleConn := harness.dialWebSocket(t, true)
	assertWebSocketClosedWithCode(t, staleConn, websocket.ClosePolicyViolation)

	reloginResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/api/auth/login",
		map[string]any{"username": "snapshot-admin", "password": updatedPassword, "session_duration": "7_days"},
		nil,
	)
	assertStatus(t, reloginResponse, http.StatusOK)

	freshSession := harness.requestJSON(t, http.MethodGet, "/api/auth/session", nil, nil)
	assertStatus(t, freshSession, http.StatusOK)

	freshConn := harness.dialWebSocket(t, true)
	assertRealtimeMessage(t, freshConn, map[string]any{"type": "authenticated", "username": "snapshot-admin"})
	assertRealtimeMessage(t, freshConn, map[string]any{"type": "heartbeat"})
	_ = freshConn.Close()
}

func TestRealtimeDashboardTopologyParity(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := harness.seedRealtimeDashboardRoute(t, profileID, "consistency")
	harness.insertDashboardActivity(t, route, profileID, 8301, 9301, harness.fixedNow)
	from24h := url.QueryEscape(harness.fixedNow.Add(-24 * time.Hour).Format(time.RFC3339))
	to24h := url.QueryEscape(harness.fixedNow.Format(time.RFC3339))

	dashboardResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/dashboard", nil, runtimeModelHeader(profileID))
	assertStatus(t, dashboardResponse, http.StatusOK)
	var dashboardSnapshot statsdomain.DashboardSnapshot
	decodeJSONResponse(t, dashboardResponse, &dashboardSnapshot)

	summaryResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/summary?from_time="+from24h, nil, runtimeModelHeader(profileID))
	assertStatus(t, summaryResponse, http.StatusOK)
	var summary statsdomain.StatsSummaryResponse
	decodeJSONResponse(t, summaryResponse, &summary)

	apiFamilyResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/summary?from_time="+from24h+"&group_by=api_family", nil, runtimeModelHeader(profileID))
	assertStatus(t, apiFamilyResponse, http.StatusOK)
	var apiFamily statsdomain.StatsSummaryResponse
	decodeJSONResponse(t, apiFamilyResponse, &apiFamily)

	throughputResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/throughput?from_time="+from24h+"&to_time="+to24h, nil, runtimeModelHeader(profileID))
	assertStatus(t, throughputResponse, http.StatusOK)
	var throughput statsdomain.ThroughputStatsResponse
	decodeJSONResponse(t, throughputResponse, &throughput)

	usageResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/usage-snapshot?preset=1h", nil, runtimeModelHeader(profileID))
	assertStatus(t, usageResponse, http.StatusOK)
	var usage statsdomain.UsageSnapshotResponse
	decodeJSONResponse(t, usageResponse, &usage)

	message, err := harness.realtimeService.BuildDashboardSnapshot(context.Background(), profileID)
	if err != nil {
		t.Fatalf("build dashboard snapshot after rest snapshot warmup: %v", err)
	}
	if !reflect.DeepEqual(dashboardSnapshot, message.Snapshot) {
		t.Fatalf("expected /api/stats/dashboard to match realtime snapshot, got rest=%+v realtime=%+v", dashboardSnapshot, message.Snapshot)
	}
	if !reflect.DeepEqual(dashboardSnapshot.TopologyGraph, message.Snapshot.TopologyGraph) {
		t.Fatalf("expected topology graph parity between REST and realtime, got rest=%+v realtime=%+v", dashboardSnapshot.TopologyGraph, message.Snapshot.TopologyGraph)
	}
	if summary.TotalRequests != message.Snapshot.MetricSnapshot.TotalRequests || summary.SuccessRate != message.Snapshot.MetricSnapshot.SuccessRate {
		t.Fatalf("expected /api/stats/summary to stay coherent with realtime metric_snapshot, got rest=%+v realtime=%+v", summary, message.Snapshot.MetricSnapshot)
	}
	if !reflect.DeepEqual(apiFamily.Groups, message.Snapshot.APIFamilyRows) {
		t.Fatalf("expected /api/stats/summary?group_by=api_family to match realtime api_family_rows, got rest=%+v realtime=%+v", apiFamily.Groups, message.Snapshot.APIFamilyRows)
	}
	if throughput.TotalRequests != message.Snapshot.MetricSnapshot.AverageRPMRequestTotal {
		t.Fatalf("expected /api/stats/throughput to match realtime metric_snapshot average_rpm_request_total, got rest=%+v realtime=%+v", throughput, message.Snapshot.MetricSnapshot)
	}
	if usage.Overview.TotalRequests != message.Snapshot.MetricSnapshot.TotalRequests || usage.Overview.TotalTokens != summary.TotalTokens {
		t.Fatalf("expected /api/stats/usage-snapshot?preset=1h to stay coherent with the shared dashboard aggregate totals, got usage=%+v snapshot=%+v", usage.Overview, message.Snapshot.MetricSnapshot)
	}

	conn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, conn, "authenticated")
	assertRealtimeMessage(t, conn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, conn, map[string]any{"type": "subscribe", "profile_id": profileID, "channel": "dashboard"})
	assertRealtimeMessage(t, conn, map[string]any{"type": "subscribed", "profile_id": float64(profileID), "channel": "dashboard"})
	delivered, err := harness.realtimeService.PublishDashboardSnapshot(context.Background(), profileID)
	if err != nil {
		t.Fatalf("publish dashboard snapshot after rest snapshot warmup: %v", err)
	}
	if !delivered {
		t.Fatal("expected warmed dashboard aggregate publish to deliver while subscribed")
	}
	realtimeSnapshot := decodeRealtimeDashboardSnapshot(t, readWebSocketJSON(t, conn))
	if !reflect.DeepEqual(dashboardSnapshot.TopologyGraph, realtimeSnapshot.TopologyGraph) {
		t.Fatalf("expected websocket topology graph to match REST snapshot, got rest=%+v realtime=%+v", dashboardSnapshot.TopologyGraph, realtimeSnapshot.TopologyGraph)
	}
	_ = conn.Close()
}

func TestDashboardSnapshotReplayWithoutRequestLog(t *testing.T) {
	harness := newRealtimeHarnessWithConfig(t, realtimeHarnessConfig{UseAsyncDashboardPublisher: true})
	profileID := harness.activeProfileID(t)
	route := harness.seedRealtimeDashboardRoute(t, profileID, "invalidation")
	harness.insertDashboardActivity(t, route, profileID, 8351, 9351, harness.fixedNow)

	baselineResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/dashboard", nil, runtimeModelHeader(profileID))
	assertStatus(t, baselineResponse, http.StatusOK)
	var baseline statsdomain.DashboardSnapshot
	decodeJSONResponse(t, baselineResponse, &baseline)
	baselinePublicNode := dashboardTopologyNodeForModelID(baseline.TopologyGraph, route.PublicModelID)
	if baselinePublicNode == nil || baselinePublicNode.Label != route.PublicModelLabel {
		t.Fatalf("expected warmed dashboard topology to include requested model label %q, got %+v", route.PublicModelLabel, baseline.TopologyGraph.Nodes)
	}
	baselineTargetNode := dashboardTopologyNodeForModelID(baseline.TopologyGraph, route.TargetModelID)
	if baselineTargetNode == nil || baselineTargetNode.Label != route.TargetModelLabel {
		t.Fatalf("expected warmed dashboard topology to include final target model label %q, got %+v", route.TargetModelLabel, baseline.TopologyGraph.Nodes)
	}

	conn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, conn, "authenticated")
	assertRealtimeMessage(t, conn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, conn, map[string]any{"type": "subscribe", "profile_id": profileID, "channel": "dashboard"})
	assertRealtimeMessage(t, conn, map[string]any{"type": "subscribed", "profile_id": float64(profileID), "channel": "dashboard"})
	delivered, err := harness.realtimeService.PublishDashboardSnapshot(context.Background(), profileID)
	if err != nil {
		t.Fatalf("publish warmed dashboard snapshot before model mutation: %v", err)
	}
	if !delivered {
		t.Fatal("expected initial dashboard delivery while subscribed")
	}
	initialRealtimeSnapshot := decodeRealtimeDashboardSnapshot(t, readWebSocketJSON(t, conn))
	if !reflect.DeepEqual(baseline.TopologyGraph, initialRealtimeSnapshot.TopologyGraph) {
		t.Fatalf("expected initial realtime topology graph to match REST snapshot, got rest=%+v realtime=%+v", baseline.TopologyGraph, initialRealtimeSnapshot.TopologyGraph)
	}

	var targetModelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM model_configs WHERE profile_id = $1 AND model_id = $2`, profileID, route.TargetModelID).Scan(&targetModelConfigID); err != nil {
		t.Fatalf("load target model config id: %v", err)
	}
	updatedLabel := "Updated invalidation route"
	updateResponse := harness.requestJSON(t, http.MethodPut, fmt.Sprintf("/api/models/%d", targetModelConfigID), map[string]any{"display_name": updatedLabel}, runtimeModelHeader(profileID))
	assertStatus(t, updateResponse, http.StatusOK)

	mutatedRealtimeSnapshot := decodeRealtimeDashboardSnapshot(t, readWebSocketJSON(t, conn))
	freshResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/dashboard", nil, runtimeModelHeader(profileID))
	assertStatus(t, freshResponse, http.StatusOK)
	var fresh statsdomain.DashboardSnapshot
	decodeJSONResponse(t, freshResponse, &fresh)
	if !reflect.DeepEqual(fresh.TopologyGraph, mutatedRealtimeSnapshot.TopologyGraph) {
		t.Fatalf("expected invalidated realtime topology graph to match fresh REST snapshot, got rest=%+v realtime=%+v", fresh.TopologyGraph, mutatedRealtimeSnapshot.TopologyGraph)
	}
	freshTargetNode := dashboardTopologyNodeForModelID(fresh.TopologyGraph, route.TargetModelID)
	if freshTargetNode == nil || freshTargetNode.Label != updatedLabel {
		t.Fatalf("expected dashboard topology cache to invalidate final target label to %q, got %+v", updatedLabel, fresh.TopologyGraph.Nodes)
	}
	freshPublicNode := dashboardTopologyNodeForModelID(fresh.TopologyGraph, route.PublicModelID)
	if freshPublicNode == nil || freshPublicNode.Label != route.PublicModelLabel {
		t.Fatalf("expected requested model node to remain labeled %q after target mutation, got %+v", route.PublicModelLabel, fresh.TopologyGraph.Nodes)
	}
	_ = conn.Close()
}

func TestAsyncDashboardPublisherRefreshesWarmedSnapshotOnlyFromExplicitSnapshotPublishWithoutSubscribers(t *testing.T) {
	harness := newRealtimeHarnessWithConfig(t, realtimeHarnessConfig{UseAsyncDashboardPublisher: true})
	profileID := harness.activeProfileID(t)
	route := harness.seedRealtimeDashboardRoute(t, profileID, "async-no-subscriber")

	baseline := harness.loadDashboardSnapshot(t, profileID)
	if baseline.MetricSnapshot.TotalRequests != 0 {
		t.Fatalf("expected warmed baseline dashboard snapshot to start empty, got %+v", baseline.MetricSnapshot)
	}

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"model":    route.PublicModelID,
			"messages": []map[string]any{{"role": "user", "content": "refresh dashboard without subscribers"}},
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	waitForRealtimeAsyncDashboardDrain(t, harness.asyncDashboardPublisher, 5*time.Second)

	activityOnlySnapshot := harness.loadDashboardSnapshot(t, profileID)
	if activityOnlySnapshot.MetricSnapshot.TotalRequests != 0 {
		t.Fatalf("expected runtime activity materialization not to rebuild dashboard aggregate, got %+v", activityOnlySnapshot.MetricSnapshot)
	}

	queued, err := harness.asyncDashboardPublisher.PublishDashboardSnapshot(context.Background(), profileID)
	if err != nil {
		t.Fatalf("publish explicit dashboard snapshot refresh without subscribers: %v", err)
	}
	if !queued {
		t.Fatal("expected explicit snapshot refresh to queue without subscribers")
	}
	fresh := harness.waitForDashboardTotalRequests(t, profileID, 1, 5*time.Second)
	if fresh.MetricSnapshot.TotalRequests != 1 || fresh.SnapshotRevision == "" {
		t.Fatalf("expected explicit async refresh to rebuild dashboard aggregate, got %+v", fresh)
	}
}

func TestAsyncDashboardPublisherNormalizesCallerProfilesToDefaultProfile(t *testing.T) {
	target := newRuntimeAsyncDashboardTarget()
	publisher := realtimeapi.NewAsyncDashboardPublisher(target, realtimeapi.AsyncDashboardPublisherOptions{QueueCapacity: 1, WorkerCount: 1, PublishTimeout: 5 * time.Second, ShutdownTimeout: time.Second})
	defer publisher.Close()

	accepted, err := publisher.PublishDashboardSnapshot(context.Background(), 7)
	if err != nil || !accepted {
		t.Fatalf("expected first dashboard snapshot to queue, accepted=%v err=%v", accepted, err)
	}
	target.waitUntilFirstStarted(t, 2*time.Second)
	accepted, err = publisher.PublishDashboardSnapshot(context.Background(), 7)
	if err != nil || !accepted {
		t.Fatalf("expected second profile-scoped snapshot to coalesce, accepted=%v err=%v", accepted, err)
	}
	snapshot := publisher.Snapshot()
	if snapshot.TrackedProfiles != 1 || snapshot.InflightProfiles != 1 || snapshot.CoalescedCount != 1 {
		t.Fatalf("expected profile-scoped coalescing without request-log identity, got %+v", snapshot)
	}
	target.releaseBlockedPublish()
	first := target.waitForSnapshot(t, 2*time.Second)
	second := target.waitForSnapshot(t, 2*time.Second)
	if first != profiledomain.DefaultProfileID || second != profiledomain.DefaultProfileID {
		t.Fatalf("expected both snapshot publishes to normalize to default profile %d, got %d and %d", profiledomain.DefaultProfileID, first, second)
	}
}

func TestTelemetryOutboxPublishesSplitDashboardMessages(t *testing.T) {
	publisher := newRuntimeSplitDashboardPublisher()
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{RuntimeOptions: runtimeapi.Options{DashboardUpdates: publisher}})
	profileID := harness.activeProfileID(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "split-dashboard-public-" + randomSuffix(), TargetModelID: "split-dashboard-target-" + randomSuffix(), EndpointBaseURL: harness.upstream.baseURL("/split-dashboard"), EndpointAPIKey: "split-dashboard-key"})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"messages": []map[string]any{{"role": "user", "content": "split dashboard realtime messages"}}, "model": route.PublicModelID}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	publisher.waitForActivity(t, profileID, 5*time.Second)
	publisher.assertNoSnapshot(t, 100*time.Millisecond)
	if publisher.activityCount() != 1 || publisher.snapshotCount() != 0 {
		t.Fatalf("expected runtime telemetry to publish one dashboard activity and no snapshot, got activity=%d snapshot=%d", publisher.activityCount(), publisher.snapshotCount())
	}
}

type runtimeAsyncDashboardTarget struct {
	mu           sync.Mutex
	snapshotCh   chan int
	firstStarted chan struct{}
	releaseFirst chan struct{}
	calls        int
}

func newRuntimeAsyncDashboardTarget() *runtimeAsyncDashboardTarget {
	return &runtimeAsyncDashboardTarget{snapshotCh: make(chan int, 4), firstStarted: make(chan struct{}), releaseFirst: make(chan struct{})}
}

func (t *runtimeAsyncDashboardTarget) PublishLatestDashboardSnapshot(ctx context.Context, profileID int) (bool, error) {
	t.mu.Lock()
	t.calls++
	callIndex := t.calls
	t.mu.Unlock()
	if callIndex == 1 {
		close(t.firstStarted)
		select {
		case <-t.releaseFirst:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	t.snapshotCh <- profileID
	return true, nil
}

func (t *runtimeAsyncDashboardTarget) PublishDashboardActivity(context.Context, int, int) (bool, error) {
	return false, nil
}

func (t *runtimeAsyncDashboardTarget) InvalidateDashboardSnapshot(int) {}

func (t *runtimeAsyncDashboardTarget) HasDashboardSubscribers(int) bool { return true }

func (t *runtimeAsyncDashboardTarget) waitUntilFirstStarted(testingT *testing.T, timeout time.Duration) {
	testingT.Helper()
	select {
	case <-t.firstStarted:
	case <-time.After(timeout):
		testingT.Fatal("timed out waiting for first dashboard snapshot publish")
	}
}

func (t *runtimeAsyncDashboardTarget) releaseBlockedPublish() { close(t.releaseFirst) }

func (t *runtimeAsyncDashboardTarget) waitForSnapshot(testingT *testing.T, timeout time.Duration) int {
	testingT.Helper()
	select {
	case profileID := <-t.snapshotCh:
		return profileID
	case <-time.After(timeout):
		testingT.Fatal("timed out waiting for dashboard snapshot publish")
		return 0
	}
}

type runtimeSplitDashboardPublisher struct {
	mu         sync.Mutex
	snapshots  []int
	activity   []int
	snapshotCh chan int
	activityCh chan int
}

func newRuntimeSplitDashboardPublisher() *runtimeSplitDashboardPublisher {
	return &runtimeSplitDashboardPublisher{snapshotCh: make(chan int, 4), activityCh: make(chan int, 4)}
}

func (p *runtimeSplitDashboardPublisher) PublishDashboardSnapshot(_ context.Context, profileID int) (bool, error) {
	p.mu.Lock()
	p.snapshots = append(p.snapshots, profileID)
	p.mu.Unlock()
	p.snapshotCh <- profileID
	return true, nil
}

func (p *runtimeSplitDashboardPublisher) PublishDashboardActivity(_ context.Context, requestLogID int, profileID int) (bool, error) {
	p.mu.Lock()
	p.activity = append(p.activity, requestLogID)
	p.mu.Unlock()
	p.activityCh <- profileID
	return true, nil
}

func (p *runtimeSplitDashboardPublisher) assertNoSnapshot(testingT *testing.T, timeout time.Duration) {
	testingT.Helper()
	select {
	case got := <-p.snapshotCh:
		testingT.Fatalf("expected no dashboard snapshot publish, got profile %d", got)
	case <-time.After(timeout):
	}
}

func (p *runtimeSplitDashboardPublisher) waitForActivity(testingT *testing.T, profileID int, timeout time.Duration) {
	testingT.Helper()
	select {
	case got := <-p.activityCh:
		if got != profileID {
			testingT.Fatalf("expected dashboard activity profile %d, got %d", profileID, got)
		}
	case <-time.After(timeout):
		testingT.Fatal("timed out waiting for dashboard activity publish")
	}
}

func (p *runtimeSplitDashboardPublisher) snapshotCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.snapshots)
}

func (p *runtimeSplitDashboardPublisher) activityCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.activity)
}

func newRealtimeHarness(t *testing.T) *realtimeHarness {
	t.Helper()
	return newRealtimeHarnessWithConfig(t, realtimeHarnessConfig{})
}

type realtimeHarnessConfig struct {
	UseAsyncDashboardPublisher   bool
	ManagementDatabasePoolBudget config.DatabasePoolBudget
	ManagementAdmissionBudget    config.ManagementAdmissionBudget
}

func newRealtimeHarnessWithConfig(t *testing.T, harnessConfig realtimeHarnessConfig) *realtimeHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	databaseName := "s16_runtime_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	startupService, err := startup.New(startup.Options{DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "s16-runtime-secret"})
	if err != nil {
		t.Fatalf("build S16 startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run S16 startup service: %v", err)
	}

	fixedNow := time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC)
	upstream := newUpstreamRecorder(t)
	settings := config.Settings{Host: "127.0.0.1", Port: 8000, AppEnv: config.EnvironmentProduction, DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "s16-runtime-secret", CORSAllowedOrigins: "http://localhost:5173,http://127.0.0.1:5173", AuthJWTSecret: "s16-runtime-jwt-secret", AuthAccessTokenTTLSeconds: 900, AuthRefreshTokenTTLSeconds: 604800, AuthCookieName: "prism_access_token", AuthRefreshCookieName: "prism_refresh_token", AuthCookieSecure: false}
	if harnessConfig.ManagementDatabasePoolBudget.MaxConns > 0 {
		settings.ManagementDatabasePoolBudget = harnessConfig.ManagementDatabasePoolBudget
	}
	if harnessConfig.ManagementAdmissionBudget.M2MaxConcurrent > 0 || harnessConfig.ManagementAdmissionBudget.M3MaxConcurrent > 0 {
		settings.ManagementAdmissionControlBudget = harnessConfig.ManagementAdmissionBudget
	}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create S16 pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	telemetryPool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create S16 runtime telemetry pgx pool: %v", err)
	}
	t.Cleanup(telemetryPool.Close)
	feedbackPool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create S16 runtime feedback pgx pool: %v", err)
	}
	t.Cleanup(feedbackPool.Close)
	runtimeCache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
	if err := runtimeCache.Bootstrap(testContext); err != nil {
		t.Fatalf("bootstrap S16 published runtime snapshot: %v", err)
	}
	runtimeState := loadbalancedomain.NewLocalRuntimeStateStore()
	dashboardSnapshots := statsdomain.NewDashboardAggregateStore()
	runtimeAuthCache := managementauth.NewRuntimeCacheFromShared(runtimeCache)
	authService, err := managementauth.NewService(settings, managementauth.Options{Pool: pool, RuntimeCache: runtimeAuthCache})
	if err != nil {
		t.Fatalf("build S16 auth service: %v", err)
	}
	t.Cleanup(authService.Close)
	modelsService, err := managementmodels.NewService(settings, managementmodels.Options{Pool: pool, Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatalf("build S16 models service: %v", err)
	}
	t.Cleanup(modelsService.Close)
	statsService, err := managementstats.NewService(settings, managementstats.Options{Pool: pool, Now: func() time.Time { return fixedNow }, DashboardSnapshots: dashboardSnapshots})
	if err != nil {
		t.Fatalf("build S16 stats service: %v", err)
	}
	t.Cleanup(statsService.Close)
	realtimeService, err := realtimeapi.NewService(settings, realtimeapi.Options{RealtimePool: pool, AuthService: authService, Now: func() time.Time { return fixedNow }, DashboardSnapshots: dashboardSnapshots})
	if err != nil {
		t.Fatalf("build S16 realtime service: %v", err)
	}
	t.Cleanup(realtimeService.Close)
	dashboardUpdates := runtimeapi.DashboardPublisher(realtimeService)
	var asyncDashboardPublisher *realtimeapi.AsyncDashboardPublisher
	if harnessConfig.UseAsyncDashboardPublisher {
		asyncDashboardPublisher = realtimeapi.NewAsyncDashboardPublisher(realtimeService, realtimeapi.AsyncDashboardPublisherOptions{PublishTimeout: 5 * time.Second, ShutdownTimeout: time.Second})
		realtimeService.SetAsyncDashboardPublisher(asyncDashboardPublisher)
		t.Cleanup(asyncDashboardPublisher.Close)
		dashboardUpdates = asyncDashboardPublisher
	}
	runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{ExecutionPool: pool, TelemetryPool: telemetryPool, FeedbackPool: feedbackPool, Now: func() time.Time { return fixedNow }, DashboardUpdates: dashboardUpdates, Cache: runtimeCache, RuntimeState: runtimeState})
	if err != nil {
		t.Fatalf("build S16 runtime service: %v", err)
	}
	t.Cleanup(runtimeService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "s16-runtime-test", AuthService: authService, ModelsService: modelsService, RealtimeService: realtimeService, RuntimeService: runtimeService, StatsService: statsService})
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
	baseHarness := &runtimeHarness{databaseName: databaseName, client: client, conn: conn, authService: authService, runtimeService: runtimeService, runtimeCache: runtimeCache, server: server, url: server.URL, upstream: upstream}
	return &realtimeHarness{runtimeHarness: baseHarness, realtimeService: realtimeService, statsService: statsService, asyncDashboardPublisher: asyncDashboardPublisher, fixedNow: fixedNow}
}

func (h *realtimeHarness) loadDashboardSnapshot(t *testing.T, profileID int) statsdomain.DashboardSnapshot {
	t.Helper()
	response := h.requestJSON(t, http.MethodGet, "/api/stats/dashboard", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var snapshot statsdomain.DashboardSnapshot
	decodeJSONResponse(t, response, &snapshot)
	return snapshot
}

func (h *realtimeHarness) waitForDashboardTotalRequests(t *testing.T, profileID int, want int, timeout time.Duration) statsdomain.DashboardSnapshot {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := h.loadDashboardSnapshot(t, profileID)
	for time.Now().Before(deadline) {
		last = h.loadDashboardSnapshot(t, profileID)
		if last.MetricSnapshot.TotalRequests == want {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for dashboard total_requests=%d, last snapshot %+v", want, last)
	return statsdomain.DashboardSnapshot{}
}

func waitForRealtimeAsyncDashboardDrain(t *testing.T, publisher *realtimeapi.AsyncDashboardPublisher, timeout time.Duration) realtimeapi.AsyncDashboardPublisherSnapshot {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		snapshot := publisher.Snapshot()
		if snapshot.Drained {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	snapshot := publisher.Snapshot()
	t.Fatalf("timed out waiting for async dashboard publisher to drain, last snapshot %+v", snapshot)
	return snapshot
}

func (h *realtimeHarness) dialWebSocket(t *testing.T, includeCookies bool) *websocket.Conn {
	t.Helper()
	conn, _, err := h.dialWebSocketWithOrigin(t, includeCookies, "")
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	return conn
}

func (h *realtimeHarness) dialWebSocketWithOrigin(t *testing.T, includeCookies bool, origin string) (*websocket.Conn, *http.Response, error) {
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
	if strings.TrimSpace(origin) != "" {
		headers.Set("Origin", origin)
	}
	return websocket.DefaultDialer.Dial(strings.Replace(h.url, "http://", "ws://", 1)+"/api/realtime/ws", headers)
}

func (h *realtimeHarness) seedRealtimeDashboardRoute(t *testing.T, profileID int, suffix string) seededDashboardRoute {
	t.Helper()
	strategyID := h.seedLegacyStrategy(t, profileID, "s16-strategy-"+suffix+"-"+randomSuffix(), "round-robin")
	targetModelID := "native-" + suffix + "-" + randomSuffix()
	targetModelLabel := "Native " + suffix + " route"
	targetModelConfigID := h.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelID := "public-" + suffix + "-" + randomSuffix()
	publicModelLabel := "Proxy " + suffix + " route"
	publicModelConfigID := h.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	if _, err := h.conn.Exec(context.Background(), `UPDATE model_configs SET display_name = $1 WHERE id = $2`, targetModelLabel, targetModelConfigID); err != nil {
		t.Fatalf("set realtime target model display name: %v", err)
	}
	if _, err := h.conn.Exec(context.Background(), `UPDATE model_configs SET display_name = $1 WHERE id = $2`, publicModelLabel, publicModelConfigID); err != nil {
		t.Fatalf("set realtime public model display name: %v", err)
	}
	h.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointName := "Endpoint " + suffix + " " + randomSuffix()
	endpointID := h.seedEndpoint(t, profileID, endpointName, h.upstream.baseURL("/"+suffix), "endpoint-key-"+suffix, 0)
	connectionID := h.seedConnection(t, profileID, targetModelConfigID, endpointID, "connection-"+suffix+"-"+randomSuffix(), nil, nil, 0)
	return seededDashboardRoute{PublicModelID: publicModelID, PublicModelLabel: publicModelLabel, TargetModelID: targetModelID, TargetModelLabel: targetModelLabel, EndpointID: endpointID, EndpointName: endpointName, EndpointBaseURL: h.upstream.baseURL("/" + suffix), ConnectionID: connectionID}
}

func (h *realtimeHarness) insertDashboardActivity(t *testing.T, route seededDashboardRoute, profileID int, requestLogID int, usageEventID int, createdAt time.Time) int {
	t.Helper()
	ensureRuntimeTestLogPartitions(t, h.databaseName,
		runtimeTestLogPartitionFor("request_logs", createdAt),
		runtimeTestLogPartitionFor("usage_request_events", createdAt),
	)
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO request_logs (id, profile_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, ingress_request_id, attempt_number, provider_correlation_id, endpoint_base_url, status_code, response_time_ms, is_stream, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, success_flag, billable_flag, priced_flag, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, request_path, endpoint_description, created_at) VALUES ($1, $2, $3, $4, 'openai', $5, $6, $7, 1, $8, $9, 200, 1200, FALSE, 11, 7, 25, 4, 2, 1, TRUE, TRUE, TRUE, 1250, 'USD', '$', '/v1/chat/completions', $10, $11)`,
		requestLogID,
		profileID,
		route.PublicModelID,
		route.TargetModelID,
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
		`INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, resolved_target_model_id, api_family, endpoint_id, endpoint_label_snapshot, connection_id, status_code, success_flag, billable_flag, priced_flag, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, attempt_count, request_path, created_at, response_time_ms) VALUES ($1, $2, $3, $4, $5, 'openai', $6, $7, $8, 200, TRUE, TRUE, TRUE, 11, 7, 25, 4, 2, 1, 1250, 'USD', '$', 1, '/v1/chat/completions', $9, 1200)`,
		usageEventID,
		profileID,
		fmt.Sprintf("ingress-%d", requestLogID),
		route.PublicModelID,
		route.TargetModelID,
		route.EndpointID,
		route.EndpointName,
		route.ConnectionID,
		createdAt,
	); err != nil {
		t.Fatalf("insert dashboard usage event %d: %v", usageEventID, err)
	}
	return requestLogID
}

func seedRealtimeVerifiedAuthSettings(t *testing.T, harness *realtimeHarness, username string, password string, _ string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash realtime auth password: %v", err)
	}
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE app_auth_settings SET auth_enabled = TRUE, username = $1, password_hash = $2, token_version = 0, updated_at = $3 WHERE singleton_key = 'app'`,
		username,
		string(hash),
		harness.fixedNow,
	); err != nil {
		t.Fatalf("seed realtime auth settings: %v", err)
	}
	harness.authService.InvalidateAppAuthSettingsSnapshot()
}

type seededDashboardRoute struct {
	PublicModelID    string
	PublicModelLabel string
	TargetModelID    string
	TargetModelLabel string
	EndpointID       int
	EndpointName     string
	EndpointBaseURL  string
	ConnectionID     int
}

func realtimeDashboardRoutingLinkForEndpoint(t *testing.T, routing statsdomain.DashboardRoutingHealthMap, endpointID int) statsdomain.DashboardRoutingLink {
	t.Helper()
	for _, link := range routing.Links {
		if link.EndpointID == endpointID {
			return link
		}
	}
	t.Fatalf("expected dashboard routing link for endpoint %d, got %+v", endpointID, routing.Links)
	return statsdomain.DashboardRoutingLink{}
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

func assertDashboardActivityProfileID(t *testing.T, message map[string]any, profileID int) {
	t.Helper()
	if message["type"] != "dashboard.activity" || message["profile_id"] != float64(profileID) {
		t.Fatalf("expected dashboard.activity for profile %d, got %+v", profileID, message)
	}
	if _, ok := message["activity"].(map[string]any); !ok {
		t.Fatalf("expected dashboard activity object, got %+v", message)
	}
}

func legacyDashboardMessageType() string {
	return "dashboard" + "." + "update"
}

func legacyDashboardActivityRowsKey() string {
	return "recent" + "_" + "requests"
}

func assertNoMixedDashboardUpdateEnvelope(t *testing.T, message map[string]any) {
	t.Helper()
	if message["type"] == legacyDashboardMessageType() {
		t.Fatalf("did not expect mixed dashboard envelope, got %+v", message)
	}
	if _, ok := message["request_log"]; ok {
		t.Fatalf("did not expect mixed request_log envelope, got %+v", message)
	}
}

func assertDashboardSnapshotEnvelopeShape(t *testing.T, message realtimeapi.DashboardSnapshotMessage) {
	t.Helper()
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal dashboard snapshot message: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode dashboard snapshot message: %v", err)
	}
	assertMapKeys(t, payload, []string{"profile_id", "snapshot", "type"})
	snapshotPayload := payload["snapshot"].(map[string]any)
	for _, forbidden := range []string{legacyDashboardActivityRowsKey(), "request_log_id", "request_cursor", "activity_watermark"} {
		if _, ok := snapshotPayload[forbidden]; ok {
			t.Fatalf("did not expect dashboard snapshot to expose request-history field %q, got %+v", forbidden, snapshotPayload)
		}
	}
	if snapshotPayload["snapshot_revision"] == "" || snapshotPayload["source_watermark"] == nil {
		t.Fatalf("expected dashboard snapshot revision and source watermark, got %+v", snapshotPayload)
	}
}

func assertDashboardActivityEnvelopeShape(t *testing.T, message realtimeapi.DashboardActivityMessage) {
	t.Helper()
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal dashboard activity message: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode dashboard activity message: %v", err)
	}
	assertMapKeys(t, payload, []string{"activity", "activity_watermark", "profile_id", "type"})
	if _, ok := payload["snapshot"]; ok {
		t.Fatalf("did not expect dashboard activity to expose snapshot, got %+v", payload)
	}
	if _, ok := payload["activity"].(map[string]any); !ok {
		t.Fatalf("expected dashboard activity to serialize as object, got %+v", payload)
	}
}

func assertMapKeys(t *testing.T, payload map[string]any, expected []string) {
	t.Helper()
	if len(payload) != len(expected) {
		t.Fatalf("expected keys %v, got %+v", expected, payload)
	}
	for _, key := range expected {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected key %q, got %+v", key, payload)
		}
	}
}

func readDashboardMessageByType(t *testing.T, conn *websocket.Conn, messageType string) map[string]any {
	t.Helper()
	for range 3 {
		message := readWebSocketJSON(t, conn)
		if message["type"] == messageType {
			return message
		}
	}
	t.Fatalf("timed out waiting for dashboard message type %q", messageType)
	return nil
}

func decodeRealtimeDashboardSnapshot(t *testing.T, message map[string]any) statsdomain.DashboardSnapshot {
	t.Helper()
	raw, err := json.Marshal(message["snapshot"])
	if err != nil {
		t.Fatalf("marshal realtime dashboard snapshot: %v", err)
	}
	var snapshot statsdomain.DashboardSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("decode realtime dashboard snapshot: %v", err)
	}
	return snapshot
}

func dashboardTopologyNodeForModelID(graph statsdomain.DashboardTopologyGraph, modelID string) *statsdomain.DashboardTopologyNode {
	for index := range graph.Nodes {
		if graph.Nodes[index].ModelID != nil && *graph.Nodes[index].ModelID == modelID {
			return &graph.Nodes[index]
		}
	}
	return nil
}

func assertUsageSnapshotMergedTokenSemantics(t *testing.T, snapshot statsdomain.UsageSnapshotResponse, wantInput int, wantOutput int, wantTotal int, wantCached int, wantReasoning int) {
	t.Helper()
	if snapshot.Overview.InputTokens != wantInput || snapshot.Overview.OutputTokens != wantOutput || snapshot.Overview.TotalTokens != wantTotal || snapshot.Overview.CachedTokens != wantCached || snapshot.Overview.ReasoningTokens != wantReasoning {
		t.Fatalf("expected usage snapshot overview input/output/total/cached/reasoning=%d/%d/%d/%d/%d, got %+v", wantInput, wantOutput, wantTotal, wantCached, wantReasoning, snapshot.Overview)
	}
	trendTotals := sumAllModelTokenTrendPoints(snapshot.TokenUsageTrends.Hourly)
	if trendTotals.inputTokens != wantInput || trendTotals.outputTokens != wantOutput || trendTotals.totalTokens != wantTotal || trendTotals.cachedTokens != wantCached || trendTotals.reasoningTokens != wantReasoning {
		t.Fatalf("expected usage snapshot token trend input/output/total/cached/reasoning=%d/%d/%d/%d/%d, got %+v", wantInput, wantOutput, wantTotal, wantCached, wantReasoning, trendTotals)
	}
	breakdownTotals := sumTokenTypeBreakdownPoints(snapshot.TokenTypeBreakdown.Hourly)
	if breakdownTotals.inputTokens != wantInput || breakdownTotals.outputTokens != wantOutput || breakdownTotals.cachedTokens != wantCached || breakdownTotals.reasoningTokens != wantReasoning {
		t.Fatalf("expected usage snapshot token breakdown input/output/cached/reasoning=%d/%d/%d/%d, got %+v", wantInput, wantOutput, wantCached, wantReasoning, breakdownTotals)
	}
	modelTotals := sumUsageModelStatisticTokens(snapshot.ModelStatistics)
	if modelTotals.inputTokens != wantInput || modelTotals.outputTokens != wantOutput || modelTotals.totalTokens != wantTotal || modelTotals.cachedTokens != wantCached || modelTotals.reasoningTokens != wantReasoning {
		t.Fatalf("expected usage snapshot model stats input/output/total/cached/reasoning=%d/%d/%d/%d/%d, got %+v", wantInput, wantOutput, wantTotal, wantCached, wantReasoning, modelTotals)
	}
}

type usageSnapshotTokenTotals struct {
	inputTokens     int
	outputTokens    int
	totalTokens     int
	cachedTokens    int
	reasoningTokens int
}

func sumAllModelTokenTrendPoints(series []statsdomain.UsageTokenTrendSeries) usageSnapshotTokenTotals {
	for _, item := range series {
		if item.Key != "all" {
			continue
		}
		totals := usageSnapshotTokenTotals{}
		for _, point := range item.Points {
			totals.inputTokens += point.InputTokens
			totals.outputTokens += point.OutputTokens
			totals.totalTokens += point.TotalTokens
			totals.cachedTokens += point.CachedTokens
			totals.reasoningTokens += point.ReasoningTokens
		}
		return totals
	}
	return usageSnapshotTokenTotals{}
}

func sumTokenTypeBreakdownPoints(points []statsdomain.UsageTokenTypeBreakdownPoint) usageSnapshotTokenTotals {
	totals := usageSnapshotTokenTotals{}
	for _, point := range points {
		totals.inputTokens += point.InputTokens
		totals.outputTokens += point.OutputTokens
		totals.cachedTokens += point.CachedTokens
		totals.reasoningTokens += point.ReasoningTokens
	}
	return totals
}

func sumUsageModelStatisticTokens(items []statsdomain.UsageModelStatistic) usageSnapshotTokenTotals {
	totals := usageSnapshotTokenTotals{}
	for _, item := range items {
		if item.InputTokens != nil {
			totals.inputTokens += *item.InputTokens
		}
		if item.OutputTokens != nil {
			totals.outputTokens += *item.OutputTokens
		}
		if item.CachedTokens != nil {
			totals.cachedTokens += *item.CachedTokens
		}
		if item.ReasoningTokens != nil {
			totals.reasoningTokens += *item.ReasoningTokens
		}
		totals.totalTokens += item.TotalTokens
	}
	return totals
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

func assertAnalyticsSnapshot(t *testing.T, message map[string]any, profileID int, preset string, sequence float64) {
	t.Helper()
	if message["type"] != "analytics.snapshot" || message["channel"] != "analytics" || message["profile_id"] != float64(profileID) || message["preset"] != preset || message["sequence"] != sequence {
		t.Fatalf("unexpected analytics snapshot envelope: %+v", message)
	}
	if _, ok := message["snapshot"].(map[string]any); !ok {
		t.Fatalf("expected analytics snapshot payload, got %+v", message)
	}
	if _, ok := message["endpoint_model_statistics_by_endpoint_id"].(map[string]any); !ok {
		t.Fatalf("expected analytics endpoint model statistics payload, got %+v", message)
	}
}

func assertAnalyticsErrorCode(t *testing.T, message map[string]any, code string) {
	t.Helper()
	if message["type"] != "analytics.error" || message["channel"] != "analytics" || message["code"] != code {
		t.Fatalf("expected analytics error code %q, got %+v", code, message)
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

func TestRealtimeAnalyticsPublishActiveScopesOnly(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := harness.activeProfileID(t)
	profileTwoID := harness.createProfile(t, "Analytics Default Scope Secondary")
	route := harness.seedRealtimeDashboardRoute(t, profileID, "analytics-active")
	harness.insertDashboardActivity(t, route, profileID, 8450, 9450, harness.fixedNow.Add(-10*time.Minute))
	routeTwo := harness.seedRealtimeDashboardRoute(t, profileTwoID, "analytics-secondary-profile")
	harness.insertDashboardActivity(t, routeTwo, profileTwoID, 8451, 9451, harness.fixedNow.Add(-10*time.Minute))

	oneHourConn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, oneHourConn, "authenticated")
	assertRealtimeMessage(t, oneHourConn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, oneHourConn, map[string]any{"type": "subscribe", "profile_id": profileID, "channel": "analytics", "preset": "1h"})
	assertRealtimeMessage(t, oneHourConn, map[string]any{"type": "subscribed", "profile_id": float64(profileID), "channel": "analytics", "preset": "1h"})
	assertAnalyticsSnapshot(t, readWebSocketJSON(t, oneHourConn), profileID, "1h", float64(1))

	thirtyDayConn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, thirtyDayConn, "authenticated")
	assertRealtimeMessage(t, thirtyDayConn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, thirtyDayConn, map[string]any{"type": "subscribe", "profile_id": profileID, "channel": "analytics", "preset": "30d"})
	assertRealtimeMessage(t, thirtyDayConn, map[string]any{"type": "subscribed", "profile_id": float64(profileID), "channel": "analytics", "preset": "30d"})
	assertAnalyticsSnapshot(t, readWebSocketJSON(t, thirtyDayConn), profileID, "30d", float64(1))

	delivered, err := harness.realtimeService.PublishAnalyticsUpdates(context.Background(), profileTwoID)
	if err != nil {
		t.Fatalf("publish analytics secondary profile: %v", err)
	}
	if !delivered {
		t.Fatal("expected secondary profile analytics publish to deliver to Default subscribers")
	}
	assertAnalyticsSnapshot(t, readWebSocketJSON(t, oneHourConn), profileID, "1h", float64(2))
	assertAnalyticsSnapshot(t, readWebSocketJSON(t, thirtyDayConn), profileID, "30d", float64(2))
	delivered, err = harness.realtimeService.PublishAnalyticsUpdates(context.Background(), profileID)
	if err != nil {
		t.Fatalf("publish active analytics scopes: %v", err)
	}
	if !delivered {
		t.Fatal("expected active analytics scopes to report delivery")
	}
	assertAnalyticsSnapshot(t, readWebSocketJSON(t, oneHourConn), profileID, "1h", float64(2))
	assertAnalyticsSnapshot(t, readWebSocketJSON(t, thirtyDayConn), profileID, "30d", float64(2))

	writeWebSocketJSON(t, oneHourConn, map[string]any{"type": "unsubscribe_channel", "channel": "analytics", "preset": "1h"})
	assertRealtimeMessage(t, oneHourConn, map[string]any{"type": "unsubscribed", "channel": "analytics", "preset": "1h"})
	delivered, err = harness.realtimeService.PublishAnalyticsUpdates(context.Background(), profileID)
	if err != nil {
		t.Fatalf("publish after one active analytics unsubscribe: %v", err)
	}
	if !delivered {
		t.Fatal("expected remaining active analytics scope to report delivery")
	}
	assertNoRealtimeMessage(t, oneHourConn)
	assertAnalyticsSnapshot(t, readWebSocketJSON(t, thirtyDayConn), profileID, "30d", float64(3))

	_ = oneHourConn.Close()
	_ = thirtyDayConn.Close()
}

func TestRealtimeAnalyticsScopedSubscriptions(t *testing.T) {
	manager := realtimeapi.NewConnectionManager(500 * time.Millisecond)
	t.Cleanup(manager.Close)
	profileID := 42
	oneHourConn, oneHourID := newManagedRealtimeConnection(t, manager)
	thirtyDayConn, thirtyDayID := newManagedRealtimeConnection(t, manager)

	if !manager.Subscribe(oneHourID, profileID, "analytics", "1h") {
		t.Fatal("subscribe analytics 1h")
	}
	if !manager.Subscribe(thirtyDayID, profileID, "analytics", "30d") {
		t.Fatal("subscribe analytics 30d")
	}
	assertStringSliceEqual(t, manager.ActiveScopes(profileID, "analytics"), []string{"1h", "30d"})

	if delivered := manager.BroadcastToProfile(profileID, "analytics", map[string]any{"type": "analytics.snapshot", "preset": "1h"}, "1h"); delivered != 1 {
		t.Fatalf("1h delivered count = %d, want 1", delivered)
	}
	assertRealtimeMessage(t, oneHourConn, map[string]any{"type": "analytics.snapshot", "preset": "1h"})

	if delivered := manager.BroadcastToProfile(profileID, "analytics", map[string]any{"type": "analytics.snapshot", "preset": "30d"}, "30d"); delivered != 1 {
		t.Fatalf("30d delivered count = %d, want 1", delivered)
	}
	assertRealtimeMessage(t, thirtyDayConn, map[string]any{"type": "analytics.snapshot", "preset": "30d"})

	if !manager.UnsubscribeChannel(oneHourID, "analytics", "1h") {
		t.Fatal("unsubscribe analytics 1h")
	}
	assertStringSliceEqual(t, manager.ActiveScopes(profileID, "analytics"), []string{"30d"})
	if delivered := manager.BroadcastToProfile(profileID, "analytics", map[string]any{"type": "analytics.snapshot", "preset": "1h"}, "1h"); delivered != 0 {
		t.Fatalf("1h delivered count after unsubscribe = %d, want 0", delivered)
	}
	assertNoRealtimeMessage(t, oneHourConn)
	if delivered := manager.BroadcastToProfile(profileID, "analytics", map[string]any{"type": "analytics.snapshot", "preset": "30d"}, "30d"); delivered != 1 {
		t.Fatalf("30d delivered count after 1h unsubscribe = %d, want 1", delivered)
	}
	assertRealtimeMessage(t, thirtyDayConn, map[string]any{"type": "analytics.snapshot", "preset": "30d"})

	profileTwoID := 84
	if !manager.Subscribe(thirtyDayID, profileTwoID, "analytics", "7d") {
		t.Fatal("subscribe analytics profile replacement")
	}
	assertStringSliceEqual(t, manager.ActiveScopes(profileID, "analytics"), nil)
	assertStringSliceEqual(t, manager.ActiveScopes(profileTwoID, "analytics"), []string{"7d"})

	manager.Disconnect(thirtyDayID)
	assertStringSliceEqual(t, manager.ActiveScopes(profileTwoID, "analytics"), nil)
	_ = oneHourConn.Close()
	_ = thirtyDayConn.Close()
}

func newManagedRealtimeConnection(t *testing.T, manager *realtimeapi.ConnectionManager) (*websocket.Conn, string) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	connectionIDCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade realtime manager websocket: %v", err)
			return
		}
		connectionID := manager.Connect(socket)
		connectionIDCh <- connectionID
		for {
			if _, _, err := socket.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1), nil)
	if err != nil {
		t.Fatalf("dial realtime manager websocket: %v", err)
	}
	select {
	case connectionID := <-connectionIDCh:
		if connectionID == "" {
			t.Fatal("expected realtime connection ID")
		}
		return conn, connectionID
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for realtime manager connection")
		return nil, ""
	}
}

func assertNoRealtimeMessage(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	var message map[string]any
	err := conn.ReadJSON(&message)
	_ = conn.SetReadDeadline(time.Time{})
	if err == nil {
		t.Fatalf("expected no realtime message, got %+v", message)
	}
	var netErr net.Error
	if websocket.IsUnexpectedCloseError(err) && !(errors.As(err, &netErr) && netErr.Timeout()) {
		t.Fatalf("unexpected websocket close while checking no message: %v", err)
	}
}

func assertStringSliceEqual(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("string slice = %+v, want %+v", got, want)
	}
}

func usageModelStatisticsFromEndpointTest(items []statsdomain.EndpointModelStatistic) []statsdomain.UsageModelStatistic {
	models := make([]statsdomain.UsageModelStatistic, 0, len(items))
	for _, item := range items {
		models = append(models, statsdomain.UsageModelStatistic{
			ModelID:              item.ModelID,
			ModelLabel:           item.ModelLabel,
			RequestCount:         item.RequestCount,
			SuccessCount:         item.SuccessCount,
			FailedCount:          item.FailedCount,
			PricedRequestCount:   item.PricedRequestCount,
			UnpricedRequestCount: item.UnpricedRequestCount,
			SuccessRate:          item.SuccessRate,
			P50TTFTMS:            item.P50TTFTMS,
			P95TTFTMS:            item.P95TTFTMS,
			TotalTokens:          item.TotalTokens,
			TotalCostMicros:      item.TotalCostMicros,
			AvgOutputRateTPS:     item.AvgOutputRateTPS,
		})
	}
	return models
}
