import { describe, expect, it } from "vitest"

import type { DashboardOverviewData, DashboardMetricSnapshot } from "@/pages/dashboard/useDashboardPageData"
import {
  buildObserveBands,
  buildObserveModelPath,
  buildObserveRequestPath,
  clampPercent,
  getObserveAnomalyCount,
  shouldShowRealtimeStaleBanner,
} from "@/features/observe/observeModel"

const metricSnapshot: DashboardMetricSnapshot = {
  activeModels: 2,
  averageRpm: 1.5,
  averageRpmRequestTotal: 12,
  avgLatency: 240,
  errorRate: 6.5,
  p95Latency: 3200,
  pricedRequestCount: 9,
  streamShare: 20,
  successRate: 93.5,
  totalCost: 250000,
  totalModels: 3,
  totalRequests: 12,
  unpricedRequestCount: 2,
}
function createOverviewData(overrides: Partial<DashboardOverviewData> = {}): DashboardOverviewData {
  return {
    apiFamilyRows: [],
    coverage24h: null,
    coverage30d: null,
    generatedAt: "2026-04-11T00:00:00Z",
    health: { lag_seconds: 420, stale: true, stale_after_seconds: 300 },
    metricSnapshot,
    modelDisplayNames: new Map(),
    recentRequests: [],
    routingDiagramData: null,
    routingDiagramError: null,
    routingDiagramLoading: false,
    topSpendingModels: [],
    ...overrides,
  }
}

describe("observe dashboard model", () => {
  it("builds typed drilldown paths for model and request targets", () => {
    expect(buildObserveModelPath(101)).toBe("/models/101")
    expect(buildObserveRequestPath({ type: "request", requestId: 301 })).toBe("/observe/requests?request_id=301")
    expect(buildObserveRequestPath({ type: "endpoint", endpointId: 201 })).toBe("/observe/requests?endpoint_id=201")
    expect(buildObserveRequestPath({ type: "model", modelId: "model-a" })).toBe("/observe/requests?model_id=model-a")
  })

  it("derives bands without losing reporting currency formatting", () => {
    const bands = buildObserveBands({
      snapshot: metricSnapshot,
      formatCurrency: (micros) => `$${(micros / 1_000_000).toFixed(2)} USD`,
      formatNumber: (value, options) => new Intl.NumberFormat("en-US", options).format(value),
    })

    expect(bands.map((band) => band.id)).toEqual(["spend", "latency", "success"])
    expect(bands[0]).toMatchObject({ value: "$0.25 USD", tone: "warning" })
    expect(bands[1]).toMatchObject({ value: "3,200ms", tone: "danger" })
    expect(bands[2]).toMatchObject({ value: "93.5%", tone: "warning" })
  })

  it("keeps stale realtime visibility non-blocking", () => {
    expect(shouldShowRealtimeStaleBanner({ connectionState: "connected", stale: true })).toBe(true)
    expect(shouldShowRealtimeStaleBanner({ connectionState: "reconnecting", stale: false })).toBe(true)
    expect(shouldShowRealtimeStaleBanner({ connectionState: "connected", stale: false })).toBe(false)
  })

  it("counts aggregate anomalies from health, spend, latency, and success signals", () => {
    expect(getObserveAnomalyCount(createOverviewData())).toBe(4)
    expect(clampPercent(-10)).toBe(0)
    expect(clampPercent(110)).toBe(100)
  })
})
