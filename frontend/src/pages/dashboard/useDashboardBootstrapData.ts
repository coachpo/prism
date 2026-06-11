import { useCallback, useRef, useState } from "react";
import { api } from "@/lib/api";
import type { DashboardSnapshot } from "@/lib/types";

type Params = {
  latestDashboardRequestIdRef: React.RefObject<number>;
  revision: number;
  selectedProfileId: number | null;
};

export type DashboardSnapshotReconcileOptions = {
  allowEqualRequestId?: boolean;
  requestId?: number;
};

export type DashboardSnapshotReconciler = (
  snapshot: DashboardSnapshot,
  options?: DashboardSnapshotReconcileOptions,
) => boolean;

let dashboardBootstrapPromise:
  | {
      key: string;
      promise: Promise<DashboardSnapshot>;
    }
  | null = null;

function buildDashboardBootstrapKey(revision: number, selectedProfileId: number | null) {
  return `${selectedProfileId ?? "none"}:${revision}`;
}

function getLatestDashboardSnapshotRequestId(snapshot: DashboardSnapshot) {
  return snapshot.recent_requests.reduce(
    (maxId, request) => Math.max(maxId, request.id),
    0,
  );
}

async function loadDashboardBootstrapData(
  revision: number,
  selectedProfileId: number | null,
  options: {
    reuseInFlight?: boolean;
  } = {},
): Promise<DashboardSnapshot> {
  const { reuseInFlight = false } = options;
  const key = buildDashboardBootstrapKey(revision, selectedProfileId);
  if (reuseInFlight && dashboardBootstrapPromise?.key === key) {
    return dashboardBootstrapPromise.promise;
  }

  const loadPromise = api.stats.dashboard();

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
  const [dashboardSnapshot, setDashboardSnapshot] = useState<DashboardSnapshot | null>(null);
  const [dashboardError, setDashboardError] = useState<string | null>(null);
  const [routingDiagramError, setRoutingDiagramError] = useState<string | null>(null);
  const requestVersionRef = useRef(0);

  const reconcileDashboardSnapshot = useCallback<DashboardSnapshotReconciler>(
    (snapshot, { allowEqualRequestId = false, requestId } = {}) => {
      const nextRequestId = requestId ?? getLatestDashboardSnapshotRequestId(snapshot);
      const currentRequestId = latestDashboardRequestIdRef.current;
      const isStale = allowEqualRequestId
        ? nextRequestId < currentRequestId
        : nextRequestId <= currentRequestId;

      if (isStale) {
        return false;
      }

      latestDashboardRequestIdRef.current = nextRequestId;
      setDashboardSnapshot(snapshot);
      return true;
    },
    [latestDashboardRequestIdRef],
  );

  const fetchDashboardData = useCallback(
    async ({
      silent = false,
      reuseInFlight = false,
    }: {
      silent?: boolean;
      reuseInFlight?: boolean;
    } = {}) => {
      const requestVersion = ++requestVersionRef.current;

      if (!silent) {
        setLoading(true);
      }

      setDashboardError(null);
      setRoutingDiagramError(null);
      try {
        const snapshot = await loadDashboardBootstrapData(
          revision,
          selectedProfileId,
          { reuseInFlight },
        );

        if (requestVersion !== requestVersionRef.current) {
          return;
        }

        reconcileDashboardSnapshot(snapshot, {
          allowEqualRequestId: true,
          requestId: getLatestDashboardSnapshotRequestId(snapshot),
        });
      } catch (error) {
        console.error("Failed to fetch dashboard data", error);
        if (requestVersion === requestVersionRef.current) {
          setDashboardError(error instanceof Error ? error.message : "Failed to fetch dashboard data");
        }
      } finally {
        if (requestVersion === requestVersionRef.current) {
          setLoading(false);
        }
      }
    },
    [reconcileDashboardSnapshot, revision, selectedProfileId]
  );

  return {
    dashboardError,
    dashboardSnapshot,
    fetchDashboardData,
    loading,
    reconcileDashboardSnapshot,
    routingDiagramError,
    routingDiagramLoading: loading,
    setRoutingDiagramError,
  };
}
