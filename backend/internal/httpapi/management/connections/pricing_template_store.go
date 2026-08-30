package connections

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/jackc/pgx/v5"
)

type pricingTemplateCard struct {
	InputPrice         string  `json:"input_price"`
	OutputPrice        string  `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
}

type pricingTemplateWindow struct {
	WeekdayMask int `json:"weekday_mask"`
	StartMinute int `json:"start_minute"`
	EndMinute   int `json:"end_minute"`
}

type pricingTemplateSchedule struct {
	Timezone string                  `json:"timezone"`
	Windows  []pricingTemplateWindow `json:"windows"`
}

type pricingTemplateConnectionUsageRecord struct {
	ConnectionID   int
	ConnectionName *string
	ModelConfigID  int
	ModelID        *string
	EndpointID     int
	EndpointName   *string
}

type pricingTemplateResponse struct {
	ID                  int                      `json:"id"`
	ProfileID           int                      `json:"profile_id"`
	Name                string                   `json:"name"`
	Description         *string                  `json:"description"`
	PricingUnit         string                   `json:"pricing_unit"`
	PricingCurrencyCode string                   `json:"pricing_currency_code"`
	TemplateKind        string                   `json:"template_kind"`
	Card                *pricingTemplateCard     `json:"card,omitempty"`
	BaseCard            *pricingTemplateCard     `json:"base_card,omitempty"`
	Tier                *pricingTemplateTier     `json:"tier,omitempty"`
	PeakCard            *pricingTemplateCard     `json:"peak_card,omitempty"`
	OffpeakCard         *pricingTemplateCard     `json:"offpeak_card,omitempty"`
	Schedule            *pricingTemplateSchedule `json:"schedule,omitempty"`
	// Internal normalized card/window shape used to compare and persist full revisions.
	cards                  map[string]pricingTemplateCard
	windows                []terminaltarget.Window
	scheduleDigest         *string
	Version                int        `json:"version"`
	RevisionID             int64      `json:"revision_id"`
	VersionEffectiveAt     *time.Time `json:"version_effective_at"`
	ReportingCurrencyEpoch *int       `json:"reporting_currency_epoch"`
	ActiveCurrencySymbol   string     `json:"active_currency_symbol"`
	DeletedAt              *time.Time `json:"deleted_at,omitempty"`
	RevisionCount          int64      `json:"revision_count"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	// Catalog coordinates name the models.dev offering these prices were
	// imported from. Both stay null on every manually authored template.
	CatalogProviderID *string `json:"catalog_provider_id"`
	CatalogModelID    *string `json:"catalog_model_id"`
	// RevisionSource/CatalogRevision describe the current revision only:
	// "manual" with a null revision, or "catalog" with the catalog revision
	// the import was replayed against.
	RevisionSource  string  `json:"revision_source"`
	CatalogRevision *string `json:"catalog_revision"`
}

const pricingTemplateSelectQuery = `SELECT templates.id, templates.profile_id, templates.name, templates.description,
			templates.created_at, templates.updated_at, templates.deleted_at,
			templates.catalog_provider_id, templates.catalog_model_id,
			revisions.id, revisions.version, revisions.pricing_unit, revisions.currency_code,
			revisions.reporting_currency_epoch, revisions.template_kind, revisions.tier_input_tokens_above,
			revisions.pricing_schedule_timezone, revisions.pricing_schedule_digest, revisions.effective_at,
			revisions.revision_source, revisions.catalog_revision,
			epochs.currency_symbol,
			(SELECT count(*) FROM pricing_template_revisions AS all_revisions WHERE all_revisions.template_id = templates.id) AS revision_count
		FROM pricing_templates AS templates
		LEFT JOIN pricing_template_revisions AS revisions ON revisions.id = templates.current_revision_id
		LEFT JOIN reporting_currency_epochs AS epochs ON epochs.id = revisions.reporting_currency_epoch_id`

func ensureUniquePricingTemplateName(ctx context.Context, exec queryExecutor, profileID int, templateName string, excludeID *int) error {
	var existingID int
	err := exec.QueryRow(ctx, `SELECT id FROM pricing_templates WHERE profile_id = $1 AND name = $2 AND deleted_at IS NULL LIMIT 1`, profileID, templateName).Scan(&existingID)
	if err == nil {
		if excludeID != nil && existingID == *excludeID {
			return nil
		}
		return &DomainError{StatusCode: 409, Detail: fmt.Sprintf("Pricing template name '%s' already exists", templateName)}
	}
	if err == pgx.ErrNoRows {
		return nil
	}
	return fmt.Errorf("query pricing template name availability for %q: %w", templateName, err)
}

