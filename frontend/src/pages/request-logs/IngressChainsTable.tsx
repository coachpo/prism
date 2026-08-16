import { Fragment, useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import {
  OperatorClippedBadge,
  OperatorEmptyState,
  OperatorMissingValue,
  OperatorStatusBadge,
  OperatorTypeBadge,
  OperatorValueBadge,
  type OperatorStatusTier,
} from "@/shared/design-system";
import { OperationalTableSkeletonRows, operationalRowStripe } from "@/shared/table/operationalTable";
import type {
  ChainIngressItem as IngressChainItem,
  FinalizedSummary,
  RequestLogRowV2 as IngressChainRow,
} from "@/lib/types/request-logs-v2";

const CHAIN_COLUMN_COUNT = 10;

/**
 * Ingress-chain view (SPEC: `view=ingress_chains`): outer pages of retained
 * attempt chains per ingress request with bounded nested row pages. Chains
 * expand in place; per-chain row pagination uses the signed `row_cursor`;
 * chain pagination uses `chain_cursor`. Row clicks open the ordinary request
 * detail sheet (no audit payload is fetched here).
 *
 * This is the landing view, so it renders the `finalized_summary` the backend
 * already returns rather than an identifier and a count. Where the summary is
 * absent the cells say so — a chain whose finalized evidence is unavailable
 * must not read as a successful request with zero cost.
 */
export function IngressChainsTable({
  chains,
  hasMoreChains,
  chainPageCounts,
  onLoadMoreChains,
  onLoadMoreRows,
  onSelectRow,
  loading,
}: {
  chains: IngressChainItem[];
  hasMoreChains: boolean;
  nextChainCursor: string;
  chainPageCounts: { ingress: number; attempts: number; rows: number };
  onLoadMoreChains: () => void;
  onLoadMoreRows: (ingressRequestId: string, rowCursor: string) => void;
  onSelectRow: (requestLogId: string) => void;
  loading: boolean;
}) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.requestLogs;
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const toggle = (ingressRequestId: string) => {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(ingressRequestId)) next.delete(ingressRequestId);
      else next.add(ingressRequestId);
      return next;
    });
  };

  return (
    <div
      className="operator-table-shell overflow-hidden rounded-lg border border-border bg-panel"
      data-testid="ingress-chains-table"
    >
      <div className="flex items-center justify-between gap-2 border-b border-border bg-inset px-[var(--density-card-pad-x)] py-2 text-xs text-muted-foreground">
        <span data-testid="chain-page-counts">
          {copy.chainCounts(
            formatNumber(chainPageCounts.ingress),
            formatNumber(chainPageCounts.attempts),
            formatNumber(chainPageCounts.rows),
          )}
        </span>
      </div>

      {chains.length === 0 && !loading ? (
        <OperatorEmptyState title={copy.chainEmpty} description={copy.chainEmptyDescription} />
      ) : (
        <div className="overflow-x-auto">
          <Table aria-label={copy.requestLogsTitle}>
            <TableHeader>
              <TableRow>
                <TableHead className="w-8" />
                <TableHead>{copy.chainColumnTime}</TableHead>
                <TableHead>{copy.chainColumnResult}</TableHead>
                <TableHead>{copy.chainColumnRequestedModel}</TableHead>
                <TableHead>{copy.chainColumnFinalTarget}</TableHead>
                <TableHead>{copy.chainColumnEndpoint}</TableHead>
                <TableHead className="text-right">{copy.chainColumnAttempts}</TableHead>
                <TableHead className="text-right">TTFT</TableHead>
                <TableHead className="text-right">{copy.chainColumnTokens}</TableHead>
                <TableHead className="text-right">{copy.chainColumnCost}</TableHead>
                <TableHead>{copy.chainColumnPricing}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading && chains.length === 0 ? (
                <OperationalTableSkeletonRows columns={CHAIN_COLUMN_COUNT + 1} rows={6} />
              ) : null}
              {chains.map((chain) => (
                <Fragment key={chain.ingress_request_id}>
                  <ChainSummaryRow
                    chain={chain}
                    expanded={expanded.has(chain.ingress_request_id)}
                    onToggle={() => toggle(chain.ingress_request_id)}
                  />
                  {expanded.has(chain.ingress_request_id) ? (
                    <TableRow data-testid={`chain-${chain.ingress_request_id}`}>
                      <TableCell colSpan={CHAIN_COLUMN_COUNT + 1} className="bg-inset p-0">
                        <div className="flex flex-col gap-0.5 px-[var(--density-card-pad-x)] py-2">
                          {chain.retained_rows.map((row) => (
                            <ChainRowButton
                              key={row.request_log_id}
                              row={row}
                              onSelect={() => onSelectRow(row.request_log_id)}
                            />
                          ))}
                          {!chain.retained_rows_page_complete && chain.next_row_cursor ? (
                            <Button
                              variant="ghost"
                              size="sm"
                              className="justify-start text-xs"
                              onClick={() => onLoadMoreRows(chain.ingress_request_id, chain.next_row_cursor!)}
                              data-testid={`chain-rows-more-${chain.ingress_request_id}`}
                            >
                              {copy.chainLoadMoreRows}
                            </Button>
                          ) : null}
                        </div>
                      </TableCell>
                    </TableRow>
                  ) : null}
                </Fragment>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {hasMoreChains ? (
        <div className="flex justify-end border-t border-border bg-inset px-[var(--density-card-pad-x)] py-2">
          <Button variant="outline" size="sm" onClick={onLoadMoreChains} data-testid="chain-more">
            {messages.routingHealth.nextPage}
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function resultTier(summary: FinalizedSummary | null): OperatorStatusTier {
  if (!summary) return "idle";
  if (summary.final_result === "completed") return "healthy";
  if (summary.final_result === "client_disconnected") return "degraded";
  return "failing";
}

function pricingIntent(status: string) {
  switch (status) {
    case "priced":
      return "healthy" as const;
    case "unpriced":
      return "degraded" as const;
    case "ineligible":
      return "idle" as const;
    default:
      return "failing" as const;
  }
}

function ChainSummaryRow({
  chain,
  expanded,
  onToggle,
}: {
  chain: IngressChainItem;
  expanded: boolean;
  onToggle: () => void;
}) {
  const { formatNumber, messages } = useLocale();
  const { format } = useTimezone();
  const copy = messages.requestLogs;
  const observe = messages.observe;
  const summary = chain.finalized_summary;
  const tier = resultTier(summary);
  // Finalized evidence is either authoritative or unavailable; unavailable is a
  // gap in the record, not a completed request with empty numbers.
  const evidenceMissing = chain.finalized_evidence_state !== "authoritative" || summary === null;
  const missingReason = evidenceMissing ? copy.finalizedEvidenceUnavailable : messages.honesty.noValue;

  return (
    <TableRow
      data-testid={`chain-summary-${chain.ingress_request_id}`}
      className={cn("group/row", operationalRowStripe(tier))}
    >
      <TableCell className="w-8 pr-0">
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={expanded}
          aria-label={copy.chainToggleAria(chain.ingress_request_id)}
          className="flex size-6 items-center justify-center rounded-[4px] text-muted-foreground hover:bg-inset hover:text-foreground"
        >
          {expanded ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
        </button>
      </TableCell>

      <TableCell className="whitespace-nowrap font-mono tabular-nums">
        {chain.started_at ? format(chain.started_at) : <OperatorMissingValue reason={missingReason} />}
      </TableCell>

      <TableCell>
        <div className="flex flex-wrap items-center gap-1">
          {summary ? (
            <OperatorStatusBadge
              intent={tier}
              label={String(summary.final_status_code)}
              preserveLabel
            />
          ) : (
            <OperatorStatusBadge intent="idle" label={copy.finalizedEvidenceUnavailableShort} preserveLabel />
          )}
          {summary?.final_error_code ? (
            <span className="font-mono text-xs text-failing">{summary.final_error_code}</span>
          ) : null}
          {chain.chain_complete === false ? (
            <OperatorClippedBadge label={copy.chainIncomplete} reason={copy.chainIncompleteReason} />
          ) : null}
          {chain.legacy_unknown_row_count > 0 ? (
            <OperatorTypeBadge
              intent="degraded"
              label={copy.chainLegacyUnknown(formatNumber(chain.legacy_unknown_row_count))}
              preserveLabel
            />
          ) : null}
        </div>
      </TableCell>

      <TableCell className="max-w-52 truncate">
        {summary?.requested_model?.label ?? <OperatorMissingValue reason={missingReason} />}
      </TableCell>

      <TableCell className="max-w-52 truncate">
        {summary?.terminal_target?.label ?? summary?.resolved_model?.label ?? (
          <OperatorMissingValue reason={missingReason} />
        )}
      </TableCell>

      <TableCell className="max-w-40 truncate">
        {summary?.endpoint?.label ?? <OperatorMissingValue reason={missingReason} />}
      </TableCell>

      <TableCell className="text-right font-mono tabular-nums">
        {summary ? (
          formatNumber(summary.attempt_count)
        ) : (
          <OperatorMissingValue reason={missingReason} />
        )}
      </TableCell>

      <TableCell className="text-right font-mono tabular-nums">
        {summary?.ttft_ms == null ? (
          <OperatorMissingValue reason={missingReason} />
        ) : (
          `${formatNumber(summary.ttft_ms)} ms`
        )}
      </TableCell>

      <TableCell className="text-right font-mono tabular-nums">
        {summary?.total_tokens == null ? (
          <OperatorMissingValue reason={missingReason} />
        ) : (
          formatNumber(summary.total_tokens)
        )}
      </TableCell>

      <TableCell className="text-right font-mono tabular-nums">
        {summary?.total_cost_user_currency_micros == null ? (
          <OperatorMissingValue reason={missingReason} />
        ) : (
          `${summary.report_currency_symbol ?? "$"}${(
            Number(summary.total_cost_user_currency_micros) / 1_000_000
          ).toFixed(4)}`
        )}
      </TableCell>

      <TableCell>
        {summary ? (
          <OperatorTypeBadge
            intent={pricingIntent(summary.final_pricing_status)}
            label={pricingLabel(summary.final_pricing_status, observe)}
            preserveLabel
          />
        ) : (
          <OperatorMissingValue reason={missingReason} />
        )}
      </TableCell>
    </TableRow>
  );
}

function pricingLabel(status: string, copy: ReturnType<typeof useLocale>["messages"]["observe"]): string {
  switch (status) {
    case "priced":
      return copy.pricingPriced;
    case "unpriced":
      return copy.pricingUnpriced;
    case "ineligible":
      return copy.pricingIneligible;
    default:
      return copy.pricingUnknown;
  }
}

/**
 * `legacy_unknown` is deliberately left untranslated: it is the backend's own
 * name for rows whose kind was never recorded, and inventing a Chinese label
 * would imply the kind is known.
 */
function rowKindLabel(kind: string, copy: ReturnType<typeof useLocale>["messages"]["requestLogs"]): string {
  switch (kind) {
    case "planning":
      return copy.rowKindPlanning;
    case "admission":
      return copy.rowKindAdmission;
    case "upstream":
      return copy.rowKindUpstream;
    default:
      return copy.rowKindLegacyUnknown;
  }
}

function ChainRowButton({ row, onSelect }: { row: IngressChainRow; onSelect: () => void }) {
  const { messages } = useLocale();
  const statusCode = row.upstream_status_code ?? row.gateway_status_code ?? row.legacy_status_code;
  return (
    <button
      type="button"
      className="flex flex-wrap items-center gap-2 rounded-md px-2 py-1 text-left text-xs hover:bg-panel"
      onClick={onSelect}
      data-testid={`chain-row-${row.request_log_id}`}
    >
      <span className="font-mono text-muted-foreground">#{row.request_log_id}</span>
      <OperatorTypeBadge label={rowKindLabel(row.row_kind, messages.requestLogs)} preserveLabel className="text-[10px]" />
      {statusCode !== null && statusCode !== undefined ? (
        <OperatorValueBadge
          label={String(statusCode)}
          intent={statusCode < 400 ? "healthy" : "failing"}
          className="text-[10px]"
        />
      ) : null}
      {row.attempt_number !== null && row.attempt_number !== undefined ? (
        <span className="text-muted-foreground">{messages.requestLogs.attemptLabel(row.attempt_number)}</span>
      ) : null}
      {row.is_winner === true ? (
        <OperatorValueBadge label={messages.requestLogs.winner} intent="healthy" className="text-[10px]" />
      ) : null}
      {row.error_code ? <span className="font-mono text-destructive">{row.error_code}</span> : null}
    </button>
  );
}
