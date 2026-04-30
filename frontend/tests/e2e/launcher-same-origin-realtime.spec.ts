import { expect, test } from "@playwright/test";

const launcherBaseUrl = process.env.PLAYWRIGHT_BASE_URL?.trim();

test.describe("launcher same-origin realtime", () => {
  test("keeps browser traffic same-origin and opens realtime websocket", async ({ page }) => {
    if (!launcherBaseUrl) {
      test.skip(true, "requires PLAYWRIGHT_BASE_URL pointing at an externally started launcher");
      return;
    }

    const browserRequests: string[] = [];
    const consoleErrors: string[] = [];

    page.on("request", (request) => {
      browserRequests.push(request.url());
    });

    page.on("console", (message) => {
      if (message.type() === "error") {
        consoleErrors.push(message.text());
      }
    });

    await page.goto("/", { waitUntil: "domcontentloaded" });
    await page.waitForLoadState("networkidle");

    expect(new URL(page.url()).origin).toBe(new URL(launcherBaseUrl).origin);

    const websocketResult = await page.evaluate(async () => {
      const url = `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/api/realtime/ws`;
      const retryDelaysMs = [250, 500, 1000, 1500];

      const wait = (delayMs: number) => new Promise((resolve) => {
        window.setTimeout(resolve, delayMs);
      });

      type WebsocketAttempt = {
        attempt: number;
        events: string[];
        opened: boolean;
        reason: string | null;
        url: string;
      };

      const openWebsocket = async (attempt: number) => await new Promise<WebsocketAttempt>((resolve) => {
        const events: string[] = [];
        const socket = new WebSocket(url);
        let settled = false;

        const finish = (opened: boolean, reason: string | null) => {
          if (settled) {
            return;
          }

          settled = true;
          clearTimeout(timer);

          try {
            socket.close();
          } catch {
            // Ignore close errors after the result is recorded.
          }

          resolve({ attempt, events, opened, reason, url });
        };

        socket.addEventListener("open", () => {
          events.push("open");
          finish(true, null);
        });

        socket.addEventListener("error", () => {
          events.push("error");
        });

        socket.addEventListener("close", (event) => {
          events.push(`close:${event.code}`);
          if (!events.includes("open")) {
            finish(false, event.reason || `close:${event.code}`);
          }
        });

        const timer = window.setTimeout(() => {
          finish(events.includes("open"), "timeout");
        }, 5000);
      });

      const attempts: WebsocketAttempt[] = [];
      for (let attempt = 1; attempt <= retryDelaysMs.length + 1; attempt += 1) {
        const result = await openWebsocket(attempt);
        attempts.push(result);

        if (result.opened || attempt > retryDelaysMs.length) {
          return { ...result, attempts };
        }

        await wait(retryDelaysMs[attempt - 1]);
      }

      return {
        attempt: attempts.length,
        attempts,
        events: [],
        opened: false,
        reason: "retry-exhausted",
        url,
      };
    });

    expect(new URL(websocketResult.url).host).toBe(new URL(launcherBaseUrl).host);
    expect(new URL(websocketResult.url).pathname).toBe("/api/realtime/ws");
    expect(
      websocketResult.opened,
      JSON.stringify(websocketResult.attempts)
    ).toBe(true);

    expect(
      browserRequests.filter((url) => url.startsWith("http://localhost:18000"))
    ).toHaveLength(0);

    const websocketConsoleErrors = consoleErrors.filter((message) => {
      const normalized = message.toLowerCase();
      const isExpectedTransientRetryFailure =
        websocketResult.opened &&
        normalized.includes(websocketResult.url.toLowerCase()) &&
        normalized.includes("websocket connection") &&
        normalized.includes("failed") &&
        !normalized.includes("403");

      if (isExpectedTransientRetryFailure) {
        return false;
      }

      return normalized.includes("403") ||
        (normalized.includes("websocket connection") && normalized.includes("failed"));
    });

    expect(websocketConsoleErrors).toEqual([]);
  });
});
