import { AlertCircle, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/i18n/useLocale"
import { getStaticMessages } from "@/i18n/staticMessages"
import type { ConfigurationWarning, DiagnosticsOperationCoverage, RoutingDiagnosticsResult } from "@/lib/types"
import { CONFIGURATION_WARNING_CODES } from "@/lib/types"
import {
  OperatorCallout,
  OperatorLoadingState,
  OperatorSectionCard,
  OperatorStatusBadge,
} from "@/shared/design-system"
import {
  OPENAI_RESPONSES_OPERATIONS,
  OPENAI_CHAT_COMPLETIONS_OPERATION,
} from "./classifyOpenAICoverage"

interface OpenAICoverageSummaryProps {
  diagnostics: RoutingDiagnosticsResult | null
  loading: boolean
  error: string | null
  onRetry?: () => void
}

interface GroupPresentation {
  key: string
  label: string
  coverage: DiagnosticsOperationCoverage | null
}

function groupStatusLabel(status: string | undefined): { label: string; intent: "success" | "warning" | "danger" | "neutral" } {
  switch (status) {
    case "routable":
      return { label: "routable", intent: "success" }
    case "compatible_but_ineligible":
      return { label: "compatible_but_ineligible", intent: "warning" }
    case "uncovered":
      return { label: "uncovered", intent: "danger" }
    default:
      return { label: "not_accepted", intent: "neutral" }
  }
}

function buildGroups(diagnostics: RoutingDiagnosticsResult): GroupPresentation[] {
  const byOperation = new Map<string, DiagnosticsOperationCoverage>()
  for (const coverage of diagnostics.operation_coverage) {
    byOperation.set(coverage.operation_name, coverage)
  }
  const chat = byOperation.get(OPENAI_CHAT_COMPLETIONS_OPERATION) ?? null
  const responsesMembers = OPENAI_RESPONSES_OPERATIONS.map((operation) => byOperation.get(operation)).filter(
    (coverage): coverage is DiagnosticsOperationCoverage => Boolean(coverage),
  )
  // Responses group aggregates the whole family: routable when any member is
  // routable, otherwise compatible when any member is capability-covered.
  let responses: DiagnosticsOperationCoverage | null = null
  if (responsesMembers.length > 0) {
    responses = {
      operation_name: "responses",
      accepted: responsesMembers.some((member) => member.accepted),
      capability_covered: responsesMembers.some((member) => member.capability_covered),
      statically_routable: responsesMembers.some((member) => member.statically_routable),
      resolved_stage: responsesMembers.find((member) => member.resolved_stage)?.resolved_stage ?? null,
      compatible_access_target_ids: [...new Set(responsesMembers.flatMap((member) => member.compatible_access_target_ids))],
      access_target_ids: [...new Set(responsesMembers.flatMap((member) => member.access_target_ids))],
    }
  }
  return [
    { key: "chat_completions", label: "Chat Completions", coverage: chat },
    { key: "responses", label: "Responses", coverage: responses },
  ]
}

function warningToPresentation(warning: ConfigurationWarning) {
  const messages = getMessageBundle()
  switch (warning.code) {
    case CONFIGURATION_WARNING_CODES.operationUncovered: {
      const reason = warning.details?.reason
      const operations = warning.operation_names.join("、")
      return {
        intent: "danger" as const,
        message:
          reason === "no_static_eligible_target"
            ? messages.routing.warningUncoveredEligible(operations)
            : messages.routing.warningUncovered(operations),
      }
    }
    case CONFIGURATION_WARNING_CODES.targetPartialCoverage:
      return { intent: "warning" as const, message: messages.routing.warningPartial }
    case CONFIGURATION_WARNING_CODES.targetIncompatible:
      return { intent: "danger" as const, message: messages.routing.warningIncompatible }
    case CONFIGURATION_WARNING_CODES.singleTruncatesTargets: {
      const stage = warning.details?.stage === "model_targets" ? "模型目标" : "终端目标"
      return { intent: "warning" as const, message: messages.routing.singleTruncatesStage(stage) }
    }
    default:
      return { intent: "warning" as const, message: warning.message }
  }
}

function getMessageBundle() {
  return getStaticMessages()
}

// OpenAICoverageSummary renders the authoritative operation coverage computed
// by the backend analyzer. The frontend never re-derives capability or
// eligibility from card text.
export function OpenAICoverageSummary({ diagnostics, loading, error, onRetry }: OpenAICoverageSummaryProps) {
  const { messages } = useLocale()
  const copy = messages.routing

  return (
    <OperatorSectionCard
      title={copy.coverageSummaryTitle}
      description={copy.coverageSummaryDescription}
      contentClassName="flex flex-col gap-3"
      data-testid="openai-coverage-summary"
    >
      {loading && !diagnostics ? <OperatorLoadingState title={copy.coverageLoading} /> : null}
      {error && !diagnostics ? (
        <OperatorCallout
          intent="danger"
          description={error}
          role="alert"
          action={
            onRetry ? (
              <Button type="button" variant="outline" size="sm" onClick={onRetry}>
                <RefreshCw data-icon="inline-start" />
                {copy.retry}
              </Button>
            ) : undefined
          }
        />
      ) : null}
      {!loading && !error && !diagnostics ? <p className="text-sm text-muted-foreground">{copy.coverageEmpty}</p> : null}
      {diagnostics ? (
        <>
          <div className="flex flex-col gap-2">
            {buildGroups(diagnostics).map((group) => {
              const coverage = group.coverage
              if (!coverage) {
                return (
                  <div key={group.key} className="flex items-center justify-between gap-3 rounded-md border px-3 py-2">
                    <span className="text-sm font-medium">{group.label}</span>
                    <OperatorStatusBadge intent="neutral" label={copy.coverageNotAccepted} preserveLabel />
                  </div>
                )
              }
              const presentation = groupStatusLabel(
                coverage.statically_routable
                  ? "routable"
                  : coverage.capability_covered
                    ? "compatible_but_ineligible"
                    : coverage.accepted
                      ? "uncovered"
                      : "not_accepted",
              )
              const statusLabels: Record<string, string> = {
                routable: copy.coverageRoutable,
                compatible_but_ineligible: copy.coverageCompatibleButIneligible,
                uncovered: copy.coverageUncovered,
                not_accepted: copy.coverageNotAccepted,
              }
              const statusLabel = statusLabels[presentation.label] ?? presentation.label
              const stageLabel =
                coverage.resolved_stage === "model_targets"
                  ? copy.resolvedByModelStage
                  : coverage.resolved_stage === "terminal_targets"
                    ? copy.resolvedByTerminalStage
                    : null
              return (
                <div key={group.key} className="flex flex-col gap-1 rounded-md border px-3 py-2 sm:flex-row sm:items-center sm:justify-between">
                  <span className="text-sm font-medium">{group.label}</span>
                  <div className="flex flex-wrap items-center gap-2">
                    <OperatorStatusBadge intent={presentation.intent} label={statusLabel} preserveLabel />
                    {stageLabel ? <span className="text-xs text-muted-foreground">{stageLabel}</span> : null}
                  </div>
                </div>
              )
            })}
          </div>
          {diagnostics.configuration_warnings.length > 0 ? (
            <div className="flex flex-col gap-2" data-testid="routing-warnings">
              {diagnostics.configuration_warnings.map((warning, index) => {
                const presentation = warningToPresentation(warning)
                return (
                  <OperatorCallout key={`${warning.code}-${index}`} intent={presentation.intent} description={presentation.message}>
                    <span className="inline-flex items-center gap-1">
                      <AlertCircle data-icon="inline-start" />
                      {warning.code}
                    </span>
                  </OperatorCallout>
                )
              })}
            </div>
          ) : null}
        </>
      ) : null}
    </OperatorSectionCard>
  )
}
