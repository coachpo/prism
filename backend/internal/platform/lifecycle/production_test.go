package lifecycle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestProductionCleanupOrdersSideEffectDrainBeforeSchedulerStopAndDBCloseLast(t *testing.T) {
	var events []string
	record := func(name string) ShutdownHook {
		return func(context.Context) error {
			events = append(events, name)
			return nil
		}
	}
	resources := &productionResources{
		realtimeShutdown: []ShutdownHook{record("realtime close")},
		sideEffectDrain:  []ShutdownHook{record("side effect drain")},
		serviceClose:     []ShutdownHook{record("service close")},
		dbClose:          record("db close"),
	}
	if err := resources.cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	want := []string{"realtime close", "side effect drain", "service close", "db close"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("cleanup order = %v, want %v", events, want)
	}
}

func TestNewProductionAppUsesPlatformHTTPServerAssemblyWithoutDatabaseOwnership(t *testing.T) {
	settings := config.Settings{
		Host:                   "127.0.0.1",
		Port:                   18000,
		AppEnv:                 config.EnvironmentDevelopment,
		RuntimeTransportConfig: config.RuntimeTransportConfig{RequestTimeout: time.Second},
	}
	app, server, err := NewProductionApp(context.Background(), settings, ProductionOptions{})
	if err != nil {
		t.Fatalf("build production app without database: %v", err)
	}
	if app == nil {
		t.Fatal("expected lifecycle app")
	}
	if server == nil {
		t.Fatal("expected HTTP server")
	}
	if server.Addr != "127.0.0.1:18000" {
		t.Fatalf("server addr = %q, want 127.0.0.1:18000", server.Addr)
	}
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", response.Code)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown production app without database: %v", err)
	}
}
