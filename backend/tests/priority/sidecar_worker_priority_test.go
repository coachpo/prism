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
	platformpriority "github.com/coachpo/prism/backend/internal/platform/priority"
)

func TestSidecarWorkersRunAsBoundedLowBackgroundJobs(t *testing.T) {
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
	if got := guardScheduler.Submit(context.Background(), background.JobRequest{Worker: managementsidecars.SidecarWatchdogWorkerName, PriorityOverride: background.PriorityNormalBackground}); got.Status != background.SubmitRejectedInvalidPriority {
		t.Fatalf("sidecar watchdog worker accepted elevated priority: %+v", got)
	}

	if err := scheduler.Start(t.Context()); err != nil {
		t.Fatalf("start sidecar worker scheduler: %v", err)
	}
	for _, worker := range []background.WorkerName{managementsidecars.SidecarSyncWorkerName, managementsidecars.SidecarWatchdogWorkerName} {
		result := scheduler.Submit(context.Background(), background.JobRequest{Worker: worker})
		if result.Status != background.SubmitAccepted {
			t.Fatalf("submit %s status = %s reason=%s", worker, result.Status, result.Reason)
		}
	}
	drain := scheduler.Drain(context.Background(), time.Now().Add(2*time.Second))
	if drain.TimedOut || drain.Failed != 0 || drain.Completed != 2 {
		t.Fatalf("sidecar worker drain result = %+v", drain)
	}

	calls := store.callsSnapshot()
	if len(calls) != 2 {
		t.Fatalf("expected sync and watchdog workers to touch the store once each, got %+v", calls)
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

func TestSidecarWorkerLifecycleAvoidsRuntimeLanes(t *testing.T) {
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
		"sidecarWatchdogWorkerTimeout      = 30 * time.Second",
		"Priority:         background.PriorityLowBackground",
		"MaxPriority:      background.PriorityLowBackground",
		"Timeout:          sidecarSyncWorkerTimeout",
		"Timeout:          sidecarWatchdogWorkerTimeout",
	} {
		if !strings.Contains(workerSource, marker) {
			t.Fatalf("sidecar worker source missing priority/timeout marker %q", marker)
		}
	}
	if strings.Contains(workerSource, "PriorityNormalBackground") || strings.Contains(workerSource, "PriorityHighBackground") {
		t.Fatalf("sidecar worker source must not register normal/high background priority")
	}
}

type sidecarWorkerCall struct {
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
	metadata, hasMetadata := platformpriority.MetadataFromContext(ctx)
	deadline, hasDeadline := ctx.Deadline()
	call := sidecarWorkerCall{hasMetadata: hasMetadata, hasDeadline: hasDeadline}
	if metadata != nil {
		call.metadata = *metadata
	}
	if hasDeadline {
		call.timeout = time.Until(deadline)
	}
	s.mu.Lock()
	s.calls = append(s.calls, call)
	s.mu.Unlock()
	return nil, nil
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
func (*sidecarObservingStore) SaveAuthSnapshot(context.Context, managementsidecars.SidecarAuthSnapshotInput) (managementsidecars.SidecarAuthSnapshot, error) {
	return managementsidecars.SidecarAuthSnapshot{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) ReplaceAuthSnapshots(context.Context, int, []managementsidecars.SidecarAuthSnapshotInput) ([]managementsidecars.SidecarAuthSnapshot, error) {
	return nil, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) GetAuthSnapshot(context.Context, int, string) (managementsidecars.SidecarAuthSnapshot, bool, error) {
	return managementsidecars.SidecarAuthSnapshot{}, false, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) ListAuthSnapshots(context.Context, int) ([]managementsidecars.SidecarAuthSnapshot, error) {
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
func (*sidecarObservingStore) GetOrCreateWatchdogPolicy(context.Context, int) (managementsidecars.SidecarWatchdogPolicy, error) {
	return managementsidecars.SidecarWatchdogPolicy{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) UpsertWatchdogPolicy(context.Context, managementsidecars.SidecarWatchdogPolicyInput) (managementsidecars.SidecarWatchdogPolicy, error) {
	return managementsidecars.SidecarWatchdogPolicy{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) CreateWatchdogHold(context.Context, managementsidecars.SidecarWatchdogHoldInput) (managementsidecars.SidecarWatchdogHold, error) {
	return managementsidecars.SidecarWatchdogHold{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) GetActiveWatchdogHold(context.Context, int, string) (managementsidecars.SidecarWatchdogHold, bool, error) {
	return managementsidecars.SidecarWatchdogHold{}, false, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) ListActiveWatchdogHolds(context.Context, int) ([]managementsidecars.SidecarWatchdogHold, error) {
	return nil, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) UpdateWatchdogHold(context.Context, int, managementsidecars.SidecarWatchdogHoldInput) (managementsidecars.SidecarWatchdogHold, error) {
	return managementsidecars.SidecarWatchdogHold{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) CreateWatchdogAction(context.Context, managementsidecars.SidecarWatchdogActionInput) (managementsidecars.SidecarWatchdogAction, error) {
	return managementsidecars.SidecarWatchdogAction{}, errSidecarPriorityUnexpectedStoreCall
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
