package connections

import (
	"context"
	"fmt"
)

type connectionReferenceRecord struct {
	TargetID      int
	ModelConfigID int
	ModelID       string
	APIFamily     string
	Position      int
	IsEnabled     bool
}

func listConnectionReferenceRows(ctx context.Context, exec queryExecutor, profileID int, connectionID int) ([]connectionReferenceRecord, error) {
	rows, err := exec.Query(ctx, `SELECT model_access_targets.id, model_configs.id, model_configs.model_id, model_configs.api_family, model_access_targets.position, model_access_targets.is_enabled FROM model_access_targets JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id WHERE model_access_targets.profile_id = $1 AND model_access_targets.target_connection_id = $2 ORDER BY model_configs.model_id ASC, model_configs.id ASC, model_access_targets.position ASC`, profileID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("query connection %d references for profile %d: %w", connectionID, profileID, err)
	}
	defer rows.Close()
	items := make([]connectionReferenceRecord, 0)
	for rows.Next() {
		item, scanErr := scanConnectionReferenceRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connection %d references for profile %d: %w", connectionID, profileID, err)
	}
	return items, nil
}

func scanConnectionReferenceRecord(scanner interface{ Scan(...any) error }) (connectionReferenceRecord, error) {
	record := connectionReferenceRecord{}
	if err := scanner.Scan(&record.TargetID, &record.ModelConfigID, &record.ModelID, &record.APIFamily, &record.Position, &record.IsEnabled); err != nil {
		return connectionReferenceRecord{}, err
	}
	return record, nil
}
