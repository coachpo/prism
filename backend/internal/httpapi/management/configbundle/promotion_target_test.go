package configbundle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

const importedPromotionTargetDetail = "context_overflow_promotion_target_id must reference an imported model"

var configBundlePromotionPostgres struct {
	once          sync.Once
	containerName string
	hostPort      string
	err           error
}

type configBundlePromotionHarness struct {
	containerName string
	hostPort      string
}

func TestBundleExportIncludesPromotionTarget(t *testing.T) {
	ctx, conn := configBundleMigratedConn(t, "configbundle_export_includes_promotion_target")
	now := time.Date(2026, time.June, 5, 21, 0, 0, 0, time.UTC)
	profileID := insertConfigBundleProfile(t, ctx, conn, "export-profile", now)
	service := &Service{bundleSecretKeyID: "kid", now: func() time.Time { return now }}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin export seed transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := service.executeProfileImport(ctx, tx, profileID, validPromotionTargetBundleRequest()); err != nil {
		t.Fatalf("seed imported bundle for export: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit export seed transaction: %v", err)
	}
	bundle, err := service.buildProfileBundle(ctx, conn, profileID, now, "kid", false)
	if err != nil {
		t.Fatalf("build profile bundle: %v", err)
	}
	if len(bundle.Models) != 2 {
		t.Fatalf("expected two exported models, got %d", len(bundle.Models))
	}
	source := exportedBundleModelByID(t, bundle.Models, "source-small")
	requirePromotionTargetEquals(t, source.ContextOverflowPromotionTargetID, "target-large")
}

func TestBundlePreviewAcceptsValidPromotionTarget(t *testing.T) {
	ctx, conn := configBundleMigratedConn(t, "configbundle_preview_accepts_valid_promotion_target")
	now := time.Date(2026, time.June, 5, 21, 5, 0, 0, time.UTC)
	profileID := insertConfigBundleProfile(t, ctx, conn, "preview-valid-profile", now)
	service := &Service{bundleSecretKeyID: "kid", now: func() time.Time { return now }}

	preview, err := service.previewProfileImport(ctx, conn, profileID, validPromotionTargetBundleRequest())
	if err != nil {
		t.Fatalf("preview valid promotion target bundle: %v", err)
	}
	if !preview.Ready || len(preview.BlockingErrors) != 0 {
		t.Fatalf("expected ready preview with no blocking errors, got %+v", preview)
	}
}

func TestBundlePreviewRejectsSelfPromotionTarget(t *testing.T) {
	assertPreviewAndImportPromotionTargetFailure(t, "configbundle_preview_rejects_self_promotion_target", func(request *profileImportRequest) {
		request.Models[0].ContextOverflowPromotionTargetID = stringPtr("source-small")
	}, promotionTargetValidationCodeSelf, "context_overflow_promotion_target_id cannot reference the source model")
}

func TestBundlePreviewRejectsDisabledPromotionTarget(t *testing.T) {
	assertPreviewAndImportPromotionTargetFailure(t, "configbundle_preview_rejects_disabled_promotion_target", func(request *profileImportRequest) {
		request.Models[1].IsEnabled = false
	}, promotionTargetValidationCodeDisabled, "context_overflow_promotion_target_id must reference an enabled model")
}

func TestBundlePreviewRejectsFacadePromotionTarget(t *testing.T) {
	assertPreviewAndImportPromotionTargetFailure(t, "configbundle_preview_rejects_facade_promotion_target", func(request *profileImportRequest) {
		request.Models[1].FacadeEnabled = true
		request.Models[1].FacadeSelectionPolicy = stringPtr(facadeSelectionPolicyOrderedEligibleContext)
		request.Models[1].FacadeFallbackPolicy = stringPtr(facadeFallbackPolicySkipIneligibleTargets)
	}, promotionTargetValidationCodeFacade, "context_overflow_promotion_target_id must reference a non-facade model")
}

func TestBundleImportRejectsUnknownPromotionTarget(t *testing.T) {
	assertPreviewAndImportPromotionTargetFailure(t, "configbundle_import_rejects_unknown_promotion_target", func(request *profileImportRequest) {
		request.Models[0].ContextOverflowPromotionTargetID = stringPtr("missing-model")
	}, promotionTargetValidationCodeUnknown, importedPromotionTargetDetail)
}

