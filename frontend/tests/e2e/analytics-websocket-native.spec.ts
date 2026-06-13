import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-05-04T00:00:00Z";
const usageStatisticsStorageKey = "prism.statistics.usage-state";
const profile = {
  id: 1,
  name: "Websocket Profile",
  description: null,
  is_active: true,
  is_default: true,
  is_editable: true,
  version: 1,
  created_at: timestamp,
  deleted_at: null,
  updated_at: timestamp,
};

function createModel(modelId: string, displayName: string, id: number) {
  return {
    id,
    api_family: "openai",
    model_id: modelId,
    display_name: displayName,
    model_type: "proxy",
    proxy_targets: [],
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createSnapshot({
  costMicros,
  generatedAt,
  preset,
  totalRequests,
  totalTokens,
}: {
  costMicros: number;
  generatedAt: string;
  preset: "1h" | "6h" | "24h" | "7d" | "30d" | "all";
  totalRequests: number;
  totalTokens: number;
}) {
  return {
    generated_at: generatedAt,
    time_range: {
      preset,
      start_at: "2026-05-03T00:00:00Z",
      end_at: generatedAt,
    },
    currency: { code: "USD", symbol: "$" },
    overview: {
      total_requests: totalRequests,
      success_requests: totalRequests - 1,
      failed_requests: 1,
      success_rate: 97.6,
      total_tokens: totalTokens,
      input_tokens: Math.floor(totalTokens * 0.6),
      output_tokens: Math.floor(totalTokens * 0.35),
      cached_tokens: Math.floor(totalTokens * 0.03),
      reasoning_tokens: Math.floor(totalTokens * 0.02),
      average_rpm: 2.4,
      average_tpm: 128,
      total_cost_micros: costMicros,
      rolling_window_minutes: 30,
      rolling_request_count: 4,
      rolling_token_count: 900,
      rolling_rpm: 0.13,
      rolling_tpm: 30,
    },
    service_health: {
      availability_percentage: 99.1,
      request_count: totalRequests,
      success_count: totalRequests - 1,
      failed_count: 1,
      interval_minutes: 60,
      cells: [
        {
          bucket_start: "2026-05-03T00:00:00Z",
          request_count: totalRequests,
          success_count: totalRequests - 1,
          failed_count: 1,
          availability_percentage: 99.1,
          status: "ok",
        },
      ],
    },
    request_trends: {
      hourly: [
        {
          key: "all",
          label: "All requests",
          total_requests: totalRequests,
          points: [{ bucket_start: "2026-05-03T00:00:00Z", request_count: totalRequests, success_count: totalRequests - 1, failed_count: 1, rpm: 2.4 }],
        },
      ],
      daily: [
        {
          key: "all",
          label: "All requests",
          total_requests: totalRequests,
          points: [{ bucket_start: "2026-05-03T00:00:00Z", request_count: totalRequests, success_count: totalRequests - 1, failed_count: 1, rpm: 0.1 }],
        },
      ],
    },
    token_usage_trends: {
      hourly: [
        {
          key: "all",
          label: "All models",
          total_tokens: totalTokens,
          points: [{ bucket_start: "2026-05-03T00:00:00Z", total_tokens: totalTokens, input_tokens: Math.floor(totalTokens * 0.6), output_tokens: Math.floor(totalTokens * 0.35), cached_tokens: Math.floor(totalTokens * 0.03), reasoning_tokens: Math.floor(totalTokens * 0.02), tpm: 128 }],
        },
      ],
      daily: [
        {
          key: "all",
          label: "All models",
          total_tokens: totalTokens,
          points: [{ bucket_start: "2026-05-03T00:00:00Z", total_tokens: totalTokens, input_tokens: Math.floor(totalTokens * 0.6), output_tokens: Math.floor(totalTokens * 0.35), cached_tokens: Math.floor(totalTokens * 0.03), reasoning_tokens: Math.floor(totalTokens * 0.02), tpm: 5.3 }],
        },
      ],
    },
    token_type_breakdown: {
      hourly: [{ bucket_start: "2026-05-03T00:00:00Z", input_tokens: Math.floor(totalTokens * 0.6), output_tokens: Math.floor(totalTokens * 0.35), cached_tokens: Math.floor(totalTokens * 0.03), reasoning_tokens: Math.floor(totalTokens * 0.02) }],
      daily: [{ bucket_start: "2026-05-03T00:00:00Z", input_tokens: Math.floor(totalTokens * 0.6), output_tokens: Math.floor(totalTokens * 0.35), cached_tokens: Math.floor(totalTokens * 0.03), reasoning_tokens: Math.floor(totalTokens * 0.02) }],
    },
    cost_overview: {
      total_cost_micros: costMicros,
      priced_request_count: totalRequests,
      unpriced_request_count: 0,
      hourly: [{ bucket_start: "2026-05-03T00:00:00Z", total_cost_micros: costMicros }],
      daily: [{ bucket_start: "2026-05-03T00:00:00Z", total_cost_micros: costMicros }],
    },
    endpoint_statistics: [
      {
        endpoint_id: 10,
        endpoint_label: "Primary websocket endpoint",
        p50_ttft_ms: 111,
        p95_ttft_ms: 222,
        request_count: totalRequests,
        success_rate: 97.6,
        total_tokens: totalTokens,
        avg_output_rate_tps: 76.5,
        total_cost_micros: costMicros,
      },
    ],
    model_statistics: [
      {
        model_id: "gpt-websocket",
        model_label: "Websocket aggregate model",
        p50_ttft_ms: 111,
        p95_ttft_ms: 222,
        success_count: totalRequests - 1,
        failed_count: 1,
        priced_request_count: totalRequests,
        unpriced_request_count: 0,
        request_count: totalRequests,
        success_rate: 97.6,
        input_tokens: Math.floor(totalTokens * 0.6),
        output_tokens: Math.floor(totalTokens * 0.35),
        cached_tokens: Math.floor(totalTokens * 0.03),
        reasoning_tokens: Math.floor(totalTokens * 0.02),
        total_tokens: totalTokens,
        avg_output_rate_tps: 76.5,
        total_cost_micros: costMicros,
      },
    ],
    proxy_api_key_statistics: [],
  };
}

function createAnalyticsSnapshotMessage({
  costMicros,
  generatedAt,
  preset,
  profileId = 1,
  sequence,
  totalRequests,
  totalTokens,
}: {
  costMicros: number;
  generatedAt: string;
  preset: "1h" | "6h" | "24h" | "7d" | "30d" | "all";
  profileId?: number;
  sequence: number;
  totalRequests: number;
  totalTokens: number;
}) {
  return {
    type: "analytics.snapshot",
    channel: "analytics",
    profile_id: profileId,
    preset,
    sequence,
    generated_at: generatedAt,
    snapshot: createSnapshot({ costMicros, generatedAt, preset, totalRequests, totalTokens }),
    endpoint_model_statistics_by_endpoint_id: {
      "10": [
        {
          model_id: "gpt-websocket",
          model_label: "Embedded websocket model",
          p50_ttft_ms: 101,
          p95_ttft_ms: 202,
          success_count: totalRequests - 1,
          failed_count: 1,
          priced_request_count: totalRequests,
          unpriced_request_count: 0,
          request_count: totalRequests,
          success_rate: 97.6,
          input_tokens: Math.floor(totalTokens * 0.6),
          output_tokens: Math.floor(totalTokens * 0.35),
          cached_tokens: Math.floor(totalTokens * 0.03),
          reasoning_tokens: Math.floor(totalTokens * 0.02),
          total_tokens: totalTokens,
          avg_output_rate_tps: 76.5,
          total_cost_micros: costMicros,
        },
      ],
    },
  };
}

async function seedUsageStatisticsState(page: Page) {
  await page.addInitScript(
    ({ storageKey }) => {
      localStorage.setItem("prism.locale", "en");
      localStorage.setItem(
        storageKey,
        JSON.stringify({
          version: 1,
          state: {
            selectedTimeRange: "24h",
            selectedModelLines: [],
            chartGranularity: {
              costOverview: "hourly",
              requestTrends: "hourly",
              tokenTypeBreakdown: "hourly",
              tokenUsageTrends: "hourly",
            },
          },
        }),
      );
    },
    { storageKey: usageStatisticsStorageKey },
  );
}

async function mockBackendRoutes(page: Page, forbiddenRequests: string[]) {
  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname } = url;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) => route.fulfill({
      status,
      contentType: "application/json",
      body: JSON.stringify(body),
    });

    if (pathname === "/api/stats/usage-snapshot" || /^\/api\/stats\/endpoints\/\d+\/models$/.test(pathname)) {
      forbiddenRequests.push(pathname);
      return fulfillJson({ error: "forbidden analytics REST request" }, 500);
    }

    if (pathname === "/api/auth/status") {
      return fulfillJson({ auth_enabled: false });
    }

    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({ profiles: [profile], active_profile: profile, profile_limits: { max_profiles: 5 } });
    }

    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }

    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }

    if (pathname === "/api/models") {
      return fulfillJson([createModel("gpt-websocket", "Websocket aggregate model", 101)]);
    }

    if (pathname === "/api/loadbalance/strategies" || pathname === "/api/endpoints") {
      return fulfillJson([]);
    }

    return fulfillJson({}, 404);
  });
}

