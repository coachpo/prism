import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { useTimezone } from "@/hooks/useTimezone";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { Endpoint } from "@/lib/types";
import { getSharedEndpoints, setSharedEndpoints } from "@/lib/referenceData";
import { useEndpointReferences } from "./useEndpointReferences";
import { summaryFor } from "./useEndpointReferenceSummaries";

export type ReviewFilter =
  | "all"
  | "referenced"
  | "unreferenced"
  | "inactive_only";

export type EndpointSortKey =
  | "name"
  | "updated_at"
  | "direct_reference_count";

export function useEndpointList() {
  const [endpoints, setEndpoints] = useState<Endpoint[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [endpointLoadError, setEndpointLoadError] = useState(false);
  const [endpointLoadAttempt, setEndpointLoadAttempt] = useState(0);
  const [searchQuery, setSearchQuery] = useState("");
  const [reviewFilter, setReviewFilter] = useState<ReviewFilter>("all");
  const [sortKey, setSortKey] = useState<EndpointSortKey>("name");
  const [sortDescending, setSortDescending] = useState(false);
  const [referenceRollbackNotice, setReferenceRollbackNotice] = useState<
    string | null
  >(null);
  const { format: formatTime } = useTimezone();

  const revision = 0;
  const endpointIds = useMemo(
    () => endpoints.map((endpoint) => endpoint.id),
    [endpoints],
  );
  const references = useEndpointReferences(endpointIds);

  // Initial load from the shared Endpoint cache owner.
  useEffect(() => {
    let cancelled = false;
    setIsLoading(true);
    setEndpointLoadError(false);
    void (async () => {
      try {
        const loaded = await getSharedEndpoints(revision, true);
        if (cancelled) return;
        setEndpoints(loaded);
      } catch {
        if (!cancelled) {
          setEndpointLoadError(true);
          toast.error(getStaticMessages().endpointsData.loadFailed);
        }
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [endpointLoadAttempt, revision]);

  const retryEndpointLoad = useCallback(() => {
    setEndpointLoadAttempt((current) => current + 1);
  }, []);

  const commitEndpoints = useCallback(
    (updater: (current: Endpoint[]) => Endpoint[]) => {
      setEndpoints((current) => {
        const next = updater(current);
        setSharedEndpoints(revision, next);
        return next;
      });
    },
    [revision],
  );

  // Reference-derived controls stay fail-closed until every visible summary
  // is fresh. Text search remains available, while stale/unknown counts are
  // still rendered by the row as evidence rather than treated as zero.
  // 复位本身对操作者是无声的（控件当着他的面弹回默认值），所以同时留下一句
  // 理由，由页面播报出来。
  useEffect(() => {
    if (!references.hasUnknownOrStale) {
      setReferenceRollbackNotice(null);
      return;
    }
    const copy = getStaticMessages().endpointsPage;
    const filterRolledBack = reviewFilter !== "all";
    const sortRolledBack = sortKey === "direct_reference_count";
    if (filterRolledBack) setReviewFilter("all");
    if (sortRolledBack) {
      setSortKey("name");
      setSortDescending(false);
    }
    if (filterRolledBack) {
      setReferenceRollbackNotice(copy.referenceFilterRolledBack);
    } else if (sortRolledBack) {
      setReferenceRollbackNotice(copy.referenceSortRolledBack);
    }
  }, [references.hasUnknownOrStale, reviewFilter, sortKey]);

  const normalizedSearch = searchQuery.trim().toLowerCase();
  const unknownReferenceIds = useMemo(
    () =>
      new Set(
        endpoints
          .filter((endpoint) => {
            const state = references.summaries[endpoint.id];
            return !state || state.status !== "ready";
          })
          .map((endpoint) => endpoint.id),
      ),
    [endpoints, references.summaries],
  );

  const filteredEndpoints = useMemo(() => {
    const effectiveReviewFilter = references.hasUnknownOrStale
      ? "all"
      : reviewFilter;
    return endpoints.filter((endpoint) => {
      const matchesSearch =
        normalizedSearch.length === 0 ||
        endpoint.name.toLowerCase().includes(normalizedSearch) ||
        endpoint.base_url.toLowerCase().includes(normalizedSearch);
      if (!matchesSearch || effectiveReviewFilter === "all") return matchesSearch;

      const value = summaryFor(references.summaries[endpoint.id]);
      if (!value) return true;
      if (effectiveReviewFilter === "referenced") {
        return value.direct_reference_count > 0;
      }
      if (effectiveReviewFilter === "unreferenced") {
        return value.direct_reference_count === 0;
      }
      if (effectiveReviewFilter === "inactive_only") {
        return (
          value.direct_reference_count > 0 &&
          value.enabled_reference_count === 0
        );
      }
      return true;
    });
  }, [
    endpoints,
    normalizedSearch,
    references.hasUnknownOrStale,
    references.summaries,
    reviewFilter,
  ]);

  const sortedEndpoints = useMemo(() => {
    const items = [...filteredEndpoints];
    const effectiveSortKey =
      references.hasUnknownOrStale && sortKey === "direct_reference_count"
        ? "name"
        : sortKey;
    const effectiveSortDescending =
      references.hasUnknownOrStale && sortKey === "direct_reference_count"
        ? false
        : sortDescending;
    const direction = effectiveSortDescending ? -1 : 1;
    items.sort((left, right) => {
      let comparison = 0;
      if (effectiveSortKey === "name") {
        comparison = left.name.localeCompare(right.name, "zh-CN");
        if (comparison === 0) comparison = left.id - right.id;
      } else if (effectiveSortKey === "updated_at") {
        comparison = left.updated_at.localeCompare(right.updated_at);
        if (comparison === 0) comparison = left.id - right.id;
      } else {
        const leftSummary = summaryFor(references.summaries[left.id]);
        const rightSummary = summaryFor(references.summaries[right.id]);
        // Unknown sorts last in whichever direction the operator picked, so
        // it never masquerades as a count of zero.
        if (!leftSummary || !rightSummary) {
          if (leftSummary === rightSummary) {
            return left.name.localeCompare(right.name, "zh-CN");
          }
          return leftSummary ? -1 : 1;
        }
        comparison =
          leftSummary.direct_reference_count -
          rightSummary.direct_reference_count;
        if (comparison === 0) {
          comparison = left.name.localeCompare(right.name, "zh-CN");
        }
      }
      return comparison * direction;
    });
    return items;
  }, [
    filteredEndpoints,
    references.hasUnknownOrStale,
    references.summaries,
    sortDescending,
    sortKey,
  ]);

  const toggleSort = useCallback(
    (key: EndpointSortKey) => {
      if (
        key === "direct_reference_count" &&
        references.hasUnknownOrStale
      ) {
        return;
      }
      if (sortKey === key) {
        setSortDescending((current) => !current);
      } else {
        setSortKey(key);
        setSortDescending(false);
      }
    },
    [references.hasUnknownOrStale, sortKey],
  );

  return {
    commitEndpoints,
    endpointLoadError,
    endpoints,
    filteredEndpoints: sortedEndpoints,
    formatTime,
    isLoading,
    referenceRollbackNotice,
    references,
    retryEndpointLoad,
    reviewFilter,
    searchQuery,
    setReviewFilter,
    setSearchQuery,
    setSortDescending,
    setSortKey,
    sortDescending,
    sortKey,
    toggleSort,
    unknownReferenceIds,
  };
}
