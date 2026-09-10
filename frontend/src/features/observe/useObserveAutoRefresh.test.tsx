import { act, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useObserveAutoRefresh, type ObserveRefreshInterval } from "./useObserveAutoRefresh";
import { useObserveReadCycle } from "./observeReadCycleContext";
import { ObserveReadCycleProvider } from "./ObserveReadCycleProvider";

afterEach(() => { vi.useRealTimers(); Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" }); });

it("defaults off, waits for actual reads, pauses hidden tabs and cancels on leave", async () => {
  vi.useFakeTimers();
  const refresh = vi.fn();
  const { result, rerender, unmount } = renderHook(({ interval }: { interval: ObserveRefreshInterval }) => {
    const cycle = useObserveReadCycle();
    const visible = useObserveAutoRefresh(interval, refresh, cycle.isBusy);
    return { ...cycle, visible };
  }, { initialProps: { interval: 0 }, wrapper: ObserveReadCycleProvider });
  act(() => vi.advanceTimersByTime(120_000));
  expect(refresh).not.toHaveBeenCalled();
  let settle!: () => void;
  const reading = result.current.track(new Promise<void>(resolve => { settle = resolve; }));
  rerender({ interval: 30 });
  act(() => vi.advanceTimersByTime(90_000));
  expect(refresh).not.toHaveBeenCalled();
  await act(async () => { settle(); await reading; });
  act(() => vi.advanceTimersByTime(30_000));
  expect(refresh).toHaveBeenCalledTimes(1);
  act(() => { Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" }); document.dispatchEvent(new Event("visibilitychange")); });
  act(() => vi.advanceTimersByTime(90_000));
  expect(result.current.visible).toBe(false);
  expect(refresh).toHaveBeenCalledTimes(1);
  act(() => { Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" }); document.dispatchEvent(new Event("visibilitychange")); });
  act(() => vi.advanceTimersByTime(30_000));
  expect(refresh).toHaveBeenCalledTimes(2);
  unmount();
  act(() => vi.advanceTimersByTime(90_000));
  expect(refresh).toHaveBeenCalledTimes(2);
});
