import { AlertTriangle, Coins, Copy, FileText } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { SpendTrustNote } from "@/components/SpendTrustIndicator";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useReportingCurrencyContext } from "@/context/ReportingCurrencyContext";
import { cn, formatApiFamily } from "@/lib/utils";
import type { MouseEvent } from "react";
import type { RequestLogDetail } from "@/lib/types";
import {
  formatCost,
  formatTokenRate,
  formatTtft,
  formatTokens,
} from "../columns";
import { formatUnpricedReasonLabel, resolveSpendTrustState } from "@/lib/costing";
import {
  OperatorCallout,
  OperatorTypeBadge,
  OperatorValueBadge,
} from "@/shared/design-system";
import {
  ApiFamilyPill,
  DetailRow,
  SectionCard,
  SummaryStat,
} from "./requestLogDetailShared";
import { copyRequestLogText, getStatusIntent, getStatusTone } from "./requestLogDetailUtils";
import { resolveRequestAuditCaptureMode } from "../requestLogAuditState";
import {
  getStreamOutcomeIntent,
  getStreamOutcomeLabel,
  hasStreamTelemetryOutcome,
  isHistoricalUnknownStreamRow,
  isStreamUsageUnavailableReason,
  shouldShowStreamStatus,
} from "../streamTelemetry";

