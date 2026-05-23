package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/lifecycle"
	"github.com/coachpo/prism/backend/internal/platform/migrate"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

func TestLoadBootstrapSettingsDefaultsToLocalConfigJSON(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	databaseURL := "postgres://default-bootstrap@db.invalid:5432/prism?sslmode=disable"
	t.Setenv(config.BootstrapConfigPathEnv, "")
	t.Setenv("DATABASE_URL", databaseURL)

	bootstrapConfig, err := loadBootstrapSettings()
	if err != nil {
		t.Fatalf("load bootstrap settings from default path: %v", err)
	}
	if bootstrapConfig.ConfigPath != defaultBootstrapConfigPath {
		t.Fatalf("expected default config path %q, got %q", defaultBootstrapConfigPath, bootstrapConfig.ConfigPath)
	}
	if _, err := os.Stat(filepath.Join(tempDir, defaultBootstrapConfigPath)); err != nil {
		t.Fatalf("expected default bootstrap config file to be created: %v", err)
	}
	assertLoadedBootstrapSettings(t, bootstrapConfig, databaseURL)
}

func TestLoadBootstrapSettingsUsesExplicitBootstrapPath(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "nested", "bootstrap.json")
	databaseURL := "postgres://explicit-bootstrap@db.invalid:5432/prism?sslmode=disable"
	t.Setenv(config.BootstrapConfigPathEnv, "  "+configPath+"  ")
	t.Setenv("DATABASE_URL", databaseURL)

	bootstrapConfig, err := loadBootstrapSettings()
	if err != nil {
		t.Fatalf("load bootstrap settings from explicit path: %v", err)
	}
	if bootstrapConfig.ConfigPath != configPath {
		t.Fatalf("expected explicit config path %q, got %q", configPath, bootstrapConfig.ConfigPath)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected explicit bootstrap config file to be created: %v", err)
	}
	assertLoadedBootstrapSettings(t, bootstrapConfig, databaseURL)
}

func TestLoadBootstrapSettingsRepairsStaleDocsEnabledConfigJSON(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	databaseURL := "postgres://stale-bootstrap@db.invalid:5432/prism?sslmode=disable"
	t.Setenv(config.BootstrapConfigPathEnv, "")
	t.Setenv("DATABASE_URL", databaseURL)

	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})
	if _, err := manager.LoadOrSeed(defaultBootstrapConfigPath); err != nil {
		t.Fatalf("seed bootstrap config: %v", err)
	}

	configPath := filepath.Join(tempDir, defaultBootstrapConfigPath)
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read seeded bootstrap config: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal seeded bootstrap config: %v", err)
	}
	payload["docsEnabled"] = true
	mutated, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal stale bootstrap config: %v", err)
	}
	mutated = append(mutated, '\n')
	if err := os.WriteFile(configPath, mutated, 0o600); err != nil {
		t.Fatalf("write stale bootstrap config: %v", err)
	}

	bootstrapConfig, err := loadBootstrapSettings()
	if err != nil {
		t.Fatalf("repair stale bootstrap config: %v", err)
	}
	assertLoadedBootstrapSettings(t, bootstrapConfig, databaseURL)

	repairedRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read repaired bootstrap config: %v", err)
	}
	if strings.Contains(string(repairedRaw), `"docsEnabled"`) {
		t.Fatalf("expected repaired bootstrap config to omit docsEnabled, got:\n%s", repairedRaw)
	}
}

func TestRunPrintEffectiveStartupSettingsReturnsBeforeStartupAndServerWork(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "run-print-bootstrap.json")
	databaseURL := "postgres://run-startup-print@db.invalid:5432/prism?sslmode=disable"
	t.Setenv(config.BootstrapConfigPathEnv, configPath)
	t.Setenv("DATABASE_URL", databaseURL)
	t.Setenv("PRISM_PRINT_EFFECTIVE_STARTUP_SETTINGS", "1")

	var runErr error
	outputText := captureStdout(t, func() {
		runErr = run(context.Background())
	})
	if runErr != nil {
		t.Fatalf("run print effective startup settings: %v", runErr)
	}
	assertEffectiveStartupSettingsOutput(t, outputText, configPath, databaseURL)
	assertNoStartupOrServerWorkLogged(t, outputText)
}

