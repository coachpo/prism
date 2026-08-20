import type { ApiFamily } from "./vendor";

export interface Endpoint {
 id: number;
 profile_id?: number;
 name: string;
 base_url: string;
 has_api_key: boolean;
 api_key_fingerprint: string | null;
 api_key_updated_at: string | null;
 config_revision: number;
 created_at: string;
 updated_at: string;
}

export interface EndpointCreate {
 name: string;
 base_url: string;
 api_key: string;
}

export interface EndpointUpdate {
 name?: string;
 base_url?: string;
 api_key?: string | null;
 /** Optimistic-concurrency guard; the updated_at of the endpoint row the
  *  form was opened with. A mismatch returns 409 endpoint_stale. */
 expected_updated_at?: string;
}

export interface EndpointReferenceSummary {
 direct_reference_count: number;
 referencing_model_count: number;
 enabled_reference_count: number;
 orphan_reference_count: number;
}

export interface EndpointReferencePricingTemplate {
 id: number;
 name: string;
 current_revision_id: string | null;
 current_version: number;
 template_kind: PricingTemplateKind;
}

export interface EndpointReferenceOwnerModel {
 id: number;
 model_id: string;
 display_name: string | null;
 is_enabled: boolean;
 openai_accepted_format: OpenAIAcceptedFormat | null;
 openai_image_operations: OpenAIImageOperations | null;
}

export interface EndpointReferenceAccessTarget {
 id: number;
 position: number;
 is_enabled: boolean;
}

export interface EndpointReferenceItem {
 kind: "owned_terminal_target" | "orphan_connection";
 connection_id: number;
 terminal_target_id: number;
 terminal_target_name: string | null;
 api_family: ApiFamily;
 connection_is_active: boolean;
 access_target: EndpointReferenceAccessTarget | null;
 owner_model: EndpointReferenceOwnerModel | null;
 openai_text_capability: OpenAITextCapability | null;
 openai_image_capability: OpenAIImageCapability | null;
 /** Configuration only: whether a routing schedule exists, never whether it is open now. */
 has_routing_schedule: boolean;
 pricing_template: EndpointReferencePricingTemplate | null;
 enabled: boolean;
 inactive_reasons: Array<
  | "model_disabled"
  | "access_target_disabled"
  | "connection_inactive"
  | "orphaned"
  | "configuration_integrity_error"
 >;
}

export interface EndpointReferencePage {
 items: EndpointReferenceItem[];
 total_count: number;
 next_cursor: string | null;
 reference_snapshot_hash: string;
}

export interface EndpointReferenceDetail {
 endpoint_id: number;
 summary: EndpointReferenceSummary;
 reference_page: EndpointReferencePage;
}

export interface EndpointReferenceBatchItem {
 endpoint_id: number;
 summary: EndpointReferenceSummary;
}

export interface EndpointReferenceBatchResponse {
 items: EndpointReferenceBatchItem[];
}

export type EndpointVerifyOutcome =
 | "verified"
 | "authentication_failed"
 | "probe_unsupported"
 | "api_mismatch"
 | "upstream_rejected"
 | "upstream_unavailable"
 | "unreachable"
 | "timeout";

export interface EndpointVerifyResult {
 endpoint_id: number;
 api_family: ApiFamily;
 config_revision: number;
 api_key_fingerprint: string | null;
 is_current: boolean;
 outcome: EndpointVerifyOutcome;
 probe_path: string;
 upstream_status: number | null;
 duration_ms: number;
 error_summary: string | null;
}

export interface EndpointVerifyRequest {
 api_family: ApiFamily;
 expected_config_revision: number;
}

export interface OrphanCleanupResponse {
 deleted: boolean;
 connection_id: number;
}

export interface PricingTemplateListItem {
 id: number;
 profile_id: number;
 name: string;
 description: string | null;
 pricing_unit: "PER_1M";
 pricing_currency_code: string;
 version: number;
 updated_at: string;
}

