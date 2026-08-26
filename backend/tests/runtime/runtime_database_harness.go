package runtimetest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

var sharedPostgresHarness testPostgresHarness

type testPostgresHarness struct {
	containerName string
	hostPort      string
}

type runtimeHarness struct {
	databaseName   string
	client         *http.Client
	conn           *pgx.Conn
	authService    *managementauth.Service
	runtimeService *runtimeapi.Service
	runtimeCache   *runtimeapi.SharedCache
	server         *httptest.Server
	url            string
	upstream       *upstreamRecorder

	snapshotRefreshSuspend int
}

func (h testPostgresHarness) openDatabase(tb testing.TB, ctx context.Context, databaseName string) *pgx.Conn {
	tb.Helper()
	adminConn := connectDatabase(tb, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		tb.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+quoteIdentifier(databaseName)); err != nil {
		tb.Fatalf("create database %s: %v", databaseName, err)
	}
	return connectDatabase(tb, ctx, h.connectionString(databaseName))
}

func (h testPostgresHarness) dropDatabase(databaseName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adminConn, err := pgx.Connect(ctx, h.connectionString("postgres"))
	if err != nil {
		return
	}
	defer func() { _ = adminConn.Close(context.Background()) }()
	_, _ = adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdentifier(databaseName)+` WITH (FORCE)`)
}

func (h testPostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func newRuntimeHarness(tb testing.TB) *runtimeHarness {
	tb.Helper()
	return newRuntimeHarnessWithConfig(tb, runtimeHarnessConfig{})
}

func newEnforcedRuntimeHarness(tb testing.TB) *runtimeHarness {
	tb.Helper()
	return newRuntimeHarnessWithConfig(tb, runtimeHarnessConfig{})
}

type runtimeHarnessConfig struct {
	RuntimeOptions  runtimeapi.Options
	SettingsMutator func(*config.Settings)
}

func newRuntimeHarnessWithConfig(tb testing.TB, config runtimeHarnessConfig) *runtimeHarness {
	tb.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	databaseName := "runtime_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(tb, testContext, databaseName)
	tb.Cleanup(func() {
		sharedPostgresHarness.dropDatabase(databaseName)
	})
	tb.Cleanup(func() {
		_ = conn.Close(context.Background())
	})
	return newRuntimeHarnessForDatabaseWithConfig(tb, databaseName, conn, config)
}

func restartRuntimeHarness(t *testing.T, databaseName string) *runtimeHarness {
	t.Helper()
	return restartRuntimeHarnessWithConfig(t, databaseName, runtimeHarnessConfig{})
}

func restartRuntimeHarnessWithConfig(t *testing.T, databaseName string, config runtimeHarnessConfig) *runtimeHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := connectDatabase(t, testContext, sharedPostgresHarness.connectionString(databaseName))
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
	})
	return newRuntimeHarnessForDatabaseWithConfig(t, databaseName, conn, config)
}
