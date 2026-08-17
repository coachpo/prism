package settings

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type settingsOperationRow struct {
	ResourceKind string
	OperationID  string
	RequestHash  string
	ResultJSON   []byte
}

func (s *retentionService) loadOperation(ctx context.Context, tx pgx.Tx, resourceKind string, operationID string) (settingsOperationRow, error) {
	var row settingsOperationRow
	err := tx.QueryRow(ctx, `SELECT resource_kind, operation_id, request_hash, result_json
		FROM settings_mutation_operations WHERE resource_kind = $1 AND operation_id = $2`,
		resourceKind, operationID).Scan(&row.ResourceKind, &row.OperationID, &row.RequestHash, &row.ResultJSON)
	if err != nil {
		return settingsOperationRow{}, err
	}
	return row, nil
}

func (s *retentionService) recordOperation(ctx context.Context, tx pgx.Tx, resourceKind string, operationID string, requestHash string, resultJSON []byte) error {
	_, err := tx.Exec(ctx, `INSERT INTO settings_mutation_operations (
		resource_kind, operation_id, request_hash, state, result_json, created_at, updated_at
	) VALUES ($1, $2, $3, 'completed', $4, now(), now())
	ON CONFLICT (resource_kind, operation_id) DO NOTHING`,
		resourceKind, operationID, requestHash, resultJSON)
	return err
}
