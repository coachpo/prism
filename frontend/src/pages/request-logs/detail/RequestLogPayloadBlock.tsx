import { Copy } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { copyRequestLogText } from "./requestLogDetailUtils";

interface RequestLogPayloadBlockProps {
  title: string;
  content: string;
}

export function RequestLogPayloadBlock({ title, content }: RequestLogPayloadBlockProps) {
  const { messages } = useLocale();
  const hasContent = content.length > 0;

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-3">
        <p className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground">{title}</p>
        <Button
          variant="outline"
          size="sm"
          className="h-7 rounded-full px-2.5 text-[11px]"
          disabled={!hasContent}
          onClick={() => {
            if (hasContent) {
              void copyRequestLogText(content, title.toLowerCase());
            }
          }}
        >
          <Copy className="h-3 w-3" />
          {messages.requestLogs.copy}
        </Button>
      </div>
      <ScrollArea className="h-56 rounded-xl border border-zinc-800 bg-zinc-950 shadow-inner">
        <pre className="min-h-full whitespace-pre-wrap break-all p-3 text-[11px] leading-5 text-zinc-50">
          {hasContent ? content : messages.requestLogs.noCaptured(title.toLowerCase())}
        </pre>
      </ScrollArea>
    </div>
  );
}
