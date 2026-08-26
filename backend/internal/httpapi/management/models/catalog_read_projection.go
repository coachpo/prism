package models

import (
	"context"
	"net/http"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/jackc/pgx/v5"
)

const catalogAutoMatchCandidateBudget = 20

// autoMatchHint computes the unique-exact match state for one api_family from
// an already-fetched catalog. The result never binds anything on its own.
func autoMatchHint(catalog *modelsdev.Catalog, apiFamily, modelID string) *modelCatalogAutoMatchPayload {
	matches := modelsdev.ExactMatches(catalog, apiFamily, modelID)
	if len(matches) > catalogAutoMatchCandidateBudget {
		matches = matches[:catalogAutoMatchCandidateBudget]
	}
	hint := &modelCatalogAutoMatchPayload{Available: true, Candidates: matches}
	switch {
	case len(matches) == 1:
		hint.Unique = true
		hint.Reason = "unique_match"
	case len(matches) > 1:
		hint.Reason = "ambiguous"
	default:
		hint.Reason = "no_match"
	}
	return hint
}

func catalogResponseFromBinding(binding catalogBindingRecord) modelCatalogResponse {
	payload := modelCatalogResponse{Bound: false, Source: nil, Override: nil, Effective: nil}
	if binding.ModelConfigID == 0 {
		return payload
	}
	response := binding.response()
	if response != nil {
		payload = *response
	}
	return payload
}

// loadModelForCatalog resolves the profile-scoped model record and rejects
// unknown ids before any catalog work happens.
func loadModelForCatalog(ctx context.Context, tx pgx.Tx, profileID, modelConfigID int) (modelRecord, error) {
	record, found, err := loadModelRecord(ctx, tx, profileID, modelConfigID, false)
	if err != nil {
		return modelRecord{}, err
	}
	if !found {
		return modelRecord{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
	}
	return record, nil
}
