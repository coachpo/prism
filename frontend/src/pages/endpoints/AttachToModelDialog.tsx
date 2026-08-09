import { useEffect, useRef, useState } from "react"
import { Loader2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useLocale } from "@/i18n/useLocale"
import { api } from "@/lib/api"
import type { Endpoint, ModelConfigListItem } from "@/lib/types"

type AttachToModelDialogProps = {
  endpoint: Endpoint | null
  onOpenChange: (open: boolean) => void
  onNavigate: (modelId: number) => void
}

export function AttachToModelDialog({ endpoint, onOpenChange, onNavigate }: AttachToModelDialogProps) {
  const { messages } = useLocale()
  const copy = messages.endpointsPage
  const [models, setModels] = useState<ModelConfigListItem[] | null>(null)
  const [loadError, setLoadError] = useState(false)
  const requestedEndpointRef = useRef<number | null>(null)

  // The parent remounts this dialog with key={endpoint?.id}, so state is
  // already fresh per Endpoint; this effect only performs the async fetch.
  useEffect(() => {
    if (!endpoint) return
    const endpointId = endpoint.id
    requestedEndpointRef.current = endpointId
    let cancelled = false
    void api.models.list()
      .then((items) => {
        if (!cancelled && requestedEndpointRef.current === endpointId) setModels(items)
      })
      .catch(() => {
        if (!cancelled && requestedEndpointRef.current === endpointId) setLoadError(true)
      })
    return () => {
      cancelled = true
    }
  }, [endpoint])

  const open = Boolean(endpoint)

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onOpenChange(false) }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{copy.attachToModel}</DialogTitle>
          <DialogDescription>
            {endpoint ? `${endpoint.name} — ${copy.description}` : ""}
          </DialogDescription>
        </DialogHeader>
        <DialogBody className="flex max-h-[60vh] min-h-0 flex-col gap-2 overflow-y-auto">
          {loadError ? (
            <p className="text-sm text-destructive">{messages.endpointsData.loadFailed}</p>
          ) : models === null ? (
            <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" />
              {copy.searchEndpoints}
            </p>
          ) : models.length === 0 ? (
            <p className="text-sm text-muted-foreground">{messages.modelsUi.noModelsConfigured}</p>
          ) : (
            models.map((model) => (
              <button
                key={model.id}
                type="button"
                data-testid={`attach-model-option-${model.id}`}
                className="flex min-w-0 flex-col gap-0.5 rounded-lg border border-outline-variant px-3 py-2 text-left hover:bg-surface-container-low"
                onClick={() => {
                  onNavigate(model.id)
                  onOpenChange(false)
                }}
              >
                <span className="truncate text-sm font-medium text-foreground">{model.display_name || model.model_id}</span>
                <span className="truncate font-mono text-[11px] text-muted-foreground">{model.model_id} · {model.api_family}</span>
              </button>
            ))
          )}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {messages.settingsDialogs.cancel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
