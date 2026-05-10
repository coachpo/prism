import { Boxes } from "lucide-react";
import { EmptyState } from "@/components/EmptyState";
import { StatusBadge, TypeBadge } from "@/components/StatusBadge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { SidecarProviderSnapshot } from "@/lib/types";

interface ProviderInventoryTableProps {
  loading: boolean;
  providerSnapshots: SidecarProviderSnapshot[];
}

function formatTimestamp(value: string | undefined, locale: string, fallback: string) {
  if (!value) {
    return fallback;
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return fallback;
  }
  return date.toLocaleString(locale);
}

function maskedSnapshotSummary(snapshot: unknown) {
  if (!snapshot || typeof snapshot !== "object") {
    return "No extra snapshot fields";
  }
  const keys = Object.keys(snapshot as Record<string, unknown>).filter((key) => !/secret|token|key|password/i.test(key));
  if (keys.length === 0) {
    return "Snapshot fields masked";
  }
  return `Masked fields: ${keys.slice(0, 4).join(", ")}${keys.length > 4 ? "…" : ""}`;
}

export function ProviderInventoryTable({ loading, providerSnapshots }: ProviderInventoryTableProps) {
  const { locale, messages } = useLocale();

  return (
    <Card data-testid="sidecar-provider-inventory">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm">
          <Boxes className="h-4 w-4" />
          Provider inventory
        </CardTitle>
        <CardDescription className="text-xs">
          Read-only provider metadata synced through Prism backend APIs. API keys and provider secrets are masked and never requested.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {loading ? (
          <div className="space-y-2">
            <div className="h-14 animate-pulse rounded-md bg-muted/50" />
            <div className="h-14 animate-pulse rounded-md bg-muted/50" />
          </div>
        ) : providerSnapshots.length === 0 ? (
          <EmptyState title="No provider snapshots" description="Run a sidecar sync to populate provider inventory." />
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Provider</TableHead>
                  <TableHead>Item</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Observed</TableHead>
                  <TableHead>Masked snapshot</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {providerSnapshots.map((provider) => (
                  <TableRow key={`${provider.provider_key}:${provider.provider_item_key}`}>
                    <TableCell>
                      <TypeBadge label={provider.provider_key} intent="info" preserveLabel />
                    </TableCell>
                    <TableCell>
                      <div className="flex min-w-48 flex-col gap-1">
                        <span className="font-medium">{provider.name ?? provider.provider_item_key}</span>
                        <span className="font-mono text-xs text-muted-foreground">{provider.provider_item_key}</span>
                        {provider.label ? <span className="text-xs text-muted-foreground">{provider.label}</span> : null}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-col gap-2">
                        <StatusBadge label={provider.status ?? "unknown"} intent={provider.disabled ? "danger" : provider.status ? "success" : "muted"} />
                        {provider.disabled ? <TypeBadge label="Disabled" intent="warning" preserveLabel /> : null}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {formatTimestamp(provider.observed_at, locale, messages.common.unavailable)}
                    </TableCell>
                    <TableCell className="max-w-64 text-xs text-muted-foreground">
                      {maskedSnapshotSummary(provider.snapshot)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
