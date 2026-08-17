package settings

// This file owns the bounded, resumable currency-migration draft protocol.
// All live routes below consume the server-side draft contract from the
// Pricing spec; no full-list preview/commit compatibility path is mounted.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type currencyMigrationDraftCreateRequest struct {
	DraftID                        string  `json:"draft_id"`
	MigrationOperationID           string  `json:"migration_operation_id"`
	OperationKind                  string  `json:"operation_kind"`
	TargetCurrencyCode             string  `json:"target_currency_code"`
	TargetCurrencySymbol           string  `json:"target_currency_symbol"`
	ExpectedInventoryID            *string `json:"expected_inventory_id"`
	ExpectedInventoryHash          *string `json:"expected_inventory_hash"`
	ExpectedInventoryGeneration    *int64  `json:"expected_inventory_generation"`
	ExpectedReportingCurrencyEpoch *int64  `json:"expected_reporting_currency_epoch"`
	ExpectedSettingsUpdatedAt      string  `json:"expected_settings_updated_at"`
}

type currencyMigrationDraftChunkRowRequest struct {
	TemplateID         int     `json:"template_id"`
	ExpectedVersion    int     `json:"expected_version"`
	ExpectedUpdatedAt  string  `json:"expected_updated_at"`
	InputPrice         *string `json:"input_price"`
	OutputPrice        *string `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
}

// UnmarshalJSON makes the five price keys explicit. In particular, a
// specialty price may be JSON null, but it may not be silently omitted.
func (row *currencyMigrationDraftChunkRowRequest) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	allowed := map[string]struct{}{
		"template_id": {}, "expected_version": {}, "expected_updated_at": {},
		"input_price": {}, "output_price": {}, "cached_input_price": {},
		"cache_creation_price": {}, "reasoning_price": {},
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unknown field %q", key)
		}
	}
	for _, key := range []string{"template_id", "expected_version", "expected_updated_at", "input_price", "output_price", "cached_input_price", "cache_creation_price", "reasoning_price"} {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("%s is required", key)
		}
	}
	type plain currencyMigrationDraftChunkRowRequest
	var decoded plain
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*row = currencyMigrationDraftChunkRowRequest(decoded)
	return nil
}

type currencyMigrationDraftChunkRequest struct {
	Items []currencyMigrationDraftChunkRowRequest `json:"items"`
}

type currencyMigrationDraftPreviewRequest struct {
	OperationKind                  string  `json:"operation_kind"`
	MigrationOperationID           string  `json:"migration_operation_id"`
	DraftID                        string  `json:"draft_id"`
	DraftHash                      string  `json:"draft_hash"`
	ExpectedInventoryID            *string `json:"expected_inventory_id"`
	ExpectedInventoryHash          *string `json:"expected_inventory_hash"`
	ExpectedInventoryGeneration    *int64  `json:"expected_inventory_generation"`
	ExpectedReportingCurrencyEpoch *int64  `json:"expected_reporting_currency_epoch"`
	ExpectedSettingsUpdatedAt      string  `json:"expected_settings_updated_at"`
}

type currencyMigrationDraftCommitRequest struct {
	OperationKind                  string  `json:"operation_kind"`
	MigrationOperationID           string  `json:"migration_operation_id"`
	DraftID                        string  `json:"draft_id"`
	DraftHash                      string  `json:"draft_hash"`
	PreviewHash                    string  `json:"preview_hash"`
	ExpectedInventoryID            *string `json:"expected_inventory_id"`
	ExpectedInventoryHash          *string `json:"expected_inventory_hash"`
	ExpectedInventoryGeneration    *int64  `json:"expected_inventory_generation"`
	ExpectedReportingCurrencyEpoch *int64  `json:"expected_reporting_currency_epoch"`
	ExpectedSettingsUpdatedAt      string  `json:"expected_settings_updated_at"`
}

type currencyMigrationDraftChunkPage struct {
	Items         []currencyMigrationDraftChunkSummary `json:"items"`
	TotalCount    int                                  `json:"total_count"`
	ConsumedCount int                                  `json:"consumed_count"`
	NextCursor    *string                              `json:"next_cursor"`
}

type currencyMigrationDraftChunkSummary struct {
	Ordinal     int    `json:"ordinal"`
	RowCount    int    `json:"row_count"`
	ContentHash string `json:"content_hash"`
}

type currencyMigrationDraftHeaderResponse struct {
	DraftID                        string                          `json:"draft_id"`
	MigrationOperationID           string                          `json:"migration_operation_id"`
	OperationKind                  string                          `json:"operation_kind"`
	TargetCurrencyCode             string                          `json:"target_currency_code"`
	TargetCurrencySymbol           string                          `json:"target_currency_symbol"`
	ExpectedInventoryID            *string                         `json:"expected_inventory_id"`
	ExpectedInventoryHash          *string                         `json:"expected_inventory_hash"`
	ExpectedInventoryGeneration    *int64                          `json:"expected_inventory_generation"`
	ExpectedReportingCurrencyEpoch *int64                          `json:"expected_reporting_currency_epoch"`
	ExpectedSettingsUpdatedAt      string                          `json:"expected_settings_updated_at"`
	Status                         string                          `json:"status"`
	NormalizedHeaderHash           string                          `json:"normalized_header_hash"`
	ReceivedChunkCount             int                             `json:"received_chunk_count"`
	ChunkPage                      currencyMigrationDraftChunkPage `json:"chunk_page"`
	DraftHash                      *string                         `json:"draft_hash"`
	TemplateCount                  *int                            `json:"template_count"`
	CommittedResultOperationID     *string                         `json:"committed_result_operation_id"`
	CreatedAt                      string                          `json:"created_at"`
	UpdatedAt                      string                          `json:"updated_at"`
}

type currencyMigrationDraftItem struct {
	TemplateID         int     `json:"template_id"`
	ExpectedVersion    int     `json:"expected_version"`
	ExpectedUpdatedAt  string  `json:"expected_updated_at"`
	InputPrice         string  `json:"input_price"`
	OutputPrice        string  `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
	TemplateName       string  `json:"template_name,omitempty"`
	ReferenceCount     int64   `json:"reference_count,omitempty"`
}

