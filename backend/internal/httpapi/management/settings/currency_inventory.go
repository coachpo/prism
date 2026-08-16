package settings

// This file is the Settings read-only handoff to Pricing migration
// inventories. It never creates, updates, or archives inventory evidence;
// the Pricing migration scanner owns those rows.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"
)

type pricingMigrationReportingCurrencyEvidence struct {
	EvidenceID        string   `json:"evidence_id"`
	RawCurrencyCode   string   `json:"raw_currency_code"`
	RawCurrencySymbol string   `json:"raw_currency_symbol"`
	SettingsUpdatedAt string   `json:"settings_updated_at"`
	ValidationCodes   []string `json:"validation_codes"`
}

type pricingMigrationInventorySummary struct {
	InventoryID               string                                     `json:"inventory_id"`
	InventoryHash             string                                     `json:"inventory_hash"`
	Generation                int64                                      `json:"generation"`
	IssueCodes                []string                                   `json:"issue_codes"`
	TemplateIssueCount        int                                        `json:"template_issue_count"`
	LegacyFXRowCount          int                                        `json:"legacy_fx_row_count"`
	LiveFXDependencyCount     int                                        `json:"live_fx_dependency_count"`
	RecommendedOperationKind  string                                     `json:"recommended_operation_kind"`
	ArchiveOnlyAvailable      bool                                       `json:"archive_only_available"`
	TemplateScaffoldURL       string                                     `json:"template_scaffold_url"`
	FXEvidenceURL             string                                     `json:"fx_evidence_url"`
	ReportingCurrencyEvidence *pricingMigrationReportingCurrencyEvidence `json:"reporting_currency_evidence,omitempty"`
}

type pricingMigrationInventoryPage struct {
	InventoryID              string                              `json:"inventory_id"`
	InventoryHash            string                              `json:"inventory_hash"`
	Generation               int64                               `json:"generation"`
	TotalActiveTemplateCount int                                 `json:"total_active_template_count"`
	Items                    []pricingMigrationInventoryTemplate `json:"items"`
	TotalCount               int                                 `json:"total_count"`
	NextCursor               *string                             `json:"next_cursor"`
}

type pricingMigrationInventoryTemplate struct {
	TemplateID                   int      `json:"template_id"`
	Name                         string   `json:"name"`
	UpdatedAt                    string   `json:"updated_at"`
	BaseVersion                  int      `json:"base_version"`
	CurrentRevisionID            *string  `json:"current_revision_id"`
	CurrentInputPrice            *string  `json:"current_input_price"`
	CurrentOutputPrice           *string  `json:"current_output_price"`
	CurrentCachedInputPrice      *string  `json:"current_cached_input_price"`
	CurrentCacheCreationPrice    *string  `json:"current_cache_creation_price"`
	CurrentReasoningPrice        *string  `json:"current_reasoning_price"`
	LegacyEvidenceID             *string  `json:"legacy_template_evidence_id"`
	RawPricingUnit               *string  `json:"raw_pricing_unit"`
	RawCurrencyCode              *string  `json:"raw_currency_code"`
	RawInputPrice                *string  `json:"raw_input_price"`
	RawOutputPrice               *string  `json:"raw_output_price"`
	RawCachedInputPrice          *string  `json:"raw_cached_input_price"`
	RawCacheCreationPrice        *string  `json:"raw_cache_creation_price"`
	RawReasoningPrice            *string  `json:"raw_reasoning_price"`
	IssueCodes                   []string `json:"issue_codes"`
	ModelReferenceCount          int      `json:"model_reference_count"`
	EndpointReferenceCount       int      `json:"endpoint_reference_count"`
	TerminalTargetReferenceCount int      `json:"terminal_target_reference_count"`
}

type pricingMigrationFXEvidencePage struct {
	InventoryID   string                       `json:"inventory_id"`
	InventoryHash string                       `json:"inventory_hash"`
	Generation    int64                        `json:"generation"`
	Items         []pricingMigrationFXEvidence `json:"items"`
	TotalCount    int                          `json:"total_count"`
	NextCursor    *string                      `json:"next_cursor"`
}

