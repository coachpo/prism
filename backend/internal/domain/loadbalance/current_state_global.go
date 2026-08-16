package loadbalance

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Global current state read model (SPEC §6): the configured-target union of the
// current profile, with process-local runtime observations left-joined per
// existing state key and a stable configuration-identity cursor.

const (
	CurrentStateObservationObserved   = "observed"
	CurrentStateObservationUnobserved = "unobserved"
)

// CurrentStateItemGlobal is one configured terminal target row in the global
// projection.
type CurrentStateItemGlobal struct {
	Model                               CurrentStateModelIdentity    `json:"model"`
	Endpoint                            CurrentStateEndpointIdentity `json:"endpoint"`
	TerminalTarget                      CurrentStateTargetIdentity   `json:"terminal_target"`
	ObservationState                    string                       `json:"observation_state"`
	State                               *string                      `json:"state"`
	Available                           *bool                        `json:"available"`
	CycleRetryAttempts                  *int                         `json:"cycle_retry_attempts"`
	CumulativeRetryAttempts             *int                         `json:"cumulative_retry_attempts"`
	NextRetryAt                         *time.Time                   `json:"next_retry_at"`
	LastRetryDelayMS                    *int                         `json:"last_retry_delay_ms"`
	BanMode                             *string                      `json:"ban_mode"`
	BannedUntilAt                       *time.Time                   `json:"banned_until_at"`
	LastFailureKind                     *string                      `json:"last_failure_kind"`
	LastSuccessAt                       *time.Time                   `json:"last_success_at"`
	LastSuccessResponseHeadersLatencyMS *int                         `json:"last_success_response_headers_latency_ms"`
	InFlightStream                      *int                         `json:"in_flight_stream"`
	InFlightNonStream                   *int                         `json:"in_flight_non_stream"`
	QPSWindowStartedAt                  *time.Time                   `json:"qps_window_started_at"`
	QPSWindowRequestCount               *int                         `json:"qps_window_request_count"`
	CreatedAt                           *time.Time                   `json:"created_at"`
	UpdatedAt                           *time.Time                   `json:"updated_at"`
}

type CurrentStateModelIdentity struct {
	ModelConfigID int    `json:"model_config_id"`
	ID            string `json:"id"`
	Label         string `json:"label"`
	Configured    bool   `json:"configured"`
}

type CurrentStateEndpointIdentity struct {
	ID         int    `json:"id"`
	Label      string `json:"label"`
	Configured bool   `json:"configured"`
}

type CurrentStateTargetIdentity struct {
	ID         int    `json:"id"`
	Label      string `json:"label"`
	Configured bool   `json:"configured"`
}

// CurrentStateCompleteness is the process-local object-cohort completeness.
type CurrentStateCompleteness struct {
	State                 string         `json:"state"`
	Complete              bool           `json:"complete"`
	ConfiguredTargetCount int            `json:"configured_target_count"`
	ObservedTargetCount   int            `json:"observed_target_count"`
	UnobservedTargetCount int            `json:"unobserved_target_count"`
	ObservedSubsetCounts  map[string]int `json:"observed_subset_counts"`
}

// GlobalCurrentStateResponse is the full envelope.
type GlobalCurrentStateResponse struct {
	GeneratedAt           time.Time                `json:"generated_at"`
	Scope                 string                   `json:"scope"`
	InstanceID            string                   `json:"instance_id"`
	ConfigurationRevision string                   `json:"configuration_revision"`
	Completeness          CurrentStateCompleteness `json:"completeness"`
	Items                 []CurrentStateItemGlobal `json:"items"`
	HasMore               bool                     `json:"has_more"`
	NextCursor            *string                  `json:"next_cursor"`
}

// CurrentStateFilters are the optional object/state filters. State filters only
// filter rows; they never change the configured/observed denominator.
type CurrentStateFilters struct {
	ModelID          *string
	States           []string
	EndpointID       *int
	TerminalTargetID *int
}

// CurrentStateCursor binds profile, object filters, limit and the configuration
// revision; it never binds volatile runtime state revisions.
type CurrentStateCursor struct {
	ProfileID             int
	ConfigurationRevision int64
	ModelID               *string
	EndpointID            *int
	TerminalTargetID      *int
	Limit                 int
	AfterSortKey          string
	AfterModelID          string
	AfterModelConfigID    int
	AfterTargetID         int
}

