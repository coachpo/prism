package settings

import (
	"encoding/json"
	"time"
)

type costingSettingsResponse struct {
	ProfileID                  int                               `json:"profile_id"`
	ReportCurrencyCode         *string                           `json:"report_currency_code"`
	ReportCurrencySymbol       *string                           `json:"report_currency_symbol"`
	ReportingCurrencyEpoch     *string                           `json:"reporting_currency_epoch"`
	CurrencyEffectiveAt        *string                           `json:"currency_effective_at"`
	PricingMigrationState      string                            `json:"pricing_migration_state"`
	LegacyMigrationIssues      []string                          `json:"legacy_migration_issues"`
	PricingMigrationInventory  *pricingMigrationInventorySummary `json:"pricing_migration_inventory"`
	TimezonePreference         *string                           `json:"timezone_preference"`
	PricingTemplateGeneration  string                            `json:"pricing_template_generation"`
	PricingReferenceGeneration string                            `json:"pricing_reference_generation"`
	ActiveTemplateCount        int                               `json:"active_template_count"`
	UpdatedAt                  string                            `json:"updated_at"`
}

type costingSettingsUpdateRequest struct {
	ReportCurrencySymbol optionalString `json:"report_currency_symbol"`
	TimezonePreference   optionalString `json:"timezone_preference"`
	ExpectedUpdatedAt    optionalString `json:"expected_updated_at"`
	// Legacy fields are rejected, never silently accepted (SPEC 5.3).
	ReportCurrencyCode optionalString   `json:"report_currency_code"`
	EndpointFXMappings *json.RawMessage `json:"endpoint_fx_mappings"`
}

type optionalString struct {
	Set   bool
	Value *string
}

// UnmarshalJSON records presence so missing and explicit-null can be told
// apart (the symbol/timezone update contract needs both).
func (field *optionalString) UnmarshalJSON(raw []byte) error {
	field.Set = true
	if string(raw) == "null" {
		field.Value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	field.Value = &value
	return nil
}

type userSettingsRow struct {
	ID                              int
	ProfileID                       int
	ReportCurrencyCode              string
	ReportCurrencySymbol            string
	TimezonePreference              *string
	CurrentReportingCurrencyEpochID *int64
	PricingMigrationState           string
	LegacyMigrationIssues           []string
	PricingTemplateGeneration       int64
	PricingReferenceGeneration      int64
	CreatedAt                       time.Time
	UpdatedAt                       time.Time
}

type currencyMigrationCommitResponse struct {
	OldCurrencyCode         *string `json:"old_currency_code"`
	NewCurrencyCode         string  `json:"new_currency_code"`
	OldEpoch                *int    `json:"old_epoch"`
	NewEpoch                *int    `json:"new_epoch"`
	RevisionChangeCount     int     `json:"revision_change_count"`
	TemplateCount           int     `json:"template_count"`
	MigrationOperationID    string  `json:"migration_operation_id"`
	EpochChange             bool    `json:"epoch_change"`
	ArchivedFXEvidenceCount int     `json:"archived_fx_evidence_count,omitempty"`
	InventoryID             *string `json:"inventory_id,omitempty"`
}
