import { useMemo } from "react"
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
import { cn, formatApiFamily } from "@/lib/utils"
import {
  OperatorEmptyState,
  OperatorMissingValue,
  OperatorStalenessBadge,
  OperatorStatusBadge,
  OperatorTypeBadge,
} from "@/shared/design-system"
import {
  OperationalTablePagination,
  operationalRowActionsClassName,
  paginateOperationalRows,
  sortOperationalRows,
  type OperationalSortDirection,
  type OperationalSortState,
  type OperationalSortValue,
} from "@/shared/table/operationalTable"
import { formatLatencyForDisplay } from "@/pages/model-detail/modelDetailMetricsAndPaths"
import type { ModelDerivedMetric } from "@/pages/models/modelTableContracts"
import { isSingleTruncated } from "./modelRoutingFlags"

const MODEL_PAGE_SIZES = [25, 50, 100] as const
const MODEL_COLUMN_COUNT = 10

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
  filtered: ManagedModelConfigListItem[]
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
  search: string
  selectedIds: Set<number>
  setDeleteTarget: (model: ManagedModelConfigListItem) => void
  sortBy: ModelSortColumn
  sortOrder: OperationalSortDirection
  togglingModelIds: Set<number>
}

function modelTitle(model: ManagedModelConfigListItem) {
  return model.display_name || model.model_id
}

function ModelIdentityCell({ model }: { model: ManagedModelConfigListItem }) {
  const { messages } = useLocale()
  const title = modelTitle(model)
  const showModelId = title !== model.model_id

  return (
    <div className="flex min-w-56 flex-col gap-0.5">
      <div className="flex min-w-0 items-center gap-1">
        <span className="truncate font-medium" title={title}>{title}</span>
        <CopyButton
          aria-label={messages.modelDetail.copyModelIdAria(model.model_id)}
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
    </div>
  )
}

/** A metric column head that names its own window and basis. */
function MetricHead({
  basis,
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
}) {
  const { messages } = useLocale()
  const copy = messages.modelsPage
  const active = sort.column === sortKey

  return (
    <TableHead
      aria-sort={active ? (sort.direction === "asc" ? "ascending" : "descending") : "none"}
      className="text-right"
      title={basis}
    >
      <div className="flex flex-col items-end gap-0.5">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className={cn("h-6 gap-1 px-1 text-xs font-medium text-muted-foreground hover:text-foreground", active && "text-foreground")}
          onClick={() => onSort(sortKey)}
        >
          {label}
          <span aria-hidden="true" className="text-text-disabled">
            {active ? (sort.direction === "asc" ? "↑" : "↓") : "↕"}
          </span>
        </Button>
        <span className="inline-flex items-center gap-1 font-mono text-[10px] text-muted-foreground">
          {window}
          <span aria-hidden="true" className="cursor-help text-text-disabled">
            ?
          </span>
        </span>
        <span className="sr-only">{basis}</span>
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
  render,
  value,
}: {
  failed: boolean
  loading: boolean
  render: (value: number) => string
  value: number | null | undefined
}) {
  const { messages } = useLocale()
  const copy = messages.modelsPage

  if (loading && value == null) return <Skeleton className="ml-auto h-4 w-12" />
  if (failed && value == null) {
    return (
      <span className="font-mono text-xs font-medium text-failing" title={copy.metricsUnavailableReason}>
        {copy.metricsUnavailable}
      </span>
    )
  }
  if (value == null) return <OperatorMissingValue className="text-xs" />
  return <span className="font-mono text-xs tabular-nums">{render(value)}</span>
}

