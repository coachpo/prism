import type { ReactNode } from "react"
import { useLocale } from "@/i18n/useLocale"
import { OperatorCallout, OperatorStatusBadge } from "@/shared/design-system"

interface AccessTargetStageSectionProps {
  stage: "model_targets" | "terminal_targets"
  children: ReactNode
  truncatedBySingle: boolean
  emptyState?: ReactNode
}

// AccessTargetStageSection renders one of the two routing stages with its
// explicit ordering semantics. Stage-local numbering is the only position
// vocabulary; the two stages are never presented as a flat mixed list.
export function AccessTargetStageSection({
  stage,
  children,
  truncatedBySingle,
  emptyState,
}: AccessTargetStageSectionProps) {
  const { messages } = useLocale()
  const copy = messages.routing
  const isModelStage = stage === "model_targets"
  const title = isModelStage ? copy.modelStageTitle : copy.terminalStageTitle
  const description = isModelStage ? copy.modelStageDescription : copy.terminalStageDescription

  return (
    <section
      aria-labelledby={`access-target-stage-${stage}`}
      className="flex flex-col gap-3"
      data-testid={`access-target-stage-${stage}`}
    >
      <div className="flex flex-col gap-1">
        <p id={`access-target-stage-${stage}`} className="text-sm font-medium text-foreground">
          {title}
        </p>
        <p className="text-sm text-muted-foreground" aria-describedby={`access-target-stage-${stage}`}>
          {description}
        </p>
      </div>
      {truncatedBySingle ? (
        <OperatorCallout intent="warning" description={copy.singleTruncatesStage(isModelStage ? "模型目标" : "终端目标")}>
          <OperatorStatusBadge intent="warning" label={copy.truncatedBySingle} preserveLabel />
        </OperatorCallout>
      ) : null}
      <div className="flex flex-col gap-2">{children}</div>
      {emptyState}
    </section>
  )
}
