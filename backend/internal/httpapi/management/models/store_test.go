package models

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

var modelsStorePostgres struct {
	once          sync.Once
	containerName string
	hostPort      string
	err           error
}

type modelsPostgresHarness struct {
	containerName string
	hostPort      string
}

func TestModelsStorePromotionTargetPersists(t *testing.T) {
	ctx, conn := modelsMigratedConn(t, "models_promotion_target_persists")
	now := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	profileID := seedModelsStoreProfile(t, ctx, conn, now)

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin create transaction: %v", err)
	}
	target := mustInsertModelRecord(t, ctx, tx, testModelRecord(profileID, "gpt-5.4", now, nil))
	source := mustInsertModelRecord(t, ctx, tx, testModelRecord(profileID, "gpt-5.5", now, stringPtr(target.ModelID)))
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit create transaction: %v", err)
	}

	nextTime := now.Add(time.Minute)
	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin update transaction: %v", err)
	}
	nextTarget := mustInsertModelRecord(t, ctx, tx, testModelRecord(profileID, "gpt-5.6", nextTime, nil))
	source.ContextOverflowPromotionTargetID = stringPtr(nextTarget.ModelID)
	source.UpdatedAt = nextTime
	updated, err := updateModel(ctx, tx, source)
	if err != nil {
		t.Fatalf("update model promotion target: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit update transaction: %v", err)
	}

	loaded, found, err := loadModelRecord(ctx, conn, profileID, updated.ID, false)
	if err != nil {
		t.Fatalf("load updated model: %v", err)
	}
	if !found {
		t.Fatal("expected updated model to exist")
	}
	requirePromotionTargetEquals(t, loaded.ContextOverflowPromotionTargetID, nextTarget.ModelID)

	listed, err := listModelRecords(ctx, conn, profileID)
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	listedSource, ok := findModelRecordByID(listed, updated.ID)
	if !ok {
		t.Fatalf("expected source model %d in listed records", updated.ID)
	}
	requirePromotionTargetEquals(t, listedSource.ContextOverflowPromotionTargetID, nextTarget.ModelID)

	var stored sql.NullString
	if err := conn.QueryRow(ctx, `SELECT context_overflow_promotion_target_id FROM model_configs WHERE id = $1`, updated.ID).Scan(&stored); err != nil {
		t.Fatalf("query stored promotion target: %v", err)
	}
	if !stored.Valid || stored.String != nextTarget.ModelID {
		t.Fatalf("expected stored promotion target %q, got %+v", nextTarget.ModelID, stored)
	}

	detail := buildModelDetailResponse(loaded, nil, nil)
	listItem := buildModelListResponse(listedSource, nil, nil, nil, nil)
	requirePromotionTargetEquals(t, detail.ContextOverflowPromotionTargetID, nextTarget.ModelID)
	requirePromotionTargetEquals(t, listItem.ContextOverflowPromotionTargetID, nextTarget.ModelID)
}

func TestModelsStorePromotionTargetNullRoundTrip(t *testing.T) {
	ctx, conn := modelsMigratedConn(t, "models_promotion_target_null_roundtrip")
	now := time.Date(2026, time.June, 5, 18, 45, 0, 0, time.UTC)
	profileID := seedModelsStoreProfile(t, ctx, conn, now)

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin create transaction: %v", err)
	}
	target := mustInsertModelRecord(t, ctx, tx, testModelRecord(profileID, "gpt-5.4", now, nil))
	source := mustInsertModelRecord(t, ctx, tx, testModelRecord(profileID, "gpt-5.5", now, stringPtr(target.ModelID)))
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit create transaction: %v", err)
	}

	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin null update transaction: %v", err)
	}
	source.ContextOverflowPromotionTargetID = nil
	source.UpdatedAt = now.Add(time.Minute)
	updated, err := updateModel(ctx, tx, source)
	if err != nil {
		t.Fatalf("clear model promotion target: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit null update transaction: %v", err)
	}

	loaded, found, err := loadModelRecord(ctx, conn, profileID, updated.ID, false)
	if err != nil {
		t.Fatalf("load cleared model: %v", err)
	}
	if !found {
		t.Fatal("expected cleared model to exist")
	}
	if loaded.ContextOverflowPromotionTargetID != nil {
		t.Fatalf("expected cleared promotion target to stay nil, got %q", *loaded.ContextOverflowPromotionTargetID)
	}

	listed, err := listModelRecords(ctx, conn, profileID)
	if err != nil {
		t.Fatalf("list models after clear: %v", err)
	}
	listedSource, ok := findModelRecordByID(listed, updated.ID)
	if !ok {
		t.Fatalf("expected source model %d in listed records", updated.ID)
	}
	if listedSource.ContextOverflowPromotionTargetID != nil {
		t.Fatalf("expected listed promotion target to stay nil, got %q", *listedSource.ContextOverflowPromotionTargetID)
	}

	var stored sql.NullString
	if err := conn.QueryRow(ctx, `SELECT context_overflow_promotion_target_id FROM model_configs WHERE id = $1`, updated.ID).Scan(&stored); err != nil {
		t.Fatalf("query cleared promotion target: %v", err)
	}
	if stored.Valid {
		t.Fatalf("expected stored promotion target to be NULL, got %+v", stored)
	}

	detail := buildModelDetailResponse(loaded, nil, nil)
	listItem := buildModelListResponse(listedSource, nil, nil, nil, nil)
	if detail.ContextOverflowPromotionTargetID != nil || listItem.ContextOverflowPromotionTargetID != nil {
		t.Fatalf("expected nil response promotion targets, got detail=%v list=%v", detail.ContextOverflowPromotionTargetID, listItem.ContextOverflowPromotionTargetID)
	}
}

