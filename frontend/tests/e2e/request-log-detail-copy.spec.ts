import { expect, test, type BrowserContext, type Page } from "@playwright/test";

const timestamp = "2026-04-13T00:00:00Z";
const rawErrorDetail = JSON.stringify({
  error: {
    message: "Upstream request failed",
    type: "bad_gateway",
  },
});
const formattedErrorDetail = JSON.stringify(JSON.parse(rawErrorDetail), null, 2);
function createRequestLogDetail(routingOverrides: Record<string, unknown> = {}) {
  const baseRouting = {
    profile_id: 1,
    endpoint_label: "Primary endpoint",
    endpoint_id: 1,
    terminal_target_id: 501,
    selected_terminal_target_id: 501,
    endpoint_base_url: "https://api.example.test",
    endpoint_description: "Primary endpoint",
    audit_enabled_at_request: true,
    audit_capture_bodies_at_request: true,
  };
  const routing = {
    ...baseRouting,
    ...routingOverrides,
  };

  return {
    summary: {
      id: 101,
      created_at: timestamp,
      model_id: "gpt-4o-mini",
      resolved_target_model_id: null,
      api_family: "openai",
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
        filter_options: { models: [], endpoints: [], clients: [], resolved_target_models: [] },
      });
    }

    if (pathname === "/api/stats/requests/101") {
      return fulfillJson(detail);
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });
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

    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: (command: string) => {
        if (command === "copy") {
          const textarea = document.querySelector("textarea") as HTMLTextAreaElement | null;
          const usedSheetRoot = Boolean(textarea?.closest("[data-testid='request-log-detail-sheet']"));

          (window as Window & { __fallbackUsedSheetRoot?: boolean }).__fallbackUsedSheetRoot = usedSheetRoot;
          windowWithCopyCapture.__copiedText = usedSheetRoot ? textarea?.value ?? "" : "";

          return usedSheetRoot;
        }

        return false;
      },
    });
  });
  await page.goto("/observe/requests?request_id=101");
  await page.waitForLoadState("networkidle");

  const drawer = page.getByTestId("request-log-detail-sheet");

  await expect(drawer).toBeVisible({ timeout: 15000 });
  return { drawer, ownerRouteRequests };
}

test.describe("request log detail copy regression", () => {
  test("request-log detail overview error detail copy button writes the formatted block text", async ({ page, context }) => {
    const { drawer, ownerRouteRequests } = await openRequestLogDetail(page, context);
    const overviewCopyButton = drawer.getByRole("button", { name: /^Copy$/ });
    const routingContext = drawer.getByText("Routing context", { exact: true }).locator("xpath=..");

    await expect(drawer.getByText("Review requested model, final target model, selected terminal target, routing, tokens, costs, and request-time audit provenance.")).toBeVisible();
    await expect(routingContext).toBeVisible();
    await expect(routingContext.locator("span").filter({ hasText: /^Connection$/ })).toHaveCount(0);
    await expect(routingContext.getByRole("link", { name: "Open connection" })).toHaveCount(0);
    await expect(routingContext).toContainText("Selected Terminal Target");
    await expect(routingContext).toContainText("#501");
    await expect(routingContext).not.toContainText("Context-routing decision");
    await expect(routingContext).not.toContainText("context_routing");
    expect(ownerRouteRequests).toEqual([]);
    await expect(overviewCopyButton).toHaveCount(1);

    await drawer.locator("pre:visible").evaluateAll((elements) => {
      elements.forEach((element, index) => {
        element.textContent = `mutated-overview-${index}`;
      });
    });

    await expectCopyWithoutDownload(page, () => overviewCopyButton.click(), formattedErrorDetail);
  });

  test("request-log detail legacy rows do not collapse executed terminal targets into selected targets", async ({ page, context }) => {
    const legacyDetail = createRequestLogDetail({
      selected_terminal_target_id: null,
    });
    const { drawer } = await openRequestLogDetail(page, context, legacyDetail);
    const routingContext = drawer.getByText("Routing context", { exact: true }).locator("xpath=..");

    await expect(routingContext).toContainText("Selected Terminal Target");
    await expect(routingContext).toContainText("No terminal target selected");
    await expect(routingContext).not.toContainText("#501");
  });
});
