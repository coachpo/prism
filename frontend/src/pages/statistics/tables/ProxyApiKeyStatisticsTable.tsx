import { Link } from "@tanstack/react-router";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import {
  isNoneProxyApiKeyLabel,
  isUnknownBucketProxyApiKeyLabel,
} from "@/i18n/staticMessages";
import { formatMoneyMicros } from "@/lib/costing";
import type { UsageProxyApiKeyStatistic, UsageSnapshotCurrency } from "@/lib/types";
import { cn } from "@/lib/utils";
import { DataTable, type DataTableColumn } from "./data-table";
import { OperatorCallout, OperatorEmptyState } from "@/shared/design-system";

interface ProxyApiKeyStatisticsTableProps {
  currency?: UsageSnapshotCurrency;
  items: UsageProxyApiKeyStatistic[];
  /** Current auth mode; null when unknown/unavailable. */
  authEnabled: boolean | null;
  authModeLoaded: boolean;
}

export function ProxyApiKeyStatisticsTable({
  currency = { code: "USD", symbol: "$" },
  items,
  authEnabled,
  authModeLoaded,
}: ProxyApiKeyStatisticsTableProps) {
  const { formatNumber, locale, messages } = useLocale();
  const copy = messages.statistics;
  const rows = [...items].sort((left, right) => right.request_count - left.request_count);

  const displayLabel = (label: string): string => {
    if (isNoneProxyApiKeyLabel(label)) {
      return copy.noIdentifiedProxyApiKey;
    }
    if (isUnknownBucketProxyApiKeyLabel(label)) {
      return copy.proxyKeyAttributionUnknown;
    }
    return label;
  };

  const columns: DataTableColumn<UsageProxyApiKeyStatistic>[] = [
    {
      cell: (item) => <span className="font-medium text-foreground">{displayLabel(item.proxy_api_key_label)}</span>,
      header: copy.proxyApiKey,
      id: "label",
      sortValue: (item) => item.proxy_api_key_label,
    },
    {
      cell: (item) => formatNumber(item.request_count),
      className: "text-right tabular-nums",
      header: copy.requests,
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
      header: copy.successRate,
      headerClassName: "text-right",
      id: "success-rate",
      sortValue: (item) => item.success_rate,
    },
    {
      cell: (item) => formatNumber(item.total_tokens),
      className: "text-right tabular-nums",
      header: copy.totalTokens,
      headerClassName: "text-right",
      id: "tokens",
      sortValue: (item) => item.total_tokens,
    },
    {
      cell: (item) =>
        item.total_cost_micros > 0
          ? formatMoneyMicros(item.total_cost_micros, currency.symbol, currency.code, 2, 6, locale)
          : "—",
      className: "text-right tabular-nums",
      header: copy.costHeader,
      headerClassName: "text-right",
      id: "cost",
      sortValue: (item) => item.total_cost_micros,
    },
  ];

  const showAuthOffExplanation = authModeLoaded && authEnabled === false;
  const onlyUnkeyedBuckets =
    rows.length > 0 && rows.every((item) => isNoneProxyApiKeyLabel(item.proxy_api_key_label) || isUnknownBucketProxyApiKeyLabel(item.proxy_api_key_label));

  return (
    <Card>
      <CardHeader title={copy.proxyApiKeyStatisticsTitle} />
      <CardContent className="flex flex-col gap-4">
        {showAuthOffExplanation ? (
          <OperatorCallout intent="info" title={messages.proxyApiKeys.authenticationOff} description={copy.proxyKeyAuthOffExplanation} />
        ) : null}

        {rows.length === 0 ? (
          <OperatorEmptyState
            icon={<span aria-hidden="true">🔑</span>}
            title={copy.noProxyApiKeyUsageTitle}
            description={copy.noProxyApiKeyUsageDescription}
          />
        ) : (
          <DataTable<UsageProxyApiKeyStatistic>
            columns={columns}
            emptyState={
              <OperatorEmptyState
                className="py-10"
                description={copy.noProxyApiKeyUsageDescription}
                title={copy.noProxyApiKeyUsageTitle}
              />
            }
            getRowId={(item) => String(item.proxy_api_key_id ?? item.proxy_api_key_label)}
            initialSort={{ columnId: "requests", direction: "desc" }}
            items={rows}
            testId="statistics-proxy-key-table"
          />
        )}

        {rows.length === 0 || onlyUnkeyedBuckets ? (
          <div className="flex flex-wrap items-center gap-2">
            <Button asChild variant="outline" size="sm">
              <Link to="/control/proxy-keys">{copy.proxyKeyCreateCta}</Link>
            </Button>
            <Button asChild variant="outline" size="sm">
              <Link to="/system/settings?tab=global&section=authentication#authentication">
                {copy.proxyKeyAuthSettingsCta}
              </Link>
            </Button>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

function getSuccessRateClass(rate: number): string {
  if (rate >= 95) return "text-success";
  if (rate >= 80) return "text-foreground";
  return "text-destructive";
}
