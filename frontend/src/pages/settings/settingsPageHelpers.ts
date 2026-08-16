import type { CostingSettingsResponse, CostingSettingsUpdate } from "@/lib/types";
import { getCurrentLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";

// Settings canonical URL contract (Settings SPEC §7.2/§12.2): public scope is
// exactly `global|instance`, sections are allowlisted per scope, and the
// legacy `tab` parameter is invalid and dropped during canonicalization.

export const SETTINGS_SCOPES = {
  global: "global",
  instance: "instance",
} as const;

export type SettingsScope = (typeof SETTINGS_SCOPES)[keyof typeof SETTINGS_SCOPES];

export const GLOBAL_SECTIONS = [
  { id: "billing-currency" },
  { id: "timezone" },
  { id: "audit-privacy" },
  { id: "header-blocklist" },
  { id: "client-rules" },
] as const;

export const INSTANCE_SECTIONS = [
  { id: "authentication" },
  { id: "retention" },
  { id: "manual-cleanup" },
  { id: "retention-jobs" },
] as const;

// The directory lists cards; `GLOBAL_SECTIONS` stays the URL allowlist.
// `timezone` is no longer its own card — it is an anchor inside the merged
// basis-and-display card — so the shipped `?section=timezone` deep link still
// resolves while the directory shows one entry per card.
export const GLOBAL_NAV_SECTIONS = [
  { id: "billing-currency" },
  { id: "audit-privacy" },
  { id: "header-blocklist" },
  { id: "client-rules" },
] as const;

export const INSTANCE_NAV_SECTIONS = INSTANCE_SECTIONS;

export const GLOBAL_SECTION_IDS = new Set<string>(GLOBAL_SECTIONS.map((section) => section.id));
export const INSTANCE_SECTION_IDS = new Set<string>(INSTANCE_SECTIONS.map((section) => section.id));

export const DEFAULT_SECTION_BY_SCOPE: Record<SettingsScope, string> = {
  global: "billing-currency",
  instance: "authentication",
};

// Billing-currency section-owned Pricing parameters (SPEC §12.2): allowlisted
// only under an explicit section=billing-currency.
export const CURRENCY_COSTING_ACTIONS = new Set(["currency_cutover", "repair_same_currency", "archive_unused_fx"]);

export type CleanupType = "" | "requests" | "audits" | "loadbalance_events" | "statistics";
export type DeleteCleanupType = Exclude<CleanupType, "">;
export type RetentionPreset = "" | "1" | "7" | "30" | "90" | "all";

// Destructive retention confirmation has no client-side keyword: the server
// issues `confirmation_keyword` with each preflight and compares it exactly.
export const AUTH_PASSWORD_MIN_LENGTH = 8;
export const AUTH_PASSWORD_MAX_LENGTH = 512;

function getSettingsMessages() {
  return getStaticMessages();
}

export function getCleanupTypeLabel(type: DeleteCleanupType): string {
  const messages = getSettingsMessages();

  switch (type) {
    case "requests":
      return messages.settingsDialogs.cleanupTypeRequests;
    case "audits":
      return messages.settingsDialogs.cleanupTypeAudits;
    case "loadbalance_events":
      return messages.settingsDialogs.cleanupTypeLoadbalanceEvents;
    case "statistics":
      return messages.settingsDialogs.cleanupTypeStatistics;
  }
}

export function getCleanupRetentionLabel(deleteAll: boolean, days: number | null): string {
  const messages = getSettingsMessages();

  if (deleteAll) {
    return messages.settingsDialogs.allData;
  }

  return messages.settingsDialogs.olderThanDays(days);
}

export const DEFAULT_COSTING_FORM: CostingSettingsUpdate = {
  report_currency_code: "USD",
  report_currency_symbol: "$",
  timezone_preference: null,
};

export const normalizeCostingForm = (
  form: CostingSettingsUpdate | CostingSettingsResponse,
): CostingSettingsUpdate => ({
  ...form,
  // A pending/legacy migration is deliberately represented by nullable
  // server fields. The editable form keeps those fields as empty strings so
  // ordinary billing validation cannot accidentally author a new epoch.
  report_currency_code: form.report_currency_code?.trim().toUpperCase() ?? "",
  report_currency_symbol: form.report_currency_symbol?.trim() ?? "",
  timezone_preference: form.timezone_preference ?? null,
  expected_updated_at: "expected_updated_at" in form ? form.expected_updated_at ?? null : null,
  reporting_currency_epoch: form.reporting_currency_epoch ?? undefined,
});

export const validateAuthPassword = (value: string): string | null => {
  const messages = getSettingsMessages();
  if (!value) {
    return null;
  }
  if (value.length < AUTH_PASSWORD_MIN_LENGTH) {
    return messages.settingsAuth.passwordMinLength(AUTH_PASSWORD_MIN_LENGTH);
  }
  if (value.length > AUTH_PASSWORD_MAX_LENGTH) {
    return messages.settingsAuth.passwordMaxLength(AUTH_PASSWORD_MAX_LENGTH);
  }
  return null;
};

// Current-instant timezone preview (SPEC §11.2): the preview always uses the
// current clock instant and IANA current offset; the fixed 2026-02-27 source
// was removed. The caller supplies the instant so tests stay deterministic.
export const formatTimezoneOffset = (timezone: string, instant: Date = new Date()): string => {
  try {
    const parts = new Intl.DateTimeFormat("en-US", {
      timeZone: timezone,
      timeZoneName: "longOffset",
    }).formatToParts(instant);
    return parts.find((part) => part.type === "timeZoneName")?.value?.replace("GMT", "UTC") ?? "UTC";
  } catch {
    return "UTC";
  }
};

export const formatTimezonePreview = (timezone: string, instant: Date = new Date()): string => {
  const messages = getSettingsMessages();
  try {
    const parts = new Intl.DateTimeFormat(getCurrentLocale(), {
      timeZone: timezone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    }).formatToParts(instant);

    const byType = new Map(parts.map((part) => [part.type, part.value]));
    const year = byType.get("year") ?? "0000";
    const month = byType.get("month") ?? "00";
    const day = byType.get("day") ?? "00";
    const hour = byType.get("hour") ?? "00";
    const minute = byType.get("minute") ?? "00";
    return `${year}-${month}-${day} ${hour}:${minute}`;
  } catch {
    return messages.settingsTimezone.unavailable;
  }
};
