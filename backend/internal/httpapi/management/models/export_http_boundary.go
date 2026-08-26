package models

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

// exportStaleCode is the stable drift code returned with HTTP 409 when the
// render request's digest no longer matches current source facts.
const exportStaleCode = "export_source_stale"

type exportRenderRequestInput struct {
	Request    *exportRenderRequest
	BaseURL    string
	ProviderID string
	APIKey     string
}

func (s *Service) prepareExportRenderRequest(w http.ResponseWriter, r *http.Request, platform modelexport.Platform) (*exportRenderRequestInput, bool) {
	request, ok := decodeExportRenderRequest(w, r, s.corsSnapshot())
	if !ok {
		return nil, false
	}
	if len(request.ModelConfigIDs) == 0 {
		writeExportFieldError(w, r, s.corsSnapshot(), "export_selection_required", "model_config_ids must not be empty")
		return nil, false
	}
	baseURL, err := normalizeExportBaseURL(request.BaseURL)
	if err != nil {
		writeExportFieldError(w, r, s.corsSnapshot(), "export_invalid_base_url", "base_url must be an HTTP(S) origin without path, user info, query, or fragment")
		return nil, false
	}
	providerID := strings.TrimSpace(request.ProviderID)
	if providerID == "" {
		providerID = modelexport.PiProviderID
	}
	if strings.Contains(providerID, "/") {
		writeExportFieldError(w, r, s.corsSnapshot(), "export_invalid_provider_id", "provider_id must not contain '/'")
		return nil, false
	}
	if platform == modelexport.PlatformPi && request.DefaultModelConfigID != nil {
		writeExportFieldError(w, r, s.corsSnapshot(), "export_default_model_invalid", "default_model_config_id is supported only for OpenCode")
		return nil, false
	}
	apiKey := ""
	if request.Credential.Include {
		apiKey = strings.TrimSpace(request.Credential.APIKey)
	} else if strings.TrimSpace(request.Credential.APIKey) != "" {
		writeExportFieldError(w, r, s.corsSnapshot(), "export_credential_invalid", "credential.api_key requires credential.include=true")
		return nil, false
	}
	for _, enhancementWire := range request.Enhancements {
		if err := enhancementWire.decode().ValidateForPlatform(platform); err != nil {
			s.writeExportDomainError(w, r, err)
			return nil, false
		}
	}
	return &exportRenderRequestInput{
		Request:    request,
		BaseURL:    baseURL,
		ProviderID: providerID,
		APIKey:     apiKey,
	}, true
}

func parseExportPlatform(w http.ResponseWriter, r *http.Request, cors platformcors.Snapshot) (modelexport.Platform, bool) {
	parsed, err := modelexport.ParsePlatform(chi.URLParam(r, "platform"))
	if err != nil {
		responseutil.WriteError(w, r, cors, http.StatusNotFound, "unsupported export platform")
		return "", false
	}
	return parsed, true
}

// decodeExportRenderRequest decodes the strict render body once, up front, so
// malformed payloads never enter a database transaction.
func decodeExportRenderRequest(w http.ResponseWriter, r *http.Request, cors platformcors.Snapshot) (*exportRenderRequest, bool) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	decoder.DisallowUnknownFields()
	var request exportRenderRequest
	if err := decoder.Decode(&request); err != nil {
		responseutil.WriteError(w, r, cors, http.StatusBadRequest, responseutil.SanitizeDecodeError(err))
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		responseutil.WriteError(w, r, cors, http.StatusBadRequest, "request body must contain exactly one JSON object")
		return nil, false
	}
	request.ExpectedSourceDigest = strings.TrimSpace(request.ExpectedSourceDigest)
	if request.ExpectedSourceDigest == "" {
		responseutil.WriteErrorFields(w, r, cors, http.StatusUnprocessableEntity, "expected_source_digest is required", map[string]any{"code": "export_digest_required"})
		return nil, false
	}
	return &request, true
}

func normalizeExportBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(trimmed, "#") || parsed.Opaque != "" {
		return "", errors.New("invalid Prism gateway origin")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("Prism gateway origin must not include a path")
	}
	parsed.Path, parsed.RawPath = "", ""
	parsed.ForceQuery = false
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func writeExportFieldError(w http.ResponseWriter, r *http.Request, cors platformcors.Snapshot, code string, message string) {
	responseutil.SetPrivateNoStoreHeaders(w)
	responseutil.WriteErrorFields(w, r, cors, http.StatusUnprocessableEntity, message, map[string]any{"code": code})
}

// writeExportDomainError maps typed domain failures onto their wire codes.
func (s *Service) writeExportDomainError(w http.ResponseWriter, r *http.Request, err error) {
	cors := s.corsSnapshot()
	var stale *modelexport.ErrSourceStale
	var unselectable *modelexport.ErrUnselectableModel
	var locked *modelexport.ErrLockedField
	var sensitive *modelexport.ErrSensitiveField
	var invalidEnhancement *modelexport.ErrInvalidEnhancement
	var targetSchema *modelexport.ErrTargetSchema
	var invalidDefault *modelexport.ErrDefaultModel
	responseutil.SetPrivateNoStoreHeaders(w)
	switch {
	case errors.As(err, &stale):
		responseutil.WriteErrorFields(w, r, cors, http.StatusConflict, stale.Error(), map[string]any{"code": exportStaleCode})
	case errors.As(err, &unselectable):
		responseutil.WriteErrorFields(w, r, cors, http.StatusUnprocessableEntity, unselectable.Error(), map[string]any{
			"code":            "export_model_unselectable",
			"model_config_id": unselectable.ModelConfigID,
			"reason":          unselectable.Reason,
		})
	case errors.As(err, &locked):
		responseutil.WriteErrorFields(w, r, cors, http.StatusUnprocessableEntity, locked.Error(), map[string]any{"code": "export_enhancement_rejected", "field": locked.Field})
	case errors.As(err, &sensitive):
		responseutil.WriteErrorFields(w, r, cors, http.StatusUnprocessableEntity, sensitive.Error(), map[string]any{"code": "export_enhancement_rejected", "field": sensitive.Field})
	case errors.As(err, &invalidEnhancement):
		responseutil.WriteErrorFields(w, r, cors, http.StatusUnprocessableEntity, invalidEnhancement.Error(), map[string]any{"code": "target_schema_invalid", "field": invalidEnhancement.Field})
	case errors.As(err, &targetSchema):
		responseutil.WriteErrorFields(w, r, cors, http.StatusUnprocessableEntity, targetSchema.Error(), map[string]any{"code": "target_schema_invalid", "field": targetSchema.Field})
	case errors.As(err, &invalidDefault):
		responseutil.WriteErrorFields(w, r, cors, http.StatusUnprocessableEntity, invalidDefault.Error(), map[string]any{"code": "export_default_model_invalid"})
	default:
		writeDomainError(w, r, cors, err)
	}
}
