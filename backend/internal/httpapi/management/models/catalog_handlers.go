package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

// catalogAutoMatchCandidateBudget bounds the candidate list embedded in
// auto-match payloads so responses stay bounded regardless of catalog size.
const catalogAutoMatchCandidateBudget = 20

// maxOverrideStringChars caps operator-authored string overrides.
const maxOverrideStringChars = 500

func routeIntOrBadRequest(w http.ResponseWriter, r *http.Request, cors platformcors.Snapshot) (int, bool) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, cors, http.StatusBadRequest, err.Error())
		return 0, false
	}
	return modelConfigID, true
}

func newCatalogDomainError(statusCode int, detail string, fields map[string]any) error {
	return &domainError{StatusCode: statusCode, Detail: detail, Fields: fields}
}

func isCatalogUnavailable(err error) bool {
	return errors.Is(err, modelsdev.ErrCatalogUnavailable)
}

func catalogFetchFailed(err error) error {
	return &domainError{
		StatusCode: http.StatusBadGateway,
		Detail:     fmt.Sprintf("models_dev_catalog_unavailable: %v", err),
	}
}

func catalogStaleError(expected, current string) error {
	return &domainError{
		StatusCode: http.StatusConflict,
		Detail:     "models_dev_catalog_stale: the previewed catalog revision no longer matches current data",
		Fields: map[string]any{
			"expected_catalog_revision": expected,
			"current_catalog_revision":  current,
		},
	}
}

func (s *Service) writeCatalogDomainError(w http.ResponseWriter, r *http.Request, err error) {
	writeDomainError(w, r, s.corsSnapshot(), err)
}

// fetchValidatedCatalog performs the network round trip strictly outside any
// database transaction and fails closed on transport or schema problems.
func (s *Service) fetchValidatedCatalog(ctx context.Context) (*modelsdev.Catalog, error) {
	catalog, err := s.catalog.Fetch(ctx)
	if err != nil {
		if isCatalogUnavailable(err) {
			return nil, catalogFetchFailed(err)
		}
		return nil, err
	}
	return catalog, nil
}

// autoMatchHint computes the unique-exact match state for one api_family from
// an already-fetched catalog. The result never binds anything on its own.
func autoMatchHint(catalog *modelsdev.Catalog, apiFamily, modelID string) *modelCatalogAutoMatchPayload {
	matches := modelsdev.ExactMatches(catalog, apiFamily, modelID)
	if len(matches) > catalogAutoMatchCandidateBudget {
		matches = matches[:catalogAutoMatchCandidateBudget]
	}
	hint := &modelCatalogAutoMatchPayload{Available: true, Candidates: matches}
	switch {
	case len(matches) == 1:
		hint.Unique = true
		hint.Reason = "unique_match"
	case len(matches) > 1:
		hint.Reason = "ambiguous"
	default:
		hint.Reason = "no_match"
	}
	return hint
}

func catalogResponseFromBinding(binding catalogBindingRecord) modelCatalogResponse {
	payload := modelCatalogResponse{Bound: false, Source: nil, Override: nil, Effective: nil}
	if binding.ModelConfigID == 0 {
		return payload
	}
	response := binding.response()
	if response != nil {
		payload = *response
	}
	return payload
}

// loadModelForCatalog resolves the profile-scoped model record and rejects
// unknown ids before any catalog work happens.
func loadModelForCatalog(ctx context.Context, tx pgx.Tx, profileID, modelConfigID int) (modelRecord, error) {
	record, found, err := loadModelRecord(ctx, tx, profileID, modelConfigID, false)
	if err != nil {
		return modelRecord{}, err
	}
	if !found {
		return modelRecord{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
	}
	return record, nil
}

func (s *Service) handleGetModelCatalog(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (modelCatalogResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return modelCatalogResponse{}, err
		}
		record, err := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID)
		if err != nil {
			return modelCatalogResponse{}, err
		}
		binding, _, err := loadCatalogBinding(r.Context(), tx, profile.ID, modelConfigID)
		if err != nil {
			return modelCatalogResponse{}, err
		}
		payload := catalogResponseFromBinding(binding)
		if !payload.Bound && s.catalog != nil {
			if snapshot := s.catalog.Snapshot(); snapshot != nil {
				payload.AutoMatch = autoMatchHint(snapshot, record.APIFamily, record.ModelID)
			}
		}
		return payload, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetCatalogCandidates(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "family"
	}
	if scope != "family" && scope != "all" {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "scope must be family or all", nil))
		return
	}
	limit := parseBoundedLimit(r.URL.Query().Get("limit"))
	offset, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
	if err != nil || offset < 0 {
		offset = 0
	}
	var apiFamily string
	listErr := pgxutil.InTx(r.Context(), s.pool, "model", func(tx pgx.Tx) error {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return profileErr
		}
		record, recordErr := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID)
		if recordErr != nil {
			return recordErr
		}
		apiFamily = record.APIFamily
		return nil
	})
	if listErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), listErr)
		return
	}
	catalog, err := s.currentCatalog(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	items, total := modelsdev.SearchCandidates(catalog, apiFamily, query, scope, limit, offset)
	responseutil.WriteJSON(w, http.StatusOK, modelCatalogCandidatesResponse{
		Items: items, Total: total, Limit: limit, Offset: offset, Scope: scope, Query: query,
	})
}

