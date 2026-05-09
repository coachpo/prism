import type {
  ModelType,
  ProxyTarget,
} from "./model-stats";
import type { LoadbalanceStrategyCreate } from "./loadbalance";
import type { ApiFamily } from "./vendor";
import type { OpenAIProbeEndpointVariant } from "./routing";

export interface ConfigEndpointExport {
  name: string;
  base_url: string;
  api_key_secret_ref: string | null;
  position?: number | null;
}

export interface ConfigEndpointImport {
  name: string;
  base_url: string;
  api_key_secret_ref?: string | null;
  position?: number | null;
}

export interface ConfigPricingTemplateExport {
  name: string;
  description: string | null;
  pricing_unit: "PER_1M";
  pricing_currency_code: string;
  input_price: string;
  output_price: string;
  cached_input_price: string | null;
  cache_creation_price: string | null;
  reasoning_price: string | null;
  version: number;
}

export interface ConfigPricingTemplateImport {
  name: string;
  description?: string | null;
  pricing_unit?: "PER_1M";
  pricing_currency_code: string;
  input_price: string;
  output_price: string;
  cached_input_price?: string | null;
  cache_creation_price?: string | null;
  reasoning_price?: string | null;
  version?: number;
}

export type ConfigLoadbalanceStrategyExport = LoadbalanceStrategyCreate;

export type ConfigLoadbalanceStrategyImport = LoadbalanceStrategyCreate;

export interface ConfigConnectionExport {
  endpoint_name: string;
  pricing_template_name: string | null;
  is_active: boolean;
  priority: number;
  name: string | null;
  auth_type: ApiFamily | null;
  custom_headers: Record<string, string> | null;
  openai_probe_endpoint_variant: OpenAIProbeEndpointVariant | null;
  qps_limit: number | null;
  max_in_flight_non_stream: number | null;
  max_in_flight_stream: number | null;
}

export interface ConfigConnectionImport {
  endpoint_name: string;
  pricing_template_name?: string | null;
  is_active?: boolean;
  priority?: number;
  name?: string | null;
  auth_type?: ApiFamily | null;
  custom_headers?: Record<string, string> | null;
  openai_probe_endpoint_variant?: OpenAIProbeEndpointVariant | null;
  qps_limit?: number | null;
  max_in_flight_non_stream?: number | null;
  max_in_flight_stream?: number | null;
}

export interface ConfigModelExport {
  vendor_key: string | null;
  api_family: ApiFamily;
  model_id: string;
  display_name: string | null;
  model_type: ModelType;
  proxy_targets: ProxyTarget[];
  loadbalance_strategy_name: string | null;
  is_enabled: boolean;
  connections: ConfigConnectionExport[];
}

export type ConfigModelImport =
  | {
      vendor_key?: string | null;
      api_family: ApiFamily;
      model_id: string;
      display_name?: string | null;
      model_type: "native";
      proxy_targets: [];
      loadbalance_strategy_name: string;
      is_enabled?: boolean;
      connections: ConfigConnectionImport[];
    }
  | {
      vendor_key?: string | null;
      api_family: ApiFamily;
      model_id: string;
      display_name?: string | null;
      model_type: "proxy";
      proxy_targets: ProxyTarget[];
      loadbalance_strategy_name: null;
      is_enabled?: boolean;
      connections: [];
    };

export interface ConfigEndpointFxRateExport {
  model_id: string;
  endpoint_name: string;
  fx_rate: string;
}

export interface ConfigEndpointFxRateImport {
  model_id: string;
  endpoint_name: string;
  fx_rate: string;
}

export interface ConfigUserSettingsExport {
  report_currency_code: string;
  report_currency_symbol: string;
  endpoint_fx_mappings: ConfigEndpointFxRateExport[];
  timezone_preference?: string | null;
}

export interface ConfigUserSettingsImport {
  report_currency_code?: string;
  report_currency_symbol?: string;
  endpoint_fx_mappings?: ConfigEndpointFxRateImport[];
  timezone_preference?: string | null;
}

export interface ConfigVendorRef {
  key: string;
  name_hint?: string | null;
  description_hint?: string | null;
  icon_key_hint?: string | null;
}

export interface ConfigSecretPayloadEntry {
  ref: string;
  ciphertext: string;
}

export interface ConfigSecretPayload {
  kind: "encrypted";
  cipher: "fernet-v1";
  key_id: string;
  entries: ConfigSecretPayloadEntry[];
}

