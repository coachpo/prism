import { useEffect, useState } from "react"

import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/i18n/useLocale"
import { api } from "@/lib/api"
import type { LoadbalanceStrategy, StrategyPreviewResponse } from "@/lib/types"
import { cn } from "@/lib/utils"
import {
  OperatorErrorState,
  OperatorInsetPanel,
  OperatorRetryButton,
  OperatorSectionCard,
  OperatorStatusBadge,
  OperatorValueBadge,
} from "@/shared/design-system"
import { formatDurationMs, formatDurationSeconds } from "./strategyValueBadges"

interface StrategyPreviewTimelineProps {
  strategy: LoadbalanceStrategy | null
}

/**
 * The retry/ban projection as a horizontal timeline on the page.
 *
 * It used to live at the bottom of the edit dialog, which meant the only way
 * to see what a strategy actually does was to open it for editing. Selecting a
 * row projects it here instead, and the jitter band is drawn as a bar under
 * each node so the spread is visible rather than described.
 */
export function StrategyPreviewTimeline({ strategy }: StrategyPreviewTimelineProps) {
  const { messages } = useLocale()
  const copy = messages.routingStrategyTable
  // The call site keys this component by strategy id, so a selection change
  // remounts it rather than needing an effect to reset state.
  const [state, setState] = useState<
    { phase: "idle" } | { phase: "loading" } | { phase: "error"; message: string } | { phase: "ready"; data: StrategyPreviewResponse }
  >(() => (strategy ? { phase: "loading" } : { phase: "idle" }))
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    if (!strategy) {
      return
    }
    let cancelled = false
    void api.loadbalanceStrategies
      .preview({
        legacy_strategy_type: strategy.legacy_strategy_type,
        failure_status_codes: strategy.failure_status_codes,
        ban_mode: strategy.ban_mode,
        retry_base_delay_ms: strategy.retry_base_delay_ms,
        retry_backoff_multiplier: strategy.retry_backoff_multiplier,
        retry_jitter_ratio: strategy.retry_jitter_ratio,
        retry_max_delay_ms: strategy.retry_max_delay_ms,
        cycle_retry_attempt_limit: strategy.cycle_retry_attempt_limit,
        ban_cumulative_retry_attempt_threshold: strategy.ban_cumulative_retry_attempt_threshold,
        ban_duration_seconds: strategy.ban_duration_seconds,
      })
      .then((data) => {
        if (!cancelled) setState({ phase: "ready", data })
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setState({ phase: "error", message: error instanceof Error ? error.message : copy.previewFailed })
        }
      })
    return () => {
      cancelled = true
    }
  }, [attempt, copy.previewFailed, strategy])

  return (
    <OperatorSectionCard
      data-testid="strategy-preview-timeline"
      title={copy.previewTitle}
      description={strategy ? copy.previewSubtitle(strategy.name) : copy.previewSelectPrompt}
      contentClassName="flex flex-col gap-3"
    >
      {state.phase === "idle" ? (
        <p className="text-xs text-muted-foreground">{copy.previewSelectPrompt}</p>
      ) : state.phase === "loading" ? (
        <Skeleton className="h-24 rounded-md" />
      ) : state.phase === "error" ? (
        <OperatorErrorState
          title={copy.previewFailed}
          description={state.message}
          action={<OperatorRetryButton onClick={() => setAttempt((current) => current + 1)}>{copy.retry}</OperatorRetryButton>}
        />
      ) : (
        <PreviewSteps data={state.data} />
      )}
    </OperatorSectionCard>
  )
}

function PreviewSteps({ data }: { data: StrategyPreviewResponse }) {
  const { messages } = useLocale()
  const copy = messages.routingStrategyTable
  // One shared scale so the bands are comparable across steps.
  const maxDelay = Math.max(1, ...data.steps.map((step) => step.jitter_max_delay_ms))

  return (
    <div className="flex flex-col gap-3">
      <ol className="flex min-w-0 gap-3 overflow-x-auto pb-1">
        {data.steps.map((step) => {
          const offset = (step.jitter_min_delay_ms / maxDelay) * 100
          const width = Math.max(2, ((step.jitter_max_delay_ms - step.jitter_min_delay_ms) / maxDelay) * 100)
          return (
            <li key={step.failure_ordinal} className="min-w-44 flex-1">
              <OperatorInsetPanel className="gap-1.5 p-2.5">
                <p className="text-[11px] font-medium text-muted-foreground">
                  {copy.previewStepLabel(String(step.failure_ordinal))}
                </p>
                <p className="font-mono text-sm tabular-nums">{formatDurationMs(step.nominal_delay_ms)}</p>
                <div className="h-1 w-full rounded-full bg-panel" title={copy.previewJitterBand(formatDurationMs(step.jitter_min_delay_ms), formatDurationMs(step.jitter_max_delay_ms))}>
                  <div
                    className="h-full rounded-full bg-primary/60"
                    style={{ marginLeft: `${offset}%`, width: `${width}%` }}
                  />
                </div>
                <p className="font-mono text-[11px] tabular-nums text-muted-foreground">
                  {copy.previewJitterBand(
                    formatDurationMs(step.jitter_min_delay_ms),
                    formatDurationMs(step.jitter_max_delay_ms),
                  )}
                </p>
                <div className={cn("flex flex-wrap gap-1", !step.cycle_exhausted && !step.ban_transition && "hidden")}>
                  {step.cycle_exhausted ? (
                    <OperatorStatusBadge intent="degraded" preserveLabel label={copy.previewCycleExhaustedNode} />
                  ) : null}
                  {step.ban_transition ? (
                    <OperatorStatusBadge intent="failing" preserveLabel label={copy.previewBanTransitionNode} />
                  ) : null}
                </div>
              </OperatorInsetPanel>
            </li>
          )
        })}
      </ol>

      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        {data.has_more ? <span>{copy.previewHasMore}</span> : null}
        {data.ban_projection.mode !== "off" ? (
          <OperatorValueBadge
            label={
              data.ban_projection.mode === "until_reset"
                ? copy.banUntilResetShort
                : copy.banDuration(formatDurationSeconds(data.ban_projection.duration_seconds))
            }
          />
        ) : null}
      </div>
    </div>
  )
}
