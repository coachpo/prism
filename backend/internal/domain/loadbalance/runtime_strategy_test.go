package loadbalance

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func mustLoadbalanceJSON(t *testing.T, value any) []byte {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal loadbalance fixture: %v", err)
	}
	return raw
}

func TestRuntimeStrategyFailoverStatusCodes(t *testing.T) {
	custom := RuntimeStrategy{FailureStatusCodes: []int{408, 429}}
	if got := custom.FailoverStatusCodes(); !reflect.DeepEqual(got, []int{408, 429}) {
		t.Fatalf("expected configured failover codes, got %v", got)
	}

	defaulted := RuntimeStrategy{}
	if got := defaulted.FailoverStatusCodes(); !reflect.DeepEqual(got, defaultRuntimeFailoverStatusCodes) {
		t.Fatalf("expected default failover codes, got %v", got)
	}
}

func TestRuntimeStrategyHedgePolicyDefaultsDisabled(t *testing.T) {
	policy := RuntimeStrategy{}.HedgePolicy()
	if policy.Enabled || policy.Delay != 0 || policy.MaxAdditionalAttempts != 0 {
		t.Fatalf("expected hedge policy to stay disabled for Ban Policy strategies, got %+v", policy)
	}
}

func TestOrderConnectionIDsSingleReturnsOnlyFirstEligibleConnection(t *testing.T) {
	strategy := RuntimeStrategy{LegacyStrategyType: stringPointer("single")}
	connections := []ConnectionOrderCandidate{{ID: 4, Priority: 2}, {ID: 3, Priority: 1}, {ID: 2, Priority: 1}}

	got, err := OrderConnectionIDs(7, 11, strategy, connections, nil, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("order connection ids: %v", err)
	}
	want := []int{2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected single ordering %v, got %v", want, got)
	}
}

func TestOrderConnectionIDsFillFirstPreservesStableFailoverOrder(t *testing.T) {
	strategy := RuntimeStrategy{LegacyStrategyType: stringPointer("fill-first")}
	connections := []ConnectionOrderCandidate{{ID: 4, Priority: 2}, {ID: 3, Priority: 1}, {ID: 2, Priority: 1}}

	got, err := OrderConnectionIDs(7, 11, strategy, connections, nil, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("order connection ids: %v", err)
	}
	want := []int{2, 3, 4}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected fill-first ordering %v, got %v", want, got)
	}
}

func TestOrderConnectionIDsDefaultPreservesStableOrder(t *testing.T) {
	nowAt := time.Date(2026, time.April, 20, 18, 0, 0, 0, time.UTC)
	strategy := RuntimeStrategy{}
	connections := []ConnectionOrderCandidate{{ID: 20, Priority: 1}, {ID: 10, Priority: 0}}

	got, err := OrderConnectionIDs(7, 11, strategy, connections, nil, nil, nowAt)
	if err != nil {
		t.Fatalf("order connection ids: %v", err)
	}
	want := []int{10, 20}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected default ordering %v, got %v", want, got)
	}
}

func TestOrderConnectionIDsRoundRobinRotatesPrimary(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	store.ClaimRoundRobinCursor(7, 11, 3)
	strategy := RuntimeStrategy{LegacyStrategyType: stringPointer("round-robin")}
	connections := []ConnectionOrderCandidate{{ID: 4, Priority: 2}, {ID: 3, Priority: 1}, {ID: 2, Priority: 1}}

	got, err := OrderConnectionIDs(7, 11, strategy, connections, nil, store, time.Date(2026, time.April, 20, 18, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("order connection ids: %v", err)
	}
	want := []int{3, 4, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected round-robin ordering %v, got %v", want, got)
	}
	if cursor := store.PeekRoundRobinCursor(7, 11, 3); cursor != 2 {
		t.Fatalf("expected next cursor 2, got %d", cursor)
	}
}