// currentCatalog returns the cached snapshot, fetching lazily when the process
// has not loaded one yet. The fetch stays outside database transactions.
func (s *Service) currentCatalog(ctx context.Context) (*modelsdev.Catalog, error) {
	if snapshot := s.catalog.Snapshot(); snapshot != nil {
		return snapshot, nil
	}
	return s.fetchValidatedCatalog(ctx)
}

func parseBoundedLimit(raw string) int {
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

type modelCatalogMatchPreviewResponse struct {
	Committable     bool                  `json:"committable"`
	ProviderID      string                `json:"provider_id,omitempty"`
	CatalogModelID  string                `json:"catalog_model_id,omitempty"`
	Candidates      []modelsdev.Candidate `json:"candidates"`
	Reason          string                `json:"reason"`
	CatalogRevision string                `json:"catalog_revision"`
	FetchedAt       time.Time             `json:"fetched_at"`
}

func (s *Service) handleMatchCatalogPreview(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	if !s.requireCatalogClient(w, r) {
		return
	}
	var apiFamily, modelID string
	loadErr := pgxutil.InTx(r.Context(), s.pool, "model", func(tx pgx.Tx) error {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return profileErr
		}
		record, recordErr := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID)
		if recordErr != nil {
			return recordErr
		}
		apiFamily, modelID = record.APIFamily, record.ModelID
		return nil
	})
	if loadErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), loadErr)
		return
	}
	catalog, err := s.fetchValidatedCatalog(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	matches := modelsdev.ExactMatches(catalog, apiFamily, modelID)
	preview := modelCatalogMatchPreviewResponse{
		Candidates:      matches,
		CatalogRevision: catalog.ETag,
		FetchedAt:       catalog.FetchedAt,
	}
	switch len(matches) {
	case 1:
		preview.Committable = true
		preview.ProviderID = matches[0].ProviderID
		preview.CatalogModelID = matches[0].ModelID
		preview.Reason = "unique_match"
	case 0:
		preview.Reason = "no_match"
	default:
		preview.Reason = "ambiguous"
	}
	responseutil.WriteJSON(w, http.StatusOK, preview)
}

func (s *Service) requireCatalogClient(w http.ResponseWriter, r *http.Request) bool {
	if s.catalog == nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "models_dev_catalog_client_missing: catalog client is not configured")
		return false
	}
	return true
}

