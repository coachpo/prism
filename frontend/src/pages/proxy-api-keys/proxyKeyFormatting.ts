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

export function getRuntimeStatusLabel(item: ProxyApiKey) {
  const messages = getProxyKeyMessages();
  if (!item.is_active) {
    return messages.proxyApiKeys.disabled;
  }

  return messages.proxyApiKeys.active;
}

export function getRuntimeStatusTone(item: ProxyApiKey, authEnabled: boolean) {
  if (!item.is_active) {
    return "border-slate-300/70 bg-slate-100/80 text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200";
  }

  return authEnabled
    ? "border-emerald-300/60 bg-emerald-100/70 text-emerald-900 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200"
    : "border-sky-300/70 bg-sky-100/80 text-sky-900 dark:border-sky-900/80 dark:bg-sky-950/30 dark:text-sky-200";
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
