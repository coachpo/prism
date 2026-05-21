package priority

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	managementsidecars "github.com/coachpo/prism/backend/internal/httpapi/management/sidecars"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
	platformpriority "github.com/coachpo/prism/backend/internal/platform/priority"
)

func TestSidecarWorkerPriorityRunsAsBoundedLowBackgroundJob(t *testing.T) {
	store := &sidecarObservingStore{}
	service, err := managementsidecars.NewService(config.Settings{SecretEncryptionKey: "sidecar-priority-secret"}, managementsidecars.Options{Store: store})
	if err != nil {
		t.Fatalf("build sidecar service: %v", err)
	}
	scheduler := background.NewScheduler(background.Config{})
	if err := service.RegisterBackgroundWorker(scheduler); err != nil {
		t.Fatalf("register sidecar workers: %v", err)
	}

	guardScheduler := background.NewScheduler(background.Config{})
	if err := service.RegisterBackgroundWorker(guardScheduler); err != nil {
		t.Fatalf("register sidecar guard workers: %v", err)
	}
	if got := guardScheduler.Submit(context.Background(), background.JobRequest{Worker: managementsidecars.SidecarSyncWorkerName, PriorityOverride: background.PriorityNormalBackground}); got.Status != background.SubmitRejectedInvalidPriority {
		t.Fatalf("sidecar sync worker accepted elevated priority: %+v", got)
	}
	workers := guardScheduler.RegisteredWorkers()
	if len(workers) != 1 || workers[0] != managementsidecars.SidecarSyncWorkerName {
		t.Fatalf("sidecar worker registry = %v want only %s", workers, managementsidecars.SidecarSyncWorkerName)
	}

	if err := scheduler.Start(t.Context()); err != nil {
		t.Fatalf("start sidecar worker scheduler: %v", err)
	}
	result := scheduler.Submit(context.Background(), background.JobRequest{Worker: managementsidecars.SidecarSyncWorkerName})
	if result.Status != background.SubmitAccepted {
		t.Fatalf("submit %s status = %s reason=%s", managementsidecars.SidecarSyncWorkerName, result.Status, result.Reason)
	}
	drain := scheduler.Drain(context.Background(), time.Now().Add(2*time.Second))
	if drain.TimedOut || drain.Failed != 0 || drain.Completed != 1 {
		t.Fatalf("sidecar worker drain result = %+v", drain)
	}

	calls := store.callsSnapshot()
	if len(calls) != 1 || calls[0].operation != "list_sidecars" {
		t.Fatalf("expected only the sync list call, got %+v", calls)
	}
	for _, call := range calls {
		if !call.hasMetadata || call.metadata.Priority != platformpriority.PriorityBackground || call.metadata.BackgroundSubclass != platformpriority.BackgroundSubclassLow {
			t.Fatalf("sidecar worker used wrong priority metadata: %+v", call)
		}
		if !call.hasDeadline || call.timeout <= 0 || call.timeout > 31*time.Second {
			t.Fatalf("sidecar worker did not receive bounded timeout context: %+v", call)
		}
	}
}

func TestSidecarRetentionManagedTablesMatchCurrentLogSet(t *testing.T) {
	expected := []string{"request_logs", "audit_logs", "usage_request_events", "loadbalance_events"}
	managed := logretention.ManagedTables()
	if len(managed) != len(expected) {
		t.Fatalf("managed retention tables = %v want %v", managed, expected)
	}
	for index := range expected {
		if managed[index] != expected[index] {
			t.Fatalf("managed retention tables = %v want %v", managed, expected)
		}
	}
}

func TestSidecarWorkerPriorityLifecycleAvoidsRuntimeLanes(t *testing.T) {
	lifecycle := readSidecarPriorityBackendFile(t, "internal/platform/lifecycle/production.go")
	for _, marker := range []string{
		"managementPool := databasePools.Management.Raw()",
		"sidecarsService, err := managementsidecars.NewService(settings, managementsidecars.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: managementPool})",
		"sidecarsService.RegisterBackgroundWorker",
	} {
		if !strings.Contains(lifecycle, marker) {
			t.Fatalf("sidecar lifecycle wiring missing marker %q", marker)
		}
	}
	for _, forbidden := range []string{
		"managementsidecars.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: runtimeExecutionPool}",
		"managementsidecars.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: runtimeTelemetryPool}",
		"managementsidecars.Options{CORSOriginProvider: resources.deps.HotBootstrapConfigRuntime, Pool: runtimeFeedbackPool}",
	} {
		if strings.Contains(lifecycle, forbidden) {
			t.Fatalf("sidecar lifecycle wiring borrowed runtime lane marker %q", forbidden)
		}
	}

	workerSource := readSidecarPriorityBackendFile(t, "internal/httpapi/management/sidecars/worker.go")
	for _, marker := range []string{
		"sidecarSyncWorkerTimeout      = 30 * time.Second",
		"Priority:         background.PriorityLowBackground",
		"MaxPriority:      background.PriorityLowBackground",
		"Timeout:          sidecarSyncWorkerTimeout",
		"sidecar provider sync worker completed",
	} {
		if !strings.Contains(workerSource, marker) {
			t.Fatalf("sidecar worker source missing provider-sync priority/timeout marker %q", marker)
		}
	}

	syncSource := readSidecarPriorityBackendFile(t, "internal/httpapi/management/sidecars/sync.go")
	for _, forbidden := range []string{
		"s.store.ReplaceAuthFiles(ctx, instance.ID",
		"Auth" + "SnapshotCount",
	} {
		if strings.Contains(syncSource, forbidden) {
			t.Fatalf("sidecar provider sync source retained auth inventory sync marker %q", forbidden)
		}
	}
}

