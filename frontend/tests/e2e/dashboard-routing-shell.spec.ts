import { expect, test, type Locator, type Page } from "@playwright/test";
import { createDashboardSnapshot } from "./dashboard-aggregate-fixtures";

const timestamp = "2026-04-11T00:00:00Z";
const mobileViewport = { width: 390, height: 844 };
const desktopViewport = { width: 1280, height: 900 };

function createProfile(id: number, name: string, isActive = false) {
  return {
    id,
    name,
    description: null,
    is_active: isActive,
    is_default: id === 1,
    is_editable: true,
    version: 1,
    created_at: timestamp,
    deleted_at: null,
    updated_at: timestamp,
  };
}

function createModelListItem() {
  return {
    id: 101,
    vendor_id: null,
    vendor: null,
    api_family: "openai" as const,
    model_id: "model-a",
    display_name: "Model A",
    model_type: "native",
    proxy_targets: [],
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    is_enabled: true,
    connection_count: 1,
    active_connection_count: 1,
    health_success_rate: 97.6,
    health_total_requests: 42,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createConnection() {
  return {
    id: 501,
    model_config_id: 101,
    endpoint_id: 201,
    endpoint: {
      id: 201,
      name: "Endpoint A",
      base_url: "https://endpoint-a.example",
      has_api_key: true,
      masked_api_key: "••••",
      position: 0,
      created_at: timestamp,
      updated_at: timestamp,
    },
    is_active: true,
    priority: 0,
    name: null,
    auth_type: null,
    custom_headers: null,
    openai_probe_endpoint_variant: null,
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    health_status: "healthy",
    health_detail: null,
    last_health_check: timestamp,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createModelDetail() {
  return {
    ...createModelListItem(),
    connections: [createConnection()],
  };
}

function createRecentRequestLogItem() {
  return {
    id: 301,
    created_at: timestamp,
    model_id: "model-a",
    model_label: "Model A",
    resolved_target_model_id: null,
    resolved_target_model_label: null,
    is_proxy_origin: false,
    caller_client_display: "Dashboard Fixture Row",
    upstream_client_display: "Dashboard Fixture Row",
    user_agent_overridden: false,
    api_family: "openai" as const,
    vendor_id: null,
    vendor_key: null,
    vendor_name: null,
    endpoint_id: 201,
    endpoint_label: "Endpoint A",
    connection_id: 501,
    ttft_ms: 80,
    completion_duration_ms: 240,
    status_code: 200,
    response_time_ms: 640,
    is_stream: false,
    stream_outcome: "not_streaming" as const,
    stream_error_kind: null,
    reasoning_effort: null,
    output_tokens: 48,
    total_tokens: 120,
    total_cost_user_currency_micros: 250000,
    priced_flag: true,
    unpriced_reason: null,
    report_currency_symbol: "$",
  };
}

function createRequestLogDetail() {
  return {
    summary: {
      id: 301,
      created_at: timestamp,
      model_id: "model-a",
      model_label: "Model A",
      resolved_target_model_id: null,
      resolved_target_model_label: null,
      api_family: "openai" as const,
      vendor_id: null,
      vendor_key: null,
      vendor_name: null,
      status_code: 200,
      response_time_ms: 640,
      is_stream: false,
      is_proxy_origin: false,
      ttft_ms: 80,
      completion_duration_ms: 240,
    },
    request: {
      request_path: "/v1/responses",
      ingress_request_id: "dashboard-ingress-301",
      attempt_number: 1,
      provider_correlation_id: "provider-corr-301",
      proxy_api_key_id: null,
      proxy_api_key_name_snapshot: null,
      caller_user_agent: "Dashboard Fixture Row",
      upstream_user_agent: "Dashboard Fixture Row",
      caller_client_display: "Dashboard Fixture Row",
      upstream_client_display: "Dashboard Fixture Row",
      user_agent_overridden: false,
      error_detail: null,
    },
    routing: {
      profile_id: 1,
      model_id: "model-a",
      resolved_target_model_id: null,
      endpoint_id: 201,
      endpoint_label: "Endpoint A",
      endpoint_base_url: "https://endpoint-a.example",
      endpoint_description: "Primary endpoint",
      connection_id: 501,
      audit_enabled_at_request: true,
    },
    usage: {
      input_tokens: 72,
      output_tokens: 48,
      total_tokens: 120,
      success_flag: true,
      billable_flag: true,
      priced_flag: true,
      unpriced_reason: null,
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 0,
      reasoning_tokens: 0,
    },
    costing: {
      input_cost_micros: 100000,
      output_cost_micros: 150000,
      cache_read_input_cost_micros: 0,
      cache_creation_input_cost_micros: 0,
      reasoning_cost_micros: 0,
      total_cost_original_micros: 250000,
      total_cost_user_currency_micros: 250000,
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

function createRequestLogsResponse() {
  return {
    items: [createRecentRequestLogItem()],
    total: 1,
    limit: 12,
    offset: 0,
    filter_options: {
      endpoints: [],
      models: [],
    },
  };
}

async function mockDashboardRoutes(
  page: Page,
  options: { dashboardSnapshot?: ReturnType<typeof createDashboardSnapshot> } = {},
) {
  const profiles = [createProfile(1, "Red Team", true)];
  const modelDetail = createModelDetail();
  const requestLogDetail = createRequestLogDetail();
  const dashboardSnapshot = options.dashboardSnapshot ?? createDashboardSnapshot({
    recentRequests: [createRecentRequestLogItem()],
  });

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname } = url;

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
        profiles,
        active_profile: profiles[0],
        profile_limits: { max_profiles: 5 },
      });
    }

    if (pathname === "/api/stats/dashboard") {
      return fulfillJson(dashboardSnapshot);
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
      return fulfillJson(createRequestLogsResponse());
    }

    if (pathname === "/api/stats/requests/301") {
      return fulfillJson(requestLogDetail);
    }

    if (pathname === "/api/models") {
      return fulfillJson([createModelListItem()]);
    }

    if (pathname === "/api/models/101") {
      return fulfillJson(modelDetail);
    }

    if (pathname === "/api/endpoints") {
      return fulfillJson([]);
    }

    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson([]);
    }

    if (pathname === "/api/pricing-templates") {
      return fulfillJson([]);
    }

    if (pathname === "/api/vendors") {
      return fulfillJson([]);
    }

    if (pathname === "/api/loadbalance/current-state") {
      return fulfillJson({ items: [] });
    }

    return fulfillJson({});
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
}

function getRoutingCard(page: Page) {
  return page.getByTestId("routing-diagram-card");
}

async function tabUntilFocused(page: Page, locator: Locator, maxTabs = 40) {
  await expect(locator).toBeVisible();

  for (let index = 0; index < maxTabs; index += 1) {
    const isFocused = await locator.evaluate((element) => element === document.activeElement);
    if (isFocused) {
      await expect(locator).toBeFocused();
      return;
    }

    await page.keyboard.press("Tab");
  }

  await expect(locator).toBeFocused();
}

async function expectNoHorizontalOverflow(page: Page, locator: Locator) {
  const [pageRootDimensions, elementDimensions] = await Promise.all([
    page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth,
    })),
    locator.evaluate((element) => ({
      clientWidth: element.clientWidth,
      scrollWidth: element.scrollWidth,
    })),
  ]);

  expect(pageRootDimensions.scrollWidth).toBeLessThanOrEqual(pageRootDimensions.clientWidth);
  expect(elementDimensions.scrollWidth).toBeLessThanOrEqual(elementDimensions.clientWidth);
}

