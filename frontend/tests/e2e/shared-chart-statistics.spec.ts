import { expect, test, type Page } from "@playwright/test";

/**
 * Observe regression journey: three tabs sharing one URL preset, a Now strip
 * with rolling 30m RPM/TPM, a Window KPI grid with pricing coverage, and a
 * single main chart with metric/group switching. All data is mocked through
 * the observe v2 read models; assertions target semantics and ARIA, not chart
 * library internals.
 */

function summaryFixture() {
  const coverage = {
    requested_preset: "24h",
    from_time: "2026-08-08T00:00:00Z",
    to_time: "2026-08-09T00:00:00Z",
    retention_from_time: "2026-07-10T00:00:00Z",
    source: "raw",
    complete: true,
    gaps: [],
    precision: { ttft: "exact", output_rate: "exact" },
  };
  return {
    generated_at: "2026-08-09T00:00:00Z",
    coverage,
    caliber: { scope: "ingress" },
    dataset_coverage: { usage_request_events: coverage },
    samples: {
      observation_count: 1364,
      latency_sample_count: 1302,
      latency_missing_count: 62,
      cost_sample_count: 1300,
      cost_missing_count: 48,
    },
    cost_segments: [
      {
        segment_key: "e.1",
        reporting_currency_epoch: 1,
        currency_attribution: "identified",
        currency_code: "USD",
        display_symbol: "$",
        observed_symbols: ["$"],
        observed_symbol_count: 1,
        observed_symbols_truncated: false,
        request_count: 1364,
        pricing_eligible_request_count: 1348,
        pricing_ineligible_request_count: 16,
        priced_request_count: 1300,
        unpriced_request_count: 48,
        pricing_unknown_request_count: 0,
        unpriced_reason_counts: {
          PRICING_DISABLED: 20,
          MISSING_TOKEN_USAGE: 10,
          STREAM_USAGE_UNAVAILABLE: 12,
          MISSING_PRICE_DATA: 6,
        },
        pricing_coverage_state: "partial",
        known_cost_micros: "10470000",
      },
    ],
    request_count: 1364,
    http_success_count: 1348,
    http_failed_count: 16,
    http_success_rate: 98.83,
    completed_count: 1335,
    stream_error_count: 9,
    client_disconnected_count: 4,
    failed_count: 25,
    ttft_sample_count: 1302,
    p50_ttft_ms: 2900,
    p95_ttft_ms: 4900,
    output_rate_sample_count: 1288,
    avg_output_rate_tps: 83.0,
    total_tokens: 350,
    // Partial cache-basis coverage: 1200 of the 1364 window requests are
    // comparable, so the share renders with a clipped badge.
    cache_basis_request_count: 1200,
    cache_basis_input_tokens: 420000,
    cache_basis_cache_read_tokens: 1280000,
    cache_basis_cache_creation_tokens: 90000,
    pricing_reconciliation: {
      pricing_eligible_request_count: 1348,
      pricing_ineligible_request_count: 16,
      priced_request_count: 1300,
      unpriced_request_count: 48,
      pricing_unknown_request_count: 0,
      unpriced_reason_counts: {
        PRICING_DISABLED: 20,
        MISSING_TOKEN_USAGE: 10,
        STREAM_USAGE_UNAVAILABLE: 12,
        MISSING_PRICE_DATA: 6,
      },
      pricing_coverage_state: "partial",
    },
    window_average_rpm: 0.95,
    window_average_tpm: null,
  };
}

