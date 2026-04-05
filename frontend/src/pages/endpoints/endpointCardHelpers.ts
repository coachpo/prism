import type { Endpoint, ModelConfigListItem } from "@/lib/types";

const MODEL_BADGE_STYLES = [
  "border-primary/25 bg-primary/10 text-primary",
] as const;

function hashString(value: string): number {
  let hash = 0;
  for (let i = 0; i < value.length; i += 1) {
    hash = (hash * 31 + value.charCodeAt(i)) >>> 0;
  }
  return hash;
}

function normalizeModelColorKey(value: string): string {
  return value.trim().toLowerCase().replace(/\s+/g, " ");
}

export function getModelBadgeClass(model: ModelConfigListItem): string {
  const colorKey = normalizeModelColorKey(model.display_name || model.model_id);
  return MODEL_BADGE_STYLES[hashString(colorKey) % MODEL_BADGE_STYLES.length];
}

export function getEndpointHost(baseUrl: string): string {
  try {
    return new URL(baseUrl).host;
  } catch {
    return baseUrl;
  }
}

export function buildDuplicateName(sourceName: string, existingNames: Set<string>): string {
  const baseName = `${sourceName.trim()} copy`;
  if (!existingNames.has(baseName)) {
    return baseName;
  }

  let suffix = 2;
  while (existingNames.has(`${baseName} ${suffix}`)) {
    suffix += 1;
  }
  return `${baseName} ${suffix}`;
}

export function getMaskedApiKey(endpoint: Endpoint): string {
  return endpoint.masked_api_key || "••••••";
}
