import { AlertTriangle, Clock3 } from "lucide-react";
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
import type { AuditDetailErrorKind } from "../useAuditDetail";

interface RequestLogAuditTabProps {
  audits: AuditLogDetail[];
  loading: boolean;
  error: AuditDetailErrorKind | null;
  formatTimestamp: (iso: string) => string;
}

export function RequestLogAuditTab({ audits, loading, error, formatTimestamp }: RequestLogAuditTabProps) {
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

  if (error) {
    const errorTitle =
      error === "capture_unavailable"
        ? messages.requestLogs.auditCaptureUnavailable
        : messages.requestLogs.auditLoadFailedTitle;
    const errorMessage =
      error === "capture_unavailable"
        ? messages.requestLogs.auditCaptureDisabledForVendor
        : messages.requestLogs.auditLoadFailed;

    return (
      <div className="flex flex-col items-center gap-3 rounded-xl border border-amber-500/20 bg-amber-500/10 px-4 py-10 text-center">
        <AlertTriangle className="h-5 w-5 text-amber-600 dark:text-amber-400" />
        <div className="max-w-full space-y-2">
          <p className="text-sm font-medium">{errorTitle}</p>
          <p className="text-sm text-muted-foreground">{errorMessage}</p>
        </div>
      </div>
    );
  }

  if (audits.length === 0) {
    return (
      <div className="rounded-xl border border-border/70 bg-muted/20 px-4 py-10 text-center text-sm text-muted-foreground">
        {messages.requestLogs.noAuditRecords}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {audits.map((audit) => (
        <Card key={audit.id} className="overflow-hidden border-border/70 shadow-sm">
          <div className="flex flex-col gap-3 border-b border-border/70 bg-muted/20 px-4 py-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="space-y-1">
              <div className="flex flex-wrap items-center gap-2">
                <ValueBadge label={String(audit.response_status)} intent={getStatusIntent(audit.response_status)} className="px-1.5 py-0 font-mono" />
                <Badge variant="outline" className="border-border/70 bg-background/80 text-[10px] font-medium">
                  {messages.requestLogs.auditCapture}
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
            <RequestLogPayloadBlock title={messages.requestLogs.requestBody} content={audit.request_body ?? ""} />
            <Separator />
            <RequestLogPayloadBlock title={messages.requestLogs.response(audit.response_status)} content={audit.response_body ?? ""} />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
