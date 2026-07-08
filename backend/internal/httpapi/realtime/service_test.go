package realtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestCheckOriginUsesPublishedCORSProvider(t *testing.T) {
	corsProvider := newMutableRealtimeCORSProvider("https://old.example.test")
	service := &Service{corsOriginProvider: corsProvider}

	if !service.checkOrigin(realtimeOriginRequest("https://old.example.test")) {
		t.Fatal("expected initial configured origin to be allowed")
	}
	if service.checkOrigin(realtimeOriginRequest("https://new.example.test")) {
		t.Fatal("expected new origin to be rejected before publish")
	}

	corsProvider.publish("https://new.example.test")

	if !service.checkOrigin(realtimeOriginRequest("https://new.example.test")) {
		t.Fatal("expected published origin to be allowed")
	}
	if service.checkOrigin(realtimeOriginRequest("https://old.example.test")) {
		t.Fatal("expected retired origin to be rejected")
	}
}

func TestConnectionManagerCloseClosesActiveWebSockets(t *testing.T) {
	manager := NewConnectionManager(100 * time.Millisecond)
	connected := make(chan struct{})
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		connectionID := manager.Connect(socket)
		defer manager.Disconnect(connectionID)
		close(connected)
		for {
			if _, _, err := socket.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer func() { _ = conn.Close() }()
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for websocket connection")
	}

	manager.Close()
	_, _, err = conn.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseGoingAway) {
		t.Fatalf("expected going-away close after manager shutdown, got %v", err)
	}
	if got := manager.Stats()["total_connections"]; got != 0 {
		t.Fatalf("total connections after close = %v, want 0", got)
	}
}

func realtimeOriginRequest(origin string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/realtime/ws", nil)
	request.Header.Set("Origin", origin)
	return request
}

func newManagedRealtimeTestConnection(t *testing.T, manager *ConnectionManager) (*websocket.Conn, *RealtimeConnection, string) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	connectionIDCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
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
		t.Fatalf("dial websocket: %v", err)
	}

	var connectionID string
	select {
	case connectionID = <-connectionIDCh:
		if connectionID == "" {
			t.Fatal("expected realtime connection ID")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for realtime connection")
	}

	connection := manager.GetConnection(connectionID)
	if connection == nil {
		t.Fatal("expected managed realtime connection")
	}
	return conn, connection, connectionID
}

func readRealtimeTestMessage(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	var message map[string]any
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatalf("read websocket JSON: %v", err)
	}
	return message
}

type mutableRealtimeCORSProvider struct {
	mu       sync.RWMutex
	snapshot platformcors.Snapshot
}

func newMutableRealtimeCORSProvider(origins ...string) *mutableRealtimeCORSProvider {
	return &mutableRealtimeCORSProvider{snapshot: platformcors.NewSnapshot(origins)}
}

func (p *mutableRealtimeCORSProvider) CORSSnapshot() platformcors.Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.snapshot
}

func (p *mutableRealtimeCORSProvider) publish(origins ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.snapshot = platformcors.NewSnapshot(origins)
}

func TestHandleDashboardSubscribeUsesDefaultProfileID(t *testing.T) {
	manager := NewConnectionManager(time.Second)
	t.Cleanup(manager.Close)
	clientConn, connection, connectionID := newManagedRealtimeTestConnection(t, manager)
	defer func() { _ = clientConn.Close() }()

	service := &Service{manager: manager}
	if !service.handleDashboardSubscribe(connectionID, connection, "dashboard") {
		t.Fatal("expected dashboard subscribe to succeed")
	}

	message := readRealtimeTestMessage(t, clientConn)
	if message["type"] != "subscribed" || message["profile_id"] != float64(profiledomain.DefaultProfileID) || message["channel"] != "dashboard" {
		t.Fatalf("unexpected subscribed message: %+v", message)
	}
	if got := manager.ActiveProfileIDs("dashboard"); len(got) != 1 || got[0] != profiledomain.DefaultProfileID {
		t.Fatalf("active dashboard profile IDs = %+v, want [%d]", got, profiledomain.DefaultProfileID)
	}
}

func TestValidateAnalyticsScopeUsesDefaultProfileID(t *testing.T) {
	service := &Service{}

	profileID, preset, ok := service.validateAnalyticsScope(&RealtimeConnection{authenticated: true}, inboundMessage{ProfileID: 999, Preset: "1h"})
	if !ok {
		t.Fatal("expected analytics scope validation to succeed")
	}
	if profileID != profiledomain.DefaultProfileID {
		t.Fatalf("profile ID = %d, want %d", profileID, profiledomain.DefaultProfileID)
	}
	if preset != "1h" {
		t.Fatalf("preset = %q, want %q", preset, "1h")
	}
}

func TestCancelAnalyticsSnapshotUsesContext(t *testing.T) {
	pool := newCancellationTestPool(t)
	service := &Service{pool: pool, manager: NewConnectionManager(time.Second), dashboardSnapshots: statsdomain.NewDashboardAggregateStore(), now: time.Now}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.BuildAnalyticsSnapshot(ctx, 1, "1h", time.Now()); err == nil {
		t.Fatal("expected canceled context to abort analytics snapshot build")
	}
}

func newCancellationTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://127.0.0.1:1/prism?sslmode=disable")
	if err != nil {
		t.Fatalf("create cancellation test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestRealtimeLimiterEnforcesPerSubjectQuota(t *testing.T) {
	limiter := newRealtimeLimiter()
	releases := make([]func(), 0, maxRealtimeConnectionsPerSubject)
	for range maxRealtimeConnectionsPerSubject {
		release, ok := limiter.Acquire("subject:1")
		if !ok {
			t.Fatal("expected subject quota to admit connection")
		}
		releases = append(releases, release)
	}
	if release, ok := limiter.Acquire("subject:1"); ok {
		release()
		t.Fatal("expected subject quota rejection")
	}
	if release, ok := limiter.Acquire("subject:2"); !ok {
		t.Fatal("expected another subject to remain admissible")
	} else {
		release()
	}
	releases[0]()
	releases[0]()
	if release, ok := limiter.Acquire("subject:1"); !ok {
		t.Fatal("expected released subject slot to be reusable")
	} else {
		release()
	}
	for _, release := range releases[1:] {
		release()
	}
}

func TestRealtimeLimiterEnforcesGlobalQuota(t *testing.T) {
	limiter := newRealtimeLimiter()
	releases := make([]func(), 0, maxRealtimeConnections)
	for index := range maxRealtimeConnections {
		release, ok := limiter.Acquire("")
		if !ok {
			t.Fatalf("expected global quota to admit connection %d", index)
		}
		releases = append(releases, release)
	}
	if release, ok := limiter.Acquire("subject-overflow"); ok {
		release()
		t.Fatal("expected global quota rejection")
	}
	releases[0]()
	if release, ok := limiter.Acquire("subject-overflow"); !ok {
		t.Fatal("expected released global slot to be reusable")
	} else {
		release()
	}
	for _, release := range releases[1:] {
		release()
	}
}
