function normalizeVersionValue(value: string | undefined, fallback: string) {
  const normalizedValue = String(value ?? "").trim();
  return normalizedValue || fallback;
}

export const APP_VERSION = normalizeVersionValue(import.meta.env.VITE_APP_VERSION, "0.0.0");

export function formatVersionLabel(appVersion: string, gitRunNumber: string, gitRevision: string) {
  return `${normalizeVersionValue(appVersion, APP_VERSION)}.${normalizeVersionValue(gitRunNumber, "local")}.${normalizeVersionValue(gitRevision, "unknown")}`;
}

const GIT_RUN_NUMBER = String(import.meta.env.VITE_GIT_RUN_NUMBER ?? "local").trim() || "local";
const GIT_REVISION = String(import.meta.env.VITE_GIT_REVISION ?? "unknown").trim() || "unknown";

export const VERSION_LABEL = formatVersionLabel(APP_VERSION, GIT_RUN_NUMBER, GIT_REVISION);
