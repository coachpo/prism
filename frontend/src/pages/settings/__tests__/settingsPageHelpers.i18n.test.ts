import { beforeEach, describe, expect, it } from "vitest";
import {
  formatTimezonePreview,
  validateAuthPassword,
  validateFxRate,
} from "../settingsPageHelpers";

describe("settingsPageHelpers i18n", () => {
  beforeEach(() => {
    document.documentElement.lang = "zh-CN";
  });

  it("returns localized validation text for auth passwords", () => {
    expect(validateAuthPassword("short")).toBe("密码至少需要 8 个字符");
  });

  it("returns localized validation text for FX rate errors", () => {
    expect(validateFxRate("")).toBe("FX 汇率为必填项");
    expect(validateFxRate("0")).toBe("FX 汇率必须大于零");
  });

  it("formats the timezone preview using the active locale", () => {
    expect(formatTimezonePreview("Asia/Shanghai")).toMatch(/2026/);
  });
});
