package connections

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

// catalogPricingImportSchemaVersion pins the catalog import contract. It is
// independent from the JSON import schema so the two contracts can evolve
// without touching each other.
const catalogPricingImportSchemaVersion = 1

// catalogPricingTargetLimit bounds how many Terminal Targets one assignment
// may address in a single atomic commit.
const catalogPricingTargetLimit = 50

type catalogPricingPreviewRequest struct {
	ModelConfigID  *int   `json:"model_config_id"`
	ProviderID     string `json:"provider_id"`
	CatalogModelID string `json:"catalog_model_id"`
	ConnectionIDs  []int  `json:"connection_ids"`
}

// catalogPricingCommitRequest replays the preview payload (offering
// resolution + connection ids) together with the guards that make stale or
// conflicting commits impossible.
type catalogPricingCommitRequest struct {
	SchemaVersion           int    `json:"schema_version"`
	ModelConfigID           *int   `json:"model_config_id"`
	ProviderID              string `json:"provider_id"`
	CatalogModelID          string `json:"catalog_model_id"`
	ConnectionIDs           []int  `json:"connection_ids"`
	PreviewHash             string `json:"preview_hash"`
	ExpectedCatalogRevision string `json:"expected_catalog_revision"`
	ConfirmDrift            bool   `json:"confirm_drift"`
}

type catalogOfferingPayload struct {
	ProviderID     string  `json:"provider_id"`
	CatalogModelID string  `json:"catalog_model_id"`
	Name           string  `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`
	Family         *string `json:"family,omitempty"`
	Status         *string `json:"status,omitempty"`
	OpenWeights    *bool   `json:"open_weights,omitempty"`
}

type catalogPriceCardPayload struct {
	InputPrice         string  `json:"input_price"`
	OutputPrice        string  `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
}

type catalogPricePlanPayload struct {
	TemplateKind      string                             `json:"template_kind"`
	Cards             map[string]catalogPriceCardPayload `json:"cards"`
	TierThreshold     *int64                             `json:"tier_input_tokens_above,omitempty"`
	Incompatibilities []modelsdev.Incompatibility        `json:"incompatibilities"`
}

type catalogLinkedTemplatePayload struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Version      int    `json:"version"`
	RevisionID   int64  `json:"revision_id"`
	TemplateKind string `json:"template_kind"`
	UpdatedAt    string `json:"updated_at"`
}

type catalogTargetState struct {
	ConnectionID      int       `json:"connection_id"`
	Name              *string   `json:"name"`
	EndpointName      *string   `json:"endpoint_name"`
	PricingTemplateID *int      `json:"pricing_template_id"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type catalogPricingPreviewResponse struct {
	SchemaVersion         int                           `json:"schema_version"`
	Offering              catalogOfferingPayload        `json:"offering"`
	CatalogRevision       string                        `json:"catalog_revision"`
	FetchedAt             time.Time                     `json:"fetched_at"`
	Plan                  catalogPricePlanPayload       `json:"plan"`
	Template              *catalogLinkedTemplatePayload `json:"template,omitempty"`
	Action                string                        `json:"action"` // create | reuse | drift
	Drift                 bool                          `json:"drift"`
	Committable           bool                          `json:"committable"`
	PreviewHash           string                        `json:"preview_hash,omitempty"`
	Targets               []catalogTargetState          `json:"targets"`
	ReportingCurrencyCode string                        `json:"reporting_currency_code"`
}

type catalogPricingCommitResponse struct {
	Created        bool  `json:"created"`
	Updated        bool  `json:"updated"`
	Assigned       []int `json:"assigned_connection_ids"`
	TemplateID     int   `json:"template_id"`
	RevisionID     int64 `json:"revision_id"`
	Version        int   `json:"version"`
	DriftConfirmed bool  `json:"drift_confirmed"`
}

func (s *Service) requireCatalogClient(w http.ResponseWriter, r *http.Request) bool {
	if s.catalog == nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "models_dev_catalog_client_missing: catalog client is not configured")
		return false
	}
	return true
}

