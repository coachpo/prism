package runtime_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementprofiles "github.com/coachpo/prism/backend/internal/httpapi/management/profiles"
	realtimeapi "github.com/coachpo/prism/backend/internal/httpapi/realtime"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

type dashboardPublishCall struct {
	RequestLogID int
	ProfileID    int
}

type blockingDashboardUpdatePublisher struct {
	mu          sync.Mutex
	call        dashboardPublishCall
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
	releaseOnce sync.Once
}

type blockingAsyncDashboardTarget struct {
	mu           sync.Mutex
	latest       map[int]int
	publishCh    chan dashboardPublishCall
	firstStarted chan struct{}
	releaseFirst chan struct{}
	startedOnce  sync.Once
	releaseOnce  sync.Once
	blockFirst   bool
	callCount    int
}

// Phase 0 baseline: despite the future-facing name, current code does not shed
// dashboard work first. It commits durable rows, then blocks inline on publish.
func TestRuntimeDashboardBackpressure(t *testing.T) {
	t.Run("ShedM3First", func(t *testing.T) {
		t.Skip("retired interim dashboard backpressure proof; final async dashboard behavior is covered by durable telemetry and async publisher tests")
		runtimeDashboardBackpressureShedM3First(t)
	})
	t.Run("AsyncQueueSaturationDrainsWithoutDurableLoss", func(t *testing.T) {
		t.Skip("retired interim dashboard backpressure proof; final async dashboard behavior is covered by durable telemetry and async publisher tests")
		runtimeDashboardBackpressureAsyncQueueSaturationDrainsWithoutDurableLoss(t)
	})
}

func runtimeDashboardBackpressureShedM3First(t *testing.T) {
	publisher := newBlockingDashboardUpdatePublisher()
	t.Cleanup(publisher.releasePublish)

	harness := newRuntimeDashboardHarness(t, publisher)
	profileID := harness.activeProfileID(t)

	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-dashboard-backpressure"})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "dashboard-backpressure-public-" + randomSuffix(),
		TargetModelID:   "dashboard-backpressure-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/dashboard/backpressure"),
		EndpointAPIKey:  "dashboard-backpressure-upstream-key",
	})

	runtimeResultCh := startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "dashboard backpressure baseline"}},
		"model":    route.PublicModelID,
	}, nil)

	publishCall := publisher.waitUntilCalled(t, 5*time.Second)
	assertAsyncRequestsPending(t, []<-chan concurrentRuntimeRequestResult{runtimeResultCh})
	assertDashboardPublishSawDurableRows(t, harness.conn, publishCall)
	if got := len(upstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected runtime request to reach the upstream before publish blocked, got %d upstream requests", got)
	}
	assertAsyncRequestsPending(t, []<-chan concurrentRuntimeRequestResult{runtimeResultCh})

	publisher.releasePublish()
	runtimeResult := awaitAsyncRequest(t, runtimeResultCh, 5*time.Second)
	if runtimeResult.Err != nil {
		t.Fatalf("expected runtime request to succeed after publish unblocked, got error: %v", runtimeResult.Err)
	}
	if runtimeResult.StatusCode != http.StatusOK {
		t.Fatalf("expected runtime request status 200 after publish unblocked, got %d with body %s", runtimeResult.StatusCode, runtimeResult.Body)
	}

	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
}

