import { useMemo, useState } from "react";
import { Activity, Eye, Loader2, Pencil, PlugZap, Plus, RefreshCw, Search, Server, Trash2 } from "lucide-react";
import { EmptyState } from "@/components/EmptyState";
import { IconActionButton, IconActionGroup } from "@/components/IconActionGroup";
import { StatusBadge, TypeBadge } from "@/components/StatusBadge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import { OperationalTablePagination, OperationalTableSkeletonRows, SortableTableHead, getNextOperationalSort, paginateOperationalRows, sortOperationalRows, type OperationalSortState, type OperationalSortValue } from "@/shared/table/operationalTable";
import type { BadgeIntent } from "@/components/StatusBadge";
import type { SidecarInstance, SidecarManagementAuthState } from "@/lib/types";

type SidecarHealthState = "healthy" | "stale" | "degraded" | "disabled";
type ManagementAuthLabels = Record<SidecarManagementAuthState, string>;
type SidecarSortColumn = "name" | "health" | "sync" | "security";
type SidecarFilterMode = "all" | SidecarHealthState;
const SIDECAR_PAGE_SIZE = 10;

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

function getSidecarSearchText(sidecar: SidecarInstance, labels: ManagementAuthLabels) {
  return [
    sidecar.name,
    sidecar.base_url_canonical,
    sidecar.environment_label,
    labels[sidecar.management_auth_state],
    sidecar.last_sync_error,
    sidecar.pause_metadata?.reason,
  ].filter(Boolean).join(" ").toLowerCase();
}

function getSidecarSortValue(sidecar: SidecarInstance, column: SidecarSortColumn): OperationalSortValue {
  if (column === "name") return sidecar.name;
  if (column === "health") return getSidecarHealthState(sidecar);
  if (column === "sync") return sidecar.last_successful_sync_at ?? sidecar.last_sync_at ?? "";
  return sidecar.credential_state.management_password_configured;
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
  const [query, setQuery] = useState("");
  const [filterMode, setFilterMode] = useState<SidecarFilterMode>("all");
  const [sort, setSort] = useState<OperationalSortState<SidecarSortColumn>>({ column: "name", direction: "asc" });
  const [pageIndex, setPageIndex] = useState(0);
  const healthCounts = sidecars.reduce(
    (counts, sidecar) => {
      counts[getSidecarHealthState(sidecar)] += 1;
      return counts;
    },
    { healthy: 0, stale: 0, degraded: 0, disabled: 0 },
  );
  const filteredSidecars = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return sidecars.filter((sidecar) => {
      if (filterMode !== "all" && getSidecarHealthState(sidecar) !== filterMode) return false;
      if (!normalizedQuery) return true;
      return getSidecarSearchText(sidecar, copy.managementAuthLabels).includes(normalizedQuery);
    });
  }, [copy.managementAuthLabels, filterMode, query, sidecars]);
  const sortedSidecars = useMemo(
    () => sortOperationalRows(filteredSidecars, sort, getSidecarSortValue, locale),
    [filteredSidecars, locale, sort],
  );
  const page = paginateOperationalRows(sortedSidecars, pageIndex, SIDECAR_PAGE_SIZE);
  const updateSort = (column: SidecarSortColumn) => {
    setSort((current) => getNextOperationalSort(current, column));
    setPageIndex(0);
  };

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
        {!sidecarsLoading && sidecars.length > 0 ? (
          <div className="flex flex-col gap-3 rounded-xl border bg-card p-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="relative w-full xl:max-w-sm">
              <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                aria-label="Filter sidecars"
                className="h-9 pl-9"
                type="search"
                value={query}
                onChange={(event) => {
                  setQuery(event.target.value);
                  setPageIndex(0);
                }}
              />
            </div>
            <Select value={filterMode} onValueChange={(value) => { setFilterMode(value as SidecarFilterMode); setPageIndex(0); }}>
              <SelectTrigger aria-label="Filter sidecar health" className="h-9 w-full sm:w-[220px]"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All health states</SelectItem>
                <SelectItem value="healthy">{copy.healthLabels.healthy}</SelectItem>
                <SelectItem value="stale">{copy.healthLabels.stale}</SelectItem>
                <SelectItem value="degraded">{copy.healthLabels.degraded}</SelectItem>
                <SelectItem value="disabled">{copy.healthLabels.disabled}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        ) : null}
        {sidecarsLoading ? (
          <div className="rounded-md border" data-testid="sidecars-summary">
            <Table>
              <TableBody><OperationalTableSkeletonRows columns={5} rows={4} /></TableBody>
            </Table>
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
        ) : sortedSidecars.length === 0 ? (
          <div data-testid="sidecars-summary">
            <EmptyState title="No sidecars match filters" description="Adjust the table filter and try again." />
          </div>
        ) : (
          <div className="overflow-hidden rounded-md border" data-testid="sidecars-summary">
            <Table>
              <TableHeader>
                <TableRow>
                  <SortableTableHead sortKey="name" sort={sort} onSort={updateSort}>{copy.nameLabel}</SortableTableHead>
                  <SortableTableHead sortKey="health" sort={sort} onSort={updateSort}>{copy.statusColumn}</SortableTableHead>
                  <SortableTableHead sortKey="sync" sort={sort} onSort={updateSort}>{copy.syncColumn}</SortableTableHead>
                  <SortableTableHead sortKey="security" sort={sort} onSort={updateSort}>{copy.securityColumn}</SortableTableHead>
                  <th className="h-[var(--density-table-head-h)] px-[var(--density-table-cell-px)] text-right text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">{copy.actionsColumn}</th>
                </TableRow>
              </TableHeader>
              <TableBody>
                {page.pageRows.map((sidecar) => {
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
                            <IconActionButton aria-label={`${copy.viewDetails}: ${sidecar.name}`} onClick={() => onSelect(sidecar.id)}>
                              <Eye />
                            </IconActionButton>
                            <IconActionButton aria-label={`${copy.testConnection}: ${sidecar.name}`} disabled={isTesting} onClick={() => void onTestConnection(sidecar)}>
                              {isTesting ? <Loader2 className="animate-spin" /> : <PlugZap />}
                            </IconActionButton>
                            <IconActionButton aria-label={`${copy.syncNow}: ${sidecar.name}`} disabled={isSyncing} onClick={() => void onManualSync(sidecar)}>
                              {isSyncing ? <Loader2 className="animate-spin" /> : <RefreshCw />}
                            </IconActionButton>
                            <IconActionButton aria-label={`${copy.editSidecar}: ${sidecar.name}`} disabled={isPreparingEdit} onClick={() => void onEdit(sidecar)}>
                              {isPreparingEdit ? <Loader2 className="animate-spin" /> : <Pencil />}
                            </IconActionButton>
                            <IconActionButton aria-label={`${copy.deleteAction}: ${sidecar.name}`} destructive onClick={() => onDelete(sidecar)}>
                              <Trash2 />
                            </IconActionButton>
                          </IconActionGroup>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
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
              totalRows={sortedSidecars.length}
              zeroLabel={messages.requestLogs.zeroResults}
            />
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
