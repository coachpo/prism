package loadbalance

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	runtimeFailureKindTransientHTTP = "transient_http"
	runtimeFailureKindConnectError  = "connect_error"
	runtimeLeaseKindNonStream       = "non_stream"
	runtimeLeaseKindHalfOpenProbe   = "half_open_probe"
	runtimeProbeLeaseTTL            = 2 * time.Minute
	runtimeInFlightLeaseTTL         = 2 * time.Minute
)

type RuntimeConnectionState struct {
	ConnectionID        int
	CircuitState        string
	BanMode             string
	BannedUntilAt       *time.Time
	OpenUntilAt         *time.Time
	ProbeAvailableAt    *time.Time
	WindowStartedAt     *time.Time
	WindowRequestCount  int
	InFlightNonStream   int
	InFlightStream      int
	ConsecutiveFailures int
	LastFailureKind     *string
	LastCooldownSeconds float64
	MaxCooldownStrikes  int
	ProbeEligibleLogged bool
	LiveP95LatencyMS    *int
	LastLiveFailureKind *string
	LastLiveFailureAt   *time.Time
	LastLiveSuccessAt   *time.Time
}

type RuntimeConnectionAdmission struct {
	QPSLimit             *int
	MaxInFlightNonStream *int
	MaxInFlightStream    *int
}

func LoadRuntimeConnectionStates(ctx context.Context, exec queryExecutor, profileID int, connectionIDs []int) (map[int]RuntimeConnectionState, error) {
	if len(connectionIDs) == 0 {
		return map[int]RuntimeConnectionState{}, nil
	}

	args := make([]any, 0, len(connectionIDs)+1)
	args = append(args, profileID)
	for _, connectionID := range connectionIDs {
		args = append(args, connectionID)
	}

	query := fmt.Sprintf(
		`SELECT connection_id, circuit_state, ban_mode, banned_until_at, open_until_at, probe_available_at,
			window_started_at, window_request_count, in_flight_non_stream, in_flight_stream, consecutive_failures,
			last_failure_kind, last_cooldown_seconds::float8, max_cooldown_strikes, probe_eligible_logged,
			live_p95_latency_ms, last_live_failure_kind, last_live_failure_at, last_live_success_at
		FROM routing_connection_runtime_state
		WHERE profile_id = $1 AND connection_id IN (%s)
		ORDER BY connection_id ASC`,
		runtimeStatePlaceholders(2, len(connectionIDs)),
	)

	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query runtime state for %d connections in profile %d: %w", len(connectionIDs), profileID, err)
	}
	defer rows.Close()

	states := make(map[int]RuntimeConnectionState, len(connectionIDs))
	for rows.Next() {
		state, scanErr := scanRuntimeConnectionState(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		states[state.ConnectionID] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime state rows for profile %d: %w", profileID, err)
	}
	return states, nil
}

