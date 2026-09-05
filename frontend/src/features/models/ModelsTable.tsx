import type { ReactNode } from "react"
import { useId, useMemo, useState } from "react"
import { Link, useNavigate } from "@tanstack/react-router"
import { MoreHorizontal, Pencil, Plus, Server, Trash2 } from "lucide-react"

import { CopyButton } from "@/components/CopyButton"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useReportingCurrencyContext } from "@/context/ReportingCurrencyContext"
import { useLocale } from "@/i18n/useLocale"
import type { ManagedModelConfigListItem } from "@/lib/api/models"
import { formatMoneyMicros } from "@/lib/costing"
import { formatApiFamily } from "@/components/apiFamilyPresentation"
import { cn } from "@/lib/utils"
import {
  OperatorDestructiveDialog,
  OperatorEmptyState,
  OperatorClippedBadge,
  OperatorHelpHint,
  OperatorMissingValue,
  OperatorStalenessBadge,
  OperatorStatusBadge,
  OperatorTypeBadge,
} from "@/shared/design-system"
import {
  operationalRowActionsClassName,
} from "@/shared/table/operationalTable"
import { OperationalTablePagination } from "@/shared/table/paginationControls"
import {
  paginateOperationalRows,
  sortOperationalRows,
  type OperationalSortDirection,
  type OperationalSortState,
  type OperationalSortValue,
} from "@/shared/table/operationalTableState"
import { formatLatencyForDisplay } from "@/pages/model-detail/modelDetailMetricsAndPaths"
import type { ModelDerivedMetric } from "@/pages/models/modelTableContracts"
import type { ObservabilityScope } from "@/lib/types/model-stats"
import { isSingleTruncated } from "./modelRoutingFlags"
import { ModelExitMappingCell } from "./ModelExitMappingCell"
import type { ModelInventoryView } from "./modelView"

const MODEL_PAGE_SIZES = [25, 50, 100] as const
const MODEL_COLUMN_COUNT = 10
/** 勾选列 + 身份/家族/状态/出口映射/策略：口径列组之前的那几列。 */
const MODEL_COLUMNS_BEFORE_METRICS = 6
/** 四个指标列之后只剩「操作」。 */
const MODEL_METRIC_COLUMN_COUNT = 4

export type ModelSortColumn =
  | "name"
  | "api_family"
  | "status"
  | "targets"
  | "strategy"
  | "success"
  | "p95"
  | "requests"
  | "spend"

type Props = {
  scope: ObservabilityScope
  /** 口径分段控件，渲染在它重算的四列正上方。 */
  metricsScopeControl?: ReactNode
  /** 成本口径被保留期裁剪时的实际起点（已本地化的日期）。 */
  spendRetentionFrom?: string | null
  filtered: ManagedModelConfigListItem[]
  hasActiveFilters: boolean
  metricsFailed: boolean
  metricsLoading: boolean
  modelMetrics24h: Record<number, ModelDerivedMetric>
  modelSpend30dMicros: Record<number, number | null>
  onClearFilters: () => void
  onCreate: () => void
  onEdit: (model?: ManagedModelConfigListItem) => void
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
  onSelectionChange: (ids: Set<number>) => void
  onSetEnabled: (model: ManagedModelConfigListItem, enabled: boolean) => Promise<boolean>
  onSetManyEnabled: (models: ManagedModelConfigListItem[], enabled: boolean) => Promise<void>
  onSort: (column: ModelSortColumn, direction: OperationalSortDirection) => void
  page: number
  pageSize?: number
  selectedIds: Set<number>
  setDeleteTarget: (model: ManagedModelConfigListItem) => void
  sortBy: ModelSortColumn
  sortOrder: OperationalSortDirection
  togglingModelIds: Set<number>
  view: ModelInventoryView
}

function modelTitle(model: ManagedModelConfigListItem) {
  return model.display_name || model.model_id
}