type pricingMigrationFXEvidence struct {
	EvidenceID      string `json:"legacy_fx_evidence_id"`
	SourceFXRowID   string `json:"source_fx_row_id"`
	ModelID         string `json:"model_id"`
	EndpointID      string `json:"endpoint_id"`
	FXRate          string `json:"fx_rate"`
	SourceCreatedAt string `json:"source_created_at"`
	SourceUpdatedAt string `json:"source_updated_at"`
	RowHash         string `json:"row_hash"`
	Attribution     string `json:"attribution"`
	ScanProofCode   string `json:"scan_proof_code"`
	ScanProofHash   string `json:"scan_proof_hash"`
	DependencyCount int    `json:"dependency_count"`
}

type pricingMigrationInventoryRow struct {
	InventoryID                    int64
	ProfileID                      int
	Generation                     int64
	SettingsGeneration             int64
	EpochGeneration                *int64
	TemplateGeneration             int64
	ReferenceGeneration            int64
	IssueCodes                     []string
	FXEvidenceCount                int
	FXAssessmentCount              int
	FXDependencyCount              int
	TemplateEvidenceCount          int
	ReportingCurrencyEvidenceCount int
	FXEvidenceHashRoot             *string
	TemplateEvidenceHashRoot       *string
	ReportingCurrencyHashRoot      *string
	LegacyFXSourceCount            int64
}

func (s *Service) handleListCurrencyMigrationInventoryTemplates(w http.ResponseWriter, r *http.Request) {
	page, err := s.loadCurrencyMigrationInventoryTemplatePage(r)
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, page)
}

func (s *Service) handleListCurrencyMigrationInventoryFXEvidence(w http.ResponseWriter, r *http.Request) {
	page, err := s.loadCurrencyMigrationInventoryFXEvidencePage(r)
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, page)
}

func (s *Service) loadCurrencyMigrationInventoryTemplatePage(r *http.Request) (pricingMigrationInventoryPage, error) {
	return pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "currency migration inventory templates", func(tx pgx.Tx) (pricingMigrationInventoryPage, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return pricingMigrationInventoryPage{}, err
		}
		inventoryID, err := parsePositiveInventoryID(chi.URLParam(r, "inventory_id"))
		if err != nil {
			return pricingMigrationInventoryPage{}, err
		}
		inventory, err := loadCurrencyMigrationInventory(r.Context(), tx, profile.ID, &inventoryID)
		if err != nil {
			return pricingMigrationInventoryPage{}, err
		}
		if inventory == nil {
			return pricingMigrationInventoryPage{}, currencyMigrationInventoryStale()
		}
		hash := currencyMigrationInventoryHash(*inventory)
		cursor, err := s.decodeCurrencyDraftCursor(r.URL.Query().Get("cursor"), currencyDraftCursor{ProfileID: profile.ID, DraftID: strconv.FormatInt(inventory.InventoryID, 10), Kind: "inventory-templates", Binding: hash})
		if err != nil {
			return pricingMigrationInventoryPage{}, err
		}
		limit, err := currencyMigrationInventoryPageLimit(r, 50, 100)
		if err != nil {
			return pricingMigrationInventoryPage{}, err
		}
		var total int
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM pricing_templates WHERE profile_id = $1 AND deleted_at IS NULL`, profile.ID).Scan(&total); err != nil {
			return pricingMigrationInventoryPage{}, fmt.Errorf("count inventory templates: %w", err)
		}
		rows, err := tx.Query(r.Context(), `
			SELECT templates.id, templates.name, templates.updated_at, templates.current_revision_id,
				revisions.id, revisions.version, revisions.input_price, revisions.output_price,
				revisions.cached_input_price, revisions.cache_creation_price, revisions.reasoning_price,
				evidence.legacy_template_evidence_id, evidence.public_version, evidence.pricing_unit, evidence.currency_code,
				evidence.input_price, evidence.output_price, evidence.cached_input_price,
				evidence.cache_creation_price, evidence.reasoning_price, evidence.issue_codes,
				(SELECT count(DISTINCT targets.source_model_config_id) FROM model_access_targets AS targets JOIN connections AS refs ON refs.id = targets.target_connection_id WHERE refs.profile_id = $1 AND refs.pricing_template_id = templates.id AND targets.profile_id = $1 AND targets.target_type = 'connection'),
				(SELECT count(DISTINCT refs.endpoint_id) FROM connections AS refs WHERE refs.profile_id = $1 AND refs.pricing_template_id = templates.id),
				(SELECT count(DISTINCT refs.id) FROM connections AS refs WHERE refs.profile_id = $1 AND refs.pricing_template_id = templates.id)
			FROM pricing_templates AS templates
			LEFT JOIN pricing_template_revisions AS revisions ON revisions.id = templates.current_revision_id
			LEFT JOIN pricing_migration_legacy_template_evidence AS evidence ON evidence.inventory_id = $2 AND evidence.template_id = templates.id
			WHERE templates.profile_id = $1 AND templates.deleted_at IS NULL AND templates.id > $3
			ORDER BY templates.id ASC LIMIT $4`, profile.ID, inventory.InventoryID, cursor.LastID, limit)
		if err != nil {
			return pricingMigrationInventoryPage{}, fmt.Errorf("load inventory template page: %w", err)
		}
		defer rows.Close()
		items := make([]pricingMigrationInventoryTemplate, 0, limit)
		lastID := cursor.LastID
		for rows.Next() {
			item, err := scanCurrencyMigrationInventoryTemplate(rows)
			if err != nil {
				return pricingMigrationInventoryPage{}, err
			}
			items = append(items, item)
			lastID = item.TemplateID
		}
		if err := rows.Err(); err != nil {
			return pricingMigrationInventoryPage{}, err
		}
		page := pricingMigrationInventoryPage{InventoryID: strconv.FormatInt(inventory.InventoryID, 10), InventoryHash: hash, Generation: inventory.Generation, TotalActiveTemplateCount: total, Items: items, TotalCount: total}
		if len(items) > 0 {
			var remaining int
			if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM pricing_templates WHERE profile_id = $1 AND deleted_at IS NULL AND id > $2`, profile.ID, lastID).Scan(&remaining); err != nil {
				return pricingMigrationInventoryPage{}, err
			}
			if remaining > 0 {
				value := s.encodeCurrencyDraftCursor(currencyDraftCursor{ProfileID: profile.ID, DraftID: strconv.FormatInt(inventory.InventoryID, 10), Kind: "inventory-templates", Binding: hash, LastID: lastID})
				page.NextCursor = &value
			}
		}
		return page, nil
	})
}

