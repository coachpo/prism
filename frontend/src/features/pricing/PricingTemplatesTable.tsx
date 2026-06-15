import { useMemo, useState } from "react"
import { Coins, Eye, Loader2, Pencil, Plus, Search, Trash2 } from "lucide-react"

import { IconActionButton, IconActionGroup } from "@/components/IconActionGroup"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import {
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useLocale } from "@/i18n/useLocale"
import type { PricingTemplate } from "@/lib/types"
import {
  OperatorEmptyState,
  OperatorValueBadge,
} from "@/shared/design-system"
import {
  OperationalTablePagination,
  OperationalTableSkeletonRows,
  SortableTableHead,
  getNextOperationalSort,
  paginateOperationalRows,
  sortOperationalRows,
  type OperationalSortState,
  type OperationalSortValue,
} from "@/shared/table/operationalTable"
import { normalizeTemplatePrice } from "./pricingSchemas"

type PricingSortColumn = "name" | "currency" | "input" | "output" | "version"
const PRICING_PAGE_SIZE = 10

interface PricingTemplatesTableProps {
  onCreate: () => void
  onDelete: (template: PricingTemplate) => Promise<void>
  onEdit: (template: PricingTemplate) => Promise<void>
  onViewUsage: (template: PricingTemplate) => Promise<void>
  pricingTemplatePreparingEditId: number | null
  pricingTemplates: PricingTemplate[]
  pricingTemplatesLoading: boolean
}

function priceSortValue(value: string | null | undefined) {
  const parsed = Number(normalizeTemplatePrice(value))
  return Number.isFinite(parsed) ? parsed : 0
}

function getSortValue(template: PricingTemplate, column: PricingSortColumn): OperationalSortValue {
  if (column === "name") return template.name
  if (column === "currency") return template.pricing_currency_code
  if (column === "input") return priceSortValue(template.input_price)
  if (column === "output") return priceSortValue(template.output_price)
  return template.version
}

function matchesPricingFilter(template: PricingTemplate, query: string) {
  const normalized = query.trim().toLowerCase()
  if (!normalized) return true
  return [template.name, template.description, template.pricing_currency_code, template.pricing_unit]
    .filter((value): value is string => Boolean(value))
    .some((value) => value.toLowerCase().includes(normalized))
}

