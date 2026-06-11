import type { ComponentProps, ReactNode } from "react"
import { CommandIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  CommandDialog,
  CommandEmpty,
  CommandInput,
  CommandList,
} from "@/components/ui/command"
import { cn } from "@/lib/utils"

type OperatorCommandPaletteProps = ComponentProps<typeof CommandDialog> & {
  searchPlaceholder?: string
  emptyLabel?: string
  children: ReactNode
}
export function OperatorCommandPalette({
  searchPlaceholder = "Search commands, routes, and operations",
  emptyLabel = "No matching operator command.",
  children,
  ...props
}: OperatorCommandPaletteProps) {
  return (
    <CommandDialog {...props}>
      <CommandInput placeholder={searchPlaceholder} />
      <CommandList>
        <CommandEmpty>{emptyLabel}</CommandEmpty>
        {children}
      </CommandList>
    </CommandDialog>
  )
}

export function OperatorCommandPaletteTrigger({
  className,
  children = "Command",
  ...props
}: ComponentProps<typeof Button>) {
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      data-testid="command-palette-trigger"
      className={cn("operator-command-trigger", className)}
      {...props}
    >
      <CommandIcon data-icon="inline-start" />
      {children}
    </Button>
  )
}