export interface ConfigVendorExport {
  key: string;
  name: string;
  description: string | null;
  icon_key: string | null;
  audit_enabled: boolean;
  audit_capture_bodies: boolean;
}

export type ConfigVendorImport = ConfigVendorExport;

export interface ConfigExportResponse {
  version: 1;
  bundle_kind: "profile_config";
  exported_at: string;
  vendor_refs: ConfigVendorRef[];
  endpoints: ConfigEndpointExport[];
  pricing_templates: ConfigPricingTemplateExport[];
  loadbalance_strategies: ConfigLoadbalanceStrategyExport[];
  models: ConfigModelExport[];
  profile_settings?: ConfigUserSettingsExport | null;
  header_blocklist_rules: HeaderBlocklistRuleExport[];
  user_agent_client_rules: UserAgentClientRuleExport[];
  secret_payload: ConfigSecretPayload;
}

export interface ConfigImportRequest {
  version: 1;
  bundle_kind: "profile_config";
  exported_at?: string;
  vendor_refs: ConfigVendorRef[];
  endpoints: ConfigEndpointImport[];
  pricing_templates: ConfigPricingTemplateImport[];
  loadbalance_strategies: ConfigLoadbalanceStrategyImport[];
  models: ConfigModelImport[];
  profile_settings?: ConfigUserSettingsImport | null;
  header_blocklist_rules?: HeaderBlocklistRuleExport[];
  user_agent_client_rules?: UserAgentClientRuleExport[];
  secret_payload: ConfigSecretPayload;
}

export interface ConfigImportResponse {
  endpoints_imported: number;
  pricing_templates_imported: number;
  strategies_imported: number;
  models_imported: number;
  connections_imported: number;
}

export interface ConfigImportVendorResolution {
  vendor_key: string;
  resolution: "reuse" | "create";
  warning?: string | null;
}

export interface ConfigImportReplacementScope {
  target: "selected_profile";
  endpoints: number;
  pricing_templates: number;
  loadbalance_strategies: number;
  models: number;
  connections: number;
  header_blocklist_rules: number;
  user_agent_client_rules: number;
  profile_settings: boolean;
}

export interface ConfigImportUntouchedScope {
  other_profiles: boolean;
  existing_global_vendor_metadata: boolean;
  request_logs: boolean;
}

export interface ConfigImportVendorSummary {
  create_count: number;
  reuse_count: number;
  warning_count: number;
}

export interface ConfigImportSecretSummary {
  endpoint_secret_refs: number;
  secret_payload_entries: number;
  decryptable_secret_refs: number;
}

export interface ConfigImportPreviewResponse {
  ready: boolean;
  version: 1;
  bundle_kind: "profile_config";
  preview_token: string;
  bundle_fingerprint: string;
  replacement_scope: ConfigImportReplacementScope;
  untouched_scope: ConfigImportUntouchedScope;
  vendor_summary: ConfigImportVendorSummary;
  secret_summary: ConfigImportSecretSummary;
  endpoints_imported: number;
  pricing_templates_imported: number;
  strategies_imported: number;
  models_imported: number;
  connections_imported: number;
  vendor_resolutions: ConfigImportVendorResolution[];
  secret_key_id: string;
  decryptable_secret_refs: string[];
  blocking_errors: string[];
  warnings: string[];
}

export interface VendorCatalogExportResponse {
  version: 1;
  bundle_kind: "vendor_catalog";
  exported_at: string;
  vendors: ConfigVendorExport[];
}

export interface VendorCatalogImportRequest {
  version: 1;
  bundle_kind: "vendor_catalog";
  exported_at?: string;
  vendors: ConfigVendorImport[];
}

export interface VendorCatalogImportMutationScope {
  target: "global_vendor_catalog";
  create_count: number;
  update_count: number;
  unchanged_count: number;
}

export interface VendorCatalogImportUntouchedScope {
  profiles: boolean;
  profile_scoped_config: boolean;
  request_logs: boolean;
}

export interface VendorCatalogImportPreviewResponse {
  ready: boolean;
  version: 1;
  bundle_kind: "vendor_catalog";
  preview_token: string;
  bundle_fingerprint: string;
  mutation_scope: VendorCatalogImportMutationScope;
  untouched_scope: VendorCatalogImportUntouchedScope;
  create_count: number;
  update_count: number;
  blocking_errors: string[];
  warnings: string[];
}

export interface VendorCatalogImportResponse {
  created_count: number;
  updated_count: number;
}

