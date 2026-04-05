import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import {
  isKnownAllModelsLabel,
  isKnownUnknownEndpointLabel,
  isKnownUnknownProxyApiKeyLabel,
} from "@/i18n/staticMessages";
import { useLocale } from "@/i18n/useLocale";
import type {
  UsageCostOverviewPoint,
  UsageModelStatistic,
  UsageRequestTrendSeries,
  UsageSnapshotResponse,
  UsageStatisticsPageState,
  UsageTokenTrendSeries,
  UsageTokenTypeBreakdownPoint,
} from "@/lib/types";

interface UseUsageStatisticsPageDataParams {
  revision: number;
  selectedProfileId: number | null;
  state: UsageStatisticsPageState;
}

function collectModelLineIds(snapshot: UsageSnapshotResponse | null): string[] {
  if (!snapshot) {
    return [];
  }

  const keys = new Set<string>();

  for (const collection of [
    ...snapshot.request_trends.hourly,
    ...snapshot.request_trends.daily,
    ...snapshot.token_usage_trends.hourly,
    ...snapshot.token_usage_trends.daily,
  ]) {
    if (collection.key !== "all") {
      keys.add(collection.key);
    }
  }

  return [...keys].sort((left, right) => left.localeCompare(right));
}

function resolveSelectedModelLines(available: string[], selected: string[]): string[] {
  const validSelections = available.filter((modelId) => selected.includes(modelId));
  return validSelections.length > 0 ? validSelections : [];
}

function filterSeriesBySelectedModels<T extends { key: string }>(
  series: T[],
  selectedModelLines: string[],
): T[] {
  if (selectedModelLines.length === 0) {
    return series.filter((entry) => entry.key === "all");
  }

  return series.filter(
    (entry) => entry.key === "all" || selectedModelLines.includes(entry.key),
  );
}

