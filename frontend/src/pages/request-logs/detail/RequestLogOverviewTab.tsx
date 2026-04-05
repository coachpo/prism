import { AlertTriangle, Coins, Copy, FileText } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { TypeBadge, ValueBadge } from "@/components/StatusBadge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn, formatApiFamily } from "@/lib/utils";
import type { RequestLogDetail } from "@/lib/types";
import {
  formatCost,
  formatTokens,
  isProxyOriginRequest,
  type RequestLogModelResolver,
} from "../columns";
import {
  ApiFamilyPill,
  DetailRow,
  SectionCard,
  SummaryStat,
} from "./requestLogDetailShared";
import { copyRequestLogText, getStatusIntent, getStatusTone } from "./requestLogDetailUtils";

interface RequestLogOverviewTabProps {
  request: RequestLogDetail;
  formatTimestamp: (iso: string) => string;
  resolveModelLabel: RequestLogModelResolver;
}

function formatErrorDetail(errorDetail: string) {
  try {
    const parsed = JSON.parse(errorDetail) as unknown;
    if (typeof parsed === "object" && parsed !== null) {
      return JSON.stringify(parsed, null, 2);
    }
  } catch {
    return errorDetail;
  }

  return errorDetail;
}

function SectionSubheading({ children }: { children: React.ReactNode }) {
  return (
    <p className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground">
      {children}
    </p>
  );
}

