import type { ConnectionState } from "@/lib/websocket";
import { getStaticMessages } from "@/i18n/staticMessages";
import { StatusDot, type StatusDotIntent } from "@/components/ui/status-dot";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

interface WebSocketStatusIndicatorProps {
  connectionState: ConnectionState;
  isSyncing?: boolean;
}

export function WebSocketStatusIndicator({
  connectionState,
  isSyncing = false,
}: WebSocketStatusIndicatorProps) {
  const copy = getStaticMessages().common;
  const statusStyles: Record<
    ConnectionState,
    { intent: StatusDotIntent; animated: boolean; label: string }
  > = {
    connected: {
      intent: "healthy",
      animated: true,
      label: copy.connected,
    },
    connecting: {
      intent: "downgrade",
      animated: true,
      label: copy.connecting,
    },
    reconnecting: {
      intent: "downgrade",
      animated: true,
      label: copy.reconnecting,
    },
    disconnected: {
      intent: "muted",
      animated: false,
      label: copy.disconnected,
    },
  };
  const status = isSyncing
    ? { intent: "info" as const, animated: true, label: copy.syncing }
    : statusStyles[connectionState];

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <StatusDot intent={status.intent} animated={status.animated} />
        </TooltipTrigger>
        <TooltipContent>
          <p>{status.label}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
