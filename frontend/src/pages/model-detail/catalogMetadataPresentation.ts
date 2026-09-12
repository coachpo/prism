import type { Messages } from "@/i18n/messages";
import type { ModelCatalogMetadata } from "@/lib/types";
import { getStaticMessages } from "@/i18n/staticMessages";

export type CatalogFieldKey = keyof ModelCatalogMetadata;
export type CatalogFieldKind =
  | "string"
  | "date"
  | "boolean"
  | "string_list"
  | "integer"
  | "status";

// Stable display order is also the address space used by the override editor.
export const CATALOG_FIELD_ORDER: CatalogFieldKey[] = [
  "name",
  "description",
  "family",
  "release_date",
  "last_updated",
  "knowledge",
  "reasoning",
  "tool_call",
  "structured_output",
  "temperature",
  "attachment",
  "modalities_input",
  "modalities_output",
  "limit_context",
  "limit_input",
  "limit_output",
  "open_weights",
  "status",
];

export const CATALOG_FIELD_KINDS: Record<CatalogFieldKey, CatalogFieldKind> = {
  name: "string",
  description: "string",
  family: "string",
  release_date: "date",
  last_updated: "date",
  knowledge: "date",
  attachment: "boolean",
  reasoning: "boolean",
  tool_call: "boolean",
  structured_output: "boolean",
  temperature: "boolean",
  modalities_input: "string_list",
  modalities_output: "string_list",
  limit_context: "integer",
  limit_input: "integer",
  limit_output: "integer",
  open_weights: "boolean",
  status: "status",
};

export function renderCatalogFieldValue(
  metadata: Partial<ModelCatalogMetadata> | null,
  key: CatalogFieldKey,
): string | null {
  const value = metadata?.[key];
  if (value === null || value === undefined) return null;
  const copy = getStaticMessages().modelCatalog;
  if (Array.isArray(value)) return value.length ? value.map(catalogModalityLabel).join("、") : copy.formatNone;
  if (typeof value === "boolean") return value ? copy.overrideBooleanTrue : copy.overrideBooleanFalse;
  if (key === "status") return catalogStatusLabel(String(value));
  return String(value);
}

export function catalogModalityLabel(value: string): string {
  const copy = getStaticMessages().modelCatalog;
  const labels: Record<string, string> = { text: copy.formatText, image: copy.formatImage, audio: copy.formatAudio, video: copy.formatVideo, pdf: copy.formatPdf };
  return labels[value] ?? copy.formatOther;
}

export function catalogStatusLabel(value: string): string {
  const copy = getStaticMessages().modelCatalog;
  const labels: Record<string, string> = { alpha: copy.statusAlpha, beta: copy.statusBeta, deprecated: copy.statusDeprecated };
  return labels[value] ?? copy.statusUnknown;
}

export function renderCatalogPreviewValue(value: string | null, field: string): string | null {
  if (value === null) return null;
  let parsed: unknown = value;
  try { parsed = JSON.parse(value); } catch { /* Plain catalog values need no decoding. */ }
  return renderCatalogFieldValue({ [field]: parsed }, field as CatalogFieldKey);
}

export function catalogFieldLabel(
  copy: Messages["modelCatalog"],
  key: CatalogFieldKey,
): string {
  const labels: Record<CatalogFieldKey, string> = {
    name: copy.fieldName,
    description: copy.fieldDescription,
    family: copy.fieldFamily,
    release_date: copy.fieldReleaseDate,
    last_updated: copy.fieldLastUpdated,
    knowledge: copy.fieldKnowledge,
    reasoning: copy.fieldReasoning,
    tool_call: copy.fieldToolCall,
    structured_output: copy.fieldStructuredOutput,
    temperature: copy.fieldTemperature,
    attachment: copy.fieldAttachment,
    modalities_input: copy.fieldModalitiesInput,
    modalities_output: copy.fieldModalitiesOutput,
    limit_context: copy.fieldLimitContext,
    limit_input: copy.fieldLimitInput,
    limit_output: copy.fieldLimitOutput,
    open_weights: copy.fieldOpenWeights,
    status: copy.fieldStatus,
  };
  return labels[key] ?? copy.fieldUnknown;
}
