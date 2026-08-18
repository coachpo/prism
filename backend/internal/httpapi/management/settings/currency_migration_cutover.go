package settings

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type currencyMigrationTieredTemplate struct {
	TemplateID         int     `json:"template_id"`
	Name               string  `json:"name"`
	InputTokensAbove   int     `json:"input_tokens_above"`
	InputPrice         string  `json:"input_price"`
	OutputPrice        string  `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
}

type currencyMigrationTieredTemplatesDetail struct {
	CurrentCurrencyCode string                            `json:"current_currency_code"`
	Templates           []currencyMigrationTieredTemplate `json:"templates"`
	Recovery            string                            `json:"recovery"`
}

func (detail currencyMigrationTieredTemplatesDetail) Error() string {
	return "currency_migration_blocked_by_tiered_templates"
}

func rejectCurrencyMigrationWithTieredTemplates(ctx context.Context, tx pgx.Tx, profileID int) error {
	var currentCurrencyCode sql.NullString
	if err := tx.QueryRow(ctx, `SELECT report_currency_code FROM user_settings WHERE profile_id = $1`, profileID).Scan(&currentCurrencyCode); err != nil {
		return fmt.Errorf("load current currency for tiered-template guard: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT templates.id, templates.name, revisions.tier_input_tokens_above,
		revisions.tier_input_price, revisions.tier_output_price, revisions.tier_cached_input_price,
		revisions.tier_cache_creation_price, revisions.tier_reasoning_price
		FROM pricing_templates AS templates
		JOIN pricing_template_revisions AS revisions ON revisions.id = templates.current_revision_id
		WHERE templates.profile_id = $1 AND templates.deleted_at IS NULL
		  AND revisions.tier_input_tokens_above IS NOT NULL
		ORDER BY templates.id ASC`, profileID)
	if err != nil {
		return fmt.Errorf("query tiered pricing templates for currency guard: %w", err)
	}
	defer rows.Close()
	items := make([]currencyMigrationTieredTemplate, 0)
	for rows.Next() {
		var item currencyMigrationTieredTemplate
		var threshold sql.NullInt32
		var input, output, cached, creation, reasoning sql.NullString
		if err := rows.Scan(&item.TemplateID, &item.Name, &threshold, &input, &output, &cached, &creation, &reasoning); err != nil {
			return fmt.Errorf("scan tiered pricing template for currency guard: %w", err)
		}
		if !threshold.Valid {
			continue
		}
		item.InputTokensAbove = int(threshold.Int32)
		item.InputPrice = input.String
		item.OutputPrice = output.String
		item.CachedInputPrice = nullableSQLString(cached)
		item.CacheCreationPrice = nullableSQLString(creation)
		item.ReasoningPrice = nullableSQLString(reasoning)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate tiered pricing templates for currency guard: %w", err)
	}
	if len(items) == 0 {
		return nil
	}
	return &domainError{StatusCode: http.StatusConflict, Detail: currencyMigrationTieredTemplatesDetail{
		CurrentCurrencyCode: currentCurrencyCode.String,
		Templates:           items,
		Recovery:            "clear_tiers_before_currency_migration",
	}}
}

