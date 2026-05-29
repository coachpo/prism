package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestProductionCleanupOrdersSideEffectDrainBeforeSchedulerStopAndDBCloseLast(t *testing.T) {
	const workerName background.WorkerName = "setup-cleanup-test"
	scheduler := background.NewScheduler(background.Config{})
	if err := scheduler.Register(background.WorkerSpec{
		Name:           workerName,
		Priority:       background.PriorityLowBackground,
		DrainPolicy:    background.DrainFinishRunning,
		CoalescePolicy: background.CoalesceNone,
	}, func(context.Context, background.Job) background.JobResult {
		return background.JobResult{Status: background.JobSucceeded}
	}); err != nil {
		t.Fatalf("register cleanup test worker: %v", err)
	}

	sideEffectErr := errors.New("side effect drain failed")
	var events []string
	record := func(name string, err error) ShutdownHook {
		return func(ctx context.Context) error {
			assertSetupFailureCleanupContext(t, ctx)
			events = append(events, name)
			return err
		}
	}
	resources := &productionResources{
		scheduler:        scheduler,
		realtimeShutdown: []ShutdownHook{record("realtime close", nil)},
		sideEffectDrain:  []ShutdownHook{record("side effect drain", sideEffectErr)},
		serviceClose: []ShutdownHook{func(ctx context.Context) error {
			assertSetupFailureCleanupContext(t, ctx)
			result := scheduler.Submit(ctx, background.JobRequest{Worker: workerName})
			if result.Status != background.SubmitRejectedStopping {
				t.Fatalf("scheduler submit during service close status = %q, want %q", result.Status, background.SubmitRejectedStopping)
			}
			events = append(events, "service close")
			return nil
		}},
		dbClose: record("db close", nil),
	}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), setupFailureCleanupContextKey{}, "setup-failure"))
	cancel()

	err := resources.cleanupForSetupFailure(ctx)
	if !errors.Is(err, sideEffectErr) {
		t.Fatalf("cleanup error %v does not include %v", err, sideEffectErr)
	}
	want := []string{"realtime close", "side effect drain", "service close", "db close"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("cleanup order = %v, want %v", events, want)
	}
}

func assertSetupFailureCleanupContext(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
		t.Fatalf("setup-failure cleanup hook received canceled context: %v", ctx.Err())
	default:
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("setup-failure cleanup hook did not receive a deadline")
	}
	if got := ctx.Value(setupFailureCleanupContextKey{}); got != "setup-failure" {
		t.Fatalf("setup-failure cleanup context value = %v, want setup-failure", got)
	}
}

type setupFailureCleanupContextKey struct{}

func TestRuntimeSideEffectOptionsFromSettings(t *testing.T) {
	settings := config.Settings{RuntimeSideEffectsConfig: config.RuntimeSideEffectsConfig{AttemptTimeout: 17 * time.Second}}
	if got := runtimeSideEffectOptions(settings); got != (runtimeapi.RuntimeSideEffectOptions{AttemptTimeout: 17 * time.Second}) {
		t.Fatalf("runtime side-effect options = %+v, want attempt timeout 17s", got)
	}
}

func TestProductionAppBuildsWithStartupTelemetry(t *testing.T) {
	settings := config.Settings{
		Host:                   "127.0.0.1",
		Port:                   18000,
		AppEnv:                 config.EnvironmentDevelopment,
		RuntimeTransportConfig: config.RuntimeTransportConfig{RequestTimeout: time.Second},
	}
	var shutdownCalled bool
	app, server, err := NewProductionApp(context.Background(), settings, ProductionOptions{
		TelemetryShutdown: func(context.Context) error {
			shutdownCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("build production app with startup telemetry: %v", err)
	}
	if app == nil || server == nil {
		t.Fatalf("expected app and server, got app=%v server=%v", app, server)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown production app with startup telemetry: %v", err)
	}
	if !shutdownCalled {
		t.Fatal("expected startup telemetry shutdown hook to flush during lifecycle shutdown")
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
