import { expect, test, type BrowserContext, type Page } from "@playwright/test";

const timestamp = "2026-04-13T00:00:00Z";
const expectedFromTime = "2026-04-12T12:00:00.000Z";
const expectedToTime = "2026-04-13T12:00:00.000Z";
const redactedHeaders = "content-type: application/json\nauthorization: Bearer [REDACTED]";
const requestBody = "original request body\nline two";
const responseBody = "original response body\nline two";

type Scenario =
  | "full"
  | "metadata_only"
  | "disabled"
  | "missing_request"
  | "no_records"
  | "list_failure"
  | "detail_failure"
  | "invalid_created";

function scenarioConfig(scenario: Scenario) {
  return {
    auditCaptureBodiesAtRequest: scenario === "full" || scenario === "detail_failure" || scenario === "list_failure" || scenario === "invalid_created" || scenario === "no_records",
    auditEnabledAtRequest: scenario !== "disabled",
    createdAt: scenario === "invalid_created" ? "not-a-date" : timestamp,
    listFails: scenario === "list_failure",
    detailFails: scenario === "detail_failure",
    listItems: scenario === "no_records" ? [] : [201, 202],
    requestBody: scenario === "metadata_only" ? null : requestBody,
    requestBodyStored: scenario !== "metadata_only",
    responseBody: scenario === "metadata_only" ? null : responseBody,
    responseBodyStored: scenario !== "metadata_only",
  };
}

function createProfile() {
  return {
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
  };
}

function createRequestLogListItem() {
  return {
    id: 101,
    created_at: timestamp,
    model_id: "gpt-4o-mini",
    model_label: "GPT-4o mini",
    resolved_target_model_id: null,
    resolved_target_model_label: null,
    is_proxy_origin: false,
    caller_client_display: "Prism QA Browser",
    upstream_client_display: "Prism QA Browser",
    user_agent_overridden: false,
    api_family: "openai",
    vendor_id: 1,
    vendor_key: "openai",
    vendor_name: "OpenAI",
    endpoint_id: 1,
    endpoint_label: "Primary endpoint",
    connection_id: null,
    ttft_ms: null,
    completion_duration_ms: null,
    status_code: 200,
    response_time_ms: 125,
    is_stream: false,
    stream_outcome: "not_streaming",
    stream_error_kind: null,
    output_tokens: 8,
    total_tokens: 20,
    total_cost_user_currency_micros: 3000,
    priced_flag: true,
    unpriced_reason: null,
    report_currency_symbol: "$",
  };
}

function createRequestLogDetail(scenario: Scenario) {
  const config = scenarioConfig(scenario);
  return {
    summary: {
      id: 101,
      created_at: config.createdAt,
      model_id: "gpt-4o-mini",
      model_label: "GPT-4o mini",
      resolved_target_model_id: null,
      resolved_target_model_label: null,
      is_proxy_origin: false,
      api_family: "openai",
      vendor_id: 1,
      vendor_key: "openai",
      vendor_name: "OpenAI",
      status_code: 200,
      response_time_ms: 125,
      ttft_ms: null,
      completion_duration_ms: null,
      is_stream: false,
      stream_outcome: "not_streaming",
      stream_error_kind: null,
      stream_error_detail: null,
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
      request_generation_params: null,
      request_generation_params_status: null,
      error_detail: null,
    },
    routing: {
      profile_id: 1,
      endpoint_label: "Primary endpoint",
      endpoint_id: 1,
      terminal_target_id: null,
      selected_terminal_target_id: null,
      context_routing: null,
      endpoint_base_url: "https://api.example.test",
      endpoint_description: "Primary endpoint",
      audit_enabled_at_request: config.auditEnabledAtRequest,
      audit_capture_bodies_at_request: config.auditCaptureBodiesAtRequest,
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

function createAuditListItem(id: number, scenario: Scenario) {
  const config = scenarioConfig(scenario);
  return {
    id,
    request_log_id: 101,
    profile_id: 1,
    vendor_id: 1,
    model_id: "gpt-4o-mini",
    endpoint_id: 1,
    connection_id: null,
    endpoint_base_url: "https://api.example.test",
    endpoint_description: "Primary endpoint",
    request_method: "POST",
    request_url: `https://api.example.test/v1/responses?audit=${id}`,
    request_headers: redactedHeaders,
    request_body_preview: config.requestBody,
    request_body_stored: config.requestBodyStored,
    response_status: id === 202 ? 201 : 200,
    response_body_stored: config.responseBodyStored,
    is_stream: false,
    duration_ms: id === 202 ? 225 : 125,
    audit_enabled_at_request: config.auditEnabledAtRequest,
    audit_capture_bodies_at_request: config.auditCaptureBodiesAtRequest,
    created_at: timestamp,
  };
}
function createAuditDetail(id: number, scenario: Scenario) {
  const config = scenarioConfig(scenario);
  return {
    ...createAuditListItem(id, scenario),
    request_body: id === 202 ? "selected audit request body" : config.requestBody,
    response_headers: "content-type: application/json\nx-prism-audit: [REDACTED]",
    response_body: id === 202 ? "selected audit response body" : config.responseBody,
  };
}

async function installCopyHarness(page: Page, context: BrowserContext) {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: () => Promise.reject(new Error("clipboard unavailable")) },
      configurable: true,
    });
    (window as Window & { __copiedText?: string }).__copiedText = "";
    (window as Window & { __fallbackUsedDedicatedRoot?: boolean }).__fallbackUsedDedicatedRoot = false;
    const execCommandOverride = (command: string) => {
      if (command !== "copy") return false;
      const textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
      const usedDedicatedRoot = Boolean(textarea?.closest("[data-testid='dedicated-request-log-audit-page']"));
      (window as Window & { __fallbackUsedDedicatedRoot?: boolean }).__fallbackUsedDedicatedRoot = usedDedicatedRoot;
      (window as Window & { __copiedText?: string }).__copiedText = usedDedicatedRoot ? textarea?.value ?? "" : "";
      return usedDedicatedRoot;
    };
    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: execCommandOverride,
    });
  });
}

