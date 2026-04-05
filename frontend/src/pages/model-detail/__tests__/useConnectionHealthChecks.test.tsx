import { renderHook, act } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useConnectionHealthChecks } from "../useConnectionHealthChecks";

const api = vi.hoisted(() => ({
  connections: {
    healthCheck: vi.fn(),
  },
}));

vi.mock("@/lib/api", () => ({ api }));

describe("useConnectionHealthChecks", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.connections.healthCheck.mockResolvedValue({
      health_status: "healthy",
      response_time_ms: 120,
      detail: "ok",
      checked_at: "2026-03-15T10:00:00Z",
    });
  });

  it("runs health checks only when requested", async () => {
    const setConnections = vi.fn();
    const onSuccessfulChecks = vi.fn();
    const { result } = renderHook(() =>
      useConnectionHealthChecks({
        setConnections,
        onSuccessfulChecks,
      })
    );

    await act(async () => {
      await result.current.runHealthChecks([1]);
    });

    expect(api.connections.healthCheck).toHaveBeenCalledTimes(1);
    expect(setConnections).toHaveBeenCalled();
    expect(onSuccessfulChecks).toHaveBeenCalledWith([1]);
  });
});
