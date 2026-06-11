import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { expect, test, type Locator, type Page } from "@playwright/test";

const timestamp = "2026-05-28T12:00:00Z";
const routeReadyTimeout = 15_000;
const evidenceDir = resolve(process.cwd(), "../.omo/evidence/frontend-rewrite");
const keyboardTracePath = resolve(evidenceDir, "task-17-keyboard-tables.zip");
const cursorScreenshotPath = resolve(evidenceDir, "task-17-cursor-url.png");
const configAuthoringDefaults = {
  context_window_tokens: null,
  default_output_token_reserve: 4096,
  max_context_utilization: 0.9,
  preferred_context_utilization_threshold: null,
};
const facadePolicyDefaults = {
  facade_enabled: true,
  facade_selection_policy: "weighted_eligible_context",
  facade_fallback_policy: "redistribute_ineligible_weight",
};

test.beforeAll(() => {
  mkdirSync(evidenceDir, { recursive: true });
});

function profile() {
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

function model(id: number, modelId: string, displayName: string) {
  return {
    id,
    profile_id: 1,
    vendor_id: null,
    vendor: null,
    api_family: "openai",
    model_id: modelId,
    display_name: displayName,
    loadbalance_strategy_id: 10,
    loadbalance_strategy: null,
    access_targets: [],
    is_enabled: true,
    connection_count: 1,
    active_connection_count: 1,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

const endpoint = {
  id: 201,
  profile_id: 1,
  name: "Terminal endpoint",
  base_url: "https://terminal.example.invalid",
  has_api_key: true,
  masked_api_key: "••••terminal",
  position: 0,
  created_at: timestamp,
  updated_at: timestamp,
};

const strategy = {
  id: 10,
  profile_id: 1,
  name: "Default legacy routing",
  legacy_strategy_type: "fill-first",
  failure_status_codes: [429, 500],
  ban_mode: "temporary",
  retry_base_delay_ms: 1000,
  retry_backoff_multiplier: 2,
  retry_jitter_ratio: 0.2,
  retry_max_delay_ms: 8000,
  cycle_retry_attempt_limit: 3,
  ban_cumulative_retry_attempt_threshold: 4,
  ban_duration_seconds: 60,
  attached_model_count: 1,
  created_at: timestamp,
  updated_at: timestamp,
};

const requestedModel = model(101, "public-model", "Public Model");
const finalTargetModel = model(102, "terminal-model", "Terminal Model");

function task17Models() {
  return [
    requestedModel,
    finalTargetModel,
    ...Array.from({ length: 28 }, (_, index) => model(200 + index, `aux-${String(index + 1).padStart(2, "0")}`, `Aux Model ${String(index + 1).padStart(2, "0")}`)),
  ];
}

function requestLogItem(id = 301) {
  return {
    id,
    created_at: timestamp,
    model_id: requestedModel.model_id,
    model_label: requestedModel.display_name,
    resolved_target_model_id: finalTargetModel.model_id,
    resolved_target_model_label: finalTargetModel.display_name,
    caller_client_display: "Task 17 test client",
    upstream_client_display: "Task 17 test client",
    user_agent_overridden: false,
    api_family: "openai",
    vendor_id: null,
    vendor_key: null,
    vendor_name: null,
    endpoint_id: endpoint.id,
    endpoint_label: endpoint.name,
    connection_id: 501,
    ttft_ms: 42,
    completion_duration_ms: 142,
    status_code: 200,
    response_time_ms: 180,
    is_stream: false,
    stream_outcome: "not_streaming",
    stream_error_kind: null,
    reasoning_effort: null,
    output_tokens: 8,
    total_tokens: 18,
    total_cost_user_currency_micros: 1200,
    priced_flag: true,
    unpriced_reason: null,
    report_currency_symbol: "$",
  };
}

function requestLogDetail(id = 301) {
  return {
    summary: {
      ...requestLogItem(id),
      vendor_id: null,
      vendor_key: null,
      vendor_name: null,
      stream_error_detail: null,
    },
    request: {
      request_path: "/v1/chat/completions",
      ingress_request_id: "ingress-task-17",
      attempt_number: 1,
      provider_correlation_id: null,
      proxy_api_key_id: null,
      proxy_api_key_name_snapshot: null,
      caller_user_agent: "Task 17 test client",
      upstream_user_agent: "Task 17 test client",
      caller_client_display: "Task 17 test client",
      upstream_client_display: "Task 17 test client",
      user_agent_overridden: false,
      request_generation_params: null,
      request_generation_params_status: null,
      error_detail: null,
    },
    routing: {
      profile_id: 1,
      endpoint_label: endpoint.name,
      endpoint_id: endpoint.id,
      connection_id: 501,
      endpoint_base_url: endpoint.base_url,
      endpoint_description: null,
      audit_enabled_at_request: false,
      audit_capture_bodies_at_request: false,
    },
    usage: {
      input_tokens: 10,
      output_tokens: 8,
      total_tokens: 18,
      success_flag: true,
      billable_flag: true,
      priced_flag: true,
      unpriced_reason: null,
      cache_read_input_tokens: null,
      cache_creation_input_tokens: null,
      reasoning_tokens: null,
    },
    costing: {
      input_cost_micros: 600,
      output_cost_micros: 600,
      cache_read_input_cost_micros: null,
      cache_creation_input_cost_micros: null,
      reasoning_cost_micros: null,
      total_cost_original_micros: 1200,
      total_cost_user_currency_micros: 1200,
      currency_code_original: "USD",
      report_currency_code: "USD",
      report_currency_symbol: "$",
      fx_rate_used: null,
      fx_rate_source: null,
    },
    pricing: {
      pricing_snapshot_unit: null,
      pricing_snapshot_input: null,
      pricing_snapshot_output: null,
      pricing_snapshot_cache_read_input: null,
      pricing_snapshot_cache_creation_input: null,
      pricing_snapshot_reasoning: null,
      pricing_config_version_used: null,
    },
  };
}

function profileBundleV3() {
  return {
    version: 3,
    bundle_kind: "profile_config",
    exported_at: timestamp,
    vendor_refs: [],
    endpoints: [{ name: endpoint.name, base_url: endpoint.base_url, api_key_secret_ref: null, position: 0 }],
    pricing_templates: [],
    connections: [{
      ref: "terminal-connection",
      name: "Terminal connection",
      endpoint_name: endpoint.name,
      api_family: "openai",
      ...configAuthoringDefaults,
      pricing_template_name: null,
      is_active: true,
      auth_type: null,
      custom_headers: null,
      openai_text_capability: "dual_native",
      openai_probe_endpoint_variant: null,
      qps_limit: null,
      max_in_flight_non_stream: null,
      max_in_flight_stream: null,
    }],
    loadbalance_strategies: [{
      name: strategy.name,
      legacy_strategy_type: "fill-first",
      failure_status_codes: [429, 500],
      ban_mode: "temporary",
      retry_base_delay_ms: 1000,
      retry_backoff_multiplier: 2,
      retry_jitter_ratio: 0.2,
      retry_max_delay_ms: 8000,
      cycle_retry_attempt_limit: 3,
      ban_cumulative_retry_attempt_threshold: 4,
      ban_duration_seconds: 60,
    }],
    models: [{
      vendor_key: null,
      api_family: "openai",
      model_id: requestedModel.model_id,
      display_name: requestedModel.display_name,
      loadbalance_strategy_name: strategy.name,
      ...configAuthoringDefaults,
      ...facadePolicyDefaults,
      is_enabled: true,
      access_targets: [{ position: 0, is_enabled: true, target_type: "connection", connection_ref: "terminal-connection" }],
    }],
    profile_settings: null,
    header_blocklist_rules: [],
    user_agent_client_rules: [],
    secret_payload: { kind: "encrypted", cipher: "fernet-v1", key_id: "task-17", entries: [] },
  };
}

function pricingTemplate(id: number, name: string) {
  return { id, profile_id: 1, name, description: `${name} pricing`, pricing_unit: "PER_1M", pricing_currency_code: "USD", input_price: String(id), output_price: String(id + 1), cached_input_price: "0", cache_creation_price: "0", reasoning_price: "0", version: id, created_at: timestamp, updated_at: timestamp };
}

function task17PricingTemplates() {
  return Array.from({ length: 12 }, (_, index) => pricingTemplate(index + 1, index === 10 ? "Beta Task 17" : `Pricing ${String(index + 1).padStart(2, "0")}`));
}

function sidecar(id: number, name: string, state: "valid" | "missing_management_auth" = "valid") {
  const hostSlug = name.toLowerCase().replace(/ /g, "-");
  return { id, name, base_url: `https://${hostSlug}.example.invalid`, base_url_canonical: `https://${hostSlug}.example.invalid`, enabled: true, environment_label: id % 2 === 0 ? "production" : "staging", management_auth_state: state, credential_state: { management_password_configured: state === "valid" }, allow_insecure_http: false, skip_tls_verify: false, allow_private_network: false, last_sync_at: timestamp, last_successful_sync_at: state === "valid" ? timestamp : null, last_sync_error: state === "valid" ? null : "Missing management auth", snapshot_stale_after: "2099-01-01T00:00:00Z", pause_metadata: null, version: 1, created_at: timestamp, updated_at: timestamp };
}

function task17Sidecars() {
  return Array.from({ length: 12 }, (_, index) => sidecar(index + 1, index === 10 ? "Beta Sidecar" : `Sidecar ${String(index + 1).padStart(2, "0")}`, index % 5 === 0 ? "missing_management_auth" : "valid"));
}

async function expectFirstActionGroupFitsButtonCluster(table: Locator) {
  const firstActionCell = table.locator("tbody tr").first().locator("td").last();
  const firstActionButton = firstActionCell.getByRole("button").first();
  await expect(firstActionButton).toBeVisible({ timeout: routeReadyTimeout });

  const actionGroup = firstActionButton.locator("xpath=..");
  await expect(actionGroup).toHaveClass(/(?:^|\s)inline-flex(?:\s|$)/);
  await expect(actionGroup).toHaveClass(/(?:^|\s)w-fit(?:\s|$)/);

  const buttons = actionGroup.getByRole("button");
  const buttonCount = await buttons.count();
  expect(buttonCount).toBeGreaterThan(0);

  const groupBox = await actionGroup.boundingBox();
  const firstButtonBox = await buttons.first().boundingBox();
  const lastButtonBox = await buttons.nth(buttonCount - 1).boundingBox();

  if (!groupBox || !firstButtonBox || !lastButtonBox) {
    throw new Error("Expected visible action group and icon buttons to have bounding boxes");
  }

  const buttonClusterWidth = lastButtonBox.x + lastButtonBox.width - firstButtonBox.x;
  expect(groupBox.width).toBeGreaterThanOrEqual(buttonClusterWidth);
  expect(groupBox.width).toBeLessThanOrEqual(buttonClusterWidth + 12);
}

async function mockRoutes(page: Page) {
  const activeProfile = profile();

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname } = url;

    if (!pathname.startsWith("/api/")) return route.continue();

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false });
    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({ profiles: [activeProfile], active_profile: activeProfile, profile_limits: { max_profiles: 5 } });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" });
    if (pathname === "/api/settings/auth") return fulfillJson({ auth_enabled: false, username: null, has_password: false, email: null, pending_email: null, email_bound_at: null, email_verification_required: false });
    if (pathname === "/api/settings/log-retention") return fulfillJson({ request_logs_retention_days: 30, statistics_retention_days: 30, audit_logs_retention_days: 30, loadbalance_events_retention_days: 30 });
    if (pathname === "/api/vendors") return fulfillJson([]);
    if (pathname === "/api/config/header-blocklist-rules") return fulfillJson([]);
    if (pathname === "/api/config/user-agent-client-rules") return fulfillJson([]);
    if (pathname === "/api/models") return fulfillJson(task17Models());
    if (pathname === "/api/endpoints") return fulfillJson([endpoint]);
    if (pathname === "/api/models/by-endpoints") {
      return fulfillJson({ items: [{ endpoint_id: endpoint.id, models: [requestedModel, finalTargetModel] }] });
    }
    if (pathname === "/api/loadbalance/strategies") return fulfillJson([strategy]);
    if (pathname === "/api/stats/models/metrics" && request.method() === "POST") {
      return fulfillJson({ items: task17Models().map((item, index) => ({ model_id: item.model_id, success_rate: 99 - index, request_count_24h: 100 + index, p95_latency_ms: 40 + index, spend_30d_micros: 1000 + index })) });
    }
    if (pathname === "/api/pricing-templates" && request.method() === "GET") return fulfillJson(task17PricingTemplates());
    if (/^\/api\/pricing-templates\/\d+$/.test(pathname) && request.method() === "GET") return fulfillJson(task17PricingTemplates().find((item) => pathname.endsWith(`/${item.id}`)) ?? task17PricingTemplates()[0]);
    if (/^\/api\/pricing-templates\/\d+\/connections$/.test(pathname)) return fulfillJson({ items: [] });
    if (pathname === "/api/sidecars" && request.method() === "GET") return fulfillJson({ items: task17Sidecars() });
    if (/^\/api\/sidecars\/\d+$/.test(pathname) && request.method() === "GET") return fulfillJson(task17Sidecars().find((item) => pathname.endsWith(`/${item.id}`)) ?? task17Sidecars()[0]);
    if (/^\/api\/sidecars\/\d+\/auth-files$/.test(pathname)) return fulfillJson({ items: [] });
    if (/^\/api\/sidecars\/\d+\/provider-snapshots$/.test(pathname)) return fulfillJson({ items: [] });
    if (pathname === "/api/stats/requests") {
      const offset = Number(url.searchParams.get("offset") ?? url.searchParams.get("cursor") ?? "0");
      const id = offset >= 100 ? 302 : 301;
      return fulfillJson({
        items: [requestLogItem(id)],
        total: 150,
        limit: Number(url.searchParams.get("limit") ?? "100"),
        offset,
        filter_options: { models: [{ model_id: requestedModel.model_id, model_label: requestedModel.display_name }], endpoints: [{ endpoint_id: endpoint.id, endpoint_label: endpoint.name }] },
      });
    }
    if (pathname === "/api/stats/requests/301") return fulfillJson(requestLogDetail(301));
    if (pathname === "/api/stats/requests/302") return fulfillJson(requestLogDetail(302));
    if (pathname === "/api/config/profile/import/preview") {
      return fulfillJson({
        ready: true,
        version: 3,
        bundle_kind: "profile_config",
        preview_token: "task-17-preview",
        bundle_fingerprint: "task-17-fingerprint",
        replacement_scope: { target: "selected_profile", endpoints: 1, pricing_templates: 0, loadbalance_strategies: 1, models: 1, connections: 1, header_blocklist_rules: 0, user_agent_client_rules: 0, profile_settings: false },
        untouched_scope: { other_profiles: true, existing_global_vendor_metadata: true, request_logs: true },
        vendor_summary: { create_count: 0, reuse_count: 0, warning_count: 0 },
        secret_summary: { endpoint_secret_refs: 0, secret_payload_entries: 0, decryptable_secret_refs: 0 },
        endpoints_imported: 1,
        pricing_templates_imported: 0,
        strategies_imported: 1,
        models_imported: 1,
        connections_imported: 1,
        vendor_resolutions: [],
        secret_key_id: "task-17",
        decryptable_secret_refs: [],
        blocking_errors: [],
        warnings: [],
      });
    }
    if (pathname === "/api/config/profile/import") {
      return fulfillJson({ endpoints_imported: 1, pricing_templates_imported: 0, strategies_imported: 1, models_imported: 1, connections_imported: 1 });
    }

    return fulfillJson({ error: `Unhandled ${request.method()} ${pathname}` }, 500);
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
}

