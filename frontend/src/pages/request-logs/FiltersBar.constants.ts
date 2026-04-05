import { getStaticMessages } from "@/i18n/staticMessages";

export function getTimeLabel(value: string) {
  const copy = getStaticMessages().requestLogs;
  switch (value) {
    case "1h":
      return copy.lastHour;
    case "6h":
      return copy.last6Hours;
    case "24h":
      return copy.last24Hours;
    case "7d":
      return copy.last7Days;
    case "30d":
      return copy.last30Days;
    case "all":
      return copy.requestLogsAllTime;
    default:
      return value;
  }
}

export function getLatencyLabel(value: string) {
  const copy = getStaticMessages().requestLogs;
  switch (value) {
    case "all":
      return copy.anyLatency;
    case "fast":
      return copy.latencyFast;
    case "normal":
      return copy.latencyNormal;
    case "slow":
      return copy.latencySlow;
    case "very_slow":
      return copy.latencyVerySlow;
    default:
      return value;
  }
}

export function getStatusFamilyLabel(value: string) {
  const copy = getStaticMessages().requestLogs;
  switch (value) {
    case "all":
      return copy.allStatuses;
    case "4xx":
      return copy.fourHundredsOnly;
    case "5xx":
      return copy.fiveHundredsOnly;
    default:
      return value;
  }
}
