import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { LocaleProvider } from '@/i18n/LocaleProvider';
import { EXPORT_PRESET_KEY, exportPresetsFile } from '@/lib/preferences/exportPresets';
import { ExportPresetsPanel } from './ExportPresetsPanel';
afterEach(() => { cleanup(); localStorage.clear(); });
it('explains refusal to export or extend readable legacy oversized presets without replacing them', () => {
  const presets = Array.from({ length: 3 }, (_, index) => ({ name: `旧预设-${index}`, client: 'opencode' as const, modelIds: Array.from({ length: 500 }, () => '\u0001'.repeat(256)), gatewayOrigin: 'http://localhost:8080', providerId: 'prism' }));
  const raw = JSON.stringify(exportPresetsFile(presets));
  localStorage.setItem(EXPORT_PRESET_KEY, raw);
  render(<LocaleProvider><ExportPresetsPanel client="opencode" models={[]} selectedIds={new Set()} gatewayOrigin="http://localhost:8080" providerId="prism" blocked={false} onApply={() => {}} /></LocaleProvider>);
  expect(screen.getByText('旧预设-0')).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: '导出偏好文件' }));
  expect(screen.getByText(/超过 256 KiB 文件上限/)).toBeVisible();
  fireEvent.change(screen.getByRole('textbox', { name: '预设名称' }), { target: { value: '新预设' } });
  fireEvent.click(screen.getByRole('button', { name: /^保存预设$/ }));
  expect(localStorage.getItem(EXPORT_PRESET_KEY)).toBe(raw);
  expect(screen.getByRole('textbox', { name: '预设名称' })).toHaveValue('新预设');
});

it('explains blocked Pi references and applies only current ready models after explicit removal', () => {
  const preset = { name: 'Pi 工作', client: 'pi' as const, modelIds: ['ready', 'unbound'], gatewayOrigin: 'http://localhost:8080', providerId: 'prism' };
  localStorage.setItem(EXPORT_PRESET_KEY, JSON.stringify(exportPresetsFile([preset])));
  const apply = vi.fn();
  render(<LocaleProvider><ExportPresetsPanel client="pi" models={[
    { model_config_id: 1, model_id: 'ready', selectable: true, readiness: { status: 'ready' } },
    { model_config_id: 2, model_id: 'unbound', selectable: true, readiness: { status: 'blocked' } },
  ]} selectedIds={new Set()} gatewayOrigin="http://localhost:8080" providerId="prism" blocked={false} onApply={apply} /></LocaleProvider>);
  fireEvent.click(screen.getByRole('button', { name: '恢复预设' }));
  expect(apply).not.toHaveBeenCalled();
  expect(screen.getByText(/下列模型.*unbound/)).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: '移除失效选择并恢复' }));
  expect(apply).toHaveBeenCalledWith(new Set([1]), preset);
});
