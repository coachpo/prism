// Pi 绑定与 OpenCode 导出共用页面旅程；接口全部 mock。
// 真客户端与受控上游往返由 backend/tests/runtime/opencode_* 负责。
import { expect, test } from "@playwright/test";
import { readFile } from "node:fs/promises";

import { installExportRoutes, renderPayload } from "./model-export-fixtures";
import { openCodeRenderPayload, openCodeSource } from "./opencode-export-fixtures";

test("export journey: bind Pi, inspect OpenCode metadata, and deliver isolated client files", async ({
  page,
}) => {
  const { outbound, pageErrors, unexpectedApi, openCodeRequests } =
    await installExportRoutes(page);
  // A plain HTTP IP origin exercises the same Clipboard API absence as a LAN
  // deployment. 0.0.0.0 reaches the local Vite listener without localhost's
  // secure-context exemption.
  const exportURL = new URL("/route/models/export", test.info().project.use.baseURL);
  exportURL.hostname = "0.0.0.0";
  await page.addInitScript(() => {
    const copied: string[] = [];
    Object.defineProperty(window, "exportCopiedTexts", { value: copied });
    const copy = document.execCommand.bind(document);
    document.execCommand = (command, showUI, value) => {
      const selected = (document.activeElement as HTMLTextAreaElement | null)?.value;
      const success = copy(command, showUI, value);
      if (command === "copy" && success && selected !== undefined) copied.push(selected);
      return success;
    };
  });
  await page.goto(exportURL.href);
  expect(await page.evaluate(() => ({ secure: isSecureContext, clipboard: typeof navigator.clipboard })))
    .toEqual({ secure: false, clipboard: "undefined" });
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

  // The second target starts from its own source and selection, with no Pi
  // binding workflow. Missing limits explain the blocked row before export.
  await page.getByRole("radio", { name: "OpenCode", exact: true }).click();
  const openCodeRow = page.getByTestId("opencode-export-row-3");
  await expect(openCodeRow.getByRole("checkbox")).toBeChecked();
  const missingLimits = page.getByTestId("opencode-export-row-4");
  await expect(missingLimits.getByRole("checkbox")).toBeDisabled();
  await expect(missingLimits).toContainText("上下文 / 输出上限");
  await openCodeRow.getByText("查看最终值与来源", { exact: true }).click();
  await expect(openCodeRow.getByText("Catalog GPT X", { exact: true })).toBeVisible();
  await expect(openCodeRow.getByText("models.dev 人工覆盖").first()).toBeVisible();
  await expect(openCodeRow.getByRole("link", { name: "到模型详情补全 models.dev 元数据" }))
    .toHaveAttribute("href", "/route/models/3");
  await expect(page.getByText(/OpenCode 可能显示零估算，这不等于免费/)).toBeVisible();
  await page.locator("#export-model-search").fill("codex/");
  await expect(missingLimits).toHaveCount(0);
  await page.locator("#export-model-search").fill("");
  await page.getByRole("textbox", { name: "Prism Gateway origin" }).fill("http://127.0.0.1:8000");

  await generateButton.click();
  await dialog.getByText("手动输入统一密钥", { exact: true }).click();
  await expect(dialog.getByRole("button", { name: "确认生成" })).toBeDisabled();
  await dialog.getByLabel(/^Prism 代理密钥/).fill("  prism-e2e-synthetic-key  ");
  await dialog.getByRole("button", { name: "确认生成" }).click();
  await expect(sheet).toBeVisible();
  const keyedPayload = openCodeRenderPayload("prism-e2e-synthetic-key");
  expect(await preview.textContent()).toBe(keyedPayload.content);
  expect(openCodeRequests[0]).toEqual({
    expected_source_digest: openCodeSource.source_digest,
    model_config_ids: [3],
    base_url: "http://127.0.0.1:8000",
    provider_id: "prism",
    credential: { include: true, api_key: "prism-e2e-synthetic-key" },
  });
  await sheet.getByRole("button", { name: "关闭并清除" }).click();

  // A new generation starts with no key. All delivery actions use this exact
  // response; the fragment is only the map beneath singular `provider`.
  await generateButton.click();
  await expect(dialog.getByRole("radio", { name: /不嵌入密钥/ })).toBeChecked();
  await expect(dialog.locator("#export-manual-key")).toHaveCount(0);
  await dialog.getByRole("button", { name: "确认生成" }).click();
  await expect(sheet).toBeVisible();
  const openCodePayload = openCodeRenderPayload();
  expect(await preview.textContent()).toBe(openCodePayload.content);
  expect(openCodeRequests[1].credential).toEqual({ include: false });
  await sheet.getByRole("button", { name: "复制", exact: true }).click();
  await expect(sheet.getByRole("button", { name: "已复制", exact: true })).toBeVisible();
  await sheet.getByRole("button", { name: "复制 provider 合并片段", exact: true }).click();
  const copied = await page.evaluate(() =>
    (window as unknown as { exportCopiedTexts: string[] }).exportCopiedTexts,
  );
  expect(copied).toEqual([
    openCodePayload.content,
    `${JSON.stringify(JSON.parse(openCodePayload.content).provider, null, 2)}\n`,
  ]);

  const openCodeDownloadPromise = page.waitForEvent("download");
  await sheet.getByRole("button", { name: "下载", exact: true }).click();
  const openCodeDownload = await openCodeDownloadPromise;
  expect(openCodeDownload.suggestedFilename()).toBe("opencode-prism.json");
  const downloadedPath = test.info().outputPath("downloaded-opencode-prism.json");
  await openCodeDownload.saveAs(downloadedPath);
  expect(await readFile(downloadedPath, "utf8")).toBe(openCodePayload.content);
  const rawViewPromise = page.context().waitForEvent("page");
  await sheet.getByRole("button", { name: "在新标签页查看原始 JSON", exact: true }).click();
  const rawView = await rawViewPromise;
  await rawView.waitForLoadState("domcontentloaded");
  expect(rawView.url()).toMatch(/^blob:/);
  expect(await rawView.locator("body").innerText()).toContain(openCodePayload.content.trim());
  await rawView.close();
  await page.setViewportSize({ width: 1440, height: 1000 });
  await sheet.evaluate((element) => { element.scrollTop = 0; });
  await sheet.screenshot({ path: test.info().outputPath("export-result.png") });
  await sheet.getByRole("button", { name: "关闭并清除" }).click();
  await page.getByRole("radio", { name: "Pi", exact: true }).click();
  await expect(row).toBeVisible();
  await expect(generateButton).toBeEnabled();
  await expect(sheet).toHaveCount(0);

  expect(outbound.filter((url) => /pi\.dev|models\.dev/i.test(url))).toEqual([]);
  expect(unexpectedApi).toEqual([]);
  expect(pageErrors).toEqual([]);
});

