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

func TestRuntimeStrategyHedgePolicyHonorsConfiguredAdditionalAttemptBudget(t *testing.T) {
	strategy := RuntimeStrategy{
		StrategyType: "adaptive",
		RoutingPolicyRaw: mustLoadbalanceJSON(t, map[string]any{
			"hedge": map[string]any{
				"enabled":                 true,
				"delay_ms":                250,
				"max_additional_attempts": 3,
			},
		}),
	}

	policy := strategy.HedgePolicy()
	if !policy.Enabled {
		t.Fatal("expected adaptive hedge policy to stay enabled")
	}
	if policy.Delay != 250*time.Millisecond {
		t.Fatalf("expected 250ms hedge delay, got %s", policy.Delay)
	}
	if policy.MaxAdditionalAttempts != 3 {
		t.Fatalf("expected max_additional_attempts=3, got %d", policy.MaxAdditionalAttempts)
	}
}

func TestOrderConnectionIDsSingleReturnsOnlyFirstEligibleConnection(t *testing.T) {
	strategy := RuntimeStrategy{StrategyType: "legacy", LegacyStrategyType: stringPointer("single")}
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
	strategy := RuntimeStrategy{StrategyType: "legacy", LegacyStrategyType: stringPointer("fill-first")}
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

	got, err := OrderConnectionIDs(7, 11, strategy, connections, states, nil, nowAt)
	if err != nil {
		t.Fatalf("order connection ids: %v", err)
	}
	want := []int{20, 10}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected adaptive ordering %v, got %v", want, got)
	}
}

func TestOrderConnectionIDsRoundRobinRotatesPrimary(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	store.ClaimRoundRobinCursor(7, 11, 3)
	strategy := RuntimeStrategy{StrategyType: "legacy", LegacyStrategyType: stringPointer("round-robin")}
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
