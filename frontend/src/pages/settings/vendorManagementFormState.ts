import type {
  Vendor,
  VendorCreate,
  VendorDeleteConflictDetail,
  VendorModelUsageItem,
  VendorUpdate,
} from "@/lib/types";

export interface VendorFormState {
  key: string;
  name: string;
  description: string;
  icon_key: string | null;
}

export const DEFAULT_VENDOR_FORM: VendorFormState = {
  key: "",
  name: "",
  description: "",
  icon_key: null,
};

export function vendorFormStateFromVendor(vendor: Vendor): VendorFormState {
  return {
    key: vendor.key,
    name: vendor.name,
    description: vendor.description ?? "",
    icon_key: vendor.icon_key,
  };
}

export function normalizeVendorPayload(
  form: VendorFormState,
): VendorCreate & VendorUpdate {
  const description = form.description.trim();
  const iconKey = form.icon_key?.trim().toLowerCase() ?? "";

  return {
    key: form.key.trim(),
    name: form.name.trim(),
    description: description ? description : null,
    icon_key: iconKey ? iconKey : null,
  };
}

export function parseVendorUsageRows(detail: unknown): VendorModelUsageItem[] {
  if (!detail || typeof detail !== "object") {
    return [];
  }

  const { models } = detail as Partial<VendorDeleteConflictDetail>;

  if (!Array.isArray(models)) {
    return [];
  }

  return models.filter(isVendorModelUsageItem);
}

function isVendorModelUsageItem(value: unknown): value is VendorModelUsageItem {
  if (!value || typeof value !== "object") {
    return false;
  }

  const row = value as Partial<VendorModelUsageItem>;

  return (
    typeof row.model_config_id === "number" &&
    typeof row.profile_id === "number" &&
    typeof row.profile_name === "string" &&
    typeof row.model_id === "string" &&
    (typeof row.display_name === "string" || row.display_name === null || row.display_name === undefined) &&
    (row.model_type === "native" || row.model_type === "proxy") &&
    (row.api_family === "openai" || row.api_family === "anthropic" || row.api_family === "gemini") &&
    typeof row.is_enabled === "boolean"
  );
}
