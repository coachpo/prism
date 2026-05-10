package sidecars

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestWatchdogDuplicateRunSkipsWithoutSideEffects(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, "http://127.0.0.1:18080", true, 3600)
	blocker := &blockingWatchdogGetStore{persistence: service.store, sidecarID: sidecar.ID, started: make(chan struct{}), release: make(chan struct{})}
	service.store = blocker

	resultCh := make(chan struct {
		result SidecarWatchdogResult
		err    error
	}, 1)
	go func() {
		result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
		resultCh <- struct {
			result SidecarWatchdogResult
			err    error
		}{result: result, err: err}
	}()

	select {
	case <-blocker.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first watchdog run to hold the sidecar lock")
	}
	duplicate, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("duplicate watchdog run should skip without error: %v", err)
	}
	if !duplicate.Skipped || duplicate.SkipReason != "watchdog_already_running" || duplicate.Reconciled || duplicate.ActionCount != 0 {
		t.Fatalf("duplicate watchdog run was not idempotently skipped: %+v", duplicate)
	}

	close(blocker.release)
	select {
	case first := <-resultCh:
		if first.err != nil {
			t.Fatalf("first watchdog run failed: %v", first.err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first watchdog run to finish")
	}
	if actions := listWatchdogActions(t, service, sidecar.ID); len(actions) != 0 {
		t.Fatalf("duplicate watchdog skip should not create action history, got %+v", actions)
	}
}

type blockingWatchdogGetStore struct {
	persistence
	sidecarID int
	started   chan struct{}
	release   chan struct{}
	once      sync.Once
}

func (s *blockingWatchdogGetStore) GetSidecarInstance(ctx context.Context, id int) (SidecarInstance, bool, error) {
	if id == s.sidecarID {
		block := false
		s.once.Do(func() {
			block = true
			close(s.started)
		})
		if block {
			select {
			case <-s.release:
			case <-ctx.Done():
				return SidecarInstance{}, false, ctx.Err()
			}
		}
	}
	return s.persistence.GetSidecarInstance(ctx, id)
}

func (s *blockingWatchdogGetStore) ListWatchdogActions(ctx context.Context, sidecarID int) ([]SidecarWatchdogAction, error) {
	store, ok := s.persistence.(actionHistoryPersistence)
	if !ok {
		return nil, nil
	}
	return store.ListWatchdogActions(ctx, sidecarID)
}
