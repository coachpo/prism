package connections

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

func offeringPayloadFrom(model *modelsdev.Model) catalogOfferingPayload {
	return catalogOfferingPayload{
		ProviderID:     model.ProviderID,
		CatalogModelID: model.ModelID,
		Name:           model.Name,
		Description:    model.Description,
		Family:         model.Family,
		Status:         model.Status,
		OpenWeights:    model.OpenWeights,
	}
}

func priceCardsPayload(plan modelsdev.PricePlan) map[string]catalogPriceCardPayload {
	cards := make(map[string]catalogPriceCardPayload, len(plan.Cards))
	for role, card := range plan.Cards {
		cards[role] = catalogPriceCardPayload{
			InputPrice:         card.InputPrice,
			OutputPrice:        card.OutputPrice,
			CachedInputPrice:   cloneString(card.CachedInputPrice),
			CacheCreationPrice: cloneString(card.CacheCreationPrice),
			ReasoningPrice:     cloneString(card.ReasoningPrice),
		}
	}
	return cards
}

// pricingShapeFromPlan converts a committable catalog plan into the shared
// typed template shape used by every writer.
func pricingShapeFromPlan(plan modelsdev.PricePlan) pricingTemplateShape {
	shape := pricingTemplateShape{Kind: pricingkind.Kind(plan.Kind), Cards: map[string]pricingTemplateCard{}}
	for role, card := range plan.Cards {
		shape.Cards[role] = pricingTemplateCard{
			InputPrice:         card.InputPrice,
			OutputPrice:        card.OutputPrice,
			CachedInputPrice:   cloneString(card.CachedInputPrice),
			CacheCreationPrice: cloneString(card.CacheCreationPrice),
			ReasoningPrice:     cloneString(card.ReasoningPrice),
		}
	}
	if plan.TierThreshold != nil {
		threshold := int(*plan.TierThreshold)
		shape.TierThreshold = &threshold
	}
	return shape
}

// activeReportingCurrencyCode reads the current epoch's currency code without
// locking; writers re-load it FOR UPDATE inside their own transactions.
func activeReportingCurrencyCode(ctx context.Context, exec queryExecutor, profileID int) (string, error) {
	var currencyCode string
	err := exec.QueryRow(ctx,
		`SELECT epochs.currency_code FROM reporting_currency_epochs AS epochs
		 JOIN user_settings AS settings ON settings.current_reporting_currency_epoch_id = epochs.id
		 WHERE settings.profile_id = $1 AND epochs.superseded_at IS NULL`, profileID).Scan(&currencyCode)
	if err != nil {
		return "", fmt.Errorf("load active reporting currency epoch for profile %d: %w", profileID, err)
	}
	return currencyCode, nil
}

// loadCatalogLinkedTemplate finds the single source-linked template of an
// offering, or nil when the offering has no template yet. The commit caller
// locks the row; previews pass forUpdate=false so they stay legal inside
// read-only transactions.
func loadCatalogLinkedTemplate(ctx context.Context, exec queryExecutor, profileID int, offering modelsdev.Offering, forUpdate bool) (*pricingTemplateResponse, error) {
	var templateID int
	err := exec.QueryRow(ctx,
		`SELECT id FROM pricing_templates
		  WHERE profile_id = $1 AND catalog_provider_id = $2 AND catalog_model_id = $3 AND deleted_at IS NULL`,
		profileID, offering.ProviderID, offering.ModelID).Scan(&templateID)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find catalog-linked pricing template for %s/%s: %w", offering.ProviderID, offering.ModelID, err)
	}
	template, found, err := loadPricingTemplate(ctx, exec, profileID, templateID, forUpdate)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &template, nil
}

