import type { ComponentProps } from "react";
import { Children } from "react";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

type ButtonProps = ComponentProps<typeof Button>;

type IconActionGroupProps = ComponentProps<"div">;

interface IconActionButtonProps extends Omit<ButtonProps, "variant"> {
  destructive?: boolean;
}

export const iconActionButtonClassName =
  "shrink-0 rounded-md border border-transparent bg-panel text-muted-foreground transition-colors hover:border-border hover:bg-inset hover:text-foreground";

export function IconActionGroup({ className, ...props }: IconActionGroupProps) {
  return (
    <div
      data-slot="icon-action-group"
      className={cn(
        "inline-flex w-fit shrink-0 items-center gap-0.5 rounded-md border border-border bg-inset p-0.5",
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
  const button = (
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

  // 只有图标的按钮必须带 tooltip：这一组里往往夹着一个不可逆的删除，
  // 光看图标分不出来。children 恒为图标节点，不能拿它判断「有没有可见文本」——
  // 只有真的渲染了字符串才算有文本。
  const tooltipLabel = props["aria-label"];
  const hasVisibleText = Children.toArray(props.children).some(
    (child) => typeof child === "string" && child.trim() !== "",
  );
  if (hasVisibleText || !tooltipLabel) {
    return button;
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>{button}</TooltipTrigger>
      <TooltipContent>{tooltipLabel}</TooltipContent>
    </Tooltip>
  );
}
