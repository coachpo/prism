import { Link } from "@tanstack/react-router";
import { Terminal } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { useRequestLogChain } from "./useRequestLogChain";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import type { ChainResponse, RequestLogDetail } from "@/lib/types/request-logs";
import { RequestLogOverviewTab } from "./detail/RequestLogOverviewTab";

interface RequestLogDetailSheetProps {
  request: RequestLogDetail | null;
  open: boolean;
  onClose: () => void;
  formatTimestamp: (iso: string) => string;
  canPrevious?: boolean;
  canNext?: boolean;
  onPrevious?: () => void;
  onNext?: () => void;
}

export function RequestLogDetailSheet({
  request,
  open,
  onClose,
  formatTimestamp,
  canPrevious = false,
  canNext = false,
  onPrevious,
  onNext,
}: RequestLogDetailSheetProps) {
  const { messages } = useLocale();
  const hasRequestContext = Boolean(request);
  const ingressRequestId = request?.request.ingress_request_id ?? null;
  const { chain, loading: chainLoading } = useRequestLogChain({
    ingressRequestId,
    enabled: open && ingressRequestId !== null,
  });

  return (
    <Sheet open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onClose(); }}>
      <SheetContent
        className="w-full overflow-x-hidden overflow-y-auto border-l border-border bg-panel px-0 sm:max-w-3xl xl:max-w-[72rem]"
        data-clipboard-fallback-root=""
        data-testid="request-log-detail-sheet"
      >
        <div className="flex min-h-full flex-col gap-4 px-4 pb-5 pt-4 sm:px-6">
          <SheetHeader className="gap-2 pr-8 text-left">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Terminal className="h-3.5 w-3.5" />
              <span>{messages.requestLogs.technicalInspection}</span>
            </div>
            <SheetTitle className="text-xl font-semibold tracking-tight">
              {messages.requestLogs.requestTitle(request?.summary.request_log_id ?? "")}
            </SheetTitle>
            <SheetDescription className="text-sm text-muted-foreground">
              {messages.requestLogs.detailDescription}
              {hasRequestContext ? ` ${messages.requestLogs.requestedModel} / ${messages.requestLogs.finalTargetModel}.` : ""}
            </SheetDescription>
          </SheetHeader>

          {request && (
            <div className="flex items-center justify-end gap-2" aria-label={messages.requestLogs.requestNavigation}>
              <Button type="button" variant="outline" size="sm" data-testid="sheet-previous" disabled={!canPrevious} onClick={onPrevious}>
                {messages.requestLogs.previousPage}
              </Button>
              <Button type="button" variant="outline" size="sm" data-testid="sheet-next" disabled={!canNext} onClick={onNext}>
                {messages.requestLogs.nextPage}
              </Button>
            </div>
          )}

          {request && (
            <div className="flex min-w-0 flex-col gap-3">
              <div className="flex justify-end">
                <Button variant="outline" size="sm" asChild>
                  <Link
                    to="/observe/requests/$requestId/audit"
                    params={{ requestId: String(request.summary.request_log_id) }}
                  >
                    <Terminal data-icon="inline-start" />
                    {messages.requestLogs.openDedicatedAuditPage}
                  </Link>
                </Button>
              </div>
              <RequestLogOverviewTab
                request={request}
                formatTimestamp={formatTimestamp}
              />
              {ingressRequestId ? (
                <RetainedChainSection
                  ingressRequestId={ingressRequestId}
                  currentRequestLogId={request.summary.request_log_id}
                  chain={chain}
                  loading={chainLoading}
                />
              ) : null}
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}


// Retained ingress chain section (Requests SPEC §12.2): server-owned attempt
// order with triggers, winner markers, and chain completeness. The client
// never reconstructs the chain from the current page.
function RetainedChainSection({
  ingressRequestId,
  currentRequestLogId,
  chain,
  loading,
}: {
  ingressRequestId: string;
  currentRequestLogId: string;
  chain: ChainResponse | null;
  loading: boolean;
}) {
  const { messages } = useLocale();
  const ingressItem = chain?.items.find((item) => item.ingress_request_id === ingressRequestId) ?? null;

  return (
    <section className="rounded-lg border border-border bg-panel" data-testid="request-log-chain-section">
      <div className="border-b border-border bg-inset px-4 py-3">
        <h3 className="text-sm font-semibold tracking-tight text-foreground">
          {messages.requestLogs.retainedChain ?? "保留尝试链"}
        </h3>
        <p className="mt-0.5 font-mono text-[11px] text-muted-foreground break-all">
          {ingressRequestId}
        </p>
      </div>
      <div className="flex flex-col gap-2 p-4">
        {loading ? (
          <p className="text-xs text-muted-foreground">…</p>
        ) : !ingressItem ? (
          <p className="text-xs text-muted-foreground">{messages.requestLogs.chainUnavailable ?? "无法加载该入口请求的保留尝试链。"}</p>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
              <span>
                {messages.requestLogs.retainedAttempts ?? "保留尝试"}: {ingressItem.retained_upstream_attempt_count}
              </span>
              <span aria-hidden="true">·</span>
              <span>
                {messages.requestLogs.retainedRows ?? "保留行"}: {ingressItem.retained_request_log_row_count}
              </span>
              {ingressItem.legacy_unknown_row_count > 0 ? (
                <>
                  <span aria-hidden="true">·</span>
                  <span>{messages.requestLogs.legacyRows ?? "历史行"}: {ingressItem.legacy_unknown_row_count}</span>
                </>
              ) : null}
              {ingressItem.chain_complete !== null ? (
                <>
                  <span aria-hidden="true">·</span>
                  <span className={ingressItem.chain_complete ? "text-healthy" : "text-degraded"}>
                    {ingressItem.chain_complete
                      ? (messages.requestLogs.chainComplete ?? "链完整")
                      : (messages.requestLogs.chainIncomplete ?? "链不完整（保留/证据缺口）")}
                  </span>
                </>
              ) : null}
            </div>
            {ingressItem.failover_occurred || ingressItem.hedge_occurred || ingressItem.same_target_retry_occurred ? (
              <div className="flex flex-wrap gap-2 text-[11px]">
                {ingressItem.same_target_retry_occurred ? (
                  <span className="rounded-full border border-border px-2 py-0.5 text-muted-foreground">retry_same_target</span>
                ) : null}
                {ingressItem.hedge_occurred ? (
                  <span className="rounded-full border border-primary/30 bg-primary/10 px-2 py-0.5 text-primary">hedge</span>
                ) : null}
                {ingressItem.failover_occurred ? (
                  <span className="rounded-full border border-degraded/30 bg-degraded/10 px-2 py-0.5 text-degraded">confirmed failover</span>
                ) : null}
              </div>
            ) : null}
            <ol className="flex flex-col gap-1.5">
              {ingressItem.retained_rows.map((row) => {
                const isCurrent = row.request_log_id === currentRequestLogId;
                const status = row.row_kind === "upstream" ? row.upstream_status_code : row.gateway_status_code ?? row.legacy_status_code;
                return (
                  <li
                    key={row.request_log_id}
                    className={cnRow(isCurrent, row.attempt_result)}
                    aria-current={isCurrent ? "true" : undefined}
                  >
                    <span className="shrink-0 font-mono text-[11px] text-muted-foreground">#{row.attempt_number ?? "—"}</span>
                    <span className="shrink-0 font-mono text-[11px]">{row.attempt_trigger ?? "unknown"}</span>
                    <span className="shrink-0 font-mono text-[11px]">{row.attempt_result ?? "—"}</span>
                    {status !== null ? (
                      <span className="shrink-0 font-mono text-[11px]">{status}</span>
                    ) : null}
                    {row.is_winner === true ? (
                      <span className="shrink-0 rounded-full bg-healthy/15 px-2 py-0.5 text-[10px] text-healthy">winner</span>
                    ) : null}
                    {isCurrent ? (
                      <span className="shrink-0 rounded-full bg-primary/15 px-2 py-0.5 text-[10px] text-primary">current</span>
                    ) : null}
                  </li>
                );
              })}
            </ol>
            {ingressItem.retained_rows_page_complete === false && ingressItem.next_row_cursor ? (
              <p className="text-[11px] text-muted-foreground">
                {messages.requestLogs.chainMoreRows ?? "该链还有更多保留行（嵌套分页）。"}
              </p>
            ) : null}
          </>
        )}
      </div>
    </section>
  );
}

function cnRow(isCurrent: boolean, attemptResult: string | null | undefined): string {
  const base = "flex flex-wrap items-center gap-2 rounded-lg border px-3 py-2 ";
  if (isCurrent) return base + "border-primary/25 bg-primary/[0.06]";
  if (attemptResult === "http_error" || attemptResult === "stream_error" || attemptResult === "transport_error") {
    return base + "border-destructive/20 bg-destructive/[0.04]";
  }
  return base + "border-border bg-inset";
}
