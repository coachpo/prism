package loadbalance

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func mustLoadbalanceJSON(t *testing.T, value any) []byte {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal loadbalance fixture: %v", err)
	}
	return raw
}

type panicQueryExecutor struct{}

func (panicQueryExecutor) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec call")
}

func (panicQueryExecutor) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}

func (panicQueryExecutor) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow call")
}

type fakeScanRow struct {
	scan func(...any) error
}

func (row fakeScanRow) Scan(dest ...any) error {
	return row.scan(dest...)
}

type fakeRoundRobinExecutor struct {
	rowID         int
	stateExists   bool
	insertCalls   int
	updateCalls   int
	loadedCursor  int
	updatedCursor int
}

func (exec *fakeRoundRobinExecutor) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(query, "INSERT INTO loadbalance_round_robin_state"):
		exec.insertCalls++
		exec.stateExists = true
		if exec.rowID == 0 {
			exec.rowID = 1
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.Contains(query, "UPDATE loadbalance_round_robin_state"):
		exec.updateCalls++
		cursor, ok := args[1].(int)
		if !ok {
			return pgconn.CommandTag{}, fmt.Errorf("unexpected cursor type %T", args[1])
		}
		exec.updatedCursor = cursor
		exec.loadedCursor = cursor
		return pgconn.NewCommandTag("UPDATE 1"), nil
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (*fakeRoundRobinExecutor) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}

func (exec *fakeRoundRobinExecutor) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if !strings.Contains(query, "FROM loadbalance_round_robin_state") {
		return fakeScanRow{scan: func(...any) error {
			return fmt.Errorf("unexpected QueryRow query: %s", query)
		}}
	}
	if !exec.stateExists {
		return fakeScanRow{scan: func(...any) error { return pgx.ErrNoRows }}
	}
	return fakeScanRow{scan: func(dest ...any) error {
		*dest[0].(*int) = exec.rowID
		*dest[1].(*int) = exec.loadedCursor
		return nil
	}}
}

func TestRuntimeStrategyFailoverStatusCodes(t *testing.T) {
	adaptive := RuntimeStrategy{
		StrategyType:     "adaptive",
		RoutingPolicyRaw: mustLoadbalanceJSON(t, map[string]any{"circuit_breaker": map[string]any{"failure_status_codes": []int{408, 429}}}),
	}
	if got := adaptive.FailoverStatusCodes(); !reflect.DeepEqual(got, []int{408, 429}) {
		t.Fatalf("expected adaptive failover codes, got %v", got)
	}

	legacy := RuntimeStrategy{
		StrategyType:    "legacy",
		AutoRecoveryRaw: mustLoadbalanceJSON(t, map[string]any{"status_codes": []int{500, 503}}),
	}
	if got := legacy.FailoverStatusCodes(); !reflect.DeepEqual(got, []int{500, 503}) {
		t.Fatalf("expected legacy failover codes, got %v", got)
	}

	invalid := RuntimeStrategy{StrategyType: "adaptive", RoutingPolicyRaw: []byte("{")}
	if got := invalid.FailoverStatusCodes(); !reflect.DeepEqual(got, defaultRuntimeFailoverStatusCodes) {
		t.Fatalf("expected default failover codes on invalid payload, got %v", got)
	}
}

func TestOrderConnectionIDsSingleReturnsOnlyFirstEligibleConnection(t *testing.T) {
	strategy := RuntimeStrategy{StrategyType: "legacy", LegacyStrategyType: stringPointer("single")}
	connections := []ConnectionOrderCandidate{{ID: 4, Priority: 2}, {ID: 3, Priority: 1}, {ID: 2, Priority: 1}}

	got, err := OrderConnectionIDs(context.Background(), panicQueryExecutor{}, 7, 11, strategy, connections, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("order connection ids: %v", err)
	}
	want := []int{2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected single ordering %v, got %v", want, got)
	}
}

func TestOrderConnectionIDsFillFirstPreservesStableFailoverOrder(t *testing.T) {
	strategy := RuntimeStrategy{StrategyType: "legacy", LegacyStrategyType: stringPointer("fill-first")}
	connections := []ConnectionOrderCandidate{{ID: 4, Priority: 2}, {ID: 3, Priority: 1}, {ID: 2, Priority: 1}}

	got, err := OrderConnectionIDs(context.Background(), panicQueryExecutor{}, 7, 11, strategy, connections, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("order connection ids: %v", err)
	}
	want := []int{2, 3, 4}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected fill-first ordering %v, got %v", want, got)
	}
}

func TestOrderConnectionIDsAdaptiveRanksHealthierCandidateFirst(t *testing.T) {
	nowAt := time.Date(2026, time.April, 20, 18, 0, 0, 0, time.UTC)
	strategy := RuntimeStrategy{StrategyType: "adaptive"}
	connections := []ConnectionOrderCandidate{{ID: 10, Priority: 0}, {ID: 20, Priority: 1}}
	freshFailureAt := nowAt.Add(-30 * time.Second)
	healthySuccessAt := nowAt.Add(-10 * time.Second)
	highLatency := 900
	lowLatency := 120
	states := map[int]RuntimeConnectionState{
		10: {ConnectionID: 10, CircuitState: "open", LiveP95LatencyMS: &highLatency, LastLiveFailureAt: &freshFailureAt},
		20: {ConnectionID: 20, CircuitState: "closed", LiveP95LatencyMS: &lowLatency, LastLiveSuccessAt: &healthySuccessAt},
	}

	got, err := OrderConnectionIDs(context.Background(), panicQueryExecutor{}, 7, 11, strategy, connections, states, nowAt)
	if err != nil {
		t.Fatalf("order connection ids: %v", err)
	}
	want := []int{20, 10}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected adaptive ordering %v, got %v", want, got)
	}
}

func TestOrderConnectionIDsRoundRobinRotatesPrimary(t *testing.T) {
	exec := &fakeRoundRobinExecutor{rowID: 17, loadedCursor: 1}
	strategy := RuntimeStrategy{StrategyType: "legacy", LegacyStrategyType: stringPointer("round-robin")}
	connections := []ConnectionOrderCandidate{{ID: 4, Priority: 2}, {ID: 3, Priority: 1}, {ID: 2, Priority: 1}}

	got, err := OrderConnectionIDs(context.Background(), exec, 7, 11, strategy, connections, nil, time.Date(2026, time.April, 20, 18, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("order connection ids: %v", err)
	}
	want := []int{3, 4, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected round-robin ordering %v, got %v", want, got)
	}
	if exec.insertCalls != 1 || exec.updateCalls != 1 {
		t.Fatalf("expected one insert and one update, got inserts=%d updates=%d", exec.insertCalls, exec.updateCalls)
	}
	if exec.updatedCursor != 2 {
		t.Fatalf("expected next cursor 2, got %d", exec.updatedCursor)
	}
}

func stringPointer(value string) *string {
	return &value
}