func TestBundleImportRejectsApiFamilyMismatchPromotionTarget(t *testing.T) {
	assertPreviewAndImportPromotionTargetFailure(t, "configbundle_import_rejects_api_family_mismatch_promotion_target", func(request *profileImportRequest) {
		request.Connections[1].APIFamily = "anthropic"
		request.Connections[1].OpenAITextCapability = nil
		request.Connections[1].OpenAITextCapabilitySet = false
		request.Models[1].APIFamily = "anthropic"
		request.Models[1].OpenAIAcceptedFormat = nil
	}, promotionTargetValidationCodeAPIFamilyMismatch, "context_overflow_promotion_target_id must reference a model with the same api_family")
}

func TestBundlePreviewAcceptsRecursivePromotionChainWithoutImmediateLargerWindow(t *testing.T) {
	ctx, conn := configBundleMigratedConn(t, "configbundle_preview_accepts_recursive_promotion_chain")
	now := time.Date(2026, time.June, 5, 21, 20, 0, 0, time.UTC)
	profileID := insertConfigBundleProfile(t, ctx, conn, "preview-valid-recursive-profile", now)
	service := &Service{bundleSecretKeyID: "kid", now: func() time.Time { return now }}
	request := validRecursivePromotionTargetBundleRequest()

	preview, err := service.previewProfileImport(ctx, conn, profileID, request)
	if err != nil {
		t.Fatalf("preview recursive promotion target bundle: %v", err)
	}
	if !preview.Ready || len(preview.BlockingErrors) != 0 {
		t.Fatalf("expected ready preview with no blocking errors, got %+v", preview)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin recursive import transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := service.executeProfileImport(ctx, tx, profileID, request); err != nil {
		t.Fatalf("execute recursive promotion target bundle: %v", err)
	}
}

func TestBundleImportRejectsPromotionCycle(t *testing.T) {
	assertPreviewAndImportPromotionTargetFailure(t, "configbundle_import_rejects_promotion_cycle", func(request *profileImportRequest) {
		request.Models[1].ContextOverflowPromotionTargetID = stringPtr("source-small")
	}, promotionTargetValidationCodeCycle, "context_overflow_promotion_target_id must not introduce a promotion target cycle")
}

func TestBundleImportRejectsPromotionMaxDepth(t *testing.T) {
	assertPreviewAndImportPromotionTargetFailure(t, "configbundle_import_rejects_promotion_max_depth", func(request *profileImportRequest) {
		request.Models[1].ContextOverflowPromotionTargetID = stringPtr("target-hop-2")
		request.Connections = append(request.Connections,
			promotionTargetConnection("target-hop-2-conn", 24_000, 2),
			promotionTargetConnection("target-hop-3-conn", 32_000, 3),
			promotionTargetConnection("target-hop-4-conn", 40_000, 4),
		)
		request.Models = append(request.Models,
			promotionTargetModel("target-hop-2", "target-hop-2-conn", stringPtr("target-hop-3"), 24_000),
			promotionTargetModel("target-hop-3", "target-hop-3-conn", stringPtr("target-hop-4"), 32_000),
			promotionTargetModel("target-hop-4", "target-hop-4-conn", nil, 40_000),
		)
	}, promotionTargetValidationCodeMaxDepth, "context_overflow_promotion_target_id promotion chain cannot exceed depth 3")
}

func TestBundleImportRejectsSameTerminalPromotionTarget(t *testing.T) {
	assertPreviewAndImportPromotionTargetFailure(t, "configbundle_import_rejects_same_terminal_promotion_target", func(request *profileImportRequest) {
		request.Connections = request.Connections[1:]
		request.Models[0].AccessTargets = []accessTargetExport{{Position: 0, IsEnabled: true, TargetType: "model", TargetModelID: stringPtr("target-large")}}
	}, promotionTargetValidationCodeSameTerminal, "context_overflow_promotion_target_id must not resolve to the same terminal target as the source model")
}

func TestBundleImportRejectsMissingPromotionTarget(t *testing.T) {
	assertPreviewAndImportPromotionTargetFailure(t, "configbundle_import_rejects_missing_promotion_target", func(request *profileImportRequest) {
		request.Models[0].ContextOverflowPromotionTargetID = stringPtr("   ")
	}, promotionTargetValidationCodeUnknown, importedPromotionTargetDetail)
}

func assertPreviewPromotionTargetFailure(t *testing.T, name string, mutate func(*profileImportRequest), code string, detail string) {
	t.Helper()
	ctx, conn := configBundleMigratedConn(t, name)
	now := time.Date(2026, time.June, 5, 21, 10, 0, 0, time.UTC)
	profileID := insertConfigBundleProfile(t, ctx, conn, name+"-profile", now)
	service := &Service{bundleSecretKeyID: "kid", now: func() time.Time { return now }}
	request := validPromotionTargetBundleRequest()
	mutate(&request)

	_, err := service.previewProfileImport(ctx, conn, profileID, request)
	requireConfigBundleRoutingIssue(t, err, detail, code, "models[0].context_overflow_promotion_target_id")
}

func assertImportPromotionTargetFailure(t *testing.T, name string, mutate func(*profileImportRequest), code string, detail string) {
	t.Helper()
	ctx, conn := configBundleMigratedConn(t, name)
	now := time.Date(2026, time.June, 5, 21, 15, 0, 0, time.UTC)
	profileID := insertConfigBundleProfile(t, ctx, conn, name+"-profile", now)
	strategyID := insertConfigBundleStrategy(t, ctx, conn, profileID, "existing-strategy", now)
	insertConfigBundleModel(t, ctx, conn, profileID, strategyID, "existing-model", now)
	service := &Service{bundleSecretKeyID: "kid", now: func() time.Time { return now }}
	request := validPromotionTargetBundleRequest()
	mutate(&request)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin failed-import transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = service.executeProfileImport(ctx, tx, profileID, request)
	requireConfigBundleRoutingIssue(t, err, detail, code, "models[0].context_overflow_promotion_target_id")
	if got := countConfigBundleModels(t, ctx, tx, profileID); got != 1 {
		t.Fatalf("expected failed import to preserve existing profile model state, got %d models", got)
	}
	if got := countConfigBundleModelByID(t, ctx, tx, profileID, "existing-model"); got != 1 {
		t.Fatalf("expected existing model to remain after failed import, got count=%d", got)
	}
}

func assertPreviewAndImportPromotionTargetFailure(t *testing.T, name string, mutate func(*profileImportRequest), code string, detail string) {
	t.Helper()
	assertPreviewPromotionTargetFailure(t, name+"_preview", mutate, code, detail)
	assertImportPromotionTargetFailure(t, name+"_execute", mutate, code, detail)
}

func requireConfigBundleRoutingIssue(t *testing.T, err error, detail string, code string, path string) {
	t.Helper()
	domainErr := requireConfigBundleDomainError(t, err, 400, detail)
	issues, ok := domainErr.Fields["routing_plan_issues"].([]routingPlanValidationIssue)
	if !ok {
		t.Fatalf("expected routing_plan_issues payload, got %+v", domainErr.Fields)
	}
	if len(issues) != 1 {
		t.Fatalf("expected one routing_plan_issue, got %+v", issues)
	}
	if issues[0].Code != code || issues[0].Path != path || issues[0].Message != detail {
		t.Fatalf("unexpected routing_plan_issue: %+v", issues[0])
	}
}

func exportedBundleModelByID(t *testing.T, models []modelExport, modelID string) modelExport {
	t.Helper()
	for _, model := range models {
		if model.ModelID == modelID {
			return model
		}
	}
	t.Fatalf("expected exported model %q, got %+v", modelID, models)
	return modelExport{}
}

func requirePromotionTargetEquals(t *testing.T, value *string, expected string) {
	t.Helper()
	if value == nil || *value != expected {
		t.Fatalf("expected promotion target %q, got %#v", expected, value)
	}
}

func validPromotionTargetBundleRequest() profileImportRequest {
	return profileImportRequest{
		Version:    canonicalProfileBundleVersion,
		BundleKind: canonicalProfileBundleKind,
		Endpoints:  []endpointExport{{Name: "OpenAI", BaseURL: "https://api.openai.com/v1", Position: 0}},
		LoadbalanceStrategies: []loadbalanceStrategyExport{{
			Name:                               "Default single",
			LegacyStrategyType:                 stringPtr("single"),
			FailureStatusCodes:                 []int{429},
			BanMode:                            stringPtr("off"),
			RetryBaseDelayMS:                   intPtr(60000),
			RetryBackoffMultiplier:             float64Ptr(2),
			RetryJitterRatio:                   float64Ptr(0.2),
			RetryMaxDelayMS:                    intPtr(900000),
			CycleRetryAttemptLimit:             intPtr(3),
			BanCumulativeRetryAttemptThreshold: intPtr(0),
			BanDurationSeconds:                 intPtr(0),
		}},
		Connections: []connectionExport{
			{Ref: "source-conn", APIFamily: "openai", EndpointName: "OpenAI", ContextWindowTokens: intPtr(8_000), DefaultOutputTokenReserve: intPtr(4096), MaxContextUtilization: float64Ptr(1.0), IsActive: true, Priority: 0, OpenAITextCapability: stringPtr("responses_only"), OpenAITextCapabilitySet: true},
			{Ref: "target-conn", APIFamily: "openai", EndpointName: "OpenAI", ContextWindowTokens: intPtr(16_000), DefaultOutputTokenReserve: intPtr(4096), MaxContextUtilization: float64Ptr(1.0), IsActive: true, Priority: 1, OpenAITextCapability: stringPtr("responses_only"), OpenAITextCapabilitySet: true},
		},
		Models: []modelExport{
			{APIFamily: "openai", ModelID: "source-small", DisplayName: stringPtr("Source Small"), LoadbalanceStrategyName: stringPtr("Default single"), ContextWindowTokens: intPtr(8_000), DefaultOutputTokenReserve: intPtr(4096), MaxContextUtilization: float64Ptr(1.0), ContextOverflowPromotionTargetID: stringPtr("target-large"), OpenAIAcceptedFormat: stringPtr("dual_native"), IsEnabled: true, AccessTargets: []accessTargetExport{{Position: 0, IsEnabled: true, TargetType: "connection", ConnectionRef: stringPtr("source-conn")}}},
			{APIFamily: "openai", ModelID: "target-large", DisplayName: stringPtr("Target Large"), LoadbalanceStrategyName: stringPtr("Default single"), ContextWindowTokens: intPtr(16_000), DefaultOutputTokenReserve: intPtr(4096), MaxContextUtilization: float64Ptr(1.0), OpenAIAcceptedFormat: stringPtr("dual_native"), IsEnabled: true, AccessTargets: []accessTargetExport{{Position: 0, IsEnabled: true, TargetType: "connection", ConnectionRef: stringPtr("target-conn")}}},
		},
		ProfileSettings: &profileSettingsExport{
			ReportCurrencyCode:   "USD",
			ReportCurrencySymbol: "$",
			AuditAPIFamilySettings: []auditAPIFamilySettingExport{
				{APIFamily: "openai", AuditEnabled: true, AuditCaptureBodies: false},
				{APIFamily: "anthropic", AuditEnabled: false, AuditCaptureBodies: false},
				{APIFamily: "gemini", AuditEnabled: false, AuditCaptureBodies: false},
			},
			AuditAPIFamilySettingsIsSet: true,
		},
		HeaderBlocklistRules: []headerBlocklistRuleExport{},
		UserAgentClientRules: []userAgentClientRuleExport{},
		SecretPayload:        secretPayloadExport{Kind: "encrypted", Cipher: bundleSecretCipher, KeyID: "kid", Entries: []secretPayloadEntry{}},
	}
}

func validRecursivePromotionTargetBundleRequest() profileImportRequest {
	request := validPromotionTargetBundleRequest()
	request.Models[0].ContextWindowTokens = intPtr(16_000)
	request.Connections[0].ContextWindowTokens = intPtr(16_000)
	request.Models[1].ContextWindowTokens = intPtr(8_000)
	request.Connections[1].ContextWindowTokens = intPtr(8_000)
	request.Models[1].ContextOverflowPromotionTargetID = stringPtr("target-terminal")
	request.Connections = append(request.Connections, promotionTargetConnection("target-terminal-conn", 32_000, 2))
	request.Models = append(request.Models, promotionTargetModel("target-terminal", "target-terminal-conn", nil, 32_000))
	return request
}

func promotionTargetConnection(ref string, contextWindowTokens int, priority int) connectionExport {
	return connectionExport{Ref: ref, APIFamily: "openai", EndpointName: "OpenAI", ContextWindowTokens: intPtr(contextWindowTokens), DefaultOutputTokenReserve: intPtr(4096), MaxContextUtilization: float64Ptr(1.0), IsActive: true, Priority: priority, OpenAITextCapability: stringPtr("responses_only"), OpenAITextCapabilitySet: true}
}

func promotionTargetModel(modelID string, connectionRef string, promotionTargetID *string, contextWindowTokens int) modelExport {
	return modelExport{APIFamily: "openai", ModelID: modelID, DisplayName: stringPtr(modelID), LoadbalanceStrategyName: stringPtr("Default single"), ContextWindowTokens: intPtr(contextWindowTokens), DefaultOutputTokenReserve: intPtr(4096), MaxContextUtilization: float64Ptr(1.0), ContextOverflowPromotionTargetID: promotionTargetID, OpenAIAcceptedFormat: stringPtr("dual_native"), IsEnabled: true, AccessTargets: []accessTargetExport{{Position: 0, IsEnabled: true, TargetType: "connection", ConnectionRef: stringPtr(connectionRef)}}}
}

func configBundleMigratedConn(t *testing.T, name string) (context.Context, *pgx.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	harness := configBundleStoreHarness(t)
	databaseName := fmt.Sprintf("%s_%s", name, configBundleRandomSuffix(t))
	conn := harness.openDatabase(t, ctx, databaseName)
	runner, err := migrate.New(migrate.Options{})
	if err != nil {
		t.Fatalf("build migration runner: %v", err)
	}
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations for %s: %v", databaseName, err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return ctx, conn
}

func configBundleStoreHarness(t *testing.T) configBundlePromotionHarness {
	t.Helper()
	configBundlePromotionPostgres.once.Do(func() {
		containerName := "prism-configbundle-" + configBundleRandomSuffix(t)
		if _, err := runConfigBundleDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
			configBundlePromotionPostgres.err = err
			return
		}
		configBundlePromotionPostgres.containerName = containerName
		hostPort, err := configBundleDockerPort(containerName)
		if err != nil {
			configBundlePromotionPostgres.err = err
			return
		}
		if err := waitForConfigBundlePostgres(hostPort); err != nil {
			configBundlePromotionPostgres.err = err
			return
		}
		configBundlePromotionPostgres.hostPort = hostPort
	})
	if configBundlePromotionPostgres.err != nil {
		t.Fatalf("start postgres harness: %v", configBundlePromotionPostgres.err)
	}
	return configBundlePromotionHarness{containerName: configBundlePromotionPostgres.containerName, hostPort: configBundlePromotionPostgres.hostPort}
}

