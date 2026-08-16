package connections

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// accessTargetFlatResponse is the flat access-target summary returned inside
// owner-scoped connection mutation envelopes. It carries identity, type and
// mixed-list order so the frontend can reconcile the mixed list after a
// mutation; the authoritative full detail (nested model/connection summaries)
// remains the model detail read. This is a deliberate flat subset of the
// ModelAccessTarget shape to avoid duplicating the models package loader.
type accessTargetFlatResponse struct {
	ID                  int       `json:"id"`
	TargetType          string    `json:"target_type"`
	TargetModelConfigID *int      `json:"target_model_config_id"`
	ConnectionID        *int      `json:"connection_id"`
	TerminalTargetID    *int      `json:"terminal_target_id"`
	Position            int       `json:"position"`
	IsEnabled           bool      `json:"is_enabled"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func listOwnerAccessTargetFlat(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) ([]accessTargetFlatResponse, error) {
	rows, err := exec.Query(ctx, `SELECT id, target_type, target_model_config_id, target_connection_id, position, is_enabled, created_at, updated_at FROM model_access_targets WHERE profile_id = $1 AND source_model_config_id = $2 ORDER BY position ASC, id ASC`, profileID, modelConfigID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]accessTargetFlatResponse, 0)
	for rows.Next() {
		var item accessTargetFlatResponse
		var targetModelConfigID *int
		var targetConnectionID *int
		if err := rows.Scan(&item.ID, &item.TargetType, &targetModelConfigID, &targetConnectionID, &item.Position, &item.IsEnabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.TargetModelConfigID = targetModelConfigID
		item.ConnectionID = targetConnectionID
		item.TerminalTargetID = targetConnectionID
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func accessTargetFlatResponsesFromRows(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) ([]accessTargetFlatResponse, error) {
	if modelConfigID <= 0 {
		return []accessTargetFlatResponse{}, nil
	}
	items, err := listOwnerAccessTargetFlat(ctx, exec, profileID, modelConfigID)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return []accessTargetFlatResponse{}, nil
	}
	return items, nil
}

var _ = pgx.ErrNoRows
