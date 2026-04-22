package loadbalance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/pgxutil"
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetStrategy(w http.ResponseWriter, r *http.Request) {
	strategyID, err := routeInt(r, "strategy_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateStrategy(w http.ResponseWriter, r *http.Request) {
	requestBody, err := decodeStrategyRequest(r)
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	persisted, err := canonicalizeStrategyRequest(requestBody)
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateStrategy(w http.ResponseWriter, r *http.Request) {
	strategyID, err := routeInt(r, "strategy_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	requestBody, err := decodeStrategyRequest(r)
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	persisted, err := canonicalizeStrategyRequest(requestBody)
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (loadbalanceStrategyResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		current, found, err := loadStrategyRow(r.Context(), tx, profile.ID, strategyID, true)
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
		before, err := strategyResponseFromRow(current)
		if err != nil {
			return loadbalanceStrategyResponse{}, err
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
		if strategyPolicyChanged(before, after) {
			if err := clearStrategyState(r.Context(), tx, profile.ID, strategyID); err != nil {
				return loadbalanceStrategyResponse{}, err
			}
		}
		return after, nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteStrategy(w http.ResponseWriter, r *http.Request) {
	strategyID, err := routeInt(r, "strategy_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
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
		writeDomainError(w, r, s.allowedOrigins, err)
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
		createdNames := make([]string, 0, 2)
		existingNames := make([]string, 0, 2)
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
			payload := strategyPersistedPayload{Name: spec.Name, StrategyType: spec.StrategyType, LegacyStrategyType: spec.LegacyStrategyType, AutoRecovery: spec.AutoRecovery, RoutingPolicy: spec.RoutingPolicy}
			if spec.AutoRecovery != nil {
				payload.AutoRecoveryJSON, err = json.Marshal(spec.AutoRecovery)
				if err != nil {
					return loadbalanceStrategyDefaultsResponse{}, fmt.Errorf("marshal default auto_recovery: %w", err)
				}
			}
			if spec.RoutingPolicy != nil {
				payload.RoutingPolicyJSON, err = json.Marshal(spec.RoutingPolicy)
				if err != nil {
					return loadbalanceStrategyDefaultsResponse{}, fmt.Errorf("marshal default routing_policy: %w", err)
				}
			}
			if _, err := insertStrategy(r.Context(), tx, profile.ID, payload, now); err != nil {
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func strategyPolicyChanged(before loadbalanceStrategyResponse, after loadbalanceStrategyResponse) bool {
	if before.StrategyType != after.StrategyType {
		return true
	}
	if !reflect.DeepEqual(before.LegacyStrategyType, after.LegacyStrategyType) {
		return true
	}
	if !reflect.DeepEqual(before.AutoRecovery, after.AutoRecovery) {
		return true
	}
	if !reflect.DeepEqual(before.RoutingPolicy, after.RoutingPolicy) {
		return true
	}
	return false
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

func writeDomainError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, err error) {
	var loadbalanceErr *domainError
	if errors.As(err, &loadbalanceErr) {
		writeError(w, r, allowedOrigins, loadbalanceErr.StatusCode, loadbalanceErr.Detail)
		return
	}
	var loadbalanceDomainErr *loadbalancedomain.HTTPError
	if errors.As(err, &loadbalanceDomainErr) {
		writeError(w, r, allowedOrigins, loadbalanceDomainErr.StatusCode, loadbalanceDomainErr.Detail)
		return
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		writeError(w, r, allowedOrigins, profileErr.StatusCode, profileErr.Detail)
		return
	}
	writeError(w, r, allowedOrigins, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, statusCode int, detail any) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		if _, ok := allowedOrigins[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
	}
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
