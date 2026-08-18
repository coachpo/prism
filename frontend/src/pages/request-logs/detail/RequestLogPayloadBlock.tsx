import { useId, useMemo, useState, type MouseEvent } from "react";
import { AlertTriangle, Copy, Wrench } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import type { ApiFamily } from "@/lib/types";
import type { RequestLogHeaderEntry, RequestLogPayloadDocument } from "./requestLogPayloadDocuments";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { copyRequestLogText } from "./requestLogDetailUtils";
import {
  buildRequestLogHeaderDocument,
  buildRequestLogPayloadDocument,
  formatRequestLogHeaderRaw,
  formatRequestLogPayloadRaw,
  type RequestLogPayloadBodyKind,
} from "./requestLogPayloadDocuments";
import { buildPayloadViewModel, prettyPrintJson, type PayloadViewKind } from "./payloadDocumentViewModel";
import { viewLabel } from "./payloadViewLabels";
import type { TranscriptDocument, TranscriptToolCall, TranscriptToolResult } from "./streamTranscript";

type PayloadViewMode = "rendered" | "raw";
type RequestLogPayloadContentKind = "headers" | "payload";

interface RequestLogPayloadBlockProps {
  title: string;
  content: string;
  emptyState?: string;
  apiFamily?: ApiFamily | null;
  bodyKind?: RequestLogPayloadBodyKind;
  contentKind?: RequestLogPayloadContentKind;
  operationName?: string | null;
}

function ToolCallCard({ call }: { call: TranscriptToolCall }) {
  const { messages } = useLocale();
  return (
    <article className="rounded-lg border border-primary/25 bg-primary/[0.06] p-3" data-testid="tool-call-card">
      <div className="mb-2 flex items-center gap-2">
        <Wrench className="size-3.5 text-primary" />
        <Badge variant="outline" className="border-primary/30 bg-background font-mono text-xs">
          {call.name ?? messages.requestLogs.toolCall}
        </Badge>
      </div>
      {call.argumentsJson ? (
        <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-words rounded border border-border bg-background/70 p-2 font-mono text-[11px] leading-5 text-foreground [overflow-wrap:anywhere]">
          {call.argumentsJson}
        </pre>
      ) : null}
    </article>
  );
}

function ToolResultCard({ result }: { result: TranscriptToolResult }) {
  const { messages } = useLocale();
  return (
    <article
      className={cn(
        "rounded-lg border p-3",
        result.isError ? "border-destructive/30 bg-destructive/[0.06]" : "border-border bg-inset",
      )}
      data-testid="tool-result-card"
    >
      <div className="mb-2 flex items-center gap-2">
        {result.isError ? <AlertTriangle className="size-3.5 text-destructive" /> : <Wrench className="size-3.5 text-muted-foreground" />}
        <Badge variant="outline" className="border-border bg-background font-mono text-xs">
          {result.isError ? messages.requestLogs.toolError : messages.requestLogs.toolResult}
        </Badge>
      </div>
      {result.content ? (
        <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-words rounded border border-border bg-background/70 p-2 font-mono text-[11px] leading-5 text-foreground [overflow-wrap:anywhere]">
          {result.content}
        </pre>
      ) : null}
    </article>
  );
}

function TranscriptView({ transcript }: { transcript: TranscriptDocument }) {
  const visibleTurns = transcript.turns.filter(
    (turn) => turn.text.length > 0 || turn.toolCalls.length > 0 || turn.toolResults.length > 0,
  );
  if (visibleTurns.length === 0) {
    return <p className="text-xs text-muted-foreground">—</p>;
  }
  return (
    <div className="flex flex-col gap-3">
      {visibleTurns.map((turn, index) => (
        <article key={`${turn.role}-${index}`} className="rounded-lg border border-border bg-inset p-3">
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <Badge variant="outline" className="border-border bg-background font-mono text-xs">
              {turn.role}
            </Badge>
            {turn.terminalState ? (
              <Badge variant="secondary" className="rounded-md px-2 py-0.5 font-mono text-[10px]">
                {turn.terminalState}
              </Badge>
            ) : null}
          </div>
          {turn.text.length > 0 ? (
            <p className="whitespace-pre-wrap break-words text-sm leading-6 text-foreground [overflow-wrap:anywhere]">
              {turn.text}
            </p>
          ) : null}
          {turn.toolCalls.length > 0 ? (
            <div className="mt-2 flex flex-col gap-2">
              {turn.toolCalls.map((call, callIndex) => (
                <ToolCallCard key={`${call.id ?? call.index}-${callIndex}`} call={call} />
              ))}
            </div>
          ) : null}
          {turn.toolResults.length > 0 ? (
            <div className="mt-2 flex flex-col gap-2">
              {turn.toolResults.map((result, resultIndex) => (
                <ToolResultCard key={resultIndex} result={result} />
              ))}
            </div>
          ) : null}
        </article>
      ))}
      {transcript.usage ? (
        <div className="rounded-lg border border-border bg-inset p-3">
          <p className="mb-1 text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground">
            Token usage
          </p>
          <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-5 text-foreground [overflow-wrap:anywhere]">
            {prettyPrintJson(JSON.parse(transcript.usage))}
          </pre>
        </div>
      ) : null}
    </div>
  );
}

