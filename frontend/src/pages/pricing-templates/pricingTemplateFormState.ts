import type {
  PricingTemplate,
  PricingTemplateConnectionUsageItem,
} from "@/lib/types";
import { getStaticMessages } from "@/i18n/staticMessages";

export type PricingTemplateFormState = {
  name: string;
  description: string;
  pricing_currency_code: string;
  input_price: string;
  output_price: string;
  cached_input_price: string;
  cache_creation_price: string;
  reasoning_price: string;
  missing_special_token_price_policy: "MAP_TO_OUTPUT" | "ZERO_COST";
};

export const DEFAULT_PRICING_TEMPLATE_FORM: PricingTemplateFormState = {
  name: "",
  description: "",
  pricing_currency_code: "USD",
  input_price: "",
  output_price: "",
  cached_input_price: "",
  cache_creation_price: "",
  reasoning_price: "",
  missing_special_token_price_policy: "MAP_TO_OUTPUT",
};

export const pricingTemplateFormStateFromTemplate = (
  template: PricingTemplate
): PricingTemplateFormState => ({
  name: template.name,
  description: template.description ?? "",
  pricing_currency_code: template.pricing_currency_code,
  input_price: template.input_price,
  output_price: template.output_price,
  cached_input_price: template.cached_input_price ?? "",
  cache_creation_price: template.cache_creation_price ?? "",
  reasoning_price: template.reasoning_price ?? "",
  missing_special_token_price_policy: template.missing_special_token_price_policy,
});

export const normalizeOptionalTemplatePrice = (value: string): string | null => {
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : null;
};

export const isNonNegativeDecimalString = (value: string): boolean => {
  const trimmed = value.trim();
  if (!/^\d+(\.\d+)?$/.test(trimmed)) {
    return false;
  }
  const parsed = Number(trimmed);
  return Number.isFinite(parsed) && parsed >= 0;
};

export const parsePricingTemplateUsageRows = (
  detail: unknown
): PricingTemplateConnectionUsageItem[] => {
  if (!detail || typeof detail !== "object") {
    return [];
  }
  const payload = detail as { connections?: unknown; detail?: unknown };
  const maybeConnections =
    payload.connections ??
    (
      payload.detail &&
      typeof payload.detail === "object" &&
      "connections" in payload.detail
        ? (payload.detail as { connections?: unknown }).connections
        : undefined
    );
  if (!Array.isArray(maybeConnections)) {
    return [];
  }

  const rows: PricingTemplateConnectionUsageItem[] = [];
  for (const connection of maybeConnections) {
    if (!connection || typeof connection !== "object") {
      continue;
    }

    const entry = connection as Record<string, unknown>;
    const connectionId =
      typeof entry.connection_id === "number" ? entry.connection_id : null;
    const modelConfigId =
      typeof entry.model_config_id === "number" ? entry.model_config_id : null;
    const endpointId =
      typeof entry.endpoint_id === "number" ? entry.endpoint_id : null;
    if (connectionId === null || modelConfigId === null || endpointId === null) {
      continue;
    }

    const modelId =
      typeof entry.model_id === "string" && entry.model_id.trim().length > 0
        ? entry.model_id
        : getStaticMessages().pricingTemplatesData.unknownModel;
    const endpointName =
      typeof entry.endpoint_name === "string" && entry.endpoint_name.trim().length > 0
        ? entry.endpoint_name
        : getStaticMessages().pricingTemplatesData.endpointWithId(String(endpointId));

    rows.push({
      connection_id: connectionId,
      connection_name:
        typeof entry.connection_name === "string" ? entry.connection_name : null,
      model_config_id: modelConfigId,
      model_id: modelId,
      endpoint_id: endpointId,
      endpoint_name: endpointName,
    });
  }

  return rows;
};
