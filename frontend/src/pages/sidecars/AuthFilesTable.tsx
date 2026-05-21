import { type ComponentProps, useMemo, useState } from "react";
import { AlertTriangle, Boxes, Check, ChevronLeft, ChevronRight, Loader2, PencilLine, Power, PowerOff, RefreshCw, Search, SlidersHorizontal, Trash2 } from "lucide-react";
import { EmptyState } from "@/components/EmptyState";
import { StatusBadge, TypeBadge, ValueBadge, type BadgeIntent } from "@/components/StatusBadge";
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
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
import { Textarea } from "@/components/ui/textarea";
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
import type { SidecarAuthModel, SidecarAuthModelsResponse, SidecarAuthMutationFieldsInput, SidecarAuthSnapshot, SidecarAuthTraceHeaderName } from "@/lib/types";

type FormSubmitEvent = Parameters<NonNullable<ComponentProps<"form">["onSubmit"]>>[0];

type PendingMutation =
  | { kind: "priority"; snapshot: SidecarAuthSnapshot; priority: number }
  | { kind: "status"; snapshot: SidecarAuthSnapshot; disabled: boolean };

export type AuthMutationNoticeKind = "success" | "stale_snapshot" | "failed" | "refresh_failed";

export type AuthFieldsPatchPayload = Omit<SidecarAuthMutationFieldsInput, "force_live">;

type AuthMutationRetry =
  | { kind: "status"; disabled: boolean }
  | { kind: "fields"; fields: AuthFieldsPatchPayload };

export interface AuthMutationNotice {
  kind: AuthMutationNoticeKind;
  message: string;
  retry?: AuthMutationRetry;
}

type AuthSortMode = "name" | "routing-priority-desc" | "routing-priority-asc";

interface AuthSearchLabels {
  enabledLabel: string;
  disabledLabel: string;
  missingPriorityLabel: string;
  priorityLabel: (priority: number) => string;
  unavailableLabel: string;
  unobservedStatus: string;
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
  authSnapshots: SidecarAuthSnapshot[];
  authMutationNotices: Record<string, AuthMutationNotice | undefined>;
  loading: boolean;
  mutatingAuthKey: string | null;
  onDeleteAuthFile: (snapshot: SidecarAuthSnapshot, confirmName: string) => Promise<void>;
  onLoadModels: (snapshot: SidecarAuthSnapshot) => Promise<SidecarAuthModelsResponse>;
  onPatchFields: (snapshot: SidecarAuthSnapshot, fields: AuthFieldsPatchPayload, options?: { forceLive?: boolean }) => Promise<void>;
  onPatchPriority: (snapshot: SidecarAuthSnapshot, priority: number) => Promise<void>;
  onPatchStatus: (snapshot: SidecarAuthSnapshot, disabled: boolean, options?: { forceLive?: boolean }) => Promise<void>;
}

const DEFAULT_AUTH_PAGE_SIZE = 30;
const AUTH_FIELD_TRACE_HEADERS = ["x-correlation-id", "x-request-id", "x-trace-id"] as const satisfies readonly SidecarAuthTraceHeaderName[];

type AuthFieldDraft = {
  headers: Record<SidecarAuthTraceHeaderName, string>;
  note: string;
  prefix: string;
  proxy_url: string;
};

type AuthModelsDialogStatus = "loading" | "loaded" | "unsupported" | "error";

type AuthModelsDialogState = {
  error?: string;
  models: SidecarAuthModel[];
  snapshot: SidecarAuthSnapshot;
  status: AuthModelsDialogStatus;
};

type AuthDeleteDialogState = {
  confirmName: string;
  snapshot: SidecarAuthSnapshot;
};

function createEmptyAuthFieldDraft(): AuthFieldDraft {
  return {
    headers: {
      "x-correlation-id": "",
      "x-request-id": "",
      "x-trace-id": "",
    },
    note: "",
    prefix: "",
    proxy_url: "",
  };
}

function optionalTrimmedString(value: string) {
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
}

