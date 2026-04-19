package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

func TestCutoverBaselineStamp(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := openCutoverClone(t, testContext, harness, "cutover_baseline_stamp")
	defer conn.Close(testContext)

	before := snapshotApplicationSchema(t, testContext, runner, conn)

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("stamp cutover clone: %v", err)
	}

	assertStampedBaseline(t, result)

	after := snapshotApplicationSchema(t, testContext, runner, conn)
	if before != after {
		t.Fatalf("expected stamp path to leave application schema unchanged")
	}

	assertCutoverMatch(t, testContext, runner, conn)
	assertHistoryVersions(t, testContext, conn, []string{migrate.DefaultBaselineVersion})
	assertLegacyAlembicVersion(t, testContext, conn, legacyAlembicTip)
}

func TestCutoverSchemaDrift(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "cutover_schema_drift"
	conn := openCutoverClone(t, testContext, harness, databaseName)
	defer conn.Close(testContext)

	startupService := newCutoverStartupService(t, harness.connectionString(databaseName))
	result, err := startupService.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("run startup on cutover clone: %v", err)
	}

	assertStampedBaseline(t, result.Migration)
	assertCutoverMatch(t, testContext, runner, conn)
	assertHistoryVersions(t, testContext, conn, []string{migrate.DefaultBaselineVersion})
	assertLegacyAlembicVersion(t, testContext, conn, legacyAlembicTip)
}

func TestCutoverRehearsal(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "cutover_rehearsal"
	conn := openCutoverClone(t, testContext, harness, databaseName)
	defer conn.Close(testContext)

	before := snapshotApplicationSchema(t, testContext, runner, conn)
	startupService := newCutoverStartupService(t, harness.connectionString(databaseName))

	firstRun, err := startupService.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("run initial cutover rehearsal: %v", err)
	}
	assertStampedBaseline(t, firstRun.Migration)

	afterFirstRun := snapshotApplicationSchema(t, testContext, runner, conn)
	if before != afterFirstRun {
		t.Fatalf("expected startup on stamped clone to leave application schema unchanged")
	}

	secondRun, err := startupService.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("rerun cutover rehearsal: %v", err)
	}
	if secondRun.Migration.Outcome != migrate.OutcomeNoop {
		t.Fatalf("expected second cutover rehearsal migration outcome %q, got %q", migrate.OutcomeNoop, secondRun.Migration.Outcome)
	}
	if len(secondRun.Migration.Versions) != 0 {
		t.Fatalf("expected second cutover rehearsal to report no migration versions, got %v", secondRun.Migration.Versions)
	}

	afterSecondRun := snapshotApplicationSchema(t, testContext, runner, conn)
	if afterFirstRun != afterSecondRun {
		t.Fatalf("expected repeated cutover rehearsal to keep application schema unchanged")
	}

	assertCutoverMatch(t, testContext, runner, conn)
	assertHistoryVersions(t, testContext, conn, []string{migrate.DefaultBaselineVersion})
	assertLegacyAlembicVersion(t, testContext, conn, legacyAlembicTip)
}

func openCutoverClone(t *testing.T, ctx context.Context, harness postgresHarness, databaseName string) *pgx.Conn {
	t.Helper()

	conn := harness.openDatabase(t, ctx, databaseName)
	applySQLFile(t, ctx, conn, cutoverSchemaPath(t))
	seedLegacyAlembicVersion(t, ctx, conn)

	return conn
}

func newCutoverStartupService(t *testing.T, databaseURL string) startup.Service {
	t.Helper()

	service, err := startup.New(startup.Options{
		DatabaseURL:         databaseURL,
		SecretEncryptionKey: "cutover-secret",
	})
	if err != nil {
		t.Fatalf("build cutover startup service: %v", err)
	}

	return service
}

func assertStampedBaseline(t *testing.T, result migrate.Result) {
	t.Helper()

	if result.Outcome != migrate.OutcomeStamp {
		t.Fatalf("expected cutover clone to stamp baseline, got %q", result.Outcome)
	}
	if got, want := result.Versions, []string{migrate.DefaultBaselineVersion}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("expected stamped versions %v, got %v", want, got)
	}
}
