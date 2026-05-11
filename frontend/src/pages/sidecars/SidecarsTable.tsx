import { Activity, Eye, Loader2, Pencil, PlugZap, Plus, RefreshCw, Server, Trash2 } from "lucide-react";
import { EmptyState } from "@/components/EmptyState";
import { IconActionButton, IconActionGroup } from "@/components/IconActionGroup";
import { StatusBadge, TypeBadge } from "@/components/StatusBadge";
import { Button } from "@/components/ui/button";
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
import type { BadgeIntent } from "@/components/StatusBadge";
import type { SidecarInstance, SidecarManagementAuthState } from "@/lib/types";

type SidecarHealthState = "healthy" | "stale" | "degraded" | "disabled";
type ManagementAuthLabels = Record<SidecarManagementAuthState, string>;

interface SidecarsTableProps {
  onCreate: () => void;
  onDelete: (sidecar: SidecarInstance) => void;
  onEdit: (sidecar: SidecarInstance) => Promise<void>;
  onManualSync: (sidecar: SidecarInstance) => Promise<void>;
  onSelect: (sidecarId: number) => void;
  onTestConnection: (sidecar: SidecarInstance) => Promise<void>;
  preparingEditSidecarId: number | null;
  selectedSidecarId: number | null;
  sidecars: SidecarInstance[];
  sidecarsLoading: boolean;
  syncingSidecarId: number | null;
  testingSidecarId: number | null;
}

function isPast(timestamp?: string) {
  return timestamp ? Date.parse(timestamp) <= Date.now() : false;
}

function getSidecarHealthState(sidecar: SidecarInstance): SidecarHealthState {
  if (!sidecar.enabled) {
    return "disabled";
  }
  if (sidecar.management_auth_state === "invalid_management_auth" || sidecar.last_sync_error || sidecar.pause_metadata) {
    return "degraded";
  }
  if (isPast(sidecar.snapshot_stale_after) || !sidecar.last_successful_sync_at) {
    return "stale";
  }
  return "healthy";
}

function getHealthIntent(state: SidecarHealthState): BadgeIntent {
  if (state === "healthy") {
    return "success";
  }
  if (state === "stale") {
    return "warning";
  }
  if (state === "degraded") {
    return "danger";
  }
  return "muted";
}