func (s *Service) handleBindModelCatalog(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	if !s.requireCatalogClient(w, r) {
		return
	}
	var requestBody modelCatalogBindRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	providerID := strings.TrimSpace(requestBody.ProviderID)
	catalogModelID := strings.TrimSpace(requestBody.CatalogModelID)
	expectedRevision := strings.TrimSpace(requestBody.ExpectedCatalogRevision)
	if expectedRevision == "" {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "expected_catalog_revision is required so stale previews cannot commit", map[string]any{"field": "expected_catalog_revision"}))
		return
	}
	if (providerID == "") != (catalogModelID == "") {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "provider_id and catalog_model_id must be provided together for manual binding", map[string]any{"field": "provider_id"}))
		return
	}

	var apiFamily, runtimeModelID string
	loadErr := pgxutil.InTx(r.Context(), s.pool, "model", func(tx pgx.Tx) error {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return profileErr
		}
		record, recordErr := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID)
		if recordErr != nil {
			return recordErr
		}
		apiFamily, runtimeModelID = record.APIFamily, record.ModelID
		return nil
	})
	if loadErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), loadErr)
		return
	}

	// Remote I/O happens here, outside every transaction below.
	catalog, err := s.fetchValidatedCatalog(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if catalog.ETag != expectedRevision {
		writeDomainError(w, r, s.corsSnapshot(), catalogStaleError(expectedRevision, catalog.ETag))
		return
	}

	matchSource := catalogMatchSourceUnique
	if providerID != "" {
		matchSource = catalogMatchSourceManual
		model, found := catalog.Find(providerID, catalogModelID)
		if !found {
			s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "models_dev_offering_unknown: the requested provider/model pair does not exist in the catalog", map[string]any{"field": "provider_id", "provider_id": providerID, "catalog_model_id": catalogModelID}))
			return
		}
		_ = model
	} else {
		matches := modelsdev.ExactMatches(catalog, apiFamily, runtimeModelID)
		switch len(matches) {
		case 1:
			providerID, catalogModelID = matches[0].ProviderID, matches[0].ModelID
		case 0:
			s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusConflict, "models_dev_match_missing: no exact catalog id matches this model id; bind explicitly", map[string]any{"reason": "no_match", "candidates": []modelsdev.Candidate{}, "model_id": runtimeModelID}))
			return
		default:
			s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusConflict, "models_dev_match_ambiguous: the model id matches multiple providers; bind explicitly", map[string]any{"reason": "ambiguous", "candidates": matches}))
			return
		}
	}

	sourceMetadata := catalogMetadataFromCoordinates(catalog, providerID, catalogModelID)
	now := s.nowUTC()
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (modelCatalogResponse, error) {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return modelCatalogResponse{}, profileErr
		}
		if _, loadErr := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID); loadErr != nil {
			return modelCatalogResponse{}, loadErr
		}
		existing, _, loadBindingErr := loadCatalogBinding(r.Context(), tx, profile.ID, modelConfigID)
		if loadBindingErr != nil {
			return modelCatalogResponse{}, loadBindingErr
		}
		sameOffering := existing.ProviderID == providerID && existing.CatalogModelID == catalogModelID
		record := catalogBindingRecord{
			ModelConfigID:   modelConfigID,
			ProviderID:      providerID,
			CatalogModelID:  catalogModelID,
			MatchSource:     matchSource,
			CatalogRevision: catalog.ETag,
			FetchedAt:       catalog.FetchedAt,
			UpdatedAt:       now,
			Source:          sourceMetadata,
			Override:        existing.Override,
		}
		// Rebinding to a different offering invalidates prior overrides: they
		// described another provider's metadata. Same-offering rebinds keep
		// both overrides and the original match_source.
		if !sameOffering {
			record.Override = modelCatalogMetadata{}
		} else if existing.MatchSource != "" {
			record.MatchSource = existing.MatchSource
		}
		if upsertErr := upsertCatalogBinding(r.Context(), tx, record, now); upsertErr != nil {
			return modelCatalogResponse{}, upsertErr
		}
		saved, _, saveErr := loadCatalogBinding(r.Context(), tx, profile.ID, modelConfigID)
		if saveErr != nil {
			return modelCatalogResponse{}, saveErr
		}
		return catalogResponseFromBinding(saved), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func catalogMetadataFromCoordinates(catalog *modelsdev.Catalog, providerID, catalogModelID string) modelCatalogMetadata {
	model, _ := catalog.Find(providerID, catalogModelID)
	if model == nil {
		return modelCatalogMetadata{}
	}
	return catalogMetadataFromModel(model)
}

func (s *Service) handleRefreshCatalogPreview(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	if !s.requireCatalogClient(w, r) {
		return
	}
	var binding catalogBindingRecord
	_, loadErr := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (struct{}, error) {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return struct{}{}, profileErr
		}
		if _, modelErr := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID); modelErr != nil {
			return struct{}{}, modelErr
		}
		if _, findErr := loadBoundCatalogBinding(r.Context(), tx, profile.ID, modelConfigID, &binding); findErr != nil {
			return struct{}{}, findErr
		}
		return struct{}{}, nil
	})
	if loadErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), loadErr)
		return
	}
	catalog, err := s.fetchValidatedCatalog(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	nextModel, exists := catalog.Find(binding.ProviderID, binding.CatalogModelID)
	if !exists {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusConflict, "models_dev_offering_missing: the bound offering disappeared from the catalog; nothing was changed", map[string]any{"provider_id": binding.ProviderID, "catalog_model_id": binding.CatalogModelID}))
		return
	}
	next := catalogMetadataFromModel(nextModel)
	changes, changed := diffCatalogSource(binding.Source, next)
	responseutil.WriteJSON(w, http.StatusOK, modelCatalogRefreshPreviewResponse{
		Bound:           true,
		ProviderID:      binding.ProviderID,
		CatalogModelID:  binding.CatalogModelID,
		Changed:         changed,
		Changes:         changes,
		CatalogRevision: catalog.ETag,
		FetchedAt:       catalog.FetchedAt,
	})
}

