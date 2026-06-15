import { KeyRound } from "lucide-react"
import { useEffect } from "react"
import { CopyButton } from "@/components/CopyButton"
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
import { useLocale } from "@/i18n/useLocale"
import { OperatorInsetPanel } from "@/shared/design-system"

interface ProxyKeySecretDialogProps {
  secret: string | null
  onClear: () => void
}

export function ProxyKeySecretDialog({ secret, onClear }: ProxyKeySecretDialogProps) {
  const { messages } = useLocale()
  const copy = messages.proxyApiKeys

  useEffect(() => () => onClear(), [onClear])

  return (
    <Dialog
      open={secret !== null}
      onOpenChange={(open) => {
        if (!open) onClear()
      }}
    >
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <KeyRound className="text-primary" />
            {copy.newSecret}
          </DialogTitle>
          <DialogDescription>{copy.newSecretDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody>
          {secret ? (
            <OperatorInsetPanel
              data-testid="proxy-key-secret"
              className="flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"
            >
              <p className="min-w-0 break-all font-mono text-sm text-foreground">{secret}</p>
              <CopyButton
                value={secret}
                label={copy.copyKey}
                targetLabel={copy.apiKey}
                variant="outline"
                className="shrink-0"
              />
            </OperatorInsetPanel>
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClear}>
            {messages.common.close}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
