package loadbalance

import (
	"testing"
	"time"
)

func TestRuntimeLocalAdmissionRespectsQPSAndInflight(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	policy := runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}
	nowAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)

	t.Run("qps window", func(t *testing.T) {
		qpsLimit := 1
		decision := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{
			ProfileID:     1,
			ModelConfigID: 10,
			ConnectionID:  100,
			Admission:     RuntimeConnectionAdmission{QPSLimit: &qpsLimit},
			Policy:        policy,
			ObservedAt:    nowAt,
		})
		if decision.AdmissionReason != "" || decision.Skipped {
			t.Fatalf("expected first qps-limited attempt to acquire, got %+v", decision)
		}
		store.FinishConnectionAttempt(decision.Handle, nowAt.Add(10*time.Millisecond))

		second := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{
			ProfileID:     1,
			ModelConfigID: 10,
			ConnectionID:  100,
			Admission:     RuntimeConnectionAdmission{QPSLimit: &qpsLimit},
			Policy:        policy,
			ObservedAt:    nowAt.Add(500 * time.Millisecond),
		})
		if second.AdmissionReason != "qps_limit" {
			t.Fatalf("expected qps_limit rejection, got %+v", second)
		}

		third := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{
			ProfileID:     1,
			ModelConfigID: 10,
			ConnectionID:  100,
			Admission:     RuntimeConnectionAdmission{QPSLimit: &qpsLimit},
			Policy:        policy,
			ObservedAt:    nowAt.Add(1100 * time.Millisecond),
		})
		if third.AdmissionReason != "" || third.Skipped {
			t.Fatalf("expected qps window reset to allow the next attempt, got %+v", third)
		}
	})

	t.Run("non-stream in-flight", func(t *testing.T) {
		maxInFlight := 1
		first := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{
			ProfileID:     1,
			ModelConfigID: 11,
			ConnectionID:  101,
			Admission:     RuntimeConnectionAdmission{MaxInFlightNonStream: &maxInFlight},
			Policy:        policy,
			ObservedAt:    nowAt,
		})
		if first.AdmissionReason != "" || first.Skipped {
			t.Fatalf("expected first non-stream attempt to acquire, got %+v", first)
		}
		second := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{
			ProfileID:     1,
			ModelConfigID: 11,
			ConnectionID:  101,
			Admission:     RuntimeConnectionAdmission{MaxInFlightNonStream: &maxInFlight},
			Policy:        policy,
			ObservedAt:    nowAt.Add(5 * time.Millisecond),
		})
		if second.AdmissionReason != "max_in_flight_non_stream" {
			t.Fatalf("expected max_in_flight_non_stream rejection, got %+v", second)
		}
		store.FinishConnectionAttempt(first.Handle, nowAt.Add(20*time.Millisecond))
	})

	t.Run("stream in-flight", func(t *testing.T) {
		maxInFlight := 1
		first := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{
			ProfileID:     1,
			ModelConfigID: 12,
			ConnectionID:  102,
			Admission:     RuntimeConnectionAdmission{MaxInFlightStream: &maxInFlight},
			Policy:        policy,
			IsStreaming:   true,
			ObservedAt:    nowAt,
		})
		if first.AdmissionReason != "" || first.Skipped {
			t.Fatalf("expected first stream attempt to acquire, got %+v", first)
		}
		second := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{
			ProfileID:     1,
			ModelConfigID: 12,
			ConnectionID:  102,
			Admission:     RuntimeConnectionAdmission{MaxInFlightStream: &maxInFlight},
			Policy:        policy,
			IsStreaming:   true,
			ObservedAt:    nowAt.Add(5 * time.Millisecond),
		})
		if second.AdmissionReason != "max_in_flight_stream" {
			t.Fatalf("expected max_in_flight_stream rejection, got %+v", second)
		}
		store.FinishConnectionAttempt(first.Handle, nowAt.Add(20*time.Millisecond))
	})
}

