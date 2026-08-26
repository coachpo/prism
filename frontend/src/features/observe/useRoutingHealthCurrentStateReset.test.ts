import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { LoadbalanceCurrentStateResetResponse } from "@/lib/types";
import { useRoutingHealthCurrentStateReset } from "./useRoutingHealthCurrentStateReset";

const mocks = vi.hoisted(() => ({
  resetCurrentState: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  api: {
    loadbalance: {
      resetCurrentState: mocks.resetCurrentState,
    },
  },
}));

describe("useRoutingHealthCurrentStateReset", () => {
  it("calibrates through the read owner and refreshes after a successful reset", async () => {
    const response = {
      connection_id: 42,
      cleared: true,
      state: null,
    } as LoadbalanceCurrentStateResetResponse;
    mocks.resetCurrentState.mockResolvedValue(response);
    const applyResetSnapshot = vi.fn();
    const load = vi.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() =>
      useRoutingHealthCurrentStateReset({
        applyResetSnapshot,
        load,
        resetFailedMessage: "Reset failed",
        resetNothingToClearMessage: "Nothing to clear",
      }),
    );

    await act(async () => {
      await result.current.resetTarget(42);
    });

    expect(mocks.resetCurrentState).toHaveBeenCalledWith(42);
    expect(applyResetSnapshot).toHaveBeenCalledWith(42, response);
    expect(load).toHaveBeenCalledWith("refresh");
    expect(result.current.resettingTargetId).toBeNull();
    expect(result.current.resetError).toBeNull();
  });
});
