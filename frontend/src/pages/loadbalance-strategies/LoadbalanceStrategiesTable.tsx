import { useState } from "react"
import { ChevronDown, ChevronUp, Loader2, Plus, RefreshCw, Star } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { OperationalTableSkeletonRows } from "@/shared/table/operationalTable"
import { banBadges, failureStatusCodeLabel, retryBadges } from "./strategyValueBadges"
import { useLocale } from "@/i18n/useLocale"
import type { LoadbalanceStrategy } from "@/lib/types"
import type { FragmentState } from "@/features/loadbalance/useBanPoliciesFeatureData"
import type { StrategyImpactState, SetDefaultState } from "@/features/loadbalance/useBanPoliciesFeatureData"
import { BAN_POLICY_PRESETS } from "@/features/loadbalance/banPolicySchemas"
import {
  OperatorCallout,
  OperatorEmptyState,
  OperatorErrorState,
  OperatorLoadingState,
  OperatorTableShell,
  OperatorTypeBadge,
  OperatorValueBadge,
} from "@/shared/design-system"

interface LoadbalanceStrategiesTableProps {
  fragment: FragmentState<LoadbalanceStrategy[]>
  defaultsCompleteness: { complete: boolean; missing: string[]; existingCount: number }
  defaultsCreating: boolean
  defaultsError: string | null
  preparingEditId: number | null
  setDefaultState: Record<number, SetDefaultState>
  impactStates: Record<number, StrategyImpactState>
  onCreateDefaults: () => void
  onEdit: (strategy: LoadbalanceStrategy) => void
  onDelete: (strategy: LoadbalanceStrategy) => void
  onSetDefault: (strategyId: number) => void
  onClearSetDefaultError: (strategyId: number) => void
  onToggleImpact: (strategyId: number) => void
  onLoadMoreImpact: (strategyId: number) => void
  onRetryImpact: (strategyId: number) => void
  onRetry: () => void
  onSelect: (strategy: LoadbalanceStrategy) => void
  selectedId: number | null
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
  const { formatNumber, messages } = useLocale()
  const copy = messages.routingStrategyTable
  const strategyCopy = messages.loadbalanceStrategyCopy
  const [sortColumn, setSortColumn] = useState<"name" | "type" | "default">("default")

  const strategies = fragment.data ?? []
  const conflicts = Object.entries(setDefaultState).filter(([, state]) => Boolean(state?.error))
  const sorted = [...strategies].sort((left, right) => {
    switch (sortColumn) {
      case "name":
        return left.name.localeCompare(right.name)
      case "type":
        return left.legacy_strategy_type.localeCompare(right.legacy_strategy_type) || left.id - right.id
      default:
        if (left.is_default !== right.is_default) return left.is_default ? -1 : 1
        return left.id - right.id
    }
  })

  // Card headers carry a state summary, not the page title.
  const defaultStrategy = strategies.find((strategy) => strategy.is_default) ?? null
  const banEnabledCount = strategies.filter((strategy) => strategy.ban_mode !== "off").length
  const strategySummary = strategies.length > 0 ? (
    <>
      <span>{copy.tableSummary(formatNumber(strategies.length), formatNumber(banEnabledCount))}</span>
      <span aria-hidden="true">·</span>
      <span>
        {defaultStrategy ? copy.tableSummaryDefault(defaultStrategy.name) : copy.tableSummaryNoDefault}
      </span>
    </>
  ) : null

