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
	VendorID   *int
}

type runtimeEventPayload struct {
	EventType           string
	FailureKind         *string
	ConsecutiveFailures int
	CooldownSeconds     float64
	BlockedUntilAt      *time.Time
	FailureThreshold    *int
	BackoffMultiplier   *float64
	MaxCooldownSeconds  *int
	MaxCooldownStrikes  *int
	BanMode             *string
	BannedUntilAt       *time.Time
}

func insertRuntimeLoadbalanceEvent(ctx context.Context, exec queryExecutor, profileID int, connectionID int, observedAt time.Time, payload runtimeEventPayload) error {
	metadata, err := loadRuntimeEventMetadata(ctx, exec, profileID, connectionID)
	if err != nil {
		return err
	}
	_, err = exec.Exec(
		ctx,
		`INSERT INTO loadbalance_events (profile_id, connection_id, event_type, failure_kind, consecutive_failures, cooldown_seconds, blocked_until_mono, model_id, endpoint_id, vendor_id, failure_threshold, backoff_multiplier, max_cooldown_seconds, max_cooldown_strikes, ban_mode, banned_until_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		profileID,
		connectionID,
		payload.EventType,
		nullableStringPointerArg(payload.FailureKind),
		payload.ConsecutiveFailures,
		payload.CooldownSeconds,
		nullableBlockedUntilMonoArg(payload.BlockedUntilAt),
		nullableStringPointerArg(metadata.ModelID),
		nullableIntPointerArg(metadata.EndpointID),
		nullableIntPointerArg(metadata.VendorID),
		nullableIntPointerArg(payload.FailureThreshold),
		nullableFloatPointerArg(payload.BackoffMultiplier),
		nullableIntPointerArg(payload.MaxCooldownSeconds),
		nullableIntPointerArg(payload.MaxCooldownStrikes),
		nullableStringPointerArg(payload.BanMode),
		nullableTimeArg(payload.BannedUntilAt),
		observedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert loadbalance event for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func loadRuntimeEventMetadata(ctx context.Context, exec queryExecutor, profileID int, connectionID int) (runtimeEventMetadata, error) {
	var modelID sql.NullString
	var endpointID sql.NullInt32
	var vendorID sql.NullInt32
	err := exec.QueryRow(
		ctx,
		`SELECT model_configs.model_id, connections.endpoint_id, model_configs.vendor_id
		FROM connections
		JOIN model_configs ON model_configs.id = connections.model_config_id
		WHERE connections.profile_id = $1 AND connections.id = $2
		LIMIT 1`,
		profileID,
		connectionID,
	).Scan(&modelID, &endpointID, &vendorID)
	if err != nil {
		return runtimeEventMetadata{}, fmt.Errorf("load runtime event metadata for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return runtimeEventMetadata{
		ModelID:    nullableString(modelID),
		EndpointID: nullableInt(endpointID),
		VendorID:   nullableInt(vendorID),
	}, nil
}

func RecordRuntimePlanningSkip(ctx context.Context, exec queryExecutor, profileID int, connectionID int, state RuntimeConnectionState, strategy RuntimeStrategy, observedAt time.Time) error {
	payload := buildRuntimePlanningSkipEventPayload(state, strategy, observedAt)
	if err := insertRuntimeLoadbalanceEvent(ctx, exec, profileID, connectionID, observedAt.UTC(), payload); err != nil {
		return fmt.Errorf("record runtime planning skip event for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func RecordRuntimeAdmissionRejection(ctx context.Context, exec queryExecutor, profileID int, connectionID int, observedAt time.Time) error {
	payload := runtimeEventPayload{EventType: "not_opened"}
	if err := insertRuntimeLoadbalanceEvent(ctx, exec, profileID, connectionID, observedAt.UTC(), payload); err != nil {
		return fmt.Errorf("record runtime admission rejection event for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func InsertRuntimeProbeEligibleEvent(ctx context.Context, exec queryExecutor, profileID int, connectionID int, state RuntimeConnectionState, strategy RuntimeStrategy, observedAt time.Time) error {
	payload := buildRuntimeProbeEligibleEventPayload(state, strategy)
	if err := insertRuntimeLoadbalanceEvent(ctx, exec, profileID, connectionID, observedAt.UTC(), payload); err != nil {
		return fmt.Errorf("record runtime probe-eligible event for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func InsertRuntimeRecoveryEvent(ctx context.Context, exec queryExecutor, profileID int, connectionID int, transition RuntimeStateTransition, strategy RuntimeStrategy, observedAt time.Time) error {
	if !transition.RecoveryEventEligible {
		return nil
	}
	payload := buildRuntimeRecoveryEventPayload(transition.PreviousState, strategy)
	if err := insertRuntimeLoadbalanceEvent(ctx, exec, profileID, connectionID, observedAt.UTC(), payload); err != nil {
		return fmt.Errorf("record runtime recovery event for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func InsertRuntimeFailureEvent(ctx context.Context, exec queryExecutor, profileID int, connectionID int, transition RuntimeStateTransition, strategy RuntimeStrategy, failureKind string, observedAt time.Time) error {
	payload := buildRuntimeFailureEventPayload(
		transition.PreviousState,
		strategy,
		failureKind,
		transition.CurrentState.ConsecutiveFailures,
		transition.CurrentState.LastCooldownSeconds,
		transition.CurrentState.MaxCooldownStrikes,
		transition.CurrentState.BanMode,
		transition.CurrentState.BannedUntilAt,
		transition.CurrentState.OpenUntilAt,
		observedAt.UTC(),
	)
	if err := insertRuntimeLoadbalanceEvent(ctx, exec, profileID, connectionID, observedAt.UTC(), payload); err != nil {
		return fmt.Errorf("record runtime failure event for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func buildRuntimePlanningSkipEventPayload(state RuntimeConnectionState, strategy RuntimeStrategy, observedAt time.Time) runtimeEventPayload {
	policy := strategy.FeedbackPolicy()
	nowAt := observedAt.UTC()
	failureKind := state.LastFailureKind
	if failureKind == nil {
		failureKind = state.LastLiveFailureKind
	}
	banModeValue := normalizeBanMode(state.BanMode)
	eventType := "opened"
	blockedUntilAt := state.OpenUntilAt
	var bannedUntilAt *time.Time
	if banModeValue == "manual" || (state.BannedUntilAt != nil && state.BannedUntilAt.After(nowAt)) {
		eventType = "banned"
		blockedUntilAt = nil
		bannedUntilAt = state.BannedUntilAt
	} else if state.ProbeAvailableAt != nil && state.ProbeAvailableAt.After(nowAt) {
		blockedUntilAt = state.ProbeAvailableAt
	}
	return runtimeEventPayload{
		EventType:           eventType,
		FailureKind:         failureKind,
		ConsecutiveFailures: state.ConsecutiveFailures,
		CooldownSeconds:     state.LastCooldownSeconds,
		BlockedUntilAt:      blockedUntilAt,
		FailureThreshold:    intPointer(policy.FailureThreshold),
		BackoffMultiplier:   float64Pointer(policy.BackoffMultiplier),
		MaxCooldownSeconds:  intPointer(policy.MaxOpenSeconds),
		MaxCooldownStrikes:  intPointer(state.MaxCooldownStrikes),
		BanMode:             stringPointerIfNotEmpty(banModeValue),
		BannedUntilAt:       bannedUntilAt,
	}
}

func buildRuntimeProbeEligibleEventPayload(state RuntimeConnectionState, strategy RuntimeStrategy) runtimeEventPayload {
	policy := strategy.FeedbackPolicy()
	failureKind := state.LastFailureKind
	if failureKind == nil {
		failureKind = state.LastLiveFailureKind
	}
	blockedUntilAt := state.ProbeAvailableAt
	if blockedUntilAt == nil {
		blockedUntilAt = state.OpenUntilAt
	}
	banModeValue := normalizeBanMode(state.BanMode)
	return runtimeEventPayload{
		EventType:           "probe_eligible",
		FailureKind:         failureKind,
		ConsecutiveFailures: state.ConsecutiveFailures,
		CooldownSeconds:     state.LastCooldownSeconds,
		BlockedUntilAt:      blockedUntilAt,
		FailureThreshold:    intPointer(policy.FailureThreshold),
		BackoffMultiplier:   float64Pointer(policy.BackoffMultiplier),
		MaxCooldownSeconds:  intPointer(policy.MaxOpenSeconds),
		MaxCooldownStrikes:  intPointer(state.MaxCooldownStrikes),
		BanMode:             stringPointerIfNotEmpty(banModeValue),
		BannedUntilAt:       state.BannedUntilAt,
	}
}

func buildRuntimeFailureEventPayload(previousState RuntimeConnectionState, strategy RuntimeStrategy, failureKind string, consecutiveFailures int, cooldownSeconds float64, maxCooldownStrikes int, banMode string, bannedUntilAt *time.Time, blockedUntilAt *time.Time, observedAt time.Time) runtimeEventPayload {
	policy := strategy.FeedbackPolicy()
	failureKindValue := failureKind
	banModeValue := strings.ToLower(strings.TrimSpace(banMode))
	if banModeValue == "" {
		banModeValue = "off"
	}
	eventType := "not_opened"
	if cooldownSeconds > 0 {
		eventType = "opened"
		if previousState.OpenUntilAt != nil && previousState.OpenUntilAt.After(observedAt.UTC()) {
			eventType = "extended"
		}
		if banModeValue != "off" && (banModeValue == "manual" || bannedUntilAt != nil) {
			eventType = "banned"
		}
	}
	return runtimeEventPayload{
		EventType:           eventType,
		FailureKind:         &failureKindValue,
		ConsecutiveFailures: consecutiveFailures,
		CooldownSeconds:     cooldownSeconds,
		BlockedUntilAt:      blockedUntilAt,
		FailureThreshold:    intPointer(policy.FailureThreshold),
		BackoffMultiplier:   float64Pointer(policy.BackoffMultiplier),
		MaxCooldownSeconds:  intPointer(policy.MaxOpenSeconds),
		MaxCooldownStrikes:  intPointer(maxCooldownStrikes),
		BanMode:             stringPointerIfNotEmpty(banModeValue),
		BannedUntilAt:       bannedUntilAt,
	}
}

func buildRuntimeRecoveryEventPayload(previousState RuntimeConnectionState, strategy RuntimeStrategy) runtimeEventPayload {
	policy := strategy.FeedbackPolicy()
	failureKind := previousState.LastFailureKind
	if failureKind == nil {
		failureKind = previousState.LastLiveFailureKind
	}
	return runtimeEventPayload{
		EventType:           "recovered",
		FailureKind:         failureKind,
		ConsecutiveFailures: previousState.ConsecutiveFailures,
		CooldownSeconds:     previousState.LastCooldownSeconds,
		FailureThreshold:    intPointer(policy.FailureThreshold),
		BackoffMultiplier:   float64Pointer(policy.BackoffMultiplier),
		MaxCooldownSeconds:  intPointer(policy.MaxOpenSeconds),
		MaxCooldownStrikes:  intPointer(previousState.MaxCooldownStrikes),
		BanMode:             stringPointerIfNotEmpty(strings.TrimSpace(previousState.BanMode)),
		BannedUntilAt:       previousState.BannedUntilAt,
	}
}

func shouldRecordRuntimeRecoveryEvent(state RuntimeConnectionState) bool {
	if state.ConsecutiveFailures > 0 || state.LastCooldownSeconds > 0 || state.MaxCooldownStrikes > 0 {
		return true
	}
	if state.LastFailureKind != nil || state.LastLiveFailureKind != nil || state.LastLiveFailureAt != nil {
		return true
	}
	if state.OpenUntilAt != nil || state.ProbeAvailableAt != nil || state.BannedUntilAt != nil {
		return true
	}
	return strings.TrimSpace(strings.ToLower(state.CircuitState)) != "" && !strings.EqualFold(strings.TrimSpace(state.CircuitState), "closed")
}

func nullableBlockedUntilMonoArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return float64(value.UTC().UnixNano()) / float64(time.Second)
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
