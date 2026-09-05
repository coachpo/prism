import { expect, test } from "@playwright/test";

import { mockPrismRoutes } from "./request-log-dedicated-audit-fixtures";

function createRequestLogDetailFixture() {
  return {
    summary: {
      request_log_id: "101",
      created_at: "2026-08-09T10:00:00Z",
      ingress_model_id: "gpt-4o",
      model_label: "GPT-4o",
      attempt_target_model_id: null,
      attempt_target_model_label: null,
      is_proxy_origin: false,
      api_family: "openai",
      status_code: 200,
      response_time_ms: 300,
      ttft_ms: 120,
      completion_duration_ms: null,
      is_stream: false,
      stream_outcome: "not_streaming",
      stream_error_kind: null,
      stream_error_detail: null,
    },
    request: {
      request_path: "/v1/chat/completions",
      ingress_request_id: "ingress-101",
      attempt_number: 1,
      provider_correlation_id: "corr-101",
      proxy_api_key_id: null,
      proxy_api_key_name_snapshot: null,
      caller_user_agent: "Prism QA Browser",
      upstream_user_agent: "Prism QA Browser",
      caller_client_display: "Prism QA Browser",
      upstream_client_display: "Prism QA Browser",
      user_agent_overridden: false,
      request_generation_params: null,
      request_generation_params_status: null,
      error_detail: null,
    },
    routing: {
      profile_id: 1,
      endpoint_label: "OpenRouter",
      endpoint_id: 7,
      terminal_target_id: null,
      selected_terminal_target_id: null,
      endpoint_base_url: "https://openrouter.test",
      endpoint_description: "OpenRouter",
      audit_enabled_at_request: true,
      audit_capture_bodies_at_request: false,
    },
    usage: {
      input_tokens: 12,
      output_tokens: 100,
      total_tokens: 500,
      success_flag: true,
      billable_flag: true,
      priced_flag: true,
      unpriced_reason: null,
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 0,
      reasoning_tokens: 0,
    },
    costing: {
      input_cost_micros: 1000,
      output_cost_micros: 2000,
      cache_read_input_cost_micros: 0,
      cache_creation_input_cost_micros: 0,
      reasoning_cost_micros: 0,
      total_cost_original_micros: 1200,
      total_cost_user_currency_micros: 1200,
      currency_code_original: "USD",
      report_currency_code: "USD",
      report_currency_symbol: "$",
      fx_rate_used: "1",
      fx_rate_source: "manual",
    },
    pricing: {
      pricing_snapshot_unit: "1M tokens",
      pricing_snapshot_input: "0.10",
      pricing_snapshot_output: "0.20",
      pricing_snapshot_cache_read_input: null,
      pricing_snapshot_cache_creation_input: null,
      pricing_snapshot_reasoning: null,
      pricing_config_version_used: 1,
      pricing_status: "priced",
      pricing_evidence_trust: "trusted",
      pricing_template_id: null,
      reporting_currency_epoch: 1,
      legacy_pricing_evidence: null,
    },
    caliber: {},
    dataset_coverage: {},
    samples: {},
  };
}

test("probe sheet nav", async ({ page }) => {
  const logs: string[] = [];
  page.on("console", (msg) => logs.push(`${msg.type()}: ${msg.text()}`));
  page.on("pageerror", (err) => logs.push(`pageerror: ${err.message}\n${err.stack}`));
  const rows = [
    {
      request_log_id: "101",
      profile_id: 1,
      created_at: "2026-08-09T10:00:00Z",
      ingress_model_id: "gpt-4o",
      model_label: "GPT-4o",
      attempt_target_model_id: "gpt-4o",
      attempt_target_model_label: "GPT-4o",
      api_family: "openai",
      endpoint_id: 7,
      endpoint_label: "OpenRouter",
      status_code: 200,
      response_time_ms: 300,
      ttft_ms: 120,
      is_stream: false,
      stream_outcome: "not_streaming",
      output_tokens: 100,
      total_tokens: 500,
      total_cost_user_currency_micros: 1200,
      priced_flag: true,
      row_kind: "upstream",
      attempt_number: 1,
    },
    {
      request_log_id: "102",
      profile_id: 1,
      created_at: "2026-08-09T10:01:00Z",
      ingress_model_id: "gpt-4o",
      model_label: "GPT-4o",
      attempt_target_model_id: "gpt-4o",
      attempt_target_model_label: "GPT-4o",
      api_family: "openai",
      endpoint_id: 7,
      endpoint_label: "OpenRouter",
      status_code: 200,
      response_time_ms: 320,
      ttft_ms: 130,
      is_stream: false,
      stream_outcome: "not_streaming",
      output_tokens: 90,
      total_tokens: 480,
      total_cost_user_currency_micros: 1100,
      priced_flag: true,
      row_kind: "upstream",
      attempt_number: 1,
    },
  ];
  await mockPrismRoutes(page, "metadata_only");
  await page.route("**/api/stats/requests*", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: rows,
        total: 2,
        limit: 50,
        offset: 0,
        filter_options: { endpoints: [], ingress_models: [], clients: [], attempt_target_models: [] },
        caliber: {},
        dataset_coverage: {},
        samples: {},
      }),
    }),
  );
  await page.route("**/api/stats/requests/101", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ...createRequestLogDetailFixture(), summary: { ...createRequestLogDetailFixture().summary, request_log_id: "101" } }) }),
  );
  await page.route("**/api/stats/requests/102", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ...createRequestLogDetailFixture(), summary: { ...createRequestLogDetailFixture().summary, request_log_id: "102" } }) }),
  );


  await page.goto("/observe/requests?view=attempts");
  await expect(page.getByTestId("request-logs-table")).toBeVisible();
  await page.getByTestId("request-log-row-101").click();
  await page.waitForTimeout(2000);
  console.log("=== CONSOLE ===\n" + logs.join("\n---\n"));
  expect(true).toBe(true);
});
