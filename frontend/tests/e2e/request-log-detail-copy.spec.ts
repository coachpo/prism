import { expect, test, type BrowserContext, type Page } from "@playwright/test";
import type { RequestLogDetailRouting } from "../../src/lib/types";

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

function normalizeClipboardText(value: string) {
  return value.split("\r\n").join("\n");
}

function createRequestLogDetail(routingOverrides: Partial<RequestLogDetailRouting> = {}) {
  const baseRouting = {
    profile_id: 1,
    endpoint_label: "Primary endpoint",
    endpoint_id: 1,
    terminal_target_id: 501,
    selected_terminal_target_id: 501,
    context_routing: {
      policy: "cheapest_eligible_context",
      selected_terminal_target_id: 501,
      estimation_method: "openai_chat_heuristic_v1",
      estimated_input_tokens: 120,
      reserved_output_tokens: 4096,
      estimated_total_context_tokens: 4216,
      usable_context_window_tokens: 115200,
      cost_ranking_method: "estimated_blended_request_cost_then_access_target_position_then_terminal_target_id",
      selected_estimated_blended_cost_micros: 1250,
      skipped_terminal_targets: [
        {
          terminal_target_id: 502,
          endpoint_id: 2,
          reason: "estimated_context_exceeds_usable_window",
          usable_context_window_tokens: 4096,
          estimated_total_context_tokens: 4216,
        },
      ],
    },
    endpoint_base_url: "https://api.example.test",
    endpoint_description: "Primary endpoint",
    audit_enabled_at_request: true,
    audit_capture_bodies_at_request: true,
  } satisfies RequestLogDetailRouting;
  const routing = {
    ...baseRouting,
    ...routingOverrides,
    context_routing: "context_routing" in routingOverrides
      ? routingOverrides.context_routing
      : baseRouting.context_routing,
  } satisfies RequestLogDetailRouting;

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
    routing,
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
  return page.evaluate(() => (window as Window & { __copiedText?: string }).__copiedText ?? "");
}

async function readFallbackMountState(page: Page) {
  return page.evaluate(() => (window as Window & { __fallbackUsedSheetRoot?: boolean }).__fallbackUsedSheetRoot ?? false);
}

async function readDownloadAttempted(page: Page) {
  return page.evaluate(() => (window as Window & { __downloadAttempted?: boolean }).__downloadAttempted ?? false);
}

async function resetCopyHarnessState(page: Page) {
  await page.evaluate(() => {
    (window as Window & { __copiedText?: string }).__copiedText = "";
    (window as Window & { __fallbackUsedSheetRoot?: boolean }).__fallbackUsedSheetRoot = false;
    (window as Window & { __downloadAttempted?: boolean }).__downloadAttempted = false;
  });
}

async function expectCopyWithoutDownload(page: Page, trigger: () => Promise<void>, expectedText: string) {
  await resetCopyHarnessState(page);

  const downloadTriggeredPromise = page
    .waitForEvent("download", { timeout: 500 })
    .then(async (download) => {
      await download.cancel().catch(() => {});
      return true;
    })
    .catch(() => false);

  await trigger();

  await expect.poll(() => readCopiedText(page)).toBe(expectedText);
  await expect.poll(() => readFallbackMountState(page)).toBe(true);
  await expect.poll(() => readDownloadAttempted(page)).toBe(false);
  expect(await downloadTriggeredPromise).toBe(false);
}

async function mockRequestLogDetailRoutes(
  page: Page,
  detail: ReturnType<typeof createRequestLogDetail> = createRequestLogDetail(),
) {
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

    if (pathname === "/api/stats/requests") {
      return fulfillJson({
        items: [],
        total: 0,
        limit: 20,
        offset: 0,
        filter_options: { models: [], endpoints: [] },
      });
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

async function openRequestLogDetail(
  page: Page,
  context: BrowserContext,
  detail: ReturnType<typeof createRequestLogDetail> = createRequestLogDetail(),
) {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const ownerRouteRequests: string[] = [];
  page.on("request", (request) => {
    const pathname = new URL(request.url()).pathname;
    if (/^\/api\/connections\/\d+\/owner$/.test(pathname)) {
      ownerRouteRequests.push(request.url());
    }
  });
  await mockRequestLogDetailRoutes(page, detail);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", {
      value: {
        writeText: () => Promise.reject(new Error("clipboard unavailable")),
      },
      configurable: true,
    });

    const windowWithCopyCapture = window as Window & { __copiedText?: string };
    windowWithCopyCapture.__copiedText = "";
    (window as Window & { __fallbackUsedSheetRoot?: boolean }).__fallbackUsedSheetRoot = false;
    (window as Window & { __downloadAttempted?: boolean }).__downloadAttempted = false;

    const nativeAnchorClick = HTMLAnchorElement.prototype.click;
    HTMLAnchorElement.prototype.click = function clickWithDownloadCapture(this: HTMLAnchorElement) {
      const href = this.getAttribute("href") ?? this.href ?? "";
      const looksLikeDownload = this.hasAttribute("download") || href.startsWith("blob:") || href.startsWith("data:");

      if (looksLikeDownload) {
        (window as Window & { __downloadAttempted?: boolean }).__downloadAttempted = true;
      }

      return nativeAnchorClick.call(this);
    };

    document.execCommand = ((command: string) => {
      if (command === "copy") {
        const textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
        const usedSheetRoot = Boolean(textarea?.closest("[data-testid='request-log-detail-sheet']"));

        (window as Window & { __fallbackUsedSheetRoot?: boolean }).__fallbackUsedSheetRoot = usedSheetRoot;
        windowWithCopyCapture.__copiedText = usedSheetRoot ? textarea?.value ?? "" : "";

        return usedSheetRoot;
      }

      return false;
    }) as typeof document.execCommand;
  });
  await page.goto("/request-logs?request_id=101");
  await page.waitForLoadState("networkidle");

  const drawer = page.getByTestId("request-log-detail-sheet");

  await expect(drawer).toBeVisible({ timeout: 15000 });
  return { drawer, ownerRouteRequests };
}

