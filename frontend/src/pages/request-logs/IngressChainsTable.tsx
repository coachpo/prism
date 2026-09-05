import { Fragment, useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import {
	ChevronDown,
	ChevronLeft,
	ChevronRight,
	Copy,
	FileSearch,
	PanelRight,
} from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import { Button } from "@/components/ui/button";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
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
import { copyRequestLogText } from "./detail/requestLogClipboard";
import {
	OperationalTableSkeletonRows,
	operationalRowStripe,
} from "@/shared/table/operationalTable";
import {
	LoadMoreControl,
	PaginationLiveStatus,
} from "@/shared/table/paginationControls";
import type {
	ChainIngressItem,
	FinalizedSummary,
	RequestLogChainRow,
} from "@/lib/types/request-logs";
import {
	describeTokenRateMissing,
	formatTokenRate,
} from "./requestLogMetricPresentation";
import type { ChainRowReadState } from "./useRequestLogIngressChains";
import { UpstreamModelIdValue } from "./UpstreamModelIdValue";

const CHAIN_COLUMN_COUNT = 12;

/**
 * Ingress-chain view (SPEC: `view=ingress_chains`): outer pages of retained
 * attempt chains per ingress request with bounded nested row pages. Chains
 * expand in place; per-chain row pagination uses the signed `row_cursor`;
 * chain pagination uses `chain_cursor`. Row clicks open the ordinary request
 * detail sheet (no audit payload is fetched here).
 *
 * Honest states: an outer page turn replaces the rows with skeletons under a
 * busy shell (the old page never masquerades as the new URL's cohort), while
 * a same-scope refresh keeps the rows. A failed row_cursor append keeps the
 * loaded rows and retries inline on its own chain.
 *
 * This is the landing view, so it renders the `finalized_summary` the backend
 * already returns rather than an identifier and a count. Where the summary is
 * absent the cells say so — a chain whose finalized evidence is unavailable
 * must not read as a successful request with zero cost.
 */
export function IngressChainsTable({
	chains,
	total,
	hasPreviousChains,
	hasMoreChains,
	chainPageStart,
	chainPageCounts,
	replacing,
	chainRowReads,
	onLoadPreviousChains,
	onLoadNextChains,
	onLoadMoreRows,
	onSelectRow,
	loading,
	retentionClipped,
	emptyAction,
}: {
	chains: ChainIngressItem[];
	total: number;
	hasPreviousChains: boolean;
	hasMoreChains: boolean;
	chainPageStart: number | null;
	chainPageCounts: { ingress: number; attempts: number; rows: number };
	/** True while an outer replace read has old rows withdrawn for skeletons. */
	replacing: boolean;
	chainRowReads: Record<string, ChainRowReadState>;
	onLoadPreviousChains: () => void;
	onLoadNextChains: () => void;
	onLoadMoreRows: (ingressRequestId: string, rowCursor: string) => void;
	onSelectRow: (requestLogId: string) => void;
	loading: boolean;
	/** True only when the backend's coverage says this window was clipped. */
	retentionClipped: boolean;
	/** Next step for a filtered-to-nothing result; omitted when there is none. */
	emptyAction?: ReactNode;
}) {
	const { formatNumber, messages } = useLocale();
	const copy = messages.requestLogs;
	const tableCopy = messages.operationalTable;
	const [expanded, setExpanded] = useState<Set<string>>(new Set());
	const showPendingRows = replacing && chains.length > 0;
	const chainPageEnd =
		total > 0 && chainPageStart !== null
			? Math.min(chainPageStart + chainPageCounts.ingress, total)
			: 0;
	const pageSummary =
		total > 0 && chainPageStart !== null
			? copy.resultsRange(
					formatNumber(chainPageStart + 1),
					formatNumber(chainPageEnd),
					formatNumber(total),
				)
			: total > 0
				? copy.chainPageSummary(
						formatNumber(chainPageCounts.ingress),
						formatNumber(total),
					)
				: copy.zeroResults;

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
			<PaginationLiveStatus
				message={showPendingRows ? tableCopy.loadingTargetPage : null}
			/>
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
				<OperatorEmptyState
					title={
						retentionClipped ? copy.emptyCoverageClipped : copy.chainEmpty
					}
					description={
						retentionClipped ? (
							<span className="inline-flex flex-col items-center gap-2">
								<span>{copy.emptyCoverageClippedDescription}</span>
								<OperatorClippedBadge
									label={messages.honesty.outsideRetention}
									reason={messages.honesty.outsideRetentionReason}
								/>
							</span>
						) : (
							copy.chainEmptyDescription
						)
					}
					action={emptyAction}
				/>
			) : (
				<div className="overflow-x-auto" aria-busy={showPendingRows}>
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
								<TableHead className="text-right">{copy.tokenRate}</TableHead>
								<TableHead className="text-right">{copy.chainColumnTokens}</TableHead>
								<TableHead className="text-right">{copy.chainColumnCost}</TableHead>
								<TableHead>{copy.chainColumnPricing}</TableHead>
								<TableHead className="text-right">
									{copy.chainRowActions}
								</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{showPendingRows || (loading && chains.length === 0) ? (
								<OperationalTableSkeletonRows
									columns={CHAIN_COLUMN_COUNT + 1}
									rows={6}
								/>
							) : null}
							{!showPendingRows
								? chains.map((chain) => (
										<Fragment key={chain.ingress_request_id}>
											<ChainSummaryRow
												chain={chain}
												expanded={expanded.has(chain.ingress_request_id)}
												onToggle={() => toggle(chain.ingress_request_id)}
												onSelectRow={onSelectRow}
											/>
											{expanded.has(chain.ingress_request_id) ? (
												<TableRow data-testid={`chain-${chain.ingress_request_id}`}>
													<TableCell
														colSpan={CHAIN_COLUMN_COUNT + 1}
													className="max-w-0 bg-inset p-0"
													>
														<ChainRowsPanel
															chain={chain}
															readState={chainRowReads[chain.ingress_request_id]}
															onLoadMore={() =>
																onLoadMoreRows(chain.ingress_request_id, chain.next_row_cursor!)
															}
															onSelectRow={onSelectRow}
														/>
													</TableCell>
												</TableRow>
											) : null}
										</Fragment>
									))
								: null}
						</TableBody>
					</Table>
				</div>
			)}

			<div className="flex flex-col gap-3 border-t border-border bg-inset px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
				<span
					className="text-xs text-muted-foreground"
					data-testid="chain-page-range"
				>
					{pageSummary}
				</span>
				<div className="flex items-center gap-1">
					<Button
						variant="outline"
						size="icon"
						className="size-8 rounded-full"
						aria-label={copy.previousPage}
						disabled={loading || !hasPreviousChains}
						onClick={onLoadPreviousChains}
						data-testid="chain-previous"
					>
						<ChevronLeft />
					</Button>
					<Button
						variant="outline"
						size="icon"
						className="size-8 rounded-full"
						aria-label={copy.nextPage}
						disabled={loading || !hasMoreChains}
						onClick={onLoadNextChains}
						data-testid="chain-more"
					>
						<ChevronRight />
					</Button>
				</div>
			</div>
		</div>
	);
}

