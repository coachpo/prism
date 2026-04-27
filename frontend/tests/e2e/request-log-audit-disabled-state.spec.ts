import { expect, test, type Locator, type Page } from "@playwright/test";

const timestamp = "2026-04-13T00:00:00Z";

function createRequestLogDetail(auditEnabledAtRequest: boolean) {
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
      error_detail: null,
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
      audit_enabled_at_request: auditEnabledAtRequest,
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
    request_headers: "content-type: application/json\nauthorization: Bearer [REDACTED]",
    request_body_preview: '{"input":"hello"}',
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
    request_headers: "content-type: application/json\nauthorization: Bearer [REDACTED]",
    request_body: '{"input":"hello"}',
    response_status: 200,
    response_headers: "content-type: application/json",
    response_body: '{"id":"resp_101","status":"ok"}',
    is_stream: false,
    duration_ms: 125,
    created_at: timestamp,
  };
}

async function mockRequestLogDetailRoutes(page: Page, auditEnabledAtRequest: boolean) {
  const detail = createRequestLogDetail(auditEnabledAtRequest);
  const auditListItem = createAuditListItem();
  const auditDetail = createAuditDetail();
  let auditListRequests = 0;
  let auditDetailRequests = 0;

  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
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
      auditListRequests += 1;
      if (!auditEnabledAtRequest) {
        return fulfillJson({ detail: "Audit capture unavailable for this request" }, 409);
      }
      return fulfillJson({
        items: searchParams.get("request_log_id") === "101" ? [auditListItem] : [],
        total: searchParams.get("request_log_id") === "101" ? 1 : 0,
        limit: 20,
        offset: 0,
      });
    }

    if (pathname === "/api/audit/logs/201") {
      auditDetailRequests += 1;
      return fulfillJson(auditDetail);
    }

    if (pathname.startsWith("/api/audit/logs/")) {
      auditDetailRequests += 1;
      return fulfillJson({}, 404);
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));

  return {
    getAuditListRequests: () => auditListRequests,
    getAuditDetailRequests: () => auditDetailRequests,
  };
}

async function openRequestLogDetail(
  page: Page,
  auditEnabledAtRequest: boolean,
  initialPath = "/request-logs?request_id=101",
) {
  const counters = await mockRequestLogDetailRoutes(page, auditEnabledAtRequest);

  await page.goto(initialPath);

  const drawer = page.getByTestId("request-log-detail-sheet");
  await expect(drawer).toBeVisible({ timeout: 15000 });

  return { drawer, counters };
}

async function expectExactAuditUrlContract(page: Page, drawer: Locator) {
  const overviewTab = drawer.getByRole("tab", { name: "Overview" });
  const auditTab = drawer.getByRole("tab", { name: "Audit" });

  await expect(page).toHaveURL(/\/request-logs\?request_id=101&detail_tab=audit$/);
  await expect(auditTab).toHaveAttribute("data-state", "active");

  await Promise.all([
    page.waitForURL(/\/request-logs\?request_id=101$/),
    overviewTab.click(),
  ]);
  await expect(overviewTab).toHaveAttribute("data-state", "active");

  await Promise.all([
    page.waitForURL(/\/request-logs\?request_id=101&detail_tab=audit$/),
    auditTab.click(),
  ]);
  await expect(auditTab).toHaveAttribute("data-state", "active");
}

test.describe("request log audit disabled state", () => {
  test("audit deeplink keeps exact-mode URL state while disabled snapshots make zero audit API requests", async ({ page }) => {
    const { drawer, counters } = await openRequestLogDetail(
      page,
      false,
      "/request-logs?request_id=101&detail_tab=audit",
    );

    await expect(drawer.getByText("Audit capture unavailable")).toBeVisible();
    await expect(drawer.getByText("Audit logging may be disabled for this vendor.")).toBeVisible();
    expect(counters.getAuditListRequests()).toBe(0);
    expect(counters.getAuditDetailRequests()).toBe(0);

    await expectExactAuditUrlContract(page, drawer);
  });

  test("audit deeplink keeps exact-mode URL state while enabled snapshots lazy-load audit logs", async ({ page }) => {
    const { drawer, counters } = await openRequestLogDetail(
      page,
      true,
      "/request-logs?request_id=101&detail_tab=audit",
    );

    await expect.poll(() => counters.getAuditListRequests()).toBeGreaterThan(0);
    await expect.poll(() => counters.getAuditDetailRequests()).toBeGreaterThan(0);
    await expect(drawer.getByText("Audit capture unavailable")).not.toBeVisible();
    await expect(drawer.getByText('{"input":"hello"}')).toBeVisible();
    await expect(drawer.getByText('{"id":"resp_101","status":"ok"}')).toBeVisible();

    await expectExactAuditUrlContract(page, drawer);
  });
});
