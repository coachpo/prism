import { expect, test, type BrowserContext, type Locator, type Page } from "@playwright/test";

const timestamp = "2026-04-13T00:00:00Z";
const expectedFromTime = "2026-04-12T12:00:00.000Z";
const expectedToTime = "2026-04-13T12:00:00.000Z";
const redactedHeaders = "content-type: application/json\nauthorization: Bearer [REDACTED]";
const jsonRequestHeaders = JSON.stringify({
  authorization: "Bearer live-secret-token",
  "content-type": "application/json",
  cookie: "session=live-cookie",
  "user-agent": "prism-postdual-overflow-gpt55-deepseek-1781125557",
});
const jsonResponseHeaders = JSON.stringify({
  "access-control-allow-credentials": "true",
  "content-type": "application/json",
  date: "Wed, 10 Jun 2026 21:06:01 GMT",
  server: "elb",
  "set-cookie": "session=live-response-cookie",
  "strict-transport-security": "max-age=31536000; includeSubDomains; preload",
  vary: "origin, access-control-request-method, access-control-request-headers",
  "x-client-credential": "live-client-credential",
});
const requestBody = "original request body\nline two";
const responseBody = "original response body\nline two";
const longRepeatedRequestToken = Array.from({ length: 220 }, () => "request-token").join(" ");
const longRepeatedRequestBody = JSON.stringify({
  model: "gpt-4o-mini",
  input: longRepeatedRequestToken,
  max_output_tokens: 8,
  stream: false,
});
const openAiDocumentRequestBody = JSON.stringify({
  model: "gpt-4o-mini",
  messages: [
    { role: "system", content: "You are concise." },
    { role: "user", content: "Reply with exactly ok." },
  ],
  max_tokens: 8,
  stream: false,
});
const openAiDocumentResponseBody = JSON.stringify({
  id: "chatcmpl_101",
  object: "chat.completion",
  model: "gpt-4o-mini",
  choices: [{ index: 0, message: { role: "assistant", content: "ok" }, finish_reason: "stop" }],
  usage: { prompt_tokens: 12, completion_tokens: 1, total_tokens: 13 },
});
const geminiDocumentRequestBody = JSON.stringify({
  systemInstruction: { parts: [{ text: "Be brief." }] },
  contents: [{ role: "user", parts: [{ text: "Summarize the route." }] }],
  generationConfig: { temperature: 0.2, maxOutputTokens: 128 },
});
const geminiDocumentResponseBody = JSON.stringify({
  candidates: [{ content: { role: "model", parts: [{ text: "Route summary." }] }, finishReason: "STOP" }],
  usageMetadata: { promptTokenCount: 10, candidatesTokenCount: 2, totalTokenCount: 12 },
});
const anthropicDocumentRequestBody = JSON.stringify({
  model: "claude-3-5-sonnet-latest",
  system: "You are precise.",
  messages: [{ role: "user", content: [{ type: "text", text: "Explain the audit." }] }],
  max_tokens: 64,
  stream: false,
});
const anthropicDocumentResponseBody = JSON.stringify({
  id: "msg_101",
  type: "message",
  role: "assistant",
  content: [{ type: "text", text: "Audit explained." }],
  stop_reason: "end_turn",
  usage: { input_tokens: 8, output_tokens: 3 },
});

type Scenario =
  | "full"
  | "metadata_only"
  | "disabled"
  | "missing_request"
  | "no_records"
  | "list_failure"
  | "detail_failure"
  | "invalid_created"
  | "json_headers"
  | "long_body"
  | "openai_document"
  | "gemini_document"
  | "anthropic_document";

type ApiFamilyFixture = "openai" | "gemini" | "anthropic";

function getScenarioApiFamily(scenario: Scenario): ApiFamilyFixture {
  if (scenario === "gemini_document") return "gemini";
  if (scenario === "anthropic_document") return "anthropic";
  return "openai";
}

