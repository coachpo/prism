// DESIGN.md《Tables · Header》：sticky 表头要生效，纵横两轴必须落在同一个
// 滚动容器上。Table 原语自带一层 overflow-x-auto，它就是包含块 —— 把高度上限
// 加在外面那层不会让表头黏住，只有加在 data-slot="table-container" 上才行。
// 这三条断言就是在盯这个静默失效：滚过一屏之后表头必须还在容器顶部。
import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-08-09T10:00:00Z";

function attemptRow(index: number) {
  const id = String(1000 + index);
  return {
    request_log_id: id,
    row_kind: "upstream",
    ingress_request_id: `ingress-${id}`,
    attempt_number: 1,
    attempt_trigger: "initial",
    attempt_result: "completed",
    is_winner: true,
    created_at: timestamp,
    ingress_model_id: "gpt-4o-mini",
    model_label: "GPT-4o mini",
    attempt_target_model_id: "gpt-4o-mini",
    attempt_target_model_label: "GPT-4o mini",
    upstream_model_id: "provider/gpt-4o-mini",
    caller_client_display: "Prism QA Browser",
    upstream_client_display: "Prism QA Browser",
    user_agent_overridden: false,
    api_family: "openai",
    endpoint_id: 1,
    endpoint_label: "Primary endpoint",
    terminal_target_id: null,
    terminal_target_label: null,
    terminal_target_configured: false,
    terminal_target_owner_model_id: null,
    ttft_ms: 120,
    completion_duration_ms: 200,
    output_rate_tps: null,
    output_rate_state: "unmeasurable",
    output_rate_reason: "missing_completion_duration",
    upstream_status_code: 200,
    gateway_status_code: null,
    legacy_status_code: null,
    attempt_duration_ms: 125,
    legacy_duration_ms: null,
    is_stream: false,
    stream_outcome: "not_streaming",
    stream_error_kind: null,
    error_source: null,
    error_code: null,
    failure_stage: null,
    failure_detail_preview: null,
    failure_detail_source: "error_detail",
    failure_detail_preview_truncated: false,
    failure_detail_redacted: false,
    reasoning_effort: null,
    output_tokens: 10,
    total_tokens: 20,
    total_cost_user_currency_micros: 3000,
    pricing_status: "priced",
    pricing_resolution_kind: null,
    pricing_evidence_trust: "trusted",
    unpriced_reason: null,
    report_currency_symbol: "$",
    proxy_api_key_id: null,
    proxy_api_key_name_snapshot: null,
    proxy_api_key_attribution_state: "none",
  };
}

function chainItem(index: number) {
  const id = String(1000 + index);
  return {
    ingress_request_id: `ingress-${id}`,
    started_at: timestamp,
    completed_at: timestamp,
    elapsed_ms: 125,
    elapsed_evidence_state: "authoritative",
    finalized_evidence_state: "authoritative",
    finalized_summary: {
      request_log_id: id,
      final_status_code: 200,
      final_result: "completed",
      final_error_code: null,
      ingress_model: { id: "gpt-4o-mini", label: "GPT-4o mini" },
      final_target_model: { id: "gpt-4o-mini", label: "GPT-4o mini" },
      final_upstream_model_id: "provider/gpt-4o-mini",
      terminal_target: null,
      endpoint: { id: 1, label: "Primary endpoint" },
      ttft_ms: 120,
      output_rate_tps: null,
      output_rate_state: "unmeasurable",
      output_rate_reason: "missing_completion_duration",
      total_tokens: 20,
      total_cost_user_currency_micros: 3000,
      report_currency_symbol: "$",
      final_pricing_status: "priced",
      attempt_count: 1,
    },
    expected_attempt_count: 1,
    expected_request_log_row_count: 1,
    retained_upstream_attempt_count: 1,
    retained_request_log_row_count: 1,
    legacy_unknown_row_count: 0,
    chain_complete: true,
    same_target_retry_occurred: false,
    hedge_occurred: false,
    failover_occurred: false,
    routing_evidence_complete: true,
    retained_rows_loaded_count: 1,
    retained_rows_page_complete: true,
    next_row_cursor: null,
    retained_rows: [attemptRow(index)],
  };
}