export type PricingTemplateKind = "standard" | "tiered" | "peak_valley";

export interface PricingCard {
 input_price: string;
 output_price: string;
 cached_input_price: PricingComponentPrice;
 cache_creation_price: PricingComponentPrice;
 reasoning_price: PricingComponentPrice;
}

export interface PricingTemplateWindow {
 weekday_mask: number;
 start_minute: number;
 end_minute: number;
}

export interface PricingTemplateSchedule {
 timezone: string;
 windows: PricingTemplateWindow[];
}

export interface PricingTemplateTier {
 input_tokens_above: number;
 card: PricingCard;
}

export interface PricingTemplateListPageRevision {
 revision_id: string;
 version: number;
 pricing_unit: "PER_1M";
 currency_code: string;
 reporting_currency_epoch: number | null;
 currency_attribution: "active_epoch" | "legacy_foreign" | "pre_epoch_pending";
 template_kind: PricingTemplateKind;
 card?: PricingCard;
 base_card?: PricingCard;
 tier?: PricingTemplateTier | null;
 peak_card?: PricingCard;
 offpeak_card?: PricingCard;
 schedule_timezone?: string | null;
 schedule_digest?: string | null;
 effective_at: string | null;
 created_at: string;
 created_by_kind: string;
 created_by_operation_id: string | null;
}

export interface PricingTemplateListPageItem {
 id: string;
 profile_id: string;
 name: string;
 description: string | null;
 current_revision: PricingTemplateListPageRevision;
 configuration_status: "complete" | "incomplete";
 missing_specialty_components: string[];
 model_reference_count: number;
 endpoint_reference_count: number;
 terminal_target_reference_count: number;
 created_at: string;
 updated_at: string;
 deleted_at: string | null;
}

export interface PricingTemplateListPage {
 items: PricingTemplateListPageItem[];
 total_count: number;
 consumed_count: number;
 list_snapshot_hash: string;
 next_cursor: string | null;
}

interface PricingTemplateBase {
 id: number;
 profile_id: number;
 name: string;
 description: string | null;
 pricing_unit: "PER_1M";
 pricing_currency_code: string;
 active_currency_symbol: string;
 version: number;
 revision_id: number;
 version_effective_at: string | null;
 reporting_currency_epoch: number | null;
 revision_count: number;
 created_at: string;
 updated_at: string;
}

export type PricingTemplateVariant =
 | {
    template_kind: "standard";
    card: PricingCard;
    base_card?: never;
    tier?: never;
    peak_card?: never;
    offpeak_card?: never;
    schedule?: never;
   }
 | {
    template_kind: "tiered";
    card?: never;
    base_card: PricingCard;
    tier: PricingTemplateTier;
    peak_card?: never;
    offpeak_card?: never;
    schedule?: never;
   }
 | {
    template_kind: "peak_valley";
    card?: never;
    base_card?: never;
    tier?: never;
    peak_card: PricingCard;
    offpeak_card: PricingCard;
    schedule: PricingTemplateSchedule;
   };

export type PricingTemplate = PricingTemplateBase & PricingTemplateVariant;

// PricingComponentPrice is `string` for base prices and `string | null` for
// the three specialty prices (explicit null = unconfigured, never "0").
export type PricingComponentPrice = string | null;

interface PricingTemplateWriteBase {
 name: string;
 description?: string | null;
}

export type PricingTemplateCreate = PricingTemplateWriteBase & PricingTemplateVariant;

export type PricingTemplateImportMode = "upsert_by_name" | "create_only";

export interface PricingTemplateImportRequest {
 schema_version: 2;
 mode: PricingTemplateImportMode;
 templates: PricingTemplateCreate[];
}

export interface PricingTemplateImportError {
 index: number;
 name?: string;
 detail: string;
}

