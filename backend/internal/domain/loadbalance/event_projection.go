package loadbalance

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

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
