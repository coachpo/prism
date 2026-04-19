package startup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

const (
	SkipStartupSequenceEnv          = "PRISM_SKIP_STARTUP_SEQUENCE"
	DefaultProfileName              = "Default"
	DefaultProfileDescription       = "System default profile"
	AppAuthSingletonKey             = "app"
	MissingRequestLogBackfillReason = "MISSING_REQUEST_LOG_BACKFILL"
)

type Step string

const (
	StepMigrations                  Step = "migrations"
	StepUsageEventBillingReconcile  Step = "usage_event_billing_reconcile"
	StepVendorSeed                  Step = "vendor_seed"
	StepProfileInvariantSeed        Step = "profile_invariant_seed"
	StepUserSettingsSeed            Step = "user_settings_seed"
	StepUserAgentClientRuleSeed     Step = "user_agent_client_rule_seed"
	StepAppAuthSettingsSeed         Step = "app_auth_settings_seed"
	StepEndpointSecretNormalization Step = "endpoint_secret_normalization"
	StepHeaderBlocklistRuleSeed     Step = "header_blocklist_rule_seed"
)

type BillingReconciliationResult struct {
	PendingRowCount          int
	Ran                      bool
	MatchedRequestLogCount   int
	UnmatchedUsageEventCount int
	DuplicateCandidateCount  int
}

type Result struct {
	Skipped               bool
	ExecutedSteps         []Step
	Migration             migrate.Result
	BillingReconciliation BillingReconciliationResult
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
	if os.Getenv(SkipStartupSequenceEnv) == "1" {
		return Result{Skipped: true}, nil
	}
	if s.databaseURL == "" {
		return Result{}, fmt.Errorf("database URL is required")
	}

	conn, err := pgx.Connect(ctx, s.databaseURL)
	if err != nil {
		return Result{}, fmt.Errorf("connect startup database: %w", err)
	}
	defer conn.Close(ctx)

	return s.RunWithConn(ctx, conn)
}

func (s Service) RunWithConn(ctx context.Context, conn *pgx.Conn) (Result, error) {
	if os.Getenv(SkipStartupSequenceEnv) == "1" {
		return Result{Skipped: true}, nil
	}
	if strings.TrimSpace(s.secretEncryptionKey) == "" {
		return Result{}, fmt.Errorf("secret encryption key is required")
	}

	result := Result{ExecutedSteps: make([]Step, 0, 9)}

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

	if err := s.runStep(ctx, &result, StepUsageEventBillingReconcile, func() error {
		reconciliation, err := s.reconcileUsageRequestEventBillingFields(ctx, conn)
		if err != nil {
			return err
		}
		result.BillingReconciliation = reconciliation
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

func withTransaction(ctx context.Context, conn *pgx.Conn, run func(pgx.Tx) error) error {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin startup transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := run(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit startup transaction: %w", err)
	}

	return nil
}
