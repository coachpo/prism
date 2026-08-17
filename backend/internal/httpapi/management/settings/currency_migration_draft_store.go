package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type currencyMigrationDraftHeaderRow struct {
	DraftID                        string
	ProfileID                      int
	MigrationOperationID           string
	OperationKind                  string
	TargetCurrencyCode             string
	TargetCurrencySymbol           string
	ExpectedInventoryID            *int64
	ExpectedInventoryHash          *string
	ExpectedInventoryGeneration    *int64
	ExpectedReportingCurrencyEpoch *int64
	ExpectedSettingsUpdatedAt      time.Time
	Status                         string
	NormalizedHeaderHash           string
	ReceivedChunkCount             int
	DraftHash                      *string
	TemplateCount                  *int
	CommittedResultOperationID     *string
	CreatedAt                      time.Time
	UpdatedAt                      time.Time
	ExpiresAt                      time.Time
}

type currencyDraftAuthoritativeTemplate struct {
	ID                 int
	Name               string
	Version            int
	RevisionID         *int64
	LegacyEvidenceID   *int64
	UpdatedAt          time.Time
	InputPrice         *string
	OutputPrice        *string
	CachedInputPrice   *string
	CacheCreationPrice *string
	ReasoningPrice     *string
	ReferenceCount     int64
}

func loadCurrencyDraftByID(ctx context.Context, tx pgx.Tx, profileID int, draftID string, forUpdate bool) (currencyMigrationDraftHeaderRow, bool, error) {
	return loadCurrencyDraft(ctx, tx, `draft_id = $2::uuid`, profileID, draftID, forUpdate)
}

func loadCurrencyDraftByOperation(ctx context.Context, tx pgx.Tx, profileID int, operationID string, forUpdate bool) (currencyMigrationDraftHeaderRow, bool, error) {
	return loadCurrencyDraft(ctx, tx, `migration_operation_id = $2::uuid`, profileID, operationID, forUpdate)
}

func loadCurrencyDraft(ctx context.Context, tx pgx.Tx, predicate string, profileID int, value string, forUpdate bool) (currencyMigrationDraftHeaderRow, bool, error) {
	query := `SELECT draft_id::text, profile_id, migration_operation_id::text, operation_kind, target_currency_code, target_currency_symbol,
		expected_inventory_id, expected_inventory_hash, expected_inventory_generation, expected_reporting_currency_epoch,
		expected_settings_updated_at, status, normalized_header_hash, received_chunk_count, draft_hash, template_count,
		committed_result_operation_id::text, created_at, updated_at, expires_at
		FROM pricing_currency_migration_drafts WHERE profile_id = $1 AND ` + predicate
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var row currencyMigrationDraftHeaderRow
	var inventoryID, inventoryGeneration, epoch *int64
	var inventoryHash, draftHash, committedID *string
	err := tx.QueryRow(ctx, query, profileID, value).Scan(
		&row.DraftID, &row.ProfileID, &row.MigrationOperationID, &row.OperationKind, &row.TargetCurrencyCode, &row.TargetCurrencySymbol,
		&inventoryID, &inventoryHash, &inventoryGeneration, &epoch, &row.ExpectedSettingsUpdatedAt, &row.Status, &row.NormalizedHeaderHash,
		&row.ReceivedChunkCount, &draftHash, &row.TemplateCount, &committedID, &row.CreatedAt, &row.UpdatedAt, &row.ExpiresAt,
	)
	if err == pgx.ErrNoRows {
		return currencyMigrationDraftHeaderRow{}, false, nil
	}
	if err != nil {
		return currencyMigrationDraftHeaderRow{}, false, fmt.Errorf("load currency migration draft: %w", err)
	}
	row.ExpectedInventoryID, row.ExpectedInventoryHash, row.ExpectedInventoryGeneration, row.ExpectedReportingCurrencyEpoch = inventoryID, inventoryHash, inventoryGeneration, epoch
	row.DraftHash, row.CommittedResultOperationID = draftHash, committedID
	return row, true, nil
}