async function copiedText(page: Page) {
  return page.evaluate(() => (window as Window & { __copiedText?: string }).__copiedText ?? "");
}
async function usedDedicatedFallbackRoot(page: Page) {
  return page.evaluate(() => (window as Window & { __fallbackUsedDedicatedRoot?: boolean }).__fallbackUsedDedicatedRoot ?? false);
}

async function mockPrismRoutes(page: Page, scenario: Scenario) {
  const config = scenarioConfig(scenario);
  const auditListSearchParams: string[] = [];
  const auditDetailRequests: number[] = [];

  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    const { pathname, searchParams } = url;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") {
      return fulfillJson({ auth_enabled: false });
    }

    if (pathname === "/api/profiles/bootstrap") {
      const profile = createProfile();
      return fulfillJson({
        profiles: [profile],
        active_profile: profile,
        profile_limits: { max_profiles: 5 },
      });
    }

    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }

    if (pathname === "/api/stats/requests") {
      return fulfillJson({
        items: [createRequestLogListItem()],
        total: 1,
        limit: 100,
        offset: 0,
        filter_options: { models: [], endpoints: [] },
      });
    }

    if (pathname === "/api/stats/requests/101") {
      if (scenario === "missing_request") return fulfillJson({ detail: "missing" }, 404);
      return fulfillJson(createRequestLogDetail(scenario));
    }
    if (pathname === "/api/audit/logs") {
      auditListSearchParams.push(searchParams.toString());
      if (!config.auditEnabledAtRequest) return fulfillJson({ detail: "audit disabled" }, 409);
      if (config.listFails) return fulfillJson({ detail: "audit list failed" }, 500);
      const items = config.listItems.map((id) => createAuditListItem(id, scenario));
      return fulfillJson({
        items,
        next_cursor: null,
        has_more: false,
        window: { from: searchParams.get("from"), to: searchParams.get("to") },
        limit: 20,
        sort: "desc",
      });
    }

    if (pathname.startsWith("/api/audit/logs/")) {
      const pathSegments = pathname.split("/");
      const auditId = Number(pathSegments[pathSegments.length - 1]);
      auditDetailRequests.push(auditId);
      if (config.detailFails) return fulfillJson({ detail: "audit detail failed" }, 500);
      if (!config.listItems.includes(auditId)) return fulfillJson({ detail: "missing audit" }, 404);
      return fulfillJson(createAuditDetail(auditId, scenario));
    }

    return fulfillJson({}, 404);
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));

  return {
    auditDetailRequests,
    auditListSearchParams,
  };
}
function expectAuditWindow(searchParamString: string) {
  const params = new URLSearchParams(searchParamString);
  expect(params.get("request_log_id")).toBe("101");
  expect(params.get("from")).toBe(expectedFromTime);
  expect(params.get("to")).toBe(expectedToTime);
  expect(params.get("limit")).toBe("20");
}