type currencyMigrationDraftItemResponse struct {
	TemplateID         int     `json:"template_id"`
	TemplateName       string  `json:"template_name"`
	ExpectedVersion    int     `json:"expected_version"`
	ExpectedUpdatedAt  string  `json:"expected_updated_at"`
	InputPrice         string  `json:"input_price"`
	OutputPrice        string  `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
	ReferenceCount     int64   `json:"reference_count"`
}

type currencyMigrationDraftItemPage struct {
	Items      []currencyMigrationDraftItemResponse `json:"items"`
	TotalCount int                                  `json:"total_count"`
	NextCursor *string                              `json:"next_cursor"`
	HasMore    bool                                 `json:"has_more"`
	Limit      int                                  `json:"limit"`
}

type currencyMigrationPreviewItem struct {
	TemplateID                int     `json:"template_id"`
	Name                      string  `json:"name"`
	CurrentVersion            int     `json:"current_version"`
	NextVersion               int     `json:"next_version"`
	CurrentInputPrice         *string `json:"current_input_price"`
	CurrentOutputPrice        *string `json:"current_output_price"`
	CurrentCachedInputPrice   *string `json:"current_cached_input_price"`
	CurrentCacheCreationPrice *string `json:"current_cache_creation_price"`
	CurrentReasoningPrice     *string `json:"current_reasoning_price"`
	NewInputPrice             string  `json:"new_input_price"`
	NewOutputPrice            string  `json:"new_output_price"`
	NewCachedInputPrice       *string `json:"new_cached_input_price"`
	NewCacheCreationPrice     *string `json:"new_cache_creation_price"`
	NewReasoningPrice         *string `json:"new_reasoning_price"`
	ReferenceCount            int64   `json:"reference_count"`
}

type currencyMigrationPreviewPage struct {
	Items      []currencyMigrationPreviewItem `json:"items"`
	TotalCount int                            `json:"total_count"`
	NextCursor *string                        `json:"next_cursor"`
	HasMore    bool                           `json:"has_more"`
	Limit      int                            `json:"limit"`
}

type currencyMigrationDraftPreviewResponse struct {
	OperationKind           string                          `json:"operation_kind"`
	MigrationOperationID    string                          `json:"migration_operation_id"`
	DraftID                 string                          `json:"draft_id"`
	DraftHash               string                          `json:"draft_hash"`
	PreviewHash             string                          `json:"preview_hash"`
	TargetCurrencyCode      string                          `json:"target_currency_code"`
	TargetCurrencySymbol    string                          `json:"target_currency_symbol"`
	CurrentCurrencyCode     *string                         `json:"current_currency_code"`
	CurrentEpoch            *int                            `json:"current_epoch"`
	NextEpoch               *int                            `json:"next_epoch"`
	TemplateCount           int                             `json:"template_count"`
	RevisionChangeCount     int                             `json:"revision_change_count"`
	TemplatePage            currencyMigrationPreviewPage    `json:"template_page"`
	Committable             bool                            `json:"committable"`
	ValidationErrors        []map[string]any                `json:"validation_errors"`
	EpochChange             bool                            `json:"epoch_change"`
	InventoryID             *string                         `json:"inventory_id,omitempty"`
	InventoryHash           *string                         `json:"inventory_hash,omitempty"`
	ArchivedFXEvidenceCount int                             `json:"archived_fx_evidence_count,omitempty"`
	FXEvidencePage          *pricingMigrationFXEvidencePage `json:"fx_evidence_page,omitempty"`
}

type currencyMigrationPreviewItemsResponse struct {
	PreviewHash string                       `json:"preview_hash"`
	Page        currencyMigrationPreviewPage `json:"page"`
}

func currencyMigrationCommitPayloadHash(header currencyMigrationDraftHeaderRow, previewHash string) string {
	sum := sha256.Sum256([]byte(header.NormalizedHeaderHash + ":" + stringValue(header.DraftHash) + ":" + previewHash))
	return hex.EncodeToString(sum[:])
}
