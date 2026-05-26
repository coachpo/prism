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
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
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
	if message.RequestLog.ID != requestLogID || message.RequestLog.ProfileID != profileID || len(message.Snapshot.RoutingHealthMap.Links) == 0 {
		t.Fatalf("expected built payload to include request log and nested routing snapshot, got %+v", message)
	}
	if message.RequestLog.ModelLabel != route.PublicModelLabel {
		t.Fatalf("expected realtime request_log.model_label=%q, got %+v", route.PublicModelLabel, message.RequestLog)
	}
	if message.RequestLog.ResolvedTargetModelLabel == nil || *message.RequestLog.ResolvedTargetModelLabel != route.TargetModelLabel {
		t.Fatalf("expected realtime request_log.resolved_target_model_label=%q, got %+v", route.TargetModelLabel, message.RequestLog)
	}
	if !message.RequestLog.IsProxyOrigin {
		t.Fatalf("expected realtime request_log.is_proxy_origin=true, got %+v", message.RequestLog)
	}
	assertRealtimeRequestLogTokenPointers(t, message.RequestLog, 11, 7, 25, 4, 2, 1)
	var requestLogPayload map[string]any
	rawRequestLogPayload, err := json.Marshal(message.RequestLog)
	if err != nil {
		t.Fatalf("marshal realtime request_log payload: %v", err)
	}
	if err := json.Unmarshal(rawRequestLogPayload, &requestLogPayload); err != nil {
		t.Fatalf("decode realtime request_log payload: %v", err)
	}
	if _, ok := requestLogPayload["stream_error_detail"]; ok {
		t.Fatalf("did not expect realtime request_log to expose stream_error_detail, got %+v", requestLogPayload)
	}
	detailResponse := harness.requestJSON(t, http.MethodGet, fmt.Sprintf("/api/stats/requests/%d", requestLogID), nil, runtimeModelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	assertRealtimeRequestLogMatchesRESTDetail(t, requestLogPayload, detailPayload)

	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET priced_flag = TRUE, unpriced_reason = NULL, total_cost_user_currency_micros = NULL WHERE id = $1 AND profile_id = $2`, requestLogID, profileID); err != nil {
		t.Fatalf("mark realtime request log with missing cost: %v", err)
	}
	message, err = harness.realtimeService.BuildDashboardUpdate(context.Background(), requestLogID, profileID)
	if err != nil {
		t.Fatalf("build dashboard update payload with normalized missing cost: %v", err)
	}
	rawRequestLogPayload, err = json.Marshal(message.RequestLog)
	if err != nil {
		t.Fatalf("marshal realtime missing-cost request_log payload: %v", err)
	}
	if err := json.Unmarshal(rawRequestLogPayload, &requestLogPayload); err != nil {
		t.Fatalf("decode realtime missing-cost request_log payload: %v", err)
	}
	detailResponse = harness.requestJSON(t, http.MethodGet, fmt.Sprintf("/api/stats/requests/%d", requestLogID), nil, runtimeModelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	decodeJSONResponse(t, detailResponse, &detailPayload)
	assertRealtimeRequestLogMatchesRESTDetail(t, requestLogPayload, detailPayload)
	if requestLogPayload["priced_flag"] != false || requestLogPayload["unpriced_reason"] != "MISSING_PRICE_DATA" {
		t.Fatalf("expected realtime missing-cost request_log to use REST spend normalization, got %+v", requestLogPayload)
	}

	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET is_stream = FALSE, stream_outcome = 'upstream_read_error', stream_error_kind = 'upstream_read_failed', stream_error_detail = 'upstream socket closed' WHERE id = $1 AND profile_id = $2`, requestLogID, profileID); err != nil {
		t.Fatalf("mark realtime request log with actual stream outcome: %v", err)
	}
	message, err = harness.realtimeService.BuildDashboardUpdate(context.Background(), requestLogID, profileID)
	if err != nil {
		t.Fatalf("build dashboard update payload with actual stream outcome: %v", err)
	}
	if message.RequestLog.IsStream || message.RequestLog.StreamOutcome != "upstream_read_error" || message.RequestLog.StreamErrorKind == nil || *message.RequestLog.StreamErrorKind != "upstream_read_failed" {
		t.Fatalf("expected realtime request_log to preserve non-not_streaming stream outcome even when is_stream is false, got %+v", message.RequestLog)
	}
	rawRequestLogPayload, err = json.Marshal(message.RequestLog)
	if err != nil {
		t.Fatalf("marshal realtime actual-stream request_log payload: %v", err)
	}
	if err := json.Unmarshal(rawRequestLogPayload, &requestLogPayload); err != nil {
		t.Fatalf("decode realtime actual-stream request_log payload: %v", err)
	}
	if _, ok := requestLogPayload["stream_error_detail"]; ok {
		t.Fatalf("did not expect realtime actual-stream request_log to expose stream_error_detail, got %+v", requestLogPayload)
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
	availabilityPercentage := 95.24
	cellAvailabilityPercentage := 100.0
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
			ServiceHealth:         statsdomain.UsageServiceHealth{AvailabilityPercentage: &availabilityPercentage, RequestCount: 42, SuccessCount: 40, FailedCount: 2, IntervalMinutes: 5, Cells: []statsdomain.UsageServiceHealthCell{{BucketStart: generatedAt.Add(-5 * time.Minute), RequestCount: 4, SuccessCount: 4, FailedCount: 0, AvailabilityPercentage: &cellAvailabilityPercentage, Status: "ok"}}},
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
	profileID := harness.activeProfileID(t)
	route := harness.seedRealtimeDashboardRoute(t, profileID, "analytics-initial")
	harness.insertDashboardActivity(t, route, profileID, 8401, 9401, harness.fixedNow.Add(-10*time.Minute))

	conn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, conn, "authenticated")
	assertRealtimeMessage(t, conn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, conn, map[string]any{"type": "subscribe", "profile_id": profileID, "channel": "analytics", "preset": "1h"})
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
	writeWebSocketJSON(t, conn, map[string]any{"type": "refresh", "profile_id": profileID, "channel": "analytics", "preset": "1h"})
	refreshedSnapshot := readWebSocketJSON(t, conn)
	assertAnalyticsSnapshot(t, refreshedSnapshot, profileID, "1h", float64(2))
	if snapshotPayload := refreshedSnapshot["snapshot"].(map[string]any); snapshotPayload["overview"].(map[string]any)["total_requests"] != float64(2) {
		t.Fatalf("expected refreshed analytics snapshot to include newly seeded usage, got %+v", snapshotPayload["overview"])
	}
	writeWebSocketJSON(t, conn, map[string]any{"type": "ping"})
	assertRealtimeMessage(t, conn, map[string]any{"type": "pong"})
	writeWebSocketJSON(t, conn, map[string]any{"type": "unsubscribe_channel", "channel": "analytics", "preset": "1h"})
	assertRealtimeMessage(t, conn, map[string]any{"type": "unsubscribed", "channel": "analytics", "preset": "1h"})
	writeWebSocketJSON(t, conn, map[string]any{"type": "refresh", "profile_id": profileID, "channel": "analytics", "preset": "1h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "scope_not_subscribed")
	_ = conn.Close()
}

func TestRealtimeAnalyticsSubscribeValidationErrors(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := harness.seedRealtimeDashboardRoute(t, profileID, "analytics-validation")
	harness.insertDashboardActivity(t, route, profileID, 8402, 9402, harness.fixedNow.Add(-5*time.Minute))

	conn := harness.dialWebSocket(t, false)
	assertRealtimeMessageType(t, conn, "authenticated")
	assertRealtimeMessage(t, conn, map[string]any{"type": "heartbeat"})
	writeWebSocketJSON(t, conn, map[string]any{"type": "subscribe", "profile_id": profileID, "channel": "analytics", "preset": "2h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "invalid_preset")
	writeWebSocketJSON(t, conn, map[string]any{"type": "subscribe", "channel": "analytics", "preset": "1h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "profile_id_required")
	writeWebSocketJSON(t, conn, map[string]any{"type": "subscribe", "profile_id": 999999, "channel": "analytics", "preset": "1h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "profile_not_found")
	writeWebSocketJSON(t, conn, map[string]any{"type": "refresh", "profile_id": profileID, "channel": "analytics", "preset": "1h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "scope_not_subscribed")
	writeWebSocketJSON(t, conn, map[string]any{"type": "unsubscribe_channel", "channel": "analytics"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "invalid_preset")
	writeWebSocketJSON(t, conn, map[string]any{"type": "unsubscribe_channel", "channel": "analytics", "preset": "2h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "invalid_preset")
	writeWebSocketJSON(t, conn, map[string]any{"type": "unsubscribe_channel", "channel": "analytics", "preset": "1h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "scope_not_subscribed")
	writeWebSocketJSON(t, conn, map[string]any{"type": 12, "profile_id": profileID, "channel": "analytics", "preset": "1h"})
	assertAnalyticsErrorCode(t, readWebSocketJSON(t, conn), "malformed_message")

	writeWebSocketJSON(t, conn, map[string]any{"type": "subscribe", "profile_id": profileID, "channel": "analytics", "preset": "1h"})
	assertRealtimeMessage(t, conn, map[string]any{"type": "subscribed", "profile_id": float64(profileID), "channel": "analytics", "preset": "1h"})
	assertAnalyticsSnapshot(t, readWebSocketJSON(t, conn), profileID, "1h", float64(1))
	_ = conn.Close()
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
	if requestLog["model_label"] != route.PublicModelLabel {
		t.Fatalf("expected delivered request_log.model_label=%q, got %+v", route.PublicModelLabel, requestLog)
	}
	if requestLog["resolved_target_model_label"] != route.TargetModelLabel {
		t.Fatalf("expected delivered request_log.resolved_target_model_label=%q, got %+v", route.TargetModelLabel, requestLog)
	}
	if requestLog["is_proxy_origin"] != true {
		t.Fatalf("expected delivered request_log.is_proxy_origin=true, got %+v", requestLog)
	}
	if _, ok := message["stats_summary_24h"]; ok {
		t.Fatalf("did not expect legacy stats_summary_24h field in dashboard.update, got %+v", message)
	}
	snapshot := message["snapshot"].(map[string]any)
	metricSnapshot := snapshot["metric_snapshot"].(map[string]any)
	if metricSnapshot["total_requests"] != float64(1) || metricSnapshot["success_rate"] != float64(100) {
		t.Fatalf("unexpected delivered metric_snapshot payload: %+v", metricSnapshot)
	}
	routingHealthMap := snapshot["routing_health_map"].(map[string]any)
	links := routingHealthMap["links"].([]any)
	if len(links) == 0 {
		t.Fatalf("expected delivered nested routing_health_map links, got %+v", routingHealthMap)
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
	_ = baselineConn.Close()

	updatedPassword := "snapshot-password-456"
	updateResponse := harness.requestJSON(
		t,
		http.MethodPut,
		"/api/settings/auth",
		map[string]any{"auth_enabled": true, "username": "snapshot-admin", "password": updatedPassword},
		nil,
	)
	assertStatus(t, updateResponse, http.StatusOK)

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

func TestDashboardSnapshotConsistencyBetweenRESTAndRealtime(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := harness.seedRealtimeDashboardRoute(t, profileID, "consistency")
	requestLogID := harness.insertDashboardActivity(t, route, profileID, 8301, 9301, harness.fixedNow)
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

	message, err := harness.realtimeService.BuildDashboardUpdate(context.Background(), requestLogID, profileID)
	if err != nil {
		t.Fatalf("build dashboard update after rest snapshot warmup: %v", err)
	}
	if !reflect.DeepEqual(dashboardSnapshot, message.Snapshot) {
		t.Fatalf("expected /api/stats/dashboard to match realtime snapshot, got rest=%+v realtime=%+v", dashboardSnapshot, message.Snapshot)
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
	delivered, err := harness.realtimeService.PublishDashboardUpdate(context.Background(), requestLogID, profileID)
	if err != nil {
		t.Fatalf("publish dashboard update after rest snapshot warmup: %v", err)
	}
	if !delivered {
		t.Fatal("expected warmed dashboard aggregate publish to deliver while subscribed")
	}
	assertNestedRequestLogProfileID(t, readWebSocketJSON(t, conn), profileID)
	_ = conn.Close()
}

func TestDashboardSnapshotInvalidatesAfterModelMutation(t *testing.T) {
	harness := newRealtimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := harness.seedRealtimeDashboardRoute(t, profileID, "invalidation")
	harness.insertDashboardActivity(t, route, profileID, 8351, 9351, harness.fixedNow)

	baselineResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/dashboard", nil, runtimeModelHeader(profileID))
	assertStatus(t, baselineResponse, http.StatusOK)
	var baseline statsdomain.DashboardSnapshot
	decodeJSONResponse(t, baselineResponse, &baseline)
	if len(baseline.RoutingHealthMap.Links) == 0 || baseline.RoutingHealthMap.Links[0].ModelLabel != route.TargetModelLabel {
		t.Fatalf("expected warmed dashboard snapshot to use target model label %q, got %+v", route.TargetModelLabel, baseline.RoutingHealthMap.Links)
	}

	var targetModelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM model_configs WHERE profile_id = $1 AND model_id = $2`, profileID, route.TargetModelID).Scan(&targetModelConfigID); err != nil {
		t.Fatalf("load target model config id: %v", err)
	}
	updatedLabel := "Updated invalidation route"
	updateResponse := harness.requestJSON(t, http.MethodPut, fmt.Sprintf("/api/models/%d", targetModelConfigID), map[string]any{"display_name": updatedLabel}, runtimeModelHeader(profileID))
	assertStatus(t, updateResponse, http.StatusOK)

	freshResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/dashboard", nil, runtimeModelHeader(profileID))
	assertStatus(t, freshResponse, http.StatusOK)
	var fresh statsdomain.DashboardSnapshot
	decodeJSONResponse(t, freshResponse, &fresh)
	if len(fresh.RoutingHealthMap.Links) == 0 || fresh.RoutingHealthMap.Links[0].ModelLabel != updatedLabel {
		t.Fatalf("expected dashboard snapshot cache to invalidate after model mutation, got %+v", fresh.RoutingHealthMap.Links)
	}
}

func TestAsyncDashboardPublisherRefreshesWarmedSnapshotWithoutSubscribers(t *testing.T) {
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

	fresh := harness.waitForDashboardTotalRequests(t, profileID, 1, 5*time.Second)
	if fresh.MetricSnapshot.TotalRequests != 1 || len(fresh.RecentRequests) != 1 {
		t.Fatalf("expected async no-subscriber refresh to rebuild dashboard aggregate, got %+v", fresh)
	}
}

func newRealtimeHarness(t *testing.T) *realtimeHarness {
	t.Helper()
	return newRealtimeHarnessWithConfig(t, realtimeHarnessConfig{})
}

type realtimeHarnessConfig struct {
	UseAsyncDashboardPublisher bool
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
	settings := config.Settings{Host: "127.0.0.1", Port: 8000, AppEnv: config.EnvironmentProduction, DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "s16-runtime-secret", CORSAllowedOrigins: "http://localhost:5173,http://127.0.0.1:5173", AuthJWTSecret: "s16-runtime-jwt-secret", AuthAccessTokenTTLSeconds: 900, AuthRefreshTokenTTLSeconds: 604800, AuthResetCodeTTLSeconds: 600, AuthCookieName: "prism_access_token", AuthRefreshCookieName: "prism_refresh_token", AuthCookieSecure: false}
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
	profilesService, err := managementprofiles.NewService(settings, managementprofiles.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build S16 profiles service: %v", err)
	}
	t.Cleanup(profilesService.Close)
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
	dashboardUpdates := runtimeapi.DashboardUpdatePublisher(realtimeService)
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
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "s16-runtime-test", AuthService: authService, ModelsService: modelsService, ProfilesService: profilesService, RealtimeService: realtimeService, RuntimeService: runtimeService, StatsService: statsService})
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
	baseHarness := &runtimeHarness{databaseName: databaseName, client: client, conn: conn, authService: authService, profilesService: profilesService, runtimeService: runtimeService, runtimeCache: runtimeCache, server: server, url: server.URL, upstream: upstream}
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
		`INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, status_code, success_flag, billable_flag, priced_flag, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, attempt_count, request_path, created_at, response_time_ms) VALUES ($1, $2, $3, $4, $5, 'openai', $6, $7, 200, TRUE, TRUE, TRUE, 11, 7, 25, 4, 2, 1, 1250, 'USD', '$', 1, '/v1/chat/completions', $8, 1200)`,
		usageEventID,
		profileID,
		fmt.Sprintf("ingress-%d", requestLogID),
		route.PublicModelID,
		route.TargetModelID,
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

func assertRealtimeRequestLogTokenPointers(t *testing.T, entry realtimeapi.RequestLogEntry, wantInput int, wantOutput int, wantTotal int, wantCacheRead int, wantCacheCreation int, wantReasoning int) {
	t.Helper()
	if entry.InputTokens == nil || *entry.InputTokens != wantInput || entry.OutputTokens == nil || *entry.OutputTokens != wantOutput || entry.TotalTokens == nil || *entry.TotalTokens != wantTotal || entry.CacheReadInputTokens == nil || *entry.CacheReadInputTokens != wantCacheRead || entry.CacheCreationInputTokens == nil || *entry.CacheCreationInputTokens != wantCacheCreation || entry.ReasoningTokens == nil || *entry.ReasoningTokens != wantReasoning {
		t.Fatalf("expected realtime request_log input/output/total/cache-read/cache-creation/reasoning=%d/%d/%d/%d/%d/%d, got %+v", wantInput, wantOutput, wantTotal, wantCacheRead, wantCacheCreation, wantReasoning, entry)
	}
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

func assertRealtimeRequestLogMatchesRESTDetail(t *testing.T, requestLog map[string]any, detail map[string]any) {
	t.Helper()
	summary := asMapRuntime(t, detail["summary"])
	request := asMapRuntime(t, detail["request"])
	routing := asMapRuntime(t, detail["routing"])
	usage := asMapRuntime(t, detail["usage"])
	costing := asMapRuntime(t, detail["costing"])
	pricing := asMapRuntime(t, detail["pricing"])
	fields := []struct {
		realtimeKey string
		restPayload map[string]any
		restKey     string
	}{
		{"id", summary, "id"},
		{"model_id", summary, "model_id"},
		{"model_label", summary, "model_label"},
		{"resolved_target_model_id", summary, "resolved_target_model_id"},
		{"resolved_target_model_label", summary, "resolved_target_model_label"},
		{"is_proxy_origin", summary, "is_proxy_origin"},
		{"api_family", summary, "api_family"},
		{"vendor_id", summary, "vendor_id"},
		{"vendor_key", summary, "vendor_key"},
		{"vendor_name", summary, "vendor_name"},
		{"status_code", summary, "status_code"},
		{"response_time_ms", summary, "response_time_ms"},
		{"ttft_ms", summary, "ttft_ms"},
		{"completion_duration_ms", summary, "completion_duration_ms"},
		{"is_stream", summary, "is_stream"},
		{"stream_outcome", summary, "stream_outcome"},
		{"stream_error_kind", summary, "stream_error_kind"},
		{"request_path", request, "request_path"},
		{"ingress_request_id", request, "ingress_request_id"},
		{"attempt_number", request, "attempt_number"},
		{"provider_correlation_id", request, "provider_correlation_id"},
		{"proxy_api_key_id", request, "proxy_api_key_id"},
		{"proxy_api_key_name_snapshot", request, "proxy_api_key_name_snapshot"},
		{"error_detail", request, "error_detail"},
		{"profile_id", routing, "profile_id"},
		{"endpoint_id", routing, "endpoint_id"},
		{"connection_id", routing, "connection_id"},
		{"endpoint_base_url", routing, "endpoint_base_url"},
		{"endpoint_description", routing, "endpoint_description"},
		{"input_tokens", usage, "input_tokens"},
		{"output_tokens", usage, "output_tokens"},
		{"total_tokens", usage, "total_tokens"},
		{"success_flag", usage, "success_flag"},
		{"billable_flag", usage, "billable_flag"},
		{"priced_flag", usage, "priced_flag"},
		{"unpriced_reason", usage, "unpriced_reason"},
		{"cache_read_input_tokens", usage, "cache_read_input_tokens"},
		{"cache_creation_input_tokens", usage, "cache_creation_input_tokens"},
		{"reasoning_tokens", usage, "reasoning_tokens"},
		{"input_cost_micros", costing, "input_cost_micros"},
		{"output_cost_micros", costing, "output_cost_micros"},
		{"cache_read_input_cost_micros", costing, "cache_read_input_cost_micros"},
		{"cache_creation_input_cost_micros", costing, "cache_creation_input_cost_micros"},
		{"reasoning_cost_micros", costing, "reasoning_cost_micros"},
		{"total_cost_original_micros", costing, "total_cost_original_micros"},
		{"total_cost_user_currency_micros", costing, "total_cost_user_currency_micros"},
		{"currency_code_original", costing, "currency_code_original"},
		{"report_currency_code", costing, "report_currency_code"},
		{"report_currency_symbol", costing, "report_currency_symbol"},
		{"fx_rate_used", costing, "fx_rate_used"},
		{"fx_rate_source", costing, "fx_rate_source"},
		{"pricing_snapshot_unit", pricing, "pricing_snapshot_unit"},
		{"pricing_snapshot_input", pricing, "pricing_snapshot_input"},
		{"pricing_snapshot_output", pricing, "pricing_snapshot_output"},
		{"pricing_snapshot_cache_read_input", pricing, "pricing_snapshot_cache_read_input"},
		{"pricing_snapshot_cache_creation_input", pricing, "pricing_snapshot_cache_creation_input"},
		{"pricing_snapshot_reasoning", pricing, "pricing_snapshot_reasoning"},
		{"pricing_config_version_used", pricing, "pricing_config_version_used"},
	}
	for _, field := range fields {
		if !reflect.DeepEqual(requestLog[field.realtimeKey], field.restPayload[field.restKey]) {
			t.Fatalf("expected realtime request_log.%s to match REST %s=%v, got %v", field.realtimeKey, field.restKey, field.restPayload[field.restKey], requestLog[field.realtimeKey])
		}
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
	profileTwoID := harness.createProfile(t, "Analytics Active Scope Secondary")
	route := harness.seedRealtimeDashboardRoute(t, profileID, "analytics-active")
	harness.insertDashboardActivity(t, route, profileID, 8450, 9450, harness.fixedNow.Add(-10*time.Minute))
	routeTwo := harness.seedRealtimeDashboardRoute(t, profileTwoID, "analytics-inactive-profile")
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
		t.Fatalf("publish analytics inactive profile: %v", err)
	}
	if delivered {
		t.Fatal("expected inactive profile analytics publish to report no delivery")
	}
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
