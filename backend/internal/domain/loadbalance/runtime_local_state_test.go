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

func TestRuntimeLocalRetryCycleAndBanTransitions(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	nowAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	strategy := RuntimeStrategy{RetryBaseDelayMS: 1000, RetryBackoffMultiplier: 2, RetryMaxDelayMS: 10_000, CycleRetryAttemptLimit: 2, BanMode: "temporary", BanCumulativeRetryAttemptThreshold: 5, BanDurationSeconds: 300}

	first := store.RecordRuntimeFailoverHTTPFailure(1, 20, 200, strategy, nowAt)
	if first.CurrentState.CycleRetryAttempts != 1 || first.CurrentState.CumulativeRetryAttempts != 1 || first.CurrentState.LastRetryDelayMS != 1000 || first.CurrentState.NextRetryAt == nil {
		t.Fatalf("expected first failure to schedule retry state, got %+v", first.CurrentState)
	}
	second := store.RecordRuntimeTransportFailure(1, 20, 200, strategy, nowAt.Add(500*time.Millisecond))
	if second.CurrentState.CycleRetryAttempts != 2 || second.CurrentState.CumulativeRetryAttempts != 2 || second.CurrentState.LastRetryDelayMS != 2000 {
		t.Fatalf("expected second same-cycle failure to exhaust retry cycle, got %+v", second.CurrentState)
	}
	if second.CurrentState.LastFailureKind == nil || *second.CurrentState.LastFailureKind != runtimeFailureKindConnectError {
		t.Fatalf("expected transport failure marker, got %+v", second.CurrentState)
	}
	retryExhaustedPayload := buildRuntimeFailureEventPayload(second.CurrentState, strategy, runtimeFailureKindConnectError)
	if retryExhaustedPayload.EventType != "retry_exhausted" {
		t.Fatalf("expected cycle exhaustion below ban threshold to record retry_exhausted, got %+v", retryExhaustedPayload)
	}
	var bannedTransition RuntimeStateTransition
	for attempt := 0; attempt < 3; attempt++ {
		bannedTransition = store.RecordRuntimeTransportFailure(1, 20, 200, strategy, nowAt.Add(time.Duration(attempt+2)*time.Second))
	}
	banned := bannedTransition.CurrentState
	if banned.BanMode != "temporary" || banned.BannedUntilAt == nil || banned.CumulativeRetryAttempts != 5 {
		t.Fatalf("expected cumulative failures at the configured ban threshold to ban temporarily, got %+v", banned)
	}
	bannedPayload := buildRuntimeFailureEventPayload(banned, strategy, runtimeFailureKindConnectError)
	if bannedPayload.EventType != "banned" {
		t.Fatalf("expected threshold ban to record banned event, got %+v", bannedPayload)
	}
	expiredAt := banned.BannedUntilAt.Add(time.Millisecond)
	expiredDecision := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{ProfileID: 1, ModelConfigID: 20, ConnectionID: 200, Policy: runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}, ObservedAt: expiredAt})
	if expiredDecision.Skipped || expiredDecision.AdmissionReason != "" {
		t.Fatalf("expected temporary ban expiry to allow connection, got %+v", expiredDecision)
	}
	expiredState, ok := store.SnapshotConnectionState(1, 200)
	if !ok || expiredState.BanMode != "off" || expiredState.BannedUntilAt != nil || expiredState.CycleRetryAttempts != 5 || expiredState.CumulativeRetryAttempts != 5 {
		t.Fatalf("expected temporary ban expiry to clear only active ban state, got %+v ok=%t", expiredState, ok)
	}
	third := store.RecordRuntimeSuccess(1, 20, 200, strategy, 321, expiredAt.Add(time.Second))
	if !third.RecoveryEventEligible || third.CurrentState.CycleRetryAttempts != 0 || third.CurrentState.CumulativeRetryAttempts != 0 || third.CurrentState.BanMode != "off" || third.CurrentState.NextRetryAt != nil {
		t.Fatalf("expected success to clear retry and ban state, got %+v", third)
	}
	if third.CurrentState.LiveP95LatencyMS == nil || *third.CurrentState.LiveP95LatencyMS != 321 || third.CurrentState.LastSuccessAt == nil {
		t.Fatalf("expected success to retain latency and timestamp, got %+v", third.CurrentState)
	}

	untilResetStore := NewLocalRuntimeStateStore()
	untilResetStrategy := RuntimeStrategy{RetryBaseDelayMS: 1000, CycleRetryAttemptLimit: 1, BanMode: "until_reset", BanCumulativeRetryAttemptThreshold: 2}
	untilResetStore.RecordRuntimeTransportFailure(1, 21, 201, untilResetStrategy, nowAt)
	untilResetTransition := untilResetStore.RecordRuntimeTransportFailure(1, 21, 201, untilResetStrategy, nowAt.Add(time.Second))
	untilResetState := untilResetTransition.CurrentState
	if untilResetState.BanMode != "until_reset" || untilResetState.BannedUntilAt != nil || untilResetState.CumulativeRetryAttempts != 2 {
		t.Fatalf("expected until_reset to ban indefinitely at threshold without banned_until_at, got %+v", untilResetState)
	}
	untilResetPayload := buildRuntimeFailureEventPayload(untilResetState, untilResetStrategy, runtimeFailureKindConnectError)
	if untilResetPayload.EventType != "banned" {
		t.Fatalf("expected until_reset threshold ban to record banned event, got %+v", untilResetPayload)
	}
	futureDecision := untilResetStore.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{ProfileID: 1, ModelConfigID: 21, ConnectionID: 201, Policy: runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}, ObservedAt: nowAt.Add(24 * time.Hour)})
	if !futureDecision.Skipped {
		t.Fatalf("expected until_reset ban to remain ineligible without reset, got %+v", futureDecision)
	}
	if !untilResetStore.ResetConnection(1, 201) {
		t.Fatal("expected reset to clear until_reset connection state")
	}
	if resetState, ok := untilResetStore.SnapshotConnectionState(1, 201); ok {
		t.Fatalf("expected reset to remove retry and ban counters, got %+v", resetState)
	}
}

