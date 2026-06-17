package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/lifecycle"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
	"github.com/coachpo/prism/backend/internal/platform/migrate"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

func TestStartupCreatesLogPartitions(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	databaseName := "startup_creates_log_partitions"
	conn := harness.openDatabase(t, testContext, databaseName)
	defer func() { _ = conn.Close(testContext) }()

	service := newStartupService(t, harness.connectionString(databaseName), nil)
	if _, err := service.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup sequence before production lifecycle: %v", err)
	}

	app, _, err := lifecycle.NewProductionApp(testContext, productionLifecycleSettings(harness.connectionString(databaseName)), lifecycle.ProductionOptions{})
	if err != nil {
		t.Fatalf("build production app with log partition bootstrap: %v", err)
	}
	defer func() {
		if err := app.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown production app: %v", err)
		}
	}()

	assertStartupLogPartitionHorizon(t, testContext, conn)
}

func TestStartupFailsWhenLogPartitionBootstrapFails(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	databaseName := "startup_fails_log_partition_bootstrap"
	conn := harness.openDatabase(t, testContext, databaseName)
	defer func() { _ = conn.Close(testContext) }()

	service := newStartupService(t, harness.connectionString(databaseName), nil)
	if _, err := service.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup sequence before bootstrap failure setup: %v", err)
	}
	if _, err := conn.Exec(testContext, `ALTER TABLE request_logs RENAME TO request_logs_bootstrap_broken`); err != nil {
		t.Fatalf("break request_logs partition root: %v", err)
	}

	app, server, err := lifecycle.NewProductionApp(testContext, productionLifecycleSettings(harness.connectionString(databaseName)), lifecycle.ProductionOptions{})
	if err == nil {
		if app != nil {
			_ = app.Shutdown(context.Background())
		}
		t.Fatalf("expected production app startup to fail, got app=%v server=%v", app, server)
	}
	if !strings.Contains(err.Error(), "bootstrap log partition horizon") || !strings.Contains(err.Error(), "request_logs") {
		t.Fatalf("expected contextual log partition bootstrap error, got %v", err)
	}
}

func TestStartupSeeds(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, testContext, "startup_seeds")
	defer func() { _ = conn.Close(testContext) }()

	service := newStartupService(t, harness.connectionString("startup_seeds"), nil)
	result, err := service.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("run startup sequence on empty database: %v", err)
	}

	assertStartupStepOrder(t, result)
	if result.Migration.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected startup to apply baseline on empty database, got %q", result.Migration.Outcome)
	}

	profile := loadSingleProfile(t, testContext, conn, "name = 'Default'")
	if profile.Description != DefaultProfileDescription {
		t.Fatalf("expected default profile description %q, got %q", DefaultProfileDescription, profile.Description)
	}
	if !profile.IsActive || !profile.IsDefault || !profile.IsEditable {
		t.Fatalf("expected seeded default profile to be active/default/editable, got %+v", profile)
	}
	if profile.Version != 1 {
		t.Fatalf("expected first-boot default profile version 1, got %d", profile.Version)
	}
	if profile.DeletedAt.Valid {
		t.Fatalf("expected seeded default profile to be non-deleted")
	}

	var userSettingsProfileID int
	var reportCurrencyCode string
	var reportCurrencySymbol string
	var timezonePreference sql.NullString
	if err := conn.QueryRow(
		testContext,
		`SELECT profile_id, report_currency_code, report_currency_symbol, timezone_preference FROM user_settings ORDER BY profile_id ASC LIMIT 1`,
	).Scan(&userSettingsProfileID, &reportCurrencyCode, &reportCurrencySymbol, &timezonePreference); err != nil {
		t.Fatalf("load seeded user_settings row: %v", err)
	}
	if userSettingsProfileID != profile.ID {
		t.Fatalf("expected seeded user_settings row for default profile id %d, got %d", profile.ID, userSettingsProfileID)
	}
	if reportCurrencyCode != "USD" {
		t.Fatalf("expected USD report currency code, got %q", reportCurrencyCode)
	}
	if reportCurrencySymbol != "$" {
		t.Fatalf("expected $ report currency symbol, got %q", reportCurrencySymbol)
	}
	if timezonePreference.Valid {
		t.Fatalf("expected nil timezone_preference on seeded user_settings row")
	}

	var appAuthCount int
	if err := conn.QueryRow(testContext, `SELECT COUNT(*) FROM app_auth_settings WHERE singleton_key = 'app' AND auth_enabled = FALSE`).Scan(&appAuthCount); err != nil {
		t.Fatalf("count seeded app auth settings: %v", err)
	}
	if appAuthCount != 1 {
		t.Fatalf("expected exactly one seeded app auth settings row, got %d", appAuthCount)
	}

	assertCount(t, testContext, conn, `SELECT COUNT(*) FROM user_agent_client_rules WHERE is_system = TRUE`, len(startup.SystemUserAgentClientRuleDefaults))
	assertCount(t, testContext, conn, `SELECT COUNT(*) FROM header_blocklist_rules WHERE is_system = TRUE`, len(startup.SystemHeaderBlocklistDefaults))
	assertCount(t, testContext, conn, `SELECT COUNT(*) FROM loadbalance_strategies`, 0)
}

func TestStartupSeedsMissingBootstrapOnly(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bootstrap.json")
	firstDatabaseURL := "postgres://startup-missing-first@db.invalid:5432/prism?sslmode=disable"
	secondDatabaseURL := "postgres://startup-missing-second@db.invalid:5432/prism?sslmode=disable"

	firstOutput := runBackendPrintEffectiveStartupSettings(t, configPath, firstDatabaseURL)
	assertStartupPrintLine(t, firstOutput, config.BootstrapConfigPathEnv+"="+configPath)
	assertStartupPrintLine(t, firstOutput, "DATABASE_URL="+firstDatabaseURL)
	seededState := readStartupBootstrapFileState(t, configPath)

	secondOutput := runBackendPrintEffectiveStartupSettings(t, configPath, secondDatabaseURL)
	assertStartupPrintLine(t, secondOutput, "DATABASE_URL="+firstDatabaseURL)
	assertStartupPrintDoesNotContain(t, secondOutput, secondDatabaseURL)
	assertStartupBootstrapFileStatePreserved(t, configPath, seededState)
}