func TestRuntimeLocalCircuitTransitions(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	nowAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	strategy := RuntimeStrategy{StrategyType: "adaptive", RoutingPolicyRaw: mustLoadbalanceJSON(t, map[string]any{
		"circuit_breaker": map[string]any{"failure_threshold": 1, "base_open_seconds": 60, "backoff_multiplier": 2.0, "max_open_seconds": 900, "ban_mode": "off", "max_open_strikes_before_ban": 0, "ban_duration_seconds": 0},
	})}
	first := store.RecordRuntimeFailoverHTTPFailure(1, 20, 200, strategy, nowAt)
	if first.CurrentState.CircuitState != "open" || first.CurrentState.ConsecutiveFailures != 1 || first.CurrentState.LastCooldownSeconds != 60 {
		t.Fatalf("expected first failure to open the circuit, got %+v", first.CurrentState)
	}
	second := store.RecordRuntimeTransportFailure(1, 20, 200, strategy, nowAt.Add(time.Second))
	if second.CurrentState.CircuitState != "open" || second.CurrentState.ConsecutiveFailures != 2 || second.CurrentState.LastCooldownSeconds != 120 {
		t.Fatalf("expected second failure to extend the open interval, got %+v", second.CurrentState)
	}
	if second.CurrentState.LastFailureKind == nil || *second.CurrentState.LastFailureKind != runtimeFailureKindConnectError {
		t.Fatalf("expected transport failure marker, got %+v", second.CurrentState)
	}
	third := store.RecordRuntimeSuccess(1, 20, 200, strategy, 321, nowAt.Add(2*time.Second))
	if !third.RecoveryEventEligible || third.CurrentState.CircuitState != "closed" || third.CurrentState.ConsecutiveFailures != 0 {
		t.Fatalf("expected success to recover and close the circuit, got %+v", third)
	}
	if third.CurrentState.LiveP95LatencyMS == nil || *third.CurrentState.LiveP95LatencyMS != 321 {
		t.Fatalf("expected success to retain latency memory, got %+v", third.CurrentState)
	}

	banStrategy := RuntimeStrategy{StrategyType: "adaptive", RoutingPolicyRaw: mustLoadbalanceJSON(t, map[string]any{
		"circuit_breaker": map[string]any{"failure_threshold": 1, "base_open_seconds": 60, "backoff_multiplier": 2.0, "max_open_seconds": 900, "ban_mode": "temporary", "max_open_strikes_before_ban": 1, "ban_duration_seconds": 300},
	})}
	banned := store.RecordRuntimeFailoverHTTPFailure(1, 21, 201, banStrategy, nowAt)
	if banned.CurrentState.BanMode != "temporary" || banned.CurrentState.BannedUntilAt == nil {
		t.Fatalf("expected temporary ban escalation, got %+v", banned.CurrentState)
	}
}

func TestRuntimeLocalHalfOpenProbeExclusivity(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	nowAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	probeEligibleAt := nowAt.Add(-1 * time.Minute)
	store.SeedConnectionState(1, 30, 300, RuntimeConnectionState{
		ConnectionID:        300,
		CircuitState:        "open",
		BanMode:             "off",
		OpenUntilAt:         &probeEligibleAt,
		ProbeAvailableAt:    &probeEligibleAt,
		ConsecutiveFailures: 1,
		LastCooldownSeconds: 60,
	}, probeEligibleAt, nowAt)

	first := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{ProfileID: 1, ModelConfigID: 30, ConnectionID: 300, Policy: runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}, ObservedAt: nowAt})
	if first.AdmissionReason != "" || first.Skipped {
		t.Fatalf("expected first probe attempt to acquire, got %+v", first)
	}
	stateWhileHeld, ok := store.SnapshotConnectionState(1, 300)
	if !ok || stateWhileHeld.CircuitState != "half_open" {
		t.Fatalf("expected half_open snapshot while probe is held, got %+v ok=%t", stateWhileHeld, ok)
	}
	second := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{ProfileID: 1, ModelConfigID: 30, ConnectionID: 300, Policy: runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}, ObservedAt: nowAt.Add(10 * time.Millisecond)})
	if !second.Skipped || second.AdmissionReason != "" {
		t.Fatalf("expected second probe attempt to skip while the first holds exclusivity, got %+v", second)
	}
	store.FinishConnectionAttempt(first.Handle, nowAt.Add(20*time.Millisecond))
	third := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{ProfileID: 1, ModelConfigID: 30, ConnectionID: 300, Policy: runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}, ObservedAt: nowAt.Add(30 * time.Millisecond)})
	if third.AdmissionReason != "" || third.Skipped {
		t.Fatalf("expected probe exclusivity to release after completion, got %+v", third)
	}
}

func TestRuntimeLocalRoundRobinCursorAdvancesOncePerLaunch(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	strategy := RuntimeStrategy{StrategyType: "legacy", LegacyStrategyType: stringPointer("round-robin")}
	connections := []ConnectionOrderCandidate{{ID: 1, Priority: 0}, {ID: 2, Priority: 1}}
	nowAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)

	first, err := OrderConnectionIDs(1, 40, strategy, connections, nil, store, nowAt)
	if err != nil {
		t.Fatalf("order round-robin launch one: %v", err)
	}
	second, err := OrderConnectionIDs(1, 40, strategy, connections, nil, store, nowAt.Add(time.Millisecond))
	if err != nil {
		t.Fatalf("order round-robin launch two: %v", err)
	}
	if len(first) != 2 || len(second) != 2 || first[0] == second[0] {
		t.Fatalf("expected round-robin launches to rotate primaries, got first=%v second=%v", first, second)
	}
	if cursor := store.PeekRoundRobinCursor(1, 40, 2); cursor != 0 {
		t.Fatalf("expected next cursor to wrap after two launches, got %d", cursor)
	}
}

