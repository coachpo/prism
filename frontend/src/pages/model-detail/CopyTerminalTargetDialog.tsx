import { useMemo, useState } from "react"
import { Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useLocale } from "@/i18n/useLocale"
import type { ModelConfigListItem, OpenAITextCapability } from "@/lib/types"
import { OperatorCallout, OperatorStatusBadge, OperatorTypeBadge } from "@/shared/design-system"
import { classifyOpenAICoverage } from "@/pages/model-detail/classifyOpenAICoverage"

interface CopyTerminalTargetDialogProps {
  isOpen: boolean
  onOpenChange: (open: boolean) => void
  sourceModelConfigId: number
  sourceCapability: OpenAITextCapability | null
  destinationModels: ModelConfigListItem[]
  onCopy: (destinationModelConfigIds: number[], enableCopies: boolean) => Promise<void>
}

// CopyTerminalTargetDialog copies a Terminal Target to multiple same-family
// destination models in one transactional request. New Access Targets default
// to not participating in routing; the operator must opt in explicitly.
export function CopyTerminalTargetDialog({
  isOpen,
  onOpenChange,
  sourceModelConfigId,
  sourceCapability,
  destinationModels,
  onCopy,
}: CopyTerminalTargetDialogProps) {
  const { messages } = useLocale()
  const copy = messages.routing
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [enableCopies, setEnableCopies] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const destinations = useMemo(
    () => destinationModels.filter((model) => model.id !== sourceModelConfigId),
    [destinationModels, sourceModelConfigId],
  )

  const toggleModel = (modelId: number) => {
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(modelId)) {
        next.delete(modelId)
      } else {
        next.add(modelId)
      }
      return next
    })
  }

  const canSubmit = selected.size > 0 && !submitting

  const handleSubmit = async () => {
    if (!canSubmit) return
    setSubmitting(true)
    setError(null)
    try {
      await onCopy([...selected], enableCopies)
      setSelected(new Set())
      setEnableCopies(false)
      onOpenChange(false)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={isOpen} onOpenChange={(open) => { if (!open && !submitting) onOpenChange(false) }}>
      <DialogContent className="max-h-[90vh] overflow-hidden sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{copy.copyTargetTitle}</DialogTitle>
          <DialogDescription>{copy.copyTargetDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="min-h-0 flex-1 overflow-y-auto pr-1">
          {error ? <OperatorCallout intent="danger" description={error} /> : null}
          <div className="flex flex-col gap-2">
            {destinations.map((model) => {
              const preview = sourceCapability
                ? classifyOpenAICoverage(model.openai_accepted_format ?? "dual_native", sourceCapability)
                : null
              const disabled = preview?.coverage === "none"
              const checked = selected.has(model.id)
              const badgeIntent = preview?.coverage === "full" ? "success" : preview?.coverage === "partial" ? "warning" : "danger"
              const badgeLabel = preview?.coverage === "full"
                ? copy.coverageFull
                : preview?.coverage === "partial"
                  ? copy.coveragePartial
                  : copy.coverageNone
              return (
                <label
                  key={model.id}
                  className={`flex items-start justify-between gap-3 rounded-md border px-3 py-2.5 ${checked ? "border-primary/40 bg-surface-container-low" : "border-outline-variant bg-background"} ${disabled ? "opacity-60" : ""}`}
                  data-testid={`copy-destination-${model.id}`}
                >
                  <div className="flex min-w-0 flex-col gap-1">
                    <span className="truncate text-sm font-medium">
                      {model.display_name || model.model_id}
                    </span>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <OperatorTypeBadge intent={model.is_enabled ? "success" : "muted"} label={model.is_enabled ? copy.destinationEnabled : copy.destinationDisabled} preserveLabel />
                      {preview ? (
                        <OperatorStatusBadge intent={badgeIntent} label={badgeLabel} preserveLabel />
                      ) : null}
                      {preview && preview.coverage === "none" ? (
                        <span className="text-xs text-muted-foreground">{copy.noneCapabilityReason}</span>
                      ) : null}
                      {preview && preview.coverage === "partial" ? (
                        <span className="text-xs text-muted-foreground">{copy.missingOperations(preview.unsupportedAcceptedOperations.join("、"))}</span>
                      ) : null}
                    </div>
                  </div>
                  <Switch checked={checked} disabled={disabled || submitting} onCheckedChange={() => toggleModel(model.id)} aria-label={copy.copyDestinationToggle(model.display_name || model.model_id)} />
                </label>
              )
            })}
          </div>

          <div className="mt-4 flex flex-col gap-2 rounded-md border border-outline-variant bg-surface-container-low px-3 py-2.5">
            <div className="flex items-center justify-between gap-3">
              <div className="flex flex-col gap-0.5">
                <Label>{copy.enableCopiesLabel}</Label>
                <p className="text-xs text-muted-foreground">{copy.enableCopiesDescription}</p>
              </div>
              <Switch checked={enableCopies} onCheckedChange={setEnableCopies} aria-label={copy.enableCopiesLabel} />
            </div>
            <p className="text-xs text-muted-foreground">
              {enableCopies ? copy.enableCopiesImpact : copy.noTrafficImpact}
            </p>
          </div>
        </DialogBody>
        <DialogFooter className="sm:justify-between">
          <Button type="button" variant="outline" disabled={submitting} onClick={() => onOpenChange(false)}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button type="button" disabled={!canSubmit} aria-busy={submitting} onClick={() => void handleSubmit()}>
            {submitting ? <Loader2 data-icon="inline-start" className="animate-spin" /> : null}
            {copy.copyTargets(selected.size)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
