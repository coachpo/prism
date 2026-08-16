// SSE framing (Requests SPEC §11.3): LF/CRLF/CR-only line endings, optional
// leading BOM, standard field/value parsing, blank-line event framing,
// multi-line `data:`, `event:`, comment/ping lines, `[DONE]`, trailing
// event without a final blank line, and per-event JSON parsing. The parser
// is shared by message reassembly, JSON-events, and Raw SSE views.
export type SseFieldName = "data" | "event" | "id" | "retry";

export interface SseEvent {
  index: number;
  eventName: string;
  data: string;
  id: string | null;
  raw: string;
  // true when the event was terminated by a blank line; false for a trailing
  // flush that only happens when the capture is known complete.
  terminated: boolean;
  json: unknown | null;
  jsonError: string | null;
}

export interface SseFramingResult {
  events: SseEvent[];
  incompleteTail: string | null;
  // true when the input ended mid-event without a terminating blank line.
  hasIncompleteTail: boolean;
}

function parseEventJson(data: string): { json: unknown | null; jsonError: string | null } {
  const trimmed = data.trim();
  if (trimmed === "" || trimmed === "[DONE]") return { json: null, jsonError: null };
  try {
    return { json: JSON.parse(trimmed) as unknown, jsonError: null };
  } catch (err) {
    return { json: null, jsonError: err instanceof Error ? err.message : String(err) };
  }
}

function normalizeLineEndings(content: string): string {
  return content.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
}

function splitLines(content: string): string[] {
  return content.split("\n");
}

function parseFieldLine(line: string): { name: SseFieldName | string; value: string } | null {
  if (line === "" || line.startsWith(":")) return null;
  const colonIndex = line.indexOf(":");
  if (colonIndex === -1) return { name: line, value: "" };
  const name = line.slice(0, colonIndex).trim();
  let value = line.slice(colonIndex + 1);
  if (value.startsWith(" ")) value = value.slice(1);
  return { name, value };
}

export function frameSseEvents(content: string): SseFramingResult {
  const normalized = normalizeLineEndings(content);
  // Strip a single leading BOM (U+FEFF) before framing.
  const withoutBom = normalized.startsWith("\uFEFF") ? normalized.slice(1) : normalized;
  const lines = splitLines(withoutBom);

  const events: SseEvent[] = [];
  let currentData: string[] = [];
  let currentEventName = "message";
  let currentId: string | null = null;
  let currentRaw: string[] = [];
  let eventIndex = 0;
  let incomplete = false;

  const flushEvent = (terminated: boolean) => {
    const raw = currentRaw.join("\n");
    const data = currentData.join("\n");
    events.push({
      index: eventIndex,
      eventName: currentEventName || "message",
      data,
      id: currentId,
      raw,
      terminated,
      ...parseEventJson(data),
    });
    eventIndex += 1;
    currentData = [];
    currentEventName = "message";
    currentId = null;
    currentRaw = [];
  };

  for (const line of lines) {
    if (line === "") {
      // Blank line terminates the current event.
      if (currentRaw.length > 0 || currentData.length > 0) {
        flushEvent(true);
      }
      continue;
    }
    const field = parseFieldLine(line);
    if (field === null) {
      // Comment/ping line: keep in raw but do not affect the event.
      currentRaw.push(line);
      continue;
    }
    switch (field.name) {
      case "data":
        currentData.push(field.value);
        currentRaw.push(line);
        break;
      case "event":
        currentEventName = field.value || "message";
        currentRaw.push(line);
        break;
      case "id":
        currentId = field.value;
        currentRaw.push(line);
        break;
      case "retry":
        currentRaw.push(line);
        break;
      default:
        // Unknown fields are ignored per SSE spec but kept in raw.
        currentRaw.push(line);
        break;
    }
  }

  // Trailing event without a terminating blank line: the caller decides
  // whether the capture is complete. The parser reports it as an incomplete
  // tail; consumers flush it only when capture completeness allows.
  if (currentRaw.length > 0 || currentData.length > 0) {
    incomplete = true;
  }

  return {
    events,
    incompleteTail: incomplete ? currentRaw.join("\n") : null,
    hasIncompleteTail: incomplete,
  };
}

export function isSseContent(content: string): boolean {
  const trimmed = content.trimStart();
  if (trimmed === "") return false;
  // An SSE payload contains one or more `data:`/`event:` field lines or
  // starts with a known SSE marker.
  if (trimmed.startsWith("\uFEFF")) {
    return trimmed.slice(1).includes("data:");
  }
  return /(^|\n)(data|event|id|retry):/.test(trimmed.slice(0, 2048));
}
