import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import type { LoadbalanceEvent } from "@/lib/types";

const LOADBALANCE_EVENTS_PAGE_SIZE = 25;

export function useModelLoadbalanceEvents(modelId: string, revision: number) {
  const [events, setEvents] = useState<LoadbalanceEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const requestIdRef = useRef(0);
  const resetKey = `${modelId}:${revision}`;
  const resetKeyRef = useRef(resetKey);

  const fetchEvents = useCallback(async (nextOffset: number) => {
    const requestId = ++requestIdRef.current;
    setLoading(true);

    try {
      const data = await api.loadbalance.listEvents({
        model_id: modelId,
        limit: LOADBALANCE_EVENTS_PAGE_SIZE,
        offset: nextOffset,
      });

      if (requestId !== requestIdRef.current) {
        return;
      }

      setEvents(data.items);
      setTotal(data.total);
    } catch (error) {
      if (requestId !== requestIdRef.current) {
        return;
      }

      console.error("Failed to load model loadbalance events", error);
    } finally {
      if (requestId === requestIdRef.current) {
        setLoading(false);
      }
    }
  }, [modelId]);

  useEffect(() => {
    if (resetKeyRef.current !== resetKey) {
      resetKeyRef.current = resetKey;
      requestIdRef.current += 1;
      setEvents([]);
      setTotal(0);

      if (offset !== 0) {
        setOffset(0);
        return;
      }
    }

    void fetchEvents(offset);
  }, [fetchEvents, offset, resetKey]);

  return {
    events,
    loading,
    total,
    offset,
    limit: LOADBALANCE_EVENTS_PAGE_SIZE,
    refresh: () => fetchEvents(offset),
    goToPreviousPage: () => {
      setOffset((current) => Math.max(0, current - LOADBALANCE_EVENTS_PAGE_SIZE));
    },
    goToNextPage: () => {
      setOffset((current) => current + LOADBALANCE_EVENTS_PAGE_SIZE);
    },
  };
}
