import { useState } from "react";
import { Link } from "@tanstack/react-router";
import {
    ChevronDown,
    ChevronUp,
    Loader2,
    Plus,
    RefreshCw,
    Star,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table";
import { OperationalTableSkeletonRows } from "@/shared/table/operationalTable";
import { LoadMoreControl } from "@/shared/table/paginationControls";
import {
    banBadges,
    failureStatusCodeSummary,
    retryBadges,
} from "./strategyValueBadges";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import type { LoadbalanceStrategy } from "@/lib/types";
import type { SetDefaultState } from "@/features/loadbalance/useBanPolicyMutations";
import type { StrategyImpactState } from "@/features/loadbalance/useStrategyImpactPager";
import type { FragmentState } from "@/features/loadbalance/strategyFragmentState";
import { BAN_POLICY_PRESETS } from "@/features/loadbalance/banPolicySchemas";
import {
    OperatorCallout,
    OperatorEmptyState,
    OperatorErrorState,
    OperatorHelpHint,
    OperatorLoadingState,
    OperatorTableShell,
    OperatorTypeBadge,
    OperatorValueBadge,
} from "@/shared/design-system";

interface LoadbalanceStrategiesTableProps {
    fragment: FragmentState<LoadbalanceStrategy[]>;
    defaultsCompleteness: {
        complete: boolean;
        missing: string[];
        existingCount: number;
    };
    defaultsCreating: boolean;
    defaultsError: string | null;
    preparingEditId: number | null;
    setDefaultState: Record<number, SetDefaultState>;
    impactStates: Record<number, StrategyImpactState>;
    onCreateDefaults: () => void;
    onEdit: (strategy: LoadbalanceStrategy) => void;
    onDelete: (strategy: LoadbalanceStrategy) => void;
    onSetDefault: (strategyId: number) => void;
    onClearSetDefaultError: (strategyId: number) => void;
    onToggleImpact: (strategyId: number) => void;
    onLoadMoreImpact: (strategyId: number) => void;
    onRetryImpact: (strategyId: number) => void;
    onRetry: () => void;
    onSelect: (strategy: LoadbalanceStrategy) => void;
    selectedId: number | null;
}

export function LoadbalanceStrategiesTable({
    fragment,
    defaultsCompleteness,
    defaultsCreating,
    defaultsError,
    preparingEditId,
    setDefaultState,
    impactStates,
    onCreateDefaults,
    onEdit,
    onDelete,
    onSetDefault,
    onClearSetDefaultError,
    onToggleImpact,
    onLoadMoreImpact,
    onRetryImpact,
    onRetry,
    onSelect,
    selectedId,
}: LoadbalanceStrategiesTableProps) {
    const { formatNumber, messages } = useLocale();
    const copy = messages.routingStrategyTable;
    const strategyCopy = messages.loadbalanceStrategyCopy;
    const [sortColumn, setSortColumn] = useState<"name" | "type" | "default">(
        "default",
    );

    const strategies = fragment.data ?? [];
    const conflicts = Object.entries(setDefaultState).filter(([, state]) =>
        Boolean(state?.error),
    );
    const sorted = [...strategies].sort((left, right) => {
        switch (sortColumn) {
            case "name":
                return left.name.localeCompare(right.name);
            case "type":
                return (
                    left.legacy_strategy_type.localeCompare(
                        right.legacy_strategy_type,
                    ) || left.id - right.id
                );
            default:
                if (left.is_default !== right.is_default)
                    return left.is_default ? -1 : 1;
                return left.id - right.id;
        }
    });

    // Card headers carry a state summary, not the page title.
    const defaultStrategy =
        strategies.find((strategy) => strategy.is_default) ?? null;
    const banEnabledCount = strategies.filter(
        (strategy) => strategy.ban_mode !== "off",
    ).length;
    const strategySummary =
        strategies.length > 0 ? (
            <>
                <span>
                    {copy.tableSummary(
                        formatNumber(strategies.length),
                        formatNumber(banEnabledCount),
                    )}
                </span>
                <span aria-hidden="true">·</span>
                <span>
                    {defaultStrategy
                        ? copy.tableSummaryDefault(defaultStrategy.name)
                        : copy.tableSummaryNoDefault}
                </span>
                {/* 内置策略是否齐全也是这张表的状态，与条数同属摘要行，
          不再单独占一条横贯卡片的说明带。 */}
                {defaultsCompleteness.complete ? (
                    <>
                        <span aria-hidden="true">·</span>
                        <span
                            className="inline-flex items-center gap-1"
                            data-testid="built-in-complete"
                        >
                            <Star className="size-3.5" />
                            {copy.builtInComplete}
                        </span>
                    </>
                ) : null}
            </>
        ) : null;

    return (
        <OperatorTableShell
            summary={strategySummary}
            actions={
                !defaultsCompleteness.complete ? (
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={onCreateDefaults}
                        disabled={defaultsCreating}
                    >
                        {defaultsCreating ? (
                            <Loader2
                                data-icon="inline-start"
                                className="animate-spin"
                            />
                        ) : (
                            <Plus data-icon="inline-start" />
                        )}
                        {copy.completeBuiltInStrategies}
                    </Button>
                ) : null
            }
        >
            {/* 状态块自带留白并与卡头左右对齐；表格本身贴边铺满，
          卡片边框就是表格边框，不再多套一层“卡中卡”。 */}
            {defaultsError ? (
                <div className="px-[var(--density-card-pad-x)] pb-[var(--density-card-pad-y)]">
                    <OperatorErrorState
                        title={copy.loadFailed}
                        description={defaultsError}
                        action={
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                onClick={onCreateDefaults}
                                disabled={defaultsCreating}
                            >
                                <RefreshCw data-icon="inline-start" />
                                {copy.retry}
                            </Button>
                        }
                    />
                </div>
            ) : null}

            {/* A default-strategy conflict is a page-level fact — someone else moved
          the default — so it sits above the table rather than inside one row. */}
            {conflicts.length > 0 ? (
                <div className="flex flex-col gap-2 px-[var(--density-card-pad-x)] pb-[var(--density-card-pad-y)]">
                    {conflicts.map(([strategyId, state]) => (
                        <OperatorCallout
                            key={strategyId}
                            intent="danger"
                            title={copy.defaultConflictTitle}
                            description={
                                state.error ?? copy.defaultChangedConflict
                            }
                            action={
                                <Button
                                    type="button"
                                    variant="outline"
                                    size="sm"
                                    onClick={() =>
                                        onClearSetDefaultError(
                                            Number(strategyId),
                                        )
                                    }
                                >
                                    {copy.defaultConflictAction}
                                </Button>
                            }
                        />
                    ))}
                </div>
            ) : null}

            {fragment.phase === "error" ? (
                <div className="px-[var(--density-card-pad-x)] pb-[var(--density-card-pad-y)]">
                    <OperatorErrorState
                        title={
                            fragment.stale
                                ? copy.loadFailedStale(
                                      fragment.lastSuccessfulAt ?? "",
                                  )
                                : copy.loadFailed
                        }
                        description={fragment.error ?? ""}
                        action={
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                onClick={onRetry}
                            >
                                <RefreshCw data-icon="inline-start" />
                                {copy.retry}
                            </Button>
                        }
                    />
                </div>
            ) : null}

            {fragment.phase === "empty" ? (
                <div className="px-[var(--density-card-pad-x)] pb-[var(--density-card-pad-y)]">
                    <OperatorEmptyState
                        title={copy.emptyTitle}
                        description={copy.emptyDescription}
                        action={
                            <Button
                                type="button"
                                size="sm"
                                onClick={onCreateDefaults}
                                disabled={defaultsCreating}
                            >
                                {copy.completeBuiltInStrategies}
                            </Button>
                        }
                    />
                </div>
            ) : null}

            {/* Loading keeps the shell and the header and swaps in skeleton rows,
          so the table does not collapse and jump back on arrival. */}
            {fragment.phase === "loading" ||
            (fragment.phase === "ready" && sorted.length > 0) ? (
                <>
                    <div className="overflow-x-auto">
                        <Table
                            aria-label={
                                messages.loadbalanceStrategiesPage.title
                            }
                        >
                            <TableHeader>
                                <TableRow>
                                    {/* 名称列吃掉剩余宽度，其余列按内容给下限，
                      让 7 列在常规桌面宽度内放得下、不必横向滚动。 */}
                                    <TableHead className="sticky left-0 z-20 w-[26%] min-w-52 bg-inset shadow-[inset_-1px_0_0_0_var(--color-border)]">
                                        <button
                                            type="button"
                                            className="inline-flex items-center gap-1 text-left"
                                            onClick={() =>
                                                setSortColumn(
                                                    sortColumn === "name"
                                                        ? "type"
                                                        : "name",
                                                )
                                            }
                                            aria-sort={
                                                sortColumn === "name"
                                                    ? "ascending"
                                                    : "none"
                                            }
                                        >
                                            {copy.nameLabel}
                                        </button>
                                    </TableHead>
                                    <TableHead className="w-28">
                                        <button
                                            type="button"
                                            className="inline-flex items-center gap-1 text-left"
                                            onClick={() =>
                                                setSortColumn(
                                                    sortColumn === "type"
                                                        ? "default"
                                                        : "type",
                                                )
                                            }
                                            aria-sort={
                                                sortColumn === "type"
                                                    ? "ascending"
                                                    : "none"
                                            }
                                        >
                                            {copy.strategyTypeLabel}
                                        </button>
                                    </TableHead>
                                    <TableHead className="w-36">
                                        {copy.defaultBadge}
                                    </TableHead>
                                    <TableHead className="w-40">
                                        {copy.attachedModels}
                                    </TableHead>
                                    <TableHead className="w-[22%] min-w-48">
                                        {copy.retrySummaryColumn}
                                    </TableHead>
                                    <TableHead className="w-40">
                                        {copy.banSummaryColumn}
                                    </TableHead>
                                    <TableHead className="sticky right-0 z-20 w-24 bg-inset text-right shadow-[inset_1px_0_0_0_var(--color-border)]">
                                        {copy.actions}
                                    </TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {fragment.phase === "loading" ? (
                                    <OperationalTableSkeletonRows
                                        columns={7}
                                        rows={4}
                                    />
                                ) : null}
                                {fragment.phase === "ready"
                                    ? sorted.map((strategy) => (
                                          <StrategyRow
                                              key={strategy.id}
                                              strategy={strategy}
                                              preparing={
                                                  preparingEditId ===
                                                  strategy.id
                                              }
                                              selected={
                                                  selectedId === strategy.id
                                              }
                                              setDefaultState={
                                                  setDefaultState[strategy.id]
                                              }
                                              impactState={
                                                  impactStates[strategy.id]
                                              }
                                              strategyCopy={strategyCopy}
                                              copy={copy}
                                              formatNumber={formatNumber}
                                              onEdit={onEdit}
                                              onDelete={onDelete}
                                              onSelect={onSelect}
                                              onSetDefault={onSetDefault}
                                              onClearSetDefaultError={
                                                  onClearSetDefaultError
                                              }
                                              onToggleImpact={onToggleImpact}
                                              onLoadMoreImpact={
                                                  onLoadMoreImpact
                                              }
                                              onRetryImpact={onRetryImpact}
                                          />
                                      ))
                                    : null}
                            </TableBody>
                        </Table>
                    </div>

                    {/* “新模型默认”改的是什么，是整列的注解而非某一行的属性：
              作为表尾脚注说明一次，主轴（卡头 → 表头 → 数据）不被打断。 */}
                    {defaultStrategy ? (
                        <p className="border-t border-border px-[var(--density-card-pad-x)] pt-2 text-xs text-muted-foreground">
                            {copy.defaultOnlyAffectsNewModels}
                        </p>
                    ) : null}
                </>
            ) : null}
        </OperatorTableShell>
    );
}

interface StrategyRowProps {
    strategy: LoadbalanceStrategy;
    preparing: boolean;
    selected: boolean;
    onSelect: (strategy: LoadbalanceStrategy) => void;
    setDefaultState?: SetDefaultState;
    impactState?: StrategyImpactState;
    strategyCopy: ReturnType<
        typeof useLocale
    >["messages"]["loadbalanceStrategyCopy"];
    copy: ReturnType<typeof useLocale>["messages"]["routingStrategyTable"];
    formatNumber: (value: number) => string;
    onEdit: (strategy: LoadbalanceStrategy) => void;
    onDelete: (strategy: LoadbalanceStrategy) => void;
    onSetDefault: (strategyId: number) => void;
    onClearSetDefaultError: (strategyId: number) => void;
    onToggleImpact: (strategyId: number) => void;
    onLoadMoreImpact: (strategyId: number) => void;
    onRetryImpact: (strategyId: number) => void;
}

function StrategyRow(props: StrategyRowProps) {
    const { strategy, setDefaultState, impactState, formatNumber } = props;
    const summaryByStrategyType = summaryForStrategy(
        strategy,
        props.strategyCopy,
    );
    const balancedPreset = BAN_POLICY_PRESETS.balanced;
    const retryIsBalanced =
        strategy.retry_base_delay_ms === balancedPreset.retry_base_delay_ms &&
        strategy.retry_max_delay_ms === balancedPreset.retry_max_delay_ms &&
        strategy.cycle_retry_attempt_limit ===
            balancedPreset.cycle_retry_attempt_limit;
    const banIsBalanced =
        strategy.ban_mode === balancedPreset.ban_mode &&
        strategy.ban_cumulative_retry_attempt_threshold ===
            balancedPreset.ban_cumulative_retry_attempt_threshold &&
        strategy.ban_duration_seconds === balancedPreset.ban_duration_seconds;
    const statusCodes = failureStatusCodeSummary(strategy);

    return (
        <>
            <TableRow
                data-testid={`strategy-row-${strategy.id}`}
                data-selected={props.selected || undefined}
                className={cn(
                    "cursor-pointer",
                    props.selected && "bg-primary-soft/25",
                )}
                onClick={() => props.onSelect(strategy)}
            >
                <TableCell
                    className={cn(
                        "sticky left-0 z-10 align-top shadow-[inset_-1px_0_0_0_var(--color-border)]",
                        props.selected ? "bg-primary-soft/25" : "bg-panel",
                    )}
                >
                    {/* 单元格默认 whitespace-nowrap，这里的路由方式说明是整句，
              必须允许换行，否则它会把整张表撑出横向滚动。 */}
                    <div className="flex min-w-0 flex-col gap-1">
                        {/* 推演过去只有行 onClick 一个入口：鼠标能点，键盘够不到。
                名称本身做成按钮后它才有名字、有角色、有 tab 停点。 */}
                        <button
                            type="button"
                            className="text-left font-medium underline-offset-4 hover:underline"
                            aria-current={props.selected ? "true" : undefined}
                            aria-label={props.copy.previewRowAction(
                                strategy.name,
                            )}
                            onClick={(event) => {
                                event.stopPropagation();
                                props.onSelect(strategy);
                            }}
                        >
                            {strategy.name}
                        </button>
                        <span className="text-xs whitespace-normal text-muted-foreground">
                            {summaryByStrategyType}
                        </span>
                    </div>
                </TableCell>
                <TableCell className="align-top">
                    <OperatorTypeBadge
                        label={
                            props.strategyCopy[
                                legacyStrategyLabelKey(
                                    strategy.legacy_strategy_type,
                                )
                            ]
                        }
                    />
                </TableCell>

                {/* Default-for-new-models and attached-models answer different
            questions, so they no longer share a cell. */}
                <TableCell className="align-top">
                    <div className="flex flex-col items-start gap-1.5">
                        {strategy.is_default ? (
                            <OperatorTypeBadge
                                label={props.copy.defaultBadge}
                                intent="accent"
                            />
                        ) : (
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                onClick={(event) => {
                                    event.stopPropagation();
                                    props.onSetDefault(strategy.id);
                                }}
                                disabled={setDefaultState?.pending}
                                aria-busy={setDefaultState?.pending ?? false}
                            >
                                {setDefaultState?.pending ? (
                                    <Loader2
                                        data-icon="inline-start"
                                        className="animate-spin"
                                    />
                                ) : (
                                    <Star data-icon="inline-start" />
                                )}
                                {props.copy.setAsDefault}
                            </Button>
                        )}
                    </div>
                </TableCell>

                <TableCell className="align-top">
                    <div className="flex flex-col items-start gap-1">
                        {/* 计数是配置链的下一环入口：点进模型配置列表，只看绑在
                            这条策略上的那一批。零绑定时没有可去之处，保持纯数字。 */}
                        {strategy.attached_model_count > 0 ? (
                            <Link
                                to="/route/models"
                                search={{ strategy_id: strategy.id }}
                                onClick={(event) => event.stopPropagation()}
                                aria-label={props.copy.attachedModelsLink(
                                    strategy.name,
                                )}
                                className="font-mono text-sm tabular-nums text-primary underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
                            >
                                {formatNumber(strategy.attached_model_count)}
                            </Link>
                        ) : (
                            <span className="font-mono text-sm tabular-nums">
                                {formatNumber(strategy.attached_model_count)}
                            </span>
                        )}
                        {strategy.attached_model_count > 0 ? (
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                onClick={(event) => {
                                    event.stopPropagation();
                                    props.onToggleImpact(strategy.id);
                                }}
                                aria-expanded={impactState?.expanded ?? false}
                                aria-controls={`strategy-impact-${strategy.id}`}
                                aria-label={
                                    impactState?.expanded
                                        ? props.copy.attachedModelsCollapse
                                        : props.copy.attachedModelsExpand(
                                              strategy.attached_model_count,
                                          )
                                }
                            >
                                {impactState?.expanded ? (
                                    <ChevronUp data-icon="inline-start" />
                                ) : (
                                    <ChevronDown data-icon="inline-start" />
                                )}
                                {impactState?.expanded
                                    ? props.copy.attachedModelsCollapseRow
                                    : props.copy.attachedModelsExpandRow}
                            </Button>
                        ) : null}
                    </div>
                </TableCell>

                <TableCell className="align-top">
                    <div className="flex flex-col gap-1">
                        {/* 状态码名单收进同一排徽章：单元格默认一行，最多两行。 */}
                        <div className="flex flex-wrap items-center gap-1">
                            {retryBadges(strategy).map((badge) => (
                                <OperatorValueBadge
                                    key={badge.key}
                                    label={badge.label}
                                />
                            ))}
                            <OperatorValueBadge label={statusCodes.label} />
                            {statusCodes.detail ? (
                                // 整行点击会选中策略并把预览滚进视口；读名单不该顺带这么做。
                                <span
                                    onClick={(event) => event.stopPropagation()}
                                >
                                    <OperatorHelpHint
                                        label={statusCodes.detail}
                                    />
                                </span>
                            ) : null}
                        </div>
                        {retryIsBalanced ? (
                            <span className="text-xs text-muted-foreground">
                                {props.copy.retryBalancedDefault}
                            </span>
                        ) : null}
                    </div>
                </TableCell>

                <TableCell className="align-top">
                    <div className="flex flex-col gap-1">
                        {strategy.ban_mode === "off" ? (
                            <span className="text-xs text-muted-foreground">
                                {props.copy.banOff}
                            </span>
                        ) : (
                            <div className="flex flex-wrap gap-1">
                                {banBadges(strategy).map((badge) => (
                                    <OperatorValueBadge
                                        key={badge.key}
                                        label={badge.label}
                                    />
                                ))}
                            </div>
                        )}
                        {banIsBalanced ? (
                            <span className="text-xs text-muted-foreground">
                                {props.copy.banBalancedDefault}
                            </span>
                        ) : null}
                    </div>
                </TableCell>

                <TableCell
                    className={cn(
                        "sticky right-0 z-10 align-top text-right shadow-[inset_1px_0_0_0_var(--color-border)]",
                        props.selected ? "bg-primary-soft/25" : "bg-panel",
                    )}
                >
                    <div className="flex flex-col items-end gap-1">
                        <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            onClick={(event) => {
                                event.stopPropagation();
                                props.onEdit(strategy);
                            }}
                            disabled={props.preparing}
                        >
                            {props.preparing ? (
                                <Loader2
                                    data-icon="inline-start"
                                    className="animate-spin"
                                />
                            ) : null}
                            {props.copy.edit}
                        </Button>
                        {/* Always clickable: why a delete is blocked belongs in the
                confirmation flow, not in a greyed-out control. */}
                        <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={(event) => {
                                event.stopPropagation();
                                props.onDelete(strategy);
                            }}
                        >
                            {props.copy.delete}
                        </Button>
                    </div>
                </TableCell>
            </TableRow>
            {impactState?.expanded ? (
                <TableRow id={`strategy-impact-${strategy.id}`}>
                    <TableCell colSpan={7} className="bg-inset">
                        <StrategyImpactList
                            strategyId={strategy.id}
                            impactState={impactState}
                            copy={props.copy}
                            formatNumber={props.formatNumber}
                            onLoadMore={props.onLoadMoreImpact}
                            onRetry={props.onRetryImpact}
                        />
                    </TableCell>
                </TableRow>
            ) : null}
        </>
    );
}

function StrategyImpactList({
    strategyId,
    impactState,
    copy,
    formatNumber,
    onLoadMore,
    onRetry,
}: {
    strategyId: number;
    impactState: StrategyImpactState;
    copy: ReturnType<typeof useLocale>["messages"]["routingStrategyTable"];
    formatNumber: (value: number) => string;
    onLoadMore: (strategyId: number) => void;
    onRetry: (strategyId: number) => void;
}) {
    const { messages } = useLocale();
    const tableCopy = messages.operationalTable;
    const fragment = impactState.fragment;
    const data = fragment.data;
    // A failed append keeps the loaded models on screen; only a read that never
    // produced anything replaces the panel with an error surface.
    if (fragment.phase === "error" && (!data || data.items.length === 0)) {
        return (
            <OperatorErrorState
                title={copy.attachedModelsFailed}
                description={fragment.error ?? ""}
                action={
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => onRetry(strategyId)}
                    >
                        <RefreshCw data-icon="inline-start" />
                        {copy.retry}
                    </Button>
                }
            />
        );
    }
    if (!data || data.items.length === 0) {
        if (fragment.phase === "loading") {
            return (
                <OperatorLoadingState
                    title={copy.attachedModels}
                    description={copy.attachedModelsEmpty}
                />
            );
        }
        return (
            <span className="text-sm text-muted-foreground">
                {copy.attachedModelsEmpty}
            </span>
        );
    }
    const appendFailed = fragment.error !== null && fragment.phase !== "error";
    return (
        <div className="flex flex-col gap-2">
            {appendFailed ? (
                <p
                    role="alert"
                    className="text-xs text-failing"
                    data-testid={`strategy-impact-error-${strategyId}`}
                >
                    {fragment.error}
                </p>
            ) : null}
            <ul className="flex flex-col gap-1 text-sm">
                {data.items.map((item) => (
                    <li
                        key={item.model_config_id}
                        className="flex flex-wrap items-center gap-2"
                    >
                        <span className="font-medium">
                            {item.display_name || item.model_id}
                        </span>
                        <span className="font-mono text-xs text-muted-foreground">
                            {item.model_id}
                        </span>
                        <OperatorTypeBadge
                            label={
                                item.is_enabled ? copy.enabled : copy.disabled
                            }
                        />
                    </li>
                ))}
            </ul>
            <LoadMoreControl
                testId={`strategy-impact-more-${strategyId}`}
                pending={fragment.phase === "loading"}
                error={appendFailed ? fragment.error : null}
                hasMore={Boolean(data.has_more)}
                labels={{
                    loadMore: copy.attachedModelsExpand(
                        formatNumber(data.attached_model_count),
                    ),
                    loading: tableCopy.loadingMore,
                    retry: tableCopy.retryLoadMore,
                }}
                onLoadMore={() =>
                    appendFailed ? onRetry(strategyId) : onLoadMore(strategyId)
                }
            />
        </div>
    );
}

function legacyStrategyLabelKey(
    type: LoadbalanceStrategy["legacy_strategy_type"],
): "singleLabel" | "fillFirstLabel" | "roundRobinLabel" {
    switch (type) {
        case "single":
            return "singleLabel";
        case "fill-first":
            return "fillFirstLabel";
        case "round-robin":
            return "roundRobinLabel";
    }
}

function summaryForStrategy(
    strategy: LoadbalanceStrategy,
    strategyCopy: {
        singleSummary: string;
        fillFirstSummary: string;
        roundRobinSummary: string;
    },
): string {
    switch (strategy.legacy_strategy_type) {
        case "single":
            return strategyCopy.singleSummary;
        case "fill-first":
            return strategyCopy.fillFirstSummary;
        case "round-robin":
            return strategyCopy.roundRobinSummary;
        default:
            return strategy.legacy_strategy_type;
    }
}
