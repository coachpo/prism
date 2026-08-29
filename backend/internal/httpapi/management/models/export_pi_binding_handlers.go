package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/pidev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

// Shared helpers for the Pi catalog binding surface (bind, refresh, override,
// unbind). These mirror the models.dev catalog binding helpers in
// catalog_handlers.go/catalog_remote.go; the two catalogs stay independent
// implementations by design (different trust models: Pi requires a
// SHA-256-verified body revision, models.dev trusts its ETag).

func newPiDomainError(statusCode int, detail string, fields map[string]any) error {
	return &domainError{StatusCode: statusCode, Detail: detail, Fields: fields}
}

func (s *Service) requirePiCatalogClient(w http.ResponseWriter, r *http.Request) bool {
	if s.piCatalog == nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "pi_catalog_client_missing: pi.dev catalog client is not configured")
		return false
	}
	return true
}

func piCatalogFetchFailed(err error) error {
	return &domainError{
		StatusCode: http.StatusBadGateway,
		Detail:     fmt.Sprintf("pi_catalog_unavailable: %v", err),
	}
}

func piCatalogStaleError(expected, current string) error {
	return &domainError{
		StatusCode: http.StatusConflict,
		Detail:     "pi_catalog_stale: the previewed catalog revision no longer matches current data",
		Fields: map[string]any{
			"expected_catalog_revision": expected,
			"current_catalog_revision":  current,
		},
	}
}

func piZeroCandidateStatus(catalog *pidev.Catalog, modelID string) string {
	if catalog.HasExactID(modelID) {
		return "api_mismatch"
	}
	return "not_in_catalog"
}

func piCandidateWiresFromModels(candidates []*pidev.Model) []piCandidateWire {
	wires := make([]piCandidateWire, 0, len(candidates))
	for _, candidate := range candidates {
		wires = append(wires, piCandidateWire{
			ProviderID:       candidate.ProviderID,
			ModelID:          candidate.ModelID,
			API:              candidate.API,
			Name:             candidate.Name,
			Reasoning:        candidate.Reasoning,
			Input:            candidate.Input,
			ContextWindow:    candidate.ContextWindow,
			MaxTokens:        candidate.MaxTokens,
			ThinkingLevelMap: candidate.ThinkingLevelMap,
			Compat:           candidate.Compat,
		})
	}
	return wires
}

// piBindingMetadataFromModel projects a validated pi.dev catalog entry into
// the binding storage shape. Only the seven safe leaves are copied; cost,
// headers, samplingParams, fallback, and routing never reach this struct
// because pidev.Model itself never parses them into typed fields.
func piBindingMetadataFromModel(model *pidev.Model) piBindingMetadata {
	if model == nil {
		return piBindingMetadata{}
	}
	return piBindingMetadata{
		Name:             copyStringPtr(model.Name),
		Reasoning:        model.Reasoning,
		Input:            cloneStringSlice(model.Input),
		ContextWindow:    copyInt64Ptr(model.ContextWindow),
		MaxTokens:        copyInt64Ptr(model.MaxTokens),
		ThinkingLevelMap: cloneThinkingLevelMap(model.ThinkingLevelMap),
		Compat:           cloneCompat(model.Compat),
	}
}

// piBindingPlatformCandidate converts one binding's effective (source with
// override applied) metadata into the modelexport.PlatformCandidate shape
// RenderPi consumes. This is the render-time counterpart of
// piBindingMetadataFromModel: it never touches the live catalog, only the
// persisted, already-validated binding row.
func piBindingPlatformCandidate(effective piBindingMetadata) modelexport.PlatformCandidate {
	candidate := modelexport.PlatformCandidate{Metadata: modelexport.MetadataLayer{}, DerivedFields: map[string]json.RawMessage{}}
	values := map[string]json.RawMessage{}
	if effective.Name != nil && strings.TrimSpace(*effective.Name) != "" {
		values[modelexport.MetaName] = marshalRawJSON(*effective.Name)
	}
	if effective.Reasoning != nil {
		values[modelexport.MetaReasoning] = marshalRawJSON(*effective.Reasoning)
	}
	if effective.Input != nil {
		values[modelexport.MetaModalitiesInput] = marshalRawJSON(effective.Input)
	}
	if effective.ContextWindow != nil {
		values[modelexport.MetaContextWindow] = marshalRawJSON(*effective.ContextWindow)
	}
	if effective.MaxTokens != nil {
		values[modelexport.MetaMaxOutputTokens] = marshalRawJSON(*effective.MaxTokens)
	}
	candidate.Metadata = modelexport.NewMetadataLayer(values)
	if effective.ThinkingLevelMap != nil {
		candidate.DerivedFields["thinkingLevelMap"] = marshalRawJSON(effective.ThinkingLevelMap)
	}
	if effective.Compat != nil {
		candidate.DerivedFields["compat"] = marshalRawJSON(effective.Compat)
	}
	return candidate
}

// loadModelForPi resolves the profile-scoped model record and reports its
// expected Pi API compatibility alongside it. Unknown ids and API families
// with no Pi mapping both fail before any catalog work happens.
func (s *Service) loadModelForPi(ctx context.Context, r *http.Request, modelConfigID int) (modelRecord, string, error) {
	record, err := pgxutil.InReadOnlyTxValue(ctx, s.pool, "model", func(tx pgx.Tx) (modelRecord, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return modelRecord{}, profileErr
		}
		return loadModelForCatalog(ctx, tx, profile.ID, modelConfigID)
	})
	if err != nil {
		return modelRecord{}, "", err
	}
	expectedAPI := piExpectedAPI(record.APIFamily, record.OpenAIAcceptedFormat)
	if expectedAPI == "" {
		return record, "", newPiDomainError(http.StatusUnprocessableEntity, fmt.Sprintf("pi_api_family_unsupported: api_family %q has no Pi API mapping", record.APIFamily), map[string]any{"api_family": record.APIFamily})
	}
	return record, expectedAPI, nil
}

// loadBoundPiBinding loads a binding and reports whether the surface may
// proceed; unbound models reject refresh/override flows instead of silently
// creating one.
func loadBoundPiBinding(ctx context.Context, exec queryExecutor, profileID, modelConfigID int, out *piBindingRecord) (bool, error) {
	binding, found, err := loadPiBinding(ctx, exec, profileID, modelConfigID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, newPiDomainError(http.StatusConflict, "pi_not_bound: bind a pi.dev candidate before refreshing or overriding metadata", nil)
	}
	*out = binding
	return true, nil
}