func TestPrintEffectiveStartupSettingsExitsBeforeStartupAndServerWork(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "print-bootstrap.json")
	databaseURL := "postgres://startup-print@db.invalid:5432/prism?sslmode=disable"
	command := exec.Command(os.Args[0], "-test.run=^TestPrintEffectiveStartupSettingsHelperProcess$")
	command.Env = append(os.Environ(),
		"GO_WANT_PRISM_PRINT_HELPER=1",
		config.BootstrapConfigPathEnv+"="+configPath,
		"DATABASE_URL="+databaseURL,
		"PRISM_PRINT_EFFECTIVE_STARTUP_SETTINGS=1",
	)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("print effective startup settings helper failed: %v\n%s", err, output)
	}
	outputText := string(output)
	assertEffectiveStartupSettingsOutput(t, outputText, configPath, databaseURL)
	assertNoStartupOrServerWorkLogged(t, outputText)
}

func TestPrintEffectiveStartupSettingsHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_PRISM_PRINT_HELPER") != "1" {
		return
	}
	main()
	os.Exit(0)
}

func TestRunStartupTimeoutCancelsStartupContext(t *testing.T) {
	started := make(chan struct{})
	startupRunner := fakeStartupRunner{
		run: func(ctx context.Context) (startup.Result, error) {
			close(started)
			<-ctx.Done()
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatalf("expected startup context deadline exceeded, got %v", ctx.Err())
			}
			return startup.Result{}, ctx.Err()
		},
	}

	_, err := runStartupWithTimeout(context.Background(), startupRunner, time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded from startup timeout, got %v", err)
	}
	select {
	case <-started:
	default:
		t.Fatal("startup runner was not invoked")
	}
}

func TestRunLogsStartupStepAndMigrationOutcome(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "run-logs-bootstrap.json")
	databaseURL := "postgres://run-startup-logs@db.invalid:5432/prism?sslmode=disable"
	t.Setenv(config.BootstrapConfigPathEnv, configPath)
	t.Setenv("DATABASE_URL", databaseURL)

	logs := captureSlog(t)
	restoreRunSeams := replaceRunSeams(t)
	defer restoreRunSeams()

	var observedOptions startup.Options
	newStartupRunner = func(options startup.Options) (startupRunner, error) {
		observedOptions = options
		return fakeStartupRunner{run: func(ctx context.Context) (startup.Result, error) {
			if ctx == context.Background() {
				t.Fatal("startup received background context")
			}
			if options.StepObserver == nil {
				t.Fatal("expected startup StepObserver")
			}
			options.StepObserver(startup.StepMigrations)
			return startup.Result{Migration: migrate.Result{Outcome: migrate.OutcomeNoop}}, nil
		}}, nil
	}
	newPlatformApp = func(ctx context.Context, settings config.Settings, options lifecycle.ProductionOptions) (*lifecycle.App, *http.Server, error) {
		server := &http.Server{Addr: "127.0.0.1:0"}
		app := lifecycle.NewApp(lifecycle.Options{HTTPServer: fakeLifecycleHTTPServer{serveErr: http.ErrServerClosed}})
		return app, server, nil
	}

	if err := run(context.Background()); err != nil {
		t.Fatalf("run with fake startup/server: %v", err)
	}
	if observedOptions.DatabaseURL != databaseURL {
		t.Fatalf("expected startup database URL %q, got %q", databaseURL, observedOptions.DatabaseURL)
	}
	logText := logs.String()
	assertLogContainsAttrs(t, logText, "startup step started", map[string]string{"step": string(startup.StepMigrations)})
	assertLogContainsAttrs(t, logText, "startup sequence completed", map[string]string{"migration_outcome": string(migrate.OutcomeNoop)})
	assertLogDoesNotContainSecrets(t, logText, databaseURL)
}

