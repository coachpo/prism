import type { AuthSettings, ProxyApiKey } from "@/lib/types";
import { getCurrentLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { OperatorBadgeIntent, OperatorStatusTier } from "@/shared/design-system";

function getProxyKeyMessages() {
  return getStaticMessages();
}

export function isAuthSettingsEnabled(authSettings: AuthSettings | null) {
  return authSettings?.auth_mode?.effective === "enabled" || authSettings?.auth_enabled === true;
}

/**
 * Enforcement state as a runtime tier. Auth off is `degraded` rather than
 * `idle`: keys exist and traffic flows, but nothing is checking them.
 */
export function getAuthStatusTier(authSettings: AuthSettings | null): OperatorStatusTier {
  if (!authSettings) {
    return "idle";
  }

  return isAuthSettingsEnabled(authSettings) ? "healthy" : "degraded";
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

export function getProxyKeyLifecycleLabel(item: ProxyApiKey, authEnabled: boolean) {
  const copy = getProxyKeyMessages().proxyApiKeys;

  if (isProxyKeyExpired(item.expires_at)) {
    return copy.expired;
  }
  if (!item.is_active) {
    return copy.retired;
  }

  return authEnabled ? copy.active : copy.prepared;
}

/**
 * 生命周期档位读的全是配置字段（expires_at / is_active / 鉴权开关），不是运行观测：
 * 只有「已过期」是上游当场拒绝的运行结果，仍走 failing；活跃 / 已准备 / 已退役
 * 三档改用非运行时色调。否则鉴权一开整列变绿，连一次请求都没承载过的僵尸密钥
 * 也会被读成「正在正常服务」。
 */
export function getProxyKeyLifecycleIntent(item: ProxyApiKey, authEnabled: boolean): OperatorBadgeIntent {
  if (isProxyKeyExpired(item.expires_at)) {
    return "failing";
  }
  if (!item.is_active) {
    return "muted";
  }

  return authEnabled ? "neutral" : "muted";
}

// Rotation is in-place, so a key carries its own rotation history instead of
// pointing at a predecessor or successor row.
export function getProxyKeyRotationLabel(item: ProxyApiKey) {
  const copy = getProxyKeyMessages().proxyApiKeys;

  if (item.rotation_count <= 0 || !item.rotated_at) {
    return copy.neverRotated;
  }

  return copy.rotatedTimes(item.rotation_count, formatDateTime(item.rotated_at));
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
