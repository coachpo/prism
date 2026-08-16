package settings

// Archive-only currency migration is deliberately separate from the template
// draft protocol. It proves every legacy FX row is unused, records an
// immutable operation, and advances the inventory head without changing an
// epoch, template revision, or source FX row.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/jackc/pgx/v5"
)

type archiveCurrencyMigrationProjection struct {
	Settings       userSettingsRow
	Inventory      pricingMigrationInventoryRow
	InventoryHash  string
	Epoch          int
	ActiveEpochID  int64
	EvidenceCount  int
	FXEvidencePage pricingMigrationFXEvidencePage
}

func (s *Service) handleCurrencyMigrationArchivePreview(w http.ResponseWriter, r *http.Request, request currencyMigrationDraftPreviewRequest) {
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "currency migration archive preview", func(tx pgx.Tx) (currencyMigrationDraftPreviewResponse, error) {
		if err := auditdomain.CheckAffectedWriterAdmission(r.Context(), tx); err != nil {
			return currencyMigrationDraftPreviewResponse{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "currency_migration_owner_unavailable"}
		}
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return currencyMigrationDraftPreviewResponse{}, err
		}
		projection, err := s.loadArchiveCurrencyMigrationProjection(r.Context(), tx, profile.ID, request, false)
		if err != nil {
			return currencyMigrationDraftPreviewResponse{}, err
		}
		previewHash := archiveCurrencyMigrationPreviewHash(request, projection)
		inventoryID := strconv.FormatInt(projection.Inventory.InventoryID, 10)
		currentCode := nullableNonEmptyString(projection.Settings.ReportCurrencyCode)
		return currencyMigrationDraftPreviewResponse{
			OperationKind: request.OperationKind, MigrationOperationID: request.MigrationOperationID,
			TargetCurrencyCode: projection.Settings.ReportCurrencyCode, TargetCurrencySymbol: projection.Settings.ReportCurrencySymbol,
			CurrentCurrencyCode: currentCode, CurrentEpoch: &projection.Epoch, NextEpoch: nil,
			TemplateCount: 0, RevisionChangeCount: 0, TemplatePage: currencyMigrationPreviewPage{Items: []currencyMigrationPreviewItem{}, TotalCount: 0, Limit: currencyDraftPreviewLimit},
			PreviewHash: previewHash, DraftHash: "", DraftID: "", Committable: true, ValidationErrors: []map[string]any{},
			EpochChange: false, InventoryID: &inventoryID, InventoryHash: &projection.InventoryHash,
			ArchivedFXEvidenceCount: projection.EvidenceCount, FXEvidencePage: &projection.FXEvidencePage,
		}, nil
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func (s *Service) loadArchiveCurrencyMigrationProjection(ctx context.Context, tx pgx.Tx, profileID int, request currencyMigrationDraftPreviewRequest, forUpdate bool) (archiveCurrencyMigrationProjection, error) {
	if request.OperationKind != "archive_unused_fx" {
		return archiveCurrencyMigrationProjection{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "currency_migration_invalid_kind: archive preview requires archive_unused_fx"}
	}
	if request.DraftID != "" || request.DraftHash != "" {
		return archiveCurrencyMigrationProjection{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "currency_migration_invalid_kind: archive preview does not accept a template draft"}
	}
	if request.ExpectedInventoryID == nil || request.ExpectedInventoryHash == nil || request.ExpectedInventoryGeneration == nil || request.ExpectedSettingsUpdatedAt == "" || request.ExpectedReportingCurrencyEpoch == nil {
		return archiveCurrencyMigrationProjection{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "currency_migration_inventory_conflict: archive preview requires inventory and settings CAS"}
	}
	inventoryID, err := parsePositiveInventoryID(*request.ExpectedInventoryID)
	if err != nil {
		return archiveCurrencyMigrationProjection{}, err
	}
	settings, found, err := loadUserSettings(ctx, tx, profileID, forUpdate)
	if err != nil {
		return archiveCurrencyMigrationProjection{}, err
	}
	if !found {
		return archiveCurrencyMigrationProjection{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_required: currency settings are missing"}
	}
	expectedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(request.ExpectedSettingsUpdatedAt))
	if err != nil || !settings.UpdatedAt.UTC().Truncate(time.Second).Equal(expectedAt.UTC().Truncate(time.Second)) {
		return archiveCurrencyMigrationProjection{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_stale: costing settings changed"}
	}
	if settings.CurrentReportingCurrencyEpochID == nil {
		return archiveCurrencyMigrationProjection{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_inventory_conflict: archive requires an active reporting currency epoch"}
	}
	var epoch int
	var activeEpochID int64
	epochQuery := `SELECT id, epoch FROM reporting_currency_epochs WHERE id = $1 AND profile_id = $2 AND superseded_at IS NULL`
	if forUpdate {
		epochQuery += ` FOR UPDATE`
	}
	if err := tx.QueryRow(ctx, epochQuery, *settings.CurrentReportingCurrencyEpochID, profileID).Scan(&activeEpochID, &epoch); err != nil {
		return archiveCurrencyMigrationProjection{}, fmt.Errorf("load active currency epoch for archive: %w", err)
	}
	if request.ExpectedReportingCurrencyEpoch == nil || *request.ExpectedReportingCurrencyEpoch != int64(epoch) {
		return archiveCurrencyMigrationProjection{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_stale: reporting currency epoch changed"}
	}
	inventory, err := loadCurrencyMigrationInventory(ctx, tx, profileID, &inventoryID)
	if err != nil {
		return archiveCurrencyMigrationProjection{}, err
	}
	if inventory == nil || inventory.Generation != *request.ExpectedInventoryGeneration || currencyMigrationInventoryHash(*inventory) != strings.TrimSpace(*request.ExpectedInventoryHash) {
		return archiveCurrencyMigrationProjection{}, currencyMigrationInventoryStale()
	}
	if inventory.FXEvidenceCount < 1 || inventory.FXDependencyCount != 0 || len(inventory.IssueCodes) != 1 || inventory.IssueCodes[0] != "unused_fx_evidence" {
		return archiveCurrencyMigrationProjection{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_inventory_conflict: inventory is not archive-only"}
	}
	var nonUnused int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM currency_migration_legacy_fx_evidence AS evidence LEFT JOIN currency_migration_legacy_fx_assessments AS assessments ON assessments.legacy_fx_evidence_id = evidence.legacy_fx_evidence_id WHERE evidence.inventory_id = $1 AND assessments.attribution IS DISTINCT FROM 'unused'`, inventoryID).Scan(&nonUnused); err != nil {
		return archiveCurrencyMigrationProjection{}, err
	}
	if nonUnused != 0 {
		return archiveCurrencyMigrationProjection{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_stale: FX evidence assessment changed"}
	}
	var nonReady int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_templates AS templates LEFT JOIN pricing_template_revisions AS revisions ON revisions.id = templates.current_revision_id WHERE templates.profile_id = $1 AND templates.deleted_at IS NULL AND (revisions.id IS NULL OR revisions.currency_code IS DISTINCT FROM $2 OR revisions.reporting_currency_epoch_id IS DISTINCT FROM $3 OR revisions.currency_attribution <> 'active_epoch' OR revisions.input_price IS NULL OR revisions.output_price IS NULL)`, profileID, settings.ReportCurrencyCode, activeEpochID).Scan(&nonReady); err != nil {
		return archiveCurrencyMigrationProjection{}, err
	}
	if nonReady != 0 {
		return archiveCurrencyMigrationProjection{}, &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_inventory_conflict: active templates are not single-currency ready"}
	}
	page, err := s.loadArchiveFXEvidencePage(ctx, tx, profileID, *inventory, currencyDraftPreviewLimit, 0)
	if err != nil {
		return archiveCurrencyMigrationProjection{}, err
	}
	return archiveCurrencyMigrationProjection{Settings: settings, Inventory: *inventory, InventoryHash: currencyMigrationInventoryHash(*inventory), Epoch: epoch, ActiveEpochID: activeEpochID, EvidenceCount: inventory.FXEvidenceCount, FXEvidencePage: page}, nil
}

func (s *Service) loadArchiveFXEvidencePage(ctx context.Context, tx pgx.Tx, profileID int, inventory pricingMigrationInventoryRow, limit, lastID int) (pricingMigrationFXEvidencePage, error) {
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM currency_migration_legacy_fx_evidence WHERE inventory_id = $1`, inventory.InventoryID).Scan(&total); err != nil {
		return pricingMigrationFXEvidencePage{}, err
	}
	rows, err := tx.Query(ctx, `SELECT evidence.legacy_fx_evidence_id, evidence.source_fx_row_id, evidence.model_id, evidence.endpoint_id, evidence.fx_rate, evidence.source_created_at, evidence.source_updated_at, evidence.row_hash, assessments.attribution, assessments.scan_proof_code, assessments.scan_proof_hash, (SELECT count(*) FROM currency_migration_legacy_fx_dependencies AS dependencies WHERE dependencies.legacy_fx_evidence_id = evidence.legacy_fx_evidence_id) FROM currency_migration_legacy_fx_evidence AS evidence LEFT JOIN currency_migration_legacy_fx_assessments AS assessments ON assessments.legacy_fx_evidence_id = evidence.legacy_fx_evidence_id WHERE evidence.inventory_id = $1 AND evidence.legacy_fx_evidence_id > $2 ORDER BY evidence.legacy_fx_evidence_id ASC LIMIT $3`, inventory.InventoryID, lastID, limit)
	if err != nil {
		return pricingMigrationFXEvidencePage{}, err
	}
	defer rows.Close()
	items := make([]pricingMigrationFXEvidence, 0, limit)
	last := lastID
	for rows.Next() {
		var item pricingMigrationFXEvidence
		var evidenceID, sourceID, endpointID int64
		var createdAt, updatedAt time.Time
		var attribution, proofCode, proofHash sql.NullString
		if err := rows.Scan(&evidenceID, &sourceID, &item.ModelID, &endpointID, &item.FXRate, &createdAt, &updatedAt, &item.RowHash, &attribution, &proofCode, &proofHash, &item.DependencyCount); err != nil {
			return pricingMigrationFXEvidencePage{}, err
		}
		item.EvidenceID = strconv.FormatInt(evidenceID, 10)
		item.SourceFXRowID = strconv.FormatInt(sourceID, 10)
		item.EndpointID = strconv.FormatInt(endpointID, 10)
		item.SourceCreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		item.SourceUpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		item.Attribution, item.ScanProofCode, item.ScanProofHash = attribution.String, proofCode.String, proofHash.String
		items = append(items, item)
		last = int(evidenceID)
	}
	if err := rows.Err(); err != nil {
		return pricingMigrationFXEvidencePage{}, err
	}
	page := pricingMigrationFXEvidencePage{InventoryID: strconv.FormatInt(inventory.InventoryID, 10), InventoryHash: currencyMigrationInventoryHash(inventory), Generation: inventory.Generation, Items: items, TotalCount: total}
	if len(items) > 0 {
		var remaining int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM currency_migration_legacy_fx_evidence WHERE inventory_id = $1 AND legacy_fx_evidence_id > $2`, inventory.InventoryID, last).Scan(&remaining); err != nil {
			return pricingMigrationFXEvidencePage{}, err
		}
		if remaining > 0 {
			cursor := s.encodeCurrencyDraftCursor(currencyDraftCursor{ProfileID: profileID, DraftID: strconv.FormatInt(inventory.InventoryID, 10), Kind: "inventory-fx", Binding: currencyMigrationInventoryHash(inventory), LastID: last})
			page.NextCursor = &cursor
		}
	}
	return page, nil
}

func archiveCurrencyMigrationPreviewHash(request currencyMigrationDraftPreviewRequest, projection archiveCurrencyMigrationProjection) string {
	canonical := struct {
		OperationID   string  `json:"operation_id"`
		InventoryID   int64   `json:"inventory_id"`
		InventoryHash string  `json:"inventory_hash"`
		SettingsAt    string  `json:"settings_at"`
		Epoch         int     `json:"epoch"`
		FXRoot        *string `json:"fx_root"`
		FXCount       int     `json:"fx_count"`
	}{request.MigrationOperationID, projection.Inventory.InventoryID, projection.InventoryHash, projection.Settings.UpdatedAt.UTC().Format(time.RFC3339Nano), projection.Epoch, projection.Inventory.FXEvidenceHashRoot, projection.EvidenceCount}
	raw, _ := json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func archiveCurrencyMigrationPayloadHash(request currencyMigrationDraftCommitRequest) string {
	value := fmt.Sprintf("archive_unused_fx|%s|%s|%s|%d|%s", request.MigrationOperationID, valueOrString(request.ExpectedInventoryID), valueOrString(request.ExpectedInventoryHash), valueOrInt64(request.ExpectedInventoryGeneration), strings.TrimSpace(request.PreviewHash))
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func valueOrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *Service) handleCurrencyMigrationArchiveCommit(w http.ResponseWriter, r *http.Request, request currencyMigrationDraftCommitRequest) {
	response, err := pgxutil.InRepeatableReadWriteTxValue(r.Context(), s.pool, "currency migration archive commit", func(tx pgx.Tx) (currencyMigrationCommitResponse, error) {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return currencyMigrationCommitResponse{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "currency_migration_owner_unavailable"}
		}
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		operationID, err := normalizeUUIDV4(request.MigrationOperationID)
		if err != nil {
			return currencyMigrationCommitResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "migration_operation_id must be a UUIDv4"}
		}
		request.MigrationOperationID = operationID
		payloadHash := archiveCurrencyMigrationPayloadHash(request)
		if existing, ok, err := loadCurrencyMigrationResult(r.Context(), tx, operationID); err != nil {
			return currencyMigrationCommitResponse{}, err
		} else if ok {
			var kind, oldPayload, oldPreview string
			if err := tx.QueryRow(r.Context(), `SELECT result_kind, normalized_payload_hash, preview_hash FROM pricing_mutation_operations WHERE operation_id = $1::uuid`, operationID).Scan(&kind, &oldPayload, &oldPreview); err != nil {
				return currencyMigrationCommitResponse{}, err
			}
			if kind != request.OperationKind || oldPayload != payloadHash || oldPreview != strings.TrimSpace(request.PreviewHash) {
				return currencyMigrationCommitResponse{}, currencyMigrationOperationConflict()
			}
			return existing, nil
		}
		if err := lockCurrencyProfile(r.Context(), tx, profile.ID); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		projection, err := s.loadArchiveCurrencyMigrationProjection(r.Context(), tx, profile.ID, currencyMigrationDraftPreviewRequest{
			OperationKind: request.OperationKind, MigrationOperationID: operationID, ExpectedInventoryID: request.ExpectedInventoryID,
			ExpectedInventoryHash: request.ExpectedInventoryHash, ExpectedInventoryGeneration: request.ExpectedInventoryGeneration,
			ExpectedReportingCurrencyEpoch: request.ExpectedReportingCurrencyEpoch, ExpectedSettingsUpdatedAt: request.ExpectedSettingsUpdatedAt,
		}, true)
		if err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		previewRequest := currencyMigrationDraftPreviewRequest{OperationKind: request.OperationKind, MigrationOperationID: operationID, ExpectedInventoryID: request.ExpectedInventoryID, ExpectedInventoryHash: request.ExpectedInventoryHash, ExpectedInventoryGeneration: request.ExpectedInventoryGeneration, ExpectedReportingCurrencyEpoch: request.ExpectedReportingCurrencyEpoch, ExpectedSettingsUpdatedAt: request.ExpectedSettingsUpdatedAt}
		if archiveCurrencyMigrationPreviewHash(previewRequest, projection) != strings.TrimSpace(request.PreviewHash) {
			return currencyMigrationCommitResponse{}, currencyMigrationPreviewStale()
		}
		if err := claimCurrencyMigrationReservation(r.Context(), tx, profile.ID, operationID, request.OperationKind, payloadHash, s.nowUTC()); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		archivedAt := s.nowUTC()
		if err := tx.QueryRow(r.Context(), `SELECT clock_timestamp()`).Scan(&archivedAt); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		oldEpoch := projection.Epoch
		inventoryID := strconv.FormatInt(projection.Inventory.InventoryID, 10)
		response := currencyMigrationCommitResponse{OldCurrencyCode: nullableNonEmptyString(projection.Settings.ReportCurrencyCode), NewCurrencyCode: projection.Settings.ReportCurrencyCode, OldEpoch: &oldEpoch, NewEpoch: nil, MigrationOperationID: operationID, EpochChange: false, ArchivedFXEvidenceCount: projection.EvidenceCount, InventoryID: &inventoryID}
		resultRaw, _ := json.Marshal(response)
		resultHash := sha256.Sum256(resultRaw)
		if _, err := tx.Exec(r.Context(), `INSERT INTO pricing_mutation_operations (operation_id, profile_id, result_kind, normalized_payload_hash, preview_hash, operation_recorded_at, success_summary, result_hash, created_at) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::jsonb, $8, $6)`, operationID, profile.ID, request.OperationKind, payloadHash, strings.TrimSpace(request.PreviewHash), archivedAt, resultRaw, hex.EncodeToString(resultHash[:])); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		itemsHash := sha256.Sum256(nil)
		if _, err := tx.Exec(r.Context(), `INSERT INTO currency_migration_ledger (operation_id, operation_kind, profile_id, old_epoch_id, old_epoch, new_epoch_id, new_epoch, legacy_reporting_currency_evidence_id, normalized_payload_hash, inventory_id, inventory_hash, item_count, items_hash, committed_result, committed_at) VALUES ($1::uuid, 'archive_unused_fx', $2, $3, $4, NULL, NULL, NULL, $5, $6, $7, 0, $8, $9::jsonb, $10)`, operationID, profile.ID, projection.ActiveEpochID, projection.Epoch, payloadHash, projection.Inventory.InventoryID, projection.InventoryHash, hex.EncodeToString(itemsHash[:]), resultRaw, archivedAt); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO pricing_migration_inventories (profile_id, generation, supersedes_inventory_id, settings_generation, epoch_generation, template_generation, reference_generation, issue_codes, fx_evidence_count, fx_assessment_count, fx_dependency_count, template_evidence_count, reporting_currency_evidence_count, fx_evidence_hash_root, template_evidence_hash_root, reporting_currency_evidence_hash_root, legacy_fx_source_count, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, '{}', 0, 0, 0, 0, 0, NULL, NULL, NULL, 0, $8)`, profile.ID, projection.Inventory.Generation+1, projection.Inventory.InventoryID, projection.Inventory.SettingsGeneration, projection.Inventory.EpochGeneration, projection.Inventory.TemplateGeneration, projection.Inventory.ReferenceGeneration+1, archivedAt); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE user_settings SET pricing_migration_state = 'ready', legacy_migration_issues = '{}', pricing_reference_generation = pricing_reference_generation + 1, updated_at = $2 WHERE id = $1`, projection.Settings.ID, archivedAt); err != nil {
			return currencyMigrationCommitResponse{}, err
		}
		return response, nil
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}
