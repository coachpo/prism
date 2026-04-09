export type NonEmptyArray<T> = [T, ...T[]];

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
  cached_input_price: string | null;
  cache_creation_price: string | null;
  reasoning_price: string | null;
  missing_special_token_price_policy: "MAP_TO_OUTPUT" | "ZERO_COST";
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
  cached_input_price?: string | null;
  cache_creation_price?: string | null;
  reasoning_price?: string | null;
  missing_special_token_price_policy?: "MAP_TO_OUTPUT" | "ZERO_COST";
}

export interface PricingTemplateUpdate {
  expected_updated_at: string;
  name?: string;
  description?: string | null;
  pricing_unit?: "PER_1M";
  pricing_currency_code?: string;
  input_price?: string;
  output_price?: string;
  cached_input_price?: string | null;
  cache_creation_price?: string | null;
  reasoning_price?: string | null;
  missing_special_token_price_policy?: "MAP_TO_OUTPUT" | "ZERO_COST";
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

export interface Connection {
  id: number;
  model_config_id: number;
  endpoint_id: number;
  endpoint?: Endpoint;
  is_active: boolean;
  priority: number;
  name: string | null;
  auth_type: string | null;
  custom_headers: Record<string, string> | null;
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

export interface ConnectionCreate {
  endpoint_id?: number;
  endpoint_create?: EndpointCreate;
  is_active?: boolean;
  name?: string | null;
  auth_type?: string | null;
  custom_headers?: Record<string, string> | null;
  pricing_template_id?: number | null;
  qps_limit?: number | null;
  max_in_flight_non_stream?: number | null;
  max_in_flight_stream?: number | null;
}

export interface ConnectionUpdate {
  endpoint_id?: number;
  endpoint_create?: EndpointCreate;
  is_active?: boolean;
  name?: string | null;
  auth_type?: string | null;
  custom_headers?: Record<string, string> | null;
  pricing_template_id?: number | null;
  qps_limit?: number | null;
  max_in_flight_non_stream?: number | null;
  max_in_flight_stream?: number | null;
}

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

export interface ConnectionOwnerResponse {
  connection_id: number;
  model_config_id: number;
  model_id: string;
  connection_name: string | null;
  endpoint_id: number;
  endpoint_name: string;
  endpoint_base_url: string;
}

export interface ConnectionDropdownItem {
  id: number;
  endpoint_id: number;
  name: string | null;
}

export interface ConnectionDropdownResponse {
  items: ConnectionDropdownItem[];
}

export interface ModelConnectionsBatchParams {
  model_config_ids: NonEmptyArray<number>;
}

export interface ModelConnectionsBatchItem {
  model_config_id: number;
  connections: Connection[];
}

export interface ModelConnectionsBatchResponse {
  items: ModelConnectionsBatchItem[];
}
