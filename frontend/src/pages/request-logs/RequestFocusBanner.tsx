import { AlertCircle, X } from "lucide-react";
import { SemanticCallout } from "@/components/SemanticCallout";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";

interface RequestFocusBannerProps {
  requestId: string;
  onExit: () => void;
}

export function RequestFocusBanner({ requestId, onExit }: RequestFocusBannerProps) {
  const { messages } = useLocale();

  return (
    <SemanticCallout
      intent="info"
      icon={<AlertCircle />}
      description={messages.requestLogs.viewingRequest(requestId)}
      className="py-2.5"
      action={(
        <Button variant="ghost" size="sm" className="h-7 gap-1.5 text-xs" onClick={onExit}>
          <X data-icon="inline-start" />
          {messages.requestLogs.exit}
        </Button>
      )}
    />
  );
}
