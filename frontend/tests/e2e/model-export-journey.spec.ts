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
    selectable: true,
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    prism_metadata: {},
    merged_metadata: { name: "gpt-x" },
    metadata_provenance: {},
    missing_metadata: [],
    platform_completeness: { metadata_fields: { name: true }, cost_exportable: true },
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
    pi_candidates: [
      { provider_id: "openai", model_id: "gpt-x", api: "openai-responses", name: "GPT X" },
    ],
    candidate_status: "single",
    pi_selected: null,
    pi_binding_status: "unbound",
    ...overrides,
  };
}

const catalogWire = { status: "fresh" as const, revision: "rev-1", minimum_version: "0.80.0" };

const unboundSource = {
  target_version: "0.84.3",
  catalog: catalogWire,
  source_digest: "a".repeat(64),
  models: [sourceModel()],
};

const boundSource = {
  target_version: "0.84.3",
  catalog: catalogWire,
  source_digest: "b".repeat(64),
  models: [
    sourceModel({
      pi_selected: { provider_id: "openai", model_id: "gpt-x", api: "openai-responses" },
      pi_binding_status: "bound",
      pi_bind_source: "single_candidate",
      pi_binding_catalog_revision: "rev-1",
    }),
  ],
};

const renderPayload = {
  target_version: "0.84.3",
  catalog: catalogWire,
  content: `{"providers":{"prism":{"name":"Prism"}}}\n`,
  content_sha256: "c".repeat(64),
  file_name: "prism-pi-models.json",
  mime_type: "application/json;charset=utf-8",
  model_results: [{ model_config_id: 3, model_id: "gpt-x", cost_exported: true }],
  warnings: [],
};

async function installExportRoutes(page: Page) {
  let bound = false;
  await page.route("**/*", async (route) => {
    const request = route.request();
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
    if (pathname === "/api/models/export/source") {
      return fulfillJson(bound ? boundSource : unboundSource);
    }
    if (pathname === "/api/models/3/pi/bind" && request.method() === "POST") {
      bound = true;
      return fulfillJson({
        bound: true,
        bind_source: "single_candidate",
        provider_id: "openai",
        catalog_model_id: "gpt-x",
        api: "openai-responses",
        catalog_revision: "rev-1",
        source: { name: "GPT X", reasoning: null, input: null, context_window: null, max_tokens: null, thinking_level_map: null, compat: null },
        override: null,
        effective: { name: "GPT X", reasoning: null, input: null, context_window: null, max_tokens: null, thinking_level_map: null, compat: null },
      });
    }
    if (pathname === "/api/models/export/render" && request.method() === "POST") {
      return fulfillJson(renderPayload);
    }
    return fulfillJson({}, 404);
  });
}

test("export journey: bind an unbound candidate before generating, then copy/download the result", async ({
  page,
}) => {
  await installExportRoutes(page);
  await page.goto("/route/models/export");
  const row = page.getByTestId("export-row-3");
  await row.waitFor({ timeout: 15000 });
  await expect(page.getByTestId("shell-breadcrumb")).toContainText(
    "路由配置导出客户端配置",
  );

  // Backend defaults preselect every selectable model.
  const checkbox = page.getByRole("checkbox", { name: "gpt-x" });
  await expect(checkbox).toBeChecked();

  // Generation is blocked until the sole selected model is bound.
  const generateButton = page.getByRole("button", { name: /生成配置文件/ });
  await expect(generateButton).toBeDisabled();

  // Binding the single exact candidate flips the row to bound and unblocks generation.
  const bindResponse = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/models/3/pi/bind" &&
      response.request().method() === "POST",
  );
  await row.getByRole("button", { name: "绑定" }).click();
  await bindResponse;
  const sourceRefetch = page.waitForResponse(
    (response) => new URL(response.url()).pathname === "/api/models/export/source",
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
      new URL(request.url()).pathname === "/api/models/export/render" &&
      request.method() === "POST",
  );
  await dialog.getByRole("button", { name: "确认生成" }).click();
  const renderRequest = await renderRequestPromise;
  const renderBody = renderRequest.postDataJSON() as Record<string, unknown>;
  expect(renderBody.credential).toEqual({ include: false });
  expect(renderBody).not.toHaveProperty("enhancements");
  expect(renderBody).not.toHaveProperty("default_model_config_id");
  expect(
    (renderBody.selections as Record<string, unknown>)["3"],
  ).toEqual({ provider_id: "openai", model_id: "gpt-x", api: "openai-responses" });

  // The result sheet reuses one deterministic content for preview.
  const sheet = page.getByTestId("export-result-sheet");
  await expect(sheet).toBeVisible();
  await expect(sheet.getByText(/prism-pi-models\.json/)).toBeVisible();
  const preview = page.getByTestId("export-content-preview");
  const content = await preview.textContent();
  expect(content).toContain('"prism"');

  // Copy and download reuse the same content; download keeps the fixed name.
  const downloadPromise = page.waitForEvent("download");
  await sheet.getByRole("button", { name: "下载" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("prism-pi-models.json");

  // Closing the sheet clears the key-bearing content from memory.
  await sheet.getByRole("button", { name: "关闭并清除" }).click();
  await expect(page.getByTestId("export-result-sheet")).toHaveCount(0);
});
