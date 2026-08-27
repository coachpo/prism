package contracttest

import (
	"os"
	"strings"
	"testing"
)

// TestResolvedTargetIndexesFollowPartitionConvention guards the
// final_execution/route_attempt indexes without requiring a live database. The
// Docker-backed migration suite separately proves the whole migration set.
func TestResolvedTargetIndexesFollowPartitionConvention(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/000026_resolved_target_indexes.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(raw)
	if strings.Contains(strings.ToUpper(sql), " ON ONLY ") {
		t.Fatal("partitioned parent indexes must not use ON ONLY")
	}
	for _, fragment := range []string{
		"idx_request_logs_resolved_target_created",
		"idx_request_logs_terminal_target_actual",
		"idx_usage_request_events_resolved_target_created",
		"idx_usage_request_events_terminal_target_final",
		"WHERE row_kind = 'upstream'",
		"final_attempt_number IS NOT NULL",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration is missing %q", fragment)
		}
	}
}
