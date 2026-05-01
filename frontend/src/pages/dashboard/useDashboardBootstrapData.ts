import { useCallback, useState } from "react";
import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import { getSharedModels } from "@/lib/referenceData";
import type {
  ModelConfigListItem,
  NonEmptyArray,
  RequestLogListItem,
  RequestLogListResponse,
  SpendingReportResponse,
  StatsSummary,
  ThroughputStatsResponse,
} from "@/lib/types";
import { buildRoutingDiagramData, type RoutingDiagramData } from "./routingDiagram";
import { getEmptyRoutingDiagramData } from "./dashboardDataUtils";

type Params = {
  latestDashboardRequestIdRef: React.RefObject<number>;
  revision: number;
  selectedProfileId: number | null;
};

interface DashboardBootstrapResult {
  modelsData: ModelConfigListItem[];
  apiFamilyStatsData: StatsSummary | null;
  requestsData: RequestLogListResponse | null;
  routingResult: {
    data: RoutingDiagramData;
    error: string | null;
  };
  spendingData: SpendingReportResponse | null;
  statsData: StatsSummary | null;
  throughputData: ThroughputStatsResponse | null;
}

let dashboardBootstrapPromise:
  | {
      key: string;
      promise: Promise<DashboardBootstrapResult>;
    }
  | null = null;

function buildDashboardBootstrapKey(revision: number, selectedProfileId: number | null) {
  return `${selectedProfileId ?? "none"}:${revision}`;
}

function toNonEmptyArray<T>(items: T[]): NonEmptyArray<T> | null {
  const [first, ...rest] = items;
  return first === undefined ? null : [first, ...rest];
}

async function loadOptionalDashboardData<T>(label: string, promise: Promise<T>): Promise<T | null> {
  try {
    return await promise;
  } catch (error) {
    console.warn(`Failed to fetch dashboard ${label}`, error);
    return null;
  }
}

async function loadDashboardBootstrapData(
  revision: number,
  selectedProfileId: number | null,
  options: {
    forceRefresh?: boolean;
    reuseInFlight?: boolean;
  } = {},
): Promise<DashboardBootstrapResult> {
  const { forceRefresh = false, reuseInFlight = false } = options;
  const key = buildDashboardBootstrapKey(revision, selectedProfileId);
  if (reuseInFlight && dashboardBootstrapPromise?.key === key) {
    return dashboardBootstrapPromise.promise;
  }

  const from24h = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString();
  const to24h = new Date().toISOString();
  const modelsPromise = getSharedModels(revision, forceRefresh);

  const loadPromise = Promise.all([
    modelsPromise,
    loadOptionalDashboardData("summary", api.stats.summary({ from_time: from24h })),
    loadOptionalDashboardData(
      "API family summary",
      api.stats.summary({ from_time: from24h, group_by: "api_family" }),
    ),
    loadOptionalDashboardData(
      "spending summary",
      api.stats.spending({ preset: "last_30_days", top_n: 5 }),
    ),
    loadOptionalDashboardData(
      "throughput",
      api.stats.throughput({ from_time: from24h, to_time: to24h }),
    ),
    loadOptionalDashboardData("recent requests", api.stats.requests({ limit: 12 })),
    (async () => {
      try {
        const modelsData = await modelsPromise;
        const modelConfigIds = toNonEmptyArray(modelsData.map((model) => model.id));

        if (!modelConfigIds) {
          return {
            data: getEmptyRoutingDiagramData(),
            error: null,
          };
        }

        const [routeSuccessRates, trafficResult, connectionBatch] = await Promise.all([
          api.stats.connectionSuccessRates({
            from_time: from24h,
            to_time: to24h,
          }),
          api.stats.spending({
            preset: "custom",
            from_time: from24h,
            to_time: to24h,
            group_by: "model_endpoint",
            limit: 500,
          }),
          api.connections.byModels({
            model_config_ids: modelConfigIds,
          }),
        ]);

        const modelsById = new Map(modelsData.map((model) => [model.id, model]));

        return {
          data: buildRoutingDiagramData({
            connectionsByModel: connectionBatch.items.flatMap((item) => {
              const model = modelsById.get(item.model_config_id);
              return model ? [{ model, connections: item.connections }] : [];
            }),
            connectionSuccessRates: routeSuccessRates,
            trafficGroups: trafficResult.groups,
          }),
          error: null,
        };
      } catch (error) {
        console.error("Failed to fetch routing diagram data", error);
        return {
          data: getEmptyRoutingDiagramData(),
          error: getStaticMessages().dashboard.routingDiagramLoadFailed,
        };
      }
    })(),
  ]).then(
    ([modelsData, statsData, apiFamilyStatsData, spendingData, throughputData, requestsData, routingResult]) => ({
      modelsData,
      statsData,
      apiFamilyStatsData,
      spendingData,
      throughputData,
      requestsData,
      routingResult,
    }),
  );

  if (reuseInFlight) {
    dashboardBootstrapPromise = {
      key,
      promise: loadPromise,
    };
    void loadPromise.finally(() => {
      if (dashboardBootstrapPromise?.promise === loadPromise) {
        dashboardBootstrapPromise = null;
      }
    });
  }

  return loadPromise;
}

