import { AlertTriangle, Coins, Copy, FileText } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { formatApiFamily } from "@/components/apiFamilyPresentation";
import type { MouseEvent } from "react";
import type {
  RequestLogDetail,
  PricingProjection,
} from "@/lib/types/request-logs";
import {
  describeTokenRateMissing,
  formatCost,
  formatTokenRate,
  formatTokens,
  formatTtft,
} from "../requestLogMetricPresentation";
import { formatUnpricedReasonLabel } from "@/lib/costing";
import {
  cacheReadShare,
  classifyTokenComponents,
  describeUnpricedCause,
} from "../pricingExplanation";
import type { CacheReadShare } from "../pricingExplanation";
import {
  OperatorCallout,
  OperatorMissingValue,
  OperatorTypeBadge,
  OperatorValueBadge,
} from "@/shared/design-system";
import {
  ApiFamilyPill,
  DetailRow,
  SectionCard,
  SummaryStat,
} from "./requestLogDetailShared";
import { getStatusIntent, getStatusTone } from "./requestLogStatus";
import { copyRequestLogText } from "./requestLogClipboard";
import { resolveRequestAuditCaptureMode } from "../requestLogAuditState";
import {
  getStreamOutcomeIntent,
  getStreamOutcomeLabel,
  hasStreamTelemetryOutcome,
  isStreamUsageUnavailableReason,
  shouldShowStreamStatus,
} from "../streamTelemetry";
import { RequestLogPricingEvidence } from "./RequestLogPricingEvidence";

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

function getClientPrimaryValue(
  display: string | null,
  rawUserAgent: string | null,
): string {
  return display ?? rawUserAgent ?? "—";
}

function renderClientDetailValue(
  display: string | null,
  rawUserAgent: string | null,
) {
  const primaryValue = getClientPrimaryValue(display, rawUserAgent);
  const showRawValue = rawUserAgent !== null && rawUserAgent !== primaryValue;

  return (
    <div className="flex flex-col gap-1">
      <p>{primaryValue}</p>
      {showRawValue ? (
        <p className="font-mono text-[11px] text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
          {rawUserAgent}
        </p>
      ) : null}
    </div>
  );
}

// Scoped row status: upstream rows use the upstream HTTP status, planning/
// admission rows the gateway status, legacy rows the legacy projection.
function scopedStatus(request: RequestLogDetail): number | null {
  const summary = request.summary;
  switch (summary.row_kind) {
    case "upstream":
      return summary.upstream_status_code;
    case "planning":
    case "admission":
      return summary.gateway_status_code;
    default:
      return summary.legacy_status_code;
  }
}

function scopedDuration(request: RequestLogDetail): number | null {
  const summary = request.summary;
  return summary.row_kind === "upstream"
    ? summary.attempt_duration_ms
    : summary.legacy_duration_ms;
}

function requestAttemptTriggerLabel(
  value: RequestLogDetail["summary"]["attempt_trigger"],
  copy: ReturnType<typeof useLocale>["messages"]["requestLogs"],
) {
  switch (value) {
    case "initial":
      return copy.attemptTriggerInitial;
    case "retry_same_target":
      return copy.attemptTriggerRetrySameTarget;
    case "hedge":
      return copy.attemptTriggerHedge;
    case "failover":
      return copy.attemptTriggerFailover;
    default:
      return copy.attemptTriggerUnavailable;
  }
}

function pricingStateLabel(
  pricing: PricingProjection,
  messages: ReturnType<typeof useLocale>["messages"],
): string {
  switch (pricing.pricing_status) {
    case "priced":
      return messages.requestLogs.priced;
    case "unpriced":
      return messages.requestLogs.unpricedOnly;
    case "ineligible":
      return messages.requestLogs.notPricedNotApplicable ?? "不适用";
    case "unknown":
      return messages.requestLogs.unknown ?? "未知";
    default:
      return "—";
  }
}

/** Each unavailable reason keeps its own sentence: a reader who sees the em
 *  dash can tell an incomparable operation from an unreported component. */
function renderCacheReadShare(
  share: CacheReadShare,
  messages: ReturnType<typeof useLocale>["messages"],
) {
  if (share.kind === "value") {
    return (
      <span className="font-mono tabular-nums">{`${(share.share * 100).toFixed(1)}%`}</span>
    );
  }
  const copy = messages.requestLogs;
  const reason = {
    incomparable_operation: copy.cacheReadShareIncomparable,
    indeterminate_operation: copy.cacheReadShareIndeterminate,
    components_missing: copy.cacheReadShareComponentsMissing,
    no_prompt_tokens: copy.cacheReadShareNoPromptTokens,
  }[share.kind];
  return <OperatorMissingValue reason={reason} />;
}