func loadCatalogTargetStates(ctx context.Context, exec queryExecutor, profileID int, connectionIDs []int) ([]catalogTargetState, error) {
	states := make([]catalogTargetState, 0, len(connectionIDs))
	for _, connectionID := range connectionIDs {
		var state catalogTargetState
		state.ConnectionID = connectionID
		err := exec.QueryRow(ctx,
			`SELECT connections.name, endpoints.name, connections.pricing_template_id, connections.updated_at
			   FROM connections
			   LEFT JOIN endpoints ON endpoints.id = connections.endpoint_id
			  WHERE connections.id = $1 AND connections.profile_id = $2`,
			connectionID, profileID).Scan(&state.Name, &state.EndpointName, &state.PricingTemplateID, &state.UpdatedAt)
		if err == pgx.ErrNoRows {
			return nil, &domainError{StatusCode: http.StatusNotFound, Detail: fmt.Sprintf("Terminal Target %d does not exist in this profile", connectionID)}
		}
		if err != nil {
			return nil, fmt.Errorf("load Terminal Target %d for catalog assignment: %w", connectionID, err)
		}
		states = append(states, state)
	}
	return states, nil
}

// buildCatalogPricingPreview performs the second read-only phase after the
// catalog snapshot has been fetched. Plan construction and all projection
// reads remain separate from the network call and do not write state.
func (s *Service) buildCatalogPricingPreview(ctx context.Context, r *http.Request, scope catalogPricingPreviewScope, catalog *modelsdev.Catalog, model *modelsdev.Model, connectionIDs []int) (catalogPricingPreviewResponse, error) {
	return pgxutil.InReadOnlyTxValue(ctx, s.pool, "connection", func(tx pgx.Tx) (catalogPricingPreviewResponse, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return catalogPricingPreviewResponse{}, profileErr
		}
		currencyCode, epochErr := activeReportingCurrencyCode(ctx, tx, profile.ID)
		if epochErr != nil {
			return catalogPricingPreviewResponse{}, epochErr
		}
		plan := modelsdev.BuildPricePlan(scope.offering, model, currencyCode)
		linked, linkErr := loadCatalogLinkedTemplate(ctx, tx, profile.ID, scope.offering, false)
		if linkErr != nil {
			return catalogPricingPreviewResponse{}, linkErr
		}
		drift := linked != nil && !pricingTemplateShapesEqual(pricingTemplateShapeFromResponse(*linked), pricingShapeFromPlan(plan))
		targets, targetErr := loadCatalogTargetStates(ctx, tx, profile.ID, connectionIDs)
		if targetErr != nil {
			return catalogPricingPreviewResponse{}, targetErr
		}
		hash, hashErr := hashCatalogPricingImport(newCatalogPricingHashInput(catalogPricingImportSchemaVersion, scope.offering, catalog.ETag, plan, linked, drift, targets))
		if hashErr != nil {
			return catalogPricingPreviewResponse{}, hashErr
		}
		action := "create"
		if linked != nil {
			action = "reuse"
			if drift {
				action = "drift"
			}
		}
		response := catalogPricingPreviewResponse{
			SchemaVersion:         catalogPricingImportSchemaVersion,
			Offering:              offeringPayloadFrom(model),
			Model:                 scope.prismModel,
			CatalogRevision:       catalog.ETag,
			FetchedAt:             catalog.FetchedAt,
			ReportingCurrencyCode: currencyCode,
			CatalogCurrency:       modelsdev.CatalogPriceCurrency,
			PricingUnit:           pricingkind.UnitPer1M,
			Drift:                 drift,
			Committable:           plan.Committable(),
			PreviewHash:           hash,
			Targets:               targets,
			Action:                action,
			Plan: catalogPricePlanPayload{
				TemplateKind:      plan.Kind,
				Cards:             priceCardsPayload(plan),
				TierThreshold:     plan.TierThreshold,
				Incompatibilities: plan.Incompatibilities,
			},
		}
		if linked != nil {
			response.Template = &catalogLinkedTemplatePayload{
				ID: linked.ID, Name: linked.Name, Version: linked.Version,
				RevisionID: linked.RevisionID, TemplateKind: linked.TemplateKind,
				UpdatedAt: linked.UpdatedAt.UTC().Format(time.RFC3339Nano),
			}
		}
		return response, nil
	})
}
