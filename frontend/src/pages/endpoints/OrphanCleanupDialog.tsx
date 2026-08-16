import { AlertTriangle } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useLocale } from "@/i18n/useLocale"
import type { EndpointReferenceItem } from "@/lib/types"

export type OrphanCleanupEndpoint = { id: number; name: string; base_url: string }

type OrphanCleanupDialogProps = {
  target: { endpoint: OrphanCleanupEndpoint; item: EndpointReferenceItem } | null
  onConfirm: (endpoint: OrphanCleanupEndpoint, item: EndpointReferenceItem) => void
  onOpenChange: (open: boolean) => void
}

export function OrphanCleanupDialog({ target, onConfirm, onOpenChange }: OrphanCleanupDialogProps) {
  const { messages } = useLocale()
  const copy = messages.endpointsUi
  const open = Boolean(target)
  const item = target?.item

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onOpenChange(false) }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{copy.orphanCleanup}</DialogTitle>
          <DialogDescription>{target ? copy.orphanCleanupDescription : ""}</DialogDescription>
        </DialogHeader>
        <DialogBody>
          {item ? (
            <div className="flex flex-col gap-3 rounded-lg border border-destructive/25 bg-destructive/5 p-4">
              <p className="text-sm font-medium text-foreground">
                {copy.orphanRowLabel(String(item.connection_id))}
              </p>
              <p className="text-xs text-muted-foreground">
                {copy.orphanCleanupConfirm(target?.endpoint.name ?? "", String(item.connection_id))}
              </p>
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <AlertTriangle className="size-3.5" />
                <span>{item.api_family}{item.openai_text_capability ? ` · ${item.openai_text_capability}` : ""}{item.connection_is_active ? " · active" : " · inactive"}</span>
              </div>
            </div>
          ) : null}
        </DialogBody>
        <DialogFooter className="sm:justify-between">
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button
            variant="destructive"
            disabled={!target}
            data-testid="orphan-cleanup-confirm"
            onClick={() => {
              if (!target) return
              onConfirm(target.endpoint, target.item)
              onOpenChange(false)
            }}
          >
            {copy.orphanCleanup}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
