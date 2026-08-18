export interface CostingSettingsResponse {
  profile_id: number;
  report_currency_code: string | null;
  report_currency_symbol: string | null;
  reporting_currency_epoch: string | null;
  currency_effective_at?: string | null;
  pricing_migration_state: string;
  legacy_migration_issues: string[];
  timezone_preference?: string | null;
  pricing_template_generation: string;
  pricing_reference_generation: string;
  active_template_count: number;
  pricing_migration_inventory: PricingMigrationInventorySummary | null;
  updated_at: string;
}

export interface PricingMigrationInventorySummary {
  inventory_id: string;
  inventory_hash: string;
  generation: number;
  issue_codes: string[];
  template_issue_count: number;
  legacy_fx_row_count: number;
  live_fx_dependency_count: number;
  recommended_operation_kind: "currency_cutover" | "repair_same_currency" | "archive_unused_fx";
  archive_only_available: boolean;
  template_scaffold_url: string;
  fx_evidence_url: string;
  reporting_currency_evidence?: {
    evidence_id: string;
    raw_currency_code: string;
    raw_currency_symbol: string;
    settings_updated_at: string;
    validation_codes: string[];
  } | null;
}

export interface PricingMigrationInventoryTemplate {
  template_id: number;
  name: string;
  updated_at: string;
  base_version: number;
  current_revision_id: string | null;
  current_input_price: string | null;
  current_output_price: string | null;
  current_cached_input_price: string | null;
  current_cache_creation_price: string | null;
  current_reasoning_price: string | null;
  legacy_template_evidence_id: string | null;
  raw_pricing_unit: string | null;
  raw_currency_code: string | null;
  raw_input_price: string | null;
  raw_output_price: string | null;
  raw_cached_input_price: string | null;
  raw_cache_creation_price: string | null;
  raw_reasoning_price: string | null;
  issue_codes: string[];
  model_reference_count: number;
  endpoint_reference_count: number;
  terminal_target_reference_count: number;
}

export interface PricingMigrationInventoryTemplatePage {
  inventory_id: string;
  inventory_hash: string;
  generation: number;
  total_active_template_count: number;
  items: PricingMigrationInventoryTemplate[];
  total_count: number;
  next_cursor: string | null;
}

export interface TimezonePreferenceResponse {
  timezone_preference?: string | null;
}

export interface CostingSettingsUpdate {
  report_currency_code: string;
  report_currency_symbol: string;
  timezone_preference?: string | null;
  expected_updated_at?: string | null;
  reporting_currency_epoch?: number | string;
  pricing_migration_state?: string;
  pricing_template_generation?: number | string;
  pricing_reference_generation?: number | string;
  pricing_migration_inventory?: PricingMigrationInventorySummary | null;
  active_template_count?: number;
}

// Normal costing PUTs never author the active currency code. Code changes use
// the dedicated preview/commit migration owner; this mutation keeps symbol,
// timezone and the shared Pricing CAS as the only ordinary write fields.
export interface CostingSettingsMutation {
  report_currency_symbol?: string;
  timezone_preference?: string | null;
  expected_updated_at?: string | null;
}

export interface TimezonePreferenceUpdate {
  timezone_preference?: string | null;
}

export type CurrencyMigrationOperationKind = "currency_cutover" | "repair_same_currency" | "archive_unused_fx";
export type CurrencyMigrationDraftOperationKind = Exclude<CurrencyMigrationOperationKind, "archive_unused_fx">;

export interface CurrencyMigrationDraftChunkSummary {
  ordinal: number;
  row_count: number;
  content_hash: string;
}

export interface CurrencyMigrationDraftChunkPage {
  items: CurrencyMigrationDraftChunkSummary[];
  total_count: number;
  consumed_count: number;
  next_cursor: string | null;
}