test.describe("request log detail copy regression", () => {
  test("overview error detail copy button writes the formatted block text", async ({ page, context }) => {
    const { drawer, ownerRouteRequests } = await openRequestLogDetail(page, context);
    const overviewCopyButton = drawer.getByRole("button", { name: /^Copy$/ });
    const routingContext = drawer.getByText("Routing context", { exact: true }).locator("xpath=..");

    await expect(routingContext).toBeVisible();
    await expect(routingContext.locator("span").filter({ hasText: /^Connection$/ })).toHaveCount(0);
    await expect(routingContext.getByRole("link", { name: "Open connection" })).toHaveCount(0);
    await expect(routingContext).toContainText("Selected Terminal Target");
    await expect(routingContext).toContainText("#501");
    await expect(routingContext).toContainText("Context-routing decision");
    await expect(routingContext).toContainText("cheapest_eligible_context");
    await expect(routingContext).toContainText("openai_chat_heuristic_v1");
    await expect(routingContext).toContainText("estimated_blended_request_cost_then_access_target_position_then_terminal_target_id");
    await expect(routingContext).toContainText("Selected estimated blended cost");
    await expect(routingContext).toContainText("1,250 micros");
    await expect(routingContext).toContainText("Skipped terminal targets");
    await expect(routingContext).toContainText("#502");
    await expect(routingContext).toContainText("Estimated context exceeds usable context window");
    expect(ownerRouteRequests).toEqual([]);
    await expect(overviewCopyButton).toHaveCount(1);

    await drawer.locator("pre:visible").evaluateAll((elements) => {
      elements.forEach((element, index) => {
        element.textContent = `mutated-overview-${index}`;
      });
    });

    await expectCopyWithoutDownload(page, () => overviewCopyButton.click(), formattedErrorDetail);
  });

  test("legacy request-log rows do not collapse executed terminal targets into selected targets", async ({ page, context }) => {
    const legacyDetail = createRequestLogDetail({
      selected_terminal_target_id: undefined,
      context_routing: null,
    });
    const { drawer } = await openRequestLogDetail(page, context, legacyDetail);
    const routingContext = drawer.getByText("Routing context", { exact: true }).locator("xpath=..");

    await expect(routingContext).toContainText("Selected Terminal Target");
    await expect(routingContext).toContainText("No terminal target selected");
    await expect(routingContext).not.toContainText("#501");
  });

  test("audit payload copy buttons write their corresponding payload blocks", async ({ page, context }) => {
    const { drawer, ownerRouteRequests } = await openRequestLogDetail(page, context);

    await drawer.getByRole("tab", { name: "Audit" }).click();
    expect(ownerRouteRequests).toEqual([]);

    const copyButtons = drawer.getByRole("button", { name: /^Copy$/ });
    const visiblePreBlocks = drawer.locator("pre:visible");

    await expect(copyButtons).toHaveCount(3);
    await expect(visiblePreBlocks).toHaveCount(4);

    await visiblePreBlocks.evaluateAll((elements) => {
      elements[1].textContent = "mutated-audit-headers";
      elements[2].textContent = "mutated-audit-request";
      elements[3].textContent = "mutated-audit-response";
    });

    await expectCopyWithoutDownload(page, () => copyButtons.nth(0).click(), normalizeClipboardText(auditHeaders));
    await expectCopyWithoutDownload(page, () => copyButtons.nth(1).click(), normalizeClipboardText(auditRequestBody));
    await expectCopyWithoutDownload(page, () => copyButtons.nth(2).click(), normalizeClipboardText(auditResponseBody));
  });
});
