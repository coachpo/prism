package connections

import (
	"context"
	"fmt"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

// replaceConnectionRoutingWindows rewrites a connection's window rows whole.
// A wire window carries no stable identity, the PATCH contract is whole-field
// replacement, and the row count is bounded at RoutingScheduleMaxWindows, so a
// delete-then-insert matches the contract exactly while a diff would have to
// guess which stored row each payload row meant. The id churn is harmless:
// connection_routing_windows.id has no inbound foreign key and never reaches
// the wire. Callers already hold the profile-scoped row lock.
func replaceConnectionRoutingWindows(ctx context.Context, exec queryExecutor, profileID int, connectionID int, windows []terminaltarget.Window, currentTime time.Time) error {
	if _, err := exec.Exec(ctx, `DELETE FROM connection_routing_windows WHERE profile_id = $1 AND connection_id = $2`, profileID, connectionID); err != nil {
		return fmt.Errorf("delete routing windows for connection %d: %w", connectionID, err)
	}
	if len(windows) == 0 {
		return nil
	}
	masks := make([]int, 0, len(windows))
	starts := make([]int, 0, len(windows))
	ends := make([]int, 0, len(windows))
	for _, window := range windows {
		masks = append(masks, window.WeekdayMask)
		starts = append(starts, window.StartMinute)
		ends = append(ends, window.EndMinute)
	}
	if _, err := exec.Exec(ctx, `INSERT INTO connection_routing_windows (connection_id, profile_id, weekday_mask, start_minute, end_minute, created_at, updated_at) SELECT $1, $2, routing_window.weekday_mask, routing_window.start_minute, routing_window.end_minute, $6, $6 FROM unnest($3::smallint[], $4::smallint[], $5::smallint[]) AS routing_window(weekday_mask, start_minute, end_minute)`,
		connectionID, profileID, int16ArrayArg(masks), int16ArrayArg(starts), int16ArrayArg(ends), currentTime); err != nil {
		return fmt.Errorf("insert routing windows for connection %d: %w", connectionID, err)
	}
	return nil
}

// copyConnectionRoutingWindows clones the window rows of one connection onto
// another inside the same profile. The connectionResponse literal clone cannot
// carry child rows, so a copy that omits this call silently produces a target
// with no schedule while every other assertion still passes.
func copyConnectionRoutingWindows(ctx context.Context, exec queryExecutor, profileID int, sourceConnectionID int, targetConnectionID int, currentTime time.Time) error {
	if _, err := exec.Exec(ctx, `INSERT INTO connection_routing_windows (connection_id, profile_id, weekday_mask, start_minute, end_minute, created_at, updated_at) SELECT $2, profile_id, weekday_mask, start_minute, end_minute, $4, $4 FROM connection_routing_windows WHERE profile_id = $3 AND connection_id = $1`,
		sourceConnectionID, targetConnectionID, profileID, currentTime); err != nil {
		return fmt.Errorf("copy routing windows from connection %d to %d: %w", sourceConnectionID, targetConnectionID, err)
	}
	return nil
}

func loadConnectionRoutingWindows(ctx context.Context, exec queryExecutor, profileID int, connectionIDs []int) (map[int][]terminaltarget.Window, error) {
	if len(connectionIDs) == 0 {
		return map[int][]terminaltarget.Window{}, nil
	}
	rows, err := exec.Query(ctx, `SELECT connection_id, weekday_mask, start_minute, end_minute FROM connection_routing_windows WHERE profile_id = $1 AND connection_id = ANY($2) ORDER BY connection_id ASC, weekday_mask ASC, start_minute ASC, end_minute ASC`, profileID, int32ArrayArg(connectionIDs))
	if err != nil {
		return nil, fmt.Errorf("query routing windows for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	windowsByConnection := map[int][]terminaltarget.Window{}
	for rows.Next() {
		var connectionID int
		var window terminaltarget.Window
		if err := rows.Scan(&connectionID, &window.WeekdayMask, &window.StartMinute, &window.EndMinute); err != nil {
			return nil, fmt.Errorf("scan routing window: %w", err)
		}
		windowsByConnection[connectionID] = append(windowsByConnection[connectionID], window)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routing windows for profile %d: %w", profileID, err)
	}
	return windowsByConnection, nil
}

// attachConnectionRoutingWindows fills the window rows of already-scanned
// connections in one batch read. The parent SELECT only carries the timezone
// column, so without this call every connection would render as configured
// with zero windows.
func attachConnectionRoutingWindows(ctx context.Context, exec queryExecutor, profileID int, items []connectionResponse, now time.Time) error {
	if len(items) == 0 {
		return nil
	}
	connectionIDs := make([]int, 0, len(items))
	for _, item := range items {
		connectionIDs = append(connectionIDs, item.ID)
	}
	windowsByConnection, err := loadConnectionRoutingWindows(ctx, exec, profileID, connectionIDs)
	if err != nil {
		return err
	}
	for index := range items {
		if windows, ok := windowsByConnection[items[index].ID]; ok {
			if items[index].RoutingSchedule == nil {
				// Window rows without a timezone can only come from a write that
				// bypassed the API. Surfacing it with an empty timezone makes it
				// compile to Unresolved, which is the honest reading.
				items[index].RoutingSchedule = &RoutingSchedulePayload{}
			}
			items[index].RoutingSchedule.Windows = routingWindowPayloadsFromWindows(windows)
		}
		// The evaluated state is filled in the same pass as the windows so no
		// read path can ship configuration without it; a surface that returned
		// only configuration would leave the client no way to say whether the
		// leg is on duty right now, and no test would notice.
		timezone, windows := routingScheduleConfigFromResponse(items[index])
		items[index].RoutingScheduleState = RoutingScheduleStateForConfig(timezone, windows, items[index].IsActive, now)
	}
	return nil
}
