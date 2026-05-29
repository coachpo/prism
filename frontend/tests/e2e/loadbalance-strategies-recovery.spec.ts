import { expect, test, type Locator } from "@playwright/test";

const timestamp = "2026-04-10T00:00:00Z";

function createCostingSettings() {
  return {
    report_currency_code: "USD",
    report_currency_symbol: "$",
    endpoint_fx_mappings: [],
    timezone_preference: null,
  };
}

function createStrategyRow({
  id,
  name,
  banMode,
  banDurationSeconds = 0,
  banCumulativeRetryAttemptThreshold = 0,
}: {
  id: number;
  name: string;
  banMode: "off" | "temporary" | "until_reset";
  banDurationSeconds?: number;
  banCumulativeRetryAttemptThreshold?: number;
}) {
  return {
    id,
    profile_id: 1,
    name,
    legacy_strategy_type: "single",
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

  await expect(recoveryLines).toHaveText(lines);
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

    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({
        profiles: [
          {
            id: 1,
            name: "Default",
            description: null,
            is_active: true,
            is_default: true,
            is_editable: true,
            version: 1,
            created_at: timestamp,
            deleted_at: null,
            updated_at: timestamp,
          },
        ],
        active_profile: {
          id: 1,
          name: "Default",
          description: null,
          is_active: true,
          is_default: true,
          is_editable: true,
          version: 1,
          created_at: timestamp,
          deleted_at: null,
          updated_at: timestamp,
        },
        profile_limits: { max_profiles: 5 },
      });
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

    if (pathname === "/api/vendors") {
      return fulfillJson([]);
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });

  await page.goto("/loadbalance-strategies");

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
