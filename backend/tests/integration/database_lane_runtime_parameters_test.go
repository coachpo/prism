package integrationtest

import (
	"context"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformdb "github.com/coachpo/prism/backend/internal/platform/db"
)

// TestManagementLaneDisablesJIT proves the management lane negotiates
// jit=off in its connection startup packet while the runtime lanes keep the
// server default. The stats read path plans partitioned statements whose cost
// crosses jit_above_cost at retention scale; JIT then re-compiles on every
// execution instead of amortizing, so the management lane opts out. The
// runtime write path must not inherit that decision from a server-wide
// setting.
func TestManagementLaneDisablesJIT(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openEmptyDatabase(t, testContext, "lane_runtime_parameters")
	defer func() { _ = conn.Close(testContext) }()

	var serverDefault string
	if err := conn.QueryRow(testContext, `SHOW jit`).Scan(&serverDefault); err != nil {
		t.Fatalf("read server jit default: %v", err)
	}
	if serverDefault != "on" {
		t.Skipf("server jit default is %q; the lane contract is only observable against the stock default", serverDefault)
	}

	pools, err := platformdb.OpenDatabasePools(testContext, harness.connectionString("lane_runtime_parameters"), config.DefaultPostgresPoolsBudget())
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	defer pools.Close()

	for _, testCase := range []struct {
		lane string
		pool platformdb.LanePool
		want string
	}{
		{lane: "management", pool: pools.Management, want: "off"},
		{lane: "runtime_execution", pool: pools.RuntimeExecution, want: serverDefault},
		{lane: "runtime_telemetry", pool: pools.RuntimeTelemetry, want: serverDefault},
	} {
		var jit string
		if err := testCase.pool.Raw().QueryRow(testContext, `SHOW jit`).Scan(&jit); err != nil {
			t.Fatalf("read jit on lane %s: %v", testCase.lane, err)
		}
		if jit != testCase.want {
			t.Fatalf("lane %s jit = %q, want %q", testCase.lane, jit, testCase.want)
		}
	}
}