export interface PricingTemplateImportResponse {
 created: number;
 updated: number;
 skipped: string[];
 errors: PricingTemplateImportError[];
 preview_hash?: string;
 committable?: boolean;
  items?: Array<{
  name: string;
  action: string;
  template_kind?: PricingTemplateKind;
  current_version?: number;
  next_version?: number;
  template_kind_changed: boolean;
  pricing_structure_changed: boolean;
 }>;
}

export interface PricingTemplateImportCommitRequest {
 schema_version: 2;
 mode: PricingTemplateImportMode;
 templates: PricingTemplateCreate[];
 preview_hash: string;
}

interface PricingTemplateUpdateBase {
 expected_updated_at: string;
 name?: string;
 description?: string | null;
}

export type PricingTemplateUpdate = PricingTemplateUpdateBase &
 (PricingTemplateVariant | {
   template_kind?: never;
   card?: never;
   base_card?: never;
   tier?: never;
   peak_card?: never;
   offpeak_card?: never;
   schedule?: never;
  });

export interface PricingTemplateRevision {
 revision_id: number;
 version: number;
 pricing_unit: "PER_1M";
 currency_code: string;
 reporting_currency_epoch: number | null;
 currency_attribution: "active_epoch" | "legacy_foreign" | "pre_epoch_pending";
 template_kind: PricingTemplateKind;
 card?: PricingCard;
 base_card?: PricingCard;
 peak_card?: PricingCard;
 offpeak_card?: PricingCard;
 schedule?: PricingTemplateSchedule;
 tier: PricingTemplateTier | null;
 effective_at: string | null;
 created_at: string;
 created_by_kind: string;
}

export interface PricingTemplateImpact {
 template_id: number;
 name: string;
 current_version: number;
 next_version: number;
 reference_count: number;
 references: PricingTemplateConnectionUsageItem[];
 revision_count: number;
 deleted_at?: string | null;
}

export interface PricingTemplateConnectionUsageItem {
 connection_id: number;
 connection_name: string | null;
 model_config_id: number;
 model_id: string;
 endpoint_id: number;
 endpoint_name: string;
}

export interface PricingTemplateConnectionsResponse {
 template_id: number;
 items: PricingTemplateConnectionUsageItem[];
}

export interface ConnectionPricingTemplateUpdate {
 pricing_template_id: number | null;
}

/** One authored routing window. `end_minute` above 1440 continues into the next day. */
export interface RoutingScheduleWindow {
 weekday_mask: number;
 start_minute: number;
 end_minute: number;
}

/** Stored routing-schedule configuration. Carries no evaluated conclusion. */
export interface RoutingSchedule {
 timezone: string;
 windows: RoutingScheduleWindow[];
}

export type RoutingScheduleStatus =
 | "open"
 | "closed"
 | "unresolved"
 | "not_evaluated";
export type RoutingScheduleNotEvaluatedReason = "connection_inactive";

/**
 * Server-computed state of a routing schedule at an instant.
 *
 * The `*_at` fields are optional rather than nullable because the server omits
 * the key entirely when the matching `_known` flag is false. Typing them as
 * `| null` would make consumers test `=== null` and never match.
 */
export interface RoutingScheduleState {
 status: RoutingScheduleStatus;
 not_evaluated_reason?: RoutingScheduleNotEvaluatedReason;
 timezone: string;
 evaluated_at: string;
 next_open_at?: string;
 next_open_at_known: boolean;
 next_close_at?: string;
 next_close_at_known: boolean;
}

export interface ConnectionPricingTemplateSummary {
 id: number;
 name: string;
 pricing_unit: "PER_1M";
 pricing_currency_code: string;
 template_kind: PricingTemplateKind;
 version: number;
}

export type OpenAIAcceptedFormat =
 | "responses_only"
 | "chat_completions_only"
 | "dual_native";

export type OpenAITextCapability = OpenAIAcceptedFormat;

/**
 * The OpenAI image dimension is independent of the text dimension: a model or
 * Terminal Target may serve text only, images only, or both. `null` means the
 * row does not serve images at all.
 */
