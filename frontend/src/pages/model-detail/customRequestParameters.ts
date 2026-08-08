import type { JsonObject } from "@/lib/types";

/**
 * Client-side parser and validator for the Connection custom request
 * parameters JSON editor. The rules mirror the backend shared validator in
 * backend/internal/domain/terminaltarget so the editor fails closed on the
 * same inputs the management API rejects.
 */

/**
 * Top-level member count of a configured value (0 when unconfigured).
 */
export function customRequestParametersTopLevelCount(value: JsonObject | null): number {
  return value ? Object.keys(value).length : 0;
}

export const CUSTOM_REQUEST_PARAMETERS_MAX_COMPACT_BYTES = 65536;
export const CUSTOM_REQUEST_PARAMETERS_MAX_DEPTH = 16;
export const CUSTOM_REQUEST_PARAMETERS_MAX_MEMBERS = 256;
export const CUSTOM_REQUEST_PARAMETERS_SAFE_INTEGER_MIN = -(2 ** 53 - 1);
export const CUSTOM_REQUEST_PARAMETERS_SAFE_INTEGER_MAX = 2 ** 53 - 1;

export const CUSTOM_REQUEST_PARAMETERS_PROTECTED_KEYS = [
  "model",
  "models",
  "stream",
  "messages",
  "input",
  "contents",
  "instructions",
  "system",
  "systemInstruction",
] as const;

export interface CustomRequestParametersParseError {
  reason:
    | "not_object"
    | "duplicate_key"
    | "blank_key"
    | "protected_field"
    | "too_large"
    | "too_deep"
    | "too_many_members"
    | "number_out_of_range";
  path: string;
  limit?: number;
}

export interface CustomRequestParametersParseResult {
  value: JsonObject | null;
  error: CustomRequestParametersParseError | null;
}

/**
 * Parses and validates the editor draft. Blank text and `{}` normalize to
 * null (unconfigured). Returns the parsed object only when the whole input
 * satisfies the shared contract; otherwise returns a locatable error.
 */
export function parseCustomRequestParametersDraft(
  draft: string,
): CustomRequestParametersParseResult {
  const trimmed = draft.trim();
  if (trimmed.length === 0) {
    return { value: null, error: null };
  }

  const scanError = scanCustomRequestParametersJson(trimmed);
  if (scanError) {
    return { value: null, error: scanError };
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return {
      value: null,
      error: { reason: "not_object", path: "custom_request_parameters" },
    };
  }
  if (parsed === null) {
    return { value: null, error: null };
  }
  if (typeof parsed !== "object" || Array.isArray(parsed)) {
    return {
      value: null,
      error: { reason: "not_object", path: "custom_request_parameters" },
    };
  }

  const compact = JSON.stringify(parsed);
  const compactByteLength = new TextEncoder().encode(compact).byteLength;
  if (compactByteLength > CUSTOM_REQUEST_PARAMETERS_MAX_COMPACT_BYTES) {
    return {
      value: null,
      error: {
        reason: "too_large",
        path: "custom_request_parameters",
        limit: CUSTOM_REQUEST_PARAMETERS_MAX_COMPACT_BYTES,
      },
    };
  }
  if (Object.keys(parsed as JsonObject).length === 0) {
    return { value: null, error: null };
  }
  return { value: parsed as JsonObject, error: null };
}

/**
 * Scans the raw JSON text for duplicate keys, blank keys, protected top-level
 * keys, depth/member limits, and out-of-range numbers without relying on
 * JSON.parse (which silently keeps the last duplicate key). Returns the
 * shallowest locatable failure or null when the text is structurally valid.
 */
function scanCustomRequestParametersJson(raw: string): CustomRequestParametersParseError | null {
  const scanner = new CustomRequestParametersScanner(raw);
  return scanner.scanRoot();
}

class CustomRequestParametersScanner {
  private readonly text: string;
  private index = 0;
  private members = 0;

  constructor(text: string) {
    this.text = text;
  }

  scanRoot(): CustomRequestParametersParseError | null {
    this.skipWhitespace();
    if (this.text.slice(this.index, this.index + 4) === "null") {
      this.index += 4;
      this.skipWhitespace();
      if (this.index !== this.text.length) {
        return { reason: "not_object", path: "custom_request_parameters" };
      }
      return null;
    }
    if (this.peek() !== "{") {
      return { reason: "not_object", path: "custom_request_parameters" };
    }
    const error = this.scanObject(1, "custom_request_parameters");
    if (error) {
      return error;
    }
    this.skipWhitespace();
    if (this.index !== this.text.length) {
      return { reason: "not_object", path: "custom_request_parameters" };
    }
    return null;
  }