func TestRuntimeLocalRetryWindowResetsCycleOnly(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	nowAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	strategy := RuntimeStrategy{RetryBaseDelayMS: 1000, RetryBackoffMultiplier: 2, CycleRetryAttemptLimit: 1, BanMode: "temporary", BanCumulativeRetryAttemptThreshold: 2, BanDurationSeconds: 60}
	first := store.RecordRuntimeTransportFailure(1, 30, 300, strategy, nowAt)
	if first.CurrentState.NextRetryAt == nil {
		t.Fatalf("expected retry window, got %+v", first.CurrentState)
	}
	decision := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{ProfileID: 1, ModelConfigID: 30, ConnectionID: 300, Policy: runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}, ObservedAt: first.CurrentState.NextRetryAt.Add(time.Millisecond)})
	if decision.Skipped || decision.AdmissionReason != "" {
		t.Fatalf("expected retry window expiry to allow the next attempt, got %+v", decision)
	}
	state, ok := store.SnapshotConnectionState(1, 300)
	if !ok || state.CycleRetryAttempts != 0 || state.CumulativeRetryAttempts != 1 || state.NextRetryAt != nil {
		t.Fatalf("expected cycle reset with cumulative attempts preserved, got %+v ok=%t", state, ok)
	}
	second := store.RecordRuntimeTransportFailure(1, 30, 300, strategy, first.CurrentState.NextRetryAt.Add(2*time.Millisecond))
	if second.CurrentState.CycleRetryAttempts != 1 || second.CurrentState.CumulativeRetryAttempts != 2 || second.CurrentState.BanMode != "temporary" || second.CurrentState.BannedUntilAt == nil {
		t.Fatalf("expected preserved cumulative attempts to trigger threshold ban after retry-window expiry, got %+v", second.CurrentState)
	}
}

func TestRuntimeLocalCurrentStateUsesConnectionGlobalRetryState(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	nowAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	strategy := RuntimeStrategy{RetryBaseDelayMS: 500, CycleRetryAttemptLimit: 3, BanMode: "off"}
	store.RecordRuntimeTransportFailure(1, 40, 400, strategy, nowAt)

	items := store.SnapshotCurrentState(1, 41, []int{400}, nowAt.Add(100*time.Millisecond))
	if len(items) != 1 {
		t.Fatalf("expected shared connection retry state to be visible to another model, got %+v", items)
	}
	if items[0].State != "retry_wait" || items[0].CycleRetryAttempts != 1 || items[0].CumulativeRetryAttempts != 1 || items[0].NextRetryAt == nil {
		t.Fatalf("expected retry_wait current state for shared connection, got %+v", items[0])
	}
}

