package loadbalance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type HTTPError struct {
	StatusCode int
	Detail     string
}

func (err *HTTPError) Error() string {
	return err.Detail
}

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type CurrentStateItem struct {
	ConnectionID        int        `json:"connection_id"`
	CircuitState        *string    `json:"circuit_state"`
	ProbeAvailableAt    *time.Time `json:"probe_available_at"`
	WindowStartedAt     *time.Time `json:"window_started_at"`
	WindowRequestCount  int        `json:"window_request_count"`
	InFlightNonStream   int        `json:"in_flight_non_stream"`
	InFlightStream      int        `json:"in_flight_stream"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastFailureKind     *string    `json:"last_failure_kind"`
	LastCooldownSeconds float64    `json:"last_cooldown_seconds"`
	MaxCooldownStrikes  int        `json:"max_cooldown_strikes"`
	BanMode             string     `json:"ban_mode"`
	BannedUntilAt       *time.Time `json:"banned_until_at"`
	BlockedUntilAt      *time.Time `json:"blocked_until_at"`
	ProbeEligibleLogged bool       `json:"probe_eligible_logged"`
	LiveP95LatencyMS    *int       `json:"live_p95_latency_ms"`
	LastLiveFailureAt   *time.Time `json:"last_live_failure_at"`
	LastLiveSuccessAt   *time.Time `json:"last_live_success_at"`
	State               string     `json:"state"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type CurrentStateListResponse struct {
	Items []CurrentStateItem `json:"items"`
}

type CurrentStateResetResponse struct {
	ConnectionID int  `json:"connection_id"`
	Cleared      bool `json:"cleared"`
}

type EventSummary struct {
	Event     string `json:"event"`
	Reason    string `json:"reason"`
	Operation string `json:"operation"`
	Cooldown  string `json:"cooldown"`
}

type Event struct {
	ID                  int64        `json:"id"`
	ProfileID           int          `json:"profile_id"`
	ConnectionID        int          `json:"connection_id"`
	EventType           string       `json:"event_type"`
	FailureKind         *string      `json:"failure_kind"`
	ConsecutiveFailures int          `json:"consecutive_failures"`
	CooldownSeconds     float64      `json:"cooldown_seconds"`
	BlockedUntilMono    *float64     `json:"blocked_until_mono"`
	ModelID             *string      `json:"model_id"`
	EndpointID          *int         `json:"endpoint_id"`
	VendorID            *int         `json:"vendor_id"`
	MaxCooldownStrikes  *int         `json:"max_cooldown_strikes"`
	BanMode             *string      `json:"ban_mode"`
	BannedUntilAt       *time.Time   `json:"banned_until_at"`
	Summary             EventSummary `json:"summary"`
	CreatedAt           time.Time    `json:"created_at"`
}

type EventDetail struct {
	Event
	FailureThreshold   *int     `json:"failure_threshold"`
	BackoffMultiplier  *float64 `json:"backoff_multiplier"`
	MaxCooldownSeconds *int     `json:"max_cooldown_seconds"`
}

type EventListResponse struct {
	Items  []Event `json:"items"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

type DeleteParams struct {
	ProfileID     int
	Before        *time.Time
	OlderThanDays *int
	DeleteAll     bool
	ReferenceNow  time.Time
}

type RuntimeCurrentStateProvider interface {
	SnapshotCurrentState(profileID int, modelConfigID int, orderedConnectionIDs []int, referenceNow time.Time) []CurrentStateItem
	ResetConnection(profileID int, connectionID int) bool
	ResetRoundRobinCursor(profileID int, modelConfigID int) bool
}

func ListCurrentState(ctx context.Context, exec queryExecutor, provider RuntimeCurrentStateProvider, profileID int, modelConfigID int, referenceNow time.Time) (CurrentStateListResponse, error) {
	var existingModelID int
	err := exec.QueryRow(ctx, `SELECT id FROM model_configs WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, modelConfigID).Scan(&existingModelID)
	if err == pgx.ErrNoRows {
		return CurrentStateListResponse{}, &HTTPError{StatusCode: 404, Detail: "Model not found"}
	}
	if err != nil {
		return CurrentStateListResponse{}, fmt.Errorf("load model %d for profile %d: %w", modelConfigID, profileID, err)
	}
	orderedConnectionIDs, err := listCurrentStateConnectionIDs(ctx, exec, profileID, modelConfigID)
	if err != nil {
		return CurrentStateListResponse{}, err
	}
	if provider == nil {
		return CurrentStateListResponse{Items: []CurrentStateItem{}}, nil
	}
	return CurrentStateListResponse{Items: provider.SnapshotCurrentState(profileID, modelConfigID, orderedConnectionIDs, referenceNow)}, nil
}

