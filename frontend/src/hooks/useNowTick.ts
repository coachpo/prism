import { useEffect, useState } from "react";

/**
 * Re-renders on an interval so a rendered conclusion can notice it has expired.
 *
 * Server-computed states such as a routing window's open/closed verdict carry
 * the boundary at which they stop being true. Without a tick, a badge rendered
 * at 17:55 keeps asserting "open" at 18:05 for as long as the page stays
 * mounted. This only drives the staleness comparison; it never recomputes the
 * verdict itself.
 */
export function useNowTick(intervalMs = 30_000): number {
  const [nowMs, setNowMs] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNowMs(Date.now()), intervalMs);
    return () => window.clearInterval(timer);
  }, [intervalMs]);
  return nowMs;
}
