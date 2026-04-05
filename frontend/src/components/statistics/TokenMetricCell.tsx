import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";

interface TokenMetricCellProps {
  value: number | null | undefined;
  nullReason: string;
  formatValue: (value: number) => string;
  className?: string;
}

export function TokenMetricCell({
  value,
  nullReason,
  formatValue,
  className,
}: TokenMetricCellProps) {
  const { messages } = useLocale();
  if (value === null || value === undefined) {
    if (!nullReason) {
      return <span className={cn("text-muted-foreground/80", className)}>{messages.common.notApplicable}</span>;
    }

    return (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <span className={cn("cursor-help text-muted-foreground/80", className)}>{messages.common.notApplicable}</span>
          </TooltipTrigger>
          <TooltipContent side="top" className="max-w-xs">
            {nullReason}
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    );
  }

  return <span className={cn("tabular-nums", className)}>{formatValue(value)}</span>;
}
