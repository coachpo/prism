package connections

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type modelRecord struct {
	ID                    int
	ProfileID             int
	ModelID               string
	APIFamily             string
	IsEnabled             bool
	OpenAIAcceptedFormat  *string
	OpenAIImageOperations *string
}

func loadModelRecord(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, forUpdate bool) (modelRecord, bool, error) {
	query := `SELECT id, profile_id, model_id, api_family, is_enabled, openai_accepted_format, openai_image_operations FROM model_configs WHERE profile_id = $1 AND id = $2`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	query += ` LIMIT 1`
	record, err := scanModelRecord(exec.QueryRow(ctx, query, profileID, modelConfigID))
	if err == pgx.ErrNoRows {
		return modelRecord{}, false, nil
	}
	if err != nil {
		return modelRecord{}, false, fmt.Errorf("load model %d in profile %d: %w", modelConfigID, profileID, err)
	}
	return record, true, nil
}

func ensureModelConfigIDsExist(ctx context.Context, exec queryExecutor, profileID int, modelConfigIDs []int) error {
	if len(modelConfigIDs) == 0 {
		return &DomainError{StatusCode: 400, Detail: "model_config_ids must contain at least one model config id"}
	}
	args := []any{profileID, int32ArrayArg(modelConfigIDs)}
	query := `SELECT id FROM model_configs WHERE profile_id = $1 AND id = ANY($2) ORDER BY id ASC`
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query model ids for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	existing := map[int]struct{}{}
	for rows.Next() {
		var modelConfigID int
		if err := rows.Scan(&modelConfigID); err != nil {
			return fmt.Errorf("scan model id: %w", err)
		}
		existing[modelConfigID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate model ids for profile %d: %w", profileID, err)
	}
	for _, modelConfigID := range modelConfigIDs {
		if _, ok := existing[modelConfigID]; !ok {
			return &DomainError{StatusCode: 404, Detail: "Model configuration not found"}
		}
	}
	return nil
}

func scanModelRecord(scanner interface{ Scan(...any) error }) (modelRecord, error) {
	record := modelRecord{}
	var openAIAcceptedFormat sql.NullString
	var openAIImageOperations sql.NullString
	if err := scanner.Scan(&record.ID, &record.ProfileID, &record.ModelID, &record.APIFamily, &record.IsEnabled, &openAIAcceptedFormat, &openAIImageOperations); err != nil {
		return modelRecord{}, err
	}
	record.OpenAIAcceptedFormat = nullableStringValue(openAIAcceptedFormat)
	record.OpenAIImageOperations = nullableStringValue(openAIImageOperations)
	return record, nil
}
