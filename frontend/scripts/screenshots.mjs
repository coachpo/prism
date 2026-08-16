// Visual evidence capture for Requests/Audit v2 (task-7). Mocks the v2 API
// shapes and captures the Requests table, chain rows, failure detail sheet,
// pricing projections, and Audit page at 1680x1050, 1200x800+sidebar, narrow.
import { chromium } from "@playwright/test";
import path from "node:path";
import fs from "node:fs";

const OUT = process.argv[2] || "artifacts/evidence/2026-08-09-requests-logs/frontend/screenshots";
fs.mkdirSync(OUT, { recursive: true });

const PORT = 15299;
const BASE = `http://127.0.0.1:${PORT}`;

const now = new Date("2026-08-09T10:00:00Z").toISOString();

function row(id, overrides = {}) {
  return {
    request_log_id: String(id),
    row_kind: "upstream",
    ingress_request_id: `018f9${id}7a-${id}000-4000-8000-${String(id).padStart(12, "0")}`,
    attempt_number: 1,
    attempt_trigger: "initial",
    attempt_result: null,
    is_winner: null,
    created_at: now,
    model_id: "gpt-5.6",
    resolved_target_model_id: "gpt-5.6",
    endpoint_id: 7,
    terminal_target_id: 23,
    terminal_target_label: "OpenCode Go / GPT-5.6",
    terminal_target_configured: true,
    terminal_target_owner_model_id: "gpt-5.6",
    total_tokens: 1840,
    total_cost_user_currency_micros: "23100",
    pricing_status: "priced",
    unpriced_reason: null,
    pricing_evidence_trust: "trusted",
    stream_outcome: "not_streaming",
    stream_error_kind: null,
    error_source: null,
    error_code: null,
    failure_stage: null,
    failure_detail_preview: null,
    failure_detail_source: "error_detail",
    failure_detail_preview_truncated: false,
    failure_detail_redacted: false,
    failure_detail_persistence_truncated: false,
    ...overrides,
  };
}

function failedRow(id, detail) {
  return row(id, {
    attempt_result: "http_error",
    upstream_status_code: 429,
    gateway_status_code: null,
    legacy_status_code: null,
    error_source: "upstream",
    error_code: "rate_limit_exceeded",
    failure_stage: "upstream_response",
    failure_detail_preview: detail.slice(0, 240),
    failure_detail_preview_truncated: detail.length > 240,
    total_cost_user_currency_micros: null,
    pricing_status: "ineligible",
  });
}

function chainItem(ingressId, rows) {
  const first = rows[0];
  return {
    ingress_request_id: ingressId,
    started_at: now,
    completed_at: now,
    elapsed_ms: 12400,
    elapsed_evidence_state: "authoritative",
    finalized_evidence_state: "authoritative",
    finalized_summary: {
      request_log_id: first.request_log_id,
      final_status_code: rows.some((r) => r.attempt_result === "http_error") ? 429 : 200,
      final_result: rows.some((r) => r.attempt_result === "http_error") ? "failed" : "completed",
      final_error_code: rows.some((r) => r.attempt_result === "http_error") ? "rate_limit_exceeded" : null,
      requested_model: { id: "agent", label: "Agent" },
      resolved_model: { id: "gpt-5.6", label: "GPT-5.6" },
      terminal_target: { id: 23, label: "OpenCode Go / GPT-5.6", configured: true, owner_model_id: "gpt-5.6" },
      endpoint: { id: 7, label: "OpenCode Go" },
      ttft_ms: 320,
      output_rate_tps: 48.2,
      total_tokens: 1840,
      total_cost_user_currency_micros: "23100",
      report_currency_code: "USD",
      report_currency_symbol: "$",
      reporting_currency_epoch: 3,
      currency_attribution: "identified",
      cost_segment_key: "e.3",
      final_pricing_status: "priced",
      final_unpriced_reason: null,
      final_pricing_resolution_kind: null,
      missing_price_components: null,
      final_pricing_evidence_trust: "trusted",
      pricing_template_id_used: 41,
      pricing_template_name_snapshot: "GPT-5.6 Standard",
      pricing_template_revision_id_used: "901",
      pricing_config_version_used: 4,
      pricing_version_effective_at: "2026-08-01T12:00:00Z",
      pricing_snapshot_unit: "PER_1M",
      pricing_snapshot_input: "2.5",
      pricing_snapshot_output: "10",
      pricing_snapshot_cache_read_input: null,
      pricing_snapshot_cache_creation_input: "0",
      pricing_snapshot_reasoning: "3",
      attempt_count: rows.length,
      final_attempt_number: rows.length,
      final_attempt_trigger: "initial",
      final_target_entry_trigger: "initial",
    },
    expected_attempt_count: rows.length,
    expected_request_log_row_count: rows.length,
    retained_upstream_attempt_count: rows.filter((r) => r.row_kind === "upstream").length,
    retained_request_log_row_count: rows.length,
    legacy_unknown_row_count: 0,
    chain_complete: true,
    same_target_retry_occurred: false,
    hedge_occurred: false,
    failover_occurred: rows.some((r) => r.attempt_trigger === "failover"),
    routing_evidence_complete: true,
    retained_rows_loaded_count: rows.length,
    retained_rows_page_complete: true,
    next_row_cursor: null,
    retained_rows: rows,
  };
}

