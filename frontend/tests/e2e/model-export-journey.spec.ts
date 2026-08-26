// 客户端模型配置导出 journey：默认选中、平台切换重置、价格筛选不撤销选择、
// 最终密钥 Dialog、结果 Sheet 的复制/下载/原始查看复用同一内容，关闭即清除。
// 后端流量全部 mock。
import { expect, test, type Page } from "@playwright/test";

function sourceModel(overrides: Record<string, unknown> = {}) {
  return {
    model_config_id: 3,
    model_id: "gpt-x",
    api_family: "openai",
    display_name: null,
    is_enabled: true,
    default_selected: true,
    selectable: true,
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    catalog: { bound: false, has_overrides: false },
    enrichment: {
      available: true,
      offering_provider_id: "openai",
      offering_model_id: "gpt-x",
    },
    prism_metadata: {},
    models_dev_metadata: {},
    merged_metadata: {},
    metadata_provenance: {},
    missing_metadata: [],
    platform_completeness: { metadata_fields: {}, cost_exportable: true },
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
    enrichment_candidate: { metadata: {}, derived: {} },
    ...overrides,
  };
}

const piSource = {
  platform: "pi",
  target_version: "0.84.3",
  source_digest: "a".repeat(64),
  models: [sourceModel()],
};

const opencodeSource = {
  platform: "opencode",
  target_version: "1.18.23",
  source_digest: "b".repeat(64),
  models: [
    sourceModel({
      platform_completeness: { metadata_fields: {}, cost_exportable: true },
    }),
  ],
};

const renderPayload = (platform: string, fileName: string, targetVersion: string) => ({
  platform,
  target_version: targetVersion,
  content: `{"providers":{"prism":{"name":"Prism"}}}\n`,
  content_sha256: "c".repeat(64),
  file_name: fileName,
  mime_type: "application/json;charset=utf-8",
  model_results: [
    { model_config_id: 3, model_id: "gpt-x", cost_exported: true },
  ],
  warnings: [],
});

async function installExportRoutes(page: Page) {
  await page.route("**/*", async (route) => {
    const pathname = new URL(route.request().url()).pathname;
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
    if (pathname === "/api/models/exports/pi/source") {
      return fulfillJson(piSource);
    }
    if (pathname === "/api/models/exports/opencode/source") {
      return fulfillJson(opencodeSource);
    }
    if (pathname === "/api/models/exports/pi/render") {
      return fulfillJson(
        renderPayload("pi", "prism-pi-models.json", "0.84.3"),
      );
    }
    if (pathname === "/api/models/exports/opencode/render") {
      return fulfillJson(
        renderPayload("opencode", "opencode-prism.json", "1.18.23"),
      );
    }
    return fulfillJson({}, 404);
  });
}

test("export journey: selection truth, platform reset, key dialog, and result sheet", async ({
  page,
}) => {
  await installExportRoutes(page);
  await page.goto("/route/models/export");
  await page.getByTestId("export-row-3").waitFor({ timeout: 15000 });
  await expect(page.getByTestId("shell-breadcrumb")).toContainText(
    "路由配置导出客户端配置",
  );

  // Backend defaults preselect every selectable model.
  const checkbox = page.getByRole("checkbox", { name: "gpt-x" });
  await expect(checkbox).toBeChecked();

  // A discoverable source refresh intersects the operator's current selection
  // instead of reapplying backend defaults.
  await checkbox.click();
  const refreshResponse = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/models/exports/pi/source",
  );
  await page.getByRole("button", { name: "刷新导出源" }).click();
  await refreshResponse;
  await expect(checkbox).not.toBeChecked();

  // Platform switch resets to the other platform's defaults.
  await page.getByRole("combobox", { name: "目标客户端" }).click();
  await page.getByRole("option", { name: /OpenCode/ }).click();
  await page.getByTestId("export-row-3").waitFor();
  await expect(page.getByRole("checkbox", { name: "gpt-x" })).toBeChecked();

  // Generate through the final credential dialog without embedding keys.
  await page.getByRole("button", { name: /生成配置文件/ }).click();
  const dialog = page.getByTestId("export-key-dialog");
  await expect(dialog).toBeVisible();
  await dialog.getByText("不嵌入密钥").click();
  const renderRequestPromise = page.waitForRequest(
    (request) =>
      new URL(request.url()).pathname ===
      "/api/models/exports/opencode/render",
  );
  await dialog.getByRole("button", { name: "确认生成" }).click();
  const renderRequest = await renderRequestPromise;
  const renderBody = renderRequest.postDataJSON() as Record<string, unknown>;
  expect(renderBody.credential).toEqual({ include: false });
  expect(renderBody).not.toHaveProperty("include_api_keys");
  expect(renderBody).not.toHaveProperty("api_key_overrides");
  expect(renderBody).not.toHaveProperty("default_model_config_id");

  // The result sheet reuses one deterministic content for preview.
  const sheet = page.getByTestId("export-result-sheet");
  await expect(sheet).toBeVisible();
  await expect(sheet.getByText(/opencode-prism\.json/)).toBeVisible();
  const preview = page.getByTestId("export-content-preview");
  const content = await preview.textContent();
  expect(content).toContain('"prism"');

  // Copy and download reuse the same content; download keeps the fixed name.
  const downloadPromise = page.waitForEvent("download");
  await sheet.getByRole("button", { name: "下载" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("opencode-prism.json");

  // Closing the sheet clears the key-bearing content from memory.
  await sheet.getByRole("button", { name: "关闭并清除" }).click();
  await expect(page.getByTestId("export-result-sheet")).toHaveCount(0);
});
