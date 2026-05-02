package realtime

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

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
