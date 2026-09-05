import { AlertTriangle } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { formatApiFamily } from "@/components/apiFamilyPresentation"
import { useLocale } from "@/i18n/useLocale"
import type { Messages } from "@/i18n/messages"
import type { EndpointReferenceItem } from "@/lib/types"

type EndpointsCopy = Messages["endpointsUi"]

export type OrphanCleanupEndpoint = { id: number; name: string; base_url: string }

type OrphanCleanupDialogProps = {
  target: { endpoint: OrphanCleanupEndpoint; item: EndpointReferenceItem } | null
  onConfirm: (endpoint: OrphanCleanupEndpoint, item: EndpointReferenceItem) => void
  onOpenChange: (open: boolean) => void
}

function textCapabilityLabel(copy: EndpointsCopy, capability: string): string {
  switch (capability) {
    case "dual_native":
      return copy.protocolCapabilityDual
    case "chat_completions_only":
      return copy.protocolCapabilityChatCompletionsOnly
    case "responses_only":
      return copy.protocolCapabilityResponsesOnly
    default:
      return copy.protocolCapabilityUnknown
  }
}

function describeConnection(copy: EndpointsCopy, item: EndpointReferenceItem): string {
  const parts = [formatApiFamily(item.api_family)]
  if (item.openai_text_capability) {
    parts.push(textCapabilityLabel(copy, item.openai_text_capability))
  }
  parts.push(
    item.connection_is_active
      ? copy.connectionActiveLabel
      : copy.connectionInactiveLabel,
  )
  return parts.join(" · ")
}

export function OrphanCleanupDialog({ target, onConfirm, onOpenChange }: OrphanCleanupDialogProps) {
  const { messages } = useLocale()
  const copy = messages.endpointsUi
  const open = Boolean(target)
  const item = target?.item

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onOpenChange(false) }}>
      <DialogContent size="sm">
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
                {/* 枚举键与英文状态词都不能直接上屏：api_family 走展示函数，
                    文本能力与激活状态各自过中文字典（未知值有具名兜底）。 */}
                <span>{describeConnection(copy, item)}</span>
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
