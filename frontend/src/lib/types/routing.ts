import type { ApiFamily } from "./vendor";

export type PricingComponentPrice = string;

export interface Endpoint {
  id: number;
  profile_id?: number;
  name: string;
  base_url: string;
  has_api_key: boolean;
  masked_api_key: string | null;
  position: number;
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

export interface PricingTemplate {
  id: number;
  profile_id: number;
  name: string;
  description: string | null;
  pricing_unit: "PER_1M";
  pricing_currency_code: string;
  input_price: string;
  output_price: string;
  cached_input_price: PricingComponentPrice;
  cache_creation_price: PricingComponentPrice;
  reasoning_price: PricingComponentPrice;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface PricingTemplateCreate {
  name: string;
  description?: string | null;
  pricing_unit?: "PER_1M";
  pricing_currency_code: string;
  input_price: string;
  output_price: string;
  cached_input_price?: PricingComponentPrice;
  cache_creation_price?: PricingComponentPrice;
  reasoning_price?: PricingComponentPrice;
}

export interface PricingTemplateUpdate {
  expected_updated_at: string;
  name?: string;
  description?: string | null;
  pricing_unit?: "PER_1M";
  pricing_currency_code?: string;
  input_price?: string;
  output_price?: string;
  cached_input_price?: PricingComponentPrice;
  cache_creation_price?: PricingComponentPrice;
  reasoning_price?: PricingComponentPrice;
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

export interface ConnectionPricingTemplateSummary {
  id: number;
  name: string;
  pricing_unit: "PER_1M";
  pricing_currency_code: string;
  version: number;
}

export type OpenAIProbeEndpointVariant =
  | "responses_minimal"
  | "responses_reasoning_none"
  | "chat_completions_minimal"
  | "chat_completions_reasoning_none";

export interface ContextCapabilityFields {
  context_window_tokens: number | null;
  default_output_token_reserve: number;
  max_context_utilization: number;
}

export interface ContextCapabilityOverrides {
  context_window_tokens: number | null;
  default_output_token_reserve: number | null;
  max_context_utilization: number | null;
}

export interface Connection extends ContextCapabilityFields {
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
  openai_probe_endpoint_variant: OpenAIProbeEndpointVariant | null;
  context_capability_overrides?: ContextCapabilityOverrides;
  pricing_template_id: number | null;
  qps_limit: number | null;
  max_in_flight_non_stream: number | null;
  max_in_flight_stream: number | null;
  pricing_template: ConnectionPricingTemplateSummary | null;
  health_status: string;
  health_detail: string | null;
  last_health_check: string | null;
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
  openai_probe_endpoint_variant?: OpenAIProbeEndpointVariant | null;
  context_window_tokens?: number | null;
  default_output_token_reserve?: number | null;
  max_context_utilization?: number | null;
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
  openai_probe_endpoint_variant?: OpenAIProbeEndpointVariant | null;
  context_window_tokens?: number | null;
  default_output_token_reserve?: number | null;
  max_context_utilization?: number | null;
  pricing_template_id?: number | null;
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

export interface HealthCheckResponse {
  connection_id: number;
  health_status: string;
  checked_at: string;
  detail: string;
  response_time_ms: number;
}

export interface ConnectionHealthCheckPreviewResponse {
  health_status: string;
  checked_at: string;
  detail: string;
  response_time_ms: number;
}

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
