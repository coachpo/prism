package endpoints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func (s *Service) handleListEndpoints(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint", func(tx pgx.Tx) ([]endpointResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		records, err := listOrderedEndpoints(r.Context(), tx, profile.ID)
		if err != nil {
			return nil, err
		}
		items := make([]endpointResponse, 0, len(records))
		for _, record := range records {
			items = append(items, responseFromRecord(record))
		}
		return items, nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleListEndpointConnections(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint", func(tx pgx.Tx) (connectionDropdownResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionDropdownResponse{}, err
		}
		items, err := listConnectionDropdownItems(r.Context(), tx, profile.ID)
		if err != nil {
			return connectionDropdownResponse{}, err
		}
		return connectionDropdownResponse{Items: items}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateEndpoint(w http.ResponseWriter, r *http.Request) {
	var requestBody endpointCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	endpointName := strings.TrimSpace(requestBody.Name)
	if endpointName == "" {
		writeError(w, r, s.allowedOrigins, http.StatusUnprocessableEntity, "name must not be empty")
		return
	}
	normalizedURL := endpointdomain.NormalizeBaseURL(requestBody.BaseURL)
	if warnings := endpointdomain.ValidateBaseURL(normalizedURL); len(warnings) > 0 {
		writeError(w, r, s.allowedOrigins, http.StatusUnprocessableEntity, strings.Join(warnings, "; "))
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint", func(tx pgx.Tx) (endpointResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return endpointResponse{}, err
		}
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return endpointResponse{}, err
		}
		if err := ensureUniqueEndpointName(r.Context(), tx, profile.ID, endpointName, nil); err != nil {
			return endpointResponse{}, err
		}
		encryptedAPIKey, err := endpointdomain.EncryptSecret(requestBody.APIKey, s.secretEncryptionKey, s.now)
		if err != nil {
			return endpointResponse{}, err
		}
		position, err := nextEndpointPosition(r.Context(), tx, profile.ID)
		if err != nil {
			return endpointResponse{}, err
		}
		record, err := insertEndpoint(r.Context(), tx, endpointRecord{ProfileID: profile.ID, Name: endpointName, BaseURL: normalizedURL, APIKey: encryptedAPIKey, Position: position, CreatedAt: s.nowUTC(), UpdatedAt: s.nowUTC()})
		if err != nil {
			return endpointResponse{}, err
		}
		return responseFromRecord(record), nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateEndpoint(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	var requestBody endpointUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint", func(tx pgx.Tx) (endpointResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return endpointResponse{}, err
		}
		record, found, err := loadEndpointRecord(r.Context(), tx, profile.ID, endpointID, true)
		if err != nil {
			return endpointResponse{}, err
		}
		if !found {
			return endpointResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}

		clearDependentRecoveryState := false
		if requestBody.Name.Set {
			endpointName := strings.TrimSpace(stringValue(requestBody.Name.Value))
			if endpointName == "" {
				return endpointResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "name must not be empty"}
			}
			if err := ensureUniqueEndpointName(r.Context(), tx, profile.ID, endpointName, intPtr(record.ID)); err != nil {
				return endpointResponse{}, err
			}
			record.Name = endpointName
		}
		if requestBody.BaseURL.Set {
			normalizedURL := endpointdomain.NormalizeBaseURL(stringValue(requestBody.BaseURL.Value))
			if warnings := endpointdomain.ValidateBaseURL(normalizedURL); len(warnings) > 0 {
				return endpointResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: strings.Join(warnings, "; ")}
			}
			if normalizedURL != record.BaseURL {
				clearDependentRecoveryState = true
			}
			record.BaseURL = normalizedURL
		}
		if requestBody.APIKey.Set && strings.TrimSpace(stringValue(requestBody.APIKey.Value)) != "" {
			incomingAPIKey := stringValue(requestBody.APIKey.Value)
			currentAPIKey, decryptErr := endpointdomain.DecryptSecret(record.APIKey, s.secretEncryptionKey)
			if decryptErr != nil || currentAPIKey != incomingAPIKey {
				clearDependentRecoveryState = true
			}
			encryptedAPIKey, encryptErr := endpointdomain.EncryptSecret(incomingAPIKey, s.secretEncryptionKey, s.now)
			if encryptErr != nil {
				return endpointResponse{}, encryptErr
			}
			record.APIKey = encryptedAPIKey
		}
		if clearDependentRecoveryState {
			usageRows, err := listEndpointUsageRows(r.Context(), tx, profile.ID, record.ID)
			if err != nil {
				return endpointResponse{}, err
			}
			connectionIDs := make([]int, 0, len(usageRows))
			for _, row := range usageRows {
				connectionIDs = append(connectionIDs, row.ConnectionID)
			}
			if err := clearConnectionRuntimeStates(r.Context(), tx, profile.ID, connectionIDs); err != nil {
				return endpointResponse{}, err
			}
		}
		record.UpdatedAt = s.nowUTC()
		updated, err := updateEndpointRecord(r.Context(), tx, record)
		if err != nil {
			return endpointResponse{}, err
		}
		return responseFromRecord(updated), nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleMoveEndpointPosition(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	var requestBody endpointPositionMoveRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint", func(tx pgx.Tx) ([]endpointResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return nil, err
		}
		records, err := listOrderedEndpoints(r.Context(), tx, profile.ID)
		if err != nil {
			return nil, err
		}
		currentIndex := -1
		for index := range records {
			if records[index].ID == endpointID {
				currentIndex = index
				break
			}
		}
		if currentIndex == -1 {
			return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}
		if requestBody.ToIndex < 0 || requestBody.ToIndex >= len(records) {
			return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("to_index must be between 0 and %d", len(records)-1)}
		}
		if requestBody.ToIndex == currentIndex {
			items := make([]endpointResponse, 0, len(records))
			for _, record := range records {
				items = append(items, responseFromRecord(record))
			}
			return items, nil
		}

		moved := records[currentIndex]
		records = append(records[:currentIndex], records[currentIndex+1:]...)
		records = append(records[:requestBody.ToIndex], append([]endpointRecord{moved}, records[requestBody.ToIndex:]...)...)
		normalizeEndpointPositions(records, s.nowUTC())
		if err := persistEndpointPositions(r.Context(), tx, records, s.nowUTC()); err != nil {
			return nil, err
		}
		items := make([]endpointResponse, 0, len(records))
		for _, record := range records {
			items = append(items, responseFromRecord(record))
		}
		return items, nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDuplicateEndpoint(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint", func(tx pgx.Tx) (endpointResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return endpointResponse{}, err
		}
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return endpointResponse{}, err
		}
		source, found, err := loadEndpointRecord(r.Context(), tx, profile.ID, endpointID, false)
		if err != nil {
			return endpointResponse{}, err
		}
		if !found {
			return endpointResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}
		existingNames, err := listEndpointNames(r.Context(), tx, profile.ID)
		if err != nil {
			return endpointResponse{}, err
		}
		position, err := nextEndpointPosition(r.Context(), tx, profile.ID)
		if err != nil {
			return endpointResponse{}, err
		}
		duplicate, err := insertEndpoint(r.Context(), tx, endpointRecord{ProfileID: profile.ID, Name: endpointdomain.BuildDuplicateEndpointName(source.Name, existingNames), BaseURL: source.BaseURL, APIKey: source.APIKey, Position: position, CreatedAt: s.nowUTC(), UpdatedAt: s.nowUTC()})
		if err != nil {
			return endpointResponse{}, err
		}
		return responseFromRecord(duplicate), nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Service) handleDeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint", func(tx pgx.Tx) (deletedResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return deletedResponse{}, err
		}
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return deletedResponse{}, err
		}
		record, found, err := loadEndpointRecord(r.Context(), tx, profile.ID, endpointID, true)
		if err != nil {
			return deletedResponse{}, err
		}
		if !found {
			return deletedResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}
		usageRows, err := listEndpointUsageRows(r.Context(), tx, profile.ID, endpointID)
		if err != nil {
			return deletedResponse{}, err
		}
		if len(usageRows) > 0 {
			connections := make([]map[string]any, 0, len(usageRows))
			for _, row := range usageRows {
				connections = append(connections, map[string]any{"connection_id": row.ConnectionID, "model_config_id": row.ModelConfigID, "model_id": row.ModelID, "name": row.Name})
			}
			return deletedResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: map[string]any{"message": "Cannot delete endpoint that is referenced by connections", "connections": connections}}
		}
		if err := deleteEndpoint(r.Context(), tx, endpointID); err != nil {
			return deletedResponse{}, err
		}
		remaining, err := listOrderedEndpoints(r.Context(), tx, profile.ID)
		if err != nil {
			return deletedResponse{}, err
		}
		if normalizeEndpointPositions(remaining, s.nowUTC()) {
			if err := persistEndpointPositions(r.Context(), tx, remaining, s.nowUTC()); err != nil {
				return deletedResponse{}, err
			}
		}
		_ = record
		return deletedResponse{Deleted: true}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func resolveEffectiveProfile(ctx context.Context, tx pgx.Tx, r *http.Request) (profiledomain.Profile, error) {
	return profiledomain.ResolveEffectiveProfile(ctx, tx, r.Header.Get(profiledomain.ProfileIDHeader))
}

func normalizeEndpointPositions(records []endpointRecord, currentTime time.Time) bool {
	changed := false
	for index := range records {
		if records[index].Position == index {
			continue
		}
		records[index].Position = index
		records[index].UpdatedAt = currentTime
		changed = true
	}
	return changed
}

func decodeJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	return json.NewDecoder(request.Body).Decode(target)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeDomainError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, err error) {
	var endpointErr *domainError
	if errors.As(err, &endpointErr) {
		writeError(w, r, allowedOrigins, endpointErr.StatusCode, endpointErr.Detail)
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

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
