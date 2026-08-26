import { describe, expect, it } from "vitest";

import { DEFAULT_COSTING_FORM, normalizeCostingForm } from "./costingForm";

describe("costing form contract", () => {
  it("keeps the fresh form defaults", () => {
    expect(DEFAULT_COSTING_FORM).toEqual({
      report_currency_code: "USD",
      report_currency_symbol: "$",
      timezone_preference: null,
    });
  });

  it("normalizes editable currency fields without dropping CAS state", () => {
    expect(
      normalizeCostingForm({
        report_currency_code: " eur ",
        report_currency_symbol: " € ",
        timezone_preference: undefined,
        expected_updated_at: "2026-08-13T00:00:00Z",
        reporting_currency_epoch: "4",
      }),
    ).toMatchObject({
      report_currency_code: "EUR",
      report_currency_symbol: "€",
      timezone_preference: null,
      expected_updated_at: "2026-08-13T00:00:00Z",
      reporting_currency_epoch: "4",
    });
  });
});
