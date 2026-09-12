import { getStaticMessages } from "@/i18n/staticMessages";
import type { PiOverrideField } from "./piOverrideDraft";

export const PI_FIELD_LABEL_KEYS: Record<PiOverrideField, string> = {
  name: "overrideNameLabel",
  reasoning: "overrideReasoningLabel",
  input: "overrideInputLabel",
  context_window: "overrideContextWindowLabel",
  max_tokens: "overrideMaxTokensLabel",
  thinking_level_map: "overrideThinkingLevelMapLabel",
  compat: "overrideCompatLabel",
};

export function piFieldLabel(field: string): string {
  const copy = getStaticMessages().modelExportPage;
  const key = PI_FIELD_LABEL_KEYS[field as PiOverrideField];
  return key ? (copy as Record<string, string>)[key] : copy.unknownFieldLabel;
}

export function piInputLabel(value: string): string {
  const copy = getStaticMessages().modelExportPage;
  return value === "text" ? copy.inputText : value === "image" ? copy.inputImage : copy.inputOther;
}

export function piThinkingLabel(level: string): string {
  const copy = getStaticMessages().modelExportPage;
  const labels: Record<string, string> = {
    off: copy.thinkingOff,
    minimal: copy.thinkingMinimal,
    low: copy.thinkingLow,
    medium: copy.thinkingMedium,
    high: copy.thinkingHigh,
    xhigh: copy.thinkingExtraHigh,
    max: copy.thinkingMax,
  };
  return labels[level] ?? copy.unknownFieldLabel;
}

/** Human-readable values for saved metadata and refresh comparisons. */
export function piMetadataValueLabel(value: unknown, field: string): string | null {
  const copy = getStaticMessages().modelExportPage;
  if (value === null || value === undefined) return null;
  if (typeof value === "boolean") return value ? copy.overrideBooleanTrue : copy.overrideBooleanFalse;
  if (Array.isArray(value)) {
    return value.length ? value.map((item) => field === "input" ? piInputLabel(String(item)) : piMetadataValueLabel(item, "") ?? copy.optionUnset).join("、") : copy.noOptions;
  }
  if (typeof value === "object") {
    const entries = Object.entries(value);
    if (!entries.length) return copy.noOptions;
    return entries.map(([key, item]) => `${field === "thinking_level_map" ? piThinkingLabel(key) : key}：${piMetadataValueLabel(item, "") ?? copy.optionUnset}`).join("；");
  }
  return String(value);
}

export function piPreviewValueLabel(value: string | null, field: string): string | null {
  if (value === null) return null;
  if (field === "name") return value;
  try {
    return piMetadataValueLabel(JSON.parse(value), field);
  } catch {
    return field === "input" ? value.split(/[,、]\s*/).map(piInputLabel).join("、") : value;
  }
}
