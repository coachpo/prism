import { describe, expect, it } from 'vitest';
import { applyRequestLogStatePatch, parsePageSearch, requestLogStateForView, stateToSearch } from './queryParams';
import { buildRequestLogQueryParams } from './requestLogQuery';
import { buildExportParams } from './requestLogsCsv';

describe('retained chain ranking URL contract', () => {
  it.each(['elapsed_ms', 'total_cost_user_currency_micros'])('preserves %s through URL, list and full export', (metric) => {
    const state = parsePageSearch({ view: 'ingress_chains', sort_by: metric, sort_order: 'asc', cost_segment_key: 'e.4' });
    expect(state.sort_by).toBe(metric);
    expect(parsePageSearch(stateToSearch(state))).toEqual(state);
    for (const params of [buildRequestLogQueryParams(state), buildExportParams(state)]) {
      expect(params).toMatchObject({ sort_by: metric, sort_order: 'asc', cost_segment_key: 'e.4' });
    }
    expect(buildExportParams(state)).not.toHaveProperty('chain_cursor');
  });
  it('invalidates page cursors when the metric changes and keeps attempt grammar separate', () => {
    const state = parsePageSearch({ view: 'ingress_chains', sort_by: 'elapsed_ms', chain_cursor: 'opaque' });
    expect(applyRequestLogStatePatch(state, { sort_by: 'created_at' }).chain_cursor).toBe('');
    expect(requestLogStateForView(state, 'attempts').sort_by).toBe('created_at');
    expect(parsePageSearch({ view: 'attempts', sort_by: 'elapsed_ms' }).sort_by).toBe('created_at');
  });
});
