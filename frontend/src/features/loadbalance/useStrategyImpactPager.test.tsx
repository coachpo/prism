import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { StrategyImpactListResponse } from "@/lib/types";
import { useStrategyImpactPager } from "./useStrategyImpactPager";

const mocks = vi.hoisted(() => ({
  impact: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  api: {
    loadbalanceStrategies: {
      impact: mocks.impact,
    },
  },
}));

function impactItem(modelConfigId: number): StrategyImpactListResponse["items"][number] {
  return {
    model_config_id: modelConfigId,
    model_id: `model-${modelConfigId}`,
    display_name: `Model ${modelConfigId}`,
    is_enabled: true,
  };
}

function impactPage(
  items: StrategyImpactListResponse["items"],
  nextCursor: string | null,
): StrategyImpactListResponse {
  return {
    strategy_id: 7,
    attached_model_count: 2,
    items,
    has_more: nextCursor !== null,
    next_cursor: nextCursor,
  };
}

describe("useStrategyImpactPager", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("retains the first page after an append error and retries the same cursor", async () => {
    let appendAttempts = 0;
    mocks.impact.mockImplementation(
      async (_strategyId: number, params: { cursor?: string }) => {
        if (!params.cursor) {
          return impactPage([impactItem(1)], "cursor-1");
        }
        appendAttempts += 1;
        if (appendAttempts === 1) throw new Error("temporary impact failure");
        return impactPage([impactItem(1), impactItem(2)], null);
      },
    );

    const { result } = renderHook(() => useStrategyImpactPager([7]));

    await act(async () => {
      await result.current.toggleImpact(7);
    });
    await waitFor(() => {
      expect(result.current.impactStates[7]?.fragment.phase).toBe("ready");
    });

    await act(async () => {
      await result.current.loadMoreImpact(7);
    });
    expect(result.current.impactStates[7]?.fragment.stale).toBe(true);
    expect(
      result.current.impactStates[7]?.fragment.data?.items.map(
        (item) => item.model_config_id,
      ),
    ).toEqual([1]);

    await act(async () => {
      await result.current.loadMoreImpact(7);
    });
    await waitFor(() => {
      expect(
        result.current.impactStates[7]?.fragment.data?.items.map(
          (item) => item.model_config_id,
        ),
      ).toEqual([1, 2]);
    });
    expect(mocks.impact).toHaveBeenNthCalledWith(2, 7, {
      limit: 25,
      cursor: "cursor-1",
    });
    expect(mocks.impact).toHaveBeenNthCalledWith(3, 7, {
      limit: 25,
      cursor: "cursor-1",
    });
  });
});
