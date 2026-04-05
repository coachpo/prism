import { useCallback, useEffect, useState } from "react";
import type {
  UsageChartGranularity,
  UsageSnapshotPreset,
  UsageStatisticsChartKey,
  UsageStatisticsPageState,
} from "@/lib/types";
import {
  readUsageStatisticsPageState,
  USAGE_STATISTICS_STORAGE_KEY,
  writeUsageStatisticsPageState,
} from "./usageStatisticsStorage";

export { USAGE_STATISTICS_STORAGE_KEY };

export function useUsageStatisticsPageState() {
  const [state, setState] = useState<UsageStatisticsPageState>(() => readUsageStatisticsPageState());

  useEffect(() => {
    writeUsageStatisticsPageState(state);
  }, [state]);

  const setSelectedTimeRange = useCallback((selectedTimeRange: UsageSnapshotPreset) => {
    setState((current) => ({ ...current, selectedTimeRange }));
  }, []);

  const setSelectedModelLines = useCallback((selectedModelLines: string[]) => {
    setState((current) => ({ ...current, selectedModelLines }));
  }, []);

  const toggleSelectedModelLine = useCallback((modelId: string) => {
    setState((current) => ({
      ...current,
      selectedModelLines: current.selectedModelLines.includes(modelId)
        ? current.selectedModelLines.filter((value) => value !== modelId)
        : [...current.selectedModelLines, modelId],
    }));
  }, []);

  const setChartGranularity = useCallback(
    (key: UsageStatisticsChartKey, granularity: UsageChartGranularity) => {
      setState((current) => ({
        ...current,
        chartGranularity: {
          ...current.chartGranularity,
          [key]: granularity,
        },
      }));
    },
    [],
  );

  return {
    state,
    setChartGranularity,
    setSelectedModelLines,
    setSelectedTimeRange,
    toggleSelectedModelLine,
  };
}

export type UsageStatisticsPageActions = ReturnType<typeof useUsageStatisticsPageState>;
