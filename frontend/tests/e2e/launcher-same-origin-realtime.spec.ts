import { expect, test } from "@playwright/test";

const launcherBaseUrl = process.env.PLAYWRIGHT_BASE_URL?.trim();

test.describe("launcher same-origin realtime", () => {
  test("keeps browser traffic same-origin and opens realtime websocket", async ({ page }) => {
    test.skip(
      !launcherBaseUrl,
      "requires PLAYWRIGHT_BASE_URL pointing at an externally started launcher"
    );

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

      return await new Promise<{
        events: string[];
        opened: boolean;
        reason: string | null;
        url: string;
      }>((resolve) => {
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

          resolve({ events, opened, reason, url });
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
        }, 10000);
      });
    });

    expect(new URL(websocketResult.url).host).toBe(new URL(launcherBaseUrl).host);
    expect(new URL(websocketResult.url).pathname).toBe("/api/realtime/ws");
    expect(websocketResult.opened, websocketResult.reason ?? undefined).toBe(true);

    expect(
      browserRequests.filter((url) => url.startsWith("http://localhost:18000"))
    ).toHaveLength(0);

    const websocketConsoleErrors = consoleErrors.filter((message) => {
      const normalized = message.toLowerCase();
      return normalized.includes("403") ||
        (normalized.includes("websocket connection") && normalized.includes("failed"));
    });

    expect(websocketConsoleErrors).toEqual([]);
  });
});