export type OpenAIImageOperations =
 | "generations"
 | "edits"
 | "generations_and_edits";

export type OpenAIImageCapability = OpenAIImageOperations;

// Recursive JSON value model shared by the custom request parameters editor
// and the Connection payload contract.
export type JsonValue =
 | null
 | boolean
 | number
 | string
 | JsonValue[]
 | JsonObject;
export type JsonObject = { [key: string]: JsonValue };

export interface Connection {
 id: number;
 profile_id: number;
 model_config_id?: number | null;
 api_family: ApiFamily;
 endpoint_id: number;
 endpoint?: Endpoint;
 is_active: boolean;
 priority: number;
 name: string | null;
 auth_type: string | null;
 custom_headers: Record<string, string> | null;
 custom_headers_redacted: string[] | null;
 custom_request_parameters: JsonObject | null;
 routing_schedule: RoutingSchedule | null;
 routing_schedule_state: RoutingScheduleState | null;
 openai_text_capability: OpenAITextCapability | null;
 openai_image_capability: OpenAIImageCapability | null;
 pricing_template_id: number | null;
 qps_limit: number | null;
 max_in_flight_non_stream: number | null;
 max_in_flight_stream: number | null;
 pricing_template: ConnectionPricingTemplateSummary | null;
 created_at: string;
 updated_at: string;
}

export type TerminalTarget = Connection;

export interface ConnectionCreate {
 api_family: ApiFamily;
 endpoint_id?: number;
 endpoint_create?: EndpointCreate;
 is_active?: boolean;
 name?: string | null;
 auth_type?: string | null;
 custom_headers?: Record<string, string> | null;
 custom_request_parameters?: JsonObject | null;
 routing_schedule?: RoutingSchedule | null;
 openai_text_capability?: OpenAITextCapability | null;
 openai_image_capability?: OpenAIImageCapability | null;
 pricing_template_id?: number | null;
 qps_limit?: number | null;
 max_in_flight_non_stream?: number | null;
 max_in_flight_stream?: number | null;
}

export interface ConnectionUpdate {
 api_family?: ApiFamily;
 endpoint_id?: number;
 endpoint_create?: EndpointCreate;
 is_active?: boolean;
 name?: string | null;
 auth_type?: string | null;
 custom_headers?: Record<string, string> | null;
 custom_request_parameters?: JsonObject | null;
 routing_schedule?: RoutingSchedule | null;
 openai_text_capability?: OpenAITextCapability | null;
 openai_image_capability?: OpenAIImageCapability | null;
 pricing_template_id?: number | null;
 /**
  * Required by the backend whenever `pricing_template_id` is sent: both CAS
  * fields guard concurrent overwrites of the Terminal Target pricing
  * reference. Omit all three fields to leave pricing untouched.
  */
 expected_connection_updated_at?: string;
 expected_pricing_template_id?: number | null;
 qps_limit?: number | null;
 max_in_flight_non_stream?: number | null;
 max_in_flight_stream?: number | null;
}

export type ModelConnectionCreate = Omit<ConnectionCreate, "api_family"> & {
 api_family?: ApiFamily;
};

export type ModelTerminalTargetCreate = ModelConnectionCreate;

export type ModelConnectionUpdate = Omit<ConnectionUpdate, "api_family">;

export type ModelTerminalTargetUpdate = ModelConnectionUpdate;

export interface ConnectionReference {
 target_id: number;
 model_config_id: number;
 model_id: string;
 api_family: ApiFamily;
 position: number;
 is_enabled: boolean;
}

export interface ConnectionReferencesResponse {
 connection_id: number;
 items: ConnectionReference[];
}

export interface ConnectionDropdownItem {
 id: number;
 endpoint_id: number;
 name: string | null;
}

export interface ConnectionDropdownResponse {
 items: ConnectionDropdownItem[];
}
