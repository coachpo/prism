// Payload view model (Requests SPEC §11.1–11.3): content-aware views.
// - Streaming SSE: [ 消息 ] [ JSON 事件 ] [ 原始 SSE ] — the message view is
//   an operation-aware reassembled transcript with tool cards; JSON events
//   virtualize per-event parsed JSON; Raw SSE shows the byte-exact text.
// - Non-stream valid JSON: [ 消息 ] [ JSON ].
// - Invalid UTF-8 / binary / unknown text: no fabricated views.
import { frameSseEvents, isSseContent, type SseEvent } from "./sseFraming";
import {
  accumulateSseEvent,
  buildTranscriptDocument,
  createEmptyTranscriptState,
  extractAnthropicTools,
  extractGeminiTools,
  extractOpenAiRequestTools,
  extractOpenAiResponseTools,
  type TranscriptDocument,
} from "./streamTranscript";

export type PayloadViewKind = "transcript" | "json_events" | "raw_sse" | "json" | "raw_text" | "unparseable";

export interface PayloadViewAvailability {
  kind: PayloadViewKind;
  labelKey: string;
}

export interface PayloadViewModel {
  isStreaming: boolean;
  // bytes are valid UTF-8 text (false for binary/invalid bodies)
  readableText: boolean;
  availability: PayloadViewAvailability[];
  transcript: TranscriptDocument | null;
  sseEvents: SseEvent[] | null;
  incompleteTail: string | null;
  hasIncompleteTail: boolean;
  // raw text shown in Raw SSE / Raw views (byte-exact stored prefix decoded
  // as UTF-8; only populated when readableText is true)
  rawText: string | null;
  unparseableReason: string | null;
}

export function isLikelyStreamingBody(content: string): boolean {
  return isSseContent(content);
}

export function buildPayloadViewModel(
  content: string,
  apiFamily: "openai" | "anthropic" | "gemini",
  bodyKind: "request" | "response",
  operationName: string | null,
): PayloadViewModel {
  if (content.length === 0) {
    return {
      isStreaming: false,
      readableText: true,
      availability: [],
      transcript: null,
      sseEvents: null,
      incompleteTail: null,
      hasIncompleteTail: false,
      rawText: null,
      unparseableReason: null,
    };
  }

  const readableText = isReadableUtf8(content);
  if (!readableText) {
    return {
      isStreaming: false,
      readableText: false,
      availability: [{ kind: "unparseable", labelKey: "unparseable" }],
      transcript: null,
      sseEvents: null,
      incompleteTail: null,
      hasIncompleteTail: false,
      rawText: null,
      unparseableReason: "invalid_utf8_or_binary",
    };
  }

  const streaming = isLikelyStreamingBody(content);
  if (streaming) {
    const framing = frameSseEvents(content);
    const state = createEmptyTranscriptState();
    for (const event of framing.events) {
      accumulateSseEvent(state, event, apiFamily, operationName);
    }
    const transcript = buildTranscriptDocument(state);
    const availability: PayloadViewAvailability[] = [
      { kind: "transcript", labelKey: "messageView" },
      { kind: "json_events", labelKey: "jsonEventsView" },
      { kind: "raw_sse", labelKey: "rawSseView" },
    ];
    return {
      isStreaming: true,
      readableText: true,
      availability,
      transcript,
      sseEvents: framing.events,
      incompleteTail: framing.incompleteTail,
      hasIncompleteTail: framing.hasIncompleteTail,
      rawText: content,
      unparseableReason: null,
    };
  }

  // Non-streaming JSON: message view (with tool cards) + JSON view.
  const parsed = tryParseJson(content);
  if (parsed !== null) {
    const transcript = buildNonStreamingTranscript(parsed, apiFamily, bodyKind);
    return {
      isStreaming: false,
      readableText: true,
      availability: [
        { kind: "transcript", labelKey: "messageView" },
        { kind: "json", labelKey: "jsonView" },
      ],
      transcript,
      sseEvents: null,
      incompleteTail: null,
      hasIncompleteTail: false,
      rawText: content,
      unparseableReason: null,
    };
  }

  // Plain text or unknown: only a meaningful raw-text view.
  return {
    isStreaming: false,
    readableText: true,
    availability: [{ kind: "raw_text", labelKey: "rawTextView" }],
    transcript: null,
    sseEvents: null,
    incompleteTail: null,
    hasIncompleteTail: false,
    rawText: content,
    unparseableReason: null,
  };
}

function isReadableUtf8(content: string): boolean {
  // The stored prefix was decoded as UTF-8 with replacement chars for
  // invalid sequences; treat U+FFFD-heavy content as unparseable.
  const replacementCount = (content.match(/\uFFFD/g) ?? []).length;
  return replacementCount === 0;
}

function tryParseJson(content: string): unknown | null {
  try {
    return JSON.parse(content) as unknown;
  } catch {
    return null;
  }
}

function buildNonStreamingTranscript(
  value: unknown,
  apiFamily: "openai" | "anthropic" | "gemini",
  bodyKind: "request" | "response",
): TranscriptDocument | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return null;
  const body = value as Record<string, unknown>;

  let calls: { calls: ReturnType<typeof extractOpenAiRequestTools>["calls"]; results: ReturnType<typeof extractOpenAiRequestTools>["results"] } | null = null;

  if (apiFamily === "openai") {
    calls = bodyKind === "request" ? extractOpenAiRequestTools(body) : extractOpenAiResponseTools(body);
  } else if (apiFamily === "anthropic") {
    calls = extractAnthropicTools(body);
  } else {
    calls = extractGeminiTools(body);
  }

  const turns: TranscriptDocument["turns"] = [];
  if (calls.calls.length > 0 || calls.results.length > 0) {
    turns.push({ role: "tool_calls", text: "", toolCalls: calls.calls, toolResults: calls.results, terminalState: null });
  }
  if (turns.length === 0) return null;
  return { turns, usage: null, incomplete: false };
}

export function prettyPrintJson(value: unknown): string {
  return JSON.stringify(value, null, 2);
}
