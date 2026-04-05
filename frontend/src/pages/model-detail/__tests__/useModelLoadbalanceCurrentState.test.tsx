import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  LoadbalanceCurrentStateItem,
  LoadbalanceCurrentStateListResponse,
} from "@/lib/types";
import { useModelLoadbalanceCurrentState } from "../useModelLoadbalanceCurrentState";

function createDeferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });

  return { promise, resolve };
}

function buildCurrentStateItem(
  overrides: Partial<LoadbalanceCurrentStateItem> = {}
): LoadbalanceCurrentStateItem {
  return {
    connection_id: 1,
    consecutive_failures: 2,
    last_failure_kind: "timeout",
    last_cooldown_seconds: 30,
    blocked_until_at: "2026-03-23T10:05:00Z",
    state: "blocked",
    max_cooldown_strikes: 0,
    ban_mode: "off",
    banned_until_at: null,
    created_at: "2026-03-23T10:00:00Z",
    updated_at: "2026-03-23T10:01:00Z",
    ...overrides,
  } as LoadbalanceCurrentStateItem;
}

const api = vi.hoisted(() => ({
  loadbalance: {
    listCurrentState: vi.fn(),
    resetCurrentState: vi.fn(),
  },
}));

const toast = vi.hoisted(() => ({
  error: vi.fn(),
}));

vi.mock("@/lib/api", () => ({ api }));
vi.mock("sonner", () => ({ toast }));

describe("useModelLoadbalanceCurrentState", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.loadbalance.listCurrentState.mockResolvedValue({ items: [] });
    api.loadbalance.resetCurrentState.mockResolvedValue({
      connection_id: 1,
      cleared: true,
    });
  });

  it("does not fetch current state when disabled", async () => {
    renderHook(() =>
      useModelLoadbalanceCurrentState({
        modelConfigId: 11,
        revision: 1,
        enabled: false,
      })
    );

    await waitFor(() => {
      expect(api.loadbalance.listCurrentState).not.toHaveBeenCalled();
    });
  });

  it("ignores stale current-state responses after switching models", async () => {
    const firstResponse = createDeferred<LoadbalanceCurrentStateListResponse>();

    api.loadbalance.listCurrentState
      .mockImplementationOnce(() => firstResponse.promise)
      .mockResolvedValueOnce({
        items: [
          buildCurrentStateItem({
            connection_id: 9,
            state: "blocked",
            blocked_until_at: "2026-03-23T10:10:00Z",
          }),
        ],
      });

    const { result, rerender } = renderHook(
      ({ modelConfigId, revision }) =>
        useModelLoadbalanceCurrentState({ modelConfigId, revision }),
      {
        initialProps: {
          modelConfigId: 1,
          revision: 1,
        },
      }
    );

    await waitFor(() => {
      expect(api.loadbalance.listCurrentState).toHaveBeenCalledTimes(1);
    });

    rerender({ modelConfigId: 2, revision: 2 });

    await waitFor(() => {
      expect(api.loadbalance.listCurrentState).toHaveBeenCalledTimes(2);
    });

    await waitFor(() => {
      expect(result.current.currentStateByConnectionId.get(9)?.state).toBe(
        "blocked"
      );
    });

    firstResponse.resolve({
      items: [
        buildCurrentStateItem({
          connection_id: 5,
          state: "blocked",
        }),
      ],
    });

    await waitFor(() => {
      expect(result.current.currentStateByConnectionId.has(5)).toBe(false);
    });
  });

  it("clears the local cooldown signal immediately after a reset succeeds", async () => {
    api.loadbalance.listCurrentState.mockResolvedValue({
      items: [
        buildCurrentStateItem({
          connection_id: 7,
          state: "banned" as LoadbalanceCurrentStateItem["state"],
          ban_mode: "manual" as LoadbalanceCurrentStateItem["ban_mode"],
          max_cooldown_strikes: 3,
        }),
      ],
    });

    const resetResponse = createDeferred<{ connection_id: number; cleared: boolean }>();
    api.loadbalance.resetCurrentState.mockImplementationOnce(() => resetResponse.promise);

    const { result } = renderHook(() =>
      useModelLoadbalanceCurrentState({ modelConfigId: 7, revision: 1 })
    );

    await waitFor(() => {
      expect(result.current.currentStateByConnectionId.has(7)).toBe(true);
      expect(result.current.currentStateByConnectionId.get(7)?.state).toBe("banned");
    });

    act(() => {
      void result.current.resetCooldown(7);
    });

    await waitFor(() => {
      expect(result.current.resettingConnectionIds.has(7)).toBe(true);
    });

    await act(async () => {
      resetResponse.resolve({ connection_id: 7, cleared: true });
      await resetResponse.promise;
    });

    await waitFor(() => {
      expect(result.current.currentStateByConnectionId.has(7)).toBe(false);
      expect(result.current.resettingConnectionIds.has(7)).toBe(false);
    });
  });

  it("keeps the cooldown signal and shows an error when reset fails", async () => {
    api.loadbalance.listCurrentState.mockResolvedValue({
      items: [buildCurrentStateItem({ connection_id: 4, state: "counting" })],
    });
    api.loadbalance.resetCurrentState.mockRejectedValueOnce(
      new Error("Reset failed")
    );

    const { result } = renderHook(() =>
      useModelLoadbalanceCurrentState({ modelConfigId: 4, revision: 1 })
    );

    await waitFor(() => {
      expect(result.current.currentStateByConnectionId.has(4)).toBe(true);
    });

    await act(async () => {
      await result.current.resetCooldown(4);
    });

    expect(result.current.currentStateByConnectionId.has(4)).toBe(true);
    expect(toast.error).toHaveBeenCalledWith("Reset failed");
  });
});
