import type { ComponentProps, ReactNode } from "react"
import { SearchIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Field, FieldContent, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { cn } from "@/lib/utils"

export type OperatorToolbarProps = ComponentProps<"div"> & {
  align?: "start" | "between"
}

export function OperatorToolbar({
  align = "between",
  children,
  className,
  ...props
}: OperatorToolbarProps) {
  return (
    <div
      className={cn(
        "operator-section-surface flex min-w-0 flex-col gap-3 rounded-lg border p-[var(--density-card-pad-x)] sm:flex-row sm:items-center",
        align === "between" ? "sm:justify-between" : "sm:justify-start",
        className,
      )}
      {...props}
    >
      {children}
    </div>
  )
}

export type OperatorSearchInputProps = ComponentProps<typeof Input> & {
  wrapperClassName?: string
}

export function OperatorSearchInput({
  className,
  wrapperClassName,
  ...props
}: OperatorSearchInputProps) {
  return (
    <div className={cn("relative w-full min-w-0 xl:max-w-sm", wrapperClassName)}>
      <SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input type="search" className={cn("pl-9", className)} {...props} />
    </div>
  )
}

export type OperatorSwitchFieldProps = {
  label: string
  description?: ReactNode
  checked?: boolean
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
  className?: string
}

export function OperatorSwitchField({
  checked,
  className,
  description,
  disabled,
  label,
  onCheckedChange,
}: OperatorSwitchFieldProps) {
  return (
    <Field
      orientation="horizontal"
      data-disabled={disabled ? true : undefined}
      className={cn("items-center justify-between rounded-lg border p-3", className)}
    >
      <FieldContent>
        <FieldLabel>{label}</FieldLabel>
        {description ? <FieldDescription>{description}</FieldDescription> : null}
      </FieldContent>
      <Switch
        checked={checked ?? false}
        onCheckedChange={onCheckedChange}
        disabled={disabled}
        aria-label={label}
      />
    </Field>
  )
}

export type OperatorIconButtonProps = ComponentProps<typeof Button>

export function OperatorIconButton({ className, size = "icon-sm", variant = "ghost", ...props }: OperatorIconButtonProps) {
  return (
    <Button
      size={size}
      variant={variant}
      className={cn("shrink-0 rounded-full text-muted-foreground hover:text-foreground", className)}
      {...props}
    />
  )
}
