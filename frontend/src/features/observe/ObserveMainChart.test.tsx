/**
 * Main chart series table: every metric draws its own window-total column, not
 * just the request count. Before the fix, `cost`, `tokens` and `errors` left
 * the metric's values out of the table while the chart drew them, so the two
 * views could not be cross-checked.
 */
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { beforeAll, beforeEach, describe, expect, it } from "vitest";

import type { FragmentState } from "@/features/observe/useObserveFragments";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { UsageSeriesResponse } from "@/lib/api/observability";
import { rewriteTestServer } from "@/test/msw/server";
import { ObserveMainChart } from "./ObserveMainChart";
import type { ObserveMetric, ObserveScope } from "./observeSearch";

const BUCKETS = ["2026-08-08T00:00:00Z", "2026-08-08T01:00:00Z"] as const;

const PRICING_RECONCILIATION: UsageSeriesResponse["series"][number]["points"][number]["pricing_reconciliation"] =
  {
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

function point(
  bucketStart: string,
  overrides: Partial<SeriesPoint> = {},
): SeriesPoint {
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
    output_rate_sample_count: 480,
    avg_output_rate_tps: 96.4,
    cache_basis_request_count: 500,
    cache_basis_input_tokens: 12000,
    cache_basis_cache_read_tokens: 3000,
    cache_basis_cache_creation_tokens: 600,
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

function fragmentFor(
  metric: ObserveMetric,
  series: UsageSeriesResponse["series"] = SERIES,
  scope: ObserveScope = "ingress",
): FragmentState<UsageSeriesResponse> {
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
      caliber: { scope },
      dataset_coverage: {},
      samples: {},
      series,
    },
  };
}

async function renderTable(
  metric: ObserveMetric,
  series?: UsageSeriesResponse["series"],
) {
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
  // The switchers are Radix ToggleGroups; single selection exposes each item
  // as a radio inside a labelled group with roving arrow-key navigation.
  await userEvent.click(screen.getByRole("radio", { name: "表" }));
}