func TestRunContextCancellationShutsDownLifecycleAppWithoutExit(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "run-cancel-bootstrap.json")
	databaseURL := "postgres://run-cancel@db.invalid:5432/prism?sslmode=disable"
	t.Setenv(config.BootstrapConfigPathEnv, configPath)
	t.Setenv("DATABASE_URL", databaseURL)

	restoreRunSeams := replaceRunSeams(t)
	defer restoreRunSeams()

	server := newCancellableLifecycleHTTPServer()
	dbClosed := make(chan struct{})
	newStartupRunner = func(options startup.Options) (startupRunner, error) {
		return fakeStartupRunner{run: func(context.Context) (startup.Result, error) {
			return startup.Result{Migration: migrate.Result{Outcome: migrate.OutcomeNoop}}, nil
		}}, nil
	}
	newPlatformApp = func(ctx context.Context, settings config.Settings, options lifecycle.ProductionOptions) (*lifecycle.App, *http.Server, error) {
		app := lifecycle.NewApp(lifecycle.Options{
			HTTPServer: server,
			DBClose: func(context.Context) error {
				close(dbClosed)
				return nil
			},
		})
		return app, &http.Server{Addr: "127.0.0.1:0"}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- run(ctx) }()
	server.waitStarted(t)
	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("run returned error after context cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not return after context cancellation")
	}
	select {
	case <-dbClosed:
	case <-time.After(time.Second):
		t.Fatal("lifecycle app did not run shutdown hooks")
	}
}

func TestRunPrintEffectiveStartupSettingsExcludesStartupLogs(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "run-print-logs-bootstrap.json")
	databaseURL := "postgres://run-startup-print-logs@db.invalid:5432/prism?sslmode=disable"
	t.Setenv(config.BootstrapConfigPathEnv, configPath)
	t.Setenv("DATABASE_URL", databaseURL)
	t.Setenv("PRISM_PRINT_EFFECTIVE_STARTUP_SETTINGS", "1")

	logs := captureSlog(t)
	restoreRunSeams := replaceRunSeams(t)
	defer restoreRunSeams()
	newStartupRunner = func(options startup.Options) (startupRunner, error) {
		t.Fatal("print-effective mode must not build startup service")
		return nil, nil
	}
	newPlatformApp = func(ctx context.Context, settings config.Settings, options lifecycle.ProductionOptions) (*lifecycle.App, *http.Server, error) {
		t.Fatal("print-effective mode must not build app")
		return nil, nil, nil
	}

	var runErr error
	outputText := captureStdout(t, func() {
		runErr = run(context.Background())
	})
	if runErr != nil {
		t.Fatalf("run print effective startup settings: %v", runErr)
	}
	assertEffectiveStartupSettingsOutput(t, outputText, configPath, databaseURL)
	assertNoStartupOrServerWorkLogged(t, outputText)
	assertNoStartupOrServerWorkLogged(t, logs.String())
}

func assertLoadedBootstrapSettings(t *testing.T, bootstrapConfig bootstrapStartupConfig, wantDatabaseURL string) {
	t.Helper()
	if bootstrapConfig.Settings.DatabaseURL != wantDatabaseURL {
		t.Fatalf("expected loaded database URL %q, got %q", wantDatabaseURL, bootstrapConfig.Settings.DatabaseURL)
	}
	if bootstrapConfig.LoadedRevision != 1 {
		t.Fatalf("expected loaded bootstrap revision 1, got %d", bootstrapConfig.LoadedRevision)
	}
	if strings.TrimSpace(bootstrapConfig.LoadedDocumentETag) == "" {
		t.Fatal("expected loaded bootstrap document ETag")
	}
}