const detail429 = {
  summary: {
    request_log_id: "1327",
    created_at: now,
    model_id: "gpt-5.6",
    model_label: "GPT-5.6",
    resolved_target_model_id: "gpt-5.6",
    resolved_target_model_label: "GPT-5.6",
    api_family: "openai",
    row_kind: "upstream",
    upstream_status_code: 429,
    gateway_status_code: null,
    legacy_status_code: null,
    attempt_duration_ms: 842,
    legacy_duration_ms: null,
    ttft_ms: null,
    completion_duration_ms: null,
    is_stream: false,
    stream_outcome: "not_streaming",
    stream_error_kind: null,
    attempt_number: 1,
    attempt_trigger: "initial",
    attempt_result: "http_error",
    is_winner: true,
  },
  request: {
    operation_name: "openai.chat_completions",
    upstream_operation_name: "openai.chat_completions",
    operation_translation_mode: null,
    request_path: "/v1/chat/completions",
    upstream_request_path: "/v1/chat/completions",
    ingress_request_id: "018f91327a-1000-4000-8000-000000001327",
    provider_correlation_id: "req_9f8d7c6b",
    proxy_api_key_id: 5,
    proxy_api_key_name_snapshot: "production-key",
    caller_user_agent: "codex/1.0",
    upstream_user_agent: "openai-python/1.0",
    caller_client_display: "Codex",
    upstream_client_display: "OpenAI SDK",
    user_agent_overridden: true,
    request_generation_params: { provider: "openai", temperature: 0.7, max_output_tokens: 2048, reasoning: { effort: "low", source_field: "reasoning_effort" } },
    request_generation_params_status: "complete",
    metadata_redacted_fields: ["authorization"],
    metadata_truncated_fields: [],
    url_scrub_provenance: "runtime_scrubbed",
  },
  routing: {
    profile_id: 1,
    endpoint_label: "OpenCode Go",
    endpoint_id: 7,
    terminal_target_id: 23,
    selected_terminal_target_id: 23,
    endpoint_base_url: "https://api.openai.com",
    endpoint_description: "Production OpenAI endpoint",
    audit_enabled_at_request: true,
    audit_capture_bodies_at_request: true,
  },
  usage: {
    input_tokens: 42,
    output_tokens: null,
    total_tokens: null,
    success_flag: false,
    cache_read_input_tokens: 0,
    cache_creation_input_tokens: 0,
    reasoning_tokens: 5,
  },
  failure: {
    category: "upstream_http",
    source: "upstream",
    stage: "upstream_response",
    code: "rate_limit_exceeded",
    detail: "{\"error\":{\"message\":\"Rate limit reached for gpt-5.6 in organization org-123 on requests per day. Limit: 10000 / day.\",\"type\":\"rate_limit_exceeded\",\"param\":null,\"code\":\"rate_limit_exceeded\"}}",
    detail_redacted: false,
    detail_truncated: false,
    detail_source: "error_detail",
    evidence_state: "authoritative",
    upstream_request_started: true,
    response_headers_received: true,
    first_body_or_stream_event_seen: true,
    stream_outcome: "not_streaming",
    stream_error_kind: null,
    stream_error_detail: null,
  },
  terminal_target: {
    kind: "terminal_target",
    terminal_target_id: "23",
    owner_model_config_id: "42",
    name: "OpenCode Go / GPT-5.6",
    name_source: "current",
    deleted: false,
    configured: true,
  },
  endpoint: { kind: "endpoint", id: "7", name: "OpenCode Go", name_source: "current", deleted: false, configured: true },
  routing_provenance: {
    initial_terminal_target: {
      kind: "terminal_target",
      terminal_target_id: "19",
      owner_model_config_id: "42",
      name: "Initial Target #19",
      name_source: "snapshot",
      deleted: null,
      configured: null,
    },
    differs_from_actual: true,
  },
  pricing: {
    pricing_status: "ineligible",
    unpriced_reason: null,
    pricing_resolution_kind: null,
    missing_price_components: null,
    pricing_evidence_trust: "trusted",
    total_cost_user_currency_micros: null,
    total_cost_original_micros: null,
    currency_code_original: "USD",
    fx_rate_used: "1",
    fx_rate_source: "DEFAULT_1_TO_1",
    report_currency_code: "USD",
    report_currency_symbol: "$",
    reporting_currency_epoch: 3,
    currency_attribution: "identified",
    cost_segment_key: "e.3",
    pricing_template_id_used: 41,
    pricing_template_name_snapshot: "GPT-5.6 Standard",
    pricing_template_revision_id_used: "901",
    pricing_config_version_used: 4,
    pricing_version_effective_at: "2026-08-01T12:00:00Z",
    pricing_snapshot_unit: "PER_1M",
    pricing_snapshot_input: "2.5",
    pricing_snapshot_output: "10",
    pricing_snapshot_cache_read_input: null,
    pricing_snapshot_cache_creation_input: "0",
    pricing_snapshot_reasoning: "3",
    evidence_state: "authoritative",
  },
  legacy_pricing_evidence: null,
  current_pricing_template: {
    template_id: 41,
    deleted: false,
    current_revision_id: "944",
    current_version: 6,
    current_effective_at: "2026-08-08T09:00:00Z",
    matches_request_revision: false,
  },
};