function seriesFixture(
  metric = "requests",
  scope = "ingress",
  groupBy = "none",
) {
  const coverage = {
    requested_preset: "24h",
    from_time: "2026-08-08T00:00:00Z",
    to_time: "2026-08-09T00:00:00Z",
    source: "raw",
    complete: true,
    gaps: [],
  };
  return {
    generated_at: "2026-08-09T00:00:00Z",
    coverage,
    metric,
    group_by: groupBy,
    selection_basis:
      scope === "route_attempt" ? "attempt_count" : "request_count",
    interval: "1h",
    series_limit: 6,
    truncated: false,
    caliber: { scope },
    dataset_coverage:
      scope === "route_attempt"
        ? { request_logs: coverage }
        : { usage_request_events: coverage },
    samples: {
      observation_count: 1364,
      latency_sample_count: 1302,
      latency_missing_count: 62,
      cost_sample_count: 1300,
      cost_missing_count: 48,
    },
    series: [
      {
        key: "total",
        entity_id: null,
        label: "Total",
        configured: null,
        request_count: 1364,
        points: [
          {
            bucket_start: "2026-08-08T00:00:00Z",
            request_count: 1364,
            http_success_count: 1348,
            http_failed_count: 16,
            failed_count: 25,
            client_disconnected_count: 4,
            ttft_sample_count: 1302,
            p50_ttft_ms: 2900,
            p95_ttft_ms: 4900,
            total_tokens: 350,
            known_cost_micros: "10470000",
            pricing_reconciliation: summaryFixture().pricing_reconciliation,
          },
        ],
      },
    ],
  };
}

function nowFixture() {
  return {
    generated_at: "2026-08-09T00:00:00Z",
    health: { stale: false, cache_lag_ms: null },
    rolling: {
      window_minutes: 30,
      coverage: {
        requested_preset: "rolling",
        from_time: "2026-08-09T00:00:00Z",
        to_time: "2026-08-09T00:30:00Z",
        source: "raw",
        complete: true,
        gaps: [],
      },
      request_count: 241,
      token_sample_count: 238,
      token_coverage_complete: false,
      token_count: 94500000,
      rpm: 8.03,
      tpm: 3150000.0,
    },
    enabled_model_count: 12,
  };
}

type ObserveReadLog = {
  queryScopes: string[];
  seriesQueries: Array<{ groupBy: string | null; queryContext: string | null }>;
  errorQueries: Array<{ groupBy: string | null; queryContext: string | null }>;
};

