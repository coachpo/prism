import { Fragment, useMemo, useState } from "react";
import {
  ChevronDown,
  ChevronRight,
  Coins,
  MoreHorizontal,
  Pencil,
  Trash2,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import type {
  PricingCard,
  PricingTemplate,
  PricingTemplateConnectionUsageItem,
  PricingTemplateRevision,
} from "@/lib/types";
import { cn } from "@/lib/utils";
import {
  OperatorEmptyState,
  OperatorCallout,
  OperatorErrorState,
  OperatorHelpHint,
  OperatorMissingValue,
  OperatorRetryButton,
  OperatorStalenessBadge,
  OperatorSearchInput,
  OperatorValueBadge,
} from "@/shared/design-system";
import {
  OperationalTableSkeletonRows,
  SortableTableHead,
  operationalRowActionsClassName,
} from "@/shared/table/operationalTable";
import { OperationalTablePagination } from "@/shared/table/paginationControls";
import {
  getNextOperationalSort,
  paginateOperationalRows,
  sortOperationalRows,
  type OperationalSortState,
  type OperationalSortValue,
} from "@/shared/table/operationalTableState";
import { normalizeTemplatePrice } from "./pricingSchemas";
import { PricingTemplateHistoryPanel } from "./PricingTemplateHistoryPanel";
import { PricingTemplateRatePanel, RateCell } from "./PricingTemplateRatePanel";
import { cardRoleLabel, templateRateCards } from "./pricingRateCards";
import { PricingTemplateUsagePanel } from "./PricingTemplateUsagePanel";
import {
  isRecentlyChanged,
  totalReferences,
  type PricingListFacts,
} from "./usePricingListFacts";

const PRICING_PAGE_SIZES = [10, 25, 50] as const;
const PRICING_COLUMN_COUNT = 13;

type PricingSortColumn =
  | "name"
  | "currency"
  | "input"
  | "output"
  | "version"
  | "updated";
export type PricingFilter =
  | "all"
  | "incomplete"
  | "unreferenced"
  | "recently_changed";
type DetailView = "usage" | "history";

interface PricingTemplatesTableProps {
  detailHistory: PricingTemplateRevision[];
  detailHistoryError: string | null;
  detailHistoryLoading: boolean;
  detailUsage: PricingTemplateConnectionUsageItem[];
  detailUsageError: string | null;
  detailUsageLoading: boolean;
  facts: PricingListFacts;
  filter: PricingFilter;
  onDelete: (template: PricingTemplate) => Promise<void>;
  onEdit: (template: PricingTemplate) => Promise<void>;
  onFilterChange: (filter: PricingFilter) => void;
  onLoadHistory: (template: PricingTemplate) => Promise<void>;
  onLoadUsage: (template: PricingTemplate) => Promise<void>;
  onRetry: () => void;
  pricingTemplateError: string | null;
  pricingTemplatePreparingEditId: number | null;
  pricingTemplates: PricingTemplate[];
  pricingTemplatesLoading: boolean;
}

function priceSortValue(value: string | null | undefined) {
  const normalized = normalizeTemplatePrice(value);
  // 空串不是 0：没有价的模板不参与比价排序，由调用方统一沉到末尾。
  if (normalized === "") return null;
  const parsed = Number(normalized);
  return Number.isFinite(parsed) ? parsed : null;
}

/** 排序口径：一律取首档（普通卡 / 阶梯基础卡 / 峰段卡），表头已写明。 */
function representativeCard(template: PricingTemplate): PricingCard | null {
  return templateRateCards(template)[0]?.card ?? null;
}

/**
 * 一行的一个费率分量。两张卡的模板两档不同就并排渲染成区间，相同时只渲染
 * 一次；区间的两端各带一个只给读屏的档位名，避免读出两个没有口径的裸数字。
 */
function TemplateRateCell({
  cards,
  field,
  specialty,
  symbol,
}: {
  cards: Array<{ role: string; card: PricingCard | null }>;
  field: keyof PricingCard;
  specialty?: boolean;
  symbol: string;
}) {
  const copy = useLocale().messages.pricingTemplatesUi;
  const distinct =
    cards.length > 1 &&
    normalizeTemplatePrice(cards[0].card?.[field]) !==
      normalizeTemplatePrice(cards[1].card?.[field]);
  const shown = distinct ? cards : [cards[0] ?? { role: "standard", card: null }];
  return (
    <span className="inline-flex flex-wrap items-center justify-end gap-1">
      {shown.map(({ role, card }, index) => (
        <Fragment key={role}>
          {index > 0 ? (
            <span aria-hidden="true" className="text-muted-foreground">
              –
            </span>
          ) : null}
          {distinct ? (
            <span className="sr-only">{cardRoleLabel(role, copy)}</span>
          ) : null}
          <RateCell
            specialty={specialty}
            symbol={symbol}
            value={card?.[field]}
          />
        </Fragment>
      ))}
    </span>
  );
}

/** 该行两档口径的全文说明，挂在类型徽章旁的帮助按钮上。 */
function rateBasisHint(
  template: PricingTemplate,
  copy: ReturnType<typeof useLocale>["messages"]["pricingTemplatesUi"],
): string | null {
  if (template.template_kind === "tiered") {
    return template.tier
      ? copy.rateBasisTiered(template.tier.input_tokens_above)
      : copy.rateBasisTieredUnknown;
  }
  if (template.template_kind === "peak_valley") {
    const windows = template.schedule?.windows?.length ?? 0;
    return template.schedule?.timezone && windows > 0
      ? copy.rateBasisPeakValley(template.schedule.timezone, windows)
      : copy.rateBasisPeakValleyUnknown;
  }
  return null;
}

function kindLabel(
  template: PricingTemplate,
  copy: ReturnType<typeof useLocale>["messages"]["pricingTemplatesUi"],
): string {
  if (template.template_kind === "standard") return copy.kindStandard;
  if (template.template_kind === "tiered") return copy.kindTiered;
  if (template.template_kind === "peak_valley") return copy.kindPeakValley;
  return copy.unknownKind;
}

function getSortValue(
  template: PricingTemplate,
  column: PricingSortColumn,
): OperationalSortValue {
  if (column === "name") return template.name;
  if (column === "currency") return template.pricing_currency_code;
  const card = representativeCard(template);
  if (column === "input") return priceSortValue(card?.input_price);
  if (column === "output") return priceSortValue(card?.output_price);
  if (column === "version") return template.version;
  return template.updated_at;
}

function matchesPricingFilter(template: PricingTemplate, query: string) {
  const normalized = query.trim().toLowerCase();
  if (!normalized) return true;
  return [
    template.name,
    template.description,
    template.pricing_currency_code,
    template.pricing_unit,
    template.template_kind,
  ]
    .filter((value): value is string => Boolean(value))
    .some((value) => value.toLowerCase().includes(normalized));
}

/**
 * A specialty rate has three distinct states that must not look alike:
 * a real number, an unconfigured rate (em dash plus an idle badge), and a
 * value the read never produced.
 */

export function PricingTemplatesTable({
  detailHistory,
  detailHistoryError,
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
  const { formatNumber, locale, messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const copy = messages.pricingTemplatesUi;
  const tableCopy = messages.operationalTable;
  const [query, setQuery] = useState("");
  // Newest change first: on a pricing table the question is almost always
  // "what moved recently".
  const [sort, setSort] = useState<OperationalSortState<PricingSortColumn>>({
    column: "updated",
    direction: "desc",
  });
  const [pageIndex, setPageIndex] = useState(0);
  const [pageSize, setPageSize] = useState<number>(PRICING_PAGE_SIZES[0]);
  const [expandedId, setExpandedId] = useState<number | null>(null);
  const [detailView, setDetailView] = useState<DetailView>("usage");

  const filteredTemplates = useMemo(
    () =>
      pricingTemplates.filter((template) => {
        if (!matchesPricingFilter(template, query)) return false;
        const item = facts.byId.get(template.id);
        if (filter === "incomplete")
          return item?.configuration_status === "incomplete";
        if (filter === "unreferenced") return totalReferences(item) === 0;
        if (filter === "recently_changed")
          return isRecentlyChanged(template.version_effective_at);
        return true;
      }),
    [facts.byId, filter, pricingTemplates, query],
  );
  const sortedTemplates = useMemo(() => {
    const sorted = sortOperationalRows(
      filteredTemplates,
      sort,
      getSortValue,
      locale,
    );
    // 共享排序器只在升序把空值排到末尾，降序会把它们整体反转到最前。一个
    // 价格都显示不出来的行不该占据比价视图的首屏，两个方向都沉底。
    const present: PricingTemplate[] = [];
    const missing: PricingTemplate[] = [];
    for (const template of sorted) {
      const value = getSortValue(template, sort.column);
      (value === null || value === undefined ? missing : present).push(template);
    }
    return [...present, ...missing];
  }, [filteredTemplates, locale, sort]);
  const page = paginateOperationalRows(sortedTemplates, pageIndex, pageSize);
  const updateSort = (column: PricingSortColumn) => {
    setSort((current) => getNextOperationalSort(current, column));
    setPageIndex(0);
  };

  const toggleRow = async (template: PricingTemplate, view: DetailView) => {
    if (expandedId === template.id && detailView === view) {
      setExpandedId(null);
      return;
    }
    setExpandedId(template.id);
    setDetailView(view);
    if (view === "usage") await onLoadUsage(template);
    else await onLoadHistory(template);
  };

  return (
    <Card
      className="operator-table-shell gap-0 overflow-hidden"
      data-testid="pricing-templates-table"
    >
      <CardHeader className="border-b pb-3">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <p
            className="flex items-center gap-2 text-xs text-muted-foreground"
            data-testid="pricing-templates-summary"
          >
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
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => onFilterChange("all")}
              >
                {copy.clearFilters}
              </Button>
            ) : null}
            <OperatorSearchInput
              aria-label={copy.filterTemplates}
              className="h-[var(--density-control-h-sm)]"
              placeholder={copy.filterTemplates}
              value={query}
              onChange={(event) => {
                setQuery(event.target.value);
                setPageIndex(0);
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
            action={
              <OperatorRetryButton onClick={onRetry}>
                {messages.common.retry}
              </OperatorRetryButton>
            }
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
              <TableHead colSpan={4}>{copy.groupIdentity}</TableHead>
              <TableHead colSpan={5} className="text-center">
                {/* 单位与口径都只在分组表头写一次：阶梯与峰谷并列两档，
                    排序取首档，全文说明挂在可聚焦的帮助按钮上。 */}
                <span className="inline-flex items-center gap-1">
                  {copy.groupRates}
                  <span className="font-normal text-muted-foreground">
                    {copy.rateUnitPerMillion} · {copy.rateColumnBasis}
                  </span>
                  <OperatorHelpHint
                    align="center"
                    label={copy.rateColumnBasisHint}
                  />
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
              <SortableTableHead
                sortKey="currency"
                sort={sort}
                onSort={updateSort}
              >
                {copy.columnCurrency}
              </SortableTableHead>
              <SortableTableHead
                sortKey="version"
                sort={sort}
                onSort={updateSort}
              >
                {copy.columnVersion}
              </SortableTableHead>
              <TableHead>{copy.columnTier}</TableHead>
              <SortableTableHead
                sortKey="input"
                sort={sort}
                onSort={updateSort}
                align="right"
              >
                {copy.rateInput}
              </SortableTableHead>
              <SortableTableHead
                sortKey="output"
                sort={sort}
                onSort={updateSort}
                align="right"
              >
                {copy.rateOutput}
              </SortableTableHead>
              <TableHead className="text-right">
                {copy.rateCachedInput}
              </TableHead>
              <TableHead className="text-right">
                {copy.rateCacheCreation}
              </TableHead>
              <TableHead className="text-right">{copy.rateReasoning}</TableHead>
              <TableHead>{copy.columnReferences}</TableHead>
              <SortableTableHead
                sortKey="updated"
                sort={sort}
                onSort={updateSort}
              >
                {copy.columnUpdatedAt}
              </SortableTableHead>
              <TableHead className="text-right">{copy.actions}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {pricingTemplatesLoading ? (
              <OperationalTableSkeletonRows
                columns={PRICING_COLUMN_COUNT}
                rows={4}
              />
            ) : null}

            {!pricingTemplatesLoading
              ? page.pageRows.map((template) => {
                  const isPreparingEdit =
                    pricingTemplatePreparingEditId === template.id;
                  const item = facts.byId.get(template.id);
                  const references = totalReferences(item);
                  const expanded = expandedId === template.id;
                  const cards = templateRateCards(template);
                  const basisHint = rateBasisHint(template, copy);

                  return (
                    <Fragment key={template.id}>
                      <TableRow
                        className="group/row"
                        data-testid={`pricing-template-row-${template.id}`}
                      >
                        <TableCell className="align-top">
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-sm"
                            aria-expanded={expanded}
                            aria-label={
                              expanded
                                ? copy.collapseRow(template.name)
                                : copy.expandRow(template.name)
                            }
                            onClick={() => void toggleRow(template, detailView)}
                          >
                            {expanded ? <ChevronDown /> : <ChevronRight />}
                          </Button>
                        </TableCell>
                        <TableCell className="align-top">
                          <div className="flex min-w-48 flex-col gap-0.5">
                            <span className="font-medium">{template.name}</span>
                            {/* Source-linked provenance: the models.dev offering
                                these prices were imported from. A manual
                                template carries no coordinate and shows nothing
                                rather than a placeholder. */}
                            {template.catalog_provider_id &&
                            template.catalog_model_id ? (
                              <span
                                className="truncate font-mono text-xs text-muted-foreground"
                                data-testid={`pricing-template-source-${template.id}`}
                              >
                                {template.catalog_provider_id}/
                                {template.catalog_model_id}
                              </span>
                            ) : null}
                            {template.description ? (
                              <span className="truncate text-xs text-muted-foreground">
                                {template.description}
                              </span>
                            ) : null}
                          </div>
                        </TableCell>
                        <TableCell className="align-top">
                          <OperatorValueBadge
                            label={template.pricing_currency_code}
                            className="text-xs"
                          />
                        </TableCell>
                        <TableCell className="align-top">
                          <OperatorValueBadge
                            label={`v${template.version}`}
                            className="text-xs"
                          />
                        </TableCell>
                        <TableCell className="align-top">
                          <span className="inline-flex items-center gap-0.5">
                            <OperatorValueBadge
                              label={kindLabel(template, copy)}
                              className="text-xs"
                            />
                            {basisHint ? (
                              <OperatorHelpHint label={basisHint} />
                            ) : null}
                          </span>
                        </TableCell>
                        <TableCell className="align-top text-right">
                          <TemplateRateCell
                            cards={cards}
                            field="input_price"
                            symbol={template.active_currency_symbol}
                          />
                        </TableCell>
                        <TableCell className="align-top text-right">
                          <TemplateRateCell
                            cards={cards}
                            field="output_price"
                            symbol={template.active_currency_symbol}
                          />
                        </TableCell>
                        <TableCell className="align-top text-right">
                          <TemplateRateCell
                            cards={cards}
                            field="cached_input_price"
                            specialty
                            symbol={template.active_currency_symbol}
                          />
                        </TableCell>
                        <TableCell className="align-top text-right">
                          <TemplateRateCell
                            cards={cards}
                            field="cache_creation_price"
                            specialty
                            symbol={template.active_currency_symbol}
                          />
                        </TableCell>
                        <TableCell className="align-top text-right">
                          <TemplateRateCell
                            cards={cards}
                            field="reasoning_price"
                            specialty
                            symbol={template.active_currency_symbol}
                          />
                        </TableCell>
                        <TableCell className="align-top">
                          {facts.loading && !item ? (
                            <Skeleton className="h-4 w-24" />
                          ) : facts.failed && !item ? (
                            <span
                              className="text-xs font-medium text-failing"
                              title={copy.referencesUnavailableReason}
                            >
                              {copy.referencesUnavailable}
                            </span>
                          ) : item ? (
                            <span className="font-mono text-xs tabular-nums text-muted-foreground">
                              {references === 0
                                ? copy.referencesNone
                                : copy.referencesSummary(
                                    formatNumber(item.model_reference_count),
                                    formatNumber(item.endpoint_reference_count),
                                    formatNumber(
                                      item.terminal_target_reference_count,
                                    ),
                                  )}
                            </span>
                          ) : (
                            <OperatorMissingValue className="text-xs" />
                          )}
                        </TableCell>
                        <TableCell className="align-top">
                          <span className="font-mono text-xs tabular-nums">
                            {formatTime(template.updated_at)}
                          </span>
                        </TableCell>
                        <TableCell className="align-top text-right">
                          <div
                            className={cn(
                              operationalRowActionsClassName,
                              "gap-1",
                            )}
                          >
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
                                <Button
                                  type="button"
                                  variant="outline"
                                  size="icon-sm"
                                  aria-label={copy.actions}
                                >
                                  <MoreHorizontal />
                                </Button>
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="end">
                                <DropdownMenuItem
                                  onSelect={() =>
                                    void toggleRow(template, "usage")
                                  }
                                >
                                  {copy.detailViewUsage}
                                </DropdownMenuItem>
                                <DropdownMenuItem
                                  onSelect={() =>
                                    void toggleRow(template, "history")
                                  }
                                >
                                  {copy.detailViewHistory}
                                </DropdownMenuItem>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem
                                  variant="destructive"
                                  onSelect={() => void onDelete(template)}
                                >
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
                          <TableCell
                            colSpan={PRICING_COLUMN_COUNT}
                            className="bg-inset"
                          >
                            <div className="flex flex-col gap-3">
                              <div className="flex items-center gap-1">
                                <Button
                                  type="button"
                                  size="sm"
                                  variant={
                                    detailView === "usage"
                                      ? "secondary"
                                      : "ghost"
                                  }
                                  onClick={() =>
                                    void toggleRow(template, "usage")
                                  }
                                >
                                  {copy.detailViewUsage}
                                </Button>
                                <Button
                                  type="button"
                                  size="sm"
                                  variant={
                                    detailView === "history"
                                      ? "secondary"
                                      : "ghost"
                                  }
                                  onClick={() =>
                                    void toggleRow(template, "history")
                                  }
                                >
                                  {copy.detailViewHistory}
                                </Button>
                              </div>

                              <PricingTemplateRatePanel template={template} />

                              {detailView === "usage" ? (
                                <PricingTemplateUsagePanel
                                  error={detailUsageError}
                                  loading={detailUsageLoading}
                                  rows={detailUsage}
                                  onRetry={() => void onLoadUsage(template)}
                                />
                              ) : (
                                <PricingTemplateHistoryPanel
                                  error={detailHistoryError}
                                  loading={detailHistoryLoading}
                                  revisions={detailHistory}
                                  onRetry={() => void onLoadHistory(template)}
                                  template={template}
                                />
                              )}
                            </div>
                          </TableCell>
                        </TableRow>
                      ) : null}
                    </Fragment>
                  );
                })
              : null}
          </TableBody>
        </Table>

        {!pricingTemplatesLoading &&
        pricingTemplateError &&
        pricingTemplates.length === 0 ? (
          <div className="p-3">
            <OperatorErrorState
              title={messages.pricingTemplatesData.loadFailed}
              description={pricingTemplateError}
              action={
                <OperatorRetryButton onClick={onRetry}>
                  {messages.common.retry}
                </OperatorRetryButton>
              }
            />
          </div>
        ) : null}

        {!pricingTemplatesLoading &&
        !pricingTemplateError &&
        pricingTemplates.length === 0 ? (
          <div className="p-3">
            <OperatorEmptyState
              title={copy.noTemplatesConfigured}
              description={copy.description}
            />
          </div>
        ) : null}

        {!pricingTemplatesLoading &&
        pricingTemplates.length > 0 &&
        sortedTemplates.length === 0 ? (
          <div className="p-3">
            <OperatorEmptyState
              title={copy.noTemplatesMatchFilters}
              description={copy.noTemplatesMatchFiltersDescription}
              action={
                <Button
                  variant="outline"
                  onClick={() => {
                    setQuery("");
                    onFilterChange("all");
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
                setPageSize(value);
                setPageIndex(0);
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
  );
}
