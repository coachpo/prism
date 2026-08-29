import type {
  CatalogOverridePatch,
  ModelCatalogMetadata,
} from "@/lib/types";
import {
  CATALOG_FIELD_KINDS,
  type CatalogFieldKey,
} from "./catalogMetadataPresentation";

export type CatalogOverrideDraftEntry =
  | { mode: "restore" }
  | { mode: "value"; raw: string };

export type CatalogOverrideDraft = Partial<
  Record<CatalogFieldKey, CatalogOverrideDraftEntry>
>;

export interface CatalogOverrideDraftResult {
  patch: CatalogOverridePatch;
  errors: Partial<Record<CatalogFieldKey, string>>;
}

const STATUS_VALUES = new Set(["alpha", "beta", "deprecated"]);
const utf8Length = (value: string) => new TextEncoder().encode(value).length;

/** Converts typed controls into the exact missing/null/value wire semantics. */
export function buildCatalogOverridePatch(
  draft: CatalogOverrideDraft,
): CatalogOverrideDraftResult {
  const patch: Record<string, unknown> = {};
  const errors: Partial<Record<CatalogFieldKey, string>> = {};

  for (const [rawKey, entry] of Object.entries(draft)) {
    const key = rawKey as CatalogFieldKey;
    if (!entry) continue;
    if (entry.mode === "restore") {
      patch[key] = null;
      continue;
    }

    const kind = CATALOG_FIELD_KINDS[key];
    if (kind === "string" || kind === "date") {
      if (utf8Length(entry.raw) > 500) {
        errors[key] = "字符串不能超过 500 个字符";
        continue;
      }
      if (
        kind === "date" &&
        entry.raw.trim() !== "" &&
        !/^\d{4}-\d{2}(?:-\d{2})?$/.test(entry.raw.trim())
      ) {
        errors[key] = "日期必须为 YYYY-MM 或 YYYY-MM-DD";
        continue;
      }
      // Do not trim: an empty string is a value; only mode=restore emits null.
      patch[key] = entry.raw;
      continue;
    }
    if (kind === "boolean") {
      if (entry.raw !== "true" && entry.raw !== "false") {
        errors[key] = "请选择是或否";
        continue;
      }
      patch[key] = entry.raw === "true";
      continue;
    }
    if (kind === "integer") {
      if (!/^(?:0|[1-9]\d*)$/.test(entry.raw)) {
        errors[key] = "请输入非负整数";
        continue;
      }
      const parsed = Number(entry.raw);
      if (!Number.isSafeInteger(parsed)) {
        errors[key] = "整数超出浏览器可安全表示范围";
        continue;
      }
      patch[key] = parsed;
      continue;
    }
    if (kind === "string_list") {
      const values = entry.raw.trim() === ""
        ? []
        : entry.raw.split(",").map((item) => item.trim());
      if (values.some((item) => utf8Length(item) > 500)) {
        errors[key] = "列表项不能超过 500 个字符";
        continue;
      }
      patch[key] = values;
      continue;
    }
    if (!STATUS_VALUES.has(entry.raw)) {
      errors[key] = "请选择 alpha、beta 或 deprecated";
      continue;
    }
    patch[key] = entry.raw;
  }

  return { patch: patch as CatalogOverridePatch, errors };
}

export function catalogOverrideValueToRaw(
  metadata: ModelCatalogMetadata | null,
  key: CatalogFieldKey,
): string {
  const value = metadata?.[key];
  if (value === null || value === undefined) return "";
  if (Array.isArray(value)) return value.join(", ");
  return String(value);
}
