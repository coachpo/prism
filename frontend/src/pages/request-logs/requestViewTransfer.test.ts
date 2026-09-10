import { PreferenceFileTooLargeError, serializePreferenceFile } from '@/lib/preferences/storage';
import { describe, expect, it } from 'vitest';
import { parsePageSearch } from './queryParams';
import { loadSavedViewsResult, saveRequestLogView, savedViewStateOf } from './requestLogSavedViews';
import { invalidViewReferences, parseRequestViewsFile, requestViewsFile, type ViewReferenceOptions } from './requestViewTransfer';
const options: ViewReferenceOptions = { model_id: [{ value: 'new-model', label: 'Model' }], endpoint_id: [], terminal_target_id: [], client_rule_id: [], proxy_api_key_id: [], resolved_target_model_id: [], cost_segment_key: [] };
function view() { return { id: 'old', name: '失败请求', createdAt: '', updatedAt: '', state: savedViewStateOf(parsePageSearch({ ingress_model_id: 'old-model', endpoint: '3', ingress_final_result: 'failed', view: 'attempts', sort_by: 'ttft_ms', sort_order: 'asc' })) }; }
describe('portable request views', () => {
  it('carries filters and sorting but never request identity, signed context or cursor', () => {
    const source = view();
    source.state = savedViewStateOf({ ...parsePageSearch({}), ...source.state, request_id: '37', ingress_request_id: 'req-secret', observe_return: 'signed', query_context: 'signed', chain_cursor: 'cursor' });
    const file = requestViewsFile([source]);
    const text = JSON.stringify(file);
    expect(text).not.toMatch(/req-secret|signed|cursor/);
    expect(parseRequestViewsFile(text)[0].state).toEqual(source.state);
  });
  it('round trips ingress elapsed and segmented cost rankings without signed pagination', () => {
    for (const sort_by of ['elapsed_ms', 'total_cost_user_currency_micros']) {
      const source = view();
      source.state = savedViewStateOf(parsePageSearch({ view: 'ingress_chains', sort_by, sort_order: 'desc', cost_segment_key: 'e.1' }));
      const restored = parseRequestViewsFile(JSON.stringify(requestViewsFile([source])))[0];
      expect(restored.state).toMatchObject({ view: 'ingress_chains', sort_by, sort_order: 'desc', cost_segment_key: 'e.1' });
    }
  });
  it('bounds cumulative saved views before mutation and leaves oversized legacy storage readable', () => {
    localStorage.clear();
    const large = parsePageSearch({ view: 'attempts', ingress_model_id: '模'.repeat(2048), attempt_target_model_id: '型'.repeat(2048), error_text: '\u0001'.repeat(2048) });
    let rejected = false;
    for (let index = 0; index < 20; index++) {
      const before = localStorage.getItem('prism.request-logs.saved-views.v1');
      try { saveRequestLogView(`view-${index}`, large); }
      catch (error) { expect(error).toBeInstanceOf(PreferenceFileTooLargeError); expect(localStorage.getItem('prism.request-logs.saved-views.v1')).toBe(before); rejected = true; break; }
    }
    expect(rejected).toBe(true);
    expect(() => serializePreferenceFile(requestViewsFile(loadSavedViewsResult().views))).not.toThrow();
    const oversized = Array.from({ length: 20 }, (_, index) => ({ ...view(), id: `legacy-${index}`, name: `legacy-${index}`, state: savedViewStateOf(large) }));
    localStorage.setItem('prism.request-logs.saved-views.v1', JSON.stringify({ version: 1, views: oversized }));
    expect(loadSavedViewsResult()).toMatchObject({ error: false, views: oversized });
    expect(() => serializePreferenceFile(requestViewsFile(oversized))).toThrow(PreferenceFileTooLargeError);
    localStorage.clear();
  });
  it('does not widen a missing or renamed entity filter to all', () => {
    const restored = parseRequestViewsFile(JSON.stringify(requestViewsFile([view()])))[0];
    expect(invalidViewReferences(restored, options)).toEqual(['model_id', 'endpoint_id']);
    expect(restored.state.model_id).toBe('old-model');
    restored.state.model_id = 'new-model'; restored.state.endpoint_id = '';
    expect(invalidViewReferences(restored, options)).toEqual([]);
  });
  it('rejects unsupported versions, unknown/transient fields and invalid enum rather than falling back', () => {
    const file = requestViewsFile([view()]);
    expect(() => parseRequestViewsFile(JSON.stringify({ ...file, version: 99 }))).toThrow();
    for (const field of [{ api_key: 'secret' }, { query_context: 'signed' }, { sort_by: 'unknown' }, { pricing_status: 'all', unpriced_reason: 'MISSING_PRICE_DATA' }, { view: 'ingress_chains', api_family: 'openai' }]) {
      expect(() => parseRequestViewsFile(JSON.stringify({ ...file, views: [{ name: 'unsafe', state: { ...view().state, ...field } }] }))).toThrow();
    }
  });
});
