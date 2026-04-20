import { defineConfig } from "@playwright/test";

const baseURL = process.env.PLAYWRIGHT_BASE_URL?.trim() || "http://127.0.0.1:4173";
const disableWebServer =
  process.env.PLAYWRIGHT_DISABLE_WEBSERVER === "1" ||
  process.env.PLAYWRIGHT_DISABLE_WEBSERVER?.toLowerCase() === "true";

export default defineConfig({
  testDir: "./tests/e2e",
  use: {
    baseURL,
    screenshot: "only-on-failure",
    trace: "on-first-retry",
  },
  webServer: disableWebServer
    ? undefined
    : {
        command: "pnpm exec vite --host 127.0.0.1 --port 4173 --strictPort",
        url: "http://127.0.0.1:4173",
        reuseExistingServer: !process.env.CI,
      },
});
