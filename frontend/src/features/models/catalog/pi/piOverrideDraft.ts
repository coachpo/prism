import type { PiOverrideFieldValue } from "@/lib/types";
import type { PiBindingMetadataWire } from "@/lib/types";

export const PI_OVERRIDE_FIELD_ORDER = [
  "name",
  "reasoning",
  "input",
  "context_window",
  "max_tokens",
  "thinking_level_map",
  "compat",
] as const;

export type PiOverrideField = (typeof PI_OVERRIDE_FIELD_ORDER)[number];

export type PiOverrideDraftEntry =
  | { mode: "restore" }
  | { mode: "value"; raw: string };

export type PiOverrideDraft = Partial<
  Record<PiOverrideField, PiOverrideDraftEntry>
>;

export type PiOverrideDraftError =
  | "boolean_required"
  | "input_invalid"
  | "integer_invalid"
  | "json_invalid"
  | "name_required"
  | "object_required"
  | "thinking_map_invalid";

export interface PiOverrideDraftResult {
  fields: Record<string, PiOverrideFieldValue>;
  errors: Partial<Record<PiOverrideField, PiOverrideDraftError>>;
}

const THINKING_LEVELS = new Set([
  "off",
  "minimal",
  "low",
  "medium",
  "high",
  "xhigh",
  "max",
]);

function parseObject(
  raw: string,
): Record<string, unknown> | PiOverrideDraftError {
  let value: unknown;
  try {
    value = JSON.parse(raw) as unknown;
  } catch {
    return "json_invalid";
  }
  if (value === null || Array.isArray(value) || typeof value !== "object") {
    return "object_required";
  }
  return value as Record<string, unknown>;
}

type ParsedValue =
  | { value: PiOverrideFieldValue }
  | { error: PiOverrideDraftError };

function parseValue(field: PiOverrideField, raw: string): ParsedValue {
  switch (field) {
    case "name":
      return raw.length > 0 ? { value: raw } : { error: "name_required" };
    case "reasoning":
      if (raw !== "true" && raw !== "false") {
        return { error: "boolean_required" };
      }
      return { value: raw === "true" };
    case "input": {
      const values =
        raw.trim() === "" ? [] : raw.split(",").map((item) => item.trim());
      if (values.some((value) => value !== "text" && value !== "image")) {
        return { error: "input_invalid" };
      }
      return { value: values };
    }
    case "context_window":
    case "max_tokens": {
      if (!/^[1-9]\d*$/.test(raw.trim())) {
        return { error: "integer_invalid" };
      }
      const value = Number(raw.trim());
      return Number.isSafeInteger(value)
        ? { value }
        : { error: "integer_invalid" };
    }
    case "thinking_level_map": {
      const value = parseObject(raw);
      if (typeof value === "string") return { error: value };
      for (const [key, item] of Object.entries(value)) {
        if (
          !THINKING_LEVELS.has(key) ||
          (item !== null && typeof item !== "string")
        ) {
          return { error: "thinking_map_invalid" };
        }
      }
      return { value: value as Record<string, string | null> };
    }
    case "compat": {
      const value = parseObject(raw);
      return typeof value === "string" ? { error: value } : { value };
    }
  }
}

/** Builds a sparse PUT payload: absent draft entries never touch storage. */
export function buildPiOverrideFields(
  draft: PiOverrideDraft,
): PiOverrideDraftResult {
  const fields: Record<string, PiOverrideFieldValue> = {};
  const errors: Partial<Record<PiOverrideField, PiOverrideDraftError>> = {};

  for (const field of PI_OVERRIDE_FIELD_ORDER) {
    const entry = draft[field];
    if (!entry) continue;
    if (entry.mode === "restore") {
      fields[field] = null;
      continue;
    }
    const parsed = parseValue(field, entry.raw);
    if ("error" in parsed) {
      errors[field] = parsed.error;
      continue;
    }
    fields[field] = parsed.value;
  }

  return { fields, errors };
}

export function piBindingMetadataValue(
  metadata: PiBindingMetadataWire | null | undefined,
  field: PiOverrideField,
): PiOverrideFieldValue | undefined {
  const value = metadata?.[field];
  return value === null || value === undefined ? undefined : value;
}

export function piOverrideValueToRaw(
  metadata: PiBindingMetadataWire | null | undefined,
  field: PiOverrideField,
): string {
  const value = piBindingMetadataValue(metadata, field);
  if (value === undefined) return "";
  if (field === "input") return (value as string[]).join(", ");
  if (field === "thinking_level_map" || field === "compat") {
    return JSON.stringify(value, null, 2);
  }
  return String(value);
}

/**
 * 与 models.dev 面板共用同一套呈现规则：布尔是「是/否」而不是 true/false，
 * 数组用顿号连接。同一个字段在两个目录面板里长成两个样子会让人怀疑数据不同。
 */
export function formatPiBindingMetadataValue(
  metadata: PiBindingMetadataWire | null | undefined,
  field: PiOverrideField,
): string | null {
  const value = piBindingMetadataValue(metadata, field);
  if (value === undefined || value === null) return null;
  if (Array.isArray(value)) return value.join("、");
  if (typeof value === "boolean") return value ? "是" : "否";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}
