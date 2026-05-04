import { useMemo } from "react";
import { useRealtimeData } from "@/hooks/useRealtimeData";
import type { AnalyticsRealtimeSnapshotPayload } from "@/lib/websocket";
import type { UsageSnapshotPreset } from "@/lib/types";

interface UseUsageStatisticsRealtimeDataParams {
  onSnapshot: (payload: AnalyticsRealtimeSnapshotPayload) => void;
  preset: UsageSnapshotPreset;
  selectedProfileId: number | null;
}

export function useUsageStatisticsRealtimeData({
  onSnapshot,
  preset,
  selectedProfileId,
}: UseUsageStatisticsRealtimeDataParams) {
  const scope = useMemo(() => ({ preset }), [preset]);

  return useRealtimeData({
    channel: "analytics",
    enabled: selectedProfileId !== null,
    onData: onSnapshot,
    profileId: selectedProfileId,
    scope,
  });
}
