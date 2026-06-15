import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { formatMoneyMicros } from "@/lib/costing";
import type { UsageProxyApiKeyStatistic, UsageSnapshotCurrency } from "@/lib/types";
import { cn } from "@/lib/utils";
import { DataTable, type DataTableColumn } from "./data-table";
import { OperatorEmptyState } from "@/shared/design-system";

interface ProxyApiKeyStatisticsTableProps {
  currency?: UsageSnapshotCurrency;
  items: UsageProxyApiKeyStatistic[];
}

export function ProxyApiKeyStatisticsTable({
  currency = { code: "USD", symbol: "$" },
  items,
}: ProxyApiKeyStatisticsTableProps) {
  const { formatNumber, locale, messages } = useLocale();
  const rows = [...items].sort((left, right) => right.request_count - left.request_count);

  const columns: DataTableColumn<UsageProxyApiKeyStatistic>[] = [
    {
      cell: (item) => <span className="font-medium text-foreground">{item.proxy_api_key_label}</span>,
      header: messages.statistics.proxyApiKey,
      id: "label",
      sortValue: (item) => item.proxy_api_key_label,
    },
    {
      cell: (item) => formatNumber(item.request_count),
      className: "text-right tabular-nums",
      header: messages.statistics.requests,
      headerClassName: "text-right",
      id: "requests",
      sortValue: (item) => item.request_count,
    },
    {
      cell: (item) => (
        <span className={cn("tabular-nums font-medium", getSuccessRateClass(item.success_rate))}>
          {formatNumber(item.success_rate, {
            maximumFractionDigits: 1,
            minimumFractionDigits: 1,
          })}
          %
        </span>
      ),
      className: "text-right tabular-nums",
      header: messages.statistics.successRate,
      headerClassName: "text-right",
      id: "success-rate",
      sortValue: (item) => item.success_rate,
    },
    {
      cell: (item) => formatNumber(item.total_tokens),
      className: "text-right tabular-nums",
      header: messages.statistics.totalTokens,
      headerClassName: "text-right",
      id: "tokens",
      sortValue: (item) => item.total_tokens,
    },
    {
      cell: (item) =>
        formatMoneyMicros(
          item.total_cost_micros,
          currency.symbol,
          currency.code,
          2,
          6,
          locale,
        ),
      className: "text-right tabular-nums",
      header: messages.statistics.totalSpend,
      headerClassName: "text-right",
      id: "spend",
      sortValue: (item) => item.total_cost_micros,
    },
  ];

  return (
    <Card className="operator-section-surface">
      <CardHeader className="border-b border-outline-variant pb-4">
        <div className="flex flex-col gap-1">
          <h2 className="text-lg font-semibold tracking-tight">
            {messages.statistics.proxyApiKeyStatisticsTitle}
          </h2>
          <p className="text-sm text-muted-foreground">{messages.statistics.proxyApiKey}</p>
        </div>
      </CardHeader>

      <CardContent className="pt-6">
        <DataTable
          columns={columns}
          emptyState={
            <OperatorEmptyState
              className="py-10"
              description={messages.statistics.noProxyApiKeyUsageDescription}
              title={messages.statistics.noProxyApiKeyUsageTitle}
            />
          }
          getRowId={(item) => String(item.proxy_api_key_id ?? item.proxy_api_key_label)}
          initialSort={{ columnId: "requests", direction: "desc" }}
          items={rows}
          testId="statistics-proxy-key-table"
        />
      </CardContent>
    </Card>
  );
}

function getSuccessRateClass(successRate: number) {
  return successRate >= 95
    ? "text-success"
    : successRate >= 80
      ? "text-warning"
      : "text-destructive";
}
