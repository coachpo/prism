/**
 * Main chart series table: every metric draws its own window-total column, not
 * just the request count. Before the fix, `cost`, `tokens` and `errors` left
 * the metric's values out of the table while the chart drew them, so the two
 * views could not be cross-checked.
 */
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { beforeAll, beforeEach, describe, expect, it } from "vitest";

import type { FragmentState } from "@/features/observe/useObserveFragments";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { UsageSeriesResponse } from "@/lib/api/observability";
import { rewriteTestServer } from "@/test/msw/server";
import { ObserveMainChart } from "./ObserveMainChart";
import type { ObserveMetric } from "./observeSearch";

const BUCKETS = ["2026-08-08T00:00:00Z", "2026-08-08T01:00:00Z"] as const;

const PRICING_RECONCILIATION: UsageSeriesResponse["series"][number]["points"][number]["pricing_reconciliation"] = {
  pricing_eligible_request_count: 1,
  pricing_ineligible_request_count: 0,
  priced_request_count: 1,
  unpriced_request_count: 0,
  pricing_unknown_request_count: 0,
  unpriced_reason_counts: {
    PRICING_DISABLED: 0,
    MISSING_TOKEN_USAGE: 0,
    STREAM_USAGE_UNAVAILABLE: 0,
    MISSING_PRICE_DATA: 0,
  },
  pricing_coverage_state: "complete",
};

type SeriesPoint = UsageSeriesResponse["series"][number]["points"][number];

function point(bucketStart: string, overrides: Partial<SeriesPoint> = {}): SeriesPoint {
  return {
    bucket_start: bucketStart,
    request_count: 500,
    http_success_count: 499,
    http_failed_count: 1,
    failed_count: 1,
    client_disconnected_count: 0,
    ttft_sample_count: 500,
    p50_ttft_ms: 2900,
    p95_ttft_ms: 4900,
    total_tokens: 350,
    known_cost_micros: "10470000",
    pricing_reconciliation: PRICING_RECONCILIATION,
    ...overrides,
  };
}

const SERIES: UsageSeriesResponse["series"] = [
  {
    key: "total",
    entity_id: null,
    label: "Total",
    configured: null,
    request_count: 1000,
    points: [point(BUCKETS[0]), point(BUCKETS[1])],
  },
];

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

function coverage(): UsageSeriesResponse["coverage"] {
  return {
    requested_preset: "1h",
    from_time: "2026-08-08T00:00:00Z",
    to_time: "2026-08-08T02:00:00Z",
    retention_from_time: null,
    source: "raw",
    complete: true,
    gaps: [],
  };
}

function fragmentFor(metric: ObserveMetric, series: UsageSeriesResponse["series"] = SERIES): FragmentState<UsageSeriesResponse> {
  return {
    phase: "ready",
    stale: false,
    error: null,
    retryAfterMs: null,
    data: {
      generated_at: "2026-08-08T02:00:00Z",
      coverage: coverage(),
      metric,
      group_by: "none",
      selection_basis: "request_count",
      interval: "1h",
      series_limit: 6,
      truncated: false,
      series,
    },
  };
}

async function renderTable(metric: ObserveMetric, series?: UsageSeriesResponse["series"]) {
  render(
    <LocaleProvider>
      <ObserveMainChart
        fragment={fragmentFor(metric, series)}
        metric={metric}
        groupBy="none"
        onMetricChange={() => {}}
        onGroupByChange={() => {}}
      />
    </LocaleProvider>,
  );
  await userEvent.click(screen.getByRole("button", { name: "表" }));
}

describe("ObserveMainChart series table", () => {
  // The chart mode mounts recharts' ResponsiveContainer, which drives the
  // plot size from ResizeObserver; jsdom has none, so the tests stub it the
  // same way the request-logs table tests do.
  beforeAll(() => {
    globalThis.ResizeObserver ??= ResizeObserverStub as never;
  });

  beforeEach(() => {
    // The timezone preference read drives /api/settings/costing; the harness
    // fails unhandled requests, so the observe test answers it locally.
    rewriteTestServer.use(
      http.get("/api/settings/costing", () =>
        HttpResponse.json({ timezone_preference: null }),
      ),
    );
  });

  it("shows the window-total cost column and its trusted sums", async () => {
    await renderTable("cost");
    expect(screen.getByRole("columnheader", { name: "窗口合计 · 已知成本" })).toBeInTheDocument();
    // Two buckets × 10470000 micros, formatted with the active currency.
    expect(screen.getByText("$20.94")).toBeInTheDocument();
  });

  it("shows the window-total token column and its sums", async () => {
    await renderTable("tokens");
    expect(screen.getByRole("columnheader", { name: "窗口合计 · 令牌数" })).toBeInTheDocument();
    expect(screen.getByText("700")).toBeInTheDocument();
  });

  it("shows the window-total error column and its sums", async () => {
    await renderTable("errors");
    expect(screen.getByRole("columnheader", { name: "窗口合计 · 错误数" })).toBeInTheDocument();
    // failed_count + client_disconnected_count across both buckets.
    expect(screen.getByText("2")).toBeInTheDocument();
  });

  it("keeps the request-count window total next to the metric column", async () => {
    await renderTable("cost");
    expect(screen.getByText("窗口合计 · 请求数")).toBeInTheDocument();
    expect(screen.getByText("1,000")).toBeInTheDocument();
  });

  it("leaves an all-null token window missing instead of claiming zero", async () => {
    const unmeasured = [
      point(BUCKETS[0], { total_tokens: null }),
      point(BUCKETS[1], { total_tokens: null }),
    ];
    await renderTable("tokens", [{ ...SERIES[0], points: unmeasured }]);
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("keeps the last-bucket success/failed breakout for the requests metric", async () => {
    await renderTable("requests");
    // The last-bucket time depends on the run's timezone, so only the metric
    // halves of the headers are asserted.
    expect(screen.getByRole("columnheader", { name: /成功$/ })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: /失败$/ })).toBeInTheDocument();
    expect(screen.getByText("499")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
  });

  it("keeps the last-bucket P50/P95 breakout for the ttft metric", async () => {
    await renderTable("ttft");
    expect(screen.getByRole("columnheader", { name: /P50$/ })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: /P95$/ })).toBeInTheDocument();
    expect(screen.getByText("2,900")).toBeInTheDocument();
    expect(screen.getByText("4,900")).toBeInTheDocument();
  });
});