import type {
  UsageEndpointStatistic,
  UsageModelStatistic,
  UsageProxyApiKeyStatistic,
  UsageSnapshotCurrency,
} from "@/lib/types";
import { EndpointStatisticsTable } from "../tables/EndpointStatisticsTable";
import { ModelStatisticsTable } from "../tables/ModelStatisticsTable";
import { ProxyApiKeyStatisticsTable } from "../tables/ProxyApiKeyStatisticsTable";

interface UsageTablesSectionProps {
  currency: UsageSnapshotCurrency;
  endpointModelStatisticsByEndpointId: Record<number, UsageModelStatistic[]>;
  endpointModelStatisticsErrors: Record<number, string>;
  endpointModelStatisticsLoading: Record<number, boolean>;
  endpointStatistics: UsageEndpointStatistic[];
  modelStatistics: UsageModelStatistic[];
  onLoadEndpointModelStatistics: (endpointId: number) => Promise<void>;
  proxyApiKeyStatistics: UsageProxyApiKeyStatistic[];
  tableResetKey: string;
}

export function UsageTablesSection({
  currency,
  endpointModelStatisticsByEndpointId,
  endpointModelStatisticsErrors,
  endpointModelStatisticsLoading,
  endpointStatistics,
  modelStatistics,
  onLoadEndpointModelStatistics,
  proxyApiKeyStatistics,
  tableResetKey,
}: UsageTablesSectionProps) {
  return (
    <>
      <div className="space-y-6">
        <ModelStatisticsTable currency={currency} items={modelStatistics} />
        <EndpointStatisticsTable
          currency={currency}
          endpointModelStatisticsByEndpointId={endpointModelStatisticsByEndpointId}
          endpointModelStatisticsErrors={endpointModelStatisticsErrors}
          endpointModelStatisticsLoading={endpointModelStatisticsLoading}
          items={endpointStatistics}
          key={tableResetKey}
          onLoadEndpointModelStatistics={onLoadEndpointModelStatistics}
        />
      </div>

      <ProxyApiKeyStatisticsTable currency={currency} items={proxyApiKeyStatistics} />
    </>
  );
}
