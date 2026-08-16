package lifecycle

import (
	"slices"
	"testing"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/alerting"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
	"github.com/coachpo/prism/backend/internal/platform/managementsideeffects"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRegisterDatabaseBackgroundWorkersRegistersProductionWorkers(t *testing.T) {
	scheduler := background.NewScheduler(background.Config{})
	settings := config.Settings{
		Host:                "127.0.0.1",
		Port:                8000,
		AppEnv:              config.EnvironmentProduction,
		SecretEncryptionKey: "lifecycle-test-secret",
		CORSAllowedOrigins:  "http://localhost:5173,http://127.0.0.1:5173",
		AuthJWTSecret:       "lifecycle-test-jwt-secret",
	}

	refreshPool := &pgxpool.Pool{}
	managementPool := &pgxpool.Pool{}
	proxyKeyUsagePool := &pgxpool.Pool{}
	backgroundPool := &pgxpool.Pool{}
	runtimeExecutionPool := &pgxpool.Pool{}
	runtimeTelemetryPool := &pgxpool.Pool{}
	runtimeFeedbackPool := &pgxpool.Pool{}

	cache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{
		RefreshPool:         refreshPool,
		SecretEncryptionKey: settings.SecretEncryptionKey,
		Scheduler:           scheduler,
	})
	authCache := managementauth.NewRuntimeCacheFromShared(cache)
	authService, err := managementauth.NewService(settings, managementauth.Options{
		Pool:              managementPool,
		ProxyKeyUsagePool: proxyKeyUsagePool,
		RuntimeCache:      authCache,
		Scheduler:         scheduler,
	})
	if err != nil {
		t.Fatalf("build auth service: %v", err)
	}

	logRetentionStore := logretention.NewStore(logretention.Options{Pool: backgroundPool})
	alertWebhookOutbox := alerting.NewStore(alerting.Options{Pool: backgroundPool, Scheduler: scheduler})
	managementSideEffects := managementsideeffects.NewDispatcher(managementsideeffects.Options{Pool: backgroundPool, Scheduler: scheduler})
	managementJobs := managementjobs.NewStore(managementjobs.Options{Pool: backgroundPool, Scheduler: scheduler, LogRetention: logRetentionStore})

	runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{
		ExecutionPool:             runtimeExecutionPool,
		TelemetryPool:             runtimeTelemetryPool,
		FeedbackPool:              runtimeFeedbackPool,
		Cache:                     cache,
		RuntimeState:              loadbalancedomain.NewLocalRuntimeStateStore(),
		LogPartitionEnsurer:       logRetentionStore,
		AssumeLogPartitionHorizon: true,
		Scheduler:                 scheduler,
		FeedbackPipeline:          runtimeapi.RuntimeFeedbackPipelineOptions{AlertOutbox: alertWebhookOutbox},
		SideEffects:               runtimeSideEffectOptions(settings),
	})
	if err != nil {
		t.Fatalf("build runtime service: %v", err)
	}

	err = registerDatabaseBackgroundWorkers(
		databaseBackgroundServices{
			scheduler:             scheduler,
			managementSideEffects: managementSideEffects,
			logRetention:          logRetentionStore,
			managementJobs:        managementJobs,
			alertWebhookOutbox:    alertWebhookOutbox,
		},
		runtimePlanningServices{
			cache:     cache,
			state:     loadbalancedomain.NewLocalRuntimeStateStore(),
			authCache: authCache,
		},
		authServices{management: authService},
		runtimeService,
	)
	if err != nil {
		t.Fatalf("register production background workers: %v", err)
	}

	got := scheduler.RegisteredWorkers()
	slices.Sort(got)
	want := []background.WorkerName{
		"alert_webhook_worker",
		"log_partition_maintenance",
		"management_audit_delete_jobs",
		"management_side_effect_outbox",
		"proxy_key_usage_writer",
		"runtime_feedback_pipeline",
		"runtime_shared_cache_refresh",
		"runtime_side_effects_activity",
		"runtime_telemetry_outbox",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("production worker set = %v, want %v", got, want)
	}
}
