package vendors

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/vendordomain"
)

func (s *Service) handleListVendors(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "vendor", func(tx pgx.Tx) ([]vendorResponse, error) {
		items, err := listVendors(r.Context(), tx)
		if err != nil {
			return nil, err
		}
		response := make([]vendorResponse, 0, len(items))
		for _, item := range items {
			response = append(response, vendorResponseFromRecord(item))
		}
		return response, nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateVendor(w http.ResponseWriter, r *http.Request) {
	var requestBody vendorCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	normalizeCreateRequest(&requestBody)
	if vendordomain.IsReadonlyVendorKey(requestBody.Key) {
		writeReadonlyVendorError(w, r, s.allowedOrigins, requestBody.Key)
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "vendor", func(tx pgx.Tx) (vendorResponse, error) {
		if err := ensureVendorUniqueness(r.Context(), tx, stringPtr(requestBody.Key), stringPtr(requestBody.Name), nil); err != nil {
			return vendorResponse{}, err
		}
		record, err := insertVendor(r.Context(), tx, requestBody, s.nowUTC())
		if err != nil {
			return vendorResponse{}, err
		}
		return vendorResponseFromRecord(record), nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Service) handleGetVendor(w http.ResponseWriter, r *http.Request) {
	vendorID, err := routeInt(r, "vendor_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "vendor", func(tx pgx.Tx) (vendorResponse, error) {
		record, found, err := loadVendor(r.Context(), tx, vendorID, false)
		if err != nil {
			return vendorResponse{}, err
		}
		if !found {
			return vendorResponse{}, vendorNotFoundError()
		}
		return vendorResponseFromRecord(record), nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleListVendorModels(w http.ResponseWriter, r *http.Request) {
	vendorID, err := routeInt(r, "vendor_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "vendor", func(tx pgx.Tx) ([]vendorModelUsageItem, error) {
		if _, found, err := loadVendor(r.Context(), tx, vendorID, false); err != nil {
			return nil, err
		} else if !found {
			return nil, vendorNotFoundError()
		}
		return listVendorModelUsage(r.Context(), tx, vendorID)
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleUpdateVendor(w http.ResponseWriter, r *http.Request) {
	vendorID, err := routeInt(r, "vendor_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	var requestBody vendorUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	normalizeUpdateRequest(&requestBody)

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "vendor", func(tx pgx.Tx) (vendorResponse, error) {
		record, found, err := loadVendor(r.Context(), tx, vendorID, true)
		if err != nil {
			return vendorResponse{}, err
		}
		if !found {
			return vendorResponse{}, vendorNotFoundError()
		}
		if requestBody.Key.Set && requestBody.Key.Value != nil && vendordomain.IsReadonlyVendorKey(*requestBody.Key.Value) && *requestBody.Key.Value != record.Key {
			return vendorResponse{}, readonlyVendorError(*requestBody.Key.Value)
		}
		if vendordomain.IsReadonlyVendorKey(record.Key) && hasIdentityUpdates(requestBody) {
			return vendorResponse{}, readonlyVendorError(record.Key)
		}
		if err := ensureVendorUniqueness(r.Context(), tx, normalizedUniqueValue(requestBody.Key), normalizedUniqueValue(requestBody.Name), &record.ID); err != nil {
			return vendorResponse{}, err
		}

		nextKey := any(record.Key)
		nextName := any(record.Name)
		nextDescription := nullableString(record.Description)
		nextIconKey := nullableString(record.IconKey)
		nextAuditEnabled := record.AuditEnabled
		nextAuditCaptureBodies := record.AuditCaptureBodies

		if requestBody.Key.Set {
			nextKey = nullableString(requestBody.Key.Value)
		}
		if requestBody.Name.Set {
			nextName = nullableString(requestBody.Name.Value)
		}
		if requestBody.Description.Set {
			nextDescription = nullableString(requestBody.Description.Value)
		}
		if requestBody.IconKey.Set {
			nextIconKey = nullableString(requestBody.IconKey.Value)
		}
		if requestBody.AuditEnabled.Set {
			nextAuditEnabled = requestBody.AuditEnabled.Value
		}
		if requestBody.AuditCaptureBodies.Set {
			nextAuditCaptureBodies = requestBody.AuditCaptureBodies.Value
		}

		updatedRecord, err := updateVendor(r.Context(), tx, record.ID, nextKey, nextName, nextDescription, nextIconKey, nextAuditEnabled, nextAuditCaptureBodies, s.nowUTC())
		if err != nil {
			return vendorResponse{}, err
		}
		return vendorResponseFromRecord(updatedRecord), nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteVendor(w http.ResponseWriter, r *http.Request) {
	vendorID, err := routeInt(r, "vendor_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := pgxutil.InTxValue(r.Context(), s.pool, "vendor", func(tx pgx.Tx) (struct{}, error) {
		record, found, err := loadVendor(r.Context(), tx, vendorID, true)
		if err != nil {
			return struct{}{}, err
		}
		if !found {
			return struct{}{}, vendorNotFoundError()
		}
		if vendordomain.IsReadonlyVendorKey(record.Key) {
			return struct{}{}, readonlyVendorError(record.Key)
		}
		if err := deleteVendor(r.Context(), tx, vendorID); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, nil
	}); err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func normalizeCreateRequest(requestBody *vendorCreateRequest) {
	requestBody.Key = strings.ToLower(strings.TrimSpace(requestBody.Key))
	requestBody.Name = strings.TrimSpace(requestBody.Name)
	requestBody.Description = normalizeOptionalString(requestBody.Description, false, true)
	requestBody.IconKey = normalizeOptionalString(requestBody.IconKey, true, true)
}

func normalizeUpdateRequest(requestBody *vendorUpdateRequest) {
	requestBody.Key = optionalString{Set: requestBody.Key.Set, Value: normalizeOptionalString(requestBody.Key.Value, true, false)}
	requestBody.Name = optionalString{Set: requestBody.Name.Set, Value: normalizeOptionalString(requestBody.Name.Value, false, false)}
	requestBody.Description = optionalString{Set: requestBody.Description.Set, Value: normalizeOptionalString(requestBody.Description.Value, false, true)}
	requestBody.IconKey = optionalString{Set: requestBody.IconKey.Set, Value: normalizeOptionalString(requestBody.IconKey.Value, true, true)}
}

func normalizeOptionalString(value *string, lower bool, emptyToNil bool) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if lower {
		normalized = strings.ToLower(normalized)
	}
	if emptyToNil && normalized == "" {
		return nil
	}
	return &normalized
}

func normalizedUniqueValue(value optionalString) *string {
	if !value.Set {
		return nil
	}
	return value.Value
}

func hasIdentityUpdates(requestBody vendorUpdateRequest) bool {
	return requestBody.Key.Set || requestBody.Name.Set || requestBody.Description.Set || requestBody.IconKey.Set
}

func vendorNotFoundError() error {
	return &domainError{StatusCode: http.StatusNotFound, Detail: "Vendor not found"}
}

func readonlyVendorError(vendorKey string) error {
	label := strings.TrimSpace(vendorKey)
	if label == "" {
		label = "system vendor"
	}
	return &domainError{StatusCode: http.StatusForbidden, Detail: fmt.Sprintf("Vendor '%s' is readonly and cannot be modified here", label)}
}

func writeReadonlyVendorError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, vendorKey string) {
	writeError(w, r, allowedOrigins, http.StatusForbidden, readonlyVendorError(vendorKey).Error())
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
	var vendorErr *domainError
	if errors.As(err, &vendorErr) {
		writeError(w, r, allowedOrigins, vendorErr.StatusCode, vendorErr.Detail)
		return
	}
	writeError(w, r, allowedOrigins, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, statusCode int, detail string) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		if _, ok := allowedOrigins[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
	}
	writeJSON(w, statusCode, map[string]string{"detail": detail})
}

func routeInt(request *http.Request, name string) (int, error) {
	value := strings.TrimSpace(chi.URLParam(request, name))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func stringPtr(value string) *string {
	result := value
	return &result
}
