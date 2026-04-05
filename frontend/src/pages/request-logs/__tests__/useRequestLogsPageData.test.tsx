import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { parsePageState } from "../queryParams";
import { useRequestLogsPageData } from "../useRequestLogsPageData";

const api = vi.hoisted(() => ({
  endpoints: {
    list: vi.fn(),
  },
  models: {
    list: vi.fn(),
  },
  stats: {
    requests: vi.fn(),
  },
}));

vi.mock("@/lib/api", () => ({ api }));

function parseState(search: string) {
  return parsePageState(new URLSearchParams(search));
}

describe("useRequestLogsPageData", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
    api.models.list.mockResolvedValue([]);
    api.endpoints.list.mockResolvedValue([]);
    api.stats.requests.mockResolvedValue({
      items: [],
      total: 0,
      limit: 100,
      offset: 0,
    });
  });

  afterEach(() => {
    vi.runOnlyPendingTimers();
    vi.useRealTimers();
  });

  it("does not send request_id or removed list filters to the list endpoint", async () => {
    renderHook(() =>
      useRequestLogsPageData({
        revision: 1,
        state: parseState(
          "ingress_request_id=ingress_req_42&model_id=gpt-5.4&api_family=openai&endpoint_id=99&status_family=5xx&request_id=123&limit=300&offset=600",
        ),
        enabled: true,
      }),
    );

    await act(async () => {
      vi.advanceTimersByTime(0);
      await Promise.resolve();
    });

    await act(async () => {
      vi.advanceTimersByTime(300);
      await Promise.resolve();
    });

    expect(api.stats.requests).toHaveBeenCalledWith({
      ingress_request_id: "ingress_req_42",
      model_id: "gpt-5.4",
      status_family: "5xx",
      endpoint_id: 99,
      from_time: expect.any(String),
      limit: 300,
      offset: 600,
    });
  });

  it("returns only model and endpoint filter options", async () => {
    const { result } = renderHook(() =>
      useRequestLogsPageData({
        revision: 1,
        state: parseState("ingress_request_id=ingress_req_42&model_id=gpt-5.4&endpoint_id=99&status_family=5xx"),
        enabled: true,
      }),
    );

    await act(async () => {
      vi.advanceTimersByTime(0);
      await Promise.resolve();
    });

    await act(async () => {
      vi.advanceTimersByTime(300);
      await Promise.resolve();
    });

    expect(result.current.filterOptions).toEqual({
      models: [],
      endpoints: [],
    });
  });

  it("does not bootstrap browse data in exact-request mode", async () => {
    const { result } = renderHook(() =>
      useRequestLogsPageData({
        revision: 1,
        state: parseState("request_id=123&ingress_request_id=ingress_req_42"),
        enabled: false,
      }),
    );

    await act(async () => {
      vi.advanceTimersByTime(0);
      await Promise.resolve();
    });

    await act(async () => {
      vi.advanceTimersByTime(300);
      await Promise.resolve();
    });

    expect(api.models.list).not.toHaveBeenCalled();
    expect(api.endpoints.list).not.toHaveBeenCalled();
    expect(api.stats.requests).not.toHaveBeenCalled();
    expect(result.current.items).toEqual([]);
    expect(result.current.total).toBe(0);
    expect(result.current.loading).toBe(false);
    expect(result.current.filterOptionsLoaded).toBe(false);
  });
});