interface RequestLogOverviewTabProps {
  request: RequestLogDetail;
  formatTimestamp: (iso: string) => string;
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

function getClientPrimaryValue(display: string | null, rawUserAgent: string | null): string {
  return display ?? rawUserAgent ?? "—";
}

function renderClientDetailValue(display: string | null, rawUserAgent: string | null) {
  const primaryValue = getClientPrimaryValue(display, rawUserAgent);
  const showRawValue = rawUserAgent !== null && rawUserAgent !== primaryValue;

  return (
    <div className="space-y-1">
      <p>{primaryValue}</p>
      {showRawValue ? (
        <p className="font-mono text-[11px] text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
          {rawUserAgent}
        </p>
      ) : null}
    </div>
  );
}

function formatPricingSnapshotValue(value: string | null): string {
  return value ?? "—";
}

function renderAuditCaptureState(
  routing: RequestLogDetail["routing"],
  messages: ReturnType<typeof useLocale>["messages"],
) {
  const captureMode = resolveRequestAuditCaptureMode(routing);

  switch (captureMode) {
    case "disabled":
      return <OperatorTypeBadge label={messages.requestLogs.auditDisabledAtRequest} intent="muted" />;
    case "metadata_only":
      return <OperatorTypeBadge label={messages.requestLogs.auditMetadataOnly} intent="info" />;
    case "full":
      return <OperatorTypeBadge label={messages.requestLogs.auditFullCapture} intent="success" />;
  }
}

function formatContextRoutingReason(
  reason: string,
  messages: ReturnType<typeof useLocale>["messages"],
) {
  switch (reason) {
    case "estimated_context_exceeds_usable_window":
      return messages.requestLogs.skippedTargetReasonContextWindowExceeded;
    case "usable_context_window_unavailable":
      return messages.requestLogs.skippedTargetReasonUsableContextWindowUnavailable;
    default:
      return reason;
  }
}

function formatRoutingTokenValue(value: number | null | undefined) {
  return value == null ? "—" : formatTokens(value);
}

export function RequestLogOverviewTab({
  request,
  formatTimestamp,
}: RequestLogOverviewTabProps) {
  const { currencyState } = useReportingCurrencyContext();
  const { formatNumber, messages } = useLocale();
  const summary = request.summary;
  const requestInfo = request.request;
  const routing = request.routing;
  const contextRouting = routing.context_routing ?? null;
  const selectedTerminalTargetId = routing.selected_terminal_target_id
    ?? contextRouting?.selected_terminal_target_id
    ?? null;
  const usage = request.usage;
  const costing = request.costing;
  const spendTrust = resolveSpendTrustState(
    {
      costMicros: costing.total_cost_user_currency_micros,
      priced: usage.priced_flag,
      unpricedReason: usage.unpriced_reason,
    },
    currencyState,
  );
  const tone = getStatusTone(summary.status_code);
  const requestedModelLabel = summary.model_label;
  const finalTargetModelId = summary.resolved_target_model_id ?? summary.model_id;
  const finalTargetLabel = summary.resolved_target_model_label ?? requestedModelLabel;
  const formattedErrorDetail = requestInfo.error_detail ? formatErrorDetail(requestInfo.error_detail) : null;
  const hasFormattedErrorDetail = formattedErrorDetail !== null && formattedErrorDetail !== requestInfo.error_detail;
  const requestReasoningEffort = requestInfo.request_generation_params?.reasoning?.effort ?? null;
  const apiFamily = summary.api_family;
  const callerClientPrimaryValue = getClientPrimaryValue(
    requestInfo.caller_client_display,
    requestInfo.caller_user_agent,
  );
  const upstreamClientPrimaryValue = getClientPrimaryValue(
    requestInfo.upstream_client_display,
    requestInfo.upstream_user_agent,
  );
  const showUpstreamClient =
    requestInfo.user_agent_overridden
    || requestInfo.upstream_client_display !== null
    || requestInfo.upstream_user_agent !== null;
  const showCallerClient =
    requestInfo.caller_client_display !== null
    || requestInfo.caller_user_agent !== null
    || requestInfo.user_agent_overridden;
  const streamUsageUnavailable = isStreamUsageUnavailableReason(usage.unpriced_reason);
  const historicalUnknownStream = isHistoricalUnknownStreamRow(summary.is_stream, summary.stream_outcome);
  const streamStatusLabel = getStreamOutcomeLabel(summary.stream_outcome, messages.requestLogs);
  const hasStreamTelemetry = hasStreamTelemetryOutcome(summary.stream_outcome);
  const showStreamStatus = shouldShowStreamStatus(summary.stream_outcome);
  const totalTokensValue = streamUsageUnavailable && usage.total_tokens === null
    ? messages.requestLogs.streamUsageUnavailable
    : formatTokens(usage.total_tokens);
  const totalCostValue = streamUsageUnavailable && costing.total_cost_user_currency_micros === null
    ? messages.requestLogs.streamUsageUnavailable
    : spendTrust === "unpriced"
      ? messages.spendTrust.unpriced
      : formatCost(costing.total_cost_user_currency_micros, costing.report_currency_symbol);

  const handleCopyErrorDetail = (event: MouseEvent<HTMLButtonElement>) => {
    if (!formattedErrorDetail) return;

    const container = event.currentTarget.closest("[data-clipboard-fallback-root]") as HTMLElement | null;
    void copyRequestLogText(formattedErrorDetail, messages.requestLogs.errorDetail, container);
  };

  return (
    <div className="space-y-3">
      <Card className={cn("overflow-hidden border shadow-sm", tone.card)}>
        <CardContent className="space-y-4 p-4">
          <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
            <div className="min-w-0 space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                <OperatorValueBadge label={String(summary.status_code)} intent={getStatusIntent(summary.status_code)} className="px-1.5 py-0 font-mono" />
                {hasStreamTelemetry ? (
                  <OperatorTypeBadge
                    label={streamStatusLabel}
                    intent={getStreamOutcomeIntent(summary.stream_outcome)}
                    className="px-2 py-0.5"
                    preserveLabel
                  />
                ) : null}
                <ApiFamilyPill apiFamily={apiFamily} />
              </div>

              <div className="min-w-0 space-y-1.5">
                <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                  <h3 className="truncate text-lg font-semibold tracking-tight sm:text-xl">{requestedModelLabel}</h3>
                </div>
                {requestedModelLabel !== summary.model_id ? (
                  <p className="font-mono text-[11px] text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                    {summary.model_id}
                  </p>
                ) : null}
                <p className="text-xs text-muted-foreground">
                  {messages.requestLogs.finalTargetModel}: {finalTargetLabel}
                </p>
                <p className="font-mono text-xs text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                  {requestInfo.request_path}
                </p>
              </div>
            </div>

            <div className="grid gap-2 sm:grid-cols-3 xl:w-[540px]" data-testid="request-log-summary-strip">
              <SummaryStat
                label={messages.requestLogs.latency}
                value={`${formatNumber(summary.response_time_ms)}ms`}
                valueClassName="font-mono"
              />
              <SummaryStat
                label={messages.requestLogs.ttft}
                value={formatTtft(summary.ttft_ms)}
                valueClassName="font-mono"
              />
              <SummaryStat
                label={messages.requestLogs.tokenRate}
                value={formatTokenRate(usage.output_tokens, summary.ttft_ms, summary.completion_duration_ms)}
                valueClassName="font-mono"
              />
              <SummaryStat
                label={messages.requestLogs.totalTokens}
                value={totalTokensValue}
                valueClassName="font-mono"
              />
              <SummaryStat
                label={messages.requestLogs.totalCost}
                value={(
                  <div className="flex flex-col items-start gap-1">
                    <span className="font-mono">
                      {totalCostValue}
                    </span>
                  </div>
                )}
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
        <OperatorCallout intent="danger" icon={<AlertTriangle />} className="shadow-sm">
          <div className="flex min-w-0 flex-col gap-3">
            <OperatorValueBadge label={String(summary.status_code)} intent={getStatusIntent(summary.status_code)} className="px-1.5 py-0 font-mono" />
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div className="flex flex-col gap-1">
                <p className="text-xs font-medium uppercase tracking-[0.16em]">{messages.requestLogs.errorDetail}</p>
                <p className="text-xs">
                  {hasFormattedErrorDetail
                    ? messages.requestLogs.formattedForReadability
                    : messages.requestLogs.capturedFailureDetail}
                </p>
              </div>
              <Button
                variant="outline"
                size="sm"
                className="h-7 rounded-full border-destructive/20 px-2.5 text-[11px] text-destructive hover:border-destructive/40 hover:bg-destructive/10"
                onClick={handleCopyErrorDetail}
              >
                <Copy data-icon="inline-start" />
                {messages.requestLogs.copy}
              </Button>
            </div>

            <ScrollArea className="max-h-56 rounded-lg border border-destructive/15 bg-background/85 shadow-inner">
              <pre className="max-w-full whitespace-pre-wrap break-words p-3 font-mono text-[11px] leading-5 text-foreground [overflow-wrap:anywhere]">
                {formattedErrorDetail}
              </pre>
            </ScrollArea>
          </div>
        </OperatorCallout>
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
              <DetailRow label={messages.requestLogs.proxyApiKey}>
                <span className="font-mono text-[12px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                  {requestInfo.proxy_api_key_name_snapshot ?? messages.requestLogs.proxyApiKeyNotRecorded}
                </span>
              </DetailRow>
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
              <DetailRow label={messages.requestLogs.finalTargetModel}>
                <div className="space-y-1">
                  <p>{finalTargetLabel}</p>
                  {finalTargetLabel !== finalTargetModelId ? (
                    <p className="font-mono text-[11px] text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                      {finalTargetModelId}
                    </p>
                  ) : null}
                </div>
              </DetailRow>
              {showCallerClient ? (
                <DetailRow label={messages.requestLogs.callerClient}>
                  {renderClientDetailValue(
                    requestInfo.caller_client_display,
                    requestInfo.caller_user_agent,
                  )}
                </DetailRow>
              ) : null}
              {showUpstreamClient
                && (requestInfo.user_agent_overridden
                  || upstreamClientPrimaryValue !== callerClientPrimaryValue
                  || requestInfo.upstream_user_agent !== requestInfo.caller_user_agent) ? (
                <DetailRow label={messages.requestLogs.upstreamClient}>
                  {renderClientDetailValue(
                    requestInfo.upstream_client_display,
                    requestInfo.upstream_user_agent,
                  )}
                </DetailRow>
              ) : null}
              <DetailRow label={messages.common.apiFamily}>
                <span className="flex items-center gap-2">
                  <ApiFamilyIcon apiFamily={apiFamily ?? ""} size={16} />
                  {formatApiFamily(apiFamily ?? "")}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.path}>
                <span className="font-mono text-[12px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                  {requestInfo.request_path}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.stream}>
                {hasStreamTelemetry ? (
                  <OperatorTypeBadge
                    label={streamStatusLabel}
                    intent={getStreamOutcomeIntent(summary.stream_outcome)}
                    preserveLabel
                  />
                ) : messages.requestLogs.no}
              </DetailRow>
              {showStreamStatus ? (
                <DetailRow label={messages.requestLogs.streamStatus}>{streamStatusLabel}</DetailRow>
              ) : null}
              {summary.stream_error_detail ? (
                <DetailRow label={messages.requestLogs.streamErrorDetail}>
                  <span className="font-mono text-[12px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                    {summary.stream_error_detail}
                  </span>
                </DetailRow>
              ) : null}
              {requestReasoningEffort ? (
                <DetailRow label={messages.requestLogs.reasoningEffort}>
                  <span className="font-mono">{requestReasoningEffort}</span>
                </DetailRow>
              ) : null}
            </div>

            <div className="space-y-1 border-t border-border/60 pt-3">
              <SectionSubheading>{messages.requestLogs.routingContext}</SectionSubheading>
              <DetailRow label={messages.requestLogs.endpoint}>
                <div className="space-y-1">
                  <p>{routing.endpoint_label}</p>
                  {routing.endpoint_id !== null ? (
                    <p className="font-mono text-[11px] text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                      #{routing.endpoint_id}
                    </p>
                  ) : null}
                </div>
              </DetailRow>
              <DetailRow label={messages.requestLogs.selectedTerminalTarget}>
                {selectedTerminalTargetId !== null ? (
                  <span className="font-mono text-[12px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                    #{selectedTerminalTargetId}
                  </span>
                ) : messages.requestLogs.noTerminalTargetSelected}
              </DetailRow>
              {routing.endpoint_base_url ? (
                <DetailRow label={messages.requestLogs.baseUrl}>
                  <span className="font-mono text-[12px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                    {routing.endpoint_base_url}
                  </span>
                </DetailRow>
              ) : null}
              <DetailRow label={messages.requestLogs.auditCapture}>
                {renderAuditCaptureState(routing, messages)}
              </DetailRow>
              {contextRouting ? (
                <>
                  {contextRouting.estimation_method ? (
                    <DetailRow label={messages.requestLogs.estimationMethod}>
                      <span className="font-mono text-[12px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                        {contextRouting.estimation_method}
                      </span>
                    </DetailRow>
                  ) : null}
                  {contextRouting.usable_context_window_tokens != null ? (
                    <DetailRow label={messages.requestLogs.usableContextWindow}>
                      <span className="font-mono">{formatRoutingTokenValue(contextRouting.usable_context_window_tokens)}</span>
                    </DetailRow>
                  ) : null}
                  {contextRouting.cost_ranking_method ? (
                    <DetailRow label={messages.requestLogs.costRankingMethod}>
                      <span className="font-mono text-[11px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                        {contextRouting.cost_ranking_method}
                      </span>
                    </DetailRow>
                  ) : null}
                  {contextRouting.selected_estimated_blended_cost_micros != null ? (
                    <DetailRow label={messages.requestLogs.selectedEstimatedBlendedCost}>
                      <span className="font-mono">
                        {formatNumber(contextRouting.selected_estimated_blended_cost_micros)} micros
                      </span>
                    </DetailRow>
                  ) : null}
                  <DetailRow label={messages.requestLogs.contextRoutingDecision}>
                    <div className="space-y-2">
                      <div className="space-y-1 text-sm">
                        <p><span className="text-muted-foreground">{messages.requestLogs.routingPolicy}:</span> <span className="font-mono">{contextRouting.policy}</span></p>
                        <p><span className="text-muted-foreground">{messages.requestLogs.estimatedInputTokens}:</span> <span className="font-mono">{formatRoutingTokenValue(contextRouting.estimated_input_tokens)}</span></p>
                        <p><span className="text-muted-foreground">{messages.requestLogs.reservedOutputTokens}:</span> <span className="font-mono">{formatRoutingTokenValue(contextRouting.reserved_output_tokens)}</span></p>
                        <p><span className="text-muted-foreground">{messages.requestLogs.estimatedTotalContextTokens}:</span> <span className="font-mono">{formatRoutingTokenValue(contextRouting.estimated_total_context_tokens)}</span></p>
                      </div>
                      {contextRouting.skipped_terminal_targets?.length ? (
                        <div className="space-y-1">
                          <p className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground">
                            {messages.requestLogs.skippedTerminalTargets}
                          </p>
                          <ul className="space-y-1 text-sm text-foreground">
                            {contextRouting.skipped_terminal_targets.map((target, index) => (
                              <li key={`${target.terminal_target_id ?? "none"}-${index}`} className="rounded-lg border border-border/60 bg-muted/20 px-2.5 py-2">
                                <p className="font-mono text-[12px]">
                                  #{target.terminal_target_id ?? "—"}
                                  {target.endpoint_id != null ? ` · endpoint #${target.endpoint_id}` : ""}
                                </p>
                                <p className="text-muted-foreground">
                                  {formatContextRoutingReason(target.reason, messages)}
                                </p>
                                <p className="font-mono text-[11px] text-muted-foreground">
                                  {messages.requestLogs.usableContextWindow}: {formatRoutingTokenValue(target.usable_context_window_tokens)}
                                  {" · "}
                                  {messages.requestLogs.estimatedTotalContextTokens}: {formatRoutingTokenValue(target.estimated_total_context_tokens)}
                                </p>
                              </li>
                            ))}
                          </ul>
                        </div>
                      ) : null}
                    </div>
                  </DetailRow>
                </>
              ) : null}
            </div>
          </div>
        </SectionCard>

        <SectionCard icon={Coins} title={`${messages.requestLogs.tokenUsage} / ${messages.requestLogs.costBreakdown}`}>
          <div className="space-y-3">
            <div className="space-y-1">
              <SectionSubheading>{messages.requestLogs.tokenUsage}</SectionSubheading>
              <DetailRow label={messages.requestLogs.input}>
                <span className="font-mono">
                  {streamUsageUnavailable && usage.input_tokens === null ? messages.requestLogs.streamUsageUnavailable : formatTokens(usage.input_tokens)}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.output}>
                <span className="font-mono">
                  {streamUsageUnavailable && usage.output_tokens === null ? messages.requestLogs.streamUsageUnavailable : formatTokens(usage.output_tokens)}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.total}><span className="font-mono">{totalTokensValue}</span></DetailRow>
              <DetailRow label={messages.requestLogs.cacheRead}>
                <span className="font-mono">{formatTokens(usage.cache_read_input_tokens)}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.cacheCreation}>
                <span className="font-mono">{formatTokens(usage.cache_creation_input_tokens)}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.reasoning}>
                <span className="font-mono">{formatTokens(usage.reasoning_tokens)}</span>
              </DetailRow>
            </div>

            <div className="space-y-1 border-t border-border/60 pt-3">
              <SectionSubheading>{messages.requestLogs.costBreakdown}</SectionSubheading>
              <DetailRow label={messages.requestLogs.input}><span className="font-mono">{formatCost(costing.input_cost_micros, costing.report_currency_symbol)}</span></DetailRow>
              <DetailRow label={messages.requestLogs.output}><span className="font-mono">{formatCost(costing.output_cost_micros, costing.report_currency_symbol)}</span></DetailRow>
              <DetailRow label={messages.requestLogs.total}>
                <div className="space-y-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-mono">
                      {totalCostValue}
                    </span>
                  </div>
                  {spendTrust !== "verified" && !streamUsageUnavailable ? (
                    <SpendTrustNote
                      spendTrust={spendTrust}
                      showPricingTemplatesLink={spendTrust === "unpriced" && !historicalUnknownStream}
                    />
                  ) : null}
                </div>
              </DetailRow>
              <DetailRow label={messages.requestLogs.priced}>{usage.priced_flag ? messages.requestLogs.yes : messages.requestLogs.no}</DetailRow>
              <DetailRow label={messages.requestLogs.billable}>{usage.billable_flag ? messages.requestLogs.yes : messages.requestLogs.no}</DetailRow>
              {usage.unpriced_reason ? (
                <DetailRow label={messages.requestLogs.whyUnpriced}>
                  {formatUnpricedReasonLabel(usage.unpriced_reason)}
                </DetailRow>
              ) : null}
              <DetailRow label={messages.requestLogs.reportCurrency}>
                <span className="font-mono">{costing.report_currency_code ?? "—"}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.sourceCurrency}>
                <span className="font-mono">{costing.currency_code_original ?? "—"}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.fxRateUsed}>
                <span className="font-mono">{costing.fx_rate_used ?? "—"}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.fxRateSource}>
                <span className="font-mono">{costing.fx_rate_source ?? "—"}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.pricingUnit}>
                <span className="font-mono">{request.pricing.pricing_snapshot_unit ?? "—"}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.pricingConfigVersion}>
                <span className="font-mono">{request.pricing.pricing_config_version_used ?? "—"}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.pricingSnapshotInput}>
                <span className="font-mono">{formatPricingSnapshotValue(request.pricing.pricing_snapshot_input)}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.pricingSnapshotOutput}>
                <span className="font-mono">{formatPricingSnapshotValue(request.pricing.pricing_snapshot_output)}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.pricingSnapshotCacheRead}>
                <span className="font-mono">{formatPricingSnapshotValue(request.pricing.pricing_snapshot_cache_read_input)}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.pricingSnapshotCacheCreation}>
                <span className="font-mono">{formatPricingSnapshotValue(request.pricing.pricing_snapshot_cache_creation_input)}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.pricingSnapshotReasoning}>
                <span className="font-mono">{formatPricingSnapshotValue(request.pricing.pricing_snapshot_reasoning)}</span>
              </DetailRow>
            </div>
          </div>
        </SectionCard>
      </div>
    </div>
  );
}
