import { Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { PricingTemplateConnectionUsageItem } from "@/lib/types";
import { OperatorErrorState, OperatorInsetPanel, OperatorRetryButton } from "@/shared/design-system";

export function PricingTemplateUsagePanel({ error, loading, onRetry, rows }: { error: string | null; loading: boolean; onRetry: () => void; rows: PricingTemplateConnectionUsageItem[] }) {
  const { messages } = useLocale();
  const copy = messages.pricingTemplatesUi;
  if (loading) return <Skeleton className="h-20 rounded-md" />;
  if (error) return <OperatorErrorState title={messages.pricingTemplatesData.loadUsageFailed} description={error} action={<OperatorRetryButton onClick={onRetry}>{messages.common.retry}</OperatorRetryButton>} />;
  if (rows.length === 0) return <OperatorInsetPanel><p className="text-xs text-muted-foreground">{copy.templateUnused}</p></OperatorInsetPanel>;
  return (
    <OperatorInsetPanel className="p-0">
      <Table>
        <TableHeader><TableRow><TableHead>{copy.model}</TableHead><TableHead>{copy.endpoint}</TableHead><TableHead>{copy.terminalTargetColumn}</TableHead><TableHead className="text-right">{copy.actions}</TableHead></TableRow></TableHeader>
        <TableBody>
          {rows.map((row) => (
            <TableRow key={`${row.connection_id}-${row.model_config_id}`}>
              <TableCell><Link to="/route/models/$modelId" params={{ modelId: String(row.model_config_id) }} aria-label={copy.openModel(row.model_id)} className="font-mono text-xs underline-offset-2 hover:underline">{row.model_id}</Link></TableCell>
              <TableCell><Link to="/route/endpoints" aria-label={copy.openEndpoint(row.endpoint_name)} className="text-xs underline-offset-2 hover:underline">{row.endpoint_name}</Link></TableCell>
              <TableCell className="text-xs">{row.connection_name ?? copy.unnamed}</TableCell>
              <TableCell className="text-right"><Button asChild type="button" variant="outline" size="sm"><Link to="/route/models/$modelId" params={{ modelId: String(row.model_config_id) }}>{copy.rebindToOtherTemplate}</Link></Button></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </OperatorInsetPanel>
  );
}