func TestModelsStoreRejectsPromotionTargetOnTerminalRows(t *testing.T) {
	ctx, conn := modelsMigratedConn(t, "models_promotion_target_terminal_rows")

	_, err := conn.Exec(ctx, `UPDATE model_access_targets SET context_overflow_promotion_target_id = $1`, "gpt-5.4")
	requireUndefinedColumnError(t, err, "context_overflow_promotion_target_id")
	assertPromotionTargetColumnAbsent(t, ctx, conn, "model_access_targets")
}

func TestModelsStoreRejectsPromotionTargetOnConnections(t *testing.T) {
	ctx, conn := modelsMigratedConn(t, "models_promotion_target_connections")

	_, err := conn.Exec(ctx, `UPDATE connections SET context_overflow_promotion_target_id = $1`, "gpt-5.4")
	requireUndefinedColumnError(t, err, "context_overflow_promotion_target_id")
	assertPromotionTargetColumnAbsent(t, ctx, conn, "connections")
}

func TestModelsStoreUsesFlatAccessTargetsWithoutObsoleteColumns(t *testing.T) {
	ctx, conn := modelsMigratedConn(t, "models_flat_access_targets")
	now := time.Date(2026, time.June, 5, 19, 0, 0, 0, time.UTC)
	profileID := seedModelsStoreProfile(t, ctx, conn, now)

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin flat access target transaction: %v", err)
	}
	target := mustInsertModelRecord(t, ctx, tx, testModelRecord(profileID, "flat-target", now, nil))
	source := mustInsertModelRecord(t, ctx, tx, testModelRecord(profileID, "flat-source", now, nil))
	if err := replaceAccessTargets(ctx, tx, profileID, source.ID, []resolvedAccessTarget{{TargetType: "model", Position: 2, IsEnabled: true, Model: &target}}, now); err != nil {
		t.Fatalf("replace flat access targets: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit flat access target transaction: %v", err)
	}

	loaded, err := loadAccessTargetsForModels(ctx, conn, profileID, []int{source.ID})
	if err != nil {
		t.Fatalf("load flat access targets: %v", err)
	}
	targets := loaded[source.ID]
	if len(targets) != 1 || targets[0].Position != 2 || !targets[0].IsEnabled || targets[0].TargetModel == nil || targets[0].TargetModel.ModelID != target.ModelID {
		t.Fatalf("unexpected flat access target records: %+v", targets)
	}
	assertAccessTargetColumnAbsent(t, ctx, conn, "weight")
	assertAccessTargetColumnAbsent(t, ctx, conn, "target_priority")
}

func mustInsertModelRecord(t *testing.T, ctx context.Context, tx pgx.Tx, record modelRecord) modelRecord {
	t.Helper()
	created, err := insertModel(ctx, tx, record)
	if err != nil {
		t.Fatalf("insert model %q: %v", record.ModelID, err)
	}
	return created
}

