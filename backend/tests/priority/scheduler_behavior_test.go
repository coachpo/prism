package priority

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/alerting"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
	"github.com/coachpo/prism/backend/internal/platform/managementsideeffects"
)

func TestSchedulerRegistersProductionWorkers(t *testing.T) {
	ctx, pool, databaseURL := openPriorityTestPool(t)
	settings := priorityTestSettings(databaseURL)
	scheduler := background.NewScheduler(background.Config{})

	runtimeCache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{
		RefreshPool:         pool,
		SecretEncryptionKey: settings.SecretEncryptionKey,
		Scheduler:           scheduler,
	})
	if err := runtimeCache.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap runtime cache: %v", err)
	}

	authService, err := managementauth.NewService(settings, managementauth.Options{
		Pool:              pool,
		ProxyKeyUsagePool: pool,
		Scheduler:         scheduler,
	})
	if err != nil {
		t.Fatalf("build management auth service: %v", err)
	}
	t.Cleanup(authService.Close)

	logRetentionStore := logretention.NewStore(logretention.Options{Pool: pool})
	managementJobs := managementjobs.NewStore(managementjobs.Options{Pool: pool, Scheduler: scheduler, LogRetention: logRetentionStore})
	managementSideEffects := managementsideeffects.NewDispatcher(managementsideeffects.Options{Pool: pool, Scheduler: scheduler})
	alertOutbox := alerting.NewStore(alerting.Options{Pool: pool, Scheduler: scheduler, WebhookURLProvider: staticWebhookURLProvider("https://alerts.example.test/webhook")})
	executionPool := openPriorityAuxPool(t, databaseURL)
	telemetryPool := openPriorityAuxPool(t, databaseURL)
	feedbackPool := openPriorityAuxPool(t, databaseURL)
	runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{
		ExecutionPool:             executionPool,
		TelemetryPool:             telemetryPool,
		FeedbackPool:              feedbackPool,
		Cache:                     runtimeCache,
		RuntimeState:              loadbalancedomain.NewLocalRuntimeStateStore(),
		LogPartitionEnsurer:       logRetentionStore,
		AssumeLogPartitionHorizon: true,
		Scheduler:                 scheduler,
		FeedbackPipeline:          runtimeapi.RuntimeFeedbackPipelineOptions{AlertOutbox: alertOutbox},
	})
	if err != nil {
		t.Fatalf("build runtime service: %v", err)
	}
	t.Cleanup(runtimeService.Close)

	for _, register := range []func(*background.Scheduler) error{
		runtimeCache.RegisterBackgroundWorker,
		authService.RegisterBackgroundWorkers,
		managementJobs.RegisterBackgroundWorker,
		managementSideEffects.RegisterBackgroundWorker,
		alertOutbox.RegisterBackgroundWorker,
		logRetentionStore.RegisterBackgroundWorker,
		runtimeService.RegisterBackgroundWorkers,
	} {
		if err := register(scheduler); err != nil {
			t.Fatalf("register background worker: %v", err)
		}
	}

	got := scheduler.RegisteredWorkers()
	slices.Sort(got)
	want := []background.WorkerName{
		alerting.WorkerName,
		logretention.WorkerName,
		managementjobs.WorkerName,
		managementsideeffects.WorkerName,
		"proxy_key_usage_writer",
		"runtime_feedback_pipeline",
		"runtime_shared_cache_refresh",
		"runtime_side_effects_activity",
		"runtime_telemetry_outbox",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("expected production worker set %v, got %v", want, got)
	}
}

type staticWebhookURLProvider string

func (s staticWebhookURLProvider) AlertingWebhookURL() string {
	return string(s)
}

func openPriorityAuxPool(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open auxiliary priority pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
