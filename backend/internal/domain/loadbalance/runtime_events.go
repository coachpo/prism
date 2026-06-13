package loadbalance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type runtimeEventMetadata struct {
	ModelID    *string
	EndpointID *int
}

type PartitionEnsurer interface {
	EnsurePartitionForTime(context.Context, string, time.Time) error
}

type runtimeEventPayload struct {
	EventType                                string
	FailureKind                              *string
	CycleRetryAttempts                       int
	CumulativeRetryAttempts                  int
	NextRetryAt                              *time.Time
	LastRetryDelayMS                         int
	BanMode                                  *string
	PolicyCycleRetryAttemptLimit             *int
	PolicyBanCumulativeRetryAttemptThreshold *int
	BannedUntilAt                            *time.Time
	LastSuccessAt                            *time.Time
}

func insertRuntimeLoadbalanceEvent(ctx context.Context, exec queryExecutor, partitionEnsurer PartitionEnsurer, profileID int, modelConfigID int, connectionID int, observedAt time.Time, payload runtimeEventPayload) error {
	observedAt = observedAt.UTC()
	if partitionEnsurer == nil {
		return fmt.Errorf("loadbalance event partition ensurer unavailable")
	}
	if err := partitionEnsurer.EnsurePartitionForTime(ctx, "loadbalance_events", observedAt); err != nil {
		return fmt.Errorf("ensure loadbalance event partition for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	metadata, err := loadRuntimeEventMetadata(ctx, exec, profileID, modelConfigID, connectionID)
	if err != nil {
		return err
	}
	_, err = exec.Exec(
		ctx,
		`INSERT INTO loadbalance_events (profile_id, connection_id, event_type, failure_kind, cycle_retry_attempts, cumulative_retry_attempts, next_retry_at, last_retry_delay_ms, model_id, endpoint_id, ban_mode, policy_cycle_retry_attempt_limit, policy_ban_cumulative_retry_attempt_threshold, banned_until_at, last_success_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		profileID,
		connectionID,
		payload.EventType,
		nullableStringPointerArg(payload.FailureKind),
		payload.CycleRetryAttempts,
		payload.CumulativeRetryAttempts,
		nullableTimeArg(payload.NextRetryAt),
		payload.LastRetryDelayMS,
		nullableStringPointerArg(metadata.ModelID),
		nullableIntPointerArg(metadata.EndpointID),
		nullableStringPointerArg(payload.BanMode),
		nullableIntPointerArg(payload.PolicyCycleRetryAttemptLimit),
		nullableIntPointerArg(payload.PolicyBanCumulativeRetryAttemptThreshold),
		nullableTimeArg(payload.BannedUntilAt),
		nullableTimeArg(payload.LastSuccessAt),
		observedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert loadbalance event for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func loadRuntimeEventMetadata(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, connectionID int) (runtimeEventMetadata, error) {
	var modelID sql.NullString
	var endpointID sql.NullInt32
	err := exec.QueryRow(
		ctx,
		`SELECT model_configs.model_id, connections.endpoint_id
		FROM model_access_targets
		JOIN connections ON connections.id = model_access_targets.target_connection_id
		JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id
		WHERE model_access_targets.profile_id = $1 AND connections.id = $2
		ORDER BY CASE WHEN model_access_targets.source_model_config_id = $3 THEN 0 ELSE 1 END, model_access_targets.position ASC, model_access_targets.id ASC
		LIMIT 1`,
		profileID,
		connectionID,
		modelConfigID,
	).Scan(&modelID, &endpointID)
	if err != nil {
		return runtimeEventMetadata{}, fmt.Errorf("load runtime event metadata for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return runtimeEventMetadata{
		ModelID:    nullableString(modelID),
		EndpointID: nullableInt(endpointID),
	}, nil
}

func InsertRuntimeAdmissionRejectedEvent(ctx context.Context, exec queryExecutor, partitionEnsurer PartitionEnsurer, profileID int, modelConfigID int, connectionID int, state RuntimeConnectionState, observedAt time.Time) error {
	payload := buildRuntimeAdmissionRejectedEventPayload(state)
	if err := insertRuntimeLoadbalanceEvent(ctx, exec, partitionEnsurer, profileID, modelConfigID, connectionID, observedAt.UTC(), payload); err != nil {
		return fmt.Errorf("record runtime admission rejected event for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func InsertRuntimeUnbannedEvent(ctx context.Context, exec queryExecutor, partitionEnsurer PartitionEnsurer, profileID int, modelConfigID int, connectionID int, state RuntimeConnectionState, observedAt time.Time) error {
	payload := buildRuntimeUnbannedEventPayload(state)
	if err := insertRuntimeLoadbalanceEvent(ctx, exec, partitionEnsurer, profileID, modelConfigID, connectionID, observedAt.UTC(), payload); err != nil {
		return fmt.Errorf("record runtime unbanned event for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func InsertRuntimeRecoveryEvent(ctx context.Context, exec queryExecutor, partitionEnsurer PartitionEnsurer, profileID int, modelConfigID int, connectionID int, transition RuntimeStateTransition, strategy RuntimeStrategy, observedAt time.Time) error {
	if !transition.RecoveryEventEligible {
		return nil
	}
	payload := buildRuntimeRecoveryEventPayload(transition.PreviousState, transition.CurrentState)
	if err := insertRuntimeLoadbalanceEvent(ctx, exec, partitionEnsurer, profileID, modelConfigID, connectionID, observedAt.UTC(), payload); err != nil {
		return fmt.Errorf("record runtime recovery event for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func InsertRuntimeFailureEvent(ctx context.Context, exec queryExecutor, partitionEnsurer PartitionEnsurer, profileID int, modelConfigID int, connectionID int, transition RuntimeStateTransition, strategy RuntimeStrategy, failureKind string, observedAt time.Time) error {
	payload := buildRuntimeFailureEventPayload(transition.CurrentState, strategy, failureKind)
	if err := insertRuntimeLoadbalanceEvent(ctx, exec, partitionEnsurer, profileID, modelConfigID, connectionID, observedAt.UTC(), payload); err != nil {
		return fmt.Errorf("record runtime failure event for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func buildRuntimeAdmissionRejectedEventPayload(state RuntimeConnectionState) runtimeEventPayload {
	return runtimeEventPayload{
		EventType:               "admission_rejected",
		FailureKind:             state.LastFailureKind,
		CycleRetryAttempts:      state.CycleRetryAttempts,
		CumulativeRetryAttempts: state.CumulativeRetryAttempts,
		NextRetryAt:             state.NextRetryAt,
		LastRetryDelayMS:        state.LastRetryDelayMS,
		BanMode:                 stringPointerIfNotEmpty(normalizeBanMode(state.BanMode)),
		BannedUntilAt:           state.BannedUntilAt,
		LastSuccessAt:           state.LastSuccessAt,
	}
}

func buildRuntimeUnbannedEventPayload(state RuntimeConnectionState) runtimeEventPayload {
	return runtimeEventPayload{
		EventType:               "unbanned",
		FailureKind:             state.LastFailureKind,
		CycleRetryAttempts:      state.CycleRetryAttempts,
		CumulativeRetryAttempts: state.CumulativeRetryAttempts,
		NextRetryAt:             state.NextRetryAt,
		LastRetryDelayMS:        state.LastRetryDelayMS,
		BanMode:                 stringPointerIfNotEmpty(normalizeBanMode(state.BanMode)),
		BannedUntilAt:           state.BannedUntilAt,
		LastSuccessAt:           state.LastSuccessAt,
	}
}

func buildRuntimeFailureEventPayload(state RuntimeConnectionState, strategy RuntimeStrategy, failureKind string) runtimeEventPayload {
	policy := strategy.FeedbackPolicy()
	failureKindValue := failureKind
	banModeValue := normalizeBanMode(state.BanMode)
	eventType := "retry_scheduled"
	if runtimeFailureEventStateIsBanned(state, policy, banModeValue) {
		eventType = "banned"
	} else if state.CycleRetryAttempts >= maxInt(policy.CycleRetryAttemptLimit, 1) {
		eventType = "retry_exhausted"
	}
	return runtimeEventPayload{
		EventType:                                eventType,
		FailureKind:                              &failureKindValue,
		CycleRetryAttempts:                       state.CycleRetryAttempts,
		CumulativeRetryAttempts:                  state.CumulativeRetryAttempts,
		NextRetryAt:                              state.NextRetryAt,
		LastRetryDelayMS:                         state.LastRetryDelayMS,
		BanMode:                                  stringPointerIfNotEmpty(banModeValue),
		PolicyCycleRetryAttemptLimit:             intPointer(policy.CycleRetryAttemptLimit),
		PolicyBanCumulativeRetryAttemptThreshold: intPointer(policy.BanCumulativeRetryAttemptThreshold),
		BannedUntilAt:                            state.BannedUntilAt,
		LastSuccessAt:                            state.LastSuccessAt,
	}
}

func runtimeFailureEventStateIsBanned(state RuntimeConnectionState, policy runtimeFeedbackPolicy, banModeValue string) bool {
	if banModeValue == "off" {
		return false
	}
	if banModeValue != "until_reset" && state.BannedUntilAt == nil {
		return false
	}
	if normalizeBanMode(policy.BanMode) == "off" {
		return true
	}
	return cumulativeRetryAttemptsReachedBanThreshold(policy, state.CumulativeRetryAttempts)
}

func buildRuntimeRecoveryEventPayload(previousState RuntimeConnectionState, currentState RuntimeConnectionState) runtimeEventPayload {
	return runtimeEventPayload{
		EventType:               "recovered",
		FailureKind:             previousState.LastFailureKind,
		CycleRetryAttempts:      previousState.CycleRetryAttempts,
		CumulativeRetryAttempts: previousState.CumulativeRetryAttempts,
		NextRetryAt:             previousState.NextRetryAt,
		LastRetryDelayMS:        previousState.LastRetryDelayMS,
		BanMode:                 stringPointerIfNotEmpty(normalizeBanMode(previousState.BanMode)),
		BannedUntilAt:           previousState.BannedUntilAt,
		LastSuccessAt:           currentState.LastSuccessAt,
	}
}

func shouldRecordRuntimeRecoveryEvent(state RuntimeConnectionState) bool {
	if state.CycleRetryAttempts > 0 || state.CumulativeRetryAttempts > 0 || state.LastRetryDelayMS > 0 {
		return true
	}
	if state.LastFailureKind != nil || state.NextRetryAt != nil || state.BannedUntilAt != nil {
		return true
	}
	return normalizeBanMode(state.BanMode) != "off"
}

func nullableStringPointerArg(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableIntPointerArg(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloatPointerArg(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func intPointer(value int) *int {
	resolved := value
	return &resolved
}

func float64Pointer(value float64) *float64 {
	resolved := value
	return &resolved
}

func stringPointerIfNotEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
