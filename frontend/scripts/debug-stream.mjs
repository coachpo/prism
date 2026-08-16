import { chromium } from "@playwright/test";
const browser = await chromium.launch();
const page = await browser.newPage();
await page.addInitScript(() => {
  window.__logs = [];
  const orig = console.error;
  console.error = (...args) => { window.__logs.push(args.join(" ")); orig(...args); };
});
// mock minimal
await page.route("**/*", async (route) => {
  const url = new URL(route.request().url());
  if (!url.pathname.startsWith("/api/")) return route.continue();
  const j = (b, s = 200) => route.fulfill({ status: s, contentType: "application/json", body: JSON.stringify(b) });
  if (url.pathname === "/api/auth/status") return j({ auth_enabled: false });
  if (url.pathname === "/api/settings/costing") return j({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
  if (url.pathname === "/api/settings/timezone") return j({ timezone_preference: "UTC" });
  if (/^\/api\/stats\/requests\/\d+$/.test(url.pathname)) return j({ summary: { request_log_id: "101", created_at: "2026-04-13T00:00:00Z", model_id: "gpt-4o-mini", model_label: "GPT-4o mini", resolved_target_model_id: null, resolved_target_model_label: null, api_family: "openai", row_kind: "upstream", upstream_status_code: 200, gateway_status_code: null, legacy_status_code: null, attempt_duration_ms: 125, legacy_duration_ms: null, ttft_ms: null, completion_duration_ms: null, is_stream: true, stream_outcome: "completed", stream_error_kind: null, attempt_number: 1, attempt_trigger: null, attempt_result: null, is_winner: null }, request: { operation_name: "openai.chat_completions", request_path: "/v1/chat/completions", ingress_request_id: "ingress-101", metadata_redacted_fields: [], metadata_truncated_fields: [], url_scrub_provenance: "runtime_scrubbed" }, routing: { profile_id: 1, endpoint_label: "x", endpoint_id: null, terminal_target_id: null, selected_terminal_target_id: null, audit_enabled_at_request: true, audit_capture_bodies_at_request: true }, usage: { input_tokens: null, output_tokens: null, total_tokens: null, success_flag: true }, failure: null, terminal_target: null, endpoint: null, routing_provenance: { initial_terminal_target: null, differs_from_actual: false }, pricing: { pricing_status: "priced", pricing_evidence_trust: "trusted", total_cost_user_currency_micros: null, report_currency_symbol: "$", evidence_state: "authoritative" }, legacy_pricing_evidence: null, current_pricing_template: null });
  if (url.pathname === "/api/audit/logs") {
    return j({ items: [{ id: 201, request_log_id: 101, profile_id: 1, model_id: "gpt-4o-mini", endpoint_id: null, connection_id: null, request_method: "POST", request_url: "https://x/v1/chat/completions", request_headers: "{}", request_body_preview: null, request_body_stored: false, response_status: 200, response_body_stored: true, is_stream: true, duration_ms: 125, audit_enabled_at_request: true, audit_capture_bodies_at_request: true, created_at: "2026-04-13T00:00:00Z" }], next_cursor: null, has_more: false, window: { from: "x", to: "y" }, limit: 20 });
  }
  if (/^\/api\/audit\/logs\/\d+$/.test(url.pathname)) {
    const sse = ['data: {"choices":[{"delta":{"role":"assistant","content":""}}]}', '', 'data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_weather","arguments":"{\\"city\\":\\"Paris\\"}"}}]}}]}', '', 'data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}', '', 'data: [DONE]', ''].join("\n");
    return j({ id: 201, request_log_id: 101, profile_id: 1, model_id: "gpt-4o-mini", endpoint_id: null, connection_id: null, request_method: "POST", request_url: "https://x/v1/chat/completions", request_headers: "{}", request_body: null, request_body_stored: false, response_status: 200, response_headers: "{}", response_body: sse, response_body_stored: true, is_stream: true, duration_ms: 125, audit_enabled_at_request: true, audit_capture_bodies_at_request: true, created_at: "2026-04-13T00:00:00Z" });
  }
  return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
});
await page.goto("http://127.0.0.1:15299/observe/requests/101/audit?audit_id=201", { waitUntil: "networkidle" });
await page.waitForTimeout(2500);
console.log("errors:", await page.evaluate(() => window.__logs?.slice(0, 5)));
const body = await page.locator("body").innerText();
console.log(body.includes("tool-call-card"), body.slice(0, 600));
await browser.close();