function JsonEventsView({ events }: { events: { index: number; eventName: string; data: string; raw: string; json: unknown | null; jsonError: string | null }[] }) {
  const { messages } = useLocale();
  if (events.length === 0) {
    return <p className="text-xs text-muted-foreground">—</p>;
  }
  return (
    <div className="flex flex-col gap-2">
      {events.map((event) => (
        <article key={event.index} className="rounded-lg border border-border bg-inset p-3" data-testid="json-event">
          <div className="mb-2 flex items-center gap-2">
            <Badge variant="outline" className="border-border bg-background font-mono text-xs">
              {event.index + 1}
            </Badge>
            <Badge variant="secondary" className="rounded-md px-2 py-0.5 font-mono text-[10px]">
              {event.eventName}
            </Badge>
          </div>
          {event.json !== null ? (
            <pre className="max-h-56 overflow-auto whitespace-pre-wrap break-words font-mono text-[11px] leading-5 text-foreground [overflow-wrap:anywhere]">
              {prettyPrintJson(event.json)}
            </pre>
          ) : (
            <p className="text-xs text-muted-foreground">
              {event.data === "[DONE]" ? "[DONE]" : messages.requestLogs.jsonEventBadJson}
            </p>
          )}
        </article>
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
  contentKind = "payload",
  operationName = null,
}: RequestLogPayloadBlockProps) {
  const { messages } = useLocale();
  const [viewMode, setViewMode] = useState<PayloadViewMode>("rendered");
  const [payloadView, setPayloadView] = useState<PayloadViewKind>("transcript");
  const headingId = useId();
  const hasContent = content.length > 0;
  const family = apiFamily ?? "openai";

  const viewModel = useMemo(
    () => (contentKind === "headers" ? null : buildPayloadViewModel(content, family, bodyKind ?? "response", operationName)),
    [content, contentKind, family, bodyKind, operationName],
  );
  const headerDocument = useMemo(
    () => (contentKind === "headers" ? buildRequestLogHeaderDocument(content) : null),
    [content, contentKind],
  );
  const rawContent = useMemo(() => {
    if (!hasContent) return "";
    return contentKind === "headers" ? formatRequestLogHeaderRaw(content) : formatRequestLogPayloadRaw(content);
  }, [content, contentKind, hasContent]);
  const displayMode = hasContent ? viewMode : "rendered";

  const isStreamingBody = viewModel?.isStreaming === true;
  const availableViews = viewModel?.availability ?? [];
  const activePayloadView: PayloadViewKind | null = availableViews.some((view) => view.kind === payloadView)
    ? payloadView
    : availableViews[0]?.kind ?? null;

  const copyContent = displayMode === "raw" ? rawContent : content;
  const shouldConstrainContent = contentKind === "payload" && bodyKind !== undefined;

  const handleCopy = (event: MouseEvent<HTMLButtonElement>) => {
    if (!hasContent) return;
    const container = event.currentTarget.closest("[data-clipboard-fallback-root]") as HTMLElement | null;
    void copyRequestLogText(copyContent, title.toLowerCase(), container);
  };

  const renderPayloadView = () => {
    if (!viewModel) return null;
    switch (activePayloadView) {
      case "transcript":
        if (!viewModel.isStreaming && contentKind !== "headers") {
          // Non-streaming JSON bodies keep the operation-aware sectioned
          // document (message transcript, tool calls, usage) as the
          // canonical message view; the streaming transcript view is only
          // for SSE bodies.
          const legacyDocument = buildRequestLogPayloadDocument({ apiFamily: family, bodyKind: bodyKind ?? "response", content });
          if (legacyDocument) {
            return <LegacyDocumentView document={legacyDocument} />;
          }
        }
        return viewModel.transcript ? <TranscriptView transcript={viewModel.transcript} /> : null;
      case "json_events":
        return viewModel.sseEvents ? <JsonEventsView events={viewModel.sseEvents} /> : null;
      case "raw_sse":
      case "raw_text":
      case "json":
        return (
          <pre className="max-w-full whitespace-pre-wrap break-words rounded-lg border border-border bg-inset p-3 font-mono text-xs leading-5 text-foreground [overflow-wrap:anywhere]">
            {viewModel.rawText}
          </pre>
        );
      case "unparseable":
        return (
          <div className="flex flex-col gap-2 rounded-lg border border-degraded/25 bg-degraded/[0.06] p-3">
            <p className="text-xs text-degraded">{messages.requestLogs.unparseableView}</p>
            <p className="text-xs text-muted-foreground">
              {viewModel.unparseableReason === "invalid_utf8_or_binary"
                ? "该 body 包含无效 UTF-8 或二进制数据，无法提供消息/JSON/文本视图；原始字节可通过下载获取。"
                : "—"}
            </p>
          </div>
        );
      default:
        return null;
    }
  };

  const renderRendered = () => {
    if (contentKind === "headers") {
      if (!headerDocument) return null;
      switch (headerDocument.kind) {
        case "entries":
          return <HeaderDocumentView entries={headerDocument.entries} />;
        case "empty":
          return (
            <p className="text-sm text-muted-foreground">
              <span aria-hidden="true">—</span> {messages.requestLogs.headerEmpty(title)}
            </p>
          );
        case "absent":
          return (
            <p className="text-sm text-muted-foreground">
              <span aria-hidden="true">—</span> {messages.requestLogs.noCaptured(title)}
            </p>
          );
        case "malformed":
          return (
            <div className="flex flex-col gap-2 rounded-lg border border-degraded/25 bg-degraded/[0.06] p-3" data-testid="request-log-headers-malformed">
              <p className="text-xs text-degraded">{messages.requestLogs.headerMalformed(title)}</p>
            </div>
          );
      }
    }
    if (viewModel) {
      if (viewModel.availability.length === 0) {
        return <p className="text-sm text-muted-foreground">{emptyState ?? messages.requestLogs.noCaptured(title.toLowerCase())}</p>;
      }
      return (
        <div className="flex min-w-0 flex-col gap-3">
          {viewModel.availability.length > 1 ? (
            <div className="inline-flex w-fit items-center gap-1 rounded-lg border border-border bg-inset p-1" aria-label={`${title} payload view`}>
              {viewModel.availability.map((view) => (
                <Button
                  key={view.kind}
                  aria-pressed={activePayloadView === view.kind}
                  onClick={() => setPayloadView(view.kind)}
                  size="xs"
                  type="button"
                  variant={activePayloadView === view.kind ? "secondary" : "ghost"}
                >
                  {viewLabel(view.kind, messages.requestLogs)}
                </Button>
              ))}
            </div>
          ) : null}
          {viewModel.isStreaming && viewModel.hasIncompleteTail ? (
            <p className="text-[11px] text-degraded">{messages.requestLogs.streamIncompleteNote}</p>
          ) : null}
          {renderPayloadView()}
        </div>
      );
    }
    return <p className="text-sm text-muted-foreground">{emptyState ?? messages.requestLogs.noCaptured(title.toLowerCase())}</p>;
  };

  const contentNode = displayMode === "raw" ? (
    <pre className="max-w-full whitespace-pre-wrap break-words rounded-lg border border-border bg-inset p-3 font-mono text-xs leading-5 text-foreground [overflow-wrap:anywhere]">
      {rawContent}
    </pre>
  ) : (
    renderRendered()
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
          <div className="inline-flex items-center gap-1 rounded-lg border border-border bg-inset p-1" aria-label={`${title} view mode`}>
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
              {isStreamingBody ? messages.requestLogs.rawSseView : messages.requestLogs.payloadRawJson}
            </Button>
          </div>
          <Button variant="outline" size="sm" className="rounded-full" disabled={!hasContent} onClick={handleCopy}>
            <Copy data-icon="inline-start" />
            {messages.requestLogs.copy}
          </Button>
        </div>
      </div>
      {shouldConstrainContent ? (
        <div className="min-w-0 max-h-[30rem] overflow-y-auto" data-testid={`request-log-${bodyKind}-body-content`}>
          {contentNode}
        </div>
      ) : (
        contentNode
      )}
    </section>
  );
}

function LegacyDocumentView({ document }: { document: RequestLogPayloadDocument }) {
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
                <article key={`${line.label}-${index}`} className="rounded-lg border border-border bg-inset p-3">
                  <div className="mb-2 flex items-center gap-2">
                    <Badge variant="outline" className="border-border bg-background font-mono text-xs">
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
            <dl className="divide-y divide-border/60 rounded-lg border border-border bg-inset">
              {section.lines.map((line, index) => (
                <div key={`${line.label}-${index}`} className="grid gap-1 p-3 sm:grid-cols-[minmax(8rem,14rem)_minmax(0,1fr)] sm:gap-4">
                  <dt className="min-w-0 break-words font-mono text-xs font-medium uppercase tracking-wide text-muted-foreground [overflow-wrap:anywhere]">
                    {line.label}
                  </dt>
                  <dd
                    className={cn(
                      "min-w-0 whitespace-pre-wrap break-words text-sm leading-6 text-foreground [overflow-wrap:anywhere]",
                      line.mono && "font-mono text-xs leading-5",
                    )}
                  >
                    {line.value}
                  </dd>
                </div>
              ))}
            </dl>
          )}
        </section>
      ))}
    </div>
  );
}

function HeaderDocumentView({ entries }: { entries: RequestLogHeaderEntry[] }) {
  return (
    <dl className="divide-y divide-border/60 rounded-lg border border-border bg-inset">
      {entries.map((entry, index) => (
        <div key={`${entry.name}-${index}`} className="grid gap-1 p-3 sm:grid-cols-[minmax(8rem,14rem)_minmax(0,1fr)] sm:gap-4">
          <dt className="min-w-0 break-words font-mono text-xs font-medium uppercase tracking-wide text-muted-foreground [overflow-wrap:anywhere]">
            {entry.name}
          </dt>
          <dd className="min-w-0 whitespace-pre-wrap break-words font-mono text-xs leading-5 text-foreground [overflow-wrap:anywhere]">
            {entry.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}
