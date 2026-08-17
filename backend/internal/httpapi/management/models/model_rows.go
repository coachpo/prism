package models

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type modelRecord struct {
	ID                    int
	ProfileID             int
	APIFamily             string
	ModelID               string
	DisplayName           *string
	LoadbalanceStrategyID *int
	OpenAIAcceptedFormat  *string
	OpenAIImageOperations *string
	IsEnabled             bool
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type accessTargetRecord struct {
	ID                  int
	ProfileID           int
	SourceModelConfigID int
	TargetType          string
	TargetModelConfigID *int
	TargetConnectionID  *int
	Position            int
	IsEnabled           bool
	TargetModel         *modelRecord
	Connection          *connectionTargetSummary
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type resolvedAccessTarget struct {
	TargetType string
	Position   int
	IsEnabled  bool
	Model      *modelRecord
	Connection *connectionTargetSummary
}

type preservedConnectionAccessTarget struct {
	ID        int
	Position  int
	IsEnabled bool
	Update    bool
}

type strategyRecord struct {
	ID                                 int
	Name                               string
	LegacyStrategyType                 string
	IsDefault                          bool
	FailureStatusCodes                 []int
	BanMode                            string
	RetryBaseDelayMS                   int
	RetryBackoffMultiplier             float64
	RetryJitterRatio                   float64
	RetryMaxDelayMS                    int
	CycleRetryAttemptLimit             int
	BanCumulativeRetryAttemptThreshold int
	BanDurationSeconds                 int
}

type modelConnectionCounts struct {
	Total  int
	Active int
}

type modelHealthStats struct {
	SuccessRate   *float64
	TotalRequests int
}

type endpointModelConnectionRow struct {
	EndpointID           int
	TerminalConnectionID int
	ConnectionIsActive   bool
	ReachableModelID     int
	ReachableModelData   modelRecord
}

const modelRecordSelectColumns = `model_configs.id, model_configs.profile_id, model_configs.api_family, model_configs.model_id, model_configs.display_name, model_configs.loadbalance_strategy_id, model_configs.openai_accepted_format, model_configs.openai_image_operations, model_configs.is_enabled, model_configs.created_at, model_configs.updated_at`

func listModelRecords(ctx context.Context, exec queryExecutor, profileID int) ([]modelRecord, error) {
	rows, err := exec.Query(ctx, `SELECT `+modelRecordSelectColumns+` FROM model_configs WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query models for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]modelRecord, 0)
	for rows.Next() {
		record, scanErr := scanModelRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate models for profile %d: %w", profileID, err)
	}
	return items, nil
}

func loadModelRecord(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, forUpdate bool) (modelRecord, bool, error) {
	query := `SELECT ` + modelRecordSelectColumns + ` FROM model_configs WHERE profile_id = $1 AND id = $2`
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

func insertModel(ctx context.Context, tx pgx.Tx, record modelRecord) (modelRecord, error) {
	var createdID int
	if err := tx.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, openai_image_operations, is_enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`, record.ProfileID, record.APIFamily, record.ModelID, nullableString(record.DisplayName), nullableInt(record.LoadbalanceStrategyID), nullableString(record.OpenAIAcceptedFormat), nullableString(record.OpenAIImageOperations), record.IsEnabled, record.CreatedAt, record.UpdatedAt).Scan(&createdID); err != nil {
		if isUniqueViolation(err, "uq_model_configs_profile_model_id") {
			return modelRecord{}, &domainError{StatusCode: 409, Detail: fmt.Sprintf("Model ID '%s' already exists", record.ModelID)}
		}
		return modelRecord{}, fmt.Errorf("insert model %q: %w", record.ModelID, err)
	}
	record.ID = createdID
	return record, nil
}

func updateModel(ctx context.Context, tx pgx.Tx, record modelRecord) (modelRecord, error) {
	if _, err := tx.Exec(ctx, `UPDATE model_configs SET api_family = $2, model_id = $3, display_name = $4, loadbalance_strategy_id = $5, openai_accepted_format = $6, openai_image_operations = $7, is_enabled = $8, updated_at = $9 WHERE id = $1`, record.ID, record.APIFamily, record.ModelID, nullableString(record.DisplayName), nullableInt(record.LoadbalanceStrategyID), nullableString(record.OpenAIAcceptedFormat), nullableString(record.OpenAIImageOperations), record.IsEnabled, record.UpdatedAt); err != nil {
		if isUniqueViolation(err, "uq_model_configs_profile_model_id") {
			return modelRecord{}, &domainError{StatusCode: 409, Detail: fmt.Sprintf("Model ID '%s' already exists", record.ModelID)}
		}
		return modelRecord{}, fmt.Errorf("update model %d: %w", record.ID, err)
	}
	return record, nil
}

func deleteModel(ctx context.Context, tx pgx.Tx, modelConfigID int) error {
	if _, err := tx.Exec(ctx, `DELETE FROM model_configs WHERE id = $1`, modelConfigID); err != nil {
		return fmt.Errorf("delete model %d: %w", modelConfigID, err)
	}
	return nil
}

func scanModelRecord(scanner interface{ Scan(...any) error }) (modelRecord, error) {
	var displayName sql.NullString
	var loadbalanceStrategyID sql.NullInt32
	var openAIAcceptedFormat sql.NullString
	var openAIImageOperations sql.NullString
	record := modelRecord{}
	if err := scanner.Scan(&record.ID, &record.ProfileID, &record.APIFamily, &record.ModelID, &displayName, &loadbalanceStrategyID, &openAIAcceptedFormat, &openAIImageOperations, &record.IsEnabled, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return modelRecord{}, err
	}
	record.DisplayName = nullableStringValue(displayName)
	record.LoadbalanceStrategyID = nullableInt32(loadbalanceStrategyID)
	record.OpenAIAcceptedFormat = nullableStringValue(openAIAcceptedFormat)
	record.OpenAIImageOperations = nullableStringValue(openAIImageOperations)
	return record, nil
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}
