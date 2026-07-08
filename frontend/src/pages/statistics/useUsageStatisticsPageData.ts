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
  UsageSnapshotPreset,
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
  derivedCachedTokens: number;
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

interface AcceptedAnalyticsSnapshotMeta {
  generatedAtMs: number;
  preset: UsageSnapshotPreset;
  profileId: number;
  sequence: number;
}

interface AnalyticsSnapshotSource {
  endpointModelStatisticsByEndpointId: Record<number, UsageModelStatistic[]>;
  generatedAt: string;
  profileId: number;
  sequence: number;
  snapshot: UsageSnapshotResponse;
}

const EMPTY_ENDPOINT_MODEL_STATISTICS_BY_ENDPOINT_ID: Record<number, UsageModelStatistic[]> = {};
const EMPTY_ENDPOINT_MODEL_STATISTICS_ERRORS: Record<number, string> = {};
const EMPTY_ENDPOINT_MODEL_STATISTICS_LOADING: Record<number, boolean> = {};
const USAGE_STATISTICS_POLL_INTERVAL_MS = 30_000;

function collectRegisteredModelLineIds(modelIds: string[]): string[] {
  return [...new Set(modelIds.map((modelId) => modelId.trim()).filter((modelId) => modelId.length > 0))]
    .sort((left, right) => left.localeCompare(right));
}

function resolveSelectedModelLines(available: string[], selected: string[]): string[] {
  const validSelections = available.filter((modelId) => selected.includes(modelId));
  return validSelections.length > 0 ? validSelections : [];
}

function getGeneratedAtMs(generatedAt: string): number {
  const generatedAtMs = Date.parse(generatedAt);
  return Number.isFinite(generatedAtMs) ? generatedAtMs : 0;
}

export function normalizeAnalyticsSnapshotProfileId(): number {
  return 1;
}

function buildRestSnapshotSource({
  profileId,
  snapshot,
}: {
  profileId: number;
  snapshot: UsageSnapshotResponse;
}): AnalyticsSnapshotSource {
  return {
    endpointModelStatisticsByEndpointId: {},
    generatedAt: snapshot.generated_at,
    profileId,
    sequence: 0,
    snapshot,
  };
}

