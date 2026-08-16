import { expect, test } from "@playwright/test";

const now = "2026-08-09T10:00:00Z";

function currentStatePayload() {
  return {
    generated_at: now,
    scope: "process",
    instance_id: "evidence-instance",
    configuration_revision: "7",
    completeness: {
      state: "partial",
      complete: false,
      configured_target_count: 3,
      observed_target_count: 2,
      unobserved_target_count: 1,
      observed_subset_counts: { retry_wait: 1, banned: 1 },
    },
    items: [
      {
        model: { model_config_id: 42, id: "gpt-4o", label: "GPT-4o Primary", configured: true },
        endpoint: { id: 7, label: "OpenRouter", configured: true },
        terminal_target: { id: 17, label: "production", configured: true },
        observation_state: "observed",
        state: "banned",
        available: false,
        cycle_retry_attempts: 3,
        cumulative_retry_attempts: 9,
        next_retry_at: null,
        last_retry_delay_ms: 240000,
        ban_mode: "temporary",
        banned_until_at: "2026-08-09T11:30:00Z",
        last_failure_kind: "timeout",
        last_success_at: "2026-08-09T08:00:00Z",
        last_success_response_headers_latency_ms: 812,
        in_flight_stream: 1,
        in_flight_non_stream: 0,
        qps_window_started_at: null,
        qps_window_request_count: 0,
        created_at: now,
        updated_at: now,
      },
      {
        model: { model_config_id: 42, id: "gpt-4o", label: "GPT-4o Primary", configured: true },
        endpoint: { id: 7, label: "OpenRouter", configured: true },
        terminal_target: { id: 18, label: "canary", configured: true },
        observation_state: "observed",
        state: "retry_wait",
        available: false,
        cycle_retry_attempts: 1,
        cumulative_retry_attempts: 2,
        next_retry_at: "2026-08-09T10:05:00Z",
        last_retry_delay_ms: 60000,
        ban_mode: "off",
        banned_until_at: null,
        last_failure_kind: "transient_http",
        last_success_at: "2026-08-09T09:00:00Z",
        last_success_response_headers_latency_ms: null,
        in_flight_stream: 0,
        in_flight_non_stream: 0,
        qps_window_started_at: null,
        qps_window_request_count: 0,
        created_at: now,
        updated_at: now,
      },
      {
        model: { model_config_id: 43, id: "claude-3-5", label: "Claude 3.5", configured: true },
        endpoint: { id: 8, label: "Anthropic", configured: true },
        terminal_target: { id: 19, label: "main", configured: true },
        observation_state: "unobserved",
        state: null,
        available: null,
        cycle_retry_attempts: null,
        cumulative_retry_attempts: null,
        next_retry_at: null,
        last_retry_delay_ms: null,
        ban_mode: null,
        banned_until_at: null,
        last_failure_kind: null,
        last_success_at: null,
        last_success_response_headers_latency_ms: null,
        in_flight_stream: null,
        in_flight_non_stream: null,
        qps_window_started_at: null,
        qps_window_request_count: null,
        created_at: null,
        updated_at: null,
      },
    ],
    has_more: false,
    next_cursor: null,
  };
}

