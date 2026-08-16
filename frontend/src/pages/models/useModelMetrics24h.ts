import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { ModelConfigListItem } from "@/lib/types";
import type { ModelDerivedMetric } from "./modelTableContracts";

export function useModelMetrics24h(models: ModelConfigListItem[]) {
  const [modelMetrics24h, setModelMetrics24h] = useState<Record<number, ModelDerivedMetric>>({});
  const [modelSpend30dMicros, setModelSpend30dMicros] = useState<Record<number, number | null>>({});
  const [metricsLoading, setMetricsLoading] = useState(false);
  // A failed read is its own state. Blanking every metric to `null` made a
  // broken telemetry call look identical to a model that genuinely has no
  // traffic, which the honesty contract treats as a defect.
  const [metricsFailed, setMetricsFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;

    const fetchModelMetrics = async () => {
      if (models.length === 0) {
        setModelMetrics24h({});
        setModelSpend30dMicros({});
        setMetricsLoading(false);
        setMetricsFailed(false);
        return;
      }

      setMetricsLoading(true);

      try {
        const uniqueModelIds = Array.from(new Set(models.map((model) => model.model_id)));
        const response = await api.stats.modelMetrics({
          model_ids: uniqueModelIds,
          summary_window_hours: 24,
          spending_preset: "last_30_days",
        });

        if (cancelled) {
          return;
        }

        const metricsByModelId = new Map(response.items.map((item) => [item.model_id, item]));
        const nextMetrics: Record<number, ModelDerivedMetric> = {};
        const nextSpend: Record<number, number | null> = {};

        for (const model of models) {
          const row = metricsByModelId.get(model.model_id);
          nextMetrics[model.id] = {
            success_rate: row?.success_rate ?? null,
            request_count_24h: row?.request_count_24h ?? null,
            p95_latency_ms: row?.p95_latency_ms ?? null,
          };
          nextSpend[model.id] = row?.spend_30d_micros ?? null;
        }

        setModelMetrics24h(nextMetrics);
        setModelSpend30dMicros(nextSpend);
        setMetricsFailed(false);
      } catch {
        if (cancelled) {
          return;
        }

        // Keep whatever last succeeded on screen and flag the failure; the
        // table renders it as a failure, never as an absent or zero metric.
        setMetricsFailed(true);
      } finally {
        if (!cancelled) {
          setMetricsLoading(false);
        }
      }
    };

    void fetchModelMetrics();

    return () => {
      cancelled = true;
    };
  }, [models]);

  return {
    metricsFailed,
    metricsLoading,
    modelMetrics24h,
    modelSpend30dMicros,
  };
}
