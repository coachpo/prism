package loadbalance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type EventSummary struct {
	Event     string `json:"event"`
	Reason    string `json:"reason"`
	Operation string `json:"operation"`
	Cooldown  string `json:"cooldown"`
}

type Event struct {
	ID                                 int64        `json:"id"`
	ProfileID                          int          `json:"profile_id"`
	ConnectionID                       int          `json:"connection_id"`
	EventType                          string       `json:"event_type"`
	FailureKind                        *string      `json:"failure_kind"`
	CycleRetryAttempts                 int          `json:"cycle_retry_attempts"`
	CumulativeRetryAttempts            int          `json:"cumulative_retry_attempts"`
	NextRetryAt                        *time.Time   `json:"next_retry_at"`
	LastRetryDelayMS                   int          `json:"last_retry_delay_ms"`
	ModelID                            *string      `json:"model_id"`
	EndpointID                         *int         `json:"endpoint_id"`
	BanMode                            *string      `json:"ban_mode"`
	CycleRetryAttemptLimit             *int         `json:"cycle_retry_attempt_limit"`
	BanCumulativeRetryAttemptThreshold *int         `json:"ban_cumulative_retry_attempt_threshold"`
	BannedUntilAt                      *time.Time   `json:"banned_until_at"`
	LastSuccessAt                      *time.Time   `json:"last_success_at"`
	Summary                            EventSummary `json:"summary"`
	CreatedAt                          time.Time    `json:"created_at"`
}

type EventDetail struct {
	Event
}