func assertEffectiveStartupSettingsOutput(t *testing.T, outputText, configPath, databaseURL string) {
	t.Helper()
	for _, expectedLine := range []string{
		config.BootstrapConfigPathEnv + "=" + configPath,
		"DATABASE_URL=" + databaseURL,
		"SERVER_HOST=0.0.0.0",
		"SERVER_PORT=18000",
		"SERVER_ADDR=0.0.0.0:18000",
	} {
		if !strings.Contains(outputText, expectedLine+"\n") {
			t.Fatalf("expected print output to contain %q, got:\n%s", expectedLine, outputText)
		}
	}
}

func assertNoStartupOrServerWorkLogged(t *testing.T, outputText string) {
	t.Helper()
	for _, forbidden := range []string{
		"startup step started",
		"startup sequence completed",
		"migration_outcome",
		"starting prism backend",
		"startup sequence failed",
		"failed to build server",
		"server exited",
	} {
		if strings.Contains(outputText, forbidden) {
			t.Fatalf("print output shows startup/server work %q:\n%s", forbidden, outputText)
		}
	}
}

type fakeStartupRunner struct {
	run func(context.Context) (startup.Result, error)
}

func (r fakeStartupRunner) Run(ctx context.Context) (startup.Result, error) {
	return r.run(ctx)
}

type fakeLifecycleHTTPServer struct {
	serveErr error
}

func (server fakeLifecycleHTTPServer) ListenAndServe() error {
	return server.serveErr
}

func (server fakeLifecycleHTTPServer) Shutdown(context.Context) error {
	return nil
}

type cancellableLifecycleHTTPServer struct {
	started  chan struct{}
	shutdown chan struct{}
}

func newCancellableLifecycleHTTPServer() *cancellableLifecycleHTTPServer {
	return &cancellableLifecycleHTTPServer{
		started:  make(chan struct{}),
		shutdown: make(chan struct{}),
	}
}

func (server *cancellableLifecycleHTTPServer) ListenAndServe() error {
	close(server.started)
	<-server.shutdown
	return http.ErrServerClosed
}

func (server *cancellableLifecycleHTTPServer) Shutdown(context.Context) error {
	select {
	case <-server.shutdown:
	default:
		close(server.shutdown)
	}
	return nil
}

func (server *cancellableLifecycleHTTPServer) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-server.started:
	case <-time.After(time.Second):
		t.Fatal("lifecycle app did not start serving")
	}
}

func replaceRunSeams(t *testing.T) func() {
	t.Helper()
	originalNewStartupRunner := newStartupRunner
	originalNewPlatformApp := newPlatformApp
	return func() {
		newStartupRunner = originalNewStartupRunner
		newPlatformApp = originalNewPlatformApp
	}
}

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() {
		slog.SetDefault(originalLogger)
	})
	return &output
}

func assertLogContainsAttrs(t *testing.T, logText, message string, attrs map[string]string) {
	t.Helper()
	for line := range strings.SplitSeq(strings.TrimSpace(logText), "\n") {
		if !strings.Contains(line, `"msg":"`+message+`"`) {
			continue
		}
		for key, value := range attrs {
			if !strings.Contains(line, `"`+key+`":"`+value+`"`) {
				t.Fatalf("log line for %q missing attr %s=%q:\n%s", message, key, value, line)
			}
		}
		return
	}
	t.Fatalf("expected log message %q in logs:\n%s", message, logText)
}

func assertLogDoesNotContainSecrets(t *testing.T, logText string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(logText, secret) {
			t.Fatalf("log output leaked secret/config value %q:\n%s", secret, logText)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout capture pipe: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = originalStdout
	}()

	fn()
	os.Stdout = originalStdout
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout capture writer: %v", err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout capture: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout capture reader: %v", err)
	}
	return string(output)
}
