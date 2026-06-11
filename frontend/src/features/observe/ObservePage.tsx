import { useCallback, useMemo } from "react"
import {
  ActivityIcon,
  AlertTriangleIcon,
  ArrowUpRightIcon,
  GaugeIcon,
  NetworkIcon,
  RadioTowerIcon,
  RefreshCwIcon,
  SparklesIcon,
  ZapIcon,
} from "lucide-react"
import { useLocation, useNavigate } from "react-router-dom"

import { WebSocketStatusIndicator } from "@/components/WebSocketStatusIndicator"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { useProfileContext } from "@/context/ProfileContext"
import { useReportingCurrencyContext } from "@/context/ReportingCurrencyContext"
import { useTimezone } from "@/hooks/useTimezone"
import type { Locale } from "@/i18n/format"
import { useLocale } from "@/i18n/useLocale"
import { formatMoneyMicros, resolveSpendTrustState } from "@/lib/costing"
import { DashboardAnalyticsContent } from "@/pages/dashboard/DashboardAnalyticsContent"
import { RoutingDiagramCard } from "@/pages/dashboard/RoutingDiagramCard"
import { useDashboardPageData, type DashboardOverviewData } from "@/pages/dashboard/useDashboardPageData"
import type { ReportingCurrencyState } from "@/lib/reportingCurrency"
import { cn } from "@/lib/utils"
import type { ConnectionState } from "@/lib/websocket"
import type { RequestLogListItem } from "@/lib/types"
import {
  buildObserveBands,
  buildObserveModelPath,
  buildObserveRequestPath,
  getObserveAnomalyCount,
  shouldShowRealtimeStaleBanner,
  type ObserveBand,
} from "./observeModel"

const toneClasses: Record<ObserveBand["tone"], string> = {
  neutral: "border-border bg-muted/40 text-foreground",
  success: "border-success/25 bg-success/10 text-success",
  warning: "border-warning/30 bg-warning/10 text-warning",
  danger: "border-destructive/30 bg-destructive/10 text-destructive",
}

