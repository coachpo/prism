import { Fragment, useId, useMemo, useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import {
	ChevronDown,
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
import { cn, truncateIdentifier } from "@/lib/utils";
import {
	OperatorClippedBadge,
	OperatorEmptyState,
	OperatorHelpHint,
	OperatorMissingValue,
	OperatorStatusBadge,
	OperatorTypeBadge,
	OperatorValueBadge,
	type OperatorStatusTier,
} from "@/shared/design-system";
import { copyRequestLogText } from "./detail/requestLogClipboard";
import {
	OperationalTableSkeletonRows,
	SortableTableHead,
	operationalRowActionsClassName,
	operationalRowStripe,
} from "@/shared/table/operationalTable";
import type { OperationalSortState } from "@/shared/table/operationalTableState";
import {
	LoadMoreControl,
	OperationalTablePagination,
	PaginationLiveStatus,
} from "@/shared/table/paginationControls";
import { CHAIN_PAGE_SIZE_OPTIONS } from "./queryParams";
import type {
	ChainIngressItem,
	FinalizedSummary,
	RequestLogChainRow,
} from "@/lib/types/request-logs";
import {
	describeTokenRateMissing,
	formatDurationMs,
	formatTokenRate,
} from "./requestLogMetricPresentation";
import type { ChainRowReadState } from "./useRequestLogIngressChains";
import { UpstreamModelIdValue } from "./UpstreamModelIdValue";

/** 后端只接受 created_at 排序（其它值 422），所以链视图只有时间这一列可排。 */
type ChainSortColumn = "time";

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
	visibleColumns,
	pageSize,
	onPageSizeChange,
	sortOrder,
	onSortOrderChange,
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
	/** 列选择器给出的可见列；表头与数据格共用同一份，勾掉必有视觉变化。 */
	visibleColumns: string[];
	pageSize: number;
	onPageSizeChange: (value: number) => void;
	sortOrder: "asc" | "desc";
	onSortOrderChange: (order: "asc" | "desc") => void;
}) {
	const { formatNumber, messages } = useLocale();
	const copy = messages.requestLogs;
	const tableCopy = messages.operationalTable;
	const headingId = useId();
	const [expanded, setExpanded] = useState<Set<string>>(new Set());
	const visible = useMemo(() => new Set(visibleColumns), [visibleColumns]);
	// 展开箭头与行操作永远在，不参与列显示偏好。
	const renderedColumnCount = visible.size + 2;
	const sort: OperationalSortState<ChainSortColumn> = {
		column: "time",
		direction: sortOrder,
	};
	const showPendingRows = replacing && chains.length > 0;
	const chainPageEnd =
		total > 0 && chainPageStart !== null
			? Math.min(chainPageStart + chainPageCounts.ingress, total)
			: 0;
	const toggle = (ingressRequestId: string) => {
		setExpanded((current) => {
			const next = new Set(current);
			if (next.has(ingressRequestId)) next.delete(ingressRequestId);
			else next.add(ingressRequestId);
			return next;
		});
	};

	return (
		// 结果区块要有一个真 h2 命名，否则读屏按 H 键从页标题直接跳过整张表。
		<section
			aria-labelledby={headingId}
			className="operator-table-shell overflow-hidden rounded-lg border border-border bg-panel"
			data-testid="ingress-chains-table"
		>
			<PaginationLiveStatus
				message={showPendingRows ? tableCopy.loadingTargetPage : null}
			/>
			<div className="flex items-center justify-between gap-2 border-b border-border bg-inset px-[var(--density-card-pad-x)] py-2 text-xs">
				<div className="flex items-center gap-1">
					<h2
						id={headingId}
						className="text-xs font-medium text-foreground"
					>
						{copy.viewIngressChains}
					</h2>
					{/* 只有时间一列可排是后端的口径限制，不写出来的话
					    「其它列点不动」看起来像坏了。 */}
					<OperatorHelpHint
						label={copy.chainSortBasis}
						className="size-6"
					/>
				</div>
				<span
					className="text-muted-foreground"
					data-testid="chain-page-counts"
				>
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
				/* sticky 表头只黏在最近的滚动容器上，而 Table 原语自己那层
				   overflow-x-auto 就是包含块：高度上限必须落在它身上，
				   加在外面这层不会让表头黏住。 */
				<div aria-busy={showPendingRows}>
					<Table
						aria-label={copy.viewIngressChains}
						scrollAreaClassName="max-h-[calc(100dvh-20rem)]"
					>
						<TableHeader>
							<TableRow>
								<TableHead className="w-8" />
								{visible.has("time") ? (
									<SortableTableHead
										sortKey="time"
										sort={sort}
										onSort={() =>
											onSortOrderChange(sortOrder === "desc" ? "asc" : "desc")
										}
									>
										{copy.chainColumnTime}
									</SortableTableHead>
								) : null}
								{visible.has("result") ? (
									<TableHead>{copy.chainColumnResult}</TableHead>
								) : null}
								{visible.has("requested_model") ? (
									<TableHead>{copy.chainColumnRequestedModel}</TableHead>
								) : null}
								{visible.has("final_target") ? (
									<TableHead>{copy.chainColumnFinalTarget}</TableHead>
								) : null}
								{visible.has("endpoint") ? (
									<TableHead>{copy.chainColumnEndpoint}</TableHead>
								) : null}
								{visible.has("attempts") ? (
									<TableHead className="text-right">{copy.chainColumnAttempts}</TableHead>
								) : null}
								{visible.has("ttft") ? (
									<TableHead className="text-right">{copy.ttft}</TableHead>
								) : null}
								{visible.has("token_rate") ? (
									<TableHead className="text-right">{copy.tokenRate}</TableHead>
								) : null}
								{visible.has("tokens") ? (
									<TableHead className="text-right">{copy.chainColumnTokens}</TableHead>
								) : null}
								{visible.has("cost") ? (
									<TableHead className="text-right">{copy.chainColumnCost}</TableHead>
								) : null}
								{visible.has("pricing") ? (
									<TableHead>{copy.chainColumnPricing}</TableHead>
								) : null}
								<TableHead className="text-right">
									{copy.chainRowActions}
								</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{showPendingRows || (loading && chains.length === 0) ? (
								<OperationalTableSkeletonRows
									columns={renderedColumnCount}
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
												visible={visible}
											/>
											{expanded.has(chain.ingress_request_id) ? (
												<TableRow data-testid={`chain-${chain.ingress_request_id}`}>
													<TableCell
														colSpan={renderedColumnCount}
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

			{/* 分页行走共享实现：「共 N 条」在左、页控件与页大小在右，
			    与 models / pricing / 代理密钥三页同一套几何。 */}
			<OperationalTablePagination
				currentPageIndex={
					chainPageStart === null ? 0 : Math.floor(chainPageStart / pageSize)
				}
				startIndex={chainPageStart ?? 0}
				endIndex={chainPageEnd}
				totalRows={total}
				formatNumber={formatNumber}
				hasPreviousPage={!loading && hasPreviousChains}
				hasNextPage={!loading && hasMoreChains}
				onPreviousPage={onLoadPreviousChains}
				onNextPage={onLoadNextChains}
				previousLabel={tableCopy.previousPage}
				nextLabel={tableCopy.nextPage}
				resultsLabel={tableCopy.resultsRange}
				totalLabel={tableCopy.totalRows}
				zeroLabel={tableCopy.zeroResults}
				pageSize={{
					ariaLabel: tableCopy.pageSize,
					onChange: onPageSizeChange,
					options: CHAIN_PAGE_SIZE_OPTIONS,
					value: pageSize,
				}}
			/>
		</section>
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

/**
 * 终端目标标签常带出口前缀（「B.ai / GLM 5.3 Flash」），副行再写一遍端点名
 * （「B.AI」）等于把同一件事渲染两次。前缀相同就只留主行。
 */
function endpointRepeatsTerminalTarget(
	terminalTargetLabel: string | null | undefined,
	endpointLabel: string | null | undefined,
): boolean {
	if (!terminalTargetLabel || !endpointLabel) return false;
	const prefix = terminalTargetLabel.split("/")[0].trim().toLowerCase();
	return prefix === endpointLabel.trim().toLowerCase();
}

function ChainSummaryRow({
	chain,
	expanded,
	onToggle,
	onSelectRow,
	visible,
}: {
	chain: ChainIngressItem;
	expanded: boolean;
	onToggle: () => void;
	onSelectRow: (requestLogId: string) => void;
	visible: ReadonlySet<string>;
}) {
	const { formatNumber, messages } = useLocale();
	const { format } = useTimezone();
	const copy = messages.requestLogs;
	const observe = messages.observe;
	const summary = chain.finalized_summary;
	const tier = resultTier(summary);
	const requestLogId = chainRequestLogId(chain);
	// 一行里把同一个模型 ID 渲染四次，吃掉 45% 表宽，把「定价状态」挤出屏幕。
	// 完全同名时只留一个短标记，省出的宽度还给真正携带信息的列。
	const finalTargetSameAsIngress =
		summary !== null &&
		summary.final_target_model?.label === summary.ingress_model?.label &&
		summary.final_upstream_model_id === (summary.ingress_model?.id ?? null);
	const upstreamIdRepeatsLabel =
		summary !== null &&
		summary.final_upstream_model_id !== null &&
		summary.final_upstream_model_id === summary.final_target_model?.label;
	const endpointRepeats =
		summary !== null &&
		endpointRepeatsTerminalTarget(
			summary.terminal_target?.label,
			summary.endpoint?.label,
		);
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

			{visible.has("time") ? (
				<TableCell className="whitespace-nowrap font-mono tabular-nums">
					{chain.started_at ? (
						format(chain.started_at)
					) : (
						<OperatorMissingValue reason={missingReason} />
					)}
				</TableCell>
			) : null}

			{visible.has("result") ? (
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
			) : null}

			{visible.has("requested_model") ? (
				<TableCell className="max-w-52 truncate">
					{summary?.ingress_model?.label ?? (
						<OperatorMissingValue reason={missingReason} />
					)}
				</TableCell>
			) : null}

			{visible.has("final_target") ? (
				<TableCell className="max-w-52">
					{summary === null ? (
						<OperatorMissingValue reason={missingReason} />
					) : finalTargetSameAsIngress ? (
						// 与入口完全同名：短标记代替第二、第三次渲染同一个 ID。
						<OperatorTypeBadge
							label={copy.chainSameAsIngress}
							preserveLabel
						/>
					) : (
						<div className="flex min-w-0 flex-col gap-0.5">
							<span className="truncate" title={summary.final_target_model?.label}>
								{summary.final_target_model?.label ?? (
									<OperatorMissingValue reason={missingReason} />
								)}
							</span>
							{upstreamIdRepeatsLabel ? null : (
								<UpstreamModelIdValue
									value={summary.final_upstream_model_id}
									missingReason={copy.upstreamModelIdMissing}
									elide
									showLabel
									copyable
									className="text-[11px] text-muted-foreground"
								/>
							)}
						</div>
					)}
				</TableCell>
			) : null}

			{visible.has("endpoint") ? (
				<TableCell className="max-w-52">
					{summary ? (
						<div className="flex min-w-0 flex-col gap-0.5 text-xs">
							<span
								className="truncate"
								title={summary.terminal_target?.label ?? undefined}
							>
								{summary.terminal_target?.label ? (
									truncateIdentifier(summary.terminal_target.label, 16, 8)
								) : (
									<OperatorMissingValue reason={copy.actualTerminalTargetMissing} />
								)}
							</span>
							{endpointRepeats ? null : (
								<span
									className="truncate text-muted-foreground"
									title={summary.endpoint?.label ?? undefined}
								>
									{summary.endpoint?.label ? (
										truncateIdentifier(summary.endpoint.label, 16, 8)
									) : (
										<OperatorMissingValue reason={copy.actualEndpointMissing} />
									)}
								</span>
							)}
						</div>
					) : (
						<OperatorMissingValue reason={missingReason} />
					)}
				</TableCell>
			) : null}

			{visible.has("attempts") ? (
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
			) : null}

			{visible.has("ttft") ? (
			<TableCell className="text-right font-mono tabular-nums">
				{summary?.ttft_ms == null ? (
					<OperatorMissingValue reason={missingReason} />
				) : (
					formatDurationMs(summary.ttft_ms)
				)}
			</TableCell>
			) : null}

			{visible.has("token_rate") ? (
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
			) : null}

			{visible.has("tokens") ? (
			<TableCell className="text-right font-mono tabular-nums">
				{summary?.total_tokens == null ? (
					<OperatorMissingValue reason={missingReason} />
				) : (
					formatNumber(summary.total_tokens)
				)}
			</TableCell>
			) : null}

			{visible.has("cost") ? (
			<TableCell className="text-right font-mono tabular-nums">
				{summary?.total_cost_user_currency_micros == null ? (
					<OperatorMissingValue reason={missingReason} />
				) : (
					`${summary.report_currency_symbol ?? "$"}${(
						summary.total_cost_user_currency_micros / 1_000_000
					).toFixed(4)}`
				)}
			</TableCell>
			) : null}

			{visible.has("pricing") ? (
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
			) : null}

			{/* 行操作：点行只有鼠标能用，审计页与请求 ID 必须有键盘可达的入口。 */}
			<TableCell className="text-right">
				{requestLogId === null ? (
					<OperatorMissingValue reason={copy.chainRequestLogIdUnavailable} />
				) : (
					<div className={operationalRowActionsClassName}>
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
									elide
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
					formatDurationMs(row.attempt_duration_ms)
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
