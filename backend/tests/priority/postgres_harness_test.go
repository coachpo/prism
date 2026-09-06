package priority

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/startup"
	"github.com/coachpo/prism/backend/tests/testsupport/containername"
)

// priorityTemplateDatabase holds one migrated and startup-seeded schema so each
// test clones it instead of replaying every migration.
const priorityTemplateDatabase = "prism_priority_template"

const priorityTestSecretEncryptionKey = "priority-test-secret"

var priorityTestPostgres struct {
	once     sync.Once
	name     string
	hostPort string
	err      error
}

type priorityPostgresHarness struct {
	hostPort string
}

func TestMain(m *testing.M) {
	code := m.Run()
	if priorityTestPostgres.name != "" {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = runPriorityDockerCommand(cleanupCtx, "rm", "-f", priorityTestPostgres.name)
		cancel()
	}
	os.Exit(code)
}

func priorityPostgres(t testing.TB) priorityPostgresHarness {
	t.Helper()
	priorityTestPostgres.once.Do(func() {
		containerName := containername.Prefix() + "-priority-" + priorityRandomSuffix()
		if err := runPriorityDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "--tmpfs", "/var/lib/postgresql/data:rw", "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
			priorityTestPostgres.err = err
			return
		}
		hostPort, err := priorityDockerPort(containerName)
		if err != nil {
			priorityTestPostgres.err = err
			return
		}
		if err := priorityWaitForPostgres(hostPort); err != nil {
			priorityTestPostgres.err = err
			return
		}
		priorityTestPostgres.name = containerName
		priorityTestPostgres.hostPort = hostPort
		if err := preparePriorityTemplateDatabase(priorityPostgresHarness{hostPort: hostPort}); err != nil {
			priorityTestPostgres.err = err
			return
		}
	})
	if priorityTestPostgres.err != nil {
		t.Fatalf("start priority postgres harness: %v", priorityTestPostgres.err)
	}
	return priorityPostgresHarness{hostPort: priorityTestPostgres.hostPort}
}

func (h priorityPostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func (h priorityPostgresHarness) openDatabase(tb testing.TB, ctx context.Context, databaseName string) *pgx.Conn {
	tb.Helper()
	adminConn, err := pgx.Connect(ctx, h.connectionString("postgres"))
	if err != nil {
		tb.Fatalf("connect admin database: %v", err)
	}
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+priorityQuoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		tb.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+priorityQuoteIdentifier(databaseName)+` TEMPLATE `+priorityQuoteIdentifier(priorityTemplateDatabase)); err != nil {
		tb.Fatalf("create database %s: %v", databaseName, err)
	}
	conn, err := pgx.Connect(ctx, h.connectionString(databaseName))
	if err != nil {
		tb.Fatalf("connect database %s: %v", databaseName, err)
	}
	return conn
}

func openPriorityTestPool(tb testing.TB) (context.Context, *pgxpool.Pool, string) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	tb.Cleanup(cancel)

	harness := priorityPostgres(tb)
	databaseName := "priority_" + priorityRandomSuffix()
	conn := harness.openDatabase(tb, ctx, databaseName)
	tb.Cleanup(func() { _ = conn.Close(context.Background()) })

	startupService, err := startup.New(startup.Options{
		DatabaseURL:         harness.connectionString(databaseName),
		SecretEncryptionKey: priorityTestSecretEncryptionKey,
	})
	if err != nil {
		tb.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(ctx, conn); err != nil {
		tb.Fatalf("run startup service: %v", err)
	}

	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		tb.Fatalf("open priority test pool: %v", err)
	}
	tb.Cleanup(pool.Close)
	return ctx, pool, harness.connectionString(databaseName)
}

func priorityTestSettings(databaseURL string) config.Settings {
	return config.Settings{
		Host:                       "127.0.0.1",
		Port:                       8000,
		AppEnv:                     config.EnvironmentProduction,
		DatabaseURL:                databaseURL,
		SecretEncryptionKey:        priorityTestSecretEncryptionKey,
		CORSAllowedOrigins:         "http://localhost:5173,http://127.0.0.1:5173",
		AuthJWTSecret:              "priority-test-jwt-secret",
		AuthAccessTokenTTLSeconds:  900,
		AuthRefreshTokenTTLSeconds: 604800,
		AuthResetCodeTTLSeconds:    600,
		AuthCookieName:             "prism_access_token",
		AuthRefreshCookieName:      "prism_refresh_token",
		AuthCookieSecure:           false,
	}
}

func runPriorityDockerCommand(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func priorityDockerPort(containerName string) (string, error) {
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

func priorityWaitForPostgres(hostPort string) error {
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

func priorityQuoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func priorityRandomSuffix() string {
	var buffer [4]byte
	_, _ = rand.Read(buffer[:])
	return hex.EncodeToString(buffer[:])
}

func preparePriorityTemplateDatabase(harness priorityPostgresHarness) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	adminConn, err := pgx.Connect(ctx, harness.connectionString("postgres"))
	if err != nil {
		return fmt.Errorf("connect postgres admin database: %w", err)
	}
	defer func() { _ = adminConn.Close(context.Background()) }()

	// PostgreSQL refuses to drop a database that is still flagged as a
	// template, so clear the flag before recreating it.
	if _, err := adminConn.Exec(ctx, `UPDATE pg_database SET datistemplate = FALSE WHERE datname = $1`, priorityTemplateDatabase); err != nil {
		return fmt.Errorf("clear template flag on %s: %w", priorityTemplateDatabase, err)
	}
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+priorityQuoteIdentifier(priorityTemplateDatabase)+` WITH (FORCE)`); err != nil {
		return fmt.Errorf("drop template database %s: %w", priorityTemplateDatabase, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+priorityQuoteIdentifier(priorityTemplateDatabase)); err != nil {
		return fmt.Errorf("create template database %s: %w", priorityTemplateDatabase, err)
	}

	templateConn, err := pgx.Connect(ctx, harness.connectionString(priorityTemplateDatabase))
	if err != nil {
		return fmt.Errorf("connect template database %s: %w", priorityTemplateDatabase, err)
	}
	startupService, err := startup.New(startup.Options{
		DatabaseURL:         harness.connectionString(priorityTemplateDatabase),
		SecretEncryptionKey: priorityTestSecretEncryptionKey,
	})
	if err != nil {
		_ = templateConn.Close(context.Background())
		return fmt.Errorf("build template startup service: %w", err)
	}
	if _, err := startupService.RunWithConn(ctx, templateConn); err != nil {
		_ = templateConn.Close(context.Background())
		return fmt.Errorf("run startup on template database %s: %w", priorityTemplateDatabase, err)
	}
	if err := templateConn.Close(ctx); err != nil {
		return fmt.Errorf("close template database %s: %w", priorityTemplateDatabase, err)
	}

	if _, err := adminConn.Exec(ctx, `ALTER DATABASE `+priorityQuoteIdentifier(priorityTemplateDatabase)+` WITH IS_TEMPLATE true`); err != nil {
		return fmt.Errorf("mark template database %s: %w", priorityTemplateDatabase, err)
	}
	if _, err := adminConn.Exec(ctx, `ALTER DATABASE `+priorityQuoteIdentifier(priorityTemplateDatabase)+` WITH ALLOW_CONNECTIONS false`); err != nil {
		return fmt.Errorf("disable connections to template database %s: %w", priorityTemplateDatabase, err)
	}
	return nil
}