  return (
    <OperatorTableShell
      summary={strategySummary}
      actions={
        !defaultsCompleteness.complete ? (
          <Button type="button" variant="outline" size="sm" onClick={onCreateDefaults} disabled={defaultsCreating}>
            {defaultsCreating ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Plus data-icon="inline-start" />}
            {copy.completeBuiltInStrategies}
          </Button>
        ) : null
      }
      contentClassName="flex flex-col gap-4"
    >
      {defaultsCompleteness.complete ? (
        <div className="flex items-center gap-2 px-1 text-sm text-foreground/60" data-testid="built-in-complete">
          <Star data-icon="inline-start" className="size-4" />
          {copy.builtInComplete}
        </div>
      ) : null}
      {defaultsError ? (
        <OperatorErrorState
          title={copy.loadFailed}
          description={defaultsError}
          action={<Button type="button" variant="outline" size="sm" onClick={onCreateDefaults} disabled={defaultsCreating}><RefreshCw data-icon="inline-start" />{copy.retry}</Button>}
        />
      ) : null}

      {/* A default-strategy conflict is a page-level fact — someone else moved
          the default — so it sits above the table rather than inside one row. */}
      {conflicts.map(([strategyId, state]) => (
        <OperatorCallout
          key={strategyId}
          intent="danger"
          title={copy.defaultConflictTitle}
          description={state.error ?? copy.defaultChangedConflict}
          action={
            <Button type="button" variant="outline" size="sm" onClick={() => onClearSetDefaultError(Number(strategyId))}>
              {copy.defaultConflictAction}
            </Button>
          }
        />
      ))}

      {fragment.phase === "error" ? (
        <OperatorErrorState
          title={fragment.stale ? copy.loadFailedStale(fragment.lastSuccessfulAt ?? "") : copy.loadFailed}
          description={fragment.error ?? ""}
          action={<Button type="button" variant="outline" size="sm" onClick={onRetry}><RefreshCw data-icon="inline-start" />{copy.retry}</Button>}
        />
      ) : null}

      {fragment.phase === "empty" ? (
        <OperatorEmptyState
          title={copy.emptyTitle}
          description={copy.emptyDescription}
          action={<Button type="button" size="sm" onClick={onCreateDefaults} disabled={defaultsCreating}>{copy.completeBuiltInStrategies}</Button>}
        />
      ) : null}

      {/* Loading keeps the shell and the header and swaps in skeleton rows,
          so the table does not collapse and jump back on arrival. */}
      {fragment.phase === "loading" || (fragment.phase === "ready" && sorted.length > 0) ? (
        <div className="overflow-hidden rounded-lg border border-border">
          <div className="overflow-x-auto">
            <Table aria-label={messages.loadbalanceStrategiesPage.title}>
              <TableHeader>
                <TableRow>
                  <TableHead className="min-w-56">
                    <button type="button" className="inline-flex items-center gap-1 text-left" onClick={() => setSortColumn(sortColumn === "name" ? "type" : "name")} aria-sort={sortColumn === "name" ? "ascending" : "none"}>
                      {copy.nameLabel}
                    </button>
                  </TableHead>
                  <TableHead className="min-w-44">
                    <button type="button" className="inline-flex items-center gap-1 text-left" onClick={() => setSortColumn(sortColumn === "type" ? "default" : "type")} aria-sort={sortColumn === "type" ? "ascending" : "none"}>
                      {copy.strategyTypeLabel}
                    </button>
                  </TableHead>
                  <TableHead className="min-w-36">{copy.defaultBadge}</TableHead>
                  <TableHead className="min-w-40">{copy.attachedModels}</TableHead>
                  <TableHead className="min-w-56">{copy.retrySummaryColumn}</TableHead>
                  <TableHead className="min-w-44">{copy.banSummaryColumn}</TableHead>
                  <TableHead className="text-right">{copy.actions}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {fragment.phase === "loading" ? <OperationalTableSkeletonRows columns={7} rows={4} /> : null}
                {fragment.phase === "ready"
                  ? sorted.map((strategy) => (
                      <StrategyRow
                        key={strategy.id}
                        strategy={strategy}
                        preparing={preparingEditId === strategy.id}
                        selected={selectedId === strategy.id}
                        setDefaultState={setDefaultState[strategy.id]}
                        impactState={impactStates[strategy.id]}
                        strategyCopy={strategyCopy}
                        copy={copy}
                        formatNumber={formatNumber}
                        onEdit={onEdit}
                        onDelete={onDelete}
                        onSelect={onSelect}
                        onSetDefault={onSetDefault}
                        onClearSetDefaultError={onClearSetDefaultError}
                        onToggleImpact={onToggleImpact}
                        onLoadMoreImpact={onLoadMoreImpact}
                        onRetryImpact={onRetryImpact}
                      />
                    ))
                  : null}
              </TableBody>
            </Table>
          </div>
        </div>
      ) : null}
    </OperatorTableShell>
  )
}

