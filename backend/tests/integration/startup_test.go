package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	vendors := loadVendorsByKey(t, testContext, conn)
	if len(vendors) != len(startup.DefaultVendors) {
		t.Fatalf("expected %d canonical vendors, got %d", len(startup.DefaultVendors), len(vendors))
	}
	for _, definition := range startup.DefaultVendors {
		vendor, ok := vendors[definition.Key]
		if !ok {
			t.Fatalf("expected vendor %q to exist", definition.Key)
		}
		if vendor.Name != definition.Name || vendor.Description != definition.Description || vendor.IconKey != definition.IconKey {
			t.Fatalf("expected canonical vendor %+v, got %+v", definition, vendor)
		}
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

func TestStartupVendorAndRuleSeeds(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, testContext, "startup_vendor_rule_seeds")
	defer func() { _ = conn.Close(testContext) }()

	runner := newRunner(t)
	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("apply baseline before startup vendor/rule test: %v", err)
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
	geminiVendorID := insertVendor(t, testContext, conn, vendorSeed{
		Key:                "gemini",
		Name:               "Gemini Existing",
		Description:        "Previous Gemini vendor",
		IconKey:            "gemini-old",
		AuditEnabled:       true,
		AuditCaptureBodies: false,
		CreatedAt:          now,
		UpdatedAt:          now,
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

	service := newStartupService(t, harness.connectionString("startup_vendor_rule_seeds"), nil)
	result, err := service.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("run startup sequence for vendor/rule canonicalization: %v", err)
	}

	assertStartupStepOrder(t, result)
	if result.Migration.Outcome != migrate.OutcomeNoop {
		t.Fatalf("expected startup migration step to noop after baseline apply, got %q", result.Migration.Outcome)
	}

	vendors := loadVendorsByKey(t, testContext, conn)
	geminiVendor, ok := vendors["gemini"]
	if !ok {
		t.Fatalf("expected canonical gemini vendor to exist")
	}
	if geminiVendor.ID != geminiVendorID {
		t.Fatalf("expected existing gemini vendor row id %d to be preserved, got %d", geminiVendorID, geminiVendor.ID)
	}
	if geminiVendor.Name != "Gemini" || geminiVendor.Description != "Google Gemini API" || geminiVendor.IconKey != "gemini" {
		t.Fatalf("expected gemini vendor identity to be canonicalized, got %+v", geminiVendor)
	}
	if !geminiVendor.AuditEnabled || geminiVendor.AuditCaptureBodies {
		t.Fatalf("expected existing gemini vendor audit flags to be preserved, got %+v", geminiVendor)
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
		ModelType:             "native",
		LoadbalanceStrategyID: sql.NullInt64{Int64: int64(strategyID), Valid: true},
		IsEnabled:             true,
		CreatedAt:             now,
		UpdatedAt:             now,
	})
	closedModelConfigID := insertModelConfig(t, testContext, conn, modelConfigSeed{
		ProfileID:             defaultProfile.ID,
		APIFamily:             "openai",
		ModelID:               "startup-runtime-closed-model",
		ModelType:             "native",
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
	openUntil := now.Add(5 * time.Minute)
	failureObservedAt := now.Add(-1 * time.Minute)
	successObservedAt := now.Add(-30 * time.Second)
	latencyMS := int32(125)
	insertRuntimeState(t, testContext, conn, runtimeStateSeed{
		ProfileID:           defaultProfile.ID,
		ConnectionID:        openConnectionID,
		ConsecutiveFailures: 2,
		LastFailureKind:     &failureKind,
		LastCooldownSeconds: 120,
		MaxCooldownStrikes:  2,
		BanMode:             "off",
		OpenUntilAt:         &openUntil,
		ProbeAvailableAt:    &openUntil,
		CircuitState:        "open",
		LastLiveFailureKind: &failureKind,
		LastLiveFailureAt:   &failureObservedAt,
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	insertRuntimeState(t, testContext, conn, runtimeStateSeed{
		ProfileID:         defaultProfile.ID,
		ConnectionID:      closedConnectionID,
		BanMode:           "off",
		CircuitState:      "closed",
		LiveP95LatencyMS:  &latencyMS,
		LastLiveSuccessAt: &successObservedAt,
		CreatedAt:         now,
		UpdatedAt:         now,
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

type vendorSeed struct {
	Key                string
	Name               string
	Description        string
	IconKey            string
	AuditEnabled       bool
	AuditCaptureBodies bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type modelConfigSeed struct {
	ProfileID              int
	VendorID               sql.NullInt64
	APIFamily              string
	ModelID                string
	ModelType              string
	LoadbalanceStrategyID  sql.NullInt64
	ProxySelectionStrategy sql.NullString
	IsEnabled              bool
	CreatedAt              time.Time
	UpdatedAt              time.Time
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
	ProfileID           int
	ConnectionID        int
	ConsecutiveFailures int
	LastFailureKind     *string
	LastCooldownSeconds float64
	MaxCooldownStrikes  int
	BanMode             string
	BannedUntilAt       *time.Time
	OpenUntilAt         *time.Time
	ProbeEligibleLogged bool
	CircuitState        string
	ProbeAvailableAt    *time.Time
	LiveP95LatencyMS    *int32
	LastLiveFailureKind *string
	LastLiveFailureAt   *time.Time
	LastLiveSuccessAt   *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type vendorSnapshot struct {
	ID                 int    `json:"id"`
	Key                string `json:"key"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	IconKey            string `json:"icon_key"`
	AuditEnabled       bool   `json:"audit_enabled"`
	AuditCaptureBodies bool   `json:"audit_capture_bodies"`
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
	ProfileID           int     `json:"profile_id"`
	ConnectionID        int     `json:"connection_id"`
	ConsecutiveFailures int     `json:"consecutive_failures"`
	LastFailureKind     *string `json:"last_failure_kind"`
	LastCooldownSeconds float64 `json:"last_cooldown_seconds"`
	MaxCooldownStrikes  int     `json:"max_cooldown_strikes"`
	BanMode             string  `json:"ban_mode"`
	BannedUntilAt       *string `json:"banned_until_at"`
	OpenUntilAt         *string `json:"open_until_at"`
	ProbeEligibleLogged bool    `json:"probe_eligible_logged"`
	CircuitState        string  `json:"circuit_state"`
	ProbeAvailableAt    *string `json:"probe_available_at"`
	LiveP95LatencyMS    *int32  `json:"live_p95_latency_ms"`
	LastLiveFailureKind *string `json:"last_live_failure_kind"`
	LastLiveFailureAt   *string `json:"last_live_failure_at"`
	LastLiveSuccessAt   *string `json:"last_live_success_at"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
}

type startupStateSnapshot struct {
	Profiles             []profileSnapshot             `json:"profiles"`
	UserSettings         []userSettingSnapshot         `json:"user_settings"`
	AppAuthSettings      []appAuthSettingsRecord       `json:"app_auth_settings"`
	Vendors              []vendorSnapshot              `json:"vendors"`
	HeaderBlocklistRules []systemHeaderRuleSnapshot    `json:"header_blocklist_rules"`
	UserAgentClientRules []systemUserAgentRuleSnapshot `json:"user_agent_client_rules"`
	Endpoints            []endpointSnapshot            `json:"endpoints"`
}

type startupBootstrapFileState struct {
	raw     []byte
	modTime time.Time
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
		startup.StepVendorSeed,
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

func insertVendor(t *testing.T, ctx context.Context, conn *pgx.Conn, seed vendorSeed) int {
	t.Helper()
	var vendorID int
	if err := conn.QueryRow(
		ctx,
		`INSERT INTO vendors (
			key,
			name,
			description,
			icon_key,
			audit_enabled,
			audit_capture_bodies,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		seed.Key,
		seed.Name,
		seed.Description,
		seed.IconKey,
		seed.AuditEnabled,
		seed.AuditCaptureBodies,
		seed.CreatedAt,
		seed.UpdatedAt,
	).Scan(&vendorID); err != nil {
		t.Fatalf("insert vendor %q: %v", seed.Key, err)
	}
	return vendorID
}

func insertModelConfig(t *testing.T, ctx context.Context, conn *pgx.Conn, seed modelConfigSeed) int {
	t.Helper()
	var modelConfigID int
	if err := conn.QueryRow(
		ctx,
		`INSERT INTO model_configs (
			profile_id,
			vendor_id,
			api_family,
			model_id,
			display_name,
			model_type,
			loadbalance_strategy_id,
			proxy_selection_strategy,
			is_enabled,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id`,
		seed.ProfileID,
		nullInt64(seed.VendorID),
		seed.APIFamily,
		seed.ModelID,
		nil,
		seed.ModelType,
		nullInt64(seed.LoadbalanceStrategyID),
		nullString(seed.ProxySelectionStrategy),
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
		`INSERT INTO loadbalance_strategies (profile_id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy, created_at, updated_at)
		 VALUES ($1, $2, 'legacy', 'round-robin', '{"mode":"disabled"}'::jsonb, NULL, $3, $3)
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
	var connectionID int
	if err := conn.QueryRow(
		ctx,
		`INSERT INTO connections (
			profile_id,
			model_config_id,
			endpoint_id,
			pricing_template_id,
			qps_limit,
			max_in_flight_non_stream,
			max_in_flight_stream,
			openai_probe_endpoint_variant,
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
		) VALUES ($1, $2, $3, NULL, NULL, NULL, NULL, NULL, TRUE, $4, $5, NULL, NULL, 'healthy', NULL, NULL, $6, $7)
		RETURNING id`,
		seed.ProfileID,
		seed.ModelConfigID,
		seed.EndpointID,
		seed.Priority,
		seed.Name,
		seed.CreatedAt,
		seed.UpdatedAt,
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
			consecutive_failures,
			last_failure_kind,
			last_cooldown_seconds,
			max_cooldown_strikes,
			ban_mode,
			banned_until_at,
			open_until_at,
			probe_eligible_logged,
			circuit_state,
			probe_available_at,
			live_p95_latency_ms,
			last_live_failure_kind,
			last_live_failure_at,
			last_live_success_at,
			created_at,
			updated_at
		) VALUES ($1, $2, NULL, 0, 0, 0, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		seed.ProfileID,
		seed.ConnectionID,
		seed.ConsecutiveFailures,
		nullableSeedString(seed.LastFailureKind),
		seed.LastCooldownSeconds,
		seed.MaxCooldownStrikes,
		normalizeSeedBanMode(seed.BanMode),
		nullableSeedTime(seed.BannedUntilAt),
		nullableSeedTime(seed.OpenUntilAt),
		seed.ProbeEligibleLogged,
		normalizeSeedCircuitState(seed.CircuitState),
		nullableSeedTime(seed.ProbeAvailableAt),
		nullableSeedInt32(seed.LiveP95LatencyMS),
		nullableSeedString(seed.LastLiveFailureKind),
		nullableSeedTime(seed.LastLiveFailureAt),
		nullableSeedTime(seed.LastLiveSuccessAt),
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

func loadVendorsByKey(t *testing.T, ctx context.Context, conn *pgx.Conn) map[string]vendorSnapshot {
	t.Helper()
	rows, err := conn.Query(
		ctx,
		`SELECT id, key, name, COALESCE(description, ''), COALESCE(icon_key, ''), audit_enabled, audit_capture_bodies FROM vendors ORDER BY key ASC`,
	)
	if err != nil {
		t.Fatalf("query vendors by key: %v", err)
	}
	defer rows.Close()

	vendors := map[string]vendorSnapshot{}
	for rows.Next() {
		var row vendorSnapshot
		if err := rows.Scan(&row.ID, &row.Key, &row.Name, &row.Description, &row.IconKey, &row.AuditEnabled, &row.AuditCaptureBodies); err != nil {
			t.Fatalf("scan vendor by key: %v", err)
		}
		vendors[row.Key] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate vendors by key: %v", err)
	}
	return vendors
}

func snapshotStartupState(t *testing.T, ctx context.Context, conn *pgx.Conn) string {
	t.Helper()
	snapshot := startupStateSnapshot{
		Profiles:             loadProfileSnapshots(t, ctx, conn),
		UserSettings:         loadUserSettingSnapshots(t, ctx, conn),
		AppAuthSettings:      loadAppAuthSettingsRecords(t, ctx, conn),
		Vendors:              loadVendorSnapshots(t, ctx, conn),
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

func loadVendorSnapshots(t *testing.T, ctx context.Context, conn *pgx.Conn) []vendorSnapshot {
	t.Helper()
	rows, err := conn.Query(
		ctx,
		`SELECT id, key, name, COALESCE(description, ''), COALESCE(icon_key, ''), audit_enabled, audit_capture_bodies FROM vendors ORDER BY id ASC`,
	)
	if err != nil {
		t.Fatalf("query vendor snapshots: %v", err)
	}
	defer rows.Close()

	items := []vendorSnapshot{}
	for rows.Next() {
		var item vendorSnapshot
		if err := rows.Scan(&item.ID, &item.Key, &item.Name, &item.Description, &item.IconKey, &item.AuditEnabled, &item.AuditCaptureBodies); err != nil {
			t.Fatalf("scan vendor snapshot: %v", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate vendor snapshots: %v", err)
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
		`SELECT profile_id, connection_id, consecutive_failures, last_failure_kind, last_cooldown_seconds::float8,
			max_cooldown_strikes, ban_mode, banned_until_at, open_until_at, probe_eligible_logged, circuit_state,
			probe_available_at, live_p95_latency_ms, last_live_failure_kind, last_live_failure_at, last_live_success_at,
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
			item                runtimeStateSnapshot
			lastFailureKind     sql.NullString
			bannedUntilAt       sql.NullTime
			openUntilAt         sql.NullTime
			probeAvailableAt    sql.NullTime
			liveP95LatencyMS    sql.NullInt32
			lastLiveFailureKind sql.NullString
			lastLiveFailureAt   sql.NullTime
			lastLiveSuccessAt   sql.NullTime
			createdAt           time.Time
			updatedAt           time.Time
		)
		if err := rows.Scan(
			&item.ProfileID,
			&item.ConnectionID,
			&item.ConsecutiveFailures,
			&lastFailureKind,
			&item.LastCooldownSeconds,
			&item.MaxCooldownStrikes,
			&item.BanMode,
			&bannedUntilAt,
			&openUntilAt,
			&item.ProbeEligibleLogged,
			&item.CircuitState,
			&probeAvailableAt,
			&liveP95LatencyMS,
			&lastLiveFailureKind,
			&lastLiveFailureAt,
			&lastLiveSuccessAt,
			&createdAt,
			&updatedAt,
		); err != nil {
			t.Fatalf("scan runtime-state snapshot: %v", err)
		}
		item.LastFailureKind = formatNullableString(lastFailureKind)
		item.BannedUntilAt = formatNullableTime(bannedUntilAt)
		item.OpenUntilAt = formatNullableTime(openUntilAt)
		item.ProbeAvailableAt = formatNullableTime(probeAvailableAt)
		item.LiveP95LatencyMS = formatNullableInt32(liveP95LatencyMS)
		item.LastLiveFailureKind = formatNullableString(lastLiveFailureKind)
		item.LastLiveFailureAt = formatNullableTime(lastLiveFailureAt)
		item.LastLiveSuccessAt = formatNullableTime(lastLiveSuccessAt)
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

func nullString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
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

func normalizeSeedCircuitState(value string) string {
	if strings.TrimSpace(value) == "" {
		return "closed"
	}
	return value
}

const DefaultProfileDescription = "System default profile"
