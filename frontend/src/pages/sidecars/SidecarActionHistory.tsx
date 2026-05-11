import { History } from "lucide-react";
import { EmptyState } from "@/components/EmptyState";
import { TypeBadge, ValueBadge } from "@/components/StatusBadge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
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
import {
  formatActionStatus,
  formatActionType,
  isKnownActionType,
  isProbeActionType,
  sidecarActionIntent,
  sidecarStatusIntent,
  type ActionTypeLabels,
} from "./sidecarActionPresentation";

interface SidecarActionHistoryProps {
  actions: SidecarActionHistoryItem[];
  loading: boolean;
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

function redactSensitiveText(value: string | undefined, redactedLabel: string) {
  if (!value) {
    return "—";
  }
  return value
    .replace(/Bearer\s+[A-Za-z0-9._~+/=-]+/gi, `Bearer [${redactedLabel}]`)
    .replace(/(api[_-]?key|token|secret|password|authorization)(\s*[:=]\s*)[^\s,;}]+/gi, `$1$2[${redactedLabel}]`);
}

function formatReason(action: SidecarActionHistoryItem, actionLabels: ActionTypeLabels, redactedLabel: string) {
  if (!action.reason) {
    return isKnownActionType(action.action_type) ? actionLabels[action.action_type] : "—";
  }
  if (isProbeActionType(action.action_type)) {
    return isKnownActionType(action.action_type) ? actionLabels[action.action_type] : action.action_type;
  }
  try {
    const parsed = JSON.parse(action.reason) as { request?: { route?: string; fields?: string[] }; error?: string };
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
  return redactSensitiveText(action.reason, redactedLabel);
}

function formatErrorMessage(action: SidecarActionHistoryItem, actionLabels: ActionTypeLabels, redactedLabel: string) {
  if (!action.error_message) {
    return null;
  }
  if (isProbeActionType(action.action_type)) {
    const statusCode = action.error_message.match(/\bstatus=\d{3}\b/)?.[0];
    return statusCode && isKnownActionType(action.action_type)
      ? `${actionLabels[action.action_type]} · ${statusCode}`
      : formatActionType(action.action_type, actionLabels);
  }
  return redactSensitiveText(action.error_message, redactedLabel);
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
            <ScrollArea className="h-144">
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
                  {actions.map((action) => {
                    const errorMessage = formatErrorMessage(action, copy.actionTypeLabels, copy.redactedLabel);

                    return (
                      <TableRow key={action.id}>
                        <TableCell>
                          <TypeBadge label={formatActionType(action.action_type, copy.actionTypeLabels)} intent={sidecarActionIntent(action.action_type)} preserveLabel />
                        </TableCell>
                        <TableCell>
                          <TypeBadge label={formatActionStatus(action.status, copy.actionStatusLabels)} intent={sidecarStatusIntent(action.status)} preserveLabel />
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
                          {formatReason(action, copy.actionTypeLabels, copy.redactedLabel)}
                          {errorMessage ? <p className="mt-1 text-destructive">{errorMessage}</p> : null}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          <div className="flex flex-col gap-1">
                            <span>{formatTimestamp(action.created_at, locale, messages.common.unavailable)}</span>
                            {action.completed_at ? <span>{copy.actionHistoryCompletedAt(formatTimestamp(action.completed_at, locale, messages.common.unavailable))}</span> : null}
                          </div>
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </ScrollArea>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
