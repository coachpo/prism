import * as React from "react"
import { Label as LabelPrimitive } from "radix-ui"

import { getStaticMessages } from "@/i18n/staticMessages"
import { cn } from "@/lib/utils"

function Label({
  className,
  children,
  required = false,
  ...props
}: React.ComponentProps<typeof LabelPrimitive.Root> & { required?: boolean }) {
  return (
    <LabelPrimitive.Root
      data-slot="label"
      className={cn(
        "flex items-center gap-2 text-sm leading-none font-medium select-none group-data-[disabled=true]:pointer-events-none group-data-[disabled=true]:opacity-50 peer-disabled:cursor-not-allowed peer-disabled:opacity-50",
        className
      )}
      {...props}
    >
      {children}
      {required ? (
        <span className="-ml-1 text-destructive">
          <span aria-hidden="true">*</span>
          <span className="sr-only">{getStaticMessages().common.required}</span>
        </span>
      ) : null}
    </LabelPrimitive.Root>
  )
}

export { Label }