function ModelIdentityCell({
  model,
  view,
}: {
  model: ManagedModelConfigListItem
  view: ModelInventoryView
}) {
  const { formatNumber, messages } = useLocale()
  const title = modelTitle(model)
  const showModelId = title !== model.model_id
  // 只在混合视图里，这枚徽章才在区分两类行；单一视图下 11 行全一样，
  // 它只是把名称列撑宽、把扫描打断。
  const showRoleBadge = view === "all"

  return (
    <div className="flex min-w-48 flex-col gap-0.5">
      <div className="flex min-w-0 items-center gap-1">
        {/* 模型名就是进详情的入口：原本它只是一段带 title 的文本，
            唯一常显的链接是整格出口映射。 */}
        <Link
          to="/route/models/$modelId"
          params={{ modelId: String(model.id) }}
          className="truncate font-medium underline-offset-2 hover:underline"
          title={title}
        >
          {title}
        </Link>
        {showRoleBadge ? (
          <OperatorTypeBadge
            intent={model.direct_request_enabled === true ? "accent" : "neutral"}
            label={model.direct_request_enabled === true ? messages.modelsPage.viewEntries : messages.modelsPage.viewModelTargets}
            preserveLabel
          />
        ) : null}
        <CopyButton
          aria-label={messages.modelDetail.copyModelIdAria()}
          className="size-6 rounded-md text-muted-foreground hover:text-foreground"
          errorMessage={messages.auth.loginFailed}
          label=""
          size="icon-xs"
          successMessage={messages.requestLogs.copy}
          targetLabel={messages.modelDetail.modelIdLabel}
          value={model.model_id}
        />
      </div>
      {showModelId ? <span className="truncate font-mono text-xs text-muted-foreground">{model.model_id}</span> : null}
      {model.direct_request_enabled === false ? (
        <span
          className="text-xs text-muted-foreground"
          title={model.incoming_model_target_count === 0
            ? model.configuration_warnings?.find((warning) => warning.code === "model_target_unreferenced")?.message
            : undefined}
        >
          {model.incoming_model_target_count > 0
            ? messages.modelsPage.incomingModelTargetCount(formatNumber(model.incoming_model_target_count))
            : messages.modelsPage.unreferencedModelTarget}
        </span>
      ) : null}
    </div>
  )
}

/** A metric column head that names its own window and basis. */
function MetricHead({
  basis,
  clipped,
  label,
  onSort,
  sort,
  sortKey,
  stale,
  window,
}: {
  basis: string
  label: string
  onSort: (column: ModelSortColumn) => void
  sort: OperationalSortState<ModelSortColumn>
  sortKey: ModelSortColumn
  stale: boolean
  window: string
  clipped?: ReactNode
}) {
  const { messages } = useLocale()
  const copy = messages.modelsPage
  const active = sort.column === sortKey
  // columnheader 的名字必须只剩「列名 + 窗口」。名字若由内容计算，帮助按钮的
  // aria-label（口径全文）会被并进列名，排序时每次重播一整句。
  const nameId = useId()
  const basisId = useId()

  return (
    <TableHead
      aria-sort={active ? (sort.direction === "asc" ? "ascending" : "descending") : "none"}
      aria-labelledby={nameId}
      aria-describedby={basisId}
      className="p-0 text-right"
    >
      {/* 窗口与列名同一行：把它挤到第二行会让整个表头高出一档，
          而表头是首屏预算里最贵的部分之一。 */}
      <div className="flex flex-col items-end">
        <span className="flex w-full min-w-0 items-center justify-end gap-1 pr-[var(--density-table-cell-px)]">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className={cn(
              "h-[var(--density-table-head-h)] min-w-0 justify-end gap-1 rounded-none px-1 text-xs font-medium text-muted-foreground hover:text-foreground",
              active && "text-foreground",
            )}
            onClick={() => onSort(sortKey)}
          >
            <span className="truncate">{label}</span>
            <span className="shrink-0 text-[11px] font-normal text-muted-foreground">
              {window}
            </span>
            <SortGlyph active={active} direction={sort.direction} />
          </Button>
          <OperatorHelpHint label={basis} align="end" />
        </span>
        <span id={nameId} className="sr-only">{`${label} ${window}`}</span>
        <span id={basisId} className="sr-only">
          {basis}
        </span>
        {clipped}
        {stale ? (
          <OperatorStalenessBadge label={copy.metricsUnavailable} reason={copy.metricsUnavailableReason} />
        ) : null}
      </div>
    </TableHead>
  )
}

