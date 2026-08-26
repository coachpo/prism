import { describe, expect, it } from "vitest";

import {
  AUTH_PASSWORD_MAX_LENGTH,
  AUTH_PASSWORD_MIN_LENGTH,
  validateAuthPassword,
} from "./authenticationPassword";

describe("authentication password contract", () => {
  it("keeps the password bounds and optional-empty behavior", () => {
    expect(AUTH_PASSWORD_MIN_LENGTH).toBe(8);
    expect(AUTH_PASSWORD_MAX_LENGTH).toBe(512);
    expect(validateAuthPassword("")).toBeNull();
    expect(validateAuthPassword("a".repeat(AUTH_PASSWORD_MIN_LENGTH - 1))).toContain("8");
    expect(validateAuthPassword("a".repeat(AUTH_PASSWORD_MAX_LENGTH + 1))).toContain("512");
    expect(validateAuthPassword("a".repeat(AUTH_PASSWORD_MIN_LENGTH))).toBeNull();
  });
});
