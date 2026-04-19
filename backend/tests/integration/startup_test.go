package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

func TestStartupSeeds(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, testContext, "startup_seeds")
	defer conn.Close(testContext)

	service := newStartupService(t, harness.connectionString("startup_seeds"), nil)
	result, err := service.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("run startup sequence on empty database: %v", err)
	}

	assertStartupStepOrder(t, result)
	if result.Migration.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected startup to apply baseline on empty database, got %q", result.Migration.Outcome)
	}
	if result.BillingReconciliation.Ran {
		t.Fatalf("expected billing reconciliation to skip when no pending rows exist")
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

func TestStartupVendorAndRuleSeeds(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, testContext, "startup_vendor_rule_seeds")
	defer conn.Close(testContext)

	runner := newRunner(t)
	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("apply baseline before startup vendor/rule test: %v", err)
	}

	now := time.Date(2026, 4, 18, 11, 30, 0, 0, time.UTC)
	profileID := insertProfile(t, testContext, conn, profileSeed{
		Name:        "Seed Profile",
		Description: "existing profile",
		IsActive:    true,
		IsDefault:   false,
		IsEditable:  true,
		Version:     0,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	googleVendorID := insertVendor(t, testContext, conn, vendorSeed{
		Key:                "google",
		Name:               "Google Legacy",
		Description:        "Legacy Google vendor",
		IconKey:            "google-legacy",
		AuditEnabled:       true,
		AuditCaptureBodies: false,
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	geminiVendorID := insertVendor(t, testContext, conn, vendorSeed{
		Key:                "gemini",
		Name:               "Gemini Legacy",
		Description:        "Old Gemini vendor",
		IconKey:            "gemini-legacy",
		AuditEnabled:       true,
		AuditCaptureBodies: false,
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	insertModelConfig(t, testContext, conn, modelConfigSeed{
		ProfileID: profileID,
		VendorID:  sql.NullInt64{Int64: int64(googleVendorID), Valid: true},
		APIFamily: "openai",
		ModelID:   "proxy-google-model",
		ModelType: "proxy",
		IsEnabled: true,
		CreatedAt: now,
		UpdatedAt: now,
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
	if _, ok := vendors["google"]; ok {
		t.Fatalf("expected legacy google vendor row to be merged away")
	}
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
	assertCount(t, testContext, conn, `SELECT COUNT(*) FROM vendors WHERE key = 'google'`, 0)

	var rewiredVendorID int
	if err := conn.QueryRow(testContext, `SELECT vendor_id FROM model_configs WHERE model_id = 'proxy-google-model'`).Scan(&rewiredVendorID); err != nil {
		t.Fatalf("load rewired model_config vendor_id: %v", err)
	}
	if rewiredVendorID != geminiVendorID {
		t.Fatalf("expected model_configs.vendor_id to rewire from legacy google id %d to gemini id %d, got %d", googleVendorID, geminiVendorID, rewiredVendorID)
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

func TestStartupReconciliation(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, testContext, "startup_reconciliation")
	defer conn.Close(testContext)

	runner := newRunner(t)
	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("apply baseline before reconciliation test: %v", err)
	}

	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	profileID := insertProfile(t, testContext, conn, profileSeed{
		Name:      "Reconciliation Profile",
		IsActive:  true,
		IsDefault: false,
		Version:   0,
		CreatedAt: now,
		UpdatedAt: now,
	})
	insertRequestLog(t, testContext, conn, requestLogSeed{
		ProfileID:        profileID,
		IngressRequestID: "req-reconcile-1",
		AttemptNumber:    sql.NullInt32{Int32: 2, Valid: true},
		ModelID:          "gpt-5.4",
		APIFamily:        "openai",
		StatusCode:       200,
		ResponseTimeMS:   123,
		IsStream:         false,
		SuccessFlag:      sql.NullBool{Bool: true, Valid: true},
		BillableFlag:     sql.NullBool{Bool: true, Valid: true},
		PricedFlag:       sql.NullBool{Bool: false, Valid: true},
		UnpricedReason:   sql.NullString{String: "MATCHED_PENDING_PRICE", Valid: true},
		RequestPath:      "/v1/chat/completions",
		CreatedAt:        now,
	})
	insertUsageRequestEvent(t, testContext, conn, usageRequestEventSeed{
		ProfileID:        profileID,
		IngressRequestID: "req-reconcile-1",
		ModelID:          "gpt-5.4",
		APIFamily:        "openai",
		StatusCode:       200,
		SuccessFlag:      true,
		AttemptCount:     2,
		RequestPath:      "/v1/chat/completions",
		CreatedAt:        now,
	})

	observedSteps := make([]startup.Step, 0, 9)
	service := newStartupService(t, harness.connectionString("startup_reconciliation"), func(step startup.Step) {
		observedSteps = append(observedSteps, step)
	})

	firstResult, err := service.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("run startup sequence with pending usage-event rows: %v", err)
	}

	assertStartupStepOrder(t, firstResult)
	assertObservedStepOrder(t, observedSteps)
	if !firstResult.BillingReconciliation.Ran {
		t.Fatalf("expected billing reconciliation to run when pending rows exist")
	}
	if firstResult.BillingReconciliation.PendingRowCount != 1 {
		t.Fatalf("expected one pending usage-event row, got %d", firstResult.BillingReconciliation.PendingRowCount)
	}
	if firstResult.BillingReconciliation.MatchedRequestLogCount != 1 || firstResult.BillingReconciliation.UnmatchedUsageEventCount != 0 || firstResult.BillingReconciliation.DuplicateCandidateCount != 0 {
		t.Fatalf("unexpected billing reconciliation summary: %+v", firstResult.BillingReconciliation)
	}

	var billableFlag bool
	var pricedFlag bool
	var unpricedReason sql.NullString
	if err := conn.QueryRow(
		testContext,
		`SELECT billable_flag, priced_flag, unpriced_reason FROM usage_request_events WHERE ingress_request_id = 'req-reconcile-1'`,
	).Scan(&billableFlag, &pricedFlag, &unpricedReason); err != nil {
		t.Fatalf("load reconciled usage_request_event row: %v", err)
	}
	if !billableFlag || pricedFlag || !unpricedReason.Valid || unpricedReason.String != "MATCHED_PENDING_PRICE" {
		t.Fatalf("expected reconciled usage_request_event row to match request_log billing fields, got billable=%v priced=%v unpriced_reason=%v", billableFlag, pricedFlag, unpricedReason)
	}
	assertCount(t, testContext, conn, `SELECT COUNT(*) FROM vendors`, len(startup.DefaultVendors))
	assertCount(t, testContext, conn, `SELECT COUNT(*) FROM app_auth_settings`, 1)

	observedSteps = observedSteps[:0]
	secondResult, err := service.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("rerun startup sequence after reconciliation cleared pending rows: %v", err)
	}
	assertStartupStepOrder(t, secondResult)
	assertObservedStepOrder(t, observedSteps)
	if secondResult.BillingReconciliation.Ran || secondResult.BillingReconciliation.PendingRowCount != 0 {
		t.Fatalf("expected billing reconciliation to skip once pending rows are cleared, got %+v", secondResult.BillingReconciliation)
	}
}

func TestStartupIdempotency(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, testContext, "startup_idempotency")
	defer conn.Close(testContext)

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
	if firstResult.BillingReconciliation.Ran {
		t.Fatalf("expected no billing reconciliation work in idempotency setup, got %+v", firstResult.BillingReconciliation)
	}

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
	if secondResult.BillingReconciliation.Ran {
		t.Fatalf("expected second startup run to skip billing reconciliation, got %+v", secondResult.BillingReconciliation)
	}

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
	ProfileID int
	VendorID  sql.NullInt64
	APIFamily string
	ModelID   string
	ModelType string
	IsEnabled bool
	CreatedAt time.Time
	UpdatedAt time.Time
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

type requestLogSeed struct {
	ProfileID        int
	IngressRequestID string
	AttemptNumber    sql.NullInt32
	ModelID          string
	APIFamily        string
	StatusCode       int
	ResponseTimeMS   int
	IsStream         bool
	SuccessFlag      sql.NullBool
	BillableFlag     sql.NullBool
	PricedFlag       sql.NullBool
	UnpricedReason   sql.NullString
	RequestPath      string
	CreatedAt        time.Time
}

type usageRequestEventSeed struct {
	ProfileID        int
	IngressRequestID string
	ModelID          string
	APIFamily        string
	StatusCode       int
	SuccessFlag      bool
	AttemptCount     int
	RequestPath      string
	CreatedAt        time.Time
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

type appAuthSnapshot struct {
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

type startupStateSnapshot struct {
	Profiles             []profileSnapshot             `json:"profiles"`
	UserSettings         []userSettingSnapshot         `json:"user_settings"`
	AppAuthSettings      []appAuthSnapshot             `json:"app_auth_settings"`
	Vendors              []vendorSnapshot              `json:"vendors"`
	HeaderBlocklistRules []systemHeaderRuleSnapshot    `json:"header_blocklist_rules"`
	UserAgentClientRules []systemUserAgentRuleSnapshot `json:"user_agent_client_rules"`
	Endpoints            []endpointSnapshot            `json:"endpoints"`
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

func assertStartupStepOrder(t *testing.T, result startup.Result) {
	t.Helper()
	assertObservedStepOrder(t, result.ExecutedSteps)
}

func assertObservedStepOrder(t *testing.T, steps []startup.Step) {
	t.Helper()
	want := []startup.Step{
		startup.StepMigrations,
		startup.StepUsageEventBillingReconcile,
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
			is_enabled,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`,
		seed.ProfileID,
		nullInt64(seed.VendorID),
		seed.APIFamily,
		seed.ModelID,
		nil,
		seed.ModelType,
		nil,
		seed.IsEnabled,
		seed.CreatedAt,
		seed.UpdatedAt,
	).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert model_config %q: %v", seed.ModelID, err)
	}
	return modelConfigID
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

func insertRequestLog(t *testing.T, ctx context.Context, conn *pgx.Conn, seed requestLogSeed) {
	t.Helper()
	if _, err := conn.Exec(
		ctx,
		`INSERT INTO request_logs (
			profile_id,
			model_id,
			api_family,
			ingress_request_id,
			attempt_number,
			status_code,
			response_time_ms,
			is_stream,
			success_flag,
			billable_flag,
			priced_flag,
			unpriced_reason,
			request_path,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14
		)`,
		seed.ProfileID,
		seed.ModelID,
		seed.APIFamily,
		seed.IngressRequestID,
		nullInt32(seed.AttemptNumber),
		seed.StatusCode,
		seed.ResponseTimeMS,
		seed.IsStream,
		nullBool(seed.SuccessFlag),
		nullBool(seed.BillableFlag),
		nullBool(seed.PricedFlag),
		nullString(seed.UnpricedReason),
		seed.RequestPath,
		seed.CreatedAt,
	); err != nil {
		t.Fatalf("insert request_log %q: %v", seed.IngressRequestID, err)
	}
}

func insertUsageRequestEvent(t *testing.T, ctx context.Context, conn *pgx.Conn, seed usageRequestEventSeed) {
	t.Helper()
	if _, err := conn.Exec(
		ctx,
		`INSERT INTO usage_request_events (
			profile_id,
			ingress_request_id,
			model_id,
			api_family,
			status_code,
			success_flag,
			attempt_count,
			request_path,
			created_at,
			billable_flag,
			priced_flag,
			unpriced_reason
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12
		)`,
		seed.ProfileID,
		seed.IngressRequestID,
		seed.ModelID,
		seed.APIFamily,
		seed.StatusCode,
		seed.SuccessFlag,
		seed.AttemptCount,
		seed.RequestPath,
		seed.CreatedAt,
		nil,
		nil,
		nil,
	); err != nil {
		t.Fatalf("insert usage_request_event %q: %v", seed.IngressRequestID, err)
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
		AppAuthSettings:      loadAppAuthSnapshots(t, ctx, conn),
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

func loadAppAuthSnapshots(t *testing.T, ctx context.Context, conn *pgx.Conn) []appAuthSnapshot {
	t.Helper()
	rows, err := conn.Query(
		ctx,
		`SELECT singleton_key, auth_enabled, email_verification_attempt_count, must_change_password, token_version, created_at, updated_at FROM app_auth_settings ORDER BY id ASC`,
	)
	if err != nil {
		t.Fatalf("query app_auth_settings snapshots: %v", err)
	}
	defer rows.Close()

	items := []appAuthSnapshot{}
	for rows.Next() {
		var item appAuthSnapshot
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

func queryRows(t *testing.T, ctx context.Context, conn *pgx.Conn, query string) [][]any {
	t.Helper()
	rows, err := conn.Query(ctx, query)
	if err != nil {
		t.Fatalf("query rows %q: %v", query, err)
	}
	defer rows.Close()

	values := [][]any{}
	for rows.Next() {
		rowValues, err := rows.Values()
		if err != nil {
			t.Fatalf("read row values for query %q: %v", query, err)
		}
		values = append(values, rowValues)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rows for query %q: %v", query, err)
	}
	return values
}

func nullBool(value sql.NullBool) any {
	if !value.Valid {
		return nil
	}
	return value.Bool
}

func nullInt32(value sql.NullInt32) any {
	if !value.Valid {
		return nil
	}
	return value.Int32
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

const DefaultProfileDescription = "System default profile"