function renderAuditCaptureState(
  routing: RequestLogDetail["routing"],
  messages: ReturnType<typeof useLocale>["messages"],
) {
  const captureMode = resolveRequestAuditCaptureMode(routing);

  switch (captureMode) {
    case "disabled":
      return (
        <OperatorTypeBadge
          label={messages.requestLogs.auditDisabledAtRequest}
          intent="muted"
        />
      );
    case "metadata_only":
      return (
        <OperatorTypeBadge
          label={messages.requestLogs.auditMetadataOnly}
          intent="accent"
        />
      );
    case "full":
      return (
        <OperatorTypeBadge
          label={messages.requestLogs.auditFullCapture}
          intent="healthy"
        />
      );
  }
}

export function RequestLogOverviewTab({
  request,
  formatTimestamp,
}: RequestLogOverviewTabProps) {
  const { formatNumber, messages } = useLocale();
  const summary = request.summary;
  const requestInfo = request.request;
  const routing = request.routing;
  const selectedTerminalTargetId = routing.selected_terminal_target_id ?? null;
  const usage = request.usage;
  const pricing = request.pricing;
  const failure = request.failure;
  const statusCode = scopedStatus(request);
  const durationMs = scopedDuration(request);
  // No scoped status means no tone to imply; a missing status must not read as
  // a server error.
  const tone =
    statusCode === null
      ? { card: "border-l-border" }
      : getStatusTone(statusCode);
  const requestedModelLabel = summary.model_label;
  const finalTargetModelId = summary.attempt_target_model_id;
  const finalTargetLabel = summary.attempt_target_model_label;
  const failureDetail = failure?.detail ?? null;
  const formattedErrorDetail = failureDetail
    ? formatErrorDetail(failureDetail)
    : null;
  const hasFormattedErrorDetail =
    formattedErrorDetail !== null && formattedErrorDetail !== failureDetail;
  const requestReasoningEffort =
    requestInfo.request_generation_params?.reasoning?.effort ?? null;
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
    requestInfo.user_agent_overridden ||
    requestInfo.upstream_client_display !== null ||
    requestInfo.upstream_user_agent !== null;
  const showCallerClient =
    requestInfo.caller_client_display !== null ||
    requestInfo.caller_user_agent !== null ||
    requestInfo.user_agent_overridden;
  const streamUsageUnavailable = isStreamUsageUnavailableReason(
    pricing.unpriced_reason,
  );
  const unpricedCause = describeUnpricedCause({
    pricingStatus: pricing.pricing_status,
    unpricedReason: pricing.unpriced_reason,
    streamOutcome: summary.stream_outcome as string | null,
  });
  const tokenCoverage = classifyTokenComponents({
    input: usage.input_tokens,
    output: usage.output_tokens,
    total: usage.total_tokens,
    cacheRead: usage.cache_read_input_tokens,
    cacheCreation: usage.cache_creation_input_tokens,
    reasoning: usage.reasoning_tokens,
  });
  const cacheReadShareResult = cacheReadShare({
    input: usage.input_tokens,
    cacheRead: usage.cache_read_input_tokens,
    cacheCreation: usage.cache_creation_input_tokens,
    operationName: request.request.operation_name,
  });
  const streamOutcome =
    summary.stream_outcome as import("@/lib/types").StreamOutcome;
  const streamStatusLabel = getStreamOutcomeLabel(
    streamOutcome,
    messages.requestLogs,
  );
  const hasStreamTelemetry = hasStreamTelemetryOutcome(streamOutcome);
  const showStreamStatus = shouldShowStreamStatus(streamOutcome);
  const totalTokensValue =
    streamUsageUnavailable && usage.total_tokens === null
      ? messages.requestLogs.streamUsageUnavailable
      : formatTokens(usage.total_tokens);
  const isPriced = pricing.pricing_status === "priced";
  const totalCostMicros = pricing.total_cost_user_currency_micros ?? null;
  const totalCostValue =
    streamUsageUnavailable && totalCostMicros === null
      ? messages.requestLogs.streamUsageUnavailable
      : !isPriced
        ? messages.spendTrust.unpriced
        : formatCost(totalCostMicros, pricing.report_currency_symbol);

  const handleCopyErrorDetail = (event: MouseEvent<HTMLButtonElement>) => {
    if (!formattedErrorDetail) return;

    const container = event.currentTarget.closest(
      "[data-clipboard-fallback-root]",
    ) as HTMLElement | null;
    void copyRequestLogText(
      formattedErrorDetail,
      messages.requestLogs.errorDetail,
      container,
    );
  };

  return (
    <div className="flex flex-col gap-3">
      <Card className={cn("overflow-hidden border", tone.card)}>
        <CardContent className="flex flex-col gap-4 p-4">
          <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
            <div className="flex min-w-0 flex-col gap-3">
              <div className="flex flex-wrap items-center gap-2">
                {statusCode !== null ? (
                  <OperatorValueBadge
                    label={String(statusCode)}
                    intent={getStatusIntent(statusCode)}
                    className="px-1.5 py-0 font-mono"
                  />
                ) : null}
                <OperatorTypeBadge
                  label={pricingStateLabel(pricing, messages)}
                  intent={
                    isPriced
                      ? "healthy"
                      : pricing.pricing_status === "ineligible"
                        ? "idle"
                        : "degraded"
                  }
                  className="px-2 py-0.5"
                  preserveLabel
                />
                {hasStreamTelemetry ? (
                  <OperatorTypeBadge
                    label={streamStatusLabel}
                    intent={getStreamOutcomeIntent(streamOutcome)}
                    className="px-2 py-0.5"
                    preserveLabel
                  />
                ) : null}
                <ApiFamilyPill apiFamily={apiFamily} />
              </div>

              <div className="flex min-w-0 flex-col gap-1.5">
                <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                  <h3 className="truncate text-lg font-semibold tracking-tight sm:text-xl">
                    {requestedModelLabel}
                  </h3>
                </div>
                {requestedModelLabel !== summary.ingress_model_id ? (
                  <p className="font-mono text-[11px] text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                    {summary.ingress_model_id}
                  </p>
                ) : null}
                <p className="text-xs text-muted-foreground">
                  {messages.requestLogs.attemptTargetModel}:{" "}
                  {finalTargetLabel ?? finalTargetModelId ?? (
                    <OperatorMissingValue
                      reason={
                        messages.requestLogs.attemptTargetEvidenceUnavailable
                      }
                    />
                  )}
                </p>
                <p className="font-mono text-xs text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                  {requestInfo.request_path}
                </p>
              </div>
            </div>

            <div
              className="grid gap-2 sm:grid-cols-3 xl:w-[540px]"
              data-testid="request-log-summary-strip"
            >
              <SummaryStat
                label={messages.requestLogs.latency}
                value={
                  durationMs === null ? (
                    <OperatorMissingValue reason={messages.honesty.noValue} />
                  ) : (
                    `${formatNumber(durationMs)} ms`
                  )
                }
                valueClassName="font-mono"
              />
              <SummaryStat
                label={messages.requestLogs.ttft}
                value={formatTtft(summary.ttft_ms)}
                valueClassName="font-mono"
              />
              <SummaryStat
                label={messages.requestLogs.tokenRate}
                value={
                  summary.output_rate_state === "measured" ? (
                    formatTokenRate(
                      summary.output_rate_tps,
                      summary.output_rate_state,
                    )
                  ) : (
                    <OperatorMissingValue
                      reason={describeTokenRateMissing({
                        rateTps: summary.output_rate_tps,
                        state: summary.output_rate_state,
                        reason: summary.output_rate_reason,
                      })}
                    />
                  )
                }
                valueClassName="font-mono"
              />
              <SummaryStat
                label={messages.requestLogs.totalTokens}
                value={totalTokensValue}
                valueClassName="font-mono"
              />
              <SummaryStat
                label={messages.requestLogs.totalCost}
                value={
                  <div className="flex flex-col items-start gap-1">
                    <span className="font-mono">{totalCostValue}</span>
                  </div>
                }
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
        <OperatorCallout intent="danger" icon={<AlertTriangle />} className="">
          <div className="flex min-w-0 flex-col gap-3">
            {statusCode !== null ? (
              <OperatorValueBadge
                label={String(statusCode)}
                intent={getStatusIntent(statusCode)}
                className="px-1.5 py-0 font-mono"
              />
            ) : null}
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div className="flex flex-col gap-1">
                <p className="text-xs font-medium uppercase tracking-[0.16em]">
                  {messages.requestLogs.errorDetail}
                </p>
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

      <div
        className="grid gap-3 xl:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)]"
        data-testid="request-log-overview-grid"
      >
        <SectionCard
          icon={FileText}
          title={messages.requestLogs.requestDetails}
        >
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1">
              <DetailRow label={messages.requestLogs.requestId}>
                <span className="font-mono">#{summary.request_log_id}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.time}>
                <span className="font-mono text-xs">
                  {formatTimestamp(summary.created_at)}
                </span>
              </DetailRow>
              {requestInfo.ingress_request_id ? (
                <DetailRow label={messages.requestLogs.ingressRequestId}>
                  <span className="font-mono text-[12px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                    {requestInfo.ingress_request_id}
                  </span>
                </DetailRow>
              ) : null}
              {summary.attempt_number !== null ? (
                <DetailRow label={messages.requestLogs.attemptNumber}>
                  <span className="font-mono">
                    {formatNumber(summary.attempt_number)}
                  </span>
                </DetailRow>
              ) : null}
              {summary.attempt_trigger ? (
                <DetailRow
                  label={messages.requestLogs.attemptTrigger ?? "尝试触发"}
                >
                  <span>
                    {requestAttemptTriggerLabel(
                      summary.attempt_trigger,
                      messages.requestLogs,
                    )}
                  </span>
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
                  {requestInfo.proxy_api_key_name_snapshot ??
                    messages.requestLogs.proxyApiKeyNotRecorded}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.requestedModel}>
                <div className="flex flex-col gap-1">
                  <p>{requestedModelLabel}</p>
                  {requestedModelLabel !== summary.ingress_model_id ? (
                    <p className="font-mono text-[11px] text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                      {summary.ingress_model_id}
                    </p>
                  ) : null}
                </div>
              </DetailRow>
              <DetailRow label={messages.requestLogs.attemptTargetModel}>
                {finalTargetModelId === null ? (
                  <OperatorMissingValue
                    reason={
                      messages.requestLogs.attemptTargetEvidenceUnavailable
                    }
                  />
                ) : (
                  <div className="flex flex-col gap-1">
                    <p>{finalTargetLabel ?? finalTargetModelId}</p>
                    {finalTargetLabel &&
                    finalTargetLabel !== finalTargetModelId ? (
                      <p className="font-mono text-[11px] text-muted-foreground whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                        {finalTargetModelId}
                      </p>
                    ) : null}
                  </div>
                )}
              </DetailRow>
              {request.terminal_target ? (
                <DetailRow
                  label={messages.requestLogs.terminalTarget ?? "终端目标"}
                >
                  <div className="flex flex-col gap-1">
                    <p>
                      {request.terminal_target.name ??
                        `#${request.terminal_target.terminal_target_id}`}
                    </p>
                    <p className="font-mono text-[11px] text-muted-foreground">
                      #{request.terminal_target.terminal_target_id}
                    </p>
                  </div>
                </DetailRow>
              ) : null}
              {showCallerClient ? (
                <DetailRow label={messages.requestLogs.callerClient}>
                  {renderClientDetailValue(
                    requestInfo.caller_client_display,
                    requestInfo.caller_user_agent,
                  )}
                </DetailRow>
              ) : null}
              {showUpstreamClient &&
              (requestInfo.user_agent_overridden ||
                upstreamClientPrimaryValue !== callerClientPrimaryValue ||
                requestInfo.upstream_user_agent !==
                  requestInfo.caller_user_agent) ? (
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
                    intent={getStreamOutcomeIntent(streamOutcome)}
                    preserveLabel
                  />
                ) : (
                  messages.requestLogs.no
                )}
              </DetailRow>
              {showStreamStatus ? (
                <DetailRow label={messages.requestLogs.streamStatus}>
                  {streamStatusLabel}
                </DetailRow>
              ) : null}
              {summary.stream_error_kind ? (
                <DetailRow
                  label={messages.requestLogs.streamErrorKind ?? "流式错误类型"}
                >
                  <span className="font-mono text-[12px] whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
                    {summary.stream_error_kind}
                  </span>
                </DetailRow>
              ) : null}
              {requestReasoningEffort ? (
                <DetailRow label={messages.requestLogs.reasoningEffort}>
                  <span className="font-mono">{requestReasoningEffort}</span>
                </DetailRow>
              ) : null}
            </div>

            <div className="flex flex-col gap-1 border-t border-border pt-3">
              <SectionSubheading>
                {messages.requestLogs.routingContext}
              </SectionSubheading>
              <DetailRow label={messages.requestLogs.endpoint}>
                <div className="flex flex-col gap-1">
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
                ) : (
                  messages.requestLogs.noTerminalTargetSelected
                )}
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
            </div>
          </div>
        </SectionCard>

        <SectionCard
          icon={Coins}
          title={`${messages.requestLogs.tokenUsage} / ${messages.requestLogs.costBreakdown}`}
        >
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1">
              <SectionSubheading>
                {messages.requestLogs.tokenUsage}
              </SectionSubheading>
              <DetailRow label={messages.requestLogs.input}>
                <span className="font-mono">
                  {streamUsageUnavailable && usage.input_tokens === null
                    ? messages.requestLogs.streamUsageUnavailable
                    : formatTokens(usage.input_tokens)}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.output}>
                <span className="font-mono">
                  {streamUsageUnavailable && usage.output_tokens === null
                    ? messages.requestLogs.streamUsageUnavailable
                    : formatTokens(usage.output_tokens)}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.total}>
                <span className="font-mono">{totalTokensValue}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.cacheRead}>
                <span className="font-mono">
                  {streamUsageUnavailable &&
                  usage.cache_read_input_tokens === null
                    ? messages.requestLogs.streamUsageUnavailable
                    : formatTokens(usage.cache_read_input_tokens)}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.cacheCreation}>
                <span className="font-mono">
                  {streamUsageUnavailable &&
                  usage.cache_creation_input_tokens === null
                    ? messages.requestLogs.streamUsageUnavailable
                    : formatTokens(usage.cache_creation_input_tokens)}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.cacheReadShare}>
                {streamUsageUnavailable &&
                usage.cache_read_input_tokens === null ? (
                  <span className="font-mono">
                    {messages.requestLogs.streamUsageUnavailable}
                  </span>
                ) : (
                  renderCacheReadShare(cacheReadShareResult, messages)
                )}
              </DetailRow>
              <DetailRow label={messages.requestLogs.reasoning}>
                <span className="font-mono">
                  {streamUsageUnavailable && usage.reasoning_tokens === null
                    ? messages.requestLogs.streamUsageUnavailable
                    : formatTokens(usage.reasoning_tokens)}
                </span>
              </DetailRow>
              {tokenCoverage.kind === "residual" ||
              tokenCoverage.kind === "total_only" ? (
                <DetailRow label={messages.requestLogs.uncategorizedTokens}>
                  <span className="font-mono">
                    {formatTokens(tokenCoverage.uncategorized)}
                  </span>
                </DetailRow>
              ) : null}
              <span className="text-[11px] text-muted-foreground">
                {tokenCoverage.kind === "total_only"
                  ? messages.requestLogs.tokenComponentsTotalOnly
                  : messages.requestLogs.tokenComponentBasisDisjoint}
              </span>
            </div>

            <div className="flex flex-col gap-1 border-t border-border pt-3">
              <SectionSubheading>
                {messages.requestLogs.costBreakdown}
              </SectionSubheading>
              <DetailRow label={messages.requestLogs.totalCost}>
                <div className="flex flex-col gap-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-mono">{totalCostValue}</span>
                  </div>
                  {!isPriced && !streamUsageUnavailable ? (
                    <span className="text-[11px] text-muted-foreground">
                      {messages.spendTrust.unpriced}
                      {pricing.unpriced_reason
                        ? ` · ${formatUnpricedReasonLabel(pricing.unpriced_reason)}`
                        : ""}
                    </span>
                  ) : null}
                </div>
              </DetailRow>
              {pricing.unpriced_reason ? (
                <DetailRow label={messages.requestLogs.whyUnpriced}>
                  <div className="flex flex-col gap-1">
                    <span>
                      {formatUnpricedReasonLabel(pricing.unpriced_reason)}
                    </span>
                    {unpricedCause ? (
                      <span className="text-[11px] text-muted-foreground">
                        {unpricedCause}
                      </span>
                    ) : null}
                  </div>
                </DetailRow>
              ) : null}
              <DetailRow label={messages.requestLogs.reportCurrency}>
                <span className="font-mono">
                  {pricing.report_currency_code ?? "—"}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.sourceCurrency}>
                <span className="font-mono">
                  {pricing.currency_code_original ?? "—"}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.fxRateUsed}>
                <span className="font-mono">{pricing.fx_rate_used ?? "—"}</span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.fxRateSource}>
                <span className="font-mono">
                  {pricing.fx_rate_source ?? "—"}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.pricingUnit}>
                <span className="font-mono">
                  {pricing.pricing_snapshot_unit ?? "—"}
                </span>
              </DetailRow>
              <DetailRow label={messages.requestLogs.pricingConfigVersion}>
                <span className="font-mono">
                  {pricing.pricing_config_version_used ?? "—"}
                </span>
              </DetailRow>
              <RequestLogPricingEvidence
                pricing={pricing}
                formatTimestamp={formatTimestamp}
              />
            </div>
          </div>
        </SectionCard>
      </div>
    </div>
  );
}