func isCatalogUnavailableErr(err error) bool {
	return errors.Is(err, modelsdev.ErrCatalogUnavailable)
}

func catalogFetchDomainError(err error) error {
	return &domainError{
		StatusCode: http.StatusBadGateway,
		Detail:     fmt.Sprintf("models_dev_catalog_unavailable: %v", err),
	}
}

func catalogStaleDomainError(expected, current string) error {
	return &domainError{
		StatusCode: http.StatusConflict,
		Detail:     "models_dev_catalog_stale: the previewed catalog revision no longer matches current data",
		Fields: map[string]any{
			"expected_catalog_revision": expected,
			"current_catalog_revision":  current,
		},
	}
}

// fetchCatalogOutsideTx performs the network round trip strictly before any
// database transaction begins.
func (s *Service) fetchCatalogOutsideTx(ctx context.Context) (*modelsdev.Catalog, error) {
	catalog, err := s.catalog.Fetch(ctx)
	if err != nil {
		if isCatalogUnavailableErr(err) {
			return nil, catalogFetchDomainError(err)
		}
		return nil, err
	}
	return catalog, nil
}

func normalizeConnectionIDs(raw []int) ([]int, error) {
	seen := map[int]struct{}{}
	ids := make([]int, 0, len(raw))
	for _, id := range raw {
		if id <= 0 {
			return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "connection_ids must carry positive connection ids"}
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Ints(ids)
	if len(ids) > catalogPricingTargetLimit {
		return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("connection_ids must not exceed %d targets", catalogPricingTargetLimit)}
	}
	return ids, nil
}

// resolveCatalogOffering turns the request into offering coordinates: either
// explicit provider+model or the model's persisted binding.
func resolveCatalogOffering(ctx context.Context, tx pgx.Tx, profileID int, requestBody catalogPricingPreviewRequest) (modelsdev.Offering, error) {
	providerID := strings.TrimSpace(requestBody.ProviderID)
	catalogModelID := strings.TrimSpace(requestBody.CatalogModelID)
	if providerID != "" || catalogModelID != "" {
		if providerID == "" || catalogModelID == "" {
			return modelsdev.Offering{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "provider_id and catalog_model_id must be provided together"}
		}
		return modelsdev.Offering{ProviderID: providerID, ModelID: catalogModelID}, nil
	}
	if requestBody.ModelConfigID == nil || *requestBody.ModelConfigID <= 0 {
		return modelsdev.Offering{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "either model_config_id or an explicit provider_id + catalog_model_id pair is required"}
	}
	binding, found, err := loadCatalogBindingForProfile(ctx, tx, profileID, *requestBody.ModelConfigID)
	if err != nil {
		return modelsdev.Offering{}, err
	}
	if !found {
		return modelsdev.Offering{}, &domainError{StatusCode: http.StatusConflict, Detail: "models_dev_not_bound: bind the model to a catalog offering before generating prices"}
	}
	return modelsdev.Offering{ProviderID: binding.ProviderID, ModelID: binding.CatalogModelID}, nil
}

type catalogBindingCoordinates struct {
	ProviderID     string
	CatalogModelID string
}

func loadCatalogBindingForProfile(ctx context.Context, exec queryExecutor, profileID, modelConfigID int) (catalogBindingCoordinates, bool, error) {
	var binding catalogBindingCoordinates
	err := exec.QueryRow(ctx,
		`SELECT bindings.provider_id, bindings.catalog_model_id
		   FROM model_catalog_bindings AS bindings
		   JOIN model_configs AS configs ON configs.id = bindings.model_config_id
		  WHERE bindings.model_config_id = $1 AND configs.profile_id = $2`,
		modelConfigID, profileID).Scan(&binding.ProviderID, &binding.CatalogModelID)
	if err == pgx.ErrNoRows {
		return catalogBindingCoordinates{}, false, nil
	}
	if err != nil {
		return catalogBindingCoordinates{}, false, fmt.Errorf("load catalog binding for model %d: %w", modelConfigID, err)
	}
	return binding, true, nil
}

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