func (s *Service) loadCurrencyMigrationInventoryFXEvidencePage(r *http.Request) (pricingMigrationFXEvidencePage, error) {
	return pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "currency migration inventory fx evidence", func(tx pgx.Tx) (pricingMigrationFXEvidencePage, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return pricingMigrationFXEvidencePage{}, err
		}
		inventoryID, err := parsePositiveInventoryID(chi.URLParam(r, "inventory_id"))
		if err != nil {
			return pricingMigrationFXEvidencePage{}, err
		}
		inventory, err := loadCurrencyMigrationInventory(r.Context(), tx, profile.ID, &inventoryID)
		if err != nil {
			return pricingMigrationFXEvidencePage{}, err
		}
		if inventory == nil {
			return pricingMigrationFXEvidencePage{}, currencyMigrationInventoryStale()
		}
		hash := currencyMigrationInventoryHash(*inventory)
		cursor, err := s.decodeCurrencyDraftCursor(r.URL.Query().Get("cursor"), currencyDraftCursor{ProfileID: profile.ID, DraftID: strconv.FormatInt(inventory.InventoryID, 10), Kind: "inventory-fx", Binding: hash})
		if err != nil {
			return pricingMigrationFXEvidencePage{}, err
		}
		limit, err := currencyMigrationInventoryPageLimit(r, 50, 100)
		if err != nil {
			return pricingMigrationFXEvidencePage{}, err
		}
		var total int
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM currency_migration_legacy_fx_evidence WHERE inventory_id = $1`, inventory.InventoryID).Scan(&total); err != nil {
			return pricingMigrationFXEvidencePage{}, err
		}
		rows, err := tx.Query(r.Context(), `
			SELECT evidence.legacy_fx_evidence_id, evidence.source_fx_row_id, evidence.model_id, evidence.endpoint_id,
				evidence.fx_rate, evidence.source_created_at, evidence.source_updated_at, evidence.row_hash,
				assessments.attribution, assessments.scan_proof_code, assessments.scan_proof_hash,
				(SELECT count(*) FROM currency_migration_legacy_fx_dependencies AS dependencies WHERE dependencies.legacy_fx_evidence_id = evidence.legacy_fx_evidence_id)
			FROM currency_migration_legacy_fx_evidence AS evidence
			LEFT JOIN currency_migration_legacy_fx_assessments AS assessments ON assessments.legacy_fx_evidence_id = evidence.legacy_fx_evidence_id
			WHERE evidence.inventory_id = $1 AND evidence.legacy_fx_evidence_id > $2
			ORDER BY evidence.legacy_fx_evidence_id ASC LIMIT $3`, inventory.InventoryID, cursor.LastID, limit)
		if err != nil {
			return pricingMigrationFXEvidencePage{}, fmt.Errorf("load inventory fx evidence page: %w", err)
		}
		defer rows.Close()
		items := make([]pricingMigrationFXEvidence, 0, limit)
		lastID := cursor.LastID
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
			lastID = int(evidenceID)
		}
		if err := rows.Err(); err != nil {
			return pricingMigrationFXEvidencePage{}, err
		}
		page := pricingMigrationFXEvidencePage{InventoryID: strconv.FormatInt(inventory.InventoryID, 10), InventoryHash: hash, Generation: inventory.Generation, Items: items, TotalCount: total}
		if len(items) > 0 {
			var remaining int
			if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM currency_migration_legacy_fx_evidence WHERE inventory_id = $1 AND legacy_fx_evidence_id > $2`, inventory.InventoryID, lastID).Scan(&remaining); err != nil {
				return pricingMigrationFXEvidencePage{}, err
			}
			if remaining > 0 {
				value := s.encodeCurrencyDraftCursor(currencyDraftCursor{ProfileID: profile.ID, DraftID: strconv.FormatInt(inventory.InventoryID, 10), Kind: "inventory-fx", Binding: hash, LastID: lastID})
				page.NextCursor = &value
			}
		}
		return page, nil
	})
}

