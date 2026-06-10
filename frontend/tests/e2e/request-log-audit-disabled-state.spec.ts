import { expect, test, type Locator, type Page } from "@playwright/test";

const timestamp = "2026-04-13T00:00:00Z";
const expectedAuditFromTime = "2026-04-12T12:00:00.000Z";
const expectedAuditToTime = "2026-04-13T12:00:00.000Z";
const alternateTimestamp = "2026-04-14T06:30:00Z";
const streamingSseResponseBody = 'event: message\ndata: {"id":"resp_stream","delta":"hel"}\n\nevent: message\ndata: {"id":"resp_stream","delta":"lo"}\n\nevent: done\ndata: [DONE]\n\n';
const openAiSheetRequestBody = JSON.stringify({
  model: "gpt-4o-mini",
  input: "hello",
  max_output_tokens: 16,
  stream: false,
});
const openAiSheetResponseBody = JSON.stringify({
  id: "resp_101",
  status: "completed",
  model: "gpt-4o-mini",
  output: [{ type: "message", role: "assistant", content: [{ type: "output_text", text: "ok" }] }],
  usage: { input_tokens: 12, output_tokens: 1, total_tokens: 13 },
});
const expectedAlternateAuditFromTime = "2026-04-13T18:30:00.000Z";
const expectedAlternateAuditToTime = "2026-04-14T18:30:00.000Z";

type AuditScenario =
  | "disabled"
  | "metadata_only"
  | "full"
  | "full_streaming"
  | "old_stream_unstored"
  | "invalid_created"
  | "fetch_failure"
  | "orphan_visibility";

function getScenarioConfig(scenario: AuditScenario) {
  switch (scenario) {
    case "disabled":
      return {
        auditCaptureBodiesAtRequest: false,
        auditEnabledAtRequest: false,
        isStream: false,
        listFails: false,
        requestBody: null,
        requestBodyStored: false,
        responseBody: null,
        responseBodyStored: false,
      };
    case "metadata_only":
      return {
        auditCaptureBodiesAtRequest: false,
        auditEnabledAtRequest: true,
        isStream: false,
        listFails: false,
        requestBody: null,
        requestBodyStored: false,
        responseBody: null,
        responseBodyStored: false,
      };
    case "full":
      return {
        auditCaptureBodiesAtRequest: true,
        auditEnabledAtRequest: true,
        isStream: false,
        listFails: false,
        requestBody: openAiSheetRequestBody,
        requestBodyStored: true,
        responseBody: openAiSheetResponseBody,
        responseBodyStored: true,
      };
    case "full_streaming":
      return {
        auditCaptureBodiesAtRequest: true,
        auditEnabledAtRequest: true,
        isStream: true,
        listFails: false,
        requestBody: '{"input":"stream me"}',
        requestBodyStored: true,
        responseBody: streamingSseResponseBody,
        responseBodyStored: true,
      };
    case "old_stream_unstored":
      return {
        auditCaptureBodiesAtRequest: true,
        auditEnabledAtRequest: true,
        isStream: true,
        listFails: false,
        requestBody: '{"input":"legacy stream"}',
        requestBodyStored: true,
        responseBody: null,
        responseBodyStored: false,
      };
    case "invalid_created":
      return {
        auditCaptureBodiesAtRequest: true,
        auditEnabledAtRequest: true,
        isStream: false,
        listFails: false,
        requestBody: '{"input":"invalid time"}',
        requestBodyStored: true,
        responseBody: '{"id":"resp_invalid","status":"ok"}',
        responseBodyStored: true,
      };
    case "fetch_failure":
      return {
        auditCaptureBodiesAtRequest: true,
        auditEnabledAtRequest: true,
        isStream: false,
        listFails: true,
        requestBody: openAiSheetRequestBody,
        requestBodyStored: true,
        responseBody: openAiSheetResponseBody,
        responseBodyStored: true,
      };
    case "orphan_visibility":
      return {
        auditCaptureBodiesAtRequest: true,
        auditEnabledAtRequest: true,
        isStream: false,
        listFails: false,
        requestBody: '{"input":"orphan me"}',
        requestBodyStored: true,
        responseBody: '{"id":"resp_201","status":"ok"}',
        responseBodyStored: true,
      };
  }
}

