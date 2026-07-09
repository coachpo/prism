package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

const integrationTemplateDatabase = "template1_prism"

var sharedIntegrationPostgresHarness postgresHarness

type postgresHarness struct {
	containerName string
	hostPort      string
}

func TestMain(m *testing.M) {
	harness, err := startSharedPostgresHarness()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := prepareTemplateDatabase(harness); err != nil {
		fmt.Fprintln(os.Stderr, err)
		cleanupSharedPostgresHarness(harness)
		os.Exit(1)
	}
	sharedIntegrationPostgresHarness = harness

	code := m.Run()
	cleanupSharedPostgresHarness(harness)
	os.Exit(code)
}

func newPostgresHarness(t *testing.T) postgresHarness {
	t.Helper()
	if sharedIntegrationPostgresHarness.containerName == "" {
		t.Fatal("shared integration postgres harness not initialized")
	}
	return sharedIntegrationPostgresHarness
}

func (h postgresHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	return h.openTemplateDatabase(t, ctx, databaseName)
}

func (h postgresHarness) openTemplateDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	return h.openDatabaseFromTemplate(t, ctx, databaseName, integrationTemplateDatabase)
}

func (h postgresHarness) openEmptyDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	return h.openDatabaseFromTemplate(t, ctx, databaseName, "")
}

func (h postgresHarness) openDatabaseFromTemplate(t *testing.T, ctx context.Context, databaseName string, templateName string) *pgx.Conn {
	t.Helper()
	adminConn := connect(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()

	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	createStatement := `CREATE DATABASE ` + quoteIdentifier(databaseName)
	if templateName != "" {
		createStatement += ` TEMPLATE ` + quoteIdentifier(templateName)
	}
	if _, err := adminConn.Exec(ctx, createStatement); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}

	return connect(t, ctx, h.connectionString(databaseName))
}

func (h postgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func connect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()

	conn, err := connectDatabase(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres %s: %v", dsn, err)
	}

	return conn
}

func connectDatabase(ctx context.Context, dsn string) (*pgx.Conn, error) {
	return pgx.Connect(ctx, dsn)
}

func dockerPort(t *testing.T, containerName string) string {
	t.Helper()

	output, err := dockerPortForContainer(containerName)
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func dockerPortForContainer(containerName string) (string, error) {
	output, err := runDockerCommand(context.Background(), "port", containerName, "5432/tcp")
	if err != nil {
		return "", err
	}
	firstLine := strings.TrimSpace(strings.Split(output, "\n")[0])
	_, port, err := net.SplitHostPort(firstLine)
	if err != nil {
		return "", fmt.Errorf("parse docker port output %q: %w", firstLine, err)
	}
	return port, nil
}

func waitForPostgres(t *testing.T, hostPort string) {
	t.Helper()
	if err := waitForPostgresPort(hostPort); err != nil {
		t.Fatal(err)
	}
}

func waitForPostgresPort(hostPort string) error {
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, err := connectDatabase(ctx, fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/postgres?sslmode=disable", hostPort))
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

func runDockerCommand(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func runDockerCommandOrFail(t *testing.T, ctx context.Context, args ...string) string {
	t.Helper()
	output, err := runDockerCommand(ctx, args...)
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func randomSuffix(t *testing.T) string {
	t.Helper()

	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}

	return hex.EncodeToString(buffer)
}

func startSharedPostgresHarness() (postgresHarness, error) {
	containerName := "prism-integration-" + randomSuffixString()
	if _, err := runDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
		return postgresHarness{}, err
	}
	hostPort, err := dockerPortForContainer(containerName)
	if err != nil {
		return postgresHarness{}, err
	}
	if err := waitForPostgresPort(hostPort); err != nil {
		return postgresHarness{}, err
	}
	return postgresHarness{containerName: containerName, hostPort: hostPort}, nil
}

func prepareTemplateDatabase(h postgresHarness) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adminConn, err := connectDatabase(ctx, h.connectionString("postgres"))
	if err != nil {
		return fmt.Errorf("connect to postgres admin database: %w", err)
	}
	defer func() { _ = adminConn.Close(context.Background()) }()

	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdentifier(integrationTemplateDatabase)+` WITH (FORCE)`); err != nil {
		return fmt.Errorf("drop template database %s: %w", integrationTemplateDatabase, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+quoteIdentifier(integrationTemplateDatabase)); err != nil {
		return fmt.Errorf("create template database %s: %w", integrationTemplateDatabase, err)
	}

	templateConn, err := connectDatabase(ctx, h.connectionString(integrationTemplateDatabase))
	if err != nil {
		return fmt.Errorf("connect to template database %s: %w", integrationTemplateDatabase, err)
	}
	runner, err := migrate.New(migrate.Options{})
	if err != nil {
		_ = templateConn.Close(context.Background())
		return fmt.Errorf("build migration runner: %w", err)
	}
	if _, err := runner.Run(ctx, templateConn); err != nil {
		_ = templateConn.Close(context.Background())
		return fmt.Errorf("migrate template database %s: %w", integrationTemplateDatabase, err)
	}
	if err := templateConn.Close(ctx); err != nil {
		return fmt.Errorf("close template database %s: %w", integrationTemplateDatabase, err)
	}
	if _, err := adminConn.Exec(ctx, `ALTER DATABASE `+quoteIdentifier(integrationTemplateDatabase)+` WITH IS_TEMPLATE true`); err != nil {
		return fmt.Errorf("mark template database %s as template: %w", integrationTemplateDatabase, err)
	}
	if _, err := adminConn.Exec(ctx, `ALTER DATABASE `+quoteIdentifier(integrationTemplateDatabase)+` WITH ALLOW_CONNECTIONS false`); err != nil {
		return fmt.Errorf("disable direct connections to template database %s: %w", integrationTemplateDatabase, err)
	}
	return nil
}

func cleanupSharedPostgresHarness(h postgresHarness) {
	if h.containerName == "" {
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = runDockerCommand(cleanupContext, "rm", "-f", h.containerName)
}

func randomSuffixString() string {
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(buffer)
}
