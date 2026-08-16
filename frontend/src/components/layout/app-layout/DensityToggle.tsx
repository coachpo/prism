import { Rows2, Rows3 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useLocale } from "@/i18n/useLocale";
import type { OperatorDensityMode } from "@/shared/design-system";

type Props = {
  mode: OperatorDensityMode;
  onToggle: () => void;
};

export function DensityToggle({ mode, onToggle }: Props) {
  const { messages } = useLocale();
  const copy = messages.shell;
  const label = mode === "compact" ? copy.densityCompact : copy.densityStandard;
  const Icon = mode === "compact" ? Rows3 : Rows2;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          onClick={onToggle}
          data-testid="header-density-toggle"
          data-density-mode={mode}
          aria-label={`${copy.densitySwitch}（${copy.density}：${label}）`}
          className="rounded-md text-muted-foreground hover:text-foreground"
        >
          <Icon aria-hidden="true" />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{`${copy.density}：${label}`}</TooltipContent>
    </Tooltip>
  );
}