async function getViewportTransform(diagram: Locator) {
  return diagram.locator(".react-flow__viewport").evaluate((element) => {
    if (!(element instanceof HTMLElement)) {
      return "";
    }

    return element.style.transform;
  });
}

async function dragLocatorBy(
  page: Page,
  locator: Locator,
  {
    deltaX,
    deltaY,
    startXRatio = 0.5,
    startYRatio = 0.5,
  }: {
    deltaX: number;
    deltaY: number;
    startXRatio?: number;
    startYRatio?: number;
  },
) {
  await expect(locator).toBeVisible();
  const box = await locator.boundingBox();

  expect(box).not.toBeNull();

  const startX = box!.x + (box!.width * startXRatio);
  const startY = box!.y + (box!.height * startYRatio);

  await page.mouse.move(startX, startY);
  await page.mouse.down();
  await page.mouse.move(startX + deltaX, startY + deltaY, { steps: 12 });
  await page.mouse.up();
}

function getBoundingBoxOrigin(box: NonNullable<Awaited<ReturnType<Locator["boundingBox"]>>>) {
  return {
    x: Math.round(box.x),
    y: Math.round(box.y),
  };
}

function expectBoundingBoxDistance(
  current: { x: number; y: number },
  reference: { x: number; y: number },
  {
    max,
    min,
  }: {
    max?: number;
    min?: number;
  },
) {
  const distance = Math.hypot(current.x - reference.x, current.y - reference.y);

  if (typeof max === "number") {
    expect(distance).toBeLessThanOrEqual(max);
  }

  if (typeof min === "number") {
    expect(distance).toBeGreaterThanOrEqual(min);
  }
}

