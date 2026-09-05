import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

/**
 * 口径、原因、帮助说明的唯一实现。
 *
 * 这些说明是「诚实优于整洁」的载体，只挂在 `title` 或 `aria-hidden` 的字形上
 * 等于对键盘和触屏用户不存在。这个组件是 28×28 的真实按钮：可聚焦、可点开、
 * 有可访问名称，说明文本同时进 tooltip 与 `title`。
 */
export function OperatorHelpHint({
  className,
  label,
  side = "bottom",
  align = "start",
}: {
  /** 说明全文，同时作为按钮的可访问名称。 */
  label: string
  className?: string
  side?: "top" | "right" | "bottom" | "left"
  align?: "start" | "center" | "end"
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          aria-label={label}
          title={label}
          className={cn(
            "inline-flex size-7 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-primary-soft hover:text-on-primary-soft focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2",
            className,
          )}
        >
          <span aria-hidden="true">?</span>
        </button>
      </TooltipTrigger>
      <TooltipContent
        side={side}
        align={align}
        className="max-w-sm whitespace-normal text-left"
      >
        {label}
      </TooltipContent>
    </Tooltip>
  )
}