interface StrategyRowProps {
  strategy: LoadbalanceStrategy
  preparing: boolean
  selected: boolean
  onSelect: (strategy: LoadbalanceStrategy) => void
  setDefaultState?: SetDefaultState
  impactState?: StrategyImpactState
  strategyCopy: ReturnType<typeof useLocale>["messages"]["loadbalanceStrategyCopy"]
  copy: ReturnType<typeof useLocale>["messages"]["routingStrategyTable"]
  formatNumber: (value: number) => string
  onEdit: (strategy: LoadbalanceStrategy) => void
  onDelete: (strategy: LoadbalanceStrategy) => void
  onSetDefault: (strategyId: number) => void
  onClearSetDefaultError: (strategyId: number) => void
  onToggleImpact: (strategyId: number) => void
  onLoadMoreImpact: (strategyId: number) => void
  onRetryImpact: (strategyId: number) => void
}

function StrategyRow(props: StrategyRowProps) {
  const { strategy, setDefaultState, impactState, formatNumber } = props
  const summaryByStrategyType = summaryForStrategy(strategy, props.strategyCopy)
  const balancedPreset = BAN_POLICY_PRESETS.balanced
  const retryIsBalanced = strategy.retry_base_delay_ms === balancedPreset.retry_base_delay_ms &&
    strategy.retry_max_delay_ms === balancedPreset.retry_max_delay_ms &&
    strategy.cycle_retry_attempt_limit === balancedPreset.cycle_retry_attempt_limit
  const banIsBalanced = strategy.ban_mode === balancedPreset.ban_mode &&
    strategy.ban_cumulative_retry_attempt_threshold === balancedPreset.ban_cumulative_retry_attempt_threshold &&
    strategy.ban_duration_seconds === balancedPreset.ban_duration_seconds

  return (
    <>
      <TableRow
        data-testid={`strategy-row-${strategy.id}`}
        data-selected={props.selected || undefined}
        className={props.selected ? "bg-primary-soft/25" : undefined}
        onClick={() => props.onSelect(strategy)}
      >
        <TableCell className="align-top">
          <div className="flex flex-col gap-1">
            <span className="font-medium">{strategy.name}</span>
            <span className="text-xs text-foreground/60">{summaryByStrategyType}</span>
          </div>
        </TableCell>
        <TableCell className="align-top">
          <OperatorTypeBadge label={props.strategyCopy[legacyStrategyLabelKey(strategy.legacy_strategy_type)]} />
        </TableCell>

        {/* Default-for-new-models and attached-models answer different
            questions, so they no longer share a cell. */}
        <TableCell className="align-top">
          <div className="flex flex-col items-start gap-1.5">
            {strategy.is_default ? (
              <>
                <OperatorTypeBadge label={props.copy.defaultBadge} intent="accent" />
                <span className="text-xs text-foreground/60">{props.copy.defaultOnlyAffectsNewModels}</span>
              </>
            ) : (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={(event) => {
                  event.stopPropagation()
                  props.onSetDefault(strategy.id)
                }}
                disabled={setDefaultState?.pending}
                aria-busy={setDefaultState?.pending ?? false}
              >
                {setDefaultState?.pending ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Star data-icon="inline-start" />}
                {props.copy.setAsDefault}
              </Button>
            )}
          </div>
        </TableCell>

        <TableCell className="align-top">
          <div className="flex flex-col items-start gap-1">
            <span className="font-mono text-sm tabular-nums">{formatNumber(strategy.attached_model_count)}</span>
            {strategy.attached_model_count > 0 ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={(event) => {
                  event.stopPropagation()
                  props.onToggleImpact(strategy.id)
                }}
                aria-expanded={impactState?.expanded ?? false}
                aria-controls={`strategy-impact-${strategy.id}`}
              >
                {impactState?.expanded ? <ChevronUp data-icon="inline-start" /> : <ChevronDown data-icon="inline-start" />}
                {impactState?.expanded ? props.copy.attachedModelsCollapse : props.copy.attachedModelsExpand(strategy.attached_model_count)}
              </Button>
            ) : null}
          </div>
        </TableCell>

        <TableCell className="align-top">
          <div className="flex flex-col gap-1">
            <div className="flex flex-wrap gap-1">
              {retryBadges(strategy).map((badge) => (
                <OperatorValueBadge key={badge.key} label={badge.label} />
              ))}
            </div>
            <span className="text-[11px] text-foreground/60">{failureStatusCodeLabel(strategy)}</span>
            {retryIsBalanced ? <span className="text-[11px] text-foreground/60">{props.copy.retryBalancedDefault}</span> : null}
          </div>
        </TableCell>

        <TableCell className="align-top">
          <div className="flex flex-col gap-1">
            {strategy.ban_mode === "off" ? (
              <span className="text-xs text-foreground/60">{props.copy.banOff}</span>
            ) : (
              <div className="flex flex-wrap gap-1">
                {banBadges(strategy).map((badge) => (
                  <OperatorValueBadge key={badge.key} label={badge.label} />
                ))}
              </div>
            )}
            {banIsBalanced ? <span className="text-[11px] text-foreground/60">{props.copy.banBalancedDefault}</span> : null}
          </div>
        </TableCell>

        <TableCell className="align-top text-right">
          <div className="flex flex-col items-end gap-1">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={(event) => {
                event.stopPropagation()
                props.onEdit(strategy)
              }}
              disabled={props.preparing}
            >
              {props.preparing ? <Loader2 data-icon="inline-start" className="animate-spin" /> : null}
              {props.copy.edit}
            </Button>
            {/* Always clickable: why a delete is blocked belongs in the
                confirmation flow, not in a greyed-out control. */}
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={(event) => {
                event.stopPropagation()
                props.onDelete(strategy)
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
            <StrategyImpactList strategyId={strategy.id} impactState={impactState} copy={props.copy} formatNumber={props.formatNumber} onLoadMore={props.onLoadMoreImpact} onRetry={props.onRetryImpact} />
          </TableCell>
        </TableRow>
      ) : null}
    </>
  )
}