function MetricValue({
  failed,
  loading,
  missingReason,
  partialReason,
  render,
  value,
}: {
  failed: boolean
  loading: boolean
  /** 这一格为什么没有数：无流量、无样本、无可信成本，三者必须能分辨。 */
  missingReason?: string
  partialReason?: string
  render: (value: number) => string
  value: number | null | undefined
}) {
  const { messages } = useLocale()
  const copy = messages.modelsPage

  if (loading && value == null) return <Skeleton className="ml-auto h-4 w-12" />
  if (failed && value == null) {
    // 整表 44 个红字会淹没真正的异常：这里只留一个紧凑标记，
    // 恢复动作与完整说明在表格上方的页面级通知里。
    return (
      <span
        className="font-mono text-xs text-failing"
        title={copy.metricsUnavailableReason}
      >
        <span aria-hidden="true">— ⚠</span>
        <span className="sr-only">{copy.metricsUnavailable}</span>
      </span>
    )
  }
  if (value == null)
    return <OperatorMissingValue className="text-xs" reason={missingReason} />
  return (
    <span className="inline-flex flex-col items-end gap-0.5">
      <span className="font-mono text-xs tabular-nums">{render(value)}</span>
      {partialReason ? (
        <OperatorClippedBadge
          label={copy.metricsPartial}
          reason={partialReason}
        />
      ) : null}
    </span>
  )
}

