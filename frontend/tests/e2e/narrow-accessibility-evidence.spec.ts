import { expect, test } from "@playwright/test";
import { mockPrismRoutes } from "./request-log-dedicated-audit-fixtures";

/**
 * Narrow viewport + keyboard accessibility evidence (Observe SPEC OB-34..41):
 * 390×844 narrow layout renders without horizontal scroll, keyboard
 * navigation reaches every tab, and reduced-motion is honored. These run
 * against the seeded local stack (same data as the 1200/1680 evidence).
 */

const NARROW_VIEWPORT = { width: 390, height: 844 };

async function installObserveRoutes(page: import("@playwright/test").Page) {
  await mockPrismRoutes(page, "full");
  const fulfill = async (
    route: import("@playwright/test").Route,
    body: unknown,
  ) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(body),
    });
  };
  await page.route("**/api/stats/query-context**", (route) => {
    const scope = new URL(route.request().url()).searchParams.get("scope") ?? "ingress";
    const coverage = {
      requested_preset: "24h",
      from_time: "2026-08-08T00:00:00Z",
      to_time: "2026-08-09T00:00:00Z",
      source: "raw",
      complete: true,
      gaps: [],
    };
    return fulfill(route, {
      query_context: "narrow-observe-context",
      scope,
      caliber: { scope },
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
    });
  });
  await page.route("**/api/stats/usage-summary**", (route) =>
    fulfill(route, {
      generated_at: "2026-08-09T00:00:00Z",
      caliber: { scope: "ingress" },
      dataset_coverage: {},
      samples: { observation_count: 0, latency_sample_count: 0, latency_missing_count: 0, cost_sample_count: 0, cost_missing_count: 0 },
      coverage: {
        requested_preset: "24h",
        from_time: "2026-08-08T00:00:00Z",
        to_time: "2026-08-09T00:00:00Z",
        source: "raw",
        complete: true,
        gaps: [],
      },
      cost_segments: [],
      request_count: 0,
      http_success_count: 0,
      http_failed_count: 0,
      http_success_rate: null,
      completed_count: 0,
      stream_error_count: 0,
      client_disconnected_count: 0,
      failed_count: 0,
      ttft_sample_count: 0,
      p50_ttft_ms: null,
      p95_ttft_ms: null,
      output_rate_sample_count: 0,
      avg_output_rate_tps: null,
      total_tokens: null,
      cache_basis_request_count: 0,
      cache_basis_input_tokens: null,
      cache_basis_cache_read_tokens: null,
      cache_basis_cache_creation_tokens: null,
      pricing_reconciliation: {
        pricing_eligible_request_count: 0,
        pricing_ineligible_request_count: 0,
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
      },
      window_average_rpm: null,
      window_average_tpm: null,
    }),
  );
  await page.route("**/api/stats/usage-series**", (route) => {
    const url = new URL(route.request().url());
    return fulfill(route, {
      generated_at: "2026-08-09T00:00:00Z",
      caliber: { scope: "ingress" },
      dataset_coverage: {},
      samples: { observation_count: 3, latency_sample_count: 2, latency_missing_count: 1, cost_sample_count: 0, cost_missing_count: 3 },
      coverage: {
        requested_preset: "24h",
        from_time: "2026-08-08T00:00:00Z",
        to_time: "2026-08-09T00:00:00Z",
        source: "raw",
        complete: true,
        gaps: [],
      },
      metric: url.searchParams.get("metric") ?? "requests",
      group_by: url.searchParams.get("group_by") ?? "none",
      selection_basis: "request_count",
      interval: "1h",
      series_limit: 6,
      truncated: false,
      series: [
        {
          key: "total",
          entity_id: null,
          label: "Total",
          configured: null,
          request_count: 3,
          points: [
            {
              bucket_start: "2026-08-08T01:00:00Z",
              request_count: 3,
              http_success_count: 2,
              http_failed_count: 1,
              failed_count: 1,
              client_disconnected_count: 0,
              ttft_sample_count: 2,
              p50_ttft_ms: 900,
              p95_ttft_ms: 1500,
              total_tokens: 700,
              known_cost_micros: null,
              output_rate_sample_count: 2,
              avg_output_rate_tps: 84.5,
              cache_basis_request_count: 1,
              cache_basis_input_tokens: 400,
              cache_basis_cache_read_tokens: 100,
              cache_basis_cache_creation_tokens: 0,
              pricing_reconciliation: {
                pricing_eligible_request_count: 3,
                pricing_ineligible_request_count: 0,
                priced_request_count: 0,
                unpriced_request_count: 0,
                pricing_unknown_request_count: 3,
                unpriced_reason_counts: {
                  PRICING_DISABLED: 0,
                  MISSING_TOKEN_USAGE: 0,
                  STREAM_USAGE_UNAVAILABLE: 0,
                  MISSING_PRICE_DATA: 0,
                },
                pricing_coverage_state: "no_trusted_cost",
              },
            },
          ],
        },
      ],
    });
  });
  await page.route("**/api/stats/dashboard/now**", (route) =>
    fulfill(route, {
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
        request_count: 0,
        token_sample_count: 0,
        token_coverage_complete: true,
        token_count: null,
        rpm: null,
        tpm: null,
      },
      enabled_model_count: 0,
    }),
  );
  await page.route("**/api/stats/observe-activity**", (route) =>
    fulfill(route, {
      generated_at: "2026-08-09T00:00:00Z",
      coverage: {
        requested_preset: "24h",
        from_time: "2026-08-08T00:00:00Z",
        to_time: "2026-08-09T00:00:00Z",
        source: "raw",
        complete: true,
        gaps: [],
      },
      items: [],
      has_more: false,
    }),
  );
  await page.route("**/api/stats/usage-errors**", (route) =>
    fulfill(route, {
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
        view: "ingress_chains",
        query_context: "narrow-observe-context",
        final_from_time: "2026-08-08T00:00:00Z",
        final_to_time: "2026-08-09T00:00:00Z",
        base_request_filters: {},
      },
      summary: {
        request_count: 0,
        http_error_count: 0,
        stream_error_count: 0,
        failed_count: 0,
        client_disconnected_count: 0,
        diagnostic_stream_anomaly_count: 0,
      },
      timeline: [],
      http_statuses: [],
      stream_outcomes: [],
      groups: [],
      other: {
        http_statuses: {
          count: 0,
          denominator: 0,
          percentage: null,
          request_filters: null,
        },
        stream_outcomes: {
          count: 0,
          denominator: 0,
          percentage: null,
          request_filters: null,
        },
        groups: {
          count: 0,
          denominator: 0,
          percentage: null,
          request_filters: null,
        },
      },
    }),
  );
  await page.route("**/api/loadbalance/current-state**", (route) =>
    fulfill(route, {
      generated_at: "2026-08-09T00:00:00Z",
      scope: "process",
      instance_id: "narrow",
      configuration_revision: "1",
      completeness: {
        state: "no_config",
        complete: true,
        configured_target_count: 0,
        observed_target_count: 0,
        unobserved_target_count: 0,
        observed_subset_counts: null,
      },
      items: [],
      has_more: false,
      next_cursor: null,
    }),
  );
  await page.route("**/api/loadbalance/events/query-context**", (route) =>
    fulfill(route, {
      query_context: "narrow-events-context",
      requested_preset: "24h",
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
    }),
  );
  await page.route("**/api/loadbalance/events**", (route) =>
    fulfill(route, {
      items: [],
      has_more: false,
      next_cursor: null,
      coverage: { complete: true, gaps: [] },
    }),
  );
}