function createRequestLogDetail(scenario: AuditScenario) {
  const config = getScenarioConfig(scenario);

  return {
    summary: {
      id: 101,
      created_at: scenario === "invalid_created" ? "not-a-date" : timestamp,
      model_id: "gpt-4o-mini",
      resolved_target_model_id: null,
      api_family: "openai",
      vendor_id: 1,
      vendor_key: "openai",
      vendor_name: "OpenAI",
      status_code: 502,
      response_time_ms: 125,
      is_stream: config.isStream,
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
      audit_enabled_at_request: config.auditEnabledAtRequest,
      audit_capture_bodies_at_request: config.auditCaptureBodiesAtRequest,
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

function createAuditListItem(scenario: AuditScenario) {
  const config = getScenarioConfig(scenario);

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
    request_body_preview: config.requestBody,
    request_body_stored: config.requestBodyStored,
    response_status: 200,
    response_body_stored: config.responseBodyStored,
    is_stream: config.isStream,
    duration_ms: 125,
    audit_enabled_at_request: config.auditEnabledAtRequest,
    audit_capture_bodies_at_request: config.auditCaptureBodiesAtRequest,
    created_at: timestamp,
  };
}

function createAuditDetail(scenario: AuditScenario) {
  const config = getScenarioConfig(scenario);

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
    request_body: config.requestBody,
    request_body_stored: config.requestBodyStored,
    response_status: 200,
    response_headers: "content-type: application/json",
    response_body: config.responseBody,
    response_body_stored: config.responseBodyStored,
    is_stream: config.isStream,
    duration_ms: 125,
    audit_enabled_at_request: config.auditEnabledAtRequest,
    audit_capture_bodies_at_request: config.auditCaptureBodiesAtRequest,
    created_at: timestamp,
  };
}

async function mockRequestLogDetailRoutes(page: Page, scenario: AuditScenario) {
  const config = getScenarioConfig(scenario);
  const detail = createRequestLogDetail(scenario);
  const auditListItem = createAuditListItem(scenario);
  const auditDetail = createAuditDetail(scenario);
  let auditListRequests = 0;
  let auditDetailRequests = 0;
  const auditListSearchParams: string[] = [];

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
        profiles: [{ id: 1, name: "Default", description: null, is_active: true, is_default: true, is_editable: true, version: 1, created_at: timestamp, deleted_at: null, updated_at: timestamp }],
        active_profile: { id: 1, name: "Default", description: null, is_active: true, is_default: true, is_editable: true, version: 1, created_at: timestamp, deleted_at: null, updated_at: timestamp },
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
      auditListSearchParams.push(searchParams.toString());

      if (!config.auditEnabledAtRequest) {
        return fulfillJson({ detail: "Audit should not be fetched for disabled rows" }, 409);
      }

      if (config.listFails) {
        return fulfillJson({ detail: "audit list failed" }, 500);
      }

      return fulfillJson({
        items: searchParams.get("request_log_id") === "101" ? [auditListItem] : [],
        next_cursor: null,
        has_more: false,
        window: { from: searchParams.get("from"), to: searchParams.get("to") },
        limit: 20,
        sort: "desc",
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
    getAuditDetailRequests: () => auditDetailRequests,
    getAuditListRequests: () => auditListRequests,
    getAuditListSearchParams: () => auditListSearchParams,
  };
}

async function mockSwitchingAuditRoutes(page: Page) {
  const firstDetail = createRequestLogDetail("full");
  const alternateBaseDetail = createRequestLogDetail("full");
  const secondDetail = {
    ...alternateBaseDetail,
    summary: { ...alternateBaseDetail.summary, id: 202, created_at: alternateTimestamp },
    request: { ...alternateBaseDetail.request, ingress_request_id: "ingress-202" },
  };
  const firstAuditListItem = createAuditListItem("full");
  const secondAuditListItem = {
    ...createAuditListItem("full"),
    id: 302,
    request_log_id: 202,
    created_at: alternateTimestamp,
  };
  const firstAuditDetail = createAuditDetail("full");
  const secondAuditDetail = {
    ...createAuditDetail("full"),
    id: 302,
    request_log_id: 202,
    request_body: '{"input":"switched request"}',
    response_body: '{"id":"resp_202","status":"ok"}',
    created_at: alternateTimestamp,
  };
  let auditDetailRequests = 0;
  const auditListSearchParams: string[] = [];
  let releaseSecondAuditList: (() => void) | null = null;
  const secondAuditListGate = new Promise<void>((resolve) => {
    releaseSecondAuditList = resolve;
  });

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
        profiles: [{ id: 1, name: "Default", description: null, is_active: true, is_default: true, is_editable: true, version: 1, created_at: timestamp, deleted_at: null, updated_at: timestamp }],
        active_profile: { id: 1, name: "Default", description: null, is_active: true, is_default: true, is_editable: true, version: 1, created_at: timestamp, deleted_at: null, updated_at: timestamp },
        profile_limits: { max_profiles: 5 },
      });
    }

    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }

    if (pathname === "/api/stats/requests/101") {
      return fulfillJson(firstDetail);
    }

    if (pathname === "/api/stats/requests/202") {
      return fulfillJson(secondDetail);
    }

    if (pathname === "/api/audit/logs") {
      const requestLogId = searchParams.get("request_log_id");
      auditListSearchParams.push(searchParams.toString());

      if (requestLogId === "202") {
        await secondAuditListGate;
      }

      return fulfillJson({
        items: requestLogId === "202" ? [secondAuditListItem] : [firstAuditListItem],
        next_cursor: null,
        has_more: false,
        window: { from: searchParams.get("from"), to: searchParams.get("to") },
        limit: 20,
        sort: "desc",
      });
    }

    if (pathname === "/api/audit/logs/201") {
      auditDetailRequests += 1;
      return fulfillJson(firstAuditDetail);
    }

    if (pathname === "/api/audit/logs/302") {
      auditDetailRequests += 1;
      return fulfillJson(secondAuditDetail);
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));

  return {
    getAuditDetailRequests: () => auditDetailRequests,
    getAuditListSearchParams: () => auditListSearchParams,
    releaseSecondAuditList: () => releaseSecondAuditList?.(),
  };
}

