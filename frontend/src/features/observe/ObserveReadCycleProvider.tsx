import { useCallback, useMemo, useRef, useState, type ReactNode } from "react";
import { ObserveReadCycleContext } from "./observeReadCycleContext";

/** Counts the dashboard's actual reads, including lazy panels, before another refresh begins. */
export function ObserveReadCycleProvider({ children }: { children: ReactNode }) {
  const pending = useRef(0);
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const track = useCallback(<T,>(read: Promise<T>): Promise<T> => {
    pending.current += 1;
    setBusy(true);
    return read.finally(() => { pending.current -= 1; if (!pending.current) setBusy(false); });
  }, []);
  const isBusy = useCallback(() => pending.current > 0, []);
  const advance = useCallback(() => setRevision(value => value + 1), []);
  const value = useMemo(() => ({ track, isBusy, revision, busy, advance }), [track, isBusy, revision, busy, advance]);
  return <ObserveReadCycleContext value={value}>{children}</ObserveReadCycleContext>;
}
