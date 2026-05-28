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
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetStrategy(w http.ResponseWriter, r *http.Request) {
	strategyID, err := routeInt(r, "strategy_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
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
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateStrategy(w http.ResponseWriter, r *http.Request) {
	requestBody, err := decodeStrategyRequest(r)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
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
		exists, err := strategyNameExists(r.Context(), tx, profile.ID, persisted.Name, nil)
		if err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		if exists {
			return loadbalanceStrategyResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "Loadbalance strategy name already exists"}
		}
		created, err := insertStrategy(r.Context(), tx, profile.ID, persisted, s.nowUTC())
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
	writeJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateStrategy(w http.ResponseWriter, r *http.Request) {
	strategyID, err := routeInt(r, "strategy_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	requestBody, err := decodeStrategyRequest(r)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
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
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteStrategy(w http.ResponseWriter, r *http.Request) {
	strategyID, err := routeInt(r, "strategy_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (deletedResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
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
		if err := deleteStrategy(r.Context(), tx, strategyID); err != nil {
			return deletedResponse{}, err
		}
		return deletedResponse{Deleted: true}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateStrategyDefaults(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (loadbalanceStrategyDefaultsResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
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
		createdNames := make([]string, 0, 3)
		existingNames := make([]string, 0, 3)
		conflictingNames := make([]string, 0)
		for _, spec := range canonicalDefaultStrategySpecs() {
			current, ok := existingByName[spec.Name]
			if !ok {
				continue
			}
			if strategyMatchesCanonicalDefault(current, spec) {
				existingNames = append(existingNames, spec.Name)
				continue
			}
			conflictingNames = append(conflictingNames, spec.Name)
		}
		if len(conflictingNames) > 0 {
			return loadbalanceStrategyDefaultsResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: map[string]any{"message": "Canonical loadbalance strategy default name conflict", "conflicting_names": conflictingNames}}
		}
		now := s.nowUTC()
		for _, spec := range canonicalDefaultStrategySpecs() {
			if _, ok := existingByName[spec.Name]; ok {
				continue
			}
			if _, err := insertStrategy(r.Context(), tx, profile.ID, defaultStrategyPayload(spec), now); err != nil {
				return loadbalanceStrategyDefaultsResponse{}, err
			}
			createdNames = append(createdNames, spec.Name)
		}
		finalRows, err := listStrategyRows(r.Context(), tx, profile.ID)
		if err != nil {
			return loadbalanceStrategyDefaultsResponse{}, err
		}
		items := make([]loadbalanceStrategyResponse, 0, len(finalRows))
		for _, row := range finalRows {
			response, err := strategyResponseFromRow(row)
			if err != nil {
				return loadbalanceStrategyDefaultsResponse{}, err
			}
			items = append(items, response)
		}
		return loadbalanceStrategyDefaultsResponse{Items: items, CreatedCount: len(createdNames), CreatedNames: createdNames, ExistingNames: existingNames}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func decodeStrategyRequest(request *http.Request) (loadbalanceStrategyRequest, error) {
	defer func() { _ = request.Body.Close() }()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var requestBody loadbalanceStrategyRequest
	if err := decoder.Decode(&requestBody); err != nil {
		return loadbalanceStrategyRequest{}, err
	}
	return requestBody, nil
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var loadbalanceErr *domainError
	if errors.As(err, &loadbalanceErr) {
		writeError(w, r, corsSnapshot, loadbalanceErr.StatusCode, loadbalanceErr.Detail)
		return
	}
	var loadbalanceDomainErr *loadbalancedomain.HTTPError
	if errors.As(err, &loadbalanceDomainErr) {
		writeError(w, r, corsSnapshot, loadbalanceDomainErr.StatusCode, loadbalanceDomainErr.Detail)
		return
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail any) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
	writeJSON(w, statusCode, map[string]any{"detail": detail})
}

func routeInt(request *http.Request, name string) (int, error) {
	value := strings.TrimSpace(chi.URLParam(request, name))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}