export function RequestLogOverviewTab({
  request,
  formatTimestamp,
  resolveModelLabel,
}: RequestLogOverviewTabProps) {
  const { formatNumber, messages } = useLocale();
  const summary = request.summary;
  const requestInfo = request.request;
  const routing = request.routing;
  const usage = request.usage;
  const costing = request.costing;
  const tone = getStatusTone(summary.status_code);
  const requestedModelLabel = resolveModelLabel(summary.model_id);
  const resolvedTargetLabel = summary.resolved_target_model_id
    ? resolveModelLabel(summary.resolved_target_model_id)
    : null;
  const formattedErrorDetail = requestInfo.error_detail ? formatErrorDetail(requestInfo.error_detail) : null;
  const hasFormattedErrorDetail = formattedErrorDetail !== null && formattedErrorDetail !== requestInfo.error_detail;
  const apiFamily = summary.api_family;
  const isProxyOrigin = isProxyOriginRequest(summary, resolveModelLabel);

  return (
    <div className="space-y-3">
      <Card className={cn("overflow-hidden border shadow-sm", tone.card)}>
        <CardContent className="space-y-4 p-4">
          <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
            <div className="min-w-0 space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                <ValueBadge label={String(summary.status_code)} intent={getStatusIntent(summary.status_code)} className="px-1.5 py-0 font-mono" />
                {summary.is_stream && <TypeBadge label={messages.requestLogs.streaming} intent="blue" className="px-2 py-0.5" />}
                {isProxyOrigin ? (
                  <TypeBadge label={messages.requestLogs.proxyOrigin} intent="accent" className="px-2 py-0.5" />
                ) : null}
                <ApiFamilyPill apiFamily={apiFamily} />
              </div>

              <div className="min-w-0 space-y-1.5">
                <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                  <h3 className="truncate text-lg font-semibold tracking-tight sm:text-xl">{requestedModelLabel}</h3>
                  {summary.vendor_name ? (
                    <span className="text-xs text-muted-foreground">{summary.vendor_name}</span>
                  ) : null}
                </div>
                {requestedModelLabel !== summary.model_id ? (
                  <p className="font-mono text-[11px] text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                    {summary.model_id}
                  </p>
                ) : null}
                {resolvedTargetLabel && summary.resolved_target_model_id !== summary.model_id ? (
                  <p className="text-xs text-muted-foreground">
                    {messages.requestLogs.resolvedTarget}: {resolvedTargetLabel}
                  </p>
                ) : null}
                <p className="font-mono text-xs text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                  {requestInfo.request_path}
                </p>
              </div>
            </div>

            <div className="grid gap-2 sm:grid-cols-2 xl:w-[360px]" data-testid="request-log-summary-strip">
              <SummaryStat
                label={messages.requestLogs.latency}
                value={`${formatNumber(summary.response_time_ms)}ms`}
                valueClassName="font-mono"
              />
              <SummaryStat
                label={messages.requestLogs.totalTokens}
                value={formatTokens(usage.total_tokens)}
                valueClassName="font-mono"
              />
              <SummaryStat
                label={messages.requestLogs.totalCost}
                value={formatCost(costing.total_cost_user_currency_micros, costing.report_currency_symbol)}
                valueClassName="font-mono"
              />
              <SummaryStat
                label={messages.requestLogs.timestamp}
                value={formatTimestamp(summary.created_at)}
                valueClassName="font-mono text-xs"
              />
            </div>
          </div>
        </CardContent>
      </Card>

      {formattedErrorDetail ? (
        <div className="rounded-xl border border-red-500/25 bg-red-500/10 p-3 shadow-sm sm:p-4">
          <div className="flex items-start gap-3">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-red-600 dark:text-red-400" />
            <div className="min-w-0 flex-1 space-y-3">
              <ValueBadge label={String(summary.status_code)} intent={getStatusIntent(summary.status_code)} className="px-1.5 py-0 font-mono" />
              <div className="flex flex-wrap items-start justify-between gap-2">
                <div className="space-y-1">
                  <p className="text-xs font-medium uppercase tracking-[0.18em] text-red-700 dark:text-red-300">{messages.requestLogs.errorDetail}</p>
                  <p className="text-xs text-red-700/85 dark:text-red-300/85">
                    {hasFormattedErrorDetail
                      ? messages.requestLogs.formattedForReadability
                      : messages.requestLogs.capturedFailureDetail}
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 rounded-full border-red-500/20 px-2.5 text-[11px] text-red-700 hover:border-red-500/40 hover:bg-red-500/10 dark:text-red-200"
                  onClick={() => {
                    void copyRequestLogText(formattedErrorDetail, messages.requestLogs.errorDetail);
                  }}
                >
                  <Copy className="h-3 w-3" />
                  {messages.requestLogs.copy}
                </Button>
              </div>

              <ScrollArea className="max-h-56 rounded-lg border border-red-500/15 bg-background/85 shadow-inner">
                <pre className="max-w-full whitespace-pre-wrap break-words p-3 font-mono text-[11px] leading-5 text-foreground [overflow-wrap:anywhere]">
                  {formattedErrorDetail}
                </pre>
              </ScrollArea>
            </div>
          </div>
        </div>
      ) : null}

      <div className="grid gap-3 xl:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)]" data-testid="request-log-overview-grid">
        <SectionCard icon={FileText} title={messages.requestLogs.requestDetails}>
          <div className="space-y-3">
            <div className="space-y-1">
              <DetailRow label={messages.requestLogs.requestId}><span className="font-mono">#{summary.id}</span></DetailRow>
              <DetailRow label={messages.requestLogs.time}><span className="font-mono text-xs">{formatTimestamp(summary.created_at)}</span></DetailRow>
              {requestInfo.ingress_request_id ? (
                <DetailRow label={messages.requestLogs.ingressRequestId}>
                  <span className="font-mono text-[12px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                    {requestInfo.ingress_request_id}
                  </span>
                </DetailRow>
              ) : null}
              {requestInfo.attempt_number !== null ? (
                <DetailRow label={messages.requestLogs.attemptNumber}>
                  <span className="font-mono">{formatNumber(requestInfo.attempt_number)}</span>
                </DetailRow>
              ) : null}
              {requestInfo.provider_correlation_id ? (
                <DetailRow label={messages.requestLogs.providerCorrelationId}>
                  <span className="font-mono text-[12px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                    {requestInfo.provider_correlation_id}
                  </span>
                </DetailRow>
              ) : null}
              <DetailRow label={messages.requestLogs.requestedModel}>
                <div className="space-y-1">
                  <p>{requestedModelLabel}</p>
                  {requestedModelLabel !== summary.model_id ? (
                    <p className="font-mono text-[11px] text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                      {summary.model_id}
                    </p>
                  ) : null}
                </div>
              </DetailRow>
              {resolvedTargetLabel && summary.resolved_target_model_id !== summary.model_id ? (
                <DetailRow label={messages.requestLogs.resolvedTarget}>
                  <div className="space-y-1">
                    <p>{resolvedTargetLabel}</p>
                    {resolvedTargetLabel !== summary.resolved_target_model_id ? (
                      <p className="font-mono text-[11px] text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                        {summary.resolved_target_model_id}
                      </p>
                    ) : null}
                  </div>
                </DetailRow>
              ) : null}
              <DetailRow label={messages.common.apiFamily}>
                <span className="flex items-center gap-2">
                  <ApiFamilyIcon apiFamily={apiFamily ?? ""} size={16} />
                  {formatApiFamily(apiFamily ?? "")}
                </span>
              </DetailRow>
              {summary.vendor_name ? (
                <DetailRow label={messages.common.vendor}>{summary.vendor_name}</DetailRow>
              ) : null}
              <DetailRow label={messages.requestLogs.path}>
                <span className="font-mono text-[12px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                  {requestInfo.request_path}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.stream}>
                {summary.is_stream ? <TypeBadge label={messages.requestLogs.streaming} intent="blue" /> : messages.requestLogs.no}
              </DetailRow>
            </div>

            <div className="space-y-1 border-t border-border/60 pt-3">
              <SectionSubheading>{messages.requestLogs.routingContext}</SectionSubheading>
              {routing.endpoint_id !== null ? (
                <DetailRow label={messages.requestLogs.endpoint}>
                  <span className="font-mono text-[12px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                    #{routing.endpoint_id}
                    {routing.endpoint_description ? ` - ${routing.endpoint_description}` : ""}
                  </span>
                </DetailRow>
              ) : null}
              {routing.endpoint_base_url ? (
                <DetailRow label={messages.requestLogs.baseUrl}>
                  <span className="font-mono text-[12px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                    {routing.endpoint_base_url}
                  </span>
                </DetailRow>
              ) : null}
            </div>
          </div>
        </SectionCard>

        <SectionCard icon={Coins} title={`${messages.requestLogs.tokenUsage} / ${messages.requestLogs.costBreakdown}`}>
          <div className="space-y-3">
            <div className="space-y-1">
              <SectionSubheading>{messages.requestLogs.tokenUsage}</SectionSubheading>
              <DetailRow label={messages.requestLogs.input}><span className="font-mono">{formatTokens(usage.input_tokens)}</span></DetailRow>
              <DetailRow label={messages.requestLogs.output}><span className="font-mono">{formatTokens(usage.output_tokens)}</span></DetailRow>
              <DetailRow label={messages.requestLogs.total}><span className="font-mono">{formatTokens(usage.total_tokens)}</span></DetailRow>
              {(usage.cache_read_input_tokens ?? 0) > 0 ? (
                <DetailRow label={messages.requestLogs.cacheRead}><span className="font-mono">{formatTokens(usage.cache_read_input_tokens)}</span></DetailRow>
              ) : null}
              {(usage.cache_creation_input_tokens ?? 0) > 0 ? (
                <DetailRow label={messages.requestLogs.cacheCreation}><span className="font-mono">{formatTokens(usage.cache_creation_input_tokens)}</span></DetailRow>
              ) : null}
              {(usage.reasoning_tokens ?? 0) > 0 ? (
                <DetailRow label={messages.requestLogs.reasoning}><span className="font-mono">{formatTokens(usage.reasoning_tokens)}</span></DetailRow>
              ) : null}
            </div>

            <div className="space-y-1 border-t border-border/60 pt-3">
              <SectionSubheading>{messages.requestLogs.costBreakdown}</SectionSubheading>
              <DetailRow label={messages.requestLogs.input}><span className="font-mono">{formatCost(costing.input_cost_micros, costing.report_currency_symbol)}</span></DetailRow>
              <DetailRow label={messages.requestLogs.output}><span className="font-mono">{formatCost(costing.output_cost_micros, costing.report_currency_symbol)}</span></DetailRow>
              <DetailRow label={messages.requestLogs.total}><span className="font-mono">{formatCost(costing.total_cost_user_currency_micros, costing.report_currency_symbol)}</span></DetailRow>
              <DetailRow label={messages.requestLogs.priced}>{usage.priced_flag ? messages.requestLogs.yes : messages.requestLogs.no}</DetailRow>
              <DetailRow label={messages.requestLogs.billable}>{usage.billable_flag ? messages.requestLogs.yes : messages.requestLogs.no}</DetailRow>
              {usage.unpriced_reason ? (
                <DetailRow label={messages.requestLogs.whyUnpriced}>{usage.unpriced_reason}</DetailRow>
              ) : null}
            </div>
          </div>
        </SectionCard>
      </div>
    </div>
  );
}
