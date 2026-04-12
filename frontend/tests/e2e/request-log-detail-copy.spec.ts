import { expect, test, type BrowserContext, type Page } from "@playwright/test";

const timestamp = "2026-04-13T00:00:00Z";
const rawErrorDetail = JSON.stringify({
  error: {
    message: "Upstream request failed",
    type: "bad_gateway",
  },
});
const formattedErrorDetail = JSON.stringify(JSON.parse(rawErrorDetail), null, 2);
const auditHeaders = "content-type: application/json\r\nauthorization: Bearer [REDACTED]";
const auditRequestBody = "line-1\r\nline-2\r\nline-3";
const auditResponseBody = "event: response.created\r\ndata: {\"id\":\"resp_101\"}";

function createRequestLogDetail() {
  return {
    summary: {
      id: 101,
      created_at: timestamp,
      model_id: "gpt-4o-mini",
      resolved_target_model_id: null,
      api_family: "openai",
      vendor_id: 1,
      vendor_key: "openai",
      vendor_name: "OpenAI",
      status_code: 502,
      response_time_ms: 125,
      is_stream: false,
    },
    request: {
      request_path: "/v1/responses",
      ingress_request_id: "ingress-101",
      attempt_number: 1,
      provider_correlation_id: "provider-corr-101",
      proxy_api_key_id: null,
      proxy_api_key_name_snapshot: null,
      caller_user_agent: "Prism QA Browser",
      upstream_user_agent: "Prism QA Browser",
      caller_client_display: "Prism QA Browser",
      upstream_client_display: "Prism QA Browser",
      user_agent_overridden: false,
      error_detail: rawErrorDetail,
    },
    routing: {
      profile_id: 1,
      model_id: "gpt-4o-mini",
      resolved_target_model_id: null,
      api_family: "openai",
      vendor_id: 1,
      vendor_key: "openai",
      vendor_name: "OpenAI",
      endpoint_id: 1,
      connection_id: null,
      endpoint_base_url: "https://api.example.test",
      endpoint_description: "Primary endpoint",
    },
    usage: {
      input_tokens: 12,
      output_tokens: 8,
      total_tokens: 20,
      success_flag: false,
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

function createAuditListItem() {
  return {
    id: 201,
    request_log_id: 101,
    profile_id: 1,
    vendor_id: 1,
    model_id: "gpt-4o-mini",
    endpoint_id: 1,
    connection_id: null,
    endpoint_base_url: "https://api.example.test",
    endpoint_description: "Primary endpoint",
    request_method: "POST",
    request_url: "https://api.example.test/v1/responses",
    request_headers: auditHeaders,
    request_body_preview: auditRequestBody,
    response_status: 200,
    is_stream: false,
    duration_ms: 125,
    created_at: timestamp,
  };
}

function createAuditDetail() {
  return {
    id: 201,
    request_log_id: 101,
    profile_id: 1,
    vendor_id: 1,
    model_id: "gpt-4o-mini",
    endpoint_id: 1,
    connection_id: null,
    endpoint_base_url: "https://api.example.test",
    endpoint_description: "Primary endpoint",
    request_method: "POST",
    request_url: "https://api.example.test/v1/responses",
    request_headers: auditHeaders,
    request_body: auditRequestBody,
    response_status: 200,
    response_headers: "content-type: application/json",
    response_body: auditResponseBody,
    is_stream: false,
    duration_ms: 125,
    created_at: timestamp,
  };
}

async function readCopiedText(page: Page) {
  return page.evaluate(() => navigator.clipboard.readText());
}

async function mockRequestLogDetailRoutes(page: Page) {
  const detail = createRequestLogDetail();
  const auditListItem = createAuditListItem();
  const auditDetail = createAuditDetail();

  await page.route("**/*", async (route) => {
    const pathname = new URL(route.request().url()).pathname;
    const searchParams = new URL(route.request().url()).searchParams;

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

    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }

    if (pathname === "/api/stats/requests/101") {
      return fulfillJson(detail);
    }

    if (pathname === "/api/audit/logs") {
      return fulfillJson({
        items: searchParams.get("request_log_id") === "101" ? [auditListItem] : [],
        total: 1,
        limit: 20,
        offset: 0,
      });
    }

    if (pathname === "/api/audit/logs/201") {
      return fulfillJson(auditDetail);
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
}

async function openRequestLogDetail(page: Page, context: BrowserContext) {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await mockRequestLogDetailRoutes(page);
  await page.goto("/request-logs?request_id=101");
  await page.waitForLoadState("networkidle");

  const drawer = page.getByTestId("request-log-detail-sheet");

  await expect(drawer).toBeVisible({ timeout: 15000 });
  return drawer;
}

test.describe("request log detail copy regression", () => {
  test("overview error detail copy button writes the formatted block text", async ({ page, context }) => {
    const drawer = await openRequestLogDetail(page, context);

    await drawer.locator("pre:visible").evaluateAll((elements) => {
      elements.forEach((element, index) => {
        element.textContent = `mutated-overview-${index}`;
      });
    });

    await drawer.locator("button:visible").filter({ hasText: "Copy" }).click();

    await expect.poll(() => readCopiedText(page)).toBe(formattedErrorDetail);
  });

  test("audit payload copy buttons write their corresponding payload blocks", async ({ page, context }) => {
    const drawer = await openRequestLogDetail(page, context);

    await drawer.getByRole("tab", { name: "Audit" }).click();

    const copyButtons = drawer.locator("button:visible").filter({ hasText: "Copy" });
    const visiblePreBlocks = drawer.locator("pre:visible");

    await expect(copyButtons).toHaveCount(3);
    await expect(visiblePreBlocks).toHaveCount(4);

    await visiblePreBlocks.evaluateAll((elements) => {
      elements[1].textContent = "mutated-audit-headers";
      elements[2].textContent = "mutated-audit-request";
      elements[3].textContent = "mutated-audit-response";
    });

    await copyButtons.nth(0).click();
    await expect.poll(() => readCopiedText(page)).toBe(auditHeaders);

    await copyButtons.nth(1).click();
    await expect.poll(() => readCopiedText(page)).toBe(auditRequestBody);

    await copyButtons.nth(2).click();
    await expect.poll(() => readCopiedText(page)).toBe(auditResponseBody);
  });
});
