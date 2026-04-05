import type { Connection, CostingSettingsUpdate, EndpointFxMapping } from "@/lib/types";
import {
  compareStringsForLocale,
  formatNumber,
  getCurrentLocale,
} from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";
import { isValidPositiveDecimalString } from "@/lib/costing";

export const SETTINGS_TABS = {
  profile: "profile",
  global: "global",
} as const;

export type SettingsTab = (typeof SETTINGS_TABS)[keyof typeof SETTINGS_TABS];

export const INSTANCE_SECTION_IDS = new Set(["authentication"]);

export const SETTINGS_SECTIONS = [
  { id: "backup" },
  { id: "billing-currency" },
  { id: "timezone" },
  { id: "audit-configuration" },
  { id: "retention-deletion" },
] as const;

export const SETTINGS_SECTION_IDS = new Set<string>(SETTINGS_SECTIONS.map((section) => section.id));

export type CleanupType = "" | "requests" | "audits" | "loadbalance_events" | "statistics";
export type DeleteCleanupType = Exclude<CleanupType, "">;
export type RetentionPreset = "" | "1" | "7" | "30" | "90" | "all";

export const DELETE_CONFIRM_KEYWORD = getStaticMessages().settingsDialogs.deleteConfirmKeyword;
export const FX_RATE_MAX_DECIMALS = 6;
export const TIMEZONE_PREVIEW_SOURCE = new Date("2026-02-27T21:39:00Z");
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

export const DEFAULT_COSTING_FORM: CostingSettingsUpdate = {
  report_currency_code: "USD",
  report_currency_symbol: "$",
  timezone_preference: null,
  endpoint_fx_mappings: [],
};

export const getConnectionName = (connection: Pick<Connection, "name">): string =>
  connection.name ?? "";

export const getMappingKey = (mapping: EndpointFxMapping): string =>
  `${mapping.model_id}::${mapping.endpoint_id}`;

export const normalizeMappings = (mappings: EndpointFxMapping[]): EndpointFxMapping[] =>
  [...mappings]
    .map((mapping) => ({
      ...mapping,
      fx_rate: mapping.fx_rate.trim(),
    }))
    .sort((a, b) => {
      if (a.model_id === b.model_id) {
        return a.endpoint_id - b.endpoint_id;
      }
      return compareStringsForLocale(a.model_id, b.model_id);
    });

export const normalizeCostingForm = (form: CostingSettingsUpdate): CostingSettingsUpdate => ({
  report_currency_code: form.report_currency_code.trim().toUpperCase(),
  report_currency_symbol: form.report_currency_symbol.trim(),
  timezone_preference: form.timezone_preference ?? null,
  endpoint_fx_mappings: normalizeMappings(form.endpoint_fx_mappings),
});

export const areMappingsEqual = (left: EndpointFxMapping[], right: EndpointFxMapping[]): boolean => {
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (
      left[index].model_id !== right[index].model_id ||
      left[index].endpoint_id !== right[index].endpoint_id ||
      left[index].fx_rate !== right[index].fx_rate
    ) {
      return false;
    }
  }
  return true;
};

export const validateFxRate = (rawValue: string): string | null => {
  const messages = getSettingsMessages();
  const value = rawValue.trim();
  if (!value) {
    return messages.settingsFx.rateRequired;
  }
  if (!isValidPositiveDecimalString(value)) {
    return messages.settingsFx.rateMustBeGreaterThanZero;
  }
  const [, decimals = ""] = value.split(".");
  if (decimals.length > FX_RATE_MAX_DECIMALS) {
    return messages.settingsFx.decimalPlacesLimit(FX_RATE_MAX_DECIMALS);
  }
  return null;
};

export const validateMappings = (mappings: EndpointFxMapping[]): string | null => {
  const messages = getSettingsMessages();
  const seen = new Set<string>();
  for (const mapping of mappings) {
    const key = getMappingKey(mapping);
    if (seen.has(key)) {
      return messages.settingsFx.duplicateMapping(mapping.model_id, mapping.endpoint_id);
    }
    seen.add(key);

    const fxRateError = validateFxRate(mapping.fx_rate);
    if (fxRateError) {
      return messages.settingsFx.rateForMapping(mapping.model_id, mapping.endpoint_id, fxRateError);
    }
  }
  return null;
};

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

export const formatFxRateDisplay = (value: string): string => {
  const numeric = Number.parseFloat(value);
  if (!Number.isFinite(numeric)) {
    return value;
  }
  return formatNumber(numeric, getCurrentLocale(), {
    minimumFractionDigits: 0,
    maximumFractionDigits: FX_RATE_MAX_DECIMALS,
  });
};

export const formatTimezonePreview = (timezone: string): string => {
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
    }).formatToParts(TIMEZONE_PREVIEW_SOURCE);

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