test.beforeEach(async ({ page }) => {
  await installObserveRoutes(page);
});

test("narrow 390x844 observe page has no horizontal overflow and all tabs reachable by keyboard", async ({
  page,
}) => {
  await page.setViewportSize(NARROW_VIEWPORT);
  await page.goto("/observe");
  await expect(page.getByTestId("observe-page")).toBeVisible();

  const noHorizontalOverflow = await page.evaluate(() => {
    const root = document.documentElement;
    return root.scrollWidth <= root.clientWidth + 1;
  });
  expect(noHorizontalOverflow).toBe(true);

  const analysisScopes = page
    .getByRole("group", { name: "分析单位" })
    .getByRole("radio");
  await expect(analysisScopes).toHaveCount(3);
  await analysisScopes.nth(0).focus();
  await page.keyboard.press("ArrowRight");
  await expect(analysisScopes.nth(1)).toBeFocused();
  await page.keyboard.press("Space");
  await expect(analysisScopes.nth(1)).toBeChecked();
  await expect(page).toHaveURL(/scope=final_execution/);

  // Keyboard: every tab trigger is focusable and arrow-key navigable.
  const tabs = page.getByRole("tab");
  const count = await tabs.count();
  expect(count).toBeGreaterThanOrEqual(3);
  await tabs.nth(0).focus();
  await expect(tabs.nth(0)).toBeFocused();
  for (let index = 1; index < count; index++) {
    await page.keyboard.press("ArrowRight");
    await expect(tabs.nth(index)).toBeFocused();
  }

  const terminalScopes = page
    .getByRole("tablist", { name: "终端目标统计口径" })
    .getByRole("tab");
  await expect(terminalScopes).toHaveCount(2);
  await terminalScopes.nth(0).focus();
  await page.keyboard.press("ArrowRight");
  await expect(terminalScopes.nth(1)).toBeFocused();
  await expect(terminalScopes.nth(1)).toHaveAttribute(
    "data-state",
    "active",
  );
});