func (h configBundlePromotionHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := configBundleConnect(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+configBundleQuoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+configBundleQuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return configBundleConnect(t, ctx, h.connectionString(databaseName))
}

func (h configBundlePromotionHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func configBundleConnect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres %s: %v", dsn, err)
	}
	return conn
}

func configBundleDockerPort(containerName string) (string, error) {
	output, err := runConfigBundleDockerCommand(context.Background(), "port", containerName, "5432/tcp")
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

func waitForConfigBundlePostgres(hostPort string) error {
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

func runConfigBundleDockerCommand(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func configBundleQuoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func configBundleRandomSuffix(t *testing.T) string {
	t.Helper()
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}
	return hex.EncodeToString(buffer)
}

func insertConfigBundleProfile(t *testing.T, ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, name string, now time.Time) int {
	t.Helper()
	var profileID int
	if err := exec.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, $2, TRUE, FALSE, TRUE, 1, $3, $3) RETURNING id`, name, nil, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile %q: %v", name, err)
	}
	return profileID
}

func insertConfigBundleStrategy(t *testing.T, ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, profileID int, name string, now time.Time) int {
	t.Helper()
	var strategyID int
	if err := exec.QueryRow(ctx, `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, $2, 'single', ARRAY[429], 'off', 60000, 2.0, 0.2, 900000, 3, 0, 0, $3, $3) RETURNING id`, profileID, name, now).Scan(&strategyID); err != nil {
		t.Fatalf("insert loadbalance strategy %q: %v", name, err)
	}
	return strategyID
}

func insertConfigBundleModel(t *testing.T, ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, profileID int, strategyID int, modelID string, now time.Time) {
	t.Helper()
	if err := exec.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, context_window_tokens, default_output_token_reserve, max_context_utilization, preferred_context_utilization_threshold, facade_enabled, facade_selection_policy, facade_fallback_policy, context_overflow_promotion_target_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', $2, $2, $3, NULL, 4096, 0.9, NULL, FALSE, NULL, NULL, NULL, 'dual_native', TRUE, $4, $4) RETURNING id`, profileID, modelID, strategyID, now).Scan(new(int)); err != nil {
		t.Fatalf("insert model %q: %v", modelID, err)
	}
}

func countConfigBundleModels(t *testing.T, ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, profileID int) int {
	t.Helper()
	var count int
	if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM model_configs WHERE profile_id = $1`, profileID).Scan(&count); err != nil {
		t.Fatalf("count models for profile %d: %v", profileID, err)
	}
	return count
}

func countConfigBundleModelByID(t *testing.T, ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, profileID int, modelID string) int {
	t.Helper()
	var count int
	if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM model_configs WHERE profile_id = $1 AND model_id = $2`, profileID, modelID).Scan(&count); err != nil {
		t.Fatalf("count model %q in profile %d: %v", modelID, profileID, err)
	}
	return count
}
