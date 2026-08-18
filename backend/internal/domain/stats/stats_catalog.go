package stats

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type endpointRecord struct {
	ID      int
	Name    *string
	BaseURL *string
}

func loadCurrentEndpoints(ctx context.Context, exec queryExecutor, profileID int) ([]endpointRecord, map[int]endpointRecord, error) {
	rows, err := exec.Query(ctx, `SELECT id, name, base_url FROM endpoints WHERE profile_id = $1 ORDER BY lower(name) ASC, name ASC, id ASC`, profileID)
	if err != nil {
		return nil, nil, fmt.Errorf("query endpoints for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]endpointRecord, 0)
	itemsByID := map[int]endpointRecord{}
	for rows.Next() {
		var name sql.NullString
		var baseURL sql.NullString
		var item endpointRecord
		if err := rows.Scan(&item.ID, &name, &baseURL); err != nil {
			return nil, nil, fmt.Errorf("scan endpoint record: %w", err)
		}
		item.Name = nullableString(name)
		item.BaseURL = nullableString(baseURL)
		items = append(items, item)
		itemsByID[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate endpoints for profile %d: %w", profileID, err)
	}
	return items, itemsByID, nil
}

type connectionRecord struct {
	ID           int
	Name         *string
	IsActive     bool
	OwnerModelID *string
}

// loadCurrentConnections loads the full connection catalog for a profile in
// one pass. Connection counts are in the tens, and a read failure must
// propagate - presenting read failure as configured:false would violate the
// honesty contract.
func loadCurrentConnections(ctx context.Context, exec queryExecutor, profileID int) (map[int]connectionRecord, error) {
	rows, err := exec.Query(ctx, `SELECT connections.id, connections.name, connections.is_active,
		(SELECT model_configs.model_id FROM model_access_targets
		   JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id
		  WHERE model_access_targets.profile_id = connections.profile_id
		    AND model_access_targets.target_connection_id = connections.id
		  ORDER BY model_access_targets.position ASC, model_access_targets.id ASC LIMIT 1) AS owner_model_id
		FROM connections WHERE connections.profile_id = $1`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query connections for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	itemsByID := map[int]connectionRecord{}
	for rows.Next() {
		var name, owner sql.NullString
		var item connectionRecord
		if err := rows.Scan(&item.ID, &name, &item.IsActive, &owner); err != nil {
			return nil, fmt.Errorf("scan connection record: %w", err)
		}
		item.Name = nullableString(name)
		item.OwnerModelID = nullableString(owner)
		itemsByID[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connections for profile %d: %w", profileID, err)
	}
	return itemsByID, nil
}

// terminalTargetProjection is the single shared resolution result for the
// list, chain, and detail views.
type terminalTargetProjection struct {
	Label        *string
	Configured   bool
	Deleted      bool
	OwnerModelID *string
}

// resolveTerminalTargetProjection is a pure function: the catalog is loaded
// outside the cursor, so no query is issued here. The three states are
// expressed by (Label, Configured, Deleted): an existing active connection
// carries its name; an existing but inactive connection keeps its name with
// Configured=false; a missing connection row is Deleted=true. Read failures
// are not expressed here - a catalog load failure already became a 5xx.
func resolveTerminalTargetProjection(catalog map[int]connectionRecord, connectionID *int) terminalTargetProjection {
	if connectionID == nil {
		return terminalTargetProjection{}
	}
	record, ok := catalog[*connectionID]
	if !ok {
		return terminalTargetProjection{Deleted: true}
	}
	projection := terminalTargetProjection{Configured: record.IsActive, OwnerModelID: record.OwnerModelID}
	if record.Name != nil && strings.TrimSpace(*record.Name) != "" {
		label := strings.TrimSpace(*record.Name)
		projection.Label = &label
	}
	return projection
}

func resolveEndpointLabel(name *string, baseURL *string, historicalBaseURL *string, endpointID *int, unknownLabel string) string {
	if name != nil && strings.TrimSpace(*name) != "" {
		return strings.TrimSpace(*name)
	}
	if baseURL != nil && strings.TrimSpace(*baseURL) != "" {
		return strings.TrimSpace(*baseURL)
	}
	if historicalBaseURL != nil && strings.TrimSpace(*historicalBaseURL) != "" {
		return strings.TrimSpace(*historicalBaseURL)
	}
	if endpointID != nil {
		return fmt.Sprintf("Endpoint %d", *endpointID)
	}
	return unknownLabel
}
