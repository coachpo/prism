package sidecars

import (
	"context"
	"slices"
	"testing"

	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestSidecarBackgroundWorkerRegistrationIncludesSyncOnly(t *testing.T) {
	service, err := NewService(config.Settings{SecretEncryptionKey: "unit-test-secret"}, Options{})
	if err != nil {
		t.Fatalf("new sidecar service: %v", err)
	}
	scheduler := background.NewScheduler(background.Config{})
	if err := service.RegisterBackgroundWorker(scheduler); err != nil {
		t.Fatalf("register sidecar workers: %v", err)
	}
	workers := scheduler.RegisteredWorkers()
	if !slices.Equal(workers, []background.WorkerName{SidecarSyncWorkerName}) {
		t.Fatalf("registered workers = %v, want only %s", workers, SidecarSyncWorkerName)
	}
	accepted := scheduler.Submit(context.Background(), background.JobRequest{Worker: SidecarSyncWorkerName})
	if accepted.Status != background.SubmitAccepted {
		t.Fatalf("sync worker submit status = %s reason=%s", accepted.Status, accepted.Reason)
	}
}