func loadPricingTemplate(ctx context.Context, exec queryExecutor, profileID int, templateID int, forUpdate bool) (pricingTemplateResponse, bool, error) {
	query := pricingTemplateSelectQuery + ` WHERE templates.profile_id = $1 AND templates.id = $2`
	if forUpdate {
		query += ` FOR UPDATE OF templates`
	}
	query += ` LIMIT 1`
	item, err := scanPricingTemplateResponse(exec.QueryRow(ctx, query, profileID, templateID))
	if err == pgx.ErrNoRows {
		return pricingTemplateResponse{}, false, nil
	}
	if err != nil {
		return pricingTemplateResponse{}, false, fmt.Errorf("load pricing template %d in profile %d: %w", templateID, profileID, err)
	}
	if err := hydratePricingTemplateResponse(ctx, exec, &item); err != nil {
		return pricingTemplateResponse{}, false, err
	}
	return item, true, nil
}

func listPricingTemplates(ctx context.Context, exec queryExecutor, profileID int) ([]pricingTemplateResponse, error) {
	rows, err := exec.Query(ctx, pricingTemplateSelectQuery+` WHERE templates.profile_id = $1 AND templates.deleted_at IS NULL ORDER BY templates.updated_at DESC, templates.id DESC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query pricing templates for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]pricingTemplateResponse, 0)
	for rows.Next() {
		item, scanErr := scanPricingTemplateResponse(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pricing templates for profile %d: %w", profileID, err)
	}
	rows.Close()
	for index := range items {
		if err := hydratePricingTemplateResponse(ctx, exec, &items[index]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// createPricingTemplateWithRevision inserts the logical template, its initial
// active-epoch revision and the immutable mutation evidence in one transaction.
func createPricingTemplateWithRevision(ctx context.Context, tx pgx.Tx, profileID int, currentTime time.Time, name string, requestBody pricingTemplateCreateRequest) (pricingTemplateResponse, error) {
	shape, err := normalizePricingTemplateShape(requestBody)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	return createPricingTemplateWithShape(ctx, tx, profileID, currentTime, name, normalizeOptionalTrimmedString(requestBody.Description), shape, nil)
}

func reserveAndRecordPricingMutation(ctx context.Context, tx pgx.Tx, profileID int, resultKind string, templateID int, templateName string, currentTime time.Time) (string, error) {
	var operationID string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&operationID); err != nil {
		return "", fmt.Errorf("generate pricing mutation operation id: %w", err)
	}
	identityHash := fmt.Sprintf("prism-pricing-mutation:%d:%s:%d:%s", profileID, resultKind, templateID, templateName)
	if _, err := tx.Exec(ctx, `INSERT INTO pricing_mutation_operation_reservations (operation_id, profile_id, intended_result_kind, normalized_identity_hash, created_at) VALUES ($1, $2, $3, $4, $5)`, operationID, profileID, resultKind, identityHash, currentTime); err != nil {
		return "", fmt.Errorf("reserve pricing mutation operation: %w", err)
	}
	successSummary, err := json.Marshal(map[string]any{"template_id": templateID, "template_name": templateName})
	if err != nil {
		return "", fmt.Errorf("marshal pricing mutation summary: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO pricing_mutation_operations (operation_id, profile_id, result_kind, normalized_payload_hash, preview_hash, operation_recorded_at, success_summary, result_hash, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $6)`, operationID, profileID, resultKind, identityHash, identityHash, currentTime, successSummary, identityHash); err != nil {
		return "", fmt.Errorf("record pricing mutation operation: %w", err)
	}
	return operationID, nil
}

func insertPricingMutationResultItem(ctx context.Context, tx pgx.Tx, operationID string, ordinal int, templateID int, action string, version *int, revisionID *int64, currentTime time.Time, templateName string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pricing_mutation_result_items (operation_id, ordinal, template_id, action, version, revision_id, revision_effective_at, template_name_snapshot) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, operationID, ordinal, templateID, action, version, revisionID, currentTime, templateName)
	if err != nil {
		return fmt.Errorf("insert pricing mutation result item: %w", err)
	}
	return nil
}

