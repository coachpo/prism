import { expect, test } from "@playwright/test";

const now = "2026-08-09T10:00:00Z";

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
      return fulfillJson({
        generated_at: now,
        scope: "process",
        instance_id: "evidence-instance",
        configuration_revision: "7",
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
      });
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
