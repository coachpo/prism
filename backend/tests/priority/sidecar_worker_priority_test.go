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

func TestSidecarWorkerPriorityRunsAsBoundedLowBackgroundJobs(t *testing.T) {
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
	if len(calls) != 3 {
		t.Fatalf("expected sync list, watchdog list, and watchdog probe cleanup calls, got %+v", calls)
	}
	if !sidecarPriorityCallsInclude(calls, "cleanup_probe_observations") {
		t.Fatalf("sidecar watchdog worker did not clean probe observations under scheduler context: %+v", calls)
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

func TestSidecarActionHistoryIsOnlyRetainedSidecarWorkerTable(t *testing.T) {
	managed := map[string]bool{}
	for _, table := range logretention.ManagedTables() {
		managed[table] = true
	}
	if !managed["sidecar_watchdog_actions"] {
		t.Fatalf("sidecar action history is missing from managed retention tables: %v", logretention.ManagedTables())
	}
	for _, table := range []string{"sidecar_watchdog_pending_actions", "sidecar_watchdog_sweep_items", "sidecar_quota_probe_observations", "sidecar_auth_quota_states", "sidecar_quota_scan_runs"} {
		if managed[table] {
			t.Fatalf("live sidecar table %s must not be retention-managed: %v", table, logretention.ManagedTables())
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
func (*sidecarObservingStore) ListAuthQuotaStates(context.Context, int) ([]managementsidecars.SidecarAuthQuotaState, error) {
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
func (*sidecarObservingStore) CreateQuotaScanRun(context.Context, managementsidecars.SidecarQuotaScanRunInput) (managementsidecars.SidecarQuotaScanRun, error) {
	return managementsidecars.SidecarQuotaScanRun{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) UpdateQuotaScanRun(context.Context, int, managementsidecars.SidecarQuotaScanRunInput) (managementsidecars.SidecarQuotaScanRun, error) {
	return managementsidecars.SidecarQuotaScanRun{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) GetQuotaScanRun(context.Context, int, int) (managementsidecars.SidecarQuotaScanRun, bool, error) {
	return managementsidecars.SidecarQuotaScanRun{}, false, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) ListQuotaScanRuns(context.Context, int) ([]managementsidecars.SidecarQuotaScanRun, error) {
	return nil, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) CreateWatchdogProbeObservation(context.Context, managementsidecars.SidecarWatchdogProbeObservationInput) (managementsidecars.SidecarWatchdogProbeObservation, error) {
	return managementsidecars.SidecarWatchdogProbeObservation{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) ListWatchdogProbeObservations(context.Context, int, int) ([]managementsidecars.SidecarWatchdogProbeObservation, error) {
	return nil, errSidecarPriorityUnexpectedStoreCall
}
func (s *sidecarObservingStore) CleanupWatchdogProbeObservations(ctx context.Context) (int64, error) {
	s.recordCall(ctx, "cleanup_probe_observations")
	return 0, nil
}
func (*sidecarObservingStore) PersistWatchdogProbeDecision(context.Context, managementsidecars.SidecarWatchdogProbeDecision) (managementsidecars.SidecarWatchdogProbeDecisionResult, error) {
	return managementsidecars.SidecarWatchdogProbeDecisionResult{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) PersistQuotaProbeDecision(context.Context, managementsidecars.SidecarQuotaPersistDecision) (managementsidecars.SidecarQuotaPersistResult, error) {
	return managementsidecars.SidecarQuotaPersistResult{}, errSidecarPriorityUnexpectedStoreCall
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
func (*sidecarObservingStore) ListDueWatchdogHolds(context.Context, int, time.Time) ([]managementsidecars.SidecarWatchdogHold, error) {
	return nil, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) UpdateWatchdogHold(context.Context, int, managementsidecars.SidecarWatchdogHoldInput) (managementsidecars.SidecarWatchdogHold, error) {
	return managementsidecars.SidecarWatchdogHold{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) CreateWatchdogAction(context.Context, managementsidecars.SidecarWatchdogActionInput) (managementsidecars.SidecarWatchdogAction, error) {
	return managementsidecars.SidecarWatchdogAction{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) UpdateWatchdogAction(context.Context, int, managementsidecars.SidecarWatchdogActionInput) (managementsidecars.SidecarWatchdogAction, error) {
	return managementsidecars.SidecarWatchdogAction{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) GetWatchdogActionByHistoryKey(context.Context, int, time.Time, int) (managementsidecars.SidecarWatchdogAction, bool, error) {
	return managementsidecars.SidecarWatchdogAction{}, false, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) CreateWatchdogPendingAction(context.Context, managementsidecars.SidecarWatchdogPendingActionInput) (managementsidecars.SidecarWatchdogPendingAction, error) {
	return managementsidecars.SidecarWatchdogPendingAction{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) UpdateWatchdogPendingAction(context.Context, int, managementsidecars.SidecarWatchdogPendingActionInput) (managementsidecars.SidecarWatchdogPendingAction, error) {
	return managementsidecars.SidecarWatchdogPendingAction{}, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) ListWatchdogPendingActions(context.Context, int) ([]managementsidecars.SidecarWatchdogPendingAction, error) {
	return nil, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) ClaimWatchdogPendingActions(context.Context, int, int) ([]managementsidecars.SidecarWatchdogPendingAction, error) {
	return nil, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) DeleteWatchdogPendingAction(context.Context, int, int) (bool, error) {
	return false, errSidecarPriorityUnexpectedStoreCall
}
func (*sidecarObservingStore) DeleteWatchdogPendingActionByHistoryKey(context.Context, int, time.Time, int) (bool, error) {
	return false, errSidecarPriorityUnexpectedStoreCall
}

func sidecarPriorityCallsInclude(calls []sidecarWorkerCall, operation string) bool {
	for _, call := range calls {
		if call.operation == operation {
			return true
		}
	}
	return false
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
