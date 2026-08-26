import { useCallback, useMemo, useRef } from "react";

import { useEndpointReferenceDetails } from "./useEndpointReferenceDetails";
import { useEndpointReferenceSummaries } from "./useEndpointReferenceSummaries";

/**
 * Compose the two Endpoint reference lifecycles while keeping their state
 * owners independent. One generation lane is shared so batch summaries,
 * detail snapshots, and mutation-adopted preflights fence one another.
 */
export function useEndpointReferences(endpointIds: number[]) {
  const generationByEndpoint = useRef<Record<number, number>>({});
  const uniqueIds = useMemo(
    () => Array.from(new Set(endpointIds)),
    [endpointIds],
  );

  const issue = useCallback(
    (endpointId: number) => {
      const next = (generationByEndpoint.current[endpointId] ?? 0) + 1;
      generationByEndpoint.current[endpointId] = next;
      return next;
    },
    [],
  );
  const issueBatch = useCallback(
    (ids: readonly number[]) => {
      const generations: Record<number, number> = {};
      for (const id of ids) generations[id] = issue(id);
      return generations;
    },
    [issue],
  );
  const isCurrent = useCallback(
    (endpointId: number, generation: number) =>
      generationByEndpoint.current[endpointId] === generation,
    [],
  );
  const remove = useCallback((endpointId: number) => {
    delete generationByEndpoint.current[endpointId];
  }, []);

  const generation = useMemo(
    () => ({ issue, issueBatch, isCurrent, remove }),
    [issue, issueBatch, isCurrent, remove],
  );
  const summaries = useEndpointReferenceSummaries(uniqueIds, generation);
  const details = useEndpointReferenceDetails(
    uniqueIds,
    generation,
    summaries.replaceFromDetail,
  );
  const {
    addEndpoint: addSummaryEndpoint,
    removeEndpoint: removeSummaryEndpoint,
  } = summaries;
  const {
    addEndpoint: addDetailEndpoint,
    removeEndpoint: removeDetailEndpoint,
    loadDetail,
  } = details;

  const addEndpoint = useCallback(
    (endpointId: number) => {
      addSummaryEndpoint(endpointId);
      addDetailEndpoint(endpointId);
    },
    [addDetailEndpoint, addSummaryEndpoint],
  );
  const removeEndpoint = useCallback(
    (endpointId: number) => {
      removeSummaryEndpoint(endpointId);
      removeDetailEndpoint(endpointId);
    },
    [removeDetailEndpoint, removeSummaryEndpoint],
  );
  const invalidateEndpoint = useCallback(
    (endpointId: number) => {
      void loadDetail(endpointId);
    },
    [loadDetail],
  );

  return {
    addEndpoint,
    adoptDetail: details.adoptDetail,
    details: details.details,
    hasIntegrityError: summaries.hasIntegrityError,
    hasReferenceError: summaries.hasReferenceError,
    hasUnknownOrStale: summaries.hasUnknownOrStale,
    inFlight: summaries.inFlight,
    invalidateEndpoint,
    loadDetail: details.loadDetail,
    loadMore: details.loadMore,
    removeEndpoint,
    retry: summaries.retry,
    summaries: summaries.summaries,
  };
}

export type EndpointReferenceController = ReturnType<
  typeof useEndpointReferences
>;
