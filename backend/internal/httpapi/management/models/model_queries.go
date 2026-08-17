package models

// Model query composition joins the model, strategy, access-target, and
// retained-health read paths used by route handlers. It owns query fanout but
// leaves row scanning and graph rules to their dedicated modules.

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

func ensureModelIDAvailable(ctx context.Context, exec queryExecutor, profileID int, modelID string, excludeID *int) error {
	query := `SELECT id FROM model_configs WHERE profile_id = $1 AND model_id = $2`
	args := []any{profileID, modelID}
	if excludeID != nil {
		query += ` AND id <> $3`
		args = append(args, *excludeID)
	}
	query += ` LIMIT 1`
	var existingID int
	err := exec.QueryRow(ctx, query, args...).Scan(&existingID)
	if err == nil {
		return &domainError{StatusCode: 409, Detail: fmt.Sprintf("Model ID '%s' already exists", modelID)}
	}
	if err == pgx.ErrNoRows {
		return nil
	}
	return fmt.Errorf("query model id availability for %q: %w", modelID, err)
}

func loadModelRelations(ctx context.Context, tx pgx.Tx, profileID int, records []modelRecord) (map[int]strategyRecord, map[int][]accessTargetRecord, map[string]modelHealthStats, error) {
	strategyIDs := uniqueIntValues(records, func(record modelRecord) *int { return record.LoadbalanceStrategyID })
	modelIDs := uniqueModelIDs(records)
	strategies, err := loadStrategyRecordsByIDs(ctx, tx, profileID, strategyIDs)
	if err != nil {
		return nil, nil, nil, err
	}
	accessTargets, err := loadAccessTargetsForModels(ctx, tx, profileID, modelIDs)
	if err != nil {
		return nil, nil, nil, err
	}
	health, err := listModelHealthStats(ctx, tx, profileID)
	if err != nil {
		return nil, nil, nil, err
	}
	return strategies, accessTargets, health, nil
}

func uniqueIntValues(records []modelRecord, selector func(modelRecord) *int) []int {
	seen := map[int]struct{}{}
	values := make([]int, 0)
	for _, record := range records {
		value := selector(record)
		if value == nil {
			continue
		}
		if _, ok := seen[*value]; ok {
			continue
		}
		seen[*value] = struct{}{}
		values = append(values, *value)
	}
	sort.Ints(values)
	return values
}

func uniqueModelIDs(records []modelRecord) []int {
	seen := map[int]struct{}{}
	values := make([]int, 0, len(records))
	for _, record := range records {
		if _, ok := seen[record.ID]; ok {
			continue
		}
		seen[record.ID] = struct{}{}
		values = append(values, record.ID)
	}
	sort.Ints(values)
	return values
}
