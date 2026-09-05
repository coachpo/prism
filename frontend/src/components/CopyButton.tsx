import type { ComponentProps } from "react";
import { Copy } from "lucide-react";
import { toast } from "sonner";
import { getStaticMessages } from "@/i18n/staticMessages";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { copyTextToClipboard } from "@/lib/clipboard";

type ButtonProps = ComponentProps<typeof Button>;

interface CopyButtonProps extends Omit<ButtonProps, "onClick"> {
  value: string;
  label?: string;
  targetLabel?: string;
  successMessage?: string;
  errorMessage?: string;
  stopPropagation?: boolean;
}

export function CopyButton({
  value,
  label,
  targetLabel,
  successMessage,
  errorMessage,
  stopPropagation = false,
  type = "button",
  variant = "ghost",
  size = "sm",
  children,
  ...props
}: CopyButtonProps) {
  const messages = getStaticMessages();
  const resolvedLabel = label ?? messages.common.copy;
  const resolvedTargetLabel = targetLabel ?? resolvedLabel;

  const handleCopy = async () => {
    const copied = await copyTextToClipboard(value);
    if (!copied) {
      toast.error(errorMessage ?? messages.common.copyFailed(resolvedTargetLabel));
      return;
    }
    toast.success(successMessage ?? messages.common.copiedToClipboard(resolvedTargetLabel));
  };

  const button = (
    <Button
      type={type}
      variant={variant}
      size={size}
      onClick={(event) => {
        if (stopPropagation) {
          event.stopPropagation();
        }
        void handleCopy();
      }}
      {...props}
    >
      {children ?? (
        <>
          <Copy className="h-3.5 w-3.5" />
          {resolvedLabel ? <span>{resolvedLabel}</span> : null}
        </>
      )}
    </Button>
  );

  // 只有图标的按钮必须带 tooltip：新运维否则只能靠点一下才知道它做什么。
  const tooltipLabel = props["aria-label"];
  if (resolvedLabel || children || !tooltipLabel) {
    return button;
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>{button}</TooltipTrigger>
      <TooltipContent>{tooltipLabel}</TooltipContent>
    </Tooltip>
  );
}