test("legacy strategy ui and request log target labels", async ({ page }) => {
  await mockRoutes(page);

  await page.goto("/route/ban-policies");
  await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: routeReadyTimeout });
  await expect(page.getByText("Loading application...")).toHaveCount(0, { timeout: routeReadyTimeout });
  await expect(page.getByText("Ban Policy").first()).toBeVisible({ timeout: routeReadyTimeout });
  await expect(page.getByText("terminal-target selection families plus Ban Policy").first()).toBeVisible();
  await expect(page.getByText(/Adaptive|Auto Recovery|Routing Policy/)).toHaveCount(0);

  await page.goto("/observe/requests");
  await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: routeReadyTimeout });
  await expect(page.getByText("Loading application...")).toHaveCount(0, { timeout: routeReadyTimeout });
  const requestLogsTable = page.getByTestId("request-logs-table");
  await expect(requestLogsTable).toBeVisible({ timeout: routeReadyTimeout });
  await expect(requestLogsTable.getByText("Requested Model", { exact: true })).toBeVisible({ timeout: routeReadyTimeout });
  await expect(requestLogsTable.getByText("Final Target Model", { exact: true })).toBeVisible();
  await expect(requestLogsTable.getByText("Public Model", { exact: true })).toBeVisible({ timeout: routeReadyTimeout });
  await expect(requestLogsTable.getByText("Terminal Model", { exact: true })).toBeVisible();
  await expect(page.getByText(/Proxy origin|Resolved target/)).toHaveCount(0);

  await page.getByTestId("request-logs-table").getByRole("button").first().click();
  await expect(page.getByTestId("request-log-detail-sheet")).toBeVisible();
  await expect(page.getByText("Requested Model").first()).toBeVisible();
  await expect(page.getByText("Final Target Model").first()).toBeVisible();
  await expect(page.getByText("Terminal Model").first()).toBeVisible();
});

