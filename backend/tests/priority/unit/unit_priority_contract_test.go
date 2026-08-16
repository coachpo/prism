package unit_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformdb "github.com/coachpo/prism/backend/internal/platform/db"
	"github.com/coachpo/prism/backend/internal/platform/priority"
)

func TestPriorityUnitContract(t *testing.T) {
	t.Run("priority metadata fails closed", func(t *testing.T) {
		if _, err := priority.RequireMetadata(context.Background()); !errors.Is(err, priority.ErrMissingPriority) {
			t.Fatalf("missing metadata should fail closed with ErrMissingPriority, got %v", err)
		}
		for _, metadata := range []priority.Metadata{
			{Priority: priority.PriorityProxy, ManagementTier: priority.ManagementTierM1},
			{Priority: priority.PriorityManagement},
			{Priority: priority.PriorityBackground, ManagementTier: priority.ManagementTierM3},
		} {
			if err := metadata.Validate(); err == nil {
				t.Fatalf("expected invalid metadata to be rejected: %+v", metadata)
			}
		}
		ctx := priority.WithMetadata(context.Background(), priority.Metadata{Priority: priority.PriorityBackground, BackgroundSubclass: priority.BackgroundSubclassNormal})
		if got, err := priority.RequireMetadata(ctx); err != nil || got.Priority != priority.PriorityBackground || got.BackgroundSubclass != priority.BackgroundSubclassNormal {
			t.Fatalf("expected valid background metadata, got %+v err=%v", got, err)
		}
	})

	t.Run("admission rejects escalation and admits downgrades", func(t *testing.T) {
		controller := admission.NewController(admission.Limits{Proxy: 1, ManagementM1: 1, ManagementM2: 1, ManagementM3: 1, Background: 1})
		parent, release, err := controller.Admit(context.Background(), admission.Spec{Name: "parent M2", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM2}, Timeout: time.Second})
		if err != nil {
			t.Fatalf("admit parent: %v", err)
		}
		defer release()
		child, cancel, err := admission.ChildContext(parent, admission.Spec{Name: "child M3", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3}, Timeout: time.Second})
		if err != nil {
			t.Fatalf("expected M2 parent to allow M3 child downgrade: %v", err)
		}
		cancel()
		if _, err := admission.RequireWorkload(child); err != nil {
			t.Fatalf("child context should carry workload metadata: %v", err)
		}
		_, _, err = admission.ChildContext(parent, admission.Spec{Name: "child M1", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM1}, Timeout: time.Second})
		if !errors.Is(err, admission.ErrPriorityEscalation) {
			t.Fatalf("expected child priority escalation rejection, got %v", err)
		}
	})

	t.Run("scheduler validates queues metadata retry and drain policies", func(t *testing.T) {
		scheduler := background.NewScheduler(background.Config{GlobalQueueLimit: 1})
		if err := scheduler.Register(background.WorkerSpec{Name: "unit_worker", Priority: background.PriorityNormalBackground, QueueLimit: 1, ConcurrencyLimit: 1, DrainPolicy: background.DrainFlush, CoalescePolicy: background.CoalesceNone, RetryPolicy: &background.RetryPolicy{MaxAttempts: 2, Delay: time.Millisecond}}, func(ctx context.Context, _ background.Job) background.JobResult {
			metadata, err := priority.RequireMetadata(ctx)
			if err != nil || metadata.Priority != priority.PriorityBackground || metadata.BackgroundSubclass != priority.BackgroundSubclassNormal {
				t.Errorf("scheduler handler missing background metadata: %+v err=%v", metadata, err)
			}
			return background.JobResult{Status: background.JobSucceeded}
		}); err != nil {
			t.Fatalf("register unit worker: %v", err)
		}
		if got := scheduler.Submit(context.Background(), background.JobRequest{Worker: "unit_worker"}); got.Status != background.SubmitAccepted {
			t.Fatalf("expected first queued job accepted, got %+v", got)
		}
		if got := scheduler.Submit(context.Background(), background.JobRequest{Worker: "unit_worker"}); got.Status != background.SubmitRejectedBackpressure {
			t.Fatalf("expected global queue backpressure, got %+v", got)
		}
		if err := scheduler.Start(context.Background()); err != nil {
			t.Fatalf("start scheduler: %v", err)
		}
		if err := scheduler.Stop(context.Background(), time.Now().Add(time.Second)); err != nil {
			t.Fatalf("stop scheduler: %v", err)
		}
		if got := scheduler.Submit(context.Background(), background.JobRequest{Worker: "unit_worker"}); got.Status != background.SubmitRejectedStopping {
			t.Fatalf("expected stopped scheduler to reject submissions, got %+v", got)
		}
	})

	t.Run("db lane budget and labels stay explicit", func(t *testing.T) {
		budget := config.DefaultPostgresPoolsBudget()
		if err := budget.Validate(); err != nil {
			t.Fatalf("default postgres pool budget invalid: %v", err)
		}
		if budget.SumMaxConns() > 100 {
			t.Fatalf("default budget must stay under postgres default max_connections=100, got sum=%d", budget.SumMaxConns())
		}
		want := []string{"background_jobs", "cache_refresh", "management", "runtime_execution", "runtime_feedback", "runtime_telemetry"}
		if got := strings.Join(platformdb.SortedLaneNames(), ","); got != strings.Join(want, ",") {
			t.Fatalf("unexpected lane names: %s", got)
		}
	})
}
