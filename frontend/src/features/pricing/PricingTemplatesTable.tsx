import { Fragment, useMemo, useState } from "react"
import { ChevronDown, ChevronRight, Coins, MoreHorizontal, Pencil, Trash2 } from "lucide-react"
import { Link } from "@tanstack/react-router"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader } from "@/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useLocale } from "@/i18n/useLocale"
import { useTimezone } from "@/hooks/useTimezone"
import type {
  PricingTemplate,
  PricingTemplateConnectionUsageItem,
  PricingTemplateRevision,
} from "@/lib/types"
import { cn } from "@/lib/utils"
import {
  OperatorEmptyState,
  OperatorCallout,
  OperatorErrorState,
  OperatorInsetPanel,
  OperatorMissingValue,
  OperatorRetryButton,
  OperatorStalenessBadge,
  OperatorStatusBadge,
  OperatorSearchInput,
  OperatorValueBadge,
} from "@/shared/design-system"
import {
  OperationalTablePagination,
  OperationalTableSkeletonRows,
  SortableTableHead,
  getNextOperationalSort,
  operationalRowActionsClassName,
  paginateOperationalRows,
  sortOperationalRows,
  type OperationalSortState,
  type OperationalSortValue,
} from "@/shared/table/operationalTable"
import { normalizeTemplatePrice } from "./pricingSchemas"
import { isRecentlyChanged, totalReferences, type PricingListFacts } from "./usePricingListFacts"

const PRICING_PAGE_SIZES = [10, 25, 50] as const
const PRICING_COLUMN_COUNT = 12

type PricingSortColumn = "name" | "currency" | "input" | "output" | "version" | "updated"
export type PricingFilter = "all" | "incomplete" | "unreferenced" | "recently_changed"
type DetailView = "usage" | "history"

interface PricingTemplatesTableProps {
  detailHistory: PricingTemplateRevision[]
  detailHistoryLoading: boolean
  detailUsage: PricingTemplateConnectionUsageItem[]
  detailUsageError: string | null
  detailUsageLoading: boolean
  facts: PricingListFacts
  filter: PricingFilter
  onDelete: (template: PricingTemplate) => Promise<void>
  onEdit: (template: PricingTemplate) => Promise<void>
  onFilterChange: (filter: PricingFilter) => void
  onLoadHistory: (template: PricingTemplate) => Promise<void>
  onLoadUsage: (template: PricingTemplate) => Promise<void>
  onRetry: () => void
  pricingTemplateError: string | null
  pricingTemplatePreparingEditId: number | null
  pricingTemplates: PricingTemplate[]
  pricingTemplatesLoading: boolean
}

function priceSortValue(value: string | null | undefined) {
  const parsed = Number(normalizeTemplatePrice(value))
  return Number.isFinite(parsed) ? parsed : null
}

function getSortValue(template: PricingTemplate, column: PricingSortColumn): OperationalSortValue {
  if (column === "name") return template.name
  if (column === "currency") return template.pricing_currency_code
  if (column === "input") return priceSortValue(template.input_price)
  if (column === "output") return priceSortValue(template.output_price)
  if (column === "version") return template.version
  return template.updated_at
}

function matchesPricingFilter(template: PricingTemplate, query: string) {
  const normalized = query.trim().toLowerCase()
  if (!normalized) return true
  return [template.name, template.description, template.pricing_currency_code, template.pricing_unit]
    .filter((value): value is string => Boolean(value))
    .some((value) => value.toLowerCase().includes(normalized))
}

/**
 * A specialty rate has three distinct states that must not look alike:
 * a real number, an unconfigured rate (em dash plus an idle badge), and a
 * value the read never produced.
 */
