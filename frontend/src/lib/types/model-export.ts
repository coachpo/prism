// Pi-only wire types for the managed client model-config export surface
// (GET /api/models/exports/pi/source, POST /api/models/exports/pi/render, the
// single-model management read GET /api/models/{id}/pi) and the persisted
// binding surface (POST .../pi/bind, POST .../pi/search,
// POST .../pi/refresh/{preview,commit}, PUT|DELETE .../pi/override,
// DELETE .../pi). These mirror backend
// internal/httpapi/management/models/{export_pi_types,export_pi_binding_types,pi_catalog_types}.go.
// Absence is undefined, never zero. models.dev types stay in
// `lib/types/model-catalog.ts`; the two sources are never merged into one
// weak union.

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
 dropped_fields?: string[];
}

export interface PiSelectedWire {
 provider_id: string;
 model_id: string;
 api: string;
}

/**
 * Bounded pi.dev directory model-id search. The query is a literal model-id
 * fragment only; provider, display name, and every other field are never
 * searched, and no result is ever preselected. `offset` pages the ranked
 * same-API hit set; the ranking is computed before the window is cut.
 */
export interface PiCatalogSearchRequest {
 model_id_query: string;
 limit?: number;
 offset?: number;
}

/** The Prism-owned identity a Pi export row will carry, whatever the catalog says. */
export interface PiExportIdentityWire {
 model_config_id: number;
 model_id: string;
 api: string;
 /** Always "operator_input": the exported provider key is never catalog-derived. */
 provider_id_source: string;
}

export interface PiCatalogSearchResponse {
 query: string;
 api: string;
 limit: number;
 offset: number;
 total: number;
 returned: number;
 truncated: boolean;
 /** Permanent false: a search publishes evidence, it never chooses. */
 selected: boolean;
 catalog: PiCatalogWire;
 fetched_at: string;
 /** When the served revision was last revalidated (304 refreshes this). */
 checked_at: string;
 export_identity: PiExportIdentityWire;
 results: PiCandidateWire[];
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

export interface ExportCompleteness {
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
 /**
  * Prism's own final Pi API for this model, or empty when the family and
  * accepted-format pair has no Pi text API. Directory search and bind are
  * offered only when this is determinable, and every offered coordinate must
  * carry exactly this value.
  */
 pi_api?: string;
 prism_metadata: Record<string, unknown>;
 merged_metadata: Record<string, unknown>;
 metadata_provenance: Record<string, string>;
 missing_metadata: string[];
 completeness: ExportCompleteness;
 targets: ExportSourceTargetRow[];
 price_risk: ExportPriceRisk;
 warnings?: string[];
 /** Live catalog evidence: discovery only, never render authority. */
 pi_candidates: PiCandidateWire[];
 candidate_status: PiCandidateStatus;
 /** Persisted binding evidence: render authority when pi_selected is set. */
 pi_selected?: PiSelectedWire | null;
 pi_binding_status: PiBindingStatus;
 /** Frozen Prism identity snapshot and final Pi API still match current truth. */
 pi_binding_renderable: boolean;
 pi_bind_source?: "single_candidate" | "manual";
 /**
  * Prism full model id frozen at bind time. A later Prism rename is checked
  * against this value; whether the binding is cross-directory is determined by
  * comparing `pi_selected.model_id` with this snapshot.
  */
 pi_binding_prism_model_id?: string;
 pi_binding_catalog_revision?: string;
 pi_binding_fetched_at?: string;
 pi_binding_updated_at?: string;
 pi_binding_dropped_fields?: string[];
 /** Frozen source, explicit overrides, and their effective projection. */
 pi_binding_source?: PiBindingMetadataWire | null;
 pi_binding_override?: PiBindingMetadataWire | null;
 pi_binding_effective?: PiBindingMetadataWire | null;
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
 /** Prism identity snapshot frozen with this coordinate. */
 prism_model_id_at_bind?: string;
 catalog_revision?: string;
 fetched_at?: string;
 updated_at?: string;
 source: PiBindingMetadataWire | null;
 override: PiBindingMetadataWire | null;
 effective: PiBindingMetadataWire | null;
 dropped_fields?: string[];
}

export interface PiBindingFieldChange {
 field: string;
 current: string | null;
 next: string | null;
 kind: "added" | "removed" | "changed";
}

export interface PiRefreshPreviewResponse {
 bound: boolean;
 provider_id: string;
 catalog_model_id: string;
 api: string;
 changed: boolean;
 changes: PiBindingFieldChange[];
 catalog_revision: string;
 binding_updated_at: string;
 fetched_at: string;
}

export interface PiBindRequest {
 provider_id?: string;
 catalog_model_id?: string;
 expected_catalog_revision: string;
 /** Prism full model id the operator confirmed this bind against. */
 expected_prism_model_id: string;
 /** Final Pi API the operator confirmed this bind against. */
 expected_pi_api: string;
}

export interface PiRefreshCommitRequest {
 expected_provider_id: string;
 expected_catalog_model_id: string;
 expected_api: string;
 expected_binding_updated_at: string;
 expected_catalog_revision: string;
}

export type PiOverrideFieldValue =
 | string
 | boolean
 | number
 | string[]
 | Record<string, string | null>
 | Record<string, unknown>
 | null;

export interface PiRenderRequest {
 expected_source_digest: string;
 model_config_ids: number[];
 base_url: string;
 provider_id: string;
 credential: {
  include: boolean;
  api_key?: string;
 };
 selections: Record<
  number,
  { provider_id: string; model_id: string; api: string }
 >;
}

// --- Single-model Pi management read (GET /api/models/{id}/pi) ---

export interface PiModelReadIdentityWire {
 model_config_id: number;
 model_id: string;
 api_family: string;
 pi_api?: string;
}

export interface PiCatalogReadWire {
 status: "fresh" | "stale" | "unavailable";
 revision?: string;
 minimum_version?: string;
 etag?: string;
 fetched_at?: string;
 checked_at?: string;
}

/**
 * Single-model Pi management read. Carries the model identity, one catalog
 * evidence block, live exact-candidate evidence, and the persisted binding —
 * and deliberately no targets, pricing, source digest, credential, or render
 * result.
 */
export interface PiModelReadResponse {
 model: PiModelReadIdentityWire;
 catalog: PiCatalogReadWire;
 candidate_status: PiCandidateStatus;
 candidates: PiCandidateWire[];
 binding_status: PiBindingStatus;
 binding_renderable: boolean;
 binding: PiBindingResponse;
}