function eventsPayload() {
  return {
    generated_at: now,
    coverage: { complete: true, gaps: [] },
    source_status: { delivery: "best_effort", transition_ledger_complete: false, dropped_event_count: null },
    items: [
      {
        event_id: "1002",
        created_at: "2026-08-09T09:59:30Z",
        event_type: "banned",
        summary: {
          version: 1,
          code: "loadbalance.banned",
          params: {
            evidence_state: "complete",
            failure_kind: "timeout",
            cycle_retry_attempts: 3,
            cumulative_retry_attempts: 9,
            last_retry_delay_ms: 240000,
            ban_mode: "temporary",
            policy_ban_cumulative_retry_attempt_threshold: 8,
            banned_until_at: "2026-08-09T11:30:00Z",
          },
        },
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
        request_context_filters: {
          schema_version: 1,
          kind: "contextual_window",
          correlation: "not_exact",
          from_time: "2026-08-09T09:44:30Z",
          to_time: "2026-08-09T10:14:30Z",
          model_id: "gpt-4o",
          endpoint_id: 7,
          terminal_target_id: 17,
        },
        request_context_unavailable_reason: null,
      },
      {
        event_id: "1001",
        created_at: "2026-08-09T09:58:00Z",
        event_type: "admission_rejected",
        summary: {
          version: 1,
          code: "loadbalance.admission_rejected",
          params: { evidence_state: "complete", admission_reason: "qps_limit" },
        },
        failure_kind: null,
        admission_reason: "qps_limit",
        model: { model_config_id: 42, id: "gpt-4o", label: "GPT-4o Primary", configured: true, attribution: "identified" },
        endpoint: { id: 7, label: "OpenRouter", configured: true, attribution: "identified" },
        terminal_target: { id: 18, owner_model_config_id: 42, label: "canary", configured: true, attribution: "identified" },
        cycle_retry_attempts: 0,
        cumulative_retry_attempts: 0,
        next_retry_at: null,
        last_retry_delay_ms: 0,
        ban_mode: null,
        policy_cycle_retry_attempt_limit: null,
        policy_ban_cumulative_retry_attempt_threshold: null,
        banned_until_at: null,
        last_success_at: null,
        request_context_filters: null,
        request_context_unavailable_reason: "request_retention_no_overlap",
      },
    ],
    has_more: false,
    next_cursor: null,
  };
}

test("routing health shows global current state and the events timeline with typed summaries", async ({ page }) => {
  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }
    const fulfillJson = (body, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") return fulfillJson({ state: "disabled", transition_state: null, login_available: false, effective_generation: "1", retry_after_seconds: null });
    if (pathname === "/api/settings/costing") return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" });
    if (pathname === "/api/loadbalance/events/query-context") {
      return fulfillJson({
        query_context: "signed-context-token",
        requested_preset: "24h",
        event_bounds: { from_time: "2026-08-08T10:00:00Z", to_time: "2026-08-09T10:00:00Z" },
        coverage: { complete: true, gaps: [] },
        source_status: { delivery: "best_effort", transition_ledger_complete: false, dropped_event_count: null },
        generated_at: now,
      });
    }
    if (pathname === "/api/loadbalance/events") return fulfillJson(eventsPayload());
    if (pathname === "/api/loadbalance/current-state") return fulfillJson(currentStatePayload());
    if (pathname === "/api/loadbalance/events/1002") return fulfillJson({ ...eventsPayload().items[0], request_context_unavailable_reason: null });
    if (pathname === "/api/stats/requests") {
      return fulfillJson({ items: [], total: 0, filter_options: { models: [], endpoints: [], clients: [], resolved_target_models: [] } });
    }
    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });

  await page.goto("/observe?tab=events");
  await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: 15000 });
  await expect(page.getByTestId("routing-health-tab")).toBeVisible();

  // Current State: configured union with observed + unobserved rows and a
  // partial completeness notice; unobserved rows never fake availability.
  await expect(page.getByText("已观测 2 / 已配置 3 个目标")).toBeVisible();
  await expect(page.getByText("观测部分")).toBeVisible();
  await expect(page.getByRole("row", { name: /GPT-4o Primary/ }).first()).toContainText("已封禁");
  await expect(page.getByRole("row", { name: /Claude 3.5/ })).toContainText("本进程尚未观测");
  await expect(page.getByRole("row", { name: /Claude 3.5/ })).not.toContainText("当前无冷却限制");

  // Events timeline: typed V1 summaries render in zh-CN without backend prose.
  await expect(page.getByTestId("event-row-1002")).toContainText("GPT-4o Primary");
  await expect(page.getByTestId("event-row-1001")).toContainText("QPS 限制");
  await expect(page.getByText("已记录的路由事件（来源可能不完整）").first()).toBeVisible();

  // Detail sheet: complete facts, safe object links and the Requests handoff.
  await page.getByTestId("event-row-1002").getByRole("button", { name: "查看详情" }).click();
  await expect(page.getByText("事件详情")).toBeVisible();
  await expect(page.getByText("发生了什么", { exact: true })).toBeVisible();
  await expect(page.getByText("重试与封禁快照", { exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "调查附近请求" })).toBeVisible();
  const requestsLink = page.getByRole("link", { name: "调查附近请求" });
  await expect(requestsLink).toHaveAttribute("href", /\/observe\/requests/);
  await expect(requestsLink).toHaveAttribute("href", /from_time=2026-08-09T09%3A44%3A30Z/);
  await expect(requestsLink).toHaveAttribute("href", /to_time=2026-08-09T10%3A14%3A30Z/);
  await expect(requestsLink).toHaveAttribute("href", /terminal_target_id=17/);
  await expect(requestsLink).toHaveAttribute("href", /observe_return=/);

  // The Requests page renders the validated return action which restores the
  // source event context (event selection, window and sort).
  await requestsLink.click();
  await expect(page.getByRole("heading", { name: "请求日志" })).toBeVisible();
  await expect(page.getByRole("link", { name: "返回路由健康" })).toBeVisible();
  await page.getByRole("link", { name: "返回路由健康" }).click();
  await expect(page).toHaveURL(/\/observe\/routing-health/);
  await expect(page).toHaveURL(/event_id=1002/);
  await expect(page).toHaveURL(/preset=24h/);
  await expect(page.getByText("事件详情")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByText("事件详情")).toHaveCount(0);
});