export function ObservePage() {
  const location = useLocation()
  const navigate = useNavigate()
  const activeTab = new URLSearchParams(location.search).get("tab") === "analytics" ? "analytics" : "overview"
  const { revision, selectedProfile } = useProfileContext()
  const { currencyState } = useReportingCurrencyContext()
  const { format: formatTime } = useTimezone()
  const { formatNumber, locale } = useLocale()
  const data = useDashboardPageData({
    revision,
    selectedProfileId: selectedProfile?.id ?? null,
  })

  const overviewData = data.overviewData
  const formatCurrency = useCallback(
    (micros: number) => formatMoneyMicros(micros, undefined, undefined, 2, 6, locale),
    [locale],
  )
  const bands = useMemo(
    () => buildObserveBands({ formatCurrency, formatNumber, snapshot: overviewData.metricSnapshot }),
    [formatCurrency, formatNumber, overviewData.metricSnapshot],
  )
  const anomalyCount = getObserveAnomalyCount(overviewData)
  const realtimeStale = shouldShowRealtimeStaleBanner({
    connectionState: data.connectionState,
    stale: overviewData.health?.stale ?? false,
  })

  const handleRefresh = useCallback(() => {
    void data.refreshDashboard()
  }, [data])
  const handleSelectModel = useCallback((modelConfigId: number) => {
    navigate(buildObserveModelPath(modelConfigId))
  }, [navigate])
  const handleDrillDownRequests = useCallback((params: { endpoint_id?: number; model_id?: string }) => {
    if (params.endpoint_id) {
      navigate(buildObserveRequestPath({ type: "endpoint", endpointId: params.endpoint_id }))
      return
    }
    if (params.model_id) {
      navigate(buildObserveRequestPath({ type: "model", modelId: params.model_id }))
    }
  }, [navigate])
  const handleSelectRequest = useCallback((requestId: number) => {
    navigate(buildObserveRequestPath({ type: "request", requestId }))
  }, [navigate])

  if (activeTab === "analytics") {
    return (
      <main data-testid="observe-dashboard" className="operator-page-transition flex flex-col gap-6">
        <ObserveHero
          activeTab={activeTab}
          connectionState={data.connectionState}
          isRefreshing={data.isRefreshing}
          isSyncing={data.isSyncing}
          generatedAt={overviewData.generatedAt}
          selectedProfileName={selectedProfile?.name ?? "No selected profile"}
          onRefresh={handleRefresh}
          onShowAnalytics={() => navigate("/observe?tab=analytics")}
        />
        <DashboardAnalyticsContent />
      </main>
    )
  }

  if (data.loading && !overviewData.generatedAt) {
    return <ObserveLoading />
  }

  return (
    <main data-testid="observe-dashboard" className="operator-page-transition flex flex-col gap-6">
      <ObserveHero
        activeTab={activeTab}
        connectionState={data.connectionState}
        isRefreshing={data.isRefreshing}
        isSyncing={data.isSyncing}
        generatedAt={overviewData.generatedAt}
        selectedProfileName={selectedProfile?.name ?? "No selected profile"}
        onRefresh={handleRefresh}
        onShowAnalytics={() => navigate("/observe?tab=analytics")}
      />
      {realtimeStale ? (
        <RealtimeStaleBanner
          connectionState={data.connectionState}
          lagSeconds={overviewData.health?.lag_seconds ?? null}
          staleAfterSeconds={overviewData.health?.stale_after_seconds ?? null}
        />
      ) : null}

      {data.dashboardError ? (
        <Alert variant="destructive" data-testid="observe-error-state">
          <AlertTriangleIcon />
          <AlertTitle>Dashboard data could not be loaded</AlertTitle>
          <AlertDescription>{data.dashboardError}</AlertDescription>
        </Alert>
      ) : null}

      <section className="grid grid-cols-1 gap-4 xl:grid-cols-12 bento-dense">
        <div className="xl:col-span-8">
          <RoutingTheater
            data={overviewData}
            loading={data.loading}
            onSelectModel={handleSelectModel}
            onDrillDownRequests={handleDrillDownRequests}
          />
        </div>
        <div className="xl:col-span-4">
          <HealthAnomalyStrip
            anomalyCount={anomalyCount}
            overviewData={overviewData}
            connectionState={data.connectionState}
            generatedAt={overviewData.generatedAt}
            formatTime={formatTime}
          />
        </div>
        <SpendLatencySuccessBands bands={bands} metricsHighlighted={data.metricsHighlighted} />
        <div className="xl:col-span-8">
          <RequestStream
            clearRecentRequestHighlight={data.clearRecentRequestHighlight}
            recentNewIds={data.recentNewIds}
            requests={overviewData.recentRequests}
            formatNumber={formatNumber}
            formatTime={formatTime}
            onSelectRequest={handleSelectRequest}
          />
        </div>
        <div className="xl:col-span-4">
          <TopRouteSpend
            overviewData={overviewData}
            currencyTrust={currencyState.trust}
            formatCurrency={formatCurrency}
            onOpenRequests={() => navigate("/observe/requests")}
          />
        </div>
      </section>
    </main>
  )
}

