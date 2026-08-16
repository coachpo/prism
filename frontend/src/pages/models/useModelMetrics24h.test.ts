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

describe("model telemetry data truth", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("reports a failed read as a failure and keeps the last successful values", async () => {
    mocks.modelMetrics.mockResolvedValueOnce({
      items: [{ model_id: "gpt-4o-mini", success_rate: 99.5, request_count_24h: 12, p95_latency_ms: 340, spend_30d_micros: 5_000 }],
    });

    const { rerender, result } = renderHook(({ list }) => useModelMetrics24h(list), {
      initialProps: { list: models },
    });

    await waitFor(() => {
      expect(result.current.modelMetrics24h[7]?.success_rate).toBe(99.5);
    });
    expect(result.current.metricsFailed).toBe(false);

    // A second read fails: the numbers already on screen must survive, and the
    // failure must be visible as a failure rather than as blanked-out metrics.
    mocks.modelMetrics.mockRejectedValueOnce(new Error("boom"));
    rerender({ list: [...models] });

    await waitFor(() => {
      expect(result.current.metricsFailed).toBe(true);
    });
    expect(result.current.modelMetrics24h[7]?.success_rate).toBe(99.5);
    expect(result.current.modelSpend30dMicros[7]).toBe(5_000);
  });

  it("reports genuinely absent metrics as null rather than zero", async () => {
    mocks.modelMetrics.mockResolvedValueOnce({ items: [] });

    const { result } = renderHook(() => useModelMetrics24h(models));

    await waitFor(() => {
      expect(result.current.metricsLoading).toBe(false);
    });
    expect(result.current.modelMetrics24h[7]).toEqual({
      success_rate: null,
      request_count_24h: null,
      p95_latency_ms: null,
    });
    expect(result.current.modelSpend30dMicros[7]).toBeNull();
    expect(result.current.metricsFailed).toBe(false);
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
