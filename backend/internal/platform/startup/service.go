package startup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/openaimodecheck"
	"github.com/coachpo/prism/backend/internal/platform/migrate"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

const (
	DefaultProfileName        = "Default"
	DefaultProfileDescription = "System default profile"
	AppAuthSingletonKey       = "app"
)

type Step string

const (
	StepMigrations                   Step = "migrations"
	StepPricingSchemaTransitionCheck Step = "pricing_schema_transition_check"
	StepProfileInvariantSeed         Step = "profile_invariant_seed"
	StepStrategyDefaultSeed          Step = "strategy_default_seed"
	StepUserSettingsSeed             Step = "user_settings_seed"
	StepUserAgentClientRuleSeed      Step = "user_agent_client_rule_seed"
	StepAppAuthSettingsSeed          Step = "app_auth_settings_seed"
	StepEndpointSecretNormalization  Step = "endpoint_secret_normalization"
	StepHeaderBlocklistRuleSeed      Step = "header_blocklist_rule_seed"
	StepOpenAITextModeCheck          Step = "openai_text_mode_check"
	StepObservabilityUpgrade         Step = "observability_v2_upgrade"
	StepLegacyRetentionCutover       Step = "legacy_retention_cutover"
	StepProfileAuditSettingsSeed     Step = "profile_audit_settings_seed"
	StepSettingsSchemaFinalize       Step = "settings_schema_finalize"
)

type Result struct {
	Skipped       bool
	ExecutedSteps []Step
	Migration     migrate.Result
}

type Options struct {
	DatabaseURL         string
	SecretEncryptionKey string
	MigrationsDir       string
	TimeNow             func() time.Time
	StepObserver        func(Step)
}

type Service struct {
	databaseURL         string
	secretEncryptionKey string
	migrateRunner       migrate.Runner
	now                 func() time.Time
	stepObserver        func(Step)
}

type queryExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, arguments ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row
}

func New(options Options) (Service, error) {
	migrateRunner, err := migrate.New(migrate.Options{
		MigrationsDir: options.MigrationsDir,
	})
	if err != nil {
		return Service{}, fmt.Errorf("build startup migration runner: %w", err)
	}

	now := options.TimeNow
	if now == nil {
		now = time.Now
	}

	return Service{
		databaseURL:         strings.TrimSpace(options.DatabaseURL),
		secretEncryptionKey: options.SecretEncryptionKey,
		migrateRunner:       migrateRunner,
		now:                 now,
		stepObserver:        options.StepObserver,
	}, nil
}

