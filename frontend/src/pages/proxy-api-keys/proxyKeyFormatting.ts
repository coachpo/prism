import type { AuthSettings, ProxyApiKey } from "@/lib/types";
import { getCurrentLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";

function getProxyKeyMessages() {
  return getStaticMessages();
}

export function getAuthStatusTone(authSettings: AuthSettings | null) {
  if (!authSettings) {
    return "border-slate-300/70 bg-slate-100/80 text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200";
  }

  return authSettings.auth_enabled
    ? "border-emerald-300/60 bg-emerald-100/70 text-emerald-900 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200"
    : "border-amber-300/60 bg-amber-100/70 text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200";
}

export function toDateTimeLocalValue(value: string | null) {
  if (!value) {
    return "";
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }

  const pad = (segment: number) => String(segment).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function normalizeExpiresAtInput(value: string) {
  const trimmed = value.trim();
  if (!trimmed) {
    return null;
  }

  const date = new Date(trimmed);
  if (Number.isNaN(date.getTime())) {
    return null;
  }

  return date.toISOString();
}

export function isProxyKeyExpired(expiresAt: string | null, now = Date.now()) {
  if (!expiresAt) {
    return false;
  }

  const expiresAtMs = new Date(expiresAt).getTime();
  return Number.isFinite(expiresAtMs) && expiresAtMs <= now;
}

export function getProxyKeyLifecycleLabel(
  item: ProxyApiKey,
  authEnabled: boolean,
  successorId: number | null,
) {
  const copy = getProxyKeyMessages().proxyApiKeys;

  if (successorId !== null) {
    return copy.rotated;
  }
  if (isProxyKeyExpired(item.expires_at)) {
    return copy.expired;
  }
  if (!item.is_active) {
    return copy.retired;
  }

  return authEnabled ? copy.active : copy.prepared;
}

export function getProxyKeyLifecycleTone(
  item: ProxyApiKey,
  authEnabled: boolean,
  successorId: number | null,
) {
  if (successorId !== null) {
    return "border-violet-500/25 bg-violet-500/10 text-violet-700 dark:text-violet-400";
  }
  if (isProxyKeyExpired(item.expires_at)) {
    return "border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-400";
  }
  if (!item.is_active) {
    return "border-slate-300/70 bg-slate-100/80 text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200";
  }

  return authEnabled
    ? "border-emerald-300/60 bg-emerald-100/70 text-emerald-900 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200"
    : "border-sky-300/70 bg-sky-100/80 text-sky-900 dark:border-sky-900/80 dark:bg-sky-950/30 dark:text-sky-200";
}

export function getProxyKeyLineageLabel(item: ProxyApiKey, successorId: number | null) {
  const copy = getProxyKeyMessages().proxyApiKeys;

  if (successorId !== null) {
    return copy.rotatedTo(successorId);
  }
  if (item.rotated_from_id !== null) {
    return copy.rotatedFrom(item.rotated_from_id);
  }

  return copy.currentKey;
}

export function formatDateTime(value: string | null, fallback = "Unknown") {
  const messages = getProxyKeyMessages();
  if (!value) {
    return fallback === "Unknown" ? messages.proxyApiKeys.unknown : fallback;
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return date.toLocaleString(getCurrentLocale(), {
    dateStyle: "medium",
    timeStyle: "short",
  });
}

export function formatLastUsed(value: string | null) {
  return formatDateTime(value, getProxyKeyMessages().proxyApiKeys.never);
}