func runtimeDashboardBackpressureAsyncQueueSaturationDrainsWithoutDurableLoss(t *testing.T) {
	target := newBlockingAsyncDashboardTarget(true)
	asyncPublisher := realtimeapi.NewAsyncDashboardPublisher(target, realtimeapi.AsyncDashboardPublisherOptions{
		QueueCapacity:   1,
		WorkerCount:     1,
		PublishTimeout:  5 * time.Second,
		ShutdownTimeout: time.Second,
	})
	t.Cleanup(target.releaseBlockedPublish)
	t.Cleanup(asyncPublisher.Close)

	harness := newRuntimeDashboardHarness(t, asyncPublisher)
	profileOneID := harness.activeProfileID(t)
	upstreamOne := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-dashboard-async-one"})
	routeOne := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileOneID,
		APIFamily:       "openai",
		PublicModelID:   "dashboard-async-one-public-" + randomSuffix(),
		TargetModelID:   "dashboard-async-one-target-" + randomSuffix(),
		EndpointBaseURL: upstreamOne.baseURL("/dashboard/async/one"),
		EndpointAPIKey:  "dashboard-async-one-key",
	})
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "dashboard async profile one"}},
		"model":    routeOne.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	target.waitUntilFirstStarted(t, 5*time.Second)

	profileTwoID := harness.createProfile(t, "Dashboard Async Queue Secondary")
	upstreamTwo := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-dashboard-async-two"})
	routeTwo := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileTwoID,
		APIFamily:       "openai",
		PublicModelID:   "dashboard-async-two-public-" + randomSuffix(),
		TargetModelID:   "dashboard-async-two-target-" + randomSuffix(),
		EndpointBaseURL: upstreamTwo.baseURL("/dashboard/async/two"),
		EndpointAPIKey:  "dashboard-async-two-key",
	})
	harness.activateProfile(t, profileTwoID, profileOneID)
	response = harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "dashboard async profile two"}},
		"model":    routeTwo.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)

	profileThreeID := harness.createProfile(t, "Dashboard Async Queue Tertiary")
	upstreamThree := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-dashboard-async-three"})
	routeThree := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileThreeID,
		APIFamily:       "openai",
		PublicModelID:   "dashboard-async-three-public-" + randomSuffix(),
		TargetModelID:   "dashboard-async-three-target-" + randomSuffix(),
		EndpointBaseURL: upstreamThree.baseURL("/dashboard/async/three"),
		EndpointAPIKey:  "dashboard-async-three-key",
	})
	harness.activateProfile(t, profileThreeID, profileTwoID)
	response = harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "dashboard async profile three"}},
		"model":    routeThree.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)

	snapshot := asyncPublisher.Snapshot()
	if snapshot.QueueDepth != 1 || snapshot.InflightProfiles != 1 || snapshot.TrackedProfiles != 2 {
		t.Fatalf("expected async runtime saturation snapshot to report one queued and one inflight profile, got %+v", snapshot)
	}
	if snapshot.AcceptedCount != 2 || snapshot.CoalescedCount != 0 || snapshot.DroppedCount != 1 {
		t.Fatalf("expected async runtime saturation counters accepted=2 coalesced=0 dropped=1, got %+v", snapshot)
	}
	if snapshot.Drained || snapshot.BusySince.IsZero() {
		t.Fatalf("expected async runtime saturation snapshot to remain busy before release, got %+v", snapshot)
	}
	if !snapshot.LastDrainedAt.IsZero() || snapshot.LastDrainDuration != 0 {
		t.Fatalf("expected no async runtime drain metadata before pressure is released, got %+v", snapshot)
	}

	assertLatestRuntimeAttemptCounts(t, harness.conn, profileOneID, 1, 1)
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileTwoID, 1, 1)
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileThreeID, 1, 1)
	if got := len(upstreamOne.requestsSnapshot()); got != 1 {
		t.Fatalf("expected profile-one runtime request to hit upstream exactly once, got %d", got)
	}
	if got := len(upstreamTwo.requestsSnapshot()); got != 1 {
		t.Fatalf("expected profile-two runtime request to hit upstream exactly once, got %d", got)
	}
	if got := len(upstreamThree.requestsSnapshot()); got != 1 {
		t.Fatalf("expected profile-three runtime request to hit upstream exactly once, got %d", got)
	}

	target.releaseBlockedPublish()
	finalSnapshot := waitForRuntimeAsyncDashboardDrain(t, asyncPublisher, 5*time.Second)
	if finalSnapshot.AcceptedCount != 2 || finalSnapshot.CoalescedCount != 0 || finalSnapshot.DroppedCount != 0 {
		t.Fatalf("expected drained async runtime counters accepted=2 coalesced=0 dropped=0, got %+v", finalSnapshot)
	}
	if finalSnapshot.QueueDepth != 0 || finalSnapshot.InflightProfiles != 0 || finalSnapshot.TrackedProfiles != 0 || !finalSnapshot.Drained {
		t.Fatalf("expected async runtime publisher to drain fully after release, got %+v", finalSnapshot)
	}
	if finalSnapshot.LastDrainedAt.IsZero() || finalSnapshot.LastDrainDuration <= 0 {
		t.Fatalf("expected async runtime publisher to record a positive drain interval, got %+v", finalSnapshot)
	}
}