func TestStartupPreservesExistingBootstrap(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bootstrap.json")
	fileDatabaseURL := "postgres://startup-preserve-file@db.invalid:5432/prism?sslmode=disable"
	envDatabaseURL := "postgres://startup-preserve-env@db.invalid:5432/prism?sslmode=disable"

	runBackendPrintEffectiveStartupSettings(t, configPath, fileDatabaseURL)
	mutateStartupBootstrapJSON(t, configPath, func(payload map[string]any) {
		startupBootstrapObject(t, payload, "server")["port"] = 18000
		runtimePayload := startupBootstrapObject(t, payload, "runtime")
		startupBootstrapObject(t, runtimePayload, "transport")["requestTimeout"] = "60s"
	})
	before := setStartupBootstrapFileModTime(t, configPath, time.Date(2026, 5, 25, 13, 0, 0, 0, time.UTC))

	output := runBackendPrintEffectiveStartupSettings(t, configPath, envDatabaseURL)
	assertStartupPrintLine(t, output, "DATABASE_URL="+fileDatabaseURL)
	assertStartupPrintLine(t, output, "SERVER_PORT=18000")
	assertStartupPrintDoesNotContain(t, output, envDatabaseURL)
	assertStartupBootstrapFileStatePreserved(t, configPath, before)
}

func TestBackendStartupWithStartupTelemetryConfig(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	databaseName := "backend_startup_telemetry_config"
	conn := harness.openDatabase(t, testContext, databaseName)
	defer func() { _ = conn.Close(testContext) }()

	configPath := filepath.Join(t.TempDir(), "bootstrap.json")
	backendPort := reserveLocalTCPPort(t)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer collector.Close()

	t.Setenv("DATABASE_URL", harness.connectionString(databaseName))
	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})
	if _, err := manager.LoadOrSeed(configPath); err != nil {
		t.Fatalf("seed startup telemetry bootstrap config: %v", err)
	}
	mutateStartupBootstrapJSON(t, configPath, func(payload map[string]any) {
		server := startupBootstrapObject(t, payload, "server")
		server["host"] = "127.0.0.1"
		server["port"] = backendPort
		payload["telemetry"] = startupTelemetryBootstrapPayload(collector.URL)
	})

	binaryPath := buildBackendBinary(t, testContext)
	output := runBackendUntilHealthyThenInterrupt(t, testContext, binaryPath, configPath, backendPort)
	if !strings.Contains(output, "starting prism backend") {
		t.Fatalf("expected backend startup log, got:\n%s", output)
	}
}

func TestStartupIgnoresLegacySkipEnv(t *testing.T) {
	t.Setenv("PRISM_SKIP_STARTUP_SEQUENCE", "1")

	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, testContext, "startup_ignores_legacy_skip_env")
	defer func() { _ = conn.Close(testContext) }()

	service := newStartupService(t, harness.connectionString("startup_ignores_legacy_skip_env"), nil)
	result, err := service.Run(testContext)
	if err != nil {
		t.Fatalf("run startup sequence with legacy skip env set: %v", err)
	}
	if result.Skipped {
		t.Fatal("expected legacy PRISM_SKIP_STARTUP_SEQUENCE env to be ignored")
	}
	assertStartupStepOrder(t, result)
	if result.Migration.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected startup to apply baseline with legacy skip env set, got %q", result.Migration.Outcome)
	}

	profile := loadSingleProfile(t, testContext, conn, "name = 'Default'")
	if profile.Description != DefaultProfileDescription {
		t.Fatalf("expected default profile description %q after startup run, got %q", DefaultProfileDescription, profile.Description)
	}
}

func TestStartupRuleSeeds(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, testContext, "startup_rule_seeds")
	defer func() { _ = conn.Close(testContext) }()

	runner := newRunner(t)
	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("apply baseline before startup rule test: %v", err)
	}

	now := time.Date(2026, 4, 18, 11, 30, 0, 0, time.UTC)
	insertProfile(t, testContext, conn, profileSeed{
		Name:        "Seed Profile",
		Description: "existing profile",
		IsActive:    true,
		IsDefault:   false,
		IsEditable:  true,
		Version:     0,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	insertSystemUserAgentRule(t, testContext, conn, systemUserAgentRuleSeed{
		Name:      "Claude Code",
		Pattern:   "claude(?:\\s|-)?code",
		Enabled:   false,
		CreatedAt: now,
		UpdatedAt: now,
	})
	insertSystemHeaderBlocklistRule(t, testContext, conn, systemHeaderBlocklistRuleSeed{
		Name:      "Custom Via Header",
		MatchType: "exact",
		Pattern:   "via",
		Enabled:   false,
		CreatedAt: now,
		UpdatedAt: now,
	})

	service := newStartupService(t, harness.connectionString("startup_rule_seeds"), nil)
	result, err := service.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("run startup sequence for rule canonicalization: %v", err)
	}

	assertStartupStepOrder(t, result)
	if result.Migration.Outcome != migrate.OutcomeNoop {
		t.Fatalf("expected startup migration step to noop after baseline apply, got %q", result.Migration.Outcome)
	}

	var claudeCount int
	var claudePattern string
	var claudeEnabled bool
	var enabledInt int
	if err := conn.QueryRow(
		testContext,
		`SELECT COUNT(*), MIN(pattern), MIN(enabled::int)::int FROM user_agent_client_rules WHERE is_system = TRUE AND name = 'Claude Code'`,
	).Scan(&claudeCount, &claudePattern, &enabledInt); err != nil {
		t.Fatalf("load canonical Claude Code rule: %v", err)
	}
	claudeEnabled = enabledInt != 0
	if claudeCount != 1 {
		t.Fatalf("expected exactly one Claude Code system rule, got %d", claudeCount)
	}
	if claudePattern != "claude(?:\\s|-)?(?:code|cli)" {
		t.Fatalf("expected canonical Claude Code pattern, got %q", claudePattern)
	}
	if claudeEnabled {
		t.Fatalf("expected existing Claude Code rule enabled=false to be preserved")
	}
	assertCount(t, testContext, conn, `SELECT COUNT(*) FROM user_agent_client_rules WHERE is_system = TRUE`, len(startup.SystemUserAgentClientRuleDefaults))

	var viaRuleName string
	var viaEnabled bool
	if err := conn.QueryRow(
		testContext,
		`SELECT name, enabled FROM header_blocklist_rules WHERE is_system = TRUE AND match_type = 'exact' AND pattern = 'via'`,
	).Scan(&viaRuleName, &viaEnabled); err != nil {
		t.Fatalf("load preserved Via header blocklist rule: %v", err)
	}
	if viaRuleName != "Custom Via Header" || viaEnabled {
		t.Fatalf("expected existing Via header rule to be preserved, got name=%q enabled=%v", viaRuleName, viaEnabled)
	}
	assertCount(t, testContext, conn, `SELECT COUNT(*) FROM header_blocklist_rules WHERE is_system = TRUE`, len(startup.SystemHeaderBlocklistDefaults))
}

