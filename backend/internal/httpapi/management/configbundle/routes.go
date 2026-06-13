package configbundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/coachpo/prism/backend/internal/profiledomain"
)

func (s *Service) handleExportProfileBundle(w http.ResponseWriter, r *http.Request) {
	s.exportProfileBundle(w, r, false)
}

func (s *Service) handleExportProfileBundleWithSecrets(w http.ResponseWriter, r *http.Request) {
	if err := requireDangerousProfileExportConfirm(r); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	s.exportProfileBundle(w, r, true)
}

func (s *Service) exportProfileBundle(w http.ResponseWriter, r *http.Request, includeSecrets bool) {
	exportTime := s.nowUTC()
	keyID, err := s.resolvedBundleSecretKeyID()
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	bundle, err := pgxutil.InTxValue(r.Context(), s.pool, "config bundle", func(tx pgx.Tx) (profileBundleResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return profileBundleResponse{}, err
		}
		return s.buildProfileBundle(r.Context(), tx, profile.ID, exportTime, keyID, includeSecrets)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	writeDownloadJSON(w, http.StatusOK, bundle, profileExportFilename(exportTime))
}

func (s *Service) handlePreviewProfileImport(w http.ResponseWriter, r *http.Request) {
	var requestBody profileImportRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	if err := validateProfileBundleEnvelope(requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if err := validateProfileImportRequest(requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	bundleFingerprint, err := profileImportBundleFingerprint(requestBody)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config bundle", func(tx pgx.Tx) (profileImportPreviewResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return profileImportPreviewResponse{}, err
		}
		previewToken, err := s.issueProfilePreviewToken(profile.ID, bundleFingerprint)
		if err != nil {
			return profileImportPreviewResponse{}, err
		}
		preview, previewErr := s.previewProfileImport(r.Context(), tx, profile.ID, requestBody)
		if previewErr != nil {
			preview = buildProfilePreviewErrorResponse(requestBody, previewErrorDetail(previewErr))
		}
		preview.PreviewToken = previewToken
		preview.BundleFingerprint = bundleFingerprint
		return preview, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleImportProfileBundle(w http.ResponseWriter, r *http.Request) {
	var requestBody profileImportRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	if err := validateProfileBundleEnvelope(requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if err := validateProfileImportRequest(requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	previewToken, err := requirePreviewTokenHeader(r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config bundle", func(tx pgx.Tx) (profileImportResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return profileImportResponse{}, err
		}
		if err := s.validateProfilePreviewToken(previewToken, profile.ID, requestBody); err != nil {
			return profileImportResponse{}, err
		}
		return s.executeProfileImport(r.Context(), tx, profile.ID, requestBody)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func profileExportFilename(exportTime time.Time) string {
	return fmt.Sprintf("prism-profile-config-v%d-%s.json", canonicalProfileBundleVersion, exportTime.UTC().Format("2006-01-02"))
}

func buildProfilePreviewErrorResponse(data profileImportRequest, detail string) profileImportPreviewResponse {
	return buildProfilePreviewResponse(data, []string{}, []string{detail})
}

func previewErrorDetail(err error) string {
	if bundleErr, ok := errors.AsType[*domainError](err); ok {
		return bundleErr.Detail
	}
	if profileErr, ok := errors.AsType[*profiledomain.HTTPError](err); ok {
		return profileErr.Detail
	}
	return "Internal server error"
}

func decodeJSONBody(request *http.Request, target any) error {
	defer func() {
		_ = request.Body.Close()
	}()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeDownloadJSON(w http.ResponseWriter, statusCode int, payload any, filename string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	if bundleErr, ok := errors.AsType[*domainError](err); ok {
		writeErrorFields(w, r, corsSnapshot, bundleErr.StatusCode, bundleErr.Detail, bundleErr.Fields)
		return
	}
	if profileErr, ok := errors.AsType[*profiledomain.HTTPError](err); ok {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail string) {
	writeErrorFields(w, r, corsSnapshot, statusCode, detail, nil)
}

func writeErrorFields(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail string, fields map[string]any) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
	if len(fields) == 0 {
		writeJSON(w, statusCode, map[string]string{"detail": detail})
		return
	}
	payload := map[string]any{"detail": detail}
	maps.Copy(payload, fields)
	writeJSON(w, statusCode, payload)
}
