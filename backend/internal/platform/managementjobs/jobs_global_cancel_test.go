package managementjobs

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

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

var managementJobsPostgres struct {
	once          sync.Once
	containerName string
	hostPort      string
	err           error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if managementJobsPostgres.containerName != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = exec.CommandContext(ctx, "docker", "rm", "-f", managementJobsPostgres.containerName).Run()
		cancel()
	}
	os.Exit(code)
}

func TestGlobalLogRetentionRunningCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := managementJobsMigratedPool(t, ctx, "global_running_cancel")
	defer pool.Close()
	now := time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC)
	store := NewStore(Options{Pool: pool, Now: func() time.Time { return now }})
	cutoff := now.Add(-24 * time.Hour)
	job, err := store.CreateLogRetentionJob(ctx, CreateLogRetentionJobRequest{RequestedBy: "global", Reason: "running global cancel", Scope: LogRetentionScope{Table: "request_logs", Cutoff: &cutoff}})
	if err != nil {
		t.Fatalf("create global log retention job: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE management_jobs SET state = 'running', locked_by = 'test-worker', locked_until = now() + interval '5 minutes', last_heartbeat_at = now(), updated_at = now() WHERE id = $1`, job.ID); err != nil {
		t.Fatalf("mark global job running: %v", err)
	}
	cancelled, err := store.CancelGlobalLogRetentionJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("cancel running global job: %v", err)
	}
	if cancelled.ProfileID != 0 || cancelled.Type != TypeLogRetention || cancelled.State != "cancel_requested" || !cancelled.CancelRequested || cancelled.FinishedAt != nil {
		t.Fatalf("expected running global log retention job to request cancellation, got %+v", cancelled)
	}
	var cancelEvents int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM management_job_events WHERE job_id = $1 AND event_type = 'cancel_requested'`, job.ID).Scan(&cancelEvents); err != nil {
		t.Fatalf("count cancel events: %v", err)
	}
	if cancelEvents != 1 {
		t.Fatalf("expected one cancel event, got %d", cancelEvents)
	}
}

func managementJobsMigratedPool(t *testing.T, ctx context.Context, name string) *pgxpool.Pool {
	t.Helper()
	harness := managementJobsPostgresHarness(t)
	databaseName := fmt.Sprintf("management_jobs_%s_%s", name, managementJobsRandomSuffix(t))
	conn := harness.openDatabase(t, ctx, databaseName)
	runner, err := migrate.New(migrate.Options{})
	if err != nil {
		t.Fatalf("build migration runner: %v", err)
	}
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations for %s: %v", databaseName, err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close migration connection: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open management jobs pool: %v", err)
	}
	return pool
}

type managementJobsPostgresTestHarness struct {
	containerName string
	hostPort      string
}

func managementJobsPostgresHarness(t *testing.T) managementJobsPostgresTestHarness {
	t.Helper()
	managementJobsPostgres.once.Do(func() {
		containerName := "prism-management-jobs-" + managementJobsRandomSuffix(t)
		if _, err := runManagementJobsDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
			managementJobsPostgres.err = err
			return
		}
		managementJobsPostgres.containerName = containerName
		hostPort, err := managementJobsDockerPort(containerName)
		if err != nil {
			managementJobsPostgres.err = err
			return
		}
		if err := waitForManagementJobsPostgres(hostPort); err != nil {
			managementJobsPostgres.err = err
			return
		}
		managementJobsPostgres.hostPort = hostPort
	})
	if managementJobsPostgres.err != nil {
		t.Fatalf("start postgres harness: %v", managementJobsPostgres.err)
	}
	return managementJobsPostgresTestHarness{containerName: managementJobsPostgres.containerName, hostPort: managementJobsPostgres.hostPort}
}

func (h managementJobsPostgresTestHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := managementJobsConnect(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+managementJobsQuoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+managementJobsQuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return managementJobsConnect(t, ctx, h.connectionString(databaseName))
}

func (h managementJobsPostgresTestHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func managementJobsConnect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres %s: %v", dsn, err)
	}
	return conn
}

func managementJobsDockerPort(containerName string) (string, error) {
	output, err := runManagementJobsDockerCommand(context.Background(), "port", containerName, "5432/tcp")
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

func waitForManagementJobsPostgres(hostPort string) error {
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

func runManagementJobsDockerCommand(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func managementJobsQuoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func managementJobsRandomSuffix(t *testing.T) string {
	t.Helper()
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}
	return hex.EncodeToString(buffer)
}
