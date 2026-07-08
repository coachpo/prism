package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDashboardSpecSmokeContractSync(t *testing.T) {
	apiSpec := readDocsFile(t, "API_SPEC.md")
	smokePlan := readDocsFile(t, "SMOKE_TEST_PLAN.md")
	retiredSnapshotField := strings.Join([]string{"recent", "requests"}, "_")
	retiredRealtimeMessage := strings.Join([]string{"dashboard", "update"}, ".")
	retiredDashboardSnapshotMessage := strings.Join([]string{"dashboard", "snapshot"}, ".")
	retiredDashboardActivityMessage := strings.Join([]string{"dashboard", "activity"}, ".")

	assertDocContains(t, "API_SPEC.md", apiSpec, []string{
		"GET /api/stats/dashboard",
		"stats-only aggregate snapshot",
		"snapshot_revision",
		"source_watermark",
		"GET /api/stats/dashboard/recent-activity?limit=N",
		"activity_watermark",
		"Recent activity links into request-log investigation",
	})
	assertDocContains(t, "SMOKE_TEST_PLAN.md", smokePlan, []string{
		"Dashboard aggregate stats API",
		"stats-only overview snapshot",
		"Dashboard recent activity API",
		"Dashboard REST payload split",
		"GET /api/stats/dashboard",
		"GET /api/stats/dashboard/recent-activity",
	})
	assertDocNotContains(t, "API_SPEC.md", apiSpec, []string{retiredSnapshotField, retiredRealtimeMessage, retiredDashboardSnapshotMessage, retiredDashboardActivityMessage, "WebSocket"})
	assertDocNotContains(t, "SMOKE_TEST_PLAN.md", smokePlan, []string{retiredSnapshotField, retiredRealtimeMessage, retiredDashboardSnapshotMessage, retiredDashboardActivityMessage, "websocket", "WebSocket"})
}

func readDocsFile(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", name))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(raw)
}

func assertDocContains(t *testing.T, name string, content string, needles []string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(content, needle) {
			t.Fatalf("expected %s to contain %q", name, needle)
		}
	}
}

func assertDocNotContains(t *testing.T, name string, content string, needles []string) {
	t.Helper()
	for _, needle := range needles {
		if strings.Contains(content, needle) {
			t.Fatalf("expected %s not to contain retired dashboard contract string %q", name, needle)
		}
	}
}