func applyCurrencyMigrationDraftCutover(ctx context.Context, tx pgx.Tx, profileID int, settingsRow userSettingsRow, header currencyMigrationDraftHeaderRow, operationID, previewHash string, templates []currencyDraftAuthoritativeTemplate, items []currencyMigrationDraftItem, currentTime time.Time) (currencyMigrationCommitResponse, error) {
	if err := rejectCurrencyMigrationWithTieredTemplates(ctx, tx, profileID); err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	currentEpoch, err := loadCurrencyMigrationEpochOnly(ctx, tx, settingsRow)
	if err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	nextEpoch := 1
	if currentEpoch != nil {
		nextEpoch = *currentEpoch + 1
	}
	var inventoryID *int64
	var reportingEvidenceID *int64
	var inventoryGeneration, inventorySettingsGeneration, inventoryTemplateGeneration, inventoryReferenceGeneration int64
	var inventoryFXCount, inventoryFXDependencyCount int
	if header.ExpectedInventoryID != nil {
		parsedInventoryID := *header.ExpectedInventoryID
		if parsedInventoryID < 1 {
			return currencyMigrationCommitResponse{}, currencyMigrationInventoryStale()
		}
		inventoryID = &parsedInventoryID
		if err := tx.QueryRow(ctx, `SELECT generation, settings_generation, template_generation, reference_generation, fx_evidence_count, fx_dependency_count
			FROM pricing_migration_inventories
			WHERE inventory_id = $1 AND profile_id = $2
			  AND NOT EXISTS (SELECT 1 FROM pricing_migration_inventories AS successor WHERE successor.supersedes_inventory_id = pricing_migration_inventories.inventory_id)
			FOR SHARE`, parsedInventoryID, profileID).Scan(&inventoryGeneration, &inventorySettingsGeneration, &inventoryTemplateGeneration, &inventoryReferenceGeneration, &inventoryFXCount, &inventoryFXDependencyCount); err != nil {
			return currencyMigrationCommitResponse{}, currencyMigrationInventoryStale()
		}
		if inventoryFXCount != 0 || inventoryFXDependencyCount != 0 {
			return currencyMigrationCommitResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_inventory_conflict: live or unclassified FX evidence must be resolved before pre-epoch currency cutover"}
		}
		if err := tx.QueryRow(ctx, `SELECT legacy_reporting_currency_evidence_id FROM pricing_migration_legacy_reporting_currency_evidence WHERE inventory_id = $1`, parsedInventoryID).Scan(&reportingEvidenceID); err != nil && err != pgx.ErrNoRows {
			return currencyMigrationCommitResponse{}, err
		}
	}
	var oldEpochID *int64
	if settingsRow.CurrentReportingCurrencyEpochID != nil {
		var id int64
		if err := tx.QueryRow(ctx, `SELECT id FROM reporting_currency_epochs WHERE id = $1 FOR UPDATE`, *settingsRow.CurrentReportingCurrencyEpochID).Scan(&id); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		oldEpochID = &id
	}
	cutoverAt := currentTime
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&cutoverAt); err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	if oldEpochID != nil {
		if _, err := tx.Exec(ctx, `UPDATE reporting_currency_epochs SET superseded_at = $2, updated_at = $2 WHERE id = $1`, *oldEpochID, cutoverAt); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
	}
	var newEpochID int64
	if err := tx.QueryRow(ctx, `INSERT INTO reporting_currency_epochs (profile_id, epoch, currency_code, currency_symbol, effective_at, superseded_at, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, NULL, $5, $5) RETURNING id`, profileID, nextEpoch, header.TargetCurrencyCode, header.TargetCurrencySymbol, cutoverAt).Scan(&newEpochID); err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	// The operation parent stores the sealed draft hash as its normalized
	// payload identity; preview_hash is a separate CAS dimension. This lets a
	// lost-response retry validate the strict commit request before taking the
	// profile lock and makes same-operation/different-draft conflicts explicit.
	payloadHash := stringValue(header.DraftHash)
	response := currencyMigrationCommitResponse{OldCurrencyCode: nullableNonEmptyString(settingsRow.ReportCurrencyCode), NewCurrencyCode: header.TargetCurrencyCode, OldEpoch: currentEpoch, NewEpoch: intPtr(nextEpoch), RevisionChangeCount: len(items), TemplateCount: len(items), MigrationOperationID: operationID, EpochChange: true}
	resultRaw, _ := json.Marshal(response)
	resultHash := sha256.Sum256(resultRaw)
	if _, err := tx.Exec(ctx, `INSERT INTO pricing_mutation_operations (operation_id, profile_id, result_kind, normalized_payload_hash, preview_hash, operation_recorded_at, success_summary, result_hash, created_at) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::jsonb, $8, $6)`, operationID, profileID, header.OperationKind, payloadHash, previewHash, cutoverAt, resultRaw, hex.EncodeToString(resultHash[:])); err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	byID := make(map[int]currencyMigrationDraftItem, len(items))
	for _, item := range items {
		byID[item.TemplateID] = item
	}
	ledgerItemsHashInput := make([]map[string]any, 0, len(templates))
	for index, template := range templates {
		item, ok := byID[template.ID]
		if !ok {
			return currencyMigrationCommitResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_draft_template_set_changed"}
		}
		var revisionID int64
		if err := tx.QueryRow(ctx, `INSERT INTO pricing_template_revisions (template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, effective_at, created_at, created_by_kind, created_by_operation_id) VALUES ($1, $2, 'PER_1M', $3, $4, $5, 'active_epoch', $6, $7, $8, $9, $10, $11, $11, 'currency_migration', $12::uuid) RETURNING id`, template.ID, template.Version+1, header.TargetCurrencyCode, newEpochID, nextEpoch, item.InputPrice, item.OutputPrice, item.CachedInputPrice, item.CacheCreationPrice, item.ReasoningPrice, cutoverAt, operationID).Scan(&revisionID); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE pricing_templates SET current_revision_id = $1, updated_at = $2 WHERE id = $3 AND profile_id = $4 AND deleted_at IS NULL`, revisionID, cutoverAt, template.ID, profileID); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		if err := insertPricingMutationResultItemSettings(ctx, tx, operationID, index+1, template.ID, template.Version+1, revisionID, cutoverAt, template.Name); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO currency_migration_ledger_items (operation_id, ordinal, template_id, template_name_snapshot, old_version, new_version, old_revision_id, old_template_evidence_id, new_revision_id, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`, operationID, index+1, template.ID, template.Name, template.Version, template.Version+1, template.RevisionID, template.LegacyEvidenceID, revisionID, item.InputPrice, item.OutputPrice, item.CachedInputPrice, item.CacheCreationPrice, item.ReasoningPrice); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		ledgerItemsHashInput = append(ledgerItemsHashInput, map[string]any{"template_id": template.ID, "old_version": template.Version, "new_version": template.Version + 1, "revision_id": revisionID})
	}
	itemsRaw, _ := json.Marshal(ledgerItemsHashInput)
	itemsHash := sha256.Sum256(itemsRaw)
	if _, err := tx.Exec(ctx, `INSERT INTO currency_migration_ledger (operation_id, operation_kind, profile_id, old_epoch_id, old_epoch, new_epoch_id, new_epoch, legacy_reporting_currency_evidence_id, normalized_payload_hash, inventory_id, inventory_hash, item_count, items_hash, committed_result, committed_at) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, $15)`, operationID, header.OperationKind, profileID, oldEpochID, currentEpoch, newEpochID, nextEpoch, reportingEvidenceID, payloadHash, inventoryID, inventoryHashForLedger(header), len(templates), hex.EncodeToString(itemsHash[:]), resultRaw, cutoverAt); err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE user_settings SET report_currency_code = $2, report_currency_symbol = $3, current_reporting_currency_epoch_id = $4, pricing_migration_state = 'ready', legacy_migration_issues = '{}', pricing_template_generation = pricing_template_generation + 1, updated_at = $5 WHERE id = $1`, settingsRow.ID, header.TargetCurrencyCode, header.TargetCurrencySymbol, newEpochID, cutoverAt); err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	if inventoryID != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO pricing_migration_inventories (
			profile_id, generation, supersedes_inventory_id, settings_generation, epoch_generation,
			template_generation, reference_generation, issue_codes, fx_evidence_count,
			fx_assessment_count, fx_dependency_count, template_evidence_count,
			reporting_currency_evidence_count, fx_evidence_hash_root, template_evidence_hash_root,
			reporting_currency_evidence_hash_root, legacy_fx_source_count, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, '{}', 0, 0, 0, 0, 0, NULL, NULL, NULL, 0, $8)`,
			profileID, inventoryGeneration+1, *inventoryID, inventorySettingsGeneration+1, int64(nextEpoch), inventoryTemplateGeneration+1, inventoryReferenceGeneration, cutoverAt); err != nil {
			return currencyMigrationCommitResponse{}, fmt.Errorf("append clean currency migration inventory: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE pricing_currency_migration_drafts SET status = 'committed', committed_result_operation_id = $2::uuid, updated_at = $3 WHERE draft_id = $1::uuid`, header.DraftID, operationID, cutoverAt); err != nil {
		return currencyMigrationCommitResponse{}, err
	}
	return response, nil
}

func insertPricingMutationResultItemSettings(ctx context.Context, tx pgx.Tx, operationID string, ordinal, templateID, version int, revisionID int64, effectiveAt time.Time, name string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pricing_mutation_result_items (operation_id, ordinal, template_id, action, version, revision_id, revision_effective_at, template_name_snapshot) VALUES ($1::uuid, $2, $3, 'revision_created', $4, $5, $6, $7)`, operationID, ordinal, templateID, version, revisionID, effectiveAt, name)
	return err
}

func loadCurrencyMigrationResult(ctx context.Context, tx pgx.Tx, operationID string) (currencyMigrationCommitResponse, bool, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT committed_result FROM currency_migration_ledger WHERE operation_id = $1::uuid`, operationID).Scan(&raw)
	if err == pgx.ErrNoRows {
		return currencyMigrationCommitResponse{}, false, nil
	}
	if err != nil {
		return currencyMigrationCommitResponse{}, false, err
	}
	var response currencyMigrationCommitResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return currencyMigrationCommitResponse{}, false, err
	}
	return response, true, nil
}
