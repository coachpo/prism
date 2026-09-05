import { useEffect, useId, useMemo, useState } from "react";
import {
    KeyRound,
    Loader2,
    MoreHorizontal,
    Pencil,
    RotateCcw,
    Trash2,
} from "lucide-react";
import { Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { ProxyApiKey, ProxyKeyCapacity } from "@/lib/types";
import { cn } from "@/lib/utils";
import {
    OperatorClippedBadge,
    OperatorEmptyState,
    OperatorHelpHint,
    OperatorMissingValue,
    OperatorSearchInput,
    OperatorStalenessBadge,
    OperatorStatusBadge,
    OperatorTableShell,
    OperatorTypeBadge,
} from "@/shared/design-system";
import {
    OperationalTableSkeletonRows,
    SortableTableHead,
    operationalRowActionsClassName,
    operationalRowStripe,
} from "@/shared/table/operationalTable";
import { OperationalTablePagination } from "@/shared/table/paginationControls";
import {
    getNextOperationalSort,
    paginateOperationalRows,
    sortOperationalRows,
    type OperationalSortState,
    type OperationalSortValue,
} from "@/shared/table/operationalTableState";
import type { ProxyKeyUsageEntry } from "@/features/proxy-keys/useProxyKeyUsage";
import {
    formatDateTime,
    getProxyKeyLifecycleIntent,
    getProxyKeyLifecycleLabel,
    getProxyKeyUsagePercent,
    isProxyKeyExpired,
} from "./proxyKeyFormatting";

const LEDGER_PAGE_SIZES = [10, 25, 50] as const;
const LEDGER_COLUMN_COUNT = 7;

/**
 * The 7-day count is deliberately not sortable. Its read is scoped to the rows
 * currently on screen, so sorting by it would make the visible set depend on
 * data that is fetched from the visible set — and would reorder rows under the
 * operator as counts trickle in.
 */
type LedgerSortColumn =
    | "name"
    | "lifecycle"
    | "lastUsed"
    | "rotation"
    | "created";

interface ProxyKeyLedgerCardProps {
    authEnabled: boolean;
    capacity: ProxyKeyCapacity | null;
    deletingProxyKeyId: number | null;
    displayedProxyKeys: ProxyApiKey[];
    loading: boolean;
    onDelete: (item: ProxyApiKey) => void;
    onEdit: (item: ProxyApiKey) => void;
    onIssue: () => void;
    onRotate: (item: ProxyApiKey) => void;
    onRetryUsage: () => void;
    onVisibleKeysChange: (keyIds: number[]) => void;
    rotatingProxyKeyId: number | null;
    usage: Map<number, ProxyKeyUsageEntry>;
    usageFailed: boolean;
}

function matchesQuery(item: ProxyApiKey, query: string) {
    const normalized = query.trim().toLowerCase();
    if (!normalized) return true;
    return [item.name, item.key_preview, item.key_prefix, item.notes]
        .filter((value): value is string => Boolean(value))
        .some((value) => value.toLowerCase().includes(normalized));
}

function getSortValue(
    item: ProxyApiKey,
    column: LedgerSortColumn,
    authEnabled: boolean,
): OperationalSortValue {
    if (column === "name") return item.name;
    if (column === "lifecycle")
        return getProxyKeyLifecycleLabel(item, authEnabled);
    if (column === "lastUsed") return item.last_used_at;
    if (column === "rotation") return item.rotated_at;
    return item.created_at;
}

/** The count owns its own read, so it also owns its own four honesty states. */
function ProxyKeyUsageCell({
    entry,
    item,
}: {
    entry: ProxyKeyUsageEntry | undefined;
    item: ProxyApiKey;
}) {
    const { formatNumber, messages } = useLocale();
    const copy = messages.proxyApiKeys;

    // No cached value yet: a skeleton shaped like the number, never a zero.
    if (!entry || entry.loading) {
        return <Skeleton className="ml-auto h-4 w-10" aria-busy="true" />;
    }

    if (entry.failed) {
        return (
            <span
                className="font-mono text-xs font-medium text-failing"
                title={copy.requests7dFailedReason}
            >
                {copy.requests7dFailed}
            </span>
        );
    }

    if (entry.total === null) {
        return <OperatorMissingValue reason={copy.requests7dFailedReason} />;
    }

    // The window can outlive the credential: a key retired or expired inside the
    // last 7 days still contributes the requests it served while valid.
    const clipped = isProxyKeyExpired(item.expires_at) || !item.is_active;
    const coverageIncomplete = entry.coverageComplete === false;

    return (
        <div className="flex flex-col items-end gap-1">
            <Link
                to="/observe/requests"
                search={{ proxy_api_key_id: String(item.id), time_range: "7d" }}
                aria-label={copy.requests7dLinkAria(item.name)}
                className="font-mono tabular-nums text-foreground underline-offset-2 hover:underline"
            >
                {/* A background re-read keeps the cached number on screen and names
            itself, instead of blanking to a skeleton. */}
                <span className="inline-flex items-center gap-1">
                    {formatNumber(entry.total)}
                    {entry.refreshing ? (
                        <Loader2
                            aria-hidden="true"
                            data-testid="proxy-key-usage-refreshing"
                            className="size-3 animate-spin text-muted-foreground"
                        />
                    ) : null}
                </span>
                <span className="sr-only" role="status">
                    {entry.refreshing ? copy.requests7dRefreshing : ""}
                </span>
            </Link>
            {coverageIncomplete ? (
                <OperatorClippedBadge
                    label={messages.honesty.coverageIncomplete}
                    reason={messages.honesty.coverageIncompleteReason}
                />
            ) : null}
            {clipped ? (
                <OperatorClippedBadge
                    label={copy.requests7dClipped}
                    reason={copy.requests7dClippedReason}
                />
            ) : null}
        </div>
    );
}

function ProxyKeyLedgerRow({
    authEnabled,
    deleting,
    item,
    onDelete,
    onEdit,
    onRotate,
    rotating,
    usageEntry,
}: {
    authEnabled: boolean;
    deleting: boolean;
    item: ProxyApiKey;
    onDelete: () => void;
    onEdit: () => void;
    onRotate: () => void;
    rotating: boolean;
    usageEntry: ProxyKeyUsageEntry | undefined;
}) {
    const { formatNumber, formatRelativeTimeFromNow, messages } = useLocale();
    const copy = messages.proxyApiKeys;
    // 生命周期是配置分类，只有「已过期」保留运行态语气——行左状态条同理。
    const lifecycleIntent = getProxyKeyLifecycleIntent(item, authEnabled);
    const LifecycleBadge =
        lifecycleIntent === "failing" ? OperatorStatusBadge : OperatorTypeBadge;
    const note = item.notes?.trim();
    const expired = isProxyKeyExpired(item.expires_at);
    const busy = rotating || deleting;

    const expiryLine = item.expires_at
        ? expired
            ? copy.expiredAtTime(formatDateTime(item.expires_at))
            : copy.expiresRelative(formatRelativeTimeFromNow(item.expires_at))
        : copy.neverExpires;

    return (
        <TableRow
            className={cn(
                "group/row",
                operationalRowStripe(
                    lifecycleIntent === "failing" ? "failing" : null,
                ),
            )}
        >
            <TableCell className="align-top">
                {/* 一行标题 + 一行标识：备注与预览同行省略，紧凑密度下
                    单元格不超过两行。 */}
                <div className="flex min-w-0 flex-col gap-0.5">
                    <span className="truncate font-medium" title={item.name}>
                        {item.name}
                    </span>
                    <span className="flex min-w-0 items-center gap-1 text-xs text-muted-foreground">
                        <span
                            className="shrink-0 font-mono"
                            title={item.key_preview}
                        >
                            {item.key_preview}
                        </span>
                        {note ? (
                            <>
                                <span aria-hidden="true">·</span>
                                <span className="truncate" title={note}>
                                    {note}
                                </span>
                            </>
                        ) : null}
                    </span>
                </div>
            </TableCell>

            <TableCell className="align-top">
                <div className="flex flex-col items-start gap-1">
                    <LifecycleBadge
                        intent={lifecycleIntent}
                        label={getProxyKeyLifecycleLabel(item, authEnabled)}
                        preserveLabel
                    />
                    <span
                        className="text-xs text-muted-foreground"
                        title={
                            item.expires_at
                                ? formatDateTime(item.expires_at)
                                : undefined
                        }
                    >
                        {expiryLine}
                    </span>
                </div>
            </TableCell>

            <TableCell className="align-top text-right">
                <ProxyKeyUsageCell entry={usageEntry} item={item} />
            </TableCell>

            <TableCell className="align-top">
                <div className="flex min-w-0 flex-col gap-0.5">
                    {item.last_used_at ? (
                        <span className="font-mono text-xs tabular-nums">
                            {formatDateTime(item.last_used_at)}
                        </span>
                    ) : (
                        // 「从未使用」是已知事实，与读不到值是两回事：删除对话框
                        // 里写的也是「从未」，两处必须是同一句话。
                        <span
                            className="text-xs text-muted-foreground"
                            title={copy.lastUsedNeverReason}
                        >
                            {copy.never}
                        </span>
                    )}
                    {item.last_used_ip ? (
                        <span className="truncate font-mono text-xs text-muted-foreground">
                            {item.last_used_ip}
                        </span>
                    ) : (
                        <OperatorMissingValue
                            className="text-xs"
                            reason={copy.lastIpMissingReason}
                        />
                    )}
                </div>
            </TableCell>

            <TableCell className="align-top">
                {item.rotation_count > 0 && item.rotated_at ? (
                    <div className="flex min-w-0 flex-col gap-0.5">
                        <span className="font-mono text-xs tabular-nums">
                            {formatNumber(item.rotation_count)}
                        </span>
                        <span className="truncate font-mono text-xs text-muted-foreground">
                            {formatDateTime(item.rotated_at)}
                        </span>
                    </div>
                ) : (
                    <OperatorMissingValue
                        className="text-xs"
                        reason={copy.rotatedAtMissingReason}
                    />
                )}
            </TableCell>

            <TableCell className="align-top">
                {/* 两个时间戳常常只差几分钟，不带标签就分不出哪个是最后修改。 */}
                <div className="flex min-w-0 flex-col gap-0.5 text-xs text-muted-foreground">
                    <span>
                        <span className="text-[11px]">{copy.created}</span>{" "}
                        <span className="font-mono tabular-nums">
                            {formatDateTime(item.created_at)}
                        </span>
                    </span>
                    <span>
                        <span className="text-[11px]">{copy.updated}</span>{" "}
                        <span className="font-mono tabular-nums">
                            {formatDateTime(item.updated_at)}
                        </span>
                    </span>
                </div>
            </TableCell>

            <TableCell className="align-top text-right">
                <div className={cn(operationalRowActionsClassName, "gap-1")}>
                    <Button asChild variant="outline" size="sm" disabled={busy}>
                        <Link
                            to="/observe/requests"
                            search={{
                                proxy_api_key_id: String(item.id),
                                time_range: "7d",
                            }}
                            aria-label={copy.viewRequestsAria(item.name)}
                        >
                            {copy.viewRequests}
                        </Link>
                    </Button>
                    <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                            <Button
                                type="button"
                                variant="outline"
                                size="icon-sm"
                                disabled={busy}
                                aria-label={copy.moreActionsAria(item.name)}
                            >
                                <MoreHorizontal />
                            </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                            <DropdownMenuItem onSelect={onEdit}>
                                <Pencil />
                                {messages.common.edit}
                            </DropdownMenuItem>
                            <DropdownMenuItem onSelect={onRotate}>
                                <RotateCcw
                                    className={cn(rotating && "animate-spin")}
                                />
                                {copy.rotateAction}
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                                variant="destructive"
                                onSelect={onDelete}
                            >
                                <Trash2 />
                                {copy.deleteKey}
                            </DropdownMenuItem>
                        </DropdownMenuContent>
                    </DropdownMenu>
                </div>
            </TableCell>
        </TableRow>
    );
}

export function ProxyKeyLedgerCard({
    authEnabled,
    capacity,
    deletingProxyKeyId,
    displayedProxyKeys,
    loading,
    onDelete,
    onEdit,
    onIssue,
    onRetryUsage,
    onRotate,
    onVisibleKeysChange,
    rotatingProxyKeyId,
    usage,
    usageFailed,
}: ProxyKeyLedgerCardProps) {
    const { formatNumber, locale, messages } = useLocale();
    const copy = messages.proxyApiKeys;
    const tableCopy = messages.operationalTable;
    const [query, setQuery] = useState("");
    const [sort, setSort] = useState<OperationalSortState<LedgerSortColumn>>({
        column: "created",
        direction: "desc",
    });
    const [pageIndex, setPageIndex] = useState(0);
    const [pageSize, setPageSize] = useState<number>(LEDGER_PAGE_SIZES[0]);
    // columnheader 的名字只能是列名。名字若由内容计算，帮助按钮的 aria-label
    // （口径全文）会被并进列名，扫表时每一列都要先听完 32 个汉字。
    const requests7dNameId = useId();
    const requests7dBasisId = useId();

    const filtered = useMemo(
        () => displayedProxyKeys.filter((item) => matchesQuery(item, query)),
        [displayedProxyKeys, query],
    );
    const sorted = useMemo(
        () =>
            sortOperationalRows(
                filtered,
                sort,
                (item, column) => getSortValue(item, column, authEnabled),
                locale,
            ),
        [authEnabled, filtered, locale, sort],
    );
    const page = paginateOperationalRows(sorted, pageIndex, pageSize);
    const visibleKeySignature = page.pageRows.map((item) => item.id).join(",");

    // The usage column reads only what is on screen; the feature hook owns the
    // query set so this stays a presentation component.
    useEffect(() => {
        onVisibleKeysChange(
            visibleKeySignature
                ? visibleKeySignature.split(",").map(Number)
                : [],
        );
    }, [onVisibleKeysChange, visibleKeySignature]);

    const updateSort = (column: LedgerSortColumn) => {
        setSort((current) => getNextOperationalSort(current, column));
        setPageIndex(0);
    };

    const quotaPercent = capacity
        ? getProxyKeyUsagePercent(capacity.used, capacity.limit)
        : 0;

    const summary = (
        <>
            <span>
                {copy.ledgerSummary(formatNumber(displayedProxyKeys.length))}
            </span>
            <span aria-hidden="true">·</span>
            {capacity ? (
                <>
                    <span className="font-mono tabular-nums">
                        {copy.capacitySnapshot(
                            formatNumber(capacity.used),
                            formatNumber(capacity.limit),
                            formatNumber(capacity.remaining),
                        )}
                    </span>
                    <Progress
                        value={quotaPercent}
                        aria-label={copy.capacityQuotaAria(
                            formatNumber(capacity.used),
                            formatNumber(capacity.limit),
                        )}
                        className="h-1.5 w-24"
                    />
                    <span aria-hidden="true">·</span>
                    <span className="font-mono tabular-nums">
                        {copy.capacityCountedAt(
                            formatDateTime(capacity.counted_at),
                        )}
                    </span>
                </>
            ) : (
                <span
                    className="text-degraded"
                    title={copy.createBlockedCapacityUnknown}
                >
                    {copy.capacityUnavailable}
                </span>
            )}
            {usageFailed ? (
                <>
                    <span aria-hidden="true">·</span>
                    <Button
                        type="button"
                        variant="outline"
                        size="xs"
                        onClick={onRetryUsage}
                    >
                        {copy.requests7dRetry}
                    </Button>
                </>
            ) : null}
        </>
    );

    return (
        <OperatorTableShell
            data-testid="proxy-key-ledger"
            summary={summary}
            actions={
                <OperatorSearchInput
                    aria-label={copy.searchAria}
                    name="proxy_keys_search"
                    autoComplete="off"
                    placeholder={copy.searchPlaceholder}
                    value={query}
                    onChange={(event) => {
                        setQuery(event.target.value);
                        setPageIndex(0);
                    }}
                    wrapperClassName="sm:w-64"
                    className="h-[var(--density-control-h-sm)]"
                />
            }
        >
            <div className="overflow-x-auto">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <SortableTableHead
                                sortKey="name"
                                sort={sort}
                                onSort={updateSort}
                            >
                                {copy.columnIdentity}
                            </SortableTableHead>
                            <SortableTableHead
                                sortKey="lifecycle"
                                sort={sort}
                                onSort={updateSort}
                            >
                                {copy.columnLifecycle}
                            </SortableTableHead>
                            <TableHead
                                className="text-right"
                                aria-labelledby={requests7dNameId}
                                aria-describedby={requests7dBasisId}
                            >
                                {/* 这一列的时间窗与全表其余列不同，口径最需要说明：
                                    交给可聚焦的 OperatorHelpHint，并用
                                    aria-describedby 关联，而不是塞进列名或只挂
                                    title。 */}
                                <span className="inline-flex flex-wrap items-center justify-end gap-1">
                                    {copy.columnRequests7d}
                                    <OperatorHelpHint
                                        align="end"
                                        label={copy.columnRequests7dBasis}
                                    />
                                    {usageFailed ? (
                                        <OperatorStalenessBadge
                                            label={copy.requests7dFailedBadge}
                                            reason={copy.requests7dFailedReason}
                                        />
                                    ) : null}
                                </span>
                                <span id={requests7dNameId} className="sr-only">
                                    {copy.columnRequests7d}
                                </span>
                                <span id={requests7dBasisId} className="sr-only">
                                    {copy.columnRequests7dBasis}
                                </span>
                            </TableHead>
                            <SortableTableHead
                                sortKey="lastUsed"
                                sort={sort}
                                onSort={updateSort}
                            >
                                {copy.columnLastActivity}
                            </SortableTableHead>
                            <SortableTableHead
                                sortKey="rotation"
                                sort={sort}
                                onSort={updateSort}
                            >
                                {copy.rotation}
                            </SortableTableHead>
                            <SortableTableHead
                                sortKey="created"
                                sort={sort}
                                onSort={updateSort}
                            >
                                {copy.columnLedgerMeta}
                            </SortableTableHead>
                            <TableHead className="text-right">
                                {copy.operation}
                            </TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {loading ? (
                            <OperationalTableSkeletonRows
                                columns={LEDGER_COLUMN_COUNT}
                                rows={5}
                            />
                        ) : null}

                        {!loading
                            ? page.pageRows.map((item) => (
                                  <ProxyKeyLedgerRow
                                      key={item.id}
                                      authEnabled={authEnabled}
                                      deleting={deletingProxyKeyId === item.id}
                                      item={item}
                                      onDelete={() => onDelete(item)}
                                      onEdit={() => onEdit(item)}
                                      onRotate={() => onRotate(item)}
                                      rotating={rotatingProxyKeyId === item.id}
                                      usageEntry={usage.get(item.id)}
                                  />
                              ))
                            : null}
                    </TableBody>
                </Table>
            </div>

            {/* Kept outside the scroll container so it stays aligned to the viewport. */}
            {!loading && displayedProxyKeys.length === 0 ? (
                <div className="p-[var(--density-card-pad-x)]">
                    <OperatorEmptyState
                        icon={<KeyRound />}
                        title={copy.noProxyKeysCreated}
                        description={copy.noProxyKeysDescription}
                        action={
                            <Button onClick={onIssue}>
                                {copy.issueFirstKey}
                            </Button>
                        }
                    />
                </div>
            ) : null}

            {!loading &&
            displayedProxyKeys.length > 0 &&
            sorted.length === 0 ? (
                <div className="p-[var(--density-card-pad-x)]">
                    <OperatorEmptyState
                        title={copy.noKeysMatchFilters}
                        description={copy.noKeysMatchFiltersDescription}
                        action={
                            <Button
                                variant="outline"
                                onClick={() => setQuery("")}
                            >
                                {copy.clearSearch}
                            </Button>
                        }
                    />
                </div>
            ) : null}

            {!loading && sorted.length > 0 ? (
                <OperationalTablePagination
                    currentPageIndex={page.currentPageIndex}
                    endIndex={page.endIndex}
                    formatNumber={(value) => formatNumber(value)}
                    hasNextPage={page.hasNextPage}
                    hasPreviousPage={page.hasPreviousPage}
                    nextLabel={tableCopy.nextPage}
                    onGoToPage={setPageIndex}
                    onNextPage={() => setPageIndex(page.currentPageIndex + 1)}
                    onPreviousPage={() =>
                        setPageIndex(page.currentPageIndex - 1)
                    }
                    pageCount={page.totalPages}
                    pageLabel={tableCopy.page}
                    pageSize={{
                        ariaLabel: tableCopy.pageSize,
                        onChange: (value) => {
                            setPageSize(value);
                            setPageIndex(0);
                        },
                        options: LEDGER_PAGE_SIZES,
                        value: pageSize,
                    }}
                    previousLabel={tableCopy.previousPage}
                    resultsLabel={tableCopy.resultsRange}
                    startIndex={page.startIndex}
                    totalLabel={tableCopy.totalRows}
                    totalRows={sorted.length}
                    zeroLabel={tableCopy.zeroResults}
                />
            ) : null}
        </OperatorTableShell>
    );
}
