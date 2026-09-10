import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useRequestComparisonCapture } from "./useRequestComparisonCapture";

const get = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api", () => ({ api: { audit: { get } } }));

describe("explicit independent capture reads", () => {
  it("does not prefetch; cancels late responses and preserves the other side across failure and retry", async () => {
    get.mockReset();
    let resolveLate!: (value: unknown) => void;
    get.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveLate = resolve;
        }),
    );
    const { result, unmount } = renderHook(() =>
      useRequestComparisonCapture(["9007199254740997", "42"]),
    );
    expect(get).not.toHaveBeenCalled();
    let pending!: Promise<void>;
    act(() => {
      pending = result.current.load(0, 1);
    });
    const signal = get.mock.calls[0][1].signal as AbortSignal;
    act(() => result.current.cancel(0));
    expect(signal.aborted).toBe(true);
    await act(async () => {
      resolveLate({ request_log_id: "9007199254740997", id: 1 });
      await pending;
    });
    expect(result.current.captures[0].detail).toBeNull();
    get.mockResolvedValueOnce({ request_log_id: "42", id: 2 });
    await act(async () => {
      await result.current.load(1, 2);
    });
    get.mockRejectedValueOnce(new Error("one side failed"));
    await act(async () => {
      await result.current.load(0, 3);
    });
    expect(result.current.captures[0].error).toBe("one side failed");
    expect(result.current.captures[1].detail?.id).toBe(2);
    get.mockResolvedValueOnce({ request_log_id: "9007199254740997", id: 3 });
    await act(async () => {
      await result.current.load(0, 3);
    });
    expect(result.current.captures[0].detail?.id).toBe(3);
    expect(result.current.captures[1].detail?.id).toBe(2);
    unmount();
    expect((get.mock.calls[3][1].signal as AbortSignal).aborted).toBe(true);
  });
  it("rejects a retained capture that belongs to another request", async () => {
    get.mockResolvedValueOnce({ request_log_id: "99", id: 4 });
    const { result } = renderHook(() =>
      useRequestComparisonCapture(["1", "2"]),
    );
    await act(async () => {
      await result.current.load(0, 4);
    });
    expect(result.current.captures[0].detail).toBeNull();
    expect(result.current.captures[0].error).toBeTruthy();
  });
});
