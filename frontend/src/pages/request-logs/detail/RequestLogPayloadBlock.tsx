import { useId, useMemo, type MouseEvent } from "react";
import { Copy } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import type { ApiFamily } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { copyRequestLogText } from "./requestLogClipboard";
import { payloadRoleLabel, type RequestLogPayloadBodyKind } from "./requestLogPayloadDocuments";
import { buildPayloadViewModel } from "./payloadDocumentViewModel";

interface RequestLogPayloadBlockProps {
  title: string;
  content: string;
  emptyState?: string;
  apiFamily?: ApiFamily | null;
  bodyKind?: RequestLogPayloadBodyKind;
  contentKind?: "headers" | "payload";
  operationName?: string | null;
}

export function RequestLogPayloadBlock({ title, content, emptyState, apiFamily, bodyKind, contentKind = "payload", operationName = null }: RequestLogPayloadBlockProps) {
  const { messages } = useLocale();
  const copy = messages.requestLogs;
  const headingId = useId();
  const model = useMemo(() => contentKind === "headers" ? null : buildPayloadViewModel(content, apiFamily ?? "openai", bodyKind ?? "response", operationName), [content, contentKind, apiFamily, bodyKind, operationName]);
  const turns = model?.transcript?.turns.filter(turn => turn.text || turn.toolCalls.length || turn.toolResults.length) ?? [];
  const readableContent = turns.map(turn => `${payloadRoleLabel(turn.role)}\n${turn.text}`).filter(Boolean).join("\n\n");
  const handleCopy = (event: MouseEvent<HTMLButtonElement>) => {
    const container = event.currentTarget.closest("[data-clipboard-fallback-root]") as HTMLElement | null;
    void copyRequestLogText(readableContent, title, container);
  };

  return (
    <section className="flex min-w-0 flex-col gap-3" aria-labelledby={headingId}>
      <div className="flex items-center justify-between gap-3">
        <h3 id={headingId} className="text-sm font-semibold">{title}</h3>
        {turns.some(turn => turn.text) ? <Button variant="outline" size="sm" onClick={handleCopy}><Copy data-icon="inline-start" />{copy.copy}</Button> : null}
      </div>
      {model?.hasIncompleteTail ? <p className="text-xs text-degraded">{copy.streamIncompleteNote}</p> : null}
      <div className="min-w-0 max-h-[30rem] overflow-y-auto flex flex-col gap-3" data-testid={`request-log-${bodyKind}-body-content`}>
        {turns.length ? turns.map((turn, index) => (
          <article key={index} className="rounded-lg border border-border bg-inset p-3">
            <Badge variant="outline" className="mb-2">{payloadRoleLabel(turn.role)}</Badge>
            {turn.text ? <p className="whitespace-pre-wrap break-words text-sm [overflow-wrap:anywhere]">{turn.text}</p> : null}
            {turn.toolCalls.length ? <p className="mt-2 text-xs text-muted-foreground">{copy.contentTools}</p> : null}
            {turn.toolResults.length ? <p className="mt-2 text-xs text-muted-foreground">{copy.contentToolResult}</p> : null}
          </article>
        )) : <p className="text-sm text-muted-foreground">{contentKind === "headers" ? copy.contentHeadersExcluded : content ? copy.contentUnavailable : emptyState ?? copy.noCaptured(title)}</p>}
      </div>
    </section>
  );
}
