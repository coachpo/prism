import { expect, test, type Locator } from "@playwright/test";

const timestamp = "2026-04-10T00:00:00Z";
const routeReadyTimeout = 15_000;

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

type StrategyPayload = {
  name: string;
  legacy_strategy_type: StrategyType;
  failure_status_codes: number[];
  ban_mode: StrategyBanMode;
  retry_base_delay_ms: number;
  retry_backoff_multiplier: number;
  retry_jitter_ratio: number;
  retry_max_delay_ms: number;
  cycle_retry_attempt_limit: number;
  ban_cumulative_retry_attempt_threshold: number;
  ban_duration_seconds: number;
};

function createStrategyRow({
  id,
  name,
  legacyStrategyType = "single",
  banMode,
  banDurationSeconds = 0,
  banCumulativeRetryAttemptThreshold = 0,
}: {
  id: number;
  name: string;
  legacyStrategyType?: StrategyType;
  banMode: StrategyBanMode;
  banDurationSeconds?: number;
  banCumulativeRetryAttemptThreshold?: number;
}) {
  return {
    id,
    profile_id: 1,
    name,
    legacy_strategy_type: legacyStrategyType,
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

async function expectRecoveryLines(row: Locator, lines: string[]) {
  const recoveryLines = row.locator("td").nth(2).locator("span");

  await expect(recoveryLines).toHaveText(lines, { timeout: routeReadyTimeout });
}

test("loadbalance strategies table shows explicit Ban Policy rows by name", async ({ page }) => {
  const strategies = [
    createStrategyRow({
      id: 1,
      name: "Legacy Off",
      banMode: "off",
    }),
    createStrategyRow({
      id: 2,
      name: "Legacy Until Reset",
      banMode: "until_reset",
      banCumulativeRetryAttemptThreshold: 4,
    }),
    createStrategyRow({
      id: 3,
      name: "Legacy Temporary",
      banMode: "temporary",
      banDurationSeconds: 28800,
      banCumulativeRetryAttemptThreshold: 4,
    }),
  ];

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

    if (pathname === "/api/auth/status") {
      return fulfillJson({ auth_enabled: false });
    }

    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson(strategies);
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

  await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: routeReadyTimeout });
  await expect(page.getByText("Loading application...")).toHaveCount(0, {
    timeout: routeReadyTimeout,
  });
  await expect(page.getByRole("table")).toBeVisible({ timeout: routeReadyTimeout });
  await expect(page.getByRole("table")).toContainText("Legacy Off");
  await expect(page.getByRole("table")).toContainText("Legacy Until Reset");
  await expect(page.getByRole("table")).toContainText("Legacy Temporary");

  await expectRecoveryLines(page.getByRole("row", { name: /Legacy Off/ }), [
    "Status codes 403, 422, 429, 500, 502, 503, 504, 529",
    "Cycle retry limit 2 attempts • retry window 60,000ms base, 900,000ms max, 2x backoff, jitter 0.2",
    "Ban off; cumulative threshold disabled",
  ]);

  await expectRecoveryLines(page.getByRole("row", { name: /Legacy Until Reset/ }), [
    "Status codes 403, 422, 429, 500, 502, 503, 504, 529",
    "Cycle retry limit 2 attempts • retry window 60,000ms base, 900,000ms max, 2x backoff, jitter 0.2",
    "Cumulative threshold 4 attempts bans until reset",
  ]);

  await expectRecoveryLines(page.getByRole("row", { name: /Legacy Temporary/ }), [
    "Status codes 403, 422, 429, 500, 502, 503, 504, 529",
    "Cycle retry limit 2 attempts • retry window 60,000ms base, 900,000ms max, 2x backoff, jitter 0.2",
    "Cumulative threshold 4 attempts triggers temporary ban for 28,800s",
  ]);

  await page.getByRole("button", { name: "Add Strategy" }).first().click();
  const cycleLimitInput = page.getByLabel("Cycle Retry Attempt Limit");
  const cumulativeThresholdInput = page.getByLabel("Ban Cumulative Retry Attempt Threshold");
  await expect(cycleLimitInput).toBeVisible();
  await expect(cumulativeThresholdInput).toBeVisible();

  await page.getByLabel("Ban Mode").click();
  await expect(page.getByRole("option")).toHaveText(["Off", "Temporary", "Until reset"]);
  await page.getByRole("option", { name: "Temporary" }).click();

  await expect(cumulativeThresholdInput).toHaveValue("6");
  await cumulativeThresholdInput.fill("4");
  await cycleLimitInput.fill("5");
  await expect(cumulativeThresholdInput).toHaveValue("5");
});

