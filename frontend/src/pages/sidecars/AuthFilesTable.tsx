import { useMemo, useState } from "react";
import { Check, ChevronLeft, ChevronRight, Loader2, Search, Shield, SlidersHorizontal } from "lucide-react";
import { EmptyState } from "@/components/EmptyState";
import { StatusBadge, TypeBadge, ValueBadge, type BadgeIntent } from "@/components/StatusBadge";
import { IconActionButton, IconActionGroup } from "@/components/IconActionGroup";
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
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { SidecarActionHistoryItem, SidecarAuthQuotaState, SidecarAuthSnapshot } from "@/lib/types";
import {
  formatActionStatus,
  formatActionType,
  sidecarActionIntent,
} from "./sidecarActionPresentation";

type PendingMutation =
  | { kind: "priority"; snapshot: SidecarAuthSnapshot; priority: number }
  | { kind: "status"; snapshot: SidecarAuthSnapshot; disabled: boolean };

type AuthSortMode = "name" | "routing-priority-desc" | "routing-priority-asc";

interface AuthSearchLabels {
  enabledLabel: string;
  disabledLabel: string;
  missingPriorityLabel: string;
  priorityLabel: (priority: number) => string;
  unavailableLabel: string;
  unknownStatus: string;
}

interface UsageLimitErrorLabels {
  eligiblePromoLabel: string;
  messageLabel: string;
  planTypeLabel: string;
  resetsAtLabel: string;
  resetsInSecondsLabel: string;
  title: string;
  typeLabel: string;
}

interface UsageLimitErrorDetail {
  eligible_promo: string | null;
  message: string;
  plan_type: string;
  resets_at: number;
  resets_in_seconds: number;
  type: "usage_limit_reached";
}

interface AuthFilesTableProps {
  actionHistory: SidecarActionHistoryItem[];
  authSnapshots: SidecarAuthSnapshot[];
  loading: boolean;
  mutatingAuthKey: string | null;
  onPatchPriority: (snapshot: SidecarAuthSnapshot, priority: number, allowWatchdog: boolean) => Promise<void>;
  onPatchStatus: (snapshot: SidecarAuthSnapshot, disabled: boolean, allowWatchdog: boolean) => Promise<void>;
  quotaStates: SidecarAuthQuotaState[];
}

const AUTH_PAGE_SIZE_OPTIONS = [100, 300, 500] as const;
const DEFAULT_AUTH_PAGE_SIZE = AUTH_PAGE_SIZE_OPTIONS[0];

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

