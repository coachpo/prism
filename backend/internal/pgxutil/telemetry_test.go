package pgxutil

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPoolLaneForTelemetryIsBounded(t *testing.T) {
	pool := &pgxpool.Pool{}
	if got := poolLaneForTelemetry(pool); got != unknownDBLane {
		t.Fatalf("unregistered lane = %q want %q", got, unknownDBLane)
	}

	RegisterPoolLane(pool, "management")
	if got := poolLaneForTelemetry(pool); got != "management" {
		t.Fatalf("registered lane = %q want management", got)
	}

	RegisterPoolLane(pool, "tenant-derived-lane")
	if got := poolLaneForTelemetry(pool); got != unknownDBLane {
		t.Fatalf("unrecognized lane = %q want %q", got, unknownDBLane)
	}

	UnregisterPoolLane(pool)
	if got := poolLaneForTelemetry(pool); got != unknownDBLane {
		t.Fatalf("unregistered lane after delete = %q want %q", got, unknownDBLane)
	}
}
