import { useId, useMemo, useState, type MouseEvent } from "react";
import { Copy } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import type { ApiFamily } from "@/lib/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { copyRequestLogText } from "./requestLogDetailUtils";
import {
  buildBestEffortPayloadDocument,
  buildRequestLogHeaderDocument,
  buildRequestLogPayloadDocument,
  formatRequestLogHeaderRaw,
  formatRequestLogPayloadRaw,
  type RequestLogPayloadBodyKind,
  type RequestLogPayloadDocument,
  type RequestLogPayloadDocumentLine,
} from "./requestLogPayloadDocuments";

type PayloadViewMode = "rendered" | "raw";
type RequestLogPayloadContentKind = "headers" | "payload";

interface RequestLogPayloadBlockProps {
  title: string;
  content: string;
  emptyState?: string;
  apiFamily?: ApiFamily | null;
  bodyKind?: RequestLogPayloadBodyKind;
  contentKind?: RequestLogPayloadContentKind;
}

function FieldValue({ line }: { line: RequestLogPayloadDocumentLine }) {
  return (
    <dd
      className={cn(
        "min-w-0 whitespace-pre-wrap break-words text-sm leading-6 text-foreground [overflow-wrap:anywhere]",
        line.mono && "font-mono text-xs leading-5",
      )}
    >
      {line.value}
    </dd>
  );
}

function RequestLogPayloadDocumentView({ document }: { document: RequestLogPayloadDocument }) {
  return (
    <div className="flex flex-col gap-3">
      {document.sections.map((section) => (
        <section key={section.title} className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="secondary" className="rounded-md px-2 py-0.5 text-xs font-medium">
              {section.title}
            </Badge>
          </div>
          {section.kind === "transcript" ? (
            <div className="flex flex-col gap-3">
              {section.lines.map((line, index) => (
                <article key={`${line.label}-${index}`} className="rounded-lg border border-outline-variant bg-surface-container p-3">
                  <div className="mb-2 flex items-center gap-2">
                    <Badge variant="outline" className="border-outline-variant bg-background font-mono text-xs">
                      {line.label}
                    </Badge>
                  </div>
                  <p className="whitespace-pre-wrap break-words text-sm leading-6 text-foreground [overflow-wrap:anywhere]">
                    {line.value}
                  </p>
                </article>
              ))}
            </div>
          ) : (
            <dl className="divide-y divide-border/60 rounded-lg border border-outline-variant bg-surface-container-low">
              {section.lines.map((line, index) => (
                <div key={`${line.label}-${index}`} className="grid gap-1 p-3 sm:grid-cols-[minmax(8rem,14rem)_minmax(0,1fr)] sm:gap-4">
                  <dt className="min-w-0 break-words font-mono text-xs font-medium uppercase tracking-wide text-muted-foreground [overflow-wrap:anywhere]">
                    {line.label}
                  </dt>
                  <FieldValue line={line} />
                </div>
              ))}
            </dl>
          )}
        </section>
      ))}
    </div>
  );
}

function buildRenderedDocument({
  apiFamily,
  bodyKind,
  content,
  contentKind,
}: {
  apiFamily?: ApiFamily | null;
  bodyKind?: RequestLogPayloadBodyKind;
  content: string;
  contentKind?: RequestLogPayloadContentKind;
}) {
  if (content.length === 0) return null;
  if (contentKind === "headers") return buildRequestLogHeaderDocument(content) ?? buildBestEffortPayloadDocument(content);
  if (apiFamily && bodyKind) return buildRequestLogPayloadDocument({ apiFamily, bodyKind, content }) ?? buildBestEffortPayloadDocument(content);
  return buildBestEffortPayloadDocument(content);
}

export function RequestLogPayloadBlock({
  title,
  content,
  emptyState,
  apiFamily,
  bodyKind,
  contentKind = "payload",
}: RequestLogPayloadBlockProps) {
  const { messages } = useLocale();
  const [viewMode, setViewMode] = useState<PayloadViewMode>("rendered");
  const headingId = useId();
  const hasContent = content.length > 0;
  const document = useMemo(
    () => buildRenderedDocument({ apiFamily, bodyKind, content, contentKind }),
    [apiFamily, bodyKind, content, contentKind],
  );
  const rawContent = useMemo(() => {
    if (!hasContent) return "";
    return contentKind === "headers" ? formatRequestLogHeaderRaw(content) : formatRequestLogPayloadRaw(content);
  }, [content, contentKind, hasContent]);
  const displayMode = hasContent ? viewMode : "rendered";
  const copyContent = displayMode === "raw" ? rawContent : content;
  const shouldConstrainContent = contentKind === "payload" && bodyKind === "request";

  const handleCopy = (event: MouseEvent<HTMLButtonElement>) => {
    if (!hasContent) return;

    const container = event.currentTarget.closest("[data-clipboard-fallback-root]") as HTMLElement | null;
    void copyRequestLogText(copyContent, title.toLowerCase(), container);
  };

  const contentNode = displayMode === "raw" ? (
    <pre className="max-w-full whitespace-pre-wrap break-words rounded-lg border border-outline-variant bg-surface-container-low p-3 font-mono text-xs leading-5 text-foreground [overflow-wrap:anywhere]">
      {rawContent}
    </pre>
  ) : document ? (
    <RequestLogPayloadDocumentView document={document} />
  ) : (
    <p className="whitespace-pre-wrap break-words text-sm leading-6 text-muted-foreground [overflow-wrap:anywhere]">
      {hasContent ? content : emptyState ?? messages.requestLogs.noCaptured(title.toLowerCase())}
    </p>
  );

  return (
    <section className="flex min-w-0 flex-col gap-3" aria-labelledby={headingId}>
      <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0">
          <h2 id={headingId} className="text-sm font-semibold tracking-tight text-foreground">
            {title}
          </h2>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <div className="inline-flex items-center gap-1 rounded-lg border border-outline-variant bg-surface-container p-1" aria-label={`${title} view mode`}>
            <Button
              aria-pressed={displayMode === "rendered"}
              disabled={!hasContent}
              onClick={() => setViewMode("rendered")}
              size="xs"
              type="button"
              variant={displayMode === "rendered" ? "secondary" : "ghost"}
            >
              {messages.requestLogs.payloadRendered}
            </Button>
            <Button
              aria-pressed={displayMode === "raw"}
              disabled={!hasContent}
              onClick={() => setViewMode("raw")}
              size="xs"
              type="button"
              variant={displayMode === "raw" ? "secondary" : "ghost"}
            >
              {messages.requestLogs.payloadRawJson}
            </Button>
          </div>
          <Button variant="outline" size="sm" className="rounded-full" disabled={!hasContent} onClick={handleCopy}>
            <Copy data-icon="inline-start" />
            {messages.requestLogs.copy}
          </Button>
        </div>
      </div>
      {shouldConstrainContent ? (
        <div className="min-w-0 max-h-[90vh] overflow-y-auto" data-testid="request-log-request-body-content">
          {contentNode}
        </div>
      ) : (
        contentNode
      )}
    </section>
  );
}
