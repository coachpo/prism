import { useMemo, useState } from "react";
import { AlertTriangle, Check, Loader2, Shield, SlidersHorizontal } from "lucide-react";
import { EmptyState } from "@/components/EmptyState";
import { StatusBadge, TypeBadge, ValueBadge, type BadgeIntent } from "@/components/StatusBadge";
import { IconActionButton, IconActionGroup } from "@/components/IconActionGroup";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { SidecarActionHistoryItem, SidecarAuthSnapshot } from "@/lib/types";

type PendingMutation =
  | { kind: "priority"; snapshot: SidecarAuthSnapshot; priority: number }
  | { kind: "status"; snapshot: SidecarAuthSnapshot; disabled: boolean };

interface AuthFilesTableProps {
  actionHistory: SidecarActionHistoryItem[];
  authSnapshots: SidecarAuthSnapshot[];
  loading: boolean;
  mutatingAuthKey: string | null;
  onPatchPriority: (snapshot: SidecarAuthSnapshot, priority: number, allowWatchdog: boolean) => Promise<void>;
  onPatchStatus: (snapshot: SidecarAuthSnapshot, disabled: boolean, allowWatchdog: boolean) => Promise<void>;
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

function boolState(value: boolean | undefined, enabledLabel: string, disabledLabel: string) {
  if (value === true) {
    return disabledLabel;
  }
  if (value === false) {
    return enabledLabel;
  }
  return "Unknown";
}

function statusIntent(snapshot: SidecarAuthSnapshot): BadgeIntent {
  if (snapshot.disabled || snapshot.unavailable) {
    return "danger";
  }
  if (snapshot.quota_exceeded || snapshot.next_retry_after) {
    return "warning";
  }
  if (snapshot.status === "healthy" || snapshot.status === "available") {
    return "success";
  }
  return snapshot.status ? "info" : "muted";
}

function summarizeJson(value: unknown) {
  if (value === null || value === undefined) {
    return "—";
  }
  if (Array.isArray(value)) {
    return `${value.length} bucket${value.length === 1 ? "" : "s"}`;
  }
  if (typeof value === "object") {
    const keys = Object.keys(value as Record<string, unknown>).filter((key) => !/secret|token|key|password|authorization/i.test(key));
    return keys.length > 0 ? keys.slice(0, 3).join(", ") : "redacted";
  }
  return "redacted";
}

function latestActionByAuthId(actions: SidecarActionHistoryItem[]) {
  const byAuthId = new Map<string, SidecarActionHistoryItem>();
  actions.forEach((action) => {
    if (!action.auth_id) {
      return;
    }
    const current = byAuthId.get(action.auth_id);
    if (!current || Date.parse(action.created_at) > Date.parse(current.created_at)) {
      byAuthId.set(action.auth_id, action);
    }
  });
  return byAuthId;
}

function getPriorityInputValue(
  drafts: Record<string, string>,
  snapshot: SidecarAuthSnapshot,
) {
  return drafts[snapshot.auth_id] ?? String(snapshot.priority ?? 0);
}

function parsePriority(value: string) {
  const trimmed = value.trim();
  if (!/^\d+$/.test(trimmed)) {
    return null;
  }
  const parsed = Number(trimmed);
  return Number.isSafeInteger(parsed) ? parsed : null;
}

export function AuthFilesTable({
  actionHistory,
  authSnapshots,
  loading,
  mutatingAuthKey,
  onPatchPriority,
  onPatchStatus,
}: AuthFilesTableProps) {
  const { formatNumber, locale, messages } = useLocale();
  const [draftPriorities, setDraftPriorities] = useState<Record<string, string>>({});
  const [pendingMutation, setPendingMutation] = useState<PendingMutation | null>(null);
  const [allowWatchdog, setAllowWatchdog] = useState(false);
  const latestAction = useMemo(() => latestActionByAuthId(actionHistory), [actionHistory]);

  const clearDraftPriority = (authId: string) => {
    setDraftPriorities((current) => {
      const next = { ...current };
      delete next[authId];
      return next;
    });
  };

  const openMutation = (mutation: PendingMutation) => {
    setPendingMutation(mutation);
    setAllowWatchdog(false);
  };

  const confirmMutation = async () => {
    if (!pendingMutation) {
      return;
    }
    if (pendingMutation.kind === "priority") {
      await onPatchPriority(pendingMutation.snapshot, pendingMutation.priority, allowWatchdog);
      clearDraftPriority(pendingMutation.snapshot.auth_id);
    } else {
      await onPatchStatus(pendingMutation.snapshot, pendingMutation.disabled, allowWatchdog);
    }
    setPendingMutation(null);
  };

  return (
    <Card data-testid="sidecar-auth-files">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm">
          <SlidersHorizontal className="h-4 w-4" />
          Auth files
        </CardTitle>
        <CardDescription className="text-xs">
          Synced OAuth/auth inventory with quota, retry, priority, and watchdog state. Secrets and raw tokens are never shown.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <Alert className="border-warning/30 bg-warning/10">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>Priority 0 is not exclusion</AlertTitle>
          <AlertDescription>
            Priority 0 is the lowest, last-resort band. It may still be used if no higher-priority auth is available.
          </AlertDescription>
        </Alert>

        {loading ? (
          <div className="space-y-2">
            <div className="h-14 animate-pulse rounded-md bg-muted/50" />
            <div className="h-14 animate-pulse rounded-md bg-muted/50" />
          </div>
        ) : authSnapshots.length === 0 ? (
          <EmptyState title="No auth snapshots" description="Run a sidecar sync to populate auth inventory." />
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Auth file</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead>Priority / quota</TableHead>
                  <TableHead>Retry / recovery</TableHead>
                  <TableHead>Requests</TableHead>
                  <TableHead>Watchdog</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {authSnapshots.map((snapshot) => {
                  const latest = latestAction.get(snapshot.auth_id);
                  const priorityValue = getPriorityInputValue(draftPriorities, snapshot);
                  const parsedPriority = parsePriority(priorityValue);
                  const mutating = mutatingAuthKey === snapshot.auth_id;

                  return (
                    <TableRow key={snapshot.auth_id}>
                      <TableCell>
                        <div className="flex min-w-52 flex-col gap-1">
                          <span className="font-medium">{snapshot.name}</span>
                          <span className="font-mono text-xs text-muted-foreground">{snapshot.auth_id}</span>
                          <div className="flex flex-wrap gap-1">
                            {snapshot.provider ? <TypeBadge label={snapshot.provider} intent="info" preserveLabel /> : null}
                            {snapshot.label ? <TypeBadge label={snapshot.label} intent="muted" preserveLabel /> : null}
                            {snapshot.auth_index ? <ValueBadge label={`#${snapshot.auth_index}`} intent="muted" /> : null}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-col gap-2">
                          <StatusBadge label={snapshot.status ?? "unknown"} intent={statusIntent(snapshot)} />
                          <TypeBadge label={boolState(snapshot.disabled, "Enabled", "Disabled")} intent={snapshot.disabled ? "danger" : "success"} preserveLabel />
                          {snapshot.unavailable ? <TypeBadge label="Unavailable" intent="warning" preserveLabel /> : null}
                          {snapshot.status_message ? <span className="max-w-52 text-xs text-muted-foreground">{snapshot.status_message}</span> : null}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex min-w-44 flex-col gap-2">
                          <div className="flex flex-wrap items-center gap-1">
                            <ValueBadge label={`priority ${snapshot.priority ?? 0}`} intent={snapshot.priority === 0 || snapshot.priority === undefined ? "warning" : "info"} />
                            {snapshot.priority === undefined ? <TypeBadge label="missing resolves to 0" intent="warning" preserveLabel /> : null}
                          </div>
                          {snapshot.quota_exceeded ? <TypeBadge label="Quota exceeded" intent="warning" preserveLabel /> : null}
                          {snapshot.quota_reason ? <span className="max-w-44 text-xs text-muted-foreground">{snapshot.quota_reason}</span> : null}
                          <div className="flex items-center gap-2">
                            <Input
                              aria-label={`Priority for ${snapshot.name}`}
                              className="h-8 w-24"
                              min={0}
                              step={1}
                              type="number"
                              value={priorityValue}
                              aria-invalid={parsedPriority === null}
                              onChange={(event) => setDraftPriorities((current) => ({ ...current, [snapshot.auth_id]: event.target.value }))}
                            />
                            <Button
                              type="button"
                              size="xs"
                              variant="outline"
                              disabled={parsedPriority === null || mutating}
                              onClick={() => parsedPriority !== null && openMutation({ kind: "priority", snapshot, priority: parsedPriority })}
                            >
                              {mutating ? <Loader2 className="h-3 w-3 animate-spin" /> : <Check className="h-3 w-3" />}
                              Save
                            </Button>
                          </div>
                          {priorityValue === "0" ? <span className="text-xs text-warning">Priority 0 remains a last-resort option.</span> : null}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-col gap-1 text-xs text-muted-foreground">
                          <span>Recover: {formatTimestamp(snapshot.quota_next_recover_at, locale, messages.common.unavailable)}</span>
                          <span>Retry: {formatTimestamp(snapshot.next_retry_after, locale, messages.common.unavailable)}</span>
                          <span>Observed: {formatTimestamp(snapshot.observed_at, locale, messages.common.unavailable)}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-col gap-1 text-xs text-muted-foreground">
                          <span>Success: {formatNumber(snapshot.success_count ?? 0)}</span>
                          <span>Failed: {formatNumber(snapshot.failed_count ?? 0)}</span>
                          <span>Recent: {summarizeJson(snapshot.recent_requests)}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex min-w-44 flex-col gap-1 text-xs text-muted-foreground">
                          {latest ? (
                            <>
                              <TypeBadge label={latest.action_type} intent="accent" preserveLabel />
                              <span>{latest.status}</span>
                              {latest.hold_until ? <span>Hold until {formatTimestamp(latest.hold_until, locale, messages.common.unavailable)}</span> : null}
                            </>
                          ) : (
                            <span>No watchdog action</span>
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        <IconActionGroup className="justify-end">
                          <IconActionButton
                            disabled={mutating}
                            onClick={() => openMutation({ kind: "status", snapshot, disabled: !snapshot.disabled })}
                          >
                            {mutating ? <Loader2 className="h-4 w-4 animate-spin" /> : <Shield className="h-4 w-4" />}
                            <span className="sr-only">{snapshot.disabled ? `Enable auth ${snapshot.name}` : `Disable auth ${snapshot.name}`}</span>
                          </IconActionButton>
                        </IconActionGroup>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>

      <AlertDialog open={pendingMutation !== null} onOpenChange={(open) => !open && setPendingMutation(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Confirm manual auth mutation</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="space-y-3 text-left">
                <p>
                  Manual changes normally pause watchdog reconciliation for this auth file so the watchdog does not immediately undo operator intent.
                </p>
                {pendingMutation?.kind === "priority" && pendingMutation.priority === 0 ? (
                  <p className="font-medium text-warning">
                    Priority 0 is lowest/last resort, not guaranteed exclusion; it may still be used if no higher-priority auth is available.
                  </p>
                ) : null}
                <div className="flex items-start gap-2 rounded-lg border bg-muted/20 p-3">
                  <Checkbox
                    id="allow-watchdog-immediately"
                    checked={allowWatchdog}
                    onCheckedChange={(checked) => setAllowWatchdog(checked === true)}
                  />
                  <div className="grid gap-1 leading-none">
                    <Label htmlFor="allow-watchdog-immediately">Allow watchdog immediately</Label>
                    <p className="text-xs text-muted-foreground">
                      Sends allow_watchdog=true with this backend mutation instead of applying the normal manual pause.
                    </p>
                  </div>
                </div>
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => void confirmMutation()}>Apply change</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}