func encodeCurrentStateCursor(cursor CurrentStateCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("marshal current state cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecodeCurrentStateCursor(raw string) (CurrentStateCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return CurrentStateCursor{}, fmt.Errorf("invalid current state cursor")
	}
	var cursor CurrentStateCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return CurrentStateCursor{}, fmt.Errorf("invalid current state cursor")
	}
	if cursor.ProfileID <= 0 || cursor.Limit < 1 || cursor.Limit > 100 {
		return CurrentStateCursor{}, fmt.Errorf("invalid current state cursor")
	}
	return cursor, nil
}

// configuredTargetRow is one cohort row from the configured-target union.
type configuredTargetRow struct {
	ConnectionID       int
	OwnerModelConfigID int
	ModelID            string
	ModelLabel         string
	EndpointID         int
	EndpointLabel      string
	TargetLabel        string
}

// listConfiguredTargetRows resolves the configured-target union: distinct
// connections referenced by enabled access targets reachable from enabled
// models (bounded graph depth), with the current unique owner model identity.
func listConfiguredTargetRows(ctx context.Context, exec queryExecutor, profileID int) ([]configuredTargetRow, error) {
	rows, err := exec.Query(
		ctx,
		`WITH RECURSIVE reachable(model_config_id, depth) AS (
			SELECT id, 1 FROM model_configs WHERE profile_id = $1 AND is_enabled
			UNION ALL
			SELECT mat.target_model_config_id, r.depth + 1
			FROM reachable r
			JOIN model_access_targets mat ON mat.source_model_config_id = r.model_config_id
			WHERE mat.profile_id = $1 AND mat.is_enabled AND mat.target_type = 'model'
			  AND mat.target_model_config_id IS NOT NULL AND r.depth < 16
		),
		targets AS (
			SELECT DISTINCT mat.target_connection_id AS connection_id, mat.source_model_config_id AS owner_model_config_id
			FROM reachable r
			JOIN model_access_targets mat ON mat.source_model_config_id = r.model_config_id
			WHERE mat.profile_id = $1 AND mat.is_enabled AND mat.target_type = 'connection'
			  AND mat.target_connection_id IS NOT NULL
		)
		SELECT t.connection_id, t.owner_model_config_id, mc.model_id, COALESCE(mc.display_name, mc.model_id),
			conn.endpoint_id, endpoints.name, conn.name
		FROM targets t
		JOIN model_configs mc ON mc.id = t.owner_model_config_id AND mc.profile_id = $1
		JOIN connections conn ON conn.id = t.connection_id AND conn.profile_id = $1
		LEFT JOIN endpoints ON endpoints.id = conn.endpoint_id AND endpoints.profile_id = $1
		ORDER BY lower(COALESCE(mc.display_name, mc.model_id)) ASC, mc.model_id ASC, mc.id ASC, t.connection_id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query configured target union for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]configuredTargetRow, 0)
	for rows.Next() {
		var item configuredTargetRow
		var endpointLabel, targetLabel sql.NullString
		if err := rows.Scan(&item.ConnectionID, &item.OwnerModelConfigID, &item.ModelID, &item.ModelLabel, &item.EndpointID, &endpointLabel, &targetLabel); err != nil {
			return nil, fmt.Errorf("scan configured target row: %w", err)
		}
		item.EndpointLabel = labelOrFallback(endpointLabel, fmt.Sprintf("#%d", item.EndpointID))
		item.TargetLabel = labelOrFallback(targetLabel, fmt.Sprintf("#%d", item.ConnectionID))
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate configured target rows: %w", err)
	}
	return items, nil
}

func labelOrFallback(value sql.NullString, fallback string) string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return fallback
	}
	return value.String
}

func normalizeCurrentStateFilters(filters CurrentStateFilters) CurrentStateFilters {
	seen := map[string]struct{}{}
	states := make([]string, 0, len(filters.States))
	for _, value := range filters.States {
		normalized := strings.ToLower(strings.TrimSpace(value))
		switch normalized {
		case "available", "retry_wait", "banned", "unobserved":
		default:
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		states = append(states, normalized)
	}
	filters.States = states
	return filters
}

func currentStateCohortMatches(filters CurrentStateFilters, row configuredTargetRow) bool {
	if filters.ModelID != nil && row.ModelID != *filters.ModelID {
		return false
	}
	if filters.EndpointID != nil && row.EndpointID != *filters.EndpointID {
		return false
	}
	if filters.TerminalTargetID != nil && row.ConnectionID != *filters.TerminalTargetID {
		return false
	}
	return true
}

// ListGlobalCurrentState returns the bounded global projection of the
// configured-target union with process-local observations.
func ListGlobalCurrentState(ctx context.Context, exec queryExecutor, provider RuntimeCurrentStateProvider, profileID int, instanceID string, filters CurrentStateFilters, limit int, cursor *CurrentStateCursor, generation int64, referenceNow time.Time) (GlobalCurrentStateResponse, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	filters = normalizeCurrentStateFilters(filters)
	nowAt := referenceNow.UTC()

	cohort, err := listConfiguredTargetRows(ctx, exec, profileID)
	if err != nil {
		return GlobalCurrentStateResponse{}, err
	}

	// Object filters freeze the cohort BEFORE the state filter and pagination.
	filteredCohort := make([]configuredTargetRow, 0, len(cohort))
	for _, row := range cohort {
		if currentStateCohortMatches(filters, row) {
			filteredCohort = append(filteredCohort, row)
		}
	}

	refs := make([]RuntimeConnectionRef, 0, len(filteredCohort))
	for _, row := range filteredCohort {
		refs = append(refs, RuntimeConnectionRef{ModelConfigID: row.OwnerModelConfigID, ConnectionID: row.ConnectionID})
	}
	observations := map[int]RuntimeConnectionObservation{}
	if store, ok := provider.(*LocalRuntimeStateStore); ok && store != nil {
		observations = store.SnapshotConnectionObservations(profileID, refs, nowAt)
	}

	observedCount := 0
	subsetCounts := map[string]int{}
	for _, observation := range observations {
		observedCount++
		stateName := deriveCurrentState(observation.State.BanMode, observation.State.BannedUntilAt, observation.State.NextRetryAt, nowAt)
		subsetCounts[stateName]++
	}
	configuredCount := len(filteredCohort)
	completenessState := "unobserved"
	complete := false
	switch {
	case configuredCount == 0:
		completenessState = "no_config"
		complete = true
	case observedCount == configuredCount:
		completenessState = "ready"
		complete = true
	case observedCount == 0:
		completenessState = "unobserved"
	default:
		completenessState = "partial"
	}
	completeness := CurrentStateCompleteness{
		State:                 completenessState,
		Complete:              complete,
		ConfiguredTargetCount: configuredCount,
		ObservedTargetCount:   observedCount,
		UnobservedTargetCount: configuredCount - observedCount,
		ObservedSubsetCounts:  subsetCounts,
	}
	if observedCount > 0 && observedCount != configuredCount {
		completeness.ObservedSubsetCounts = nil
	}

	// State filter applies to rows only; unobserved matches rows without keys.
	type cohortRow struct {
		row         configuredTargetRow
		observation *RuntimeConnectionObservation
	}
	ordered := make([]cohortRow, 0, len(filteredCohort))
	for _, row := range filteredCohort {
		observation, ok := observations[row.ConnectionID]
		stateName := ""
		if ok {
			stateName = deriveCurrentState(observation.State.BanMode, observation.State.BannedUntilAt, observation.State.NextRetryAt, nowAt)
		}
		if len(filters.States) > 0 {
			matches := false
			for _, want := range filters.States {
				if want == "unobserved" && !ok {
					matches = true
					break
				}
				if ok && want == stateName {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
		}
		var observationPtr *RuntimeConnectionObservation
		if ok {
			observationPtr = &observation
		}
		ordered = append(ordered, cohortRow{row: row, observation: observationPtr})
	}

	// Stable configuration-identity sort and keyset pagination.
	sortKeyOf := func(row configuredTargetRow) string {
		return strings.ToLower(row.ModelLabel)
	}
	if cursor != nil {
		// Keyset: (lower(model label), model_id, model_config_id, target id).
		start := 0
		for start < len(ordered) {
			row := ordered[start].row
			key := sortKeyOf(row)
			if key > cursor.AfterSortKey ||
				(key == cursor.AfterSortKey && row.ModelID > cursor.AfterModelID) ||
				(key == cursor.AfterSortKey && row.ModelID == cursor.AfterModelID && row.OwnerModelConfigID > cursor.AfterModelConfigID) ||
				(key == cursor.AfterSortKey && row.ModelID == cursor.AfterModelID && row.OwnerModelConfigID == cursor.AfterModelConfigID && row.ConnectionID > cursor.AfterTargetID) {
				break
			}
			start++
		}
		ordered = ordered[start:]
	}
	hasMore := len(ordered) > limit
	if hasMore {
		ordered = ordered[:limit]
	}

	items := make([]CurrentStateItemGlobal, 0, len(ordered))
	var nextCursor *string
	for index, entry := range ordered {
		item := currentStateItemFromCohortRow(entry.row, entry.observation, nowAt)
		items = append(items, item)
		if hasMore && index == len(ordered)-1 {
			encoded, err := encodeCurrentStateCursor(CurrentStateCursor{
				ProfileID:             profileID,
				ConfigurationRevision: generation,
				ModelID:               cloneStringPtr(filters.ModelID),
				EndpointID:            cloneIntPtr(filters.EndpointID),
				TerminalTargetID:      cloneIntPtr(filters.TerminalTargetID),
				Limit:                 limit,
				AfterSortKey:          sortKeyOf(entry.row),
				AfterModelID:          entry.row.ModelID,
				AfterModelConfigID:    entry.row.OwnerModelConfigID,
				AfterTargetID:         entry.row.ConnectionID,
			})
			if err != nil {
				return GlobalCurrentStateResponse{}, err
			}
			nextCursor = &encoded
		}
	}

	return GlobalCurrentStateResponse{
		GeneratedAt:           nowAt,
		Scope:                 "process",
		InstanceID:            instanceID,
		ConfigurationRevision: fmt.Sprintf("%d", generation),
		Completeness:          completeness,
		Items:                 items,
		HasMore:               hasMore,
		NextCursor:            nextCursor,
	}, nil
}

func currentStateItemFromCohortRow(row configuredTargetRow, observation *RuntimeConnectionObservation, nowAt time.Time) CurrentStateItemGlobal {
	item := CurrentStateItemGlobal{
		Model:            CurrentStateModelIdentity{ModelConfigID: row.OwnerModelConfigID, ID: row.ModelID, Label: row.ModelLabel, Configured: true},
		Endpoint:         CurrentStateEndpointIdentity{ID: row.EndpointID, Label: row.EndpointLabel, Configured: true},
		TerminalTarget:   CurrentStateTargetIdentity{ID: row.ConnectionID, Label: row.TargetLabel, Configured: true},
		ObservationState: CurrentStateObservationUnobserved,
	}
	if observation == nil {
		return item
	}
	state := observation.State
	stateName := deriveCurrentState(state.BanMode, state.BannedUntilAt, state.NextRetryAt, nowAt)
	available := stateName == "available"
	item.ObservationState = CurrentStateObservationObserved
	item.State = &stateName
	item.Available = &available
	item.CycleRetryAttempts = intParam(state.CycleRetryAttempts)
	item.CumulativeRetryAttempts = intParam(state.CumulativeRetryAttempts)
	item.NextRetryAt = cloneTimePointer(state.NextRetryAt)
	item.LastRetryDelayMS = intParam(state.LastRetryDelayMS)
	item.BanMode = stringPointerIfNotEmpty(state.BanMode)
	item.BannedUntilAt = cloneTimePointer(state.BannedUntilAt)
	item.LastFailureKind = cloneStringPointer(state.LastFailureKind)
	item.LastSuccessAt = cloneTimePointer(state.LastSuccessAt)
	item.LastSuccessResponseHeadersLatencyMS = cloneIntPointer(state.LastSuccessResponseHeadersLatencyMS)
	item.InFlightStream = intParam(state.InFlightStream)
	item.InFlightNonStream = intParam(state.InFlightNonStream)
	item.QPSWindowStartedAt = cloneTimePointer(state.WindowStartedAt)
	item.QPSWindowRequestCount = intParam(state.WindowRequestCount)
	item.CreatedAt = &observation.CreatedAt
	item.UpdatedAt = &observation.UpdatedAt
	return item
}

var _ = pgx.ErrNoRows