function RateCell({
  symbol,
  value,
  specialty,
}: {
  symbol?: string
  value: string | null | undefined
  specialty?: boolean
}) {
  const { messages } = useLocale()
  const copy = messages.pricingTemplatesUi
  const normalized = normalizeTemplatePrice(value)

  if (normalized === "") {
    if (!specialty) return <OperatorMissingValue className="text-xs" />
    return (
      <span className="inline-flex items-center justify-end gap-1">
        <OperatorMissingValue className="text-xs" reason={copy.rateUnconfiguredReason} />
        <OperatorStatusBadge intent="idle" preserveLabel label={copy.rateUnconfigured} />
      </span>
    )
  }

  return (
    <span className="font-mono text-xs tabular-nums">
      {/* A one-character symbol hugs the number ($1.50); a currency code needs
          a gap so it does not read as part of the digits (USD 1.50). */}
      {symbol ? (
        <span className={cn("text-muted-foreground", symbol.length > 1 && "mr-1")}>{symbol}</span>
      ) : null}
      {normalized}
    </span>
  )
}

export function PricingTemplatesTable({
  detailHistory,
  detailHistoryLoading,
  detailUsage,
  detailUsageError,
  detailUsageLoading,
  facts,
  filter,
  onDelete,
  onEdit,
  onFilterChange,
  onLoadHistory,
  onLoadUsage,
  onRetry,
  pricingTemplateError,
  pricingTemplatePreparingEditId,
  pricingTemplates,
  pricingTemplatesLoading,
}: PricingTemplatesTableProps) {
  const { formatNumber, locale, messages } = useLocale()
  const { format: formatTime } = useTimezone()
  const copy = messages.pricingTemplatesUi
  const tableCopy = messages.operationalTable
  const [query, setQuery] = useState("")
  // Newest change first: on a pricing table the question is almost always
  // "what moved recently".
  const [sort, setSort] = useState<OperationalSortState<PricingSortColumn>>({ column: "updated", direction: "desc" })
  const [pageIndex, setPageIndex] = useState(0)
  const [pageSize, setPageSize] = useState<number>(PRICING_PAGE_SIZES[0])
  const [expandedId, setExpandedId] = useState<number | null>(null)
  const [detailView, setDetailView] = useState<DetailView>("usage")

  const filteredTemplates = useMemo(
    () =>
      pricingTemplates.filter((template) => {
        if (!matchesPricingFilter(template, query)) return false
        const item = facts.byId.get(template.id)
        if (filter === "incomplete") return item?.configuration_status === "incomplete"
        if (filter === "unreferenced") return totalReferences(item) === 0
        if (filter === "recently_changed") return isRecentlyChanged(template.updated_at)
        return true
      }),
    [facts.byId, filter, pricingTemplates, query],
  )
  const sortedTemplates = useMemo(
    () => sortOperationalRows(filteredTemplates, sort, getSortValue, locale),
    [filteredTemplates, locale, sort],
  )
  const page = paginateOperationalRows(sortedTemplates, pageIndex, pageSize)
  const updateSort = (column: PricingSortColumn) => {
    setSort((current) => getNextOperationalSort(current, column))
    setPageIndex(0)
  }

  const toggleRow = async (template: PricingTemplate, view: DetailView) => {
    if (expandedId === template.id && detailView === view) {
      setExpandedId(null)
      return
    }
    setExpandedId(template.id)
    setDetailView(view)
    if (view === "usage") await onLoadUsage(template)
    else await onLoadHistory(template)
  }

  return (
    <Card className="operator-table-shell gap-0 overflow-hidden" data-testid="pricing-templates-table">
      <CardHeader className="border-b pb-3">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <p className="flex items-center gap-2 text-xs text-muted-foreground" data-testid="pricing-templates-summary">
            <Coins aria-hidden="true" className="size-3.5" />
            {copy.tableSummary(formatNumber(pricingTemplates.length))}
            {facts.failed ? (
              <OperatorStalenessBadge
                label={copy.referencesUnavailable}
                reason={copy.referencesUnavailableReason}
              />
            ) : null}
          </p>
          <div className="flex items-center gap-2">
            {filter !== "all" ? (
              <Button type="button" variant="outline" size="sm" onClick={() => onFilterChange("all")}>
                {copy.clearFilters}
              </Button>
            ) : null}
            <OperatorSearchInput
              aria-label={copy.filterTemplates}
              className="h-[var(--density-control-h-sm)]"
              placeholder={copy.filterTemplates}
              value={query}
              onChange={(event) => {
                setQuery(event.target.value)
                setPageIndex(0)
              }}
              wrapperClassName="sm:w-64"
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="p-0">
        {pricingTemplateError && pricingTemplates.length > 0 ? (
          <OperatorCallout
            intent="warning"
            title={messages.pricingTemplatesData.loadFailed}
            description={pricingTemplateError}
            action={<OperatorRetryButton onClick={onRetry}>{messages.common.retry}</OperatorRetryButton>}
            className="m-3"
          />
        ) : null}

        {/* Table already ships its own overflow-x-auto container; a second one
            here just nests two scrollers that can never both scroll. */}
        <Table>
          <TableHeader>
            {/* The per-1M-token unit is stated once, on the rate group. */}
            <TableRow>
              <TableHead className="w-8" />
              <TableHead colSpan={3}>{copy.groupIdentity}</TableHead>
              <TableHead colSpan={5} className="text-center">
                <span className="inline-flex items-center gap-1">
                  {copy.groupRates}
                  <span className="font-normal text-muted-foreground">{copy.rateUnitPerMillion}</span>
                </span>
              </TableHead>
              <TableHead colSpan={2}>{copy.groupUsage}</TableHead>
              <TableHead />
            </TableRow>
            <TableRow>
              <TableHead className="w-8" />
              <SortableTableHead sortKey="name" sort={sort} onSort={updateSort}>
                {messages.settingsDialogs.name}
              </SortableTableHead>
              <SortableTableHead sortKey="currency" sort={sort} onSort={updateSort}>
                {copy.columnCurrency}
              </SortableTableHead>
              <SortableTableHead sortKey="version" sort={sort} onSort={updateSort}>
                {copy.columnVersion}
              </SortableTableHead>
              <SortableTableHead sortKey="input" sort={sort} onSort={updateSort} align="right">
                {copy.rateInput}
              </SortableTableHead>
              <SortableTableHead sortKey="output" sort={sort} onSort={updateSort} align="right">
                {copy.rateOutput}
              </SortableTableHead>
              <TableHead className="text-right">{copy.rateCachedInput}</TableHead>
              <TableHead className="text-right">{copy.rateCacheCreation}</TableHead>
              <TableHead className="text-right">{copy.rateReasoning}</TableHead>
              <TableHead>{copy.columnReferences}</TableHead>
              <SortableTableHead sortKey="updated" sort={sort} onSort={updateSort}>
                {copy.columnUpdatedAt}
              </SortableTableHead>
              <TableHead className="text-right">{copy.actions}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {pricingTemplatesLoading ? <OperationalTableSkeletonRows columns={PRICING_COLUMN_COUNT} rows={4} /> : null}

            {!pricingTemplatesLoading
              ? page.pageRows.map((template) => {
                  const isPreparingEdit = pricingTemplatePreparingEditId === template.id
                  const item = facts.byId.get(template.id)
                  const references = totalReferences(item)
                  const expanded = expandedId === template.id

                  return (
                    <Fragment key={template.id}>
                      <TableRow className="group/row" data-testid={`pricing-template-row-${template.id}`}>
                        <TableCell className="align-top">
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-sm"
                            aria-expanded={expanded}
                            aria-label={expanded ? copy.collapseRow(template.name) : copy.expandRow(template.name)}
                            onClick={() => void toggleRow(template, detailView)}
                          >
                            {expanded ? <ChevronDown /> : <ChevronRight />}
                          </Button>
                        </TableCell>
                        <TableCell className="align-top">
                          <div className="flex min-w-48 flex-col gap-0.5">
                            <span className="font-medium">{template.name}</span>
                            {template.description ? (
                              <span className="truncate text-xs text-muted-foreground">{template.description}</span>
                            ) : null}
                          </div>
                        </TableCell>
                        <TableCell className="align-top">
                          <OperatorValueBadge label={template.pricing_currency_code} className="text-xs" />
                        </TableCell>
                        <TableCell className="align-top">
                          <OperatorValueBadge label={`v${template.version}`} className="text-xs" />
                        </TableCell>
                        <TableCell className="align-top text-right">
                          <RateCell symbol={template.active_currency_symbol} value={template.input_price} />
                        </TableCell>
                        <TableCell className="align-top text-right">
                          <RateCell symbol={template.active_currency_symbol} value={template.output_price} />
                        </TableCell>
                        <TableCell className="align-top text-right">
                          <RateCell specialty symbol={template.active_currency_symbol} value={template.cached_input_price} />
                        </TableCell>
                        <TableCell className="align-top text-right">
                          <RateCell specialty symbol={template.active_currency_symbol} value={template.cache_creation_price} />
                        </TableCell>
                        <TableCell className="align-top text-right">
                          <RateCell specialty symbol={template.active_currency_symbol} value={template.reasoning_price} />
                        </TableCell>
                        <TableCell className="align-top">
                          {facts.loading && !item ? (
                            <Skeleton className="h-4 w-24" />
                          ) : facts.failed && !item ? (
                            <span className="text-xs font-medium text-failing" title={copy.referencesUnavailableReason}>
                              {copy.referencesUnavailable}
                            </span>
                          ) : item ? (
                            <span className="font-mono text-xs tabular-nums text-muted-foreground">
                              {references === 0
                                ? copy.referencesNone
                                : copy.referencesSummary(
                                    formatNumber(item.model_reference_count),
                                    formatNumber(item.endpoint_reference_count),
                                    formatNumber(item.terminal_target_reference_count),
                                  )}
                            </span>
                          ) : (
                            <OperatorMissingValue className="text-xs" />
                          )}
                        </TableCell>
                        <TableCell className="align-top">
                          <span className="font-mono text-xs tabular-nums">{formatTime(template.updated_at)}</span>
                        </TableCell>
                        <TableCell className="align-top text-right">
                          <div className={cn(operationalRowActionsClassName, "gap-1")}>
                            <Button
                              type="button"
                              variant="outline"
                              size="sm"
                              disabled={isPreparingEdit}
                              aria-label={`${messages.loadbalanceStrategiesTable.edit} ${template.name}`}
                              onClick={() => void onEdit(template)}
                            >
                              <Pencil data-icon="inline-start" />
                              {messages.loadbalanceStrategiesTable.edit}
                            </Button>
                            <DropdownMenu>
                              <DropdownMenuTrigger asChild>
                                <Button type="button" variant="outline" size="icon-sm" aria-label={copy.actions}>
                                  <MoreHorizontal />
                                </Button>
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="end">
                                <DropdownMenuItem onSelect={() => void toggleRow(template, "usage")}>
                                  {copy.detailViewUsage}
                                </DropdownMenuItem>
                                <DropdownMenuItem onSelect={() => void toggleRow(template, "history")}>
                                  {copy.detailViewHistory}
                                </DropdownMenuItem>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem variant="destructive" onSelect={() => void onDelete(template)}>
                                  <Trash2 />
                                  {messages.settingsDialogs.delete}
                                </DropdownMenuItem>
                              </DropdownMenuContent>
                            </DropdownMenu>
                          </div>
                        </TableCell>
                      </TableRow>

                      {expanded ? (
                        <TableRow>
                          <TableCell colSpan={PRICING_COLUMN_COUNT} className="bg-inset">
                            <div className="flex flex-col gap-3">
                              <div className="flex items-center gap-1">
                                <Button
                                  type="button"
                                  size="sm"
                                  variant={detailView === "usage" ? "secondary" : "ghost"}
                                  onClick={() => void toggleRow(template, "usage")}
                                >
                                  {copy.detailViewUsage}
                                </Button>
                                <Button
                                  type="button"
                                  size="sm"
                                  variant={detailView === "history" ? "secondary" : "ghost"}
                                  onClick={() => void toggleRow(template, "history")}
                                >
                                  {copy.detailViewHistory}
                                </Button>
                              </div>

                              {detailView === "usage" ? (
                                <UsagePanel
                                  error={detailUsageError}
                                  loading={detailUsageLoading}
                                  rows={detailUsage}
                                  onRetry={() => void onLoadUsage(template)}
                                />
                              ) : (
                                <HistoryPanel loading={detailHistoryLoading} revisions={detailHistory} />
                              )}
                            </div>
                          </TableCell>
                        </TableRow>
                      ) : null}
                    </Fragment>
                  )
                })
              : null}
          </TableBody>
        </Table>

        {!pricingTemplatesLoading && pricingTemplateError && pricingTemplates.length === 0 ? (
          <div className="p-3">
            <OperatorErrorState
              title={messages.pricingTemplatesData.loadFailed}
              description={pricingTemplateError}
              action={<OperatorRetryButton onClick={onRetry}>{messages.common.retry}</OperatorRetryButton>}
            />
          </div>
        ) : null}

        {!pricingTemplatesLoading && !pricingTemplateError && pricingTemplates.length === 0 ? (
          <div className="p-3">
            <OperatorEmptyState title={copy.noTemplatesConfigured} description={copy.description} />
          </div>
        ) : null}

        {!pricingTemplatesLoading && pricingTemplates.length > 0 && sortedTemplates.length === 0 ? (
          <div className="p-3">
            <OperatorEmptyState
              title={copy.noTemplatesMatchFilters}
              description={copy.noTemplatesMatchFiltersDescription}
              action={
                <Button
                  variant="outline"
                  onClick={() => {
                    setQuery("")
                    onFilterChange("all")
                  }}
                >
                  {copy.clearFilters}
                </Button>
              }
            />
          </div>
        ) : null}

        {!pricingTemplatesLoading && sortedTemplates.length > 0 ? (
          <OperationalTablePagination
            currentPageIndex={page.currentPageIndex}
            endIndex={page.endIndex}
            formatNumber={(value) => formatNumber(value)}
            hasNextPage={page.hasNextPage}
            hasPreviousPage={page.hasPreviousPage}
            nextLabel={tableCopy.nextPage}
            onGoToPage={setPageIndex}
            onNextPage={() => setPageIndex(page.currentPageIndex + 1)}
            onPreviousPage={() => setPageIndex(page.currentPageIndex - 1)}
            pageCount={page.totalPages}
            pageLabel={tableCopy.page}
            pageSize={{
              ariaLabel: tableCopy.pageSize,
              onChange: (value) => {
                setPageSize(value)
                setPageIndex(0)
              },
              options: PRICING_PAGE_SIZES,
              value: pageSize,
            }}
            previousLabel={tableCopy.previousPage}
            resultsLabel={tableCopy.resultsRange}
            startIndex={page.startIndex}
            totalLabel={tableCopy.totalRows}
            totalRows={sortedTemplates.length}
            zeroLabel={tableCopy.zeroResults}
          />
        ) : null}
      </CardContent>
    </Card>
  )
}

