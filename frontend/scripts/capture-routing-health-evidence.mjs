// Evidence capture for the routing-health (Observe events tab) surface.
import { chromium } from "@playwright/test";
import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const frontendDir = path.resolve(__dirname, "..");
const repoRoot = path.resolve(frontendDir, "..");
const evidenceDir = path.join(repoRoot, "artifacts", "evidence");
fs.mkdirSync(evidenceDir, { recursive: true });

const e2ePort = 15176;
const baseURL = `http://127.0.0.1:${e2ePort}`;
const now = "2026-08-09T10:00:00Z";

const currentState = {
  generated_at: now,
  scope: "process",
  instance_id: "evidence-instance",
  configuration_revision: "7",
  completeness: { state: "partial", complete: false, configured_target_count: 3, observed_target_count: 2, unobserved_target_count: 1, observed_subset_counts: { retry_wait: 1, banned: 1 } },
  items: [
    { model: { model_config_id: 42, id: "gpt-4o", label: "GPT-4o Primary", configured: true }, endpoint: { id: 7, label: "OpenRouter", configured: true }, terminal_target: { id: 17, label: "production", configured: true }, observation_state: "observed", state: "banned", available: false, cycle_retry_attempts: 3, cumulative_retry_attempts: 9, next_retry_at: null, last_retry_delay_ms: 240000, ban_mode: "temporary", banned_until_at: "2026-08-09T11:30:00Z", last_failure_kind: "timeout", last_success_at: "2026-08-09T08:00:00Z", last_success_response_headers_latency_ms: 812, in_flight_stream: 1, in_flight_non_stream: 0, qps_window_started_at: null, qps_window_request_count: 0, created_at: now, updated_at: now },
    { model: { model_config_id: 42, id: "gpt-4o", label: "GPT-4o Primary", configured: true }, endpoint: { id: 7, label: "OpenRouter", configured: true }, terminal_target: { id: 18, label: "canary", configured: true }, observation_state: "observed", state: "retry_wait", available: false, cycle_retry_attempts: 1, cumulative_retry_attempts: 2, next_retry_at: "2026-08-09T10:05:00Z", last_retry_delay_ms: 60000, ban_mode: "off", banned_until_at: null, last_failure_kind: "transient_http", last_success_at: "2026-08-09T09:00:00Z", last_success_response_headers_latency_ms: null, in_flight_stream: 0, in_flight_non_stream: 0, qps_window_started_at: null, qps_window_request_count: 0, created_at: now, updated_at: now },
    { model: { model_config_id: 43, id: "claude-3-5", label: "Claude 3.5", configured: true }, endpoint: { id: 8, label: "Anthropic", configured: true }, terminal_target: { id: 19, label: "main", configured: true }, observation_state: "unobserved", state: null, available: null, cycle_retry_attempts: null, cumulative_retry_attempts: null, next_retry_at: null, last_retry_delay_ms: null, ban_mode: null, banned_until_at: null, last_failure_kind: null, last_success_at: null, last_success_response_headers_latency_ms: null, in_flight_stream: null, in_flight_non_stream: null, qps_window_started_at: null, updated_at: null, created_at: null },
  ],
  has_more: false,
  next_cursor: null,
};

const events = {
  generated_at: now,
  coverage: { complete: true, gaps: [] },
  source_status: { delivery: "best_effort", transition_ledger_complete: false, dropped_event_count: null },
  items: [
    {
      event_id: "1002",
      created_at: "2026-08-09T09:59:30Z",
      event_type: "banned",
      summary: { version: 1, code: "loadbalance.banned", params: { evidence_state: "complete", failure_kind: "timeout", cycle_retry_attempts: 3, cumulative_retry_attempts: 9, last_retry_delay_ms: 240000, ban_mode: "temporary", policy_ban_cumulative_retry_attempt_threshold: 8, banned_until_at: "2026-08-09T11:30:00Z" } },
      failure_kind: "timeout",
      admission_reason: null,
      model: { model_config_id: 42, id: "gpt-4o", label: "GPT-4o Primary", configured: true, attribution: "identified" },
      endpoint: { id: 7, label: "OpenRouter", configured: true, attribution: "identified" },
      terminal_target: { id: 17, owner_model_config_id: 42, label: "production", configured: true, attribution: "identified" },
      cycle_retry_attempts: 3,
      cumulative_retry_attempts: 9,
      next_retry_at: null,
      last_retry_delay_ms: 240000,
      ban_mode: "temporary",
      policy_cycle_retry_attempt_limit: 3,
      policy_ban_cumulative_retry_attempt_threshold: 8,
      banned_until_at: "2026-08-09T11:30:00Z",
      last_success_at: null,
      request_context_filters: { schema_version: 1, kind: "contextual_window", correlation: "not_exact", from_time: "2026-08-09T09:44:30Z", to_time: "2026-08-09T10:14:30Z", model_id: "gpt-4o", endpoint_id: 7, terminal_target_id: 17 },
      request_context_unavailable_reason: null,
    },
    {
      event_id: "1001",
      created_at: "2026-08-09T09:58:00Z",
      event_type: "retry_scheduled",
      summary: { version: 1, code: "loadbalance.retry_scheduled", params: { evidence_state: "complete", failure_kind: "transient_http", cycle_retry_attempts: 1, cumulative_retry_attempts: 2, last_retry_delay_ms: 60000, next_retry_at: "2026-08-09T10:05:00Z" } },
      failure_kind: "transient_http",
      admission_reason: null,
      model: { model_config_id: 42, id: "gpt-4o", label: "GPT-4o Primary", configured: true, attribution: "identified" },
      endpoint: { id: 7, label: "OpenRouter", configured: true, attribution: "identified" },
      terminal_target: { id: 18, owner_model_config_id: 42, label: "canary", configured: true, attribution: "identified" },
      cycle_retry_attempts: 1,
      cumulative_retry_attempts: 2,
      next_retry_at: "2026-08-09T10:05:00Z",
      last_retry_delay_ms: 60000,
      ban_mode: "off",
      policy_cycle_retry_attempt_limit: 3,
      policy_ban_cumulative_retry_attempt_threshold: 0,
      banned_until_at: null,
      last_success_at: null,
      request_context_filters: null,
      request_context_unavailable_reason: "request_retention_no_overlap",
    },
    {
      event_id: "1000",
      created_at: "2026-08-09T09:55:00Z",
      event_type: "recovered",
      summary: { version: 1, code: "loadbalance.recovered", params: { evidence_state: "complete", failure_kind: "timeout", cycle_retry_attempts: 2, cumulative_retry_attempts: 6, last_retry_delay_ms: 120000, last_success_at: "2026-08-09T09:55:01Z" } },
      failure_kind: "timeout",
      admission_reason: null,
      model: { model_config_id: 43, id: "claude-3-5", label: "Claude 3.5", configured: true, attribution: "identified" },
      endpoint: { id: 8, label: "Anthropic", configured: true, attribution: "identified" },
      terminal_target: { id: 19, owner_model_config_id: 43, label: "main", configured: true, attribution: "identified" },
      cycle_retry_attempts: 2,
      cumulative_retry_attempts: 6,
      next_retry_at: null,
      last_retry_delay_ms: 120000,
      ban_mode: "off",
      policy_cycle_retry_attempt_limit: 3,
      policy_ban_cumulative_retry_attempt_threshold: 0,
      banned_until_at: null,
      last_success_at: "2026-08-09T09:55:01Z",
      request_context_filters: null,
      request_context_unavailable_reason: "request_retention_no_overlap",
    },
  ],
  has_more: false,
  next_cursor: null,
};

