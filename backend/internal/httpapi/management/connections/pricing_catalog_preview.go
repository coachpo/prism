package connections

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
)

type catalogPricingHashInput struct {
	SchemaVersion   int                           `json:"schema_version"`
	Offering        modelsdev.Offering            `json:"offering"`
	CatalogRevision string                        `json:"catalog_revision"`
	Plan            catalogPricePlanPayload       `json:"plan"`
	Template        *catalogLinkedTemplatePayload `json:"template"`
	Drift           bool                          `json:"drift"`
	Targets         []catalogTargetHashRow        `json:"targets"`
}

type catalogTargetHashRow struct {
	ConnectionID      int    `json:"connection_id"`
	PricingTemplateID *int   `json:"pricing_template_id"`
	UpdatedAt         string `json:"updated_at"`
}

func newCatalogPricingHashInput(schemaVersion int, offering modelsdev.Offering, revision string, plan modelsdev.PricePlan, linked *pricingTemplateResponse, drift bool, targets []catalogTargetState) catalogPricingHashInput {
	input := catalogPricingHashInput{
		SchemaVersion:   schemaVersion,
		Offering:        offering,
		CatalogRevision: revision,
		Plan: catalogPricePlanPayload{
			TemplateKind:      plan.Kind,
			Cards:             priceCardsPayload(plan),
			TierThreshold:     plan.TierThreshold,
			Incompatibilities: plan.Incompatibilities,
		},
		Drift:   drift,
		Targets: make([]catalogTargetHashRow, 0, len(targets)),
	}
	if linked != nil {
		input.Template = &catalogLinkedTemplatePayload{
			ID: linked.ID, Name: linked.Name, Version: linked.Version,
			RevisionID: linked.RevisionID, TemplateKind: linked.TemplateKind,
			UpdatedAt: linked.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}
	}
	for _, target := range targets {
		input.Targets = append(input.Targets, catalogTargetHashRow{
			ConnectionID:      target.ConnectionID,
			PricingTemplateID: target.PricingTemplateID,
			UpdatedAt:         target.UpdatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return input
}

func hashCatalogPricingImport(input catalogPricingHashInput) (string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("hash catalog pricing import: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:]), nil
}

func (s *Service) handleCatalogPricingPreview(w http.ResponseWriter, r *http.Request) {
	var requestBody catalogPricingPreviewRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	connectionIDs, err := normalizeConnectionIDs(requestBody.ConnectionIDs)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if !s.requireCatalogClient(w, r) {
		return
	}

	scope, err := s.resolveCatalogPricingPreviewScope(r.Context(), r, requestBody)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	// Remote I/O stays outside transactions.
	catalog, err := s.fetchCatalogOutsideTx(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	model, exists := catalog.Find(scope.offering.ProviderID, scope.offering.ModelID)
	if !exists {
		writeDomainError(w, r, s.corsSnapshot(), &domainError{
			StatusCode: http.StatusUnprocessableEntity,
			Detail:     "models_dev_offering_unknown: the requested provider/model pair does not exist in the catalog",
			Fields:     map[string]any{"provider_id": scope.offering.ProviderID, "catalog_model_id": scope.offering.ModelID},
		})
		return
	}

	preview, err := s.buildCatalogPricingPreview(r.Context(), r, scope, catalog, model, connectionIDs)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, preview)
}