func (s *Service) currencyDraftHeaderResponse(ctx context.Context, tx pgx.Tx, row currencyMigrationDraftHeaderRow) (currencyMigrationDraftHeaderResponse, error) {
	page, err := s.loadCurrencyDraftChunkPage(ctx, tx, row, row.ProfileID, currencyDraftDefaultChunkLimit, 0)
	if err != nil {
		return currencyMigrationDraftHeaderResponse{}, err
	}
	return currencyMigrationDraftHeaderResponse{
		DraftID: row.DraftID, MigrationOperationID: row.MigrationOperationID, OperationKind: row.OperationKind,
		TargetCurrencyCode: row.TargetCurrencyCode, TargetCurrencySymbol: row.TargetCurrencySymbol,
		ExpectedInventoryID: positiveDecimalPtr(row.ExpectedInventoryID), ExpectedInventoryHash: row.ExpectedInventoryHash,
		ExpectedInventoryGeneration: row.ExpectedInventoryGeneration, ExpectedReportingCurrencyEpoch: row.ExpectedReportingCurrencyEpoch,
		ExpectedSettingsUpdatedAt: row.ExpectedSettingsUpdatedAt.UTC().Format(time.RFC3339), Status: row.Status,
		NormalizedHeaderHash: row.NormalizedHeaderHash, ReceivedChunkCount: row.ReceivedChunkCount, ChunkPage: page,
		DraftHash: row.DraftHash, TemplateCount: row.TemplateCount, CommittedResultOperationID: row.CommittedResultOperationID,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func (s *Service) loadCurrencyDraftChunkPage(ctx context.Context, tx pgx.Tx, header currencyMigrationDraftHeaderRow, profileID, limit, lastOrdinal int) (currencyMigrationDraftChunkPage, error) {
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_currency_migration_draft_chunks WHERE draft_id = $1::uuid`, header.DraftID).Scan(&total); err != nil {
		return currencyMigrationDraftChunkPage{}, err
	}
	rows, err := tx.Query(ctx, `SELECT ordinal, row_count, content_hash FROM pricing_currency_migration_draft_chunks WHERE draft_id = $1::uuid AND ordinal > $2 ORDER BY ordinal ASC LIMIT $3`, header.DraftID, lastOrdinal, limit)
	if err != nil {
		return currencyMigrationDraftChunkPage{}, err
	}
	defer rows.Close()
	items := make([]currencyMigrationDraftChunkSummary, 0, limit)
	last := lastOrdinal
	for rows.Next() {
		var item currencyMigrationDraftChunkSummary
		if err := rows.Scan(&item.Ordinal, &item.RowCount, &item.ContentHash); err != nil {
			return currencyMigrationDraftChunkPage{}, err
		}
		items = append(items, item)
		last = item.Ordinal
	}
	if err := rows.Err(); err != nil {
		return currencyMigrationDraftChunkPage{}, err
	}
	page := currencyMigrationDraftChunkPage{Items: items, TotalCount: total, ConsumedCount: total}
	if len(items) > 0 {
		var remaining int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_currency_migration_draft_chunks WHERE draft_id = $1::uuid AND ordinal > $2`, header.DraftID, last).Scan(&remaining); err != nil {
			return currencyMigrationDraftChunkPage{}, err
		}
		if remaining > 0 {
			page.ConsumedCount = total - remaining
			cursor := s.encodeCurrencyDraftCursor(currencyDraftCursor{Version: 1, ProfileID: profileID, DraftID: header.DraftID, Kind: "chunks", Binding: header.NormalizedHeaderHash, LastOrdinal: last})
			page.NextCursor = &cursor
		}
	} else if lastOrdinal > 0 {
		var remaining int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_currency_migration_draft_chunks WHERE draft_id = $1::uuid AND ordinal > $2`, header.DraftID, lastOrdinal).Scan(&remaining); err != nil {
			return currencyMigrationDraftChunkPage{}, err
		}
		page.ConsumedCount = total - remaining
	}
	return page, nil
}

func (s *Service) loadCurrencyDraftItemPage(ctx context.Context, tx pgx.Tx, header currencyMigrationDraftHeaderRow, profileID, limit, lastID int) (currencyMigrationDraftItemPage, error) {
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_currency_migration_draft_items WHERE draft_id = $1::uuid`, header.DraftID).Scan(&total); err != nil {
		return currencyMigrationDraftItemPage{}, err
	}
	rows, err := tx.Query(ctx, `SELECT template_id, template_name, expected_version, expected_updated_at, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, reference_count FROM pricing_currency_migration_draft_items WHERE draft_id = $1::uuid AND template_id > $2 ORDER BY template_id ASC LIMIT $3`, header.DraftID, lastID, limit)
	if err != nil {
		return currencyMigrationDraftItemPage{}, err
	}
	defer rows.Close()
	items := make([]currencyMigrationDraftItemResponse, 0, limit)
	last := lastID
	for rows.Next() {
		var item currencyMigrationDraftItemResponse
		var expected time.Time
		if err := rows.Scan(&item.TemplateID, &item.TemplateName, &item.ExpectedVersion, &expected, &item.InputPrice, &item.OutputPrice, &item.CachedInputPrice, &item.CacheCreationPrice, &item.ReasoningPrice, &item.ReferenceCount); err != nil {
			return currencyMigrationDraftItemPage{}, err
		}
		item.ExpectedUpdatedAt = expected.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
		last = item.TemplateID
	}
	if err := rows.Err(); err != nil {
		return currencyMigrationDraftItemPage{}, err
	}
	page := currencyMigrationDraftItemPage{Items: items, TotalCount: total, Limit: limit}
	if len(items) > 0 {
		var remaining int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_currency_migration_draft_items WHERE draft_id = $1::uuid AND template_id > $2`, header.DraftID, last).Scan(&remaining); err != nil {
			return currencyMigrationDraftItemPage{}, err
		}
		page.HasMore = remaining > 0
	} else if lastID > 0 {
		var remaining int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_currency_migration_draft_items WHERE draft_id = $1::uuid AND template_id > $2`, header.DraftID, lastID).Scan(&remaining); err != nil {
			return currencyMigrationDraftItemPage{}, err
		}
		page.HasMore = remaining > 0
	}
	if page.HasMore {
		cursor := s.encodeCurrencyDraftCursor(currencyDraftCursor{Version: 1, ProfileID: profileID, DraftID: header.DraftID, Kind: "items", Binding: stringValue(header.DraftHash), LastID: last})
		page.NextCursor = &cursor
	}
	return page, nil
}