/**
 * Seven-metric main chart (output rate + cache read share): the metric
 * switcher keeps the fixed order, stays inside a labelled group that arrow keys
 * traverse, deep links accept both new metric values, and the chart/table
 * toggle is keyboard reachable — all at 390×844 without horizontal overflow.
 */
const SEVEN_METRICS = [
  "请求数",
  "错误",
  "TTFT",
  "输出速率",
  "令牌",
  "缓存读取",
  "花费",
];

test("narrow 390x844 observe main chart exposes seven keyboard-operable metrics", async ({
  page,
}) => {
  await page.setViewportSize(NARROW_VIEWPORT);
  await page.goto("/observe?tab=trend");
  const chart = page.getByTestId("observe-main-chart");
  await expect(chart).toBeVisible();

  const noHorizontalOverflow = await page.evaluate(() => {
    const root = document.documentElement;
    return root.scrollWidth <= root.clientWidth + 1;
  });
  expect(noHorizontalOverflow).toBe(true);

  // Fixed UI order: requests, errors, TTFT, output rate, tokens, cache read, cost.
  const metricGroup = chart.getByRole("group", { name: "指标" });
  const metrics = metricGroup.getByRole("radio");
  await expect(metrics).toHaveCount(7);
  const labels = await metrics.allInnerTexts();
  expect(labels).toEqual(SEVEN_METRICS);
  await expect(metrics.nth(0)).toBeChecked();

  // Roving tabindex moves focus with the arrow keys; selection follows with
  // Space, so the whole switcher is reachable without a pointer.
  await metrics.nth(0).focus();
  await expect(metrics.nth(0)).toBeFocused();
  await page.keyboard.press("ArrowRight");
  await expect(metrics.nth(1)).toBeFocused();
  await page.keyboard.press("Space");
  await expect(metrics.nth(1)).toBeChecked();
  await expect(metrics.nth(0)).not.toBeChecked();

  // The chart/table switch is its own keyboard-operable group.
  const viewSwitcher = chart.getByRole("group", { name: "图表或数据表" });
  await viewSwitcher.getByRole("radio", { name: "表" }).focus();
  await page.keyboard.press("Enter");
  await expect(chart.getByRole("table")).toBeVisible();
});