async function expectNoDesktopNodeOverlap(diagram: Locator) {
  const overlaps = await diagram.locator(".react-flow__node[data-id]").evaluateAll((elements) => {
    const nodes = elements.map((element) => {
      const wrapperRect = element.getBoundingClientRect();
      const article = element.querySelector("article");
      const articleRect = article ? article.getBoundingClientRect() : wrapperRect;
      return {
        id: element.getAttribute("data-id"),
        columnX: Math.round(wrapperRect.x),
        top: articleRect.y,
        bottom: articleRect.y + articleRect.height,
      };
    });

    const byColumn = new Map<number, typeof nodes>();
    for (const node of nodes) {
      const items = byColumn.get(node.columnX) ?? [];
      items.push(node);
      byColumn.set(node.columnX, items);
    }

    const detected = [] as Array<{
      columnX: number;
      previous: string | null;
      current: string | null;
      previousBottom: number;
      currentTop: number;
    }>;

    for (const [columnX, items] of byColumn.entries()) {
      items.sort((left, right) => left.top - right.top);
      for (let index = 1; index < items.length; index += 1) {
        const previous = items[index - 1];
        const current = items[index];
        if (current.top < previous.bottom - 1) {
          detected.push({
            columnX,
            previous: previous.id,
            current: current.id,
            previousBottom: previous.bottom,
            currentTop: current.top,
          });
        }
      }
    }

    return detected;
  });

  expect(overlaps).toEqual([]);
}

