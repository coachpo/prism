import { getStaticMessages } from "@/i18n/staticMessages";
import type { PricingTemplateConnectionUsageItem } from "@/lib/types";

export const parsePricingTemplateUsageRows = (
  detail: unknown,
): PricingTemplateConnectionUsageItem[] => {
  if (!detail || typeof detail !== "object") return [];
  const payload = detail as { connections?: unknown; detail?: unknown };
  const maybeConnections =
    payload.connections ??
    (payload.detail &&
    typeof payload.detail === "object" &&
    "connections" in payload.detail
      ? (payload.detail as { connections?: unknown }).connections
      : undefined);
  if (!Array.isArray(maybeConnections)) return [];
  const rows: PricingTemplateConnectionUsageItem[] = [];
  for (const connection of maybeConnections) {
    if (!connection || typeof connection !== "object") continue;
    const entry = connection as Record<string, unknown>;
    const connectionId =
      typeof entry.connection_id === "number" ? entry.connection_id : null;
    const modelConfigId =
      typeof entry.model_config_id === "number" ? entry.model_config_id : null;
    const endpointId =
      typeof entry.endpoint_id === "number" ? entry.endpoint_id : null;
    if (connectionId === null || modelConfigId === null || endpointId === null)
      continue;
    const modelId =
      typeof entry.model_id === "string" && entry.model_id.trim().length > 0
        ? entry.model_id
        : getStaticMessages().pricingTemplatesData.unknownModel;
    const endpointName =
      typeof entry.endpoint_name === "string" &&
      entry.endpoint_name.trim().length > 0
        ? entry.endpoint_name
        : getStaticMessages().pricingTemplatesData.endpointWithId(
            String(endpointId),
          );
    rows.push({
      connection_id: connectionId,
      connection_name:
        typeof entry.connection_name === "string"
          ? entry.connection_name
          : null,
      model_config_id: modelConfigId,
      model_id: modelId,
      endpoint_id: endpointId,
      endpoint_name: endpointName,
    });
  }
  return rows;
};