  private scanObject(depth: number, path: string): CustomRequestParametersParseError | null {
    if (depth > CUSTOM_REQUEST_PARAMETERS_MAX_DEPTH) {
      return { reason: "too_deep", path, limit: CUSTOM_REQUEST_PARAMETERS_MAX_DEPTH };
    }
    this.expect("{");
    const seen = new Set<string>();
    for (;;) {
      this.skipWhitespace();
      if (this.peek() === "}") {
        this.index += 1;
        return null;
      }
      if (this.peek() !== '"') {
        return { reason: "not_object", path };
      }
      const key = this.scanString();
      this.members += 1;
      if (this.members > CUSTOM_REQUEST_PARAMETERS_MAX_MEMBERS) {
        return { reason: "too_many_members", path, limit: CUSTOM_REQUEST_PARAMETERS_MAX_MEMBERS };
      }
      if (key.trim().length === 0) {
        return { reason: "blank_key", path };
      }
      if (seen.has(key)) {
        return { reason: "duplicate_key", path: `${path}.${key}` };
      }
      seen.add(key);
      if (depth === 1 && (CUSTOM_REQUEST_PARAMETERS_PROTECTED_KEYS as readonly string[]).includes(key)) {
        return { reason: "protected_field", path: `${path}.${key}` };
      }
      this.skipWhitespace();
      this.expect(":");
      const childPath = `${path}.${key}`;
      const error = this.scanValue(depth + 1, childPath);
      if (error) {
        return error;
      }
      this.skipWhitespace();
      const separator = this.peek();
      if (separator === ",") {
        this.index += 1;
        continue;
      }
      if (separator === "}") {
        this.index += 1;
        return null;
      }
      return { reason: "not_object", path };
    }
  }

  private scanValue(depth: number, path: string): CustomRequestParametersParseError | null {
    this.skipWhitespace();
    const character = this.peek();
    if (character === "{") {
      return this.scanObject(depth, path);
    }
    if (character === "[") {
      return this.scanArray(depth, path);
    }
    if (character === '"') {
      this.scanString();
      return null;
    }
    if (character === "t") {
      return this.consumeLiteral("true", path);
    }
    if (character === "f") {
      return this.consumeLiteral("false", path);
    }
    if (character === "n") {
      return this.consumeLiteral("null", path);
    }
    if (character === "-" || (character >= "0" && character <= "9")) {
      return this.scanNumber(path);
    }
    return { reason: "not_object", path };
  }

  private scanArray(depth: number, path: string): CustomRequestParametersParseError | null {
    if (depth > CUSTOM_REQUEST_PARAMETERS_MAX_DEPTH) {
      return { reason: "too_deep", path, limit: CUSTOM_REQUEST_PARAMETERS_MAX_DEPTH };
    }
    this.expect("[");
    for (;;) {
      this.skipWhitespace();
      if (this.peek() === "]") {
        this.index += 1;
        return null;
      }
      const error = this.scanValue(depth + 1, path);
      if (error) {
        return error;
      }
      this.skipWhitespace();
      const separator = this.peek();
      if (separator === ",") {
        this.index += 1;
        continue;
      }
      if (separator === "]") {
        this.index += 1;
        return null;
      }
      return { reason: "not_object", path };
    }
  }

  private scanString(): string {
    const start = this.index;
    this.expect('"');
    let result = "";
    for (;;) {
      const character = this.text[this.index];
      if (character === undefined) {
        break;
      }
      this.index += 1;
      if (character === '"') {
        const encoded = this.text.slice(start, this.index);
        try {
          const decoded = JSON.parse(encoded);
          return typeof decoded === "string" ? decoded : result;
        } catch {
          return result;
        }
      }
      if (character === "\\") {
        result += this.text[this.index] ?? "";
        this.index += 1;
        continue;
      }
      result += character;
    }
    return result;
  }

  private scanNumber(path: string): CustomRequestParametersParseError | null {
    const start = this.index;
    if (this.peek() === "-") {
      this.index += 1;
    }
    while (this.index < this.text.length && /[0-9]/.test(this.text[this.index] ?? "")) {
      this.index += 1;
    }
    let isFloating = false;
    if (this.peek() === ".") {
      isFloating = true;
      this.index += 1;
      while (this.index < this.text.length && /[0-9]/.test(this.text[this.index] ?? "")) {
        this.index += 1;
      }
    }
    if (this.peek() === "e" || this.peek() === "E") {
      isFloating = true;
      this.index += 1;
      if (this.peek() === "+" || this.peek() === "-") {
        this.index += 1;
      }
      while (this.index < this.text.length && /[0-9]/.test(this.text[this.index] ?? "")) {
        this.index += 1;
      }
    }
    const literal = this.text.slice(start, this.index);
    const numeric = Number(literal);
    if (!Number.isFinite(numeric)) {
      return { reason: "number_out_of_range", path };
    }
    if (!isFloating && !Number.isSafeInteger(numeric)) {
      return { reason: "number_out_of_range", path };
    }
    return null;
  }

  private consumeLiteral(literal: string, path: string): CustomRequestParametersParseError | null {
    if (this.text.slice(this.index, this.index + literal.length) !== literal) {
      return { reason: "not_object", path };
    }
    this.index += literal.length;
    return null;
  }

  private skipWhitespace(): void {
    while (this.index < this.text.length && /\s/.test(this.text[this.index] ?? "")) {
      this.index += 1;
    }
  }

  private peek(): string {
    return this.text[this.index] ?? "";
  }

  private expect(character: string): void {
    if (this.peek() === character) {
      this.index += 1;
    }
  }
}

/**
 * Serializes the current draft for the edit form: empty means unconfigured,
 * otherwise the formatted object.
 */
export function customRequestParametersDraftFromValue(
  value: JsonObject | null | undefined,
): string {
  if (!value) {
    return "";
  }
  return JSON.stringify(value, null, 2);
}
