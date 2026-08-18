package loadbalance

import (
	"context"
	"fmt"
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
	ConnectionID                        int        `json:"connection_id"`
	WindowStartedAt                     *time.Time `json:"window_started_at"`
	WindowRequestCount                  int        `json:"window_request_count"`
	InFlightNonStream                   int        `json:"in_flight_non_stream"`
	InFlightStream                      int        `json:"in_flight_stream"`
	CycleRetryAttempts                  int        `json:"cycle_retry_attempts"`
	CumulativeRetryAttempts             int        `json:"cumulative_retry_attempts"`
	NextRetryAt                         *time.Time `json:"next_retry_at"`
	LastRetryDelayMS                    int        `json:"last_retry_delay_ms"`
	BanMode                             string     `json:"ban_mode"`
	BannedUntilAt                       *time.Time `json:"banned_until_at"`
	LastFailureKind                     *string    `json:"last_failure_kind"`
	LastSuccessAt                       *time.Time `json:"last_success_at"`
	LastSuccessResponseHeadersLatencyMS *int       `json:"last_success_response_headers_latency_ms"`
	State                               string     `json:"state"`
	CreatedAt                           time.Time  `json:"created_at"`
	UpdatedAt                           time.Time  `json:"updated_at"`
}

type CurrentStateListResponse struct {
	Items []CurrentStateItem `json:"items"`
}

type CurrentStateResetResponse struct {
	ConnectionID int               `json:"connection_id"`
	Cleared      bool              `json:"cleared"`
	State        *CurrentStateItem `json:"state"`
}

type IncidentListResponse struct {
	ActiveBans   []CurrentStateItem `json:"active_bans"`
	RecentEvents []Event            `json:"recent_events"`
	GeneratedAt  time.Time          `json:"generated_at"`
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
	SnapshotActiveBans(profileID int, referenceNow time.Time) []CurrentStateItem
	// ResetConnection clears retry/ban cooldown fields only, preserving QPS window,
	// in-flight counts, last success observation, latency and round-robin cursors.
	// It returns the post-reset snapshot (nil when no process state exists) and
	// whether any cooldown field was actually cleared.
	ResetConnection(profileID int, connectionID int) (*CurrentStateItem, bool)
	ResetRoundRobinCursor(profileID int, modelConfigID int) bool
	ResetConnectionCooldown(profileID int, connectionID int, referenceNow time.Time) (bool, *CurrentStateItem)
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

func ResetCurrentState(ctx context.Context, exec queryExecutor, provider RuntimeCurrentStateProvider, profileID int, connectionID int, referenceNow time.Time) (CurrentStateResetResponse, error) {
	// The connection must exist in the effective profile; unknown or
	// cross-profile ids are a 404 (Model SPEC §10.3).
	var existingConnectionID int
	err := exec.QueryRow(ctx, `SELECT id FROM connections WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, connectionID).Scan(&existingConnectionID)
	if err == pgx.ErrNoRows {
		return CurrentStateResetResponse{}, &HTTPError{StatusCode: 404, Detail: "Connection not found"}
	}
	if err != nil {
		return CurrentStateResetResponse{}, fmt.Errorf("load connection %d for profile %d: %w", connectionID, profileID, err)
	}
	if provider == nil {
		return CurrentStateResetResponse{ConnectionID: connectionID, Cleared: false, State: nil}, nil
	}
	// Narrow cooldown reset (Model SPEC §10.3): only retry/ban cooldown is
	// cleared; QPS/in-flight/last-success/latency and round-robin cursors are
	// preserved, and the full post-reset snapshot is returned for calibration.
	cleared, snapshot := provider.ResetConnectionCooldown(profileID, connectionID, referenceNow)
	return CurrentStateResetResponse{ConnectionID: connectionID, Cleared: cleared, State: snapshot}, nil
}

func listCurrentStateConnectionIDs(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) ([]int, error) {
	rows, err := exec.Query(ctx, `SELECT connections.id FROM model_access_targets JOIN connections ON connections.id = model_access_targets.target_connection_id WHERE model_access_targets.profile_id = $1 AND model_access_targets.source_model_config_id = $2 ORDER BY model_access_targets.position ASC, connections.id ASC`, profileID, modelConfigID)
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

// ListEventsLegacy retains the pre-query-context pagination implementation for
// migration-only callers. The live observe route uses event_query.go's
// profile-scoped keyset implementation.
func ListIncidents(ctx context.Context, exec queryExecutor, provider RuntimeCurrentStateProvider, profileID int, limit int, sinceHours int, referenceNow time.Time) (IncidentListResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	if sinceHours <= 0 {
		sinceHours = 24
	}
	nowAt := referenceNow.UTC()
	sinceAt := nowAt.Add(-time.Duration(sinceHours) * time.Hour)
	rows, err := exec.Query(ctx, `SELECT id, profile_id, connection_id, event_type, failure_kind, cycle_retry_attempts, cumulative_retry_attempts, next_retry_at, last_retry_delay_ms, model_id, endpoint_id, ban_mode, policy_cycle_retry_attempt_limit, policy_ban_cumulative_retry_attempt_threshold, banned_until_at, last_success_at, created_at FROM loadbalance_events WHERE profile_id = $1 AND created_at >= $2 AND event_type IN ('banned', 'unbanned', 'recovered', 'retry_exhausted') ORDER BY created_at DESC LIMIT $3`, profileID, sinceAt, limit)
	if err != nil {
		return IncidentListResponse{}, fmt.Errorf("query loadbalance incidents for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	recentEvents := make([]Event, 0)
	for rows.Next() {
		item, scanErr := scanEvent(rows)
		if scanErr != nil {
			return IncidentListResponse{}, scanErr
		}
		recentEvents = append(recentEvents, item.Event)
	}
	if err := rows.Err(); err != nil {
		return IncidentListResponse{}, fmt.Errorf("iterate loadbalance incidents for profile %d: %w", profileID, err)
	}
	activeBans := []CurrentStateItem{}
	if provider != nil {
		activeBans = provider.SnapshotActiveBans(profileID, nowAt)
	}
	return IncidentListResponse{ActiveBans: activeBans, RecentEvents: recentEvents, GeneratedAt: nowAt}, nil
}