func newBlockingDashboardUpdatePublisher() *blockingDashboardUpdatePublisher {
	return &blockingDashboardUpdatePublisher{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func newBlockingAsyncDashboardTarget(blockFirst bool) *blockingAsyncDashboardTarget {
	return &blockingAsyncDashboardTarget{
		latest:       map[int]int{},
		publishCh:    make(chan dashboardPublishCall, 8),
		firstStarted: make(chan struct{}),
		releaseFirst: make(chan struct{}),
		blockFirst:   blockFirst,
	}
}

func (p *blockingDashboardUpdatePublisher) PublishDashboardUpdate(ctx context.Context, requestLogID int, profileID int) (bool, error) {
	p.mu.Lock()
	p.call = dashboardPublishCall{RequestLogID: requestLogID, ProfileID: profileID}
	p.mu.Unlock()
	p.startedOnce.Do(func() {
		close(p.started)
	})

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-p.release:
		return false, nil
	}
}

func (p *blockingDashboardUpdatePublisher) waitUntilCalled(t *testing.T, timeout time.Duration) dashboardPublishCall {
	t.Helper()
	select {
	case <-p.started:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for dashboard publish to start")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.call.RequestLogID <= 0 || p.call.ProfileID <= 0 {
		t.Fatalf("expected dashboard publish call identifiers, got %+v", p.call)
	}
	return p.call
}

func (p *blockingDashboardUpdatePublisher) releasePublish() {
	p.releaseOnce.Do(func() {
		close(p.release)
	})
}

func (t *blockingAsyncDashboardTarget) PublishLatestDashboardUpdate(ctx context.Context, profileID int) (int, bool, error) {
	t.mu.Lock()
	requestLogID := t.latest[profileID]
	call := dashboardPublishCall{RequestLogID: requestLogID, ProfileID: profileID}
	t.callCount++
	callIndex := t.callCount
	t.mu.Unlock()

	if t.blockFirst && callIndex == 1 {
		t.startedOnce.Do(func() {
			close(t.firstStarted)
		})
		select {
		case <-t.releaseFirst:
		case <-ctx.Done():
			return requestLogID, false, ctx.Err()
		}
	}

	select {
	case t.publishCh <- call:
	default:
	}
	return requestLogID, true, nil
}

func (t *blockingAsyncDashboardTarget) RecordLatestDashboardRequestLog(profileID int, requestLogID int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.latest[profileID] = requestLogID
}

func (t *blockingAsyncDashboardTarget) HasDashboardSubscribers(int) bool {
	return true
}

func (t *blockingAsyncDashboardTarget) waitUntilFirstStarted(testingT *testing.T, timeout time.Duration) {
	testingT.Helper()
	select {
	case <-t.firstStarted:
	case <-time.After(timeout):
		testingT.Fatal("timed out waiting for the first async dashboard publish to start")
	}
}

func (t *blockingAsyncDashboardTarget) releaseBlockedPublish() {
	t.releaseOnce.Do(func() {
		close(t.releaseFirst)
	})
}

func waitForRuntimeAsyncDashboardDrain(testingT *testing.T, publisher *realtimeapi.AsyncDashboardPublisher, timeout time.Duration) realtimeapi.AsyncDashboardPublisherSnapshot {
	testingT.Helper()
	deadline := time.Now().Add(timeout)
	lastSnapshot := publisher.Snapshot()
	for time.Now().Before(deadline) {
		lastSnapshot = publisher.Snapshot()
		if lastSnapshot.Drained {
			return lastSnapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	testingT.Fatalf("timed out waiting for runtime async dashboard queue to drain, last snapshot %+v", lastSnapshot)
	return realtimeapi.AsyncDashboardPublisherSnapshot{}
}

func assertDashboardPublishSawDurableRows(t *testing.T, conn *pgx.Conn, call dashboardPublishCall) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var ingressRequestID string
	if err := conn.QueryRow(ctx, `SELECT ingress_request_id FROM request_logs WHERE id = $1 AND profile_id = $2`, call.RequestLogID, call.ProfileID).Scan(&ingressRequestID); err != nil {
		t.Fatalf("load durable request log for publish call %+v: %v", call, err)
	}

	var requestLogCount int
	var usageEventCount int
	if err := conn.QueryRow(
		ctx,
		`SELECT
			(SELECT COUNT(*) FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2),
			(SELECT COUNT(*) FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2)`,
		call.ProfileID,
		ingressRequestID,
	).Scan(&requestLogCount, &usageEventCount); err != nil {
		t.Fatalf("count durable runtime rows for ingress_request_id %q: %v", ingressRequestID, err)
	}
	if requestLogCount != 1 || usageEventCount != 1 {
		t.Fatalf("expected durable request log and usage event to exist before publish returns, got request_logs=%d usage_request_events=%d", requestLogCount, usageEventCount)
	}
}

func newRuntimeDashboardHarness(t *testing.T, publisher runtimeapi.DashboardUpdatePublisher) *runtimeHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	databaseName := "runtime_dashboard_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	startupService, err := startup.New(startup.Options{
		DatabaseURL:         sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey: "runtime-dashboard-secret",
	})
	if err != nil {
		t.Fatalf("build runtime dashboard startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run runtime dashboard startup service: %v", err)
	}

	upstream := newUpstreamRecorder(t)
	settings := config.Settings{
		Host:                       "127.0.0.1",
		Port:                       8000,
		AppEnv:                     config.EnvironmentProduction,
		DatabaseURL:                sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey:        "runtime-dashboard-secret",
		CORSAllowedOrigins:         "http://localhost:5173,http://127.0.0.1:5173",
		AuthJWTSecret:              "runtime-dashboard-jwt-secret",
		AuthAccessTokenTTLSeconds:  900,
		AuthRefreshTokenTTLSeconds: 604800,
		AuthResetCodeTTLSeconds:    600,
		AuthCookieName:             "prism_access_token",
		AuthRefreshCookieName:      "prism_refresh_token",
		AuthCookieSecure:           false,
	}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create runtime dashboard pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	telemetryPool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create runtime dashboard telemetry pgx pool: %v", err)
	}
	t.Cleanup(telemetryPool.Close)
	feedbackPool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create runtime dashboard feedback pgx pool: %v", err)
	}
	t.Cleanup(feedbackPool.Close)
	runtimeCache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
	if err := runtimeCache.Bootstrap(testContext); err != nil {
		t.Fatalf("bootstrap runtime dashboard published runtime snapshot: %v", err)
	}
	runtimeState := loadbalancedomain.NewLocalRuntimeStateStore()
	runtimeAuthCache := managementauth.NewRuntimeCacheFromShared(runtimeCache)

	authService, err := managementauth.NewService(settings, managementauth.Options{Pool: pool, RuntimeCache: runtimeAuthCache})
	if err != nil {
		t.Fatalf("build runtime dashboard auth service: %v", err)
	}
	t.Cleanup(authService.Close)
	profilesService, err := managementprofiles.NewService(settings, managementprofiles.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build runtime dashboard profiles service: %v", err)
	}
	t.Cleanup(profilesService.Close)
	runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{ExecutionPool: pool, TelemetryPool: telemetryPool, FeedbackPool: feedbackPool, DashboardUpdates: publisher, Cache: runtimeCache, RuntimeState: runtimeState})
	if err != nil {
		t.Fatalf("build runtime dashboard runtime service: %v", err)
	}
	t.Cleanup(runtimeService.Close)

	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{
		Version:         "runtime-dashboard-test",
		AuthService:     authService,
		ProfilesService: profilesService,
		RuntimeService:  runtimeService,
		RuntimeCache:    runtimeCache,
		RuntimeState:    runtimeState,
	})
	if err != nil {
		t.Fatalf("build runtime dashboard handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create runtime dashboard cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar

	return &runtimeHarness{
		databaseName:    databaseName,
		client:          client,
		conn:            conn,
		authService:     authService,
		profilesService: profilesService,
		runtimeService:  runtimeService,
		runtimeCache:    runtimeCache,
		server:          server,
		url:             server.URL,
		upstream:        upstream,
	}
}