export function ModelsTable({
  filtered,
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
  search,
  selectedIds,
  setDeleteTarget,
  sortBy,
  sortOrder,
  togglingModelIds,
}: Props) {
  const { currencyState } = useReportingCurrencyContext()
  const { formatNumber, locale, messages } = useLocale()
  const copy = messages.modelsPage
  const tableCopy = messages.operationalTable
  const navigate = useNavigate()
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
      if (column === "targets") return model.access_targets.length
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

  if (filtered.length === 0) {
    return (
      <OperatorEmptyState
        icon={<Server />}
        title={search ? messages.modelsUi.noModelsMatchSearch : messages.modelsUi.noModelsConfigured}
        description={search ? messages.modelsUi.tryDifferentModelNameOrId : messages.modelsUi.createFirstModel}
        action={
          search ? (
            <Button variant="outline" onClick={onClearFilters}>{copy.clearFilters}</Button>
          ) : (
            // Same entry point as the page header: one way to create a model.
            <Button onClick={onCreate}><Plus data-icon="inline-start" />{copy.newModel}</Button>
          )
        }
      />
    )
  }

  return (
    <div data-testid="models-table" data-table-density="compact">
      {selectedModels.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2 border-t border-border bg-inset px-[var(--density-card-pad-x)] py-2 text-xs">
          <span className="text-muted-foreground">{copy.selectionCount(formatNumber(selectedModels.length))}</span>
          <Button type="button" variant="outline" size="sm" onClick={() => void onSetManyEnabled(selectedModels, true)}>
            {copy.bulkEnable}
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={() => void onSetManyEnabled(selectedModels, false)}>
            {copy.bulkDisable}
          </Button>
          <Button type="button" variant="ghost" size="sm" onClick={() => onSelectionChange(new Set())}>
            {copy.bulkClear}
          </Button>
        </div>
      ) : null}

      <div className="overflow-x-auto border-t border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-8">
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
              <SortHead column="name" label={copy.columnModel} onSort={updateSort} sort={sort} />
              <SortHead column="api_family" label={copy.columnApiFamily} onSort={updateSort} sort={sort} />
              <SortHead column="status" label={copy.columnStatus} onSort={updateSort} sort={sort} />
              <SortHead column="targets" label={copy.columnTargets} onSort={updateSort} sort={sort} />
              <SortHead column="strategy" label={copy.columnStrategy} onSort={updateSort} sort={sort} />
              <MetricHead
                basis={copy.successBasis}
                label={copy.columnSuccess}
                onSort={updateSort}
                sort={sort}
                sortKey="success"
                stale={metricsFailed}
                window={copy.window24h}
              />
              <MetricHead
                basis={copy.p95Basis}
                label={copy.columnP95}
                onSort={updateSort}
                sort={sort}
                sortKey="p95"
                stale={metricsFailed}
                window={copy.window24h}
              />
              <MetricHead
                basis={copy.requestsBasis}
                label={copy.columnRequests}
                onSort={updateSort}
                sort={sort}
                sortKey="requests"
                stale={metricsFailed}
                window={copy.window24h}
              />
              <MetricHead
                basis={copy.spendBasis}
                label={copy.columnSpend}
                onSort={updateSort}
                sort={sort}
                sortKey="spend"
                stale={metricsFailed}
                window={copy.window30d}
              />
              <TableHead className="text-right">{messages.pricingTemplatesUi.actions}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {pageState.pageRows.map((model) => {
              const title = modelTitle(model)
              const metrics = modelMetrics24h[model.id]
              const spend = modelSpend30dMicros[model.id] ?? null
              const enabledTargets = model.access_targets.filter((target) => target.is_enabled).length
              const truncated = isSingleTruncated(model)

              return (
                <TableRow key={model.id} className="group/row" data-testid={`models-table-row-${model.id}`}>
                  <TableCell className="align-top">
                    <Checkbox
                      aria-label={copy.selectModelAria(title)}
                      checked={selectedIds.has(model.id)}
                      onCheckedChange={() => toggleRow(model.id)}
                    />
                  </TableCell>
                  <TableCell className="align-top"><ModelIdentityCell model={model} /></TableCell>
                  <TableCell className="align-top">
                    <div className="flex flex-col items-start gap-0.5">
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
                                  : <OperatorMissingValue />}
                        </span>
                      ) : null}
                    </div>
                  </TableCell>
                  <TableCell className="align-top">
                    {/* The switch is the state: a badge beside it restated the
                        same bit in a second vocabulary. */}
                    <Switch
                      aria-label={copy.toggleModelAria(title)}
                      checked={model.is_enabled}
                      disabled={togglingModelIds.has(model.id)}
                      onCheckedChange={(checked) => void onSetEnabled(model, checked)}
                    />
                  </TableCell>
                  <TableCell className="align-top">
                    {/* The whole cell links to detail: counts alone never answer
                        "which target", so the first one is named here. */}
                    <Link
                      to="/route/models/$modelId"
                      params={{ modelId: String(model.id) }}
                      aria-label={copy.targetsLinkAria(title)}
                      className="flex min-w-40 flex-col gap-0.5 underline-offset-2 hover:underline"
                    >
                      {model.access_targets.length === 0 ? (
                        <OperatorStatusBadge intent="failing" preserveLabel label={copy.targetsNone} />
                      ) : (
                        <>
                          <span className="font-mono text-xs tabular-nums">
                            {copy.targetsCount(
                              formatNumber(enabledTargets),
                              formatNumber(model.access_targets.length),
                            )}
                          </span>
                          <span className="truncate text-xs text-muted-foreground">
                            {model.access_targets[0]?.connection?.name
                              ?? model.access_targets[0]?.target_model?.model_id
                              ?? model.access_targets[0]?.target_model_id
                              ?? ""}
                          </span>
                        </>
                      )}
                    </Link>
                  </TableCell>
                  <TableCell className="align-top">
                    <div className="flex min-w-32 flex-col items-start gap-0.5">
                      {model.loadbalance_strategy ? (
                        <span className="truncate text-xs">{model.loadbalance_strategy.name}</span>
                      ) : (
                        <OperatorMissingValue className="text-xs" reason={copy.strategyMissingReason} />
                      )}
                      {truncated ? (
                        <OperatorStatusBadge
                          intent="degraded"
                          preserveLabel
                          label={copy.singleTruncated}
                          title={copy.singleTruncatedReason(formatNumber(enabledTargets))}
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
                    />
                  </TableCell>
                  <TableCell className="align-top text-right">
                    <MetricValue
                      failed={metricsFailed}
                      loading={metricsLoading}
                      render={(value) => formatLatencyForDisplay(value)}
                      value={metrics?.p95_latency_ms}
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
                    <MetricValue
                      failed={metricsFailed}
                      loading={metricsLoading}
                      render={(value) => formatMoneyMicros(value, currencyState.currency.symbol, undefined, 2, 6, locale)}
                      value={spend}
                    />
                  </TableCell>
                  <TableCell className="align-top text-right">
                    <div className={cn(operationalRowActionsClassName, "gap-1")}>
                      {/* The accessible name names the row, not just the
                          verb: several edit buttons share one page. */}
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        aria-label={`${messages.modelsUi.editModel}: ${title}`}
                        onClick={() => void onEdit(model)}
                      >
                        <Pencil data-icon="inline-start" />
                        {messages.common.edit}
                      </Button>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button
                            type="button"
                            variant="outline"
                            size="icon-sm"
                            aria-label={messages.modelsUi.viewModelDetails(title)}
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
    </div>
  )
}

function SortHead({
  column,
  label,
  onSort,
  sort,
}: {
  column: ModelSortColumn
  label: string
  onSort: (column: ModelSortColumn) => void
  sort: OperationalSortState<ModelSortColumn>
}) {
  const active = sort.column === column
  return (
    <TableHead aria-sort={active ? (sort.direction === "asc" ? "ascending" : "descending") : "none"}>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className={cn(
          "h-6 gap-1 px-1 text-xs font-medium text-muted-foreground hover:text-foreground",
          active && "text-foreground",
        )}
        onClick={() => onSort(column)}
      >
        {label}
        <span aria-hidden="true" className="text-text-disabled">
          {active ? (sort.direction === "asc" ? "↑" : "↓") : "↕"}
        </span>
      </Button>
    </TableHead>
  )
}

export { MODEL_COLUMN_COUNT }