// catalogPricingHashInput is the canonical replay identity of one preview:
// offering, revision, plan, linked-template state, drift flag, and every
// target's CAS-relevant columns.
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

	type resolvedScope struct {
		profileID int
		offering  modelsdev.Offering
	}
	scope, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (resolvedScope, error) {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return resolvedScope{}, profileErr
		}
		offering, offerErr := resolveCatalogOffering(r.Context(), tx, profile.ID, requestBody)
		if offerErr != nil {
			return resolvedScope{}, offerErr
		}
		return resolvedScope{profileID: profile.ID, offering: offering}, nil
	})
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

	preview, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (catalogPricingPreviewResponse, error) {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return catalogPricingPreviewResponse{}, profileErr
		}
		currencyCode, epochErr := activeReportingCurrencyCode(r.Context(), tx, profile.ID)
		if epochErr != nil {
			return catalogPricingPreviewResponse{}, epochErr
		}
		plan := modelsdev.BuildPricePlan(scope.offering, model, currencyCode)
		linked, linkErr := loadCatalogLinkedTemplate(r.Context(), tx, profile.ID, scope.offering, false)
		if linkErr != nil {
			return catalogPricingPreviewResponse{}, linkErr
		}
		drift := linked != nil && !pricingTemplateShapesEqual(pricingTemplateShapeFromResponse(*linked), pricingShapeFromPlan(plan))
		targets, targetErr := loadCatalogTargetStates(r.Context(), tx, profile.ID, connectionIDs)
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
			SchemaVersion:   catalogPricingImportSchemaVersion,
			Offering:        offeringPayloadFrom(model),
			CatalogRevision: catalog.ETag,
			FetchedAt:       catalog.FetchedAt,
			Plan: catalogPricePlanPayload{
				TemplateKind:      plan.Kind,
				Cards:             priceCardsPayload(plan),
				TierThreshold:     plan.TierThreshold,
				Incompatibilities: plan.Incompatibilities,
			},
			Drift:                 drift,
			Committable:           plan.Committable(),
			PreviewHash:           hash,
			Targets:               targets,
			ReportingCurrencyCode: currencyCode,
			Action:                action,
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
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, preview)
}

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

	commitResponse, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (catalogPricingCommitResponse, error) {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return catalogPricingCommitResponse{}, profileErr
		}
		if lockErr := lockProfileRow(r.Context(), tx, profile.ID); lockErr != nil {
			return catalogPricingCommitResponse{}, lockErr
		}
		offering, offerErr := resolveCatalogOffering(r.Context(), tx, profile.ID, catalogPricingPreviewRequest{
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
		if err := tx.QueryRow(r.Context(), `SELECT epochs.id, epochs.epoch, epochs.currency_code FROM reporting_currency_epochs AS epochs JOIN user_settings AS settings ON settings.current_reporting_currency_epoch_id = epochs.id WHERE settings.profile_id = $1 AND epochs.superseded_at IS NULL FOR UPDATE OF settings`, profile.ID).Scan(&epochID, &epochOrdinal, &epochCode); err != nil {
			return catalogPricingCommitResponse{}, fmt.Errorf("lock active reporting currency epoch for profile %d: %w", profile.ID, err)
		}
		plan := modelsdev.BuildPricePlan(offering, model, epochCode)

		linked, linkErr := loadCatalogLinkedTemplate(r.Context(), tx, profile.ID, offering, true)
		if linkErr != nil {
			return catalogPricingCommitResponse{}, linkErr
		}
		plannedShape := pricingShapeFromPlan(plan)
		drift := linked != nil && !pricingTemplateShapesEqual(pricingTemplateShapeFromResponse(*linked), plannedShape)

		// Recompute every replay input from the CURRENT transactional state;
		// any movement since the preview breaks the hash below.
		targets, targetErr := loadCatalogTargetStates(r.Context(), tx, profile.ID, connectionIDs)
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
			name, nameErr := dedupeCatalogTemplateName(r.Context(), tx, profile.ID, offering)
			if nameErr != nil {
				return catalogPricingCommitResponse{}, nameErr
			}
			createdTemplate, createErr := createPricingTemplateWithShape(r.Context(), tx, profile.ID, now, name, nil, plannedShape, catalogSource)
			if createErr != nil {
				return catalogPricingCommitResponse{}, createErr
			}
			template = &createdTemplate
			created = true
		} else if drift {
			if updateErr := updatePricingTemplateWithShape(r.Context(), tx, profile.ID, *template, template.Name, template.Description, plannedShape, now, catalogSource); updateErr != nil {
				return catalogPricingCommitResponse{}, updateErr
			}
			refreshed, found, refreshErr := loadPricingTemplate(r.Context(), tx, profile.ID, template.ID, false)
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
			lockErr := lockAndAssignCatalogTarget(r.Context(), tx, profile.ID, target, template.ID, now)
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
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, commitResponse)
}

