package models

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

// exportStaleCode is the stable drift code returned with HTTP 409 when the
// render request's digest no longer matches current source facts.
const exportStaleCode = "export_source_stale"

// handleGetExportSource serves GET /api/models/exports/{platform}/source.
// One read-only consistent snapshot backs the whole response; models.dev data
// comes from the freshest in-memory snapshot after one best-effort
// revalidation outside any transaction, and a failed fetch or vanished
// offering degrades to stored-only enrichment without failing the read.
func (s *Service) handleGetExportSource(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	platform, ok := parseExportPlatform(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	catalog := s.exportCatalog(r)
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "model export source", func(tx pgx.Tx) (*exportSourceResponse, error) {
		if _, err := resolveEffectiveProfile(r.Context(), tx, r); err != nil {
			return nil, err
		}
		modelRows, targetRows, bindings, graph, err := loadExportSnapshot(r.Context(), tx)
		if err != nil {
			return nil, err
		}
		facts, candidates := buildSourceFacts(platform, exportFactsInput{
			ModelRows:  modelRows,
			TargetRows: sortTargetRowsByModel(targetRows),
			Bindings:   bindings,
			Catalog:    catalog,
			Graph:      graph,
		})
		digest, err := modelexport.ComputeSourceDigest(facts)
		if err != nil {
			return nil, err
		}
		source, err := assembleSourceResponse(platform, facts, candidates)
		if err != nil {
			return nil, err
		}
		source.SourceDigest = digest
		return source, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// handlePostExportRender serves POST /api/models/exports/{platform}/render.
// It performs no network I/O: enrichment replays strictly from the request
// body against the digest-checked database snapshot. Typed failures map onto
// stable wire codes such as 409 export_source_stale and
// 422 export_model_unselectable.
func (s *Service) handlePostExportRender(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	platform, ok := parseExportPlatform(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	request, ok := decodeExportRenderRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	if len(request.ModelConfigIDs) == 0 {
		writeExportFieldError(w, r, s.corsSnapshot(), "export_selection_required", "model_config_ids must not be empty")
		return
	}
	baseURL, err := normalizeExportBaseURL(request.BaseURL)
	if err != nil {
		writeExportFieldError(w, r, s.corsSnapshot(), "export_invalid_base_url", "base_url must be an HTTP(S) origin without path, user info, query, or fragment")
		return
	}
	providerID := strings.TrimSpace(request.ProviderID)
	if providerID == "" {
		providerID = modelexport.PiProviderID
	}
	if strings.Contains(providerID, "/") {
		writeExportFieldError(w, r, s.corsSnapshot(), "export_invalid_provider_id", "provider_id must not contain '/'")
		return
	}
	if platform == modelexport.PlatformPi && request.DefaultModelConfigID != nil {
		writeExportFieldError(w, r, s.corsSnapshot(), "export_default_model_invalid", "default_model_config_id is supported only for OpenCode")
		return
	}
	apiKey := ""
	if request.Credential.Include {
		apiKey = strings.TrimSpace(request.Credential.APIKey)
	} else if strings.TrimSpace(request.Credential.APIKey) != "" {
		writeExportFieldError(w, r, s.corsSnapshot(), "export_credential_invalid", "credential.api_key requires credential.include=true")
		return
	}
	for _, enhancementWire := range request.Enhancements {
		if err := enhancementWire.decode().ValidateForPlatform(platform); err != nil {
			s.writeExportDomainError(w, r, err)
			return
		}
	}
	// Snapshot is an immutable in-memory pointer and performs no I/O. Render
	// tries it plus the explicit no-enrichment candidate; the request never
	// supplies catalog facts.
	var catalog *modelsdev.Catalog
	if s.catalog != nil {
		catalog = s.catalog.Snapshot()
	}
	rendered, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "model export render", func(tx pgx.Tx) (*exportRenderResponse, error) {
		if _, err := resolveEffectiveProfile(r.Context(), tx, r); err != nil {
			return nil, err
		}
		modelRows, targetRows, bindings, graph, err := loadExportSnapshot(r.Context(), tx)
		if err != nil {
			return nil, err
		}
		groupedTargets := sortTargetRowsByModel(targetRows)
		currentFacts, currentCandidates := buildSourceFacts(platform, exportFactsInput{
			ModelRows:  modelRows,
			TargetRows: groupedTargets,
			Bindings:   bindings,
			Catalog:    catalog,
			Graph:      graph,
		})
		noEnrichmentFacts, noEnrichmentCandidates := buildSourceFacts(platform, exportFactsInput{
			ModelRows:  modelRows,
			TargetRows: groupedTargets,
			Bindings:   bindings,
			Catalog:    nil,
			Graph:      graph,
		})
		facts, candidates := currentFacts, currentCandidates
		digest, err := modelexport.ComputeSourceDigest(currentFacts)
		if err != nil {
			return nil, err
		}
		if request.ExpectedSourceDigest != digest {
			withoutDigest, digestErr := modelexport.ComputeSourceDigest(noEnrichmentFacts)
			if digestErr != nil {
				return nil, digestErr
			}
			if request.ExpectedSourceDigest != withoutDigest {
				return nil, &modelexport.ErrSourceStale{}
			}
			facts, candidates = noEnrichmentFacts, noEnrichmentCandidates
		}
		selection, err := modelexport.NormalizeSelection(request.ModelConfigIDs, facts)
		if err != nil {
			return nil, err
		}
		enhancements := map[int]modelexport.ManualEnhancement{}
		selected := make(map[int]struct{}, len(selection))
		for _, id := range selection {
			selected[id] = struct{}{}
			if wire, present := request.Enhancements[id]; present && wire != nil {
				enhancements[id] = wire.decode()
			}
		}
		for id := range request.Enhancements {
			if _, ok := selected[id]; !ok {
				return nil, &modelexport.ErrUnselectableModel{ModelConfigID: id, Reason: "enhancement_model_not_selected"}
			}
		}
		result, err := modelexport.DispatchRender(platform, modelexport.RenderDispatch{
			Facts:         facts,
			Selection:     selection,
			Enrichment:    candidates,
			Enhancements:  enhancements,
			BaseURL:       baseURL,
			ProviderID:    providerID,
			IncludeAPIKey: request.Credential.Include,
			APIKey:        apiKey,
			DefaultModel:  request.DefaultModelConfigID,
		})
		if err != nil {
			return nil, err
		}
		return &exportRenderResponse{
			Platform:        string(platform),
			TargetVersion:   modelexport.TargetVersion(platform),
			CatalogRevision: facts.CatalogRevision,
			Content:         result.Content,
			ContentSHA256:   result.ContentSHA256,
			FileName:        result.FileName,
			MIMEType:        result.MIMEType,
			ModelResults:    result.ModelResults,
			Warnings:        result.Warnings,
		}, nil
	})
	if err != nil {
		s.writeExportDomainError(w, r, err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, rendered)
}

// assembleSourceResponse projects the validated facts into per-model source
// rows: layered metadata with provenance and missing leaves, catalog evidence,
// platform completeness, price risk, and the replayable candidate.
func assembleSourceResponse(platform modelexport.Platform, facts modelexport.SourceFacts, candidates map[int]modelexport.PlatformCandidate) (*exportSourceResponse, error) {
	response := &exportSourceResponse{
		Platform: string(platform), TargetVersion: modelexport.TargetVersion(platform),
		CatalogRevision: facts.CatalogRevision, Models: []exportSourceModelRow{},
	}
	for _, fact := range facts.Models {
		candidate := candidates[fact.ModelConfigID]
		prismLayer := modelexport.NewMetadataLayer(fact.PrismMetadata)
		merge, err := modelexport.MergeKnownMetadata(modelexport.MergeOptions{
			Prism:     prismLayer,
			ModelsDev: candidate.Metadata,
		})
		if err != nil {
			return nil, err
		}
		priceTargets := reachablePricingTargets(fact)
		decision := modelexport.DecidePriceExport(platform, priceTargets)

		row := exportSourceModelRow{
			ModelConfigID:         fact.ModelConfigID,
			ModelID:               fact.ModelID,
			APIFamily:             fact.APIFamily,
			DisplayName:           fact.DisplayName,
			IsEnabled:             fact.IsEnabled,
			DefaultSelected:       fact.Selectable,
			Selectable:            fact.Selectable,
			UnselectableReason:    fact.UnselectableReason,
			OpenAIAcceptedFormat:  fact.OpenAIAcceptedFormat,
			OpenAIImageOperations: fact.OpenAIImageOperations,
			Catalog:               exportCatalogEvidenceWire(fact.CatalogBinding),
			Enrichment: exportEnrichmentEvidenceWire{
				Available:          fact.Enrichment.Available,
				OfferingProviderID: fact.Enrichment.OfferingProviderID,
				OfferingModelID:    fact.Enrichment.OfferingModelID,
			},
			Prism:      rawMessageMap(prismLayer),
			ModelsDev:  rawMessageMap(candidate.Metadata),
			Merged:     rawMessageMap(merge.Merged),
			Provenance: provenanceStrings(merge.Provenance),
			Missing:    merge.Missing,
			Completeness: exportPlatformCompletenessWire{
				MetadataFields: platformFieldProjection(platform, merge.Merged, candidate),
				CostExportable: decision.Exportable,
			},
			Targets:             sourceTargetRows(fact.Targets),
			PriceRisk:           exportPriceRiskWire{Exportable: decision.Exportable, WarningCodes: decision.WarningCodes},
			Warnings:            modelexport.MergeWarningCodes(modelexport.MetadataWarningCodes(platform, fact, merge.Merged), candidate.WarningCodes),
			EnrichmentCandidate: encodeEnrichmentCandidate(candidate),
		}
		response.Models = append(response.Models, row)
	}
	return response, nil
}

// platformFieldProjection states which client-facing fields this platform's
// file will carry for the model. Absent stays false; nothing downstream is
// allowed to render absence as zero.
func platformFieldProjection(platform modelexport.Platform, merged modelexport.MetadataLayer, candidate modelexport.PlatformCandidate) map[string]bool {
	_, _ = merged, candidate
	fields := map[string]bool{}
	switch platform {
	case modelexport.PlatformPi:
		for leaf, key := range map[string]string{
			modelexport.MetaName:            "name",
			modelexport.MetaReasoning:       "reasoning",
			modelexport.MetaContextWindow:   "contextWindow",
			modelexport.MetaMaxOutputTokens: "maxTokens",
			modelexport.MetaModalitiesInput: "input",
		} {
			_, ok := merged.Get(leaf)
			fields[key] = ok
		}
		_, fields["thinkingLevelMap"] = candidate.DerivedFields["thinkingLevelMap"]
	case modelexport.PlatformOpenCode:
		for leaf, key := range map[string]string{
			modelexport.MetaName:        "name",
			modelexport.MetaFamily:      "family",
			modelexport.MetaReleaseDate: "release_date",
			modelexport.MetaAttachment:  "attachment",
			modelexport.MetaReasoning:   "reasoning",
			modelexport.MetaTemperature: "temperature",
			modelexport.MetaToolCall:    "tool_call",
		} {
			_, known := merged.Get(leaf)
			fields[key] = known
		}
		_, contextKnown := merged.Get(modelexport.MetaContextWindow)
		_, inputLimitKnown := merged.Get(modelexport.MetaMaxInputTokens)
		_, outputKnown := merged.Get(modelexport.MetaMaxOutputTokens)
		fields["limit.context"] = contextKnown
		fields["limit.input"] = inputLimitKnown
		fields["limit.output"] = outputKnown
		_, inputKnown := merged.Get(modelexport.MetaModalitiesInput)
		fields["modalities.input"] = inputKnown
		_, outputModalitiesKnown := merged.Get(modelexport.MetaModalitiesOutput)
		fields["modalities.output"] = outputModalitiesKnown
		_, fields["interleaved"] = candidate.DerivedFields["interleaved"]
		fields["variants"] = false
	}
	return fields
}

// sourceTargetRows converts domain facts into wire rows without secrets.
func sourceTargetRows(targets []modelexport.TargetFact) []exportSourceTargetRow {
	rows := make([]exportSourceTargetRow, 0, len(targets))
	for _, target := range targets {
		wire := exportSourceTargetRow{
			TerminalTargetID:     target.TerminalTargetID,
			Position:             target.Position,
			EndpointID:           target.EndpointID,
			EndpointName:         target.EndpointName,
			OpenAITextCapability: target.OpenAITextCapability,
		}
		if pricing := target.Pricing; pricing != nil {
			card := func(value *modelexport.PriceCardSnapshot) *exportPriceCardWire {
				if value == nil {
					return nil
				}
				return &exportPriceCardWire{
					InputPrice:         value.InputPrice,
					OutputPrice:        value.OutputPrice,
					CachedInputPrice:   value.CachedInputPrice,
					CacheCreationPrice: value.CacheCreationPrice,
					ReasoningPrice:     value.ReasoningPrice,
				}
			}
			wire.Pricing = &exportTargetPricingWire{
				TerminalTargetID: target.TerminalTargetID,
				Kind:             string(pricing.Kind),
				CurrencyCode:     pricing.CurrencyCode,
				PricingUnit:      pricing.PricingUnit,
				TierThreshold:    pricing.TierThreshold,
				Card:             card(pricing.Card),
				BaseCard:         card(pricing.BaseCard),
				AboveCard:        card(pricing.AboveCard),
			}
		}
		rows = append(rows, wire)
	}
	return rows
}

func rawMessageMap(layer modelexport.MetadataLayer) map[string]json.RawMessage {
	values := map[string]json.RawMessage{}
	for _, leaf := range layer.Leaves() {
		value, _ := layer.Get(leaf)
		values[leaf] = value
	}
	return values
}

func provenanceStrings(provenance map[string]modelexport.MetadataSource) map[string]string {
	out := map[string]string{}
	for leaf, source := range provenance {
		out[leaf] = string(source)
	}
	return out
}

// exportCatalog returns only a freshly revalidated snapshot. A failed fetch
// never re-labels the stale cache as current enrichment.
func (s *Service) exportCatalog(r *http.Request) *modelsdev.Catalog {
	if s.catalog == nil {
		return nil
	}
	if catalog, err := s.catalog.Fetch(r.Context()); err == nil {
		return catalog
	}
	return nil
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
