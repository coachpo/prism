import type { ReactNode } from "react"

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
import { cn } from "@/lib/utils"

export interface OperatorDestructiveDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  children?: ReactNode
  onConfirm: () => void | Promise<void>
  onCancel?: () => void
  cancelLabel: ReactNode
  confirmLabel: ReactNode
  confirming?: boolean
  confirmingLabel?: ReactNode
  confirmDisabled?: boolean
  cancelDisabled?: boolean
  showConfirmButton?: boolean
  showCloseButton?: boolean
  size?: "sm" | "md" | "lg"
  contentClassName?: string
  bodyClassName?: string
  footerClassName?: string
  confirmTestId?: string
}

export function OperatorDestructiveDialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  onConfirm,
  onCancel,
  cancelLabel,
  confirmLabel,
  confirming = false,
  confirmingLabel = confirmLabel,
  confirmDisabled = false,
  cancelDisabled = false,
  showConfirmButton = true,
  showCloseButton = false,
  size = "sm",
  contentClassName,
  bodyClassName,
  footerClassName,
  confirmTestId,
}: OperatorDestructiveDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent size={size} className={contentClassName} showCloseButton={showCloseButton}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description ? <DialogDescription>{description}</DialogDescription> : null}
        </DialogHeader>

        {children ? <DialogBody className={bodyClassName}>{children}</DialogBody> : null}

        <DialogFooter className={cn("sm:justify-between", footerClassName)}>
          <Button
            type="button"
            variant="outline"
            disabled={cancelDisabled}
            onClick={() => {
              if (onCancel) {
                onCancel()
              } else {
                onOpenChange(false)
              }
            }}
          >
            {cancelLabel}
          </Button>
          {showConfirmButton ? (
            <Button
              type="button"
              variant="destructive"
              disabled={confirmDisabled || confirming}
              aria-busy={confirming || undefined}
              data-testid={confirmTestId}
              onClick={() => void onConfirm()}
            >
              {confirming ? confirmingLabel : confirmLabel}
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