export function isStaleAnalyticsSnapshot(
  current: AcceptedAnalyticsSnapshotMeta | null,
  next: AcceptedAnalyticsSnapshotMeta,
): boolean {
  if (!current) {
    return false;
  }

  if (next.profileId !== current.profileId || next.preset !== current.preset) {
    return false;
  }

  return next.sequence < current.sequence ||
    (next.sequence === current.sequence && next.generatedAtMs < current.generatedAtMs);
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
      totals.derivedCachedTokens += item.cached_tokens ?? 0;
      totals.reasoningTokens += item.reasoning_tokens ?? 0;
      totals.totalCostMicros += item.total_cost_micros;
      return totals;
    },
    {
      derivedCachedTokens: 0,
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
    cached_tokens: totals.derivedCachedTokens,
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

export function useUsageStatisticsPageData({
  revision,
  selectedProfileId,
  state,
}: UseUsageStatisticsPageDataParams) {
  const { messages } = useLocale();
  const [snapshotState, setSnapshotState] = useState<{
    scopeKey: string;
    snapshot: UsageSnapshotResponse;
  } | null>(null);
  const [errorState, setErrorState] = useState<{
    message: string;
    scopeKey: string;
  } | null>(null);
  const [modelsRevision, setModelsRevision] = useState(revision);
  const [availableModelLineIds, setAvailableModelLineIds] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [endpointModelStatisticsByEndpointId, setEndpointModelStatisticsByEndpointId] =
    useState<Record<number, UsageModelStatistic[]>>({});
  const [endpointModelStatisticsErrors, setEndpointModelStatisticsErrors] = useState<
    Record<number, string>
  >({});
  const [endpointModelStatisticsLoading, setEndpointModelStatisticsLoading] = useState<
    Record<number, boolean>
  >({});
  const snapshotScopeKey = `${selectedProfileId ?? "none"}:${state.selectedTimeRange}`;
  const activeScopeKey = `${snapshotScopeKey}:${revision}`;
  const acceptedSnapshotMetaRef = useRef<AcceptedAnalyticsSnapshotMeta | null>(null);

  const acceptSnapshotSource = useCallback(
    (source: AnalyticsSnapshotSource) => {
      const profileId = normalizeAnalyticsSnapshotProfileId();

      const nextMeta: AcceptedAnalyticsSnapshotMeta = {
        generatedAtMs: getGeneratedAtMs(source.generatedAt),
        preset: state.selectedTimeRange,
        profileId,
        sequence: source.sequence,
      };

      if (isStaleAnalyticsSnapshot(acceptedSnapshotMetaRef.current, nextMeta)) {
        return;
      }

      acceptedSnapshotMetaRef.current = nextMeta;
      setSnapshotState({ scopeKey: activeScopeKey, snapshot: source.snapshot });
      setEndpointModelStatisticsByEndpointId(source.endpointModelStatisticsByEndpointId);
      setEndpointModelStatisticsErrors({});
      setEndpointModelStatisticsLoading({});
      setErrorState(null);
      setLoading(false);
    },
    [activeScopeKey, state.selectedTimeRange],
  );

  useEffect(() => {
    acceptedSnapshotMetaRef.current = null;
    setEndpointModelStatisticsByEndpointId({});
    setEndpointModelStatisticsErrors({});
    setEndpointModelStatisticsLoading({});
  }, [snapshotScopeKey]);

  const fetchUsageSnapshot = useCallback(
    async ({ silent = false }: { silent?: boolean } = {}) => {
      if (selectedProfileId === null) {
        setLoading(false);
        return;
      }

      if (!silent) {
        setLoading(true);
      }
      setErrorState(null);

      try {
        const snapshot = await api.stats.usageSnapshot({ preset: state.selectedTimeRange });
        acceptSnapshotSource(buildRestSnapshotSource({ profileId: selectedProfileId, snapshot }));
      } catch (error) {
        if (!silent) {
          setErrorState({
            message: error instanceof Error ? error.message : messages.statistics.failedToLoadUsageStatistics,
            scopeKey: activeScopeKey,
          });
          setLoading(false);
        }
      }
    },
    [acceptSnapshotSource, activeScopeKey, messages.statistics.failedToLoadUsageStatistics, selectedProfileId, state.selectedTimeRange],
  );

  useEffect(() => {
    let active = true;

    if (selectedProfileId === null) {
      setLoading(false);
      return () => {
        active = false;
      };
    }

    void fetchUsageSnapshot();
    const intervalID = window.setInterval(() => {
      if (active) {
        void fetchUsageSnapshot({ silent: true });
      }
    }, USAGE_STATISTICS_POLL_INTERVAL_MS);

    return () => {
      active = false;
      window.clearInterval(intervalID);
    };
  }, [fetchUsageSnapshot, selectedProfileId]);

  const loadEndpointModelStatistics = useCallback(
    async (endpointId: number) => {
      if (
        selectedProfileId === null ||
        endpointModelStatisticsByEndpointId[endpointId] ||
        endpointModelStatisticsLoading[endpointId]
      ) {
        return;
      }

      setEndpointModelStatisticsLoading((current) => ({ ...current, [endpointId]: true }));
      setEndpointModelStatisticsErrors((current) => {
        const next = { ...current };
        delete next[endpointId];
        return next;
      });

      try {
        const items = await api.stats.endpointModelStatistics(endpointId, {
          preset: state.selectedTimeRange,
        });
        setEndpointModelStatisticsByEndpointId((current) => ({
          ...current,
          [endpointId]: items,
        }));
      } catch (error) {
        setEndpointModelStatisticsErrors((current) => ({
          ...current,
          [endpointId]: error instanceof Error ? error.message : messages.statistics.failedToLoadEndpointModelStatistics,
        }));
      } finally {
        setEndpointModelStatisticsLoading((current) => ({ ...current, [endpointId]: false }));
      }
    },
    [endpointModelStatisticsByEndpointId, endpointModelStatisticsLoading, messages.statistics.failedToLoadEndpointModelStatistics, selectedProfileId, state.selectedTimeRange],
  );

  const snapshot = snapshotState?.scopeKey === activeScopeKey ? snapshotState.snapshot : null;
  const activeEndpointModelStatisticsByEndpointId = snapshot
    ? endpointModelStatisticsByEndpointId
    : EMPTY_ENDPOINT_MODEL_STATISTICS_BY_ENDPOINT_ID;
  const activeEndpointModelStatisticsErrors = snapshot
    ? endpointModelStatisticsErrors
    : EMPTY_ENDPOINT_MODEL_STATISTICS_ERRORS;
  const activeEndpointModelStatisticsLoading = snapshot
    ? endpointModelStatisticsLoading
    : EMPTY_ENDPOINT_MODEL_STATISTICS_LOADING;
  const activeEndpointModelStatisticsScopeKey = snapshot
    ? `${activeScopeKey}:${snapshot.generated_at}`
    : activeScopeKey;
  const error = errorState?.scopeKey === activeScopeKey ? errorState.message : null;
  const isLoading = selectedProfileId !== null && error === null && (loading || !snapshot);

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
    let active = true;

    void getSharedModels(revision)
      .then((models) => {
        if (!active) {
          return;
        }

        setModelsRevision(revision);
        setAvailableModelLineIds(
          collectRegisteredModelLineIds(models.map((model) => model.model_id)),
        );
      })
      .catch(() => {
        if (active) {
          setModelsRevision(revision);
          setAvailableModelLineIds([]);
        }
      });

    return () => {
      active = false;
    };
  }, [revision]);

  const refresh = useCallback(async () => {
    await fetchUsageSnapshot();
  }, [fetchUsageSnapshot]);

  const selectedModelLineIds = useMemo(
    () =>
      modelsRevision === revision
        ? resolveSelectedModelLines(availableModelLineIds, state.selectedModelLines)
        : [],
    [availableModelLineIds, modelsRevision, revision, state.selectedModelLines],
  );

  useEffect(() => {
    if (!snapshot || selectedModelLineIds.length === 0) {
      return;
    }

    for (const item of snapshot.endpoint_statistics) {
      if (item.endpoint_id !== null) {
        void loadEndpointModelStatistics(item.endpoint_id);
      }
    }
  }, [loadEndpointModelStatistics, selectedModelLineIds.length, snapshot]);

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

  return {
    availableModelLineIds,
    costSummary,
    costOverviewSeries,
    endpointModelStatisticsByEndpointId: activeEndpointModelStatisticsByEndpointId,
    endpointModelStatisticsErrors: activeEndpointModelStatisticsErrors,
    endpointModelStatisticsLoading: activeEndpointModelStatisticsLoading,
    endpointModelStatisticsScopeKey: activeEndpointModelStatisticsScopeKey,
    error,
    loadEndpointModelStatistics,
    loading: isLoading,
    modelStatistics,
    overview,
    refresh,
    requestTrendSeries,
    selectedModelLineIds,
    snapshot: localizedSnapshot,
    tokenTypeBreakdown,
    tokenUsageTrendSeries,
  };
}

export type UsageStatisticsPageDataResult = ReturnType<typeof useUsageStatisticsPageData>;