function auditListItem(index: number) {
  return {
    id: 200 + index,
    request_log_id: "1000",
    request_log_created_at: timestamp,
    ingress_request_id: "ingress-1000",
    request_log_missing: false,
    profile_id: 1,
    model_id: "gpt-4o-mini",
    endpoint_id: 1,
    connection_id: null,
    endpoint_base_url: "https://api.example.test",
    endpoint_description: "Primary endpoint",
    request_method: "POST",
    request_url: `https://api.example.test/v1/responses?audit=${200 + index}`,
    request_headers: "[]",
    row_kind: "upstream",
    attempt_number: 1,
    attempt_duration_ms: 125,
    legacy_duration_ms: null,
    upstream_status_code: 200,
    gateway_status_code: null,
    legacy_status_code: null,
    audit_enabled_at_request: true,
    audit_capture_bodies_at_request: true,
    created_at: timestamp,
  };
}

async function mockManyRows(page: Page) {
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    const { pathname, searchParams } = url;
    if (!pathname.startsWith("/api/")) return route.continue();

    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") {
      return json({ state: "disabled", transition_state: null, login_available: false, effective_generation: "1", retry_after_seconds: null });
    }
    if (pathname === "/api/settings/costing") {
      return json({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/timezone") {
      return json({ timezone_preference: "UTC" });
    }
    if (pathname === "/api/stats/requests") {
      const filterOptions = { ingress_models: [], endpoints: [], clients: [], attempt_target_models: [] };
      if ((searchParams.get("view") || "ingress_chains") === "ingress_chains") {
        const items = Array.from({ length: 40 }, (_, index) => chainItem(index));
        return json({
          query_context: null,
          source_ingress_total: items.length,
          retained_ingress_total: items.length,
          retained_upstream_attempt_total: items.length,
          retained_request_log_row_total: items.length,
          legacy_unknown_row_total: 0,
          page_ingress_count: items.length,
          page_upstream_attempt_count: items.length,
          page_request_log_row_count: items.length,
          filter_options: filterOptions,
          items,
          has_more_chains: false,
          next_chain_cursor: null,
          caliber: {},
          dataset_coverage: {},
          samples: {},
        });
      }
      const items = Array.from({ length: 60 }, (_, index) => attemptRow(index));
      return json({
        items,
        total: items.length,
        limit: 100,
        offset: 0,
        filter_options: filterOptions,
        caliber: {},
        dataset_coverage: {},
        samples: {},
      });
    }
    if (pathname === "/api/stats/requests/1000") {
      return json({
        summary: {
          request_log_id: "1000",
          created_at: timestamp,
          ingress_model_id: "gpt-4o-mini",
          model_label: "GPT-4o mini",
          attempt_target_model_id: "gpt-4o-mini",
          attempt_target_model_label: "GPT-4o mini",
          upstream_model_id: "provider/gpt-4o-mini",
          api_family: "openai",
          row_kind: "upstream",
          upstream_status_code: 200,
          gateway_status_code: null,
          legacy_status_code: null,
          attempt_duration_ms: 125,
          legacy_duration_ms: null,
          is_stream: false,
          stream_outcome: "not_streaming",
          stream_error_kind: null,
          ttft_ms: 120,
          completion_duration_ms: 200,
          output_rate_tps: null,
          output_rate_state: "unmeasurable",
          output_rate_reason: "missing_completion_duration",
          attempt_number: 1,
          attempt_trigger: "initial",
          attempt_result: "completed",
          is_winner: true,
        },
        request: {
          operation_name: "responses",
          upstream_operation_name: null,
          operation_translation_mode: null,
          request_path: "/v1/responses",
          upstream_request_path: null,
          ingress_request_id: "ingress-1000",
          provider_correlation_id: null,
          proxy_api_key_id: null,
          proxy_api_key_name_snapshot: null,
          caller_user_agent: null,
          upstream_user_agent: null,
          caller_client_display: null,
          upstream_client_display: null,
          user_agent_overridden: false,
          request_generation_params: null,
          request_generation_params_status: null,
          metadata_redacted_fields: [],
          metadata_truncated_fields: [],
          url_scrub_provenance: "none",
        },
        routing: {
          profile_id: 1,
          endpoint_label: "Primary endpoint",
          endpoint_id: 1,
          terminal_target_id: null,
          selected_terminal_target_id: null,
          endpoint_base_url: "https://api.example.test",
          endpoint_description: "Primary endpoint",
          audit_enabled_at_request: true,
          audit_capture_bodies_at_request: true,
        },
        tokens: {},
        pricing: {},
      });
    }
    if (pathname === "/api/audit/logs") {
      return json({
        items: Array.from({ length: 25 }, (_, index) => auditListItem(index)),
        next_cursor: null,
        has_more: false,
        window: { from: searchParams.get("from"), to: searchParams.get("to") },
        limit: 50,
        sort: "desc",
      });
    }
    if (pathname.startsWith("/api/audit/logs/")) {
      return json({ detail: "not needed" }, 404);
    }
    return json({}, 404);
  });
}

