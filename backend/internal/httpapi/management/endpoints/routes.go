package endpoints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

type validationFailureDetail struct {
	Code   string            `json:"code"`
	Fields map[string]string `json:"fields"`
}
type nameConflictDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field"`
}

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
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
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
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateEndpoint(w http.ResponseWriter, r *http.Request) {
	var requestBody endpointCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	endpointName := strings.TrimSpace(requestBody.Name)
	normalizedURL := endpointdomain.NormalizeBaseURL(requestBody.BaseURL)
	if failures := validateEndpointFields(endpointName, normalizedURL); len(failures.Fields) > 0 {
		writeValidationFailure(w, r, s.corsSnapshot(), failures)
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
		metadata, err := endpointdomain.BuildSecretMetadata(requestBody.APIKey, s.secretEncryptionKey, s.nowUTC)
		if err != nil {
			return endpointResponse{}, err
		}
		now := s.nowUTC()
		record, err := insertEndpoint(r.Context(), tx, endpointRecord{
			ProfileID:         profile.ID,
			Name:              endpointName,
			BaseURL:           normalizedURL,
			APIKey:            metadata.EncryptedValue,
			APIKeyFingerprint: metadata.Fingerprint,
			APIKeyUpdatedAt:   metadata.KeyUpdatedAt,
			ConfigRevision:    1,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		if err != nil {
			return endpointResponse{}, err
		}
		return responseFromRecord(record), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateEndpoint(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody endpointUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint", func(tx pgx.Tx) (endpointResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return endpointResponse{}, err
		}
		// Serialize name uniqueness checks with creates, duplicates, and other
		// endpoint updates in this profile. The row lock alone does not protect
		// two different endpoints from concurrently choosing the same name.
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return endpointResponse{}, err
		}
		record, found, err := loadEndpointRecord(r.Context(), tx, profile.ID, endpointID, true)
		if err != nil {
			return endpointResponse{}, err
		}
		if !found {
			return endpointResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}

		nameChanged := false
		urlChanged := false
		keyChanged := false
		now := s.nowUTC()

		if requestBody.Name.Set {
			endpointName := strings.TrimSpace(stringValue(requestBody.Name.Value))
			if code := endpointdomain.ValidateEndpointName(endpointName); code != "" {
				return endpointResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: validationFailureDetail{Code: "validation_failed", Fields: map[string]string{"name": code}}}
			}
			if endpointName != record.Name {
				if err := ensureUniqueEndpointName(r.Context(), tx, profile.ID, endpointName, intPtr(record.ID)); err != nil {
					return endpointResponse{}, err
				}
				record.Name = endpointName
				nameChanged = true
			}
		}
		if requestBody.BaseURL.Set {
			normalizedURL := endpointdomain.NormalizeBaseURL(stringValue(requestBody.BaseURL.Value))
			if codes := endpointdomain.ValidateBaseURL(normalizedURL); len(codes) > 0 {
				return endpointResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: validationFailureDetail{Code: "validation_failed", Fields: map[string]string{"base_url": codes[0]}}}
			}
			if normalizedURL != record.BaseURL {
				record.BaseURL = normalizedURL
				urlChanged = true
			}
		}
		if requestBody.APIKey.Set {
			incomingKey := strings.TrimSpace(stringValue(requestBody.APIKey.Value))
			if incomingKey != "" {
				if endpointdomain.HasAPIKey(record.APIKey) {
					existingPlaintext, decryptErr := endpointdomain.DecryptSecret(record.APIKey, s.secretEncryptionKey)
					if decryptErr != nil {
						return endpointResponse{}, decryptErr
					}
					if !endpointdomain.APIKeyIdentityMatches(s.secretEncryptionKey, existingPlaintext, incomingKey) {
						metadata, metadataErr := endpointdomain.BuildSecretMetadata(incomingKey, s.secretEncryptionKey, s.nowUTC)
						if metadataErr != nil {
							return endpointResponse{}, metadataErr
						}
						record.APIKey = metadata.EncryptedValue
						record.APIKeyFingerprint = metadata.Fingerprint
						record.APIKeyUpdatedAt = metadata.KeyUpdatedAt
						keyChanged = true
					}
				} else {
					metadata, metadataErr := endpointdomain.BuildSecretMetadata(incomingKey, s.secretEncryptionKey, s.nowUTC)
					if metadataErr != nil {
						return endpointResponse{}, metadataErr
					}
					record.APIKey = metadata.EncryptedValue
					record.APIKeyFingerprint = metadata.Fingerprint
					record.APIKeyUpdatedAt = metadata.KeyUpdatedAt
					keyChanged = true
				}
			}
		}

		if !nameChanged && !urlChanged && !keyChanged {
			// No-op update: preserve updated_at, key time, ciphertext and revision.
			return responseFromRecord(record), nil
		}
		if urlChanged || keyChanged {
			record.ConfigRevision++
		}
		record.UpdatedAt = now
		updated, err := updateEndpointRecord(r.Context(), tx, record)
		if err != nil {
			return endpointResponse{}, err
		}
		return responseFromRecord(updated), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleDuplicateEndpoint(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
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
		source, found, err := loadEndpointRecord(r.Context(), tx, profile.ID, endpointID, true)
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
		now := s.nowUTC()
		metadata := endpointdomain.SecretMetadata{EncryptedValue: source.APIKey}
		if endpointdomain.HasAPIKey(source.APIKey) {
			plaintext, decryptErr := endpointdomain.DecryptSecret(source.APIKey, s.secretEncryptionKey)
			if decryptErr != nil {
				return endpointResponse{}, decryptErr
			}
			// Duplicate copies the key plaintext semantics and display
			// fingerprint, re-encrypts for the new row, and stamps the new
			// Endpoint's creation time as its key time.
			metadata, err = endpointdomain.BuildSecretMetadata(plaintext, s.secretEncryptionKey, s.nowUTC)
			if err != nil {
				return endpointResponse{}, err
			}
		}
		duplicate, err := insertEndpoint(r.Context(), tx, endpointRecord{
			ProfileID:         profile.ID,
			Name:              endpointdomain.BuildDuplicateEndpointName(source.Name, existingNames),
			BaseURL:           source.BaseURL,
			APIKey:            metadata.EncryptedValue,
			APIKeyFingerprint: metadata.Fingerprint,
			APIKeyUpdatedAt:   metadata.KeyUpdatedAt,
			ConfigRevision:    1,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		if err != nil {
			return endpointResponse{}, err
		}
		return responseFromRecord(duplicate), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusCreated, response)
}

func validateEndpointFields(endpointName string, normalizedURL string) validationFailureDetail {
	detail := validationFailureDetail{Code: "validation_failed", Fields: map[string]string{}}
	if code := endpointdomain.ValidateEndpointName(endpointName); code != "" {
		detail.Fields["name"] = code
	}
	if codes := endpointdomain.ValidateBaseURL(normalizedURL); len(codes) > 0 {
		detail.Fields["base_url"] = codes[0]
	}
	return detail
}

func writeValidationFailure(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, detail validationFailureDetail) {
	responseutil.WriteError(w, r, corsSnapshot, http.StatusUnprocessableEntity, detail)
}

func resolveEffectiveProfile(ctx context.Context, tx pgx.Tx, r *http.Request) (profiledomain.Profile, error) {
	return profiledomain.ResolveEffectiveProfile(ctx, tx, r.Header.Get(profiledomain.ProfileIDHeader))
}

func decodeJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	return json.NewDecoder(request.Body).Decode(target)
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	if endpointErr, ok := errors.AsType[*domainError](err); ok {
		responseutil.WriteError(w, r, corsSnapshot, endpointErr.StatusCode, endpointErr.Detail)
		return
	}
	if profileErr, ok := errors.AsType[*profiledomain.HTTPError](err); ok {
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

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intPtr(value int) *int {
	resolved := value
	return &resolved
}