func RecordRuntimeSuccess(ctx context.Context, exec queryExecutor, profileID int, connectionID int, strategy RuntimeStrategy, responseTimeMS int, observedAt time.Time) error {
	nowAt := observedAt.UTC()
	state, err := ensureRuntimeConnectionState(ctx, exec, profileID, connectionID, nowAt)
	if err != nil {
		return err
	}
	latencyMS := responseTimeMS
	if latencyMS < 1 {
		latencyMS = 1
	}
	if _, err := exec.Exec(
		ctx,
		`UPDATE routing_connection_runtime_state
		SET consecutive_failures = 0, last_failure_kind = NULL, last_cooldown_seconds = 0,
			max_cooldown_strikes = 0, ban_mode = 'off', banned_until_at = NULL, open_until_at = NULL,
			probe_eligible_logged = FALSE, circuit_state = 'closed', probe_available_at = NULL,
			live_p95_latency_ms = $3, last_live_failure_kind = NULL, last_live_failure_at = NULL,
			last_live_success_at = $4, updated_at = $4
		WHERE profile_id = $1 AND connection_id = $2`,
		profileID,
		connectionID,
		latencyMS,
		nowAt,
	); err != nil {
		return fmt.Errorf("record runtime success for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	if shouldRecordRuntimeRecoveryEvent(state) {
		if err := insertRuntimeLoadbalanceEvent(ctx, exec, profileID, connectionID, nowAt, buildRuntimeRecoveryEventPayload(state, strategy)); err != nil {
			return fmt.Errorf("record runtime recovery event for connection %d in profile %d: %w", connectionID, profileID, err)
		}
	}
	return nil
}

func RecordRuntimeFailoverHTTPFailure(ctx context.Context, exec queryExecutor, profileID int, connectionID int, strategy RuntimeStrategy, observedAt time.Time) error {
	if err := recordRuntimeFailure(ctx, exec, profileID, connectionID, strategy, observedAt, runtimeFailureKindTransientHTTP); err != nil {
		return fmt.Errorf("record runtime failover failure for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func RecordRuntimeTransportFailure(ctx context.Context, exec queryExecutor, profileID int, connectionID int, strategy RuntimeStrategy, observedAt time.Time) error {
	if err := recordRuntimeFailure(ctx, exec, profileID, connectionID, strategy, observedAt, runtimeFailureKindConnectError); err != nil {
		return fmt.Errorf("record runtime transport failure for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func recordRuntimeFailure(ctx context.Context, exec queryExecutor, profileID int, connectionID int, strategy RuntimeStrategy, observedAt time.Time, failureKind string) error {
	nowAt := observedAt.UTC()
	state, err := ensureRuntimeConnectionState(ctx, exec, profileID, connectionID, nowAt)
	if err != nil {
		return err
	}
	policy := strategy.FeedbackPolicy()
	consecutiveFailures := state.ConsecutiveFailures + 1
	circuitState := "closed"
	banMode := "off"
	var bannedUntilAt *time.Time
	var openUntilAt *time.Time
	var probeAvailableAt *time.Time
	lastCooldownSeconds := 0.0
	maxCooldownStrikes := 0
	if policy.Enabled && consecutiveFailures >= policy.FailureThreshold {
		maxCooldownStrikes = state.MaxCooldownStrikes + 1
		lastCooldownSeconds = feedbackOpenSeconds(policy, maxCooldownStrikes)
		if lastCooldownSeconds > 0 {
			openUntil := nowAt.Add(time.Duration(lastCooldownSeconds * float64(time.Second)))
			openUntilAt = &openUntil
			probeAvailableAt = &openUntil
		}
		circuitState = "open"
		if policy.MaxOpenStrikesBeforeBan > 0 && maxCooldownStrikes >= policy.MaxOpenStrikesBeforeBan {
			banMode = normalizeBanMode(policy.BanMode)
			switch banMode {
			case "temporary":
				bannedUntil := nowAt.Add(time.Duration(maxInt(policy.BanDurationSeconds, 0)) * time.Second)
				bannedUntilAt = &bannedUntil
			case "manual":
				bannedUntilAt = nil
			default:
				banMode = "off"
			}
		}
	}
	if _, err := exec.Exec(
		ctx,
		`UPDATE routing_connection_runtime_state
		SET consecutive_failures = $3, last_failure_kind = $4, last_cooldown_seconds = $5,
			max_cooldown_strikes = $6, ban_mode = $7, banned_until_at = $8, open_until_at = $9,
			probe_eligible_logged = FALSE, circuit_state = $10, probe_available_at = $11,
			last_live_failure_kind = $4, last_live_failure_at = $12, updated_at = $12
		WHERE profile_id = $1 AND connection_id = $2`,
		profileID,
		connectionID,
		consecutiveFailures,
		failureKind,
		lastCooldownSeconds,
		maxCooldownStrikes,
		banMode,
		nullableTimeArg(bannedUntilAt),
		nullableTimeArg(openUntilAt),
		circuitState,
		nullableTimeArg(probeAvailableAt),
		nowAt,
	); err != nil {
		return err
	}
	payload := buildRuntimeFailureEventPayload(state, strategy, failureKind, consecutiveFailures, lastCooldownSeconds, maxCooldownStrikes, banMode, bannedUntilAt, openUntilAt, nowAt)
	if err := insertRuntimeLoadbalanceEvent(ctx, exec, profileID, connectionID, nowAt, payload); err != nil {
		return fmt.Errorf("record runtime failure event for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func FilterEligibleConnectionIDs(candidates []ConnectionOrderCandidate, states map[int]RuntimeConnectionState, referenceNow time.Time) []int {
	eligible := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		state, ok := states[candidate.ID]
		if ok && !state.IsEligible(referenceNow) {
			continue
		}
		eligible = append(eligible, candidate.ID)
	}
	return eligible
}

func AdmissionRejectionReason(state RuntimeConnectionState, admission RuntimeConnectionAdmission, policy runtimeAdmissionPolicy, isStream bool, referenceNow time.Time) string {
	nowAt := referenceNow.UTC()
	if policy.RespectQPSLimit && admission.QPSLimit != nil && *admission.QPSLimit > 0 && state.WindowStartedAt != nil {
		windowExpiresAt := state.WindowStartedAt.UTC().Add(time.Second)
		if windowExpiresAt.After(nowAt) && state.WindowRequestCount >= *admission.QPSLimit {
			return "qps_limit"
		}
	}
	if !policy.RespectInFlightLimits {
		return ""
	}
	if isStream {
		if admission.MaxInFlightStream != nil && *admission.MaxInFlightStream > 0 && state.InFlightStream >= *admission.MaxInFlightStream {
			return "max_in_flight_stream"
		}
		return ""
	}
	if admission.MaxInFlightNonStream != nil && *admission.MaxInFlightNonStream > 0 && state.InFlightNonStream >= *admission.MaxInFlightNonStream {
		return "max_in_flight_non_stream"
	}
	return ""
}

func RequiresHalfOpenProbeLease(state RuntimeConnectionState, referenceNow time.Time) bool {
	nowAt := referenceNow.UTC()
	if state.ProbeAvailableAt == nil || state.ProbeAvailableAt.After(nowAt) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(state.CircuitState)) {
	case "open", "half_open":
		return true
	default:
		return false
	}
}

func TryAcquireRuntimeHalfOpenProbeLease(ctx context.Context, exec queryExecutor, profileID int, connectionID int, observedAt time.Time) (string, bool, error) {
	leaseToken, acquired, err := tryAcquireRuntimeLease(ctx, exec, profileID, connectionID, runtimeLeaseKindHalfOpenProbe, 1, runtimeProbeLeaseTTL, observedAt)
	if err != nil {
		return "", false, fmt.Errorf("acquire half-open runtime probe lease for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return leaseToken, acquired, nil
}

func TryAcquireRuntimeNonStreamLease(ctx context.Context, exec queryExecutor, profileID int, connectionID int, limit int, observedAt time.Time) (string, bool, error) {
	if limit <= 0 {
		return "", true, nil
	}
	leaseToken, acquired, err := tryAcquireRuntimeLease(ctx, exec, profileID, connectionID, runtimeLeaseKindNonStream, limit, runtimeInFlightLeaseTTL, observedAt)
	if err != nil {
		return "", false, fmt.Errorf("acquire non-stream runtime lease for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return leaseToken, acquired, nil
}

func tryAcquireRuntimeLease(ctx context.Context, exec queryExecutor, profileID int, connectionID int, leaseKind string, limit int, ttl time.Duration, observedAt time.Time) (string, bool, error) {
	nowAt := observedAt.UTC()
	if limit <= 0 {
		return "", true, nil
	}
	if _, err := ensureRuntimeConnectionState(ctx, exec, profileID, connectionID, nowAt); err != nil {
		return "", false, err
	}
	if _, err := exec.Exec(
		ctx,
		`DELETE FROM routing_connection_runtime_leases
		WHERE profile_id = $1 AND connection_id = $2 AND lease_kind = $3 AND expires_at <= $4`,
		profileID,
		connectionID,
		leaseKind,
		nowAt,
	); err != nil {
		return "", false, fmt.Errorf("delete expired runtime leases for connection %d in profile %d kind %q: %w", connectionID, profileID, leaseKind, err)
	}
	var activeLeaseCount int
	if err := exec.QueryRow(
		ctx,
		`SELECT COUNT(*)
		FROM routing_connection_runtime_leases
		WHERE profile_id = $1 AND connection_id = $2 AND lease_kind = $3 AND expires_at > $4`,
		profileID,
		connectionID,
		leaseKind,
		nowAt,
	).Scan(&activeLeaseCount); err != nil {
		return "", false, fmt.Errorf("count runtime leases for connection %d in profile %d kind %q: %w", connectionID, profileID, leaseKind, err)
	}
	if activeLeaseCount >= limit {
		return "", false, nil
	}
	leaseToken, err := newRuntimeLeaseToken()
	if err != nil {
		return "", false, err
	}
	expiresAt := nowAt.Add(ttl)
	if _, err := exec.Exec(
		ctx,
		`INSERT INTO routing_connection_runtime_leases (lease_token, profile_id, connection_id, lease_kind, expires_at, heartbeat_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NULL, $6, $6)`,
		leaseToken,
		profileID,
		connectionID,
		leaseKind,
		expiresAt,
		nowAt,
	); err != nil {
		return "", false, fmt.Errorf("insert runtime lease for connection %d in profile %d kind %q: %w", connectionID, profileID, leaseKind, err)
	}
	return leaseToken, true, nil
}

func ReleaseRuntimeLease(ctx context.Context, exec queryExecutor, leaseToken string) error {
	if strings.TrimSpace(leaseToken) == "" {
		return nil
	}
	if _, err := exec.Exec(ctx, `DELETE FROM routing_connection_runtime_leases WHERE lease_token = $1`, leaseToken); err != nil {
		return fmt.Errorf("release runtime lease %q: %w", leaseToken, err)
	}
	return nil
}

func (state RuntimeConnectionState) IsEligible(referenceNow time.Time) bool {
	nowAt := referenceNow.UTC()
	status := deriveCurrentState(state.BanMode, state.BannedUntilAt, state.OpenUntilAt, nowAt)
	if status == "banned" || status == "blocked" {
		return false
	}

	if state.ProbeAvailableAt == nil || !state.ProbeAvailableAt.After(nowAt) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(state.CircuitState)) {
	case "open", "half_open":
		return false
	default:
		return true
	}
}

func ensureRuntimeConnectionState(ctx context.Context, exec queryExecutor, profileID int, connectionID int, nowAt time.Time) (RuntimeConnectionState, error) {
	if _, err := exec.Exec(
		ctx,
		`INSERT INTO routing_connection_runtime_state (profile_id, connection_id, window_started_at, window_request_count, in_flight_non_stream, in_flight_stream, consecutive_failures, last_failure_kind, last_cooldown_seconds, max_cooldown_strikes, ban_mode, banned_until_at, open_until_at, probe_eligible_logged, circuit_state, probe_available_at, live_p95_latency_ms, last_live_failure_kind, last_live_failure_at, last_live_success_at, created_at, updated_at)
		VALUES ($1, $2, NULL, 0, 0, 0, 0, NULL, 0, 0, 'off', NULL, NULL, FALSE, 'closed', NULL, NULL, NULL, NULL, NULL, $3, $3)
		ON CONFLICT (profile_id, connection_id) DO NOTHING`,
		profileID,
		connectionID,
		nowAt,
	); err != nil {
		return RuntimeConnectionState{}, fmt.Errorf("ensure runtime state for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	row := exec.QueryRow(
		ctx,
		`SELECT connection_id, circuit_state, ban_mode, banned_until_at, open_until_at, probe_available_at,
			window_started_at, window_request_count, in_flight_non_stream, in_flight_stream, consecutive_failures,
			last_failure_kind, last_cooldown_seconds::float8, max_cooldown_strikes, probe_eligible_logged,
			live_p95_latency_ms, last_live_failure_kind, last_live_failure_at, last_live_success_at
		FROM routing_connection_runtime_state
		WHERE profile_id = $1 AND connection_id = $2
		FOR UPDATE`,
		profileID,
		connectionID,
	)
	state, err := scanRuntimeConnectionState(row)
	if err != nil {
		return RuntimeConnectionState{}, fmt.Errorf("load runtime state for connection %d in profile %d: %w", connectionID, profileID, err)
	}
	return state, nil
}

func feedbackOpenSeconds(policy runtimeFeedbackPolicy, strikeCount int) float64 {
	if !policy.Enabled || policy.BaseOpenSeconds <= 0 {
		return 0
	}
	multiplier := math.Pow(maxFloat(policy.BackoffMultiplier, 1), float64(maxInt(strikeCount, 1)-1))
	cooldownSeconds := float64(policy.BaseOpenSeconds) * multiplier
	if policy.MaxOpenSeconds > 0 && cooldownSeconds > float64(policy.MaxOpenSeconds) {
		cooldownSeconds = float64(policy.MaxOpenSeconds)
	}
	if cooldownSeconds < 0 {
		return 0
	}
	return cooldownSeconds
}

func scanRuntimeConnectionState(scanner interface{ Scan(...any) error }) (RuntimeConnectionState, error) {
	var bannedUntilAt sql.NullTime
	var openUntilAt sql.NullTime
	var probeAvailableAt sql.NullTime
	var windowStartedAt sql.NullTime
	var lastFailureKind sql.NullString
	var liveP95LatencyMS sql.NullInt32
	var lastLiveFailureKind sql.NullString
	var lastLiveFailureAt sql.NullTime
	var lastLiveSuccessAt sql.NullTime
	state := RuntimeConnectionState{}
	if err := scanner.Scan(
		&state.ConnectionID,
		&state.CircuitState,
		&state.BanMode,
		&bannedUntilAt,
		&openUntilAt,
		&probeAvailableAt,
		&windowStartedAt,
		&state.WindowRequestCount,
		&state.InFlightNonStream,
		&state.InFlightStream,
		&state.ConsecutiveFailures,
		&lastFailureKind,
		&state.LastCooldownSeconds,
		&state.MaxCooldownStrikes,
		&state.ProbeEligibleLogged,
		&liveP95LatencyMS,
		&lastLiveFailureKind,
		&lastLiveFailureAt,
		&lastLiveSuccessAt,
	); err != nil {
		return RuntimeConnectionState{}, fmt.Errorf("scan runtime connection state row: %w", err)
	}
	state.BannedUntilAt = nullableTime(bannedUntilAt)
	state.OpenUntilAt = nullableTime(openUntilAt)
	state.ProbeAvailableAt = nullableTime(probeAvailableAt)
	state.WindowStartedAt = nullableTime(windowStartedAt)
	state.LastFailureKind = nullableString(lastFailureKind)
	state.LiveP95LatencyMS = nullableInt(liveP95LatencyMS)
	state.LastLiveFailureKind = nullableString(lastLiveFailureKind)
	state.LastLiveFailureAt = nullableTime(lastLiveFailureAt)
	state.LastLiveSuccessAt = nullableTime(lastLiveSuccessAt)
	return state, nil
}

func nullableInt(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func newRuntimeLeaseToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate runtime lease token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func runtimeStatePlaceholders(start int, count int) string {
	parts := make([]string, count)
	for index := 0; index < count; index++ {
		parts[index] = fmt.Sprintf("$%d", start+index)
	}
	return strings.Join(parts, ", ")
}
