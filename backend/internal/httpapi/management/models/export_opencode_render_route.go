package models

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

func (s *Service) handlePostOpenCodeExportRender(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	var req openCodeRenderRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.ExpectedSourceDigest == "" || len(req.ModelConfigIDs) == 0 {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "expected_source_digest and non-empty model_config_ids are required")
		return
	}
	baseURL, err := normalizeExportBaseURL(req.BaseURL)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "base_url must be an HTTP(S) origin")
		return
	}
	providerID, err := modelexport.NormalizeOpenCodeProviderID(req.ProviderID)
	if err != nil {
		s.writeOpenCodeExportError(w, r, err)
		return
	}
	apiKey := ""
	if req.Credential.Include {
		apiKey = strings.TrimSpace(req.Credential.APIKey)
		if apiKey == "" {
			responseutil.WriteErrorFields(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "api_key must be non-empty when credential.include is true", map[string]any{"code": "credential_api_key_required"})
			return
		}
	} else if req.Credential.APIKey != "" {
		responseutil.WriteErrorFields(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "api_key must be omitted when credential.include is false", map[string]any{"code": "credential_api_key_unexpected"})
		return
	}
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "OpenCode export render", func(tx pgx.Tx) (*openCodeRenderResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		facts, err := loadOpenCodeSourceFacts(r.Context(), tx, profile.ID)
		if err != nil {
			return nil, err
		}
		digest, err := modelexport.ComputeOpenCodeSourceDigest(facts)
		if err != nil {
			return nil, err
		}
		if req.ExpectedSourceDigest != digest {
			return nil, &modelexport.ErrSourceStale{}
		}
		selection, err := modelexport.NormalizeOpenCodeSelection(req.ModelConfigIDs, facts)
		if err != nil {
			return nil, err
		}
		result, err := modelexport.RenderOpenCode(modelexport.OpenCodeInput{
			Facts: facts, Selection: selection, BaseURL: baseURL, ProviderID: providerID,
			IncludeAPIKey: req.Credential.Include, APIKey: apiKey,
		})
		if err != nil {
			return nil, err
		}
		return &openCodeRenderResponse{
			TargetVersion: modelexport.OpenCodeTargetVersion, Content: result.Content,
			ContentSHA256: result.ContentSHA256, FileName: result.FileName, MIMEType: result.MIMEType,
			ModelResults: result.ModelResults, Warnings: result.Warnings, SourceDigest: digest,
		}, nil
	})
	if err != nil {
		s.writeOpenCodeExportError(w, r, err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) writeOpenCodeExportError(w http.ResponseWriter, r *http.Request, err error) {
	var stale *modelexport.ErrSourceStale
	var model *modelexport.ErrUnselectableModel
	var schema *modelexport.ErrTargetSchema
	switch {
	case errors.As(err, &stale):
		responseutil.WriteErrorFields(w, r, s.corsSnapshot(), http.StatusConflict, "export_source_stale", map[string]any{"code": "export_source_stale"})
	case errors.As(err, &model):
		responseutil.WriteErrorFields(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, model.Error(), map[string]any{"code": "model_unselectable", "model_config_id": model.ModelConfigID, "reason": model.Reason})
	case errors.As(err, &schema):
		responseutil.WriteErrorFields(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, schema.Error(), map[string]any{"code": "export_target_invalid", "field": schema.Field})
	default:
		writeDomainError(w, r, s.corsSnapshot(), err)
	}
}
