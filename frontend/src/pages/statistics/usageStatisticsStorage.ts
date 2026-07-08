import {
  USAGE_CHART_GRANULARITIES,
  USAGE_SNAPSHOT_PRESETS,
  type UsageChartGranularity,
  type UsageSnapshotPreset,
  type UsageStatisticsChartGranularityState,
  type UsageStatisticsPageState,
} from "@/lib/types";

export const USAGE_STATISTICS_STORAGE_KEY = "prism.statistics.usage-state";
const USAGE_STATISTICS_STORAGE_VERSION = 1;

const DEFAULT_CHART_GRANULARITY: UsageStatisticsChartGranularityState = {
  costOverview: "hourly",
  requestTrends: "hourly",
  tokenTypeBreakdown: "hourly",
  tokenUsageTrends: "hourly",
};

export function getDefaultUsageStatisticsPageState(): UsageStatisticsPageState {
  return {
    chartGranularity: { ...DEFAULT_CHART_GRANULARITY },
    selectedModelLines: [],
    selectedTimeRange: "24h",
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isUsageSnapshotPreset(value: unknown): value is UsageSnapshotPreset {
  return typeof value === "string" && USAGE_SNAPSHOT_PRESETS.includes(value as UsageSnapshotPreset);
}

function isUsageChartGranularity(value: unknown): value is UsageChartGranularity {
  return typeof value === "string" && USAGE_CHART_GRANULARITIES.includes(value as UsageChartGranularity);
}

function parseChartGranularity(value: unknown): UsageStatisticsChartGranularityState {
  if (!isRecord(value)) {
    return { ...DEFAULT_CHART_GRANULARITY };
  }

  return {
    costOverview: isUsageChartGranularity(value.costOverview)
      ? value.costOverview
      : DEFAULT_CHART_GRANULARITY.costOverview,
    requestTrends: isUsageChartGranularity(value.requestTrends)
      ? value.requestTrends
      : DEFAULT_CHART_GRANULARITY.requestTrends,
    tokenTypeBreakdown: isUsageChartGranularity(value.tokenTypeBreakdown)
      ? value.tokenTypeBreakdown
      : DEFAULT_CHART_GRANULARITY.tokenTypeBreakdown,
    tokenUsageTrends: isUsageChartGranularity(value.tokenUsageTrends)
      ? value.tokenUsageTrends
      : DEFAULT_CHART_GRANULARITY.tokenUsageTrends,
  };
}

interface StorageLike {
  clear: () => void;
  getItem: (key: string) => string | null;
  key: (index: number) => string | null;
  readonly length: number;
  removeItem: (key: string) => void;
  setItem: (key: string, value: string) => void;
}

interface UsageStatisticsStorageEnvelope {
  version: number;
  state: UsageStatisticsPageState;
}

function createMemoryStorage(): StorageLike {
  let storage: Record<string, string> = {};

  return {
    clear: () => {
      storage = {};
    },
    getItem: (key) => storage[key] ?? null,
    key: (index) => Object.keys(storage)[index] ?? null,
    get length() {
      return Object.keys(storage).length;
    },
    removeItem: (key) => {
      delete storage[key];
    },
    setItem: (key, value) => {
      storage[key] = value;
    },
  };
}

function isStorageLike(value: unknown): value is StorageLike {
  return (
    isRecord(value) &&
    typeof value.clear === "function" &&
    typeof value.getItem === "function" &&
    typeof value.key === "function" &&
    typeof value.length === "number" &&
    typeof value.removeItem === "function" &&
    typeof value.setItem === "function"
  );
}

function installStorageCompat(storage: StorageLike): StorageLike {
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: storage,
  });

  if (typeof window !== "undefined") {
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: storage,
    });
  }

  return storage;
}

function getLocalStorage(): StorageLike | null {
  if (typeof window === "undefined") {
    return null;
  }

  if (isStorageLike(window.localStorage)) {
    return window.localStorage;
  }

  return installStorageCompat(createMemoryStorage());
}

function parseUsageStatisticsPageState(value: unknown): UsageStatisticsPageState {
  if (!isRecord(value)) {
    return getDefaultUsageStatisticsPageState();
  }

  return {
    chartGranularity: parseChartGranularity(value.chartGranularity),
    selectedModelLines: Array.isArray(value.selectedModelLines)
      ? value.selectedModelLines.filter((item): item is string => typeof item === "string")
      : [],
    selectedTimeRange: isUsageSnapshotPreset(value.selectedTimeRange)
      ? value.selectedTimeRange
      : "24h",
  };
}

function isUsageStatisticsStorageEnvelope(value: unknown): value is UsageStatisticsStorageEnvelope {
  return (
    isRecord(value) &&
    value.version === USAGE_STATISTICS_STORAGE_VERSION &&
    isRecord(value.state)
  );
}

if (typeof window !== "undefined" && !isStorageLike(window.localStorage)) {
  installStorageCompat(createMemoryStorage());
}

export function readUsageStatisticsPageState(): UsageStatisticsPageState {
  const storage = getLocalStorage();
  if (!storage) {
    return getDefaultUsageStatisticsPageState();
  }

  const raw = storage.getItem(USAGE_STATISTICS_STORAGE_KEY);
  if (!raw) {
    return getDefaultUsageStatisticsPageState();
  }

  try {
    const parsed: unknown = JSON.parse(raw);
    if (!isUsageStatisticsStorageEnvelope(parsed)) {
      return getDefaultUsageStatisticsPageState();
    }

    return parseUsageStatisticsPageState(parsed.state);
  } catch {
    return getDefaultUsageStatisticsPageState();
  }
}

export function writeUsageStatisticsPageState(state: UsageStatisticsPageState): void {
  const storage = getLocalStorage();
  if (!storage) {
    return;
  }

  storage.setItem(
    USAGE_STATISTICS_STORAGE_KEY,
    JSON.stringify({ state, version: USAGE_STATISTICS_STORAGE_VERSION }),
  );
}
