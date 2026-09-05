// pi-lens-ignore: typescript
import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { ModelConfigListItem } from "@/lib/types";
import type {
  ObservabilityScope,
  StatsCoverageByDataset,
} from "@/lib/types/model-stats";
import type {
  ModelDerivedMetric,
  ModelMetricsByScope,
} from "./modelTableContracts";

export type ModelMetricsScope = ObservabilityScope;

function emptyMetric(): ModelDerivedMetric {
  return {
    success_rate: null,
    request_count_24h: null,
    p95_latency_ms: null,
    known_cost_micros: null,
    caliber: null,
    samples: null,
  };
}

function emptyScopes(): ModelMetricsByScope {
  return {
    ingress: emptyMetric(),
    final_execution: emptyMetric(),
    route_attempt: emptyMetric(),
  };
}

export function useModelMetrics24h(
  models: ModelConfigListItem[],
) {
  const [modelMetricsByScope, setModelMetricsByScope] = useState<
    Record<number, ModelMetricsByScope>
  >({});
  const [coverage, setCoverage] = useState<
    { quality: StatsCoverageByDataset; spending: StatsCoverageByDataset } | null
  >(null);
  const [metricsLoading, setMetricsLoading] = useState(false);
  const [reloadToken, setReloadToken] = useState(0);
  // A failed read is its own state. Blanking every metric to `null` made a
  // broken telemetry call look identical to a model that genuinely has no
  // traffic, which the honesty contract treats as a defect.
  const [metricsFailed, setMetricsFailed] = useState(false);
  // 「这些数字是几点的」必须能回答；从未成功过时保持 null，绝不写「刚刚」。
  const [lastSuccessAt, setLastSuccessAt] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    const fetchModelMetrics = async () => {
      if (models.length === 0) {
        setModelMetricsByScope({});
        setCoverage(null);
        setMetricsLoading(false);
        setMetricsFailed(false);
        return;
      }

      setMetricsLoading(true);

      try {
        const uniqueModelIds = Array.from(
          new Set(models.map((model) => model.model_id)),
        );
        const response = await api.stats.modelMetrics({
          model_ids: uniqueModelIds,
          summary_window_hours: 24,
          spending_preset: "last_30_days",
        });

        if (cancelled) {
          return;
        }

        const metricsByModelId = new Map(
          response.items.map((item) => [item.model_id, item]),
        );
        const nextMetrics: Record<number, ModelMetricsByScope> = {};

        for (const model of models) {
          const row = metricsByModelId.get(model.model_id);
          if (!row) {
            nextMetrics[model.id] = emptyScopes();
            continue;
          }
          nextMetrics[model.id] = {
            ingress: toDerivedMetric(row.ingress),
            final_execution: toDerivedMetric(row.final_execution),
            route_attempt: toDerivedMetric(row.route_attempt),
          };
        }

        setModelMetricsByScope(nextMetrics);
        setCoverage(response.coverage ?? null);
        setMetricsFailed(false);
        setLastSuccessAt(new Date().toISOString());
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
  }, [models, reloadToken]);

  const refresh = useCallback(() => setReloadToken((value) => value + 1), []);

  return {
    coverage,
    lastSuccessAt,
    metricsFailed,
    metricsLoading,
    modelMetricsByScope,
    refresh,
  };
}

function toDerivedMetric(
  block: import("@/lib/types/model-stats").ScopeMetricBlock,
): ModelDerivedMetric {
  return {
    success_rate: block.success_rate,
    request_count_24h: block.request_count,
    p95_latency_ms: block.p95_latency_ms,
    known_cost_micros: block.known_cost_micros,
    caliber: block.caliber,
    samples: block.samples,
  };
}
