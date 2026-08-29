package models

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// handlePostPiExportRender serves POST /api/models/exports/pi/render. It performs
// no network I/O and never reads the live pi.dev catalog: every selected
// model's rendered coordinate and safe metadata come from its persisted
// model_pi_catalog_bindings row. Request-carried selections are pure
// assertions of what the caller believes source last published; they can
// never choose or change a binding, only be checked against it.
func (s *Service) handlePostPiExportRender(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	var req piRenderRequest
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
	if req.ExpectedSourceDigest == "" {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "expected_source_digest is required")
		return
	}
	if len(req.ModelConfigIDs) == 0 {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "model_config_ids must not be empty")
		return
	}
	baseURL, err := normalizeExportBaseURL(req.BaseURL)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "base_url must be an HTTP(S) origin")
		return
	}
	providerID := strings.TrimSpace(req.ProviderID)
	if providerID == "" {
		providerID = modelexport.PiProviderID
	}
	if strings.Contains(providerID, "/") {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "provider_id must not contain '/'")
		return
	}
	includeKey := req.Credential.Include
	apiKey := ""
	if includeKey {
		apiKey = strings.TrimSpace(req.Credential.APIKey)
		if apiKey == "" {
			responseutil.WriteErrorFields(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "api_key must be non-empty when credential.include is true", map[string]any{
				"code": "credential_api_key_required",
			})
			return
		}
	}
	rendered, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "pi export render", func(tx pgx.Tx) (*piRenderResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		modelRows, targetRows, graph, err := loadExportSnapshot(r.Context(), tx, profile.ID)
		if err != nil {
			return nil, err
		}
		piBindings, err := loadPiBindingsForModels(r.Context(), tx, profile.ID, exportModelConfigIDs(modelRows))
		if err != nil {
			return nil, err
		}
		grouped := sortTargetRowsByModel(targetRows)
		facts, _, err := buildPiSourceFacts(piExportFactsInput{
			ModelRows:     modelRows,
			TargetRows:    grouped,
			PiBindings:    piBindings,
			CatalogStatus: "unavailable",
			Graph:         graph,
		})
		if err != nil {
			return nil, err
		}
		digest, err := modelexport.ComputeSourceDigest(facts)
		if err != nil {
			return nil, err
		}
		if req.ExpectedSourceDigest != digest {
			return nil, &modelexport.ErrSourceStale{}
		}
		selection, err := modelexport.NormalizeSelection(req.ModelConfigIDs, facts)
		if err != nil {
			return nil, err
		}
		if err := requirePiSelectionsMatchBindings(selection, facts, req.Selections); err != nil {
			return nil, err
		}
		result, err := modelexport.RenderPi(modelexport.PiInput{
			Facts:         facts,
			Selection:     selection,
			BaseURL:       baseURL,
			ProviderID:    providerID,
			IncludeAPIKey: includeKey,
			APIKey:        apiKey,
		})
		if err != nil {
			return nil, err
		}
		return &piRenderResponse{
			TargetVersion: modelexport.PiTargetVersion,
			Content:       result.Content,
			ContentSHA256: result.ContentSHA256,
			FileName:      result.FileName,
			MIMEType:      result.MIMEType,
			ModelResults:  result.ModelResults,
			Warnings:      result.Warnings,
			SourceDigest:  digest,
		}, nil
	})
	if err != nil {
		s.writePiExportError(w, r, err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, rendered)
}

// writePiExportError maps the typed domain errors render can raise onto
// stable wire codes. A stale digest is a 409 (refetch source and retry
// unchanged); a selection that does not resolve to a current binding is a
// 422 (the request itself needs to change, not just retry).
func (s *Service) writePiExportError(w http.ResponseWriter, r *http.Request, err error) {
	if _, ok := err.(*modelexport.ErrSourceStale); ok {
		responseutil.WriteErrorFields(w, r, s.corsSnapshot(), http.StatusConflict, "export_source_stale", map[string]any{"code": "export_source_stale"})
		return
	}
	if e, ok := err.(*modelexport.ErrCandidateUnselected); ok {
		responseutil.WriteErrorFields(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, e.Error(), map[string]any{
			"code": "candidate_unselected", "model_config_id": e.ModelConfigID,
		})
		return
	}
	if e, ok := err.(*modelexport.ErrCandidateInvalid); ok {
		responseutil.WriteErrorFields(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, e.Error(), map[string]any{
			"code": "candidate_invalid", "model_config_id": e.ModelConfigID,
		})
		return
	}
	if e, ok := err.(*modelexport.ErrUnselectableModel); ok {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, e.Error())
		return
	}
	writeDomainError(w, r, s.corsSnapshot(), err)
}

// requirePiSelectionsMatchBindings checks every selected model's
// request-carried coordinate assertion against its persisted binding. A
// selected model with no binding, or an assertion naming a different
// coordinate than the one bound, fails closed: render never falls back to
// choosing a candidate on the caller's behalf.
func requirePiSelectionsMatchBindings(selection []int, facts modelexport.SourceFacts, assertedByID map[int]*piSelectedWire) error {
	selectedIDs := make(map[int]struct{}, len(selection))
	for _, id := range selection {
		selectedIDs[id] = struct{}{}
	}
	for id := range assertedByID {
		if _, selected := selectedIDs[id]; !selected {
			return &modelexport.ErrCandidateInvalid{ModelConfigID: id}
		}
	}
	if len(assertedByID) != len(selectedIDs) {
		for _, id := range selection {
			if asserted, ok := assertedByID[id]; !ok || asserted == nil {
				return &modelexport.ErrCandidateUnselected{ModelConfigID: id}
			}
		}
	}
	factsByID := make(map[int]modelexport.ModelFact, len(facts.Models))
	for _, fact := range facts.Models {
		factsByID[fact.ModelConfigID] = fact
	}
	for _, id := range selection {
		fact := factsByID[id]
		if fact.PiSelected == nil {
			return &modelexport.ErrCandidateUnselected{ModelConfigID: id}
		}
		asserted, ok := assertedByID[id]
		if !ok || asserted == nil {
			return &modelexport.ErrCandidateUnselected{ModelConfigID: id}
		}
		if asserted.ProviderID != fact.PiSelected.ProviderID || asserted.ModelID != fact.PiSelected.ModelID || asserted.API != fact.PiSelected.API {
			return &modelexport.ErrCandidateInvalid{ModelConfigID: id}
		}
	}
	return nil
}