func TestStartupIdempotency(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, testContext, "startup_idempotency")
	defer func() { _ = conn.Close(testContext) }()

	runner := newRunner(t)
	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("apply baseline before idempotency test: %v", err)
	}

	now := time.Date(2026, 4, 18, 12, 30, 0, 0, time.UTC)
	profileID := insertProfile(t, testContext, conn, profileSeed{
		Name:      "Idempotent Profile",
		IsActive:  true,
		IsDefault: false,
		Version:   0,
		CreatedAt: now,
		UpdatedAt: now,
	})
	insertEndpoint(t, testContext, conn, endpointSeed{
		ProfileID: profileID,
		Name:      "Primary endpoint",
		BaseURL:   "https://api.example.com",
		APIKey:    "plain-secret-token",
		Position:  0,
		CreatedAt: now,
		UpdatedAt: now,
	})

	service := newStartupService(t, harness.connectionString("startup_idempotency"), nil)
	firstResult, err := service.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("run first startup sequence for idempotency: %v", err)
	}
	assertStartupStepOrder(t, firstResult)

	firstSnapshot := snapshotStartupState(t, testContext, conn)
	var normalizedAPIKey string
	if err := conn.QueryRow(testContext, `SELECT api_key FROM endpoints WHERE name = 'Primary endpoint'`).Scan(&normalizedAPIKey); err != nil {
		t.Fatalf("load normalized endpoint api_key after first startup run: %v", err)
	}
	if !strings.HasPrefix(normalizedAPIKey, "enc:") {
		t.Fatalf("expected first startup run to normalize endpoint secret, got %q", normalizedAPIKey)
	}

	secondResult, err := service.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("run second startup sequence for idempotency: %v", err)
	}
	assertStartupStepOrder(t, secondResult)

	secondSnapshot := snapshotStartupState(t, testContext, conn)
	if firstSnapshot != secondSnapshot {
		t.Fatalf("expected startup-owned state to stay identical across repeated runs\n--- first ---\n%s\n--- second ---\n%s", firstSnapshot, secondSnapshot)
	}

	var secondNormalizedAPIKey string
	if err := conn.QueryRow(testContext, `SELECT api_key FROM endpoints WHERE name = 'Primary endpoint'`).Scan(&secondNormalizedAPIKey); err != nil {
		t.Fatalf("load normalized endpoint api_key after second startup run: %v", err)
	}
	if secondNormalizedAPIKey != normalizedAPIKey {
		t.Fatalf("expected already-normalized endpoint secret to stay unchanged on repeat startup")
	}
}

