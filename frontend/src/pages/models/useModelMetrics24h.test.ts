import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ModelConfigListItem } from "@/lib/types";
import { useModelMetrics24h } from "./useModelMetrics24h";
import { isSingleTruncated } from "@/features/models/modelRoutingFlags";

const mocks = vi.hoisted(() => ({ modelMetrics: vi.fn() }));

vi.mock("@/lib/api", () => ({
  api: { stats: { modelMetrics: mocks.modelMetrics } },
}));

const models = [{ id: 7, model_id: "gpt-4o-mini" } as ModelConfigListItem];

const block = (
  scope: "ingress" | "final_execution" | "route_attempt",
  overrides: Record<string, unknown> = {},
) => ({
  request_count: 12,
  success_rate: 99.5,
  p95_latency_ms: 340,
  known_cost_micros: scope === "route_attempt" ? null : 5_000,
  caliber: {
    scope,
    grain: scope,
    identity_basis: scope,
    outcome_basis: scope === "route_attempt" ? "attempt" : "finalized_ingress",
    latency_basis: "attempt_duration",
    cost_basis: scope === "route_attempt" ? "none" : "trusted_served_final",
    datasets: [],
  },
  samples: {
    observation_count: 12,
    latency_sample_count: 12,
    latency_missing_count: 0,
    cost_sample_count: scope === "route_attempt" ? 0 : 12,
    cost_missing_count: scope === "route_attempt" ? 12 : 0,
  },
  ...overrides,
});

const response = () => ({
  items: [{
    model_id: "gpt-4o-mini",
    ingress: block("ingress"),
    final_execution: block("final_execution", { request_count: 9 }),
    route_attempt: block("route_attempt", { request_count: 18 }),
  }],
  coverage: { quality: {}, spending: {} },
});

describe("model telemetry data truth", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("reports a failed read as a failure and keeps the last successful values", async () => {
    mocks.modelMetrics.mockResolvedValueOnce(response());

    const { rerender, result } = renderHook(({ list }) => useModelMetrics24h(list), {
      initialProps: { list: models },
    });

    await waitFor(() => {
      expect(result.current.modelMetricsByScope[7]?.ingress.success_rate).toBe(99.5);
    });
    expect(result.current.metricsFailed).toBe(false);

    // A second read fails: the numbers already on screen must survive, and the
    // failure must be visible as a failure rather than as blanked-out metrics.
    mocks.modelMetrics.mockRejectedValueOnce(new Error("boom"));
    rerender({ list: [...models] });

    await waitFor(() => {
      expect(result.current.metricsFailed).toBe(true);
    });
    expect(result.current.modelMetricsByScope[7]?.ingress.success_rate).toBe(99.5);
    expect(result.current.modelMetricsByScope[7]?.final_execution.request_count_24h).toBe(9);
    expect(result.current.modelMetricsByScope[7]?.route_attempt.known_cost_micros).toBeNull();
  });

  it("reports genuinely absent metrics as null rather than zero", async () => {
    mocks.modelMetrics.mockResolvedValueOnce({ items: [], coverage: { quality: {}, spending: {} } });

    const { result } = renderHook(() => useModelMetrics24h(models));

    await waitFor(() => {
      expect(result.current.metricsLoading).toBe(false);
    });
    expect(result.current.modelMetricsByScope[7]?.ingress).toEqual({
      success_rate: null,
      request_count_24h: null,
      p95_latency_ms: null,
      known_cost_micros: null,
      caliber: null,
      samples: null,
    });
    expect(result.current.metricsFailed).toBe(false);
  });

  it("loads all three scopes in one request and never asks for a scope-specific refetch", async () => {
    mocks.modelMetrics.mockResolvedValueOnce(response());
    const { result } = renderHook(() => useModelMetrics24h(models));
    await waitFor(() => expect(result.current.metricsLoading).toBe(false));
    expect(mocks.modelMetrics).toHaveBeenCalledTimes(1);
    expect(mocks.modelMetrics.mock.calls[0]?.[0]).not.toHaveProperty("scope");
    expect(result.current.modelMetricsByScope[7]?.route_attempt.request_count_24h).toBe(18);
  });
});

describe("single-strategy truncation", () => {
  const model = (strategyType: string, enabledTargets: number) =>
    ({
      loadbalance_strategy: { legacy_strategy_type: strategyType },
      access_targets: Array.from({ length: enabledTargets }, () => ({ is_enabled: true })),
    }) as unknown as Parameters<typeof isSingleTruncated>[0];

  it("flags a single strategy that has more than one enabled target", () => {
    expect(isSingleTruncated(model("single", 2))).toBe(true);
    expect(isSingleTruncated(model("single", 1))).toBe(false);
    expect(isSingleTruncated(model("round-robin", 3))).toBe(false);
  });
});
