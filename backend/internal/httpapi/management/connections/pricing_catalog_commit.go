package connections

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

func (s *Service) handleCatalogPricingCommit(w http.ResponseWriter, r *http.Request) {
	var requestBody catalogPricingCommitRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if requestBody.SchemaVersion != catalogPricingImportSchemaVersion {
		writeDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("schema_version must be %d", catalogPricingImportSchemaVersion)})
		return
	}
	connectionIDs, err := normalizeConnectionIDs(requestBody.ConnectionIDs)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	expectedRevision := strings.TrimSpace(requestBody.ExpectedCatalogRevision)
	if expectedRevision == "" {
		writeDomainError(w, r, s.corsSnapshot(), &domainError{
			StatusCode: http.StatusUnprocessableEntity,
			Detail:     "expected_catalog_revision is required so stale catalog data cannot commit",
			Fields:     map[string]any{"field": "expected_catalog_revision"},
		})
		return
	}
	if strings.TrimSpace(requestBody.PreviewHash) == "" {
		writeDomainError(w, r, s.corsSnapshot(), &domainError{
			StatusCode: http.StatusUnprocessableEntity,
			Detail:     "preview_hash is required; run the catalog pricing preview first",
			Fields:     map[string]any{"field": "preview_hash"},
		})
		return
	}
	if !s.requireCatalogClient(w, r) {
		return
	}

	// The snapshot read is memory-only: no remote I/O ever happens inside the
	// write transaction, and a cold cache simply cannot match the expected
	// revision (fail closed instead of fetching mid-transaction).
	snapshot := s.catalog.Snapshot()
	if snapshot == nil || snapshot.ETag != expectedRevision {
		current := ""
		if snapshot != nil {
			current = snapshot.ETag
		}
		writeDomainError(w, r, s.corsSnapshot(), catalogStaleDomainError(expectedRevision, current))
		return
	}

	commitResponse, err := s.commitCatalogPricingInTransaction(r.Context(), r, requestBody, snapshot, connectionIDs)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, commitResponse)
}