type EventListResponse struct {
	Items  []Event `json:"items"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
	// Keyset pagination (SPEC: bidirectional `(created_at, id)` stable keyset;
	// direction binds to URL/cursor). Only one of Offset or the keyset fields
	// is active per request.
	Direction       string       `json:"direction"`
	HasMoreNext     bool         `json:"has_more_next"`
	HasMorePrevious bool         `json:"has_more_previous"`
	NextKeyset      *EventKeyset `json:"next_keyset,omitempty"`
	PreviousKeyset  *EventKeyset `json:"previous_keyset,omitempty"`
}

type EventKeyset struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int64     `json:"id"`
}

type ListEventsParams struct {
	ProfileID    int
	ModelID      string
	Limit        int
	Offset       int
	Direction    string // "desc" (default) | "asc"
	BeforeKeyset *EventKeyset
	AfterKeyset  *EventKeyset
	ReferenceNow time.Time
}

func ListEventsLegacy(ctx context.Context, exec queryExecutor, params ListEventsParams) (EventListResponse, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	direction := strings.TrimSpace(params.Direction)
	if direction != "asc" {
		direction = "desc"
	}
	// Empty modelID selects the profile-scoped global timeline.
	var total int
	if params.ModelID == "" {
		if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM loadbalance_events WHERE profile_id = $1`, params.ProfileID).Scan(&total); err != nil {
			return EventListResponse{}, fmt.Errorf("count loadbalance events for profile %d: %w", params.ProfileID, err)
		}
	} else if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM loadbalance_events WHERE profile_id = $1 AND model_id = $2`, params.ProfileID, params.ModelID).Scan(&total); err != nil {
		return EventListResponse{}, fmt.Errorf("count loadbalance events for profile %d model %q: %w", params.ProfileID, params.ModelID, err)
	}
	const eventSelect = `SELECT id, profile_id, connection_id, event_type, failure_kind, cycle_retry_attempts, cumulative_retry_attempts, next_retry_at, last_retry_delay_ms, model_id, endpoint_id, ban_mode, policy_cycle_retry_attempt_limit, policy_ban_cumulative_retry_attempt_threshold, banned_until_at, last_success_at, created_at FROM loadbalance_events WHERE profile_id = $1`
	args := []any{params.ProfileID}
	order := "DESC"
	comparison := ""
	hasBefore := params.BeforeKeyset != nil
	hasAfter := params.AfterKeyset != nil
	if hasBefore && hasAfter {
		return EventListResponse{}, &HTTPError{StatusCode: 400, Detail: "before and after keyset cannot be combined"}
	}
	if direction == "asc" {
		order = "ASC"
	}
	if hasBefore {
		// Before page (older than the current anchor), walking backward: same
		// physical order as the direction but bounded below by the anchor.
		args = append(args, params.BeforeKeyset.CreatedAt.UTC(), params.BeforeKeyset.ID)
		comparison = fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", len(args)-1, len(args))
	} else if hasAfter {
		args = append(args, params.AfterKeyset.CreatedAt.UTC(), params.AfterKeyset.ID)
		comparison = fmt.Sprintf(" AND (created_at, id) > ($%d, $%d)", len(args)-1, len(args))
	}
	modelClause := ""
	if params.ModelID != "" {
		args = append(args, params.ModelID)
		modelClause = fmt.Sprintf(" AND model_id = $%d", len(args))
	}
	var rows pgx.Rows
	var queryErr error
	if offset > 0 && !hasBefore && !hasAfter {
		args = append(args, limit, offset)
		rows, queryErr = exec.Query(ctx, eventSelect+modelClause+comparison+fmt.Sprintf(` ORDER BY created_at %s, id %s LIMIT $%d OFFSET $%d`, order, order, len(args)-1, len(args)), args...)
	} else {
		args = append(args, limit+1)
		rows, queryErr = exec.Query(ctx, eventSelect+modelClause+comparison+fmt.Sprintf(` ORDER BY created_at %s, id %s LIMIT $%d`, order, order, len(args)), args...)
	}
	if queryErr != nil {
		return EventListResponse{}, fmt.Errorf("query loadbalance events for profile %d model %q: %w", params.ProfileID, params.ModelID, queryErr)
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
		return EventListResponse{}, fmt.Errorf("iterate loadbalance events for profile %d model %q: %w", params.ProfileID, params.ModelID, err)
	}
	hasMoreNext := false
	if !hasBefore && !hasAfter && offset == 0 {
		if len(items) > limit {
			hasMoreNext = true
			items = items[:limit]
		}
	} else if len(items) > limit {
		hasMoreNext = true
		items = items[:limit]
	}
	var nextKeyset *EventKeyset
	var previousKeyset *EventKeyset
	if len(items) > 0 {
		first := items[0]
		last := items[len(items)-1]
		previousKeyset = &EventKeyset{CreatedAt: first.CreatedAt, ID: first.ID}
		nextKeyset = &EventKeyset{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return EventListResponse{
		Items:           items,
		Total:           total,
		Limit:           limit,
		Offset:          offset,
		Direction:       direction,
		HasMoreNext:     hasMoreNext,
		HasMorePrevious: hasBefore && !hasAfter,
		NextKeyset:      nextKeyset,
		PreviousKeyset:  previousKeyset,
	}, nil
}

func scanEvent(scanner interface{ Scan(...any) error }) (EventDetail, error) {
	var failureKind sql.NullString
	var nextRetryAt sql.NullTime
	var modelID sql.NullString
	var endpointID sql.NullInt32
	var banMode sql.NullString
	var policyCycleRetryAttemptLimit sql.NullInt32
	var policyBanCumulativeRetryAttemptThreshold sql.NullInt32
	var bannedUntilAt sql.NullTime
	var lastSuccessAt sql.NullTime
	item := EventDetail{}
	if err := scanner.Scan(&item.ID, &item.ProfileID, &item.ConnectionID, &item.EventType, &failureKind, &item.CycleRetryAttempts, &item.CumulativeRetryAttempts, &nextRetryAt, &item.LastRetryDelayMS, &modelID, &endpointID, &banMode, &policyCycleRetryAttemptLimit, &policyBanCumulativeRetryAttemptThreshold, &bannedUntilAt, &lastSuccessAt, &item.CreatedAt); err != nil {
		return EventDetail{}, err
	}
	item.FailureKind = nullableString(failureKind)
	item.NextRetryAt = nullableTime(nextRetryAt)
	item.ModelID = nullableString(modelID)
	item.EndpointID = nullableInt32(endpointID)
	item.BanMode = nullableString(banMode)
	item.CycleRetryAttemptLimit = nullableInt32(policyCycleRetryAttemptLimit)
	item.BanCumulativeRetryAttemptThreshold = nullableInt32(policyBanCumulativeRetryAttemptThreshold)
	item.BannedUntilAt = nullableTime(bannedUntilAt)
	item.LastSuccessAt = nullableTime(lastSuccessAt)
	item.CreatedAt = item.CreatedAt.UTC()
	item.Summary = describeEvent(item.EventType, item.FailureKind, item.CycleRetryAttempts, item.CumulativeRetryAttempts, item.LastRetryDelayMS, item.BanCumulativeRetryAttemptThreshold)
	return item, nil
}

func deriveCurrentState(banMode string, bannedUntilAt *time.Time, nextRetryAt *time.Time, nowAt time.Time) string {
	if strings.EqualFold(strings.TrimSpace(banMode), "until_reset") {
		return "banned"
	}
	if bannedUntilAt != nil && bannedUntilAt.After(nowAt.UTC()) {
		return "banned"
	}
	if nextRetryAt != nil && nextRetryAt.After(nowAt.UTC()) {
		return "retry_wait"
	}
	return "available"
}

func describeEvent(eventType string, failureKind *string, cycleRetryAttempts int, cumulativeRetryAttempts int, lastRetryDelayMS int, policyBanCumulativeRetryAttemptThreshold *int) EventSummary {
	failureLabel := "failure"
	if failureKind != nil {
		switch *failureKind {
		case "transient_http":
			failureLabel = "retryable HTTP failure"
		case "connect_error":
			failureLabel = "transport failure"
		case "timeout":
			failureLabel = "timeout"
		}
	}
	delayLabel := formatDurationMS(lastRetryDelayMS)
	switch eventType {
	case "retry_scheduled":
		return EventSummary{Event: "Retry was scheduled", Reason: fmt.Sprintf("The %s raised this retry cycle to %d attempts and the cumulative budget to %d attempts.", failureLabel, cycleRetryAttempts, cumulativeRetryAttempts), Operation: fmt.Sprintf("Prism paused this connection until the next retry window in %s.", delayLabel), Cooldown: delayLabel}
	case "retry_exhausted":
		return EventSummary{Event: "Retry cycle was exhausted", Reason: fmt.Sprintf("The %s exhausted the current retry cycle after %d attempts.", failureLabel, cycleRetryAttempts), Operation: fmt.Sprintf("Prism will wait %s before opening a new retry cycle for this connection.", delayLabel), Cooldown: delayLabel}
	case "banned":
		return EventSummary{Event: "Connection was banned", Reason: describeBannedEventReason(failureLabel, cumulativeRetryAttempts, policyBanCumulativeRetryAttemptThreshold), Operation: "Prism removed this model-private connection from routing until the ban expires or an operator resets it.", Cooldown: delayLabel}
	case "unbanned":
		return EventSummary{Event: "Connection was unbanned", Reason: "The temporary ban expired before the next runtime attempt.", Operation: "Prism returned the model-private connection to its owner model's routing pool.", Cooldown: "Ban expired"}
	case "recovered":
		return EventSummary{Event: "Connection recovered", Reason: fmt.Sprintf("A successful response cleared the last %s retry state.", failureLabel), Operation: "Prism reset retry counters and cleared any retry wait or ban for the model-private connection.", Cooldown: "Recovered"}
	case "admission_rejected":
		return EventSummary{Event: "Admission was rejected", Reason: "The connection was rejected by QPS or in-flight admission limits.", Operation: "Prism skipped this attempt without advancing Ban Mode retry counters.", Cooldown: "Retry counters unchanged"}
	default:
		return EventSummary{Event: "Retry event was recorded", Reason: fmt.Sprintf("The connection has %d cycle retry attempts and %d cumulative retry attempts.", cycleRetryAttempts, cumulativeRetryAttempts), Operation: "Prism updated retry state for the model-private connection.", Cooldown: delayLabel}
	}
}

func describeBannedEventReason(failureLabel string, cumulativeRetryAttempts int, policyBanCumulativeRetryAttemptThreshold *int) string {
	if policyBanCumulativeRetryAttemptThreshold != nil {
		return fmt.Sprintf("The %s pushed cumulative retry attempts to %d, meeting the configured cumulative ban threshold of %d attempts.", failureLabel, cumulativeRetryAttempts, *policyBanCumulativeRetryAttemptThreshold)
	}
	return fmt.Sprintf("The %s pushed cumulative retry attempts to %d and banned the connection; no policy threshold snapshot was recorded for this historical event.", failureLabel, cumulativeRetryAttempts)
}

func formatDurationMS(milliseconds int) string {
	if milliseconds <= 0 {
		return "0 milliseconds"
	}
	if milliseconds == 1 {
		return "1 millisecond"
	}
	if milliseconds < 1000 {
		return fmt.Sprintf("%d milliseconds", milliseconds)
	}
	seconds := float64(milliseconds) / 1000
	if seconds == 1 {
		return "1 second"
	}
	if seconds == float64(int64(seconds)) {
		return fmt.Sprintf("%d seconds", int64(seconds))
	}
	return fmt.Sprintf("%.2f seconds", seconds)
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
