import { History } from "lucide-react";
import { EmptyState } from "@/components/EmptyState";
import { TypeBadge, ValueBadge, type BadgeIntent } from "@/components/StatusBadge";
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
import type { SidecarActionHistoryItem } from "@/lib/types";

interface SidecarActionHistoryProps {
  actions: SidecarActionHistoryItem[];
  loading: boolean;
}

type KnownActionStatus = "succeeded" | "success" | "skipped" | "failed" | "error";
type ActionStatusLabels = Record<KnownActionStatus, string>;

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

function isKnownActionStatus(status: string): status is KnownActionStatus {
  return status === "succeeded" || status === "success" || status === "skipped" || status === "failed" || status === "error";
}

function formatActionStatus(status: string, labels: ActionStatusLabels) {
  return isKnownActionStatus(status) ? labels[status] : status;
}

function statusIntent(status: string): BadgeIntent {
  if (status === "succeeded" || status === "success") {
    return "success";
  }
  if (status === "skipped") {
    return "warning";
  }
  if (status === "failed" || status === "error") {
    return "danger";
  }
  return "muted";
}

function actionIntent(actionType: string): BadgeIntent {
  if (actionType.includes("restore")) {
    return "success";
  }
  if (actionType.includes("deprioritize")) {
    return "warning";
  }
  if (actionType.includes("operator")) {
    return "accent";
  }
  return "info";
}

function redactSensitiveText(value: string | undefined, redactedLabel: string) {
  if (!value) {
    return "—";
  }
  return value
    .replace(/Bearer\s+[A-Za-z0-9._~+/=-]+/gi, `Bearer [${redactedLabel}]`)
    .replace(/(api[_-]?key|token|secret|password|authorization)(\s*[:=]\s*)[^\s,;}]+/gi, `$1$2[${redactedLabel}]`);
}

function formatReason(reason: string | undefined, redactedLabel: string) {
  if (!reason) {
    return "—";
  }
  try {
    const parsed = JSON.parse(reason) as { request?: { route?: string; fields?: string[] }; error?: string };
    const route = parsed.request?.route;
    const fields = parsed.request?.fields?.map((field) => redactSensitiveText(field, redactedLabel)).join(", ");
    if (route || fields) {
      return [redactSensitiveText(route, redactedLabel), fields].filter(Boolean).join(" · ");
    }
    if (parsed.error) {
      return redactSensitiveText(parsed.error, redactedLabel);
    }
  } catch {
    // Reasons may be plain strings for watchdog decisions.
  }
  return redactSensitiveText(reason, redactedLabel);
}

export function SidecarActionHistory({ actions, loading }: SidecarActionHistoryProps) {
  const { locale, messages } = useLocale();
  const copy = messages.sidecarsPage;

  return (
    <Card data-testid="sidecar-action-history">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm">
          <History className="h-4 w-4" />
          {copy.actionHistoryTitle}
        </CardTitle>
        <CardDescription className="text-xs">{copy.actionHistoryDescription}</CardDescription>
      </CardHeader>
      <CardContent>
        {loading ? (
          <div className="space-y-2">
            <div className="h-14 animate-pulse rounded-md bg-muted/50" />
            <div className="h-14 animate-pulse rounded-md bg-muted/50" />
          </div>
        ) : actions.length === 0 ? (
          <EmptyState title={copy.actionHistoryEmptyTitle} description={copy.actionHistoryEmptyDescription} />
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{copy.actionHistoryActionColumn}</TableHead>
                  <TableHead>{copy.actionHistoryStatusColumn}</TableHead>
                  <TableHead>{copy.actionHistoryAuthColumn}</TableHead>
                  <TableHead>{copy.actionHistoryPriorityColumn}</TableHead>
                  <TableHead>{copy.actionHistoryReasonColumn}</TableHead>
                  <TableHead>{copy.actionHistoryTimeColumn}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {actions.map((action) => (
                  <TableRow key={action.id}>
                    <TableCell>
                      <TypeBadge label={action.action_type} intent={actionIntent(action.action_type)} preserveLabel />
                    </TableCell>
                    <TableCell>
                      <TypeBadge label={formatActionStatus(action.status, copy.actionStatusLabels)} intent={statusIntent(action.status)} preserveLabel />
                    </TableCell>
                    <TableCell>
                      <div className="flex min-w-40 flex-col gap-1">
                        <span className="font-mono text-xs">{action.auth_id ?? "—"}</span>
                        {action.provider ? <span className="text-xs text-muted-foreground">{action.provider}</span> : null}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex min-w-44 flex-col gap-1 text-xs text-muted-foreground">
                        <div className="flex flex-wrap gap-1">
                          {action.previous_priority !== undefined ? <ValueBadge label={copy.actionHistoryFromPriority(action.previous_priority)} intent="muted" /> : null}
                          {action.target_priority !== undefined ? <ValueBadge label={copy.actionHistoryToPriority(action.target_priority)} intent="warning" /> : null}
                        </div>
                        {action.hold_until ? <span>{copy.actionHistoryHoldUntil(formatTimestamp(action.hold_until, locale, messages.common.unavailable))}</span> : null}
                      </div>
                    </TableCell>
                    <TableCell className="max-w-72 whitespace-normal text-xs text-muted-foreground">
                      {formatReason(action.reason, copy.redactedLabel)}
                      {action.error_message ? <p className="mt-1 text-destructive">{redactSensitiveText(action.error_message, copy.redactedLabel)}</p> : null}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      <div className="flex flex-col gap-1">
                        <span>{formatTimestamp(action.created_at, locale, messages.common.unavailable)}</span>
                        {action.completed_at ? <span>{copy.actionHistoryCompletedAt(formatTimestamp(action.completed_at, locale, messages.common.unavailable))}</span> : null}
                      </div>
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