test.describe("websocket-native analytics", () => {
  test("renders analytics.snapshot updates without REST stats fanout", async ({ page }) => {
    const forbiddenRequests: string[] = [];
    const websocketMessages: unknown[] = [];
    const refreshMessages: unknown[] = [];
    let sendRefreshSnapshot: () => void = () => {
      throw new Error("refresh snapshot sender was not registered");
    };

    await seedUsageStatisticsState(page);
    await mockBackendRoutes(page, forbiddenRequests);

    await page.routeWebSocket("**/api/realtime/ws", async (ws) => {
      ws.send(JSON.stringify({ type: "authenticated", username: "test" }));
      ws.onMessage((data) => {
        const message = JSON.parse(String(data));
        websocketMessages.push(message);

        if (message.type === "subscribe" && message.channel === "analytics") {
          ws.send(JSON.stringify({ type: "subscribed", channel: "analytics", profile_id: message.profile_id, preset: message.preset }));

          if (message.preset === "24h") {
            ws.send(JSON.stringify(createAnalyticsSnapshotMessage({ costMicros: 420000, generatedAt: "2026-05-04T00:00:00Z", preset: "24h", sequence: 1, totalRequests: 42, totalTokens: 4200 })));
          }

          if (message.preset === "7d") {
            ws.send(JSON.stringify(createAnalyticsSnapshotMessage({ costMicros: 9990000, generatedAt: "2026-05-04T00:01:00Z", preset: "24h", sequence: 99, totalRequests: 999, totalTokens: 999000 })));
            ws.send(JSON.stringify(createAnalyticsSnapshotMessage({ costMicros: 90000, generatedAt: "2026-05-04T00:02:00Z", preset: "7d", sequence: 1, totalRequests: 9, totalTokens: 900 })));
          }
        }

        if (message.type === "refresh" && message.channel === "analytics") {
          refreshMessages.push(message);
          sendRefreshSnapshot = () => {
            ws.send(JSON.stringify(createAnalyticsSnapshotMessage({ costMicros: 640000, generatedAt: "2026-05-04T00:03:00Z", preset: message.preset, sequence: 2, totalRequests: 64, totalTokens: 6400 })));
          };
        }
      });
    });

    await page.goto("/observe?tab=analytics");

    await expect(page).toHaveURL(/\/observe\?tab=analytics$/);
    await expect(page.getByRole("tab", { name: "Analytics" })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByTestId("usage-controls-toolbar")).toBeVisible({ timeout: 15000 });
    await expect(page.getByTestId("usage-kpi-grid").getByText("42", { exact: true })).toBeVisible();
    await expect(page.getByTestId("usage-cost-summary-total")).toHaveText(/\$0\.42(?:\sUSD)?/);
    expect(forbiddenRequests).toEqual([]);

    await page.getByRole("button", { name: "#10 Primary websocket endpoint" }).click();
    await expect(page.getByTestId("statistics-endpoint-model-table-10").getByText("Embedded websocket model")).toBeVisible();
    expect(forbiddenRequests).toEqual([]);

    const refreshButton = page.getByRole("button", { name: "Refresh usage statistics" });
    await refreshButton.click();
    await expect(refreshButton).toBeDisabled();
    await expect.poll(() => refreshMessages[refreshMessages.length - 1]).toEqual({ type: "refresh", channel: "analytics", profile_id: 1, preset: "24h" });
    sendRefreshSnapshot();
    await expect(page.getByTestId("usage-kpi-grid").getByText("64", { exact: true })).toBeVisible();
    await expect(refreshButton).toBeEnabled();

    await page.getByRole("button", { name: "Last 7 Days" }).click();
    await expect.poll(() => websocketMessages.some((message) => {
      return typeof message === "object" && message !== null &&
        "type" in message && message.type === "subscribe" &&
        "channel" in message && message.channel === "analytics" &&
        "preset" in message && message.preset === "7d";
    })).toBe(true);
    await expect(page.getByTestId("usage-kpi-grid").getByText("9", { exact: true })).toBeVisible();
    await expect(page.getByText("999", { exact: true })).toHaveCount(0);

    expect(forbiddenRequests).toEqual([]);
  });
});