for (const deepLinkedMetric of ["output_rate", "cache_read_share"]) {
  test(`deep link /observe?tab=trend&metric=${deepLinkedMetric} selects the matching metric`, async ({
    page,
  }) => {
    await page.setViewportSize(NARROW_VIEWPORT);
    await page.goto(`/observe?tab=trend&metric=${deepLinkedMetric}`);
    const chart = page.getByTestId("observe-main-chart");
    await expect(chart).toBeVisible();
    const expected =
      deepLinkedMetric === "output_rate" ? "输出速率" : "缓存读取";
    await expect(chart.getByRole("radio", { name: expected })).toBeChecked();
  });
}

test("narrow 390x844 request logs table keeps identity column reachable without horizontal page scroll", async ({
  page,
}) => {
  await page.setViewportSize(NARROW_VIEWPORT);
  await page.goto("/observe/requests?view=attempts");
  const table = page.getByTestId("request-logs-table");
  await expect(table).toBeVisible();
  const noPageOverflow = await page.evaluate(() => {
    const root = document.documentElement;
    return root.scrollWidth <= root.clientWidth + 1;
  });
  expect(noPageOverflow).toBe(true);
  // Identity column header remains visible in the scroll container.
  await expect(page.getByText("状态", { exact: true }).first()).toBeVisible();
});

test("narrow ingress attempt chain scrolls inside its inset without widening the page", async ({
  page,
}) => {
  await page.setViewportSize(NARROW_VIEWPORT);
  await page.goto("/observe/requests?view=ingress_chains");
  const summary = page.getByTestId("chain-summary-ingress-101");
  await expect(summary).toBeVisible();
  await summary.getByRole("button", { expanded: false }).click();

  const insetScroller = page
    .getByTestId("chain-ingress-101")
    .locator("div.overflow-x-auto")
    .first();
  await expect(insetScroller).toBeVisible();
  expect(
    await insetScroller.evaluate(
      (element) => element.scrollWidth > element.clientWidth,
    ),
  ).toBe(true);
  expect(
    await page.evaluate(
      () =>
        document.documentElement.scrollWidth <=
        document.documentElement.clientWidth + 1,
    ),
  ).toBe(true);
});

test("reduced motion preference does not break observe page rendering", async ({
  page,
}) => {
  await page.setViewportSize(NARROW_VIEWPORT);
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/observe");
  await expect(page.getByTestId("observe-page")).toBeVisible();
  const reduced = await page.evaluate(
    () => matchMedia("(prefers-reduced-motion: reduce)").matches,
  );
  expect(reduced).toBe(true);
  await page.screenshot({
    path: "artifacts/evidence/observe-390-reduced-motion.png",
    fullPage: true,
  });
});

test("narrow viewport visual evidence captures", async ({ page }) => {
  await page.setViewportSize(NARROW_VIEWPORT);
  await page.goto("/observe");
  await expect(page.getByTestId("observe-page")).toBeVisible();
  await page.screenshot({
    path: "artifacts/evidence/observe-390-overview.png",
    fullPage: true,
  });

  // Routing health is its own page now, not the dashboard's third tab.
  await page.goto("/observe/routing-health");
  await expect(page.getByTestId("routing-health-page")).toBeVisible();
  await page.screenshot({
    path: "artifacts/evidence/observe-390-events.png",
    fullPage: true,
  });

  await page.goto("/observe/requests?view=attempts");
  await expect(page.getByTestId("request-logs-table")).toBeVisible();
  await page.screenshot({
    path: "artifacts/evidence/requests-390-list.png",
    fullPage: true,
  });
});

