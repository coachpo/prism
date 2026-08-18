package connections

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// validatePricingTemplateID deliberately stays in store.go: it reads
// pricing_templates, but three of its four callers (routes.go, writer.go,
// composite_create.go) are connection writes, so its reason to change is the
// connection write contract. Do not pull it in here to "gather the pricing SQL".

type pricingTemplateConnectionUsageRecord struct {
	ConnectionID   int
	ConnectionName *string
	ModelConfigID  int
	ModelID        *string
	EndpointID     int
	EndpointName   *string
}

type pricingTemplateResponse struct {
	ID                     int                  `json:"id"`
	ProfileID              int                  `json:"profile_id"`
	Name                   string               `json:"name"`
	Description            *string              `json:"description"`
	PricingUnit            string               `json:"pricing_unit"`
	PricingCurrencyCode    string               `json:"pricing_currency_code"`
	InputPrice             string               `json:"input_price"`
	OutputPrice            string               `json:"output_price"`
	CachedInputPrice       *string              `json:"cached_input_price"`
	CacheCreationPrice     *string              `json:"cache_creation_price"`
	ReasoningPrice         *string              `json:"reasoning_price"`
	Tier                   *pricingTemplateTier `json:"tier"`
	Version                int                  `json:"version"`
	RevisionID             int64                `json:"revision_id"`
	VersionEffectiveAt     *time.Time           `json:"version_effective_at"`
	ReportingCurrencyEpoch *int                 `json:"reporting_currency_epoch"`
	ActiveCurrencySymbol   string               `json:"active_currency_symbol"`
	DeletedAt              *time.Time           `json:"deleted_at,omitempty"`
	RevisionCount          int64                `json:"revision_count"`
	CreatedAt              time.Time            `json:"created_at"`
	UpdatedAt              time.Time            `json:"updated_at"`
}

const pricingTemplateSelectQuery = `SELECT templates.id, templates.profile_id, templates.name, templates.description,
			templates.created_at, templates.updated_at, templates.deleted_at,
			revisions.id, revisions.version, revisions.pricing_unit, revisions.currency_code,
			revisions.reporting_currency_epoch, revisions.input_price, revisions.output_price,
			revisions.cached_input_price, revisions.cache_creation_price, revisions.reasoning_price,
			revisions.tier_input_tokens_above, revisions.tier_input_price, revisions.tier_output_price,
			revisions.tier_cached_input_price, revisions.tier_cache_creation_price, revisions.tier_reasoning_price,
			revisions.effective_at,
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
	return items, nil
}

// createPricingTemplateWithRevision inserts the logical template, its initial
// active-epoch revision and the immutable mutation evidence in one transaction.
func createPricingTemplateWithRevision(ctx context.Context, tx pgx.Tx, profileID int, currentTime time.Time, name string, requestBody pricingTemplateCreateRequest) (pricingTemplateResponse, error) {
	prices, err := normalizePricingTemplatePrices(requestBody.InputPrice, requestBody.OutputPrice, requestBody.CachedInputPrice, requestBody.CacheCreationPrice, requestBody.ReasoningPrice)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	prices.Tier, err = normalizePricingTemplateTier(requestBody.Tier, prices)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	return createPricingTemplateWithPrices(ctx, tx, profileID, currentTime, name, normalizeOptionalTrimmedString(requestBody.Description), prices)
}

func createPricingTemplateWithPrices(ctx context.Context, tx pgx.Tx, profileID int, currentTime time.Time, name string, description *string, prices pricingTemplatePrices) (pricingTemplateResponse, error) {
	var epochID int64
	var epochOrdinal int
	var epochCode string
	if err := tx.QueryRow(ctx, `SELECT epochs.id, epochs.epoch, epochs.currency_code
		FROM reporting_currency_epochs AS epochs
		JOIN user_settings AS settings ON settings.current_reporting_currency_epoch_id = epochs.id
		WHERE settings.profile_id = $1 AND epochs.superseded_at IS NULL
		FOR UPDATE OF settings`, profileID).Scan(&epochID, &epochOrdinal, &epochCode); err != nil {
		return pricingTemplateResponse{}, fmt.Errorf("load active reporting currency epoch for profile %d: %w", profileID, err)
	}

	var templateID int
	if err := tx.QueryRow(ctx, `INSERT INTO pricing_templates (profile_id, name, description, current_revision_id, created_at, updated_at)
		VALUES ($1, $2, $3, NULL, $4, $4) RETURNING id`, profileID, name, description, currentTime).Scan(&templateID); err != nil {
		return pricingTemplateResponse{}, fmt.Errorf("insert logical pricing template %q: %w", name, err)
	}
	operationID, err := reserveAndRecordPricingMutation(ctx, tx, profileID, "template_create", templateID, name, currentTime)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	var revisionID int64
	if err := tx.QueryRow(ctx, `INSERT INTO pricing_template_revisions (
		template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id,
		reporting_currency_epoch, currency_attribution, input_price, output_price,
		cached_input_price, cache_creation_price, reasoning_price,
		tier_input_tokens_above, tier_input_price, tier_output_price, tier_cached_input_price, tier_cache_creation_price, tier_reasoning_price,
		effective_at, created_at, created_by_kind, created_by_operation_id
	) VALUES ($1, 1, 'PER_1M', $2, $3, $4, 'active_epoch', $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $16, 'manual_create', $17)
		RETURNING id`, templateID, epochCode, epochID, epochOrdinal, prices.InputPrice, prices.OutputPrice,
		prices.CachedInputPrice, prices.CacheCreationPrice, prices.ReasoningPrice,
		nullableTierThreshold(prices.Tier), nullableTierPrice(prices.Tier, "input"), nullableTierPrice(prices.Tier, "output"), nullableTierSpecialtyPrice(prices.Tier, "cached_input"), nullableTierSpecialtyPrice(prices.Tier, "cache_creation"), nullableTierSpecialtyPrice(prices.Tier, "reasoning"), currentTime, operationID).Scan(&revisionID); err != nil {
		return pricingTemplateResponse{}, fmt.Errorf("insert pricing template v1 revision: %w", err)
	}
	if err := insertPricingMutationResultItem(ctx, tx, operationID, 1, templateID, "created", intPtr(1), &revisionID, currentTime, name); err != nil {
		return pricingTemplateResponse{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE pricing_templates SET current_revision_id = $1, updated_at = $2 WHERE id = $3`, revisionID, currentTime, templateID); err != nil {
		return pricingTemplateResponse{}, fmt.Errorf("close pricing template current revision pointer: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE user_settings SET pricing_template_generation = pricing_template_generation + 1, updated_at = $2 WHERE profile_id = $1`, profileID, currentTime); err != nil {
		return pricingTemplateResponse{}, fmt.Errorf("advance pricing template generation for profile %d: %w", profileID, err)
	}
	created, found, err := loadPricingTemplate(ctx, tx, profileID, templateID, false)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	if !found {
		return pricingTemplateResponse{}, fmt.Errorf("created pricing template %d disappeared", templateID)
	}
	return created, nil
}