const planningRow = row(1331, {
  row_kind: "planning",
  attempt_number: null,
  attempt_trigger: null,
  attempt_result: null,
  upstream_status_code: null,
  gateway_status_code: 503,
  error_source: "prism",
  error_code: "prism_routing_failure",
  failure_stage: "routing",
  failure_detail_preview: "No active connections available for model",
  failure_detail_preview_truncated: true,
  total_cost_user_currency_micros: null,
  pricing_status: "ineligible",
});

const failoverChainRows = [
  row(1341, {
    attempt_result: "http_error",
    upstream_status_code: 503,
    error_source: "upstream",
    error_code: "upstream_http_503",
    failure_stage: "upstream_response",
    failure_detail_preview: "primary unavailable",
    total_cost_user_currency_micros: null,
    pricing_status: "ineligible",
  }),
  row(1342, {
    attempt_number: 2,
    attempt_trigger: "retry_same_target",
    attempt_result: "http_error",
    upstream_status_code: 503,
    error_source: "upstream",
    error_code: "upstream_http_503",
    failure_stage: "upstream_response",
    failure_detail_preview: "primary still unavailable",
    total_cost_user_currency_micros: null,
    pricing_status: "ineligible",
  }),
  row(1343, {
    attempt_number: 3,
    attempt_trigger: "failover",
    attempt_result: "completed",
    is_winner: true,
    upstream_status_code: 200,
    stream_outcome: "completed",
    is_stream: true,
    attempt_duration_ms: 2100,
    total_cost_user_currency_micros: "9800",
    pricing_status: "priced",
  }),
];

const chainRows = [
  row(1327, {
    attempt_result: "http_error",
    upstream_status_code: 429,
    error_source: "upstream",
    error_code: "rate_limit_exceeded",
    failure_stage: "upstream_response",
    failure_detail_preview: "{\"error\":{\"message\":\"Rate limit reached for gpt-5.6 in organization org-123 on requests per day.",
    failure_detail_preview_truncated: true,
    total_cost_user_currency_micros: null,
    pricing_status: "ineligible",
  }),
  row(1328, {
    attempt_number: 2,
    attempt_trigger: "failover",
    upstream_status_code: 200,
    attempt_result: "completed",
    is_winner: true,
    stream_outcome: "completed",
    is_stream: true,
    attempt_duration_ms: 1234,
  }),
];

const chainResponse = {
  query_context: null,
  source_ingress_total: 3,
  retained_ingress_total: 3,
  retained_upstream_attempt_total: 4,
  retained_request_log_row_total: 4,
  legacy_unknown_row_total: 0,
  page_ingress_count: 2,
  page_upstream_attempt_count: 3,
  page_request_log_row_count: 3,
  items: [
    chainItem("018f91327a-1000-4000-8000-000000001327", chainRows),
    chainItem("018f91326b-2000-4000-8000-000000001326", [
      row(1326, { attempt_duration_ms: 500, total_tokens: 920, total_cost_user_currency_micros: "11550", stream_outcome: "completed", is_stream: true }),
    ]),
    chainItem("018f91340c-3000-4000-8000-000000001340", [planningRow]),
    chainItem("018f91350d-4000-4000-8000-000000001350", failoverChainRows),
  ],
  has_more_chains: true,
  next_chain_cursor: "signed-chain-cursor-2",
};

