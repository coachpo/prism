package admission

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/priority"
)

func TestAdmissionAttachesWorkloadMetadataAndDeadline(t *testing.T) {
	t.Parallel()

	controller := NewController(Limits{ManagementM1: 1, ManagementM2: 1, ManagementM3: 1})
	ctx, release, err := controller.Admit(context.Background(), Spec{
		Name:     "management read",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM2},
		Timeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("admit management workload: %v", err)
	}
	defer release()

	metadata, err := priority.RequireMetadata(ctx)
	if err != nil {
		t.Fatalf("expected priority metadata: %v", err)
	}
	if metadata.Priority != priority.PriorityManagement || metadata.ManagementTier != priority.ManagementTierM2 {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	workload, err := RequireWorkload(ctx)
	if err != nil {
		t.Fatalf("expected workload metadata: %v", err)
	}
	if workload.Name != "management read" {
		t.Fatalf("unexpected workload name: %q", workload.Name)
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("expected admitted context deadline")
	}
}

func TestOverloadErrorIsTyped(t *testing.T) {
	t.Parallel()

	controller := NewController(Limits{ManagementM1: 1})
	_, release, err := controller.Admit(context.Background(), Spec{
		Name:     "held M1",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM1},
		Timeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("hold M1 admission: %v", err)
	}
	defer release()

	_, _, err = controller.Admit(context.Background(), Spec{
		Name:     "second M1",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM1},
		Timeout:  time.Minute,
	})
	var overload *OverloadError
	if !errors.As(err, &overload) {
		t.Fatalf("expected typed overload error, got %v", err)
	}
	if overload.Metadata.ManagementTier != priority.ManagementTierM1 || overload.RetryAfter <= 0 {
		t.Fatalf("unexpected overload metadata: %+v", overload)
	}
}

func TestPriorityEscalationRejectedForChildWork(t *testing.T) {
	t.Parallel()

	controller := NewController(Limits{ManagementM1: 1, ManagementM2: 1, ManagementM3: 1})
	parent, release, err := controller.Admit(context.Background(), Spec{
		Name:     "M2 parent",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM2},
		Timeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("admit parent workload: %v", err)
	}
	defer release()

	child, cancel, err := ChildContext(parent, Spec{
		Name:     "M3 child",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3},
		Timeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("expected downgrade child workload: %v", err)
	}
	cancel()
	childWorkload, err := RequireWorkload(child)
	if err != nil {
		t.Fatalf("expected child workload context: %v", err)
	}
	if childWorkload.Metadata.ManagementTier != priority.ManagementTierM3 {
		t.Fatalf("unexpected child metadata: %+v", childWorkload.Metadata)
	}

	_, _, err = ChildContext(parent, Spec{
		Name:     "M1 child",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM1},
		Timeout:  time.Second,
	})
	if !errors.Is(err, ErrPriorityEscalation) {
		t.Fatalf("expected priority escalation rejection, got %v", err)
	}
}

func TestPriorityChildWorkRejectsMissingParentWorkload(t *testing.T) {
	t.Parallel()

	_, _, err := ChildContext(context.Background(), Spec{
		Name:     "background child",
		Metadata: priority.Metadata{Priority: priority.PriorityBackground, BackgroundSubclass: priority.BackgroundSubclassLow},
		Timeout:  time.Second,
	})
	if !errors.Is(err, ErrMissingWorkload) {
		t.Fatalf("expected missing parent workload rejection, got %v", err)
	}
}

func TestDeadlineCannotExtendParentForChildWork(t *testing.T) {
	t.Parallel()

	controller := NewController(Limits{Proxy: 1})
	parent, release, err := controller.Admit(context.Background(), Spec{
		Name:     "proxy parent",
		Metadata: priority.Metadata{Priority: priority.PriorityProxy},
		Timeout:  25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("admit parent workload: %v", err)
	}
	defer release()
	parentDeadline, ok := parent.Deadline()
	if !ok {
		t.Fatal("expected parent deadline")
	}

	child, cancel, err := ChildContext(parent, Spec{
		Name:     "background child",
		Metadata: priority.Metadata{Priority: priority.PriorityBackground, BackgroundSubclass: priority.BackgroundSubclassLow},
		Timeout:  time.Hour,
	})
	if err != nil {
		t.Fatalf("create child workload: %v", err)
	}
	defer cancel()
	childDeadline, ok := child.Deadline()
	if !ok {
		t.Fatal("expected child deadline")
	}
	if !childDeadline.Equal(parentDeadline) {
		t.Fatalf("expected child deadline to stay within parent deadline %s, got %s", parentDeadline, childDeadline)
	}
}

func TestPriorityAdmissionReservesM2FromM3(t *testing.T) {
	t.Parallel()

	controller := NewController(Limits{ManagementM1: 1, ManagementM2: 2, ManagementM3: 2})
	_, releaseM3, err := controller.Admit(context.Background(), Spec{
		Name:     "M3 holder",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3},
		Timeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("hold M3: %v", err)
	}
	defer releaseM3()

	_, _, err = controller.Admit(context.Background(), Spec{
		Name:     "second M3",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3},
		Timeout:  time.Minute,
	})
	if err == nil {
		t.Fatal("expected second M3 to reject before consuming the reserved M2 slot")
	}

	_, releaseM2, err := controller.Admit(context.Background(), Spec{
		Name:     "M2 after M3",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM2},
		Timeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("expected M2 to use reserved capacity while M3 is held: %v", err)
	}
	defer releaseM2()
}

func TestPriorityAdmissionAllowsOneM3WhenM2HasNoReservableCapacity(t *testing.T) {
	t.Parallel()

	controller := NewController(Limits{ManagementM1: 1, ManagementM2: 1, ManagementM3: 1})
	_, releaseM3, err := controller.Admit(context.Background(), Spec{
		Name:     "single M3",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3},
		Timeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("expected one M3 request when the only M2 slot cannot be reserved: %v", err)
	}
	defer releaseM3()

	_, _, err = controller.Admit(context.Background(), Spec{
		Name:     "second M3",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3},
		Timeout:  time.Minute,
	})
	if err == nil {
		t.Fatal("expected second M3 to reject while the only M3 slot is held")
	}

	_, _, err = controller.Admit(context.Background(), Spec{
		Name:     "M2 without spare capacity",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM2},
		Timeout:  time.Minute,
	})
	if err == nil {
		t.Fatal("expected M2 to reject while M3 holds the only unreservable M2 slot")
	}
}
