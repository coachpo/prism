import { expect, type BrowserContext, type Locator, type Page } from "@playwright/test";

const timestamp = "2026-04-13T00:00:00Z";
const expectedFromTime = "2026-04-12T12:00:00.000Z";
const expectedToTime = "2026-04-13T12:00:00.000Z";
export const redactedHeaders = "content-type: application/json\nauthorization: Bearer [REDACTED]";
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
export const openAiStreamSseBody = [
  'data: {"choices":[{"delta":{"role":"assistant","content":"Hello"}}]}',
  "",
  'data: {"choices":[{"delta":{"content":" from the stream"}}]}',
  "",
  'data: {"choices":[{"delta":{},"finish_reason":"stop"}]}',
  "",
  "data: [DONE]",
  "",
].join("\n");

export const openAiStreamToolsSseBody = [
  'data: {"choices":[{"delta":{"role":"assistant","content":""}}]}',
  "",
  'data: ' + JSON.stringify({choices: [{delta: {tool_calls: [{index: 0, id: 'call_1', function: {name: 'get_weather', arguments: '{"city":"Paris"}'}}]}}]}),
  "",
  'data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}',
  "",
  "data: [DONE]",
  "",
].join("\n");

export const longRepeatedRequestToken = Array.from({ length: 220 }, () => "request-token").join(" ");
const longRepeatedRequestBody = JSON.stringify({
  model: "gpt-4o-mini",
  input: longRepeatedRequestToken,
  max_output_tokens: 8,
  stream: false,
});
export const openAiDocumentRequestBody = JSON.stringify({
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

export type Scenario =
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
  | "anthropic_document"
  | "openai_stream"
  | "openai_stream_tools";

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
  if (scenario === "openai_stream") return openAiStreamSseBody;
  if (scenario === "openai_stream_tools") return openAiStreamToolsSseBody;
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
    scenario === "openai_stream" ||
    scenario === "openai_stream_tools" ||
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

export function createRequestLogListItem(scenario: Scenario = "full") {
  const apiFamily = getScenarioApiFamily(scenario);
  const modelId = getScenarioModelId(scenario);

  return {
    request_log_id: "101",
    row_kind: "upstream",
    ingress_request_id: "ingress-101",
    attempt_number: 1,
    attempt_trigger: null,
    attempt_result: null,
    is_winner: null,
    created_at: timestamp,
    model_id: modelId,
    model_label: getScenarioModelLabel(scenario),
    resolved_target_model_id: null,
    resolved_target_model_label: null,
    caller_client_display: "Prism QA Browser",
    upstream_client_display: "Prism QA Browser",
    user_agent_overridden: false,
    api_family: apiFamily,
    endpoint_id: 1,
    endpoint_label: "Primary endpoint",
    terminal_target_id: null,
    terminal_target_label: null,
    terminal_target_configured: false,
    ttft_ms: null,
    completion_duration_ms: null,
    upstream_status_code: 200,
    gateway_status_code: null,
    legacy_status_code: null,
    attempt_duration_ms: 125,
    legacy_duration_ms: null,
    is_stream: false,
    stream_outcome: "not_streaming",
    stream_error_kind: null,
    error_source: null,
    error_code: null,
    failure_stage: null,
    failure_detail_preview: null,
    failure_detail_source: "error_detail",
    failure_detail_preview_truncated: false,
    failure_detail_redacted: false,
    output_tokens: 8,
    total_tokens: 20,
    total_cost_user_currency_micros: 3000,
    pricing_status: "priced",
    pricing_evidence_trust: "trusted",
    unpriced_reason: null,
    report_currency_symbol: "$",
  };
}

function createRequestLogDetail(scenario: Scenario) {
  const config = scenarioConfig(scenario);
  const apiFamily = getScenarioApiFamily(scenario);
  const modelId = getScenarioModelId(scenario);

  const isStreaming = scenario === "openai_stream" || scenario === "openai_stream_tools";
  return {
    summary: {
      request_log_id: "101",
      created_at: config.createdAt,
      model_id: modelId,
      model_label: getScenarioModelLabel(scenario),
      resolved_target_model_id: null,
      resolved_target_model_label: null,
      api_family: apiFamily,
      row_kind: "upstream",
      upstream_status_code: 200,
      gateway_status_code: null,
      legacy_status_code: null,
      attempt_duration_ms: 125,
      legacy_duration_ms: null,
      ttft_ms: null,
      completion_duration_ms: null,
      is_stream: isStreaming,
      stream_outcome: isStreaming ? "completed" : "not_streaming",
      stream_error_kind: null,
      attempt_number: 1,
      attempt_trigger: null,
      attempt_result: null,
      is_winner: null,
    },
    request: {
      operation_name: scenario === "openai_stream_tools" || scenario === "openai_stream" ? "openai.chat_completions" : null,
      upstream_operation_name: null,
      operation_translation_mode: null,
      request_path: "/v1/responses",
      upstream_request_path: null,
      ingress_request_id: "ingress-101",
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
      metadata_redacted_fields: [],
      metadata_truncated_fields: [],
      url_scrub_provenance: "runtime_scrubbed",
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
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 0,
      reasoning_tokens: 0,
    },
    failure: null,
    terminal_target: null,
    endpoint: {
      kind: "endpoint",
      id: "1",
      name: "Primary endpoint",
      name_source: "current",
      deleted: false,
      configured: true,
    },
    routing_provenance: { initial_terminal_target: null, differs_from_actual: false },
    pricing: {
      pricing_status: "priced",
      unpriced_reason: null,
      pricing_resolution_kind: null,
      missing_price_components: null,
      pricing_evidence_trust: "trusted",
      total_cost_user_currency_micros: 3000,
      total_cost_original_micros: 3000,
      currency_code_original: "USD",
      fx_rate_used: "1",
      fx_rate_source: "manual",
      report_currency_code: "USD",
      report_currency_symbol: "$",
      reporting_currency_epoch: null,
      currency_attribution: "identified",
      cost_segment_key: "l.USD",
      pricing_template_id_used: null,
      pricing_template_name_snapshot: null,
      pricing_template_revision_id_used: null,
      pricing_config_version_used: 1,
      pricing_version_effective_at: null,
      pricing_snapshot_unit: "1M tokens",
      pricing_snapshot_input: "0.10",
      pricing_snapshot_output: "0.20",
      pricing_snapshot_cache_read_input: null,
      pricing_snapshot_cache_creation_input: null,
      pricing_snapshot_reasoning: null,
      evidence_state: "authoritative",
    },
    legacy_pricing_evidence: null,
    current_pricing_template: null,
  };
}

function createAuditListItem(id: number, scenario: Scenario) {
  const config = scenarioConfig(scenario);
  return {
    id,
    request_log_id: "101",
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

export async function installCopyHarness(page: Page, context: BrowserContext) {
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

export async function copiedText(page: Page) {
  return page.evaluate(() => (window as Window & { __copiedText?: string }).__copiedText ?? "");
}

export async function usedDedicatedFallbackRoot(page: Page) {
  return page.evaluate(() => (window as Window & { __fallbackUsedDedicatedRoot?: boolean }).__fallbackUsedDedicatedRoot ?? false);
}

export async function mockPrismRoutes(page: Page, scenario: Scenario) {
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
      return fulfillJson({ state: "disabled", transition_state: null, login_available: false, effective_generation: "1", retry_after_seconds: null });
    }

    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }

    if (pathname === "/api/stats/requests") {
      // Requests page defaults to view=ingress_chains; the fixture returns a
      // chain envelope with the single row plus its finalized summary.
      if ((searchParams.get("view") || "ingress_chains") === "ingress_chains") {
        return fulfillJson({
          query_context: null,
          source_ingress_total: 1,
          retained_ingress_total: 1,
          retained_upstream_attempt_total: 1,
          retained_request_log_row_total: 1,
          legacy_unknown_row_total: 0,
          page_ingress_count: 1,
          page_upstream_attempt_count: 1,
          page_request_log_row_count: 1,
          items: [
            {
              ingress_request_id: "ingress-101",
              started_at: null,
              completed_at: null,
              elapsed_ms: null,
              elapsed_evidence_state: "unavailable",
              finalized_evidence_state: "unavailable",
              finalized_summary: null,
              expected_attempt_count: null,
              expected_request_log_row_count: null,
              retained_upstream_attempt_count: 1,
              retained_request_log_row_count: 1,
              legacy_unknown_row_count: 0,
              chain_complete: null,
              same_target_retry_occurred: false,
              hedge_occurred: false,
              failover_occurred: false,
              routing_evidence_complete: null,
              retained_rows_loaded_count: 1,
              retained_rows_page_complete: true,
              next_row_cursor: null,
              retained_rows: [createRequestLogListItem(scenario)],
            },
          ],
          has_more_chains: false,
          next_chain_cursor: null,
        });
      }
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

  return {
    auditDetailRequests,
    auditListSearchParams,
  };
}

export function expectAuditWindow(searchParamString: string) {
  const params = new URLSearchParams(searchParamString);
  expect(params.get("request_log_id")).toBe("101");
  expect(params.get("from")).toBe(expectedFromTime);
  expect(params.get("to")).toBe(expectedToTime);
  expect(params.get("limit")).toBe("20");
}

export async function expectNoRedundantPayloadShell(section: Locator) {
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

export const documentBodyCases = [
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