type sidecarWorkerCall struct {
	operation   string
	metadata    platformpriority.Metadata
	hasMetadata bool
	hasDeadline bool
	timeout     time.Duration
}

type sidecarObservingStore struct {
	mu    sync.Mutex
	calls []sidecarWorkerCall
}

func (s *sidecarObservingStore) ListSidecarInstances(ctx context.Context) ([]managementsidecars.SidecarInstance, error) {
	s.recordCall(ctx, "list_sidecars")
	return nil, nil
}

func (s *sidecarObservingStore) recordCall(ctx context.Context, operation string) {
	metadata, hasMetadata := platformpriority.MetadataFromContext(ctx)
	deadline, hasDeadline := ctx.Deadline()
	call := sidecarWorkerCall{operation: operation, hasMetadata: hasMetadata, hasDeadline: hasDeadline}
	if metadata != nil {
		call.metadata = *metadata
	}
	if hasDeadline {
		call.timeout = time.Until(deadline)
	}
	s.mu.Lock()
	s.calls = append(s.calls, call)
	s.mu.Unlock()
}

func (s *sidecarObservingStore) callsSnapshot() []sidecarWorkerCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sidecarWorkerCall(nil), s.calls...)
}

var errSidecarPriorityUnexpectedStoreCall = errors.New("unexpected sidecar priority store call")

func (*sidecarObservingStore) CreateSidecarInstance(context.Context, managementsidecars.SidecarInstanceInput) (managementsidecars.SidecarInstance, error) {
	return managementsidecars.SidecarInstance{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) GetSidecarInstance(context.Context, int) (managementsidecars.SidecarInstance, bool, error) {
	return managementsidecars.SidecarInstance{}, false, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) UpdateSidecarInstance(context.Context, int, managementsidecars.SidecarInstanceInput) (managementsidecars.SidecarInstance, error) {
	return managementsidecars.SidecarInstance{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) SoftDeleteSidecarInstance(context.Context, int) (bool, error) {
	return false, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) UpdateSidecarSyncMetadata(context.Context, managementsidecars.SidecarSyncMetadataInput) (managementsidecars.SidecarInstance, error) {
	return managementsidecars.SidecarInstance{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) SaveAuthFile(context.Context, managementsidecars.SidecarAuthFileInput) (managementsidecars.SidecarAuthFile, error) {
	return managementsidecars.SidecarAuthFile{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) ReplaceAuthFiles(context.Context, int, []managementsidecars.SidecarAuthFileInput) ([]managementsidecars.SidecarAuthFile, error) {
	return nil, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) GetAuthFile(context.Context, int, string) (managementsidecars.SidecarAuthFile, bool, error) {
	return managementsidecars.SidecarAuthFile{}, false, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) ListAuthFiles(context.Context, int) ([]managementsidecars.SidecarAuthFile, error) {
	return nil, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) SaveProviderSnapshot(context.Context, managementsidecars.SidecarProviderSnapshotInput) (managementsidecars.SidecarProviderSnapshot, error) {
	return managementsidecars.SidecarProviderSnapshot{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) ReplaceProviderSnapshots(context.Context, int, string, []managementsidecars.SidecarProviderSnapshotInput) ([]managementsidecars.SidecarProviderSnapshot, error) {
	return nil, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) ListProviderSnapshots(context.Context, int) ([]managementsidecars.SidecarProviderSnapshot, error) {
	return nil, errSidecarPriorityUnexpectedStoreCall
}
func readSidecarPriorityBackendFile(t *testing.T, relative string) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	path := filepath.Join(filepath.Dir(current), "..", "..", relative)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read backend file %s: %v", relative, err)
	}
	return string(raw)
}