// dedupeCatalogTemplateName derives the source-linked template name from the
// offering coordinates and appends numeric suffixes until free.
func dedupeCatalogTemplateName(ctx context.Context, exec queryExecutor, profileID int, offering modelsdev.Offering) (string, error) {
	base := offering.ProviderID + "/" + offering.ModelID
	candidate := base
	for attempt := 1; attempt <= 50; attempt++ {
		var existingID int
		err := exec.QueryRow(ctx, `SELECT id FROM pricing_templates WHERE profile_id = $1 AND name = $2 AND deleted_at IS NULL LIMIT 1`, profileID, candidate).Scan(&existingID)
		if err == pgx.ErrNoRows {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("check catalog template name availability for %q: %w", candidate, err)
		}
		candidate = fmt.Sprintf("%s (%d)", base, attempt+1)
	}
	return "", &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("unable to derive a free catalog template name from %q", base)}
}

// lockAndAssignCatalogTarget enforces the existing Terminal Target double CAS
// (updated_at + pricing_template_id) under a sorted row lock.
func lockAndAssignCatalogTarget(ctx context.Context, tx pgx.Tx, profileID int, expected catalogTargetState, templateID int, currentTime time.Time) error {
	var currentUpdatedAt time.Time
	var currentTemplateID *int
	err := tx.QueryRow(ctx,
		`SELECT updated_at, pricing_template_id FROM connections WHERE id = $1 AND profile_id = $2 FOR UPDATE`,
		expected.ConnectionID, profileID).Scan(&currentUpdatedAt, &currentTemplateID)
	if err == pgx.ErrNoRows {
		return &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("Terminal Target %d disappeared; assignment aborted", expected.ConnectionID)}
	}
	if err != nil {
		return fmt.Errorf("lock Terminal Target %d for catalog assignment: %w", expected.ConnectionID, err)
	}
	if !currentUpdatedAt.Equal(expected.UpdatedAt) {
		return &domainError{
			StatusCode: http.StatusConflict,
			Detail:     fmt.Sprintf("Terminal Target %d changed since the preview; assignment aborted", expected.ConnectionID),
			Fields:     map[string]any{"pricing_cas_conflict": true, "connection_id": expected.ConnectionID},
		}
	}
	if (currentTemplateID == nil) != (expected.PricingTemplateID == nil) ||
		(currentTemplateID != nil && expected.PricingTemplateID != nil && *currentTemplateID != *expected.PricingTemplateID) {
		return &domainError{
			StatusCode: http.StatusConflict,
			Detail:     fmt.Sprintf("Terminal Target %d references a different pricing template since the preview; assignment aborted", expected.ConnectionID),
			Fields:     map[string]any{"pricing_cas_conflict": true, "connection_id": expected.ConnectionID},
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE connections SET pricing_template_id = $2, updated_at = $3 WHERE id = $1`, expected.ConnectionID, templateID, currentTime); err != nil {
		return fmt.Errorf("assign catalog template to Terminal Target %d: %w", expected.ConnectionID, err)
	}
	return nil
}
