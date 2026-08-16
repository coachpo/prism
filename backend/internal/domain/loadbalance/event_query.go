package loadbalance

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

// EventSortOrder values.
const (
	EventSortDesc = "desc"
	EventSortAsc  = "asc"
)

// EventQueryFilters are the server-before-pagination event filters. Repeatable
// enum filters OR within a field and AND across fields.
type EventQueryFilters struct {
	ModelID          *string
	EventTypes       []string
	FailureKinds     []string
	AdmissionReasons []string
	EndpointID       *int
	TerminalTargetID *int
}

// EventQueryBounds is a validated half-open event window; nil means unbounded.
type EventQueryBounds struct {
	FromTime *time.Time
	ToTime   *time.Time
}

func (bounds EventQueryBounds) normalized() EventQueryBounds {
	if bounds.FromTime != nil {
		from := bounds.FromTime.UTC()
		bounds.FromTime = &from
	}
	if bounds.ToTime != nil {
		to := bounds.ToTime.UTC()
		bounds.ToTime = &to
	}
	return bounds
}

// EventCursor is the opaque keyset cursor binding profile, event bounds,
// planning generation, canonical filters, sort order and limit.
type EventCursor struct {
	ProfileID          int
	BoundsFrom         *time.Time
	BoundsTo           *time.Time
	PlanningGeneration int64
	EventTypes         []string
	FailureKinds       []string
	AdmissionReasons   []string
	ModelID            *string
	EndpointID         *int
	TerminalTargetID   *int
	SortOrder          string
	Limit              int
	AfterCreatedAt     *time.Time
	AfterID            *int64
}

func (cursor EventCursor) sortKey() string { return cursor.SortOrder }

// canonicalFilters returns the canonical sorted filter list for binding.
func (cursor EventCursor) canonicalFilters() []string {
	items := make([]string, 0, 16)
	items = append(items, "m:"+nullableStringValue(cursor.ModelID), "e:"+nullableIntValue(cursor.EndpointID), "t:"+nullableIntValue(cursor.TerminalTargetID))
	items = append(items, cursor.EventTypes...)
	items = append(items, cursor.FailureKinds...)
	items = append(items, cursor.AdmissionReasons...)
	slices.Sort(items)
	return items
}

func nullableStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableIntValue(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func encodeEventCursor(cursor EventCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("marshal event cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecodeEventCursor(raw string) (EventCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return EventCursor{}, fmt.Errorf("invalid event cursor")
	}
	var cursor EventCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return EventCursor{}, fmt.Errorf("invalid event cursor")
	}
	if cursor.ProfileID <= 0 || cursor.Limit < 1 || cursor.Limit > 100 || (cursor.SortOrder != EventSortDesc && cursor.SortOrder != EventSortAsc) {
		return EventCursor{}, fmt.Errorf("invalid event cursor")
	}
	return cursor, nil
}

func (cursor EventCursor) MatchesScope(profileID int, bounds EventQueryBounds, generation int64, filters EventQueryFilters, sortOrder string, limit int) bool {
	if cursor.ProfileID != profileID || cursor.SortOrder != sortOrder || cursor.Limit != limit || cursor.PlanningGeneration != generation {
		return false
	}
	expected := cursorWith(bounds, filters)
	return equalEventCursorScope(expected, cursor)
}

func cursorWith(bounds EventQueryBounds, filters EventQueryFilters) EventCursor {
	return EventCursor{
		BoundsFrom:       bounds.FromTime,
		BoundsTo:         bounds.ToTime,
		ModelID:          filters.ModelID,
		EndpointID:       filters.EndpointID,
		TerminalTargetID: filters.TerminalTargetID,
		EventTypes:       slices.Clone(filters.EventTypes),
		FailureKinds:     slices.Clone(filters.FailureKinds),
		AdmissionReasons: slices.Clone(filters.AdmissionReasons),
	}
}

func equalEventCursorScope(left EventCursor, right EventCursor) bool {
	if !nullableTimeEqual(left.BoundsFrom, right.BoundsFrom) || !nullableTimeEqual(left.BoundsTo, right.BoundsTo) {
		return false
	}
	if !nullableStringPtrEqual(left.ModelID, right.ModelID) || !nullableIntPtrEqual(left.EndpointID, right.EndpointID) || !nullableIntPtrEqual(left.TerminalTargetID, right.TerminalTargetID) {
		return false
	}
	return slices.Equal(left.EventTypes, right.EventTypes) &&
		slices.Equal(left.FailureKinds, right.FailureKinds) &&
		slices.Equal(left.AdmissionReasons, right.AdmissionReasons)
}

