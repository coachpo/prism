import type { Connection, ModelConfig, ModelConfigListItem } from "@/lib/types";
import { formatNumber, getCurrentLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";

type ModelDetailPathTarget = Pick<ModelConfig, "id"> | Pick<ModelConfigListItem, "id">;

export const getModelDetailPath = (model: ModelDetailPathTarget): string => `/route/models/${model.id}`;

/**
 * 延迟只有这一个写法：两页对照同一个数时不该出现 `196.2s` 与 `196,261 ms`
 * 两种面貌。单位与数值之间留空格（DESIGN.md: values carry units）。
 */
export const formatLatencyForDisplay = (value: number | null): string => {
  if (value === null || !Number.isFinite(value)) return "—";
  if (value >= 1000) {
    return `${formatNumber(value / 1000, getCurrentLocale(), {
      minimumFractionDigits: 1,
      maximumFractionDigits: 1,
    })} s`;
  }
  return `${Math.round(value)} ms`;
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