export function useDashboardBootstrapData({
  latestDashboardRequestIdRef,
  revision,
  selectedProfileId,
}: Params) {
  const [loading, setLoading] = useState(true);
  const [models, setModels] = useState<ModelConfigListItem[]>([]);
  const [stats, setStats] = useState<StatsSummary | null>(null);
  const [apiFamilyStats, setApiFamilyStats] = useState<StatsSummary | null>(null);
  const [spending, setSpending] = useState<SpendingReportResponse | null>(null);
  const [throughput, setThroughput] = useState<ThroughputStatsResponse | null>(null);
  const [recentRequests, setRecentRequests] = useState<RequestLogListItem[]>([]);
  const [routingDiagramData, setRoutingDiagramData] = useState<RoutingDiagramData | null>(null);
  const [routingDiagramLoading, setRoutingDiagramLoading] = useState(true);
  const [routingDiagramError, setRoutingDiagramError] = useState<string | null>(null);
  const requestVersionRef = useState(() => ({ current: 0 }))[0];

  const fetchDashboardData = useCallback(
    async ({
      silent = false,
      forceRefresh = false,
      reuseInFlight = false,
    }: {
      silent?: boolean;
      forceRefresh?: boolean;
      reuseInFlight?: boolean;
    } = {}) => {
      const requestVersion = ++requestVersionRef.current;

      if (!silent) {
        setLoading(true);
        setRoutingDiagramLoading(true);
      }

      setRoutingDiagramError(null);
      try {
        const {
          modelsData,
          statsData,
          apiFamilyStatsData,
          spendingData,
          throughputData,
          requestsData,
          routingResult,
        } = await loadDashboardBootstrapData(
          revision,
          selectedProfileId,
          {
            forceRefresh,
            reuseInFlight,
          },
        );

        if (requestVersion !== requestVersionRef.current) {
          return;
        }

        setModels(modelsData);

        let latestFetchedRequestId: number | null = null;

        if (requestsData) {
          latestFetchedRequestId = requestsData.items.reduce(
            (maxId, request) => Math.max(maxId, request.id),
            0,
          );

          if (latestFetchedRequestId < latestDashboardRequestIdRef.current) {
            return;
          }
        }

        if (statsData) {
          setStats(statsData);
        }
        if (apiFamilyStatsData) {
          setApiFamilyStats(apiFamilyStatsData);
        }
        if (spendingData) {
          setSpending(spendingData);
        }
        if (throughputData) {
          setThroughput(throughputData);
        }
        if (requestsData) {
          setRecentRequests(requestsData.items);
          latestDashboardRequestIdRef.current = latestFetchedRequestId ?? 0;
        }
        setRoutingDiagramData(routingResult.data);
        setRoutingDiagramError(routingResult.error);
      } catch (error) {
        console.error("Failed to fetch dashboard data", error);
      } finally {
        if (requestVersion === requestVersionRef.current) {
          setLoading(false);
          setRoutingDiagramLoading(false);
        }
      }
    },
    [latestDashboardRequestIdRef, requestVersionRef, revision, selectedProfileId]
  );

  return {
    fetchDashboardData,
    loading,
    models,
    apiFamilyStats,
    recentRequests,
    routingDiagramData,
    routingDiagramError,
    routingDiagramLoading,
    setApiFamilyStats,
    setRecentRequests,
    setRoutingDiagramData,
    setRoutingDiagramError,
    setSpending,
    setStats,
    setThroughput,
    spending,
    stats,
    throughput,
  };
}