// loadBoundCatalogBinding loads a binding and reports whether the surface may
// proceed; unbound models reject refresh flows instead of silently rebinding.
func loadBoundCatalogBinding(ctx context.Context, exec queryExecutor, profileID, modelConfigID int, out *catalogBindingRecord) (bool, error) {
	binding, found, err := loadCatalogBinding(ctx, exec, profileID, modelConfigID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, newCatalogDomainError(http.StatusConflict, "models_dev_not_bound: bind a catalog offering before refreshing metadata", nil)
	}
	*out = binding
	return true, nil
}

func (s *Service) handleRefreshCatalogCommit(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	if !s.requireCatalogClient(w, r) {
		return
	}
	var requestBody modelCatalogRefreshCommitRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	expectedRevision := strings.TrimSpace(requestBody.ExpectedCatalogRevision)
	if expectedRevision == "" {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "expected_catalog_revision is required so stale data cannot commit", map[string]any{"field": "expected_catalog_revision"}))
		return
	}
	catalog, err := s.fetchValidatedCatalog(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if catalog.ETag != expectedRevision {
		writeDomainError(w, r, s.corsSnapshot(), catalogStaleError(expectedRevision, catalog.ETag))
		return
	}
	now := s.nowUTC()
	response, txErr := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (modelCatalogResponse, error) {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return modelCatalogResponse{}, profileErr
		}
		if _, modelErr := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID); modelErr != nil {
			return modelCatalogResponse{}, modelErr
		}
		var binding catalogBindingRecord
		if _, boundErr := loadBoundCatalogBinding(r.Context(), tx, profile.ID, modelConfigID, &binding); boundErr != nil {
			return modelCatalogResponse{}, boundErr
		}
		// The coordinates are re-resolved inside the transaction against the
		// same fetched revision, so a concurrent rebind cannot smuggle foreign
		// source values into the new offering's row.
		nextModel, exists := catalog.Find(binding.ProviderID, binding.CatalogModelID)
		if !exists {
			return modelCatalogResponse{}, newCatalogDomainError(http.StatusConflict, "models_dev_offering_missing: the bound offering disappeared from the catalog; nothing was changed", map[string]any{"provider_id": binding.ProviderID, "catalog_model_id": binding.CatalogModelID})
		}
		binding.Source = catalogMetadataFromModel(nextModel)
		binding.CatalogRevision = catalog.ETag
		binding.FetchedAt = catalog.FetchedAt
		binding.UpdatedAt = now
		if upsertErr := upsertCatalogBinding(r.Context(), tx, binding, now); upsertErr != nil {
			return modelCatalogResponse{}, upsertErr
		}
		saved, _, saveErr := loadCatalogBinding(r.Context(), tx, profile.ID, modelConfigID)
		if saveErr != nil {
			return modelCatalogResponse{}, saveErr
		}
		return catalogResponseFromBinding(saved), nil
	})
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// overrideFieldSpec describes one overridable metadata field with explicit
// null (restore-to-source) and typed assignment behavior.
type overrideFieldSpec struct {
	field    string
	kind     string // string | bool | int | string_list | status | date
	setNull  func(*modelCatalogMetadata)
	setValue func(*modelCatalogMetadata, any)
}

