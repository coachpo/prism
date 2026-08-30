// models.dev catalog metadata types for the management surface. These mirror
// backend/internal/httpapi/management/models/catalog_types.go: source,
// override, and effective projections plus the binding coordinates. All of it
// is management-only presentation data — none of it participates in runtime
// routing or enters the runtime snapshot.

export interface ModelCatalogMetadata {
 name: string | null;
 description: string | null;
 family: string | null;
 release_date: string | null;
 last_updated: string | null;
 knowledge: string | null;
 attachment: boolean | null;
 reasoning: boolean | null;
 tool_call: boolean | null;
 structured_output: boolean | null;
 temperature: boolean | null;
 modalities_input: string[] | null;
 modalities_output: string[] | null;
 limit_context: number | null;
 limit_input: number | null;
 limit_output: number | null;
 open_weights: boolean | null;
 status: "alpha" | "beta" | "deprecated" | null;
}

/**
 * Missing keys are untouched, null restores the source value, and every other
 * value (including an empty string or empty list) is an explicit override.
 */
export type CatalogOverridePatch = {
 [K in keyof ModelCatalogMetadata]?: ModelCatalogMetadata[K];
};

export interface CatalogCandidate {
 provider_id: string;
 provider_name: string;
 model_id: string;
 name: string;
}

export interface ModelCatalogAutoMatch {
 available: boolean;
 unique: boolean;
 candidates: CatalogCandidate[];
 reason?: "unique_match" | "ambiguous" | "no_match" | string;
}

export interface ModelCatalogResponse {
 bound: boolean;
 match_source?: "unique_match" | "manual" | string;
 provider_id?: string;
 catalog_model_id?: string;
 catalog_revision?: string;
 fetched_at?: string;
 updated_at?: string;
 source: ModelCatalogMetadata | null;
 override: ModelCatalogMetadata | null;
 effective: ModelCatalogMetadata | null;
 auto_match?: ModelCatalogAutoMatch | null;
}

export interface ModelCatalogMatchPreviewResponse {
 committable: boolean;
 provider_id?: string;
 catalog_model_id?: string;
 candidates: CatalogCandidate[];
 reason: string;
 catalog_revision: string;
 fetched_at: string;
}

export interface CatalogFieldChange {
 field: string;
 current: string | null;
 next: string | null;
 kind: "added" | "removed" | "changed";
}

export interface ModelCatalogRefreshPreviewResponse {
 bound: boolean;
 provider_id?: string;
 catalog_model_id?: string;
 changed: boolean;
 changes: CatalogFieldChange[];
 catalog_revision: string;
 fetched_at: string;
}

export interface ModelCatalogCandidatesResponse {
 items: CatalogCandidate[];
 total: number;
 limit: number;
 offset: number;
 scope: string;
 query?: string;
}

export interface CatalogBindingRequest {
 provider_id?: string;
 catalog_model_id?: string;
 expected_catalog_revision: string;
}

// Source-linked pricing import payloads mirroring
// backend/internal/httpapi/management/connections/pricing_template_catalog.go.

export type CatalogIncompatibility = {
 field: string;
 reason: string;
};

export interface CatalogPriceCard {
 input_price: string;
 output_price: string;
 cached_input_price: string | null;
 cache_creation_price: string | null;
 reasoning_price: string | null;
}

export interface CatalogPricePlan {
 template_kind: "standard" | "tiered";
 cards: Record<string, CatalogPriceCard>;
 tier_input_tokens_above?: number;
 incompatibilities: CatalogIncompatibility[];
}

export interface CatalogOfferingInfo {
 provider_id: string;
 catalog_model_id: string;
 name?: string;
 description?: string | null;
 family?: string | null;
 status?: string | null;
 open_weights?: boolean | null;
}

export interface CatalogLinkedTemplateInfo {
 id: number;
 name: string;
 version: number;
 revision_id: number;
 template_kind: string;
 updated_at: string;
}

export interface CatalogTargetState {
 connection_id: number;
 name: string | null;
 endpoint_name: string | null;
 pricing_template_id: number | null;
 updated_at: string;
}

export interface CatalogPricingPreviewRequest {
 model_config_id?: number;
 provider_id?: string;
 catalog_model_id?: string;
 connection_ids?: number[];
}

export interface CatalogPricingPreviewResponse {
 schema_version: number;
 offering: CatalogOfferingInfo;
 /**
  * The Prism model this import was authored from. Display evidence only: it is
  * absent when the caller supplied bare coordinates without a model, and it
  * never changes what the commit writes.
  */
 model?: CatalogPrismModelInfo;
 catalog_revision: string;
 fetched_at: string;
 plan: CatalogPricePlan;
 template?: CatalogLinkedTemplateInfo;
 action: "create" | "reuse" | "drift";
 drift: boolean;
 committable: boolean;
 preview_hash?: string;
 targets: CatalogTargetState[];
 reporting_currency_code: string;
 /** Fixed catalog price currency and Prism storage unit (never converted). */
 catalog_currency: string;
 pricing_unit: string;
}

export interface CatalogPrismModelInfo {
 model_config_id: number;
 model_id: string;
 display_name: string;
 api_family: string;
}

export interface CatalogPricingCommitRequest {
 schema_version: number;
 model_config_id?: number;
 provider_id?: string;
 catalog_model_id?: string;
 connection_ids?: number[];
 preview_hash: string;
 expected_catalog_revision: string;
 confirm_drift: boolean;
}

export interface CatalogPricingCommitResponse {
 created: boolean;
 updated: boolean;
 assigned_connection_ids: number[];
 template_id: number;
 template_name: string;
 revision_id: number;
 version: number;
 drift_confirmed: boolean;
}