async function main() {
  const browser = await chromium.launch();
  try {
    for (const viewport of [
      { name: "1680x1050", width: 1680, height: 1050 },
      { name: "1200x800", width: 1200, height: 800 },
    ]) {
      const context = await browser.newContext({ viewport: { width: viewport.width, height: viewport.height } });
      const page = await context.newPage();
      await page.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        const { pathname } = url;
        if (pathname.startsWith("/api/")) {
          const fulfillJson = (body, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
          if (pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false });
          if (pathname === "/api/settings/costing") return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
          if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" });
          if (pathname === "/api/stats/requests") {
            if ((url.searchParams.get("view") || "ingress_chains") === "ingress_chains") return fulfillJson(chainResponse);
            return fulfillJson({ items: chainRows, total: 2, limit: 100, offset: 0, filter_options: { models: [], endpoints: [], clients: [], resolved_target_models: [] } });
          }
          if (/^\/api\/stats\/requests\/\d+$/.test(pathname)) return fulfillJson(detail429);
          if (pathname === "/api/audit/logs") {
            return fulfillJson({
              items: [
                { id: 201, request_log_id: 1327, profile_id: 1, model_id: "gpt-5.6", endpoint_id: 7, endpoint_label: "OpenCode Go", request_method: "POST", request_url: "https://api.openai.com/v1/chat/completions", request_headers: '{"authorization":"[REDACTED]","content-type":"application/json"}', request_body_preview: "{\"model\":\"gpt-5.6\",...}", request_body_stored: true, response_status: 429, response_body_stored: true, is_stream: false, duration_ms: 842, audit_enabled_at_request: true, audit_capture_bodies_at_request: true, created_at: now },
                { id: 202, request_log_id: 1327, profile_id: 1, model_id: "gpt-5.6", endpoint_id: 7, endpoint_label: "OpenCode Go", request_method: "POST", request_url: "https://api.openai.com/v1/chat/completions", request_headers: '{"authorization":"[REDACTED]","content-type":"application/json"}', request_body_preview: "{\"model\":\"gpt-5.6\",...}", request_body_stored: true, response_status: 200, response_body_stored: true, is_stream: true, duration_ms: 3200, audit_enabled_at_request: true, audit_capture_bodies_at_request: true, created_at: now },
              ],
              next_cursor: null,
              has_more: false,
              window: { from: "2026-08-09T09:30:00Z", to: "2026-08-09T10:30:00Z" },
              limit: 20,
              query_coverage: { state: "ready", from_time: "2026-08-09T09:30:00Z", to_time: "2026-08-09T10:30:00Z", retained_day_count: 1, retention_epoch: 1 },
            });
          }
          if (/^\/api\/audit\/logs\/\d+$/.test(pathname)) {
            // Audit log 202 is the streaming SSE fixture (with tool calls);
            // log 201 is the 429 failure fixture. The page URL query params
            // never reach the API fetch, so the fixture is keyed on the id.
            const isStream = pathname === "/api/audit/logs/202";
            const streamSse = [
              'data: {"choices":[{"delta":{"role":"assistant","content":"Let me check the weather."}}]}',
              "",
              "data: " + JSON.stringify({choices: [{delta: {tool_calls: [{index: 0, id: "call_1", function: {name: "get_weather", arguments: '{"city":"Paris"}'}}]}}]}),
              "",
              'data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}',
              "",
              "data: [DONE]",
              "",
            ].join("\n");
            const logId = isStream ? 202 : 201;
            return fulfillJson({
              id: logId, request_log_id: 1327, profile_id: 1, model_id: "gpt-5.6", endpoint_id: 7, endpoint_label: "OpenCode Go",
              request_method: "POST", request_url: "https://api.openai.com/v1/chat/completions",
              request_headers: '{"authorization":"[REDACTED]","content-type":"application/json"}',
              request_body: "{\"model\":\"gpt-5.6\",\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}",
              request_body_stored: true, response_status: isStream ? 200 : 429,
              response_headers: '{"content-type":"application/json"}',
              response_body: isStream ? streamSse : "{\"error\":{\"message\":\"Rate limit reached\",\"code\":\"rate_limit_exceeded\"}}",
              response_body_stored: true, is_stream: isStream, duration_ms: isStream ? 3200 : 842,
              audit_enabled_at_request: true, audit_capture_bodies_at_request: true, created_at: now,
            });
          }
          return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
        }
        return route.continue();
      });

      // Requests list (chain view) with pricing-state column.
      await page.goto(`${BASE}/observe/requests`, { waitUntil: "networkidle" });
      await page.getByTestId("request-logs-table").waitFor({ timeout: 15000 });
      await page.waitForTimeout(800);
      await page.screenshot({ path: path.join(OUT, `requests-list-${viewport.name}.png`), fullPage: false });

      // Failure detail sheet.
      await page.getByTestId("request-log-row-1327").click();
      await page.getByTestId("request-log-detail-sheet").waitFor({ timeout: 15000 });
      await page.waitForTimeout(800);
      await page.screenshot({ path: path.join(OUT, `failure-sheet-${viewport.name}.png`), fullPage: false });

      // Audit page (failure payload).
      await page.goto(`${BASE}/observe/requests/1327/audit`, { waitUntil: "networkidle" });
      await page.getByTestId("dedicated-audit-detail").waitFor({ timeout: 15000 }).catch(() => {});
      await page.waitForTimeout(1000);
      await page.screenshot({ path: path.join(OUT, `audit-page-${viewport.name}.png`), fullPage: false });

      // Streaming audit: message reassembly with tool-call card. The fixture
      // is keyed on audit_id=202; the tool card must actually render or the
      // capture fails loudly (no silent fallback to the failure page).
      await page.goto(`${BASE}/observe/requests/1327/audit?audit_id=202`, { waitUntil: "networkidle" });
      await page.getByTestId("dedicated-audit-detail").waitFor({ timeout: 15000 });
      await page.getByTestId("tool-call-card").waitFor({ timeout: 15000 });
      await page.waitForTimeout(1000);
      await page.screenshot({ path: path.join(OUT, `stream-tools-${viewport.name}.png`), fullPage: false });

      await context.close();
    }

    // 1200x800 with expanded sidebar.
    const side = await browser.newContext({ viewport: { width: 1200, height: 800 } });
    const sidePage = await side.newPage();
    await sidePage.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (!url.pathname.startsWith("/api/")) return route.continue();
      const fulfillJson = (body, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
      if (url.pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false });
      if (url.pathname === "/api/settings/costing") return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
      if (url.pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" });
      if (url.pathname === "/api/stats/requests") return fulfillJson(chainResponse);
      if (/^\/api\/stats\/requests\/\d+$/.test(url.pathname)) return fulfillJson(detail429);
      if (url.pathname === "/api/audit/logs") return fulfillJson({ items: [], next_cursor: null, has_more: false, window: { from: null, to: null }, limit: 20, query_coverage: { state: "ready" } });
      return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    });
    await sidePage.goto(`${BASE}/observe/requests`, { waitUntil: "networkidle" });
    await sidePage.getByTestId("request-logs-table").waitFor({ timeout: 15000 });
    // Expand the sidebar if collapsed.
    const toggle = sidePage.getByRole("button", { name: "Toggle Sidebar" });
    if (await toggle.isVisible().catch(() => false)) {
      await toggle.click().catch(() => {});
    }
    await sidePage.waitForTimeout(800);
    await sidePage.screenshot({ path: path.join(OUT, "requests-list-1200x800-sidebar-expanded.png"), fullPage: false });
    await side.close();

    // Narrow viewport.
    const narrow = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const narrowPage = await narrow.newPage();
    await narrowPage.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (!url.pathname.startsWith("/api/")) return route.continue();
      const fulfillJson = (body, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
      if (url.pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false });
      if (url.pathname === "/api/settings/costing") return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
      if (url.pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" });
      if (url.pathname === "/api/stats/requests") return fulfillJson(chainResponse);
      if (/^\/api\/stats\/requests\/\d+$/.test(url.pathname)) return fulfillJson(detail429);
      if (url.pathname === "/api/audit/logs") return fulfillJson({ items: [], next_cursor: null, has_more: false, window: { from: null, to: null }, limit: 20, query_coverage: { state: "ready" } });
      return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    });
    await narrowPage.goto(`${BASE}/observe/requests`, { waitUntil: "networkidle" });
    await narrowPage.getByTestId("request-logs-table").waitFor({ timeout: 15000 });
    await narrowPage.waitForTimeout(800);
    await narrowPage.screenshot({ path: path.join(OUT, "requests-list-narrow-390.png"), fullPage: false });
    await narrowPage.getByTestId("request-log-row-1327").click();
    await narrowPage.getByTestId("request-log-detail-sheet").waitFor({ timeout: 15000 });
    await narrowPage.waitForTimeout(800);
    await narrowPage.screenshot({ path: path.join(OUT, "failure-sheet-narrow-390.png"), fullPage: false });
    await narrow.close();

    console.log("screenshots written to", OUT);
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
