// TypeScript types matching backend Pydantic schemas exactly

// --- Provider ---
export interface Provider {
  id: number;
  name: string;
  provider_type: string;
  description: string | null;
  created_at: string;
  updated_at: string;
}

// --- Endpoint ---
export interface Endpoint {
  id: number;
  model_config_id: number;
  base_url: string;
  api_key: string;
  is_active: boolean;
  priority: number;
  description: string | null;
  health_status: string;
  last_health_check: string | null;
  created_at: string;
  updated_at: string;
}

export interface EndpointCreate {
  base_url: string;
  api_key: string;
  is_active?: boolean;
  priority?: number;
  description?: string | null;
}

export interface EndpointUpdate {
  base_url?: string;
  api_key?: string;
  is_active?: boolean;
  priority?: number;
  description?: string | null;
}

export interface HealthCheckResponse {
  endpoint_id: number;
  health_status: string;
  checked_at: string;
  detail: string;
}

// --- Model Config ---
export interface ModelConfig {
  id: number;
  provider_id: number;
  provider: Provider;
  model_id: string;
  display_name: string | null;
  model_type: string;
  redirect_to: string | null;
  lb_strategy: string;
  is_enabled: boolean;
  endpoints: Endpoint[];
  created_at: string;
  updated_at: string;
}

export interface ModelConfigListItem {
  id: number;
  provider_id: number;
  provider: Provider;
  model_id: string;
  display_name: string | null;
  model_type: string;
  redirect_to: string | null;
  lb_strategy: string;
  is_enabled: boolean;
  endpoint_count: number;
  active_endpoint_count: number;
  created_at: string;
  updated_at: string;
}

export interface ModelConfigCreate {
  provider_id: number;
  model_id: string;
  display_name?: string | null;
  model_type?: string;
  redirect_to?: string | null;
  lb_strategy?: string;
  is_enabled?: boolean;
}

export interface ModelConfigUpdate {
  provider_id?: number;
  model_id?: string;
  display_name?: string | null;
  model_type?: string;
  redirect_to?: string | null;
  lb_strategy?: string;
  is_enabled?: boolean;
}