func scanCurrencyMigrationInventoryTemplate(row pgx.Row) (pricingMigrationInventoryTemplate, error) {
	var item pricingMigrationInventoryTemplate
	var updatedAt time.Time
	var currentRevisionID, revisionID, version sql.NullInt64
	var currentInput, currentOutput, currentCached, currentCreation, currentReasoning sql.NullString
	var evidenceID, evidenceVersion sql.NullInt64
	var rawUnit, rawCode, rawInput, rawOutput, rawCached, rawCreation, rawReasoning sql.NullString
	var issueCodes []string
	if err := row.Scan(&item.TemplateID, &item.Name, &updatedAt, &currentRevisionID, &revisionID, &version, &currentInput, &currentOutput, &currentCached, &currentCreation, &currentReasoning, &evidenceID, &evidenceVersion, &rawUnit, &rawCode, &rawInput, &rawOutput, &rawCached, &rawCreation, &rawReasoning, &issueCodes, &item.ModelReferenceCount, &item.EndpointReferenceCount, &item.TerminalTargetReferenceCount); err != nil {
		return pricingMigrationInventoryTemplate{}, err
	}
	item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	item.BaseVersion = int(version.Int64)
	if !version.Valid && evidenceID.Valid && evidenceVersion.Valid {
		item.BaseVersion = int(evidenceVersion.Int64)
	}
	if currentRevisionID.Valid {
		value := strconv.FormatInt(currentRevisionID.Int64, 10)
		item.CurrentRevisionID = &value
	}
	if revisionID.Valid {
		item.BaseVersion = int(version.Int64)
	}
	item.CurrentInputPrice = nullableSQLString(currentInput)
	item.CurrentOutputPrice = nullableSQLString(currentOutput)
	item.CurrentCachedInputPrice = nullableSQLString(currentCached)
	item.CurrentCacheCreationPrice = nullableSQLString(currentCreation)
	item.CurrentReasoningPrice = nullableSQLString(currentReasoning)
	if evidenceID.Valid {
		value := strconv.FormatInt(evidenceID.Int64, 10)
		item.LegacyEvidenceID = &value
	}
	item.RawPricingUnit = nullableSQLString(rawUnit)
	item.RawCurrencyCode = nullableSQLString(rawCode)
	item.RawInputPrice = nullableSQLString(rawInput)
	item.RawOutputPrice = nullableSQLString(rawOutput)
	item.RawCachedInputPrice = nullableSQLString(rawCached)
	item.RawCacheCreationPrice = nullableSQLString(rawCreation)
	item.RawReasoningPrice = nullableSQLString(rawReasoning)
	if issueCodes == nil {
		issueCodes = []string{}
	}
	item.IssueCodes = issueCodes
	return item, nil
}

