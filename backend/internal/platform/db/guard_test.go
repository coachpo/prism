package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/priority"
)

func TestPriorityDBGuardRejectsMissingWorkload(t *testing.T) {
	t.Parallel()

	_, err := RequireWorkload(context.Background(), GuardSpec{
		Name:     "management query",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM2},
	})
	if !errors.Is(err, admission.ErrMissingWorkload) {
		t.Fatalf("expected missing workload rejection, got %v", err)
	}
}

func TestPriorityDBGuardRejectsEscalation(t *testing.T) {
	t.Parallel()

	controller := admission.NewController(admission.Limits{ManagementM1: 1, ManagementM2: 2, ManagementM3: 1})
	ctx, release, err := controller.Admit(context.Background(), admission.Spec{
		Name:     "M3 request",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3},
		Timeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("admit M3 request: %v", err)
	}
	defer release()

	_, err = RequireWorkload(ctx, GuardSpec{
		Name:     "M2 query",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM2},
	})
	if !errors.Is(err, ErrPriorityEscalation) {
		t.Fatalf("expected DB priority escalation rejection, got %v", err)
	}
}

func TestPriorityDBGuardAllowsDowngrade(t *testing.T) {
	t.Parallel()

	controller := admission.NewController(admission.Limits{Proxy: 1})
	ctx, release, err := controller.Admit(context.Background(), admission.Spec{
		Name:     "proxy request",
		Metadata: priority.Metadata{Priority: priority.PriorityProxy},
		Timeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("admit proxy request: %v", err)
	}
	defer release()

	workload, err := RequireWorkload(ctx, GuardSpec{
		Name:     "background telemetry write",
		Metadata: priority.Metadata{Priority: priority.PriorityBackground, BackgroundSubclass: priority.BackgroundSubclassHigh},
	})
	if err != nil {
		t.Fatalf("expected downgraded DB work to be allowed: %v", err)
	}
	if workload.Metadata.Priority != priority.PriorityProxy {
		t.Fatalf("expected original workload metadata to remain proxy, got %+v", workload.Metadata)
	}
}