function getManagementAuthLabel(state: SidecarManagementAuthState, labels: ManagementAuthLabels) {
  return labels[state];
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

function SummaryCard({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-xl border bg-muted/20 p-4">
      <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="mt-2 text-2xl font-semibold tabular-nums">{value}</p>
    </div>
  );
}

export function SidecarsTable({
  onCreate,
  onDelete,
  onEdit,
  onManualSync,
  onSelect,
  onTestConnection,
  preparingEditSidecarId,
  sidecars,
  sidecarsLoading,
  syncingSidecarId,
  testingSidecarId,
}: SidecarsTableProps) {
  const { formatNumber, locale, messages } = useLocale();
  const copy = messages.sidecarsPage;
  const healthCounts = sidecars.reduce(
    (counts, sidecar) => {
      counts[getSidecarHealthState(sidecar)] += 1;
      return counts;
    },
    { healthy: 0, stale: 0, degraded: 0, disabled: 0 },
  );

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Server className="h-4 w-4" />
              {copy.tableTitle}
            </CardTitle>
            <CardDescription className="text-xs">{copy.tableDescription}</CardDescription>
          </div>
          <Button type="button" size="sm" onClick={onCreate}>
            <Plus className="mr-2 h-3.5 w-3.5" />
            {copy.addSidecar}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-3 md:grid-cols-4">
          <SummaryCard label={copy.summaryHealthy} value={healthCounts.healthy} />
          <SummaryCard label={copy.summaryStale} value={healthCounts.stale} />
          <SummaryCard label={copy.summaryDegraded} value={healthCounts.degraded} />
          <SummaryCard label={copy.summaryDisabled} value={healthCounts.disabled} />
        </div>

        <div className="text-sm" data-testid="sidecars-state">
          {copy.stateSummary(
            formatNumber(healthCounts.healthy),
            formatNumber(healthCounts.stale),
            formatNumber(healthCounts.degraded),
          )}
        </div>
        <div className="sr-only" data-testid="sidecars-watchdog">
          {copy.watchdogDeferred}
        </div>

        {sidecarsLoading ? (
          <div className="space-y-2">
            <div className="h-14 animate-pulse rounded-md bg-muted/50" />
            <div className="h-14 animate-pulse rounded-md bg-muted/50" />
          </div>
        ) : sidecars.length === 0 ? (
          <div data-testid="sidecars-summary">
            <EmptyState
              icon={<Server className="h-6 w-6" />}
              title={copy.emptyTitle}
              description={copy.emptyDescription}
              action={(
                <Button type="button" onClick={onCreate}>
                  <Plus className="mr-2 h-4 w-4" />
                  {copy.addSidecar}
                </Button>
              )}
            />
          </div>
        ) : (
          <div className="rounded-md border" data-testid="sidecars-summary">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{copy.nameLabel}</TableHead>
                  <TableHead>{copy.statusColumn}</TableHead>
                  <TableHead>{copy.syncColumn}</TableHead>
                  <TableHead>{copy.securityColumn}</TableHead>
                  <TableHead className="text-right">{copy.actionsColumn}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sidecars.map((sidecar) => {
                  const healthState = getSidecarHealthState(sidecar);
                  const isPreparingEdit = preparingEditSidecarId === sidecar.id;
                  const isTesting = testingSidecarId === sidecar.id;
                  const isSyncing = syncingSidecarId === sidecar.id;
                  const passwordConfigured = sidecar.credential_state.management_password_configured;

                  return (
                    <TableRow key={sidecar.id}>
                      <TableCell>
                        <div className="flex flex-col gap-1">
                          <span className="font-medium">{sidecar.name}</span>
                          <span className="font-mono text-xs text-muted-foreground">{sidecar.base_url_canonical}</span>
                          {sidecar.environment_label ? (
                            <span className="text-xs text-muted-foreground">{sidecar.environment_label}</span>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-col gap-2">
                          <StatusBadge label={copy.healthLabels[healthState]} intent={getHealthIntent(healthState)} />
                          <TypeBadge
                            label={getManagementAuthLabel(sidecar.management_auth_state, copy.managementAuthLabels)}
                            intent={sidecar.management_auth_state === "valid" ? "success" : "warning"}
                            preserveLabel
                          />
                          {sidecar.pause_metadata ? (
                            <span className="text-xs text-muted-foreground">{sidecar.pause_metadata.reason}</span>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-col gap-1 text-xs text-muted-foreground">
                          <span>{copy.lastSync}: {formatTimestamp(sidecar.last_sync_at, locale, messages.common.unavailable)}</span>
                          <span>{copy.lastSuccess}: {formatTimestamp(sidecar.last_successful_sync_at, locale, messages.common.unavailable)}</span>
                          <span>{copy.staleAfter}: {formatTimestamp(sidecar.snapshot_stale_after, locale, messages.common.unavailable)}</span>
                          {sidecar.last_sync_error ? <span className="text-destructive">{sidecar.last_sync_error}</span> : null}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-col gap-2">
                          <TypeBadge
                            label={passwordConfigured ? copy.passwordConfigured : copy.passwordMissing}
                            intent={passwordConfigured ? "success" : "warning"}
                            preserveLabel
                          />
                          {sidecar.allow_insecure_http ? <TypeBadge label={copy.insecureHttp} intent="warning" preserveLabel /> : null}
                          {sidecar.skip_tls_verify ? <TypeBadge label={copy.tlsSkipped} intent="warning" preserveLabel /> : null}
                          {sidecar.allow_private_network ? <TypeBadge label={copy.privateNetwork} intent="info" preserveLabel /> : null}
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-2">
                          <IconActionGroup>
                            <IconActionButton onClick={() => onSelect(sidecar.id)}>
                              <Eye className="h-4 w-4" />
                              <span className="sr-only">{copy.viewDetails}</span>
                            </IconActionButton>
                            <IconActionButton disabled={isTesting} onClick={() => void onTestConnection(sidecar)}>
                              {isTesting ? <Loader2 className="h-4 w-4 animate-spin" /> : <PlugZap className="h-4 w-4" />}
                              <span className="sr-only">{copy.testConnection}</span>
                            </IconActionButton>
                            <IconActionButton disabled={isSyncing} onClick={() => void onManualSync(sidecar)}>
                              {isSyncing ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
                              <span className="sr-only">{copy.syncNow}</span>
                            </IconActionButton>
                            <IconActionButton disabled={isPreparingEdit} onClick={() => void onEdit(sidecar)}>
                              {isPreparingEdit ? <Loader2 className="h-4 w-4 animate-spin" /> : <Pencil className="h-4 w-4" />}
                              <span className="sr-only">{copy.editSidecar}</span>
                            </IconActionButton>
                            <IconActionButton destructive onClick={() => onDelete(sidecar)}>
                              <Trash2 className="h-4 w-4" />
                              <span className="sr-only">{copy.deleteAction}</span>
                            </IconActionButton>
                          </IconActionGroup>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}

        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Activity className="h-3.5 w-3.5" />
          {copy.pollingDescription}
        </div>
      </CardContent>
    </Card>
  );
}
