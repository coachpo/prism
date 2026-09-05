import { CheckCircle2, ChevronDown, ChevronRight, CircleAlert, Loader2, MinusCircle } from "lucide-react"
import type { FocusEvent, RefObject } from "react"
import type { SetupCoordinatorState, SetupFact } from "@/lib/types"
import { Button } from "@/components/ui/button"
import {
  OperatorCallout,
  OperatorErrorState,
  OperatorLoadingState,
  OperatorSectionCard,
} from "@/shared/design-system"
import { useLocale } from "@/i18n/useLocale"

interface SetupCardProps {
  state: SetupCoordinatorState
  collapsed: boolean
  cardRef: RefObject<HTMLDivElement | null>
  onBlurCapture: (event: FocusEvent<HTMLDivElement>) => void
  onRetry: () => void
  onToggle: () => void
}

function factStatus(fact: SetupFact, copy: ReturnType<typeof useLocale>["messages"]["setup"]): string {
  if (fact.fetch_quality === "loading") return copy.checking
  if (fact.fetch_quality === "error") return copy.error
  if (fact.fetch_quality === "unknown") return copy.unknown
  if (fact.fetch_quality === "stale") return copy.stale
  if (fact.result === "complete") return copy.complete
  if (fact.result === "skipped") return copy.skipped
  if (fact.result === "incomplete") return copy.incomplete
  return copy.unknown
}

function FactIcon({ fact }: { fact: SetupFact }) {
  if (fact.fetch_quality === "loading") return <Loader2 className="size-4 animate-spin" aria-hidden="true" />
  if (fact.result === "complete" || fact.result === "skipped") return <CheckCircle2 className="size-4 text-healthy" aria-hidden="true" />
  if (fact.fetch_quality === "error") return <CircleAlert className="size-4 text-destructive" aria-hidden="true" />
  if (fact.fetch_quality === "unknown" || fact.fetch_quality === "stale") return <CircleAlert className="size-4 text-degraded" aria-hidden="true" />
  return <MinusCircle className="size-4 text-muted-foreground" aria-hidden="true" />
}

function FactRow({ fact }: { fact: SetupFact }) {
  const { messages } = useLocale()
  const copy = messages.setup
  const status = factStatus(fact, copy)
  return (
    <li className="flex min-w-0 flex-wrap items-start gap-3 border-b border-border/70 py-3 last:border-b-0">
      <span className="mt-0.5 shrink-0" aria-hidden="true"><FactIcon fact={fact} /></span>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
          <span className="font-medium">{fact.label}</span>
          <span className="text-xs text-muted-foreground">{status}</span>
        </div>
        {fact.detail ? <p className="mt-1 text-xs text-muted-foreground">{fact.detail}</p> : null}
        {fact.reason_codes.length > 0 && !fact.detail ? (
          <p className="mt-1 text-xs text-muted-foreground">{copy.reason(fact.reason_codes.join("、"))}</p>
        ) : null}
      </div>
      {fact.href ? (
        <a
          href={fact.href}
          className="inline-flex min-h-8 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-primary underline-offset-4 hover:underline"
        >
          {copy.openOwner}
          <ChevronRight className="size-3.5" aria-hidden="true" />
        </a>
      ) : fact.id === "runtime_self_test" ? (
        <a
          href="/system/proxy-keys"
          className="inline-flex min-h-8 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-primary underline-offset-4 hover:underline"
        >
          {copy.openSelfTest}
          <ChevronRight className="size-3.5" aria-hidden="true" />
        </a>
      ) : null}
    </li>
  )
}

function FactGroup({ facts, label }: { facts: readonly SetupFact[]; label: string }) {
  return (
    <section className="mt-2 first:mt-0">
      <h3 className="text-xs font-medium text-muted-foreground">{label}</h3>
      <ol className="divide-y-0" aria-label={label}>
        {facts.map((fact) => <FactRow key={fact.id} fact={fact} />)}
      </ol>
    </section>
  )
}

export function SetupCard({ state, collapsed, cardRef, onBlurCapture, onRetry, onToggle }: SetupCardProps) {
  const { messages } = useLocale()
  const copy = messages.setup
  const coreFacts = state.facts.filter((fact) => fact.kind === "required")
  const otherFacts = state.facts.filter((fact) => fact.kind !== "required")
  const count = state.route_configured_count
  const headline = count === null ? copy.checkingHeadline : copy.routeConfigured(count)
  const description = count === 4 ? copy.readyDescription : copy.description

  if (state.phase === "loading" && state.facts.every((fact) => fact.fetch_quality === "loading")) {
    return <OperatorLoadingState title={copy.checkingHeadline} description={copy.checkingDescription} />
  }

  return (
    <div ref={cardRef} onBlurCapture={onBlurCapture} data-testid="setup-card">
      <OperatorSectionCard
        title={copy.title}
        description={description}
        actions={
          <Button type="button" variant="ghost" size="sm" onClick={onToggle} aria-expanded={!collapsed} aria-controls="prism-setup-facts">
            {collapsed ? copy.expand : copy.collapse}
            <ChevronDown className={`size-4 transition-transform ${collapsed ? "-rotate-90" : ""}`} aria-hidden="true" />
          </Button>
        }
      >
        <div className="flex flex-wrap items-center justify-between gap-3 pb-3" aria-live="polite">
          <div>
            <p className="text-sm font-semibold">{headline}</p>
            <p className="text-xs text-muted-foreground">{copy.fourHardItems}</p>
          </div>
          {state.phase === "degraded" || state.phase === "unknown" ? (
            <Button type="button" variant="outline" size="sm" onClick={onRetry}>{copy.retry}</Button>
          ) : null}
        </div>
        {!collapsed ? (
          <div id="prism-setup-facts">
            {state.phase === "degraded" && state.error ? (
              <OperatorErrorState title={copy.degradedTitle} description={state.error} action={<Button type="button" variant="outline" size="sm" onClick={onRetry}>{copy.retry}</Button>} className="mb-3" />
            ) : null}
            {state.phase === "unknown" ? (
              <OperatorCallout intent="warning" title={copy.unknownTitle} description={copy.unknownDescription} className="mb-3" />
            ) : null}
            {/* 页头的 4 / 4 只数 kind==="required" 的四项。清单平铺时能数出 5 个
                「已完成」，与页头对不上；按同一划分分组，两个数字才出自同一处。 */}
            <section aria-label={copy.factsLabel}>
              <FactGroup label={copy.coreGroupLabel} facts={coreFacts} />
              {otherFacts.length > 0 ? (
                <FactGroup label={copy.otherGroupLabel} facts={otherFacts} />
              ) : null}
            </section>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground" data-testid="setup-card-summary">{copy.collapsedSummary}</p>
        )}
      </OperatorSectionCard>
    </div>
  )
}
