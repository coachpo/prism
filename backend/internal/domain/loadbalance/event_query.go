package loadbalance

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

func ListEvents(ctx context.Context, exec queryExecutor, profileID int, filters EventQueryFilters, bounds EventQueryBounds, sortOrder string, limit int, cursor *EventCursor, generation int64, now time.Time) (EventListEnvelope, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if sortOrder != EventSortAsc {
		sortOrder = EventSortDesc
	}
	bounds = bounds.normalized()

	query := `SELECT ` + eventRowSelectColumns + ` FROM loadbalance_events WHERE profile_id = $1`
	args := []any{profileID}
	argIndex := 2
	appendCondition := func(condition string, values ...any) {
		query += " AND " + condition
		args = append(args, values...)
	}
	if bounds.FromTime != nil {
		appendCondition(fmt.Sprintf("created_at >= $%d", argIndex), *bounds.FromTime)
		argIndex++
	}
	if bounds.ToTime != nil {
		appendCondition(fmt.Sprintf("created_at < $%d", argIndex), *bounds.ToTime)
		argIndex++
	}
	if filters.ModelID != nil {
		appendCondition(fmt.Sprintf("model_id = $%d", argIndex), *filters.ModelID)
		argIndex++
	}
	if len(filters.EventTypes) > 0 {
		appendCondition(fmt.Sprintf("event_type = ANY($%d)", argIndex), filters.EventTypes)
		argIndex++
	}
	if len(filters.FailureKinds) > 0 {
		appendCondition(fmt.Sprintf("failure_kind = ANY($%d)", argIndex), filters.FailureKinds)
		argIndex++
	}
	if len(filters.AdmissionReasons) > 0 {
		appendCondition(fmt.Sprintf("admission_reason = ANY($%d)", argIndex), filters.AdmissionReasons)
		argIndex++
	}
	if filters.EndpointID != nil {
		appendCondition(fmt.Sprintf("endpoint_id = $%d", argIndex), *filters.EndpointID)
		argIndex++
	}
	if filters.TerminalTargetID != nil {
		appendCondition(fmt.Sprintf("connection_id = $%d", argIndex), *filters.TerminalTargetID)
		argIndex++
	}
	if cursor != nil && cursor.AfterCreatedAt != nil && cursor.AfterID != nil {
		operator := ">"
		if sortOrder == EventSortDesc {
			operator = "<"
		}
		appendCondition(fmt.Sprintf("(created_at, id) %s ($%d, $%d)", operator, argIndex, argIndex+1), *cursor.AfterCreatedAt, cursor.AfterID)
		argIndex += 2
	}
	order := "created_at DESC, id DESC"
	if sortOrder == EventSortAsc {
		order = "created_at ASC, id ASC"
	}
	query += " ORDER BY " + order + fmt.Sprintf(" LIMIT $%d", argIndex)
	args = append(args, limit+1)

	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return EventListEnvelope{}, fmt.Errorf("query loadbalance events for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]eventRow, 0, limit)
	for rows.Next() {
		item, scanErr := scanEventRow(rows)
		if scanErr != nil {
			return EventListEnvelope{}, fmt.Errorf("scan loadbalance event row: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return EventListEnvelope{}, fmt.Errorf("iterate loadbalance events for profile %d: %w", profileID, err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	labels, err := loadEventLabels(ctx, exec, profileID, items)
	if err != nil {
		return EventListEnvelope{}, err
	}
	requestFloor, err := loadRequestLogsRetentionFloor(ctx, exec, now)
	if err != nil {
		return EventListEnvelope{}, err
	}

	listItems := make([]EventListItem, 0, len(items))
	var nextCursor *string
	for index, item := range items {
		listItem := eventListItemFromRow(item, labels, requestFloor, now)
		listItems = append(listItems, listItem)
		if hasMore && index == len(items)-1 {
			encoded, err := encodeEventCursor(EventCursor{
				ProfileID:          profileID,
				BoundsFrom:         bounds.FromTime,
				BoundsTo:           bounds.ToTime,
				PlanningGeneration: generation,
				EventTypes:         slices.Clone(filters.EventTypes),
				FailureKinds:       slices.Clone(filters.FailureKinds),
				AdmissionReasons:   slices.Clone(filters.AdmissionReasons),
				ModelID:            cloneStringPtr(filters.ModelID),
				EndpointID:         cloneIntPtr(filters.EndpointID),
				TerminalTargetID:   cloneIntPtr(filters.TerminalTargetID),
				SortOrder:          sortOrder,
				Limit:              limit,
				AfterCreatedAt:     &item.CreatedAt,
				AfterID:            &item.ID,
			})
			if err != nil {
				return EventListEnvelope{}, err
			}
			nextCursor = &encoded
		}
	}
	coverage := computeEventCoverage(bounds, requestFloor, now)
	return EventListEnvelope{
		GeneratedAt:  now.UTC(),
		Coverage:     coverage,
		SourceStatus: EventSourceStatus{Delivery: "best_effort", TransitionLedgerComplete: false},
		Items:        listItems,
		HasMore:      hasMore,
		NextCursor:   nextCursor,
	}, nil
}

// GetEvent returns one event detail if it belongs to the profile and lies
// inside the validated event bounds; nil otherwise (404 without leaking scope).
func GetEvent(ctx context.Context, exec queryExecutor, profileID int, eventID int64, bounds EventQueryBounds, now time.Time) (*EventListItem, error) {
	query := `SELECT ` + eventRowSelectColumns + ` FROM loadbalance_events WHERE profile_id = $1 AND id = $2`
	args := []any{profileID, eventID}
	if bounds.FromTime != nil {
		query += " AND created_at >= $3"
		args = append(args, *bounds.FromTime)
	}
	if bounds.ToTime != nil {
		query += fmt.Sprintf(" AND created_at < $%d", len(args)+1)
		args = append(args, *bounds.ToTime)
	}
	query += " LIMIT 1"
	item, err := scanEventRow(exec.QueryRow(ctx, query, args...))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load loadbalance event %d for profile %d: %w", eventID, profileID, err)
	}
	labels, err := loadEventLabels(ctx, exec, profileID, []eventRow{item})
	if err != nil {
		return nil, err
	}
	nowAt := now.UTC()
	requestFloor, err := loadRequestLogsRetentionFloor(ctx, exec, nowAt)
	if err != nil {
		return nil, err
	}
	listItem := eventListItemFromRow(item, labels, requestFloor, nowAt)
	return &listItem, nil
}

// loadRequestLogsRetentionFloor consumes the request-log retention source.
// A zero value means the source has no configured logical floor.
func loadRequestLogsRetentionFloor(ctx context.Context, exec queryExecutor, now time.Time) (time.Time, error) {
	source, err := statsdomain.LoadRetentionSourceProjection(ctx, exec, "request_logs", now.UTC())
	if err != nil {
		return time.Time{}, fmt.Errorf("load request logs retention source: %w", err)
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
		return time.Time{}, fmt.Errorf("request log purge in progress")
	}
	floor := source.ConfiguredCutoff
	if source.PublishedFloor != nil && (floor == nil || source.PublishedFloor.After(*floor)) {
		floor = source.PublishedFloor
	}
	if floor == nil {
		return time.Time{}, nil
	}
	return floor.UTC(), nil
}

// computeEventCoverage reports coverage of the requested event window against
// the loadbalance-events retention floor.
func computeEventCoverage(bounds EventQueryBounds, requestFloor time.Time, now time.Time) EventCoverage {
	_ = requestFloor
	if bounds.FromTime == nil {
		return EventCoverage{Complete: true, Gaps: []EventCoverageGap{}}
	}
	return EventCoverage{Complete: true, Gaps: []EventCoverageGap{}}
}

func eventTypeAllowlist() map[string]bool {
	return map[string]bool{
		"retry_scheduled": true, "retry_exhausted": true, "banned": true,
		"unbanned": true, "recovered": true, "admission_rejected": true,
	}
}

func NormalizeEventTypeValues(values []string) ([]string, error) {
	allowlist := eventTypeAllowlist()
	seen := map[string]struct{}{}
	items := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if !allowlist[normalized] {
			return nil, fmt.Errorf("invalid event_type %q", value)
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		items = append(items, normalized)
	}
	return items, nil
}

func NormalizeFailureKindValues(values []string) ([]string, error) {
	allowlist := map[string]bool{"transient_http": true, "connect_error": true, "timeout": true}
	seen := map[string]struct{}{}
	items := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if !allowlist[normalized] {
			return nil, fmt.Errorf("invalid failure_kind %q", value)
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		items = append(items, normalized)
	}
	return items, nil
}

func NormalizeAdmissionReasonValues(values []string) ([]string, error) {
	allowlist := map[string]bool{"qps_limit": true, "max_in_flight_stream": true, "max_in_flight_non_stream": true}
	seen := map[string]struct{}{}
	items := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if !allowlist[normalized] {
			return nil, fmt.Errorf("invalid admission_reason %q", value)
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		items = append(items, normalized)
	}
	return items, nil
}

// NullableStringPtrEqual compares two string pointers by value.
func NullableStringPtrEqual(left *string, right *string) bool {
	return nullableStringPtrEqual(left, right)
}

// NullableIntPtrEqual compares two int pointers by value.
func NullableIntPtrEqual(left *int, right *int) bool {
	return nullableIntPtrEqual(left, right)
}

// LoadEventsRetentionFloor consumes the loadbalance-events retention source.
// A zero value means the source has no configured logical floor.
func LoadEventsRetentionFloor(ctx context.Context, exec queryExecutor, now time.Time) (time.Time, error) {
	source, err := statsdomain.LoadRetentionSourceProjection(ctx, exec, "loadbalance_events", now.UTC())
	if err != nil {
		return time.Time{}, fmt.Errorf("load loadbalance events retention source: %w", err)
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
		return time.Time{}, fmt.Errorf("loadbalance event purge in progress")
	}
	floor := source.ConfiguredCutoff
	if source.PublishedFloor != nil && (floor == nil || source.PublishedFloor.After(*floor)) {
		floor = source.PublishedFloor
	}
	if floor == nil {
		return time.Time{}, nil
	}
	return floor.UTC(), nil
}

type sqlNullableStringHolder struct{ Value sql.NullString }

type sqlNullableIntHolder struct{ Value sql.NullInt32 }

type sqlNullableTimeHolder struct{ Value sql.NullTime }

func nullableStringValueOf(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func nullableIntValueOf(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableTimeValueOf(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	resolved := value.Time.UTC()
	return &resolved
}

func (filters EventQueryFilters) normalized() EventQueryFilters {
	filters.EventTypes = slices.Clone(filters.EventTypes)
	filters.FailureKinds = slices.Clone(filters.FailureKinds)
	filters.AdmissionReasons = slices.Clone(filters.AdmissionReasons)
	slices.Sort(filters.EventTypes)
	slices.Sort(filters.FailureKinds)
	slices.Sort(filters.AdmissionReasons)
	return filters
}
