import type { ComponentProps } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

type ButtonProps = ComponentProps<typeof Button>;

type IconActionGroupProps = ComponentProps<"div">;

interface IconActionButtonProps extends Omit<ButtonProps, "variant"> {
  destructive?: boolean;
}

export const iconActionButtonClassName =
  "shrink-0 rounded-md border border-transparent bg-surface text-muted-foreground transition-colors hover:border-outline-variant hover:bg-surface-container-low hover:text-foreground";

export function IconActionGroup({ className, ...props }: IconActionGroupProps) {
  return (
    <div
      data-slot="icon-action-group"
      className={cn(
        "inline-flex w-fit shrink-0 items-center gap-0.5 rounded-md border border-outline-variant bg-surface-container-low p-0.5",
        className
      )}
      {...props}
    />
  );
}

export function IconActionButton({
  className,
  destructive = false,
  size = "icon-sm",
  type = "button",
  ...props
}: IconActionButtonProps) {
  return (
    <Button
      type={type}
      variant="ghost"
      size={size}
      className={cn(
        iconActionButtonClassName,
        destructive && "text-destructive hover:border-destructive/20 hover:bg-destructive/10 hover:text-destructive",
        className
      )}
      {...props}
    />
  );
}
