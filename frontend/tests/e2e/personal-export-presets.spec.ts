import { test, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { installExportRoutes } from './model-export-fixtures';

// Real browser file download/upload; management source is an explicit fixture.
// Live local application/database acceptance is recorded separately.
test('export presets move between isolated browsers without credential state', async ({ browser }) => {
  const first = await browser.newContext(); const second = await browser.newContext();
  try {
    const a = await first.newPage(); const b = await second.newPage();
    const fixturesA = await installExportRoutes(a, { initiallyBound: true }); const fixturesB = await installExportRoutes(b, { initiallyBound: true });
    await a.goto('/route/models/export'); await a.getByTestId('export-row-3').waitFor();
    const panelA = a.getByRole('region', { name: '个人预设' });
    await panelA.getByLabel('预设名称', { exact: true }).fill('办公 Pi');
    await panelA.getByRole('button', { name: '保存预设', exact: true }).click();
    await expect(panelA.getByText('已保存到此浏览器。')).toBeVisible();
    const downloadPromise = a.waitForEvent('download');
    await panelA.getByRole('button', { name: '导出偏好文件' }).click();
    const download = await downloadPromise; const file = await download.path();
    const raw = await readFile(file!, 'utf8');
    expect(JSON.parse(raw).presets[0]).toMatchObject({ client: 'pi', modelIds: ['codex/gpt-x'] });
    expect(raw).not.toMatch(/api_key|source_digest|secret|rendered_content/);
    await b.goto('/route/models/export'); await b.getByTestId('export-row-3').waitFor();
    const panelB = b.getByRole('region', { name: '个人预设' });
    await expect(panelB.getByText('办公 Pi', { exact: true })).toHaveCount(0);
    await panelB.getByLabel('导入偏好文件').setInputFiles(file!);
    await expect(panelB.getByText('导入预览')).toBeVisible();
    await panelB.getByRole('button', { name: '保存导入项' }).click();
    await b.reload(); await b.getByTestId('export-row-3').waitFor();
    await panelB.getByRole('button', { name: '恢复预设' }).click();
    await expect(b.getByRole('checkbox', { name: 'codex/gpt-x' })).toBeChecked();
    expect(fixturesA.unexpectedApi).toEqual([]); expect(fixturesB.unexpectedApi).toEqual([]);
  } finally { await first.close(); await second.close(); }
});
