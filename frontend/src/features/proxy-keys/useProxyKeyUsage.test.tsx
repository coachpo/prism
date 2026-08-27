// C6 regression: per-key usage reads expose honest background-refresh states —
// a first read has no cached value (Skeleton), while a re-read over a cached
// value keeps the number and reports refreshing instead of blanking.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

import { useProxyKeyUsage } from "./useProxyKeyUsage";

const mocks = vi.hoisted(() => ({
  requests: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  api: {
    stats: {
      requests: mocks.requests,
    },
  },
}));

function listResponse(total: number) {
  return {
    items: [],
    total,
    total_is_exact: true,
    has_more: false,
    coverage: { complete: true },
    filter_options: {
      ingress_models: [],
      endpoints: [],
      clients: [],
      attempt_target_models: [],
    },
  };
}

function withClient(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe("proxy key usage read states (C6)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.requests.mockResolvedValue(listResponse(7));
  });

  it("reports loading before any cached value and refreshing only over cache", async () => {
    let resolveSecond: ((value: unknown) => void) | undefined = undefined;
    mocks.requests.mockImplementationOnce(async () => listResponse(7));
    const { result } = renderHook(() => useProxyKeyUsage([42]), {
      wrapper: ({ children }) => withClient(children),
    });

    // First read: no cached value yet.
    await waitFor(() => expect(result.current.entries.get(42)).toBeDefined());
    expect(result.current.entries.get(42)?.loading).toBe(true);

    await waitFor(() =>
      expect(result.current.entries.get(42)?.loading).toBe(false),
    );
    expect(result.current.entries.get(42)?.total).toBe(7);
    expect(result.current.entries.get(42)?.refreshing).toBe(false);

    // A refetch over the cached value keeps total on screen while reporting
    // refreshing; the cell must not fall back to a Skeleton.
    mocks.requests.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveSecond = resolve;
        }),
    );
    let refetch: ReturnType<typeof Object> | undefined;
    await act(async () => {
      refetch = result.current.refetch;
    });
    act(() => {
      refetch?.();
    });
    await waitFor(() =>
      expect(result.current.entries.get(42)?.refreshing).toBe(true),
    );
    expect(result.current.entries.get(42)?.total).toBe(7);
    expect(result.current.entries.get(42)?.loading).toBe(false);

    await act(async () => {
      resolveSecond?.(listResponse(9));
    });
    await waitFor(() => expect(result.current.entries.get(42)?.total).toBe(9));
    expect(result.current.entries.get(42)?.refreshing).toBe(false);
  });
});
