package lifecycle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

var lifecycleTestPostgres struct {
	once     sync.Once
	name     string
	hostPort string
	err      error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if lifecycleTestPostgres.name != "" {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = runLifecycleDockerCommand(cleanupCtx, "rm", "-f", lifecycleTestPostgres.name)
		cancel()
	}
	os.Exit(code)
}

func TestBuildProductionResourcesRegistersProductionWorkers(t *testing.T) {
	ctx, databaseURL := openLifecycleTestDatabase(t)
	settings := lifecycleTestSettings(databaseURL)

	resources, err := buildProductionResources(ctx, settings)
	if err != nil {
		t.Fatalf("build production resources: %v", err)
	}
	t.Cleanup(func() {
		if err := resources.cleanupForSetupFailure(context.Background()); err != nil {
			t.Errorf("cleanup production resources: %v", err)
		}
	})
	if resources.scheduler == nil {
		t.Fatal("expected production scheduler")
	}

	got := resources.scheduler.RegisteredWorkers()
	slices.Sort(got)
	want := []background.WorkerName{
		"alert_webhook_worker",
		"log_partition_maintenance",
		"management_audit_delete_jobs",
		"management_side_effect_outbox",
		"proxy_key_usage_writer",
		"runtime_feedback_pipeline",
		"runtime_shared_cache_refresh",
		"runtime_side_effects_activity",
		"runtime_telemetry_outbox",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("production worker set = %v, want %v", got, want)
	}
}

type lifecyclePostgresHarness struct {
	hostPort string
}

func lifecyclePostgres(tb testing.TB) lifecyclePostgresHarness {
	tb.Helper()
	lifecycleTestPostgres.once.Do(func() {
		containerName := "prism-lifecycle-" + lifecycleRandomSuffix()
		if err := runLifecycleDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
			lifecycleTestPostgres.err = err
			return
		}
		hostPort, err := lifecycleDockerPort(containerName)
		if err != nil {
			lifecycleTestPostgres.err = err
			return
		}
		if err := lifecycleWaitForPostgres(hostPort); err != nil {
			lifecycleTestPostgres.err = err
			return
		}
		lifecycleTestPostgres.name = containerName
		lifecycleTestPostgres.hostPort = hostPort
	})
	if lifecycleTestPostgres.err != nil {
		tb.Fatalf("start lifecycle postgres harness: %v", lifecycleTestPostgres.err)
	}
	return lifecyclePostgresHarness{hostPort: lifecycleTestPostgres.hostPort}
}

func (h lifecyclePostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func (h lifecyclePostgresHarness) openDatabase(tb testing.TB, ctx context.Context, databaseName string) *pgx.Conn {
	tb.Helper()
	adminConn, err := pgx.Connect(ctx, h.connectionString("postgres"))
	if err != nil {
		tb.Fatalf("connect admin database: %v", err)
	}
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+lifecycleQuoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		tb.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+lifecycleQuoteIdentifier(databaseName)); err != nil {
		tb.Fatalf("create database %s: %v", databaseName, err)
	}
	conn, err := pgx.Connect(ctx, h.connectionString(databaseName))
	if err != nil {
		tb.Fatalf("connect database %s: %v", databaseName, err)
	}
	return conn
}

func openLifecycleTestDatabase(tb testing.TB) (context.Context, string) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	tb.Cleanup(cancel)

	harness := lifecyclePostgres(tb)
	databaseName := "lifecycle_" + lifecycleRandomSuffix()
	conn := harness.openDatabase(tb, ctx, databaseName)
	tb.Cleanup(func() { _ = conn.Close(context.Background()) })

	startupService, err := startup.New(startup.Options{
		DatabaseURL:         harness.connectionString(databaseName),
		SecretEncryptionKey: "lifecycle-test-secret",
	})
	if err != nil {
		tb.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(ctx, conn); err != nil {
		tb.Fatalf("run startup service: %v", err)
	}
	return ctx, harness.connectionString(databaseName)
}

func lifecycleTestSettings(databaseURL string) config.Settings {
	return config.Settings{
		Host:                       "127.0.0.1",
		Port:                       8000,
		AppEnv:                     config.EnvironmentProduction,
		DatabaseURL:                databaseURL,
		SecretEncryptionKey:        "lifecycle-test-secret",
		CORSAllowedOrigins:         "http://localhost:5173,http://127.0.0.1:5173",
		AuthJWTSecret:              "lifecycle-test-jwt-secret",
		AuthAccessTokenTTLSeconds:  900,
		AuthRefreshTokenTTLSeconds: 604800,
		AuthResetCodeTTLSeconds:    600,
		AuthCookieName:             "prism_access_token",
		AuthRefreshCookieName:      "prism_refresh_token",
		AuthCookieSecure:           false,
	}
}

func runLifecycleDockerCommand(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func lifecycleDockerPort(containerName string) (string, error) {
	command := exec.Command("docker", "port", containerName, "5432/tcp")
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker port %s failed: %v\n%s", containerName, err, strings.TrimSpace(string(output)))
	}
	firstLine := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	_, port, err := net.SplitHostPort(firstLine)
	if err != nil {
		return "", fmt.Errorf("parse docker port output %q: %w", firstLine, err)
	}
	return port, nil
}

func lifecycleWaitForPostgres(hostPort string) error {
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/postgres?sslmode=disable", hostPort))
		if err == nil {
			_ = conn.Close(ctx)
			cancel()
			return nil
		}
		cancel()
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("postgres container on port %s did not become ready in time", hostPort)
}

func lifecycleQuoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func lifecycleRandomSuffix() string {
	var buffer [4]byte
	_, _ = rand.Read(buffer[:])
	return hex.EncodeToString(buffer[:])
}
