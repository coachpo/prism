package contracttest

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
)

var sharedPostgresHarness testPostgresHarness

type testPostgresHarness struct {
	containerName string
	hostPort      string
}

func startSharedPostgresHarness() (testPostgresHarness, error) {
	// An externally provisioned PostgreSQL lets the suite run without shelling
	// out to Docker at all, which is what closed-loop verification needs to be
	// able to certify that the case left no stray processes behind. It also lets
	// CI attach a service container instead of running docker-in-docker. The
	// credentials stay the harness defaults (prism/prism).
	if externalPort := strings.TrimSpace(os.Getenv("PRISM_TEST_POSTGRES_PORT")); externalPort != "" {
		if err := waitForPostgres(externalPort); err != nil {
			return testPostgresHarness{}, err
		}
		return testPostgresHarness{hostPort: externalPort}, nil
	}
	containerName := testDockerContainerName("postgres")
	if err := runDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
		return testPostgresHarness{}, err
	}
	hostPort, err := dockerPort(containerName)
	if err != nil {
		return testPostgresHarness{}, err
	}
	if err := waitForPostgres(hostPort); err != nil {
		return testPostgresHarness{}, err
	}
	return testPostgresHarness{containerName: containerName, hostPort: hostPort}, nil
}

// testDockerContainerPrefix returns the branch-scoped prefix used for shared
// Docker resources. On shared machines, tests from different worktrees/branches
// must not collide on container names, so the current git branch (sanitized for
// Docker name rules) is embedded in every container name. An explicit
// PRISM_TEST_DOCKER_PREFIX environment variable overrides the branch.
func testDockerContainerPrefix() string {
	if explicit := strings.TrimSpace(os.Getenv("PRISM_TEST_DOCKER_PREFIX")); explicit != "" {
		return sanitizeDockerNameComponent(explicit)
	}
	command := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	output, err := command.Output()
	if err != nil {
		return "unknown"
	}
	branch := strings.TrimSpace(string(output))
	return sanitizeDockerNameComponent(branch)
}

func sanitizeDockerNameComponent(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-_")
}

// testDockerContainerName builds a Docker container name scoped by the
// branch prefix (or PRISM_TEST_DOCKER_PREFIX) plus a random suffix.
func testDockerContainerName(role string) string {
	return fmt.Sprintf("prism-%s-%s-%s", sanitizeDockerNameComponent(role), testDockerContainerPrefix(), randomSuffix())
}

func (h testPostgresHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := connectDatabase(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+quoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return connectDatabase(t, ctx, h.connectionString(databaseName))
}

func (h testPostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func connectDatabase(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect database %s: %v", dsn, err)
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

// branchContainerPrefix returns a sanitized current-git-branch label used as
// a container-name prefix so shared Colima instances can attribute and clean
// up harness containers per branch. Falls back to "unknown" when git is not
// available (e.g. CI tarball checkouts).
func branchContainerPrefix() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	branch := strings.TrimSpace(string(output))
	if branch == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, char := range strings.ToLower(branch) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			builder.WriteRune(char)
		} else {
			builder.WriteByte('-')
		}
	}
	label := builder.String()
	if len(label) > 32 {
		label = label[:32]
	}
	return strings.Trim(label, "-.")
}