async function mockObserveRoutes(page: Page, reads?: ObserveReadLog) {
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    const { pathname } = url;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });

    if (pathname === "/api/auth/status") {
      return fulfillJson({
        state: "disabled",
        transition_state: null,
        login_available: false,
        effective_generation: "1",
        retry_after_seconds: null,
      });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson({
        report_currency_code: "USD",
        report_currency_symbol: "$",
        endpoint_fx_mappings: [],
        timezone_preference: null,
      });
    }
    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }
    if (pathname === "/api/stats/query-context") {
      const scope = url.searchParams.get("scope") ?? "ingress";
      const coverage = {
        requested_preset: url.searchParams.get("preset") ?? "24h",
        from_time: "2026-08-08T00:00:00Z",
        to_time: "2026-08-09T00:00:00Z",
        source: "raw",
        complete: true,
        gaps: [],
      };
      reads?.queryScopes.push(scope);
      return fulfillJson({
        query_context:
          scope === "ingress" ? "signed-token" : `signed-token-${scope}`,
        scope,
        requested_bounds: {
          from_time: "2026-08-08T00:00:00Z",
          to_time: "2026-08-09T00:00:00Z",
        },
        usage_bounds: {
          from_time: "2026-08-08T00:00:00Z",
          to_time: "2026-08-09T00:00:00Z",
        },
        usage_coverage: coverage,
        request_coverage: coverage,
        event_coverage: coverage,
        event_bounds: {
          from_time: "2026-08-08T00:00:00Z",
          to_time: "2026-08-09T00:00:00Z",
        },
        request_bounds: {
          from_time: "2026-08-08T00:00:00Z",
          to_time: "2026-08-09T00:00:00Z",
        },
        generated_at: "2026-08-09T00:00:00Z",
        caliber: { scope },
      });
    }
    if (pathname === "/api/stats/usage-summary") {
      return fulfillJson(summaryFixture());
    }
    if (pathname === "/api/stats/usage-series") {
      const queryContext = url.searchParams.get("query_context");
      const scope = queryContext?.includes("route_attempt")
        ? "route_attempt"
        : queryContext?.includes("final_execution")
          ? "final_execution"
          : "ingress";
      reads?.seriesQueries.push({
        groupBy: url.searchParams.get("group_by"),
        queryContext,
      });
      return fulfillJson(
        seriesFixture(
          url.searchParams.get("metric") ??
            (scope === "route_attempt" ? "attempts" : "requests"),
          scope,
          url.searchParams.get("group_by") ?? "none",
        ),
      );
    }
    if (pathname === "/api/stats/dashboard/now") {
      return fulfillJson(nowFixture());
    }
    if (pathname === "/api/stats/observe-activity") {
      return fulfillJson({
        generated_at: "2026-08-09T00:00:00Z",
        coverage: {
          requested_preset: "24h",
          from_time: "2026-08-08T00:00:00Z",
          to_time: "2026-08-09T00:00:00Z",
          source: "raw",
          complete: true,
          gaps: [],
        },
        items: [
          {
            usage_event_id: "1",
            final_ingress_request_id: "ingress-1",
            created_at: "2026-08-09T00:00:00Z",
            ingress_model_id: "facade",
            ingress_model_label: "facade",
            final_target_model_id: "actual",
            final_target_model_label: "actual",
            route_changed: true,
            attempt_count: 2,
            routing_evidence_complete: true,
            endpoint_id: 7,
            endpoint_label: "OpenRouter",
            terminal_target_id: 17,
            status_code: 200,
            final_result: "failed",
            outcome_detail: "stream_error",
            is_stream: true,
            stream_outcome: "upstream_read_error",
            stream_error_kind: "upstream_read_failed",
            ttft_ms: 1200,
            total_duration_ms: 7400,
            output_tokens: 372,
            total_tokens: 449881,
            known_cost_micros: "9300",
            final_pricing_status: "priced",
            final_unpriced_reason: null,
            reporting_currency_epoch: 1,
            report_currency_code: "USD",
            report_currency_symbol: "$",
          },
          {
            usage_event_id: "2",
            final_ingress_request_id: "ingress-2",
            created_at: "2026-08-08T23:59:00Z",
            ingress_model_id: "direct",
            ingress_model_label: "direct",
            final_target_model_id: null,
            final_target_model_label: null,
            route_changed: false,
            attempt_count: 1,
            routing_evidence_complete: true,
            endpoint_id: 7,
            endpoint_label: "OpenRouter",
            terminal_target_id: 17,
            status_code: 200,
            final_result: "completed",
            outcome_detail: "completed",
            is_stream: false,
            stream_outcome: "not_streaming",
            stream_error_kind: null,
            ttft_ms: 300,
            total_duration_ms: 1200,
            output_tokens: 100,
            total_tokens: 500,
            known_cost_micros: "1200",
            final_pricing_status: "priced",
            final_unpriced_reason: null,
            reporting_currency_epoch: 1,
            report_currency_code: "USD",
            report_currency_symbol: "$",
          },
        ],
        has_more: false,
      });
    }

    if (pathname === "/api/stats/usage-errors") {
      reads?.errorQueries.push({
        groupBy: url.searchParams.get("group_by"),
        queryContext: url.searchParams.get("query_context"),
      });
      return fulfillJson({
        generated_at: "2026-08-09T00:00:00Z",
        coverage: {
          requested_preset: "24h",
          from_time: "2026-08-08T00:00:00Z",
          to_time: "2026-08-09T00:00:00Z",
          source: "raw",
          complete: true,
          gaps: [],
        },
        requests_context: {
          view: "attempts",
          query_context:
            url.searchParams.get("query_context") ?? "signed-token",
          final_from_time: "2026-08-08T00:00:00Z",
          final_to_time: "2026-08-09T00:00:00Z",
          base_request_filters: {},
        },
        summary: {
          request_count: 25,
          http_error_count: 16,
          stream_error_count: 9,
          failed_count: 25,
          client_disconnected_count: 4,
          diagnostic_stream_anomaly_count: 13,
        },
        timeline: [
          {
            bucket_start: "2026-08-08T00:00:00Z",
            http_error_count: 16,
            stream_error_count: 9,
            failed_count: 25,
            client_disconnected_count: 4,
          },
        ],
        http_statuses: [
          {
            status_code: 503,
            count: 8,
            denominator: 16,
            percentage: 50,
            last_seen_at: "2026-08-09T00:00:00Z",
            request_filters: { final_status_code: ["503"] },
          },
        ],
        stream_outcomes: [
          {
            stream_outcome: "provider_incomplete",
            count: 9,
            denominator: 13,
            percentage: 69.2,
            last_seen_at: "2026-08-09T00:00:00Z",
            request_filters: { final_stream_outcome: ["provider_incomplete"] },
            error_kinds: [
              {
                stream_error_kind: null,
                count: 9,
                denominator: 9,
                percentage: 100,
                request_filters: {
                  final_stream_outcome: ["provider_incomplete"],
                  final_stream_error_kind: ["__null__"],
                },
              },
            ],
            other_error_kinds: {
              count: 0,
              denominator: 9,
              percentage: null,
              request_filters: null,
            },
          },
        ],
        groups: [],
        other: {
          http_statuses: {
            count: 8,
            denominator: 16,
            percentage: 50,
            request_filters: null,
          },
          stream_outcomes: {
            count: 4,
            denominator: 13,
            percentage: 30.8,
            request_filters: null,
          },
          groups: {
            count: 0,
            denominator: 0,
            percentage: null,
            request_filters: null,
          },
        },
      });
    }
    if (pathname === "/api/loadbalance/events/query-context") {
      return fulfillJson({
        query_context: "signed-events-context",
        requested_preset: url.searchParams.get("requested_preset") ?? "24h",
        event_bounds: {
          from_time: "2026-08-08T00:00:00Z",
          to_time: "2026-08-09T00:00:00Z",
        },
        coverage: { complete: true, gaps: [] },
        source_status: {
          delivery: "best_effort",
          transition_ledger_complete: false,
          dropped_event_count: null,
        },
        generated_at: "2026-08-09T00:00:00Z",
      });
    }
    if (pathname === "/api/loadbalance/events") {
      return fulfillJson({
        generated_at: "2026-08-09T00:00:00Z",
        coverage: { complete: true, gaps: [] },
        source_status: {
          delivery: "best_effort",
          transition_ledger_complete: false,
          dropped_event_count: null,
        },
        items: [],
        has_more: false,
        next_cursor: null,
      });
    }

    return route.fulfill({
      status: 404,
      contentType: "application/json",
      body: "{}",
    });
  });
}

