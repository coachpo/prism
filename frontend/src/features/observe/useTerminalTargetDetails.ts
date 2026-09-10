import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import type { Endpoint } from "@/lib/types";
import type { TerminalTargetStatisticsResponse } from "@/lib/api/observability";
import { useObserveReadCycle } from "./observeReadCycleContext";
import type { ObservePreset } from "./observeSearch";

export type TerminalTargetScope = "final_execution" | "route_attempt";
export type EndpointDetail = {
  phase: "loading" | "ready" | "error";
  error: string | null;
  response: TerminalTargetStatisticsResponse | null;
};

/** Lazy endpoint observations retain last-good data only within the same scope and preset. */
export function useTerminalTargetDetails(preset: ObservePreset, scope: TerminalTargetScope) {
  const { track, revision } = useObserveReadCycle();
  const [endpoints, setEndpoints] = useState<Endpoint[]>([]);
  const [endpointsFailed, setEndpointsFailed] = useState(false);
  const [endpointsReloadKey, setEndpointsReloadKey] = useState(0);
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(() => new Set());
  const [snapshot, setSnapshot] = useState<{ basis: string; details: ReadonlyMap<number, EndpointDetail> }>(() => ({ basis: `${preset}:${scope}`, details: new Map() }));
  const basis = `${preset}:${scope}`;
  const details = snapshot.basis === basis ? snapshot.details : new Map<number, EndpointDetail>();
  const controllers = useRef(new Map<number, AbortController>());
  const loaded = useRef(new Set<number>());

  useEffect(() => {
    const controller = new AbortController();
    void track(api.endpoints.list(controller.signal)).then(items => {
      if (controller.signal.aborted) return;
      setEndpoints(items);
      setEndpointsFailed(false);
    }).catch(() => { if (!controller.signal.aborted) setEndpointsFailed(true); });
    return () => controller.abort();
  }, [endpointsReloadKey, revision, track]);

  const load = useCallback((endpointId: number) => {
    controllers.current.get(endpointId)?.abort();
    const controller = new AbortController();
    controllers.current.set(endpointId, controller);
    loaded.current.add(endpointId);
    const update = (change: (previous?: EndpointDetail) => EndpointDetail) => setSnapshot(previous => {
      const next = new Map(previous.basis === basis ? previous.details : []);
      next.set(endpointId, change(next.get(endpointId)));
      return { basis, details: next };
    });
    update(previous => ({ phase: "loading", error: null, response: previous?.response ?? null }));
    void track(api.stats.endpointTerminalTargets(endpointId, { preset, scope }, controller.signal)).then(response => {
      if (!controller.signal.aborted) update(() => ({ phase: "ready", error: null, response }));
    }).catch((error: unknown) => {
      if (!controller.signal.aborted) update(previous => ({ phase: "error", error: error instanceof Error ? error.message : String(error), response: previous?.response ?? null }));
    });
  }, [basis, preset, scope, track]);

  useEffect(() => {
    const currentControllers = controllers.current;
    for (const id of loaded.current) load(id);
    return () => { for (const controller of currentControllers.values()) controller.abort(); };
  }, [load, revision]);

  const toggleEndpoint = (id: number) => {
    const next = new Set(expanded);
    if (next.has(id)) next.delete(id);
    else {
      next.add(id);
      if (!details.get(id) || details.get(id)?.phase === "error") load(id);
    }
    setExpanded(next);
  };
  return { endpoints, endpointsFailed, retryEndpoints: () => setEndpointsReloadKey(value => value + 1), expanded, details, load, toggleEndpoint };
}
