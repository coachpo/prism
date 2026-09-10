import { buildQuery, request } from './request';
export interface CostSegmentsPage {
  cost_segments: { segment_key: string; currency_code: string | null }[];
  cost_segments_snapshot_hash: string;
  cost_segments_next_cursor: string | null;
}
export const costSegments = {
  costSegments: (cursor?: string, signal?: AbortSignal) => request<CostSegmentsPage>(`/api/stats/cost-segments?${buildQuery({ limit: 100, cursor })}`, { signal }),
};
