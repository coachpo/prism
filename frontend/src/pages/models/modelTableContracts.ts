import type {
  ObservabilityScope,
  ScopeCaliber,
  ScopeMetricSamples,
} from "@/lib/types/model-stats";

export type ModelDerivedMetric = {
  success_rate: number | null;
  request_count_24h: number | null;
  p95_latency_ms: number | null;
  known_cost_micros: number | null;
  caliber: ScopeCaliber | null;
  samples: ScopeMetricSamples | null;
};

export type ModelMetricsByScope = Record<
  ObservabilityScope,
  ModelDerivedMetric
>;