func loadCurrencyDraftPayloadRows(ctx context.Context, tx pgx.Tx, draftID string) ([]currencyMigrationDraftItem, error) {
	rows, err := tx.Query(ctx, `SELECT ordinal, row_count, content_hash, payload FROM pricing_currency_migration_draft_chunks WHERE draft_id = $1::uuid ORDER BY ordinal ASC`, draftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all := make([]currencyMigrationDraftItem, 0)
	for rows.Next() {
		var ordinal, rowCount int
		var expectedHash string
		var payload []byte
		if err := rows.Scan(&ordinal, &rowCount, &expectedHash, &payload); err != nil {
			return nil, err
		}
		var items []currencyMigrationDraftItem
		if err := json.Unmarshal(payload, &items); err != nil {
			return nil, fmt.Errorf("decode currency migration chunk %d: %w", ordinal, err)
		}
		if len(items) != rowCount || currencyDraftItemsHash(items) != expectedHash {
			return nil, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_draft_corrupt"}
		}
		all = append(all, items...)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return all, nil
}

func loadCurrencyDraftItems(ctx context.Context, tx pgx.Tx, draftID string) ([]currencyMigrationDraftItem, error) {
	rows, err := tx.Query(ctx, `SELECT template_id, expected_version, expected_updated_at, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, template_name, reference_count FROM pricing_currency_migration_draft_items WHERE draft_id = $1::uuid ORDER BY template_id ASC`, draftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]currencyMigrationDraftItem, 0)
	for rows.Next() {
		var item currencyMigrationDraftItem
		var expected time.Time
		if err := rows.Scan(&item.TemplateID, &item.ExpectedVersion, &expected, &item.InputPrice, &item.OutputPrice, &item.CachedInputPrice, &item.CacheCreationPrice, &item.ReasoningPrice, &item.TemplateName, &item.ReferenceCount); err != nil {
			return nil, err
		}
		item.ExpectedUpdatedAt = expected.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadCurrencyDraftAuthoritativeTemplates(ctx context.Context, tx pgx.Tx, profileID int, forUpdate bool) ([]currencyDraftAuthoritativeTemplate, error) {
	var currentEpochID *int64
	if err := tx.QueryRow(ctx, `SELECT current_reporting_currency_epoch_id FROM user_settings WHERE profile_id = $1`, profileID).Scan(&currentEpochID); err != nil {
		return nil, fmt.Errorf("load currency migration epoch pointer: %w", err)
	}
	var inventoryID int64
	if currentEpochID == nil {
		if err := tx.QueryRow(ctx, `SELECT inventory.inventory_id
			FROM pricing_migration_inventories AS inventory
			WHERE inventory.profile_id = $1
			  AND NOT EXISTS (SELECT 1 FROM pricing_migration_inventories AS successor WHERE successor.supersedes_inventory_id = inventory.inventory_id)
			ORDER BY inventory.generation DESC LIMIT 1`, profileID).Scan(&inventoryID); err != nil {
			if err == pgx.ErrNoRows {
				return nil, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_inventory_stale: pending pricing profile has no authoritative inventory"}
			}
			return nil, fmt.Errorf("load pending pricing inventory: %w", err)
		}
	}
	query := `SELECT templates.id, templates.name, templates.updated_at,
		revisions.id, revisions.version, revisions.input_price, revisions.output_price,
		revisions.cached_input_price, revisions.cache_creation_price, revisions.reasoning_price,
		evidence.legacy_template_evidence_id, evidence.public_version, evidence.input_price, evidence.output_price,
		evidence.cached_input_price, evidence.cache_creation_price, evidence.reasoning_price,
		(SELECT count(*) FROM connections AS c WHERE c.profile_id = templates.profile_id AND c.pricing_template_id = templates.id)
		FROM pricing_templates AS templates
		LEFT JOIN pricing_template_revisions AS revisions ON revisions.id = templates.current_revision_id
		LEFT JOIN pricing_migration_legacy_template_evidence AS evidence ON evidence.inventory_id = $2 AND evidence.template_id = templates.id
		WHERE templates.profile_id = $1 AND templates.deleted_at IS NULL ORDER BY templates.id ASC`
	if forUpdate {
		query += ` FOR UPDATE OF templates`
	}
	rows, err := tx.Query(ctx, query, profileID, inventoryID)
	if err != nil {
		return nil, fmt.Errorf("load active currency migration templates: %w", err)
	}
	defer rows.Close()
	items := make([]currencyDraftAuthoritativeTemplate, 0)
	for rows.Next() {
		var item currencyDraftAuthoritativeTemplate
		var revisionID, revisionVersion, evidenceID, evidenceVersion sql.NullInt64
		var revisionInput, revisionOutput, revisionCached, revisionCreation, revisionReasoning sql.NullString
		var evidenceInput, evidenceOutput, evidenceCached, evidenceCreation, evidenceReasoning sql.NullString
		if err := rows.Scan(&item.ID, &item.Name, &item.UpdatedAt, &revisionID, &revisionVersion, &revisionInput, &revisionOutput, &revisionCached, &revisionCreation, &revisionReasoning, &evidenceID, &evidenceVersion, &evidenceInput, &evidenceOutput, &evidenceCached, &evidenceCreation, &evidenceReasoning, &item.ReferenceCount); err != nil {
			return nil, err
		}
		if revisionID.Valid {
			value := revisionID.Int64
			item.RevisionID = &value
			item.Version = int(revisionVersion.Int64)
			item.InputPrice = nullableStringValue(revisionInput)
			item.OutputPrice = nullableStringValue(revisionOutput)
			item.CachedInputPrice = nullableStringValue(revisionCached)
			item.CacheCreationPrice = nullableStringValue(revisionCreation)
			item.ReasoningPrice = nullableStringValue(revisionReasoning)
		} else if evidenceID.Valid {
			value := evidenceID.Int64
			item.LegacyEvidenceID = &value
			if !evidenceVersion.Valid {
				return nil, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_draft_corrupt: legacy template evidence has no public version"}
			}
			item.Version = int(evidenceVersion.Int64)
			item.InputPrice = nullableStringValue(evidenceInput)
			item.OutputPrice = nullableStringValue(evidenceOutput)
			item.CachedInputPrice = nullableStringValue(evidenceCached)
			item.CacheCreationPrice = nullableStringValue(evidenceCreation)
			item.ReasoningPrice = nullableStringValue(evidenceReasoning)
		} else {
			return nil, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_inventory_stale: active template has no current revision or matching legacy evidence"}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func sealCurrencyDraftItems(authoritative []currencyDraftAuthoritativeTemplate, rows []currencyMigrationDraftItem) ([]currencyMigrationDraftItem, error) {
	byID := make(map[int]currencyMigrationDraftItem, len(rows))
	for _, row := range rows {
		if _, exists := byID[row.TemplateID]; exists {
			return nil, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("currency_migration_draft_duplicate_template: template_id=%d", row.TemplateID)}
		}
		byID[row.TemplateID] = row
	}
	items := make([]currencyMigrationDraftItem, 0, len(authoritative))
	missing := make([]string, 0)
	for _, template := range authoritative {
		row, ok := byID[template.ID]
		if !ok {
			missing = append(missing, strconv.Itoa(template.ID))
			continue
		}
		expected, err := time.Parse(time.RFC3339Nano, row.ExpectedUpdatedAt)
		if err != nil || row.ExpectedVersion != template.Version || !expected.UTC().Equal(template.UpdatedAt.UTC()) {
			return nil, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("currency_migration_draft_stale: template_id=%d", template.ID)}
		}
		row.TemplateName, row.ReferenceCount = template.Name, template.ReferenceCount
		items = append(items, row)
		delete(byID, template.ID)
	}
	if len(missing) > 0 || len(byID) > 0 {
		extra := make([]string, 0, len(byID))
		for id := range byID {
			extra = append(extra, strconv.Itoa(id))
		}
		return nil, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("currency_migration_draft_template_set_changed: missing=%s extra=%s", strings.Join(boundedStringSlice(missing), ","), strings.Join(boundedStringSlice(extra), ","))}
	}
	return items, nil
}

func loadCurrencyMigrationEpochOnly(ctx context.Context, tx pgx.Tx, settingsRow userSettingsRow) (*int, error) {
	if settingsRow.CurrentReportingCurrencyEpochID == nil {
		return nil, nil
	}
	var epoch int
	if err := tx.QueryRow(ctx, `SELECT epoch FROM reporting_currency_epochs WHERE id = $1`, *settingsRow.CurrentReportingCurrencyEpochID).Scan(&epoch); err != nil {
		return nil, fmt.Errorf("load current reporting currency epoch: %w", err)
	}
	return &epoch, nil
}