func nullableSQLString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func loadCurrencyMigrationInventory(ctx context.Context, tx pgx.Tx, profileID int, requestedID *int64) (*pricingMigrationInventoryRow, error) {
	where := `inventory.profile_id = $1 AND NOT EXISTS (SELECT 1 FROM pricing_migration_inventories AS successor WHERE successor.supersedes_inventory_id = inventory.inventory_id)`
	args := []any{profileID}
	if requestedID != nil {
		where += ` AND inventory.inventory_id = $2`
		args = append(args, *requestedID)
	}
	row := &pricingMigrationInventoryRow{}
	query := `SELECT inventory.inventory_id, inventory.profile_id, inventory.generation, inventory.settings_generation,
		inventory.epoch_generation, inventory.template_generation, inventory.reference_generation, inventory.issue_codes,
		inventory.fx_evidence_count, inventory.fx_assessment_count, inventory.fx_dependency_count, inventory.template_evidence_count,
		inventory.reporting_currency_evidence_count, inventory.fx_evidence_hash_root, inventory.template_evidence_hash_root,
		inventory.reporting_currency_evidence_hash_root, inventory.legacy_fx_source_count
		FROM pricing_migration_inventories AS inventory WHERE ` + where + ` ORDER BY inventory.generation DESC LIMIT 1`
	if err := tx.QueryRow(ctx, query, args...).Scan(&row.InventoryID, &row.ProfileID, &row.Generation, &row.SettingsGeneration, &row.EpochGeneration, &row.TemplateGeneration, &row.ReferenceGeneration, &row.IssueCodes, &row.FXEvidenceCount, &row.FXAssessmentCount, &row.FXDependencyCount, &row.TemplateEvidenceCount, &row.ReportingCurrencyEvidenceCount, &row.FXEvidenceHashRoot, &row.TemplateEvidenceHashRoot, &row.ReportingCurrencyHashRoot, &row.LegacyFXSourceCount); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load pricing migration inventory: %w", err)
	}
	return row, nil
}

