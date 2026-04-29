import type { AuthSettings, ProxyApiKey } from "@/lib/types";
import { getCurrentLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";

function getProxyKeyMessages() {
  return getStaticMessages();
}

export function getAuthStatusTone(authSettings: AuthSettings | null) {
  if (!authSettings) {
    return "border-muted-foreground/25 bg-muted text-muted-foreground";
  }

  return authSettings.auth_enabled
    ? "border-success/25 bg-success/10 text-success"
    : "border-warning/25 bg-warning/10 text-warning";
}

export function getProxyKeyUsagePercent(used: number, limit: number) {
  if (limit <= 0) {
    return 0;
  }

  return Math.min(Math.round((used / limit) * 100), 100);
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
    return "border-primary/25 bg-primary/10 text-primary";
  }
  if (isProxyKeyExpired(item.expires_at)) {
    return "border-warning/25 bg-warning/10 text-warning";
  }
  if (!item.is_active) {
    return "border-muted-foreground/25 bg-muted text-muted-foreground";
  }

  return authEnabled
    ? "border-success/25 bg-success/10 text-success"
    : "border-info/25 bg-info/10 text-info";
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
