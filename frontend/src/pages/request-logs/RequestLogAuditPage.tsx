import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { AlertTriangle, ArrowLeft, FileSearch, ShieldOff, Terminal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import type { ApiFamily, AuditLogDetail, AuditLogListItem } from "@/lib/types";
import {
  OperatorErrorState,
  OperatorMissingValue,
  OperatorPageHeader,
  OperatorTypeBadge,
  OperatorValueBadge,
  type OperatorStatusTier,
} from "@/shared/design-system";
import { operationalRowStripe } from "@/shared/table/operationalTable";
import { cn } from "@/lib/utils";
import { AuditCaptureLedger } from "./AuditCaptureLedger";
import {
  auditScopedDurationMs,
  auditScopedStatusCode,
  decodeAuditBodyBase64,
  type AuditBodyText,
} from "./auditLogView";
import { RequestLogAuditWindowBar } from "./RequestLogAuditWindowBar";
import {
  getAuditPagePath,
  getSelectedAuditPath,
  parseRequestLogIdParam,
} from "./requestLogAuditRoute";
import { resolveRequestAuditCaptureMode, type RequestAuditCaptureMode } from "./requestLogAuditState";
import { RequestLogPayloadBlock } from "./detail/RequestLogPayloadBlock";
import { getStatusIntent } from "./detail/requestLogDetailUtils";
import {
  useDedicatedRequestLogAudit,
  type DedicatedRequestLogAuditStatus,
} from "./useDedicatedRequestLogAudit";

function parsePositiveAuditId(value: string | null | undefined): number | null {
  if (!value) return null;
  const trimmed = value.trim().replace(/^#/, "");
  if (!/^\d+$/.test(trimmed)) return null;
  const parsed = Number(trimmed);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
}
function captureBadgeIntent(mode: RequestAuditCaptureMode | null) {
  // Capture completeness is an honesty signal: metadata_only means the payload
  // was deliberately not retained, which must not read the same as a full capture.
  if (mode === "metadata_only") return "degraded" as const;
  if (mode === "full") return "healthy" as const;
  return "muted" as const;
}

function getCaptureLabel(
  mode: RequestAuditCaptureMode | null,
  messages: ReturnType<typeof useLocale>["messages"],
): string {
  if (mode === "metadata_only") return messages.requestLogs.auditMetadataOnly;
  if (mode === "full") return messages.requestLogs.auditFullCapture;
  return messages.requestLogs.auditDisabledAtRequest;
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
    : <AlertTriangle className="mt-0.5 size-5 shrink-0 text-degraded" />;

  return (
    <Card className={status === "error" ? "border-destructive/35" : "border-border"}>
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
    <Card className="border-border">
      <CardHeader>
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-4 w-72" />
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Skeleton className="h-24 w-full rounded-lg" />
        <Skeleton className="h-24 w-full rounded-lg" />
      </CardContent>
    </Card>
  );
}

/**
 * The audit records table.
 *
 * Nine columns of routing context do not fit a 320px sidebar, and below
 * 1280px the old left column stacked on top of the payload anyway. Full width
 * solves both. Selecting a row swaps only the detail below; this table does
 * not remount, so switching records no longer flashes the whole page.
 */
function AuditRecordsTable({
  auditItems,
  cursor,
  hasMore,
  nextCursor,
  requestId,
  selectedAuditId,
}: {
  auditItems: AuditLogListItem[];
  cursor: string | null;
  hasMore: boolean;
  nextCursor: string | null;
  requestId: string;
  selectedAuditId: number | null;
}) {
  const { formatNumber, messages } = useLocale();
  const { format } = useTimezone();
  const copy = messages.requestLogs;

  return (
    <Card className="gap-0 overflow-hidden border-border" data-testid="dedicated-audit-list">
      <CardHeader className="border-b py-2">
        <p className="text-xs text-muted-foreground">
          {copy.auditRecordListDescription(formatNumber(auditItems.length))}
        </p>
      </CardHeader>
      <CardContent className="p-0">
        <div className="overflow-x-auto">
          <Table aria-label={copy.auditRecordList}>
            <TableHeader>
              <TableRow>
                <TableHead>{copy.auditTableColumnAudit}</TableHead>
                <TableHead>{copy.auditTableColumnMethod}</TableHead>
                <TableHead>{copy.auditTableColumnUrl}</TableHead>
                <TableHead>{copy.model}</TableHead>
                <TableHead>{copy.endpoint}</TableHead>
                <TableHead>{copy.auditTableColumnStatus}</TableHead>
                <TableHead className="text-right">{copy.auditTableColumnDuration}</TableHead>
                <TableHead>{copy.auditTableColumnCapture}</TableHead>
                <TableHead>{copy.auditTableColumnTime}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {auditItems.map((item) => {
                const captureMode = resolveRequestAuditCaptureMode(item);
                const isSelected = item.id === selectedAuditId;
                const statusCode = auditScopedStatusCode(item);
                const durationMs = auditScopedDurationMs(item);
                const tier = statusTier(statusCode);
                return (
                  <TableRow
                    key={item.id}
                    data-testid={`audit-record-${item.id}`}
                    data-state={isSelected ? "selected" : undefined}
                    className={cn("group/row cursor-pointer", operationalRowStripe(tier))}
                  >
                    <TableCell className="font-mono tabular-nums">
                      <Link to={getSelectedAuditPath(requestId, item.id, cursor)} className="hover:underline">
                        #{item.id}
                      </Link>
                    </TableCell>
                    <TableCell className="font-mono">{item.request_method}</TableCell>
                    <TableCell className="max-w-80 truncate font-mono text-xs" title={item.request_url}>
                      {item.request_url}
                    </TableCell>
                    <TableCell className="max-w-40 truncate font-mono text-xs">{item.model_id}</TableCell>
                    <TableCell className="max-w-40 truncate text-xs">
                      {item.endpoint_description ?? item.endpoint_base_url ?? (
                        <OperatorMissingValue reason={messages.honesty.noValue} />
                      )}
                    </TableCell>
                    <TableCell>
                      {statusCode !== null && statusCode > 0 ? (
                        <OperatorValueBadge
                          label={String(statusCode)}
                          intent={getStatusIntent(statusCode)}
                        />
                      ) : (
                        <OperatorMissingValue reason={messages.honesty.noValue} />
                      )}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums">
                      {durationMs === null ? (
                        <OperatorMissingValue reason={messages.honesty.noValue} />
                      ) : (
                        `${formatNumber(durationMs)} ms`
                      )}
                    </TableCell>
                    <TableCell>
                      <OperatorTypeBadge
                        label={getCaptureLabel(captureMode, messages)}
                        intent={captureBadgeIntent(captureMode)}
                        preserveLabel
                      />
                    </TableCell>
                    <TableCell className="whitespace-nowrap font-mono tabular-nums text-xs">
                      {format(item.created_at)}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
        <div className="flex items-center justify-end gap-1 border-t border-border bg-inset px-[var(--density-card-pad-x)] py-2">
          {cursor ? (
            <Button variant="outline" size="sm" asChild>
              <Link to={getAuditPagePath(requestId, null)}>{copy.previousPage}</Link>
            </Button>
          ) : (
            <Button variant="outline" size="sm" disabled>{copy.previousPage}</Button>
          )}
          {hasMore && nextCursor ? (
            <Button variant="outline" size="sm" asChild>
              <Link to={getAuditPagePath(requestId, nextCursor)}>{copy.nextPage}</Link>
            </Button>
          ) : (
            <Button variant="outline" size="sm" disabled>{copy.nextPage}</Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function statusTier(status: number | null): OperatorStatusTier {
  if (status === null || status <= 0) return "idle";
  if (status >= 500) return "failing";
  if (status >= 400) return "degraded";
  return "healthy";
}

function getRequestBodyEmptyState(
  body: AuditBodyText,
  captureMode: RequestAuditCaptureMode | null,
  messages: ReturnType<typeof useLocale>["messages"],
): string {
  if (body.binary) {
    return messages.requestLogs.auditBinaryBodyNotShown;
  }

  if (body.text !== null) {
    return messages.requestLogs.noCaptured(messages.requestLogs.requestBody.toLowerCase());
  }

  if (captureMode === "metadata_only") {
    return messages.requestLogs.auditBodyNotStoredMetadataOnly;
  }

  return messages.requestLogs.auditRequestBodyNotStored;
}

function getResponseBodyEmptyState(
  body: AuditBodyText,
  detail: AuditLogDetail,
  captureMode: RequestAuditCaptureMode | null,
  statusCode: number | null,
  messages: ReturnType<typeof useLocale>["messages"],
): string {
  if (body.binary) {
    return messages.requestLogs.auditBinaryBodyNotShown;
  }

  if (body.text !== null) {
    return messages.requestLogs.noCaptured(messages.requestLogs.response(statusCode ?? "—").toLowerCase());
  }

  if (captureMode === "metadata_only") {
    return messages.requestLogs.auditBodyNotStoredMetadataOnly;
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
  operationName,
  formatTimestamp,
}: {
  apiFamily: ApiFamily;
  captureMode: RequestAuditCaptureMode | null;
  detail: AuditLogDetail;
  operationName: string | null;
  formatTimestamp: (iso: string) => string;
}) {
  const { formatNumber, messages } = useLocale();
  const statusCode = auditScopedStatusCode(detail);
  const durationMs = auditScopedDurationMs(detail);
  const requestBody = decodeAuditBodyBase64(detail.request_body_base64);
  const responseBody = decodeAuditBodyBase64(detail.response_body_base64);

  return (
    <Card className="overflow-hidden border-border" data-testid="dedicated-audit-detail">
      <div className="flex flex-col gap-3 border-b border-border bg-inset px-4 py-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            {statusCode !== null ? (
              <OperatorValueBadge label={String(statusCode)} intent={getStatusIntent(statusCode)} className="px-1.5 py-0" />
            ) : (
              <OperatorMissingValue reason={messages.honesty.noValue} />
            )}
            <OperatorTypeBadge label={getCaptureLabel(captureMode, messages)} intent={captureBadgeIntent(captureMode)} />
            <OperatorValueBadge label={`#${detail.id}`} className="text-[11px]" />
          </div>
          <p className="whitespace-pre-wrap break-words rounded-lg border border-border bg-panel p-3 font-mono text-xs leading-5 text-foreground shadow-inner [overflow-wrap:anywhere]">
            {`${detail.request_method} ${detail.request_url}`}
          </p>
          <p className="text-xs text-muted-foreground">{formatTimestamp(detail.created_at)}</p>
        </div>
        <OperatorValueBadge
          label={durationMs === null ? "—" : `${formatNumber(durationMs)}ms`}
          className="gap-1 px-2.5 py-1 text-[11px] font-medium"
        />
      </div>
      <CardContent className="flex flex-col gap-4 p-4">
        {/* Every payload states what was observed, kept and dropped, so a
            truncated body is never mistaken for the whole body. */}
        <div className="flex flex-col gap-2">
          <RequestLogPayloadBlock title={messages.requestLogs.requestHeaders} content={detail.request_headers || ""} contentKind="headers" />
        </div>
        <Separator />
        <div className="flex flex-col gap-2">
          <RequestLogPayloadBlock
            title={messages.requestLogs.requestBody}
            content={requestBody.text ?? ""}
            emptyState={getRequestBodyEmptyState(requestBody, captureMode, messages)}
            apiFamily={apiFamily}
            bodyKind="request"
            operationName={operationName}
          />
          <AuditCaptureLedger
            bytesObserved={detail.request_body_bytes_observed}
            bytesStored={detail.request_body_bytes_stored}
            captureStatus={detail.request_body_capture_status}
            truncated={detail.request_body_truncated}
          />
        </div>
        <Separator />
        <div className="flex flex-col gap-2">
          <RequestLogPayloadBlock
            title={messages.requestLogs.responseHeaders}
            content={detail.response_headers ?? ""}
            contentKind="headers"
          />
        </div>
        <Separator />
        <div className="flex flex-col gap-2">
          <RequestLogPayloadBlock
            title={messages.requestLogs.response(statusCode ?? "—")}
            content={responseBody.text ?? ""}
            emptyState={getResponseBodyEmptyState(responseBody, detail, captureMode, statusCode, messages)}
            apiFamily={apiFamily}
            bodyKind="response"
            operationName={operationName}
          />
          <AuditCaptureLedger
            bytesObserved={detail.response_body_bytes_observed}
            bytesStored={detail.response_body_bytes_stored}
            captureStatus={detail.response_body_capture_status}
            truncated={detail.response_body_truncated}
          />
        </div>
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

interface RequestLogAuditPageProps {
  requestIdParam?: string;
  searchParams?: URLSearchParams;
}

export function RequestLogAuditPage({
  requestIdParam,
  searchParams = new URLSearchParams(window.location.search),
}: RequestLogAuditPageProps = {}) {
  const requestId = parseRequestLogIdParam(requestIdParam);
  const auditIdParam = searchParams.get("audit_id");
  const auditCursor = searchParams.get("cursor")?.trim() || null;
  const selectedAuditId = parsePositiveAuditId(auditIdParam);
  const { format } = useTimezone();
  const { messages } = useLocale();
  const requestIdLabel = requestIdParam?.trim() || "";
  const defaultAuditPath = requestId === null ? "/observe/requests" : `/observe/requests/${requestId}/audit`;
  const state = useDedicatedRequestLogAudit({
    cursor: auditCursor ?? undefined,
    requestId,
    selectedAuditId,
    selectedAuditParamLabel: auditIdParam,
    selectedAuditParamPresent: auditIdParam !== null,
  });
  const statusContent = getStatusContent(state.status, requestIdLabel, state.error, messages);
  const auditRequestApiFamily = state.request?.summary.api_family as ApiFamily | null ?? null;

  return (
    <div className="flex flex-col gap-6 pb-8" data-clipboard-fallback-root="" data-testid="dedicated-request-log-audit-page">
      <OperatorPageHeader
        title={messages.requestLogs.auditPageTitle(requestIdLabel || "-")}
        description={messages.requestLogs.auditPageDescription}
      >
        <Button variant="outline" asChild>
          <Link to={requestId === null ? "/observe/requests" : `/observe/requests?request_id=${requestId}`}>
            <ArrowLeft data-icon="inline-start" />
            {messages.requestLogs.viewRequestInLogs}
          </Link>
        </Button>
      </OperatorPageHeader>

      {state.request ? (
        <>
          <RequestLogAuditWindowBar requestCreatedAt={state.request.summary.created_at} />
          {/* Sticky so scrolling a long SSE body never loses which request it belongs to. */}
          <Card className="sticky top-14 z-10 border-border" data-testid="audit-context-panel">
            <CardContent className="flex flex-wrap items-center gap-x-3 gap-y-1 pt-0 text-xs text-muted-foreground">
              <Terminal className="size-3.5" />
              <span className="font-medium text-foreground">
                {messages.requestLogs.requestTitle(state.request.summary.request_log_id)}
              </span>
              <Separator orientation="vertical" className="h-3" />
              <span className="font-mono">{state.request.summary.model_label}</span>
              <Separator orientation="vertical" className="h-3" />
              <span>{state.request.summary.api_family}</span>
              <Separator orientation="vertical" className="h-3" />
              <span className="font-mono tabular-nums">{format(state.request.summary.created_at)}</span>
              <OperatorTypeBadge
                label={getCaptureLabel(state.captureMode, messages)}
                intent={captureBadgeIntent(state.captureMode)}
                preserveLabel
              />
            </CardContent>
          </Card>
        </>
      ) : null}

      {state.status === "request_loading" ? <LoadingCard /> : null}

      {state.status === "invalid_request_id" || state.status === "request_missing" || state.status === "request_error" ? (
        <StatusPanel
          action={(
            <Button variant="outline" asChild>
              <Link to="/observe/requests">{messages.requestLogs.returnToRequestList}</Link>
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

      {state.status === "audit_list_error" ? (
        <OperatorErrorState
          testId="audit-list-error"
          title={messages.requestLogs.auditListLoadFailedTitle}
          description={messages.honesty.readFailedDescription}
          details={state.error}
          detailsLabel={messages.honesty.viewDetails}
          action={
            <>
              <Button variant="outline" size="sm" onClick={() => window.location.reload()}>
                {messages.common.retry}
              </Button>
              <Button variant="outline" size="sm" asChild>
                <Link to="/observe/requests">{messages.requestLogs.returnToRequestList}</Link>
              </Button>
            </>
          }
        />
      ) : null}

      {state.status === "invalid_timestamp" ? (
        <StatusPanel
          description={statusContent.description}
          status="warning"
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
        <div className="flex min-w-0 flex-col gap-4">
          <AuditRecordsTable
            auditItems={state.auditItems}
            cursor={auditCursor}
            hasMore={state.hasMore}
            nextCursor={state.nextCursor}
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
                operationName={state.request?.request.operation_name ?? null}
                formatTimestamp={format}
              />
            ) : null}
          </div>
        </div>
      ) : null}

      {state.status === "ready" && state.auditItems.length === 0 ? (
        <Card className="border-border">
          <CardContent className="pt-0">
            <FileSearch className="mb-3 size-5 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">{messages.requestLogs.noAuditRecords}</p>
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}