func listPricingTemplateConnectionUsageRows(ctx context.Context, exec queryExecutor, profileID int, templateID int) ([]pricingTemplateConnectionUsageRecord, error) {
	rows, err := exec.Query(ctx, `SELECT connections.id, connections.name, model_access_targets.source_model_config_id, model_configs.model_id, connections.endpoint_id, endpoints.name FROM model_access_targets JOIN connections ON connections.id = model_access_targets.target_connection_id LEFT JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id LEFT JOIN endpoints ON endpoints.id = connections.endpoint_id WHERE model_access_targets.profile_id = $1 AND connections.pricing_template_id = $2 ORDER BY connections.id ASC`, profileID, templateID)
	if err != nil {
		return nil, fmt.Errorf("query pricing template %d connection usage for profile %d: %w", templateID, profileID, err)
	}
	defer rows.Close()
	items := make([]pricingTemplateConnectionUsageRecord, 0)
	for rows.Next() {
		item, scanErr := scanPricingTemplateConnectionUsageRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pricing template %d connection usage for profile %d: %w", templateID, profileID, err)
	}
	return items, nil
}

func scanPricingTemplateConnectionUsageRecord(scanner interface{ Scan(...any) error }) (pricingTemplateConnectionUsageRecord, error) {
	var connectionName sql.NullString
	var modelID sql.NullString
	var endpointName sql.NullString
	item := pricingTemplateConnectionUsageRecord{}
	if err := scanner.Scan(&item.ConnectionID, &connectionName, &item.ModelConfigID, &modelID, &item.EndpointID, &endpointName); err != nil {
		return pricingTemplateConnectionUsageRecord{}, err
	}
	item.ConnectionName = nullableStringValue(connectionName)
	item.ModelID = nullableStringValue(modelID)
	item.EndpointName = nullableStringValue(endpointName)
	return item, nil
}

func scanPricingTemplateResponse(scanner interface{ Scan(...any) error }) (pricingTemplateResponse, error) {
	var description, templateKind, scheduleTimezone, scheduleDigest, symbol sql.NullString
	var catalogProvider, catalogModel, revisionSource, catalogRevision sql.NullString
	var epoch, tierThreshold sql.NullInt32
	var effectiveAt, deletedAt sql.NullTime
	var revisionCount sql.NullInt64
	item := pricingTemplateResponse{}
	if err := scanner.Scan(
		&item.ID, &item.ProfileID, &item.Name, &description,
		&item.CreatedAt, &item.UpdatedAt, &deletedAt,
		&catalogProvider, &catalogModel,
		&item.RevisionID, &item.Version, &item.PricingUnit, &item.PricingCurrencyCode,
		&epoch, &templateKind, &tierThreshold, &scheduleTimezone, &scheduleDigest,
		&effectiveAt, &revisionSource, &catalogRevision,
		&symbol, &revisionCount,
	); err != nil {
		return pricingTemplateResponse{}, err
	}
	item.Description = nullableStringValue(description)
	item.CatalogProviderID = nullableStringValue(catalogProvider)
	item.CatalogModelID = nullableStringValue(catalogModel)
	item.CatalogRevision = nullableStringValue(catalogRevision)
	// A template without a current revision has no revision evidence to name,
	// so it reads as the manual default the storage column also defaults to.
	item.RevisionSource = strings.TrimSpace(revisionSource.String)
	if item.RevisionSource == "" {
		item.RevisionSource = "manual"
	}
	// The storage columns are only ever written as a pair, so a half-populated
	// coordinate is corruption rather than a manual template and must not be
	// projected as one.
	if (item.CatalogProviderID == nil) != (item.CatalogModelID == nil) {
		return pricingTemplateResponse{}, &DomainError{StatusCode: http.StatusConflict, Detail: "pricing_template_catalog_evidence_incomplete"}
	}
	item.TemplateKind = strings.TrimSpace(templateKind.String)
	if !pricingkind.Kind(item.TemplateKind).Valid() {
		return pricingTemplateResponse{}, &DomainError{StatusCode: http.StatusConflict, Detail: "pricing_template_shape_unavailable"}
	}
	if scheduleDigest.Valid {
		digest := strings.TrimSpace(scheduleDigest.String)
		item.scheduleDigest = &digest
	}
	if tierThreshold.Valid {
		value := int(tierThreshold.Int32)
		item.Tier = &pricingTemplateTier{InputTokensAbove: value}
	}
	if scheduleTimezone.Valid {
		item.Schedule = &pricingTemplateSchedule{Timezone: strings.TrimSpace(scheduleTimezone.String)}
	}
	if epoch.Valid {
		value := int(epoch.Int32)
		item.ReportingCurrencyEpoch = &value
	}
	if effectiveAt.Valid {
		value := effectiveAt.Time.UTC()
		item.VersionEffectiveAt = &value
	}
	if deletedAt.Valid {
		value := deletedAt.Time.UTC()
		item.DeletedAt = &value
	}
	if symbol.Valid {
		item.ActiveCurrencySymbol = symbol.String
	}
	if revisionCount.Valid {
		item.RevisionCount = revisionCount.Int64
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return item, nil
}
