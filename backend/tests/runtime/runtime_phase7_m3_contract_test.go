package runtime_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/priority"
)

func TestM3ShedsManagementAuditBeforeDashboardStats(t *testing.T) {
	admissionSource := runtimePhase7ReadBackendSource(t, "internal/platform/http/admission.go")
	for _, route := range []string{`pattern: "/audit/logs", tier: priority.ManagementTierM3`, `pattern: "/audit/logs/delete-jobs", tier: priority.ManagementTierM3`, `pattern: "/management/jobs", tier: priority.ManagementTierM3`, `pattern: "/stats/dashboard", tier: priority.ManagementTierM3`} {
		if !strings.Contains(admissionSource, route) {
			t.Fatalf("expected M3 admission classification for %s", route)
		}
	}
	if strings.Index(admissionSource, `pattern: "/audit/logs"`) > strings.Index(admissionSource, `pattern: "/stats/dashboard"`) {
		t.Fatalf("expected audit management routes to be classified before dashboard stats in M3 first-shed inventory")
	}
}

func TestM3ThrottlesManagementMaintenanceWorkers(t *testing.T) {
	jobsSource := runtimePhase7ReadBackendSource(t, "internal/platform/managementjobs/jobs.go")
	serverSource := runtimePhase7ReadBackendSource(t, "internal/platform/http/server.go")
	for _, want := range []string{"PriorityLowBackground", "MaxPriority: background.PriorityLowBackground", "QueueLimit: 32", "ConcurrencyLimit: 1"} {
		if !strings.Contains(jobsSource, want) {
			t.Fatalf("expected management jobs worker throttle evidence %q", want)
		}
	}
	if !strings.Contains(serverSource, "backgroundJobsPool := databasePools.BackgroundJobs.Raw()") || !strings.Contains(serverSource, "managementJobs.RegisterBackgroundWorker") {
		t.Fatalf("expected management jobs to use scheduler-owned background_jobs lane")
	}
}

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

func runtimePhase7ReadBackendSource(t *testing.T, relative string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(runtimePhase7BackendRoot(t), relative))
	if err != nil {
		t.Fatalf("read backend source %s: %v", relative, err)
	}
	return string(raw)
}

func runtimePhase7BackendRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