async function openRequestLogDetail(
  page: Page,
  scenario: AuditScenario,
  initialPath = "/request-logs?request_id=101",
) {
  const counters = await mockRequestLogDetailRoutes(page, scenario);

  await page.goto(initialPath);

  const drawer = page.getByTestId("request-log-detail-sheet");
  await expect(drawer).toBeVisible({ timeout: 15000 });

  return { counters, drawer };
}

function expectAuditWindowParams(
  searchParamString: string,
  requestLogId = "101",
  fromTime = expectedAuditFromTime,
  toTime = expectedAuditToTime,
) {
  const params = new URLSearchParams(searchParamString);
  expect(params.get("request_log_id")).toBe(requestLogId);
  expect(params.get("from")).toBe(fromTime);
  expect(params.get("to")).toBe(toTime);
  expect(params.get("limit")).toBe("20");
}

async function expectExactAuditUrlContract(page: Page, drawer: Locator) {
  const overviewTab = drawer.getByRole("tab", { name: "Overview" });
  const auditTab = drawer.getByRole("tab", { name: "Audit" });

  await expect(page).toHaveURL(/\/request-logs\?request_id=101&detail_tab=audit$/);
  await expect(auditTab).toHaveAttribute("data-state", "active");

  await Promise.all([page.waitForURL(/\/request-logs\?request_id=101$/), overviewTab.click()]);
  await expect(overviewTab).toHaveAttribute("data-state", "active");

  await Promise.all([
    page.waitForURL(/\/request-logs\?request_id=101&detail_tab=audit$/),
    auditTab.click(),
  ]);
  await expect(auditTab).toHaveAttribute("data-state", "active");
}