export function ModelsTable({
  scope,
  metricsScopeControl,
  spendRetentionFrom,
  filtered,
  hasActiveFilters,
  metricsFailed,
  metricsLoading,
  modelMetrics24h,
  modelSpend30dMicros,
  onClearFilters,
  onCreate,
  onEdit,
  onPageChange,
  onPageSizeChange,
  onSelectionChange,
  onSetEnabled,
  onSetManyEnabled,
  onSort,
  page,
  pageSize,
  selectedIds,
  setDeleteTarget,
  sortBy,
  sortOrder,
  togglingModelIds,
  view,
}: Props) {
  const { currencyState } = useReportingCurrencyContext()
  const { formatNumber, locale, messages } = useLocale()
  const copy = messages.modelsPage
  const tableCopy = messages.operationalTable
  const navigate = useNavigate()
  const [bulkDisableTarget, setBulkDisableTarget] = useState<
    ManagedModelConfigListItem[] | null
  >(null)
  const sort: OperationalSortState<ModelSortColumn> = useMemo(
    () => ({ column: sortBy, direction: sortOrder }),
    [sortBy, sortOrder],
  )
  const effectivePageSize = pageSize ?? MODEL_PAGE_SIZES[0]

  const sortedModels = useMemo(() => {
    const getValue = (model: ManagedModelConfigListItem, column: ModelSortColumn): OperationalSortValue => {
      if (column === "name") return modelTitle(model)
      if (column === "api_family") return model.api_family
      if (column === "status") return model.is_enabled
      if (column === "targets") return model.routing_summary?.total_access_target_count ?? null
      if (column === "strategy") return model.loadbalance_strategy?.name ?? null
      if (column === "success") return modelMetrics24h[model.id]?.success_rate ?? null
      if (column === "p95") return modelMetrics24h[model.id]?.p95_latency_ms ?? null
      if (column === "requests") return modelMetrics24h[model.id]?.request_count_24h ?? null
      return modelSpend30dMicros[model.id] ?? null
    }
    return sortOperationalRows(filtered, sort, getValue, locale)
  }, [filtered, locale, modelMetrics24h, modelSpend30dMicros, sort])

  const pageState = paginateOperationalRows(sortedModels, Math.max(0, page - 1), effectivePageSize)
  const pageRowIds = pageState.pageRows.map((model) => model.id)
  const allPageSelected = pageRowIds.length > 0 && pageRowIds.every((id) => selectedIds.has(id))
  const selectedModels = filtered.filter((model) => selectedIds.has(model.id))

  const updateSort = (column: ModelSortColumn) => {
    const nextDirection: OperationalSortDirection =
      sort.column === column && sort.direction === "asc" ? "desc" : "asc"
    onSort(column, nextDirection)
  }

  const toggleRow = (id: number) => {
    const next = new Set(selectedIds)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    onSelectionChange(next)
  }

  const modelTargetViewEmpty = view === "model_targets" && !hasActiveFilters
  // 空态渲染在 tbody 里：连表头一起换掉会丢失列结构这个定位锚点，
  // 筛选到零条时操作者最需要的恰恰是「还有哪些列可以放宽」。
  const emptyState =
    filtered.length === 0 ? (
      <OperatorEmptyState
        icon={<Server />}
        title={
          hasActiveFilters
            ? copy.noModelsMatchFilters
            : modelTargetViewEmpty
              ? copy.noModelTargetsConfigured
              : view === "entries"
                ? copy.noEntryModelsConfigured
                : copy.noModelConfigsConfigured
        }
        description={
          hasActiveFilters
            ? copy.tryDifferentFilters
            : modelTargetViewEmpty
              ? copy.createModelTargetOnlyDescription
              : view === "entries"
                ? copy.createFirstEntryModel
                : copy.createFirstModelConfig
        }
        action={
          hasActiveFilters ? (
            <Button variant="outline" onClick={onClearFilters}>{copy.clearFilters}</Button>
          ) : (
            // Same entry point as the page header: one way to create a model.
            <Button onClick={onCreate}><Plus data-icon="inline-start" />{copy.newModel}</Button>
          )
        }
      />
    ) : null

  return (
    <div data-testid="models-table" data-table-density="compact">
      {selectedModels.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2 border-t border-border bg-inset px-[var(--density-card-pad-x)] py-2 text-xs">
          <span className="text-muted-foreground">{copy.selectionCount(formatNumber(selectedModels.length))}</span>
          <Button type="button" variant="outline" size="sm" onClick={() => void onSetManyEnabled(selectedModels, true)}>
            {copy.bulkEnable}
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={() => setBulkDisableTarget(selectedModels)}>
            {copy.bulkDisable}
          </Button>
          <Button type="button" variant="ghost" size="sm" onClick={() => onSelectionChange(new Set())}>
            {copy.bulkClear}
          </Button>
        </div>
      ) : null}

      {/* 同一个容器同时管纵横滚动：只有这样 thead 的 sticky 才有滚动祖先。
          高度上限必须走 scrollAreaClassName 落到表格自己的滚动容器上——
          加在外面再包一层，表头会跟着内容一起滚走。
          行数少于一屏时 max-h 不生效，页面照常整体滚动。 */}
      <div className="border-t border-border">
        <Table scrollAreaClassName="max-h-[calc(100dvh-18rem)]">
          <TableHeader className="z-20">
            {/* 口径控件必须挨着它重算的那四列：它原来在卡头，与被它改名的
                列头相距一千多像素，切换后首屏看不到任何变化。 */}
            <TableRow className="hover:bg-transparent">
              <TableHead colSpan={MODEL_COLUMNS_BEFORE_METRICS} className="border-b-0" />
              <TableHead colSpan={MODEL_METRIC_COLUMN_COUNT} className="border-b-0 py-1">
                <div className="flex items-center justify-end gap-1">
                  {metricsScopeControl}
                  <OperatorHelpHint label={copy.metricsScopeBasis(scope)} align="end" />
                </div>
              </TableHead>
              <TableHead className="border-b-0" />
            </TableRow>
            <TableRow>
              {/* 勾选列也要冻结：它排在身份列之前，不冻结就会在横滚时滑到
                  身份列底下，看起来像行被吃掉了一格。 */}
              <TableHead className="sticky left-0 z-20 w-8 bg-inset">
                <Checkbox
                  aria-label={copy.selectAllAria}
                  checked={allPageSelected}
                  onCheckedChange={(checked) => {
                    const next = new Set(selectedIds)
                    for (const id of pageRowIds) {
                      if (checked === true) next.add(id)
                      else next.delete(id)
                    }
                    onSelectionChange(next)
                  }}
                />
              </TableHead>
              <SortHead
                column="name"
                label={copy.columnModel}
                onSort={updateSort}
                sort={sort}
                className="sticky left-8 z-20 bg-inset shadow-[inset_-1px_0_0_0_var(--color-border)]"
              />
              <SortHead column="api_family" label={copy.columnApiFamily} onSort={updateSort} sort={sort} />
              <SortHead column="status" label={copy.columnStatus} onSort={updateSort} sort={sort} />
              <SortHead column="targets" label={copy.columnTargets} onSort={updateSort} sort={sort} />
              <SortHead column="strategy" label={copy.columnStrategy} onSort={updateSort} sort={sort} />
              <MetricHead
                basis={copy.scopeSuccessBasis(scope)}
                label={copy.scopeSuccessColumn(scope)}
                onSort={updateSort}
                sort={sort}
                sortKey="success"
                stale={metricsFailed}
                window={copy.window24h}
              />
              <MetricHead
                basis={copy.scopeP95Basis(scope)}
                label={copy.scopeP95Column(scope)}
                onSort={updateSort}
                sort={sort}
                sortKey="p95"
                stale={metricsFailed}
                window={copy.window24h}
              />
              <MetricHead
                basis={copy.scopeRequestsBasis(scope)}
                label={copy.scopeRequestsColumn(scope)}
                onSort={updateSort}
                sort={sort}
                sortKey="requests"
                stale={metricsFailed}
                window={copy.window24h}
              />
              <MetricHead
                basis={copy.scopeSpendBasis(scope)}
                label={copy.scopeSpendColumn()}
                onSort={updateSort}
                sort={sort}
                sortKey="spend"
                stale={metricsFailed}
                // 路由尝试口径明说不声明成本，再给它挂一个 30 天时间窗，
                // 读起来就成了「这一列只是取不到数」。没有量，就没有窗口。
                window={
                  scope === "route_attempt"
                    ? messages.common.notApplicable
                    : spendRetentionFrom
                      ? copy.spendWindowClipped(spendRetentionFrom)
                      : copy.window30d
                }
                clipped={
                  scope !== "route_attempt" && spendRetentionFrom ? (
                    <OperatorClippedBadge
                      label={messages.honesty.outsideRetention}
                      reason={copy.spendWindowClippedReason(spendRetentionFrom)}
                    />
                  ) : null
                }
              />
              <TableHead className="sticky right-0 z-20 bg-inset text-right shadow-[inset_1px_0_0_0_var(--color-border)]">
                {messages.pricingTemplatesUi.actions}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {emptyState ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={MODEL_COLUMN_COUNT + 1} className="p-0">
                  {emptyState}
                </TableCell>
              </TableRow>
            ) : null}
            {pageState.pageRows.map((model) => {
              const title = modelTitle(model)
              const metrics = modelMetrics24h[model.id]
              const spend = modelSpend30dMicros[model.id] ?? null
              const enabledTargets = model.routing_summary?.enabled_access_target_count
              const truncated = isSingleTruncated(model)

              return (
                <TableRow key={model.id} className="group/row" data-testid={`models-table-row-${model.id}`}>
                  <TableCell className="sticky left-0 z-10 w-8 bg-panel align-top">
                    <Checkbox
                      aria-label={copy.selectModelAria(title)}
                      checked={selectedIds.has(model.id)}
                      onCheckedChange={() => toggleRow(model.id)}
                    />
                  </TableCell>
                  <TableCell className="sticky left-8 z-10 bg-panel align-top shadow-[inset_-1px_0_0_0_var(--color-border)]">
                    <ModelIdentityCell model={model} view={view} />
                  </TableCell>
                  <TableCell className="align-top">
                    <div className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
                      <OperatorTypeBadge label={formatApiFamily(model.api_family ?? "")} intent="accent" preserveLabel />
                      {model.api_family === "openai" ? (
                        <span className="text-xs text-muted-foreground">
                          {model.openai_accepted_format === "chat_completions_only"
                            ? messages.routing.capabilityChatOnly
                            : model.openai_accepted_format === "responses_only"
                              ? messages.routing.capabilityResponsesOnly
                              : model.openai_accepted_format === "dual_native"
                                ? messages.routing.capabilityDual
                                : model.openai_image_operations
                                  ? messages.routing.capabilityImageOnly
                                  : (
                                      <OperatorMissingValue
                                        reason={messages.routing.capabilityUnknownReason}
                                      />
                                    )}
                        </span>
                      ) : null}
                    </div>
                  </TableCell>
                  <TableCell className="align-top">
                    {/* The switch is the state: a badge beside it restated the
                        same bit in a second vocabulary. */}
                    <span className="flex items-center gap-1.5">
                      <Switch
                        aria-label={copy.toggleModelAria(title)}
                        checked={model.is_enabled}
                        disabled={togglingModelIds.has(model.id)}
                        onCheckedChange={(checked) => void onSetEnabled(model, checked)}
                      />
                      {/* 关闭态只靠滑块位置太弱：状态必须能扫描到。 */}
                      {model.is_enabled ? null : (
                        <span className="text-xs text-muted-foreground">
                          {copy.disabledRowLabel}
                        </span>
                      )}
                    </span>
                  </TableCell>
                  <TableCell className="align-top">
                    {/* The whole cell links to detail: counts name the scale,
                        the first two (position,id) rows name the actual exits,
                        and the remainder is a pointer, not a summary. */}
                    {/* 链接名称必须是单元格里那几行出口文本本身：一个整格
                        aria-label 会把它们从读屏里整段抹掉。 */}
                    <Link
                      to="/route/models/$modelId"
                      params={{ modelId: String(model.id) }}
                      title={copy.targetsLinkAria(title)}
                      className="flex min-w-40 flex-col gap-0.5 underline-offset-2 hover:underline"
                    >
                      <ModelExitMappingCell model={model} />
                      <span className="sr-only">{copy.targetsLinkAriaSuffix}</span>
                    </Link>
                  </TableCell>
                  <TableCell className="align-top">
                    <div className="flex min-w-32 flex-col items-start gap-0.5">
                      {model.loadbalance_strategy ? (
                        <span className="truncate text-xs">{model.loadbalance_strategy.name}</span>
                      ) : (
                        <OperatorMissingValue className="text-xs" reason={copy.strategyMissingReason} />
                      )}
                      {/* 覆盖度是配置事实而不是运行观测：满屏绿点会让人以为
                          这些模型正在正常服务。只有「无法路由」保留运行态语气。 */}
                      {model.routing_summary ? (
                        model.routing_summary.coverage === "none" ? (
                          <OperatorStatusBadge
                            intent="failing"
                            preserveLabel
                            label={messages.routing.coverageNone}
                          />
                        ) : (
                          <OperatorTypeBadge
                            intent={
                              model.routing_summary.coverage === "partial"
                                ? "muted"
                                : "neutral"
                            }
                            preserveLabel
                            label={
                              model.routing_summary.coverage === "full"
                                ? messages.routing.coverageFull
                                : model.routing_summary.coverage === "partial"
                                  ? messages.routing.coveragePartial
                                  : messages.common.notApplicable
                            }
                          />
                        )
                      ) : null}
                      {truncated ? (
                        <OperatorStatusBadge
                          intent="degraded"
                          preserveLabel
                          label={copy.singleTruncated}
                          title={copy.singleTruncatedReason(formatNumber(enabledTargets ?? 0))}
                        />
                      ) : null}
                    </div>
                  </TableCell>
                  <TableCell className="align-top text-right">
                    <MetricValue
                      failed={metricsFailed}
                      loading={metricsLoading}
                      render={(value) => `${formatNumber(value, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}%`}
                      value={metrics?.success_rate}
                      missingReason={messages.modelDetail.roleMetricsNoDenominator}
                    />
                  </TableCell>
                  <TableCell className="align-top text-right">
                    <MetricValue
                      failed={metricsFailed}
                      loading={metricsLoading}
                      render={(value) => formatLatencyForDisplay(value)}
                      value={metrics?.p95_latency_ms}
                      missingReason={messages.modelDetail.roleMetricsNoLatencySample}
                      partialReason={
                        (metrics?.samples?.latency_missing_count ?? 0) > 0
                          ? copy.metricPartialSamples(
                              metrics?.samples?.latency_sample_count ?? 0,
                              metrics?.samples?.latency_missing_count ?? 0,
                            )
                          : undefined
                      }
                    />
                  </TableCell>
                  <TableCell className="align-top text-right">
                    <MetricValue
                      failed={metricsFailed}
                      loading={metricsLoading}
                      render={(value) => formatNumber(value)}
                      value={metrics?.request_count_24h}
                    />
                  </TableCell>
                  <TableCell className="align-top text-right">
                    {scope === "route_attempt" ? (
                      <OperatorMissingValue
                        className="text-xs"
                        reason={copy.routeAttemptCostUnavailable}
                      />
                    ) : (
                      <MetricValue
                        failed={metricsFailed}
                        loading={metricsLoading}
                        render={(value) => formatMoneyMicros(value, currencyState.currency.symbol, undefined, 2, 2, locale)}
                        value={spend}
                        missingReason={messages.modelDetail.roleMetricsNoTrustedCost}
                        partialReason={
                          (metrics?.samples?.cost_missing_count ?? 0) > 0
                            ? copy.metricPartialCost(
                                metrics?.samples?.cost_sample_count ?? 0,
                                metrics?.samples?.cost_missing_count ?? 0,
                              )
                            : undefined
                        }
                      />
                    )}
                  </TableCell>
                  <TableCell className="sticky right-0 z-10 bg-panel align-top text-right shadow-[inset_1px_0_0_0_var(--color-border)]">
                    <div className="flex items-center justify-end gap-1">
                      {/* The accessible name names the row, not just the
                          verb: several edit buttons share one page. */}
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className={cn(operationalRowActionsClassName, "gap-1")}
                        aria-label={`${messages.modelsUi.editModel}: ${title}`}
                        onClick={() => void onEdit(model)}
                      >
                        <Pencil data-icon="inline-start" />
                        {messages.common.edit}
                      </Button>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          {/* 溢出菜单的触发器常显（淡一点）：它藏起来时，
                              这一行看起来就是没有任何操作。 */}
                          <Button
                            type="button"
                            variant="outline"
                            size="icon-sm"
                            className="opacity-60 transition-opacity group-hover/row:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                            aria-label={copy.modelMoreActions(title)}
                          >
                            <MoreHorizontal />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem
                            onSelect={() =>
                              void navigate({ to: "/route/models/$modelId", params: { modelId: String(model.id) } })
                            }
                          >
                            {messages.modelsUi.viewModelDetails(title)}
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem variant="destructive" onSelect={() => setDeleteTarget(model)}>
                            <Trash2 />
                            {messages.settingsDialogs.delete}
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </div>
      {sortedModels.length > MODEL_PAGE_SIZES[0] ? (
      <OperationalTablePagination
        currentPageIndex={pageState.currentPageIndex}
        endIndex={pageState.endIndex}
        formatNumber={(value) => formatNumber(value)}
        hasNextPage={pageState.hasNextPage}
        hasPreviousPage={pageState.hasPreviousPage}
        nextLabel={tableCopy.nextPage}
        onGoToPage={(pageIndex) => onPageChange(pageIndex + 1)}
        onNextPage={() => onPageChange(pageState.currentPageIndex + 2)}
        onPreviousPage={() => onPageChange(pageState.currentPageIndex)}
        pageCount={pageState.totalPages}
        pageLabel={tableCopy.page}
        pageSize={{
          ariaLabel: tableCopy.pageSize,
          onChange: onPageSizeChange,
          options: MODEL_PAGE_SIZES,
          value: effectivePageSize,
        }}
        previousLabel={tableCopy.previousPage}
        resultsLabel={tableCopy.resultsRange}
        startIndex={pageState.startIndex}
        totalLabel={tableCopy.totalRows}
        totalRows={sortedModels.length}
        zeroLabel={tableCopy.zeroResults}
      />
      ) : null}

      {bulkDisableTarget ? (
        <OperatorDestructiveDialog
          open
          onOpenChange={(open) => {
            if (!open) setBulkDisableTarget(null)
          }}
          size="sm"
          title={copy.bulkDisableConfirmTitle(
            formatNumber(bulkDisableTarget.length),
          )}
          description={copy.bulkDisableConfirmDescription}
          cancelLabel={messages.settingsDialogs.cancel}
          confirmLabel={copy.bulkDisableConfirmAction(
            formatNumber(bulkDisableTarget.length),
          )}
          confirmTestId="bulk-disable-confirm"
          onConfirm={() => {
            const targets = bulkDisableTarget
            setBulkDisableTarget(null)
            void onSetManyEnabled(targets, false)
          }}
        >
          <ul className="flex max-h-48 flex-col gap-0.5 overflow-y-auto text-xs">
            {bulkDisableTarget.map((model) => (
              <li key={model.id} className="truncate font-mono">
                {modelTitle(model)}
              </li>
            ))}
          </ul>
        </OperatorDestructiveDialog>
      ) : null}
    </div>
  )
}

function SortHead({
  className,
  column,
  label,
  onSort,
  sort,
}: {
  className?: string
  column: ModelSortColumn
  label: string
  onSort: (column: ModelSortColumn) => void
  sort: OperationalSortState<ModelSortColumn>
}) {
  const active = sort.column === column
  return (
    <TableHead
      aria-sort={active ? (sort.direction === "asc" ? "ascending" : "descending") : "none"}
      className={cn("p-0", className)}
    >
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className={cn(
          "h-[var(--density-table-head-h)] w-full justify-start gap-1 rounded-none px-[var(--density-table-cell-px)] text-xs font-medium text-muted-foreground hover:text-foreground",
          active && "text-foreground",
        )}
        onClick={() => onSort(column)}
      >
        {label}
        <SortGlyph active={active} direction={sort.direction} />
      </Button>
    </TableHead>
  )
}

/**
 * 排序方向的字形承载着「当前按哪一列、哪个方向排」这个信息，
 * 所以它不能用 text-disabled（契约：禁用色永不承载信息），
 * 生效列还必须能一眼从其它列里挑出来。
 */
function SortGlyph({
  active,
  direction,
}: {
  active: boolean
  direction: "asc" | "desc"
}) {
  return (
    <span
      aria-hidden="true"
      className={active ? "text-primary" : "text-muted-foreground"}
    >
      {active ? (direction === "asc" ? "↑" : "↓") : "↕"}
    </span>
  )
}

export { MODEL_COLUMN_COUNT }
