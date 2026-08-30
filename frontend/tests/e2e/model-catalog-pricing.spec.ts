// models.dev 目录集成 journey：绑定（唯一精确自动匹配）、元信息卡状态、
// Terminal Target 的目录价格生成，以及价格页的模板导入。后端流量全部 mock。
import { expect, test, type Page } from "@playwright/test";
import {
  createEmptyIngressSpendingReport,
  expectIngressSpendingRequest,
} from "./spending-report-fixtures";

const timestamp = "2026-08-25T12:00:00Z";

function strategy() {
  return {
    id: 11,
    profile_id: 1,
    name: "Default fill-first routing",
    legacy_strategy_type: "fill-first",
    is_default: true,
    failure_status_codes: [429, 500],
    ban_mode: "off",
    retry_base_delay_ms: 1000,
    retry_backoff_multiplier: 2,
    retry_jitter_ratio: 0.2,
    retry_max_delay_ms: 8000,
    cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 0,
    ban_duration_seconds: 0,
    attached_model_count: 1,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function endpoint() {
  return {
    id: 1,
    profile_id: 1,
    name: "OpenAI Primary",
    base_url: "https://api.openai.com/v1",
    has_api_key: true,
    api_key_fingerprint: null,
    api_key_updated_at: timestamp,
    config_revision: 1,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function connection(id: number, name: string) {
  return {
    id,
    profile_id: 1,
    api_family: "openai",
    endpoint_id: 1,
    endpoint: endpoint(),
    is_active: true,
    priority: 0,
    name,
    auth_type: null,
    custom_headers: {},
    custom_headers_redacted: [],
    custom_request_parameters: null,
    routing_schedule: null,
    routing_schedule_state: null,
    openai_text_capability: "dual_native",
    openai_image_capability: null,
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

const connectionA = connection(15, "Primary Chat");

function accessTargets(connectionSnapshot: Record<string, unknown> = connectionA) {
  return [
    {
      id: 91,
      target_type: "connection",
      target_model_id: null,
      connection_id: 15,
      terminal_target_id: 15,
      position: 0,
      is_enabled: true,
      target_model: null,
      connection: connectionSnapshot,
      terminal_target: {
        id: 15,
        name: "Primary Chat",
        is_active: true,
        endpoint_id: 1,
        endpoint: {
          id: 1,
          name: "OpenAI Primary",
          base_url: "https://api.openai.com/v1",
        },
      },
      created_at: timestamp,
      updated_at: timestamp,
    },
  ];
}

const sourceMetadata = {
  name: "GPT Long",
  description: "long context fixture",
  family: "gpt-long",
  release_date: "2026-03",
  last_updated: "2026-03",
  knowledge: null,
  attachment: false,
  reasoning: true,
  tool_call: true,
  structured_output: true,
  temperature: true,
  modalities_input: ["text"],
  modalities_output: ["text"],
  limit_context: 400000,
  limit_input: null,
  limit_output: 32768,
  open_weights: false,
  status: null,
};

function emptyMetadata() {
  return {
    name: null,
    description: null,
    family: null,
    release_date: null,
    last_updated: null,
    knowledge: null,
    attachment: null,
    reasoning: null,
    tool_call: null,
    structured_output: null,
    temperature: null,
    modalities_input: null,
    modalities_output: null,
    limit_context: null,
    limit_input: null,
    limit_output: null,
    open_weights: null,
    status: null,
  };
}

function unboundCatalog() {
  return {
    bound: false,
    auto_match: { available: false, unique: false, candidates: [] },
    ...emptyMetadata(),
  };
}

function boundCatalog() {
  return {
    bound: true,
    match_source: "unique_match",
    provider_id: "openai",
    catalog_model_id: "gpt-long",
    catalog_revision: '"catalog-e2e-1"',
    fetched_at: timestamp,
    updated_at: timestamp,
    source: sourceMetadata,
    override: null,
    effective: sourceMetadata,
  };
}

function modelDetail(
  catalog: unknown,
  connectionSnapshot: Record<string, unknown> = connectionA,
) {
  return {
    id: 7,
    profile_id: 1,
    api_family: "openai",
    model_id: "detail-openai",
    display_name: "Detail OpenAI",
    loadbalance_strategy_id: 11,
    loadbalance_strategy: strategy(),
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    access_targets: accessTargets(connectionSnapshot),
    is_enabled: true,
    catalog,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function catalogPricingTemplate(providerID: string, catalogModelID: string) {
  return {
    id: 33,
    profile_id: 1,
    name: `${providerID}/${catalogModelID}`,
    description: null,
    pricing_unit: "PER_1M",
    pricing_currency_code: "USD",
    active_currency_symbol: "$",
    template_kind: "tiered",
    base_card: {
      input_price: "30",
      output_price: "180",
      cached_input_price: "0",
      cache_creation_price: "2.5",
      reasoning_price: null,
    },
    tier: {
      input_tokens_above: 272000,
      card: {
        input_price: "60",
        output_price: "270",
        cached_input_price: "0.5",
        cache_creation_price: "5",
        reasoning_price: null,
      },
    },
    version: 1,
    revision_id: 66,
    version_effective_at: timestamp,
    reporting_currency_epoch: 1,
    revision_count: 1,
    catalog_provider_id: providerID,
    catalog_model_id: catalogModelID,
    revision_source: "catalog",
    catalog_revision: '"catalog-e2e-1"',
    created_at: timestamp,
    updated_at: timestamp,
  };
}

async function mockCatalogRoutes(page: Page) {
  const state = {
    bound: false,
    // "unique_match" drives the automatic branch; "no_match" forces the
    // operator through the bounded candidate search.
    matchReason: "unique_match" as "unique_match" | "no_match" | "ambiguous",
    connection: { ...connectionA } as Record<string, unknown>,
    models: [] as unknown[],
    pricingTemplates: [] as unknown[],
    bindRequests: [] as unknown[],
    commitRequests: [] as unknown[],
    previewRequests: [] as unknown[],
    modelDetailReads: 0,
    modelConnectionReads: 0,
  };

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
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
      return fulfillJson({
        state: "disabled",
        transition_state: null,
        login_available: false,
        effective_generation: "1",
        retry_after_seconds: null,
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
    if (pathname === "/api/loadbalance/current-state") {
      return fulfillJson({
        generated_at: timestamp,
        scope: "process",
        instance_id: "e2e",
        configuration_revision: "1",
        completeness: {
          state: "ready",
          complete: true,
          configured_target_count: 1,
          observed_target_count: 0,
          unobserved_target_count: 1,
          observed_subset_counts: {},
        },
        items: [],
        has_more: false,
        next_cursor: null,
      });
    }
    if (pathname === "/api/models/7/routing-diagnostics") {
      return fulfillJson({
        model_config_id: 7,
        generated_at: timestamp,
        reachable: true,
        issues: [],
        warnings: [],
        summary: { enabled_targets: 1, total_targets: 1 },
      });
    }
    if (pathname === "/api/stats/spending") {
      expectIngressSpendingRequest(request, "detail-openai");
      return fulfillJson(createEmptyIngressSpendingReport());
    }
    if (pathname === "/api/models" && request.method() === "GET") {
      return fulfillJson(state.models);
    }
    if (pathname === "/api/endpoints") {
      return fulfillJson([endpoint()]);
    }
    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson([strategy()]);
    }
    if (pathname === "/api/pricing-templates") {
      // The bounded page surface is only reached by the pricing feature page,
      // which reads collection facts through the keyset page contract.
      if (new URL(request.url()).searchParams.has("limit")) {
        return fulfillJson({
          items: [],
          total_count: 0,
          consumed_count: 0,
          list_snapshot_hash: "e2e-snapshot",
          next_cursor: null,
        });
      }
      return fulfillJson(state.pricingTemplates);
    }
    if (pathname === "/api/endpoints/connections") {
      return fulfillJson({ items: [state.connection] });
    }
    if (pathname === "/api/models/7/connections") {
      state.modelConnectionReads += 1;
      return fulfillJson([state.connection]);
    }
    // Catalog metadata surface. The pricing-page discovery uses the same
    // routes for whichever model the operator selects, so they are matched by
    // id pattern rather than hardcoded to the detail fixture.
    const catalogMatch = pathname.match(
      /^\/api\/models\/(\d+)\/catalog(\/(candidates|match-preview|bind))?$/,
    );
    if (catalogMatch) {
      const sub = catalogMatch[3];
      if (!sub && request.method() === "GET") {
        return fulfillJson(state.bound ? boundCatalog() : unboundCatalog());
      }
      if (sub === "candidates" && request.method() === "GET") {
        const scope =
          new URL(request.url()).searchParams.get("scope") ?? "family";
        // The api_family mapping only knows the upstream-native provider, so a
        // family-scoped search finds nothing for the aggregator coordinate.
        const items =
          scope === "all"
            ? [
                {
                  provider_id: "codex",
                  provider_name: "Codex",
                  model_id: "gpt-5.6-luna",
                  name: "GPT 5.6 Luna",
                },
              ]
            : [];
        return fulfillJson({
          items,
          total: items.length,
          limit: 20,
          offset: 0,
          scope,
        });
      }
      if (sub === "match-preview" && request.method() === "POST") {
        const unique = state.matchReason === "unique_match";
        return fulfillJson({
          committable: unique,
          provider_id: unique ? "openai" : undefined,
          catalog_model_id: unique ? "gpt-long" : undefined,
          candidates: unique
            ? [
                {
                  provider_id: "openai",
                  provider_name: "OpenAI",
                  model_id: "gpt-long",
                  name: "GPT Long",
                },
              ]
            : [],
          reason: state.matchReason,
          catalog_revision: '"catalog-e2e-1"',
          fetched_at: timestamp,
        });
      }
      if (sub === "bind" && request.method() === "POST") {
        state.bindRequests.push(request.postDataJSON());
        state.bound = true;
        return fulfillJson(boundCatalog());
      }
    }
    if (pathname === "/api/models/7" && request.method() === "GET") {
      state.modelDetailReads += 1;
      return fulfillJson(
        modelDetail(
          state.bound ? boundCatalog() : unboundCatalog(),
          state.connection,
        ),
      );
    }
    // Source-linked pricing import surface.
    if (
      pathname === "/api/pricing-templates/catalog/preview" &&
      request.method() === "POST"
    ) {
      const previewBody = request.postDataJSON() as Record<string, unknown>;
      state.previewRequests.push(previewBody);
      const requestedTargets = Array.isArray(previewBody.connection_ids)
        ? (previewBody.connection_ids as number[])
        : [];
      const providerID =
        typeof previewBody.provider_id === "string"
          ? previewBody.provider_id
          : "openai";
      const catalogModelID =
        typeof previewBody.catalog_model_id === "string"
          ? previewBody.catalog_model_id
          : "gpt-long";
      return fulfillJson({
        schema_version: 1,
        offering: {
          provider_id: providerID,
          catalog_model_id: catalogModelID,
          name:
            providerID === "codex" && catalogModelID === "gpt-5.6-luna"
              ? "GPT 5.6 Luna"
              : "GPT Long",
        },
        model: {
          model_config_id: 7,
          model_id: "detail-openai",
          display_name: "Detail OpenAI",
          api_family: "openai",
        },
        catalog_revision: '"catalog-e2e-1"',
        fetched_at: timestamp,
        plan: {
          template_kind: "tiered",
          cards: {
            tier_base: {
              input_price: "30",
              output_price: "180",
              cached_input_price: "0",
              cache_creation_price: "2.5",
              reasoning_price: null,
            },
            tier_above: {
              input_price: "60",
              output_price: "270",
              cached_input_price: "0.5",
              cache_creation_price: "5",
              reasoning_price: null,
            },
          },
          tier_input_tokens_above: 272000,
          incompatibilities: [],
        },
        template: null,
        action: "create",
        drift: false,
        committable: true,
        preview_hash: "e2e-hash-1",
        targets: requestedTargets.map((connectionID) => ({
          connection_id: connectionID,
          name: `Target ${connectionID}`,
          endpoint_name: "OpenAI Primary",
          pricing_template_id: null,
          updated_at: timestamp,
        })),
        reporting_currency_code: "USD",
        catalog_currency: "USD",
        pricing_unit: "PER_1M",
      });
    }
    if (
      pathname === "/api/pricing-templates/catalog/commit" &&
      request.method() === "POST"
    ) {
      const commitBody = request.postDataJSON() as Record<string, unknown>;
      state.commitRequests.push(commitBody);
      const providerID =
        typeof commitBody.provider_id === "string"
          ? commitBody.provider_id
          : "openai";
      const catalogModelID =
        typeof commitBody.catalog_model_id === "string"
          ? commitBody.catalog_model_id
          : "gpt-long";
      const template = catalogPricingTemplate(providerID, catalogModelID);
      state.pricingTemplates = [template];
      const assignedConnectionIDs = Array.isArray(commitBody.connection_ids)
        ? (commitBody.connection_ids as number[])
        : [];
      if (assignedConnectionIDs.includes(15)) {
        state.connection = {
          ...state.connection,
          pricing_template_id: template.id,
          pricing_template: {
            id: template.id,
            name: template.name,
            template_kind: template.template_kind,
          },
          updated_at: "2026-08-25T12:01:00Z",
        };
      }
      return fulfillJson({
        created: true,
        updated: false,
        assigned_connection_ids: assignedConnectionIDs,
        template_id: 33,
        revision_id: 66,
        version: 1,
        drift_confirmed: false,
      });
    }
    return fulfillJson({});
  });

  return state;
}

test("model catalog binds via unique match and renders metadata", async ({
  page,
}) => {
  await mockCatalogRoutes(page);

  await page.goto("/models/7");
  await page
    .getByTestId("model-detail-feature-page")
    .waitFor({ timeout: 15000 });

  // Unbound state stays visible and honest. The hint paragraph is the
  // unambiguous anchor: the badge text is a substring of it.
  await expect(page.getByText(/尚未绑定目录条目/)).toBeVisible();
  await expect(page.getByText("未绑定").first()).toBeVisible();

  // Bind flow: unique exact match enters a committable preview.
  await page.getByRole("button", { name: "绑定目录" }).click();
  const bindDialog = page.getByRole("dialog");
  await expect(bindDialog.getByText("发现唯一精确匹配")).toBeVisible();
  await expect(bindDialog.getByText("openai / gpt-long")).toBeVisible();
  await bindDialog.getByRole("button", { name: "应用该匹配" }).click();

  await expect(page.getByText("自动匹配")).toBeVisible();
  await expect(page.getByText("openai / gpt-long")).toBeVisible();
  await expect(page.getByText("GPT Long")).toBeVisible();
});

test("terminal target generates catalog prices atomically", async ({
  page,
}) => {
  const state = await mockCatalogRoutes(page);
  state.bound = true;

  await page.goto("/models/7");
  await page
    .getByTestId("model-detail-feature-page")
    .waitFor({ timeout: 15000 });

  // Per-target entry point from the row actions menu.
  const row = page.getByTestId("access-target-91");
  await row.getByRole("button", { name: /更多操作|更多/ }).click();
  await page.getByTestId("terminal-pricing-action").click();

  const dialog = page.getByTestId("catalog-pricing-dialog");
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText(/长上下文阶梯价/)).toBeVisible();
  await expect(
    dialog.getByText(/输入超过 272000 tokens 时整单切换/),
  ).toBeVisible();
  await expect(dialog.getByText("30")).toBeVisible();
  await expect(dialog.getByText("60")).toBeVisible();

  // Default assignment covers exactly the current Terminal Target.
  const detailReadsBeforeCommit = state.modelDetailReads;
  const connectionReadsBeforeCommit = state.modelConnectionReads;
  await dialog.getByTestId("catalog-pricing-submit").click();
  await expect(dialog).not.toBeVisible();
  await expect(
    page.getByText(/已生成价格模板「openai\/gpt-long」并赋给 1 个终端目标/),
  ).toBeVisible();

  expect(state.commitRequests).toHaveLength(1);
  const commit = state.commitRequests[0] as Record<string, unknown>;
  expect(commit.preview_hash).toBe("e2e-hash-1");
  expect(commit.expected_catalog_revision).toBe('"catalog-e2e-1"');
  expect(commit.confirm_drift).toBe(false);
  expect(commit.connection_ids).toEqual([15]);

  // Success invalidates the shared caches and then re-reads the current model,
  // its connection list, and the pricing collection before a later edit.
  await expect
    .poll(
      () =>
        state.modelDetailReads > detailReadsBeforeCommit &&
        state.modelConnectionReads > connectionReadsBeforeCommit,
    )
    .toBe(true);
  await row.getByRole("button", { name: /编辑 Primary Chat/ }).click();
  const editDialog = page.getByRole("dialog");
  await expect(
    editDialog.getByRole("combobox", { name: "价格模板" }),
  ).toContainText("openai/gpt-long");
});

// A model-config list row is what the pricing page's model selector reads.
function selectableModel(id = 7): Record<string, unknown> {
  return {
    id,
    profile_id: 1,
    api_family: "openai",
    model_id: "detail-openai",
    display_name: "Detail OpenAI",
    loadbalance_strategy_id: 11,
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    access_targets: [],
    is_enabled: true,
    connection_count: 1,
    active_connection_count: 1,
    health_success_rate: null,
    health_total_requests: 0,
    routing_summary: null,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

test("pricing page imports a unique match as a template with no target", async ({
  page,
}) => {
  const state = await mockCatalogRoutes(page);
  state.models = [selectableModel()];

  await page.goto("/route/pricing");
  await page.getByTestId("pricing-feature-page").waitFor({ timeout: 15000 });

  await page.getByTestId("catalog-import-open").click();
  const dialog = page.getByTestId("catalog-pricing-dialog");
  await expect(dialog).toBeVisible();

  // Before a model is chosen there is nothing to commit, and the dialog says so.
  await expect(page.getByTestId("catalog-pricing-submit")).toBeDisabled();
  await expect(page.getByTestId("catalog-pricing-blockers")).toContainText(
    "尚未取得有效预览",
  );

  await dialog.getByRole("combobox", { name: /Prism 模型/ }).click();
  await page
    .getByRole("option", { name: /Detail OpenAI · detail-openai/ })
    .click();

  // The unique exact match advances into the preview without any further pick.
  await expect(dialog.getByText("发现唯一精确匹配")).toBeVisible();
  await expect(
    dialog.getByTestId("catalog-pricing-preview").getByText("openai/gpt-long"),
  ).toBeVisible();
  await expect(dialog.getByTestId("catalog-pricing-preview")).toBeVisible();

  // The preview carries the full source evidence, not just two prices.
  const previewPanel = dialog.getByTestId("catalog-pricing-preview");
  await expect(previewPanel.getByText('"catalog-e2e-1"')).toBeVisible();
  await expect(previewPanel.getByText(/USD\/PER_1M/).first()).toBeVisible();
  await expect(
    previewPanel.getByText(/输入超过 272000 tokens 时整单切换/),
  ).toBeVisible();
  // An explicit catalog zero renders as 0, never as the missing marker.
  await expect(dialog.getByTestId("catalog-pricing-cards")).toContainText("0");
  // The fifth component is absent here and must read as unconfigured.
  await expect(dialog.getByTestId("catalog-pricing-cards")).toContainText("—");

  // No Terminal Target is preselected on this surface.
  await expect(
    dialog.getByTestId("catalog-pricing-target-15"),
  ).not.toBeChecked();

  await expect(page.getByTestId("catalog-pricing-submit")).toHaveText(
    "生成或刷新模板",
  );
  await page.getByTestId("catalog-pricing-submit").click();
  await expect(dialog).not.toBeVisible();
  await expect(
    page.getByText(
      /已生成价格模板「openai\/gpt-long」，本次未赋值任何终端目标/,
    ),
  ).toBeVisible();

  // The preview was requested with an empty target set and the commit replayed
  // exactly that, so only the template moved.
  expect(state.previewRequests[0]).toMatchObject({ connection_ids: [] });
  expect(state.commitRequests).toHaveLength(1);
  expect(state.commitRequests[0]).toMatchObject({
    connection_ids: [],
    preview_hash: "e2e-hash-1",
    expected_catalog_revision: '"catalog-e2e-1"',
  });
  // Importing prices never bound the model as a side effect.
  expect(state.bindRequests).toHaveLength(0);
});

test("pricing page requires a human pick when nothing matches exactly", async ({
  page,
}) => {
  const state = await mockCatalogRoutes(page);
  state.models = [selectableModel()];
  state.matchReason = "no_match";

  await page.goto("/route/pricing");
  await page.getByTestId("pricing-feature-page").waitFor({ timeout: 15000 });
  await page.getByTestId("catalog-import-open").click();

  const dialog = page.getByTestId("catalog-pricing-dialog");
  await dialog.getByRole("combobox", { name: /Prism 模型/ }).click();
  await page
    .getByRole("option", { name: /Detail OpenAI · detail-openai/ })
    .click();

  // A single available candidate must never be taken automatically.
  await expect(dialog.getByText(/没有找到精确匹配的模型 ID/)).toBeVisible();
  await expect(dialog.getByTestId("catalog-pricing-preview")).toHaveCount(0);
  await expect(page.getByTestId("catalog-pricing-submit")).toBeDisabled();

  await dialog.getByLabel(/搜索候选/).fill("luna");
  await page.getByTestId("catalog-candidate-codex-gpt-5.6-luna").click();

  // Only the explicit pick opens the preview, and it previews with no target.
  await expect(dialog.getByTestId("catalog-pricing-preview")).toBeVisible();
  await waitForPreviewRequest(state, []);
  await expect(
    dialog
      .getByTestId("catalog-pricing-preview")
      .getByText("codex/gpt-5.6-luna"),
  ).toBeVisible();

  await page.getByTestId("catalog-pricing-submit").click();
  await expect(dialog).not.toBeVisible();
  await expect(
    page.getByText(
      /已生成价格模板「codex\/gpt-5.6-luna」，本次未赋值任何终端目标/,
    ),
  ).toBeVisible();
  expect(state.commitRequests).toHaveLength(1);
  expect(state.commitRequests[0]).toMatchObject({
    provider_id: "codex",
    catalog_model_id: "gpt-5.6-luna",
    connection_ids: [],
  });
});

test("pricing page re-previews when the operator changes the target set", async ({
  page,
}) => {
  const state = await mockCatalogRoutes(page);
  state.models = [selectableModel()];

  await page.goto("/route/pricing");
  await page.getByTestId("pricing-feature-page").waitFor({ timeout: 15000 });
  await page.getByTestId("catalog-import-open").click();

  const dialog = page.getByTestId("catalog-pricing-dialog");
  await dialog.getByRole("combobox", { name: /Prism 模型/ }).click();
  await page
    .getByRole("option", { name: /Detail OpenAI · detail-openai/ })
    .click();
  await expect(dialog.getByTestId("catalog-pricing-preview")).toBeVisible();
  await waitForPreviewRequest(state, [], 1);

  // Selecting a target invalidates the previous CAS snapshot and re-previews.
  await dialog.getByTestId("catalog-pricing-target-15").check();
  await waitForPreviewRequest(state, [15], 2);

  await page.getByTestId("catalog-pricing-submit").click();
  await expect(dialog).not.toBeVisible();
  expect(state.commitRequests[0]).toMatchObject({ connection_ids: [15] });
  await expect(
    page.getByText(/已生成价格模板「openai\/gpt-long」并赋给 1 个终端目标/),
  ).toBeVisible();
});

// waitForPreviewRequest polls the recorded mock traffic instead of sleeping, so
// a re-preview race fails fast rather than passing on stale state.
async function waitForPreviewRequest(
  state: { previewRequests: unknown[] },
  connectionIds: unknown[],
  count = 1,
) {
  await expect
    .poll(() => {
      const latest = state.previewRequests[state.previewRequests.length - 1] as
        | Record<string, unknown>
        | undefined;
      return (
        state.previewRequests.length >= count &&
        JSON.stringify((latest?.connection_ids as unknown[]) ?? []) ===
          JSON.stringify(connectionIds)
      );
    })
    .toBe(true);
}
