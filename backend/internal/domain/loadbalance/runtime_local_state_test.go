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
	if third.CurrentState.LastSuccessResponseHeadersLatencyMS == nil || *third.CurrentState.LastSuccessResponseHeadersLatencyMS != 321 || third.CurrentState.LastSuccessAt == nil {
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
	cleared, resetItem := untilResetStore.ResetConnectionCooldown(1, 201, nowAt.Add(24*time.Hour))
	if !cleared {
		t.Fatal("expected reset to clear until_reset cooldown state")
	}
	if resetItem == nil || resetItem.State != "available" {
		t.Fatalf("expected reset to leave available state, got %+v", resetItem)
	}
	if resetState, ok := untilResetStore.SnapshotConnectionState(1, 201); !ok || resetState.BanMode != "off" || resetState.BannedUntilAt != nil || resetState.CycleRetryAttempts != 0 || resetState.CumulativeRetryAttempts != 0 || resetState.LastFailureKind != nil || resetState.LastRetryDelayMS != 0 || resetState.NextRetryAt != nil {
		t.Fatalf("expected reset to clear cooldown fields while preserving state row, got %+v ok=%t", resetState, ok)
	}
}

func TestSnapshotActiveBansDoesNotConsumeExpiredTemporaryBan(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	nowAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	bannedUntil := nowAt.Add(-time.Millisecond)
	store.SeedConnectionState(1, 20, 200, RuntimeConnectionState{
		ConnectionID:            200,
		BanMode:                 "temporary",
		BannedUntilAt:           &bannedUntil,
		CycleRetryAttempts:      2,
		CumulativeRetryAttempts: 5,
		LastRetryDelayMS:        1000,
		LastFailureKind:         stringPointer(runtimeFailureKindConnectError),
	}, nowAt.Add(-time.Hour), nowAt.Add(-time.Minute))

	if activeBans := store.SnapshotActiveBans(1, nowAt); len(activeBans) != 0 {
		t.Fatalf("expected expired temporary ban to be hidden from active incidents, got %+v", activeBans)
	}
	decision := store.TryBeginConnectionAttempt(RuntimeConnectionAttemptInput{ProfileID: 1, ModelConfigID: 20, ConnectionID: 200, Policy: runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}, ObservedAt: nowAt})
	if decision.UnbannedRecord == nil {
		t.Fatalf("expected next runtime attempt to emit unbanned record after snapshot, got %+v", decision)
	}
	if decision.UnbannedRecord.BanMode != "off" || decision.UnbannedRecord.BannedUntilAt != nil {
		t.Fatalf("expected unbanned record to clear temporary ban, got %+v", *decision.UnbannedRecord)
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

func TestRuntimeNarrowCooldownResetPreservesNonCooldownObservation(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	nowAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)

	store.SeedConnectionState(1, 60, 600, RuntimeConnectionState{
		ConnectionID:                        600,
		WindowStartedAt:                     timePointer(nowAt.Add(-30 * time.Second)),
		WindowRequestCount:                  7,
		InFlightNonStream:                   2,
		InFlightStream:                      1,
		CycleRetryAttempts:                  3,
		CumulativeRetryAttempts:             4,
		NextRetryAt:                         timePointer(nowAt.Add(5 * time.Second)),
		LastRetryDelayMS:                    1500,
		BanMode:                             "until_reset",
		BannedUntilAt:                       nil,
		LastFailureKind:                     stringPointer(runtimeFailureKindTransientHTTP),
		LastSuccessAt:                       timePointer(nowAt.Add(-2 * time.Minute)),
		LastSuccessResponseHeadersLatencyMS: intPointer(412),
	}, nowAt.Add(-1*time.Hour), nowAt.Add(-1*time.Minute))

	// Advance the round-robin cursor so preservation is observable.
	store.ClaimRoundRobinCursor(1, 60, 3)
	store.ClaimRoundRobinTargetCursor(1, 60, 9, "target-set-hash", 3)
	if cursor := store.PeekRoundRobinCursor(1, 60, 3); cursor != 1 {
		t.Fatalf("expected round-robin cursor to advance before reset, got %d", cursor)
	}

	item, cleared := store.ResetConnection(1, 600)
	if !cleared || item == nil {
		t.Fatalf("expected cooldown reset to clear fields, got cleared=%t item=%+v", cleared, item)
	}
	if item.State != "available" || item.BanMode != "off" || item.BannedUntilAt != nil || item.NextRetryAt != nil || item.LastFailureKind != nil {
		t.Fatalf("expected reset to clear retry/ban cooldown fields, got %+v", item)
	}
	if item.CycleRetryAttempts != 0 || item.CumulativeRetryAttempts != 0 || item.LastRetryDelayMS != 0 {
		t.Fatalf("expected reset to zero retry counters, got %+v", item)
	}
	if item.WindowRequestCount != 7 || item.WindowStartedAt == nil || item.InFlightNonStream != 2 || item.InFlightStream != 1 {
		t.Fatalf("expected reset to preserve QPS window and in-flight counts, got %+v", item)
	}
	if item.LastSuccessAt == nil || item.LastSuccessResponseHeadersLatencyMS == nil || *item.LastSuccessResponseHeadersLatencyMS != 412 {
		t.Fatalf("expected reset to preserve last success observation and latency, got %+v", item)
	}
	if cursor := store.PeekRoundRobinCursor(1, 60, 3); cursor != 1 {
		t.Fatalf("expected reset to preserve round-robin cursor, got %d", cursor)
	}

	// A second reset has nothing left to clear but still returns the snapshot.
	secondItem, secondCleared := store.ResetConnection(1, 600)
	if secondCleared || secondItem == nil {
		t.Fatalf("expected second reset to report cleared=false with snapshot, got cleared=%t item=%+v", secondCleared, secondItem)
	}

	// No process state for an unknown connection: nil snapshot, not cleared.
	if item, cleared := store.ResetConnection(1, 999); item != nil || cleared {
		t.Fatalf("expected unknown connection reset to return nil snapshot and cleared=false, got %+v %t", item, cleared)
	}
}