var overrideFieldSpecs = []overrideFieldSpec{
	{field: "name", kind: "string", setNull: func(m *modelCatalogMetadata) { m.Name = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Name = v.(*string) }},
	{field: "description", kind: "string", setNull: func(m *modelCatalogMetadata) { m.Description = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Description = v.(*string) }},
	{field: "family", kind: "string", setNull: func(m *modelCatalogMetadata) { m.Family = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Family = v.(*string) }},
	{field: "release_date", kind: "date", setNull: func(m *modelCatalogMetadata) { m.ReleaseDate = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.ReleaseDate = v.(*string) }},
	{field: "last_updated", kind: "date", setNull: func(m *modelCatalogMetadata) { m.LastUpdated = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.LastUpdated = v.(*string) }},
	{field: "knowledge", kind: "date", setNull: func(m *modelCatalogMetadata) { m.Knowledge = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Knowledge = v.(*string) }},
	{field: "attachment", kind: "bool", setNull: func(m *modelCatalogMetadata) { m.Attachment = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Attachment = v.(*bool) }},
	{field: "reasoning", kind: "bool", setNull: func(m *modelCatalogMetadata) { m.Reasoning = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Reasoning = v.(*bool) }},
	{field: "tool_call", kind: "bool", setNull: func(m *modelCatalogMetadata) { m.ToolCall = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.ToolCall = v.(*bool) }},
	{field: "structured_output", kind: "bool", setNull: func(m *modelCatalogMetadata) { m.StructuredOutput = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.StructuredOutput = v.(*bool) }},
	{field: "temperature", kind: "bool", setNull: func(m *modelCatalogMetadata) { m.Temperature = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Temperature = v.(*bool) }},
	{field: "modalities_input", kind: "string_list", setNull: func(m *modelCatalogMetadata) { m.ModalitiesInput = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.ModalitiesInput = append([]string(nil), v.([]string)...) }},
	{field: "modalities_output", kind: "string_list", setNull: func(m *modelCatalogMetadata) { m.ModalitiesOutput = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.ModalitiesOutput = append([]string(nil), v.([]string)...) }},
	{field: "limit_context", kind: "int", setNull: func(m *modelCatalogMetadata) { m.LimitContext = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.LimitContext = v.(*int64) }},
	{field: "limit_input", kind: "int", setNull: func(m *modelCatalogMetadata) { m.LimitInput = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.LimitInput = v.(*int64) }},
	{field: "limit_output", kind: "int", setNull: func(m *modelCatalogMetadata) { m.LimitOutput = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.LimitOutput = v.(*int64) }},
	{field: "open_weights", kind: "bool", setNull: func(m *modelCatalogMetadata) { m.OpenWeights = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.OpenWeights = v.(*bool) }},
	{field: "status", kind: "status", setNull: func(m *modelCatalogMetadata) { m.Status = nil }, setValue: func(m *modelCatalogMetadata, v any) { m.Status = v.(*string) }},
}

func decodeOverrideFields(body []byte) (map[string]any, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, newCatalogDomainError(http.StatusBadRequest, "Invalid request body", nil)
	}
	if len(raw) == 0 {
		return nil, newCatalogDomainError(http.StatusUnprocessableEntity, "override payload must carry at least one field", map[string]any{"field": "body"})
	}
	values := make(map[string]any, len(raw))
	for key, valueRaw := range raw {
		var spec *overrideFieldSpec
		for index := range overrideFieldSpecs {
			if overrideFieldSpecs[index].field == key {
				spec = &overrideFieldSpecs[index]
				break
			}
		}
		if spec == nil {
			return nil, newCatalogDomainError(http.StatusUnprocessableEntity, fmt.Sprintf("unknown override field %q", key), map[string]any{"field": key})
		}
		if string(valueRaw) == "null" {
			values[key] = nil
			continue
		}
		parsed, err := parseOverrideValue(spec.kind, valueRaw)
		if err != nil {
			return nil, err
		}
		values[key] = parsed
	}
	return values, nil
}