test.describe("dedicated request-log audit page", () => {
  test("direct selected audit_id route fetches only the selected audit detail", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "full");

    await page.goto("/request-logs/101/audit?audit_id=202");

    await expect(page.getByTestId("dedicated-request-log-audit-page")).toBeVisible({ timeout: 15000 });
    await expect(page.getByTestId("shell-breadcrumb")).toContainText("Request Logs");
    await expect(page.getByTestId("shell-breadcrumb")).toContainText("#101");
    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText("Audit");
    await expect(page.getByText("selected audit request body")).toBeVisible();
    await expect(page.getByText("selected audit response body")).toBeVisible();
    await expect(page.getByText("original request body")).toHaveCount(0);
    expect(counters.auditListSearchParams).toHaveLength(1);
    expectAuditWindow(counters.auditListSearchParams[0]);
    expect(counters.auditDetailRequests).toEqual([202]);
  });

  test("disabled audit requests do not call audit APIs", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "disabled");

    await page.goto("/request-logs/101/audit");

    await expect(page.getByText("Audit disabled at request time").first()).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("metadata-only audit shows no-body copy state and copies original redacted headers", async ({ page, context }) => {
    await installCopyHarness(page, context);
    const counters = await mockPrismRoutes(page, "metadata_only");

    await page.goto("/request-logs/101/audit");

    await expect(page.getByText("Metadata only").first()).toBeVisible({ timeout: 15000 });
    await expect(page.getByText("Bearer [REDACTED]")).toBeVisible();
    await expect(page.getByText("Request body was intentionally not stored because this request used metadata-only audit capture.")).toBeVisible();
    await expect(page.getByText("Response body was intentionally not stored because this request used metadata-only audit capture.")).toBeVisible();
    const copyButtons = page.getByTestId("dedicated-audit-detail").getByRole("button", { name: /^Copy$/ });
    await expect(copyButtons).toHaveCount(4);
    await expect(copyButtons.nth(1)).toBeDisabled();
    await expect(copyButtons.nth(3)).toBeDisabled();
    await page.getByTestId("dedicated-audit-detail").locator("pre").nth(1).evaluate((element) => {
      element.textContent = "mutated header text";
    });
    await copyButtons.first().click();
    await expect.poll(() => copiedText(page)).toBe(redactedHeaders);
    await expect.poll(() => usedDedicatedFallbackRoot(page)).toBe(true);
    expect(counters.auditDetailRequests).toEqual([201]);
  });

  test("missing request state renders without audit calls", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "missing_request");

    await page.goto("/request-logs/101/audit");

    await expect(page.getByText("Request Not Found")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("no audit records state does not fetch audit details", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "no_records");

    await page.goto("/request-logs/101/audit");

    await expect(page.getByText("No audit records found for this request.")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toHaveLength(1);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("unmatched audit_id renders missing-audit state with a return action", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "full");

    await page.goto("/request-logs/101/audit?audit_id=999");

    await expect(page.getByText("Audit record not found for this request")).toBeVisible({ timeout: 15000 });
    await expect(page.getByRole("link", { name: "Show default audit record" })).toHaveAttribute("href", "/request-logs/101/audit");
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("audit list failure does not fetch audit details", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "list_failure");

    await page.goto("/request-logs/101/audit");

    await expect(page.getByText("Audit records load failed")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toHaveLength(1);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("audit detail failure preserves the audit list", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "detail_failure");

    await page.goto("/request-logs/101/audit?audit_id=201");

    await expect(page.getByText("Audit record load failed")).toBeVisible({ timeout: 15000 });
    await expect(page.getByTestId("dedicated-audit-list")).toContainText("#201");
    expect(counters.auditDetailRequests).toEqual([201]);
  });

  test("invalid request timestamp prevents audit lookup", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "invalid_created");

    await page.goto("/request-logs/101/audit");

    await expect(page.getByText("Invalid request timestamp")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("request-log row clicks still open the overview drawer, not the dedicated audit route", async ({ page }) => {
    await mockPrismRoutes(page, "full");

    await page.goto("/request-logs");
    const requestLogRow = page.getByTestId("request-logs-table").getByRole("button").filter({ hasText: "GPT-4o mini" });
    await requestLogRow.click();

    const drawer = page.getByTestId("request-log-detail-sheet");
    await expect(drawer).toBeVisible({ timeout: 15000 });
    await expect(drawer.getByRole("tab", { name: "Overview" })).toHaveAttribute("data-state", "active");
    await expect(page).toHaveURL(/\/request-logs$/);
    await drawer.getByRole("tab", { name: "Audit" }).click();
    await expect(drawer.getByRole("link", { name: "Open full audit page" })).toHaveAttribute("href", "/request-logs/101/audit");
  });
});