func (s *Service) commitCatalogPricingInTransaction(ctx context.Context, r *http.Request, requestBody catalogPricingCommitRequest, snapshot *modelsdev.Catalog, connectionIDs []int) (catalogPricingCommitResponse, error) {
	return pgxutil.InTxValue(ctx, s.pool, "connection", func(tx pgx.Tx) (catalogPricingCommitResponse, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return catalogPricingCommitResponse{}, profileErr
		}
		if lockErr := lockProfileRow(ctx, tx, profile.ID); lockErr != nil {
			return catalogPricingCommitResponse{}, lockErr
		}
		offering, offerErr := resolveCatalogOffering(ctx, tx, profile.ID, catalogPricingPreviewRequest{
			ModelConfigID:  requestBody.ModelConfigID,
			ProviderID:     requestBody.ProviderID,
			CatalogModelID: requestBody.CatalogModelID,
		})
		if offerErr != nil {
			return catalogPricingCommitResponse{}, offerErr
		}
		model, exists := snapshot.Find(offering.ProviderID, offering.ModelID)
		if !exists {
			return catalogPricingCommitResponse{}, &domainError{
				StatusCode: http.StatusUnprocessableEntity,
				Detail:     "models_dev_offering_unknown: the requested provider/model pair does not exist in the catalog",
				Fields:     map[string]any{"provider_id": offering.ProviderID, "catalog_model_id": offering.ModelID},
			}
		}

		// Active reporting currency drives both the fail-closed USD gate and
		// the appended revision's currency attribution. Locked via the writers.
		var epochID int64
		var epochOrdinal int
		var epochCode string
		if err := tx.QueryRow(ctx, `SELECT epochs.id, epochs.epoch, epochs.currency_code FROM reporting_currency_epochs AS epochs JOIN user_settings AS settings ON settings.current_reporting_currency_epoch_id = epochs.id WHERE settings.profile_id = $1 AND epochs.superseded_at IS NULL FOR UPDATE OF settings`, profile.ID).Scan(&epochID, &epochOrdinal, &epochCode); err != nil {
			return catalogPricingCommitResponse{}, fmt.Errorf("lock active reporting currency epoch for profile %d: %w", profile.ID, err)
		}
		plan := modelsdev.BuildPricePlan(offering, model, epochCode)

		linked, linkErr := loadCatalogLinkedTemplate(ctx, tx, profile.ID, offering, true)
		if linkErr != nil {
			return catalogPricingCommitResponse{}, linkErr
		}
		plannedShape := pricingShapeFromPlan(plan)
		drift := linked != nil && !pricingTemplateShapesEqual(pricingTemplateShapeFromResponse(*linked), plannedShape)

		// Recompute every replay input from the CURRENT transactional state;
		// any movement since the preview breaks the hash below.
		targets, targetErr := loadCatalogTargetStates(ctx, tx, profile.ID, connectionIDs)
		if targetErr != nil {
			return catalogPricingCommitResponse{}, targetErr
		}
		hash, hashErr := hashCatalogPricingImport(newCatalogPricingHashInput(catalogPricingImportSchemaVersion, offering, snapshot.ETag, plan, linked, drift, targets))
		if hashErr != nil {
			return catalogPricingCommitResponse{}, hashErr
		}
		if hash != strings.TrimSpace(requestBody.PreviewHash) {
			return catalogPricingCommitResponse{}, &domainError{
				StatusCode: http.StatusConflict,
				Detail:     "models_dev_pricing_preview_stale: the catalog pricing preview no longer matches current state",
			}
		}
		// Fail closed on incompatible prices before any write happens: zero
		// rows may move when the plan is not committable.
		if !plan.Committable() {
			return catalogPricingCommitResponse{}, &domainError{
				StatusCode: http.StatusUnprocessableEntity,
				Detail:     "models_dev_pricing_incompatible: the catalog prices cannot be represented as a Prism pricing template",
				Fields:     map[string]any{"incompatibilities": plan.Incompatibilities},
			}
		}
		if drift && !requestBody.ConfirmDrift {
			return catalogPricingCommitResponse{}, &domainError{
				StatusCode: http.StatusConflict,
				Detail:     "models_dev_pricing_drift_unconfirmed: the source-linked template diverged from its current shape; explicit confirmation is required",
				Fields:     map[string]any{"confirm_required": true},
			}
		}

		now := s.nowUTC()
		catalogSource := &templateCatalogSource{
			ProviderID:      offering.ProviderID,
			CatalogModelID:  offering.ModelID,
			CatalogRevision: snapshot.ETag,
		}
		template := linked
		created, updated := false, false
		if template == nil {
			name, nameErr := dedupeCatalogTemplateName(ctx, tx, profile.ID, offering)
			if nameErr != nil {
				return catalogPricingCommitResponse{}, nameErr
			}
			createdTemplate, createErr := createPricingTemplateWithShape(ctx, tx, profile.ID, now, name, nil, plannedShape, catalogSource)
			if createErr != nil {
				return catalogPricingCommitResponse{}, createErr
			}
			template = &createdTemplate
			created = true
		} else if drift {
			if updateErr := updatePricingTemplateWithShape(ctx, tx, profile.ID, *template, template.Name, template.Description, plannedShape, now, catalogSource); updateErr != nil {
				return catalogPricingCommitResponse{}, updateErr
			}
			refreshed, found, refreshErr := loadPricingTemplate(ctx, tx, profile.ID, template.ID, false)
			if refreshErr != nil {
				return catalogPricingCommitResponse{}, refreshErr
			}
			if !found {
				return catalogPricingCommitResponse{}, fmt.Errorf("catalog import template %d disappeared during commit", template.ID)
			}
			template = &refreshed
			updated = true
		}

		// Assignment phase: sorted locks, existing double CAS per target, any
		// mismatch aborts the whole transaction.
		assigned := make([]int, 0, len(connectionIDs))
		for _, target := range targets {
			lockErr := lockAndAssignCatalogTarget(ctx, tx, profile.ID, target, template.ID, now)
			if lockErr != nil {
				return catalogPricingCommitResponse{}, lockErr
			}
			assigned = append(assigned, target.ConnectionID)
		}

		return catalogPricingCommitResponse{
			Created:        created,
			Updated:        updated,
			Assigned:       assigned,
			TemplateID:     template.ID,
			RevisionID:     template.RevisionID,
			Version:        template.Version,
			DriftConfirmed: drift && requestBody.ConfirmDrift,
		}, nil
	})
}
