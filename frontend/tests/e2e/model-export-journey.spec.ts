// 客户端模型配置导出 journey：默认选中、未绑定模型的绑定操作、
// 绑定后才能进入最终密钥 Dialog、结果 Sheet 的复制/下载/原始查看复用同一内容，
// 关闭即清除。后端流量全部 mock。
import { expect, test, type Page } from "@playwright/test";

function sourceModel(overrides: Record<string, unknown> = {}) {
  return {
    model_config_id: 3,
    model_id: "gpt-x",
    api_family: "openai",
    display_name: "gpt-x",
    is_enabled: true,
    direct_request_enabled: true,
    selectable: true,
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    prism_metadata: {},
    merged_metadata: { name: "gpt-x" },
    metadata_provenance: {},
    missing_metadata: [],
    completeness: { metadata_fields: { name: true }, cost_exportable: true },
    targets: [
      {
        terminal_target_id: 11,
        position: 0,
        endpoint_id: 21,
        endpoint_name: "primary",
        openai_text_capability: "dual_native",
        pricing: {
          terminal_target_id: 11,
          template_kind: "standard",
          currency_code: "USD",
          pricing_unit: "PER_1M",
          card: {
            input_price: "3",
            output_price: "15",
            cached_input_price: "0.3",
            cache_creation_price: "3.75",
            reasoning_price: "15",
          },
        },
      },
    ],
    price_risk: { exportable: true },
    pi_api: "openai-responses",
    pi_candidates: [
      {
        provider_id: "openai",
        model_id: "gpt-x",
        api: "openai-responses",
        name: "GPT X",
      },
    ],
    candidate_status: "single",
    pi_selected: null,
    pi_binding_status: "unbound",
    pi_binding_renderable: false,
    ...overrides,
  };
}

const catalogWire = {
  status: "fresh" as const,
  revision: "rev-1",
  minimum_version: "0.80.0",
};

const searchCatalogWire = {
  ...catalogWire,
  revision: "rev-2",
};

const internalSourceModel = sourceModel({
  model_config_id: 9,
  model_id: "deepseek/deepseek-v4-flash-0731",
  direct_request_enabled: false,
});

const unboundSource = {
  target_version: "0.84.3",
  catalog: catalogWire,
  source_digest: "a".repeat(64),
  models: [
    sourceModel({
      model_id: "codex/gpt-x",
      display_name: "Codex GPT X",
      pi_candidates: [],
      candidate_status: "not_in_catalog",
    }),
    internalSourceModel,
  ],
};

const boundSource = {
  target_version: "0.84.3",
  catalog: searchCatalogWire,
  source_digest: "b".repeat(64),
  models: [
    sourceModel({
      model_id: "codex/gpt-x",
      display_name: "Codex GPT X",
      pi_candidates: [],
      candidate_status: "not_in_catalog",
      pi_selected: {
        provider_id: "alias-provider",
        model_id: "gpt-x-alias",
        api: "openai-responses",
      },
      pi_binding_status: "bound",
      pi_binding_renderable: true,
      pi_bind_source: "manual",
      pi_binding_prism_model_id: "codex/gpt-x",
      pi_binding_catalog_revision: "rev-2",
    }),
    internalSourceModel,
  ],
};

const renderPayload = {
  target_version: "0.84.3",
  content: `{"providers":{"prism":{"name":"Prism"}}}\n`,
  content_sha256: "c".repeat(64),
  file_name: "prism-pi-models.json",
  mime_type: "application/json;charset=utf-8",
  model_results: [
    { model_config_id: 3, model_id: "codex/gpt-x", cost_exported: true },
  ],
  warnings: [],
};

async function installExportRoutes(page: Page) {
  let bound = false;
  const outbound: string[] = [];
  const unexpectedApi: string[] = [];
  const pageErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.route("**/*", async (route) => {
    const request = route.request();
    outbound.push(request.url());
    const pathname = new URL(request.url()).pathname;
    if (!pathname.startsWith("/api/")) return route.continue();
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
    if (pathname === "/api/models/exports/pi/source") {
      return fulfillJson(bound ? boundSource : unboundSource);
    }
    if (pathname === "/api/models/3/pi/search" && request.method() === "POST") {
      return fulfillJson({
        query: "gpt-x",
        api: "openai-responses",
        limit: 20,
        total: 1,
        returned: 1,
        truncated: false,
        selected: false,
        catalog: searchCatalogWire,
        fetched_at: "2026-08-30T00:00:00Z",
        export_identity: {
          model_config_id: 3,
          model_id: "codex/gpt-x",
          api: "openai-responses",
          provider_id_source: "operator_input",
        },
        results: [
          {
            provider_id: "alias-provider",
            model_id: "gpt-x-alias",
            api: "openai-responses",
            name: "GPT X Alias",
            context_window: 200000,
            dropped_fields: ["headers"],
          },
        ],
      });
    }
    if (pathname === "/api/models/3/pi/bind" && request.method() === "POST") {
      bound = true;
      return fulfillJson({
        bound: true,
        bind_source: "manual",
        provider_id: "alias-provider",
        catalog_model_id: "gpt-x-alias",
        api: "openai-responses",
        prism_model_id_at_bind: "codex/gpt-x",
        catalog_revision: "rev-2",
        source: {
          name: "GPT X",
          reasoning: null,
          input: null,
          context_window: null,
          max_tokens: null,
          thinking_level_map: null,
          compat: null,
        },
        override: null,
        effective: {
          name: "GPT X",
          reasoning: null,
          input: null,
          context_window: null,
          max_tokens: null,
          thinking_level_map: null,
          compat: null,
        },
      });
    }
    if (
      pathname === "/api/models/exports/pi/render" &&
      request.method() === "POST"
    ) {
      return fulfillJson(renderPayload);
    }
    unexpectedApi.push(`${request.method()} ${pathname}`);
    return fulfillJson({ detail: "unexpected mocked API request" }, 500);
  });
  return { outbound, pageErrors, unexpectedApi };
}

