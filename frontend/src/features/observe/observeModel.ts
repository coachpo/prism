import type { ConnectionState } from "@/lib/websocket"
import type { DashboardMetricSnapshot, DashboardOverviewData } from "@/pages/dashboard/useDashboardPageData"

export type ObserveDrilldownTarget =
  | { type: "request"; requestId: number }
  | { type: "endpoint"; endpointId: number }
  | { type: "model"; modelId: string }

export interface ObserveBand {
  id: "spend" | "latency" | "success"
  label: string
  value: string
  detail: string
  tone: "neutral" | "success" | "warning" | "danger"
  progress: number
}

export function clampPercent(value: number): number {
  if (!Number.isFinite(value)) {
    return 0
  }

  return Math.max(0, Math.min(100, value))
}

export function buildObserveRequestPath(target: ObserveDrilldownTarget): string {
  const searchParams = new URLSearchParams()

  if (target.type === "request") {
    searchParams.set("request_id", String(target.requestId))
  }

  if (target.type === "endpoint") {
    searchParams.set("endpoint_id", String(target.endpointId))
  }

  if (target.type === "model") {
    searchParams.set("model_id", target.modelId)
  }

  return `/observe/requests?${searchParams.toString()}`
}

export function buildObserveModelPath(modelConfigId: number): string {
  return `/models/${encodeURIComponent(String(modelConfigId))}`
}

export function getSuccessTone(successRate: number): ObserveBand["tone"] {
  if (successRate >= 99) {
    return "success"
  }

  if (successRate >= 95) {
    return "neutral"
  }

  if (successRate >= 90) {
    return "warning"
  }

  return "danger"
}

export function getLatencyTone(p95Latency: number): ObserveBand["tone"] {
  if (p95Latency <= 750) {
    return "success"
  }

  if (p95Latency <= 1500) {
    return "neutral"
  }

  if (p95Latency <= 3000) {
    return "warning"
  }

  return "danger"
}

export function shouldShowRealtimeStaleBanner({
  connectionState,
  stale,
}: {
  connectionState: ConnectionState
  stale: boolean
}): boolean {
  return stale || connectionState === "disconnected" || connectionState === "reconnecting"
}

export function buildObserveBands({
  formatCurrency,
  formatNumber,
  snapshot,
}: {
  formatCurrency: (micros: number) => string
  formatNumber: (value: number, options?: Intl.NumberFormatOptions) => string
  snapshot: DashboardMetricSnapshot
}): ObserveBand[] {
  return [
    {
      id: "spend",
      label: "Spend corridor",
      value: formatCurrency(snapshot.totalCost),
      detail: `${formatNumber(snapshot.pricedRequestCount)} priced / ${formatNumber(snapshot.unpricedRequestCount)} unpriced`,
      tone: snapshot.unpricedRequestCount > 0 ? "warning" : "success",
      progress: clampPercent(snapshot.pricedRequestCount + snapshot.unpricedRequestCount > 0
        ? (snapshot.pricedRequestCount / (snapshot.pricedRequestCount + snapshot.unpricedRequestCount)) * 100
        : 0),
    },
    {
      id: "latency",
      label: "Latency pressure",
      value: `${formatNumber(snapshot.p95Latency)}ms`,
      detail: `avg ${formatNumber(snapshot.avgLatency)}ms across the last window`,
      tone: getLatencyTone(snapshot.p95Latency),
      progress: clampPercent((snapshot.p95Latency / 3000) * 100),
    },
    {
      id: "success",
      label: "Success lane",
      value: `${formatNumber(snapshot.successRate, { maximumFractionDigits: 1 })}%`,
      detail: `${formatNumber(snapshot.totalRequests)} requests / ${formatNumber(snapshot.errorRate, { maximumFractionDigits: 1 })}% errors`,
      tone: getSuccessTone(snapshot.successRate),
      progress: clampPercent(snapshot.successRate),
    },
  ]
}

export function getObserveAnomalyCount(overviewData: DashboardOverviewData): number {
  const snapshot = overviewData.metricSnapshot
  let anomalies = 0

  if (overviewData.health?.stale) {
    anomalies += 1
  }

  if (snapshot.errorRate > 5) {
    anomalies += 1
  }

  if (snapshot.unpricedRequestCount > 0) {
    anomalies += 1
  }

  if (snapshot.p95Latency > 3000) {
    anomalies += 1
  }

  return anomalies
}

export function toObserveTestId(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")
}