test("endpoint reachable chained models", async ({ page }) => {
  await mockRoutes(page);

  await page.goto("/route/endpoints");
  await expect(page.getByText("Reachable Models")).toBeVisible();
  await expect(page.getByText("Public Model")).toBeVisible();
  await expect(page.getByText("Terminal Model")).toBeVisible();
});

test("settings config bundle v3 preview summary", async ({ page }) => {
  await mockRoutes(page);
  const bundle = JSON.stringify(profileBundleV3());

  await page.goto("/system/settings#backup");
  await page.getByTestId("profile-import-file").setInputFiles({
    name: "profile-v3.json",
    mimeType: "application/json",
    buffer: Buffer.from(bundle),
  });

  await expect(page.getByText("1 top-level connections")).toBeVisible();
  await page.getByTestId("profile-import-preview").click();
  await expect(page.getByText("Top-level Connections", { exact: true })).toBeVisible();
  await page.getByTestId("profile-import-apply").click();
  await expect(page.getByText("Imported 1 endpoints, 1 strategies, 1 models, 1 top-level connections")).toBeVisible();
});

test("task 17 operational tables keep dense controls keyboard accessible", async ({ page, context }) => {
  await mockRoutes(page);
  await context.tracing.start({ screenshots: true, snapshots: true });

  await page.goto("/models");
  const modelsTable = page.getByTestId("models-table");
  await expect(modelsTable).toBeVisible({ timeout: routeReadyTimeout });
  await expectFirstActionGroupFitsButtonCluster(modelsTable);
  await modelsTable.getByRole("button", { name: "Model ID", exact: true }).click();
  await expect(modelsTable.getByText("Aux Model 01", { exact: true })).toBeVisible();
  await page.getByPlaceholder("Search models...").fill("Aux Model 28");
  await expect(modelsTable.getByText("Aux Model 28", { exact: true })).toBeVisible();
  await expect(modelsTable.getByText("Public Model", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: /delete.*Aux Model 28/i }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog").getByText("Delete Model")).toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByPlaceholder("Search models...").fill("");
  await modelsTable.getByRole("button", { name: "Next Page" }).click();
  await expect(modelsTable.getByText("Aux Model 28", { exact: true })).toBeVisible();

  await page.goto("/route/pricing");
  const pricingTable = page.getByTestId("pricing-templates-table");
  await expect(pricingTable).toBeVisible({ timeout: routeReadyTimeout });
  await expectFirstActionGroupFitsButtonCluster(pricingTable);
  await pricingTable.getByRole("button", { name: "Name" }).click();
  await expect(pricingTable.getByText("Pricing 12", { exact: true })).toBeVisible();
  await pricingTable.getByLabel("Filter pricing templates").fill("Beta");
  await expect(pricingTable.getByText("Beta Task 17", { exact: true })).toBeVisible();
  await expect(pricingTable.getByText("Pricing 01", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "View usage Beta Task 17" }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog").getByText("Template Usage")).toBeVisible();
  await page.keyboard.press("Escape");
  await pricingTable.getByLabel("Filter pricing templates").fill("");
  await pricingTable.getByRole("button", { name: "Next Page" }).click();
  await expect(pricingTable.getByText("Beta Task 17", { exact: true })).toBeVisible();

  await page.goto("/control/sidecars");
  const sidecarsTable = page.getByTestId("sidecars-summary");
  await expect(sidecarsTable).toBeVisible({ timeout: routeReadyTimeout });
  await sidecarsTable.getByRole("button", { name: "Name" }).click();
  await page.getByLabel("Filter sidecars").fill("Beta");
  await expect(sidecarsTable.getByText("Beta Sidecar", { exact: true })).toBeVisible();
  await expect(sidecarsTable.getByText("Sidecar 01", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "View details: Beta Sidecar" }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByTestId("sidecar-detail").getByText("Beta Sidecar detail")).toBeVisible();
  await page.getByLabel("Filter sidecars").fill("");
  await sidecarsTable.getByRole("button", { name: "Next Page" }).click();
  await expect(sidecarsTable.getByText("Beta Sidecar", { exact: true })).toBeVisible();

  await page.goto("/observe/requests?model=public-model&cursor=100");
  const requestLogsTable = page.getByTestId("request-logs-table");
  await expect(requestLogsTable).toBeVisible({ timeout: routeReadyTimeout });
  await expect(page).toHaveURL(/model=public-model/);
  await expect(page).toHaveURL(/cursor=100/);
  const row = page.getByTestId("request-log-row-302");
  await expect(row).toBeVisible({ timeout: routeReadyTimeout });
  await row.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByTestId("request-log-detail-sheet")).toBeVisible();
  await expect(page).toHaveURL(/model=public-model/);
  await expect(page).toHaveURL(/cursor=100/);
  await expect(page).toHaveURL(/selected_request_id=302/);
  await page.screenshot({ path: cursorScreenshotPath, fullPage: true });

  await context.tracing.stop({ path: keyboardTracePath });
});
