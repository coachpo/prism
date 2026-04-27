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
  strategyType,
  recovery,
}: {
  id: number;
  name: string;
} &
  (
    | {
        strategyType: "adaptive";
        recovery: {
          routing_policy: {
            routing_objective: "maximize_availability" | "minimize_latency";
            ban_mode: "off" | "manual" | "temporary";
            max_open_strikes_before_ban: number;
            ban_duration_seconds: number;
            failure_status_codes: number[];
            base_open_seconds: number;
            max_open_seconds: number;
          };
        };
      }
    | {
        strategyType: "legacy";
        recovery: {
          auto_recovery: {
            mode: "enabled";
            status_codes: number[];
            cooldown: {
              base_seconds: number;
              max_cooldown_seconds: number;
            };
            ban:
              | { mode: "off" }
              | { mode: "manual"; max_cooldown_strikes_before_ban: number }
              | {
                  mode: "temporary";
                  max_cooldown_strikes_before_ban: number;
                  ban_duration_seconds: number;
                };
          };
        };
      }
  )) {
  return {
    id,
    profile_id: 1,
    name,
    strategy_type: strategyType,
    attached_model_count: 0,
    created_at: timestamp,
    updated_at: timestamp,
    ...(strategyType === "adaptive"
      ? {
          routing_policy: {
            kind: "adaptive",
            routing_objective: recovery.routing_policy.routing_objective,
            hedge: {
              enabled: false,
              delay_ms: 1500,
              max_additional_attempts: 1,
            },
            circuit_breaker: {
              failure_status_codes: recovery.routing_policy.failure_status_codes,
              base_open_seconds: recovery.routing_policy.base_open_seconds,
              failure_threshold: 2,
              backoff_multiplier: 2,
              max_open_seconds: recovery.routing_policy.max_open_seconds,
              ban_mode: recovery.routing_policy.ban_mode,
              max_open_strikes_before_ban: recovery.routing_policy.max_open_strikes_before_ban,
              ban_duration_seconds: recovery.routing_policy.ban_duration_seconds,
            },
            admission: {
              respect_qps_limit: true,
              respect_in_flight_limits: true,
            },
          },
        }
      : {
          legacy_strategy_type: "single",
          auto_recovery: {
            mode: "enabled",
            status_codes: recovery.auto_recovery.status_codes,
            cooldown: {
              base_seconds: recovery.auto_recovery.cooldown.base_seconds,
              failure_threshold: 2,
              backoff_multiplier: 2,
              max_cooldown_seconds: recovery.auto_recovery.cooldown.max_cooldown_seconds,
            },
            ban: recovery.auto_recovery.ban,
          },
        }),
  };
}

async function expectRecoveryLines(row: Locator, lines: string[]) {
  const recoveryLines = row.locator("td").nth(2).locator("span");

  await expect(recoveryLines).toHaveText(lines);
}

test("shows adaptive and legacy recovery rows by name", async ({ page }) => {
  const statusCodes = [403, 422, 429, 500, 502, 503, 504, 529];

  const strategies = [
    createStrategyRow({
      id: 1,
      name: "Adaptive Off",
      strategyType: "adaptive",
      recovery: {
        routing_policy: {
          routing_objective: "minimize_latency",
          failure_status_codes: statusCodes,
          base_open_seconds: 60,
          max_open_seconds: 900,
          ban_mode: "off",
          max_open_strikes_before_ban: 0,
          ban_duration_seconds: 0,
        },
      },
    }),
    createStrategyRow({
      id: 2,
      name: "Adaptive Manual",
      strategyType: "adaptive",
      recovery: {
        routing_policy: {
          routing_objective: "maximize_availability",
          failure_status_codes: statusCodes,
          base_open_seconds: 60,
          max_open_seconds: 900,
          ban_mode: "manual",
          max_open_strikes_before_ban: 1,
          ban_duration_seconds: 0,
        },
      },
    }),
    createStrategyRow({
      id: 3,
      name: "Adaptive Temporary",
      strategyType: "adaptive",
      recovery: {
        routing_policy: {
          routing_objective: "minimize_latency",
          failure_status_codes: statusCodes,
          base_open_seconds: 60,
          max_open_seconds: 900,
          ban_mode: "temporary",
          max_open_strikes_before_ban: 1,
          ban_duration_seconds: 28800,
        },
      },
    }),
    createStrategyRow({
      id: 4,
      name: "Legacy Manual",
      strategyType: "legacy",
      recovery: {
        auto_recovery: {
          mode: "enabled",
          status_codes: statusCodes,
          cooldown: {
            base_seconds: 60,
            max_cooldown_seconds: 900,
          },
          ban: { mode: "manual", max_cooldown_strikes_before_ban: 1 },
        },
      },
    }),
    createStrategyRow({
      id: 5,
      name: "Legacy Temporary",
      strategyType: "legacy",
      recovery: {
        auto_recovery: {
          mode: "enabled",
          status_codes: statusCodes,
          cooldown: {
            base_seconds: 60,
            max_cooldown_seconds: 900,
          },
          ban: {
            mode: "temporary",
            max_cooldown_strikes_before_ban: 1,
            ban_duration_seconds: 28800,
          },
        },
      },
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

  await expect(page.getByRole("table")).toContainText("Adaptive Off");
  await expect(page.getByRole("table")).toContainText("Adaptive Manual");
  await expect(page.getByRole("table")).toContainText("Adaptive Temporary");
  await expect(page.getByRole("table")).toContainText("Legacy Manual");
  await expect(page.getByRole("table")).toContainText("Legacy Temporary");

  await expectRecoveryLines(page.getByRole("row", { name: /Adaptive Off/ }), [
    "Routing policy Minimize latency",
    "Status codes 403, 422, 429, 500, 502, 503, 504, 529",
    "Open window 60s base • 900s max",
    "Ban off",
  ]);

  await expectRecoveryLines(page.getByRole("row", { name: /Adaptive Manual/ }), [
    "Routing policy Maximize availability",
    "Status codes 403, 422, 429, 500, 502, 503, 504, 529",
    "Open window 60s base • 900s max",
    "Manual dismiss after 1 max-open strikes",
  ]);

  await expectRecoveryLines(page.getByRole("row", { name: /Adaptive Temporary/ }), [
    "Routing policy Minimize latency",
    "Status codes 403, 422, 429, 500, 502, 503, 504, 529",
    "Open window 60s base • 900s max",
    "Temporary ban after 1 max-open strikes • 28,800s",
  ]);

  await expectRecoveryLines(page.getByRole("row", { name: /Legacy Manual/ }), [
    "Auto recovery enabled",
    "Status codes 403, 422, 429, 500, 502, 503, 504, 529",
    "Cooldown 60s base • 900s max",
    "Ban manual dismiss after 1 max-cooldown strikes",
  ]);

  await expectRecoveryLines(page.getByRole("row", { name: /Legacy Temporary/ }), [
    "Auto recovery enabled",
    "Status codes 403, 422, 429, 500, 502, 503, 504, 529",
    "Cooldown 60s base • 900s max",
    "Temporary ban after 1 max-cooldown strikes • 28,800s",
  ]);
});
