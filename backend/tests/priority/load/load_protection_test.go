package load_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/priority"
)

func TestProxyProtectedUnderManagementAndBackgroundSaturation(t *testing.T) {
	t.Run("admission keeps proxy available under management and background pressure", func(t *testing.T) {
		controller := admission.NewController(admission.Limits{Proxy: 1, ManagementM1: 1, ManagementM2: 2, ManagementM3: 1, Background: 1})
		releases := []func(){}
		defer func() {
			for i := len(releases) - 1; i >= 0; i-- {
				releases[i]()
			}
		}()

		for _, spec := range []admission.Spec{
			{Name: "held M3", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3}, Timeout: time.Second},
			{Name: "held background", Metadata: priority.Metadata{Priority: priority.PriorityBackground, BackgroundSubclass: priority.BackgroundSubclassLow}, Timeout: time.Second},
		} {
			_, release, err := controller.Admit(context.Background(), spec)
			if err != nil {
				t.Fatalf("pre-acquire %s: %v", spec.Name, err)
			}
			releases = append(releases, release)
		}

		_, _, m3Err := controller.Admit(context.Background(), admission.Spec{Name: "rejected M3", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3}, Timeout: time.Second})
		var overload *admission.OverloadError
		if !errors.As(m3Err, &overload) || overload.RetryAfter <= 0 || !strings.Contains(overload.Resource, "M3") {
			t.Fatalf("expected M3 overload with retry metadata, got %v", m3Err)
		}
		_, _, bgErr := controller.Admit(context.Background(), admission.Spec{Name: "rejected background", Metadata: priority.Metadata{Priority: priority.PriorityBackground, BackgroundSubclass: priority.BackgroundSubclassLow}, Timeout: time.Second})
		if !errors.As(bgErr, &overload) || overload.RetryAfter <= 0 || !strings.Contains(overload.Resource, "background") {
			t.Fatalf("expected background overload with retry metadata, got %v", bgErr)
		}

		_, releaseProxy, err := controller.Admit(context.Background(), admission.Spec{Name: "protected proxy", Metadata: priority.Metadata{Priority: priority.PriorityProxy}, Timeout: time.Second})
		if err != nil {
			t.Fatalf("proxy should remain admitted under management/background saturation: %v", err)
		}
		releaseProxy()
		_, releaseM2, err := controller.Admit(context.Background(), admission.Spec{Name: "protected M2", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM2}, Timeout: time.Second})
		if err != nil {
			t.Fatalf("M3 saturation should preserve M2 reserve: %v", err)
		}
		releaseM2()
	})

	t.Run("background pressure is bounded by backpressure or coalescing", func(t *testing.T) {
		scheduler := background.NewScheduler(background.Config{GlobalQueueLimit: 2, PriorityQueueLimit: map[background.PriorityClass]int{background.PriorityNormalBackground: 2}})
		if err := scheduler.Register(background.WorkerSpec{Name: "optional_worker", Priority: background.PriorityNormalBackground, QueueLimit: 1, ConcurrencyLimit: 1, DrainPolicy: background.DrainBestEffort, CoalescePolicy: background.CoalesceNone}, func(context.Context, background.Job) background.JobResult {
			return background.JobResult{Status: background.JobSucceeded}
		}); err != nil {
			t.Fatalf("register optional worker: %v", err)
		}
		if got := scheduler.Submit(context.Background(), background.JobRequest{Worker: "optional_worker"}); got.Status != background.SubmitAccepted {
			t.Fatalf("expected first optional job accepted, got %+v", got)
		}
		if got := scheduler.Submit(context.Background(), background.JobRequest{Worker: "optional_worker"}); got.Status != background.SubmitRejectedBackpressure {
			t.Fatalf("expected optional work to shed under queue pressure, got %+v", got)
		}

		coalescing := background.NewScheduler(background.Config{})
		if err := coalescing.Register(background.WorkerSpec{Name: "refresh_worker", Priority: background.PriorityNormalBackground, QueueLimit: 8, ConcurrencyLimit: 1, DrainPolicy: background.DrainFinishRunning, CoalescePolicy: background.CoalesceMerge, Merge: func(existing any, incoming any) any {
			return existing.(int) + incoming.(int)
		}}, func(context.Context, background.Job) background.JobResult {
			return background.JobResult{Status: background.JobSucceeded}
		}); err != nil {
			t.Fatalf("register coalescing worker: %v", err)
		}
		if got := coalescing.Submit(context.Background(), background.JobRequest{Worker: "refresh_worker", Payload: 1, CoalesceKey: "cache"}); got.Status != background.SubmitAccepted {
			t.Fatalf("expected first cache refresh accepted, got %+v", got)
		}
		if got := coalescing.Submit(context.Background(), background.JobRequest{Worker: "refresh_worker", Payload: 1, CoalesceKey: "cache"}); got.Status != background.SubmitCoalesced {
			t.Fatalf("expected duplicate cache refresh to coalesce, got %+v", got)
		}
	})
}
