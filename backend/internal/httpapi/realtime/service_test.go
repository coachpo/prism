package realtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

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
