import type { ComponentProps } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

type ButtonProps = ComponentProps<typeof Button>;

type IconActionGroupProps = ComponentProps<"div">;

interface IconActionButtonProps extends Omit<ButtonProps, "variant"> {
  destructive?: boolean;
}

export const iconActionButtonClassName =
  "h-8 w-8 shrink-0 rounded-full border border-transparent bg-background/70 text-muted-foreground transition-colors hover:border-border hover:bg-background hover:text-foreground";

export function IconActionGroup({ className, ...props }: IconActionGroupProps) {
  return (
    <div
      className={cn(
        "flex shrink-0 items-center gap-0.5 rounded-full border border-border/70 bg-muted/35 p-0.5",
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
