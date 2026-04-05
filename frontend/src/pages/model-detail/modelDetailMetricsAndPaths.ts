import type { Connection } from "@/lib/types";
import { formatNumber, getCurrentLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";

type ModelDetailPathTarget = {
  id: number;
  model_type: "native" | "proxy";
};

export const getModelDetailPath = ({ id, model_type }: ModelDetailPathTarget): string =>
  model_type === "proxy" ? `/models/${id}/proxy` : `/models/${id}`;

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
