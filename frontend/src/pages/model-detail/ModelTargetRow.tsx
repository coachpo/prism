import { ArrowDown, ArrowUp, GitBranch, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { useLocale } from "@/i18n/useLocale"
import type { DiagnosticsTarget, ModelAccessTargetMutation, ModelConfigListItem } from "@/lib/types"
import { OperatorStatusBadge, OperatorTypeBadge } from "@/shared/design-system"

interface ModelTargetRowProps {
  stagePosition: number
  target: ModelAccessTargetMutation
  diagnosticsTarget: DiagnosticsTarget | null
  modelOptions: ModelConfigListItem[]
  truncatedBySingle: boolean
  busy: boolean
  disabled?: boolean
  canMoveUp: boolean
  canMoveDown: boolean
  onToggle?: (enabled: boolean) => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  onDelete?: () => void
}

function resolveModelLabel(targetModelId: string | undefined, modelOptions: ModelConfigListItem[]) {
  const model = modelOptions.find((candidate) => candidate.model_id === targetModelId)
  return model?.display_name ? `${model.display_name} (${model.model_id})` : (targetModelId ?? "")
}

// ModelTargetRow renders one Model Target row. It never fabricates a direct
// capability for the child model; recursive coverage comes from diagnostics
// and the row links to "进入目标模型继续解析".
export function ModelTargetRow({
  stagePosition,
  target,
  diagnosticsTarget,
  modelOptions,
  truncatedBySingle,
  busy,
  disabled = false,
  canMoveUp,
  canMoveDown,
  onToggle,
  onMoveUp,
  onMoveDown,
  onDelete,
}: ModelTargetRowProps) {
  const { messages } = useLocale()
  const copy = messages.routing
  const detailCopy = messages.modelDetail
  const label = resolveModelLabel(target.target_model_id ?? undefined, modelOptions)
  const rowDisposition = diagnosticsTarget?.operation_results[0]?.disposition
  const dispositionLabel =
    rowDisposition === "candidate"
      ? copy.entersChildModel
      : rowDisposition === "no_eligible_leaf"
        ? copy.childNoEligibleLeaf
        : null

  return (
    <div
      data-testid={`model-target-row-${target.target_model_id ?? "unknown"}`}
      className="flex flex-col gap-3 rounded-md border border-border bg-panel p-3 sm:flex-row sm:items-center sm:justify-between"
    >
      <div className="flex min-w-0 flex-1 items-start gap-3">
        <div className="flex size-[var(--density-control-h-sm)] shrink-0 items-center justify-center rounded-md border border-border bg-inset text-muted-foreground">
          <GitBranch />
        </div>
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium">{label}</p>
          <div className="mt-1 flex flex-wrap items-center gap-1.5">
            <OperatorTypeBadge intent="muted" label={`${copy.modelTarget} · ${copy.stagePosition(String(stagePosition))}`} preserveLabel />
            {truncatedBySingle ? (
              <OperatorStatusBadge intent="degraded" label={copy.truncatedBySingle} preserveLabel />
            ) : null}
            {dispositionLabel ? (
              <span className="text-xs text-muted-foreground">{dispositionLabel}</span>
            ) : null}
            {target.is_enabled === false ? (
              <OperatorStatusBadge intent="neutral" label={detailCopy.disabled} preserveLabel />
            ) : null}
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-end gap-2">
        <Switch
          checked={target.is_enabled !== false}
          disabled={disabled || busy}
          onCheckedChange={(checked) => onToggle?.(checked)}
          aria-label={copy.enableAccessTarget(copy.stagePosition(String(stagePosition)))}
        />
        <Button
          type="button"
          variant="outline"
          size="icon-sm"
          aria-label={copy.targetMoveUp(label)}
          disabled={disabled || busy || !canMoveUp}
          onClick={onMoveUp}
        >
          <ArrowUp />
        </Button>
        <Button
          type="button"
          variant="outline"
          size="icon-sm"
          aria-label={copy.targetMoveDown(label)}
          disabled={disabled || busy || !canMoveDown}
          onClick={onMoveDown}
        >
          <ArrowDown />
        </Button>
        <Button
          type="button"
          variant="outline"
          size="icon-sm"
          aria-label={copy.targetRemove(label)}
          disabled={disabled || busy}
          onClick={onDelete}
        >
          <Trash2 />
        </Button>
      </div>
    </div>
  )
}
