import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-11T00:00:00Z";
const modelOptionLabel = "Bootstrap Filter Model";
const endpointOptionLabel = "Bootstrap Filter Endpoint";
const clientOptionLabel = "Codex CLI";
const finalTargetModelOptionLabel = "Terminal Model";

function createRequestLogItem(overrides: Record<string, unknown> = {}) {
  return {
    id: 101,
    created_at: timestamp,
    model_id: "gpt-4o-mini",
    model_label: modelOptionLabel,
    resolved_target_model_id: null,
    resolved_target_model_label: null,
    is_proxy_origin: false,
    caller_client_display: "Browse Fixture Row",
    upstream_client_display: "Browse Fixture Row",
    user_agent_overridden: false,
    api_family: "openai",
    endpoint_id: 1,
    endpoint_label: endpointOptionLabel,
    connection_id: null,
    ttft_ms: 95,
    completion_duration_ms: 280,
    status_code: 200,
    response_time_ms: 1250,
    is_stream: true,
    output_tokens: 150,
    total_tokens: 200,
    total_cost_user_currency_micros: 750000,
    report_currency_symbol: "$",
    ...overrides,
  };
}

function createRequestLogDetail() {
  return {
    summary: {
      id: 101,
      created_at: timestamp,
      model_id: "gpt-4o-mini",
      model_label: modelOptionLabel,
      resolved_target_model_id: "terminal-model",
      resolved_target_model_label: finalTargetModelOptionLabel,
      api_family: "openai",
      status_code: 200,
      response_time_ms: 1250,
      ttft_ms: 95,
      completion_duration_ms: 280,
      is_stream: true,
      stream_outcome: "completed",
      stream_error_kind: null,
      stream_error_detail: null,
    },
    request: {
      request_path: "/v1/responses",
      ingress_request_id: "ingress-101",
      attempt_number: 1,
      provider_correlation_id: null,
      proxy_api_key_id: null,
      proxy_api_key_name_snapshot: null,
      caller_user_agent: "codex-cli/1.0",
      upstream_user_agent: "codex-cli/1.0",
      caller_client_display: clientOptionLabel,
      upstream_client_display: clientOptionLabel,
      user_agent_overridden: false,
      request_generation_params: null,
      request_generation_params_status: null,
      error_detail: null,
    },
    routing: {
      profile_id: 1,
      endpoint_label: endpointOptionLabel,
      endpoint_id: 1,
      terminal_target_id: null,
      selected_terminal_target_id: null,
      endpoint_base_url: null,
      endpoint_description: null,
      audit_enabled_at_request: false,
      audit_capture_bodies_at_request: false,
    },
    usage: {
      input_tokens: 50,
      output_tokens: 150,
      total_tokens: 200,
      success_flag: true,
      billable_flag: true,
      priced_flag: true,
      unpriced_reason: null,
      cache_read_input_tokens: null,
      cache_creation_input_tokens: null,
      reasoning_tokens: null,
    },
    costing: {
      input_cost_micros: null,
      output_cost_micros: null,
      cache_read_input_cost_micros: null,
      cache_creation_input_cost_micros: null,
      reasoning_cost_micros: null,
      total_cost_original_micros: null,
      total_cost_user_currency_micros: 750000,
      currency_code_original: "USD",
      report_currency_code: "USD",
      report_currency_symbol: "$",
      fx_rate_used: null,
      fx_rate_source: null,
    },
    pricing: {
      pricing_snapshot_unit: null,
      pricing_snapshot_input: null,
      pricing_snapshot_output: null,
      pricing_snapshot_cache_read_input: null,
      pricing_snapshot_cache_creation_input: null,
      pricing_snapshot_reasoning: null,
      pricing_config_version_used: null,
    },
  };
}

function createRequestLogsResponse(
  requestLogItems: Record<string, unknown>[],
  searchParams: URLSearchParams,
  modelOptions: Record<string, unknown>[] = [
    {
      model_id: "gpt-4o-mini",
      model_label: modelOptionLabel,
    },
  ],
  clientOptions: Record<string, unknown>[] = [
    {
      client_rule_id: 123,
      client_label: clientOptionLabel,
    },
  ],
  resolvedTargetModelOptions: Record<string, unknown>[] = [
    {
      resolved_target_model_id: "terminal-model",
      model_label: finalTargetModelOptionLabel,
    },
  ],
) {
  const limit = Number.parseInt(
    searchParams.get("limit") ?? String(requestLogItems.length),
    10,
  );
  const offset = Number.parseInt(searchParams.get("offset") ?? "0", 10);

  return {
    items: requestLogItems.slice(offset, offset + limit),
    total: requestLogItems.length,
    limit,
    offset,
    filter_options: {
      models: modelOptions,
      endpoints: [
        {
          endpoint_id: 1,
          endpoint_label: endpointOptionLabel,
        },
      ],
      clients: clientOptions,
      resolved_target_models: resolvedTargetModelOptions,
    },
  };
}

