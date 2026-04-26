package startup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

const (
	DefaultProfileName        = "Default"
	DefaultProfileDescription = "System default profile"
	AppAuthSingletonKey       = "app"
)

type Step string

const (
	StepMigrations                  Step = "migrations"
	StepVendorSeed                  Step = "vendor_seed"
	StepProfileInvariantSeed        Step = "profile_invariant_seed"
	StepUserSettingsSeed            Step = "user_settings_seed"
	StepUserAgentClientRuleSeed     Step = "user_agent_client_rule_seed"
	StepAppAuthSettingsSeed         Step = "app_auth_settings_seed"
	StepEndpointSecretNormalization Step = "endpoint_secret_normalization"
	StepHeaderBlocklistRuleSeed     Step = "header_blocklist_rule_seed"
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

	for _, step := range []struct {
		name Step
		run  func(context.Context, *pgx.Conn) error
	}{
		{name: StepVendorSeed, run: s.seedVendors},
		{name: StepProfileInvariantSeed, run: s.seedProfileInvariants},
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

func (s Service) timestamp() time.Time {
	return s.now().UTC()
}
