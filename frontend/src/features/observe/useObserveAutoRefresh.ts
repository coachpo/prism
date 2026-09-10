import { useEffect, useState, useEffectEvent } from "react";

export type ObserveRefreshInterval = 0 | 30 | 60;

/** A hidden tab never starts a read; resuming starts a full new delay, with no catch-up burst. */
export function useObserveAutoRefresh(interval: ObserveRefreshInterval, refresh: () => void, isBusy: () => boolean) {
  const runRefresh = useEffectEvent(refresh);
  const busy = useEffectEvent(isBusy);
  const [visible, setVisible] = useState(() => document.visibilityState !== "hidden");
  useEffect(() => {
    const update = () => setVisible(document.visibilityState !== "hidden");
    document.addEventListener("visibilitychange", update);
    return () => document.removeEventListener("visibilitychange", update);
  }, []);
  useEffect(() => {
    if (!interval || !visible) return;
    const timer = window.setInterval(() => {
      if (document.visibilityState !== "hidden" && !busy()) runRefresh();
    }, interval * 1000);
    return () => window.clearInterval(timer);
  }, [interval, visible]);
  return visible;
}
