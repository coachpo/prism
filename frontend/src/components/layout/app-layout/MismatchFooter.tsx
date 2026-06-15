import { AlertTriangle, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";

type Props = {
  activeProfileName: string;
  hasMismatch: boolean;
  isActivating: boolean;
  openActivateDialog: () => void;
  selectedProfileName: string;
};

export function MismatchFooter({
  activeProfileName,
  hasMismatch,
  isActivating,
  openActivateDialog,
  selectedProfileName,
}: Props) {
  const { messages } = useLocale();

  if (!hasMismatch) {
    return null;
  }

  return (
    <div className="flex items-center gap-2 rounded-lg border border-warning/35 bg-warning/10 p-2 text-warning-foreground group-data-[collapsible=icon]:hidden">
      <AlertTriangle className="text-warning-foreground/80" />
      <p className="min-w-0 flex-1 truncate text-xs font-medium">
        {selectedProfileName} · {messages.shell.runningShort(activeProfileName)}
      </p>
      <Button size="sm" onClick={openActivateDialog} disabled={isActivating}>
          {isActivating ? (
            <>
              <Loader2 data-icon="inline-start" className="animate-spin" />
              {messages.shell.activating}
            </>
          ) : (
            messages.shell.activate
          )}
      </Button>
    </div>
  );
}
