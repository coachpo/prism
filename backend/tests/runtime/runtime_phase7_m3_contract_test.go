package runtimetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/priority"
)

func TestM3ManagementShedIncludesRetryAfter(t *testing.T) {
	controller := admission.NewController(admission.Limits{ManagementM3: 1})
	_, release, err := controller.Admit(context.Background(), admission.Spec{Name: "held m3", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("hold M3 lane: %v", err)
	}
	defer release()
	_, _, err = controller.Admit(context.Background(), admission.Spec{Name: "shed m3", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3}, Timeout: time.Millisecond})
	var overload *admission.OverloadError
	if !errors.As(err, &overload) || overload.RetryAfter <= 0 || overload.Metadata.ManagementTier != priority.ManagementTierM3 {
		t.Fatalf("expected M3 overload with retry-after metadata, got err=%v overload=%+v", err, overload)
	}
}

func TestManagementWorkDoesNotExhaustRuntimeCapacity(t *testing.T) {
	controller := admission.NewController(admission.Limits{Proxy: 1, ManagementM3: 1, Background: 1})
	_, releaseM3, err := controller.Admit(context.Background(), admission.Spec{Name: "held m3", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("hold M3 lane: %v", err)
	}
	defer releaseM3()
	_, releaseBackground, err := controller.Admit(context.Background(), admission.Spec{Name: "held background", Metadata: priority.Metadata{Priority: priority.PriorityBackground, BackgroundSubclass: priority.BackgroundSubclassLow}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("hold background lane: %v", err)
	}
	defer releaseBackground()
	_, releaseProxy, err := controller.Admit(context.Background(), admission.Spec{Name: "runtime proxy", Metadata: priority.Metadata{Priority: priority.PriorityProxy}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("expected runtime proxy capacity to remain independent of management/background pressure: %v", err)
	}
	releaseProxy()
}