interface MockRouteState {
  failRequestLogs: boolean;
  modelOptions?: Record<string, unknown>[];
  clientOptions?: Record<string, unknown>[];
  resolvedTargetModelOptions?: Record<string, unknown>[];
}

async function mockRequestLogRoutes(
  page: Page,
  state: MockRouteState,
) {
  const requestLogItems = [createRequestLogItem()];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname, searchParams } = url;

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

    if (pathname === "/api/settings/costing") {
      return fulfillJson({
        report_currency_code: "USD",
        report_currency_symbol: "$",
        endpoint_fx_mappings: [],
        timezone_preference: null,
      });
    }

    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }


    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson([]);
    }

    if (pathname === "/api/endpoints") {
      throw new Error(
        "Unexpected /api/endpoints request during request-logs browse mode",
      );
    }

    if (pathname === "/api/models") {
      throw new Error(
        "Unexpected /api/models request during request-logs browse mode",
      );
    }

    if (pathname === "/api/stats/requests/101") {
      return fulfillJson(createRequestLogDetail());
    }

    if (pathname === "/api/stats/requests") {
      if (state.failRequestLogs) {
        return fulfillJson({ detail: "Failed to load request logs" }, 500);
      }

      const filteredItems = searchParams.has("client_rule_id") || searchParams.has("resolved_target_model_id")
        ? [
            createRequestLogItem({
              id: 202,
              caller_client_display: "Server Filtered Row",
              upstream_client_display: "Server Filtered Row",
              resolved_target_model_id: "terminal-model",
              resolved_target_model_label: finalTargetModelOptionLabel,
            }),
          ]
        : requestLogItems;

      return fulfillJson(
        createRequestLogsResponse(
          filteredItems,
          searchParams,
          state.modelOptions,
          state.clientOptions,
          state.resolvedTargetModelOptions,
        ),
      );
    }

    return fulfillJson({}, 404);
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
}

