function normalizeVersionValue(value: string | undefined, fallback: string) {
  const normalizedValue = String(value ?? "").trim();
  return normalizedValue || fallback;
}

export const APP_VERSION = normalizeVersionValue(import.meta.env.VITE_APP_VERSION, "0.0.0");

export function formatVersionLabel(appVersion: string, gitRunNumber: string, gitRevision: string) {
  return `${normalizeVersionValue(appVersion, APP_VERSION)} (${normalizeVersionValue(gitRunNumber, "local")} - ${normalizeVersionValue(gitRevision, "unknown")})`;
}