test("connection dialog visual evidence at 1440x900 and 390x844", async ({
  page,
}) => {
  const timestamp = "2026-08-08T12:00:00Z";
  const strategy = {
    id: 11,
    profile_id: 1,
    name: "Default fill-first routing",
    legacy_strategy_type: "fill-first",
    failure_status_codes: [429, 500],
    ban_mode: "off",
    retry_base_delay_ms: 1000,
    retry_backoff_multiplier: 2,
    retry_jitter_ratio: 0.2,
    retry_max_delay_ms: 8000,
    cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 0,
    ban_duration_seconds: 0,
    attached_model_count: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
  const endpoint = {
    id: 1,
    profile_id: 1,
    name: "OpenRouter",
    base_url: "https://openrouter.example.test/v1",
    has_api_key: true,
    masked_api_key: "sk-…abcd",
    position: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
  const connection = {
    id: 1,
    profile_id: 1,
    model_config_id: 5,
    api_family: "openai",
    endpoint_id: 1,
    endpoint,
    is_active: true,
    priority: 0,
    name: "OpenRouter Primary",
    auth_type: "openai",
    custom_headers: null,
    custom_request_parameters: null,
    openai_text_capability: "dual_native",
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    created_at: timestamp,
    updated_at: timestamp,
  };
  const modelDetail = {
    id: 5,
    profile_id: 1,
    api_family: "openai",
    model_id: "router-model",
    display_name: "Router Model",
    openai_accepted_format: "dual_native",
    loadbalance_strategy_id: 11,
    loadbalance_strategy: strategy,
    access_targets: [
      {
        id: 101,
        target_type: "connection",
        target_model_id: null,
        connection_id: connection.id,
        terminal_target_id: connection.id,
        position: 0,
        is_enabled: true,
        target_model: null,
        connection,
        terminal_target: connection,
        created_at: timestamp,
        updated_at: timestamp,
      },
    ],
    is_enabled: true,
    created_at: timestamp,
    updated_at: timestamp,
  };
  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }
    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    if (pathname === "/api/auth/status")
      return fulfillJson({
        state: "disabled",
        transition_state: null,
        login_available: false,
        effective_generation: "1",
        retry_after_seconds: null,
      });
    if (pathname === "/api/settings/costing")
      return fulfillJson({
        report_currency_code: "USD",
        report_currency_symbol: "$",
        endpoint_fx_mappings: [],
        timezone_preference: null,
      });
    if (pathname === "/api/settings/timezone")
      return fulfillJson({ timezone_preference: "UTC" });
    if (pathname === "/api/pricing-templates") return fulfillJson([]);
    if (pathname === "/api/loadbalance/strategies")
      return fulfillJson([strategy]);
    if (pathname === "/api/endpoints") return fulfillJson([endpoint]);
    if (pathname === "/api/endpoints/connections")
      return fulfillJson({
        items: [{ id: 1, endpoint_id: 1, name: "OpenRouter" }],
      });
    if (pathname === "/api/models/5/connections")
      return fulfillJson([connection]);
    if (pathname === "/api/models/5/targets")
      return fulfillJson(modelDetail.access_targets);
    if (pathname === "/api/models/5" && request.method() === "GET")
      return fulfillJson(modelDetail);
    if (pathname === "/api/models" && request.method() === "GET")
      return fulfillJson([modelDetail]);
    if (pathname.startsWith("/api/models/5/routing-diagnostics"))
      return fulfillJson({
        model_config_id: 5,
        openai_accepted_format: "dual_native",
        strategy: { id: 11, type: "fill-first" },
        accepted_operations: ["openai.chat_completions"],
        targets: [],
        operation_routes: [],
        configuration_warnings: [],
      });
    return fulfillJson({});
  });

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/models/5");
  await expect(
    page.getByRole("heading", { name: "Router Model" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "编辑 OpenRouter Primary" }).click();
  const dialog = page.getByRole("dialog", { name: /编辑终端目标/ });
  await expect(dialog).toBeVisible();
  await page.screenshot({
    path: "artifacts/evidence/connection-dialog-1440.png",
  });

  await page.setViewportSize({ width: 390, height: 844 });
  await expect(dialog).toBeVisible();
  const noHorizontalOverflow = await page.evaluate(() => {
    const root = document.documentElement;
    return root.scrollWidth <= root.clientWidth + 1;
  });
  expect(noHorizontalOverflow).toBe(true);
  await page.screenshot({
    path: "artifacts/evidence/connection-dialog-390.png",
  });
});