function UsagePanel({
  error,
  loading,
  onRetry,
  rows,
}: {
  error: string | null
  loading: boolean
  onRetry: () => void
  rows: PricingTemplateConnectionUsageItem[]
}) {
  const { messages } = useLocale()
  const copy = messages.pricingTemplatesUi

  if (loading) return <Skeleton className="h-20 rounded-md" />
  if (error) {
    return (
      <OperatorErrorState
        title={messages.pricingTemplatesData.loadUsageFailed}
        description={error}
        action={<OperatorRetryButton onClick={onRetry}>{messages.common.retry}</OperatorRetryButton>}
      />
    )
  }
  if (rows.length === 0) {
    return <OperatorInsetPanel><p className="text-xs text-muted-foreground">{copy.templateUnused}</p></OperatorInsetPanel>
  }

  return (
    <OperatorInsetPanel className="p-0">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{copy.model}</TableHead>
            <TableHead>{copy.endpoint}</TableHead>
            <TableHead>{copy.terminalTargetColumn}</TableHead>
            <TableHead className="text-right">{copy.actions}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => (
            <TableRow key={`${row.connection_id}-${row.model_config_id}`}>
              <TableCell>
                <Link
                  to="/route/models/$modelId"
                  params={{ modelId: String(row.model_config_id) }}
                  aria-label={copy.openModel(row.model_id)}
                  className="font-mono text-xs underline-offset-2 hover:underline"
                >
                  {row.model_id}
                </Link>
              </TableCell>
              <TableCell>
                <Link
                  to="/route/endpoints"
                  aria-label={copy.openEndpoint(row.endpoint_name)}
                  className="text-xs underline-offset-2 hover:underline"
                >
                  {row.endpoint_name}
                </Link>
              </TableCell>
              <TableCell className="text-xs">{row.connection_name ?? copy.unnamed}</TableCell>
              <TableCell className="text-right">
                <Button asChild type="button" variant="outline" size="sm">
                  <Link to="/route/models/$modelId" params={{ modelId: String(row.model_config_id) }}>
                    {copy.rebindToOtherTemplate}
                  </Link>
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </OperatorInsetPanel>
  )
}

/**
 * Revision history shows all five rates, not just the two base ones, and marks
 * which of them actually changed between consecutive versions.
 */
function HistoryPanel({ loading, revisions }: { loading: boolean; revisions: PricingTemplateRevision[] }) {
  const { messages } = useLocale()
  const { format: formatTime } = useTimezone()
  const copy = messages.pricingTemplatesUi
  const historyCopy = messages.pricingTemplatesHistory

  if (loading) return <Skeleton className="h-20 rounded-md" />
  if (revisions.length === 0) {
    return <OperatorInsetPanel><p className="text-xs text-muted-foreground">{historyCopy.empty}</p></OperatorInsetPanel>
  }

  return (
    <OperatorInsetPanel className="p-0">
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{copy.columnVersion}</TableHead>
              <TableHead className="text-right">{copy.rateInput}</TableHead>
              <TableHead className="text-right">{copy.rateOutput}</TableHead>
              <TableHead className="text-right">{copy.rateCachedInput}</TableHead>
              <TableHead className="text-right">{copy.rateCacheCreation}</TableHead>
              <TableHead className="text-right">{copy.rateReasoning}</TableHead>
              <TableHead>{historyCopy.effectiveAt}</TableHead>
              <TableHead>{historyCopy.createdBy}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {revisions.map((revision, index) => {
              const previous = revisions[index + 1]
              const changed = (field: keyof PricingTemplateRevision) =>
                previous ? normalizeTemplatePrice(String(revision[field] ?? "")) !== normalizeTemplatePrice(String(previous[field] ?? "")) : false
              return (
                <TableRow key={revision.revision_id}>
                  <TableCell>
                    <OperatorValueBadge label={`v${revision.version}`} className="text-xs" />
                  </TableCell>
                  <HistoryRate symbol={revision.currency_code} changed={changed("input_price")} value={revision.input_price} />
                  <HistoryRate symbol={revision.currency_code} changed={changed("output_price")} value={revision.output_price} />
                  <HistoryRate symbol={revision.currency_code} changed={changed("cached_input_price")} specialty value={revision.cached_input_price} />
                  <HistoryRate symbol={revision.currency_code} changed={changed("cache_creation_price")} specialty value={revision.cache_creation_price} />
                  <HistoryRate symbol={revision.currency_code} changed={changed("reasoning_price")} specialty value={revision.reasoning_price} />
                  <TableCell className="font-mono text-xs tabular-nums">
                    {revision.effective_at ? formatTime(revision.effective_at) : <OperatorMissingValue className="text-xs" />}
                  </TableCell>
                  <TableCell className="text-xs">
                    {historyCopy.createdByKind(revision.created_by_kind)}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </div>
    </OperatorInsetPanel>
  )
}

function HistoryRate({
  changed,
  specialty,
  symbol,
  value,
}: {
  changed: boolean
  specialty?: boolean
  symbol?: string
  value: string | null
}) {
  return (
    <TableCell className={cn("text-right", changed && "bg-primary-soft/40")}>
      <RateCell specialty={specialty} symbol={symbol} value={value} />
    </TableCell>
  )
}
