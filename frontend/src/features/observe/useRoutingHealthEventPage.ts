import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { api } from "@/lib/api";
import type { ListEventsParams } from "@/lib/api/observability";
import type {
  LoadbalanceEventListResponse,
  LoadbalanceEventType,
} from "@/lib/types";
import {
  beginPagedRead,
  commitPagedRead,
  failPagedRead,
  initialPagedListState,
  type PageReadKind,
  type PagedListState,
} from "@/shared/table/paginationStates";
import {
  type RoutingHealthSearch,
  type RoutingHealthSearchUpdater,
} from "./routingHealthSearch";
import type { RoutingHealthContextState } from "./useRoutingHealthQueryContext";

const EVENTS_PAGE_SIZE = 25;

interface UseRoutingHealthEventPageInput {
  contextState: RoutingHealthContextState;
  issueContext: () => Promise<import("@/lib/types").EventsQueryContextResponse | null>;
  loadFailedMessage: string;
  onSearchChange: RoutingHealthSearchUpdater;
  search: RoutingHealthSearch;
  windowKey: string;
}

export function useRoutingHealthEventPage({
  contextState,
  issueContext,
  loadFailedMessage,
  onSearchChange,
  search,
  windowKey,
}: UseRoutingHealthEventPageInput) {
  const eventTypes = useMemo(
    () => normalizeSearchArray(search.event_type) as LoadbalanceEventType[],
    [search.event_type],
  );
  const failureKinds = useMemo(
    () => normalizeSearchArray(search.event_failure_kind) as ListEventsParams["failure_kind"],
    [search.event_failure_kind],
  );
  const admissionReasons = useMemo(
    () => normalizeSearchArray(search.event_admission_reason) as ListEventsParams["admission_reason"],
    [search.event_admission_reason],
  );
  const eventModelId = (search.event_model_id as string) || undefined;
  const eventEndpointId = (search.event_endpoint_id as string) || undefined;
  const eventTargetId =
    (search.event_terminal_target_id as string) || undefined;
  const sortOrder = (search.event_sort_order as "desc" | "asc") || "desc";
  const eventCursor = (search.event_cursor as string) || undefined;
  const urlEventId =
    typeof search.event_id === "string" && search.event_id !== ""
      ? search.event_id
      : null;
  const filtersKey = JSON.stringify({
    eventTypes,
    failureKinds,
    admissionReasons,
    eventModelId,
    eventEndpointId,
    eventTargetId,
    sortOrder,
  });
  const [fragment, setFragment] = useState<
    PagedListState<LoadbalanceEventListResponse>
  >(() => initialPagedListState());
  const [eventCursorStack, setEventCursorStack] = useState<string[]>([]);
  const [selectedEventId, setSelectedEventId] = useState<string | null>(null);
  const generation = useRef(0);
  const loadedKeyRef = useRef<string | null>(null);
  const lastWindowKeyRef = useRef(windowKey);

  useEffect(() => {
    setSelectedEventId(urlEventId);
  }, [urlEventId]);

  useEffect(() => {
    if (lastWindowKeyRef.current === windowKey) return;
    lastWindowKeyRef.current = windowKey;
    setEventCursorStack([]);
    loadedKeyRef.current = null;
  }, [windowKey]);

  const loadEvents = useCallback(
    async (
      context: import("@/lib/types").EventsQueryContextResponse,
      kind: PageReadKind,
      cursorOverride?: string,
    ) => {
      const current = ++generation.current;
      setFragment((state) => beginPagedRead(state, kind));
      try {
        const params: ListEventsParams = {
          query_context: context.query_context,
          sort_order: sortOrder,
          limit: EVENTS_PAGE_SIZE,
          event_type: eventTypes.length > 0 ? eventTypes : undefined,
          failure_kind:
            failureKinds && failureKinds.length > 0 ? failureKinds : undefined,
          admission_reason:
            admissionReasons && admissionReasons.length > 0
              ? admissionReasons
              : undefined,
          model_id: eventModelId,
          endpoint_id: eventEndpointId ? Number(eventEndpointId) : undefined,
          terminal_target_id: eventTargetId ? Number(eventTargetId) : undefined,
          cursor: cursorOverride ?? eventCursor,
        };
        const response = await api.loadbalance.listEvents(params);
        if (current !== generation.current) return;
        const phase: PagedListState<LoadbalanceEventListResponse>["phase"] =
          response.items.length === 0
            ? response.coverage.complete
              ? "empty"
              : "partial"
            : "ready";
        setFragment((state) => commitPagedRead(state, response, phase));
        loadedKeyRef.current = JSON.stringify({
          filtersKey,
          cursor: params.cursor ?? "",
        });
      } catch (error) {
        if (current !== generation.current) return;
        setFragment((state) =>
          failPagedRead(
            state,
            error instanceof Error ? error.message : loadFailedMessage,
          ),
        );
      }
    },
    [
      admissionReasons,
      eventCursor,
      eventEndpointId,
      eventModelId,
      eventTargetId,
      eventTypes,
      failureKinds,
      filtersKey,
      loadFailedMessage,
      sortOrder,
    ],
  );

  useEffect(() => {
    if (contextState.phase !== "ready" || !contextState.context) return;
    const key = JSON.stringify({ filtersKey, cursor: eventCursor ?? "" });
    const kind: PageReadKind =
      loadedKeyRef.current === null || fragment.data === null
        ? "initial"
        : loadedKeyRef.current === key
          ? "refresh"
          : "replace";
    loadedKeyRef.current = key;
    void loadEvents(contextState.context, kind, eventCursor);
    // The committed fragment is intentionally excluded: this effect decides
    // the read kind before the request changes its state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [contextState.context, contextState.phase, eventCursor, filtersKey, loadEvents]);

  const updateSearch = useCallback(
    (patch: RoutingHealthSearch, replace = true) => {
      setEventCursorStack([]);
      loadedKeyRef.current = null;
      onSearchChange(patch, replace);
    },
    [onSearchChange],
  );

  const retryRead = useCallback(() => {
    if (!contextState.context) return;
    const kind: PageReadKind =
      fragment.data === null
        ? "initial"
        : fragment.readKind === "refresh"
          ? "refresh"
          : "replace";
    void loadEvents(contextState.context, kind, eventCursor);
  }, [contextState.context, eventCursor, fragment.data, fragment.readKind, loadEvents]);

  const refresh = useCallback(() => {
    if (contextState.context) {
      void loadEvents(contextState.context, "refresh");
    }
  }, [contextState.context, loadEvents]);

  const goPreviousPage = useCallback(() => {
    const nextStack = eventCursorStack.slice(0, -1);
    setEventCursorStack(nextStack);
    onSearchChange({
      event_cursor: nextStack.at(-1),
      event_id: undefined,
    });
  }, [eventCursorStack, onSearchChange]);

  const goNextPage = useCallback(() => {
    const next = fragment.data?.next_cursor;
    if (!next) return;
    setEventCursorStack((stack) => [...stack, next]);
    onSearchChange({ event_cursor: next, event_id: undefined });
  }, [fragment.data?.next_cursor, onSearchChange]);

  const openEventDetail = useCallback(
    (eventId: string) => {
      setSelectedEventId(eventId);
      updateSearch({ event_id: eventId }, false);
    },
    [updateSearch],
  );

  const closeEventDetail = useCallback(() => {
    setSelectedEventId(null);
    updateSearch({ event_id: undefined });
  }, [updateSearch]);

  const retryContext = useCallback(async () => {
    const context = await issueContext();
    if (context) void loadEvents(context, "refresh");
  }, [issueContext, loadEvents]);

  const retryQueryContext = useCallback(() => {
    void issueContext();
  }, [issueContext]);

  return {
    admissionReasons,
    closeEventDetail,
    eventCursor,
    eventCursorStack,
    eventEndpointId,
    eventModelId,
    eventTargetId,
    eventTypes,
    failureKinds,
    filtersKey,
    fragment,
    goNextPage,
    goPreviousPage,
    loadEvents,
    openEventDetail,
    refresh,
    retryContext,
    retryQueryContext,
    retryRead,
    selectedEventId,
    sortOrder,
    updateSearch,
  };
}

function normalizeSearchArray(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.filter((item): item is string => typeof item === "string");
  }
  if (typeof value === "string") return [value];
  return [];
}
