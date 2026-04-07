import { beforeEach, describe, expect, it } from "vitest";
import {
  isNonNegativeDecimalString,
  normalizeOptionalTemplatePrice,
  parsePricingTemplateUsageRows,
} from "../pricingTemplateFormState";

describe("pricingTemplateFormState", () => {
  beforeEach(() => {
    document.documentElement.lang = "en";
  });

  it("trims optional prices and maps blank values to null", () => {
    expect(normalizeOptionalTemplatePrice(" 1.25 ")).toBe("1.25");
    expect(normalizeOptionalTemplatePrice("   ")).toBeNull();
  });

  it("accepts only non-negative decimal strings", () => {
    expect(isNonNegativeDecimalString("0")).toBe(true);
    expect(isNonNegativeDecimalString("12.3456")).toBe(true);
    expect(isNonNegativeDecimalString("-1")).toBe(false);
    expect(isNonNegativeDecimalString("not-a-number")).toBe(false);
  });

  it("parses nested usage rows and fills fallback labels for missing names", () => {
    expect(
      parsePricingTemplateUsageRows({
        detail: {
          connections: [
            {
              connection_id: 1,
              connection_name: "Primary",
              model_config_id: 2,
              model_id: "   ",
              endpoint_id: 3,
              endpoint_name: "",
            },
            {
              connection_id: "bad",
              model_config_id: 2,
              endpoint_id: 3,
            },
          ],
        },
      }),
    ).toEqual([
      {
        connection_id: 1,
        connection_name: "Primary",
        model_config_id: 2,
        model_id: "Unknown model",
        endpoint_id: 3,
        endpoint_name: "Endpoint #3",
      },
    ]);
  });
});
