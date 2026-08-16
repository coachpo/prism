// Evidence capture for the routing-policy config surface.
// Starts the Vite dev server, mocks the management API, and saves viewport
// screenshots (1680 / 1200+sidebar / 390) into artifacts/evidence/.
import { chromium } from "@playwright/test";
import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const frontendDir = path.resolve(__dirname, "..");
const repoRoot = path.resolve(frontendDir, "..");
const evidenceDir = path.join(repoRoot, "artifacts", "evidence");
fs.mkdirSync(evidenceDir, { recursive: true });

const e2ePort = 15175;
const baseURL = `http://127.0.0.1:${e2ePort}`;

const timestamp = "2026-08-09T10:00:00Z";
const strategies = [
  {
    id: 1, profile_id: 1, name: "Default single routing", legacy_strategy_type: "single", is_default: false,
    failure_status_codes: [403, 422, 429, 500, 502, 503, 504, 529], ban_mode: "off",
    retry_base_delay_ms: 60000, retry_backoff_multiplier: 2, retry_jitter_ratio: 0.2,
    retry_max_delay_ms: 900000, cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 0, ban_duration_seconds: 0,
    attached_model_count: 1, created_at: timestamp, updated_at: timestamp,
  },
  {
    id: 2, profile_id: 1, name: "Default fill-first routing", legacy_strategy_type: "fill-first", is_default: true,
    failure_status_codes: [403, 422, 429, 500, 502, 503, 504, 529], ban_mode: "off",
    retry_base_delay_ms: 60000, retry_backoff_multiplier: 2, retry_jitter_ratio: 0.2,
    retry_max_delay_ms: 900000, cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 0, ban_duration_seconds: 0,
    attached_model_count: 2, created_at: timestamp, updated_at: timestamp,
  },
  {
    id: 3, profile_id: 1, name: "Default round-robin routing", legacy_strategy_type: "round-robin", is_default: false,
    failure_status_codes: [403, 422, 429, 500, 502, 503, 504, 529], ban_mode: "off",
    retry_base_delay_ms: 60000, retry_backoff_multiplier: 2, retry_jitter_ratio: 0.2,
    retry_max_delay_ms: 900000, cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 0, ban_duration_seconds: 0,
    attached_model_count: 0, created_at: timestamp, updated_at: timestamp,
  },
  {
    id: 4, profile_id: 1, name: "Custom conservative retry", legacy_strategy_type: "fill-first", is_default: false,
    failure_status_codes: [403, 422, 429, 500, 502, 503, 504, 529], ban_mode: "temporary",
    retry_base_delay_ms: 120000, retry_backoff_multiplier: 2, retry_jitter_ratio: 0.2,
    retry_max_delay_ms: 1800000, cycle_retry_attempt_limit: 2,
    ban_cumulative_retry_attempt_threshold: 4, ban_duration_seconds: 3600,
    attached_model_count: 0, created_at: timestamp, updated_at: timestamp,
  },
];

const impactPayload = (strategyId) => ({
  strategy_id: strategyId,
  attached_model_count: 2,
  items: [
    { model_config_id: 42, model_id: "gpt-4o", display_name: "GPT-4o Primary", is_enabled: true },
    { model_config_id: 43, model_id: "gpt-4o-mini", display_name: "GPT-4o Mini", is_enabled: true },
  ],
  has_more: false,
  next_cursor: null,
});

async function main() {
  const server = spawn("pnpm", ["exec", "vite", "--host", "127.0.0.1", "--port", String(e2ePort), "--strictPort"], {
    cwd: frontendDir,
    stdio: "ignore",
  });
  try {
    // Wait for the Vite server to accept connections (cold start can be slow).
    for (let attempt = 0; attempt < 40; attempt += 1) {
      try {
        const response = await fetch(`${baseURL}/`);
        if (response.ok) break;
      } catch {
        // not up yet
      }
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }

    const browser = await chromium.launch();
    const page = await browser.newPage();

    await page.route("**/*", async (route) => {
      const request = route.request();
      const pathname = new URL(request.url()).pathname;
      if (!pathname.startsWith("/api/")) {
        return route.continue();
      }
      const fulfillJson = (body, status = 200) =>
        route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
      if (pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false });
      if (pathname === "/api/loadbalance/strategies") return fulfillJson(strategies);
      const impactMatch = pathname.match(/^\/api\/loadbalance\/strategies\/(\d+)\/models$/);
      if (impactMatch) return fulfillJson(impactPayload(Number(impactMatch[1])));
      if (pathname === "/api/settings/costing") return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
      if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "Asia/Shanghai" });
      if (pathname === "/api/models") return fulfillJson([]);
      return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    });

    // 1680×1050
    await page.setViewportSize({ width: 1680, height: 1050 });
    await page.goto(`${baseURL}/route/ban-policies`);
    await page.getByRole("heading", { name: "路由策略", exact: true }).waitFor();
    await page.getByRole("table").getByText("Default fill-first routing").waitFor();
    await page.waitForTimeout(600);
    await page.screenshot({ path: path.join(evidenceDir, "ux-ban-strategies-1680-v2.png"), fullPage: true });

    // Impact list expanded at 1680
    await page.getByRole("row", { name: /Default fill-first routing/ }).getByRole("button", { name: /展开查看/ }).click();
    await page.waitForTimeout(400);
    await page.screenshot({ path: path.join(evidenceDir, "ux-ban-impact-1680-v2.png"), fullPage: true });

    // 1200×800 with sidebar
    await page.setViewportSize({ width: 1200, height: 800 });
    await page.goto(`${baseURL}/route/ban-policies`);
    await page.getByRole("heading", { name: "路由策略", exact: true }).waitFor();
    await page.waitForTimeout(600);
    await page.screenshot({ path: path.join(evidenceDir, "ux-ban-strategies-1200-v2.png"), fullPage: true });

    // Strategy dialog at 1200 with presets + preview
    await page.getByRole("button", { name: /新建策略/ }).first().click();
    await page.waitForTimeout(800);
    await page.screenshot({ path: path.join(evidenceDir, "ux-ban-dialog-1200-v2.png"), fullPage: true });
    await page.keyboard.press("Escape");
    await page.waitForTimeout(300);

    // 390×844
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(`${baseURL}/route/ban-policies`);
    await page.getByRole("heading", { name: "路由策略", exact: true }).waitFor();
    await page.waitForTimeout(600);
    await page.screenshot({ path: path.join(evidenceDir, "ux-ban-strategies-390-v2.png"), fullPage: true });

    await browser.close();
    console.log("evidence captured:", fs.readdirSync(evidenceDir).filter((name) => name.endsWith("v2.png")).join(", "));
  } finally {
    server.kill("SIGTERM");
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
