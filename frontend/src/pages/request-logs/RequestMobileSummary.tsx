import { describeRequestFailure } from "./requestFailurePresentation";
import { ChainRankingValue } from "./ChainRankingValue";
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import type { ChainIngressItem } from "@/lib/types/request-logs";
import { Button } from "@/components/ui/button";
import {
  OperatorMissingValue,
  OperatorValue,
  OperatorTypeBadge,
  OperatorLoadingState,
} from "@/shared/design-system";
import { formatCost, formatDurationMs } from "./requestLogMetricPresentation";

export function RequestMobileSummary({
  chains,
  onSelect,
  loading,
  hasPrevious,
  hasNext,
  onPrevious,
  onNext,
  children,
}: {
  chains: ChainIngressItem[];
  onSelect: (id: string) => void;
  loading: boolean;
  hasPrevious: boolean;
  hasNext: boolean;
  onPrevious: () => void;
  onNext: () => void;
  children: React.ReactNode;
}) {
  const { messages } = useLocale();
  const copy = messages.requestComparison;
  const { format } = useTimezone();
  const [full, setFull] = useState(false);
  return (
    <>
      <section
        className="md:hidden min-w-0 flex flex-col gap-3"
        aria-label={copy.mobileTitle}
        data-testid="request-mobile-summary"
      >
        <h2>{copy.mobileTitle}</h2>
        {loading ? (
          <OperatorLoadingState title={copy.loading} />
        ) : (
          chains.map((chain) => {
            const summary = chain.finalized_summary;
            const requestId =
              summary?.request_log_id ?? chain.retained_rows[0]?.request_log_id;
            return (
              <article
                key={chain.ingress_request_id}
                className="operator-section-surface p-3 min-w-0 flex flex-col gap-2"
              >
                <div className="flex flex-wrap justify-between gap-2">
                  <span className="font-mono break-all">
                    {summary?.ingress_model?.id ?? (
                      <OperatorMissingValue reason={copy.unknownModel} />
                    )}
                  </span>
                  <OperatorValue
                    value={summary?.final_status_code}
                    reason={summary ? messages.honesty.noValue : copy.missingFinal}
                  >
                    {(status) => <OperatorTypeBadge label={describeRequestFailure({ statusCode: Number(status), streamOutcome: summary?.final_result === "client_disconnected" ? "client_disconnected" : undefined, errorPresent: summary?.final_result === "failed" })?.title ?? messages.requestLogs.attemptResultCompleted} preserveLabel />}
                  </OperatorValue>
                </div>
                <span className="font-mono text-xs break-all">
                  {chain.started_at ? (
                    format(chain.started_at)
                  ) : (
                    <OperatorMissingValue reason={copy.missingDuration} />
                  )}
                </span>
                <dl className="grid grid-cols-2 gap-2 text-xs">
                  <div>
                    <dt>{copy.elapsed}</dt>
                    <dd className="font-mono">
                      {chain.elapsed_ms !== null &&
                      chain.elapsed_evidence_state === "authoritative" ? (
                        formatDurationMs(chain.elapsed_ms)
                      ) : (
                        <OperatorMissingValue reason={copy.missingDuration} />
                      )}
                    </dd>
                  </div>
                  <div>
                    <dt>{copy.cost}</dt>
                    <dd className="font-mono">
                      {summary?.final_pricing_status === "priced" &&
                      summary.final_pricing_evidence_trust === "trusted" &&
                      summary.total_cost_user_currency_micros != null &&
                      summary.report_currency_code ? (
                        <>
                          {formatCost(
                            summary.total_cost_user_currency_micros,
                            summary.report_currency_symbol,
                          )}{" "}
                          {summary.report_currency_code}
                        </>
                      ) : (
                        <OperatorMissingValue reason={copy.missingCost} />
                      )}
                    </dd>
                  </div>
                </dl>
                {chain.ranking ? (
                  <div className="text-xs">
                    <span>{messages.chainRanking.column}</span>
                    <div className="font-mono tabular-nums">
                      <ChainRankingValue ranking={chain.ranking} />
                    </div>
                  </div>
                ) : null}
                <span className="text-xs text-muted-foreground">
                  {copy.attempts(chain.retained_upstream_attempt_count)}
                  {chain.chain_complete === false
                    ? ` · ${messages.requestLogs.retentionCoverageTitle}`
                    : ""}
                </span>
                <div className="flex flex-wrap gap-2">
                  {requestId ? (
                    <>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => onSelect(requestId)}
                      >
                        {copy.investigate}
                      </Button>
                      <Link
                        to="/observe/requests/$requestId/audit"
                        params={{ requestId }}
                        search={{ return_to: `${window.location.pathname}${window.location.search}` }}
                        className="inline-flex min-h-8 items-center px-2 text-primary text-sm"
                      >
                        {copy.audit}
                      </Link>
                    </>
                  ) : (
                    <OperatorMissingValue reason={copy.missingFinal} />
                  )}
                </div>
              </article>
            );
          })
        )}
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            disabled={loading || !hasPrevious}
            onClick={onPrevious}
          >
            {copy.previous}
          </Button>
          <Button
            variant="outline"
            disabled={loading || !hasNext}
            onClick={onNext}
          >
            {copy.next}
          </Button>
        </div>
        <Button
          variant="outline"
          aria-expanded={full}
          onClick={() => setFull((value) => !value)}
        >
          {copy.completeChain}
        </Button>
      </section>
      <div
        className={
          full || chains.length === 0 ? "min-w-0" : "hidden min-w-0 md:block"
        }
      >
        {children}
      </div>
    </>
  );
}
