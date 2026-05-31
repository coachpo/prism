package configbundle

import (
	"encoding/json"
	"errors"
	"fmt"
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

func (s *Service) handleExportVendorCatalog(w http.ResponseWriter, r *http.Request) {
	exportTime := s.nowUTC()
	bundle, err := pgxutil.InTxValue(r.Context(), s.pool, "config bundle", func(tx pgx.Tx) (vendorCatalogResponse, error) {
		return buildVendorCatalog(r.Context(), tx, exportTime)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	writeDownloadJSON(w, http.StatusOK, bundle, vendorExportFilename(exportTime))
}

func (s *Service) handlePreviewVendorCatalogImport(w http.ResponseWriter, r *http.Request) {
	var requestBody vendorCatalogImportRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := validateVendorCatalogBundleEnvelope(requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	bundleFingerprint, err := vendorCatalogImportBundleFingerprint(requestBody)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	previewToken, err := s.issueVendorPreviewToken(bundleFingerprint)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config bundle", func(tx pgx.Tx) (vendorCatalogImportPreviewResponse, error) {
		preview, err := s.previewVendorCatalogImport(r.Context(), tx, requestBody)
		if err != nil {
			return vendorCatalogImportPreviewResponse{}, err
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

func (s *Service) handleImportVendorCatalog(w http.ResponseWriter, r *http.Request) {
	var requestBody vendorCatalogImportRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := validateVendorCatalogBundleEnvelope(requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	previewToken, err := requirePreviewTokenHeader(r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if err := s.validateVendorPreviewToken(previewToken, requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config bundle", func(tx pgx.Tx) (vendorCatalogImportResponse, error) {
		return s.importVendorCatalog(r.Context(), tx, requestBody)
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

func vendorExportFilename(exportTime time.Time) string {
	return fmt.Sprintf("prism-vendor-catalog-v%d-%s.json", canonicalVendorCatalogVersion, exportTime.UTC().Format("2006-01-02"))
}

func buildProfilePreviewErrorResponse(data profileImportRequest, detail string) profileImportPreviewResponse {
	return buildProfilePreviewResponse(data, []profileImportVendorResolution{}, []string{}, []string{detail}, []string{})
}

func previewErrorDetail(err error) string {
	var bundleErr *domainError

	if errors.As(err, &bundleErr) {
		return bundleErr.Detail
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
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
	var bundleErr *domainError
	if errors.As(err, &bundleErr) {
		writeError(w, r, corsSnapshot, bundleErr.StatusCode, bundleErr.Detail)

		return
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail string) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
	writeJSON(w, statusCode, map[string]string{"detail": detail})
}
