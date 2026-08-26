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
)

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
