import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-10T00:00:00Z";
const rawProxyKey = "sk-live-secret";

function createRequestLogDetail({
  proxyApiKeyId = 2,
  proxyApiKeyNameSnapshot = "Team A key",
}: {
  proxyApiKeyId?: number | null;
  proxyApiKeyNameSnapshot?: string | null;
} = {}) {
  return {
    summary: {
      id: 101,
      created_at: timestamp,
      model_id: "gpt-4o-mini",
      model_label: "GPT-4o mini",
      resolved_target_model_id: null,
      resolved_target_model_label: null,
      is_proxy_origin: false,
      api_family: "openai",
      status_code: 200,
      response_time_ms: 125,
      is_stream: false,
    },
    request: {
      request_path: "/v1/chat/completions",
      ingress_request_id: "ingress-101",
      attempt_number: 1,
      provider_correlation_id: "provider-corr-101",
      proxy_api_key_id: proxyApiKeyId,
      proxy_api_key_name_snapshot: proxyApiKeyNameSnapshot,
      caller_user_agent: "Prism QA Browser",
      upstream_user_agent: "Prism QA Browser",
      caller_client_display: "Prism QA Browser",
      upstream_client_display: "Prism QA Browser",
      user_agent_overridden: false,
      error_detail: null,
    },
    routing: {
      profile_id: 1,
      endpoint_label: "Not recorded",
      endpoint_id: null,
      connection_id: null,
      endpoint_base_url: null,
      endpoint_description: null,
      audit_enabled_at_request: null,
    },
    usage: {
      input_tokens: 12,
      output_tokens: 8,
      total_tokens: 20,
      success_flag: true,
      billable_flag: true,
      priced_flag: true,
      unpriced_reason: null,
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 0,
      reasoning_tokens: 0,
    },
    costing: {
      input_cost_micros: 1000,
      output_cost_micros: 2000,
      cache_read_input_cost_micros: 0,
      cache_creation_input_cost_micros: 0,
      reasoning_cost_micros: 0,
      total_cost_original_micros: 3000,
      total_cost_user_currency_micros: 3000,
      currency_code_original: "USD",
      report_currency_code: "USD",
      report_currency_symbol: "$",
      fx_rate_used: "1",
      fx_rate_source: "manual",
    },
    pricing: {
      pricing_snapshot_unit: "1M tokens",
      pricing_snapshot_input: "0.10",
      pricing_snapshot_output: "0.20",
      pricing_snapshot_cache_read_input: null,
      pricing_snapshot_cache_creation_input: null,
      pricing_snapshot_reasoning: null,
      pricing_config_version_used: 1,
    },
  };
}

async function mockRequestLogDetailRoutes(
  page: Page,
  detail: ReturnType<typeof createRequestLogDetail>,
) {
  await page.route("**/*", async (route) => {
    const pathname = new URL(route.request().url()).pathname;

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

    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }

    if (pathname === "/api/stats/requests/101") {
      return fulfillJson(detail);
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });
}

async function openRequestLogDetail(
  page: Page,
  locale: "en" | "zh-CN",
  detail: ReturnType<typeof createRequestLogDetail>,
) {
  await mockRequestLogDetailRoutes(page, detail);
  await page.addInitScript((seedLocale) => localStorage.setItem("prism.locale", seedLocale), locale);

  await page.goto("/observe/requests?request_id=101");

  const drawer = page.getByTestId("request-log-detail-sheet");
  const overview = page.getByTestId("request-log-overview-grid");

  await expect(drawer).toBeVisible();
  await expect(overview).toBeVisible();
  await expect(drawer).not.toContainText(rawProxyKey);

  return { drawer, overview };
}

test.describe("request log proxy API key detail regression", () => {
  test("renders English snapshot-backed proxy API key labels", async ({ page }) => {
    const { overview } = await openRequestLogDetail(page, "en", createRequestLogDetail());

    await expect(overview).toContainText("Proxy API key");
    await expect(overview).toContainText("Team A key");
  });

  test("renders English not-recorded fallback when snapshot and id are null", async ({ page }) => {
    const { overview } = await openRequestLogDetail(
      page,
      "en",
      createRequestLogDetail({ proxyApiKeyId: null, proxyApiKeyNameSnapshot: null }),
    );

    await expect(overview).toContainText("Proxy API key");
    await expect(overview).toContainText("Not recorded");
  });

  test("renders English snapshot label even when proxy API key id is null", async ({ page }) => {
    const { overview } = await openRequestLogDetail(
      page,
      "en",
      createRequestLogDetail({ proxyApiKeyId: null, proxyApiKeyNameSnapshot: "Team A key" }),
    );

    await expect(overview).toContainText("Proxy API key");
    await expect(overview).toContainText("Team A key");
  });

  test("renders Chinese not-recorded fallback when snapshot and id are null", async ({ page }) => {
    const { overview } = await openRequestLogDetail(
      page,
      "zh-CN",
      createRequestLogDetail({ proxyApiKeyId: null, proxyApiKeyNameSnapshot: null }),
    );

    await expect(overview).toContainText("代理 API 密钥");
    await expect(overview).toContainText("未记录");
  });
});
