import { AlertCircle, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";

interface RequestFocusBannerProps {
  requestId: string;
  onExit: () => void;
}

export function RequestFocusBanner({ requestId, onExit }: RequestFocusBannerProps) {
  const { messages } = useLocale();

  return (
    <div className="flex items-center gap-3 rounded-lg border border-blue-500/30 bg-blue-500/10 px-4 py-2.5">
      <AlertCircle className="h-4 w-4 shrink-0 text-blue-600 dark:text-blue-400" />
      <p className="min-w-0 flex-1 text-sm">
        {messages.requestLogs.viewingRequest(requestId)}
      </p>
      <Button variant="ghost" size="sm" className="h-7 gap-1.5 text-xs" onClick={onExit}>
        <X className="h-3 w-3" />
        {messages.requestLogs.exit}
      </Button>
    </div>
  );
}
