package sidecars

import (
	"context"
	"errors"
	"slices"
	"testing"

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
