import { expect, test } from "@playwright/test";

const timestamp = "2026-04-10T00:00:00Z";

function createCostingSettings() {
  return {
    report_currency_code: "USD",
    report_currency_symbol: "$",
    endpoint_fx_mappings: [],
    timezone_preference: null,
  };
}

type StrategyType = "single" | "fill-first" | "round-robin";
type StrategyBanMode = "off" | "temporary" | "until_reset";

function createStrategyRow({
  id,
  name,
  legacyStrategyType = "single",
  isDefault = false,
  banMode,
  banDurationSeconds = 0,
  banCumulativeRetryAttemptThreshold = 0,
}: {
  id: number;
  name: string;
  legacyStrategyType?: StrategyType;
  isDefault?: boolean;
  banMode: StrategyBanMode;
  banDurationSeconds?: number;
  banCumulativeRetryAttemptThreshold?: number;
}) {
  return {
    id,
    profile_id: 1,
    name,
    legacy_strategy_type: legacyStrategyType,
    is_default: isDefault,
    failure_status_codes: [403, 422, 429, 500, 502, 503, 504, 529],
    ban_mode: banMode,
    retry_base_delay_ms: 60000,
    retry_backoff_multiplier: 2,
    retry_jitter_ratio: 0.2,
    retry_max_delay_ms: 900000,
    cycle_retry_attempt_limit: 2,
    ban_cumulative_retry_attempt_threshold: banCumulativeRetryAttemptThreshold,
    ban_duration_seconds: banDurationSeconds,
    attached_model_count: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

const addStrategyButton = /新建策略|Add Strategy|新增策略/;
const cycleRetryLimitLabel = /每轮重试次数上限|Cycle Retry Attempt Limit|周期重试次数限制/;
const cumulativeThresholdLabel = /累计重试次数阈值|Ban Cumulative Retry Attempt Threshold|封禁累计重试次数阈值/;
const banModeLabel = /封禁模式|Ban Mode/;

test("routing strategy dialog links ban fields with provenance and keeps user edits", async ({ page }) => {
  const strategies = [
    createStrategyRow({
      id: 2,
      name: "Default fill-first routing",
      legacyStrategyType: "fill-first",
      isDefault: true,
      banMode: "off",
    }),
  ];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }
    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") {
      return fulfillJson({ state: "disabled", transition_state: null, login_available: false, effective_generation: "1", retry_after_seconds: null });
    }
    if (pathname === "/api/loadbalance/strategies" && request.method() === "GET") {
      return fulfillJson(strategies);
    }
    if (pathname === "/api/loadbalance/strategies/preview" && request.method() === "POST") {
      return fulfillJson({
        normalized_policy: { name: "", legacy_strategy_type: "fill-first", failure_status_codes: [403, 422, 429, 500, 502, 503, 504, 529], ban_mode: "off", retry_base_delay_ms: 60000, retry_backoff_multiplier: 2, retry_jitter_ratio: 0.2, retry_max_delay_ms: 900000, cycle_retry_attempt_limit: 3, ban_cumulative_retry_attempt_threshold: 0, ban_duration_seconds: 0 },
        steps: [
          { failure_ordinal: 1, cycle_retry_attempt: 1, cumulative_retry_attempt: 1, nominal_delay_ms: 60000, jitter_min_delay_ms: 48000, jitter_max_delay_ms: 72000, cycle_exhausted: false, ban_transition: null },
          { failure_ordinal: 2, cycle_retry_attempt: 2, cumulative_retry_attempt: 2, nominal_delay_ms: 120000, jitter_min_delay_ms: 96000, jitter_max_delay_ms: 144000, cycle_exhausted: false, ban_transition: null },
          { failure_ordinal: 3, cycle_retry_attempt: 3, cumulative_retry_attempt: 3, nominal_delay_ms: 240000, jitter_min_delay_ms: 192000, jitter_max_delay_ms: 288000, cycle_exhausted: true, ban_transition: null },
        ],
        shown_step_count: 3,
        has_more: false,
        termination_reason: "cycle_exhausted",
        cycle_exhaustion_after_attempt: 3,
        ban_projection: { mode: "off", cumulative_retry_attempt_threshold: 0, transition_at_cumulative_failure: null, duration_seconds: 0 },
      });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson(createCostingSettings());
    }
    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }
    if (pathname === "/api/models") {
      return fulfillJson([]);
    }
    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });

  await page.goto("/route/ban-policies");
  await expect(page.getByRole("heading", { name: "路由策略", exact: true })).toBeVisible();

  await page.getByRole("button", { name: addStrategyButton }).first().click();
  const cycleLimitInput = page.getByLabel(cycleRetryLimitLabel);
  const cumulativeThresholdInput = page.getByLabel(cumulativeThresholdLabel);
  await expect(cycleLimitInput).toBeVisible();
  await expect(cumulativeThresholdInput).toBeVisible();

  // The effect preview renders the shared backend steps. The page-level
  // timeline carries the same title, so scope these assertions to the dialog.
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("效果预览", { exact: true })).toBeVisible();
  await expect(dialog.getByText("固定延迟 60000ms")).toBeVisible();
  await expect(dialog.getByText("抖动范围 48000ms – 72000ms")).toBeVisible();
  await expect(dialog.getByText("本轮重试次数已用尽", { exact: true }).first()).toBeVisible();

  // Switching ban mode off -> temporary auto-fills the derived threshold
  // (cycle limit x2) and the safe 900s duration, with visible provenance.
  await page.getByLabel(banModeLabel).click();
  await expect(page.getByRole("option")).toHaveText(["关闭", "临时", "直到重置"]);
  await page.getByRole("option", { name: "临时" }).click();
  await expect(cumulativeThresholdInput).toHaveValue("6");
  await expect(page.getByText(/已根据/).first()).toBeVisible();

  // A user-edited threshold is never silently overwritten by a higher cycle
  // limit; the one-click sync action stays available.
  await cumulativeThresholdInput.fill("4");
  await cycleLimitInput.fill("5");
  await expect(cumulativeThresholdInput).toHaveValue("4");
  await expect(page.getByRole("button", { name: /一键同步为 5/ })).toBeVisible();
});