test("routing health empty, stale and error states are distinguishable", async ({ page }) => {
  let eventsRequests = 0;
  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }
    const fulfillJson = (body, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") return fulfillJson({ state: "disabled", transition_state: null, login_available: false, effective_generation: "1", retry_after_seconds: null });
    if (pathname === "/api/settings/costing") return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" });
    if (pathname === "/api/loadbalance/events/query-context") {
      return fulfillJson({ query_context: "ctx", requested_preset: "24h", event_bounds: { from_time: "2026-08-08T10:00:00Z", to_time: "2026-08-09T10:00:00Z" }, coverage: { complete: true, gaps: [] }, source_status: { delivery: "best_effort", transition_ledger_complete: false, dropped_event_count: null }, generated_at: now });
    }
    if (pathname === "/api/loadbalance/events") {
      eventsRequests += 1;
      // First request fails (503); the retry succeeds with a true empty list.
      if (eventsRequests === 1) {
        return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ detail: "temporarily overloaded" }) });
      }
      return fulfillJson({ generated_at: now, coverage: { complete: true, gaps: [] }, source_status: { delivery: "best_effort", transition_ledger_complete: false, dropped_event_count: null }, items: [], has_more: false, next_cursor: null });
    }
    if (pathname === "/api/loadbalance/current-state") {
      return fulfillJson({ ...currentStatePayload(), completeness: { state: "no_config", complete: true, configured_target_count: 0, observed_target_count: 0, unobserved_target_count: 0, observed_subset_counts: null }, items: [] });
    }
    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });

  await page.goto("/observe?tab=events");
  await expect(page.getByTestId("routing-health-tab")).toBeVisible();

  // Initial 503: error + retry, never an empty state.
  await expect(page.getByText("加载失败")).toBeVisible();
  await expect(page.getByText("所选时间没有已记录事件")).toHaveCount(0);

  // Retry recovers into a true empty state (coverage complete).
  await page.getByRole("button", { name: "重试" }).first().click();
  await expect(page.getByText("所选时间没有已记录事件")).toBeVisible();
  await expect(page.getByText("覆盖不完整")).toHaveCount(0);

  // Current State with no configured targets is a non-error no_config state.
  await expect(page.getByText("未配置目标")).toBeVisible();
});