function StrategyImpactList({ strategyId, impactState, copy, formatNumber, onLoadMore, onRetry }: {
  strategyId: number
  impactState: StrategyImpactState
  copy: ReturnType<typeof useLocale>["messages"]["routingStrategyTable"]
  formatNumber: (value: number) => string
  onLoadMore: (strategyId: number) => void
  onRetry: (strategyId: number) => void
}) {
  const fragment = impactState.fragment
  if (fragment.phase === "loading") {
    return <OperatorLoadingState title={copy.attachedModels} description={copy.attachedModelsEmpty} />
  }
  if (fragment.phase === "error") {
    return (
      <OperatorErrorState
        title={copy.attachedModelsFailed}
        description={fragment.error ?? ""}
        action={<Button type="button" variant="outline" size="sm" onClick={() => onRetry(strategyId)}><RefreshCw data-icon="inline-start" />{copy.retry}</Button>}
      />
    )
  }
  const data = fragment.data
  if (!data || data.items.length === 0) {
    return <span className="text-sm text-foreground/60">{copy.attachedModelsEmpty}</span>
  }
  return (
    <div className="flex flex-col gap-2">
      <ul className="flex flex-col gap-1 text-sm">
        {data.items.map((item) => (
          <li key={item.model_config_id} className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{item.display_name || item.model_id}</span>
            <span className="font-mono text-xs text-foreground/60">{item.model_id}</span>
            <OperatorTypeBadge label={item.is_enabled ? copy.enabled : copy.disabled} />
          </li>
        ))}
      </ul>
      {data.has_more ? (
        <Button type="button" variant="outline" size="sm" onClick={() => onLoadMore(strategyId)}>
          {copy.attachedModelsExpand(formatNumber(data.attached_model_count))}
        </Button>
      ) : null}
    </div>
  )
}

function legacyStrategyLabelKey(type: LoadbalanceStrategy["legacy_strategy_type"]): "singleLabel" | "fillFirstLabel" | "roundRobinLabel" {
  switch (type) {
    case "single":
      return "singleLabel"
    case "fill-first":
      return "fillFirstLabel"
    case "round-robin":
      return "roundRobinLabel"
  }
}

function summaryForStrategy(strategy: LoadbalanceStrategy, strategyCopy: { singleSummary: string; fillFirstSummary: string; roundRobinSummary: string }): string {
  switch (strategy.legacy_strategy_type) {
    case "single":
      return strategyCopy.singleSummary
    case "fill-first":
      return strategyCopy.fillFirstSummary
    case "round-robin":
      return strategyCopy.roundRobinSummary
    default:
      return strategy.legacy_strategy_type
  }
}

