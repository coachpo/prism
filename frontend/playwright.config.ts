import { defineConfig } from "@playwright/test";

const e2eHost = "127.0.0.1";
const e2ePort = 15174;
const defaultBaseURL = `http://${e2eHost}:${e2ePort}`;
const baseURL = process.env.PLAYWRIGHT_BASE_URL?.trim() || defaultBaseURL;
const disableWebServer =
  process.env.PLAYWRIGHT_DISABLE_WEBSERVER === "1" ||
  process.env.PLAYWRIGHT_DISABLE_WEBSERVER?.toLowerCase() === "true";

export default defineConfig({
  testDir: "./tests/e2e",
  use: {
    baseURL,
    // Optional: run against the system Chrome/Edge when the Playwright browser
    // cache is unavailable (PLAYWRIGHT_CHANNEL=chrome|msedge).
    channel: process.env.PLAYWRIGHT_CHANNEL?.trim() || undefined,
    ...(process.env.PLAYWRIGHT_EXECUTABLE_PATH?.trim()
      ? { launchOptions: { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH.trim() } }
      : {}),
    screenshot: "only-on-failure",
    trace: "on-first-retry",
  },
  webServer: disableWebServer
    ? undefined
    : {
        command: `pnpm exec vite --host ${e2eHost} --port ${e2ePort} --strictPort`,
        url: defaultBaseURL,
        reuseExistingServer: false,
      },
});