function buildAuthFieldsPatch(draft: AuthFieldDraft): AuthFieldsPatchPayload | null {
  const patch: AuthFieldsPatchPayload = {};
  const prefix = optionalTrimmedString(draft.prefix);
  const proxyUrl = optionalTrimmedString(draft.proxy_url);
  const note = optionalTrimmedString(draft.note);
  const headers: Partial<Record<SidecarAuthTraceHeaderName, string>> = {};

  if (prefix) patch.prefix = prefix;
  if (proxyUrl) patch.proxy_url = proxyUrl;
  if (note) patch.note = note;

  for (const headerName of AUTH_FIELD_TRACE_HEADERS) {
    const headerValue = optionalTrimmedString(draft.headers[headerName]);
    if (headerValue) {
      headers[headerName] = headerValue;
    }
  }

  if (Object.keys(headers).length > 0) {
    patch.headers = headers;
  }

  return Object.keys(patch).length > 0 ? patch : null;
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

function errorDetailText(error: unknown) {
  const message = error instanceof Error ? error.message : "";
  if (isRecord(error)) {
    const detail = error.detail;
    if (typeof detail === "string") {
      return `${message} ${detail}`.trim();
    }
    if (isRecord(detail) && typeof detail.detail === "string") {
      return `${message} ${detail.detail}`.trim();
    }
  }
  return message;
}

function isUnsupportedModelsError(error: unknown) {
  const status = isRecord(error) && typeof error.status === "number" ? error.status : 0;
  const detail = errorDetailText(error);
  return status === 404 || /\b(404|not found|unsupported)\b/i.test(detail);
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

function boolState(value: boolean | undefined, enabledLabel: string, disabledLabel: string, fallbackLabel: string) {
  if (value === true) {
    return disabledLabel;
  }
  if (value === false) {
    return enabledLabel;
  }
  return fallbackLabel;
}

function statusIntent(snapshot: SidecarAuthSnapshot): BadgeIntent {
  if (snapshot.disabled || snapshot.unavailable) {
    return "danger";
  }
  if (snapshot.status === "active" || snapshot.status === "available") {
    return "success";
  }
  return snapshot.status ? "info" : "muted";
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

function getPriorityInputValue(
  drafts: Record<string, string>,
  snapshot: SidecarAuthSnapshot,
) {
  return drafts[snapshot.auth_id] ?? (snapshot.priority === undefined ? "" : String(snapshot.priority));
}

function parsePriority(value: string) {
  const trimmed = value.trim();
  if (!/^\d+$/.test(trimmed)) {
    return null;
  }
  const parsed = Number(trimmed);
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : null;
}

function normalizeAuthSearch(value: string) {
  return value.trim().toLowerCase();
}

function hasStableMutationIdentity(snapshot: SidecarAuthSnapshot) {
  const authId = snapshot.auth_id.trim();
  const name = snapshot.name.trim();
  if (!authId || !name) {
    return false;
  }
  return Boolean(snapshot.auth_index?.trim()) || authId !== name;
}

function isStatusMutationEligible(snapshot: SidecarAuthSnapshot, snapshots: SidecarAuthSnapshot[]) {
  if (typeof snapshot.disabled !== "boolean" || snapshot.unavailable || !hasStableMutationIdentity(snapshot)) {
    return false;
  }

  const authIdMatches = snapshots.filter((candidate) => candidate.auth_id === snapshot.auth_id).length;
  const nameMatches = snapshots.filter((candidate) => candidate.name === snapshot.name).length;
  return authIdMatches === 1 && nameMatches === 1;
}

function isPathLikeAuthName(name: string) {
  const trimmed = name.trim();
  return !trimmed || trimmed.includes("/") || trimmed.includes("\\");
}

function isDeleteMetadataEligible(snapshot: SidecarAuthSnapshot) {
  if (!isRecord(snapshot.snapshot)) {
    return false;
  }
  const runtimeOnly = snapshot.snapshot.runtime_only;
  const source = typeof snapshot.snapshot.source === "string" ? snapshot.snapshot.source.trim().toLowerCase() : "";
  return snapshot.snapshot.delete_supported === true
    && runtimeOnly === false
    && source === "file"
    && snapshot.snapshot.path_present === true;
}

function isDeleteMutationEligible(snapshot: SidecarAuthSnapshot, snapshots: SidecarAuthSnapshot[]) {
  return isStatusMutationEligible(snapshot, snapshots)
    && !isPathLikeAuthName(snapshot.name)
    && isDeleteMetadataEligible(snapshot);
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
    boolState(snapshot.disabled, labels.enabledLabel, labels.disabledLabel, labels.unobservedStatus),
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

function statusNoticeClasses(kind: AuthMutationNoticeKind) {
  if (kind === "success") {
    return "border-success/30 bg-success/10 text-success dark:text-success";
  }
  if (kind === "failed") {
    return "border-destructive/30 bg-destructive/10 text-destructive";
  }
  return "border-warning/30 bg-warning/10 text-warning-foreground dark:text-warning";
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

function AuthMutationNoticeCard({
  mutating,
  notice,
  onRetry,
  retryLabel,
}: {
  mutating: boolean;
  notice: AuthMutationNotice;
  onRetry: () => void;
  retryLabel: string;
}) {
  return (
    <div className={`flex max-w-64 flex-col gap-2 rounded-md border px-2 py-1.5 text-xs ${statusNoticeClasses(notice.kind)}`}>
      <div className="flex items-start gap-1.5">
        <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
        <span>{notice.message}</span>
      </div>
      {notice.kind === "stale_snapshot" && notice.retry ? (
        <Button
          type="button"
          size="xs"
          variant="outline"
          className="w-fit bg-background/70"
          disabled={mutating}
          onClick={onRetry}
        >
          {mutating ? <Loader2 className="h-3 w-3 animate-spin" /> : <RefreshCw className="h-3 w-3" />}
          {retryLabel}
        </Button>
      ) : null}
    </div>
  );
}

type AuthFieldsDialogProps = {
  draft: AuthFieldDraft;
  mutating: boolean;
  onClose: () => void;
  onDraftChange: (draft: AuthFieldDraft) => void;
  onSubmit: (event: FormSubmitEvent) => void;
  open: boolean;
  patch: AuthFieldsPatchPayload | null;
  snapshot: SidecarAuthSnapshot | null;
};

function AuthModelsDialog({
  onClose,
  state,
}: {
  onClose: () => void;
  state: AuthModelsDialogState | null;
}) {
  const { messages } = useLocale();
  const copy = messages.sidecarsPage;
  const open = state !== null;
  const models = state?.models ?? [];

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && onClose()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{copy.authModelsTitle(state?.snapshot.name ?? "")}</DialogTitle>
          <DialogDescription>{copy.authModelsDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="space-y-4">
          <p className="rounded-md border border-info/30 bg-info/10 px-3 py-2 text-xs text-info dark:text-info">
            {copy.authModelsReadOnlyHint}
          </p>
          {state?.status === "loading" ? (
            <div className="space-y-2" aria-live="polite">
              <div className="h-12 animate-pulse rounded-md bg-muted/50" />
              <div className="h-12 animate-pulse rounded-md bg-muted/50" />
              <span className="sr-only">{copy.authModelsLoading}</span>
            </div>
          ) : null}
          {state?.status === "unsupported" ? (
            <EmptyState icon={<AlertTriangle className="h-6 w-6" />} title={copy.authModelsUnsupportedTitle} description={copy.authModelsUnsupportedDescription} />
          ) : null}
          {state?.status === "error" ? (
            <EmptyState icon={<AlertTriangle className="h-6 w-6" />} title={copy.authModelsErrorTitle} description={state.error ?? copy.authModelsErrorDescription} />
          ) : null}
          {state?.status === "loaded" && models.length === 0 ? (
            <EmptyState icon={<Boxes className="h-6 w-6" />} title={copy.authModelsEmptyTitle} description={copy.authModelsEmptyDescription} />
          ) : null}
          {state?.status === "loaded" && models.length > 0 ? (
            <div className="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{copy.authModelsIdColumn}</TableHead>
                    <TableHead>{copy.authModelsDisplayNameColumn}</TableHead>
                    <TableHead>{copy.authModelsTypeColumn}</TableHead>
                    <TableHead>{copy.authModelsOwnedByColumn}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {models.map((model) => (
                    <TableRow key={model.id}>
                      <TableCell><span className="font-mono text-xs">{model.id}</span></TableCell>
                      <TableCell>{model.display_name ?? messages.common.unavailable}</TableCell>
                      <TableCell>{model.type ? <TypeBadge label={model.type} intent="info" preserveLabel /> : messages.common.unavailable}</TableCell>
                      <TableCell>{model.owned_by ? <ValueBadge label={model.owned_by} intent="muted" /> : messages.common.unavailable}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>{messages.common.close}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function AuthFieldsDialog({
  draft,
  mutating,
  onClose,
  onDraftChange,
  onSubmit,
  open,
  patch,
  snapshot,
}: AuthFieldsDialogProps) {
  const { messages } = useLocale();
  const copy = messages.sidecarsPage;

  const updateField = (field: "prefix" | "proxy_url" | "note", value: string) => {
    onDraftChange({ ...draft, [field]: value });
  };
  const updateHeader = (headerName: SidecarAuthTraceHeaderName, value: string) => {
    onDraftChange({ ...draft, headers: { ...draft.headers, [headerName]: value } });
  };

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && onClose()}>
      <DialogContent className="sm:max-w-2xl" showCloseButton={!mutating}>
        <form className="contents" onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{copy.authFieldsEditTitle}</DialogTitle>
            <DialogDescription>{copy.authFieldsEditDescription(snapshot?.name ?? "")}</DialogDescription>
          </DialogHeader>
          <DialogBody className="space-y-4">
            <div className="rounded-xl border bg-muted/20 p-4">
              <p className="text-sm font-medium">{copy.authFieldsOperationalTitle}</p>
              <p className="mt-1 text-xs text-muted-foreground">{copy.authFieldsPreserveHint}</p>
              <div className="mt-4 grid gap-4 md:grid-cols-2">
                <div className="flex flex-col gap-2">
                  <Label htmlFor="auth-fields-prefix">{copy.authFieldsPrefixLabel}</Label>
                  <Input id="auth-fields-prefix" value={draft.prefix} placeholder={copy.authFieldsPrefixPlaceholder} onChange={(event) => updateField("prefix", event.target.value)} />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="auth-fields-proxy-url">{copy.authFieldsProxyUrlLabel}</Label>
                  <Input id="auth-fields-proxy-url" value={draft.proxy_url} placeholder={copy.authFieldsProxyUrlPlaceholder} onChange={(event) => updateField("proxy_url", event.target.value)} />
                </div>
                <div className="flex flex-col gap-2 md:col-span-2">
                  <Label htmlFor="auth-fields-note">{copy.authFieldsNoteLabel}</Label>
                  <Textarea id="auth-fields-note" value={draft.note} placeholder={copy.authFieldsNotePlaceholder} onChange={(event) => updateField("note", event.target.value)} />
                </div>
              </div>
            </div>
            <div className="rounded-xl border bg-muted/20 p-4">
              <p className="text-sm font-medium">{copy.authFieldsTraceHeadersTitle}</p>
              <p className="mt-1 text-xs text-muted-foreground">{copy.authFieldsTraceHeadersDescription}</p>
              <div className="mt-4 grid gap-4 md:grid-cols-3">
                {AUTH_FIELD_TRACE_HEADERS.map((headerName) => (
                  <div key={headerName} className="flex flex-col gap-2">
                    <Label htmlFor={`auth-fields-${headerName}`}>{headerName}</Label>
                    <Input id={`auth-fields-${headerName}`} value={draft.headers[headerName]} placeholder={copy.authFieldsHeaderPlaceholder(headerName)} onChange={(event) => updateHeader(headerName, event.target.value)} />
                  </div>
                ))}
              </div>
            </div>
            <p className="rounded-md border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning-foreground dark:text-warning">{copy.authFieldsNoClearHint}</p>
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose} disabled={mutating}>{copy.cancel}</Button>
            <Button type="submit" disabled={mutating || patch === null}>{mutating ? <Loader2 className="h-4 w-4 animate-spin" /> : null}{copy.authFieldsApply}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function AuthDeleteDialog({
  dialog,
  mutating,
  onClose,
  onConfirmNameChange,
  onSubmit,
}: {
  dialog: AuthDeleteDialogState | null;
  mutating: boolean;
  onClose: () => void;
  onConfirmNameChange: (value: string) => void;
  onSubmit: (event: FormSubmitEvent) => void;
}) {
  const { messages } = useLocale();
  const copy = messages.sidecarsPage;
  const snapshot = dialog?.snapshot ?? null;
  const confirmName = dialog?.confirmName ?? "";
  const mismatch = snapshot !== null && confirmName.length > 0 && confirmName !== snapshot.name;
  const canSubmit = snapshot !== null && confirmName === snapshot.name && !mutating;

  return (
    <Dialog open={snapshot !== null} onOpenChange={(nextOpen) => !nextOpen && !mutating && onClose()}>
      <DialogContent className="sm:max-w-md" showCloseButton={!mutating}>
        <form className="contents" onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{copy.authDeleteTitle}</DialogTitle>
            <DialogDescription>{copy.authDeleteDescription(snapshot?.name ?? "")}</DialogDescription>
          </DialogHeader>
          <DialogBody>
            <div className="flex flex-col gap-4 rounded-lg border border-destructive/25 bg-destructive/5 p-4 text-sm">
              <div className="flex items-start gap-3">
                <div className="mt-0.5 rounded-full bg-destructive/10 p-2 text-destructive">
                  <Trash2 className="h-4 w-4" />
                </div>
                <div className="flex min-w-0 flex-col gap-1">
                  <p className="font-medium text-destructive">{copy.authDeleteWarningTitle}</p>
                  <p className="text-muted-foreground">{copy.authDeleteWarningDescription}</p>
                  {snapshot ? <p className="break-all font-mono text-xs text-muted-foreground">{snapshot.name}</p> : null}
                </div>
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="auth-delete-confirm-name">{copy.authDeleteConfirmNameLabel}</Label>
                <Input
                  id="auth-delete-confirm-name"
                  value={confirmName}
                  autoComplete="off"
                  disabled={mutating}
                  aria-invalid={mismatch}
                  placeholder={snapshot?.name ?? ""}
                  onChange={(event) => onConfirmNameChange(event.target.value)}
                />
                <p className="text-xs text-muted-foreground">{copy.authDeleteConfirmNameHint(snapshot?.name ?? "")}</p>
                {mismatch ? <p className="text-xs text-destructive">{copy.authDeleteConfirmNameMismatch}</p> : null}
              </div>
            </div>
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose} disabled={mutating}>{copy.cancel}</Button>
            <Button type="submit" variant="destructive" disabled={!canSubmit}>
              {mutating ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
              {mutating ? copy.authDeleteDeleting : copy.authDeleteAction}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function AuthFilesTable({
  authSnapshots,
  authMutationNotices,
  loading,
  mutatingAuthKey,
  onDeleteAuthFile,
  onLoadModels,
  onPatchFields,
  onPatchPriority,
  onPatchStatus,
}: AuthFilesTableProps) {
  const { formatNumber, locale, messages } = useLocale();
  const copy = messages.sidecarsPage;
  const paginationCopy = messages.requestLogs;
  const [draftPriorities, setDraftPriorities] = useState<Record<string, string>>({});
  const [pendingMutation, setPendingMutation] = useState<PendingMutation | null>(null);
  const [fieldsDialogSnapshot, setFieldsDialogSnapshot] = useState<SidecarAuthSnapshot | null>(null);
  const [authModelsState, setAuthModelsState] = useState<AuthModelsDialogState | null>(null);
  const [authDeleteDialog, setAuthDeleteDialog] = useState<AuthDeleteDialogState | null>(null);
  const [authFieldDraft, setAuthFieldDraft] = useState<AuthFieldDraft>(() => createEmptyAuthFieldDraft());
  const [authSearch, setAuthSearch] = useState("");
  const [authSortMode, setAuthSortMode] = useState<AuthSortMode>("name");
  const [pageIndex, setPageIndex] = useState(0);
  const pageSize = DEFAULT_AUTH_PAGE_SIZE;
  const normalizedAuthSearch = normalizeAuthSearch(authSearch);
  const authSearchLabels = useMemo<AuthSearchLabels>(() => ({
    disabledLabel: copy.authDisabledLabel,
    enabledLabel: copy.authEnabledLabel,
    missingPriorityLabel: copy.authMissingPriorityResolves,
    priorityLabel: copy.authPriorityLabel,
    unavailableLabel: copy.authUnavailableLabel,
    unobservedStatus: copy.authUnobservedLabel,
  }), [
    copy.authDisabledLabel,
    copy.authEnabledLabel,
    copy.authMissingPriorityResolves,
    copy.authPriorityLabel,
    copy.authUnavailableLabel,
    copy.authUnobservedLabel,
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
  const authFieldsPatch = buildAuthFieldsPatch(authFieldDraft);
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
  };

  const openFieldsDialog = (snapshot: SidecarAuthSnapshot) => {
    setAuthFieldDraft(createEmptyAuthFieldDraft());
    setFieldsDialogSnapshot(snapshot);
  };

  const closeFieldsDialog = () => {
    setFieldsDialogSnapshot(null);
    setAuthFieldDraft(createEmptyAuthFieldDraft());
  };

  const openModelsDialog = async (snapshot: SidecarAuthSnapshot) => {
    setAuthModelsState({ models: [], snapshot, status: "loading" });
    try {
      const response = await onLoadModels(snapshot);
      setAuthModelsState((current) => current?.snapshot.auth_id === snapshot.auth_id ? {
        models: response.models ?? [],
        snapshot,
        status: "loaded",
      } : current);
    } catch (error) {
      setAuthModelsState((current) => current?.snapshot.auth_id === snapshot.auth_id ? {
        error: errorDetailText(error) || copy.authModelsErrorDescription,
        models: [],
        snapshot,
        status: isUnsupportedModelsError(error) ? "unsupported" : "error",
      } : current);
    }
  };

  const closeModelsDialog = () => {
    setAuthModelsState(null);
  };

  const openDeleteDialog = (snapshot: SidecarAuthSnapshot) => {
    setAuthDeleteDialog({ confirmName: "", snapshot });
  };

  const closeDeleteDialog = () => {
    setAuthDeleteDialog(null);
  };

  const submitDeleteDialog = async (event: FormSubmitEvent) => {
    event.preventDefault();
    if (!authDeleteDialog || authDeleteDialog.confirmName !== authDeleteDialog.snapshot.name) {
      return;
    }
    await onDeleteAuthFile(authDeleteDialog.snapshot, authDeleteDialog.confirmName);
    closeDeleteDialog();
  };

  const submitFieldsDialog = async (event: FormSubmitEvent) => {
    event.preventDefault();
    if (!fieldsDialogSnapshot || !authFieldsPatch) {
      return;
    }
    await onPatchFields(fieldsDialogSnapshot, authFieldsPatch);
    closeFieldsDialog();
  };

  const retryMutation = (snapshot: SidecarAuthSnapshot, notice: AuthMutationNotice) => {
    if (notice.retry?.kind === "status") {
      void onPatchStatus(snapshot, notice.retry.disabled, { forceLive: true });
    }
    if (notice.retry?.kind === "fields") {
      void onPatchFields(snapshot, notice.retry.fields, { forceLive: true });
    }
  };

  const confirmMutation = async () => {
    if (!pendingMutation) {
      return;
    }
    if (pendingMutation.kind === "priority") {
      await onPatchPriority(pendingMutation.snapshot, pendingMutation.priority);
      clearDraftPriority(pendingMutation.snapshot.auth_id);
    } else {
      await onPatchStatus(pendingMutation.snapshot, pendingMutation.disabled);
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
            <div className="overflow-x-auto" data-testid="sidecar-auth-files-scroll">
              <div className="min-w-[960px]">
                <ScrollArea className="h-288">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{copy.authAuthFileColumn}</TableHead>
                        <TableHead>{copy.authStateColumn}</TableHead>
                        <TableHead>{copy.authPriorityColumn}</TableHead>
                        <TableHead>{copy.authObservedLabel}</TableHead>
                        <TableHead>{copy.authRequestsColumn}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {visibleAuthSnapshots.map((snapshot) => {
                        const priorityValue = getPriorityInputValue(draftPriorities, snapshot);
                        const parsedPriority = parsePriority(priorityValue);
                        const mutating = mutatingAuthKey === snapshot.auth_id;
                        const usageLimitError = parseUsageLimitStatusMessage(snapshot.status_message);
                        const statusBadgeIntent = usageLimitError ? "danger" : statusIntent(snapshot);
                        const statusBadgeLabel = snapshot.status ?? (usageLimitError ? copy.authUsageLimitTitle : copy.authUnobservedLabel);
                        const canMutateAuth = isStatusMutationEligible(snapshot, authSnapshots);
                        const canDeleteAuth = isDeleteMutationEligible(snapshot, authSnapshots);
                        const nextDisabled = !snapshot.disabled;
                        const mutationNotice = authMutationNotices[snapshot.auth_id];

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
                                <Button
                                  type="button"
                                  size="xs"
                                  variant="outline"
                                  className="w-fit"
                                  aria-label={copy.authModelsFor(snapshot.name)}
                                  disabled={authModelsState?.status === "loading" && authModelsState.snapshot.auth_id === snapshot.auth_id}
                                  onClick={() => void openModelsDialog(snapshot)}
                                >
                                  {authModelsState?.status === "loading" && authModelsState.snapshot.auth_id === snapshot.auth_id ? <Loader2 className="h-3 w-3 animate-spin" /> : <Boxes className="h-3 w-3" />}
                                  {copy.authModelsAction}
                                </Button>
                                {canDeleteAuth ? (
                                  <Button
                                    type="button"
                                    size="xs"
                                    variant="outline"
                                    className="w-fit text-destructive hover:text-destructive"
                                    aria-label={copy.authDeleteFor(snapshot.name)}
                                    disabled={mutating}
                                    onClick={() => openDeleteDialog(snapshot)}
                                  >
                                    {mutating ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
                                    {copy.authDeleteAction}
                                  </Button>
                                ) : null}
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
                                <TypeBadge label={boolState(snapshot.disabled, copy.authEnabledLabel, copy.authDisabledLabel, copy.authUnobservedLabel)} intent={snapshot.disabled ? "danger" : "success"} preserveLabel />
                                {snapshot.unavailable ? <TypeBadge label={copy.authUnavailableLabel} intent="warning" preserveLabel /> : null}
                                {snapshot.status_message && !usageLimitError ? (
                                  <span className="max-w-52 text-xs text-muted-foreground">{snapshot.status_message}</span>
                                ) : null}
                                {canMutateAuth ? (
                                  <Button
                                    type="button"
                                    size="xs"
                                    variant={snapshot.disabled ? "default" : "outline"}
                                    className="w-fit"
                                    aria-label={snapshot.disabled ? copy.authEnableAuth(snapshot.name) : copy.authDisableAuth(snapshot.name)}
                                    disabled={mutating}
                                    onClick={() => openMutation({ kind: "status", snapshot, disabled: nextDisabled })}
                                  >
                                    {mutating ? (
                                      <Loader2 className="h-3 w-3 animate-spin" />
                                    ) : snapshot.disabled ? (
                                      <Power className="h-3 w-3" />
                                    ) : (
                                      <PowerOff className="h-3 w-3" />
                                    )}
                                    {snapshot.disabled ? copy.authEnableAction : copy.authDisableAction}
                                  </Button>
                                ) : null}
                                {mutationNotice ? (
                                  <AuthMutationNoticeCard
                                    mutating={mutating}
                                    notice={mutationNotice}
                                    retryLabel={copy.authStatusRetryLive}
                                    onRetry={() => retryMutation(snapshot, mutationNotice)}
                                  />
                                ) : null}
                              </div>
                            </TableCell>
                            <TableCell>
                              <div className="flex min-w-44 flex-col gap-2">
                                <div className="flex flex-wrap items-center gap-1">
                                  {snapshot.priority !== undefined ? <ValueBadge label={copy.authPriorityLabel(snapshot.priority)} intent="info" /> : null}
                                  {snapshot.priority === undefined ? <TypeBadge label={copy.authMissingPriorityResolves} intent="info" preserveLabel /> : null}
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
                                {parsedPriority === null ? <span className="text-xs text-warning">{copy.authPriorityValueRequired}</span> : null}
                                {canMutateAuth ? (
                                  <Button
                                    type="button"
                                    size="xs"
                                    variant="outline"
                                    className="w-fit"
                                    aria-label={copy.authFieldsEditFor(snapshot.name)}
                                    disabled={mutating}
                                    onClick={() => openFieldsDialog(snapshot)}
                                  >
                                    {mutating ? <Loader2 className="h-3 w-3 animate-spin" /> : <PencilLine className="h-3 w-3" />}
                                    {copy.authFieldsEditAction}
                                  </Button>
                                ) : null}
                              </div>
                            </TableCell>
                            <TableCell>
                              <div className="text-xs text-muted-foreground">
                                {formatTimestamp(snapshot.observed_at, locale, messages.common.unavailable)}
                              </div>
                            </TableCell>
                            <TableCell>
                              <div className="flex flex-col gap-1 text-xs text-muted-foreground">
                                <span>{copy.authSuccessRequestsLabel}: {formatNumber(snapshot.success_count ?? 0)}</span>
                                <span>{copy.authFailedRequestsLabel}: {formatNumber(snapshot.failed_count ?? 0)}</span>
                                <span>{copy.authRecentRequestsLabel}: {summarizeJson(snapshot.recent_requests, copy.bucketSummary, copy.redactedLabel)}</span>
                              </div>
                            </TableCell>
                          </TableRow>
                        );
                      })}
                    </TableBody>
                  </Table>
                </ScrollArea>
              </div>
            </div>
            <div className="flex flex-col gap-3 border-t border-border/70 bg-muted/20 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <span>
                  {paginationCopy.resultsRange(
                    formatNumber(pageStart),
                    formatNumber(pageEndIndex),
                    formatNumber(totalAuthRows),
                  )}
                </span>
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

      <AuthModelsDialog
        onClose={closeModelsDialog}
        state={authModelsState}
      />

      <AuthFieldsDialog
        draft={authFieldDraft}
        mutating={fieldsDialogSnapshot !== null && mutatingAuthKey === fieldsDialogSnapshot.auth_id}
        onClose={closeFieldsDialog}
        onDraftChange={setAuthFieldDraft}
        onSubmit={submitFieldsDialog}
        open={fieldsDialogSnapshot !== null}
        patch={authFieldsPatch}
        snapshot={fieldsDialogSnapshot}
      />

      <AuthDeleteDialog
        dialog={authDeleteDialog}
        mutating={authDeleteDialog !== null && mutatingAuthKey === authDeleteDialog.snapshot.auth_id}
        onClose={closeDeleteDialog}
        onConfirmNameChange={(value) => setAuthDeleteDialog((current) => current ? { ...current, confirmName: value } : current)}
        onSubmit={submitDeleteDialog}
      />

      <AlertDialog open={pendingMutation !== null} onOpenChange={(open) => !open && setPendingMutation(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{copy.authActionConfirmTitle}</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="space-y-3 text-left">
                <p>{copy.authActionConfirmDescription}</p>
                {pendingMutation?.kind === "priority" ? (
                  <p className="font-medium text-warning">
                    {pendingMutation.priority === 0 ? copy.authPriorityClearMutationWarning : copy.authPriorityMutationWarning}
                  </p>
                ) : null}
                {pendingMutation?.kind === "status" ? (
                  <p className="font-medium text-warning">
                    {copy.authStatusMutationWarning(pendingMutation.disabled)}
                  </p>
                ) : null}
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
