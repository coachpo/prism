import {
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useRequestLogProxyApiKeyOptions } from "./useRequestLogProxyApiKeyOptions";

const mocks = vi.hoisted(() => ({
  filterOptions: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  api: {
    stats: { proxyApiKeyFilterOptions: mocks.filterOptions },
  },
}));

const option = (id: number, name: string) => ({
  proxy_api_key_id: id,
  proxy_api_key_name: name,
  key_preview: `pm-${id}`,
  configured: true,
});

function createWrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return function QueryWrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
  };
}

describe("request-log proxy API-key option owner", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("keeps the selected option visible and preserves query key parameters", async () => {
    const selected = option(9, "Selected key");
    mocks.filterOptions.mockResolvedValue({
      items: [option(1, "Matching key")],
      selected,
    });
    const { result } = renderHook(
      () => useRequestLogProxyApiKeyOptions("9"),
      { wrapper: createWrapper() },
    );

    await waitFor(() => expect(result.current.options).toHaveLength(2));
    expect(mocks.filterOptions).toHaveBeenCalledWith({
      q: undefined,
      selected_id: 9,
    });
    expect(result.current.options.map((item) => item.proxy_api_key_id)).toEqual([
      9,
      1,
    ]);

    mocks.filterOptions.mockResolvedValue({
      items: [option(2, "Search result")],
      selected,
    });
    act(() => {
      result.current.setSearch("search");
    });
    await waitFor(() =>
      expect({
        call: mocks.filterOptions.mock.lastCall?.[0],
        ids: result.current.options.map((item) => item.proxy_api_key_id),
      }).toEqual({
        call: { q: "search", selected_id: 9 },
        ids: [9, 2],
      }),
    );
  });
});
