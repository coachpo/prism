/**
 * Pi-only wire types for the managed client model-config export surface
 * (GET /api/models/export/source, POST /render) and the persisted binding
 * surface (POST .../pi/bind, POST .../pi/refresh/{preview,commit},
 * PUT|DELETE .../pi/override, DELETE .../pi).
 * Absence is undefined, never zero.
 */

export interface PiCatalogWire {
  revision?: string;
  status: "fresh" | "stale" | "unavailable";
  minimum_version?: string;
  etag?: string;
}

export interface PiCandidateWire {
  provider_id: string;
  model_id: string;
  api: string;
  name?: string;
  reasoning?: boolean;
  input?: string[];
  context_window?: number;
  max_tokens?: number;
  thinking_level_map?: Record<string, string | null>;
  compat?: Record<string, unknown>;
}

export interface PiSelectedWire {
  provider_id: string;
  model_id: string;
  api: string;
}

/** Live pi.dev discovery evidence for one model; never render authority. */
export type PiCandidateStatus =
  | "not_in_catalog"
  | "api_mismatch"
  | "single"
  | "multiple"
  | "catalog_unavailable";

/** Persisted model_pi_catalog_bindings health; render authority when bound. */
export type PiBindingStatus = "unbound" | "bound" | "bound_drifted";

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

export interface ExportSourceModelRow {
  model_config_id: number;
  model_id: string;
  api_family: string;
  display_name: string | null;
  is_enabled: boolean;
  selectable: boolean;
  unselectable_reason?: string;
  openai_accepted_format?: string;
  openai_image_operations?: string;
  prism_metadata: Record<string, unknown>;
  merged_metadata: Record<string, unknown>;
  metadata_provenance: Record<string, string>;
  missing_metadata: string[];
  platform_completeness: ExportPlatformCompleteness;
  targets: ExportSourceTargetRow[];
  price_risk: ExportPriceRisk;
  warnings?: string[];
  /** Live catalog evidence: discovery only, never render authority. */
  pi_candidates: PiCandidateWire[];
  candidate_status: PiCandidateStatus;
  /** Persisted binding evidence: render authority when pi_selected is set. */
  pi_selected?: PiSelectedWire | null;
  pi_binding_status: PiBindingStatus;
  pi_bind_source?: "single_candidate" | "manual";
  pi_binding_catalog_revision?: string;
  pi_binding_fetched_at?: string;
  pi_binding_updated_at?: string;
}

export interface ExportSourceResponse {
  target_version: string;
  catalog: PiCatalogWire;
  models: ExportSourceModelRow[];
  source_digest: string;
  warnings?: string[];
}

export interface ExportRenderResponse {
  target_version: string;
  catalog: PiCatalogWire;
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
  source_digest: string;
}

/** The seven safe pi.dev leaves a binding freezes, in one metadata projection. */
export interface PiBindingMetadataWire {
  name: string | null;
  reasoning: boolean | null;
  input: string[] | null;
  context_window: number | null;
  max_tokens: number | null;
  thinking_level_map: Record<string, string | null> | null;
  compat: Record<string, unknown> | null;
}

export interface PiBindingResponse {
  bound: boolean;
  bind_source?: "single_candidate" | "manual";
  provider_id?: string;
  catalog_model_id?: string;
  api?: string;
  catalog_revision?: string;
  fetched_at?: string;
  updated_at?: string;
  source: PiBindingMetadataWire | null;
  override: PiBindingMetadataWire | null;
  effective: PiBindingMetadataWire | null;
}

export interface PiBindingFieldChange {
  field: string;
  current: string | null;
  next: string | null;
  kind: "added" | "removed" | "changed";
}

export interface PiRefreshPreviewResponse {
  bound: boolean;
  provider_id?: string;
  catalog_model_id?: string;
  api?: string;
  changed: boolean;
  changes: PiBindingFieldChange[];
  catalog_revision: string;
  fetched_at: string;
}
