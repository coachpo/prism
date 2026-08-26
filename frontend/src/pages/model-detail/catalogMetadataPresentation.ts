import type { Messages } from "@/i18n/messages";
import type { ModelCatalogMetadata } from "@/lib/types";

export type CatalogFieldKey = keyof ModelCatalogMetadata;

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

export const CATALOG_OVERRIDE_TEXT_FIELDS: CatalogFieldKey[] = [
  "name",
  "description",
  "family",
  "release_date",
  "last_updated",
  "knowledge",
  "status",
];

export function renderCatalogFieldValue(
  metadata: ModelCatalogMetadata | null,
  key: CatalogFieldKey,
): string | null {
  const value = metadata?.[key];
  if (value === null || value === undefined) return null;
  if (Array.isArray(value)) return value.join("、");
  if (typeof value === "boolean") return value ? "是" : "否";
  return String(value);
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
  return labels[key];
}