function ObserveHero({
  activeTab,
  connectionState,
  generatedAt,
  isRefreshing,
  isSyncing,
  onRefresh,
  onShowAnalytics,
  selectedProfileName,
}: {
  activeTab: "overview" | "analytics"
  connectionState: ConnectionState
  generatedAt: string | null
  isRefreshing: boolean
  isSyncing: boolean
  onRefresh: () => void
  onShowAnalytics: () => void
  selectedProfileName: string
}) {
  return (
    <section className="operator-surface relative overflow-hidden rounded-3xl border px-5 py-6 shadow-operator-glow md:px-8 md:py-8">
      <div className="absolute inset-y-0 right-0 hidden w-1/2 bg-[radial-gradient(circle_at_top_right,color-mix(in_oklab,var(--operator-glow)_28%,transparent),transparent_42%)] md:block" />
      <div className="relative flex flex-col gap-6 lg:flex-row lg:items-end lg:justify-between">
        <div className="flex max-w-5xl flex-col gap-4">
          <div className="flex flex-wrap items-center gap-2 text-xs font-medium uppercase tracking-[0.24em] text-muted-foreground">
            <RadioTowerIcon />
            Live routing theater
          </div>
          <h1 className="max-w-5xl text-4xl font-semibold tracking-tight md:text-6xl">
            Observe model traffic as it moves through targets, spend, and reliability lanes.
          </h1>
          <p className="max-w-3xl text-sm leading-6 text-muted-foreground md:text-base">
            Selected profile: <span className="font-medium text-foreground">{selectedProfileName}</span>. Dashboard updates stay scoped to this profile and the current reporting currency.
          </p>
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant={activeTab === "analytics" ? "default" : "outline"}
              role={activeTab === "analytics" ? "tab" : undefined}
              aria-selected={activeTab === "analytics" ? "true" : undefined}
              onClick={onShowAnalytics}
            >
              Analytics
            </Button>
          </div>
        </div>
        <div className="flex flex-col gap-3 rounded-2xl border bg-card/80 p-4 backdrop-blur">
          <div className="flex items-center justify-between gap-4">
            <span className="text-sm font-medium">Realtime status</span>
            <WebSocketStatusIndicator connectionState={connectionState} isSyncing={isSyncing} />
          </div>
          <div className="flex flex-wrap gap-2">
            <Badge variant="outline" data-testid="observe-realtime-status" className="capitalize">
              {isSyncing ? "syncing" : connectionState}
            </Badge>
            {generatedAt ? <Badge variant="secondary">Updated {new Date(generatedAt).toLocaleTimeString()}</Badge> : null}
          </div>
          <Button variant="default" onClick={onRefresh} disabled={isRefreshing} data-testid="observe-refresh-button">
            <RefreshCwIcon data-icon="inline-start" className={cn(isRefreshing ? "animate-spin" : undefined)} />
            Refresh theater
          </Button>
        </div>
      </div>
    </section>
  )
}

function RealtimeStaleBanner({
  connectionState,
  lagSeconds,
  staleAfterSeconds,
}: {
  connectionState: ConnectionState
  lagSeconds: number | null
  staleAfterSeconds: number | null
}) {
  return (
    <Alert data-testid="realtime-stale-banner" className="border-warning/35 bg-warning/10 text-warning-foreground">
      <AlertTriangleIcon />
      <AlertTitle>Realtime feed is stale, dashboard remains readable</AlertTitle>
      <AlertDescription>
        Connection is {connectionState}. Last aggregate lag is {lagSeconds ?? 0}s with a stale threshold of {staleAfterSeconds ?? 0}s.
      </AlertDescription>
    </Alert>
  )
}

function RoutingTheater({
  data,
  loading,
  onDrillDownRequests,
  onSelectModel,
}: {
  data: DashboardOverviewData
  loading: boolean
  onDrillDownRequests: (params: { endpoint_id?: number; model_id?: string }) => void
  onSelectModel: (modelConfigId: number) => void
}) {
  return (
    <Card data-testid="observe-routing-theater" className="operator-surface min-h-full overflow-hidden rounded-3xl">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-2xl">
          <NetworkIcon />
          Routing theater
        </CardTitle>
        <CardDescription>
          Backend-owned topology rendered as the primary operator surface. Nodes drill into model detail or scoped request logs.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <RoutingDiagramCard
          data={data.routingDiagramData}
          loading={loading || data.routingDiagramLoading}
          error={data.routingDiagramError}
          onSelectModel={onSelectModel}
          onDrillDownRequests={onDrillDownRequests}
        />
      </CardContent>
    </Card>
  )
}
function HealthAnomalyStrip({
  anomalyCount,
  connectionState,
  formatTime,
  generatedAt,
  overviewData,
}: {
  anomalyCount: number
  connectionState: ConnectionState
  formatTime: (isoString: string, options?: Intl.DateTimeFormatOptions) => string
  generatedAt: string | null
  overviewData: DashboardOverviewData
}) {
  const health = overviewData.health
  const stats = overviewData.routingDiagramData?.stats

  return (
    <Card data-testid="observe-health-strip" className="operator-state-surface min-h-full rounded-3xl">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <GaugeIcon />
          Health and anomaly strip
        </CardTitle>
        <CardDescription>Non-blocking operational readout for realtime freshness, topology depth, and aggregate lag.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="grid grid-cols-2 gap-3">
          <HealthTile label="Anomalies" value={String(anomalyCount)} tone={anomalyCount > 0 ? "warning" : "success"} />
          <HealthTile label="Realtime" value={connectionState} tone={connectionState === "connected" ? "success" : "warning"} />
          <HealthTile label="Models" value={String(stats?.model_count ?? 0)} tone="neutral" />
          <HealthTile label="Targets" value={String(stats?.active_terminal_target_count ?? 0)} tone="neutral" />
        </div>
        <div className="rounded-2xl border bg-background/55 p-3 text-sm text-muted-foreground">
          {generatedAt ? `Generated ${formatTime(generatedAt, { hour: "numeric", minute: "numeric", second: "numeric" })}` : "No aggregate has been generated yet."}
          {health ? ` Lag ${health.lag_seconds}s / stale after ${health.stale_after_seconds}s.` : ""}
        </div>
      </CardContent>
    </Card>
  )
}