func ResetCurrentState(ctx context.Context, exec queryExecutor, provider RuntimeCurrentStateProvider, profileID int, connectionID int) (CurrentStateResetResponse, error) {
	var modelConfigID sql.NullInt32
	err := exec.QueryRow(ctx, `SELECT model_config_id FROM connections WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, connectionID).Scan(&modelConfigID)
	if err != nil && err != pgx.ErrNoRows {
		return CurrentStateResetResponse{}, fmt.Errorf("load connection %d for profile %d: %w", connectionID, profileID, err)
	}
	cleared := false
	if provider != nil {
		cleared = provider.ResetConnection(profileID, connectionID) || cleared
		if modelConfigID.Valid {
			cleared = provider.ResetRoundRobinCursor(profileID, int(modelConfigID.Int32)) || cleared
		}
	}
	return CurrentStateResetResponse{ConnectionID: connectionID, Cleared: cleared}, nil
}

func listCurrentStateConnectionIDs(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) ([]int, error) {
	rows, err := exec.Query(ctx, `SELECT id FROM connections WHERE profile_id = $1 AND model_config_id = $2 ORDER BY priority ASC, id ASC`, profileID, modelConfigID)
	if err != nil {
		return nil, fmt.Errorf("query current-state connection ids for model %d: %w", modelConfigID, err)
	}
	defer rows.Close()
	ids := make([]int, 0)
	for rows.Next() {
		var connectionID int
		if err := rows.Scan(&connectionID); err != nil {
			return nil, fmt.Errorf("scan current-state connection id for model %d: %w", modelConfigID, err)
		}
		ids = append(ids, connectionID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current-state connection ids for model %d: %w", modelConfigID, err)
	}
	return ids, nil
}

func ListEvents(ctx context.Context, exec queryExecutor, profileID int, modelID string, limit int, offset int) (EventListResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM loadbalance_events WHERE profile_id = $1 AND model_id = $2`, profileID, modelID).Scan(&total); err != nil {
		return EventListResponse{}, fmt.Errorf("count loadbalance events for profile %d model %q: %w", profileID, modelID, err)
	}
	rows, err := exec.Query(ctx, `SELECT id, profile_id, connection_id, event_type, failure_kind, consecutive_failures, cooldown_seconds::float8, blocked_until_mono::float8, model_id, endpoint_id, vendor_id, failure_threshold, backoff_multiplier::float8, max_cooldown_seconds, max_cooldown_strikes, ban_mode, banned_until_at, created_at FROM loadbalance_events WHERE profile_id = $1 AND model_id = $2 ORDER BY created_at DESC LIMIT $3 OFFSET $4`, profileID, modelID, limit, offset)
	if err != nil {
		return EventListResponse{}, fmt.Errorf("query loadbalance events for profile %d model %q: %w", profileID, modelID, err)
	}
	defer rows.Close()
	items := make([]Event, 0)
	for rows.Next() {
		item, scanErr := scanEvent(rows)
		if scanErr != nil {
			return EventListResponse{}, scanErr
		}
		items = append(items, item.Event)
	}
	if err := rows.Err(); err != nil {
		return EventListResponse{}, fmt.Errorf("iterate loadbalance events for profile %d model %q: %w", profileID, modelID, err)
	}
	return EventListResponse{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func GetEvent(ctx context.Context, exec queryExecutor, profileID int, eventID int64) (*EventDetail, error) {
	row := exec.QueryRow(ctx, `SELECT id, profile_id, connection_id, event_type, failure_kind, consecutive_failures, cooldown_seconds::float8, blocked_until_mono::float8, model_id, endpoint_id, vendor_id, failure_threshold, backoff_multiplier::float8, max_cooldown_seconds, max_cooldown_strikes, ban_mode, banned_until_at, created_at FROM loadbalance_events WHERE profile_id = $1 AND id = $2 ORDER BY created_at DESC LIMIT 1`, profileID, eventID)
	item, err := scanEvent(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load loadbalance event %d for profile %d: %w", eventID, profileID, err)
	}
	return &item, nil
}

func scanEvent(scanner interface{ Scan(...any) error }) (EventDetail, error) {
	var failureKind sql.NullString
	var blockedUntilMono sql.NullFloat64
	var modelID sql.NullString
	var endpointID sql.NullInt32
	var vendorID sql.NullInt32
	var failureThreshold sql.NullInt32
	var backoffMultiplier sql.NullFloat64
	var maxCooldownSeconds sql.NullInt32
	var maxCooldownStrikes sql.NullInt32
	var banMode sql.NullString
	var bannedUntilAt sql.NullTime
	item := EventDetail{}
	if err := scanner.Scan(&item.ID, &item.ProfileID, &item.ConnectionID, &item.EventType, &failureKind, &item.ConsecutiveFailures, &item.CooldownSeconds, &blockedUntilMono, &modelID, &endpointID, &vendorID, &failureThreshold, &backoffMultiplier, &maxCooldownSeconds, &maxCooldownStrikes, &banMode, &bannedUntilAt, &item.CreatedAt); err != nil {
		return EventDetail{}, err
	}
	item.FailureKind = nullableString(failureKind)
	item.BlockedUntilMono = nullableFloat64(blockedUntilMono)
	item.ModelID = nullableString(modelID)
	item.EndpointID = nullableInt32(endpointID)
	item.VendorID = nullableInt32(vendorID)
	item.FailureThreshold = nullableInt32(failureThreshold)
	item.BackoffMultiplier = nullableFloat64(backoffMultiplier)
	item.MaxCooldownSeconds = nullableInt32(maxCooldownSeconds)
	item.MaxCooldownStrikes = nullableInt32(maxCooldownStrikes)
	item.BanMode = nullableString(banMode)
	item.BannedUntilAt = nullableTime(bannedUntilAt)
	item.CreatedAt = item.CreatedAt.UTC()
	item.Summary = describeEvent(item.EventType, item.FailureKind, item.ConsecutiveFailures, item.CooldownSeconds, item.FailureThreshold)
	return item, nil
}

func deriveCurrentState(banMode string, bannedUntilAt *time.Time, blockedUntilAt *time.Time, nowAt time.Time) string {
	if strings.EqualFold(strings.TrimSpace(banMode), "manual") {
		return "banned"
	}
	if bannedUntilAt != nil && bannedUntilAt.After(nowAt) {
		return "banned"
	}
	if blockedUntilAt == nil {
		return "counting"
	}
	if blockedUntilAt.After(nowAt) {
		return "blocked"
	}
	return "probe_eligible"
}

func describeEvent(eventType string, failureKind *string, consecutiveFailures int, cooldownSeconds float64, failureThreshold *int) EventSummary {
	failureLabel := "failure"
	if failureKind != nil {
		switch *failureKind {
		case "transient_http":
			failureLabel = "transient HTTP failure"
		case "connect_error":
			failureLabel = "connection error"
		case "timeout":
			failureLabel = "timeout"
		}
	}
	thresholdLabel := "the failover threshold"
	if failureThreshold != nil {
		thresholdLabel = fmt.Sprintf("the failover threshold of %d", *failureThreshold)
	}
	cooldownLabel := formatDuration(cooldownSeconds)
	switch eventType {
	case "max_cooldown_strike":
		return EventSummary{Event: "Connection hit max open interval", Reason: fmt.Sprintf("The %s pushed the connection to the configured maximum open interval after %d consecutive failures.", failureLabel, consecutiveFailures), Operation: "Prism recorded a max-open strike so operators can track whether the connection should escalate into a ban.", Cooldown: cooldownLabel}
	case "banned":
		return EventSummary{Event: "Connection was banned", Reason: fmt.Sprintf("The %s reached the ban escalation threshold after %d consecutive failures.", failureLabel, consecutiveFailures), Operation: "Prism removed the connection from normal adaptive routing until the ban clears or an operator resets it.", Cooldown: cooldownLabel}
	case "opened":
		return EventSummary{Event: "Connection opened its circuit", Reason: fmt.Sprintf("The %s raised the streak to %d consecutive failures, meeting %s.", failureLabel, consecutiveFailures, thresholdLabel), Operation: fmt.Sprintf("Prism opened the circuit for %s before the connection becomes eligible for another probe attempt.", cooldownLabel), Cooldown: cooldownLabel}
	case "extended":
		return EventSummary{Event: "Circuit open interval was extended", Reason: fmt.Sprintf("Another %s happened before the active cooldown finished, and the streak is now %d consecutive failures.", failureLabel, consecutiveFailures), Operation: fmt.Sprintf("Prism kept the circuit open and restarted the recovery timer for %s.", cooldownLabel), Cooldown: cooldownLabel}
	case "probe_eligible":
		return EventSummary{Event: "Connection became probe eligible", Reason: fmt.Sprintf("The open interval after the last %s completed, so the connection can be checked again.", failureLabel), Operation: "Prism can let this connection receive another probe or traffic attempt to confirm whether it recovered.", Cooldown: cooldownLabel + " open interval completed"}
	case "recovered":
		return EventSummary{Event: "Connection recovered", Reason: fmt.Sprintf("The connection was marked healthy again after the last %s.", failureLabel), Operation: "Prism closed the circuit and returned the connection to normal adaptive routing.", Cooldown: "Recovered after a " + cooldownLabel + " open interval"}
	default:
		return EventSummary{Event: "Failure was recorded", Reason: fmt.Sprintf("The %s raised the streak to %d consecutive failures, which is still below %s.", failureLabel, consecutiveFailures, thresholdLabel), Operation: "Prism kept the connection available and only updated the runtime failure streak, so no circuit-open interval started.", Cooldown: "No open interval started"}
	}
}

func formatDuration(seconds float64) string {
	normalized := seconds
	if normalized < 0 {
		normalized = 0
	}
	if normalized == 1 {
		return "1 second"
	}
	if normalized == float64(int64(normalized)) {
		return fmt.Sprintf("%d seconds", int64(normalized))
	}
	return fmt.Sprintf("%.2f seconds", normalized)
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	resolved := value.Time.UTC()
	return &resolved
}

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableFloat64(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	resolved := value.Float64
	return &resolved
}