func parseOverrideValue(kind string, raw json.RawMessage) (any, error) {
	fieldViolation := func(reason, message string) error {
		return newCatalogDomainError(http.StatusUnprocessableEntity, message, map[string]any{"field": kind, "reason": reason})
	}
	switch kind {
	case "string", "date":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fieldViolation("invalid_type", "must be a string or null")
		}
		if len(value) > maxOverrideStringChars {
			return nil, fieldViolation("too_long", fmt.Sprintf("must not exceed %d characters", maxOverrideStringChars))
		}
		if kind == "date" && strings.TrimSpace(value) != "" && !isLooseCatalogDate(value) {
			return nil, fieldViolation("invalid_date", "must look like YYYY-MM or YYYY-MM-DD")
		}
		return &value, nil
	case "status":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fieldViolation("invalid_type", "must be alpha, beta, deprecated, or null")
		}
		switch value {
		case modelsdev.StatusAlpha, modelsdev.StatusBeta, modelsdev.StatusDeprecated:
			return &value, nil
		default:
			return nil, fieldViolation("invalid_enum", "must be alpha, beta, deprecated, or null")
		}
	case "bool":
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fieldViolation("invalid_type", "must be a boolean or null")
		}
		return &value, nil
	case "int":
		var value int64
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fieldViolation("invalid_type", "must be a non-negative integer or null")
		}
		if value < 0 {
			return nil, fieldViolation("invalid_range", "must be a non-negative integer or null")
		}
		return &value, nil
	case "string_list":
		var value []string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fieldViolation("invalid_type", "must be an array of strings or null")
		}
		for _, item := range value {
			if len(item) > maxOverrideStringChars {
				return nil, fieldViolation("too_long", fmt.Sprintf("list entries must not exceed %d characters", maxOverrideStringChars))
			}
		}
		return value, nil
	}
	return nil, fieldViolation("invalid_type", "unsupported override type")
}

func isLooseCatalogDate(value string) bool {
	trimmed := strings.TrimSpace(value)
	parts := strings.Split(trimmed, "-")
	if len(parts) != 2 && len(parts) != 3 {
		return false
	}
	if len(parts[0]) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	if len(parts) >= 2 && len(parts[1]) != 2 {
		return false
	}
	if len(parts) == 3 && len(parts[2]) != 2 {
		return false
	}
	return true
}

func (s *Service) handlePutCatalogOverride(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	body, err := readJSONBody(r)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	values, err := decodeOverrideFields(body)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	now := s.nowUTC()
	response, txErr := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (modelCatalogResponse, error) {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return modelCatalogResponse{}, profileErr
		}
		if _, modelErr := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID); modelErr != nil {
			return modelCatalogResponse{}, modelErr
		}
		var binding catalogBindingRecord
		if _, boundErr := loadBoundCatalogBinding(r.Context(), tx, profile.ID, modelConfigID, &binding); boundErr != nil {
			return modelCatalogResponse{}, boundErr
		}
		for _, spec := range overrideFieldSpecs {
			value, present := values[spec.field]
			if !present {
				continue
			}
			if value == nil {
				spec.setNull(&binding.Override)
				continue
			}
			spec.setValue(&binding.Override, value)
		}
		binding.UpdatedAt = now
		if upsertErr := upsertCatalogBinding(r.Context(), tx, binding, now); upsertErr != nil {
			return modelCatalogResponse{}, upsertErr
		}
		saved, _, saveErr := loadCatalogBinding(r.Context(), tx, profile.ID, modelConfigID)
		if saveErr != nil {
			return modelCatalogResponse{}, saveErr
		}
		return catalogResponseFromBinding(saved), nil
	})
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleClearCatalogOverride(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	now := s.nowUTC()
	response, txErr := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (modelCatalogResponse, error) {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return modelCatalogResponse{}, profileErr
		}
		if _, modelErr := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID); modelErr != nil {
			return modelCatalogResponse{}, modelErr
		}
		var binding catalogBindingRecord
		if _, boundErr := loadBoundCatalogBinding(r.Context(), tx, profile.ID, modelConfigID, &binding); boundErr != nil {
			return modelCatalogResponse{}, boundErr
		}
		binding.Override = modelCatalogMetadata{}
		binding.UpdatedAt = now
		if upsertErr := upsertCatalogBinding(r.Context(), tx, binding, now); upsertErr != nil {
			return modelCatalogResponse{}, upsertErr
		}
		saved, _, saveErr := loadCatalogBinding(r.Context(), tx, profile.ID, modelConfigID)
		if saveErr != nil {
			return modelCatalogResponse{}, saveErr
		}
		return catalogResponseFromBinding(saved), nil
	})
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleUnbindModelCatalog(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	response, txErr := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (modelCatalogResponse, error) {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return modelCatalogResponse{}, profileErr
		}
		if _, modelErr := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID); modelErr != nil {
			return modelCatalogResponse{}, modelErr
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM model_catalog_bindings WHERE model_config_id = $1`, modelConfigID); err != nil {
			return modelCatalogResponse{}, fmt.Errorf("unbind catalog binding for model %d: %w", modelConfigID, err)
		}
		return catalogResponseFromBinding(catalogBindingRecord{}), nil
	})
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}
