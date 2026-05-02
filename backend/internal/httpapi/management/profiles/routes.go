package profiles

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func (s *Service) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "profile", func(tx pgx.Tx) ([]profileResponse, error) {
		if _, err := profiledomain.ResolveActiveProfile(r.Context(), tx, s.nowUTC); err != nil {
			return nil, err
		}
		profiles, err := profiledomain.ListNonDeletedProfiles(r.Context(), tx)
		if err != nil {
			return nil, err
		}
		result := make([]profileResponse, 0, len(profiles))
		for _, profile := range profiles {
			result = append(result, profileResponseFromDomain(profile))
		}
		return result, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetActiveProfile(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "profile", func(tx pgx.Tx) (profileResponse, error) {
		profile, err := profiledomain.ResolveActiveProfile(r.Context(), tx, s.nowUTC)
		if err != nil {
			return profileResponse{}, err
		}
		return profileResponseFromDomain(profile), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetBootstrap(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "profile", func(tx pgx.Tx) (profileBootstrapResponse, error) {
		activeProfile, err := profiledomain.ResolveActiveProfile(r.Context(), tx, s.nowUTC)
		if err != nil {
			return profileBootstrapResponse{}, err
		}
		profiles, err := profiledomain.ListNonDeletedProfiles(r.Context(), tx)
		if err != nil {
			return profileBootstrapResponse{}, err
		}
		items := make([]profileResponse, 0, len(profiles))
		for _, profile := range profiles {
			items = append(items, profileResponseFromDomain(profile))
		}
		activeResponse := profileResponseFromDomain(activeProfile)
		return profileBootstrapResponse{
			Profiles:      items,
			ActiveProfile: &activeResponse,
			ProfileLimits: profileLimitsResponse{MaxProfiles: profiledomain.MaxNonDeletedProfiles},
		}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	var requestBody profileCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "profile", func(tx pgx.Tx) (profileResponse, error) {
		count, err := profiledomain.CountNonDeletedProfiles(r.Context(), tx)
		if err != nil {
			return profileResponse{}, err
		}
		if count >= profiledomain.MaxNonDeletedProfiles {
			return profileResponse{}, &domainError{
				StatusCode: http.StatusConflict,
				Detail: fmt.Sprintf(
					"Maximum %d profiles reached. Delete a profile to create a new one.",
					profiledomain.MaxNonDeletedProfiles,
				),
			}
		}
		if err := profiledomain.EnsureProfileNameAvailable(r.Context(), tx, requestBody.Name, nil); err != nil {
			return profileResponse{}, err
		}
		if _, err := profiledomain.ResolveActiveProfile(r.Context(), tx, s.nowUTC); err != nil {
			return profileResponse{}, err
		}

		now := s.nowUTC()
		createdProfile, err := insertProfile(r.Context(), tx, requestBody, now)
		if err != nil {
			return profileResponse{}, err
		}
		if err := insertDefaultUserSettings(r.Context(), tx, createdProfile.ID, now); err != nil {
			return profileResponse{}, err
		}
		return profileResponseFromDomain(createdProfile), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	profileID, err := routeInt(r, "profile_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody profileUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "profile", func(tx pgx.Tx) (profileResponse, error) {
		profile, found, err := profiledomain.LoadNonDeletedProfileForUpdate(r.Context(), tx, profileID)
		if err != nil {
			return profileResponse{}, err
		}
		if !found {
			return profileResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Profile not found"}
		}
		if profile.IsDefault && !profile.IsEditable {
			return profileResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Default profile is locked and cannot be modified."}
		}
		if profile.IsDefault && requestBody.Name.Set && requestBody.Name.Value != nil && *requestBody.Name.Value != profiledomain.DefaultProfileName {
			return profileResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Default profile name is immutable."}
		}
		if requestBody.Name.Set && requestBody.Name.Value != nil && *requestBody.Name.Value != profile.Name {
			if err := profiledomain.EnsureProfileNameAvailable(r.Context(), tx, *requestBody.Name.Value, &profile.ID); err != nil {
				return profileResponse{}, err
			}
		}
		if requestBody.Name.Set && requestBody.Name.Value != nil && *requestBody.Name.Value == profiledomain.DefaultProfileName && !profile.IsDefault {
			return profileResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("Profile with name '%s' already exists", profiledomain.DefaultProfileName)}
		}

		if requestBody.Name.Set && requestBody.Name.Value != nil {
			profile.Name = *requestBody.Name.Value
		}
		if requestBody.Description.Set {
			profile.Description = requestBody.Description.Value
		}
		if profile.IsDefault {
			profile.Name = profiledomain.DefaultProfileName
		}
		profile.UpdatedAt = s.nowUTC()
		updatedProfile, err := updateProfileRow(r.Context(), tx, profile)
		if err != nil {
			return profileResponse{}, err
		}
		return profileResponseFromDomain(updatedProfile), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleActivateProfile(w http.ResponseWriter, r *http.Request) {
	profileID, err := routeInt(r, "profile_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody profileActivateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "profile", func(tx pgx.Tx) (profileResponse, error) {
		if _, err := profiledomain.ResolveActiveProfile(r.Context(), tx, s.nowUTC); err != nil {
			return profileResponse{}, err
		}
		currentActive, found, err := profiledomain.LoadActiveProfileForUpdate(r.Context(), tx)
		if err != nil {
			return profileResponse{}, err
		}
		if !found {
			return profileResponse{}, fmt.Errorf("active profile missing after invariant enforcement")
		}
		if currentActive.ID != requestBody.ExpectedActiveProfileID {
			return profileResponse{}, &domainError{
				StatusCode: http.StatusConflict,
				Detail: fmt.Sprintf(
					"Active profile mismatch: expected %d, got %d",
					requestBody.ExpectedActiveProfileID,
					currentActive.ID,
				),
			}
		}
		if profileID == currentActive.ID {
			return profileResponseFromDomain(currentActive), nil
		}

		targetProfile, found, err := profiledomain.LoadNonDeletedProfileForUpdate(r.Context(), tx, profileID)
		if err != nil {
			return profileResponse{}, err
		}
		if !found {
			return profileResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Profile not found"}
		}

		now := s.nowUTC()
		currentActive.IsActive = false
		currentActive.Version++
		currentActive.UpdatedAt = now
		if _, err := updateProfileActiveState(r.Context(), tx, currentActive); err != nil {
			return profileResponse{}, err
		}
		targetProfile.IsActive = true
		targetProfile.Version++
		targetProfile.UpdatedAt = now
		if _, err := updateProfileActiveState(r.Context(), tx, targetProfile); err != nil {
			if isUniqueViolation(err, "uq_profiles_single_active") {
				return profileResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "Profile activation conflict. Please retry."}
			}
			return profileResponse{}, err
		}
		return profileResponseFromDomain(targetProfile), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	profileID, err := routeInt(r, "profile_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "profile", func(tx pgx.Tx) (deletedResponse, error) {
		profile, found, err := profiledomain.LoadNonDeletedProfileForUpdate(r.Context(), tx, profileID)
		if err != nil {
			return deletedResponse{}, err
		}
		if !found {
			return deletedResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Profile not found"}
		}
		if profile.IsDefault {
			return deletedResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Default profile cannot be deleted."}
		}
		if profile.IsActive {
			return deletedResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Cannot delete active profile. Activate another profile first."}
		}
		if err := softDeleteProfile(r.Context(), tx, profile.ID, s.nowUTC()); err != nil {
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

func (s *Service) handleRuntimeProbe(w http.ResponseWriter, r *http.Request) {
	modelID, err := runtimeModelID(r)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	resolution, err := pgxutil.InTxValue(r.Context(), s.pool, "profile", func(tx pgx.Tx) (bool, error) {
		activeProfile, err := profiledomain.ResolveActiveProfile(r.Context(), tx, s.nowUTC)
		if err != nil {
			return false, err
		}
		return profiledomain.ModelExists(r.Context(), tx, activeProfile.ID, modelID)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if !resolution {
		writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "Model not found in active profile")
		return
	}
	writeError(w, r, s.corsSnapshot(), http.StatusNotImplemented, "Runtime proxy not implemented in S6")
}

func insertProfile(ctx context.Context, tx pgx.Tx, requestBody profileCreateRequest, now time.Time) (profiledomain.Profile, error) {
	profile, err := scanDomainProfile(tx.QueryRow(
		ctx,
		`INSERT INTO profiles (
			name,
			description,
			is_active,
			is_default,
			is_editable,
			version,
			deleted_at,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at`,
		requestBody.Name,
		nullableString(requestBody.Description),
		false,
		false,
		true,
		0,
		nil,
		now,
		now,
	))
	if err != nil {
		if isUniqueViolation(err, "profiles_name_key") {
			return profiledomain.Profile{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("Profile with name '%s' already exists", requestBody.Name)}
		}
		return profiledomain.Profile{}, fmt.Errorf("insert profile %q: %w", requestBody.Name, err)
	}
	return profile, nil
}

func insertDefaultUserSettings(ctx context.Context, tx pgx.Tx, profileID int, now time.Time) error {
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO user_settings (
			profile_id,
			report_currency_code,
			report_currency_symbol,
			timezone_preference,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6)`,
		profileID,
		"USD",
		"$",
		nil,
		now,
		now,
	); err != nil {
		return fmt.Errorf("insert default user settings for profile %d: %w", profileID, err)
	}
	return nil
}

func updateProfileRow(ctx context.Context, tx pgx.Tx, profile profiledomain.Profile) (profiledomain.Profile, error) {
	updatedProfile, err := scanDomainProfile(tx.QueryRow(
		ctx,
		`UPDATE profiles
		SET name = $2, description = $3, updated_at = $4
		WHERE id = $1
		RETURNING id, name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at`,
		profile.ID,
		profile.Name,
		nullableString(profile.Description),
		profile.UpdatedAt,
	))
	if err != nil {
		if isUniqueViolation(err, "profiles_name_key") {
			return profiledomain.Profile{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("Profile with name '%s' already exists", profile.Name)}
		}
		return profiledomain.Profile{}, fmt.Errorf("update profile %d: %w", profile.ID, err)
	}
	return updatedProfile, nil
}

func updateProfileActiveState(ctx context.Context, tx pgx.Tx, profile profiledomain.Profile) (profiledomain.Profile, error) {
	updatedProfile, err := scanDomainProfile(tx.QueryRow(
		ctx,
		`UPDATE profiles
		SET is_active = $2, version = $3, updated_at = $4
		WHERE id = $1
		RETURNING id, name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at`,
		profile.ID,
		profile.IsActive,
		profile.Version,
		profile.UpdatedAt,
	))
	if err != nil {
		return profiledomain.Profile{}, fmt.Errorf("update active state for profile %d: %w", profile.ID, err)
	}
	return updatedProfile, nil
}

func softDeleteProfile(ctx context.Context, tx pgx.Tx, profileID int, now time.Time) error {
	if _, err := tx.Exec(
		ctx,
		`UPDATE profiles SET deleted_at = $2, updated_at = $2 WHERE id = $1`,
		profileID,
		now,
	); err != nil {
		return fmt.Errorf("soft-delete profile %d: %w", profileID, err)
	}
	return nil
}

func scanDomainProfile(scanner interface{ Scan(...any) error }) (profiledomain.Profile, error) {
	var description sql.NullString
	var deletedAt sql.NullTime
	profile := profiledomain.Profile{}
	if err := scanner.Scan(
		&profile.ID,
		&profile.Name,
		&description,
		&profile.IsActive,
		&profile.IsDefault,
		&profile.IsEditable,
		&profile.Version,
		&deletedAt,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	); err != nil {
		return profiledomain.Profile{}, err
	}
	if description.Valid {
		value := description.String
		profile.Description = &value
	}
	if deletedAt.Valid {
		value := deletedAt.Time.UTC()
		profile.DeletedAt = &value
	}
	return profile, nil
}

func runtimeModelID(request *http.Request) (string, error) {
	path := strings.TrimSpace(request.URL.Path)
	if strings.HasPrefix(path, "/v1beta/models/") {
		trimmed := strings.TrimPrefix(path, "/v1beta/models/")
		if index := strings.IndexAny(trimmed, ":/"); index >= 0 {
			trimmed = trimmed[:index]
		}
		if strings.TrimSpace(trimmed) == "" {
			return "", errors.New("model is required")
		}
		return trimmed, nil
	}
	defer func() { _ = request.Body.Close() }()
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		return "", errors.New("invalid request body")
	}
	if strings.TrimSpace(payload.Model) == "" {
		return "", errors.New("model is required")
	}
	return strings.TrimSpace(payload.Model), nil
}

func decodeJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	decoder := json.NewDecoder(request.Body)
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var profileErr *domainError
	if errors.As(err, &profileErr) {
		writeError(w, r, corsSnapshot, profileErr.StatusCode, profileErr.Detail)
		return
	}
	var httpErr *profiledomain.HTTPError
	if errors.As(err, &httpErr) {
		writeError(w, r, corsSnapshot, httpErr.StatusCode, httpErr.Detail)
		return
	}
	writeError(w, r, corsSnapshot, http.StatusInternalServerError, "internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail string) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
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