/**
 * The expanded chain body: retained rows plus its own load-more lane. An
 * append failure never blanks the loaded rows — it renders as this chain's
 * local retryable error while everything above stays put.
 */
function ChainRowsPanel({
	chain,
	readState,
	onLoadMore,
	onSelectRow,
}: {
	chain: ChainIngressItem;
	readState: ChainRowReadState | undefined;
	onLoadMore: () => void;
	onSelectRow: (requestLogId: string) => void;
}) {
	const { messages } = useLocale();
	const tableCopy = messages.operationalTable;
	// More pages exist while the backend says this chain's retained rows are
	// incomplete and hands us the cursor for the next slice.
	const hasMore =
		!chain.retained_rows_page_complete && Boolean(chain.next_row_cursor);
	return (
			<div className="w-full max-w-full overflow-x-auto px-[var(--density-card-pad-x)] py-2">
				<div className="grid min-w-[72rem] grid-cols-[4rem_8rem_12rem_12rem_12rem_12rem_8rem_8rem_10rem] gap-2 border-b border-border px-2 py-1 text-[11px] font-medium text-muted-foreground">
					<span>{messages.requestLogs.attemptNumberShort}</span>
					<span>{messages.requestLogs.attemptTrigger}</span>
					<span>{messages.requestLogs.attemptTargetModel}</span>
					<span>{messages.requestLogs.terminalTarget}</span>
					<span>{messages.requestLogs.endpoint}</span>
					<span>{messages.requestLogs.chainColumnResult}</span>
					<span>{messages.requestLogs.attemptDuration}</span>
					<span>{messages.requestLogs.tokens}</span>
					<span>{messages.requestLogs.attemptKnownCost}</span>
				</div>
				<div className="flex min-w-[72rem] flex-col gap-0.5">
				{chain.retained_rows.map((row) => (
					<ChainRowButton
						key={row.request_log_id}
						currencySymbol={
							chain.finalized_summary?.report_currency_symbol ?? null
						}
						row={row}
					onSelect={() => onSelectRow(row.request_log_id)}
				/>
				))}
				</div>
			{readState?.error ? (
				<p
					role="alert"
					className="text-xs text-failing"
					data-testid={`chain-rows-error-${chain.ingress_request_id}`}
				>
					{readState.error}
				</p>
			) : null}
			{hasMore || readState?.pending ? (
				<LoadMoreControl
					testId={`chain-rows-more-${chain.ingress_request_id}`}
					pending={Boolean(readState?.pending)}
					error={readState?.error ?? null}
					hasMore={hasMore}
					labels={{
						loadMore: messages.requestLogs.chainLoadMoreRows,
						loading: tableCopy.loadingMore,
						retry: tableCopy.retryLoadMore,
					}}
					onLoadMore={onLoadMore}
				/>
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

/**
 * The chain's own request-log row: the finalized winner when the backend named
 * one, otherwise the winning retained row, otherwise the first retained row.
 * A chain with no retained row has no request log to open — that is a gap in
 * the record, not a row we may invent an id for.
 */
function chainRequestLogId(chain: ChainIngressItem): string | null {
	const finalized = chain.finalized_summary?.request_log_id;
	if (finalized) return finalized;
	const winner = chain.retained_rows.find((row) => row.is_winner === true);
	return winner?.request_log_id ?? chain.retained_rows[0]?.request_log_id ?? null;
}

function ChainSummaryRow({
	chain,
	expanded,
	onToggle,
	onSelectRow,
}: {
	chain: ChainIngressItem;
	expanded: boolean;
	onToggle: () => void;
	onSelectRow: (requestLogId: string) => void;
}) {
	const { formatNumber, messages } = useLocale();
	const { format } = useTimezone();
	const copy = messages.requestLogs;
	const observe = messages.observe;
	const summary = chain.finalized_summary;
	const tier = resultTier(summary);
	const requestLogId = chainRequestLogId(chain);
	// Finalized evidence is either authoritative or unavailable; unavailable is a
	// gap in the record, not a completed request with empty numbers.
	const evidenceMissing =
		chain.finalized_evidence_state !== "authoritative" || summary === null;
	const missingReason = evidenceMissing
		? copy.finalizedEvidenceUnavailable
		: messages.honesty.noValue;

	return (
		<TableRow
			data-testid={`chain-summary-${chain.ingress_request_id}`}
			className={cn(
				"group/row",
				requestLogId !== null && "cursor-pointer",
				operationalRowStripe(tier),
			)}
			onClick={
				requestLogId === null ? undefined : () => onSelectRow(requestLogId)
			}
		>
			<TableCell className="w-8 pr-0">
				<button
					type="button"
					onClick={(event) => {
						event.stopPropagation();
						onToggle();
					}}
					aria-expanded={expanded}
					aria-label={copy.chainToggleAria(chain.ingress_request_id)}
					className="flex size-7 items-center justify-center rounded-[4px] text-muted-foreground hover:bg-inset hover:text-foreground"
				>
					{expanded ? (
						<ChevronDown className="size-4" />
					) : (
						<ChevronRight className="size-4" />
					)}
				</button>
			</TableCell>

			<TableCell className="whitespace-nowrap font-mono tabular-nums">
				{chain.started_at ? (
					format(chain.started_at)
				) : (
					<OperatorMissingValue reason={missingReason} />
				)}
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
						<OperatorStatusBadge
							intent="idle"
							label={copy.finalizedEvidenceUnavailableShort}
							preserveLabel
						/>
					)}
					{summary?.final_error_code ? (
						<span className="font-mono text-xs text-failing">
							{summary.final_error_code}
						</span>
					) : null}
					{chain.chain_complete === false ? (
						<OperatorClippedBadge
							label={copy.chainIncomplete}
							reason={copy.chainIncompleteReason}
						/>
					) : null}
					{chain.legacy_unknown_row_count > 0 ? (
						<OperatorTypeBadge
							intent="degraded"
							label={copy.chainLegacyUnknown(
								formatNumber(chain.legacy_unknown_row_count),
							)}
							preserveLabel
						/>
					) : null}
				</div>
			</TableCell>

			<TableCell className="max-w-52 truncate">
					{summary?.ingress_model?.label ?? (
					<OperatorMissingValue reason={missingReason} />
				)}
			</TableCell>


					<TableCell className="max-w-52">
						{summary ? (
							<div className="flex min-w-0 flex-col gap-0.5">
								<span className="truncate">
									{summary.final_target_model?.label ?? (
										<OperatorMissingValue reason={missingReason} />
									)}
								</span>
								<UpstreamModelIdValue
									value={summary.final_upstream_model_id}
									missingReason={copy.upstreamModelIdMissing}
									showLabel
									className="truncate text-[11px] text-muted-foreground"
								/>
							</div>
						) : (
							<OperatorMissingValue reason={missingReason} />
						)}
					</TableCell>

				<TableCell className="max-w-52">
					{summary ? (
						<div className="flex min-w-0 flex-col gap-0.5 text-xs">
							<span className="truncate">
								{summary.terminal_target?.label ?? (
									<OperatorMissingValue reason={copy.actualTerminalTargetMissing} />
								)}
							</span>
							<span className="truncate text-muted-foreground">
								{summary.endpoint?.label ?? (
									<OperatorMissingValue reason={copy.actualEndpointMissing} />
								)}
							</span>
						</div>
					) : (
						<OperatorMissingValue reason={missingReason} />
					)}
				</TableCell>

			<TableCell className="text-right font-mono tabular-nums">
					{summary ? (
						<span className="inline-flex flex-col items-end gap-0.5">
							<span>{formatNumber(summary.attempt_count)}</span>
							{chain.chain_complete === false ? (
								<span className="text-[10px] text-degraded">
									{copy.attemptEvidenceCount(
										formatNumber(chain.retained_upstream_attempt_count),
										chain.expected_attempt_count === null
											? copy.expectedUnknown
											: formatNumber(chain.expected_attempt_count),
									)}
								</span>
							) : null}
						</span>
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
				{summary?.output_rate_state === "measured" &&
				summary.output_rate_tps != null ? (
					formatTokenRate(
						summary.output_rate_tps,
						summary.output_rate_state,
					)
				) : summary ? (
					<OperatorMissingValue
						reason={describeTokenRateMissing({
							rateTps: summary.output_rate_tps,
							state: summary.output_rate_state,
							reason: summary.output_rate_reason,
						})}
					/>
				) : (
					<OperatorMissingValue reason={missingReason} />
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
						summary.total_cost_user_currency_micros / 1_000_000
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

			{/* 行操作：点行只有鼠标能用，审计页与请求 ID 必须有键盘可达的入口。 */}
			<TableCell className="text-right">
				{requestLogId === null ? (
					<OperatorMissingValue reason={copy.chainRequestLogIdUnavailable} />
				) : (
					<div className="flex items-center justify-end gap-0.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover/row:opacity-100 [@media(hover:none)]:opacity-100">
						<Button
							type="button"
							variant="ghost"
							size="icon"
							className="size-7"
							aria-label={copy.chainOpenDetailAria(chain.ingress_request_id)}
							title={copy.requestDetails}
							onClick={(event) => {
								event.stopPropagation();
								onSelectRow(requestLogId);
							}}
							data-testid={`chain-open-detail-${chain.ingress_request_id}`}
						>
							<PanelRight />
						</Button>
						<Button
							type="button"
							variant="ghost"
							size="icon"
							className="size-7"
							aria-label={copy.chainViewAuditAria(chain.ingress_request_id)}
							title={copy.viewAudit}
							onClick={(event) => event.stopPropagation()}
							asChild
						>
							<Link
								to="/observe/requests/$requestId/audit"
								params={{ requestId: requestLogId }}
								search={{}}
								data-testid={`chain-view-audit-${chain.ingress_request_id}`}
							>
								<FileSearch />
							</Link>
						</Button>
						<Button
							type="button"
							variant="ghost"
							size="icon"
							className="size-7"
							aria-label={copy.chainCopyRequestIdAria(chain.ingress_request_id)}
							title={copy.copyRequestId}
							onClick={(event) => {
								event.stopPropagation();
								void copyRequestLogText(requestLogId, copy.requestId);
							}}
							data-testid={`chain-copy-request-id-${chain.ingress_request_id}`}
						>
							<Copy />
						</Button>
					</div>
				)}
			</TableCell>
		</TableRow>
	);
}

function pricingLabel(
	status: string,
	copy: ReturnType<typeof useLocale>["messages"]["observe"],
): string {
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

/** Row-kind enums always pass through the localized label dictionary. */
function rowKindLabel(
	kind: string,
	copy: ReturnType<typeof useLocale>["messages"]["requestLogs"],
): string {
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

function ChainRowButton({
	currencySymbol,
	row,
	onSelect,
}: {
	currencySymbol: string | null;
	row: RequestLogChainRow;
	onSelect: () => void;
}) {
	const { messages } = useLocale();
	const copy = messages.requestLogs;
	const statusCode =
		row.upstream_status_code ?? row.gateway_status_code ?? row.legacy_status_code;
	const isUpstreamAttempt = row.row_kind === "upstream";
	const targetModel =
		row.attempt_target_model_label ?? row.attempt_target_model_id;
	const endpoint =
		row.endpoint_label ??
		(row.endpoint_id === null ? null : copy.endpointId(row.endpoint_id));
	const terminalTarget =
		row.terminal_target_label ??
		(row.terminal_target_id === null
			? null
			: copy.terminalTargetId(row.terminal_target_id));
	const attemptCost =
		row.is_winner === true &&
		row.pricing_status === "priced" &&
		row.pricing_evidence_trust === "trusted" &&
		row.total_cost_user_currency_micros !== null
			? `${currencySymbol ?? "$"}${(
				row.total_cost_user_currency_micros / 1_000_000
			).toFixed(4)}`
			: null;

	return (
		<button
			type="button"
			className="grid w-full min-w-[72rem] grid-cols-[4rem_8rem_12rem_12rem_12rem_12rem_8rem_8rem_10rem] items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs hover:bg-panel"
			onClick={onSelect}
			data-testid={`chain-row-${row.request_log_id}`}
		>
			<span className="font-mono text-muted-foreground">
				{row.attempt_number === null
					? copy.diagnosticRowShort
					: `#${row.attempt_number}`}
			</span>
			<span>{attemptTriggerLabel(row.attempt_trigger, copy)}</span>
			<span className="flex min-w-0 flex-col gap-0.5">
				<span className="truncate">
					{isUpstreamAttempt && targetModel ? targetModel : (
						<OperatorMissingValue reason={copy.noUpstreamAttemptTarget} />
					)}
				</span>
				{isUpstreamAttempt ? (
					<UpstreamModelIdValue
						value={row.upstream_model_id}
						missingReason={copy.upstreamModelIdMissing}
						className="truncate text-[11px] text-muted-foreground"
					/>
				) : null}
			</span>
			<span className="truncate">
				{isUpstreamAttempt && terminalTarget ? terminalTarget : (
					<OperatorMissingValue reason={copy.noActualTerminalTarget} />
				)}
			</span>
			<span className="truncate">
				{isUpstreamAttempt && endpoint ? endpoint : (
					<OperatorMissingValue reason={copy.noActualEndpoint} />
				)}
			</span>
			<span className="flex flex-wrap items-center gap-1">
				<OperatorTypeBadge
					label={rowKindLabel(row.row_kind, copy)}
					preserveLabel
					className="text-[10px]"
				/>
				{statusCode !== null && statusCode !== undefined ? (
					<OperatorValueBadge
						label={String(statusCode)}
						intent={statusCode < 400 ? "healthy" : "failing"}
						className="text-[10px]"
					/>
				) : null}
				<span>{attemptResultLabel(row.attempt_result, copy)}</span>
				{row.is_winner === true ? (
					<OperatorValueBadge
						label={copy.winner}
						intent="healthy"
						className="text-[10px]"
					/>
				) : null}
			</span>
			<span className="font-mono tabular-nums">
				{row.attempt_duration_ms === null ? (
					<OperatorMissingValue reason={copy.attemptDurationMissing} />
				) : (
					`${row.attempt_duration_ms} ms`
				)}
			</span>
			<span className="font-mono tabular-nums">
				{row.total_tokens === null ? (
					<OperatorMissingValue reason={copy.attemptTokensMissing} />
				) : (
					row.total_tokens
				)}
			</span>
			<span className="font-mono tabular-nums">
				{attemptCost ?? (
					<OperatorMissingValue
						reason={
							row.is_winner === true
								? copy.attemptCostMissing
								: copy.failedAttemptCostUnknown
						}
					/>
				)}
			</span>
		</button>
	);
}

function attemptTriggerLabel(
	value: RequestLogChainRow["attempt_trigger"],
	copy: ReturnType<typeof useLocale>["messages"]["requestLogs"],
): string {
	switch (value) {
		case "initial": return copy.attemptTriggerInitial;
		case "retry_same_target": return copy.attemptTriggerRetrySameTarget;
		case "hedge": return copy.attemptTriggerHedge;
		case "failover": return copy.attemptTriggerFailover;
		default: return copy.attemptTriggerUnavailable;
	}
}

function attemptResultLabel(
	value: RequestLogChainRow["attempt_result"],
	copy: ReturnType<typeof useLocale>["messages"]["requestLogs"],
): string {
	switch (value) {
		case "completed": return copy.attemptResultCompleted;
		case "http_error": return copy.attemptResultHttpError;
		case "stream_error": return copy.attemptResultStreamError;
		case "transport_error": return copy.attemptResultTransportError;
		case "cancelled": return copy.attemptResultCancelled;
		case "client_disconnected": return copy.attemptResultClientDisconnected;
		default: return copy.attemptResultUnknown;
	}
}