test.describe("dashboard routing shell", () => {
  test("keeps the desktop routing shell chrome and React Flow drill-down behavior", async ({ page }) => {
    const consoleMessages: Array<{ text: string; type: string }> = [];
    page.on("console", (message) => {
      consoleMessages.push({ text: message.text(), type: message.type() });
    });

    await page.setViewportSize(desktopViewport);
    await mockDashboardRoutes(page);

    await page.goto("/dashboard?tab=routing");
    await expect(page).toHaveURL(/\/dashboard\?tab=routing$/);
    await expect(page.getByRole("tab")).toHaveText(["Overview", "Analytics", "Routing"]);
    await expect(page.getByRole("tab", { name: "Routing" })).toHaveAttribute("aria-selected", "true");

    const routingCard = getRoutingCard(page);
    const summaryPills = routingCard.locator('[aria-live="polite"]');
    const legendLabels = routingCard
      .getByTestId("routing-diagram-legend")
      .locator("span.font-medium");
    const desktopDiagram = routingCard.getByTestId("routing-diagram-desktop");
    const flowPane = desktopDiagram.locator(".react-flow__pane");
    const modelNode = desktopDiagram.getByTestId("routing-diagram-node-model-model-101");
    const modelAction = modelNode.getByRole("button", { name: "View model details for Model A" });
    const zoomInControl = desktopDiagram.getByRole("button", { name: /zoom in/i });
    const zoomOutControl = desktopDiagram.getByRole("button", { name: /zoom out/i });
    const fitViewControl = desktopDiagram.getByRole("button", { name: /fit view/i });

    await expect(routingCard).toBeVisible();
    await expect(routingCard).not.toHaveAttribute("data-slot", "card");
    await expect(routingCard.locator('[data-slot="card"]')).toHaveCount(0);
    await expect(desktopDiagram).toBeVisible();
    await expectNoDesktopNodeOverlap(desktopDiagram);
    await expect(page.getByTestId("routing-diagram-mobile")).toHaveCount(0);
    await expect(
      routingCard.getByText(/Desktop shows the backend-owned routing graph/i),
    ).toHaveCount(0);
    await expect(
      routingCard.getByText("Activate model or endpoint targets to open details or request logs"),
    ).toHaveCount(0);
    await expect(
      routingCard.getByText(/Entry Model -> Planner -> Access Targets -> Terminal Target -> Endpoint/i),
    ).toHaveCount(0);
    await expect(
      routingCard.getByText(/terminal-target topology after planner and access-target resolution/i),
    ).toHaveCount(0);
    await expect(
      routingCard.getByText(/browser does not reconstruct graph edges from management reads/i),
    ).toHaveCount(0);
    await expect.poll(async () => {
      const box = await desktopDiagram.boundingBox();
      return Math.round(box?.height ?? 0);
    }).toBeGreaterThanOrEqual(760);
    await expect(summaryPills).toHaveCount(0);
    await expect(zoomInControl).toBeVisible();
    await expect(zoomOutControl).toBeVisible();
    await expect(fitViewControl).toBeVisible();

    const viewportTransformBeforeZoom = await getViewportTransform(desktopDiagram);
    await zoomInControl.click();
    await expect.poll(() => getViewportTransform(desktopDiagram)).not.toBe(viewportTransformBeforeZoom);
    const viewportTransformAfterZoomIn = await getViewportTransform(desktopDiagram);
    await zoomOutControl.click();
    await expect.poll(() => getViewportTransform(desktopDiagram)).not.toBe(viewportTransformAfterZoomIn);

    await expect(legendLabels).toHaveText([
      "Model",
      "Terminal Targets",
      "Endpoint",
      "Disabled",
      "Inactive",
    ]);
    expect(
      consoleMessages
        .filter(({ type }) => type === "error")
        .map(({ text }) => text)
        .filter(
          (message) =>
            message.includes("cannot be a descendant") || message.includes("cannot contain a nested"),
        ),
    ).toEqual([]);
    expect(
      consoleMessages
        .map(({ text }) => text)
        .filter(
          (message) =>
            /react flow|reactflow/i.test(message) &&
            /(parent container|width and height)/i.test(message),
        ),
    ).toEqual([]);
    await expect(routingCard.getByText("Disabled Model", { exact: true })).toBeVisible();
    await expect(routingCard.getByText("Primary Target", { exact: true })).toBeVisible();
    await expect(routingCard.getByText("Backup Target", { exact: true })).toBeVisible();

    const viewportTransformBeforePan = await getViewportTransform(desktopDiagram);
    await dragLocatorBy(page, flowPane, {
      deltaX: -120,
      deltaY: -72,
      startXRatio: 0.9,
      startYRatio: 0.85,
    });
    const viewportTransformAfterPan = await getViewportTransform(desktopDiagram);

    expect(viewportTransformAfterPan).not.toBe(viewportTransformBeforePan);

    await fitViewControl.click();
    await expect.poll(() => getViewportTransform(desktopDiagram)).not.toBe(viewportTransformAfterPan);

    const modelNodeBeforeDrag = await modelNode.boundingBox();
    expect(modelNodeBeforeDrag).not.toBeNull();

    await dragLocatorBy(page, modelNode, {
      deltaX: 64,
      deltaY: 44,
      startXRatio: 0.25,
      startYRatio: 0.2,
    });

    const modelNodeAfterDrag = await modelNode.boundingBox();
    expect(modelNodeAfterDrag).not.toBeNull();
    expect(modelNodeAfterDrag!.x).not.toBe(modelNodeBeforeDrag!.x);
    expect(modelNodeAfterDrag!.y).not.toBe(modelNodeBeforeDrag!.y);

    await tabUntilFocused(page, modelAction);
    await expect(modelAction).toBeFocused();
    await expect(modelAction).toBeInViewport();
    await modelAction.press("Enter");
    await expect(page).toHaveURL(/\/models\/101$/);

    await page.goto("/dashboard?tab=routing");
    await expect(page).toHaveURL(/\/dashboard\?tab=routing$/);
    const refreshedRoutingCard = getRoutingCard(page);
    const endpointAction = refreshedRoutingCard
      .getByTestId("routing-diagram-desktop")
      .getByTestId("routing-diagram-node-endpoint-endpoint-201")
      .getByRole("button", {
        name: "View Request Logs: Endpoint A",
      });

    await tabUntilFocused(page, endpointAction);
    await expect(endpointAction).toBeFocused();
    await expect(endpointAction).toBeInViewport();
    await endpointAction.press("Enter");
    await expect(page).toHaveURL(/\/request-logs\?endpoint_id=201$/);
    await expect(page.getByRole("heading", { name: "Request Logs" })).toBeVisible();
    await expect(page.locator('input[name="request_id_lookup"]')).toBeVisible();
    await expect(page.locator('input[name="ingress_request_id"]')).toBeVisible();
  });

  test("preserves dragged desktop node positions across benign routing rerenders", async ({ page }) => {
    await page.setViewportSize(desktopViewport);
    await mockDashboardRoutes(page);

    await page.goto("/dashboard?tab=routing");
    await expect(page).toHaveURL(/\/dashboard\?tab=routing$/);

    const routingCard = getRoutingCard(page);
    const desktopDiagram = routingCard.getByTestId("routing-diagram-desktop");
    const modelNode = desktopDiagram.getByTestId("routing-diagram-node-model-model-101");
    const refreshButton = page.getByRole("button", { name: /refresh dashboard/i });

    await expect(desktopDiagram).toBeVisible();

    const modelNodeBeforeDrag = await modelNode.boundingBox();
    expect(modelNodeBeforeDrag).not.toBeNull();

    await dragLocatorBy(page, modelNode, {
      deltaX: 72,
      deltaY: 48,
      startXRatio: 0.25,
      startYRatio: 0.2,
    });

    const modelNodeAfterDrag = await modelNode.boundingBox();
    expect(modelNodeAfterDrag).not.toBeNull();

    const beforeDragOrigin = getBoundingBoxOrigin(modelNodeBeforeDrag!);
    const afterDragOrigin = getBoundingBoxOrigin(modelNodeAfterDrag!);

    expectBoundingBoxDistance(afterDragOrigin, beforeDragOrigin, { min: 40 });

    await Promise.all([
      page.waitForResponse(
        (response) =>
          response.url().includes("/api/stats/dashboard") &&
          response.request().method() === "GET" &&
          response.status() === 200,
      ),
      refreshButton.click(),
    ]);

    await expect(refreshButton).toBeEnabled();

    const modelNodeAfterRefresh = await modelNode.boundingBox();
    expect(modelNodeAfterRefresh).not.toBeNull();

    const afterRefreshOrigin = getBoundingBoxOrigin(modelNodeAfterRefresh!);

    expectBoundingBoxDistance(afterRefreshOrigin, afterDragOrigin, { max: 6 });
    expectBoundingBoxDistance(afterRefreshOrigin, beforeDragOrigin, { min: 40 });
  });

  test("covers the compact routing list at 390x844 with keyboard drill-down and no overflow", async ({ page }) => {
    await page.setViewportSize(mobileViewport);
    await mockDashboardRoutes(page);

    await page.goto("/dashboard?tab=routing");
    await expect(page).toHaveURL(/\/dashboard\?tab=routing$/);

    const routingCard = getRoutingCard(page);
    const modelAction = routingCard
      .getByRole("article")
      .filter({ has: page.getByRole("heading", { name: "Model A", level: 5 }) })
      .getByRole("button", { name: "View model details for Model A" });

    await expect(routingCard).toBeVisible();
    await expect(page.getByTestId("routing-diagram-mobile")).toBeVisible();
    await expect(page.getByTestId("routing-diagram-desktop")).toHaveCount(0);
    await expect(
      routingCard.getByText(/Desktop shows the backend-owned routing graph/i),
    ).toHaveCount(0);
    await expect(
      routingCard.getByText("Activate model or endpoint targets to open details or request logs"),
    ).toHaveCount(0);
    await expect(
      routingCard.getByText(/Entry Model -> Planner -> Access Targets -> Terminal Target -> Endpoint/i),
    ).toHaveCount(0);
    await expectNoHorizontalOverflow(page, routingCard);
    await tabUntilFocused(page, modelAction);
    await expect(modelAction).toBeFocused();
    await expect(modelAction).toBeInViewport();
    await modelAction.press("Enter");
    await expect(page).toHaveURL(/\/models\/101$/);

    await page.goto("/dashboard?tab=routing");
    await expect(page).toHaveURL(/\/dashboard\?tab=routing$/);
    const refreshedRoutingCard = getRoutingCard(page);
    const endpointAction = refreshedRoutingCard
      .getByRole("article")
      .filter({ has: page.getByRole("heading", { name: "Endpoint A", level: 5 }) })
      .getByRole("button", {
        name: "View Request Logs: Endpoint A",
      });

    await tabUntilFocused(page, endpointAction);
    await expect(endpointAction).toBeFocused();
    await expect(endpointAction).toBeInViewport();
    await endpointAction.press("Enter");
    await expect(page).toHaveURL(/\/request-logs\?endpoint_id=201$/);
  });

  test("does not render the removed routing strategy mix card", async ({ page }) => {
    await mockDashboardRoutes(page);

    await page.goto("/dashboard?tab=routing");
    await expect(page).toHaveURL(/\/dashboard\?tab=routing$/);

    await expect(page.getByText("Routing strategy mix")).toHaveCount(0);
    await expect(page.getByText("Legacy strategy 1")).toHaveCount(0);
    await expect(page.getByText("Adaptive strategy 1")).toHaveCount(0);
    await expect(page.getByText("Strategy not configured 1")).toHaveCount(0);
  });

  test("opens exact request investigation from recent activity", async ({ page }) => {
    await mockDashboardRoutes(page);

    await page.goto("/dashboard?tab=overview");

    const recentActivityCard = page.locator('[data-slot="card"]').filter({ hasText: "Recent Activity" }).first();

    await expect(recentActivityCard.getByRole("button", { name: /Model A/ })).toBeVisible();
    await recentActivityCard.getByRole("button", { name: /Model A/ }).click();

    await expect(page).toHaveURL(/\/request-logs\?request_id=301$/);
    await expect(page.getByTestId("request-log-detail-sheet")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Request #301" })).toBeVisible();
  });
});
