package runtimetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/startup"
)

// runtimeTemplateDatabase holds one migrated and startup-seeded schema so each
// harness clones it instead of replaying every migration.
const runtimeTemplateDatabase = "prism_runtime_template"

func connectDatabase(tb testing.TB, ctx context.Context, dsn string) *pgx.Conn {
	tb.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		tb.Fatalf("connect database %s: %v", dsn, err)
	}
	return conn
}

func runDockerCommand(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func dockerPort(containerName string) (string, error) {
	command := exec.Command("docker", "port", containerName, "5432/tcp")
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker port %s failed: %v\n%s", containerName, err, strings.TrimSpace(string(output)))
	}
	firstLine := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	_, port, splitErr := net.SplitHostPort(firstLine)
	if splitErr != nil {
		return "", fmt.Errorf("parse docker port output %q: %w", firstLine, splitErr)
	}
	return port, nil
}

func waitForPostgres(hostPort string) error {
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

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func randomSuffix() string {
	buffer := make([]byte, 4)
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}

func prepareRuntimeTemplateDatabase(harness testPostgresHarness) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	adminConn, err := pgx.Connect(ctx, harness.connectionString("postgres"))
	if err != nil {
		return fmt.Errorf("connect postgres admin database: %w", err)
	}
	defer func() { _ = adminConn.Close(context.Background()) }()

	// A reused external PostgreSQL carries the template flag over from the
	// previous run, and PostgreSQL refuses to drop a flagged database.
	if _, err := adminConn.Exec(ctx, `UPDATE pg_database SET datistemplate = FALSE WHERE datname = $1`, runtimeTemplateDatabase); err != nil {
		return fmt.Errorf("clear template flag on %s: %w", runtimeTemplateDatabase, err)
	}
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdentifier(runtimeTemplateDatabase)+` WITH (FORCE)`); err != nil {
		return fmt.Errorf("drop template database %s: %w", runtimeTemplateDatabase, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+quoteIdentifier(runtimeTemplateDatabase)); err != nil {
		return fmt.Errorf("create template database %s: %w", runtimeTemplateDatabase, err)
	}

	templateConn, err := pgx.Connect(ctx, harness.connectionString(runtimeTemplateDatabase))
	if err != nil {
		return fmt.Errorf("connect template database %s: %w", runtimeTemplateDatabase, err)
	}
	startupService, err := startup.New(startup.Options{
		DatabaseURL:         harness.connectionString(runtimeTemplateDatabase),
		SecretEncryptionKey: runtimeHarnessSecretEncryptionKey,
	})
	if err != nil {
		_ = templateConn.Close(context.Background())
		return fmt.Errorf("build template startup service: %w", err)
	}
	if _, err := startupService.RunWithConn(ctx, templateConn); err != nil {
		_ = templateConn.Close(context.Background())
		return fmt.Errorf("run startup on template database %s: %w", runtimeTemplateDatabase, err)
	}
	if err := templateConn.Close(ctx); err != nil {
		return fmt.Errorf("close template database %s: %w", runtimeTemplateDatabase, err)
	}

	if _, err := adminConn.Exec(ctx, `ALTER DATABASE `+quoteIdentifier(runtimeTemplateDatabase)+` WITH IS_TEMPLATE true`); err != nil {
		return fmt.Errorf("mark template database %s: %w", runtimeTemplateDatabase, err)
	}
	if _, err := adminConn.Exec(ctx, `ALTER DATABASE `+quoteIdentifier(runtimeTemplateDatabase)+` WITH ALLOW_CONNECTIONS false`); err != nil {
		return fmt.Errorf("disable connections to template database %s: %w", runtimeTemplateDatabase, err)
	}
	return nil
}
