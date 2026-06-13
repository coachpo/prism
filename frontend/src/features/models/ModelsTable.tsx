import { useMemo, useState } from "react"
import { Eye, Pencil, Plus, Server, Trash2 } from "lucide-react"
import { useNavigate } from "react-router-dom"

import { CopyButton } from "@/components/CopyButton"
import { EmptyState } from "@/components/EmptyState"
import { IconActionButton, IconActionGroup } from "@/components/IconActionGroup"
import { StatusBadge, TypeBadge } from "@/components/StatusBadge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useReportingCurrencyContext } from "@/context/ReportingCurrencyContext"
import { useLocale } from "@/i18n/useLocale"
import type { ManagedModelConfigListItem } from "@/lib/api/management"
import { formatMoneyMicros } from "@/lib/costing"
import { formatApiFamily } from "@/lib/utils"
import {
  OperationalTablePagination,
  SortableTableHead,
  getNextOperationalSort,
  paginateOperationalRows,
  sortOperationalRows,
  type OperationalSortState,
  type OperationalSortValue,
} from "@/shared/table/operationalTable"
import { formatLatencyForDisplay, getModelDetailPath } from "@/pages/model-detail/modelDetailMetricsAndPaths"
import type { ModelDerivedMetric } from "@/pages/models/modelTableContracts"

const MODEL_PAGE_SIZE = 25

type ModelSortColumn = "name" | "api_family" | "status" | "targets" | "success" | "spend"

type Props = {
  filtered: ManagedModelConfigListItem[]
  handleOpenDialog: (model?: ManagedModelConfigListItem) => void
  metricsLoading: boolean
  modelMetrics24h: Record<number, ModelDerivedMetric>
  modelSpend30dMicros: Record<number, number>
  search: string
  setDeleteTarget: (model: ManagedModelConfigListItem) => void
}

function modelTitle(model: ManagedModelConfigListItem) {
  return model.display_name || model.model_id
}

function targetSummary(model: ManagedModelConfigListItem, fallback: string) {
  const firstTarget = [...model.access_targets].sort((left, right) => left.position - right.position)[0]
  if (!firstTarget) return fallback
  if (firstTarget.target_type === "model") return firstTarget.target_model?.display_name || firstTarget.target_model_id || fallback
  return firstTarget.connection?.name || firstTarget.connection?.endpoint?.name || fallback
}

function getSortValue(
  model: ManagedModelConfigListItem,
  column: ModelSortColumn,
): OperationalSortValue {
  if (column === "name") return modelTitle(model)
  if (column === "api_family") return model.api_family
  if (column === "status") return model.is_enabled
  if (column === "targets") return model.access_targets.length
  return model.id
}

