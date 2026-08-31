package models

import (
	"github.com/coachpo/prism/backend/internal/domain/pidev"
)

// The live-candidate and persisted-binding status projections shared by the
// full export source and the single-model Pi read. Both surfaces compute the
// same two independent axes: discovery evidence never selects anything, and
// a compatible persisted binding stays render-authoritative through live
// catalog failure or drift.

// piBindingMatchesModel is the non-negotiable persisted-binding health gate.
// It compares the binding against the Prism identity snapshot frozen at bind
// time, never against the directory model id: an explicit cross-directory bind
// is meant to survive a directory id that differs from the Prism id, while a
// later Prism model-id or accepted-format edit must never leave the old
// coordinate render-authoritative.
func piBindingMatchesModel(binding piBindingRecord, modelID, expectedAPI string) bool {
	return binding.ProviderID != "" && binding.CatalogModelID != "" && binding.PrismModelIDAtBind != "" &&
		binding.PrismModelIDAtBind == modelID && expectedAPI != "" && binding.API == expectedAPI
}

func piCandidateStatus(catalog *pidev.Catalog, expectedAPI, modelID string, liveCandidates []*pidev.Model) string {
	if catalog == nil {
		return "catalog_unavailable"
	}
	if expectedAPI == "" {
		return "api_mismatch"
	}
	switch len(liveCandidates) {
	case 0:
		return piZeroCandidateStatus(catalog, modelID)
	case 1:
		return "single"
	default:
		return "multiple"
	}
}

// piBindingStatus reports whether a persisted binding still matches live
// catalog evidence. It stays "bound" (benefit of the doubt) whenever the
// live fetch itself is unavailable: drift is only ever asserted from
// positive evidence, never from an absent check.
func piBindingStatus(catalog *pidev.Catalog, catalogStatus string, binding piBindingRecord) string {
	if catalog == nil || catalogStatus != "fresh" {
		return "bound"
	}
	model, found := catalog.Find(binding.ProviderID, binding.CatalogModelID)
	if !found || model.API != binding.API {
		return "bound_drifted"
	}
	_, sourceChanged := diffPiBindingSource(binding.Source, piBindingMetadataFromModel(model))
	if sourceChanged || renderPiDroppedFields(binding.DroppedFields) != renderPiDroppedFields(model.DroppedFields) {
		return "bound_drifted"
	}
	return "bound"
}

// piCatalogEvidenceForStatus projects the fetched catalog into the read wire
// block. A nil catalog (unavailable) keeps the status but drops revision and
// timestamps: the read must never present last-known-good or absent evidence
// as if it were a validated revision.
func piCatalogEvidenceForStatus(catalog *pidev.Catalog, status string) piCatalogReadWire {
	wire := piCatalogReadWire{Status: status}
	if catalog == nil {
		return wire
	}
	wire.Revision = catalog.Revision
	wire.MinimumVersion = catalog.MinimumVersion
	wire.ETag = catalog.ETag
	if !catalog.FetchedAt.IsZero() {
		fetchedAt := catalog.FetchedAt
		wire.FetchedAt = &fetchedAt
	}
	if !catalog.CheckedAt.IsZero() {
		checkedAt := catalog.CheckedAt
		wire.CheckedAt = &checkedAt
	}
	return wire
}

// buildPiModelReadResponse is the single-model projection behind
// GET /api/models/{model_config_id}/pi. It consumes one DB snapshot of the
// model row plus the persisted binding, plus the caller's best-effort catalog
// fetch, and computes the same live-candidate and binding-health axes as the
// export source — without loading targets, pricing, digests, credentials, or
// any runtime graph.
func buildPiModelReadResponse(record modelRecord, expectedAPI string, catalog *pidev.Catalog, catalogStatus string, binding piBindingRecord) piModelReadResponse {
	var liveCandidates []*pidev.Model
	if catalog != nil && expectedAPI != "" {
		liveCandidates = catalog.Candidates(record.ModelID, expectedAPI)
	}
	modelWire := piModelIdentityWire{
		ModelConfigID: record.ID,
		ModelID:       record.ModelID,
		APIFamily:     record.APIFamily,
		PiAPI:         expectedAPI,
	}
	candidateWires := piCandidateWiresFromModels(liveCandidates)
	bindingStatus := "unbound"
	bindingRenderable := false
	bindingWire := piBindingResponse{Bound: false}
	if binding.bound() {
		if piBindingMatchesModel(binding, record.ModelID, expectedAPI) {
			bindingStatus = piBindingStatus(catalog, catalogStatus, binding)
			bindingRenderable = true
		} else {
			bindingStatus = "bound_drifted"
		}
		bindingWire = binding.response()
	}
	return piModelReadResponse{
		Model:             modelWire,
		Catalog:           piCatalogEvidenceForStatus(catalog, catalogStatus),
		CandidateStatus:   piCandidateStatus(catalog, expectedAPI, record.ModelID, liveCandidates),
		Candidates:        candidateWires,
		BindingStatus:     bindingStatus,
		BindingRenderable: bindingRenderable,
		Binding:           bindingWire,
	}
}