async function main() {
  const server = spawn("pnpm", ["exec", "vite", "--host", "127.0.0.1", "--port", String(e2ePort), "--strictPort"], {
    cwd: frontendDir,
    stdio: "ignore",
  });
  try {
    for (let attempt = 0; attempt < 40; attempt += 1) {
      try {
        const response = await fetch(`${baseURL}/`);
        if (response.ok) break;
      } catch {
        // not up yet
      }
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }

    const browser = await chromium.launch();
    const page = await browser.newPage();

    await page.route("**/*", async (route) => {
      const request = route.request();
      const pathname = new URL(request.url()).pathname;
      if (!pathname.startsWith("/api/")) {
        return route.continue();
      }
      const fulfillJson = (body, status = 200) =>
        route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
      if (pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false });
      if (pathname === "/api/loadbalance/events/query-context") {
        return fulfillJson({ query_context: "ctx", requested_preset: "24h", event_bounds: { from_time: "2026-08-08T10:00:00Z", to_time: "2026-08-09T10:00:00Z" }, coverage: { complete: true, gaps: [] }, source_status: { delivery: "best_effort", transition_ledger_complete: false, dropped_event_count: null }, generated_at: now });
      }
      if (pathname === "/api/loadbalance/events") return fulfillJson(events);
      if (pathname === "/api/loadbalance/events/1002") return fulfillJson({ ...events.items[0] });
      if (pathname === "/api/loadbalance/current-state") return fulfillJson(currentState);
      if (pathname === "/api/settings/costing") return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
      if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "Asia/Shanghai" });
      if (pathname === "/api/models") return fulfillJson([]);
      return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    });

    // 1680×1050
    await page.setViewportSize({ width: 1680, height: 1050 });
    await page.goto(`${baseURL}/observe?tab=events`);
    await page.getByRole("heading", { name: "路由健康", exact: true }).waitFor();
    await page.getByTestId("event-row-1002").waitFor();
    await page.waitForTimeout(700);
    await page.screenshot({ path: path.join(evidenceDir, "ux-ban-events-1680-v2.png"), fullPage: true });

    // Detail sheet at 1680
    await page.getByTestId("event-row-1002").getByRole("button", { name: "查看详情" }).click();
    await page.getByText("事件详情").waitFor();
    await page.waitForTimeout(400);
    await page.screenshot({ path: path.join(evidenceDir, "ux-ban-event-detail-v2.png"), fullPage: true });
    await page.keyboard.press("Escape");
    await page.waitForTimeout(300);

    // 1200×800 with sidebar
    await page.setViewportSize({ width: 1200, height: 800 });
    await page.goto(`${baseURL}/observe?tab=events`);
    await page.getByRole("heading", { name: "路由健康", exact: true }).waitFor();
    await page.getByTestId("event-row-1002").waitFor();
    await page.waitForTimeout(700);
    await page.screenshot({ path: path.join(evidenceDir, "ux-ban-events-1200-v2.png"), fullPage: true });

    // 390×844
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(`${baseURL}/observe?tab=events`);
    await page.getByRole("heading", { name: "路由健康", exact: true }).waitFor();
    await page.getByTestId("event-row-1002").waitFor();
    await page.waitForTimeout(700);
    await page.screenshot({ path: path.join(evidenceDir, "ux-ban-events-390-v2.png"), fullPage: true });

    await browser.close();
    console.log("routing-health evidence captured");
  } finally {
    server.kill("SIGTERM");
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
