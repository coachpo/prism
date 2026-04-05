import type { ReactNode } from "react";
import { useMemo, useState } from "react";
import { ChevronRight } from "lucide-react";
import { EmptyState } from "@/components/EmptyState";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { Locale } from "@/i18n/format";
import { formatMoneyMicros } from "@/lib/costing";
import type {
  UsageEndpointStatistic,
  UsageModelStatistic,
  UsageSnapshotCurrency,
} from "@/lib/types";
import { cn } from "@/lib/utils";

interface EndpointStatisticsTableProps {
  currency: UsageSnapshotCurrency;
  endpointModelStatisticsByEndpointId?: Record<number, UsageModelStatistic[]>;
  endpointModelStatisticsErrors?: Record<number, string>;
  endpointModelStatisticsLoading?: Record<number, boolean>;
  items: UsageEndpointStatistic[];
  onLoadEndpointModelStatistics?: (endpointId: number) => Promise<void>;
}

export function EndpointStatisticsTable({
  currency,
  endpointModelStatisticsByEndpointId = {},
  endpointModelStatisticsErrors = {},
  endpointModelStatisticsLoading = {},
  items,
  onLoadEndpointModelStatistics = async () => {},
}: EndpointStatisticsTableProps) {
  const { formatNumber, locale, messages } = useLocale();
  const [openEndpointIds, setOpenEndpointIds] = useState<Set<number>>(new Set());

  const rows = useMemo(
    () =>
      [...items].sort(
        (left, right) =>
          right.request_count - left.request_count ||
          left.endpoint_label.localeCompare(right.endpoint_label),
      ),
    [items],
  );

  return (
    <Card className="border-border/70 bg-card/95 shadow-none" data-testid="statistics-endpoint-table">
      <CardHeader className="border-b border-border/60 pb-4">
        <div className="space-y-1">
          <h2 className="text-lg font-semibold tracking-tight">
            {messages.statistics.endpointStatisticsTitle}
          </h2>
          <p className="text-sm text-muted-foreground">{messages.statistics.topEndpointsByCost}</p>
        </div>
      </CardHeader>

      <CardContent className="pt-6">
        {rows.length === 0 ? (
          <EmptyState
            className="rounded-xl border border-border/60 py-10"
            description={messages.statistics.noEndpointStatisticsDescription}
            title={messages.statistics.noEndpointStatisticsTitle}
          />
        ) : (
          <div className="overflow-hidden rounded-xl border border-border/60 bg-background/80">
            <div className="grid grid-cols-[minmax(0,1fr)_6rem_6rem_7rem_7rem] border-b border-border/60 text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground">
              <div className="px-3 py-3">{messages.statistics.endpointGroup}</div>
              <div className="border-l border-border/50 px-3 py-3 text-right">
                {messages.statistics.requests}
              </div>
              <div className="border-l border-border/50 px-3 py-3 text-right">
                {messages.statistics.successRate}
              </div>
              <div className="border-l border-border/50 px-3 py-3 text-right">
                {messages.statistics.totalTokens}
              </div>
              <div className="border-l border-border/50 px-3 py-3 text-right">
                {messages.statistics.totalSpend}
              </div>
            </div>

            <div>
              {rows.map((item) => {
                const endpointId = item.endpoint_id;

                if (endpointId === null) {
                  return (
                    <div className="border-b border-border/60 last:border-b-0" key={item.endpoint_label}>
                      <div className="grid grid-cols-[minmax(0,1fr)_6rem_6rem_7rem_7rem] text-sm">
                        <div className="min-w-0 px-3 py-3 font-medium text-foreground">
                          <span className="block truncate">{item.endpoint_label}</span>
                        </div>
                        <MetricCell>{formatNumber(item.request_count)}</MetricCell>
                        <MetricCell className={getSuccessRateClass(item.success_rate)}>
                          {formatRate(formatNumber, item.success_rate)}
                        </MetricCell>
                        <MetricCell>{formatNumber(item.total_tokens)}</MetricCell>
                        <MetricCell>
                          {formatSpend(item.total_cost_micros, currency, locale as Locale)}
                        </MetricCell>
                      </div>
                    </div>
                  );
                }

                const isOpen = openEndpointIds.has(endpointId);
                const endpointModels = endpointModelStatisticsByEndpointId[endpointId] ?? [];
                const isLoading = endpointModelStatisticsLoading[endpointId] ?? false;
                const error = endpointModelStatisticsErrors[endpointId];

                return (
                  <Collapsible
                    data-testid="statistics-endpoint-collapsible"
                    key={endpointId}
                    onOpenChange={(open) => {
                      setOpenEndpointIds((current) => {
                        const next = new Set(current);
                        if (open) {
                          next.add(endpointId);
                        } else {
                          next.delete(endpointId);
                        }
                        return next;
                      });

                      if (open) {
                        void onLoadEndpointModelStatistics(endpointId);
                      }
                    }}
                    open={isOpen}
                  >
                    <div className="border-b border-border/60 last:border-b-0">
                      <CollapsibleTrigger asChild>
                        <button
                          aria-label={`#${endpointId} ${item.endpoint_label}`}
                          className="grid w-full grid-cols-[minmax(0,1fr)_6rem_6rem_7rem_7rem] text-sm transition-colors hover:bg-muted/30"
                          type="button"
                        >
                          <div className="flex min-w-0 items-center gap-2 px-3 py-3 text-left">
                            <ChevronRight
                              className={cn(
                                "h-4 w-4 shrink-0 text-muted-foreground transition-transform",
                                isOpen && "rotate-90",
                              )}
                            />
                            <span className="truncate font-medium text-foreground">
                              #{endpointId} {item.endpoint_label}
                            </span>
                          </div>
                          <MetricCell>{formatNumber(item.request_count)}</MetricCell>
                          <MetricCell className={getSuccessRateClass(item.success_rate)}>
                            {formatRate(formatNumber, item.success_rate)}
                          </MetricCell>
                          <MetricCell>{formatNumber(item.total_tokens)}</MetricCell>
                          <MetricCell>
                            {formatSpend(item.total_cost_micros, currency, locale as Locale)}
                          </MetricCell>
                        </button>
                      </CollapsibleTrigger>

                      <CollapsibleContent>
                        <div className="border-t border-border/50 bg-muted/20 px-3 py-3">
                          {isLoading ? (
                            <p className="text-sm text-muted-foreground">
                              {messages.statistics.loadingEndpointModelStatistics}
                            </p>
                          ) : error ? (
                            <p className="text-sm text-destructive">{error}</p>
                          ) : endpointModels.length === 0 ? (
                            <p className="text-sm text-muted-foreground">
                              {messages.statistics.noModelStatisticsDescription}
                            </p>
                          ) : (
                            <div
                              className="overflow-hidden rounded-lg border border-border/60 bg-background"
                              data-testid={`statistics-endpoint-model-table-${endpointId}`}
                            >
                              <Table>
                                <TableHeader>
                                  <TableRow>
                                    <TableHead>{messages.statistics.modelGroup}</TableHead>
                                    <TableHead className="text-right">
                                      {messages.statistics.requests}
                                    </TableHead>
                                    <TableHead className="text-right">
                                      {messages.statistics.successRate}
                                    </TableHead>
                                    <TableHead className="text-right">
                                      {messages.statistics.totalTokens}
                                    </TableHead>
                                    <TableHead className="text-right">
                                      {messages.statistics.totalSpend}
                                    </TableHead>
                                  </TableRow>
                                </TableHeader>
                                <TableBody>
                                  {endpointModels.map((model) => (
                                    <TableRow key={model.model_id}>
                                      <TableCell className="font-medium text-foreground">
                                        {model.model_label}
                                      </TableCell>
                                      <TableCell className="text-right tabular-nums">
                                        {formatNumber(model.request_count)}
                                      </TableCell>
                                      <TableCell
                                        className={cn(
                                          "text-right tabular-nums font-medium",
                                          getSuccessRateClass(model.success_rate),
                                        )}
                                      >
                                        {formatRate(formatNumber, model.success_rate)}
                                      </TableCell>
                                      <TableCell className="text-right tabular-nums">
                                        {formatNumber(model.total_tokens)}
                                      </TableCell>
                                      <TableCell className="text-right tabular-nums">
                                        {formatSpend(model.total_cost_micros, currency, locale as Locale)}
                                      </TableCell>
                                    </TableRow>
                                  ))}
                                </TableBody>
                              </Table>
                            </div>
                          )}
                        </div>
                      </CollapsibleContent>
                    </div>
                  </Collapsible>
                );
              })}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function MetricCell({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("border-l border-border/50 px-3 py-3 text-right tabular-nums", className)}>
      {children}
    </div>
  );
}

function formatRate(
  formatNumber: ReturnType<typeof useLocale>["formatNumber"],
  successRate: number,
) {
  return `${formatNumber(successRate, {
    maximumFractionDigits: 1,
    minimumFractionDigits: 1,
  })}%`;
}

function formatSpend(
  totalCostMicros: number,
  currency: UsageSnapshotCurrency,
  locale: Locale,
) {
  return totalCostMicros > 0
    ? formatMoneyMicros(totalCostMicros, currency.symbol, currency.code, 2, 6, locale)
    : "—";
}

function getSuccessRateClass(successRate: number) {
  return successRate >= 95
    ? "text-emerald-600 dark:text-emerald-400"
    : successRate >= 80
      ? "text-amber-600 dark:text-amber-400"
      : "text-red-600 dark:text-red-400";
}