function ModelIdentityCell({ model }: { model: ManagedModelConfigListItem }) {
  const { messages } = useLocale()
  const title = modelTitle(model)
  const showModelId = title !== model.model_id

  return (
    <div className="flex min-w-64 flex-col gap-1">
      <div className="flex min-w-0 items-center gap-2">
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

function ModelTelemetryCell({ metrics, loading }: { metrics?: ModelDerivedMetric; loading: boolean }) {
  const { formatNumber, messages } = useLocale()
  const successRate = metrics?.success_rate ?? null
  const successLabel = loading && !metrics
    ? `... ${messages.modelsUi.successLabel}`
    : successRate === null
      ? `- ${messages.modelsUi.successLabel}`
      : `${formatNumber(successRate, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}% ${messages.modelsUi.successLabel}`

  return (
    <div className="flex min-w-32 flex-col gap-1 text-xs text-muted-foreground">
      <span className="font-medium text-foreground">{successLabel}</span>
      <span>{formatLatencyForDisplay(metrics?.p95_latency_ms ?? null)} P95</span>
      <span>{metrics?.request_count_24h ? formatNumber(metrics.request_count_24h) : "-"} {messages.modelsUi.requestsShort}</span>
    </div>
  )
}

export function ModelsTable({
  filtered,
  handleOpenDialog,
  metricsLoading,
  modelMetrics24h,
  modelSpend30dMicros,
  search,
  setDeleteTarget,
}: Props) {
  const { currencyState } = useReportingCurrencyContext()
  const { formatNumber, locale, messages } = useLocale()
  const navigate = useNavigate()
  const [sort, setSort] = useState<OperationalSortState<ModelSortColumn>>({ column: "name", direction: "asc" })
  const [pageIndex, setPageIndex] = useState(0)

  const sortedModels = useMemo(() => {
    if (sort.column === "success") {
      return sortOperationalRows(filtered, sort, (model) => modelMetrics24h[model.id]?.success_rate ?? null, locale)
    }
    if (sort.column === "spend") {
      return sortOperationalRows(filtered, sort, (model) => modelSpend30dMicros[model.id] ?? 0, locale)
    }
    return sortOperationalRows(filtered, sort, (model, column) => getSortValue(model, column), locale)
  }, [filtered, locale, modelMetrics24h, modelSpend30dMicros, sort])

  const page = paginateOperationalRows(sortedModels, pageIndex, MODEL_PAGE_SIZE)
  const updateSort = (column: ModelSortColumn) => {
    setSort((current) => getNextOperationalSort(current, column))
    setPageIndex(0)
  }

  if (filtered.length === 0) {
    return (
      <EmptyState
        icon={<Server className="size-6" />}
        title={search ? messages.modelsUi.noModelsMatchSearch : messages.modelsUi.noModelsConfigured}
        description={search ? messages.modelsUi.tryDifferentModelNameOrId : messages.modelsUi.createFirstModel}
        action={!search ? <Button size="sm" onClick={() => handleOpenDialog()}><Plus data-icon="inline-start" />{messages.modelsPage.newModel}</Button> : undefined}
      />
    )
  }

  return (
    <div data-testid="models-table" data-table-density="compact">
      <div className="overflow-hidden border-t border-border/70">
        <Table>
          <TableHeader>
            <TableRow>
              <SortableTableHead sortKey="name" sort={sort} onSort={updateSort}>{messages.modelDetail.modelIdLabel}</SortableTableHead>
              <SortableTableHead sortKey="api_family" sort={sort} onSort={updateSort}>API</SortableTableHead>
              <SortableTableHead sortKey="status" sort={sort} onSort={updateSort}>Status</SortableTableHead>
              <SortableTableHead sortKey="targets" sort={sort} onSort={updateSort}>{messages.modelDetail.targets("#")}</SortableTableHead>
              <SortableTableHead sortKey="success" sort={sort} onSort={updateSort}>{messages.modelsUi.successLabel}</SortableTableHead>
              <SortableTableHead sortKey="spend" sort={sort} onSort={updateSort} align="right">{messages.modelsUi.spendShort}</SortableTableHead>
              <th className="h-[var(--density-table-head-h)] px-[var(--density-table-cell-px)] text-right text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">
                {messages.pricingTemplatesUi.actions}
              </th>
            </TableRow>
          </TableHeader>
          <TableBody>
            {page.pageRows.map((model) => {
              const title = modelTitle(model)
              const metrics = modelMetrics24h[model.id]
              const spend = modelSpend30dMicros[model.id] ?? 0
              return (
                <TableRow key={model.id} data-testid={`models-table-row-${model.id}`}>
                  <TableCell><ModelIdentityCell model={model} /></TableCell>
                  <TableCell><TypeBadge label={formatApiFamily(model.api_family ?? "")} intent="info" preserveLabel /></TableCell>
                  <TableCell><StatusBadge label={model.is_enabled ? messages.modelDetail.enabled : messages.modelDetail.disabled} intent={model.is_enabled ? "success" : "danger"} /></TableCell>
                  <TableCell>
                    <div className="flex min-w-44 flex-col gap-1 text-xs text-muted-foreground">
                      <span className="font-medium text-foreground">{messages.modelDetail.targets(formatNumber(model.access_targets.length))}</span>
                      <span className="truncate">{targetSummary(model, messages.modelsUi.needsTarget)}</span>
                    </div>
                  </TableCell>
                  <TableCell><ModelTelemetryCell metrics={metrics} loading={metricsLoading} /></TableCell>
                  <TableCell className="text-right font-mono text-xs font-medium">
                    {formatMoneyMicros(spend, currencyState.currency.symbol, undefined, 2, 6, locale as "en" | "zh-CN")}
                  </TableCell>
                  <TableCell className="text-right">
                    <IconActionGroup className="justify-end">
                      <IconActionButton aria-label={`${messages.modelsUi.editModel}: ${title}`} onClick={() => handleOpenDialog(model)}><Pencil /></IconActionButton>
                      <IconActionButton aria-label={messages.modelsUi.viewModelDetails(title)} onClick={() => navigate(getModelDetailPath(model))}><Eye /></IconActionButton>
                      <IconActionButton destructive aria-label={messages.modelsUi.deleteModelDescription(title)} onClick={() => setDeleteTarget(model)}><Trash2 /></IconActionButton>
                    </IconActionGroup>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </div>
      <OperationalTablePagination
        currentPageIndex={page.currentPageIndex}
        endIndex={page.endIndex}
        formatNumber={(value) => formatNumber(value)}
        hasNextPage={page.hasNextPage}
        hasPreviousPage={page.hasPreviousPage}
        nextLabel={messages.requestLogs.nextPage}
        onNextPage={() => setPageIndex(page.currentPageIndex + 1)}
        onPreviousPage={() => setPageIndex(page.currentPageIndex - 1)}
        previousLabel={messages.requestLogs.previousPage}
        resultsLabel={messages.requestLogs.resultsRange}
        startIndex={page.startIndex}
        totalRows={sortedModels.length}
        zeroLabel={messages.requestLogs.zeroResults}
      />
    </div>
  )
}