export interface AuditLogListItem {
  id: number;
  request_log_id: number | null;
  profile_id: number;
  vendor_id?: number;
  model_id: string;
  endpoint_id: number | null;
  connection_id: number | null;
  endpoint_base_url: string | null;
  endpoint_description: string | null;
  request_method: string;
  request_url: string;
  request_headers: string;
  request_body_preview: string | null;
  request_body_stored: boolean;
  response_status: number;
  response_body_stored: boolean;
  is_stream: boolean;
  duration_ms: number;
  audit_enabled_at_request: boolean;
  audit_capture_bodies_at_request: boolean;
  created_at: string;
}

export interface AuditLogDetail {
  id: number;
  request_log_id: number | null;
  profile_id: number;
  vendor_id?: number;
  model_id: string;
  endpoint_id: number | null;
  connection_id: number | null;
  endpoint_base_url: string | null;
  endpoint_description: string | null;
  request_method: string;
  request_url: string;
  request_headers: string;
  request_body: string | null;
  request_body_stored: boolean;
  response_status: number;
  response_headers: string | null;
  response_body: string | null;
  response_body_stored: boolean;
  is_stream: boolean;
  duration_ms: number;
  audit_enabled_at_request: boolean;
  audit_capture_bodies_at_request: boolean;
  created_at: string;
}

export interface AuditLogListResponse {
  items: AuditLogListItem[];
  total: number;
  limit: number;
  offset: number;
}

export interface AuditLogParams {
  request_log_id?: number;
  vendor_id?: number;
  model_id?: string;
  status_code?: number;
  endpoint_id?: number;
  connection_id?: number;
  from_time?: string;
  to_time?: string;
  limit?: number;
  offset?: number;
}

export type LogRetentionTable = "request_logs" | "audit_logs" | "usage_request_events" | "loadbalance_events";

export interface LogRetentionJobScope {
  before?: string | null;
  table: LogRetentionTable;
  cutoff?: string | null;
  delete_all?: boolean;
}

export interface LogRetentionJobRequest {
  table: LogRetentionTable;
  cutoff?: string | null;
  delete_all?: boolean;
  reason: string;
}

export interface LogRetentionJobResponse {
  job_id: string;
  state: string;
  status_url: string;
  scope: LogRetentionJobScope;
}

export interface EndpointFxMapping {
  model_id: string;
  endpoint_id: number;
  fx_rate: string;
}

export interface CostingSettingsResponse {
  report_currency_code: string;
  report_currency_symbol: string;
  endpoint_fx_mappings: EndpointFxMapping[];
  timezone_preference?: string | null;
}

export interface TimezonePreferenceResponse {
  timezone_preference?: string | null;
}

export interface CostingSettingsUpdate {
  report_currency_code: string;
  report_currency_symbol: string;
  endpoint_fx_mappings: EndpointFxMapping[];
  timezone_preference?: string | null;
}

export interface TimezonePreferenceUpdate {
  timezone_preference?: string | null;
}

export interface RetentionSettingsResponse {
  request_logs_retention_days: number | null;
  statistics_retention_days: number | null;
  audit_logs_retention_days: number | null;
  loadbalance_events_retention_days: number | null;
}

export interface RetentionSettingsUpdate {
  request_logs_retention_days?: number | null;
  statistics_retention_days?: number | null;
  audit_logs_retention_days?: number | null;
  loadbalance_events_retention_days?: number | null;
}

export interface HeaderBlocklistRule {
  id: number;
  name: string;
  match_type: "exact" | "prefix";
  pattern: string;
  enabled: boolean;
  is_system: boolean;
  created_at: string;
  updated_at: string;
}

export interface HeaderBlocklistRuleCreate {
  name: string;
  match_type: "exact" | "prefix";
  pattern: string;
  enabled?: boolean;
}

export interface HeaderBlocklistRuleUpdate {
  name?: string;
  match_type?: "exact" | "prefix";
  pattern?: string;
  enabled?: boolean;
}

export interface HeaderBlocklistRuleExport {
  name: string;
  match_type: "exact" | "prefix";
  pattern: string;
  enabled: boolean;
}

export interface UserAgentClientRuleExport {
  name: string;
  pattern: string;
  enabled: boolean;
}

export interface UserAgentClientRule {
  id: number;
  name: string;
  pattern: string;
  enabled: boolean;
  is_system: boolean;
  created_at: string;
  updated_at: string;
}

export interface UserAgentClientRuleCreate {
  name: string;
  pattern: string;
  enabled?: boolean;
}

export interface UserAgentClientRuleUpdate {
  name?: string;
  pattern?: string;
  enabled?: boolean;
}
