import { afterEach, describe, expect, it, vi } from "vitest";

const TEST_APP_VERSION = "9.8.7";
const TEST_GIT_RUN_NUMBER = "123";
const TEST_GIT_REVISION = "deadbee";

async function loadAppVersionModule() {
  vi.stubEnv("VITE_APP_VERSION", TEST_APP_VERSION);
  vi.stubEnv("VITE_GIT_RUN_NUMBER", TEST_GIT_RUN_NUMBER);
  vi.stubEnv("VITE_GIT_REVISION", TEST_GIT_REVISION);
  vi.resetModules();

  return import("../appVersion");
}

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe("appVersion", () => {
  it("exposes the injected app version as the canonical frontend version", async () => {
    const appVersionModule = await loadAppVersionModule();

    expect(appVersionModule.APP_VERSION).toBe(TEST_APP_VERSION);
  });

  it("formats the visible label with git metadata as secondary details", async () => {
    const appVersionModule = await loadAppVersionModule();

    expect(appVersionModule.formatVersionLabel(TEST_APP_VERSION, TEST_GIT_RUN_NUMBER, TEST_GIT_REVISION)).toBe(
      "9.8.7 (123 - deadbee)"
    );
  });
});
