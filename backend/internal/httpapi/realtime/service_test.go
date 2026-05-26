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

func TestCancelDashboardSubscribeUsesContext(t *testing.T) {
	pool := newCancellationTestPool(t)
	service := &Service{pool: pool, manager: NewConnectionManager(time.Second), dashboardSnapshots: statsdomain.NewDashboardAggregateStore(), now: time.Now}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if service.handleDashboardSubscribe(ctx, "conn-1", &RealtimeConnection{authenticated: true}, inboundMessage{ProfileID: 1}, "") {
		t.Fatal("expected dashboard subscribe to stop on canceled context")
	}
}

func TestCancelAnalyticsSubscribeUsesContext(t *testing.T) {
	pool := newCancellationTestPool(t)
	service := &Service{pool: pool, manager: NewConnectionManager(time.Second), dashboardSnapshots: statsdomain.NewDashboardAggregateStore(), now: time.Now}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, ok := service.validateAnalyticsScope(ctx, &RealtimeConnection{authenticated: true}, inboundMessage{ProfileID: 1, Preset: "1h"}); ok {
		t.Fatal("expected analytics scope validation to stop on canceled context")
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
