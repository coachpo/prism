package auth

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func (s *Service) handleListProxyKeys(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	for _, value := range r.URL.Query()["include"] {
		if strings.TrimSpace(value) == "setup_readiness" {
			expectedGeneration := strings.TrimSpace(r.URL.Query().Get("expected_route_witness_generation"))
			if expectedGeneration == "" {
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "expected_route_witness_generation is required with include=setup_readiness")
				return
			}
			s.handleListProxyKeysWithSetupReadiness(w, r, expectedGeneration)
			return
		}
	}
	rows, err := s.listProxyAPIKeys(r.Context(), s.pool)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load proxy API keys")
		return
	}
	capacity, err := countProxyKeyCapacity(r.Context(), s.pool, s.nowUTC())
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load proxy API key capacity")
		return
	}
	response := proxyAPIKeyListResponse{Items: make([]proxyAPIKeyResponse, 0, len(rows)), Capacity: capacity}
	for _, row := range rows {
		response.Items = append(response.Items, s.serializeProxyAPIKey(row))
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateProxyKey(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	var requestBody proxyAPIKeyCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	name, err := validateProxyKeyName(requestBody.Name)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	notes := normalizeNotes(requestBody.Notes)
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (proxyAPIKeyMutationResponse, error) {
		rawKey, row, capacity, createErr := s.createProxyAPIKey(r.Context(), tx, name, notes, requestBody.ExpiresAt, authSubjectIDFromRequest(r))
		if createErr != nil {
			return proxyAPIKeyMutationResponse{}, createErr
		}
		return proxyAPIKeyMutationResponse{Key: rawKey, Item: s.serializeProxyAPIKey(row), Capacity: capacity}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusCreated, result)
}

func (s *Service) handleUpdateProxyKey(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	keyID, err := routeInt(r, "key_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody proxyAPIKeyUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	name, err := validateProxyKeyName(requestBody.Name)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	notes := normalizeNotes(requestBody.Notes)
	type updateResult struct {
		row      proxyAPIKeyRow
		capacity proxyKeyCapacitySnapshot
	}
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (updateResult, error) {
		row, capacity, updateErr := s.updateProxyAPIKey(r.Context(), tx, keyID, name, notes, requestBody.IsActive, requestBody.ExpiresAt)
		if updateErr != nil {
			return updateResult{}, updateErr
		}
		return updateResult{row: row, capacity: capacity}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, proxyAPIKeyUpdateResponse{Item: s.serializeProxyAPIKey(result.row), Capacity: result.capacity})
}

func (s *Service) handleRotateProxyKey(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	keyID, err := routeInt(r, "key_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (proxyAPIKeyMutationResponse, error) {
		rawKey, row, capacity, rotateErr := s.rotateProxyAPIKey(r.Context(), tx, keyID)
		if rotateErr != nil {
			return proxyAPIKeyMutationResponse{}, rotateErr
		}
		return proxyAPIKeyMutationResponse{Key: rawKey, Item: s.serializeProxyAPIKey(row), Capacity: capacity}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, result)
}

func (s *Service) handleDeleteProxyKey(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	keyID, err := routeInt(r, "key_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	capacity, err := pgxutil.InTxValue(r.Context(), s.pool, "auth", func(tx pgx.Tx) (proxyKeyCapacitySnapshot, error) {
		return s.deleteProxyAPIKey(r.Context(), tx, keyID)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, deletedResponse{DeletedID: keyID, Capacity: capacity})
}

func routeInt(request *http.Request, name string) (int, error) {
	value := strings.TrimSpace(chi.URLParam(request, name))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}
