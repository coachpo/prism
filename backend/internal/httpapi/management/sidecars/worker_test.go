package sidecars

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestSidecarBackgroundWorkerRegistrationIncludesWatchdog(t *testing.T) {
	service, err := NewService(config.Settings{SecretEncryptionKey: "unit-test-secret"}, Options{})
	if err != nil {
		t.Fatalf("new sidecar service: %v", err)
	}
	scheduler := background.NewScheduler(background.Config{})
	if err := service.RegisterBackgroundWorker(scheduler); err != nil {
		t.Fatalf("register sidecar workers: %v", err)
	}
	workers := scheduler.RegisteredWorkers()
	if !slices.Contains(workers, SidecarSyncWorkerName) {
		t.Fatalf("sync worker was not registered: %v", workers)
	}
	if !slices.Contains(workers, SidecarWatchdogWorkerName) {
		t.Fatalf("watchdog worker was not registered: %v", workers)
	}
	accepted := scheduler.Submit(context.Background(), background.JobRequest{Worker: SidecarWatchdogWorkerName})
	if accepted.Status != background.SubmitAccepted {
		t.Fatalf("watchdog worker submit status = %s reason=%s", accepted.Status, accepted.Reason)
	}
	elevated := scheduler.Submit(context.Background(), background.JobRequest{Worker: SidecarWatchdogWorkerName, PriorityOverride: background.PriorityNormalBackground})
	if elevated.Status != background.SubmitRejectedInvalidPriority {
		t.Fatalf("watchdog worker should stay low-priority only, got %s", elevated.Status)
	}
}

func TestPrivacyWatchdogWorkerSummaryLogsDistinguishProbeOutcomes(t *testing.T) {
	now := time.Date(2026, time.May, 11, 14, 0, 0, 0, time.UTC)
	resetAt := now.Add(time.Hour)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setAuthFile("auth-summary-held", "idx-summary-held", "codex", 10)
	upstream.setAuthFile("auth-summary-restored", "idx-summary-restored", "codex", 0)
	upstream.setAuthFile("auth-summary-failed", "idx-summary-failed", "codex", 0)
	upstream.setProbeResponse("idx-summary-held", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogExhaustedUsageBody(resetAt)})
	upstream.setProbeResponse("idx-summary-restored", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()})
	upstream.setProbeResponse("idx-summary-failed", watchdogProbeTestResponse{StatusCode: http.StatusTooManyRequests, Body: `{"error":"probe-token-secret"}`})
	service := newWatchdogTestService(t, func() time.Time { return now })

	held := createSyncTestSidecar(t, service, upstream.URL(), true, 91)
	enableWatchdogProbePolicy(t, service, held.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, held.ID, now)
	seedWatchdogProbeSnapshot(t, service, held.ID, now, "auth-summary-held", "idx-summary-held", "codex", 10)
	restored := createSyncTestSidecar(t, service, upstream.URL(), true, 92)
	enableWatchdogProbePolicy(t, service, restored.ID, 1, 5)
	createWatchdogProbeHold(t, service, restored.ID, "auth-summary-restored", "idx-summary-restored", now.Add(-time.Minute))
	failed := createSyncTestSidecar(t, service, upstream.URL(), true, 93)
	enableWatchdogProbePolicy(t, service, failed.ID, 1, 5)
	createWatchdogProbeHold(t, service, failed.ID, "auth-summary-failed", "idx-summary-failed", now.Add(-time.Minute))
	unsupported := createSyncTestSidecar(t, service, upstream.URL(), true, 94)
	enableWatchdogProbePolicy(t, service, unsupported.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, unsupported.ID, now)
	seedWatchdogProbeSnapshot(t, service, unsupported.ID, now, "auth-summary-unsupported", "idx-summary-unsupported", "gemini", 10)

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	jobResult := service.handleScheduledSidecarWatchdog(context.Background(), background.Job{Worker: SidecarWatchdogWorkerName})
	if jobResult.Status != background.JobSucceeded || jobResult.Err != nil {
		t.Fatalf("watchdog worker result = %+v", jobResult)
	}
	logText := logs.String()
	for _, want := range []string{"probed=3", "quota_held=1", "restored=1", "probe_failed=1", "unsupported_skipped=1"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("watchdog worker log missing %q in %s", want, logText)
		}
	}
	for _, forbidden := range []string{"probe-token-secret", "Authorization", "body", "headers"} {
		if strings.Contains(logText, forbidden) {
			t.Fatalf("watchdog worker log leaked %q in %s", forbidden, logText)
		}
	}
}

func TestScheduledSidecarWatchdogReportsPartialFailuresAsSucceededJob(t *testing.T) {
	service, err := NewService(config.Settings{SecretEncryptionKey: "unit-test-secret"}, Options{})
	if err != nil {
		t.Fatalf("new sidecar service: %v", err)
	}
	createSyncTestSidecar(t, service, "http://127.0.0.1:18080", true, 60)
	failed := createSyncTestSidecar(t, service, "http://127.0.0.1:18081", true, 61)
	service.store = &watchdogPartialFailureStore{persistence: service.store, failID: failed.ID}

	result := service.handleScheduledSidecarWatchdog(context.Background(), background.Job{Worker: SidecarWatchdogWorkerName})
	if result.Status != background.JobSucceeded || result.Err != nil || result.Retry {
		t.Fatalf("partial watchdog failures should not fail the worker job: %+v", result)
	}
}

type watchdogPartialFailureStore struct {
	persistence
	failID int
}

func (s *watchdogPartialFailureStore) GetSidecarInstance(ctx context.Context, id int) (SidecarInstance, bool, error) {
	if id == s.failID {
		return SidecarInstance{}, false, errors.New("forced watchdog failure")
	}
	return s.persistence.GetSidecarInstance(ctx, id)
}