func testModelRecord(profileID int, modelID string, now time.Time, promotionTargetID *string) modelRecord {
	return modelRecord{
		ProfileID:   profileID,
		APIFamily:   "openai",
		ModelID:     modelID,
		DisplayName: stringPtr(strings.ToUpper(modelID)),

		DefaultOutputTokenReserve:        4096,
		MaxContextUtilization:            0.9,
		FacadeEnabled:                    false,
		ContextOverflowPromotionTargetID: promotionTargetID,
		IsEnabled:                        true,
		CreatedAt:                        now,
		UpdatedAt:                        now,
	}
}

func seedModelsStoreProfile(t *testing.T, ctx context.Context, conn *pgx.Conn, now time.Time) int {
	t.Helper()
	var profileID int
	if err := conn.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, "Default", nil, true, true, true, 1, now, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	return profileID
}

func findModelRecordByID(records []modelRecord, modelConfigID int) (modelRecord, bool) {
	for _, record := range records {
		if record.ID == modelConfigID {
			return record, true
		}
	}
	return modelRecord{}, false
}

func requirePromotionTargetEquals(t *testing.T, got *string, want string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("expected promotion target %q, got %v", want, got)
	}
}

func requireUndefinedColumnError(t *testing.T, err error, columnName string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected undefined column error for %s", columnName)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected pg error for %s, got %T: %v", columnName, err, err)
	}
	if pgErr.Code != "42703" || !strings.Contains(err.Error(), columnName) {
		t.Fatalf("expected undefined column error for %s, got %v", columnName, err)
	}
}

func assertPromotionTargetColumnAbsent(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string) {
	t.Helper()
	var count int
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = 'context_overflow_promotion_target_id'`, tableName).Scan(&count); err != nil {
		t.Fatalf("query %s columns: %v", tableName, err)
	}
	if count != 0 {
		t.Fatalf("expected %s to reject promotion target ownership, but column exists", tableName)
	}
}

func assertAccessTargetColumnAbsent(t *testing.T, ctx context.Context, conn *pgx.Conn, columnName string) {
	t.Helper()
	var count int
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'model_access_targets' AND column_name = $1`, columnName).Scan(&count); err != nil {
		t.Fatalf("query model_access_targets column %s: %v", columnName, err)
	}
	if count != 0 {
		t.Fatalf("expected model_access_targets.%s to be absent", columnName)
	}
}

func modelsMigratedConn(t *testing.T, name string) (context.Context, *pgx.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	harness := modelsStoreHarness(t)
	databaseName := fmt.Sprintf("%s_%s", name, modelsRandomSuffix(t))
	conn := harness.openDatabase(t, ctx, databaseName)

	runner, err := migrate.New(migrate.Options{})
	if err != nil {
		t.Fatalf("build migration runner: %v", err)
	}
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations for %s: %v", databaseName, err)
	}
	t.Cleanup(func() {
		_ = conn.Close(ctx)
	})
	return ctx, conn
}

func modelsStoreHarness(t *testing.T) modelsPostgresHarness {
	t.Helper()
	modelsStorePostgres.once.Do(func() {
		containerName := "prism-models-" + modelsRandomSuffix(t)
		if _, err := runModelsDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
			modelsStorePostgres.err = err
			return
		}
		modelsStorePostgres.containerName = containerName
		hostPort, err := modelsDockerPort(containerName)
		if err != nil {
			modelsStorePostgres.err = err
			return
		}
		if err := waitForModelsPostgres(hostPort); err != nil {
			modelsStorePostgres.err = err
			return
		}
		modelsStorePostgres.hostPort = hostPort
	})

	if modelsStorePostgres.err != nil {
		t.Fatalf("start postgres harness: %v", modelsStorePostgres.err)
	}
	return modelsPostgresHarness{containerName: modelsStorePostgres.containerName, hostPort: modelsStorePostgres.hostPort}
}

func (h modelsPostgresHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := modelsConnect(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+modelsQuoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+modelsQuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return modelsConnect(t, ctx, h.connectionString(databaseName))
}

func (h modelsPostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func modelsConnect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres %s: %v", dsn, err)
	}
	return conn
}

func modelsDockerPort(containerName string) (string, error) {
	output, err := runModelsDockerCommand(context.Background(), "port", containerName, "5432/tcp")
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

func waitForModelsPostgres(hostPort string) error {
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

func runModelsDockerCommand(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)

	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func modelsQuoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func modelsRandomSuffix(t *testing.T) string {
	t.Helper()
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}
	return hex.EncodeToString(buffer)
}
