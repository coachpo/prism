import type { HTMLAttributes } from "react";
import { cn } from "@/lib/utils";

interface AnimatedListItemProps extends HTMLAttributes<HTMLDivElement> {
  isNew?: boolean;
  animation?: "top" | "left";
}

export function AnimatedListItem({
  isNew = false,
  animation = "top",
  className,
  ...props
}: AnimatedListItemProps) {
  return (
    <div
      className={cn(
        isNew && (animation === "left" ? "ws-new-row-left" : "ws-new-row"),
        className
      )}
      {...props}
    />
  );
}