/** 在给定滚动容器里滚过一屏后，表头必须还贴在容器顶部。 */
async function expectHeaderStaysAtTop(page: Page, containerSelector: string, headerSelector: string) {
  const offsets = await page.evaluate(
    ([container, header]) => {
      const scroller = document.querySelector(container) as HTMLElement | null;
      if (!scroller) return null;
      scroller.scrollTop = scroller.scrollHeight;
      const head = document.querySelector(header) as HTMLElement | null;
      if (!head) return null;
      return {
        scrolled: scroller.scrollTop,
        scrollable: scroller.scrollHeight - scroller.clientHeight,
        delta: head.getBoundingClientRect().top - scroller.getBoundingClientRect().top,
      };
    },
    [containerSelector, headerSelector] as const,
  );
  expect(offsets).not.toBeNull();
  // 先确认这个容器真的能纵向滚动，否则断言等于什么都没测。
  expect(offsets!.scrollable).toBeGreaterThan(0);
  expect(offsets!.scrolled).toBeGreaterThan(0);
  expect(Math.round(offsets!.delta)).toBeGreaterThanOrEqual(0);
  expect(Math.round(offsets!.delta)).toBeLessThanOrEqual(1);
}

test.describe("request-log sticky table headers", () => {
  test("attempts view keeps its column names while the rows scroll", async ({ page }) => {
    await mockManyRows(page);
    await page.goto("/observe/requests?view=attempts");
    await expect(page.getByTestId("request-logs-table")).toBeVisible();
    await expect(page.getByTestId("request-log-row-1000")).toBeVisible();

    await expectHeaderStaysAtTop(
      page,
      '[data-testid="request-logs-scroll"]',
      '[data-testid="request-logs-scroll"] [role="table"] > [role="row"]',
    );
  });

  test("ingress chain view keeps its column names while the rows scroll", async ({ page }) => {
    await mockManyRows(page);
    await page.goto("/observe/requests?view=ingress_chains");
    await expect(page.getByTestId("ingress-chains-table")).toBeVisible();
    await expect(page.getByTestId("chain-summary-ingress-1000")).toBeVisible();

    await expectHeaderStaysAtTop(
      page,
      '[data-testid="ingress-chains-table"] [data-slot="table-container"]',
      '[data-testid="ingress-chains-table"] thead',
    );
  });

  test("audit record list keeps its column names while the rows scroll", async ({ page }) => {
    await mockManyRows(page);
    await page.goto("/observe/requests/1000/audit");
    await expect(page.getByTestId("dedicated-audit-list")).toBeVisible();

    await expectHeaderStaysAtTop(
      page,
      '[data-testid="dedicated-audit-list"] [data-slot="table-container"]',
      '[data-testid="dedicated-audit-list"] thead',
    );
  });
});
