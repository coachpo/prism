export interface AuthStatus {
  auth_enabled: boolean;
}

export interface AuthSettings {
  auth_enabled: boolean;
  username: string | null;
  has_password: boolean;
  proxy_key_limit: number;
}

export interface AuthSettingsUpdate {
  auth_enabled: boolean;
  username?: string | null;
  password?: string | null;
}

export type LoginSessionDuration = "session" | "7_days" | "30_days";


export interface LoginRequest {
  username: string;
  password: string;
  session_duration?: LoginSessionDuration;
}

export interface SessionResponse {
  authenticated: boolean;
  auth_enabled: boolean;
  username: string | null;
}

export interface ProxyApiKey {
  id: number;
  name: string;
  key_prefix: string;
  key_preview: string;
  is_active: boolean;
  expires_at: string | null;
  last_used_at: string | null;
  last_used_ip: string | null;
  notes: string | null;
  rotated_from_id: number | null;
  created_at: string;
  updated_at: string;
}

export interface ProxyApiKeyCreate {
  name: string;
  notes?: string | null;
  expires_at?: string | null;
}

export interface ProxyApiKeyUpdate {
  name: string;
  notes: string | null;
  is_active: boolean;
  expires_at: string | null;
}

export interface ProxyApiKeyCreateResponse {
  key: string;
  item: ProxyApiKey;
}

export interface ProxyApiKeyRotateResponse {
  key: string;
  item: ProxyApiKey;
}
