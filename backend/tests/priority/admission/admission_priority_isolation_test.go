package admission_test

import (
	"context"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/priority"
)

func TestAdmissionPriorityIsolation(t *testing.T) {
	t.Parallel()

	controller := admission.NewController(admission.Limits{Proxy: 1, ManagementM1: 1, ManagementM2: 2, ManagementM3: 2, Background: 1})
	_, releaseM3, err := controller.Admit(context.Background(), admission.Spec{
		Name:     "held M3",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3},
		Timeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("hold M3 admission: %v", err)
	}
	defer releaseM3()
	_, releaseBackground, err := controller.Admit(context.Background(), admission.Spec{
		Name:     "held background",
		Metadata: priority.Metadata{Priority: priority.PriorityBackground, BackgroundSubclass: priority.BackgroundSubclassLow},
		Timeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("hold background admission: %v", err)
	}
	defer releaseBackground()

	_, _, err = controller.Admit(context.Background(), admission.Spec{
		Name:     "second M3",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3},
		Timeout:  time.Minute,
	})
	if err == nil {
		t.Fatal("expected M3 saturation to reject another M3 before consuming M2 reserve")
	}

	_, releaseM2, err := controller.Admit(context.Background(), admission.Spec{
		Name:     "M2 protected from M3",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM2},
		Timeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("expected M2 admission despite M3 pressure: %v", err)
	}
	defer releaseM2()
	_, releaseM1, err := controller.Admit(context.Background(), admission.Spec{
		Name:     "M1 protected from M3",
		Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM1},
		Timeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("expected M1 admission despite M3 pressure: %v", err)
	}
	defer releaseM1()
	_, releaseProxy, err := controller.Admit(context.Background(), admission.Spec{
		Name:     "proxy protected from management and background",
		Metadata: priority.Metadata{Priority: priority.PriorityProxy},
		Timeout:  time.Minute,
	})
	if err != nil {
		t.Fatalf("expected proxy admission despite management/background pressure: %v", err)
	}
	defer releaseProxy()

	_, _, err = controller.Admit(context.Background(), admission.Spec{
		Name:     "background full",
		Metadata: priority.Metadata{Priority: priority.PriorityBackground, BackgroundSubclass: priority.BackgroundSubclassLow},
		Timeout:  time.Minute,
	})
	if err == nil {
		t.Fatal("expected saturated background lane to reject background work")
	}
}