// Frozen-column width contract. The select column and the identity column
// behind it are pinned with a hardcoded offset, but automatic table layout
// sizes columns from their content, not from the declared w-*. When the two
// disagree the columns come apart the moment the table scrolls sideways:
// too small an offset parks the identity column on top of the select header,
// too large leaves a strip no frozen cell paints and the scrolled-away cells
// bleed through it. This fixture is too narrow to force a horizontal scroll,
// so assert the contract itself — the offset has to equal the measured width
// of the column it sits behind.
test("export table pins the identity column exactly at the select column's width", async ({
  page,
}) => {
  await installExportRoutes(page);
  await page.goto("/route/models/export");
  for (const target of ["Pi", "OpenCode"]) {
    await page.getByRole("radio", { name: target, exact: true }).click();
    await page.getByTestId(target === "Pi" ? "export-row-3" : "opencode-export-row-3").waitFor({ timeout: 15000 });

    const contract = await page.evaluate(() => {
      const container = document.querySelector<HTMLElement>(
        '[data-slot="table-container"]',
      );
      if (!container) return null;
      const rows = [
        container.querySelector("thead tr"),
        container.querySelector("tbody tr"),
      ];
      return rows.map((row) => {
        if (!row) return null;
        const [select, identity] = [row.children[0], row.children[1]];
        return {
          selectWidth: Math.round(select.getBoundingClientRect().width),
          identityOffset: getComputedStyle(identity).left,
        };
      });
    });
    expect(contract).toEqual([
      { selectWidth: 48, identityOffset: "48px" },
      { selectWidth: 48, identityOffset: "48px" },
    ]);
  }
});