test("loadbalance strategy dialog creates and edits surviving routing families", async ({ page }) => {
  const strategies = [
    createStrategyRow({
      id: 8,
      name: "Existing round robin",
      legacyStrategyType: "round-robin",
      banMode: "off",
    }),
  ];
  let createdStrategyType = "";
  let updatedStrategyType = "";

  const buildStrategyFromPayload = (id: number, payload: StrategyPayload) => ({
    ...createStrategyRow({
      id,
      name: payload.name,
      legacyStrategyType: payload.legacy_strategy_type,
      banMode: payload.ban_mode,
      banDurationSeconds: payload.ban_duration_seconds,
      banCumulativeRetryAttemptThreshold: payload.ban_cumulative_retry_attempt_threshold,
    }),
    failure_status_codes: payload.failure_status_codes,
    retry_base_delay_ms: payload.retry_base_delay_ms,
    retry_backoff_multiplier: payload.retry_backoff_multiplier,
    retry_jitter_ratio: payload.retry_jitter_ratio,
    retry_max_delay_ms: payload.retry_max_delay_ms,
    cycle_retry_attempt_limit: payload.cycle_retry_attempt_limit,
  });

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

    if (pathname === "/api/auth/status") {
      return fulfillJson({ auth_enabled: false });
    }

    if (pathname === "/api/loadbalance/strategies" && request.method() === "GET") {
      return fulfillJson(strategies);
    }

    if (pathname === "/api/loadbalance/strategies" && request.method() === "POST") {
      const createdPayload = request.postDataJSON() as StrategyPayload;
      createdStrategyType = createdPayload.legacy_strategy_type ?? "";
      const created = buildStrategyFromPayload(12, createdPayload);
      strategies.unshift(created);
      return fulfillJson(created, 201);
    }

    const strategyDetailMatch = pathname.match(/^\/api\/loadbalance\/strategies\/(\d+)$/);
    if (strategyDetailMatch && request.method() === "GET") {
      const strategyId = Number(strategyDetailMatch[1]);
      return fulfillJson(strategies.find((strategy) => strategy.id === strategyId));
    }

    if (strategyDetailMatch && request.method() === "PUT") {
      const strategyId = Number(strategyDetailMatch[1]);
      const updatedPayload = request.postDataJSON() as StrategyPayload;
      updatedStrategyType = updatedPayload.legacy_strategy_type ?? "";
      const next = buildStrategyFromPayload(strategyId, updatedPayload);
      const index = strategies.findIndex((strategy) => strategy.id === strategyId);
      strategies[index] = next;
      return fulfillJson(next);
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
  await expect(page.getByRole("table")).toContainText("Existing round robin");
  await expect(page.getByRole("table")).toContainText("Round robin");

  await page.getByRole("button", { name: "Add Strategy" }).first().click();
  await expect(page.getByText("Configure reusable terminal-target routing families and Ban Policy for this profile.")).toBeVisible();
  await page.getByLabel("Name").fill("Fill-first routing");
  await page.getByLabel("Routing family").click();
  await expect(page.getByRole("option")).toHaveText([
    "Single",
    "Fill first",
    "Round robin",
  ]);
  await page.getByRole("option", { name: "Fill first" }).click();
  await page.getByRole("button", { name: "Save Strategy" }).click();

  await expect(page.getByRole("table")).toContainText("Fill-first routing");
  await expect(page.getByRole("table")).toContainText("Fill first");
  expect(createdStrategyType).toBe("fill-first");

  const createdRow = page.getByRole("row", { name: /Fill-first routing/ });
  await createdRow.getByRole("button", { name: "Edit" }).click();
  await page.getByLabel("Name").fill("Fill-first routing updated");
  await page.getByRole("button", { name: "Save Strategy" }).click();

  await expect(page.getByRole("table")).toContainText("Fill-first routing updated");
  expect(updatedStrategyType).toBe("fill-first");
});
