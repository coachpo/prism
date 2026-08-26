/**
 * Wire types for the managed client model-config export surface
 * (/api/models/exports/{platform}/source|render). These mirror the backend
 * contracts exactly; absence is expressed as undefined, never as zero.
 */

export type ExportPlatform = "pi" | "opencode";

export interface ExportCatalogEvidence {
  bound: boolean;
  provider_id?: string;
  catalog_model_id?: string;
  catalog_revision?: string;
  match_source?: string;
  has_overrides: boolean;
}

export interface ExportEnrichmentEvidence {
  available: boolean;
  offering_provider_id?: string;
  offering_model_id?: string;
}

export interface ExportPlatformCompleteness {
  metadata_fields: Record<string, boolean>;
  cost_exportable: boolean;
}

export interface ExportPriceCardWire {
  input_price: string;
  output_price: string;
  cached_input_price: string | null;
  cache_creation_price: string | null;
  reasoning_price: string | null;
}

export interface ExportTargetPricing {
  terminal_target_id: number;
  template_kind: string;
  currency_code: string;
  pricing_unit: string;
  tier_threshold?: number;
  card?: ExportPriceCardWire | null;
  base_card?: ExportPriceCardWire | null;
  above_card?: ExportPriceCardWire | null;
}

export interface ExportSourceTargetRow {
  terminal_target_id: number;
  position: number;
  endpoint_id: number;
  endpoint_name: string;
  openai_text_capability?: string;
  pricing?: ExportTargetPricing | null;
}

export interface ExportPriceRisk {
  exportable: boolean;
  warning_codes?: string[];
}

export interface EnrichmentCandidateWire {
  metadata: Record<string, unknown>;
  derived?: Record<string, unknown>;
  warnings?: string[];
}

export interface ExportSourceModelRow {
  model_config_id: number;
  model_id: string;
  api_family: string;
  display_name: string | null;
  is_enabled: boolean;
  default_selected: boolean;
  selectable: boolean;
  unselectable_reason?: string;
  openai_accepted_format?: string;
  openai_image_operations?: string;
  catalog: ExportCatalogEvidence;
  enrichment: ExportEnrichmentEvidence;
  prism_metadata: Record<string, unknown>;
  models_dev_metadata: Record<string, unknown>;
  merged_metadata: Record<string, unknown>;
  metadata_provenance: Record<string, string>;
  missing_metadata: string[];
  platform_completeness: ExportPlatformCompleteness;
  targets: ExportSourceTargetRow[];
  price_risk: ExportPriceRisk;
  warnings?: string[];
  enrichment_candidate: EnrichmentCandidateWire | null;
}

export interface ExportSourceResponse {
  platform: ExportPlatform;
  target_version: string;
  catalog_revision?: string;
  models: ExportSourceModelRow[];
  source_digest: string;
  warnings?: string[];
}

export interface ManualEnhancementWire {
  fields?: Record<string, unknown>;
  override_fields?: string[];
}

export interface ExportRenderResponse {
  platform: ExportPlatform;
  target_version: string;
  catalog_revision?: string;
  content: string;
  content_sha256: string;
  file_name: string;
  mime_type: string;
  model_results: Array<{
    model_config_id: number;
    model_id: string;
    cost_exported: boolean;
    warning_codes?: string[];
    missing_metadata?: string[];
  }>;
  warnings?: string[];
}