func (s Service) Run(ctx context.Context) (Result, error) {
	if s.databaseURL == "" {
		return Result{}, fmt.Errorf("database URL is required")
	}

	conn, err := pgx.Connect(ctx, s.databaseURL)
	if err != nil {
		return Result{}, fmt.Errorf("connect startup database: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	return s.RunWithConn(ctx, conn)
}

func (s Service) RunWithConn(ctx context.Context, conn *pgx.Conn) (Result, error) {
	if strings.TrimSpace(s.secretEncryptionKey) == "" {
		return Result{}, fmt.Errorf("secret encryption key is required")
	}

	result := Result{ExecutedSteps: make([]Step, 0, 8)}

	if err := s.runStep(ctx, &result, StepMigrations, func() error {
		migrationResult, err := s.migrateRunner.Run(ctx, conn)
		if err != nil {
			return err
		}
		result.Migration = migrationResult
		return nil
	}); err != nil {
		return result, err
	}
	if err := s.runStep(ctx, &result, StepPricingSchemaTransitionCheck, func() error {
		return s.checkPricingSchemaTransition(ctx, conn)
	}); err != nil {
		return result, err
	}
	if err := s.runStep(ctx, &result, StepObservabilityUpgrade, func() error {
		return s.runObservabilityUpgrade(ctx, conn)
	}); err != nil {
		return result, err
	}
	if err := s.runStep(ctx, &result, StepOpenAITextModeCheck, func() error {
		return s.checkOpenAITextModeEquality(ctx, conn)
	}); err != nil {
		return result, err
	}
	if err := s.runStep(ctx, &result, StepLegacyRetentionCutover, func() error {
		return s.runLegacyRetentionCutover(ctx, conn)
	}); err != nil {
		return result, err
	}
	for _, step := range []struct {
		name Step
		run  func(context.Context, *pgx.Conn) error
	}{
		{name: StepProfileInvariantSeed, run: s.seedProfileInvariants},
		{name: StepProfileAuditSettingsSeed, run: s.seedProfileAuditSettings},
		{name: StepStrategyDefaultSeed, run: s.seedStrategyDefaults},
		{name: StepUserSettingsSeed, run: s.seedUserSettings},
		{name: StepUserAgentClientRuleSeed, run: s.seedUserAgentClientRules},
		{name: StepAppAuthSettingsSeed, run: s.seedAppAuthSettings},
		{name: StepEndpointSecretNormalization, run: s.normalizeEndpointSecrets},
		{name: StepHeaderBlocklistRuleSeed, run: s.seedHeaderBlocklistRules},
	} {
		if err := s.runStep(ctx, &result, step.name, func() error {
			return step.run(ctx, conn)
		}); err != nil {
			return result, err
		}
	}
	// The explicit finalizer must run after fresh profile and auth seeds. That
	// keeps the additive migration lossless for existing rows and lets the
	// finalizer validate the complete fresh audit/auth population before it
	// retires the duplicated legacy columns.
	if err := s.runStep(ctx, &result, StepSettingsSchemaFinalize, func() error {
		// Staged migration-dir tests apply prefixes without 000015; the
		// settings transition table simply does not exist there and the
		// legacy retention cutover and finalizer steps are both no-ops.
		var transitionExists bool
		if err := conn.QueryRow(ctx, `SELECT to_regclass('public.settings_schema_transition') IS NOT NULL`).Scan(&transitionExists); err != nil {
			return err
		}
		if !transitionExists {
			return nil
		}
		finalizer := newSettingsSchemaFinalizerWithConn(conn)
		if _, err := finalizer.Run(ctx); err != nil {
			return err
		}
		return s.seedRetentionCoverage(ctx, conn)
	}); err != nil {
		return result, err
	}

	return result, nil
}

func (s Service) runStep(ctx context.Context, result *Result, step Step, run func() error) error {
	_ = ctx
	result.ExecutedSteps = append(result.ExecutedSteps, step)
	if s.stepObserver != nil {
		s.stepObserver(step)
	}
	return run()
}

func (s Service) checkOpenAITextModeEquality(ctx context.Context, conn *pgx.Conn) error {
	report, err := openaimodecheck.Check(ctx, conn, profiledomain.DefaultProfileID)
	if err != nil {
		return fmt.Errorf("openai text mode equality check: %w", err)
	}
	if len(report.Violations) > 0 {
		return fmt.Errorf("openai text mode equality check failed: %s", report.Summary())
	}
	return nil
}

// checkPricingSchemaTransition verifies the schema-global pricing transition
// singleton after migrations (SPEC 6.3.3): exactly one row, phase=final,
// generation lease acquisition closed and no active generation lease. Any
// violation fails startup closed; the schema can never roll back.
func (s Service) checkPricingSchemaTransition(ctx context.Context, conn *pgx.Conn) error {
	var rowCount int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM pricing_schema_transition_state`).Scan(&rowCount); err != nil {
		return fmt.Errorf("load pricing schema transition state: %w", err)
	}
	if rowCount != 1 {
		return fmt.Errorf("pricing schema transition singleton must contain exactly one row, found %d", rowCount)
	}

	var phase string
	var acquisitionOpen bool
	var schemaGeneration int64
	if err := conn.QueryRow(ctx, `SELECT phase, lease_acquisition_open, schema_generation FROM pricing_schema_transition_state WHERE id = 1`).Scan(&phase, &acquisitionOpen, &schemaGeneration); err != nil {
		return fmt.Errorf("load pricing schema transition singleton: %w", err)
	}
	if phase != "final" {
		return fmt.Errorf("pricing schema transition phase must be final, found %q", phase)
	}
	if acquisitionOpen {
		return fmt.Errorf("pricing schema transition generation lease acquisition must be closed in final phase")
	}
	if schemaGeneration < 1 {
		return fmt.Errorf("pricing schema transition generation must be >= 1, found %d", schemaGeneration)
	}

	var activeLeases int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM pricing_schema_generation_leases WHERE released_at IS NULL`).Scan(&activeLeases); err != nil {
		return fmt.Errorf("count active pricing schema generation leases: %w", err)
	}
	if activeLeases != 0 {
		return fmt.Errorf("pricing schema transition final phase requires zero active generation leases, found %d", activeLeases)
	}
	return nil
}

func (s Service) timestamp() time.Time {
	return s.now().UTC()
}