export function useUsageStatisticsPageData({
  revision,
  selectedProfileId,
  state,
}: UseUsageStatisticsPageDataParams) {
  const { messages } = useLocale();
  const [snapshot, setSnapshot] = useState<UsageSnapshotResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [endpointModelStatisticsByEndpointId, setEndpointModelStatisticsByEndpointId] =
    useState<Record<number, UsageModelStatistic[]>>({});
  const [endpointModelStatisticsErrors, setEndpointModelStatisticsErrors] = useState<
    Record<number, string>
  >({});
  const [endpointModelStatisticsLoading, setEndpointModelStatisticsLoading] = useState<
    Record<number, boolean>
  >({});
  const [endpointModelStatisticsScopeKey, setEndpointModelStatisticsScopeKey] = useState(0);
  const requestIdRef = useRef(0);
  const endpointModelStatisticsLatestRequestIdRef = useRef<Record<number, number>>({});
  const endpointModelStatisticsRequestSequenceRef = useRef(0);
  const endpointModelStatisticsScopeVersionRef = useRef(0);

  const clearEndpointModelStatistics = useCallback(() => {
    endpointModelStatisticsScopeVersionRef.current += 1;
    setEndpointModelStatisticsScopeKey(endpointModelStatisticsScopeVersionRef.current);
    setEndpointModelStatisticsByEndpointId({});
    setEndpointModelStatisticsErrors({});
    setEndpointModelStatisticsLoading({});
  }, []);

  const fetchSnapshot = useCallback(async () => {
    const requestId = ++requestIdRef.current;
    setLoading(true);
    setError(null);
    clearEndpointModelStatistics();

    try {
      const nextSnapshot = await api.stats.usageSnapshot({ preset: state.selectedTimeRange });

      if (requestId !== requestIdRef.current) {
        return;
      }

      clearEndpointModelStatistics();
      setSnapshot(nextSnapshot);
    } catch (fetchError) {
      if (requestId !== requestIdRef.current) {
        return;
      }

      setError(
        fetchError instanceof Error
          ? fetchError.message
          : messages.statistics.failedToLoadUsageStatistics,
      );
      setSnapshot(null);
    } finally {
      if (requestId === requestIdRef.current) {
        setLoading(false);
      }
    }
  }, [clearEndpointModelStatistics, messages.statistics.failedToLoadUsageStatistics, state.selectedTimeRange]);

  const loadEndpointModelStatistics = useCallback(
    async (endpointId: number) => {
      if (
        endpointModelStatisticsByEndpointId[endpointId] ||
        endpointModelStatisticsLoading[endpointId]
      ) {
        return;
      }

      if (!snapshot) {
        return;
      }

      const requestScopeVersion = endpointModelStatisticsScopeVersionRef.current;
      const requestId = ++endpointModelStatisticsRequestSequenceRef.current;
      endpointModelStatisticsLatestRequestIdRef.current[endpointId] = requestId;
      setEndpointModelStatisticsLoading((current) => ({ ...current, [endpointId]: true }));
      setEndpointModelStatisticsErrors((current) => {
        const next = { ...current };
        delete next[endpointId];
        return next;
      });

      try {
        const items = await api.stats.endpointModelStatistics(endpointId, {
          from_time: snapshot.time_range.start_at ?? undefined,
          to_time: snapshot.time_range.end_at,
        });

        if (
          endpointModelStatisticsScopeVersionRef.current !== requestScopeVersion ||
          endpointModelStatisticsLatestRequestIdRef.current[endpointId] !== requestId
        ) {
          return;
        }

        setEndpointModelStatisticsByEndpointId((current) => ({
          ...current,
          [endpointId]: items,
        }));
      } catch (fetchError) {
        if (
          endpointModelStatisticsScopeVersionRef.current !== requestScopeVersion ||
          endpointModelStatisticsLatestRequestIdRef.current[endpointId] !== requestId
        ) {
          return;
        }

        setEndpointModelStatisticsErrors((current) => ({
          ...current,
          [endpointId]:
            fetchError instanceof Error
              ? fetchError.message
              : messages.statistics.failedToLoadEndpointModelStatistics,
        }));
      } finally {
        if (
          endpointModelStatisticsScopeVersionRef.current === requestScopeVersion &&
          endpointModelStatisticsLatestRequestIdRef.current[endpointId] === requestId
        ) {
          setEndpointModelStatisticsLoading((current) => ({
            ...current,
            [endpointId]: false,
          }));
        }
      }
    },
    [
      endpointModelStatisticsByEndpointId,
      endpointModelStatisticsLoading,
      messages.statistics.failedToLoadEndpointModelStatistics,
      snapshot,
    ],
  );

  const localizedSnapshot = useMemo<UsageSnapshotResponse | null>(() => {
    if (!snapshot) {
      return null;
    }

    const localizeSeriesLabel = (label: string, key: string) => {
      if (isKnownAllModelsLabel(label, key)) {
        return messages.statistics.allModels;
      }
      return label;
    };

    const localizeEndpointLabel = (label: string) => {
      if (isKnownUnknownEndpointLabel(label)) {
        return messages.modelDetail.unknownEndpoint;
      }
      return label;
    };

    const localizeProxyApiKeyLabel = (label: string | null) => {
      if (isKnownUnknownProxyApiKeyLabel(label)) {
        return messages.statistics.unknownProxyApiKey;
      }
      return label ?? messages.statistics.unknownProxyApiKey;
    };

    return {
      ...snapshot,
      endpoint_statistics: snapshot.endpoint_statistics.map((item) => ({
        ...item,
        endpoint_label: localizeEndpointLabel(item.endpoint_label),
      })),
      proxy_api_key_statistics: snapshot.proxy_api_key_statistics.map((item) => ({
        ...item,
        proxy_api_key_label: localizeProxyApiKeyLabel(item.proxy_api_key_label),
      })),
      request_trends: {
        hourly: snapshot.request_trends.hourly.map((series) => ({
          ...series,
          label: localizeSeriesLabel(series.label, series.key),
        })),
        daily: snapshot.request_trends.daily.map((series) => ({
          ...series,
          label: localizeSeriesLabel(series.label, series.key),
        })),
      },
      token_usage_trends: {
        hourly: snapshot.token_usage_trends.hourly.map((series) => ({
          ...series,
          label: localizeSeriesLabel(series.label, series.key),
        })),
        daily: snapshot.token_usage_trends.daily.map((series) => ({
          ...series,
          label: localizeSeriesLabel(series.label, series.key),
        })),
      },
    };
  }, [
    messages.modelDetail.unknownEndpoint,
    messages.statistics.allModels,
    messages.statistics.unknownProxyApiKey,
    snapshot,
  ]);

  useEffect(() => {
    void revision;
    void selectedProfileId;
    void fetchSnapshot();
  }, [fetchSnapshot, revision, selectedProfileId]);

  const refresh = useCallback(async () => {
    await fetchSnapshot();
  }, [fetchSnapshot]);

  const availableModelLineIds = useMemo(() => collectModelLineIds(snapshot), [snapshot]);

  const selectedModelLineIds = useMemo(
    () => resolveSelectedModelLines(availableModelLineIds, state.selectedModelLines),
    [availableModelLineIds, state.selectedModelLines],
  );

  const requestTrendSeries = useMemo<UsageRequestTrendSeries[]>(() => {
    if (!localizedSnapshot) {
      return [];
    }

    return filterSeriesBySelectedModels(
      localizedSnapshot.request_trends[state.chartGranularity.requestTrends],
      selectedModelLineIds,
    );
  }, [localizedSnapshot, selectedModelLineIds, state.chartGranularity.requestTrends]);

  const tokenUsageTrendSeries = useMemo<UsageTokenTrendSeries[]>(() => {
    if (!localizedSnapshot) {
      return [];
    }

    return filterSeriesBySelectedModels(
      localizedSnapshot.token_usage_trends[state.chartGranularity.tokenUsageTrends],
      selectedModelLineIds,
    );
  }, [localizedSnapshot, selectedModelLineIds, state.chartGranularity.tokenUsageTrends]);

  const tokenTypeBreakdown = useMemo<UsageTokenTypeBreakdownPoint[]>(() => {
    if (!localizedSnapshot) {
      return [];
    }

    return localizedSnapshot.token_type_breakdown[state.chartGranularity.tokenTypeBreakdown];
  }, [localizedSnapshot, state.chartGranularity.tokenTypeBreakdown]);

  const costOverviewSeries = useMemo<UsageCostOverviewPoint[]>(() => {
    if (!localizedSnapshot) {
      return [];
    }

    return localizedSnapshot.cost_overview[state.chartGranularity.costOverview];
  }, [localizedSnapshot, state.chartGranularity.costOverview]);

  return {
    availableModelLineIds,
    costOverviewSeries,
    endpointModelStatisticsByEndpointId,
    endpointModelStatisticsErrors,
    endpointModelStatisticsLoading,
    endpointModelStatisticsScopeKey,
    error,
    loadEndpointModelStatistics,
    loading,
    refresh,
    requestTrendSeries,
    selectedModelLineIds,
    snapshot: localizedSnapshot,
    tokenTypeBreakdown,
    tokenUsageTrendSeries,
  };
}
