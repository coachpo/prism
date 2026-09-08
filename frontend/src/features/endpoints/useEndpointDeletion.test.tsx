import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { api } from "@/lib/api";
import type { Endpoint, EndpointReferenceDetail } from "@/lib/types";
import { useEndpointDeletion } from "./useEndpointDeletion";
const endpoint: Endpoint = {
  id: 1, name: "Endpoint", base_url: "https://example.com",
  has_api_key: false, api_key_fingerprint: null, api_key_updated_at: null,
  config_revision: 1, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
};
const detail: EndpointReferenceDetail = {
  endpoint_id: 1,
  summary: { direct_reference_count: 2, referencing_model_count: 2, enabled_reference_count: 2, orphan_reference_count: 0 },
  reference_page: { items: [], total_count: 2, next_cursor: "next-page", reference_snapshot_hash: "snapshot" },
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}
function setup(loadMore: (id: number) => Promise<EndpointReferenceDetail | null>) {
  vi.spyOn(api.endpoints, "referencesDetail").mockResolvedValue(detail);
  return renderHook(() => useEndpointDeletion({
    endpoints: [endpoint], commitEndpoints: vi.fn(),
    references: { adoptDetail: vi.fn(), loadMore, removeEndpoint: vi.fn() },
  }));
}
describe("endpoint deletion blocker pagination", () => {
  it("exposes pending state, rejects duplicate loads, and permits retry after a failed read", async () => {
    const page = deferred<EndpointReferenceDetail | null>();
    const loadMore = vi.fn().mockReturnValueOnce(page.promise).mockResolvedValueOnce(detail);
    const { result } = setup(loadMore);
    act(() => result.current.handleDeleteRequest(endpoint));
    await waitFor(() => expect(result.current.deleteDialog.phase).toBe("blocked"));
    let loading!: Promise<void>;
    act(() => {
      loading = result.current.handleLoadMoreBlockers(1);
      void result.current.handleLoadMoreBlockers(1);
    });
    expect(result.current.deleteDialog).toMatchObject({ phase: "blocked", detail, loadingMore: true });
    expect(loadMore).toHaveBeenCalledTimes(1);
    await act(async () => { page.resolve(null); await loading; });
    expect(result.current.deleteDialog).toMatchObject({ phase: "blocked", detail, loadingMore: false });
    await act(async () => { await result.current.handleLoadMoreBlockers(1); });
    expect(loadMore).toHaveBeenCalledTimes(2);
  });
});