export interface CurrencyMigrationDraftHeader {
  draft_id: string;
  migration_operation_id: string;
  operation_kind: CurrencyMigrationDraftOperationKind;
  target_currency_code: string;
  target_currency_symbol: string;
  expected_inventory_id: string | null;
  expected_inventory_hash: string | null;
  expected_inventory_generation: number | null;
  expected_reporting_currency_epoch: number | null;
  expected_settings_updated_at: string;
  status: "uploading" | "sealed" | "committed" | "expired";
  normalized_header_hash: string;
  received_chunk_count: number;
  chunk_page: CurrencyMigrationDraftChunkPage;
  draft_hash: string | null;
  template_count: number | null;
  committed_result_operation_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface CurrencyMigrationDraftChunkItem {
  template_id: number;
  expected_version: number;
  expected_updated_at: string;
  input_price: string;
  output_price: string;
  cached_input_price: string | null;
  cache_creation_price: string | null;
  reasoning_price: string | null;
}

export interface CurrencyMigrationDraftItem {
  template_id: number;
  template_name: string;
  expected_version: number;
  expected_updated_at: string;
  input_price: string;
  output_price: string;
  cached_input_price: string | null;
  cache_creation_price: string | null;
  reasoning_price: string | null;
  reference_count: number;
}

export interface CurrencyMigrationDraftItemPage {
  items: CurrencyMigrationDraftItem[];
  total_count: number;
  next_cursor: string | null;
  has_more: boolean;
  limit: number;
}

export interface CurrencyMigrationPreviewItem {
  template_id: number;
  name: string;
  current_version: number;
  next_version: number;
  current_input_price: string | null;
  current_output_price: string | null;
  current_cached_input_price: string | null;
  current_cache_creation_price: string | null;
  current_reasoning_price: string | null;
  new_input_price: string;
  new_output_price: string;
  new_cached_input_price: string | null;
  new_cache_creation_price: string | null;
  new_reasoning_price: string | null;
  reference_count: number;
}

export interface CurrencyMigrationPreviewPage {
  items: CurrencyMigrationPreviewItem[];
  total_count: number;
  next_cursor: string | null;
  has_more: boolean;
  limit: number;
}

export interface CurrencyMigrationFXEvidence {
  legacy_fx_evidence_id: string;
  source_fx_row_id: string;
  model_id: string;
  endpoint_id: string;
  fx_rate: string;
  source_created_at: string;
  source_updated_at: string;
  row_hash: string;
  attribution: "has_live" | "unused" | "unknown";
  scan_proof_code: string;
  scan_proof_hash: string;
  dependency_count: number;
}

export interface CurrencyMigrationFXEvidencePage {
  inventory_id: string;
  inventory_hash: string;
  generation: number;
  items: CurrencyMigrationFXEvidence[];
  total_count: number;
  next_cursor: string | null;
}

export interface CurrencyMigrationPreview {
  operation_kind: CurrencyMigrationOperationKind;
  migration_operation_id: string;
  draft_id: string;
  draft_hash: string;
  preview_hash: string;
  target_currency_code: string;
  target_currency_symbol: string;
  current_currency_code: string | null;
  current_epoch: number | null;
  next_epoch: number | null;
  template_count: number;
  revision_change_count: number;
  template_page: CurrencyMigrationPreviewPage;
  committable: boolean;
  validation_errors: Array<Record<string, unknown>>;
  epoch_change: boolean;
  inventory_id?: string;
  inventory_hash?: string;
  archived_fx_evidence_count?: number;
  fx_evidence_page?: CurrencyMigrationFXEvidencePage;
}

export interface CurrencyMigrationPreviewItemsResponse {
  preview_hash: string;
  page: CurrencyMigrationPreviewPage;
}

export interface CurrencyMigrationCommitRequest {
  operation_kind: CurrencyMigrationOperationKind;
  migration_operation_id: string;
  draft_id: string;
  draft_hash: string;
  preview_hash: string;
  expected_inventory_id?: string;
  expected_inventory_hash?: string;
  expected_inventory_generation?: number;
  expected_reporting_currency_epoch?: number;
  expected_settings_updated_at?: string;
}

export interface CurrencyMigrationCommitResponse {
  old_currency_code: string | null;
  new_currency_code: string;
  old_epoch: number | null;
  new_epoch: number | null;
  revision_change_count: number;
  template_count: number;
  migration_operation_id: string;
  epoch_change: boolean;
  archived_fx_evidence_count?: number;
  inventory_id?: string;
}