func TestStartupPreservesRuntimeStatePersistenceAcrossRestart(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, testContext, "startup_runtime_state_persistence")
	defer func() { _ = conn.Close(testContext) }()

	runner := newRunner(t)
	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("apply baseline before runtime-state persistence test: %v", err)
	}

	service := newStartupService(t, harness.connectionString("startup_runtime_state_persistence"), nil)
	initialResult, err := service.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("run initial startup sequence before runtime-state persistence test: %v", err)
	}
	assertStartupStepOrder(t, initialResult)

	defaultProfile := loadSingleProfile(t, testContext, conn, "name = 'Default'")
	now := time.Date(2026, 4, 18, 14, 0, 0, 0, time.UTC)
	strategyID := insertLegacyLoadbalanceStrategy(t, testContext, conn, defaultProfile.ID, "Startup runtime persistence strategy", now)
	openModelConfigID := insertModelConfig(t, testContext, conn, modelConfigSeed{
		ProfileID:             defaultProfile.ID,
		APIFamily:             "openai",
		ModelID:               "startup-runtime-open-model",
		LoadbalanceStrategyID: sql.NullInt64{Int64: int64(strategyID), Valid: true},
		IsEnabled:             true,
		CreatedAt:             now,
		UpdatedAt:             now,
	})
	closedModelConfigID := insertModelConfig(t, testContext, conn, modelConfigSeed{
		ProfileID:             defaultProfile.ID,
		APIFamily:             "openai",
		ModelID:               "startup-runtime-closed-model",
		LoadbalanceStrategyID: sql.NullInt64{Int64: int64(strategyID), Valid: true},
		IsEnabled:             true,
		CreatedAt:             now,
		UpdatedAt:             now,
	})
	openEndpointID := insertEndpoint(t, testContext, conn, endpointSeed{
		ProfileID: defaultProfile.ID,
		Name:      "Runtime open endpoint",
		BaseURL:   "https://runtime-open.example.com",
		APIKey:    "runtime-open-secret",
		Position:  0,
		CreatedAt: now,
		UpdatedAt: now,
	})
	closedEndpointID := insertEndpoint(t, testContext, conn, endpointSeed{
		ProfileID: defaultProfile.ID,
		Name:      "Runtime closed endpoint",
		BaseURL:   "https://runtime-closed.example.com",
		APIKey:    "runtime-closed-secret",
		Position:  1,
		CreatedAt: now,
		UpdatedAt: now,
	})
	openConnectionID := insertConnection(t, testContext, conn, connectionSeed{
		ProfileID:     defaultProfile.ID,
		ModelConfigID: openModelConfigID,
		EndpointID:    openEndpointID,
		Name:          "Runtime open connection",
		Priority:      0,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	closedConnectionID := insertConnection(t, testContext, conn, connectionSeed{
		ProfileID:     defaultProfile.ID,
		ModelConfigID: closedModelConfigID,
		EndpointID:    closedEndpointID,
		Name:          "Runtime closed connection",
		Priority:      1,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	failureKind := "transient_http"
	nextRetryAt := now.Add(5 * time.Minute)
	successObservedAt := now.Add(-30 * time.Second)
	latencyMS := int32(125)
	insertRuntimeState(t, testContext, conn, runtimeStateSeed{
		ProfileID:               defaultProfile.ID,
		ConnectionID:            openConnectionID,
		CycleRetryAttempts:      2,
		CumulativeRetryAttempts: 4,
		NextRetryAt:             &nextRetryAt,
		LastRetryDelayMS:        120000,
		BanMode:                 "temporary",
		BannedUntilAt:           &nextRetryAt,
		LastFailureKind:         &failureKind,
		CreatedAt:               now,
		UpdatedAt:               now,
	})
	insertRuntimeState(t, testContext, conn, runtimeStateSeed{
		ProfileID:        defaultProfile.ID,
		ConnectionID:     closedConnectionID,
		BanMode:          "off",
		LiveP95LatencyMS: &latencyMS,
		LastSuccessAt:    &successObservedAt,
		CreatedAt:        now,
		UpdatedAt:        now,
	})

	beforeRestartSnapshot := snapshotRuntimeStateRows(t, testContext, conn)
	restartResult, err := service.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("run restart startup sequence for runtime-state persistence test: %v", err)
	}
	assertStartupStepOrder(t, restartResult)
	afterRestartSnapshot := snapshotRuntimeStateRows(t, testContext, conn)
	if beforeRestartSnapshot != afterRestartSnapshot {
		t.Fatalf("expected startup restart to preserve routing_connection_runtime_state rows\n--- before ---\n%s\n--- after ---\n%s", beforeRestartSnapshot, afterRestartSnapshot)
	}
}

type profileSeed struct {
	Name        string
	Description string
	IsActive    bool
	IsDefault   bool
	IsEditable  bool
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type modelConfigSeed struct {
	ProfileID             int
	APIFamily             string
	ModelID               string
	LoadbalanceStrategyID sql.NullInt64
	IsEnabled             bool
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type systemUserAgentRuleSeed struct {
	Name      string
	Pattern   string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type systemHeaderBlocklistRuleSeed struct {
	Name      string
	MatchType string
	Pattern   string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type endpointSeed struct {
	ProfileID int
	Name      string
	BaseURL   string
	APIKey    string
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type connectionSeed struct {
	ProfileID     int
	ModelConfigID int
	EndpointID    int
	Name          string
	Priority      int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type runtimeStateSeed struct {
	ProfileID               int
	ConnectionID            int
	CycleRetryAttempts      int
	CumulativeRetryAttempts int
	NextRetryAt             *time.Time
	LastRetryDelayMS        int
	BanMode                 string
	BannedUntilAt           *time.Time
	LastFailureKind         *string
	LastSuccessAt           *time.Time
	LiveP95LatencyMS        *int32
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type profileSnapshot struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	IsActive    bool    `json:"is_active"`
	IsDefault   bool    `json:"is_default"`
	IsEditable  bool    `json:"is_editable"`
	Version     int     `json:"version"`
	DeletedAt   *string `json:"deleted_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type userSettingSnapshot struct {
	ProfileID            int     `json:"profile_id"`
	ReportCurrencyCode   string  `json:"report_currency_code"`
	ReportCurrencySymbol string  `json:"report_currency_symbol"`
	TimezonePreference   *string `json:"timezone_preference"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

type appAuthSettingsRecord struct {
	SingletonKey                  string `json:"singleton_key"`
	AuthEnabled                   bool   `json:"auth_enabled"`
	EmailVerificationAttemptCount int    `json:"email_verification_attempt_count"`
	MustChangePassword            bool   `json:"must_change_password"`
	TokenVersion                  int    `json:"token_version"`
	CreatedAt                     string `json:"created_at"`
	UpdatedAt                     string `json:"updated_at"`
}

type systemHeaderRuleSnapshot struct {
	Name      string `json:"name"`
	MatchType string `json:"match_type"`
	Pattern   string `json:"pattern"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type systemUserAgentRuleSnapshot struct {
	Name      string `json:"name"`
	Pattern   string `json:"pattern"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type endpointSnapshot struct {
	ID        int    `json:"id"`
	ProfileID int    `json:"profile_id"`
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	Position  int    `json:"position"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type runtimeStateSnapshot struct {
	ProfileID               int     `json:"profile_id"`
	ConnectionID            int     `json:"connection_id"`
	CycleRetryAttempts      int     `json:"cycle_retry_attempts"`
	CumulativeRetryAttempts int     `json:"cumulative_retry_attempts"`
	NextRetryAt             *string `json:"next_retry_at"`
	LastRetryDelayMS        int     `json:"last_retry_delay_ms"`
	BanMode                 string  `json:"ban_mode"`
	BannedUntilAt           *string `json:"banned_until_at"`
	LastFailureKind         *string `json:"last_failure_kind"`
	LastSuccessAt           *string `json:"last_success_at"`
	LiveP95LatencyMS        *int32  `json:"live_p95_latency_ms"`
	CreatedAt               string  `json:"created_at"`
	UpdatedAt               string  `json:"updated_at"`
}

type startupStateSnapshot struct {
	Profiles             []profileSnapshot             `json:"profiles"`
	UserSettings         []userSettingSnapshot         `json:"user_settings"`
	AppAuthSettings      []appAuthSettingsRecord       `json:"app_auth_settings"`
	HeaderBlocklistRules []systemHeaderRuleSnapshot    `json:"header_blocklist_rules"`
	UserAgentClientRules []systemUserAgentRuleSnapshot `json:"user_agent_client_rules"`
	Endpoints            []endpointSnapshot            `json:"endpoints"`
}

type startupBootstrapFileState struct {
	raw     []byte
	modTime time.Time
}

func startupTelemetryBootstrapPayload(endpoint string) map[string]any {
	return map[string]any{
		"enabled": true,
		"exporter": map[string]any{
			"endpoint":    endpoint,
			"protocol":    "http/protobuf",
			"compression": "none",
			"timeout":     "1s",
			"auth": map[string]any{
				"mode": "none",
			},
			"tls": map[string]any{
				"insecureSkipVerify": false,
			},
		},
		"metrics": map[string]any{
			"enabled": true,
		},
		"traces": map[string]any{
			"enabled":       true,
			"samplingRatio": 0.5,
		},
	}
}

func reserveLocalTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve backend port: %v", err)
	}
	defer func() { _ = listener.Close() }()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("reserved listener address has type %T", listener.Addr())
	}
	return addr.Port
}

func buildBackendBinary(t *testing.T, ctx context.Context) string {
	t.Helper()
	packageDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve integration package directory: %v", err)
	}
	backendRoot := filepath.Clean(filepath.Join(packageDir, "..", ".."))
	binaryPath := filepath.Join(t.TempDir(), "prism-backend")
	command := exec.CommandContext(ctx, "go", "build", "-o", binaryPath, "./cmd/prism-backend")
	command.Dir = backendRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build backend binary: %v\n%s", err, output)
	}
	return binaryPath
}

func runBackendUntilHealthyThenInterrupt(t *testing.T, ctx context.Context, binaryPath string, configPath string, port int) string {
	t.Helper()
	var output bytes.Buffer
	command := exec.CommandContext(ctx, binaryPath)
	command.Env = append(os.Environ(),
		config.BootstrapConfigPathEnv+"="+configPath,
		"DATABASE_URL=postgres://ignored-env@db.invalid:5432/prism?sslmode=disable",
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:1/ignored-env",
	)
	command.Stdout = &output
	command.Stderr = &output
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatalf("start backend with startup telemetry config: %v", err)
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- command.Wait() }()
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	if err := waitForBackendHealth(ctx, healthURL, waitErr); err != nil {
		terminateBackendProcess(command.Process.Pid)
		t.Fatalf("backend did not become healthy with startup telemetry config: %v\n%s", err, output.String())
	}
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGINT); err != nil {
		terminateBackendProcess(command.Process.Pid)
		t.Fatalf("interrupt backend with startup telemetry config: %v", err)
	}
	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("backend exited after interrupt: %v\n%s", err, output.String())
		}
	case <-ctx.Done():
		terminateBackendProcess(command.Process.Pid)
		t.Fatalf("backend did not stop after interrupt: %v\n%s", ctx.Err(), output.String())
	}
	return output.String()
}

func waitForBackendHealth(ctx context.Context, healthURL string, waitErr <-chan error) error {
	client := http.Client{Timeout: 200 * time.Millisecond}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-waitErr:
			return fmt.Errorf("backend exited before health was ready: %w", err)
		case <-ticker.C:
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
			if err != nil {
				return err
			}
			response, err := client.Do(request)
			if err != nil {
				continue
			}
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func terminateBackendProcess(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func runBackendPrintEffectiveStartupSettings(t *testing.T, configPath, databaseURL string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	packageDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve integration package directory: %v", err)
	}
	backendRoot := filepath.Clean(filepath.Join(packageDir, "..", ".."))
	command := exec.CommandContext(ctx, "go", "run", "./cmd/prism-backend")
	command.Dir = backendRoot
	command.Env = append(os.Environ(),
		config.BootstrapConfigPathEnv+"="+configPath,
		"DATABASE_URL="+databaseURL,
		"PRISM_PRINT_EFFECTIVE_STARTUP_SETTINGS=1",
	)

	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("backend print-effective startup timed out: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("backend print-effective startup failed: %v\n%s", err, output)
	}
	return string(output)
}

func readStartupBootstrapFileState(t *testing.T, configPath string) startupBootstrapFileState {
	t.Helper()
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read startup bootstrap config %q: %v", configPath, err)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat startup bootstrap config %q: %v", configPath, err)
	}
	return startupBootstrapFileState{raw: raw, modTime: info.ModTime()}
}

func setStartupBootstrapFileModTime(t *testing.T, configPath string, modTime time.Time) startupBootstrapFileState {
	t.Helper()
	if err := os.Chtimes(configPath, modTime, modTime); err != nil {
		t.Fatalf("set startup bootstrap config mtime for %q: %v", configPath, err)
	}
	return readStartupBootstrapFileState(t, configPath)
}

func assertStartupBootstrapFileStatePreserved(t *testing.T, configPath string, before startupBootstrapFileState) {
	t.Helper()
	after := readStartupBootstrapFileState(t, configPath)
	if !bytes.Equal(after.raw, before.raw) {
		t.Fatalf("expected startup bootstrap config bytes to be preserved\nbefore:\n%s\nafter:\n%s", before.raw, after.raw)
	}
	if !after.modTime.Equal(before.modTime) {
		t.Fatalf("expected startup bootstrap config mtime %s to be preserved, got %s", before.modTime, after.modTime)
	}
}

func mutateStartupBootstrapJSON(t *testing.T, configPath string, mutate func(map[string]any)) {
	t.Helper()
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read startup bootstrap config %q: %v", configPath, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal startup bootstrap config %q: %v", configPath, err)
	}
	mutate(payload)
	mutated, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal startup bootstrap config %q: %v", configPath, err)
	}
	mutated = append(mutated, '\n')
	if err := os.WriteFile(configPath, mutated, 0o600); err != nil {
		t.Fatalf("write startup bootstrap config %q: %v", configPath, err)
	}
}

func startupBootstrapObject(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()
	object, ok := payload[key].(map[string]any)
	if !ok {
		t.Fatalf("expected startup bootstrap field %q to be an object, got %T", key, payload[key])
	}
	return object
}

func assertStartupPrintLine(t *testing.T, outputText, expectedLine string) {
	t.Helper()
	if !strings.Contains(outputText, expectedLine+"\n") {
		t.Fatalf("expected startup print output to contain %q, got:\n%s", expectedLine, outputText)
	}
}

func assertStartupPrintDoesNotContain(t *testing.T, outputText, forbidden string) {
	t.Helper()
	if forbidden != "" && strings.Contains(outputText, forbidden) {
		t.Fatalf("startup print output contained %q:\n%s", forbidden, outputText)
	}
}

func newStartupService(t *testing.T, databaseURL string, observer func(startup.Step)) startup.Service {
	t.Helper()

	service, err := startup.New(startup.Options{
		DatabaseURL:         databaseURL,
		SecretEncryptionKey: "startup-test-secret",
		TimeNow: func() time.Time {
			return time.Date(2026, 4, 18, 13, 0, 0, 0, time.UTC)
		},
		StepObserver: observer,
	})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}

	return service
}

func productionLifecycleSettings(databaseURL string) config.Settings {
	return config.Settings{
		Host:                     "127.0.0.1",
		Port:                     18000,
		AppEnv:                   config.EnvironmentProduction,
		DatabaseURL:              databaseURL,
		RuntimeTransportConfig:   config.RuntimeTransportConfig{RequestTimeout: time.Second},
		RuntimeSideEffectsConfig: config.RuntimeSideEffectsConfig{AttemptTimeout: time.Second},
		SecretEncryptionKey:      "startup-test-secret",
		AuthJWTSecret:            "startup-test-jwt-secret",
	}
}

func assertStartupLogPartitionHorizon(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	const expectedHorizonDays = 15
	for _, tableName := range logretention.ManagedTables() {
		var partitionCount int
		if err := conn.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM pg_inherits inheritance
			JOIN pg_class parent ON parent.oid = inheritance.inhparent
			JOIN pg_namespace parent_ns ON parent_ns.oid = parent.relnamespace
			WHERE parent_ns.nspname = 'public' AND parent.relname = $1`, tableName).Scan(&partitionCount); err != nil {
			t.Fatalf("count startup partitions for %s: %v", tableName, err)
		}
		if partitionCount != expectedHorizonDays {
			t.Fatalf("expected %d startup partitions for %s, got %d", expectedHorizonDays, tableName, partitionCount)
		}

		currentPartition := tableName + "_p" + time.Now().UTC().Format("20060102")
		var currentPartitionExists bool
		if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+currentPartition).Scan(&currentPartitionExists); err != nil {
			t.Fatalf("check current startup partition for %s: %v", tableName, err)
		}
		if !currentPartitionExists {
			t.Fatalf("expected current startup partition %s to exist", currentPartition)
		}
	}
}

func assertStartupStepOrder(t *testing.T, result startup.Result) {
	t.Helper()
	assertObservedStepOrder(t, result.ExecutedSteps)
}

func assertObservedStepOrder(t *testing.T, steps []startup.Step) {
	t.Helper()
	want := []startup.Step{
		startup.StepMigrations,
		startup.StepProfileInvariantSeed,
		startup.StepUserSettingsSeed,
		startup.StepUserAgentClientRuleSeed,
		startup.StepAppAuthSettingsSeed,
		startup.StepEndpointSecretNormalization,
		startup.StepHeaderBlocklistRuleSeed,
	}
	if len(steps) != len(want) {
		t.Fatalf("expected startup steps %v, got %v", want, steps)
	}
	for index := range want {
		if steps[index] != want[index] {
			t.Fatalf("expected startup steps %v, got %v", want, steps)
		}
	}
}

func assertCount(t *testing.T, ctx context.Context, conn *pgx.Conn, query string, want int) {
	t.Helper()
	var got int
	if err := conn.QueryRow(ctx, query).Scan(&got); err != nil {
		t.Fatalf("count rows with query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("expected count %d for query %q, got %d", want, query, got)
	}
}

func insertProfile(t *testing.T, ctx context.Context, conn *pgx.Conn, seed profileSeed) int {
	t.Helper()
	var profileID int
	if err := conn.QueryRow(
		ctx,
		`INSERT INTO profiles (
			name,
			description,
			is_active,
			is_default,
			is_editable,
			version,
			deleted_at,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`,
		seed.Name,
		seed.Description,
		seed.IsActive,
		seed.IsDefault,
		seed.IsEditable,
		seed.Version,
		nil,
		seed.CreatedAt,
		seed.UpdatedAt,
	).Scan(&profileID); err != nil {
		t.Fatalf("insert profile %q: %v", seed.Name, err)
	}
	return profileID
}

func insertModelConfig(t *testing.T, ctx context.Context, conn *pgx.Conn, seed modelConfigSeed) int {
	t.Helper()
	var modelConfigID int
	if err := conn.QueryRow(
		ctx,
		`INSERT INTO model_configs (
			profile_id,
			api_family,
			model_id,
			display_name,
			loadbalance_strategy_id,
			openai_accepted_format,
			is_enabled,
			created_at,
			updated_at
		) VALUES ($1, $2::varchar(50), $3, $4, $5, CASE WHEN $2::varchar(50) = 'openai' THEN 'dual_native'::text ELSE NULL::text END, $6, $7, $8)
		RETURNING id`,
		seed.ProfileID,
		seed.APIFamily,
		seed.ModelID,
		nil,
		nullInt64(seed.LoadbalanceStrategyID),
		seed.IsEnabled,
		seed.CreatedAt,
		seed.UpdatedAt,
	).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert model_config %q: %v", seed.ModelID, err)
	}
	return modelConfigID
}

func insertLegacyLoadbalanceStrategy(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, name string, now time.Time) int {
	t.Helper()
	var strategyID int
	if err := conn.QueryRow(
		ctx,
		`INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at)
		 VALUES ($1, $2, 'round-robin', ARRAY[403,422,429,500,502,503,504,529], 'off', 60000, 2.0, 0.2, 900000, 3, 0, 0, $3, $3)
		 RETURNING id`,
		profileID,
		name,
		now,
	).Scan(&strategyID); err != nil {
		t.Fatalf("insert legacy loadbalance strategy %q: %v", name, err)
	}
	return strategyID
}

func insertSystemUserAgentRule(t *testing.T, ctx context.Context, conn *pgx.Conn, seed systemUserAgentRuleSeed) {
	t.Helper()
	if _, err := conn.Exec(
		ctx,
		`INSERT INTO user_agent_client_rules (
			profile_id,
			name,
			pattern,
			enabled,
			is_system,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		nil,
		seed.Name,
		seed.Pattern,
		seed.Enabled,
		true,
		seed.CreatedAt,
		seed.UpdatedAt,
	); err != nil {
		t.Fatalf("insert system user-agent rule %q: %v", seed.Name, err)
	}
}

func insertSystemHeaderBlocklistRule(t *testing.T, ctx context.Context, conn *pgx.Conn, seed systemHeaderBlocklistRuleSeed) {
	t.Helper()
	if _, err := conn.Exec(
		ctx,
		`INSERT INTO header_blocklist_rules (
			profile_id,
			name,
			match_type,
			pattern,
			enabled,
			is_system,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		nil,
		seed.Name,
		seed.MatchType,
		seed.Pattern,
		seed.Enabled,
		true,
		seed.CreatedAt,
		seed.UpdatedAt,
	); err != nil {
		t.Fatalf("insert system header blocklist rule %q: %v", seed.Name, err)
	}
}

func insertEndpoint(t *testing.T, ctx context.Context, conn *pgx.Conn, seed endpointSeed) int {
	t.Helper()
	var endpointID int
	if err := conn.QueryRow(
		ctx,
		`INSERT INTO endpoints (
			profile_id,
			name,
			base_url,
			api_key,
			position,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		seed.ProfileID,
		seed.Name,
		seed.BaseURL,
		seed.APIKey,
		seed.Position,
		seed.CreatedAt,
		seed.UpdatedAt,
	).Scan(&endpointID); err != nil {
		t.Fatalf("insert endpoint %q: %v", seed.Name, err)
	}
	return endpointID
}

func insertConnection(t *testing.T, ctx context.Context, conn *pgx.Conn, seed connectionSeed) int {
	t.Helper()
	var apiFamily string
	if err := conn.QueryRow(ctx, `SELECT api_family FROM model_configs WHERE id = $1`, seed.ModelConfigID).Scan(&apiFamily); err != nil {
		t.Fatalf("load model api family for connection %q: %v", seed.Name, err)
	}
	var connectionID int
	if err := conn.QueryRow(
		ctx,
		`INSERT INTO connections (
			profile_id,
			api_family,
			endpoint_id,
			pricing_template_id,
			qps_limit,
			max_in_flight_non_stream,
			max_in_flight_stream,
			openai_probe_endpoint_variant,
			openai_text_capability,
			is_active,
			priority,
			name,
			auth_type,
			custom_headers,
			health_status,
			health_detail,
			last_health_check,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, NULL, NULL, NULL, NULL, NULL, $8, TRUE, $4, $5, NULL, NULL, 'healthy', NULL, NULL, $6, $7)
		RETURNING id`,
		seed.ProfileID,
		apiFamily,
		seed.EndpointID,
		seed.Priority,
		seed.Name,
		seed.CreatedAt,
		seed.UpdatedAt,
		openAITextCapabilityForSeedAPIFamily(apiFamily),
	).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection %q: %v", seed.Name, err)
	}
	return connectionID
}

func insertRuntimeState(t *testing.T, ctx context.Context, conn *pgx.Conn, seed runtimeStateSeed) {
	t.Helper()
	if _, err := conn.Exec(
		ctx,
		`INSERT INTO routing_connection_runtime_state (
			profile_id,
			connection_id,
			window_started_at,
			window_request_count,
			in_flight_non_stream,
			in_flight_stream,
			cycle_retry_attempts,
			cumulative_retry_attempts,
			next_retry_at,
			last_retry_delay_ms,
			ban_mode,
			banned_until_at,
			last_failure_kind,
			last_success_at,
			live_p95_latency_ms,
			created_at,
			updated_at
		) VALUES ($1, $2, NULL, 0, 0, 0, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		seed.ProfileID,
		seed.ConnectionID,
		seed.CycleRetryAttempts,
		seed.CumulativeRetryAttempts,
		nullableSeedTime(seed.NextRetryAt),
		seed.LastRetryDelayMS,
		normalizeSeedBanMode(seed.BanMode),
		nullableSeedTime(seed.BannedUntilAt),
		nullableSeedString(seed.LastFailureKind),
		nullableSeedTime(seed.LastSuccessAt),
		nullableSeedInt32(seed.LiveP95LatencyMS),
		seed.CreatedAt,
		seed.UpdatedAt,
	); err != nil {
		t.Fatalf("insert runtime state for connection %d: %v", seed.ConnectionID, err)
	}
}

func loadSingleProfile(t *testing.T, ctx context.Context, conn *pgx.Conn, predicate string) profileSeedRow {
	t.Helper()
	query := fmt.Sprintf(`SELECT id, name, COALESCE(description, ''), is_active, is_default, is_editable, version, deleted_at FROM profiles WHERE %s`, predicate)
	var row profileSeedRow
	if err := conn.QueryRow(ctx, query).Scan(&row.ID, &row.Name, &row.Description, &row.IsActive, &row.IsDefault, &row.IsEditable, &row.Version, &row.DeletedAt); err != nil {
		t.Fatalf("load profile row with predicate %q: %v", predicate, err)
	}
	return row
}

type profileSeedRow struct {
	ID          int
	Name        string
	Description string
	IsActive    bool
	IsDefault   bool
	IsEditable  bool
	Version     int
	DeletedAt   sql.NullTime
}

func snapshotStartupState(t *testing.T, ctx context.Context, conn *pgx.Conn) string {
	t.Helper()
	snapshot := startupStateSnapshot{
		Profiles:             loadProfileSnapshots(t, ctx, conn),
		UserSettings:         loadUserSettingSnapshots(t, ctx, conn),
		AppAuthSettings:      loadAppAuthSettingsRecords(t, ctx, conn),
		HeaderBlocklistRules: loadHeaderBlocklistRuleSnapshots(t, ctx, conn),
		UserAgentClientRules: loadUserAgentRuleSnapshots(t, ctx, conn),
		Endpoints:            loadEndpointSnapshots(t, ctx, conn),
	}

	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("marshal startup snapshot: %v", err)
	}
	return string(raw)
}

func loadProfileSnapshots(t *testing.T, ctx context.Context, conn *pgx.Conn) []profileSnapshot {
	t.Helper()
	rows, err := conn.Query(
		ctx,
		`SELECT id, name, COALESCE(description, ''), is_active, is_default, is_editable, version, deleted_at, created_at, updated_at FROM profiles ORDER BY id ASC`,
	)
	if err != nil {
		t.Fatalf("query profile snapshots: %v", err)
	}
	defer rows.Close()

	items := []profileSnapshot{}
	for rows.Next() {
		var (
			item      profileSnapshot
			deletedAt sql.NullTime
			createdAt time.Time
			updatedAt time.Time
		)
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.IsActive, &item.IsDefault, &item.IsEditable, &item.Version, &deletedAt, &createdAt, &updatedAt); err != nil {
			t.Fatalf("scan profile snapshot: %v", err)
		}
		item.DeletedAt = formatNullableTime(deletedAt)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate profile snapshots: %v", err)
	}
	return items
}

func loadUserSettingSnapshots(t *testing.T, ctx context.Context, conn *pgx.Conn) []userSettingSnapshot {
	t.Helper()
	rows, err := conn.Query(
		ctx,
		`SELECT profile_id, report_currency_code, report_currency_symbol, timezone_preference, created_at, updated_at FROM user_settings ORDER BY profile_id ASC`,
	)
	if err != nil {
		t.Fatalf("query user_settings snapshots: %v", err)
	}
	defer rows.Close()

	items := []userSettingSnapshot{}
	for rows.Next() {
		var (
			item      userSettingSnapshot
			tz        sql.NullString
			createdAt time.Time
			updatedAt time.Time
		)
		if err := rows.Scan(&item.ProfileID, &item.ReportCurrencyCode, &item.ReportCurrencySymbol, &tz, &createdAt, &updatedAt); err != nil {
			t.Fatalf("scan user_settings snapshot: %v", err)
		}
		item.TimezonePreference = formatNullableString(tz)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate user_settings snapshots: %v", err)
	}
	return items
}

func loadAppAuthSettingsRecords(t *testing.T, ctx context.Context, conn *pgx.Conn) []appAuthSettingsRecord {
	t.Helper()
	rows, err := conn.Query(
		ctx,
		`SELECT singleton_key, auth_enabled, email_verification_attempt_count, must_change_password, token_version, created_at, updated_at FROM app_auth_settings ORDER BY id ASC`,
	)
	if err != nil {
		t.Fatalf("query app_auth_settings snapshots: %v", err)
	}
	defer rows.Close()

	items := []appAuthSettingsRecord{}
	for rows.Next() {
		var item appAuthSettingsRecord
		var createdAt time.Time
		var updatedAt time.Time
		if err := rows.Scan(&item.SingletonKey, &item.AuthEnabled, &item.EmailVerificationAttemptCount, &item.MustChangePassword, &item.TokenVersion, &createdAt, &updatedAt); err != nil {
			t.Fatalf("scan app_auth_settings snapshot: %v", err)
		}
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate app_auth_settings snapshots: %v", err)
	}
	return items
}

func loadHeaderBlocklistRuleSnapshots(t *testing.T, ctx context.Context, conn *pgx.Conn) []systemHeaderRuleSnapshot {
	t.Helper()
	rows, err := conn.Query(
		ctx,
		`SELECT name, match_type, pattern, enabled, created_at, updated_at FROM header_blocklist_rules WHERE is_system = TRUE ORDER BY id ASC`,
	)
	if err != nil {
		t.Fatalf("query header blocklist snapshots: %v", err)
	}
	defer rows.Close()

	items := []systemHeaderRuleSnapshot{}
	for rows.Next() {
		var item systemHeaderRuleSnapshot
		var createdAt time.Time
		var updatedAt time.Time
		if err := rows.Scan(&item.Name, &item.MatchType, &item.Pattern, &item.Enabled, &createdAt, &updatedAt); err != nil {
			t.Fatalf("scan header blocklist snapshot: %v", err)
		}
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate header blocklist snapshots: %v", err)
	}
	return items
}

func loadUserAgentRuleSnapshots(t *testing.T, ctx context.Context, conn *pgx.Conn) []systemUserAgentRuleSnapshot {
	t.Helper()
	rows, err := conn.Query(
		ctx,
		`SELECT name, pattern, enabled, created_at, updated_at FROM user_agent_client_rules WHERE is_system = TRUE ORDER BY id ASC`,
	)
	if err != nil {
		t.Fatalf("query user-agent rule snapshots: %v", err)
	}
	defer rows.Close()

	items := []systemUserAgentRuleSnapshot{}
	for rows.Next() {
		var item systemUserAgentRuleSnapshot
		var createdAt time.Time
		var updatedAt time.Time
		if err := rows.Scan(&item.Name, &item.Pattern, &item.Enabled, &createdAt, &updatedAt); err != nil {
			t.Fatalf("scan user-agent rule snapshot: %v", err)
		}
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate user-agent rule snapshots: %v", err)
	}
	return items
}

func loadEndpointSnapshots(t *testing.T, ctx context.Context, conn *pgx.Conn) []endpointSnapshot {
	t.Helper()
	rows, err := conn.Query(
		ctx,
		`SELECT id, profile_id, name, base_url, api_key, position, created_at, updated_at FROM endpoints ORDER BY id ASC`,
	)
	if err != nil {
		t.Fatalf("query endpoint snapshots: %v", err)
	}
	defer rows.Close()

	items := []endpointSnapshot{}
	for rows.Next() {
		var item endpointSnapshot
		var createdAt time.Time
		var updatedAt time.Time
		if err := rows.Scan(&item.ID, &item.ProfileID, &item.Name, &item.BaseURL, &item.APIKey, &item.Position, &createdAt, &updatedAt); err != nil {
			t.Fatalf("scan endpoint snapshot: %v", err)
		}
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate endpoint snapshots: %v", err)
	}
	return items
}

func snapshotRuntimeStateRows(t *testing.T, ctx context.Context, conn *pgx.Conn) string {
	t.Helper()
	rows, err := conn.Query(
		ctx,
		`SELECT profile_id, connection_id, cycle_retry_attempts, cumulative_retry_attempts, next_retry_at,
			last_retry_delay_ms, ban_mode, banned_until_at, last_failure_kind, last_success_at, live_p95_latency_ms,
			created_at, updated_at
		FROM routing_connection_runtime_state
		ORDER BY profile_id ASC, connection_id ASC`,
	)
	if err != nil {
		t.Fatalf("query runtime-state snapshots: %v", err)
	}
	defer rows.Close()

	items := []runtimeStateSnapshot{}
	for rows.Next() {
		var (
			item             runtimeStateSnapshot
			nextRetryAt      sql.NullTime
			bannedUntilAt    sql.NullTime
			lastFailureKind  sql.NullString
			lastSuccessAt    sql.NullTime
			liveP95LatencyMS sql.NullInt32
			createdAt        time.Time
			updatedAt        time.Time
		)
		if err := rows.Scan(
			&item.ProfileID,
			&item.ConnectionID,
			&item.CycleRetryAttempts,
			&item.CumulativeRetryAttempts,
			&nextRetryAt,
			&item.LastRetryDelayMS,
			&item.BanMode,
			&bannedUntilAt,
			&lastFailureKind,
			&lastSuccessAt,
			&liveP95LatencyMS,
			&createdAt,
			&updatedAt,
		); err != nil {
			t.Fatalf("scan runtime-state snapshot: %v", err)
		}
		item.NextRetryAt = formatNullableTime(nextRetryAt)
		item.BannedUntilAt = formatNullableTime(bannedUntilAt)
		item.LastFailureKind = formatNullableString(lastFailureKind)
		item.LastSuccessAt = formatNullableTime(lastSuccessAt)
		item.LiveP95LatencyMS = formatNullableInt32(liveP95LatencyMS)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate runtime-state snapshots: %v", err)
	}

	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		t.Fatalf("marshal runtime-state snapshots: %v", err)
	}
	return string(raw)
}

func nullInt64(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func formatNullableTime(value sql.NullTime) *string {
	if !value.Valid {
		return nil
	}
	formatted := value.Time.UTC().Format(time.RFC3339Nano)
	return &formatted
}

func formatNullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	formatted := value.String
	return &formatted
}

func formatNullableInt32(value sql.NullInt32) *int32 {
	if !value.Valid {
		return nil
	}
	formatted := value.Int32
	return &formatted
}

func nullableSeedString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func openAITextCapabilityForSeedAPIFamily(apiFamily string) any {
	if apiFamily != "openai" {
		return nil
	}
	return "dual_native"
}

func nullableSeedTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func nullableSeedInt32(value *int32) any {
	if value == nil {
		return nil
	}
	return *value
}

func normalizeSeedBanMode(value string) string {
	if strings.TrimSpace(value) == "" {
		return "off"
	}
	return value
}

const DefaultProfileDescription = "System default profile"