func nullableTimeEqual(left *time.Time, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.UTC().Equal(right.UTC())
}

func nullableStringPtrEqual(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func nullableIntPtrEqual(left *int, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

type eventRow struct {
	ID                                       int64
	ProfileID                                int
	ConnectionID                             int
	EventType                                string
	FailureKind                              *string
	AdmissionReason                          *string
	ModelConfigID                            *int
	ModelID                                  *string
	EndpointID                               *int
	CycleRetryAttempts                       int
	CumulativeRetryAttempts                  int
	NextRetryAt                              *time.Time
	LastRetryDelayMS                         int
	BanMode                                  *string
	PolicyCycleRetryAttemptLimit             *int
	PolicyBanCumulativeRetryAttemptThreshold *int
	BannedUntilAt                            *time.Time
	LastSuccessAt                            *time.Time
	CreatedAt                                time.Time
}

const eventRowSelectColumns = `id, profile_id, connection_id, event_type, failure_kind, admission_reason, model_config_id, model_id, endpoint_id,
	cycle_retry_attempts, cumulative_retry_attempts, next_retry_at, last_retry_delay_ms, ban_mode,
	policy_cycle_retry_attempt_limit, policy_ban_cumulative_retry_attempt_threshold, banned_until_at, last_success_at, created_at`

func scanEventRow(scanner interface{ Scan(...any) error }) (eventRow, error) {
	var failureKind, admissionReason, modelID, banMode sql.NullString
	var modelConfigID, endpointID, policyCycleLimit, policyBanThreshold sql.NullInt32
	var nextRetryAt, bannedUntilAt, lastSuccessAt sql.NullTime
	item := eventRow{}
	if err := scanner.Scan(
		&item.ID, &item.ProfileID, &item.ConnectionID, &item.EventType,
		&failureKind, &admissionReason, &modelConfigID, &modelID, &endpointID,
		&item.CycleRetryAttempts, &item.CumulativeRetryAttempts,
		&nextRetryAt, &item.LastRetryDelayMS, &banMode,
		&policyCycleLimit, &policyBanThreshold,
		&bannedUntilAt, &lastSuccessAt, &item.CreatedAt,
	); err != nil {
		return eventRow{}, err
	}
	item.FailureKind = nullableStringValueOf(failureKind)
	item.AdmissionReason = nullableStringValueOf(admissionReason)
	item.ModelID = nullableStringValueOf(modelID)
	item.BanMode = nullableStringValueOf(banMode)
	item.ModelConfigID = nullableIntValueOf(modelConfigID)
	item.EndpointID = nullableIntValueOf(endpointID)
	item.PolicyCycleRetryAttemptLimit = nullableIntValueOf(policyCycleLimit)
	item.PolicyBanCumulativeRetryAttemptThreshold = nullableIntValueOf(policyBanThreshold)
	item.NextRetryAt = nullableTimeValueOf(nextRetryAt)
	item.BannedUntilAt = nullableTimeValueOf(bannedUntilAt)
	item.LastSuccessAt = nullableTimeValueOf(lastSuccessAt)
	item.CreatedAt = item.CreatedAt.UTC()
	return item, nil
}

// ListEvents returns the profile-scoped global events timeline with
// server-before-pagination filters, half-open event bounds and a bidirectional
// (created_at, id) keyset cursor.
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

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// eventLabels holds the current names resolved in one batch for a page.
type eventLabels struct {
	models              map[int]string
	endpoints           map[int]string
	connections         map[int]string
	owners              map[int]int
	existingModels      map[int]bool
	existingEndpoints   map[int]bool
	existingConnections map[int]bool
}

func loadEventLabels(ctx context.Context, exec queryExecutor, profileID int, items []eventRow) (eventLabels, error) {
	labels := eventLabels{
		models:              map[int]string{},
		endpoints:           map[int]string{},
		connections:         map[int]string{},
		owners:              map[int]int{},
		existingModels:      map[int]bool{},
		existingEndpoints:   map[int]bool{},
		existingConnections: map[int]bool{},
	}
	modelIDs := []int{}
	endpointIDs := []int{}
	connectionIDs := []int{}
	for _, item := range items {
		if item.ModelConfigID != nil {
			modelIDs = append(modelIDs, *item.ModelConfigID)
		}
		if item.EndpointID != nil {
			endpointIDs = append(endpointIDs, *item.EndpointID)
		}
		connectionIDs = append(connectionIDs, item.ConnectionID)
	}
	modelIDs = dedupeInts(modelIDs)
	endpointIDs = dedupeInts(endpointIDs)
	connectionIDs = dedupeInts(connectionIDs)

	if len(modelIDs) > 0 {
		rows, err := exec.Query(ctx, `SELECT id, COALESCE(display_name, model_id) FROM model_configs WHERE profile_id = $1 AND id = ANY($2)`, profileID, int32Slice(modelIDs))
		if err != nil {
			return eventLabels{}, fmt.Errorf("query event model labels: %w", err)
		}
		for rows.Next() {
			var id int
			var label string
			if err := rows.Scan(&id, &label); err != nil {
				rows.Close()
				return eventLabels{}, fmt.Errorf("scan event model label: %w", err)
			}
			labels.models[id] = label
			labels.existingModels[id] = true
		}
		rows.Close()
	}
	if len(endpointIDs) > 0 {
		rows, err := exec.Query(ctx, `SELECT id, name FROM endpoints WHERE profile_id = $1 AND id = ANY($2)`, profileID, int32Slice(endpointIDs))
		if err != nil {
			return eventLabels{}, fmt.Errorf("query event endpoint labels: %w", err)
		}
		for rows.Next() {
			var id int
			var label string
			if err := rows.Scan(&id, &label); err != nil {
				rows.Close()
				return eventLabels{}, fmt.Errorf("scan event endpoint label: %w", err)
			}
			labels.endpoints[id] = label
			labels.existingEndpoints[id] = true
		}
		rows.Close()
	}
	if len(connectionIDs) > 0 {
		rows, err := exec.Query(ctx, `SELECT id, name FROM connections WHERE profile_id = $1 AND id = ANY($2)`, profileID, int32Slice(connectionIDs))
		if err != nil {
			return eventLabels{}, fmt.Errorf("query event terminal target labels: %w", err)
		}
		for rows.Next() {
			var id int
			var label string
			if err := rows.Scan(&id, &label); err != nil {
				rows.Close()
				return eventLabels{}, fmt.Errorf("scan event terminal target label: %w", err)
			}
			labels.connections[id] = label
			labels.existingConnections[id] = true
		}
		rows.Close()
		ownerRows, err := exec.Query(ctx, `SELECT target_connection_id, source_model_config_id FROM model_access_targets WHERE profile_id = $1 AND target_connection_id = ANY($2) AND target_type = 'connection'`, profileID, int32Slice(connectionIDs))
		if err != nil {
			return eventLabels{}, fmt.Errorf("query event terminal target owners: %w", err)
		}
		for ownerRows.Next() {
			var connectionID int
			var ownerID int
			if err := ownerRows.Scan(&connectionID, &ownerID); err != nil {
				ownerRows.Close()
				return eventLabels{}, fmt.Errorf("scan event terminal target owner: %w", err)
			}
			if _, ok := labels.owners[connectionID]; !ok {
				labels.owners[connectionID] = ownerID
			}
		}
		ownerRows.Close()
	}
	return labels, nil
}

func dedupeInts(values []int) []int {
	seen := map[int]struct{}{}
	items := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	return items
}

func int32Slice(values []int) []int32 {
	items := make([]int32, 0, len(values))
	for _, value := range values {
		items = append(items, int32(value))
	}
	return items
}

func eventListItemFromRow(item eventRow, labels eventLabels, requestFloor time.Time, now time.Time) EventListItem {
	model := buildEventModelProjection(item, labels)
	endpoint := buildEventEndpointProjection(item, labels)
	target := buildEventTerminalTargetProjection(item, labels)
	summary := buildEventSummaryV1(item.EventType, item.FailureKind, item.AdmissionReason, item.CycleRetryAttempts, item.CumulativeRetryAttempts, item.LastRetryDelayMS, item.NextRetryAt, item.PolicyCycleRetryAttemptLimit, item.BanMode, item.PolicyBanCumulativeRetryAttemptThreshold, item.BannedUntilAt, item.LastSuccessAt)
	filters, unavailableReason := BuildEventRequestContext(item, requestFloor, now)
	return EventListItem{
		EventID:                                  fmt.Sprintf("%d", item.ID),
		CreatedAt:                                item.CreatedAt,
		EventType:                                item.EventType,
		Summary:                                  summary,
		FailureKind:                              item.FailureKind,
		AdmissionReason:                          item.AdmissionReason,
		Model:                                    model,
		Endpoint:                                 endpoint,
		TerminalTarget:                           target,
		CycleRetryAttempts:                       item.CycleRetryAttempts,
		CumulativeRetryAttempts:                  item.CumulativeRetryAttempts,
		NextRetryAt:                              item.NextRetryAt,
		LastRetryDelayMS:                         item.LastRetryDelayMS,
		BanMode:                                  item.BanMode,
		PolicyCycleRetryAttemptLimit:             item.PolicyCycleRetryAttemptLimit,
		PolicyBanCumulativeRetryAttemptThreshold: item.PolicyBanCumulativeRetryAttemptThreshold,
		BannedUntilAt:                            item.BannedUntilAt,
		LastSuccessAt:                            item.LastSuccessAt,
		RequestContextFilters:                    filters,
		RequestContextUnavailableReason:          unavailableReason,
	}
}

func buildEventModelProjection(item eventRow, labels eventLabels) EventModelProjection {
	if item.ModelID == nil {
		return EventModelProjection{Attribution: AttributionUnattributed, Label: ""}
	}
	projection := EventModelProjection{
		ModelConfigID: item.ModelConfigID,
		ID:            item.ModelID,
		Attribution:   AttributionIdentified,
	}
	if item.ModelConfigID == nil {
		// Legacy event with a persisted public id but no numeric row snapshot:
		// identified, configured unknown, persisted-ID fallback, no link.
		projection.Label = *item.ModelID
		return projection
	}
	if label, ok := labels.models[*item.ModelConfigID]; ok {
		projection.Label = label
		configured := true
		projection.Configured = &configured
		return projection
	}
	projection.Label = *item.ModelID
	configured := false
	projection.Configured = &configured
	return projection
}

func buildEventEndpointProjection(item eventRow, labels eventLabels) EventEndpointProjection {
	if item.EndpointID == nil {
		return EventEndpointProjection{Attribution: AttributionUnattributed}
	}
	projection := EventEndpointProjection{ID: item.EndpointID, Attribution: AttributionIdentified}
	if label, ok := labels.endpoints[*item.EndpointID]; ok {
		projection.Label = label
		configured := true
		projection.Configured = &configured
		return projection
	}
	projection.Label = fmt.Sprintf("#%d", *item.EndpointID)
	configured := false
	projection.Configured = &configured
	return projection
}

func buildEventTerminalTargetProjection(item eventRow, labels eventLabels) EventTerminalTargetProjection {
	projection := EventTerminalTargetProjection{ID: &item.ConnectionID, Attribution: AttributionIdentified}
	if label, ok := labels.connections[item.ConnectionID]; ok {
		projection.Label = label
		configured := true
		projection.Configured = &configured
		if ownerID, ownerOK := labels.owners[item.ConnectionID]; ownerOK {
			projection.OwnerModelConfigID = &ownerID
		}
		return projection
	}
	projection.Label = fmt.Sprintf("#%d", item.ConnectionID)
	configured := false
	projection.Configured = &configured
	return projection
}

// BuildEventRequestContext computes the closed V1 handoff window: the event
// time ±15 minutes clipped only to the Requests retention floor/horizon.
func BuildEventRequestContext(item eventRow, requestFloor time.Time, now time.Time) (*EventRequestContextFilters, *string) {
	const halfWindow = 15 * time.Minute
	from := item.CreatedAt.Add(-halfWindow)
	to := item.CreatedAt.Add(halfWindow)
	if to.After(now) {
		to = now
	}
	if from.Before(requestFloor) {
		from = requestFloor
	}
	if !from.Before(to) {
		reason := "request_retention_no_overlap"
		return nil, &reason
	}
	filters := &EventRequestContextFilters{
		SchemaVersion:    1,
		Kind:             "contextual_window",
		Correlation:      "not_exact",
		FromTime:         from.Format(time.RFC3339Nano),
		ToTime:           to.Format(time.RFC3339Nano),
		ModelID:          item.ModelID,
		EndpointID:       item.EndpointID,
		TerminalTargetID: &item.ConnectionID,
	}
	return filters, nil
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
