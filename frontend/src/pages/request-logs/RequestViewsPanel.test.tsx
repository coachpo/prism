import { fireEvent, render, screen, waitFor, cleanup } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { LocaleProvider } from '@/i18n/LocaleProvider';
import { RequestViewsPanel } from './RequestViewsPanel';
import { parsePageSearch } from './queryParams';
import { savedViewStateOf, saveRequestLogView } from './requestLogSavedViews';
import type { RequestLogPageActions } from './useRequestLogPageState';
vi.mock('@/lib/referenceData', () => ({
  getSharedModels: async () => [{ model_id: 'current', display_name: '当前模型', direct_request_enabled: true }],
  getSharedEndpoints: async () => [{ id: 3, name: '当前端点' }],
  getSharedConnectionOptions: async () => [], getSharedProxyKeys: async () => [],
}));
vi.mock('@/lib/api', () => ({ api: { stats: { costSegments: async () => ({ cost_segments: [], cost_segments_snapshot_hash: 'one', cost_segments_next_cursor: null }) } } }));
const replaceState = vi.fn();
beforeEach(() => { localStorage.clear(); replaceState.mockClear(); });
afterEach(cleanup);
function mount() {
  render(<LocaleProvider><RequestViewsPanel actions={{ state: parsePageSearch({}), replaceState } as unknown as RequestLogPageActions} filterOptions={{ models: [], endpoints: [], clients: [], resolved_target_models: [] }} filterOptionsLoaded /></LocaleProvider>);
  fireEvent.click(screen.getByRole('button', { name: '保存的视图' }));
}
it('does not apply missing model until the operator explicitly repairs the filter', async () => {
  saveRequestLogView('旧模型', parsePageSearch({ ingress_model_id: 'deleted' })); mount();
  fireEvent.click(screen.getByRole('button', { name: '恢复预设' }));
  await screen.findByText(/请核对当前实例引用/);
  expect(replaceState).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: '当前模型 · current' }));
  fireEvent.click(screen.getByRole('button', { name: '保存修复并恢复' }));
  await waitFor(() => expect(replaceState).toHaveBeenCalledWith(expect.objectContaining({ model_id: 'current' })));
});
it('requires explicit confirmation even if a numeric ID exists in the current instance', async () => {
  saveRequestLogView('端点筛选', parsePageSearch({ endpoint: '3' })); mount();
  fireEvent.click(screen.getByRole('button', { name: '恢复预设' }));
  await screen.findByText(/请核对当前实例引用/);
  expect(replaceState).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: '保存修复并恢复' }));
  await waitFor(() => expect(replaceState).toHaveBeenCalledWith(expect.objectContaining({ endpoint_id: '3' })));
});
it('reports an oversized legacy view export without replacing readable storage', () => {
  const state = savedViewStateOf(parsePageSearch({ view: 'attempts', ingress_model_id: '模'.repeat(2048), error_text: '\u0001'.repeat(2048) }));
  const views = Array.from({ length: 20 }, (_, index) => ({ id: `legacy-${index}`, name: `旧视图-${index}`, createdAt: '', updatedAt: '', state }));
  const raw = JSON.stringify({ version: 1, views });
  localStorage.setItem('prism.request-logs.saved-views.v1', raw);
  mount();
  expect(screen.getByText('旧视图-0')).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: '导出偏好文件' }));
  expect(screen.getByText(/超过 256 KiB 文件上限/)).toBeVisible();
  expect(localStorage.getItem('prism.request-logs.saved-views.v1')).toBe(raw);
});
it('shows session-only storage failure while keeping the current view usable', async () => {
  const denied = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('quota'); });
  try {
    mount();
    fireEvent.change(screen.getByRole('textbox', { name: '视图名称' }), { target: { value: '临时视图' } });
    fireEvent.click(screen.getByRole('button', { name: /^保存$/ }));
    await waitFor(() => expect(screen.getAllByText(/更改仅本次可用/).length).toBeGreaterThan(0));
    expect(screen.getByText('临时视图', { exact: true })).toBeVisible();
    expect(screen.queryByText('已保存到此浏览器。')).not.toBeInTheDocument();
  } finally { denied.mockRestore(); }
});