func TestRuntimeLocalSharedConnectionGlobalBan(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	nowAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	banStrategy := RuntimeStrategy{RetryBaseDelayMS: 1000, CycleRetryAttemptLimit: 1, BanMode: "temporary", BanCumulativeRetryAttemptThreshold: 3, BanDurationSeconds: 300}
	offStrategy := RuntimeStrategy{RetryBaseDelayMS: 1000, CycleRetryAttemptLimit: 1, BanMode: "off"}

	first := store.RecordRuntimeTransportFailure(1, 40, 400, banStrategy, nowAt)
	if first.CurrentState.BanMode != "off" || first.CurrentState.CumulativeRetryAttempts != 1 {
		t.Fatalf("expected first shared failure to stay below ban threshold, got %+v", first.CurrentState)
	}
	second := store.RecordRuntimeTransportFailure(1, 40, 400, banStrategy, nowAt.Add(100*time.Millisecond))
	if second.CurrentState.BanMode != "off" || second.CurrentState.CumulativeRetryAttempts != 2 {
		t.Fatalf("expected second shared failure to stay below ban threshold, got %+v", second.CurrentState)
	}
	bannedTransition := store.RecordRuntimeTransportFailure(1, 40, 400, banStrategy, nowAt.Add(200*time.Millisecond))
	if bannedTransition.CurrentState.BanMode != "temporary" || bannedTransition.CurrentState.BannedUntilAt == nil || bannedTransition.CurrentState.CumulativeRetryAttempts != 3 {
		t.Fatalf("expected first model to temporarily ban shared connection, got %+v", bannedTransition.CurrentState)
	}

	offTransition := store.RecordRuntimeTransportFailure(1, 41, 400, offStrategy, nowAt.Add(300*time.Millisecond))
	if offTransition.CurrentState.BanMode != "temporary" || offTransition.CurrentState.BannedUntilAt == nil {
		t.Fatalf("expected ban_mode off model to preserve shared temporary ban, got %+v", offTransition.CurrentState)
	}

	items := store.SnapshotCurrentState(1, 41, []int{400}, nowAt.Add(400*time.Millisecond))
	if len(items) != 1 || items[0].State != "banned" || items[0].BanMode != "temporary" || items[0].BannedUntilAt == nil {
		t.Fatalf("expected shared temporary ban to make connection unavailable to another model, got %+v", items)
	}
}

func TestRuntimeRetryDelayUsesDeterministicJitterHook(t *testing.T) {
	restore := setRuntimeRetryJitterOffsetForTest(func(maxOffsetMS int) int {
		if maxOffsetMS != 100 {
			t.Fatalf("expected max jitter offset 100ms, got %d", maxOffsetMS)
		}
		return -25
	})
	defer restore()

	policy := RuntimeStrategy{RetryBaseDelayMS: 1000, RetryJitterRatio: 0.1, CycleRetryAttemptLimit: 3}.FeedbackPolicy()
	if got := retryDelayMilliseconds(policy, 1); got != 975 {
		t.Fatalf("expected deterministic jittered retry delay 975ms, got %d", got)
	}
}

func TestRuntimeLocalAdmissionRejectionDoesNotIncrementRetries(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	nowAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	qpsLimit := 1
	policy := runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}
	first := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{ProfileID: 1, ModelConfigID: 50, ConnectionID: 500, Admission: RuntimeConnectionAdmission{QPSLimit: &qpsLimit}, Policy: policy, ObservedAt: nowAt})
	if first.AdmissionReason != "" || first.Skipped {
		t.Fatalf("expected first qps attempt to acquire, got %+v", first)
	}
	store.FinishConnectionAttempt(first.Handle, nowAt.Add(time.Millisecond))
	second := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{ProfileID: 1, ModelConfigID: 50, ConnectionID: 500, Admission: RuntimeConnectionAdmission{QPSLimit: &qpsLimit}, Policy: policy, ObservedAt: nowAt.Add(100 * time.Millisecond)})
	if second.AdmissionReason != "qps_limit" {
		t.Fatalf("expected qps rejection, got %+v", second)
	}
	state, ok := store.SnapshotConnectionState(1, 500)
	if !ok || state.CycleRetryAttempts != 0 || state.CumulativeRetryAttempts != 0 {
		t.Fatalf("expected admission rejection to leave retry counters unchanged, got %+v ok=%t", state, ok)
	}
}

