import type { ApiFamily } from "@/lib/types";

export type RequestLogPayloadBodyKind = "request" | "response";

export type RequestLogPayloadDocumentSectionKind = "fields" | "transcript";

export interface RequestLogPayloadDocumentLine {
  label: string;
  value: string;
  mono?: boolean;
}

export interface RequestLogPayloadDocumentSection {
  title: string;
  lines: RequestLogPayloadDocumentLine[];
  kind?: RequestLogPayloadDocumentSectionKind;
}

export interface RequestLogPayloadDocument {
  sections: RequestLogPayloadDocumentSection[];
}

interface BuildPayloadDocumentParams {
  apiFamily: ApiFamily;
  bodyKind: RequestLogPayloadBodyKind;
  content: string;
}

type JsonRecord = Record<string, unknown>;

interface JsonParseResult {
  parsed: boolean;
  value: unknown;
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function getRecord(record: JsonRecord, key: string): JsonRecord | null {
  const value = record[key];
  return isRecord(value) ? value : null;
}

function getArray(record: JsonRecord, key: string): unknown[] {
  const value = record[key];
  return Array.isArray(value) ? value : [];
}

function getString(record: JsonRecord, key: string): string | null {
  const value = record[key];
  return typeof value === "string" && value.length > 0 ? value : null;
}

function getNumber(record: JsonRecord, key: string): number | null {
  const value = record[key];
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function formatJson(value: unknown): string {
  return JSON.stringify(value, null, 2);
}

function appendSection(
  sections: RequestLogPayloadDocumentSection[],
  title: string,
  lines: RequestLogPayloadDocumentLine[],
  kind: RequestLogPayloadDocumentSectionKind = "fields",
) {
  const visibleLines = lines.filter((line) => line.value.length > 0);
  if (visibleLines.length > 0) {
    sections.push({ title, lines: visibleLines, kind });
  }
}

function parseJsonValue(content: string): JsonParseResult {
  try {
    return { parsed: true, value: JSON.parse(content) as unknown };
  } catch {
    return { parsed: false, value: null };
  }
}

function parseJsonRecord(content: string): JsonRecord | null {
  const result = parseJsonValue(content);
  return result.parsed && isRecord(result.value) ? result.value : null;
}

function formatJsonLineValue(value: unknown): { value: string; mono: boolean } {
  if (typeof value === "string") return { value, mono: false };
  if (typeof value === "number" || typeof value === "boolean" || value === null) {
    return { value: String(value), mono: true };
  }
  return { value: formatJson(value), mono: true };
}

function normalizeHeaderName(name: string): string {
  return name.trim().toLowerCase();
}

function isSensitiveHeaderName(name: string): boolean {
  const normalized = normalizeHeaderName(name);
  if (normalized === "access-control-allow-credentials") return false;

  return normalized === "authorization" ||
    normalized === "proxy-authorization" ||
    normalized === "cookie" ||
    normalized === "set-cookie" ||
    normalized.includes("api-key") ||
    normalized.includes("token") ||
    normalized.includes("secret") ||
    normalized.includes("credential");
}

function formatHeaderValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean" || value === null) return String(value);
  return formatJson(value);
}

function maskHeaderValue(name: string, value: unknown): string {
  return isSensitiveHeaderName(name) ? "[REDACTED]" : formatHeaderValue(value);
}

function buildHeaderLinesFromRecord(record: JsonRecord): RequestLogPayloadDocumentLine[] {
  return Object.entries(record).flatMap(([name, value]) => {
    const label = normalizeHeaderName(name);
    if (label.length === 0) return [];
    return [{ label, value: maskHeaderValue(label, value), mono: true }];
  });
}

function buildHeaderLinesFromText(content: string): RequestLogPayloadDocumentLine[] {
  return content.split(/\r?\n/).flatMap((line) => {
    const separatorIndex = line.indexOf(":");
    if (separatorIndex <= 0) return [];
    const label = normalizeHeaderName(line.slice(0, separatorIndex));
    if (label.length === 0) return [];
    return [{ label, value: maskHeaderValue(label, line.slice(separatorIndex + 1).trim()), mono: true }];
  });
}

function buildGenericJsonDocumentFromValue(value: unknown): RequestLogPayloadDocument | null {
  if (isRecord(value)) {
    const lines = Object.entries(value).map(([label, entryValue]) => ({
      label,
      ...formatJsonLineValue(entryValue),
    }));
    return lines.length > 0 ? { sections: [{ title: "JSON fields", lines, kind: "fields" }] } : null;
  }

  if (Array.isArray(value)) {
    const lines = value.map((entryValue, index) => ({
      label: `item ${index + 1}`,
      ...formatJsonLineValue(entryValue),
    }));
    return lines.length > 0 ? { sections: [{ title: "JSON array", lines, kind: "fields" }] } : null;
  }

  if (value === null) return { sections: [{ title: "JSON value", lines: [{ label: "value", value: "null", mono: true }] }] };
  return { sections: [{ title: "JSON value", lines: [{ label: "value", value: String(value), mono: true }] }] };
}

function buildGenericJsonDocument(content: string): RequestLogPayloadDocument | null {
  const result = parseJsonValue(content);
  return result.parsed ? buildGenericJsonDocumentFromValue(result.value) : null;
}

export function formatRequestLogPayloadRaw(content: string): string {
  const result = parseJsonValue(content);
  return result.parsed ? formatJson(result.value) : content;
}

export function formatRequestLogHeaderRaw(content: string): string {
  const result = parseJsonValue(content);
  if (result.parsed && isRecord(result.value)) {
    const maskedHeaders = Object.fromEntries(
      Object.entries(result.value).map(([name, value]) => {
        const label = normalizeHeaderName(name);
        return [label, isSensitiveHeaderName(label) ? "[REDACTED]" : value];
      }),
    );
    return formatJson(maskedHeaders);
  }

  const lines = buildHeaderLinesFromText(content);
  return lines.length > 0 ? lines.map((line) => `${line.label}: ${line.value}`).join("\n") : formatRequestLogPayloadRaw(content);
}

export function buildRequestLogHeaderDocument(content: string): RequestLogPayloadDocument | null {
  const result = parseJsonValue(content);
  const lines = result.parsed && isRecord(result.value)
    ? buildHeaderLinesFromRecord(result.value)
    : buildHeaderLinesFromText(content);

  return lines.length > 0 ? { sections: [{ title: "Headers", lines, kind: "fields" }] } : null;
}

export function buildBestEffortPayloadDocument(content: string): RequestLogPayloadDocument | null {
  return buildGenericJsonDocument(content);
}

function textFromOpenAiContent(value: unknown): string {
  if (typeof value === "string") return value;
  if (!Array.isArray(value)) return "";

  return value
    .map((part) => {
      if (typeof part === "string") return part;
      if (!isRecord(part)) return "";
      return getString(part, "text") ?? getString(part, "content") ?? "";
    })
    .filter((text) => text.length > 0)
    .join("\n");
}

function textFromGeminiParts(parts: unknown[]): string {
  return parts
    .map((part) => isRecord(part) ? getString(part, "text") ?? "" : "")
    .filter((text) => text.length > 0)
    .join("\n");
}

function textFromAnthropicContent(value: unknown): string {
  if (typeof value === "string") return value;
  if (!Array.isArray(value)) return "";

  return value
    .map((part) => {
      if (typeof part === "string") return part;
      if (!isRecord(part)) return "";
      return getString(part, "text") ?? "";
    })
    .filter((text) => text.length > 0)
    .join("\n");
}

function buildOpenAiRequestDocument(body: JsonRecord): RequestLogPayloadDocument | null {
  const sections: RequestLogPayloadDocumentSection[] = [];
  const messages = getArray(body, "messages");

  if (messages.length > 0) {
    appendSection(
      sections,
      "Message transcript",
      messages.flatMap((message, index) => {
        if (!isRecord(message)) return [];
        const role = getString(message, "role") ?? `message ${index + 1}`;
        const value = textFromOpenAiContent(message.content);
        return value.length > 0 ? [{ label: role, value }] : [];
      }),
      "transcript",
    );
  }

  const input = body.input;
  if (typeof input === "string" && input.length > 0) {
    appendSection(sections, "Input", [{ label: "input", value: input }]);
  } else if (Array.isArray(input) && input.length > 0) {
    appendSection(sections, "Input", [{ label: "input", value: formatJson(input), mono: true }]);
  }

  const maxTokens = getNumber(body, "max_tokens") ?? getNumber(body, "max_output_tokens");
  if (maxTokens !== null) {
    appendSection(sections, "Generation config", [{ label: "max tokens", value: String(maxTokens), mono: true }]);
  }

  return sections.length > 0 ? { sections } : null;
}

function buildOpenAiResponseDocument(body: JsonRecord): RequestLogPayloadDocument | null {
  const sections: RequestLogPayloadDocumentSection[] = [];
  const choices = getArray(body, "choices");

  if (choices.length > 0) {
    appendSection(
      sections,
      "Response choices",
      choices.flatMap((choice, index) => {
        if (!isRecord(choice)) return [];
        const message = getRecord(choice, "message");
        const role = message ? getString(message, "role") ?? `choice ${index}` : `choice ${index}`;
        const content = message ? textFromOpenAiContent(message.content) : textFromOpenAiContent(choice.text);
        const finishReason = getString(choice, "finish_reason");
        const value = finishReason ? `${content}\nFinish reason: ${finishReason}` : content;
        return value.trim().length > 0 ? [{ label: role, value: value.trim() }] : [];
      }),
      "transcript",
    );
  }

  const output = getArray(body, "output");
  if (output.length > 0) {
    appendSection(
      sections,
      "Assistant output",
      output.flatMap((item, index) => {
        if (!isRecord(item)) return [];
        const role = getString(item, "role") ?? `output ${index + 1}`;
        const content = textFromOpenAiContent(item.content);
        return content.length > 0 ? [{ label: role, value: content }] : [];
      }),
      "transcript",
    );
  }

  const id = getString(body, "id");
  const status = getString(body, "status");
  appendSection(sections, "Response status", [
    ...(id ? [{ label: "id", value: id, mono: true }] : []),
    ...(status ? [{ label: "status", value: status, mono: true }] : []),
  ]);

  const usage = getRecord(body, "usage");
  if (usage) {
    appendSection(sections, "Usage", [{ label: "tokens", value: formatJson(usage), mono: true }]);
  }

  const hasOpenAiShape = choices.length > 0 || output.length > 0;
  return hasOpenAiShape && sections.length > 0 ? { sections } : null;
}

function buildGeminiRequestDocument(body: JsonRecord): RequestLogPayloadDocument | null {
  const sections: RequestLogPayloadDocumentSection[] = [];
  const systemInstruction = getRecord(body, "systemInstruction");
  const systemParts = systemInstruction ? getArray(systemInstruction, "parts") : [];
  const systemText = textFromGeminiParts(systemParts);

  appendSection(sections, "System instruction", [{ label: "system", value: systemText }]);
  appendSection(
    sections,
    "Content timeline",
    getArray(body, "contents").flatMap((content, index) => {
      if (!isRecord(content)) return [];
      const role = getString(content, "role") ?? `turn ${index + 1}`;
      const value = textFromGeminiParts(getArray(content, "parts"));
      return value.length > 0 ? [{ label: role, value }] : [];
    }),
    "transcript",
  );

  const generationConfig = getRecord(body, "generationConfig");
  if (generationConfig) {
    appendSection(sections, "Generation config", [{ label: "config", value: formatJson(generationConfig), mono: true }]);
  }

  return sections.length > 0 ? { sections } : null;
}

function buildGeminiResponseDocument(body: JsonRecord): RequestLogPayloadDocument | null {
  const sections: RequestLogPayloadDocumentSection[] = [];
  const candidates = getArray(body, "candidates");

  appendSection(
    sections,
    "Candidate responses",
    candidates.flatMap((candidate, index) => {
      if (!isRecord(candidate)) return [];
      const content = getRecord(candidate, "content");
      const role = content ? getString(content, "role") ?? `candidate ${index + 1}` : `candidate ${index + 1}`;
      const text = content ? textFromGeminiParts(getArray(content, "parts")) : "";
      const finishReason = getString(candidate, "finishReason");
      const value = finishReason ? `${text}\nFinish reason: ${finishReason}` : text;
      return value.trim().length > 0 ? [{ label: role, value: value.trim() }] : [];
    }),
    "transcript",
  );

  const usageMetadata = getRecord(body, "usageMetadata");
  if (usageMetadata) {
    appendSection(sections, "Usage", [{ label: "tokens", value: formatJson(usageMetadata), mono: true }]);
  }

  return candidates.length > 0 && sections.length > 0 ? { sections } : null;
}

function buildAnthropicRequestDocument(body: JsonRecord): RequestLogPayloadDocument | null {
  const sections: RequestLogPayloadDocumentSection[] = [];
  const system = body.system;

  if (typeof system === "string" && system.length > 0) {
    appendSection(sections, "System prompt", [{ label: "system", value: system }]);
  } else if (Array.isArray(system)) {
    const text = textFromAnthropicContent(system);
    appendSection(sections, "System prompt", [{ label: "system", value: text }]);
  }

  const messages = getArray(body, "messages");
  appendSection(
    sections,
    "Message exchange",
    messages.flatMap((message, index) => {
      if (!isRecord(message)) return [];
      const role = getString(message, "role") ?? `message ${index + 1}`;
      const value = textFromAnthropicContent(message.content);
      return value.length > 0 ? [{ label: role, value }] : [];
    }),
    "transcript",
  );

  const maxTokens = getNumber(body, "max_tokens");
  if (maxTokens !== null) {
    appendSection(sections, "Generation config", [{ label: "max tokens", value: String(maxTokens), mono: true }]);
  }

  return sections.length > 0 ? { sections } : null;
}

function buildAnthropicResponseDocument(body: JsonRecord): RequestLogPayloadDocument | null {
  const sections: RequestLogPayloadDocumentSection[] = [];
  const content = body.content;

  if (Array.isArray(content)) {
    const text = textFromAnthropicContent(content);
    appendSection(sections, "Assistant content", [{ label: getString(body, "role") ?? "assistant", value: text }], "transcript");
  }

  const stopReason = getString(body, "stop_reason");
  if (stopReason) {
    appendSection(sections, "Stop reason", [{ label: "reason", value: stopReason, mono: true }]);
  }

  const usage = getRecord(body, "usage");
  if (usage) {
    appendSection(sections, "Usage", [{ label: "tokens", value: formatJson(usage), mono: true }]);
  }

  return Array.isArray(content) && sections.length > 0 ? { sections } : null;
}

export function buildRequestLogPayloadDocument({
  apiFamily,
  bodyKind,
  content,
}: BuildPayloadDocumentParams): RequestLogPayloadDocument | null {
  const body = parseJsonRecord(content);
  if (!body) return null;

  if (apiFamily === "openai") {
    return (bodyKind === "request" ? buildOpenAiRequestDocument(body) : buildOpenAiResponseDocument(body))
      ?? buildGenericJsonDocumentFromValue(body);
  }

  if (apiFamily === "gemini") {
    return (bodyKind === "request" ? buildGeminiRequestDocument(body) : buildGeminiResponseDocument(body))
      ?? buildGenericJsonDocumentFromValue(body);
  }

  return (bodyKind === "request" ? buildAnthropicRequestDocument(body) : buildAnthropicResponseDocument(body))
    ?? buildGenericJsonDocumentFromValue(body);
}
