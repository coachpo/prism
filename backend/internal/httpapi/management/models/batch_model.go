package models

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func (s *Service) prepareBatchModel(ctx context.Context, tx pgx.Tx, profileID int, action string, input batchRequest, item batchItemInput) (batchItemResult, func() error, error) {
	out := batchItemResult{ID: item.ID, Before: map[string]any{}, After: map[string]any{}}
	model, found, err := loadModelRecord(ctx, tx, profileID, item.ID, true)
	if err != nil {
		return out, nil, err
	}
	if !found {
		return out, nil, &domainError{StatusCode: 404, Detail: "Model not found"}
	}
	out.Label = model.ModelID
	if action == "model_strategy" {
		var strategyState []byte
		err = tx.QueryRow(ctx, `SELECT to_jsonb(s) FROM loadbalance_strategies s WHERE profile_id=$1 AND id=$2 FOR SHARE`, profileID, input.ReferenceID).Scan(&strategyState)
		if err == pgx.ErrNoRows {
			return out, nil, &domainError{StatusCode: 422, Detail: "Ban Policy not found"}
		}
		if err != nil {
			return out, nil, err
		}
		out.Before = map[string]any{"loadbalance_strategy_id": model.LoadbalanceStrategyID, "updated_at": model.UpdatedAt}
		// The reference snapshot participates in CAS, including a policy edit
		// between preview and apply even when its numeric identity stays the same.
		out.After = map[string]any{"loadbalance_strategy_id": input.ReferenceID, "policy_snapshot": string(strategyState)}
		out.ManualOverride = model.LoadbalanceStrategyID != nil && *model.LoadbalanceStrategyID != input.ReferenceID
		return out, func() error {
			model.LoadbalanceStrategyID = &input.ReferenceID
			model.UpdatedAt = nextCatalogBindingUpdatedAt(model.UpdatedAt, s.nowUTC())
			_, err := updateModel(ctx, tx, model)
			return err
		}, nil
	}
	binding, found, err := loadCatalogBindingForUpdate(ctx, tx, profileID, item.ID)
	if err != nil {
		return out, nil, err
	}
	if !found {
		return out, nil, &domainError{StatusCode: 422, Detail: "Bind a reliable models.dev source before editing limits"}
	}
	effective := binding.Source.effective(binding.Override)
	out.Before = map[string]any{"context_limit": effective.LimitContext, "output_limit": effective.LimitOutput, "input_limit": effective.LimitInput, "binding_updated_at": binding.UpdatedAt, "provider_id": binding.ProviderID, "catalog_model_id": binding.CatalogModelID, "model_updated_at": model.UpdatedAt}
	out.After = map[string]any{"context_limit": item.ContextLimit, "output_limit": item.OutputLimit}
	out.ManualOverride = (binding.Override.LimitContext != nil && item.ContextLimit != nil && *binding.Override.LimitContext != *item.ContextLimit) || (binding.Override.LimitOutput != nil && item.OutputLimit != nil && *binding.Override.LimitOutput != *item.OutputLimit)
	out.Error = limitInputError(item)
	if out.Error == "" && effective.LimitInput != nil && *effective.LimitInput > *item.ContextLimit {
		out.Error = "existing input_limit exceeds the proposed context_limit; repair this field first"
	}
	if out.Error != "" {
		return out, nil, nil
	}
	return out, func() error {
		binding.Override.LimitContext = item.ContextLimit
		binding.Override.LimitOutput = item.OutputLimit
		return updateCatalogBindingOverride(ctx, tx, item.ID, binding.Override, nextCatalogBindingUpdatedAt(binding.UpdatedAt, s.nowUTC()))
	}, nil
}
