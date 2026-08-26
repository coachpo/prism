import type { CostingSettingsResponse, CostingSettingsUpdate } from "@/lib/types";

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
