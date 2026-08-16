package priority

import (
	"testing"

	"github.com/coachpo/prism/backend/internal/platform/logretention"
)

func TestRetentionManagedTablesMatchCurrentLogSet(t *testing.T) {
	expected := []string{"request_logs", "audit_logs", "usage_request_events", "loadbalance_events"}
	managed := logretention.ManagedTables()
	if len(managed) != len(expected) {
		t.Fatalf("managed retention tables = %v want %v", managed, expected)
	}
	for index := range expected {
		if managed[index] != expected[index] {
			t.Fatalf("managed retention tables = %v want %v", managed, expected)
		}
	}
}
