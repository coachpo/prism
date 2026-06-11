import type { ReactNode } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { AlertTriangle, ArrowLeft, FileSearch, ShieldOff, Terminal } from "lucide-react";
import { PageHeader } from "@/components/PageHeader";
import { ValueBadge } from "@/components/StatusBadge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import type { ApiFamily, AuditLogDetail, AuditLogListItem } from "@/lib/types";
import { resolveRequestAuditCaptureMode, type RequestAuditCaptureMode } from "./requestLogAuditState";
import { RequestLogPayloadBlock } from "./detail/RequestLogPayloadBlock";
import { getStatusIntent } from "./detail/requestLogDetailUtils";
import {
  useDedicatedRequestLogAudit,
  type DedicatedRequestLogAuditStatus,
} from "./useDedicatedRequestLogAudit";

function parsePositiveInteger(value: string | null | undefined): number | null {
  if (!value) return null;
  const trimmed = value.trim().replace(/^#/, "");
  if (!/^\d+$/.test(trimmed)) return null;
  const parsed = Number(trimmed);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
}
function captureBadgeClassName(mode: RequestAuditCaptureMode | null): string {
  if (mode === "metadata_only") return "border-info/25 bg-info/10 text-info";
  if (mode === "full") return "border-success/25 bg-success/10 text-success";
  return "border-border/70 bg-muted text-muted-foreground";
}

function getCaptureLabel(
  mode: RequestAuditCaptureMode | null,
  messages: ReturnType<typeof useLocale>["messages"],
): string {
  if (mode === "metadata_only") return messages.requestLogs.auditMetadataOnly;
  if (mode === "full") return messages.requestLogs.auditFullCapture;
  return messages.requestLogs.auditDisabledAtRequest;
}

function getSelectedAuditPath(requestId: number, auditId: number): string {
  return `/request-logs/${requestId}/audit?audit_id=${auditId}`;
}
function StatusPanel({
  action,
  description,
  status,
  title,
}: {
  action?: ReactNode;
  description: string;
  status: "neutral" | "warning" | "error";
  title: string;
}) {
  const icon = status === "neutral"
    ? <ShieldOff className="mt-0.5 size-5 shrink-0 text-muted-foreground" />
    : <AlertTriangle className="mt-0.5 size-5 shrink-0 text-warning" />;

  return (
    <Card className={status === "error" ? "border-destructive/35" : "border-border/70"}>
      <CardContent className="flex flex-col gap-4 pt-0 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex items-start gap-3">
          {icon}
          <div className="flex flex-col gap-1">
            <p className="text-sm font-medium">{title}</p>
            <p className="max-w-2xl text-sm text-muted-foreground">{description}</p>
          </div>
        </div>
        {action ? <div className="shrink-0">{action}</div> : null}
      </CardContent>
    </Card>
  );
}
function LoadingCard() {
  return (
    <Card className="border-border/70">
      <CardHeader>
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-4 w-72" />
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Skeleton className="h-24 w-full rounded-xl" />
        <Skeleton className="h-24 w-full rounded-xl" />
      </CardContent>
    </Card>
  );
}

function AuditList({
  auditItems,
  requestId,
  selectedAuditId,
}: {
  auditItems: AuditLogListItem[];
  requestId: number;
  selectedAuditId: number | null;
}) {
  const { formatNumber, messages } = useLocale();
  const { format } = useTimezone();
  return (
    <Card className="border-border/70" data-testid="dedicated-audit-list">
      <CardHeader>
        <CardTitle>{messages.requestLogs.auditRecordList}</CardTitle>
        <CardDescription>
          {messages.requestLogs.auditRecordListDescription(formatNumber(auditItems.length))}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        {auditItems.map((item) => {
          const captureMode = resolveRequestAuditCaptureMode(item);
          const isSelected = item.id === selectedAuditId;
          return (
            <Button key={item.id} variant={isSelected ? "secondary" : "outline"} asChild className="h-auto justify-start px-3 py-2">
              <Link to={getSelectedAuditPath(requestId, item.id)}>
                <span className="flex min-w-0 flex-1 flex-col items-start gap-1 text-left">
                  <span className="flex flex-wrap items-center gap-2">
                    <ValueBadge label={String(item.response_status)} intent={getStatusIntent(item.response_status)} />
                    <Badge variant="outline" className={captureBadgeClassName(captureMode)}>
                      {getCaptureLabel(captureMode, messages)}
                    </Badge>
                    <span className="font-mono text-xs text-muted-foreground">#{item.id}</span>
                  </span>
                  <span className="max-w-full whitespace-normal break-words font-mono text-xs text-muted-foreground [overflow-wrap:anywhere]">
                    {item.request_method} {item.request_url}
                  </span>
                  <span className="text-xs text-muted-foreground">{format(item.created_at)}</span>
                </span>
              </Link>
            </Button>
          );
        })}
      </CardContent>
    </Card>
  );
}
function getRequestBodyEmptyState(
  detail: AuditLogDetail,
  captureMode: RequestAuditCaptureMode | null,
  messages: ReturnType<typeof useLocale>["messages"],
): string {
  if (detail.request_body_stored) {
    return messages.requestLogs.noCaptured(messages.requestLogs.requestBody.toLowerCase());
  }

  if (captureMode === "metadata_only") {
    return messages.requestLogs.auditRequestBodyNotStoredMetadataOnly;
  }

  return messages.requestLogs.auditRequestBodyNotStored;
}

function getResponseBodyEmptyState(
  detail: AuditLogDetail,
  captureMode: RequestAuditCaptureMode | null,
  messages: ReturnType<typeof useLocale>["messages"],
): string {
  if (detail.response_body_stored) {
    return messages.requestLogs.noCaptured(messages.requestLogs.response(detail.response_status).toLowerCase());
  }

  if (captureMode === "metadata_only") {
    return messages.requestLogs.auditResponseBodyNotStoredMetadataOnly;
  }

  if (detail.is_stream) {
    return messages.requestLogs.auditStreamingResponseBodyNotStored;
  }

  return messages.requestLogs.auditResponseBodyNotStored;
}

function AuditDetailCard({
  apiFamily,
  captureMode,
  detail,
  formatTimestamp,
}: {
  apiFamily: ApiFamily;
  captureMode: RequestAuditCaptureMode | null;
  detail: AuditLogDetail;
  formatTimestamp: (iso: string) => string;
}) {
  const { formatNumber, messages } = useLocale();

  return (
    <Card className="overflow-hidden border-border/70" data-testid="dedicated-audit-detail">
      <div className="flex flex-col gap-3 border-b border-border/70 bg-muted/20 px-4 py-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <ValueBadge label={String(detail.response_status)} intent={getStatusIntent(detail.response_status)} className="px-1.5 py-0" />
            <Badge variant="outline" className={captureBadgeClassName(captureMode)}>
              {getCaptureLabel(captureMode, messages)}
            </Badge>
            <Badge variant="outline" className="border-border/70 bg-background/80 font-mono text-[11px]">
              #{detail.id}
            </Badge>
          </div>
          <p className="whitespace-pre-wrap break-words rounded-lg border border-border/60 bg-background/70 p-3 font-mono text-xs leading-5 text-foreground shadow-inner [overflow-wrap:anywhere]">
            {`${detail.request_method} ${detail.request_url}`}
          </p>
          <p className="text-xs text-muted-foreground">{formatTimestamp(detail.created_at)}</p>
        </div>
        <Badge variant="outline" className="gap-1 border-border/70 bg-background/80 px-2.5 py-1 text-[11px] font-medium">
          {formatNumber(detail.duration_ms)}ms
        </Badge>
      </div>
      <CardContent className="flex flex-col gap-4 p-4">
        <RequestLogPayloadBlock title={messages.requestLogs.requestHeaders} content={detail.request_headers || ""} contentKind="headers" />
        <Separator />
        <RequestLogPayloadBlock
          title={messages.requestLogs.requestBody}
          content={detail.request_body ?? ""}
          emptyState={getRequestBodyEmptyState(detail, captureMode, messages)}
          apiFamily={apiFamily}
          bodyKind="request"
        />
        <Separator />
        <RequestLogPayloadBlock
          title={messages.requestLogs.responseHeaders}
          content={detail.response_headers ?? ""}
          contentKind="headers"
        />
        <Separator />
        <RequestLogPayloadBlock
          title={messages.requestLogs.response(detail.response_status)}
          content={detail.response_body ?? ""}
          emptyState={getResponseBodyEmptyState(detail, captureMode, messages)}
          apiFamily={apiFamily}
          bodyKind="response"
        />
      </CardContent>
    </Card>
  );
}

function getStatusContent(
  status: DedicatedRequestLogAuditStatus,
  requestIdLabel: string,
  error: string | null,
  messages: ReturnType<typeof useLocale>["messages"],
) {
  switch (status) {
    case "invalid_request_id":
      return {
        description: messages.requestLogs.invalidRequestAuditRouteDescription(requestIdLabel),
        title: messages.requestLogs.invalidRequestAuditRouteTitle,
      };
    case "request_missing":
      return {
        description: messages.requestLogs.requestNotFoundDescription(requestIdLabel),
        title: messages.requestLogs.requestNotFound,
      };
    case "request_error":
      return {
        description: error ?? messages.requestLogs.loadFailed,
        title: messages.requestLogs.requestLoadFailedTitle,
      };
    case "invalid_timestamp":
      return {
        description: messages.requestLogs.invalidAuditTimestampDescription,
        title: messages.requestLogs.invalidAuditTimestampTitle,
      };
    case "audit_list_error":
      return {
        description: error ?? messages.requestLogs.auditListLoadFailed,
        title: messages.requestLogs.auditListLoadFailedTitle,
      };
    case "audit_detail_error":
      return {
        description: error ?? messages.requestLogs.auditDetailLoadFailed,
        title: messages.requestLogs.auditDetailLoadFailedTitle,
      };
    default:
      return {
        description: "",
        title: "",
      };
  }
}

export function RequestLogAuditPage() {
  const { requestId: requestIdParam } = useParams();
  const [searchParams] = useSearchParams();
  const requestId = parsePositiveInteger(requestIdParam);
  const auditIdParam = searchParams.get("audit_id");
  const selectedAuditId = parsePositiveInteger(auditIdParam);
  const { format } = useTimezone();
  const { messages } = useLocale();
  const requestIdLabel = requestIdParam?.trim() || "";
  const defaultAuditPath = requestId === null ? "/request-logs" : `/request-logs/${requestId}/audit`;
  const state = useDedicatedRequestLogAudit({
    requestId,
    selectedAuditId,
    selectedAuditParamLabel: auditIdParam,
    selectedAuditParamPresent: auditIdParam !== null,
  });
  const statusContent = getStatusContent(state.status, requestIdLabel, state.error, messages);
  const auditRequestApiFamily = state.request?.summary.api_family ?? null;

  return (
    <div className="flex flex-col gap-6 pb-8" data-clipboard-fallback-root="" data-testid="dedicated-request-log-audit-page">
      <PageHeader
        title={messages.requestLogs.auditPageTitle(requestIdLabel || "-")}
        description={messages.requestLogs.auditPageDescription}
      >
        <Button variant="outline" asChild>
          <Link to={requestId === null ? "/request-logs" : `/request-logs?request_id=${requestId}`}>
            <ArrowLeft data-icon="inline-start" />
            {messages.requestLogs.viewRequestInLogs}
          </Link>
        </Button>
      </PageHeader>

      {state.request ? (
        <Card className="border-border/70">
          <CardContent className="flex flex-wrap items-center gap-2 pt-0 text-sm text-muted-foreground">
            <Terminal className="size-4" />
            <span>{messages.requestLogs.requestTitle(state.request.summary.id)}</span>
            <Separator orientation="vertical" className="h-4" />
            <span>{format(state.request.summary.created_at)}</span>
            <Badge variant="outline" className={captureBadgeClassName(state.captureMode)}>
              {getCaptureLabel(state.captureMode, messages)}
            </Badge>
          </CardContent>
        </Card>
      ) : null}

      {state.status === "request_loading" ? <LoadingCard /> : null}

      {state.status === "invalid_request_id" || state.status === "request_missing" || state.status === "request_error" ? (
        <StatusPanel
          action={(
            <Button variant="outline" asChild>
              <Link to="/request-logs">{messages.requestLogs.returnToRequestList}</Link>
            </Button>
          )}
          description={statusContent.description}
          status={state.status === "request_error" ? "error" : "neutral"}
          title={statusContent.title}
        />
      ) : null}

      {state.status === "disabled" ? (
        <StatusPanel
          description={messages.requestLogs.auditDisabledDescription}
          status="neutral"
          title={messages.requestLogs.auditDisabledAtRequest}
        />
      ) : null}

      {state.status === "invalid_timestamp" || state.status === "audit_list_error" ? (
        <StatusPanel
          description={statusContent.description}
          status={state.status === "audit_list_error" ? "error" : "warning"}
          title={statusContent.title}
        />
      ) : null}

      {state.status === "audit_list_loading" ? <LoadingCard /> : null}

      {state.status === "no_audit_records" ? (
        <StatusPanel
          description={messages.requestLogs.noAuditRecordsDescription}
          status="neutral"
          title={messages.requestLogs.noAuditRecords}
        />
      ) : null}

      {state.auditItems.length > 0 && requestId !== null ? (
        <div className="grid gap-4 xl:grid-cols-[22rem_minmax(0,1fr)]">
          <AuditList
            auditItems={state.auditItems}
            requestId={requestId}
            selectedAuditId={state.selectedAuditId}
          />
          <div className="flex min-w-0 flex-col gap-4">
            {state.status === "missing_audit" ? (
              <StatusPanel
                action={(
                  <Button variant="outline" asChild>
                    <Link to={defaultAuditPath}>{messages.requestLogs.showDefaultAuditRecord}</Link>
                  </Button>
                )}
                description={messages.requestLogs.missingAuditRecordDescription(state.missingAuditLabel ?? "")}
                status="warning"
                title={messages.requestLogs.missingAuditRecordTitle}
              />
            ) : null}
            {state.status === "audit_detail_loading" ? <LoadingCard /> : null}
            {state.status === "audit_detail_error" ? (
              <StatusPanel
                action={(
                  <Button variant="outline" asChild>
                    <Link to={defaultAuditPath}>{messages.requestLogs.showDefaultAuditRecord}</Link>
                  </Button>
                )}
                description={statusContent.description}
                status="error"
                title={statusContent.title}
              />
            ) : null}
            {state.detail && auditRequestApiFamily ? (
              <AuditDetailCard
                apiFamily={auditRequestApiFamily}
                captureMode={state.captureMode}
                detail={state.detail}
                formatTimestamp={format}
              />
            ) : null}
          </div>
        </div>
      ) : null}

      {state.status === "ready" && state.auditItems.length === 0 ? (
        <Card className="border-border/70">
          <CardContent className="pt-0">
            <FileSearch className="mb-3 size-5 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">{messages.requestLogs.noAuditRecords}</p>
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}