test("export journey: bind an uncatalogued Prism id through directory search, then generate", async ({
  page,
}) => {
  const { outbound, pageErrors, unexpectedApi } =
    await installExportRoutes(page);
  await page.goto("/route/models/export");
  const row = page.getByTestId("export-row-3");
  await row.waitFor({ timeout: 15000 });
  await expect(page.getByTestId("export-row-9")).toHaveCount(0);
  await expect(page.getByTestId("shell-breadcrumb")).toContainText(
    "路由配置导出客户端配置",
  );

  // Backend defaults preselect every selectable model.
  const checkbox = page.getByRole("checkbox", { name: "codex/gpt-x" });
  await expect(checkbox).toBeChecked();

  // Generation is blocked until the sole selected model is bound.
  const generateButton = page.getByRole("button", { name: /生成配置文件/ });
  await expect(generateButton).toBeDisabled();

  const bindResponse = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/models/3/pi/bind" &&
      response.request().method() === "POST",
  );
  await row.getByRole("button", { name: "绑定来源" }).click();
  const sourceDialog = page.getByRole("dialog", { name: "更换 Pi 来源" });
  await expect(sourceDialog).toBeVisible();
  await expect(
    sourceDialog.getByText("最终导出身份（由 Prism 决定）"),
  ).toBeVisible();
  const apply = sourceDialog.getByRole("button", { name: "应用绑定" });
  await expect(apply).toBeDisabled();
  await sourceDialog
    .getByRole("textbox", { name: "目录 model_id 片段" })
    .fill("gpt-x");
  const searchRequestPromise = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === "/api/models/3/pi/search" &&
      request.method() === "POST",
  );
  await sourceDialog.getByRole("button", { name: "搜索目录" }).click();
  expect((await searchRequestPromise).postDataJSON()).toEqual({
    model_id_query: "gpt-x",
    limit: 20,
    offset: 0,
  });
  await expect(apply).toBeDisabled();
  // Pre-selection options carry the coordinate and name; the full seven-field
  // evidence renders after the explicit choice (PiCandidateEvidence).
  const option = sourceDialog
    .getByRole("option")
    .filter({ hasText: "alias-provider/gpt-x-alias" });
  await option.click();
  await expect(sourceDialog.getByText("已选目录坐标")).toBeVisible();
  // Evidence renders as separate label/value nodes (dt/dd).
  await expect(sourceDialog.getByText("上下文窗口（令牌）")).toBeVisible();
  await expect(sourceDialog.getByText("200000")).toBeVisible();
  await expect(sourceDialog.getByText("目录 Provider")).toBeVisible();
  await expect(sourceDialog.getByText(/跨目录绑定/)).toBeVisible();
  await expect(apply).toBeEnabled();
  const bindRequestPromise = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === "/api/models/3/pi/bind" &&
      request.method() === "POST",
  );
  await apply.click();
  const bindRequest = await bindRequestPromise;
  const bindBody = bindRequest.postDataJSON() as Record<string, unknown>;
  expect(bindBody).toEqual({
    provider_id: "alias-provider",
    catalog_model_id: "gpt-x-alias",
    expected_catalog_revision: "rev-2",
    expected_prism_model_id: "codex/gpt-x",
    expected_pi_api: "openai-responses",
  });
  await bindResponse;
  const sourceRefetch = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/models/exports/pi/source",
  );
  await sourceRefetch;
  await expect(generateButton).toBeEnabled();

  // Generate through the final credential dialog without embedding keys.
  await generateButton.click();
  const dialog = page.getByTestId("export-key-dialog");
  await expect(dialog).toBeVisible();
  await dialog.getByText("不嵌入密钥").click();
  const renderRequestPromise = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname === "/api/models/exports/pi/render" &&
      request.method() === "POST",
  );
  await dialog.getByRole("button", { name: "确认生成" }).click();
  const renderRequest = await renderRequestPromise;
  const renderBody = renderRequest.postDataJSON() as Record<string, unknown>;
  expect(renderBody.credential).toEqual({ include: false });
  expect(renderBody).not.toHaveProperty("enhancements");
  expect(renderBody).not.toHaveProperty("default_model_config_id");
  expect((renderBody.selections as Record<string, unknown>)["3"]).toEqual({
    provider_id: "alias-provider",
    model_id: "gpt-x-alias",
    api: "openai-responses",
  });
  expect(renderBody.selections).not.toHaveProperty("9");

  // The result sheet reuses one deterministic content for preview.
  const sheet = page.getByTestId("export-result-sheet");
  await expect(sheet).toBeVisible();
  await expect(sheet.getByText(/prism-pi-models\.json/)).toBeVisible();
  const preview = page.getByTestId("export-content-preview");
  const content = await preview.textContent();
  expect(content).toContain('"prism"');
  expect(renderPayload.model_results[0].model_id).toBe("codex/gpt-x");

  // Copy and download reuse the same content; download keeps the fixed name.
  const downloadPromise = page.waitForEvent("download");
  await sheet.getByRole("button", { name: "下载" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("prism-pi-models.json");

  // Closing the sheet clears the key-bearing content from memory.
  await sheet.getByRole("button", { name: "关闭并清除" }).click();
  await expect(page.getByTestId("export-result-sheet")).toHaveCount(0);

  expect(outbound.filter((url) => /pi\.dev/i.test(url))).toEqual([]);
  expect(unexpectedApi).toEqual([]);
  expect(pageErrors).toEqual([]);
});
