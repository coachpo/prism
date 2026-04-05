export type ApiFamily = "openai" | "anthropic" | "gemini";
export type VendorIconKey = string;

export interface Vendor {
  id: number;
  key: string;
  name: string;
  description: string | null;
  icon_key: VendorIconKey | null;
  audit_enabled: boolean;
  audit_capture_bodies: boolean;
  created_at: string;
  updated_at: string;
}

export interface VendorCreate {
  key: string;
  name: string;
  description?: string | null;
  icon_key?: VendorIconKey | null;
}

export interface VendorUpdate {
  key?: string;
  name?: string;
  description?: string | null;
  icon_key?: VendorIconKey | null;
  audit_enabled?: boolean;
  audit_capture_bodies?: boolean;
}

export interface VendorModelUsageItem {
  model_config_id: number;
  profile_id: number;
  profile_name: string;
  model_id: string;
  display_name: string | null;
  model_type: "native" | "proxy";
  api_family: ApiFamily;
  is_enabled: boolean;
}

export interface VendorDeleteConflictDetail {
  message: string;
  models: VendorModelUsageItem[];
}
