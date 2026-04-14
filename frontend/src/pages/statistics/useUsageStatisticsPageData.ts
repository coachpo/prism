import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getSharedModels } from "@/lib/referenceData";
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

interface UsageModelAggregateTotals {
  cachedTokens: number;
  failedCount: number;
  inputTokens: number;
  outputTokens: number;
  pricedRequestCount: number;
  reasoningTokens: number;
  requestCount: number;
  successCount: number;
  totalCostMicros: number;
  totalTokens: number;
  unpricedRequestCount: number;
}

interface UsageSelectedCostSummary {
  priced_request_count: number;
  total_cost_micros: number;
  unpriced_request_count: number;
}

interface UsageTopEndpointSpendStatistic {
  endpoint_label: string;
  total_cost_micros: number;
}

function collectRegisteredModelLineIds(modelIds: string[]): string[] {
  return [...new Set(modelIds.map((modelId) => modelId.trim()).filter((modelId) => modelId.length > 0))]
    .sort((left, right) => left.localeCompare(right));
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

function filterModelStatisticsBySelectedModels(
  modelStatistics: UsageModelStatistic[],
  selectedModelLines: string[],
): UsageModelStatistic[] {
  if (selectedModelLines.length === 0) {
    return modelStatistics;
  }

  return modelStatistics.filter((item) => selectedModelLines.includes(item.model_id));
}

function sumModelStatistics(modelStatistics: UsageModelStatistic[]): UsageModelAggregateTotals {
  return modelStatistics.reduce<UsageModelAggregateTotals>(
    (totals, item) => {
      const successCount =
        item.success_count ?? Math.max(0, Math.round((item.success_rate / 100) * item.request_count));
      const failedCount = item.failed_count ?? Math.max(0, item.request_count - successCount);

      totals.requestCount += item.request_count;
      totals.successCount += successCount;
      totals.failedCount += failedCount;
      totals.pricedRequestCount += item.priced_request_count ?? 0;
      totals.unpricedRequestCount += item.unpriced_request_count ?? 0;
      totals.totalTokens += item.total_tokens;
      totals.inputTokens += item.input_tokens ?? 0;
      totals.outputTokens += item.output_tokens ?? 0;
      totals.cachedTokens += item.cached_tokens ?? 0;
      totals.reasoningTokens += item.reasoning_tokens ?? 0;
      totals.totalCostMicros += item.total_cost_micros;
      return totals;
    },
    {
      cachedTokens: 0,
      failedCount: 0,
      inputTokens: 0,
      outputTokens: 0,
      pricedRequestCount: 0,
      reasoningTokens: 0,
      requestCount: 0,
      successCount: 0,
      totalCostMicros: 0,
      totalTokens: 0,
      unpricedRequestCount: 0,
    },
  );
}

function scaleAverageMetric(selectedValue: number, globalValue: number, globalAverage: number) {
  if (globalValue <= 0) {
    return 0;
  }

  return (selectedValue / globalValue) * globalAverage;
}

function deriveSelectedOverview(
  overview: UsageSnapshotResponse["overview"],
  modelStatistics: UsageModelStatistic[],
  hasSelectedModels: boolean,
): UsageSnapshotResponse["overview"] {
  if (!hasSelectedModels) {
    return overview;
  }

  const totals = sumModelStatistics(modelStatistics);

  return {
    ...overview,
    average_rpm: scaleAverageMetric(
      totals.requestCount,
      overview.total_requests,
      overview.average_rpm,
    ),
    average_tpm: scaleAverageMetric(
      totals.totalTokens,
      overview.total_tokens,
      overview.average_tpm,
    ),
    cached_tokens: totals.cachedTokens,
    failed_requests: totals.failedCount,
    input_tokens: totals.inputTokens,
    output_tokens: totals.outputTokens,
    reasoning_tokens: totals.reasoningTokens,
    rolling_request_count: undefined,
    rolling_rpm: undefined,
    rolling_token_count: undefined,
    rolling_tpm: undefined,
    rolling_window_minutes: undefined,
    success_rate:
      totals.requestCount > 0 ? (totals.successCount / totals.requestCount) * 100 : 0,
    success_requests: totals.successCount,
    total_cost_micros: totals.totalCostMicros,
    total_requests: totals.requestCount,
    total_tokens: totals.totalTokens,
  };
}

function deriveSelectedCostSummary(
  costOverview: UsageSnapshotResponse["cost_overview"],
  modelStatistics: UsageModelStatistic[],
  hasSelectedModels: boolean,
): UsageSelectedCostSummary {
  if (!hasSelectedModels) {
    return costOverview;
  }

  const totals = sumModelStatistics(modelStatistics);

  return {
    priced_request_count: totals.pricedRequestCount,
    total_cost_micros: totals.totalCostMicros,
    unpriced_request_count: totals.unpricedRequestCount,
  };
}

function deriveTopEndpointSpendStatistics(
  endpointStatistics: UsageSnapshotResponse["endpoint_statistics"],
  endpointModelStatisticsByEndpointId: Record<number, UsageModelStatistic[]>,
  selectedModelLineIds: string[],
): UsageTopEndpointSpendStatistic[] {
  if (selectedModelLineIds.length === 0) {
    return endpointStatistics.map((item) => ({
      endpoint_label: item.endpoint_label,
      total_cost_micros: item.total_cost_micros,
    }));
  }

  return endpointStatistics.flatMap((item) => {
    if (item.endpoint_id == null) {
      return [];
    }

    const endpointModelStatistics = endpointModelStatisticsByEndpointId[item.endpoint_id];
    if (!endpointModelStatistics) {
      return [];
    }

    const totalCostMicros = endpointModelStatistics
      .filter((modelStatistic) => selectedModelLineIds.includes(modelStatistic.model_id))
      .reduce((sum, modelStatistic) => sum + modelStatistic.total_cost_micros, 0);

    if (totalCostMicros <= 0) {
      return [];
    }

    return [
      {
        endpoint_label: item.endpoint_label,
        total_cost_micros: totalCostMicros,
      },
    ];
  });
}

export function useUsageStatisticsPageData({
  revision,
  selectedProfileId,
  state,
}: UseUsageStatisticsPageDataParams) {
  const { messages } = useLocale();
  const [snapshot, setSnapshot] = useState<UsageSnapshotResponse | null>(null);
  const [availableModelLineIds, setAvailableModelLineIds] = useState<string[]>([]);
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

  useEffect(() => {
    let active = true;

    setAvailableModelLineIds([]);

    void getSharedModels(revision)
      .then((models) => {
        if (!active) {
          return;
        }

        setAvailableModelLineIds(
          collectRegisteredModelLineIds(models.map((model) => model.model_id)),
        );
      })
      .catch(() => {
        if (active) {
          setAvailableModelLineIds([]);
        }
      });

    return () => {
      active = false;
    };
  }, [revision]);

  const refresh = useCallback(async () => {
    await fetchSnapshot();
  }, [fetchSnapshot]);

  const selectedModelLineIds = useMemo(
    () => resolveSelectedModelLines(availableModelLineIds, state.selectedModelLines),
    [availableModelLineIds, state.selectedModelLines],
  );

  const hasSelectedModels = selectedModelLineIds.length > 0;

  const modelStatistics = useMemo<UsageModelStatistic[]>(() => {
    if (!localizedSnapshot) {
      return [];
    }

    return filterModelStatisticsBySelectedModels(
      localizedSnapshot.model_statistics,
      selectedModelLineIds,
    );
  }, [localizedSnapshot, selectedModelLineIds]);

  useEffect(() => {
    if (!localizedSnapshot) {
      return;
    }

    for (const item of localizedSnapshot.endpoint_statistics) {
      if (item.endpoint_id == null) {
        continue;
      }

      void loadEndpointModelStatistics(item.endpoint_id);
    }
  }, [loadEndpointModelStatistics, localizedSnapshot]);

  const overview = useMemo(() => {
    if (!localizedSnapshot) {
      return null;
    }

    return deriveSelectedOverview(localizedSnapshot.overview, modelStatistics, hasSelectedModels);
  }, [hasSelectedModels, localizedSnapshot, modelStatistics]);

  const costSummary = useMemo(() => {
    if (!localizedSnapshot) {
      return null;
    }

    return deriveSelectedCostSummary(
      localizedSnapshot.cost_overview,
      modelStatistics,
      hasSelectedModels,
    );
  }, [hasSelectedModels, localizedSnapshot, modelStatistics]);

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

  const topEndpointSpendStatistics = useMemo<UsageTopEndpointSpendStatistic[]>(() => {
    if (!localizedSnapshot) {
      return [];
    }

    return deriveTopEndpointSpendStatistics(
      localizedSnapshot.endpoint_statistics,
      endpointModelStatisticsByEndpointId,
      selectedModelLineIds,
    );
  }, [
    endpointModelStatisticsByEndpointId,
    localizedSnapshot,
    selectedModelLineIds,
  ]);

  return {
    availableModelLineIds,
    costSummary,
    costOverviewSeries,
    endpointModelStatisticsByEndpointId,
    endpointModelStatisticsErrors,
    endpointModelStatisticsLoading,
    endpointModelStatisticsScopeKey,
    error,
    loadEndpointModelStatistics,
    loading,
    modelStatistics,
    overview,
    refresh,
    requestTrendSeries,
    selectedModelLineIds,
    snapshot: localizedSnapshot,
    topEndpointSpendStatistics,
    tokenTypeBreakdown,
    tokenUsageTrendSeries,
  };
}

export type UsageStatisticsPageDataResult = ReturnType<typeof useUsageStatisticsPageData>;
