import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  QueryCoverage,
  RequestLogListResponse,
} from "@/lib/types";
import { parsePageSearch } from "./queryParams";
import { useRequestLogsPageData } from "./useRequestLogsPageData";

const mocks = vi.hoisted(() => ({
  chains: vi.fn(),
  requests: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  api: {
    stats: {
      chains: mocks.chains,
      requests: mocks.requests,
    },
  },
}));

vi.mock("@/i18n/staticMessages", () => ({
  getStaticMessages: () => ({
    requestLogs: { loadFailed: "load failed" },
  }),
}));

function coverage(sourceRevision: string): QueryCoverage {
  return {
    requested_from_time: "2026-08-01T00:00:00Z",
    requested_to_time: "2026-08-02T00:00:00Z",
    effective_from_time: "2026-08-01T00:00:00Z",
    effective_to_time: "2026-08-02T00:00:00Z",
    complete: true,
    gaps: [],
    state: "known",
    source_revision: sourceRevision,
  };
}

function filterOptions(label: string) {
  return {
    ingress_models: [{ ingress_model_id: label, model_label: label }],
    endpoints: [{ endpoint_id: 1, endpoint_label: label }],
    clients: [{ client_rule_id: 1, client_label: label }],
    attempt_target_models: [
      { attempt_target_model_id: label, model_label: label },
    ],
  };
}

function attemptResponse(label: string): RequestLogListResponse {
  return {
    items: [],
    total: 0,
    total_is_exact: true,
    has_more: false,
    limit: 100,
    offset: 0,
    filter_options: filterOptions(label),
    coverage: coverage(label),
    caliber: {},
    dataset_coverage: {},
    samples: {},
  };
}

function chainResponse(label: string) {
  return {
    view: "ingress_chains",
    query_context: null,
    source_ingress_total: 0,
    retained_ingress_total: 0,
    retained_upstream_attempt_total: 0,
    retained_request_log_row_total: 0,
    legacy_unknown_row_total: 0,
    page_ingress_count: 0,
    page_upstream_attempt_count: 0,
    page_request_log_row_count: 0,
    items: [],
    filter_options: filterOptions(label),
    has_more_chains: false,
    next_chain_cursor: null,
    source_coverage: coverage(label),
  };
}

describe("request-log page metadata handoff", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("keeps loaded metadata while the newly selected view is still pending", async () => {
    mocks.requests.mockResolvedValueOnce(attemptResponse("attempt"));
    let resolveChains!: (value: ReturnType<typeof chainResponse>) => void;
    mocks.chains.mockImplementationOnce(
      () => new Promise((resolve) => {
        resolveChains = resolve;
      }),
    );
    const attemptState = parsePageSearch({ view: "attempts" });
    const chainState = parsePageSearch({ view: "ingress_chains" });
    const { result, rerender } = renderHook(
      ({ state }: { state: typeof attemptState }) =>
        useRequestLogsPageData({ revision: 0, state, enabled: true }),
      { initialProps: { state: attemptState } },
    );

    await waitFor(() => expect(result.current.filterOptionsLoaded).toBe(true));
    expect(result.current.filterOptions.models[0]?.ingress_model_id).toBe("attempt");
    expect(result.current.coverage?.source_revision).toBe("attempt");

    rerender({ state: chainState });
    await waitFor(() => expect(mocks.chains).toHaveBeenCalled());
    expect(result.current.items).toEqual([]);
    expect(result.current.filterOptions.models[0]?.ingress_model_id).toBe("attempt");
    expect(result.current.coverage?.source_revision).toBe("attempt");

    await act(async () => {
      resolveChains(chainResponse("chain"));
      await Promise.resolve();
    });
    await waitFor(() =>
      expect(result.current.filterOptions.models[0]?.ingress_model_id).toBe("chain"),
    );
    expect(result.current.coverage?.source_revision).toBe("chain");
  });

  it("keeps stale active metadata but does not borrow it over an active error", async () => {
    mocks.requests
      .mockResolvedValueOnce(attemptResponse("attempt"))
      .mockRejectedValueOnce(new Error("attempt refresh failed"));
    mocks.chains.mockRejectedValueOnce(new Error("chain read failed"));
    const attemptState = parsePageSearch({ view: "attempts" });
    const chainState = parsePageSearch({ view: "ingress_chains" });
    const { result, rerender } = renderHook(
      ({ state }: { state: typeof attemptState }) =>
        useRequestLogsPageData({ revision: 0, state, enabled: true }),
      { initialProps: { state: attemptState } },
    );

    await waitFor(() => expect(result.current.filterOptionsLoaded).toBe(true));
    await act(async () => {
      result.current.refresh();
    });
    await waitFor(() => expect(result.current.stale).toBe(true));
    expect(result.current.coverage?.source_revision).toBe("attempt");

    rerender({ state: chainState });
    await waitFor(() => expect(result.current.error).toBe("chain read failed"));
    expect(result.current.stale).toBe(false);
    expect(result.current.coverage).toBeNull();
    expect(result.current.filterOptions.models[0]?.ingress_model_id).toBe("attempt");
  });

  it.each(["attempts", "ingress_chains"])("marks %s page changes pending through debounce and response", async (view) => {
    const request = view === "attempts" ? mocks.requests : mocks.chains;
    const response = view === "attempts" ? attemptResponse("page") : chainResponse("page");
    request.mockResolvedValueOnce(response);
    let rejectPage!: (error: Error) => void;
    request.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectPage = reject; }));
    const state = parsePageSearch({ view });
    const { result, rerender } = renderHook(
      ({ pageState }) => useRequestLogsPageData({ revision: 0, state: pageState, enabled: true }),
      { initialProps: { pageState: state } },
    );
    await waitFor(() => expect(result.current.loading).toBe(false));
    rerender({ pageState: { ...state, offset: 100, chain_cursor: "next-page" } });
    expect(result.current.loading).toBe(true);
    expect(result.current.readKind).toBe("replace");
    expect(request).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(request).toHaveBeenCalledTimes(2));
    expect(result.current.loading).toBe(true);
    expect(result.current.readKind).toBe("replace");
    await act(async () => { rejectPage(new Error("page failed")); });
    expect(result.current.loading).toBe(false);
    expect(result.current.stale).toBe(false);
    expect(result.current.error).toBe("page failed");
  });

  it("masks all public read metadata when the page lane is disabled", async () => {
    mocks.requests.mockResolvedValueOnce(attemptResponse("attempt"));
    const state = parsePageSearch({ view: "attempts" });
    const { result, rerender } = renderHook(
      ({ enabled }: { enabled: boolean }) =>
        useRequestLogsPageData({ revision: 0, state, enabled }),
      { initialProps: { enabled: true } },
    );

    await waitFor(() => expect(result.current.filterOptionsLoaded).toBe(true));
    expect(result.current.lastLoadedAt).not.toBeNull();
    rerender({ enabled: false });

    expect(result.current.items).toEqual([]);
    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBeNull();
    expect(result.current.stale).toBe(false);
    expect(result.current.lastLoadedAt).toBeNull();
    expect(result.current.filterOptionsLoaded).toBe(false);
    expect(result.current.coverage).toBeNull();
  });
});