function getScenarioModelId(scenario: Scenario): string {
  if (scenario === "gemini_document") return "gemini-2.5-flash";
  if (scenario === "anthropic_document") return "claude-3-5-sonnet-latest";
  return "gpt-4o-mini";
}

function getScenarioModelLabel(scenario: Scenario): string {
  if (scenario === "gemini_document") return "Gemini 2.5 Flash";
  if (scenario === "anthropic_document") return "Claude 3.5 Sonnet";
  return "GPT-4o mini";
}

function getScenarioRequestBody(scenario: Scenario): string {
  if (scenario === "long_body") return longRepeatedRequestBody;
  if (scenario === "openai_document") return openAiDocumentRequestBody;
  if (scenario === "gemini_document") return geminiDocumentRequestBody;
  if (scenario === "anthropic_document") return anthropicDocumentRequestBody;
  return requestBody;
}

function getScenarioResponseBody(scenario: Scenario): string {
  if (scenario === "openai_document") return openAiDocumentResponseBody;
  if (scenario === "gemini_document") return geminiDocumentResponseBody;
  if (scenario === "anthropic_document") return anthropicDocumentResponseBody;
  return responseBody;
}

function scenarioConfig(scenario: Scenario) {
  const capturesBody =
    scenario === "full" ||
    scenario === "json_headers" ||
    scenario === "long_body" ||
    scenario === "openai_document" ||
    scenario === "gemini_document" ||
    scenario === "anthropic_document" ||
    scenario === "detail_failure" ||
    scenario === "list_failure" ||
    scenario === "invalid_created" ||
    scenario === "no_records";

  return {
    auditCaptureBodiesAtRequest: capturesBody,
    auditEnabledAtRequest: scenario !== "disabled",
    createdAt: scenario === "invalid_created" ? "not-a-date" : timestamp,
    listFails: scenario === "list_failure",
    detailFails: scenario === "detail_failure",
    listItems: scenario === "no_records" ? [] : [201, 202],
    requestBody: scenario === "metadata_only" ? null : getScenarioRequestBody(scenario),
    requestBodyStored: scenario !== "metadata_only",
    responseBody: scenario === "metadata_only" ? null : getScenarioResponseBody(scenario),
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

function createRequestLogListItem(scenario: Scenario = "full") {
  const apiFamily = getScenarioApiFamily(scenario);
  const modelId = getScenarioModelId(scenario);

  return {
    id: 101,
    created_at: timestamp,
    model_id: modelId,
    model_label: getScenarioModelLabel(scenario),
    resolved_target_model_id: null,
    resolved_target_model_label: null,
    is_proxy_origin: false,
    caller_client_display: "Prism QA Browser",
    upstream_client_display: "Prism QA Browser",
    user_agent_overridden: false,
    api_family: apiFamily,
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
  const apiFamily = getScenarioApiFamily(scenario);
  const modelId = getScenarioModelId(scenario);

  return {
    summary: {
      id: 101,
      created_at: config.createdAt,
      model_id: modelId,
      model_label: getScenarioModelLabel(scenario),
      resolved_target_model_id: null,
      resolved_target_model_label: null,
      is_proxy_origin: false,
      api_family: apiFamily,
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
    model_id: getScenarioModelId(scenario),
    endpoint_id: 1,
    connection_id: null,
    endpoint_base_url: "https://api.example.test",
    endpoint_description: "Primary endpoint",
    request_method: "POST",
    request_url: `https://api.example.test/v1/responses?audit=${id}`,
    request_headers: scenario === "json_headers" ? jsonRequestHeaders : redactedHeaders,
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
    response_headers: scenario === "json_headers" ? jsonResponseHeaders : "content-type: application/json\nx-prism-audit: [REDACTED]",
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
        items: [createRequestLogListItem(scenario)],
        total: 1,
        limit: 100,
        offset: 0,
        filter_options: { models: [], endpoints: [], clients: [], resolved_target_models: [] },
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
      const cursor = searchParams.get("cursor");
      const ids = cursor === "page-2" ? [202] : config.listItems;
      const items = ids.map((id) => createAuditListItem(id, scenario));
      return fulfillJson({
        items,
        next_cursor: cursor === "page-2" ? null : "page-2",
        has_more: cursor !== "page-2" && config.listItems.length > 0,
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

async function expectNoRedundantPayloadShell(section: Locator) {
  const hasOldShell = await section.evaluate((element) => {
    const classNames = Array.from(element.querySelectorAll("*")).map((node) => node.getAttribute("class") ?? "");
    return classNames.some((className) => {
      const oldPayloadShell = className.includes("rounded-xl") &&
        className.includes("border-border/70") &&
        className.includes("bg-muted/30") &&
        className.includes("shadow-inner");
      const oldDocumentShell = className.includes("rounded-xl") &&
        className.includes("border-border/70") &&
        className.includes("bg-background") &&
        className.includes("shadow-sm");
      return oldPayloadShell || oldDocumentShell;
    });
  });
  expect(hasOldShell).toBe(false);
}

const documentBodyCases = [
  {
    label: "OpenAI",
    scenario: "openai_document" as const,
    rawBodyPattern: /\{"model":"gpt-4o-mini","messages"/,
    requestLabels: ["Message transcript", "system", "You are concise.", "Reply with exactly ok."],
    responseLabels: ["Response choices", "assistant", "ok", "Usage"],
  },
  {
    label: "Gemini",
    scenario: "gemini_document" as const,
    rawBodyPattern: /\{"systemInstruction":\{"parts"/,
    requestLabels: ["System instruction", "Content timeline", "Summarize the route.", "Generation config"],
    responseLabels: ["Candidate responses", "model", "Route summary.", "Usage"],
  },
  {
    label: "Anthropic",
    scenario: "anthropic_document" as const,
    rawBodyPattern: /\{"model":"claude-3-5-sonnet-latest","system"/,
    requestLabels: ["System prompt", "Message exchange", "Explain the audit."],
    responseLabels: ["Assistant content", "Audit explained.", "Stop reason", "Usage"],
  },
];

test.describe("dedicated request-log audit page", () => {
  for (const bodyCase of documentBodyCases) {
    test(`renders ${bodyCase.label} request and response audit bodies as documents`, async ({ page }) => {
      const counters = await mockPrismRoutes(page, bodyCase.scenario);

      await page.goto("/observe/requests/101/audit?audit_id=201");

      const detail = page.getByTestId("dedicated-audit-detail");
      await expect(detail).toBeVisible({ timeout: 15000 });
      const requestSection = detail.getByRole("region", { name: "Request", exact: true });
      const responseSection = detail.getByRole("region", { name: "Response (200)" });
      await expectNoRedundantPayloadShell(requestSection);
      await expectNoRedundantPayloadShell(responseSection);
      await expect(detail.getByText("[REDACTED]").first()).toBeVisible();
      for (const label of bodyCase.requestLabels) {
        await expect(detail.getByText(label).first()).toBeVisible();
      }
      for (const label of bodyCase.responseLabels) {
        await expect(detail.getByText(label).first()).toBeVisible();
      }
      await expect(detail.getByText(bodyCase.rawBodyPattern)).toHaveCount(0);
      expect(counters.auditDetailRequests).toEqual([201]);
    });
  }

  test("renders JSON and newline headers as key-value rows with sensitive values masked", async ({ page }) => {
    await mockPrismRoutes(page, "json_headers");

    await page.goto("/observe/requests/101/audit?audit_id=201");

    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible({ timeout: 15000 });

    const requestHeaders = detail.getByRole("region", { name: "Request headers" });
    await expectNoRedundantPayloadShell(requestHeaders);
    await expect(requestHeaders.locator("dt", { hasText: "authorization" })).toBeVisible();
    await expect(requestHeaders.locator("dd", { hasText: "[REDACTED]" }).first()).toBeVisible();
    await expect(requestHeaders.locator("dt", { hasText: "content-type" })).toBeVisible();
    await expect(requestHeaders.locator("dd", { hasText: "application/json" })).toBeVisible();
    await expect(requestHeaders.locator("dt", { hasText: "user-agent" })).toBeVisible();
    await expect(requestHeaders.locator("dd", { hasText: "prism-postdual-overflow-gpt55-deepseek-1781125557" })).toBeVisible();
    await expect(requestHeaders.getByText("Bearer live-secret-token")).toHaveCount(0);
    await expect(requestHeaders.getByText("session=live-cookie")).toHaveCount(0);
    await expect(requestHeaders.getByText(/\{\s*"authorization"/)).toHaveCount(0);

    const responseHeaders = detail.getByRole("region", { name: "Response headers" });
    await expectNoRedundantPayloadShell(responseHeaders);
    await expect(responseHeaders.locator("dt", { hasText: "access-control-allow-credentials" })).toBeVisible();
    await expect(responseHeaders.locator("dd", { hasText: "true" })).toBeVisible();
    await expect(responseHeaders.locator("dt", { hasText: "strict-transport-security" })).toBeVisible();
    await expect(responseHeaders.locator("dd", { hasText: "max-age=31536000; includeSubDomains; preload" })).toBeVisible();
    await expect(responseHeaders.locator("dt", { hasText: "set-cookie" })).toBeVisible();
    await expect(responseHeaders.locator("dt", { hasText: "x-client-credential" })).toBeVisible();
    await expect(responseHeaders.locator("dd", { hasText: "[REDACTED]" }).first()).toBeVisible();
    await expect(responseHeaders.getByText("session=live-response-cookie")).toHaveCount(0);
    await expect(responseHeaders.getByText("live-client-credential")).toHaveCount(0);

    await requestHeaders.getByRole("button", { name: "Raw JSON" }).click();
    await expect(requestHeaders.locator("pre")).toContainText('"authorization": "[REDACTED]"');
    await expect(requestHeaders.locator("pre")).toContainText('"user-agent": "prism-postdual-overflow-gpt55-deepseek-1781125557"');
    await expect(requestHeaders.locator("pre")).not.toContainText("Bearer live-secret-token");

    await responseHeaders.getByRole("button", { name: "Raw JSON" }).click();
    await expect(responseHeaders.locator("pre")).toContainText('"access-control-allow-credentials": "true"');
    await expect(responseHeaders.locator("pre")).toContainText('"set-cookie": "[REDACTED]"');
    await expect(responseHeaders.locator("pre")).toContainText('"x-client-credential": "[REDACTED]"');
    await expect(responseHeaders.locator("pre")).toContainText('"vary": "origin, access-control-request-method, access-control-request-headers"');
    await expect(responseHeaders.locator("pre")).not.toContainText("session=live-response-cookie");
    await expect(responseHeaders.locator("pre")).not.toContainText("live-client-credential");
  });

  test("local Raw JSON toggle pretty-prints parseable request bodies", async ({ page }) => {
    await mockPrismRoutes(page, "openai_document");

    await page.goto("/observe/requests/101/audit?audit_id=201");

    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible({ timeout: 15000 });
    const requestSection = detail.getByRole("region", { name: "Request", exact: true });
    await expect(requestSection.getByText("Message transcript")).toBeVisible();
    await requestSection.getByRole("button", { name: "Raw JSON" }).click();
    await expect(requestSection.getByRole("button", { name: "Raw JSON" })).toHaveAttribute("aria-pressed", "true");
    await expect(requestSection.locator("pre")).toContainText('"model": "gpt-4o-mini"');
    await expect(requestSection.locator("pre")).toContainText('"messages": [');
    await expect(requestSection.getByText("Message transcript")).toHaveCount(0);
  });

  test("long repeated-token request bodies scroll inside the Request Body content area only", async ({ page }) => {
    await mockPrismRoutes(page, "long_body");

    await page.goto("/observe/requests/101/audit?audit_id=201");

    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible({ timeout: 15000 });
    const requestSection = detail.getByRole("region", { name: "Request", exact: true });
    await expectNoRedundantPayloadShell(requestSection);
    const requestContent = requestSection.getByTestId("request-log-request-body-content");
    await expect(requestSection.getByRole("button", { name: "Rendered" })).toBeVisible();
    await expect(requestSection.getByRole("button", { name: "Raw JSON" })).toBeVisible();
    await expect(requestSection.getByRole("button", { name: "Copy" })).toBeVisible();
    await expect(requestSection.getByText(longRepeatedRequestToken.slice(0, 80))).toBeVisible();
    await expect(requestContent).toHaveCSS("overflow-y", "auto");
    await expect(requestContent.locator("[data-radix-scroll-area-viewport], article .overflow-y-auto")).toHaveCount(0);

    const renderedMetrics = await requestContent.evaluate((element) => ({
      clientHeight: element.clientHeight,
      maxHeight: getComputedStyle(element).maxHeight,
      scrollHeight: element.scrollHeight,
      viewportHeight: window.innerHeight,
    }));
    expect(renderedMetrics.maxHeight).not.toBe("none");
    expect(renderedMetrics.clientHeight).toBeLessThanOrEqual(Math.ceil(renderedMetrics.viewportHeight * 0.9) + 2);
    expect(renderedMetrics.scrollHeight).toBeGreaterThan(renderedMetrics.clientHeight);

    await requestSection.getByRole("button", { name: "Raw JSON" }).click();
    await expect(requestSection.getByRole("button", { name: "Raw JSON" })).toHaveAttribute("aria-pressed", "true");
    await expect(requestContent.locator("pre")).toContainText('"input": "request-token');
    const rawMetrics = await requestContent.evaluate((element) => ({
      clientHeight: element.clientHeight,
      scrollHeight: element.scrollHeight,
    }));
    expect(rawMetrics.scrollHeight).toBeGreaterThan(rawMetrics.clientHeight);

    const responseSection = detail.getByRole("region", { name: "Response (200)" });
    await expect(responseSection.getByTestId("request-log-request-body-content")).toHaveCount(0);
  });

  test("raw-mode copy writes the pretty-printed JSON shown in that section", async ({ page, context }) => {
    await installCopyHarness(page, context);
    await mockPrismRoutes(page, "openai_document");

    await page.goto("/observe/requests/101/audit?audit_id=201");

    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible({ timeout: 15000 });
    const requestSection = detail.getByRole("region", { name: "Request", exact: true });
    await requestSection.getByRole("button", { name: "Raw JSON" }).click();
    await requestSection.getByRole("button", { name: "Copy" }).click();
    await expect.poll(() => copiedText(page)).toBe(JSON.stringify(JSON.parse(openAiDocumentRequestBody), null, 2));
    await expect.poll(() => usedDedicatedFallbackRoot(page)).toBe(true);
  });

  test("direct selected audit_id route fetches only the selected audit detail", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "full");

    await page.goto("/observe/requests/101/audit?audit_id=202");

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

  test("audit cursor pagination keeps the cursor in the URL and fetches the next page", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "full");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByTestId("dedicated-audit-list")).toContainText("#201", { timeout: 15000 });
    await expect(page.getByRole("link", { name: "Next Page" })).toHaveAttribute("href", "/observe/requests/101/audit?cursor=page-2");
    await page.getByRole("link", { name: "Next Page" }).click();

    await expect(page).toHaveURL(/\/observe\/requests\/101\/audit\?cursor=page-2$/);
    await expect(page.getByTestId("dedicated-audit-list")).toContainText("#202");
    await expect(page.getByRole("link", { name: "Previous Page" })).toHaveAttribute("href", "/observe/requests/101/audit");
    expect(counters.auditListSearchParams).toHaveLength(2);
    expect(new URLSearchParams(counters.auditListSearchParams[1]).get("cursor")).toBe("page-2");
    expect(counters.auditDetailRequests).toEqual([201, 202]);
  });

  test("disabled audit requests do not call audit APIs", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "disabled");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByText("Audit disabled at request time").first()).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("metadata-only audit shows no-body copy state and copies original redacted headers", async ({ page, context }) => {
    await installCopyHarness(page, context);
    const counters = await mockPrismRoutes(page, "metadata_only");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByText("Metadata only").first()).toBeVisible({ timeout: 15000 });
    const requestHeaders = page.getByTestId("dedicated-audit-detail").getByRole("region", { name: "Request headers" });
    await expect(requestHeaders.locator("dd", { hasText: "[REDACTED]" }).first()).toBeVisible();
    await expect(page.getByText("Request body was intentionally not stored because this request used metadata-only audit capture.")).toBeVisible();
    await expect(page.getByText("Response body was intentionally not stored because this request used metadata-only audit capture.")).toBeVisible();
    const copyButtons = page.getByTestId("dedicated-audit-detail").getByRole("button", { name: /^Copy$/ });
    await expect(copyButtons).toHaveCount(4);
    await expect(copyButtons.nth(1)).toBeDisabled();
    await expect(copyButtons.nth(3)).toBeDisabled();
    await requestHeaders.locator("dd", { hasText: "[REDACTED]" }).first().evaluate((element) => {
      element.textContent = "mutated header text";
    });
    await copyButtons.first().click();
    await expect.poll(() => copiedText(page)).toBe(redactedHeaders);
    await expect.poll(() => usedDedicatedFallbackRoot(page)).toBe(true);
    expect(counters.auditDetailRequests).toEqual([201]);
  });

  test("missing request state renders without audit calls", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "missing_request");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByText("Request Not Found")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("no audit records state does not fetch audit details", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "no_records");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByText("No audit records found for this request.")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toHaveLength(1);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("unmatched audit_id renders missing-audit state with a return action", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "full");

    await page.goto("/observe/requests/101/audit?audit_id=999");

    await expect(page.getByText("Audit record not found for this request")).toBeVisible({ timeout: 15000 });
    await expect(page.getByRole("link", { name: "Show default audit record" })).toHaveAttribute("href", "/observe/requests/101/audit");
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("audit list failure does not fetch audit details", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "list_failure");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByText("Audit records load failed")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toHaveLength(1);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("audit detail failure preserves the audit list", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "detail_failure");

    await page.goto("/observe/requests/101/audit?audit_id=201");

    await expect(page.getByText("Audit record load failed")).toBeVisible({ timeout: 15000 });
    await expect(page.getByTestId("dedicated-audit-list")).toContainText("#201");
    expect(counters.auditDetailRequests).toEqual([201]);
  });

  test("invalid request timestamp prevents audit lookup", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "invalid_created");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByText("Invalid request timestamp")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("request-log row clicks still open the overview drawer with a full audit page link", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "full");

    await page.goto("/observe/requests");
    const requestLogRow = page.getByTestId("request-logs-table").getByRole("button").filter({ hasText: "GPT-4o mini" });
    await requestLogRow.click();

    const drawer = page.getByTestId("request-log-detail-sheet");
    await expect(drawer).toBeVisible({ timeout: 15000 });
    await expect(drawer.getByRole("tab", { name: "Audit" })).toHaveCount(0);
    await expect(drawer.getByText("Review requested model, final target model, selected terminal target, routing, tokens, costs, and request-time audit provenance.")).toBeVisible();
    await expect(drawer.getByTestId("request-log-overview-grid").getByText("/v1/responses")).toBeVisible();
    await expect(drawer.getByRole("link", { name: "Open full audit page" })).toHaveAttribute("href", "/observe/requests/101/audit");
    await expect(page).toHaveURL(/\/observe\/requests\?selected_request_id=101$/);
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
  });
});