func TestRuntimeLocalProxyWeightedCursorUsesSeparateWeightedKeys(t *testing.T) {
	store := NewLocalRuntimeStateStore()

	if got := store.ClaimProxyWeightedCursor(1, 60, 0); got != 0 {
		t.Fatalf("expected invalid total weight to return 0, got %d", got)
	}
	for index, want := range []int{0, 1, 2, 3, 0} {
		if got := store.ClaimProxyWeightedCursor(1, 60, 4); got != want {
			t.Fatalf("weighted cursor step %d: expected %d, got %d", index, want, got)
		}
	}
	if got := store.ClaimProxyWeightedCursor(1, 60, 3); got != 0 {
		t.Fatalf("expected changed total weight to use a separate key, got %d", got)
	}
	if got := store.ClaimRoundRobinCursor(1, 60, 2); got != 0 {
		t.Fatalf("expected native round-robin cursor to stay separate, got %d", got)
	}
	store.ClaimProxyWeightedCursor(2, 60, 4)
	store.ResetProfile(2)
	if got := store.ClaimProxyWeightedCursor(2, 60, 4); got != 0 {
		t.Fatalf("expected profile reset to clear proxy weighted cursor, got %d", got)
	}
	store.ClaimProxyWeightedCursor(1, 60, 4)
	store.ResetAll()
	if got := store.ClaimProxyWeightedCursor(1, 60, 4); got != 0 {
		t.Fatalf("expected full reset to clear proxy weighted cursor, got %d", got)
	}
}

func TestRuntimeRestartResetsEphemeralRuntimeStateSafely(t *testing.T) {
	beforeRestart := NewLocalRuntimeStateStore()
	nowAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	strategy := RuntimeStrategy{StrategyType: "adaptive", RoutingPolicyRaw: mustLoadbalanceJSON(t, map[string]any{
		"circuit_breaker": map[string]any{"failure_threshold": 1, "base_open_seconds": 60, "backoff_multiplier": 2.0, "max_open_seconds": 900, "ban_mode": "off", "max_open_strikes_before_ban": 0, "ban_duration_seconds": 0},
	})}
	beforeRestart.RecordRuntimeFailoverHTTPFailure(1, 50, 500, strategy, nowAt)
	maxInFlight := 1
	handle := beforeRestart.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{ProfileID: 1, ModelConfigID: 50, ConnectionID: 501, Admission: RuntimeConnectionAdmission{MaxInFlightNonStream: &maxInFlight}, Policy: runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}, ObservedAt: nowAt.Add(time.Second)})
	if handle.AdmissionReason != "" || handle.Skipped {
		t.Fatalf("expected pre-restart in-flight ownership to acquire on a healthy connection, got %+v", handle)
	}
	beforeRestart.ClaimRoundRobinCursor(1, 50, 2)

	openedState, ok := beforeRestart.SnapshotConnectionState(1, 500)
	if !ok || openedState.CircuitState == "closed" {
		t.Fatalf("expected pre-restart open-circuit state to be populated, got %+v ok=%t", openedState, ok)
	}
	inFlightState, ok := beforeRestart.SnapshotConnectionState(1, 501)
	if !ok || inFlightState.InFlightNonStream != 1 {
		t.Fatalf("expected pre-restart in-flight ownership to be populated, got %+v ok=%t", inFlightState, ok)
	}

	afterRestart := NewLocalRuntimeStateStore()
	if _, ok := afterRestart.SnapshotConnectionState(1, 500); ok {
		t.Fatal("expected restart to drop prior open-circuit runtime state")
	}
	if _, ok := afterRestart.SnapshotConnectionState(1, 501); ok {
		t.Fatal("expected restart to drop prior in-flight ownership")
	}
	if cursor := afterRestart.PeekRoundRobinCursor(1, 50, 2); cursor != 0 {
		t.Fatalf("expected restart to clear round-robin cursor ownership, got %d", cursor)
	}
	fresh := afterRestart.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{ProfileID: 1, ModelConfigID: 50, ConnectionID: 501, Admission: RuntimeConnectionAdmission{MaxInFlightNonStream: &maxInFlight}, Policy: runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}, ObservedAt: nowAt.Add(2 * time.Second)})
	if fresh.AdmissionReason != "" || fresh.Skipped {
		t.Fatalf("expected restart to begin from a safe empty baseline, got %+v", fresh)
	}
}
