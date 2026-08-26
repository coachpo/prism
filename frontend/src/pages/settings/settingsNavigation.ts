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
