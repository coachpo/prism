package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/coachpo/prism/backend/internal/platform/priority"
)

type multiFlag []string

type commandOptions struct {
	checks   multiFlag
	strict   bool
	format   string
	root     string
	patterns []string
}

type violation struct {
	Check  string `json:"check"`
	Code   string `json:"code"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type checkResult struct {
	Name       string      `json:"name"`
	Violations []violation `json:"violations"`
}

type checkSummary struct {
	Strict               bool           `json:"strict"`
	Format               string         `json:"format"`
	ChecksRun            int            `json:"checks_run"`
	ProductionViolations int            `json:"production_violations"`
	ViolationCount       int            `json:"violation_count"`
	ClassificationCounts map[string]int `json:"classification_counts"`
}

type commandResult struct {
	Summary    checkSummary  `json:"summary"`
	Checks     []checkResult `json:"checks"`
	Violations []violation   `json:"violations"`
}

type checkFunc func() ([]violation, error)

var strictChecks = []string{
	"direct-db",
	"unmanaged-goroutine",
	"unregistered-background-work",
	"direct-telemetry-export",
	"management-sideeffects",
	"direct-email",
	"direct-cache-mutation",
	"unclassified-side-effect",
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("priority check failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	options, err := parseOptions(args)
	if err != nil {
		return err
	}
	if len(options.patterns) == 0 {
		return errors.New("at least one package pattern is required")
	}
	if len(options.checks) == 0 && !options.strict {
		return errors.New("at least one --check is required")
	}
	if options.root != "" {
		if err := os.Chdir(options.root); err != nil {
			return fmt.Errorf("change checker root: %w", err)
		}
	}
	checks := append([]string(nil), options.checks...)
	if options.strict && len(checks) == 0 {
		checks = append(checks, strictChecks...)
	}
	result := commandResult{
		Summary: checkSummary{
			Strict:               options.strict,
			Format:               options.format,
			ClassificationCounts: inventoryClassificationCounts(),
		},
		Violations: []violation{},
	}
	registry := checkRegistry()
	for _, check := range checks {
		fn, ok := registry[check]
		if !ok {
			return fmt.Errorf("unsupported check %q", check)
		}
		violations, err := fn()
		if err != nil {
			return err
		}
		for idx := range violations {
			violations[idx].Check = check
			if violations[idx].Code == "" {
				violations[idx].Code = check
			}
		}
		result.Checks = append(result.Checks, checkResult{Name: check, Violations: violations})
		result.Violations = append(result.Violations, violations...)
		result.Summary.ClassificationCounts["checks:"+check]++
	}
	result.Summary.ChecksRun = len(result.Checks)
	result.Summary.ViolationCount = len(result.Violations)
	result.Summary.ProductionViolations = len(result.Violations)
	if options.format == "json" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return err
		}
	} else {
		if len(result.Violations) == 0 {
			_, _ = fmt.Fprintln(os.Stdout, "priority checks passed")
		}
	}
	if len(result.Violations) > 0 {
		return fmt.Errorf("priority checks failed: %s", formatViolations(result.Violations))
	}
	return nil
}

func checkRegistry() map[string]checkFunc {
	return map[string]checkFunc{
		"direct-db":                    runDirectDBCheck,
		"unmanaged-goroutine":          runUnmanagedGoroutineCheck,
		"unregistered-background-work": runUnregisteredBackgroundWorkCheck,
		"direct-telemetry-export":      runDirectTelemetryExportCheck,
		"management-sideeffects":       runManagementSideEffectsCheck,
		"direct-email":                 runDirectEmailCheck,
		"direct-cache-mutation":        runDirectCacheMutationCheck,
		"unclassified-side-effect":     runUnclassifiedSideEffectCheck,
	}
}

func inventoryClassificationCounts() map[string]int {
	inventory := priority.DefaultInventory()
	return map[string]int{
		"routes":               len(inventory.Routes),
		"resources":            len(inventory.Resources),
		"jobs":                 len(inventory.Jobs),
		"classified_routes":    countClassifiedRoutes(inventory.Routes),
		"classified_resources": countClassifiedResources(inventory.Resources),
		"classified_jobs":      countClassifiedJobs(inventory.Jobs),
	}
}

func countClassifiedRoutes(entries []priority.RouteInventoryEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.Classified {
			count++
		}
	}
	return count
}

func countClassifiedResources(entries []priority.ResourceInventoryEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.Classified {
			count++
		}
	}
	return count
}

func countClassifiedJobs(entries []priority.JobInventoryEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.Classified {
			count++
		}
	}
	return count
}

func runUnmanagedGoroutineCheck() ([]violation, error) {
	violations := []violation{}
	if err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor", "node_modules", "tests":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		slashPath := filepath.ToSlash(path)
		if allowedUnmanagedGoroutinePath(slashPath) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(raw)
		if strings.Contains(content, "go func") || strings.Contains(content, "time.NewTicker(") || strings.Contains(content, "time.NewTimer(") {
			violations = append(violations, violation{Path: slashPath, Reason: "recurring or delayed background work must be owned by internal/platform/background"})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return violations, nil
}

func allowedUnmanagedGoroutinePath(path string) bool {
	if strings.HasPrefix(path, "internal/platform/background/") || path == "cmd/prism-priority-check/main.go" {
		return true
	}
	return slices.Contains([]string{
		"internal/httpapi/runtime/runtime.go",
		"internal/platform/email/mailer.go",
		"internal/platform/config/bootstrap.go",
	}, path)
}

func runUnregisteredBackgroundWorkCheck() ([]violation, error) {
	violations := []violation{}
	serverRaw, err := os.ReadFile(filepath.FromSlash("internal/platform/http/server.go"))
	if err != nil {
		return nil, err
	}
	serverContent := string(serverRaw)
	for _, want := range []string{
		"background.NewScheduler(background.Config{})",
		"runtimePlanningCache.RegisterBackgroundWorker",
		"managementAuthService.RegisterBackgroundWorkers",
		"asyncDashboardPublisher.RegisterBackgroundWorker",
		"runtimeService.RegisterBackgroundWorkers",
		"emailOutbox.RegisterBackgroundWorker",
		"backgroundScheduler.Start(context.Background())",
		"backgroundScheduler.Stop(ctx, time.Now().Add(5*time.Second))",
	} {
		if !strings.Contains(serverContent, want) {
			violations = append(violations, violation{Path: "internal/platform/http/server.go", Reason: "missing scheduler composition point " + want})
		}
	}
	checks := map[string][]string{
		"internal/httpapi/runtime/cache.go":                          {"RegisterBackgroundWorker", "runtime_shared_cache_refresh", "handleScheduledRefresh"},
		"internal/httpapi/runtime/telemetry_outbox.go":               {"RegisterBackgroundWorker", "runtime_telemetry_outbox", "handleScheduledTelemetry"},
		"internal/httpapi/runtime/runtime_side_effects.go":           {"RegisterBackgroundWorker", "runtime_side_effects_activity", "handleRuntimeActivity"},
		"internal/httpapi/runtime/feedback_pipeline.go":              {"RegisterBackgroundWorker", "runtime_feedback_pipeline", "handleScheduledFeedback"},
		"internal/httpapi/realtime/async_publisher.go":               {"RegisterBackgroundWorker", "async_dashboard_publisher", "handleScheduledPublish"},
		"internal/httpapi/management/auth/proxy_key_usage_writer.go": {"RegisterBackgroundWorker", "proxy_key_usage_writer", "handleScheduledFlush"},
		"internal/platform/managementsideeffects/outbox.go":          {"RegisterBackgroundWorker", "management_side_effect_outbox", "handleScheduledDispatch"},
		"internal/platform/email/outbox/outbox.go":                   {"RegisterBackgroundWorker", "email_outbox_worker", "handleScheduledSend", "FOR UPDATE SKIP LOCKED"},
	}
	for path, required := range checks {
		raw, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			return nil, err
		}
		content := string(raw)
		for _, want := range required {
			if !strings.Contains(content, want) {
				violations = append(violations, violation{Path: path, Reason: "missing registered background worker evidence " + want})
			}
		}
		for _, forbidden := range []string{"go ", "time.NewTicker(", "time.NewTimer("} {
			if strings.Contains(content, forbidden) {
				violations = append(violations, violation{Path: path, Reason: "legacy worker loop remains: " + forbidden})
			}
		}
	}
	return violations, nil
}

func runDirectTelemetryExportCheck() ([]violation, error) {
	violations := []violation{}
	observabilityRaw, err := os.ReadFile(filepath.FromSlash("internal/httpapi/runtime/observability.go"))
	if err != nil {
		return nil, err
	}
	observabilityContent := string(observabilityRaw)
	for _, forbidden := range []string{
		"telemetryOutbox.Enqueue",
		"context.WithTimeout(context.Background(), 5*time.Second)",
		"context.WithTimeout(context.Background(), 5 * time.Second)",
	} {
		if strings.Contains(observabilityContent, forbidden) {
			violations = append(violations, violation{Path: "internal/httpapi/runtime/observability.go", Reason: "runtime finalizer directly persists telemetry: " + forbidden})
		}
	}
	for _, want := range []string{
		"RuntimeActivityIntent",
		"SubmitRuntimeActivity",
		"RuntimeSideEffectAccepted",
	} {
		if !strings.Contains(observabilityContent, want) {
			violations = append(violations, violation{Path: "internal/httpapi/runtime/observability.go", Reason: "missing runtime activity side-effect submission evidence " + want})
		}
	}
	sideEffectsRaw, err := os.ReadFile(filepath.FromSlash("internal/httpapi/runtime/runtime_side_effects.go"))
	if err != nil {
		return nil, err
	}
	sideEffectsContent := string(sideEffectsRaw)
	for _, want := range []string{
		"runtime_side_effects_activity",
		"RegisterBackgroundWorker",
		"handleRuntimeActivity",
		"outbox.Enqueue",
	} {
		if !strings.Contains(sideEffectsContent, want) {
			violations = append(violations, violation{Path: "internal/httpapi/runtime/runtime_side_effects.go", Reason: "missing scheduler-owned telemetry side-effect evidence " + want})
		}
	}
	runtimeRaw, err := os.ReadFile(filepath.FromSlash("internal/httpapi/runtime/runtime.go"))
	if err != nil {
		return nil, err
	}
	runtimeContent := string(runtimeRaw)
	for _, forbidden := range []string{
		"runtimeFeedbackContext",
		"persist runtime success feedback",
		"persist runtime failure feedback",
		"persist runtime transport failure",
		"persist runtime probe-eligible feedback",
	} {
		if strings.Contains(runtimeContent, forbidden) {
			violations = append(violations, violation{Path: "internal/httpapi/runtime/runtime.go", Reason: "legacy synchronous runtime feedback remains: " + forbidden})
		}
	}
	feedbackRaw, err := os.ReadFile(filepath.FromSlash("internal/httpapi/runtime/feedback_pipeline.go"))
	if err != nil {
		return nil, err
	}
	feedbackContent := string(feedbackRaw)
	for _, want := range []string{
		"runtime_feedback",
		"TryEnqueue",
		"runtime_feedback_pipeline",
		"handleScheduledFeedback",
	} {
		if !strings.Contains(feedbackContent, want) {
			violations = append(violations, violation{Path: "internal/httpapi/runtime/feedback_pipeline.go", Reason: "missing bounded feedback pipeline evidence " + want})
		}
	}
	return violations, nil
}

func runDirectEmailCheck() ([]violation, error) {
	violations := []violation{}
	serverRaw, err := os.ReadFile(filepath.FromSlash("internal/platform/http/server.go"))
	if err != nil {
		return nil, err
	}
	serverContent := string(serverRaw)
	for _, want := range []string{
		"outbox.NewStore",
		"EmailOutbox: emailOutbox",
		"emailOutbox.RegisterBackgroundWorker",
		"backgroundJobsPool",
	} {
		if !strings.Contains(serverContent, want) {
			violations = append(violations, violation{Path: "internal/platform/http/server.go", Reason: "missing scheduler-owned email outbox wiring " + want})
		}
	}
	authRaw, err := os.ReadFile(filepath.FromSlash("internal/httpapi/management/auth/routes.go"))
	if err != nil {
		return nil, err
	}
	authContent := string(authRaw)
	for _, forbidden := range []string{
		"SendEmailVerificationOTP(",
		"SendPasswordResetEmail(",
		"smtp.",
		"NewSMTPMailer",
	} {
		if strings.Contains(authContent, forbidden) {
			violations = append(violations, violation{Path: "internal/httpapi/management/auth/routes.go", Reason: "auth request path contains direct email send evidence " + forbidden})
		}
	}
	for _, want := range []string{"enqueueAuthEmail", "outbox.KindEmailVerificationOTP", "outbox.KindPasswordReset"} {
		if !strings.Contains(authContent, want) {
			violations = append(violations, violation{Path: "internal/httpapi/management/auth/routes.go", Reason: "missing auth email outbox enqueue evidence " + want})
		}
	}
	outboxRaw, err := os.ReadFile(filepath.FromSlash("internal/platform/email/outbox/outbox.go"))
	if err != nil {
		return nil, err
	}
	outboxContent := string(outboxRaw)
	for _, want := range []string{"email_outbox_worker", "FOR UPDATE SKIP LOCKED", "email_secret_ciphertext", "backoffForAttempt", "sanitizeError", "RegisterBackgroundWorker"} {
		if !strings.Contains(outboxContent, want) {
			violations = append(violations, violation{Path: "internal/platform/email/outbox/outbox.go", Reason: "missing durable email outbox worker evidence " + want})
		}
	}
	migrationRaw, err := os.ReadFile(filepath.FromSlash("migrations/000008_email_outbox.sql"))
	if err != nil {
		return nil, err
	}
	migrationContent := string(migrationRaw)
	for _, want := range []string{"email_outbox", "idx_email_outbox_idempotency_key", "idx_email_outbox_due", "idx_email_outbox_stale_locks", "idx_email_outbox_dead_letters"} {
		if !strings.Contains(migrationContent, want) {
			violations = append(violations, violation{Path: "migrations/000008_email_outbox.sql", Reason: "missing email outbox schema evidence " + want})
		}
	}
	return violations, nil
}

func runManagementSideEffectsCheck() ([]violation, error) {
	violations := []violation{}
	serverRaw, err := os.ReadFile(filepath.FromSlash("internal/platform/http/server.go"))
	if err != nil {
		return nil, err
	}
	serverContent := string(serverRaw)
	for _, want := range []string{
		"managementsideeffects.NewDispatcher",
		"managementSideEffects.RegisterBackgroundWorker",
		"SideEffects: managementSideEffects",
	} {
		if !strings.Contains(serverContent, want) {
			violations = append(violations, violation{Path: "internal/platform/http/server.go", Reason: "missing scheduler-owned management side-effect wiring " + want})
		}
	}
	outboxRaw, err := os.ReadFile(filepath.FromSlash("internal/platform/managementsideeffects/outbox.go"))
	if err != nil {
		return nil, err
	}
	outboxContent := string(outboxRaw)
	for _, want := range []string{
		"management_side_effect_outbox",
		"EventDashboardSnapshotInvalidate",
		"FOR UPDATE SKIP LOCKED",
		"failed_permanent",
		"defaultBatchSize       = 50",
		"defaultConcurrency     = 4",
		"defaultRetryCap        = 8",
		"background.PriorityNormalBackground",
	} {
		if !strings.Contains(outboxContent, want) {
			violations = append(violations, violation{Path: "internal/platform/managementsideeffects/outbox.go", Reason: "missing durable management side-effect dispatcher evidence " + want})
		}
	}
	statsRaw, err := os.ReadFile(filepath.FromSlash("internal/httpapi/management/stats/service.go"))
	if err != nil {
		return nil, err
	}
	statsContent := string(statsRaw)
	for _, forbidden := range []string{
		"s.invalidateDashboardAggregateSnapshot(profileID)",
	} {
		if strings.Contains(statsContent, forbidden) {
			violations = append(violations, violation{Path: "internal/httpapi/management/stats/service.go", Reason: "dashboard invalidation remains inline after transaction: " + forbidden})
		}
	}
	for _, want := range []string{
		"enqueueDashboardInvalidation",
		"managementsideeffects.InsertTx",
		"managementsideeffects.AfterCommit",
		"EventDashboardSnapshotInvalidate",
	} {
		if !strings.Contains(statsContent, want) {
			violations = append(violations, violation{Path: "internal/httpapi/management/stats/service.go", Reason: "missing after-commit dashboard invalidation evidence " + want})
		}
	}
	migrationRaw, err := os.ReadFile(filepath.FromSlash("migrations/000007_management_outbox.sql"))
	if err != nil {
		return nil, err
	}
	migrationContent := string(migrationRaw)
	for _, want := range []string{"management_outbox", "idx_management_outbox_dedupe_key", "idx_management_outbox_polling", "failed_permanent"} {
		if !strings.Contains(migrationContent, want) {
			violations = append(violations, violation{Path: "migrations/000007_management_outbox.sql", Reason: "missing management outbox schema evidence " + want})
		}
	}
	return violations, nil
}

func runDirectCacheMutationCheck() ([]violation, error) {
	violations := []violation{}
	middlewarePath := "internal/platform/http/runtime_cache_invalidation.go"
	middlewareRaw, err := os.ReadFile(filepath.FromSlash(middlewarePath))
	if err != nil {
		return nil, err
	}
	middlewareContent := string(middlewareRaw)
	for _, want := range []string{
		"RuntimeGenerationBumpsForRefresh",
		"WithBeforeCommitHook",
		"AdvanceRuntimeCacheGenerations",
		"ScheduleRefresh(request)",
	} {
		if !strings.Contains(middlewareContent, want) {
			violations = append(violations, violation{Path: middlewarePath, Reason: "missing generation-first cache mutation boundary " + want})
		}
	}
	for _, forbidden := range []string{
		"RefreshNow(ctx, request)",
		"failed to publish runtime auth snapshot immediately",
		"else if a.auth && authService != nil",
	} {
		if strings.Contains(middlewareContent, forbidden) {
			violations = append(violations, violation{Path: middlewarePath, Reason: "runtime cache middleware remains authoritative or fallback-driven: " + forbidden})
		}
	}
	authServicePath := "internal/httpapi/management/auth/service.go"
	authServiceRaw, err := os.ReadFile(filepath.FromSlash(authServicePath))
	if err != nil {
		return nil, err
	}
	authServiceContent := string(authServiceRaw)
	for _, forbidden := range []string{
		"context.WithTimeout(context.Background(), 30*time.Second)",
		"runtimeCache.RefreshNow(ctx, runtimeapi.RefreshRequest{Auth: true})",
		"failed to publish runtime auth snapshot immediately",
	} {
		if strings.Contains(authServiceContent, forbidden) {
			violations = append(violations, violation{Path: authServicePath, Reason: "auth service contains direct runtime cache refresh fallback: " + forbidden})
		}
	}
	generationPath := "internal/httpapi/runtime/generations.go"
	generationRaw, err := os.ReadFile(filepath.FromSlash(generationPath))
	if err != nil {
		return nil, err
	}
	generationContent := string(generationRaw)
	for _, want := range []string{"runtime_cache_generations", "ON CONFLICT", "version = runtime_cache_generations.version + 1"} {
		if !strings.Contains(generationContent, want) {
			violations = append(violations, violation{Path: generationPath, Reason: "missing durable cache generation mutation evidence " + want})
		}
	}
	cachePath := "internal/httpapi/runtime/cache.go"
	cacheRaw, err := os.ReadFile(filepath.FromSlash(cachePath))
	if err != nil {
		return nil, err
	}
	cacheContent := string(cacheRaw)
	for _, want := range []string{
		"ReadRuntimeGenerationVector(ctx, tx, DefaultRuntimeGenerationScopes())",
		"ErrRuntimeSnapshotGenerationChanged",
		"LoadFreshRuntimeAuthSettings",
		"LoadFreshRuntimeProxyKeyRecord",
		"runtime_shared_cache_refresh",
	} {
		if !strings.Contains(cacheContent, want) {
			violations = append(violations, violation{Path: cachePath, Reason: "missing freshness-aware runtime cache evidence " + want})
		}
	}
	return violations, nil
}

func runUnclassifiedSideEffectCheck() ([]violation, error) {
	violations := []violation{}
	for _, problem := range priority.DefaultInventory().ValidationProblems() {
		violations = append(violations, violation{Path: problem.Source, Reason: problem.Kind + " " + problem.Name + ": " + problem.Reason})
	}
	admissionPath := "internal/platform/http/admission.go"
	admissionRaw, err := os.ReadFile(filepath.FromSlash(admissionPath))
	if err != nil {
		return nil, err
	}
	admissionContent := string(admissionRaw)
	for _, want := range []string{"managementRouteSpecs", "admission.NewController", "priority.PriorityManagement", "priority.PriorityProxy"} {
		if !strings.Contains(admissionContent, want) {
			violations = append(violations, violation{Path: admissionPath, Reason: "missing route priority classification evidence " + want})
		}
	}
	for _, forbidden := range []string{"defaultManagementTier", "observe-only", "observe only", "missing priority is allowed"} {
		if strings.Contains(admissionContent, forbidden) {
			violations = append(violations, violation{Path: admissionPath, Reason: "observe-only or default priority bypass remains: " + forbidden})
		}
	}
	if err := filepath.WalkDir("internal", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		slashPath := filepath.ToSlash(path)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(raw)
		for _, forbidden := range []string{"observe-only", "observe only", "TODO priority", "missing priority is allowed"} {
			if strings.Contains(strings.ToLower(content), forbidden) {
				violations = append(violations, violation{Path: slashPath, Reason: "unclassified side-effect or priority bypass marker remains: " + forbidden})
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return violations, nil
}

func formatViolations(violations []violation) string {
	parts := make([]string, 0, len(violations))
	for _, item := range violations {
		check := item.Check
		if check == "" {
			check = item.Code
		}
		parts = append(parts, check+" "+item.Path+": "+item.Reason)
	}
	return strings.Join(parts, "; ")
}

func parseOptions(args []string) (commandOptions, error) {
	flags := flag.NewFlagSet("prism-priority-check", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := commandOptions{}
	flags.Var(&options.checks, "check", "priority check to run")
	flags.BoolVar(&options.strict, "strict", false, "run the full production priority enforcement suite")
	flags.StringVar(&options.format, "format", "text", "output format: text or json")
	flags.StringVar(&options.root, "root", "", "root directory to scan")
	if err := flags.Parse(args); err != nil {
		return commandOptions{}, err
	}
	options.format = strings.ToLower(strings.TrimSpace(options.format))
	if options.format == "" {
		options.format = "text"
	}
	if options.format != "text" && options.format != "json" {
		return commandOptions{}, fmt.Errorf("unsupported format %q", options.format)
	}
	options.patterns = flags.Args()
	return options, nil
}

func (f *multiFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func (f *multiFlag) Set(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("check name is required")
	}
	*f = append(*f, trimmed)
	return nil
}

func runDirectDBCheck() ([]violation, error) {
	violations := []violation{}
	if err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		slashPath := filepath.ToSlash(path)
		if slashPath == "internal/platform/db/pools.go" || slashPath == "cmd/prism-priority-check/main.go" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(raw)
		if strings.Contains(content, "pgxpool.New(") || strings.Contains(content, "pgxpool.NewWithConfig(") {
			violations = append(violations, violation{Path: slashPath, Reason: "direct pgxpool construction outside internal/platform/db"})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := validateServerLaneWiring(); err != nil {
		violations = append(violations, violation{Path: "internal/platform/http/server.go", Reason: err.Error()})
	}
	return violations, nil
}

func validateServerLaneWiring() error {
	raw, err := os.ReadFile(filepath.FromSlash("internal/platform/http/server.go"))
	if err != nil {
		return err
	}
	content := string(raw)
	required := []string{
		"databasePools.Management.Raw()",
		"databasePools.RuntimeExecution.Raw()",
		"databasePools.RuntimeTelemetry.Raw()",
		"databasePools.RuntimeFeedback.Raw()",
		"databasePools.Realtime.Raw()",
		"databasePools.CacheRefresh.Raw()",
		"databasePools.BackgroundJobs.Raw()",
		"FeedbackPool: runtimeFeedbackPool",
		"RealtimePool: realtimePool",
		"RefreshPool: cacheRefreshPool",
		"ProxyKeyUsagePool: backgroundJobsPool",
	}
	for _, want := range required {
		if !strings.Contains(content, want) {
			return fmt.Errorf("missing named lane wiring %q", want)
		}
	}
	for _, forbidden := range []string{
		"RealtimePool: managementPool",
		"RefreshPool: managementPool",
		"FeedbackPool: runtimeExecutionPool",
		"FeedbackPool: runtimeTelemetryPool",
		"ProxyKeyUsagePool: managementPool",
	} {
		if strings.Contains(content, forbidden) {
			return fmt.Errorf("forbidden lane borrowing %q", forbidden)
		}
	}
	return nil
}
