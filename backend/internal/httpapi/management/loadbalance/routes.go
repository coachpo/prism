package loadbalance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func (s *Service) handleListStrategies(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) ([]loadbalanceStrategyResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return nil, err
		}
		rows, err := listStrategyRows(r.Context(), tx, profile.ID)
		if err != nil {
			return nil, err
		}
		items := make([]loadbalanceStrategyResponse, 0, len(rows))
		for _, row := range rows {
			response, err := strategyResponseFromRow(row)
			if err != nil {
				return nil, err
			}
			items = append(items, response)
		}
		return items, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetStrategy(w http.ResponseWriter, r *http.Request) {
	strategyID, err := routeInt(r, "strategy_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (loadbalanceStrategyResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		row, found, err := loadStrategyRow(r.Context(), tx, profile.ID, strategyID, false)
		if err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		if !found {
			return loadbalanceStrategyResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Loadbalance strategy not found"}
		}
		return strategyResponseFromRow(row)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateStrategy(w http.ResponseWriter, r *http.Request) {
	requestBody, err := decodeStrategyRequest(r)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	persisted, err := canonicalizeStrategyRequest(requestBody)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (loadbalanceStrategyResponse, error) {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return loadbalanceStrategyResponse{}, &domainError{StatusCode: http.StatusServiceUnavailable, Code: "routing_owner_unavailable", Detail: "Routing authoring is temporarily unavailable"}
		}
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		if err := lockProfileStrategyDefaults(r.Context(), tx, profile.ID); err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		exists, err := strategyNameExists(r.Context(), tx, profile.ID, persisted.Name, nil)
		if err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		if exists {
			return loadbalanceStrategyResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "Loadbalance strategy name already exists"}
		}
		created, err := insertStrategy(r.Context(), tx, profile.ID, persisted, false, s.nowUTC())
		if err != nil {
			if isUniqueViolation(err, "uq_loadbalance_strategies_profile_name") {
				return loadbalanceStrategyResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "Loadbalance strategy name already exists"}
			}
			return loadbalanceStrategyResponse{}, err
		}
		return strategyResponseFromRow(created)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateStrategy(w http.ResponseWriter, r *http.Request) {
	strategyID, err := routeInt(r, "strategy_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	requestBody, err := decodeStrategyRequest(r)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	persisted, err := canonicalizeStrategyRequest(requestBody)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (loadbalanceStrategyResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		_, found, err := loadStrategyRow(r.Context(), tx, profile.ID, strategyID, true)
		if err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		if !found {
			return loadbalanceStrategyResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Loadbalance strategy not found"}
		}
		exists, err := strategyNameExists(r.Context(), tx, profile.ID, persisted.Name, &strategyID)
		if err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		if exists {
			return loadbalanceStrategyResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "Loadbalance strategy name already exists"}
		}
		updated, err := updateStrategy(r.Context(), tx, strategyID, persisted, s.nowUTC())
		if err != nil {
			if isUniqueViolation(err, "uq_loadbalance_strategies_profile_name") {
				return loadbalanceStrategyResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "Loadbalance strategy name already exists"}
			}
			return loadbalanceStrategyResponse{}, err
		}
		after, err := strategyResponseFromRow(updated)
		if err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		return after, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteStrategy(w http.ResponseWriter, r *http.Request) {
	strategyID, err := routeInt(r, "strategy_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (deletedResponse, error) {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return deletedResponse{}, &domainError{StatusCode: http.StatusServiceUnavailable, Code: "routing_owner_unavailable", Detail: "Routing authoring is temporarily unavailable"}
		}
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return deletedResponse{}, err
		}
		if err := lockProfileStrategyDefaults(r.Context(), tx, profile.ID); err != nil {
			return deletedResponse{}, err
		}
		current, found, err := loadStrategyRow(r.Context(), tx, profile.ID, strategyID, true)
		if err != nil {
			return deletedResponse{}, err
		}
		if !found {
			return deletedResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Loadbalance strategy not found"}
		}
		if current.AttachedModelCount > 0 {
			return deletedResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: map[string]any{"message": "Cannot delete loadbalance strategy that is attached to models", "attached_model_count": current.AttachedModelCount}}
		}
		if current.IsDefault {
			return deletedResponse{}, &domainError{StatusCode: http.StatusConflict, Code: "default_strategy_replacement_required", Detail: map[string]any{"message": "Cannot delete the default loadbalance strategy; set another strategy as the new model default first (or create built-in strategies when none remain)", "default_strategy_id": current.ID}}
		}
		if err := deleteStrategy(r.Context(), tx, strategyID); err != nil {
			return deletedResponse{}, err
		}
		return deletedResponse{Deleted: true}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateStrategyDefaults(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (loadbalanceStrategyDefaultsResponse, error) {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return loadbalanceStrategyDefaultsResponse{}, &domainError{StatusCode: http.StatusServiceUnavailable, Code: "routing_owner_unavailable", Detail: "Routing authoring is temporarily unavailable"}
		}
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return loadbalanceStrategyDefaultsResponse{}, err
		}
		if err := lockProfileStrategyDefaults(r.Context(), tx, profile.ID); err != nil {
			return loadbalanceStrategyDefaultsResponse{}, err
		}
		rows, err := listStrategyRows(r.Context(), tx, profile.ID)
		if err != nil {
			return loadbalanceStrategyDefaultsResponse{}, err
		}
		existingByName := map[string]loadbalanceStrategyResponse{}
		for _, row := range rows {
			response, err := strategyResponseFromRow(row)
			if err != nil {
				return loadbalanceStrategyDefaultsResponse{}, err
			}
			existingByName[response.Name] = response
		}
		createdResults := make([]loadbalanceStrategyCanonicalResult, 0, 3)
		existingResults := make([]loadbalanceStrategyCanonicalResult, 0, 3)
		conflictingNames := make([]string, 0)
		for _, spec := range canonicalDefaultStrategySpecs() {
			current, ok := existingByName[spec.Name]
			if !ok {
				continue
			}
			if strategyMatchesCanonicalDefault(current, spec) {
				existingResults = append(existingResults, loadbalanceStrategyCanonicalResult{CanonicalName: spec.Name, StrategyID: current.ID})
				continue
			}
			conflictingNames = append(conflictingNames, spec.Name)
		}
		if len(conflictingNames) > 0 {
			return loadbalanceStrategyDefaultsResponse{}, &domainError{StatusCode: http.StatusConflict, Code: "canonical_strategy_conflict", Detail: map[string]any{"message": "Canonical loadbalance strategy default name conflict", "conflicting_names": conflictingNames}}
		}
		currentDefault, defaultFound, err := loadCurrentDefaultStrategyRow(r.Context(), tx, profile.ID)
		if err != nil {
			return loadbalanceStrategyDefaultsResponse{}, err
		}
		var previousDefaultID *int
		if defaultFound {
			previousDefaultID = &currentDefault.ID
		}
		now := s.nowUTC()
		var canonicalFillFirstID *int
		for _, spec := range canonicalDefaultStrategySpecs() {
			if current, ok := existingByName[spec.Name]; ok {
				if spec.LegacyStrategyType == "fill-first" {
					id := current.ID
					canonicalFillFirstID = &id
				}
				continue
			}
			created, err := insertStrategy(r.Context(), tx, profile.ID, strategyPayloadFromRuntimeStrategy(defaultStrategyPayload(spec)), false, now)
			if err != nil {
				return loadbalanceStrategyDefaultsResponse{}, err
			}
			createdResults = append(createdResults, loadbalanceStrategyCanonicalResult{CanonicalName: spec.Name, StrategyID: created.ID})
			if spec.LegacyStrategyType == "fill-first" {
				id := created.ID
				canonicalFillFirstID = &id
			}
		}
		// An existing explicit default is never silently replaced; when no
		// default exists, the canonical fill-first row becomes the default.
		if !defaultFound && canonicalFillFirstID != nil {
			if err := setStrategyIsDefault(r.Context(), tx, *canonicalFillFirstID, true); err != nil {
				return loadbalanceStrategyDefaultsResponse{}, err
			}
		}
		finalDefault, finalDefaultFound, err := loadCurrentDefaultStrategyRow(r.Context(), tx, profile.ID)
		if err != nil {
			return loadbalanceStrategyDefaultsResponse{}, err
		}
		var defaultStrategyID *int
		defaultChanged := false
		if finalDefaultFound {
			defaultStrategyID = &finalDefault.ID
			if previousDefaultID == nil || *previousDefaultID != finalDefault.ID {
				defaultChanged = true
			}
		}
		complete := len(createdResults)+len(existingResults) == len(canonicalDefaultStrategySpecs())
		return loadbalanceStrategyDefaultsResponse{
			Created:           createdResults,
			Existing:          existingResults,
			DefaultStrategyID: defaultStrategyID,
			DefaultChanged:    defaultChanged,
			Complete:          complete,
		}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleSetStrategyDefault(w http.ResponseWriter, r *http.Request) {
	strategyID, err := routeInt(r, "strategy_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var requestEnvelope struct {
		ExpectedDefaultStrategyID json.RawMessage `json:"expected_default_strategy_id"`
	}
	if err := decoder.Decode(&requestEnvelope); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, responseutil.SanitizeDecodeError(err).Error())
		return
	}
	// expected_default_strategy_id is a present-but-nullable CAS key: an absent
	// key or a non-positive integer is a typed 400; null means the caller
	// expects that no default currently exists.
	if len(requestEnvelope.ExpectedDefaultStrategyID) == 0 {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "expected_default_strategy_id is required")
		return
	}
	var expectedDefaultStrategyID *int
	if string(requestEnvelope.ExpectedDefaultStrategyID) != "null" {
		var parsed int
		if err := json.Unmarshal(requestEnvelope.ExpectedDefaultStrategyID, &parsed); err != nil || parsed <= 0 {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "expected_default_strategy_id must be a positive integer or null")
			return
		}
		expectedDefaultStrategyID = &parsed
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (strategyDefaultMutationResponse, error) {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return strategyDefaultMutationResponse{}, &domainError{StatusCode: http.StatusServiceUnavailable, Code: "routing_owner_unavailable", Detail: "Routing authoring is temporarily unavailable"}
		}
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return strategyDefaultMutationResponse{}, err
		}
		if err := lockProfileStrategyDefaults(r.Context(), tx, profile.ID); err != nil {
			return strategyDefaultMutationResponse{}, err
		}
		_, found, err := loadStrategyRow(r.Context(), tx, profile.ID, strategyID, false)
		if err != nil {
			return strategyDefaultMutationResponse{}, err
		}
		if !found {
			return strategyDefaultMutationResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Loadbalance strategy not found"}
		}
		currentDefault, defaultFound, err := loadCurrentDefaultStrategyRow(r.Context(), tx, profile.ID)
		if err != nil {
			return strategyDefaultMutationResponse{}, err
		}
		// Idempotent no-op: the target already is the default; a stale expected
		// value must not break replay of a completed PUT.
		if defaultFound && currentDefault.ID == strategyID {
			previous := currentDefault.ID
			return strategyDefaultMutationResponse{DefaultStrategyID: strategyID, PreviousDefaultStrategyID: &previous, Changed: false}, nil
		}
		expectedMatchesCurrent := (expectedDefaultStrategyID == nil && !defaultFound) ||
			(expectedDefaultStrategyID != nil && defaultFound && *expectedDefaultStrategyID == currentDefault.ID)
		if !expectedMatchesCurrent {
			var currentID *int
			if defaultFound {
				currentID = &currentDefault.ID
			}
			return strategyDefaultMutationResponse{}, &domainError{StatusCode: http.StatusConflict, Code: "default_strategy_changed", Detail: map[string]any{"message": "Default loadbalance strategy changed since the request was prepared; reload and confirm again", "current_default_strategy_id": currentID}}
		}
		if defaultFound {
			if err := setStrategyIsDefault(r.Context(), tx, currentDefault.ID, false); err != nil {
				return strategyDefaultMutationResponse{}, err
			}
		}
		if err := setStrategyIsDefault(r.Context(), tx, strategyID, true); err != nil {
			return strategyDefaultMutationResponse{}, err
		}
		var previousID *int
		if defaultFound {
			previousID = &currentDefault.ID
		}
		return strategyDefaultMutationResponse{DefaultStrategyID: strategyID, PreviousDefaultStrategyID: previousID, Changed: true}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleListStrategyModels(w http.ResponseWriter, r *http.Request) {
	strategyID, err := routeInt(r, "strategy_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	limit, err := parsePositiveIntQueryWithDefault(r, "limit", 25)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	if limit > 100 {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "limit must be between 1 and 100")
		return
	}
	rawCursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (strategyImpactListResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return strategyImpactListResponse{}, err
		}
		strategy, found, err := loadStrategyRow(r.Context(), tx, profile.ID, strategyID, false)
		if err != nil {
			return strategyImpactListResponse{}, err
		}
		if !found {
			return strategyImpactListResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Loadbalance strategy not found"}
		}
		generation, err := readProfilePlanningGeneration(r.Context(), tx, profile.ID)
		if err != nil {
			return strategyImpactListResponse{}, err
		}
		var afterDisplayKey *string
		var afterModelConfigID *int
		if rawCursor != "" {
			cursor, err := decodeStrategyImpactCursor(rawCursor)
			if err != nil {
				return strategyImpactListResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Code: "cursor_scope_mismatch", Detail: "invalid strategy impact cursor"}
			}
			if cursor.ProfileID != profile.ID || cursor.StrategyID != strategyID || cursor.Limit != limit {
				return strategyImpactListResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Code: "cursor_scope_mismatch", Detail: "strategy impact cursor scope mismatch"}
			}
			if cursor.PlanningGeneration != generation {
				return strategyImpactListResponse{}, &domainError{StatusCode: http.StatusConflict, Code: "impact_cursor_stale", Detail: map[string]any{"message": "Strategy impact list changed since the cursor was issued; reload from the first page"}}
			}
			afterDisplayKey = &cursor.AfterDisplayKey
			afterModelConfigID = &cursor.AfterModelConfigID
		}
		rows, err := listStrategyAttachedModels(r.Context(), tx, profile.ID, strategyID, limit+1, afterDisplayKey, afterModelConfigID)
		if err != nil {
			return strategyImpactListResponse{}, err
		}
		hasMore := len(rows) > limit
		if hasMore {
			rows = rows[:limit]
		}
		items := make([]strategyImpactModelItem, 0, len(rows))
		for _, row := range rows {
			item := strategyImpactModelItem{ModelConfigID: row.ModelConfigID, ModelID: row.ModelID, IsEnabled: row.IsEnabled}
			if row.DisplayName != nil {
				item.DisplayName = *row.DisplayName
			}
			items = append(items, item)
		}
		var nextCursor *string
		if hasMore && len(rows) > 0 {
			last := rows[len(rows)-1]
			displayKey := ""
			if last.DisplayName != nil {
				displayKey = strings.ToLower(strings.TrimSpace(*last.DisplayName))
			}
			encoded, err := encodeStrategyImpactCursor(strategyImpactCursor{
				ProfileID:          profile.ID,
				StrategyID:         strategyID,
				Limit:              limit,
				PlanningGeneration: generation,
				AfterDisplayKey:    displayKey,
				AfterModelConfigID: last.ModelConfigID,
			})
			if err != nil {
				return strategyImpactListResponse{}, err
			}
			nextCursor = &encoded
		}
		return strategyImpactListResponse{StrategyID: strategyID, AttachedModelCount: strategy.AttachedModelCount, Items: items, HasMore: hasMore, NextCursor: nextCursor}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handlePreviewStrategy(w http.ResponseWriter, r *http.Request) {
	requestBody, err := decodeStrategyRequest(r)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	persisted, err := canonicalizeStrategyPolicyFields(requestBody)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	strategy := loadbalancedomain.RuntimeStrategy{
		Name:                               persisted.Name,
		LegacyStrategyType:                 &persisted.LegacyStrategyType,
		FailureStatusCodes:                 append([]int(nil), persisted.FailureStatusCodes...),
		BanMode:                            persisted.BanMode,
		RetryBaseDelayMS:                   persisted.RetryBaseDelayMS,
		RetryBackoffMultiplier:             persisted.RetryBackoffMultiplier,
		RetryJitterRatio:                   persisted.RetryJitterRatio,
		RetryMaxDelayMS:                    persisted.RetryMaxDelayMS,
		CycleRetryAttemptLimit:             persisted.CycleRetryAttemptLimit,
		BanCumulativeRetryAttemptThreshold: persisted.BanCumulativeRetryAttemptThreshold,
		BanDurationSeconds:                 persisted.BanDurationSeconds,
	}
	preview := loadbalancedomain.PreviewRetryCycle(strategy.FeedbackPolicy())
	response := strategyPreviewResponse{
		NormalizedPolicy: strategyPolicyFieldsResponse{
			Name:                               persisted.Name,
			LegacyStrategyType:                 persisted.LegacyStrategyType,
			FailureStatusCodes:                 append([]int(nil), persisted.FailureStatusCodes...),
			BanMode:                            persisted.BanMode,
			RetryBaseDelayMS:                   persisted.RetryBaseDelayMS,
			RetryBackoffMultiplier:             persisted.RetryBackoffMultiplier,
			RetryJitterRatio:                   persisted.RetryJitterRatio,
			RetryMaxDelayMS:                    persisted.RetryMaxDelayMS,
			CycleRetryAttemptLimit:             persisted.CycleRetryAttemptLimit,
			BanCumulativeRetryAttemptThreshold: persisted.BanCumulativeRetryAttemptThreshold,
			BanDurationSeconds:                 persisted.BanDurationSeconds,
		},
		RetryPreviewResult: preview,
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func decodeStrategyRequest(request *http.Request) (loadbalanceStrategyRequest, error) {
	defer func() { _ = request.Body.Close() }()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var requestBody loadbalanceStrategyRequest
	if err := decoder.Decode(&requestBody); err != nil {
		return loadbalanceStrategyRequest{}, responseutil.SanitizeDecodeError(err)
	}
	return requestBody, nil
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var loadbalanceErr *domainError
	if errors.As(err, &loadbalanceErr) {
		if strings.TrimSpace(loadbalanceErr.Code) != "" {
			responseutil.WriteErrorFields(w, r, corsSnapshot, loadbalanceErr.StatusCode, loadbalanceErr.Detail, map[string]any{"code": loadbalanceErr.Code})
			return
		}
		responseutil.WriteError(w, r, corsSnapshot, loadbalanceErr.StatusCode, loadbalanceErr.Detail)
		return
	}
	var loadbalanceDomainErr *loadbalancedomain.HTTPError
	if errors.As(err, &loadbalanceDomainErr) {
		if strings.TrimSpace(loadbalanceDomainErr.Code) != "" {
			responseutil.WriteErrorFields(w, r, corsSnapshot, loadbalanceDomainErr.StatusCode, loadbalanceDomainErr.Detail, map[string]any{"code": loadbalanceDomainErr.Code})
			return
		}
		responseutil.WriteError(w, r, corsSnapshot, loadbalanceDomainErr.StatusCode, loadbalanceDomainErr.Detail)
		return
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	responseutil.WriteError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func routeInt(request *http.Request, name string) (int, error) {
	value := strings.TrimSpace(chi.URLParam(request, name))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}