test.describe("observe page regression", () => {
  test("renders one view with a content switcher over Now, Window and the main chart", async ({
    page,
  }) => {
    await mockObserveRoutes(page);
    await page.goto("/observe");

    await expect(page.getByTestId("observe-page")).toBeVisible();
    // One view, four content values — the KPI row and the chart render once.
    await expect(page.getByRole("tab", { name: "趋势" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "错误" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "活动" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "终端目标" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "事件" })).toHaveCount(0);

    // Now strip shows rolling 30m RPM as the headline (not window average).
    await expect(page.getByTestId("now-strip")).toContainText("8.03");
    await expect(page.getByTestId("now-strip")).toContainText("3,150,000");

    await expect(page.getByTestId("observe-main-chart")).toBeVisible();
    await expect(page.getByTestId("window-kpi-grid")).toHaveCount(1);
    await expect(page.getByText(/首字耗时 P95/).first()).toBeVisible();
    await expect(page.getByText(/97\.9%/)).toBeVisible();

    // The pricing enum never reaches the screen: four Chinese segments instead.
    await expect(page.getByTestId("pricing-breakdown")).toContainText("已计价");
    await expect(page.getByTestId("pricing-priced")).toContainText("1,300");
    await expect(page.getByTestId("pricing-unpriced")).toContainText("48");
    await expect(page.getByTestId("pricing-ineligible")).toContainText("16");
    await expect(page.getByTestId("pricing-unknown")).toContainText("0");

    // Activity feed: route-changed row + named open action.
    await page.getByRole("tab", { name: "活动" }).click();
    await expect(page.getByTestId("observe-activity-table")).toBeVisible();
    await expect(page.getByTestId("route-changed").first()).toBeVisible();
    await expect(
      page.getByTestId("activity-open-request").first(),
    ).toBeVisible();
  });

  test("switches metric and group on the single main chart via URL state", async ({
    page,
  }) => {
    const reads: ObserveReadLog = {
      queryScopes: [],
      seriesQueries: [],
      errorQueries: [],
    };
    await mockObserveRoutes(page, reads);
    // The legacy `analytics` name still resolves; both old tabs showed the chart.
    await page.goto("/observe?tab=analytics");

    await expect(page.getByTestId("observe-main-chart")).toBeVisible();
    // Error panel renders HTTP/stream rankings under its own switcher value.
    await page.getByRole("tab", { name: "错误" }).click();
    await expect(page.getByTestId("observe-error-panel")).toBeVisible();
    await expect(page.getByTestId("error-status-503")).toContainText(
      "HTTP 503",
    );
    await expect(
      page.getByTestId("error-stream-provider_incomplete"),
    ).toBeVisible();
    // Metric and group selectors are single-select ToggleGroups: each option
    // is a radio inside a named group.
    await page.getByRole("tab", { name: "趋势" }).click();
    await page.getByRole("radio", { name: "首字耗时" }).click();
    await expect(page).toHaveURL(/metric=ttft/);
    await page.getByRole("radio", { name: "按入口模型" }).click();
    await expect(page).toHaveURL(/group_by=ingress_model/);

    await page.getByRole("radio", { name: "最终承载" }).click();
    await expect(page).toHaveURL(/scope=final_execution/);
    await expect(page).toHaveURL(/group_by=none/);
    await page.getByRole("radio", { name: "按最终目标模型" }).click();
    await expect(page).toHaveURL(/group_by=final_target_model/);
    await expect
      .poll(() =>
        reads.seriesQueries.some(
          (query) =>
            query.groupBy === "final_target_model" &&
            query.queryContext === "signed-token-final_execution",
        ),
      )
      .toBe(true);

    await page.getByRole("button", { name: /使用同一口径查看错误/ }).click();
    await expect(page.getByTestId("observe-error-panel")).toBeVisible();
    await expect(page.getByRole("radio", { name: "最终承载" })).toBeChecked();
    // 错误视图不渲染任何分组产物，所以它不再把 group_by 带进读里——悬空的分组
    // 参数会让 URL 声明一个页面上看不见的口径。共享的是 scope 的 query context。
    await expect
      .poll(() =>
        reads.errorQueries.some(
          (query) => query.queryContext === "signed-token-final_execution",
        ),
      )
      .toBe(true);
    expect(
      reads.errorQueries.every(
        (query) => !query.groupBy || query.groupBy === "none",
      ),
    ).toBe(true);
    await page.getByRole("tab", { name: "趋势" }).click();

    // These controls write to the URL, but they are in-page state changes, not
    // navigations. The router's default scroll reset would throw the operator
    // back to the top every time they touch a control below the fold. Bring
    // each control into view first, so Playwright's own scroll-into-view is not
    // what moves the page between the two readings.
    const groupByEndpoint = page.getByRole("radio", { name: "按端点" });
    await groupByEndpoint.scrollIntoViewIfNeeded();
    const offsetBeforeGroupChange = await page.evaluate(() => window.scrollY);
    expect(offsetBeforeGroupChange).toBeGreaterThan(0);
    await groupByEndpoint.click();
    await expect(page).toHaveURL(/group_by=endpoint/);
    expect(await page.evaluate(() => window.scrollY)).toBe(
      offsetBeforeGroupChange,
    );

    // Switching views swaps what renders below the switcher, so the page height
    // moves with it; what has to hold is that the operator keeps their place.
    const activityTab = page.getByRole("tab", { name: "活动" });
    await activityTab.scrollIntoViewIfNeeded();
    const offsetBeforeViewChange = await page.evaluate(() => window.scrollY);
    expect(offsetBeforeViewChange).toBeGreaterThan(0);
    await activityTab.click();
    await expect(page.getByTestId("observe-activity-table")).toBeVisible();
    expect(await page.evaluate(() => window.scrollY)).toBeGreaterThan(0);
  });

  test("the legacy events tab redirects to the routing health page", async ({
    page,
  }) => {
    await mockObserveRoutes(page);
    await page.goto("/observe?tab=events");

    await expect(page).toHaveURL(/\/observe\/routing-health/);
    await expect(page.getByTestId("routing-health-page")).toBeVisible();
    await expect(page.getByText("负载均衡事件", { exact: true })).toBeVisible();
  });

  test("route-attempt trend and errors share context and open attempt logs", async ({
    page,
  }) => {
    const reads: ObserveReadLog = {
      queryScopes: [],
      seriesQueries: [],
      errorQueries: [],
    };
    await mockObserveRoutes(page, reads);
    await page.goto(
      "/observe?scope=route_attempt&group_by=attempt_target_model",
    );

    await expect(page.getByRole("radio", { name: "路由尝试" })).toBeChecked();
    await expect
      .poll(() => reads.queryScopes.includes("route_attempt"))
      .toBe(true);
    await expect(
      page.getByRole("radio", { name: "按尝试目标模型" }),
    ).toBeChecked();
    await expect
      .poll(() =>
        reads.seriesQueries.some(
          (query) =>
            query.groupBy === "attempt_target_model" &&
            query.queryContext === "signed-token-route_attempt",
        ),
      )
      .toBe(true);

    await page.getByRole("tab", { name: "错误" }).click();
    await expect(page.getByTestId("observe-error-panel")).toBeVisible();
    await expect
      .poll(() =>
        reads.errorQueries.some(
          (query) => query.queryContext === "signed-token-route_attempt",
        ),
      )
      .toBe(true);
    await page.getByTestId("error-status-503").click();
    await expect(
      page.getByRole("link", { name: "在请求日志中查看全部" }),
    ).toHaveAttribute("href", /view=attempts/);
  });
});

test("observe JSON export v2 carries coverage, freshness and fragment completeness", async ({
  page,
}) => {
  await mockObserveRoutes(page);
  await page.goto("/observe");
  await expect(page.getByTestId("observe-page")).toBeVisible();
  const downloadPromise = page.waitForEvent("download");
  await page.getByTestId("observe-export-json").click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toMatch(/^observe-24h-requests\.json$/);
  const stream = await download.createReadStream();
  let raw = "";
  for await (const chunk of stream) {
    raw += chunk.toString();
  }
  const payload = JSON.parse(raw);
  expect(payload.schema).toBe("prism.observe.export.v2");
  expect(payload.selection.preset).toBe("24h");
  expect(payload.fragments.query_context.state).toBe("ready");
  expect(payload.fragments.summary.state).toBe("ready");
  expect(payload.fragments.series.state).toBe("ready");
  expect(payload.fragments.summary.dataset_coverage).not.toBeNull();
  expect(payload.fragments.series.dataset_coverage).not.toBeNull();
  expect(payload.fragments.error_breakdown.state).toBe("not_included");
  expect(payload.freshness.exported_at).toBeTruthy();
});