test.describe("request log audit investigation states", () => {
  test("disabled audit rows keep exact-mode URL state without firing audit APIs", async ({ page }) => {
    const { drawer, counters } = await openRequestLogDetail(page, "disabled", "/request-logs?request_id=101&detail_tab=audit");

    await expect(drawer.getByText("Audit disabled at request time").first()).toBeVisible();
    await expect(drawer.getByText("This request kept only request-log metadata because audit logging was off when it started.")).toBeVisible();
    expect(counters.getAuditListRequests()).toBe(0);
    expect(counters.getAuditDetailRequests()).toBe(0);
    await expectExactAuditUrlContract(page, drawer);
  });

  test("metadata-only capture stays distinct from failure states", async ({ page }) => {
    const { drawer, counters } = await openRequestLogDetail(page, "metadata_only", "/request-logs?request_id=101&detail_tab=audit");

    await expect.poll(() => counters.getAuditListRequests()).toBeGreaterThan(0);
    await expect.poll(() => counters.getAuditDetailRequests()).toBeGreaterThan(0);
    expectAuditWindowParams(counters.getAuditListSearchParams()[0]);
    await expect(drawer.getByText("Metadata only").first()).toBeVisible();
    await expect(drawer.getByText("Request body was intentionally not stored because this request used metadata-only audit capture.")).toBeVisible();
    await expect(drawer.getByText("Response body was intentionally not stored because this request used metadata-only audit capture.")).toBeVisible();
    await expectExactAuditUrlContract(page, drawer);
  });

  test("full capture shows stored request and response bodies", async ({ page }) => {
    const { drawer, counters } = await openRequestLogDetail(page, "full", "/request-logs?request_id=101&detail_tab=audit");

    await expect.poll(() => counters.getAuditListRequests()).toBeGreaterThan(0);
    await expect.poll(() => counters.getAuditDetailRequests()).toBeGreaterThan(0);
    expectAuditWindowParams(counters.getAuditListSearchParams()[0]);
    await expect(drawer.getByText("Full capture").first()).toBeVisible();
    await expect(drawer.getByText("Input").first()).toBeVisible();
    await expect(drawer.getByText("hello").first()).toBeVisible();
    await expect(drawer.getByText("Assistant output").first()).toBeVisible();
    await expect(drawer.getByText("ok").first()).toBeVisible();
    await expect(drawer.getByText(/\{"model":"gpt-4o-mini","input"/)).toHaveCount(0);
    await expectExactAuditUrlContract(page, drawer);
  });

  test("switching exact request resets audit detail by request and window key", async ({ page }) => {
    const counters = await mockSwitchingAuditRoutes(page);

    await page.goto("/request-logs?request_id=101&detail_tab=audit");
    const drawer = page.getByTestId("request-log-detail-sheet");
    await expect(drawer).toBeVisible({ timeout: 15000 });
    await expect(drawer.getByText("Input").first()).toBeVisible();
    await expect(drawer.getByText("hello").first()).toBeVisible();
    await expect.poll(() => counters.getAuditListSearchParams().length).toBe(1);
    expectAuditWindowParams(counters.getAuditListSearchParams()[0]);

    await page.goto("/request-logs?request_id=202&detail_tab=audit");
    await expect(drawer.getByRole("heading", { name: "Request #202" })).toBeVisible();
    await expect.poll(() => counters.getAuditListSearchParams().length).toBe(2);
    expectAuditWindowParams(
      counters.getAuditListSearchParams()[1],
      "202",
      expectedAlternateAuditFromTime,
      expectedAlternateAuditToTime,
    );
    await expect(drawer.getByText("hello")).toHaveCount(0);

    counters.releaseSecondAuditList();
    await expect(drawer.getByText("switched request")).toBeVisible();
    await expect(drawer.getByText("resp_202")).toBeVisible();
    await expect.poll(() => counters.getAuditDetailRequests()).toBe(2);
  });

  test("streaming full capture renders stored raw SSE response bodies", async ({ page }) => {
    const { drawer, counters } = await openRequestLogDetail(page, "full_streaming", "/request-logs?request_id=101&detail_tab=audit");

    await expect.poll(() => counters.getAuditListRequests()).toBeGreaterThan(0);
    await expect.poll(() => counters.getAuditDetailRequests()).toBeGreaterThan(0);
    await expect(drawer.getByText("Full capture").first()).toBeVisible();
    await expect(drawer.getByText("stream me")).toBeVisible();
    await expect(drawer.getByText('event: message\ndata: {"id":"resp_stream","delta":"hel"}')).toBeVisible();
    await expect(drawer.getByText("event: done\ndata: [DONE]")).toBeVisible();
  });

  test("old streaming rows without stored response bodies use neutral no-body copy", async ({ page }) => {
    const { drawer, counters } = await openRequestLogDetail(page, "old_stream_unstored", "/request-logs?request_id=101&detail_tab=audit");

    await expect.poll(() => counters.getAuditListRequests()).toBeGreaterThan(0);
    await expect.poll(() => counters.getAuditDetailRequests()).toBeGreaterThan(0);
    await expect(drawer.getByText("Full capture").first()).toBeVisible();
    await expect(drawer.getByText("legacy stream")).toBeVisible();
    await expect(drawer.getByText("Response body was not stored for this audit record.")).toBeVisible();
  });

  test("invalid request created time gates audit lookup calls", async ({ page }) => {
    const { drawer, counters } = await openRequestLogDetail(page, "invalid_created", "/request-logs?request_id=101&detail_tab=audit");

    await expect(drawer.getByText("No audit records found for this request.")).toBeVisible();
    expect(counters.getAuditListRequests()).toBe(0);
    expect(counters.getAuditDetailRequests()).toBe(0);
  });

  test("fetch failures stay visually distinct from disabled and captured states", async ({ page }) => {
    const { drawer, counters } = await openRequestLogDetail(page, "fetch_failure", "/request-logs?request_id=101&detail_tab=audit");

    await expect.poll(() => counters.getAuditListRequests()).toBeGreaterThan(0);
    await expect(drawer.getByText("Audit detail load failed")).toBeVisible({ timeout: 15000 });
    await expect(drawer.getByText("Prism expected an audit record for this request but could not load it after multiple attempts.")).toBeVisible();
    expect(counters.getAuditDetailRequests()).toBe(0);
  });

  test("orphaned audit rows stay visible even when request_log_id is null", async ({ page }) => {
    const { drawer, counters } = await openRequestLogDetail(page, "orphan_visibility", "/request-logs?request_id=101&detail_tab=audit");

    await expect.poll(() => counters.getAuditListRequests()).toBeGreaterThan(0);
    await expect.poll(() => counters.getAuditDetailRequests()).toBeGreaterThan(0);
    await expect(drawer.getByText("Full capture").first()).toBeVisible();
    await expect(drawer.getByText("orphan me")).toBeVisible();
    await expect(drawer.getByText("resp_201")).toBeVisible();
  });
});