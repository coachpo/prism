package configbundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/profiledomain"
)

func (s *Service) handleExportProfileBundle(w http.ResponseWriter, r *http.Request) {
	exportTime := s.nowUTC()
	keyID, err := s.resolvedBundleSecretKeyID()
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}

	bundle, err := pgxutil.InTxValue(r.Context(), s.pool, "config bundle", func(tx pgx.Tx) (profileBundleResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return profileBundleResponse{}, err
		}
		return s.buildProfileBundle(r.Context(), tx, profile.ID, exportTime, keyID)
	})

	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}

	writeDownloadJSON(w, http.StatusOK, bundle, profileExportFilename(exportTime))
}

func (s *Service) handlePreviewProfileImport(w http.ResponseWriter, r *http.Request) {
	var requestBody profileImportRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := validateProfileBundleEnvelope(requestBody); err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config bundle", func(tx pgx.Tx) (profileImportPreviewResponse, error) {
		preview, previewErr := s.previewProfileImport(r.Context(), tx, requestBody)
		if previewErr != nil {
			return buildProfilePreviewErrorResponse(requestBody, previewErrorDetail(previewErr)), nil
		}
		return preview, nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleImportProfileBundle(w http.ResponseWriter, r *http.Request) {
	var requestBody profileImportRequest

	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config bundle", func(tx pgx.Tx) (profileImportResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return profileImportResponse{}, err
		}
		return s.executeProfileImport(r.Context(), tx, profile.ID, requestBody)
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}

	writeDownloadJSON(w, http.StatusOK, bundle, vendorExportFilename(exportTime))
}

func (s *Service) handlePreviewVendorCatalogImport(w http.ResponseWriter, r *http.Request) {
	var requestBody vendorCatalogImportRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := validateVendorCatalogBundleEnvelope(requestBody); err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config bundle", func(tx pgx.Tx) (vendorCatalogImportPreviewResponse, error) {
		return s.previewVendorCatalogImport(r.Context(), tx, requestBody)
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleImportVendorCatalog(w http.ResponseWriter, r *http.Request) {
	var requestBody vendorCatalogImportRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "config bundle", func(tx pgx.Tx) (vendorCatalogImportResponse, error) {
		return s.importVendorCatalog(r.Context(), tx, requestBody)
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func profileExportFilename(exportTime time.Time) string {
	return fmt.Sprintf("prism-profile-config-v%d-%s.json", canonicalBundleVersion, exportTime.UTC().Format("2006-01-02"))
}

func vendorExportFilename(exportTime time.Time) string {
	return fmt.Sprintf("prism-vendor-catalog-v%d-%s.json", canonicalBundleVersion, exportTime.UTC().Format("2006-01-02"))
}

func buildProfilePreviewErrorResponse(data profileImportRequest, detail string) profileImportPreviewResponse {
	return profileImportPreviewResponse{
		Ready:                    false,
		Version:                  canonicalBundleVersion,
		BundleKind:               canonicalProfileBundleKind,
		EndpointsImported:        len(data.Endpoints),
		PricingTemplatesImported: len(data.PricingTemplates),
		StrategiesImported:       len(data.LoadbalanceStrategies),
		ModelsImported:           len(data.Models),
		ConnectionsImported:      importedConnectionCount(data.Models),
		VendorResolutions:        []profileImportVendorResolution{},
		SecretKeyID:              data.SecretPayload.KeyID,
		DecryptableSecretRefs:    []string{},
		BlockingErrors:           []string{detail},
		Warnings:                 []string{},
	}
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
	return json.NewDecoder(request.Body).Decode(target)
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

func writeDomainError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, err error) {
	var bundleErr *domainError
	if errors.As(err, &bundleErr) {
		writeError(w, r, allowedOrigins, bundleErr.StatusCode, bundleErr.Detail)

		return
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, allowedOrigins, profileErr)
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