func TestRuntimeLocalRoundRobinCursorAdvancesOncePerLaunch(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	strategy := RuntimeStrategy{LegacyStrategyType: stringPointer("round-robin")}
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

func TestRuntimeLocalFacadeWeightedCursorUsesTargetSetHash(t *testing.T) {
	store := NewLocalRuntimeStateStore()

	if got := store.ClaimProxyWeightedCursor(1, 60, "", 4); got != 0 {
		t.Fatalf("expected blank target-set hash to return 0, got %d", got)
	}
	if got := store.ClaimProxyWeightedCursor(1, 60, "eligible-a", 0); got != 0 {
		t.Fatalf("expected invalid total weight to return 0, got %d", got)
	}
	for index, want := range []int{0, 1, 2, 3, 0} {
		if got := store.ClaimProxyWeightedCursor(1, 60, "eligible-a", 4); got != want {
			t.Fatalf("stable eligible-set cursor step %d: expected %d, got %d", index, want, got)
		}
	}
	if got := store.ClaimProxyWeightedCursor(1, 60, " eligible-a ", 4); got != 1 {
		t.Fatalf("expected trimmed target-set hash to reuse the stable sequence, got %d", got)
	}
	if got := store.ClaimProxyWeightedCursor(1, 60, "eligible-b", 4); got != 0 {
		t.Fatalf("expected different eligible target set to start a separate sequence, got %d", got)
	}
	if got := store.ClaimProxyWeightedCursor(1, 60, "eligible-a", 4); got != 2 {
		t.Fatalf("expected original eligible target set to preserve its deterministic sequence, got %d", got)
	}
	if got := store.ClaimProxyWeightedCursor(1, 60, "eligible-a", 3); got != 0 {
		t.Fatalf("expected changed total weight to use a separate key, got %d", got)
	}
	if got := store.ClaimProxyWeightedCursor(1, 61, "eligible-a", 4); got != 0 {
		t.Fatalf("expected different facade model config to use a separate key, got %d", got)
	}
	if got := store.ClaimProxyWeightedCursor(2, 60, "eligible-a", 4); got != 0 {
		t.Fatalf("expected profile isolation to keep weighted cursors separate, got %d", got)
	}
	if got := store.ClaimRoundRobinCursor(1, 60, 2); got != 0 {
		t.Fatalf("expected native round-robin cursor to stay separate, got %d", got)
	}
}

func TestRuntimeLocalFacadeWeightedCursorResetsOnProfileAndFullReset(t *testing.T) {
	store := NewLocalRuntimeStateStore()

	if got := store.ClaimProxyWeightedCursor(2, 60, "eligible-a", 4); got != 0 {
		t.Fatalf("expected initial weighted cursor state for profile 2, got %d", got)
	}
	if got := store.ClaimProxyWeightedCursor(2, 60, "eligible-a", 4); got != 1 {
		t.Fatalf("expected weighted cursor to advance before profile reset, got %d", got)
	}
	store.ResetProfile(2)
	if got := store.ClaimProxyWeightedCursor(2, 60, "eligible-a", 4); got != 0 {
		t.Fatalf("expected profile reset to clear facade weighted cursor state, got %d", got)
	}

	if got := store.ClaimProxyWeightedCursor(1, 60, "eligible-a", 4); got != 0 {
		t.Fatalf("expected initial weighted cursor state for profile 1, got %d", got)
	}
	if got := store.ClaimProxyWeightedCursor(1, 60, "eligible-a", 4); got != 1 {
		t.Fatalf("expected weighted cursor to advance before full reset, got %d", got)
	}
	store.ResetAll()
	if got := store.ClaimProxyWeightedCursor(1, 60, "eligible-a", 4); got != 0 {
		t.Fatalf("expected full reset to clear facade weighted cursor state, got %d", got)
	}
}

func TestRuntimeRestartResetsEphemeralRuntimeStateSafely(t *testing.T) {
	beforeRestart := NewLocalRuntimeStateStore()
	nowAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	strategy := RuntimeStrategy{RetryBaseDelayMS: 1000, RetryBackoffMultiplier: 2, CycleRetryAttemptLimit: 3, BanMode: "off"}
	beforeRestart.RecordRuntimeFailoverHTTPFailure(1, 50, 500, strategy, nowAt)
	maxInFlight := 1
	handle := beforeRestart.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{ProfileID: 1, ModelConfigID: 50, ConnectionID: 501, Admission: RuntimeConnectionAdmission{MaxInFlightNonStream: &maxInFlight}, Policy: runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}, ObservedAt: nowAt.Add(time.Second)})
	if handle.AdmissionReason != "" || handle.Skipped {
		t.Fatalf("expected pre-restart in-flight ownership to acquire on a healthy connection, got %+v", handle)
	}
	beforeRestart.ClaimRoundRobinCursor(1, 50, 2)

	retryState, ok := beforeRestart.SnapshotConnectionState(1, 500)
	if !ok || retryState.CumulativeRetryAttempts == 0 {
		t.Fatalf("expected pre-restart retry state to be populated, got %+v ok=%t", retryState, ok)
	}
	inFlightState, ok := beforeRestart.SnapshotConnectionState(1, 501)
	if !ok || inFlightState.InFlightNonStream != 1 {
		t.Fatalf("expected pre-restart in-flight ownership to be populated, got %+v ok=%t", inFlightState, ok)
	}

	afterRestart := NewLocalRuntimeStateStore()
	if _, ok := afterRestart.SnapshotConnectionState(1, 500); ok {
		t.Fatal("expected restart to drop prior retry runtime state")
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
