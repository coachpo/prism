import { AlertTriangle, Clock3, Info, ShieldCheck, ShieldOff } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import type { AuditLogDetail } from "@/lib/types";
import { ValueBadge } from "@/components/StatusBadge";
import { RequestLogPayloadBlock } from "./RequestLogPayloadBlock";
import { getStatusIntent } from "./requestLogDetailUtils";
import type { AuditDetailState } from "../requestLogAuditState";

interface RequestLogAuditTabProps {
  audits: AuditLogDetail[];
  loading: boolean;
  state: AuditDetailState | null;
  formatTimestamp: (iso: string) => string;
}

function getRequestBodyEmptyState(
  audit: AuditLogDetail,
  state: AuditDetailState | null,
  messages: ReturnType<typeof useLocale>["messages"],
) {
  if (audit.request_body_stored) {
    return messages.requestLogs.noCaptured(messages.requestLogs.requestBody.toLowerCase());
  }

  if (state === "metadata_only") {
    return messages.requestLogs.auditRequestBodyNotStoredMetadataOnly;
  }

  return messages.requestLogs.auditRequestBodyNotStored;
}

function getResponseBodyEmptyState(
  audit: AuditLogDetail,
  state: AuditDetailState | null,
  messages: ReturnType<typeof useLocale>["messages"],
) {
  if (audit.response_body_stored) {
    return messages.requestLogs.noCaptured(messages.requestLogs.response(audit.response_status).toLowerCase());
  }

  if (state === "metadata_only") {
    return messages.requestLogs.auditResponseBodyNotStoredMetadataOnly;
  }

  if (audit.is_stream) {
    return messages.requestLogs.auditStreamingResponseBodyNotStored;
  }

  return messages.requestLogs.auditResponseBodyNotStored;
}

function AuditStateBanner({ state }: { state: AuditDetailState }) {
  const { messages } = useLocale();

  if (state === "load_failed") {
    return (
      <div className="flex items-start gap-3 rounded-xl border border-warning/35 bg-warning/10 px-4 py-4">
        <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-warning-foreground dark:text-warning" />
        <div className="space-y-1">
          <p className="text-sm font-medium">{messages.requestLogs.auditLoadFailedTitle}</p>
          <p className="text-sm text-muted-foreground">{messages.requestLogs.auditLoadFailed}</p>
        </div>
      </div>
    );
  }

  if (state === "disabled") {
    return (
      <div className="flex items-start gap-3 rounded-xl border border-border/70 bg-muted/20 px-4 py-4">
        <ShieldOff className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
        <div className="space-y-1">
          <p className="text-sm font-medium">{messages.requestLogs.auditDisabledAtRequest}</p>
          <p className="text-sm text-muted-foreground">{messages.requestLogs.auditDisabledDescription}</p>
        </div>
      </div>
    );
  }

  const bannerTone = state === "metadata_only"
    ? {
        badgeClassName: "border-info/25 bg-info/10 text-info",
        description: messages.requestLogs.auditMetadataOnlyDescription,
        icon: <Info className="mt-0.5 h-5 w-5 shrink-0 text-info" />,
        title: messages.requestLogs.auditMetadataOnly,
      }
    : {
        badgeClassName: "border-success/25 bg-success/10 text-success",
        description: messages.requestLogs.auditFullCaptureDescription,
        icon: <ShieldCheck className="mt-0.5 h-5 w-5 shrink-0 text-success" />,
        title: messages.requestLogs.auditFullCapture,
      };

  return (
    <div className="flex items-start gap-3 rounded-xl border border-border/70 bg-background/80 px-4 py-4">
      {bannerTone.icon}
      <div className="space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-sm font-medium">{bannerTone.title}</p>
          <Badge variant="outline" className={bannerTone.badgeClassName}>
            {bannerTone.title}
          </Badge>
        </div>
        <p className="text-sm text-muted-foreground">{bannerTone.description}</p>
      </div>
    </div>
  );
}

export function RequestLogAuditTab({ audits, loading, state, formatTimestamp }: RequestLogAuditTabProps) {
  const { formatNumber, messages } = useLocale();

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-10 w-full rounded-xl" />
        <Skeleton className="h-64 w-full rounded-xl" />
        <Skeleton className="h-64 w-full rounded-xl" />
      </div>
    );
  }

  if (state === "load_failed" || state === "disabled") {
    return <AuditStateBanner state={state} />;
  }

  if (audits.length === 0) {
    return (
      <div className="space-y-4">
        {state ? <AuditStateBanner state={state} /> : null}
        <div className="rounded-xl border border-border/70 bg-muted/20 px-4 py-10 text-center text-sm text-muted-foreground">
          {messages.requestLogs.noAuditRecords}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {state ? <AuditStateBanner state={state} /> : null}
      {audits.map((audit) => (
        <Card key={audit.id} className="overflow-hidden border-border/70 shadow-sm">
          <div className="flex flex-col gap-3 border-b border-border/70 bg-muted/20 px-4 py-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="space-y-1">
              <div className="flex flex-wrap items-center gap-2">
                <ValueBadge label={String(audit.response_status)} intent={getStatusIntent(audit.response_status)} className="px-1.5 py-0 font-mono" />
                <Badge
                  variant="outline"
                  className={state === "metadata_only"
                    ? "border-info/25 bg-info/10 text-info"
                    : "border-success/25 bg-success/10 text-success"
                  }
                >
                  {state === "metadata_only"
                    ? messages.requestLogs.auditMetadataOnly
                    : messages.requestLogs.auditFullCapture}
                </Badge>
              </div>
              <ScrollArea className="max-h-24 rounded-lg border border-border/60 bg-background/70 shadow-inner">
                <pre className="whitespace-pre-wrap break-words p-3 font-mono text-[12px] font-medium leading-5 tracking-tight text-foreground [overflow-wrap:anywhere]">
                  {`${audit.request_method} ${audit.request_url}`}
                </pre>
              </ScrollArea>
              <p className="text-xs text-muted-foreground">{formatTimestamp(audit.created_at)}</p>
            </div>

            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <Badge variant="outline" className="gap-1 border-border/70 bg-background/80 px-2.5 py-1 text-[11px] font-medium">
                <Clock3 className="h-3 w-3" />
                {formatNumber(audit.duration_ms)}ms
              </Badge>
              <Badge variant="outline" className="border-border/70 bg-background/80 font-mono text-[11px]">
                #{audit.id}
              </Badge>
            </div>
          </div>

          <CardContent className="space-y-4 p-4">
            <RequestLogPayloadBlock title={messages.requestLogs.requestHeaders} content={audit.request_headers || ""} />
            <Separator />
            <RequestLogPayloadBlock
              title={messages.requestLogs.requestBody}
              content={audit.request_body ?? ""}
              emptyState={getRequestBodyEmptyState(audit, state, messages)}
            />
            <Separator />
            <RequestLogPayloadBlock
              title={messages.requestLogs.response(audit.response_status)}
              content={audit.response_body ?? ""}
              emptyState={getResponseBodyEmptyState(audit, state, messages)}
            />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