func updatePricingTemplateWithPrices(ctx context.Context, tx pgx.Tx, profileID int, current pricingTemplateResponse, nextName string, nextDescription *string, prices pricingTemplatePrices, currentTime time.Time) error {
	nameChanged := nextName != current.Name
	descriptionChanged := !stringsEqualPointers(nextDescription, current.Description)
	pricesChanged := current.InputPrice != prices.InputPrice || current.OutputPrice != prices.OutputPrice ||
		!stringsEqualPointers(current.CachedInputPrice, prices.CachedInputPrice) ||
		!stringsEqualPointers(current.CacheCreationPrice, prices.CacheCreationPrice) ||
		!stringsEqualPointers(current.ReasoningPrice, prices.ReasoningPrice) ||
		!pricingTemplateTierEqual(current.Tier, prices.Tier)
	if !nameChanged && !descriptionChanged && !pricesChanged {
		return nil
	}
	if pricesChanged {
		var epochID int64
		var epochCode string
		if err := tx.QueryRow(ctx, `SELECT epochs.id, epochs.currency_code
			FROM reporting_currency_epochs AS epochs
			JOIN user_settings AS settings ON settings.current_reporting_currency_epoch_id = epochs.id
			WHERE settings.profile_id = $1 AND epochs.superseded_at IS NULL
			FOR UPDATE OF settings`, profileID).Scan(&epochID, &epochCode); err != nil {
			return fmt.Errorf("load active reporting currency epoch for profile %d: %w", profileID, err)
		}
		operationID, err := reserveAndRecordPricingMutation(ctx, tx, profileID, "template_update", current.ID, nextName, currentTime)
		if err != nil {
			return err
		}
		var revisionID int64
		if err := tx.QueryRow(ctx, `INSERT INTO pricing_template_revisions (
			template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id,
			reporting_currency_epoch, currency_attribution, input_price, output_price,
			cached_input_price, cache_creation_price, reasoning_price,
			tier_input_tokens_above, tier_input_price, tier_output_price, tier_cached_input_price, tier_cache_creation_price, tier_reasoning_price,
			effective_at, created_at, created_by_kind, created_by_operation_id
		) VALUES ($1, $2, 'PER_1M', $3, $4, $5, 'active_epoch', $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $17, 'manual_edit', $18)
		RETURNING id`, current.ID, current.Version+1, epochCode, epochID, current.ReportingCurrencyEpoch,
			prices.InputPrice, prices.OutputPrice, prices.CachedInputPrice, prices.CacheCreationPrice, prices.ReasoningPrice,
			nullableTierThreshold(prices.Tier), nullableTierPrice(prices.Tier, "input"), nullableTierPrice(prices.Tier, "output"), nullableTierSpecialtyPrice(prices.Tier, "cached_input"), nullableTierSpecialtyPrice(prices.Tier, "cache_creation"), nullableTierSpecialtyPrice(prices.Tier, "reasoning"), currentTime, operationID).Scan(&revisionID); err != nil {
			return fmt.Errorf("insert pricing template v%d revision: %w", current.Version+1, err)
		}
		action := "revision_created"
		if nameChanged || descriptionChanged {
			action = "metadata_and_revision"
		}
		if err := insertPricingMutationResultItem(ctx, tx, operationID, 1, current.ID, action, intPtr(current.Version+1), &revisionID, currentTime, nextName); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE pricing_templates SET current_revision_id = $1, updated_at = $2 WHERE id = $3`, revisionID, currentTime, current.ID); err != nil {
			return fmt.Errorf("close pricing template current revision pointer: %w", err)
		}
	} else {
		operationID, err := reserveAndRecordPricingMutation(ctx, tx, profileID, "template_update", current.ID, nextName, currentTime)
		if err != nil {
			return err
		}
		if err := insertPricingMutationResultItem(ctx, tx, operationID, 1, current.ID, "metadata_updated", nil, nil, currentTime, nextName); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE pricing_templates SET name = $2, description = $3, updated_at = $4 WHERE id = $1`, current.ID, nextName, nextDescription, currentTime); err != nil {
		return fmt.Errorf("update pricing template metadata %d: %w", current.ID, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE user_settings SET pricing_template_generation = pricing_template_generation + 1, updated_at = $2 WHERE profile_id = $1`, profileID, currentTime); err != nil {
		return fmt.Errorf("advance pricing template generation for profile %d: %w", profileID, err)
	}
	return nil
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
	if _, err := tx.Exec(ctx, `INSERT INTO pricing_mutation_operations (operation_id, profile_id, result_kind, normalized_payload_hash, preview_hash, operation_recorded_at, success_summary, result_hash, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $6)`, operationID, profileID, resultKind, identityHash, identityHash, currentTime, fmt.Sprintf(`{"template_id":%d,"template_name":%q}`, templateID, templateName), identityHash); err != nil {
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
	var description sql.NullString
	var cachedInputPrice sql.NullString
	var cacheCreationPrice sql.NullString
	var reasoningPrice sql.NullString
	var tierInputTokensAbove sql.NullInt32
	var tierInputPrice sql.NullString
	var tierOutputPrice sql.NullString
	var tierCachedInputPrice sql.NullString
	var tierCacheCreationPrice sql.NullString
	var tierReasoningPrice sql.NullString
	var epoch sql.NullInt32
	var effectiveAt sql.NullTime
	var deletedAt sql.NullTime
	var symbol sql.NullString
	var revisionCount sql.NullInt64
	item := pricingTemplateResponse{}
	if err := scanner.Scan(
		&item.ID,
		&item.ProfileID,
		&item.Name,
		&description,
		&item.CreatedAt,
		&item.UpdatedAt,
		&deletedAt,
		&item.RevisionID,
		&item.Version,
		&item.PricingUnit,
		&item.PricingCurrencyCode,
		&epoch,
		&item.InputPrice,
		&item.OutputPrice,
		&cachedInputPrice,
		&cacheCreationPrice,
		&reasoningPrice,
		&tierInputTokensAbove,
		&tierInputPrice,
		&tierOutputPrice,
		&tierCachedInputPrice,
		&tierCacheCreationPrice,
		&tierReasoningPrice,
		&effectiveAt,
		&symbol,
		&revisionCount,
	); err != nil {
		return pricingTemplateResponse{}, err
	}
	item.Description = nullableStringValue(description)
	item.CachedInputPrice = nullableStringValue(cachedInputPrice)
	item.CacheCreationPrice = nullableStringValue(cacheCreationPrice)
	item.ReasoningPrice = nullableStringValue(reasoningPrice)
	if tierInputTokensAbove.Valid {
		item.Tier = &pricingTemplateTier{
			InputTokensAbove:   int(tierInputTokensAbove.Int32),
			InputPrice:         tierInputPrice.String,
			OutputPrice:        tierOutputPrice.String,
			CachedInputPrice:   nullableStringValue(tierCachedInputPrice),
			CacheCreationPrice: nullableStringValue(tierCacheCreationPrice),
			ReasoningPrice:     nullableStringValue(tierReasoningPrice),
		}
	}
	if epoch.Valid {
		resolved := int(epoch.Int32)
		item.ReportingCurrencyEpoch = &resolved
	}
	if effectiveAt.Valid {
		resolved := effectiveAt.Time.UTC()
		item.VersionEffectiveAt = &resolved
	}
	if deletedAt.Valid {
		resolved := deletedAt.Time.UTC()
		item.DeletedAt = &resolved
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

func nullableTierThreshold(tier *pricingTemplateTier) any {
	if tier == nil {
		return nil
	}
	return tier.InputTokensAbove
}

func nullableTierPrice(tier *pricingTemplateTier, component string) any {
	if tier == nil {
		return nil
	}
	if component == "input" {
		return tier.InputPrice
	}
	return tier.OutputPrice
}

func nullableTierSpecialtyPrice(tier *pricingTemplateTier, component string) any {
	if tier == nil {
		return nil
	}
	switch component {
	case "cached_input":
		return nullableString(tier.CachedInputPrice)
	case "cache_creation":
		return nullableString(tier.CacheCreationPrice)
	default:
		return nullableString(tier.ReasoningPrice)
	}
}
