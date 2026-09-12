import type { JsonValue } from "@/lib/types";

export type SettingKind = "text" | "number" | "toggle" | "empty" | "group" | "list";
export type SettingNode =
  | { kind: "text"; value: string }
  | { kind: "number"; value: string }
  | { kind: "toggle"; value: boolean }
  | { kind: "empty" }
  | { kind: "group"; entries: SettingEntry[] }
  | { kind: "list"; entries: SettingEntry[] };
export interface SettingEntry { id: number; name: string; node: SettingNode }

let nextEntryId = 0;
export function settingEntry(name: string, node: SettingNode): SettingEntry {
  return { id: ++nextEntryId, name, node };
}

export function settingNode(value: JsonValue): SettingNode {
  if (value === null) return { kind: "empty" };
  if (typeof value === "string") return { kind: "text", value };
  if (typeof value === "number") return { kind: "number", value: String(value) };
  if (typeof value === "boolean") return { kind: "toggle", value };
  if (Array.isArray(value)) return { kind: "list", entries: value.map((item) => settingEntry("", settingNode(item))) };
  return { kind: "group", entries: Object.entries(value).map(([name, item]) => settingEntry(name, settingNode(item))) };
}

export function emptySetting(kind: SettingKind): SettingNode {
  if (kind === "empty") return { kind };
  if (kind === "toggle") return { kind, value: false };
  if (kind === "list" || kind === "group") return { kind, entries: [] };
  return { kind, value: kind === "number" ? "0" : "" };
}

export function numericSettingValue(raw: string): number | null {
  const value = raw.trim();
  return /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(value) && Number.isFinite(Number(value)) ? Number(value) : null;
}

/** Preserve incomplete numbers and duplicate names until the shared validator accepts them. */
export function serializeSettings(node: SettingNode): string {
  if (node.kind === "empty") return "null";
  if (node.kind === "text") return JSON.stringify(node.value);
  if (node.kind === "number") {
    const value = numericSettingValue(node.value);
    return value === null ? "NaN" : JSON.stringify(value);
  }
  if (node.kind === "toggle") return String(node.value);
  const values = node.entries.map((entry) => node.kind === "group"
    ? `${JSON.stringify(entry.name)}:${serializeSettings(entry.node)}`
    : serializeSettings(entry.node));
  return node.kind === "group" ? `{${values.join(",")}}` : `[${values.join(",")}]`;
}
