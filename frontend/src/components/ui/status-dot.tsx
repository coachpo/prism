import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const statusDotVariants = cva("inline-flex size-2.5 shrink-0 rounded-full", {
  variants: {
    intent: {
      primary: "bg-primary/70",
      healthy: "bg-healthy",
      downgrade: "bg-downgrade",
      unhealthy: "bg-unhealthy",
      success: "bg-success",
      warning: "bg-warning",
      danger: "bg-destructive",
      info: "bg-info",
      muted: "bg-muted-foreground/50",
    },
    animated: {
      true: "animate-pulse",
      false: "",
    },
  },
  defaultVariants: {
    intent: "muted",
    animated: false,
  },
})

type StatusDotVariantProps = VariantProps<typeof statusDotVariants>

export type StatusDotIntent = NonNullable<StatusDotVariantProps["intent"]>

function StatusDot({
  className,
  intent,
  animated,
  ...props
}: React.ComponentProps<"span"> & StatusDotVariantProps) {
  return (
    <span
      data-slot="status-dot"
      data-intent={intent ?? undefined}
      data-animated={animated ? "true" : "false"}
      className={cn(statusDotVariants({ intent, animated }), className)}
      {...props}
    />
  )
}

export { StatusDot }