function renderChart(
  metric: ObserveMetric,
  series: UsageSeriesResponse["series"],
  scope: ObserveScope = "ingress",
) {
  return render(
    <LocaleProvider>
      <ObserveMainChart
        fragment={fragmentFor(metric, series, scope)}
        metric={metric}
        groupBy="none"
        onMetricChange={() => {}}
        onGroupByChange={() => {}}
        scope={scope}
      />
    </LocaleProvider>,
  );
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
    expect(
      screen.getByRole("columnheader", { name: "窗口合计 · 已知成本" }),
    ).toBeInTheDocument();
    // Two buckets × 10470000 micros, formatted with the active currency.
    expect(screen.getByText("$20.94")).toBeInTheDocument();
  });

  it("shows the window-total token column and its sums", async () => {
    await renderTable("tokens");
    expect(
      screen.getByRole("columnheader", { name: "窗口合计 · 令牌数" }),
    ).toBeInTheDocument();
    expect(screen.getByText("700")).toBeInTheDocument();
  });

  it("shows the window-total error column and its sums", async () => {
    await renderTable("errors");
    expect(
      screen.getByRole("columnheader", { name: "窗口合计 · 错误数" }),
    ).toBeInTheDocument();
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

  // Terminal targets without a pricing template never produce a cost, so a
  // fully unpriced window is the steady state for that deployment rather than
  // an error. Summing it to $0.00 would report an audited zero spend.
  it("leaves a fully unpriced cost window missing instead of claiming zero spend", async () => {
    const unpriced = [
      point(BUCKETS[0], { known_cost_micros: null }),
      point(BUCKETS[1], { known_cost_micros: null }),
    ];
    await renderTable("cost", [{ ...SERIES[0], points: unpriced }]);
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByText("$0.00")).not.toBeInTheDocument();
  });

  it("keeps the last-bucket success/failed breakout for the requests metric", async () => {
    await renderTable("requests");
    // The last-bucket time depends on the run's timezone, so only the metric
    // halves of the headers are asserted.
    expect(
      screen.getByRole("columnheader", { name: /成功$/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("columnheader", { name: /失败$/ }),
    ).toBeInTheDocument();
    expect(screen.getByText("499")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
  });

  it("keeps the last-bucket P50/P95 breakout for the ttft metric", async () => {
    await renderTable("ttft");
    expect(
      screen.getByRole("columnheader", { name: /P50$/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("columnheader", { name: /P95$/ }),
    ).toBeInTheDocument();
    expect(screen.getByText("2,900")).toBeInTheDocument();
    expect(screen.getByText("4,900")).toBeInTheDocument();
  });

  // The last-bucket table must carry the honest states, not just the number:
  // a measured value (including a genuine zero) renders as the value, an
  // unsampled bucket reads 无样本, and an unusable cache basis reads its own
  // state instead of collapsing into a fabricated percentage.
  it("shows the last-bucket output-rate value with its sample count", async () => {
    await renderTable("output_rate");
    expect(
      screen.getByRole("columnheader", { name: /输出速率$/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("columnheader", { name: /样本$/ }),
    ).toBeInTheDocument();
    expect(screen.getByText("96.4 tok/s")).toBeInTheDocument();
    expect(screen.getByText("480 / 500")).toBeInTheDocument();
  });

  it("marks an unsampled last bucket as 无样本, never 0 tok/s", async () => {
    const unsampled = [
      point(BUCKETS[0], {
        output_rate_sample_count: 0,
        avg_output_rate_tps: null,
      }),
      point(BUCKETS[1], {
        output_rate_sample_count: 0,
        avg_output_rate_tps: null,
      }),
    ];
    await renderTable("output_rate", [{ ...SERIES[0], points: unsampled }]);
    expect(
      screen.getByTitle("该时间桶没有可测的输出速率样本。"),
    ).toBeInTheDocument();
    expect(screen.queryByText(/tok\/s/)).not.toBeInTheDocument();
  });

  it("shows the last-bucket cache share with its comparable count", async () => {
    await renderTable("cache_read_share");
    expect(
      screen.getByRole("columnheader", { name: /提示缓存读取占比$/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("columnheader", { name: /可比$/ }),
    ).toBeInTheDocument();
    // 3000 / (12000 + 3000 + 600).
    expect(screen.getByText("19.2%")).toBeInTheDocument();
    expect(screen.getByText("500 / 500")).toBeInTheDocument();
  });

  it("marks a no-comparable last bucket as 无可比 instead of 0%", async () => {
    const mixed = [
      point(BUCKETS[0], {
        cache_basis_request_count: 2,
        cache_basis_input_tokens: 100,
        cache_basis_cache_read_tokens: 0,
        cache_basis_cache_creation_tokens: 0,
      }),
      point(BUCKETS[1], {
        cache_basis_request_count: 0,
        cache_basis_input_tokens: null,
        cache_basis_cache_read_tokens: null,
        cache_basis_cache_creation_tokens: null,
      }),
    ];
    await renderTable("cache_read_share", [{ ...SERIES[0], points: mixed }]);
    // The last bucket has no comparable rows; the earlier measured share must
    // not bleed into it, and no percentage may render for this window.
    expect(
      screen.getByTitle("该时间桶没有可比的缓存分量。"),
    ).toBeInTheDocument();
    expect(screen.queryByText(/%/)).not.toBeInTheDocument();
  });

  it("marks a zero-denominator last bucket as 零分母 instead of 0%", async () => {
    const mixed = [
      point(BUCKETS[0], {
        cache_basis_request_count: 2,
        cache_basis_input_tokens: 100,
        cache_basis_cache_read_tokens: 50,
        cache_basis_cache_creation_tokens: 0,
      }),
      point(BUCKETS[1], {
        cache_basis_request_count: 2,
        cache_basis_input_tokens: 0,
        cache_basis_cache_read_tokens: 0,
        cache_basis_cache_creation_tokens: 0,
      }),
    ];
    await renderTable("cache_read_share", [{ ...SERIES[0], points: mixed }]);
    expect(
      screen.getByTitle("该时间桶的输入与缓存分量合计为零，无法计算占比。"),
    ).toBeInTheDocument();
    expect(screen.queryByText(/%/)).not.toBeInTheDocument();
  });
});

describe("ObserveMainChart honest chart states", () => {
  it.each([
    ["ingress", ["合计", "按入口模型"]],
    ["final_execution", ["合计", "按最终目标模型", "按端点", "按终端目标"]],
    [
      "route_attempt",
      [
        "合计",
        "按尝试目标模型",
        "按尝试触发原因",
        "按尝试结果",
        "按端点",
        "按终端目标",
      ],
    ],
  ] as const)("limits %s grouping controls to that analysis unit", (scope, labels) => {
    const view = renderChart("requests", SERIES, scope);
    expect(
      within(screen.getByRole("group", { name: "分组" }))
        .getAllByRole("radio")
        .map((item) => item.textContent),
    ).toEqual(labels);
    view.unmount();
  });

  it("distinguishes output no-sample from both cache missing-basis states", () => {
    const output = renderChart("output_rate", [
      {
        ...SERIES[0],
        points: SERIES[0].points.map((item) =>
          point(item.bucket_start, {
            output_rate_sample_count: 0,
            avg_output_rate_tps: null,
          }),
        ),
      },
    ]);
    expect(screen.getByTestId("output-rate-empty")).toBeInTheDocument();
    output.unmount();

    const noComparable = renderChart("cache_read_share", [
      {
        ...SERIES[0],
        points: SERIES[0].points.map((item) =>
          point(item.bucket_start, {
            cache_basis_request_count: 0,
            cache_basis_input_tokens: null,
            cache_basis_cache_read_tokens: null,
            cache_basis_cache_creation_tokens: null,
          }),
        ),
      },
    ]);
    expect(screen.getByTestId("cache-read-share-empty")).toBeInTheDocument();
    noComparable.unmount();

    renderChart("cache_read_share", [
      {
        ...SERIES[0],
        points: SERIES[0].points.map((item) =>
          point(item.bucket_start, {
            cache_basis_request_count: item.request_count,
            cache_basis_input_tokens: 0,
            cache_basis_cache_read_tokens: 0,
            cache_basis_cache_creation_tokens: 0,
          }),
        ),
      },
    ]);
    expect(
      screen.getByTestId("cache-read-share-zero-denominator-empty"),
    ).toBeInTheDocument();
    expect(screen.getByText("窗口内缓存占比分母为零")).toBeInTheDocument();
  });

  it("derives partial coverage from window totals, including a zero-coverage bucket", () => {
    const output = renderChart("output_rate", [
      {
        ...SERIES[0],
        points: [
          point(BUCKETS[0], {
            output_rate_sample_count: 500,
            avg_output_rate_tps: 80,
          }),
          point(BUCKETS[1], {
            output_rate_sample_count: 0,
            avg_output_rate_tps: null,
          }),
        ],
      },
    ]);
    expect(screen.getByText("样本 500 / 请求 1,000")).toBeInTheDocument();
    expect(screen.getByText(/部分覆盖/)).toBeInTheDocument();
    output.unmount();

    renderChart("cache_read_share", [
      {
        ...SERIES[0],
        points: [
          point(BUCKETS[0], {
            cache_basis_request_count: 500,
            cache_basis_input_tokens: 100,
            cache_basis_cache_read_tokens: 100,
            cache_basis_cache_creation_tokens: 0,
          }),
          point(BUCKETS[1], {
            cache_basis_request_count: 0,
            cache_basis_input_tokens: null,
            cache_basis_cache_read_tokens: null,
            cache_basis_cache_creation_tokens: null,
          }),
        ],
      },
    ]);
    expect(screen.getByText("可比 500 / 请求 1,000")).toBeInTheDocument();
    expect(screen.getByText(/部分覆盖/)).toBeInTheDocument();
  });
});
