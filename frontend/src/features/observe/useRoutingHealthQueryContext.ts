import { useCallback, useEffect, useRef, useState } from "react";

import { api } from "@/lib/api";
import type {
  EventsQueryContextParams,
  EventsQueryContextPreset,
} from "@/lib/api/observability";
import type { EventsQueryContextResponse } from "@/lib/types";
import {
  type RoutingHealthSearch,
  type RoutingHealthSearchUpdater,
} from "./routingHealthSearch";

export interface RoutingHealthContextState {
  phase: "idle" | "loading" | "ready" | "error";
  context: EventsQueryContextResponse | null;
  error: string | null;
}

interface UseRoutingHealthQueryContextInput {
  search: RoutingHealthSearch;
  onSearchChange: RoutingHealthSearchUpdater;
  loadFailedMessage: string;
}

export function useRoutingHealthQueryContext({
  search,
  onSearchChange,
  loadFailedMessage,
}: UseRoutingHealthQueryContextInput) {
  const preset = (search.preset as EventsQueryContextPreset) || "24h";
  const fromTime = (search.from_time as string) || undefined;
  const toTime = (search.to_time as string) || undefined;
  const eventCursor = (search.event_cursor as string) || undefined;
  const windowKey = JSON.stringify({ preset, fromTime, toTime });
  const [contextState, setContextState] = useState<RoutingHealthContextState>({
    phase: "idle",
    context: null,
    error: null,
  });
  const contextGeneration = useRef(0);
  const searchStateRef = useRef({ eventCursor, onSearchChange });
  const lastWindowKeyRef = useRef<string | null>(null);

  const issueContext = useCallback(async () => {
    const generation = ++contextGeneration.current;
    setContextState((current) => ({ ...current, phase: "loading" }));
    const params: EventsQueryContextParams =
      preset === "custom"
        ? {
            requested_preset: "custom",
            custom_from_time: fromTime,
            custom_to_time: toTime,
          }
        : { requested_preset: preset };
    try {
      const response = await api.loadbalance.issueEventsQueryContext(params);
      if (generation !== contextGeneration.current) return null;
      setContextState({ phase: "ready", context: response, error: null });
      return response;
    } catch (error) {
      if (generation !== contextGeneration.current) return null;
      setContextState({
        phase: "error",
        context: null,
        error: error instanceof Error ? error.message : loadFailedMessage,
      });
      return null;
    }
  }, [fromTime, loadFailedMessage, preset, toTime]);

  useEffect(() => {
    searchStateRef.current = { eventCursor, onSearchChange };
  }, [eventCursor, onSearchChange]);

  useEffect(() => {
    let cancelled = false;
    const previousKey = lastWindowKeyRef.current;
    lastWindowKeyRef.current = windowKey;
    const run = async () => {
      const context = await issueContext();
      if (cancelled || !context) return;
      if (
        previousKey !== null &&
        previousKey !== windowKey &&
        searchStateRef.current.eventCursor
      ) {
        searchStateRef.current.onSearchChange({ event_cursor: undefined });
      }
    };
    void run();
    return () => {
      cancelled = true;
    };
  }, [issueContext, windowKey]);

  return {
    contextState,
    eventCursor,
    fromTime,
    issueContext,
    preset,
    toTime,
    windowKey,
  };
}
