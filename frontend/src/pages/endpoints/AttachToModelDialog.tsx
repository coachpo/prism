import { useEffect, useState } from "react"
import { Loader2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useLocale } from "@/i18n/useLocale"
import { api } from "@/lib/api"
import type { Endpoint, ModelConfigListItem } from "@/lib/types"
import { OperatorCallout } from "@/shared/design-system"

type AttachToModelDialogProps = {
  endpoint: Endpoint | null
  onOpenChange: (open: boolean) => void
  onNavigate: (modelId: number) => void
  onCreateModel: () => void
}

export function AttachToModelDialog({ endpoint, onOpenChange, onNavigate, onCreateModel }: AttachToModelDialogProps) {
  const { messages } = useLocale()
  const copy = messages.endpointsPage
  const [models, setModels] = useState<ModelConfigListItem[] | null>(null)
  const [loadError, setLoadError] = useState(false)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    if (!endpoint) return
    let cancelled = false
    void api.models.list()
      .then((items) => { if (!cancelled) setModels(items) })
      .catch(() => { if (!cancelled) setLoadError(true) })
    return () => { cancelled = true }
  }, [endpoint, attempt])

  return (
    <Dialog open={Boolean(endpoint)} onOpenChange={(nextOpen) => { if (!nextOpen) onOpenChange(false) }}>
      <DialogContent size="sm">
        <DialogHeader>
          <DialogTitle>{copy.attachToModel}</DialogTitle>
          <DialogDescription>{endpoint ? `${endpoint.name} — ${copy.attachDescription}` : ""}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex min-h-0 flex-col gap-2 overflow-y-auto">
          {loadError ? (
            <OperatorCallout intent="warning" role="alert" description={copy.loadModelsFailed} action={<Button type="button" variant="outline" onClick={() => { setLoadError(false); setAttempt((value) => value + 1) }}>{messages.endpointsUi.deleteRetry}</Button>} />
          ) : models === null ? (
            <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />{copy.loadingModels}</p>
          ) : models.length === 0 ? (
            <p className="text-sm text-muted-foreground">{copy.attachEmpty}</p>
          ) : models.map((model) => (
            <button key={model.id} type="button" data-testid={`attach-model-option-${model.id}`} className="flex min-w-0 flex-col gap-0.5 rounded-lg border border-border px-3 py-2 text-left hover:bg-inset" onClick={() => { onNavigate(model.id); onOpenChange(false) }}>
              <span className="truncate text-sm font-medium text-foreground">{model.display_name || model.model_id}</span>
              <span className="truncate font-mono text-xs text-muted-foreground">{model.model_id}</span>
            </button>
          ))}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>{messages.endpointsUi.returnToServices}</Button>
          {models !== null && !loadError ? <Button type="button" onClick={onCreateModel}>{copy.createModel}</Button> : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
