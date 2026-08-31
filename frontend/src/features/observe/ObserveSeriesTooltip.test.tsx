import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { UsageSeriesResponse } from "@/lib/api/observability";
import {
  ObserveSeriesTooltip,
} from "./ObserveSeriesTooltip";
import type { ObserveMetric } from "./observeSearch";

type SeriesPoint = UsageSeriesResponse["series"][number]["points"][number];

const RECONCILIATION: SeriesPoint["pricing_reconciliation"] = {
  pricing_eligible_request_count: 0,
  pricing_ineligible_request_count: 2,
  priced_request_count: 0,
  unpriced_request_count: 0,
  pricing_unknown_request_count: 0,
  unpriced_reason_counts: {
    PRICING_DISABLED: 0,
    MISSING_TOKEN_USAGE: 0,
    STREAM_USAGE_UNAVAILABLE: 0,
    MISSING_PRICE_DATA: 0,
  },
  pricing_coverage_state: "no_eligible",
};

function point(overrides: Partial<SeriesPoint> = {}): SeriesPoint {
  return {
    bucket_start: "2026-08-08T00:00:00Z",
    request_count: 2,
    http_success_count: 2,
    http_failed_count: 0,
    failed_count: 0,
    client_disconnected_count: 0,
    ttft_sample_count: 2,
    p50_ttft_ms: 12,
    p95_ttft_ms: 18,
    total_tokens: 100,
    known_cost_micros: null,
    output_rate_sample_count: 1,
    avg_output_rate_tps: 0,
    cache_basis_request_count: 1,
    cache_basis_input_tokens: 100,
    cache_basis_cache_read_tokens: 0,
    cache_basis_cache_creation_tokens: 0,
    pricing_reconciliation: RECONCILIATION,
    ...overrides,
  };
}

function renderTooltip(
  metric: ObserveMetric,
  sourcePoint: SeriesPoint,
  value: number | null,
) {
  const pointIndex = new Map([
    [sourcePoint.bucket_start, new Map([["total", sourcePoint]])],
  ]);
  return render(
    <LocaleProvider>
      <ObserveSeriesTooltip
        active
        formatBucket={(bucket) => bucket}
        label={sourcePoint.bucket_start}
        metric={metric}
        payload={[{ dataKey: "total", name: "Total", value }]}
        pointIndex={pointIndex}
      />
    </LocaleProvider>,
  );
}

describe("ObserveSeriesTooltip", () => {
  it("shows a genuine zero output rate with its partial sample basis", () => {
    renderTooltip("output_rate", point(), 0);
    expect(screen.getByText("0.0 tok/s")).toBeInTheDocument();
    expect(screen.getByText("样本 1 / 请求 2")).toBeInTheDocument();
    expect(screen.getByText(/部分可测/)).toBeInTheDocument();
  });

  it("keeps an unsampled bucket in the tooltip as 无可测速率", () => {
    renderTooltip(
      "output_rate",
      point({ output_rate_sample_count: 0, avg_output_rate_tps: null }),
      null,
    );
    expect(screen.getByText("无可测速率")).toBeInTheDocument();
    expect(screen.getByText("样本 0 / 请求 2")).toBeInTheDocument();
  });

  it("shows cache components, comparable basis, and a genuine 0%", () => {
    renderTooltip("cache_read_share", point(), 0);
    expect(screen.getByText("0.0%")).toBeInTheDocument();
    expect(screen.getByText("缓存读取")).toBeInTheDocument();
    expect(screen.getByText("缓存创建")).toBeInTheDocument();
    expect(screen.getByText("未缓存输入")).toBeInTheDocument();
    expect(screen.getByText("可比 1 / 请求 2")).toBeInTheDocument();
    expect(screen.getByText(/部分覆盖/)).toBeInTheDocument();
  });

  it("distinguishes no-comparable and zero-denominator cache buckets", () => {
    const { unmount } = renderTooltip(
      "cache_read_share",
      point({
        cache_basis_request_count: 0,
        cache_basis_input_tokens: null,
        cache_basis_cache_read_tokens: null,
        cache_basis_cache_creation_tokens: null,
      }),
      null,
    );
    expect(screen.getByText("无可比")).toBeInTheDocument();
    expect(screen.getByText("可比 0 / 请求 2")).toBeInTheDocument();
    unmount();

    renderTooltip(
      "cache_read_share",
      point({
        cache_basis_request_count: 2,
        cache_basis_input_tokens: 0,
        cache_basis_cache_read_tokens: 0,
        cache_basis_cache_creation_tokens: 0,
      }),
      null,
    );
    expect(screen.getByText("零分母")).toBeInTheDocument();
    expect(screen.getByText("可比 2 / 请求 2")).toBeInTheDocument();
  });

  it("preserves units for the original metrics", () => {
    renderTooltip("ttft", point(), 12);
    expect(screen.getByText("12 ms")).toBeInTheDocument();
  });
});