func loadPricingMigrationInventorySummary(ctx context.Context, tx pgx.Tx, profileID int, pending bool) (*pricingMigrationInventorySummary, error) {
	inventory, err := loadCurrencyMigrationInventory(ctx, tx, profileID, nil)
	if err != nil || inventory == nil {
		return nil, err
	}
	if len(inventory.IssueCodes) == 0 && inventory.FXEvidenceCount == 0 && inventory.TemplateEvidenceCount == 0 && inventory.ReportingCurrencyEvidenceCount == 0 {
		return nil, nil
	}
	hash := currencyMigrationInventoryHash(*inventory)
	issues := append([]string(nil), inventory.IssueCodes...)
	if issues == nil {
		issues = []string{}
	}
	blocking := false
	for _, issue := range issues {
		if issue != "unused_fx_evidence" {
			blocking = true
			break
		}
	}
	var nonUnusedAssessments int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM currency_migration_legacy_fx_evidence AS evidence JOIN currency_migration_legacy_fx_assessments AS assessments ON assessments.legacy_fx_evidence_id = evidence.legacy_fx_evidence_id WHERE evidence.inventory_id = $1 AND assessments.attribution <> 'unused'`, inventory.InventoryID).Scan(&nonUnusedAssessments); err != nil {
		return nil, err
	}
	archiveOnly := !blocking && inventory.FXEvidenceCount > 0 && inventory.FXDependencyCount == 0 && nonUnusedAssessments == 0 && inventory.TemplateEvidenceCount == 0 && inventory.ReportingCurrencyEvidenceCount == 0
	recommended := "repair_same_currency"
	if archiveOnly {
		recommended = "archive_unused_fx"
	} else if pending && (containsString(issues, "invalid_reporting_currency_code") || containsString(issues, "invalid_reporting_currency_symbol")) {
		recommended = "currency_cutover"
	}
	summary := &pricingMigrationInventorySummary{
		InventoryID: strconv.FormatInt(inventory.InventoryID, 10), InventoryHash: hash, Generation: inventory.Generation,
		IssueCodes: issues, TemplateIssueCount: inventory.TemplateEvidenceCount, LegacyFXRowCount: inventory.FXEvidenceCount,
		LiveFXDependencyCount: inventory.FXDependencyCount, RecommendedOperationKind: recommended, ArchiveOnlyAvailable: archiveOnly,
		TemplateScaffoldURL: "/api/settings/costing/pricing-migration-inventories/" + strconv.FormatInt(inventory.InventoryID, 10) + "/templates",
		FXEvidenceURL:       "/api/settings/costing/pricing-migration-inventories/" + strconv.FormatInt(inventory.InventoryID, 10) + "/fx-evidence",
	}
	if inventory.ReportingCurrencyEvidenceCount == 1 {
		var evidence pricingMigrationReportingCurrencyEvidence
		var id int64
		var updatedAt, recordedAt time.Time
		if err := tx.QueryRow(ctx, `SELECT evidence.legacy_reporting_currency_evidence_id, evidence.raw_report_currency_code, evidence.raw_report_currency_symbol, evidence.settings_updated_at, evidence.validation_codes, evidence.recorded_at FROM pricing_migration_legacy_reporting_currency_evidence AS evidence WHERE evidence.inventory_id = $1`, inventory.InventoryID).Scan(&id, &evidence.RawCurrencyCode, &evidence.RawCurrencySymbol, &updatedAt, &evidence.ValidationCodes, &recordedAt); err != nil {
			return nil, err
		}
		evidence.EvidenceID = strconv.FormatInt(id, 10)
		evidence.SettingsUpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		summary.ReportingCurrencyEvidence = &evidence
	}
	return summary, nil
}

func currencyMigrationInventoryHash(row pricingMigrationInventoryRow) string {
	value := fmt.Sprintf("%d|%d|%d|%d|%d|%d|%d|%s|%d|%d|%d|%d|%d|%s|%s|%s", row.ProfileID, row.InventoryID, row.Generation, row.SettingsGeneration, valueOrInt64(row.EpochGeneration), row.TemplateGeneration, row.ReferenceGeneration, strings.Join(row.IssueCodes, ","), row.FXEvidenceCount, row.FXAssessmentCount, row.FXDependencyCount, row.TemplateEvidenceCount, row.ReportingCurrencyEvidenceCount, stringValue(row.FXEvidenceHashRoot), stringValue(row.TemplateEvidenceHashRoot), stringValue(row.ReportingCurrencyHashRoot))
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func valueOrInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func parsePositiveInventoryID(raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < 1 || strconv.FormatInt(value, 10) != strings.TrimSpace(raw) {
		return 0, &domainError{StatusCode: http.StatusBadRequest, Detail: "inventory_id must be a canonical positive decimal string"}
	}
	return value, nil
}

func currencyMigrationInventoryPageLimit(r *http.Request, defaultLimit, maxLimit int) (int, error) {
	return currencyDraftPageLimit(r, defaultLimit, maxLimit)
}

func currencyMigrationInventoryStale() error {
	return &domainError{StatusCode: http.StatusConflict, Detail: "currency_migration_inventory_stale: inventory is no longer the current head; reload costing settings"}
}

func validateCurrencyMigrationInventory(ctx context.Context, tx pgx.Tx, profileID int, rawID, expectedHash string, expectedGeneration int64) error {
	inventoryID, err := parsePositiveInventoryID(rawID)
	if err != nil {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "expected_inventory_id must be a canonical positive decimal string"}
	}
	inventory, err := loadCurrencyMigrationInventory(ctx, tx, profileID, &inventoryID)
	if err != nil {
		return err
	}
	if inventory == nil || currencyMigrationInventoryHash(*inventory) != strings.TrimSpace(expectedHash) || inventory.Generation != expectedGeneration {
		return currencyMigrationInventoryStale()
	}
	return nil
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func canonicalCurrencySymbol(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	canonical := norm.NFC.String(trimmed)
	if canonical != trimmed || len([]rune(canonical)) > 5 || len([]byte(canonical)) > 20 {
		return "", false
	}
	for _, value := range canonical {
		if unicode.IsControl(value) || unicode.In(value, unicode.Cf, unicode.Zl, unicode.Zp) {
			return "", false
		}
	}
	return canonical, true
}

func intPtr(value int) *int { return &value }
