package models

import (
	"context"
	"errors"
	"github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"net/http"
)

func (s *Service) mountBatchRoutes(api chi.Router) {
	for _, action := range []string{"model_limits", "model_strategy", "target_pricing"} {
		api.Post("/models/batch/"+action+"/preview", privateNoStore(s.batchHandler(action, false)))
		api.Post("/models/batch/"+action+"/apply", privateNoStore(s.batchHandler(action, true)))
	}
}

func (s *Service) batchHandler(action string, apply bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input batchRequest
		if err := decodeJSONBody(r, &input); err != nil {
			responseutil.WriteError(w, r, s.corsSnapshot(), 400, "Invalid request body")
			return
		}
		if err := validateBatchRequest(action, &input); err != nil {
			writeDomainError(w, r, s.corsSnapshot(), err)
			return
		}
		result, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (batchResponse, error) {
			profile, err := resolveEffectiveProfile(r.Context(), tx, r)
			if err != nil {
				return batchResponse{}, err
			}
			return s.executeBatch(r.Context(), tx, profile.ID, action, input, apply)
		})
		if err != nil {
			writeDomainError(w, r, s.corsSnapshot(), err)
			return
		}
		responseutil.WriteJSON(w, 200, result)
	}
}

func (s *Service) executeBatch(ctx context.Context, tx pgx.Tx, profileID int, action string, input batchRequest, apply bool) (batchResponse, error) {
	out := batchResponse{Action: action, CanApply: true, Items: make([]batchItemResult, 0, len(input.Items))}
	if action == "target_pricing" {
		if err := connections.LockBatchPricingScopeTx(ctx, tx, profileID); err != nil {
			return out, err
		}
	}
	// Lock owner models in deterministic order before any child binding/target.
	// This shares the single-resource writer lock order and freezes identity.
	ids := make([]int, 0, len(input.Items))
	for _, item := range input.Items {
		ids = append(ids, item.ID)
	}
	query := `SELECT id FROM model_configs WHERE profile_id=$1 AND id=ANY($2) ORDER BY id FOR UPDATE`
	if action == "target_pricing" {
		query = `SELECT id FROM model_configs WHERE profile_id=$1 AND id IN (SELECT source_model_config_id FROM model_access_targets WHERE profile_id=$1 AND target_connection_id=ANY($2)) ORDER BY id FOR UPDATE`
	}
	rows, err := tx.Query(ctx, query, profileID, int32ArrayArg(ids))
	if err != nil {
		return out, err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	writes := make([]func() error, 0, len(input.Items))
	for _, item := range input.Items {
		var result batchItemResult
		var write func() error
		if action == "target_pricing" {
			var assignment connections.BatchPricingAssignment
			assignment, err = connections.PrepareBatchPricingAssignmentTx(ctx, tx, profileID, item.ID, input.ReferenceID, s.nowUTC())
			result = batchItemResult{ID: item.ID, Label: assignment.Label, Before: assignment.Before, After: assignment.After, ManualOverride: assignment.ManualOverride}
			write = func() error { return assignment.Apply(ctx, tx, profileID, s.nowUTC()) }
		} else {
			result, write, err = s.prepareBatchModel(ctx, tx, profileID, action, input, item)
		}
		if err != nil {
			var modelErr *domainError
			var targetErr *connections.DomainError
			if !errors.As(err, &modelErr) && !errors.As(err, &targetErr) {
				return out, err
			}
			result = batchItemError(item.ID, err)
		}
		if result.Error != "" {
			out.CanApply = false
		}
		out.Items = append(out.Items, result)
		writes = append(writes, write)
	}
	out.PreviewToken = batchToken(action, input, out.Items)
	if !apply {
		return out, nil
	}
	if input.PreviewToken == "" || input.PreviewToken != out.PreviewToken {
		return out, &domainError{StatusCode: 409, Detail: "batch_preview_stale: re-read and preview every item before applying", Fields: map[string]any{"batch": out}}
	}
	if !out.CanApply {
		return out, &domainError{StatusCode: 422, Detail: "batch_invalid: no items were written", Fields: map[string]any{"batch": out}}
	}
	for _, item := range out.Items {
		if item.ManualOverride && !input.ConfirmManualOverrides {
			return out, &domainError{StatusCode: 422, Detail: "manual_override_confirmation_required: explicitly confirm the displayed replacements"}
		}
	}
	for _, write := range writes {
		if err := write(); err != nil {
			return out, err
		}
	}
	if action == "target_pricing" {
		if err := connections.FinishBatchPricingAssignmentsTx(ctx, tx, profileID, s.nowUTC()); err != nil {
			return out, err
		}
	}
	// Re-read through the same resource owners under the transaction. Applied
	// values are database facts; clients also invalidate their related lists.
	readback, err := s.executeBatch(ctx, tx, profileID, action, input, false)
	if err != nil {
		return out, err
	}
	readback.Applied = true
	return readback, nil
}
