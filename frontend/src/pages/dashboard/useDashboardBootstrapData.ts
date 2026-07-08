import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import type {
  DashboardRecentActivityItem,
  DashboardRecentActivityResponse,
  DashboardRecentActivityWatermark,
  DashboardSnapshot,
} from "@/lib/types";

export const DASHBOARD_RECENT_ACTIVITY_LIMIT = 12;

type Params = {
  revision: number;
  selectedProfileId: number | null;
};

export type DashboardSnapshotReconciler = (snapshot: DashboardSnapshot) => boolean;

export type DashboardActivityReconciler = (
  activity: DashboardRecentActivityItem,
  activityWatermark: DashboardRecentActivityWatermark,
) => boolean;

export type DashboardBootstrapFetchResult = {
  newRecentActivityIds: number[];
  recentActivityApplied: boolean;
  snapshotApplied: boolean;
};
type DashboardBootstrapData = {
  recentActivity: DashboardRecentActivityResponse;
  snapshot: DashboardSnapshot;
};

let dashboardBootstrapPromise:
  | {
      key: string;
      promise: Promise<DashboardBootstrapData>;
    }
  | null = null;

function buildDashboardBootstrapKey(revision: number, selectedProfileId: number | null) {
  return `${selectedProfileId ?? "none"}:${revision}`;
}

export function shouldApplyDashboardSnapshotRevision(
  currentRevision: string | null,
  incomingRevision: string,
) {
  if (currentRevision && incomingRevision <= currentRevision) {
    return false;
  }

  return true;
}
export function mergeDashboardRecentActivityItem(
  current: DashboardRecentActivityResponse | null,
  activity: DashboardRecentActivityItem,
  activityWatermark: DashboardRecentActivityWatermark,
  limit = DASHBOARD_RECENT_ACTIVITY_LIMIT,
): DashboardRecentActivityResponse {
  if (current?.items.some((item) => item.request_log_id === activity.request_log_id)) {
    return current;
  }

  return {
    generated_at: current?.generated_at ?? activity.created_at,
    activity_watermark: activityWatermark,
    items: [activity, ...(current?.items ?? [])].slice(0, limit),
  };
}

async function loadDashboardBootstrapData(
  revision: number,
  selectedProfileId: number | null,
  options: {
    reuseInFlight?: boolean;
  } = {},
): Promise<DashboardBootstrapData> {
  const { reuseInFlight = false } = options;
  const key = buildDashboardBootstrapKey(revision, selectedProfileId);
  if (reuseInFlight && dashboardBootstrapPromise?.key === key) {
    return dashboardBootstrapPromise.promise;
  }

  const loadPromise = Promise.all([
    api.stats.dashboard(),
    api.stats.dashboardRecentActivity({ limit: DASHBOARD_RECENT_ACTIVITY_LIMIT }),
  ]).then(([snapshot, recentActivity]) => ({ snapshot, recentActivity }));

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
  revision,
  selectedProfileId,
}: Params) {
  const [loading, setLoading] = useState(true);
  const [dashboardSnapshot, setDashboardSnapshot] = useState<DashboardSnapshot | null>(null);
  const [dashboardRecentActivity, setDashboardRecentActivity] =
    useState<DashboardRecentActivityResponse | null>(null);
  const [routingDiagramError, setRoutingDiagramError] = useState<string | null>(null);
  const latestDashboardSnapshotRevisionRef = useRef<string | null>(null);
  const recentActivityRequestIdsRef = useRef<Set<number>>(new Set());
  const requestVersionRef = useRef(0);

  const replaceDashboardRecentActivity = useCallback((recentActivity: DashboardRecentActivityResponse) => {
    recentActivityRequestIdsRef.current = new Set(
      recentActivity.items.map((item) => item.request_log_id),
    );
    setDashboardRecentActivity(recentActivity);
  }, []);
  const reconcileDashboardSnapshot = useCallback<DashboardSnapshotReconciler>(
    (snapshot) => {
      const incomingRevision = snapshot.snapshot_revision;
      const currentRevision = latestDashboardSnapshotRevisionRef.current;
      if (!shouldApplyDashboardSnapshotRevision(currentRevision, incomingRevision)) {
        return false;
      }

      latestDashboardSnapshotRevisionRef.current = incomingRevision;
      setDashboardSnapshot(snapshot);
      return true;
    },
    [],
  );

  const applyDashboardActivity = useCallback<DashboardActivityReconciler>(
    (activity, activityWatermark) => {
      if (recentActivityRequestIdsRef.current.has(activity.request_log_id)) {
        return false;
      }

      recentActivityRequestIdsRef.current.add(activity.request_log_id);
      setDashboardRecentActivity((current) =>
        mergeDashboardRecentActivityItem(current, activity, activityWatermark),
      );
      return true;
    },
    [],
  );
  useEffect(() => {
    latestDashboardSnapshotRevisionRef.current = null;
    recentActivityRequestIdsRef.current = new Set();
    setDashboardSnapshot(null);
    setDashboardRecentActivity(null);
  }, [selectedProfileId]);

  const fetchDashboardData = useCallback(
    async ({
      silent = false,
      reuseInFlight = false,
    }: {
      silent?: boolean;
      reuseInFlight?: boolean;
    } = {}): Promise<DashboardBootstrapFetchResult> => {
      const requestVersion = ++requestVersionRef.current;

      if (!silent) {
        setLoading(true);
      }

      setRoutingDiagramError(null);
      try {
        const { snapshot, recentActivity } = await loadDashboardBootstrapData(
          revision,
          selectedProfileId,
          { reuseInFlight },
        );

        if (requestVersion !== requestVersionRef.current) {
          return { newRecentActivityIds: [], recentActivityApplied: false, snapshotApplied: false };
        }

        const newRecentActivityIds = recentActivity.items
          .filter((item) => !recentActivityRequestIdsRef.current.has(item.request_log_id))
          .map((item) => item.request_log_id);
        const snapshotApplied = reconcileDashboardSnapshot(snapshot);
        replaceDashboardRecentActivity(recentActivity);
        return { newRecentActivityIds, recentActivityApplied: true, snapshotApplied };
      } catch (error) {
        console.error("Failed to fetch dashboard data", error);
        return { newRecentActivityIds: [], recentActivityApplied: false, snapshotApplied: false };
      } finally {
        if (requestVersion === requestVersionRef.current) {
          setLoading(false);
        }
      }
    },
    [reconcileDashboardSnapshot, replaceDashboardRecentActivity, revision, selectedProfileId],
  );
  return {
    applyDashboardActivity,
    dashboardRecentActivity,
    dashboardRecentActivityItems: dashboardRecentActivity?.items ?? [],
    dashboardSnapshot,
    fetchDashboardData,
    loading,
    reconcileDashboardSnapshot,
    routingDiagramError,
    routingDiagramLoading: loading,
    setRoutingDiagramError,
  };
}