function HealthTile({ label, tone, value }: { label: string; tone: ObserveBand["tone"]; value: string }) {
  return (
    <div className={cn("rounded-2xl border p-3", toneClasses[tone])}>
      <div className="text-xs uppercase tracking-[0.18em] opacity-80">{label}</div>
      <div className="mt-2 text-2xl font-semibold capitalize tracking-tight">{value}</div>
    </div>
  )
}

function SpendLatencySuccessBands({
  bands,
  metricsHighlighted,
}: {
  bands: ObserveBand[]
  metricsHighlighted: boolean
}) {
  return (
    <>
      {bands.map((band) => (
        <Card
          key={band.id}
          data-testid={`observe-band-${band.id}`}
          className={cn("xl:col-span-4 overflow-hidden rounded-3xl", metricsHighlighted ? "ws-value-updated" : undefined)}
        >
          <CardHeader>
            <CardTitle>{band.label}</CardTitle>
            <CardDescription>{band.detail}</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="text-4xl font-semibold tracking-tight">{band.value}</div>
            <div className="h-2 overflow-hidden rounded-full bg-muted" aria-hidden="true">
              <div className={cn("h-full rounded-full", toneClasses[band.tone])} style={{ width: `${band.progress}%` }} />
            </div>
          </CardContent>
        </Card>
      ))}
    </>
  )
}
function RequestStream({
  clearRecentRequestHighlight,
  formatNumber,
  formatTime,
  onSelectRequest,
  recentNewIds,
  requests,
}: {
  clearRecentRequestHighlight: (requestId: number) => void
  formatNumber: (value: number, options?: Intl.NumberFormatOptions) => string
  formatTime: (isoString: string, options?: Intl.DateTimeFormatOptions) => string
  onSelectRequest: (requestId: number) => void
  recentNewIds: Set<number>
  requests: RequestLogListItem[]
}) {
  const { currencyState } = useReportingCurrencyContext()
  const { locale } = useLocale()
  return (
    <Card data-testid="observe-request-stream" className="operator-table-shell min-h-full overflow-hidden rounded-3xl">
      <CardHeader className="border-b">
        <CardTitle className="flex items-center gap-2">
          <ActivityIcon />
          Request stream
        </CardTitle>
        <CardDescription>Most recent selected-profile requests, merged live without changing reporting currency context.</CardDescription>
      </CardHeader>
      <CardContent className="px-0">
        {requests.length === 0 ? (
          <ObserveEmptyState title="No requests observed" description="Traffic will appear here as soon as the selected profile receives runtime requests." />
        ) : (
          <div className="divide-y">
            {requests.slice(0, 8).map((request, index) => (
              <button
                key={request.id}
                type="button"
                data-testid={`request-stream-row-${index}`}
                className={cn(
                  "grid w-full grid-cols-1 gap-3 px-[var(--density-card-pad-x)] py-4 text-left transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 md:grid-cols-[1fr_auto]",
                  recentNewIds.has(request.id) ? "ws-new-row-left" : undefined,
                )}
                onAnimationEnd={() => clearRecentRequestHighlight(request.id)}
                onClick={() => onSelectRequest(request.id)}
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant="outline" className="font-mono">#{request.id}</Badge>
                    <span className="truncate text-sm font-medium">{request.model_label || request.model_id}</span>
                    <Badge variant={request.status_code >= 200 && request.status_code < 300 ? "secondary" : "destructive"}>{request.status_code}</Badge>
                  </div>
                  <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                    <span>{formatTime(request.created_at, { hour: "numeric", minute: "numeric", second: "numeric" })}</span>
                    <span>{request.endpoint_label || "No endpoint"}</span>
                    <span>{request.api_family}</span>
                  </div>
                </div>
                <div className="grid grid-cols-3 gap-3 text-right text-xs md:min-w-72">
                  <MetricMicro label="Latency" value={`${formatNumber(request.response_time_ms)}ms`} />
                  <MetricMicro label="Tokens" value={formatNumber(request.total_tokens ?? 0)} />
                  <MetricMicro label="Spend" value={formatRequestSpend(request, currencyState, locale)} />
                </div>
              </button>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function MetricMicro({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-muted-foreground">{label}</div>
      <div className="font-mono font-medium text-foreground">{value}</div>
    </div>
  )
}

function formatRequestSpend(
  request: RequestLogListItem,
  currencyState: ReportingCurrencyState,
  locale: Locale,
) {
  const spendTrust = resolveSpendTrustState(
    {
      costMicros: request.total_cost_user_currency_micros,
      priced: request.priced_flag,
      unpricedReason: request.unpriced_reason,
    },
    currencyState,
  )

  if (spendTrust === "unpriced") {
    return "Unpriced"
  }

  return formatMoneyMicros(
    request.total_cost_user_currency_micros,
    request.report_currency_symbol ?? undefined,
    request.report_currency_symbol ? undefined : currencyState.currency.code,
    2,
    6,
    locale,
  )
}

function TopRouteSpend({
  currencyTrust,
  formatCurrency,
  onOpenRequests,
  overviewData,
}: {
  currencyTrust: string
  formatCurrency: (micros: number) => string
  onOpenRequests: () => void
  overviewData: DashboardOverviewData
}) {
  const topModel = overviewData.topSpendingModels[0]

  return (
    <Card data-testid="observe-spend-leader" className="operator-state-surface min-h-full rounded-3xl">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <SparklesIcon />
          Spend leader
        </CardTitle>
        <CardDescription>Highest request-based spend in the current reporting currency.</CardDescription>
        <CardAction>
          <Badge variant="outline" className="capitalize">{currencyTrust}</Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {topModel ? (
          <>
            <div className="rounded-2xl border bg-background/60 p-4">
              <div className="text-sm text-muted-foreground">{topModel.model_id}</div>
              <div className="mt-2 text-2xl font-semibold tracking-tight">{topModel.model_label || topModel.model_id}</div>
              <div className="mt-4 text-4xl font-semibold tracking-tight">{formatCurrency(topModel.total_cost_micros)}</div>
            </div>
            <Button variant="outline" onClick={onOpenRequests}>
              Open request logs
              <ArrowUpRightIcon data-icon="inline-end" />
            </Button>
          </>
        ) : (
          <ObserveEmptyState title="No spend leader" description="Priced request totals will populate this card after traffic is observed." />
        )}
      </CardContent>
    </Card>
  )
}

function ObserveEmptyState({ title, description }: { title: string; description: string }) {
  return (
    <div className="flex min-h-48 flex-col items-center justify-center gap-2 p-6 text-center">
      <ZapIcon className="text-muted-foreground" />
      <div className="font-medium">{title}</div>
      <p className="max-w-sm text-sm text-muted-foreground">{description}</p>
    </div>
  )
}

function ObserveLoading() {
  return (
    <main data-testid="observe-loading-state" className="flex flex-col gap-6">
      <section className="operator-surface rounded-3xl border p-8">
        <Skeleton className="h-6 w-48" />
        <Skeleton className="mt-5 h-16 max-w-4xl" />
        <Skeleton className="mt-4 h-5 max-w-2xl" />
      </section>
      <section className="grid grid-cols-1 gap-4 xl:grid-cols-12">
        <Skeleton className="h-[620px] rounded-3xl xl:col-span-8" />
        <Skeleton className="h-[620px] rounded-3xl xl:col-span-4" />
        <Skeleton className="h-48 rounded-3xl xl:col-span-4" />
        <Skeleton className="h-48 rounded-3xl xl:col-span-4" />
        <Skeleton className="h-48 rounded-3xl xl:col-span-4" />
      </section>
    </main>
  )
}

export default ObservePage
