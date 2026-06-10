import type { MouseEvent } from "react";
import { Copy } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import type { ApiFamily } from "@/lib/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { copyRequestLogText } from "./requestLogDetailUtils";
import {
  buildRequestLogPayloadDocument,
  type RequestLogPayloadBodyKind,
  type RequestLogPayloadDocument,
} from "./requestLogPayloadDocuments";

interface RequestLogPayloadBlockProps {
  title: string;
  content: string;
  emptyState?: string;
  apiFamily?: ApiFamily | null;
  bodyKind?: RequestLogPayloadBodyKind;
}

function RequestLogPayloadDocumentView({ document }: { document: RequestLogPayloadDocument }) {
  return (
    <div className="flex min-h-full flex-col gap-3 p-3">
      {document.sections.map((section) => (
        <section key={section.title} className="rounded-lg border border-border/70 bg-background/80 p-3 shadow-sm">
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <Badge variant="secondary" className="rounded-md px-2 py-0.5 text-[11px] font-medium">
              {section.title}
            </Badge>
          </div>
          <div className="flex flex-col gap-3">
            {section.lines.map((line, index) => (
              <div key={`${line.label}-${index}`} className="grid gap-1.5 sm:grid-cols-[8rem_minmax(0,1fr)] sm:gap-3">
                <p className="min-w-0 font-mono text-[11px] font-medium uppercase tracking-[0.12em] text-muted-foreground">
                  {line.label}
                </p>
                <p
                  className={line.mono
                    ? "min-w-0 whitespace-pre-wrap break-words font-mono text-[11px] leading-5 text-foreground [overflow-wrap:anywhere]"
                    : "min-w-0 whitespace-pre-wrap break-words text-sm leading-6 text-foreground [overflow-wrap:anywhere]"
                  }
                >
                  {line.value}
                </p>
              </div>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}

export function RequestLogPayloadBlock({
  title,
  content,
  emptyState,
  apiFamily,
  bodyKind,
}: RequestLogPayloadBlockProps) {
  const { messages } = useLocale();
  const hasContent = content.length > 0;
  const document = hasContent && apiFamily && bodyKind
    ? buildRequestLogPayloadDocument({ apiFamily, bodyKind, content })
    : null;

  const handleCopy = (event: MouseEvent<HTMLButtonElement>) => {
    if (!hasContent) return;

    const container = event.currentTarget.closest("[data-clipboard-fallback-root]") as HTMLElement | null;
    void copyRequestLogText(content, title.toLowerCase(), container);
  };

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex min-w-0 items-center justify-between gap-3">
        <p className="min-w-0 truncate text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">{title}</p>
        <Button
          variant="outline"
          size="sm"
          className="h-7 rounded-full px-2.5 text-[11px]"
          disabled={!hasContent}
          onClick={handleCopy}
        >
          <Copy data-icon="inline-start" />
          {messages.requestLogs.copy}
        </Button>
      </div>
      <ScrollArea className="h-56 rounded-xl border border-border/70 bg-muted/45 shadow-inner">
        {document ? (
          <RequestLogPayloadDocumentView document={document} />
        ) : (
          <pre className="min-h-full max-w-full whitespace-pre-wrap break-words p-3 font-mono text-[11px] leading-5 text-foreground [overflow-wrap:anywhere]">
            {hasContent ? content : emptyState ?? messages.requestLogs.noCaptured(title.toLowerCase())}
          </pre>
        )}
      </ScrollArea>
    </div>
  );
}
