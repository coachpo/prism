import type { Connection, ModelConfig, ModelConfigListItem } from "@/lib/types";
import { formatNumber, getCurrentLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";

type ModelDetailPathTarget = Pick<ModelConfig, "id"> | Pick<ModelConfigListItem, "id">;

export const getModelDetailPath = (model: ModelDetailPathTarget): string => `/route/models/${model.id}`;

export const formatLatencyForDisplay = (value: number | null): string => {
  if (value === null || !Number.isFinite(value)) return "-";
  if (value >= 1000) {
    const fractionDigits = value >= 10000 ? 1 : 2;
    return `${formatNumber(value / 1000, getCurrentLocale(), {
      minimumFractionDigits: fractionDigits,
      maximumFractionDigits: fractionDigits,
    })}s`;
  }
  return `${Math.round(value)}ms`;
};

export const getConnectionName = (
  connection: Pick<Connection, "id" | "name" | "endpoint">
): string => {
  const explicitName = connection.name?.trim();
  if (explicitName && explicitName.length > 0) {
    return explicitName;
  }
  const endpointName = connection.endpoint?.name?.trim();
  if (endpointName && endpointName.length > 0) {
    return endpointName;
  }
  return getStaticMessages().modelDetail.connectionFallback(connection.id);
};