function formatEpochSeconds(value: number, locale: string, fallback: string) {
  if (!Number.isSafeInteger(value) || value < 0) {
    return fallback;
  }
  const date = new Date(value * 1000);
  if (Number.isNaN(date.getTime())) {
    return fallback;
  }
  return date.toLocaleString(locale);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function isUsageLimitErrorDetail(value: unknown): value is UsageLimitErrorDetail {
  if (!isRecord(value)) {
    return false;
  }

  return value.type === "usage_limit_reached"
    && typeof value.message === "string"
    && typeof value.plan_type === "string"
    && Number.isSafeInteger(value.resets_at)
    && (typeof value.eligible_promo === "string" || value.eligible_promo === null)
    && Number.isSafeInteger(value.resets_in_seconds);
}

function parseUsageLimitStatusMessage(value: string | undefined) {
  if (!value) {
    return null;
  }

  try {
    const parsed: unknown = JSON.parse(value);
    if (!isRecord(parsed)) {
      return null;
    }
    return isUsageLimitErrorDetail(parsed.error) ? parsed.error : null;
  } catch {
    return null;
  }
}

function boolState(value: boolean | undefined, enabledLabel: string, disabledLabel: string, unknownLabel: string) {
  if (value === true) {
    return disabledLabel;
  }
  if (value === false) {
    return enabledLabel;
  }
  return unknownLabel;
}

function statusIntent(snapshot: SidecarAuthSnapshot): BadgeIntent {
  if (snapshot.disabled || snapshot.unavailable) {
    return "danger";
  }
  if (snapshot.next_retry_after) {
    return "warning";
  }
  if (snapshot.status === "healthy" || snapshot.status === "available") {
    return "success";
  }
  return snapshot.status ? "info" : "muted";
}

function quotaStateIntent(quotaState: SidecarAuthQuotaState | undefined): BadgeIntent {
  if (!quotaState || quotaState.disabled || quotaState.quota_state === "unknown") {
    return "muted";
  }
  if (quotaState.quota_state === "healthy") {
    return "success";
  }
  if (quotaState.quota_state === "quota_exceeded") {
    return "warning";
  }
  return "info";
}

function summarizeJson(
  value: unknown,
  bucketSummary: (count: number) => string,
  redactedLabel: string,
) {
  if (value === null || value === undefined) {
    return "—";
  }
  if (Array.isArray(value)) {
    return bucketSummary(value.length);
  }
  if (typeof value === "object") {
    const keys = Object.keys(value as Record<string, unknown>).filter((key) => !/secret|token|key|password|authorization/i.test(key));
    return keys.length > 0 ? keys.slice(0, 3).join(", ") : redactedLabel;
  }
  return redactedLabel;
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

function normalizeAuthSearch(value: string) {
  return value.trim().toLowerCase();
}

function compareAuthText(left: string, right: string) {
  if (left < right) {
    return -1;
  }
  if (left > right) {
    return 1;
  }
  return 0;
}

function compareAuthByName(left: SidecarAuthSnapshot, right: SidecarAuthSnapshot) {
  return compareAuthText(left.name, right.name) || compareAuthText(left.auth_id, right.auth_id);
}

function compareAuthByPriorityAsc(left: SidecarAuthSnapshot, right: SidecarAuthSnapshot) {
  return (left.priority ?? 0) - (right.priority ?? 0) || compareAuthByName(left, right);
}

function buildAuthSearchFields(snapshot: SidecarAuthSnapshot, labels: AuthSearchLabels) {
  const fields = [
    snapshot.name,
    snapshot.auth_id,
    snapshot.auth_index ? `#${snapshot.auth_index}` : undefined,
    snapshot.provider,
    snapshot.label,
    snapshot.status,
    snapshot.status_message,
    labels.priorityLabel(snapshot.priority ?? 0),
    boolState(snapshot.disabled, labels.enabledLabel, labels.disabledLabel, labels.unknownStatus),
  ];

  if (snapshot.unavailable) {
    fields.push(labels.unavailableLabel);
  }
  if (snapshot.priority === undefined) {
    fields.push(labels.missingPriorityLabel);
  }

  return fields.filter((field): field is string => Boolean(field));
}

function authMatchesSearch(snapshot: SidecarAuthSnapshot, normalizedSearch: string, labels: AuthSearchLabels) {
  if (!normalizedSearch) {
    return true;
  }

  return buildAuthSearchFields(snapshot, labels).some((field) => normalizeAuthSearch(field).includes(normalizedSearch));
}

function compareAuthSnapshots(left: SidecarAuthSnapshot, right: SidecarAuthSnapshot, sortMode: AuthSortMode) {
  if (sortMode === "routing-priority-asc") {
    return compareAuthByPriorityAsc(left, right);
  }
  if (sortMode === "routing-priority-desc") {
    return (right.priority ?? 0) - (left.priority ?? 0) || compareAuthByName(left, right);
  }
  return compareAuthByName(left, right);
}

function UsageLimitStatusTooltip({
  badgeIntent,
  badgeLabel,
  error,
  fallback,
  formatNumberValue,
  labels,
  locale,
  notApplicableLabel,
}: {
  badgeIntent: BadgeIntent;
  badgeLabel: string;
  error: UsageLimitErrorDetail;
  fallback: string;
  formatNumberValue: (value: number) => string;
  labels: UsageLimitErrorLabels;
  locale: string;
  notApplicableLabel: string;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          aria-label={`${badgeLabel}: ${labels.title}`}
          className="inline-flex rounded-full outline-none ring-offset-background focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          type="button"
        >
          <StatusBadge label={badgeLabel} intent={badgeIntent} className="cursor-help" />
        </button>
      </TooltipTrigger>
      <TooltipContent
        className="rounded-lg border border-border/70 bg-popover/95 px-2.5 py-1.5 text-xs shadow-sm backdrop-blur-sm"
        side="top"
        sideOffset={4}
      >
        <div className="flex max-w-72 flex-col gap-2">
          <p className="text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
            {labels.title}
          </p>
          <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-2 gap-y-1">
            <dt className="text-muted-foreground">{labels.typeLabel}</dt>
            <dd className="min-w-0"><ValueBadge label={error.type} intent="danger" /></dd>
            <dt className="text-muted-foreground">{labels.messageLabel}</dt>
            <dd className="min-w-0 break-words font-medium text-foreground">{error.message}</dd>
            <dt className="text-muted-foreground">{labels.planTypeLabel}</dt>
            <dd className="min-w-0"><TypeBadge label={error.plan_type} intent="muted" preserveLabel /></dd>
            <dt className="text-muted-foreground">{labels.resetsAtLabel}</dt>
            <dd className="min-w-0 text-foreground">{formatEpochSeconds(error.resets_at, locale, fallback)}</dd>
            <dt className="text-muted-foreground">{labels.resetsInSecondsLabel}</dt>
            <dd className="min-w-0"><ValueBadge label={formatNumberValue(error.resets_in_seconds)} intent="warning" /></dd>
            <dt className="text-muted-foreground">{labels.eligiblePromoLabel}</dt>
            <dd className="min-w-0 text-foreground">{error.eligible_promo ?? notApplicableLabel}</dd>
          </dl>
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

export function AuthFilesTable({
  actionHistory,
  authSnapshots,
  loading,
  mutatingAuthKey,
  onPatchPriority,
  onPatchStatus,
  quotaStates,
}: AuthFilesTableProps) {
  const { formatNumber, locale, messages } = useLocale();
  const copy = messages.sidecarsPage;
  const paginationCopy = messages.requestLogs;
  const [draftPriorities, setDraftPriorities] = useState<Record<string, string>>({});
  const [pendingMutation, setPendingMutation] = useState<PendingMutation | null>(null);
  const [allowWatchdog, setAllowWatchdog] = useState(false);
  const [authSearch, setAuthSearch] = useState("");
  const [authSortMode, setAuthSortMode] = useState<AuthSortMode>("name");
  const [pageSize, setPageSize] = useState<number>(DEFAULT_AUTH_PAGE_SIZE);
  const [pageIndex, setPageIndex] = useState(0);
  const latestAction = useMemo(() => latestActionByAuthId(actionHistory), [actionHistory]);
  const quotaStatesByAuthId = useMemo(
    () => new Map(quotaStates.map((state) => [state.auth_id, state])),
    [quotaStates],
  );
  const normalizedAuthSearch = normalizeAuthSearch(authSearch);
  const authSearchLabels = useMemo<AuthSearchLabels>(() => ({
    disabledLabel: copy.authDisabledLabel,
    enabledLabel: copy.authEnabledLabel,
    missingPriorityLabel: copy.authMissingPriorityResolves,
    priorityLabel: copy.authPriorityLabel,
    unavailableLabel: copy.authUnavailableLabel,
    unknownStatus: copy.unknownStatus,
  }), [
    copy.authDisabledLabel,
    copy.authEnabledLabel,
    copy.authMissingPriorityResolves,
    copy.authPriorityLabel,
    copy.authUnavailableLabel,
    copy.unknownStatus,
  ]);
  const derivedAuthSnapshots = useMemo(() => {
    return authSnapshots
      .filter((snapshot) => authMatchesSearch(snapshot, normalizedAuthSearch, authSearchLabels))
      .sort((left, right) => compareAuthSnapshots(left, right, authSortMode));
  }, [authSearchLabels, authSnapshots, authSortMode, normalizedAuthSearch]);
  const totalAuthRows = derivedAuthSnapshots.length;
  const totalPages = Math.max(1, Math.ceil(totalAuthRows / pageSize));
  const currentPageIndex = Math.min(pageIndex, totalPages - 1);
  const pageStartIndex = currentPageIndex * pageSize;
  const pageEndIndex = Math.min(pageStartIndex + pageSize, totalAuthRows);
  const visibleAuthSnapshots = derivedAuthSnapshots.slice(pageStartIndex, pageEndIndex);
  const pageStart = totalAuthRows > 0 ? pageStartIndex + 1 : 0;
  const hasPreviousPage = currentPageIndex > 0;
  const hasNextPage = pageEndIndex < totalAuthRows;

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
          {copy.authTitle}
        </CardTitle>
        <CardDescription className="text-xs">{copy.authDescription}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {!loading && authSnapshots.length > 0 ? (
          <div className="flex flex-col gap-3 rounded-xl border bg-card p-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="relative w-full xl:max-w-sm">
              <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                aria-label={copy.authFilterLabel}
                name="sidecar_auth_search"
                type="search"
                autoComplete="off"
                placeholder={copy.authFilterPlaceholder}
                value={authSearch}
                onChange={(event) => {
                  setAuthSearch(event.target.value);
                  setPageIndex(0);
                }}
                className="h-9 pl-9"
              />
            </div>

            <Select
              value={authSortMode}
              onValueChange={(value) => {
                setAuthSortMode(value as AuthSortMode);
                setPageIndex(0);
              }}
            >
              <SelectTrigger
                aria-label={copy.authSortLabel}
                className="h-9 w-full sm:w-[240px]"
                data-testid="sidecar-auth-sort-select"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="name">{copy.authSortName}</SelectItem>
                <SelectItem value="routing-priority-desc">{copy.authSortRoutingPriorityDesc}</SelectItem>
                <SelectItem value="routing-priority-asc">{copy.authSortRoutingPriorityAsc}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        ) : null}

        {loading ? (
          <div className="space-y-2">
            <div className="h-14 animate-pulse rounded-md bg-muted/50" />
            <div className="h-14 animate-pulse rounded-md bg-muted/50" />
          </div>
        ) : authSnapshots.length === 0 ? (
          <EmptyState title={copy.authEmptyTitle} description={copy.authEmptyDescription} />
        ) : totalAuthRows === 0 ? (
          <EmptyState title={copy.authFilteredEmptyTitle} description={copy.authFilteredEmptyDescription} />
        ) : (
          <div className="overflow-hidden rounded-md border">
            <ScrollArea className="h-288">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{copy.authAuthFileColumn}</TableHead>
                    <TableHead>{copy.authStateColumn}</TableHead>
                    <TableHead>{copy.quotaStateColumn}</TableHead>
                    <TableHead>{copy.authPriorityColumn}</TableHead>
                    <TableHead>{copy.authRetryColumn}</TableHead>
                    <TableHead>{copy.authRequestsColumn}</TableHead>
                    <TableHead>{copy.authWatchdogColumn}</TableHead>
                    <TableHead className="text-right">{copy.actionsColumn}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visibleAuthSnapshots.map((snapshot) => {
                    const latest = latestAction.get(snapshot.auth_id);
                    const priorityValue = getPriorityInputValue(draftPriorities, snapshot);
                    const parsedPriority = parsePriority(priorityValue);
                    const mutating = mutatingAuthKey === snapshot.auth_id;
                    const usageLimitError = parseUsageLimitStatusMessage(snapshot.status_message);
                    const statusBadgeIntent = usageLimitError ? "danger" : statusIntent(snapshot);
                    const statusBadgeLabel = snapshot.status ?? (usageLimitError ? copy.authUsageLimitTitle : copy.unknownStatus);
                    const quotaState = quotaStatesByAuthId.get(snapshot.auth_id);
                    const latestObservedAt = quotaState?.last_snapshot_at ?? quotaState?.last_probed_at;

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
                            {usageLimitError ? (
                              <UsageLimitStatusTooltip
                                badgeIntent={statusBadgeIntent}
                                badgeLabel={statusBadgeLabel}
                                error={usageLimitError}
                                fallback={messages.common.unavailable}
                                formatNumberValue={formatNumber}
                                labels={{
                                  eligiblePromoLabel: copy.authUsageLimitEligiblePromoLabel,
                                  messageLabel: copy.authUsageLimitMessageLabel,
                                  planTypeLabel: copy.authUsageLimitPlanTypeLabel,
                                  resetsAtLabel: copy.authUsageLimitResetsAtLabel,
                                  resetsInSecondsLabel: copy.authUsageLimitResetsInSecondsLabel,
                                  title: copy.authUsageLimitTitle,
                                  typeLabel: copy.authUsageLimitTypeLabel,
                                }}
                                locale={locale}
                                notApplicableLabel={messages.common.notApplicable}
                              />
                            ) : (
                              <StatusBadge label={statusBadgeLabel} intent={statusBadgeIntent} />
                            )}
                            <TypeBadge label={boolState(snapshot.disabled, copy.authEnabledLabel, copy.authDisabledLabel, copy.unknownStatus)} intent={snapshot.disabled ? "danger" : "success"} preserveLabel />
                            {snapshot.unavailable ? <TypeBadge label={copy.authUnavailableLabel} intent="warning" preserveLabel /> : null}
                            {snapshot.status_message && !usageLimitError ? (
                              <span className="max-w-52 text-xs text-muted-foreground">{snapshot.status_message}</span>
                            ) : null}
                          </div>
                        </TableCell>
                        <TableCell data-testid={`quota-state-${snapshot.auth_id}`}>
                          <div className="flex min-w-48 flex-col gap-1 text-xs text-muted-foreground">
                            <div className="flex flex-wrap items-center gap-1">
                              <StatusBadge
                                label={quotaState ? (copy.quotaStateLabels[quotaState.quota_state] ?? quotaState.quota_state) : copy.quotaStateMissing}
                                intent={quotaStateIntent(quotaState)}
                              />
                              {quotaState?.current_priority !== undefined ? (
                                <ValueBadge
                                  label={copy.authPriorityLabel(quotaState.current_priority)}
                                  intent={quotaState.current_priority === 0 ? "warning" : "info"}
                                />
                              ) : null}
                              {quotaState?.active_hold ? <TypeBadge label={copy.quotaStateWatchdogHold} intent="warning" preserveLabel /> : null}
                            </div>
                            <span>{copy.quotaStateLatestObserved}</span>
                            {latestObservedAt ? <span>{formatTimestamp(latestObservedAt, locale, messages.common.unavailable)}</span> : null}
                            {quotaState?.quota_reason ? <span>{copy.quotaStateReason(quotaState.quota_reason)}</span> : null}
                            {quotaState?.quota_reset_at ? <span>{copy.quotaStateReset(formatTimestamp(quotaState.quota_reset_at, locale, messages.common.unavailable))}</span> : null}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex min-w-44 flex-col gap-2">
                            <div className="flex flex-wrap items-center gap-1">
                              <ValueBadge label={copy.authPriorityLabel(snapshot.priority ?? 0)} intent={snapshot.priority === 0 || snapshot.priority === undefined ? "warning" : "info"} />
                              {snapshot.priority === undefined ? <TypeBadge label={copy.authMissingPriorityResolves} intent="warning" preserveLabel /> : null}
                            </div>
                            <div className="flex items-center gap-2">
                              <Input
                                aria-label={copy.authPriorityInputLabel(snapshot.name)}
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
                                {copy.authSavePriority}
                              </Button>
                            </div>
                            {priorityValue === "0" ? <span className="text-xs text-warning">{copy.authPriority0LastResort}</span> : null}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-col gap-1 text-xs text-muted-foreground">
                            <span>{copy.authRetryLabel}: {formatTimestamp(snapshot.next_retry_after, locale, messages.common.unavailable)}</span>
                            <span>{copy.authObservedLabel}: {formatTimestamp(snapshot.observed_at, locale, messages.common.unavailable)}</span>
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-col gap-1 text-xs text-muted-foreground">
                            <span>{copy.authSuccessRequestsLabel}: {formatNumber(snapshot.success_count ?? 0)}</span>
                            <span>{copy.authFailedRequestsLabel}: {formatNumber(snapshot.failed_count ?? 0)}</span>
                            <span>{copy.authRecentRequestsLabel}: {summarizeJson(snapshot.recent_requests, copy.bucketSummary, copy.redactedLabel)}</span>
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex min-w-44 flex-col gap-1 text-xs text-muted-foreground">
                            {latest ? (
                              <>
                                <TypeBadge label={formatActionType(latest.action_type, copy.actionTypeLabels)} intent={sidecarActionIntent(latest.action_type)} preserveLabel />
                                <span>{formatActionStatus(latest.status, copy.actionStatusLabels)}</span>
                                {latest.hold_until ? <span>{copy.actionHistoryHoldUntil(formatTimestamp(latest.hold_until, locale, messages.common.unavailable))}</span> : null}
                              </>
                            ) : (
                              <span>{copy.authNoWatchdogAction}</span>
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
                              <span className="sr-only">{snapshot.disabled ? copy.authEnableAuth(snapshot.name) : copy.authDisableAuth(snapshot.name)}</span>
                            </IconActionButton>
                          </IconActionGroup>
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </ScrollArea>
            <div className="flex flex-col gap-3 border-t border-border/70 bg-muted/20 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <span>
                  {paginationCopy.resultsRange(
                    formatNumber(pageStart),
                    formatNumber(pageEndIndex),
                    formatNumber(totalAuthRows),
                  )}
                </span>
                <Select
                  value={String(pageSize)}
                  onValueChange={(value) => {
                    setPageSize(Number(value));
                    setPageIndex(0);
                  }}
                >
                  <SelectTrigger
                    className="h-8 w-[92px] rounded-full border-border/70 bg-background text-xs"
                    data-testid="sidecar-auth-page-size-select"
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {AUTH_PAGE_SIZE_OPTIONS.map((size) => (
                      <SelectItem key={size} value={String(size)}>
                        {size}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <span>{paginationCopy.rowsPerPage}</span>
              </div>

              <div className="flex items-center gap-1">
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  className="h-8 w-8 rounded-full"
                  disabled={!hasPreviousPage}
                  onClick={() => setPageIndex(Math.max(0, currentPageIndex - 1))}
                >
                  <ChevronLeft className="h-4 w-4" />
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  className="h-8 w-8 rounded-full"
                  disabled={!hasNextPage}
                  onClick={() => setPageIndex(Math.min(totalPages - 1, currentPageIndex + 1))}
                >
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </div>
            </div>
          </div>
        )}
      </CardContent>

      <AlertDialog open={pendingMutation !== null} onOpenChange={(open) => !open && setPendingMutation(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{copy.authActionConfirmTitle}</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="space-y-3 text-left">
                <p>{copy.authActionConfirmDescription}</p>
                {pendingMutation?.kind === "priority" && pendingMutation.priority === 0 ? (
                  <p className="font-medium text-warning">{copy.authPriority0MutationWarning}</p>
                ) : null}
                <div className="flex items-start gap-2 rounded-lg border bg-muted/20 p-3">
                  <Checkbox
                    id="allow-watchdog-immediately"
                    checked={allowWatchdog}
                    onCheckedChange={(checked) => setAllowWatchdog(checked === true)}
                  />
                  <div className="grid gap-1 leading-none">
                    <Label htmlFor="allow-watchdog-immediately">{copy.authActionAllowWatchdogLabel}</Label>
                    <p className="text-xs text-muted-foreground">{copy.authActionAllowWatchdogDescription}</p>
                  </div>
                </div>
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{copy.cancel}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void confirmMutation()}>{copy.actionApplyChange}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}