test.describe("request logs filter option loading", () => {
  test("keeps filter controls unloaded until the request-log payload has returned filter options once", async ({
    page,
  }) => {
    const routeState: MockRouteState = {
      failRequestLogs: true,
    };

    await mockRequestLogRoutes(page, routeState);

    await page.goto("/observe/requests");

    await expect(page.getByText("Failed to load request logs")).toBeVisible();

    await page.getByText("All models", { exact: true }).click();
    await expect(page.getByRole("option", { name: modelOptionLabel })).toHaveCount(0);
    await page.keyboard.press("Escape");

    await page.getByText("All endpoints", { exact: true }).click();
    await expect(page.getByRole("option", { name: endpointOptionLabel })).toHaveCount(0);
    await page.keyboard.press("Escape");

    routeState.failRequestLogs = false;
    await page.getByRole("button", { name: "Refresh request logs" }).click();
    await expect(page.getByText("Browse Fixture Row")).toBeVisible();

    await page.getByText("All models", { exact: true }).click();
    await expect(page.getByRole("option", { name: modelOptionLabel })).toBeVisible();
    await page.keyboard.press("Escape");

    await page.getByText("All endpoints", { exact: true }).click();
    await expect(page.getByRole("option", { name: endpointOptionLabel })).toBeVisible();
  });

  test("retains loaded filter controls after later request-list failures", async ({ page }) => {
    const routeState: MockRouteState = {
      failRequestLogs: false,
    };

    await mockRequestLogRoutes(page, routeState);

    await page.goto("/observe/requests");

    await page.getByText("All models", { exact: true }).click();
    await expect(page.getByRole("option", { name: modelOptionLabel })).toBeVisible();
    await page.keyboard.press("Escape");

    await page.getByText("All endpoints", { exact: true }).click();
    await expect(page.getByRole("option", { name: endpointOptionLabel })).toBeVisible();
    await page.keyboard.press("Escape");

    routeState.failRequestLogs = true;
    await page.getByRole("button", { name: "Refresh request logs" }).click();

    await expect(page.getByText("Failed to load request logs")).toBeVisible();

    await page.getByText("All models", { exact: true }).click();
    await expect(page.getByRole("option", { name: modelOptionLabel })).toBeVisible();
    await page.keyboard.press("Escape");

    await page.getByText("All endpoints", { exact: true }).click();
    await expect(page.getByRole("option", { name: endpointOptionLabel })).toBeVisible();
  });

  test("accepts empty model filter arrays from the request-log payload", async ({ page }) => {
    const routeState: MockRouteState = {
      failRequestLogs: false,
      modelOptions: [],
    };

    await mockRequestLogRoutes(page, routeState);

    await page.goto("/observe/requests");
    await expect(page.getByText("Browse Fixture Row")).toBeVisible();

    await page.getByText("All models", { exact: true }).click();
    await expect(page.getByRole("option", { name: modelOptionLabel })).toHaveCount(0);
    await page.keyboard.press("Escape");

    await page.getByText("All endpoints", { exact: true }).click();
    await expect(page.getByRole("option", { name: endpointOptionLabel })).toBeVisible();
  });

  test("selecting Client and final target model filters updates URL and stats requests", async ({ page }) => {
    const requestUrls: string[] = [];
    await mockRequestLogRoutes(page, { failRequestLogs: false });
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/stats/requests") {
        requestUrls.push(request.url());
      }
    });

    await page.goto("/observe/requests");
    await expect(page.getByText("Browse Fixture Row")).toBeVisible();

    const clientRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === "/api/stats/requests" && url.searchParams.get("client_rule_id") === "123";
    });
    await page.getByRole("combobox", { name: "Client" }).click();
    await page.getByRole("option", { name: clientOptionLabel }).click();
    const clientRequestUrl = new URL((await clientRequest).url());

    await expect(page).toHaveURL(/client_rule_id=123/);
    expect(clientRequestUrl.searchParams.get("client_rule_id")).toBe("123");
    expect(clientRequestUrl.searchParams.has("client_scope")).toBe(false);
    await expect(page.getByText("Server Filtered Row")).toBeVisible();

    const targetRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === "/api/stats/requests"
        && url.searchParams.get("client_rule_id") === "123"
        && url.searchParams.get("resolved_target_model_id") === "terminal-model";
    });
    await page.getByRole("combobox", { name: "Final Target Model" }).click();
    await page.getByRole("option", { name: finalTargetModelOptionLabel }).click();
    const targetRequestUrl = new URL((await targetRequest).url());

    await expect(page).toHaveURL(/resolved_target_model_id=terminal-model/);
    expect(targetRequestUrl.searchParams.get("resolved_target_model_id")).toBe("terminal-model");
    expect(requestUrls.some((url) => url.includes("client_scope"))).toBe(false);
    await page.screenshot({ path: "../artifacts/evidence/task-14-client-dropdown.png", fullPage: true });
  });

  test("clear filters removes client and final target browse filters", async ({ page }) => {
    await mockRequestLogRoutes(page, { failRequestLogs: false });
    await page.goto("/observe/requests?client_rule_id=123&resolved_target_model_id=terminal-model");
    await expect(page.getByText("Server Filtered Row")).toBeVisible();

    const clearRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === "/api/stats/requests"
        && !url.searchParams.has("client_rule_id")
        && !url.searchParams.has("resolved_target_model_id");
    });
    await page.getByRole("button", { name: /Clear Filters/i }).click();
    await clearRequest;

    await expect(page).not.toHaveURL(/client_rule_id=/);
    await expect(page).not.toHaveURL(/resolved_target_model_id=/);
    await expect(page.getByText("Browse Fixture Row")).toBeVisible();
  });

  test("exact request mode preserves request_id with browse filters present", async ({ page }) => {
    const statsRequests: string[] = [];
    await mockRequestLogRoutes(page, { failRequestLogs: false });
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/stats/requests") {
        statsRequests.push(request.url());
      }
    });

    await page.goto("/observe/requests?request_id=101&client_rule_id=123&resolved_target_model_id=terminal-model");
    await expect(page.getByTestId("request-log-detail-sheet")).toBeVisible({ timeout: 15000 });

    await expect(page).toHaveURL(/request_id=101/);
    expect(statsRequests).toEqual([]);
    await page.screenshot({ path: "../artifacts/evidence/task-14-exact-mode.png", fullPage: true });
  });
});