export function PricingTemplatesTable({
  onCreate,
  onDelete,
  onEdit,
  onViewUsage,
  pricingTemplatePreparingEditId,
  pricingTemplates,
  pricingTemplatesLoading,
}: PricingTemplatesTableProps) {
  const { formatNumber, locale, messages } = useLocale()
  const copy = messages.pricingTemplatesUi
  const dialogCopy = messages.pricingTemplateDialog
  const [query, setQuery] = useState("")
  const [sort, setSort] = useState<OperationalSortState<PricingSortColumn>>({ column: "name", direction: "asc" })
  const [pageIndex, setPageIndex] = useState(0)

  const filteredTemplates = useMemo(
    () => pricingTemplates.filter((template) => matchesPricingFilter(template, query)),
    [pricingTemplates, query],
  )
  const sortedTemplates = useMemo(
    () => sortOperationalRows(filteredTemplates, sort, getSortValue, locale),
    [filteredTemplates, locale, sort],
  )
  const page = paginateOperationalRows(sortedTemplates, pageIndex, PRICING_PAGE_SIZE)
  const updateSort = (column: PricingSortColumn) => {
    setSort((current) => getNextOperationalSort(current, column))
    setPageIndex(0)
  }

  return (
    <Card className="operator-table-shell overflow-hidden" data-testid="pricing-templates-table">
      <CardHeader className="border-b pb-3">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-sm"><Coins data-icon="inline-start" />{copy.tableTitle}</CardTitle>
            <CardDescription className="text-xs">{copy.description}</CardDescription>
          </div>
          <Button type="button" size="sm" onClick={onCreate}><Plus data-icon="inline-start" />{copy.addTemplate}</Button>
        </div>
        {!pricingTemplatesLoading && pricingTemplates.length > 0 ? (
          <div className="relative w-full xl:max-w-sm">
            <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              aria-label="Filter pricing templates"
              className="h-9 pl-9"
              type="search"
              value={query}
              onChange={(event) => {
                setQuery(event.target.value)
                setPageIndex(0)
              }}
            />
          </div>
        ) : null}
      </CardHeader>
      <CardContent className="p-0">
        <Table>
          <TableHeader>
            <TableRow>
              <SortableTableHead sortKey="name" sort={sort} onSort={updateSort}>{messages.settingsDialogs.name}</SortableTableHead>
              <SortableTableHead sortKey="currency" sort={sort} onSort={updateSort}>{copy.currency}</SortableTableHead>
              <SortableTableHead sortKey="input" sort={sort} onSort={updateSort}>{copy.input}</SortableTableHead>
              <SortableTableHead sortKey="output" sort={sort} onSort={updateSort}>{copy.output}</SortableTableHead>
              <SortableTableHead sortKey="version" sort={sort} onSort={updateSort} align="right">Version</SortableTableHead>
              <th className="h-[var(--density-table-head-h)] px-[var(--density-table-cell-px)] text-right text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">{copy.actions}</th>
            </TableRow>
          </TableHeader>
          <TableBody>
            {pricingTemplatesLoading ? <OperationalTableSkeletonRows columns={6} rows={4} /> : null}
            {!pricingTemplatesLoading && pricingTemplates.length === 0 ? (
              <TableRow><TableCell colSpan={6}><OperatorEmptyState title={copy.noTemplatesConfigured} description={copy.description} /></TableCell></TableRow>
            ) : null}
            {!pricingTemplatesLoading && pricingTemplates.length > 0 && sortedTemplates.length === 0 ? (
              <TableRow><TableCell colSpan={6}><OperatorEmptyState title="No pricing templates match filters" description="Adjust the table filter and try again." /></TableCell></TableRow>
            ) : null}
            {!pricingTemplatesLoading ? page.pageRows.map((template) => {
              const isPreparingEdit = pricingTemplatePreparingEditId === template.id
              return (
                <TableRow key={template.id} data-testid={`pricing-template-row-${template.id}`}>
                  <TableCell>
                    <div className="flex min-w-56 flex-col gap-1">
                      <span className="font-medium">{template.name}</span>
                      {template.description ? <span className="text-xs text-muted-foreground">{template.description}</span> : null}
                    </div>
                  </TableCell>
                  <TableCell><OperatorValueBadge label={template.pricing_currency_code} className="text-xs" /></TableCell>
                  <TableCell className="font-mono text-xs"><span className="font-medium text-foreground">{template.input_price}</span><div className="mt-1 flex flex-col gap-0.5 text-muted-foreground"><span>{dialogCopy.cachedInputPriceLabel}: {normalizeTemplatePrice(template.cached_input_price)}</span><span>{dialogCopy.cacheCreationPriceLabel}: {normalizeTemplatePrice(template.cache_creation_price)}</span></div></TableCell>
                  <TableCell className="font-mono text-xs"><span className="font-medium text-foreground">{template.output_price}</span><div className="mt-1 text-muted-foreground"><span>{dialogCopy.reasoningPriceLabel}: {normalizeTemplatePrice(template.reasoning_price)}</span></div></TableCell>
                  <TableCell className="text-right"><OperatorValueBadge label={`v${template.version}`} className="text-xs" /></TableCell>
                  <TableCell className="text-right">
                    <IconActionGroup className="justify-end">
                      <IconActionButton aria-label={`${copy.viewUsage} ${template.name}`} onClick={() => { void onViewUsage(template) }}><Eye /></IconActionButton>
                      <IconActionButton aria-label={`${messages.loadbalanceStrategiesTable.edit} ${template.name}`} disabled={isPreparingEdit} onClick={() => { void onEdit(template) }}>{isPreparingEdit ? <Loader2 className="animate-spin" /> : <Pencil />}</IconActionButton>
                      <IconActionButton destructive aria-label={`${messages.settingsDialogs.delete} ${template.name}`} onClick={() => { void onDelete(template) }}><Trash2 /></IconActionButton>
                    </IconActionGroup>
                  </TableCell>
                </TableRow>
              )
            }) : null}
          </TableBody>
        </Table>
        {!pricingTemplatesLoading && sortedTemplates.length > 0 ? (
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
            totalRows={sortedTemplates.length}
            zeroLabel={messages.requestLogs.zeroResults}
          />
        ) : null}
      </CardContent>
    </Card>
  )
}
