import fs from "node:fs";
import path from "node:path";
import { expect, test, type Page, type Request } from "@playwright/test";

const timestamp = "2026-06-16T00:00:00Z";
const evidenceDir = path.resolve(process.cwd(), "../.omo/evidence");
const controlsEvidencePath = path.join(evidenceDir, "task-13-settings-audit-controls.playwright.png");
const networkEvidencePath = path.join(evidenceDir, "task-13-settings-audit-network.playwright.json");

type AuditFamilyPayload = {
  api_family: "openai" | "anthropic" | "gemini";
  audit_enabled: boolean;
  audit_capture_bodies: boolean;
};

function createProfile() {
  return {
    id: 1,
    name: "Default",
    description: null,
    is_active: true,
    is_default: true,
    is_editable: true,
    version: 1,
    created_at: timestamp,
    deleted_at: null,
    updated_at: timestamp,
  };
}

function createAuthSettings() {
  return {
    auth_enabled: false,
    username: null,
    has_password: false,
    email: null,
    pending_email: null,
    email_bound_at: null,
    email_verification_required: false,
  };
}

function createRetentionSettings() {
  return {
    request_logs_retention_days: 30,
    statistics_retention_days: 30,
    audit_logs_retention_days: 30,
    loadbalance_events_retention_days: 30,
  };
}

async function mockSettingsRoutes(page: Page) {
  const profile = createProfile();
  let auditSettings: AuditFamilyPayload[] = [
    { api_family: "openai", audit_enabled: false, audit_capture_bodies: true },
    { api_family: "anthropic", audit_enabled: true, audit_capture_bodies: false },
  ];
  const auditUpdates: Array<{ method: string; pathname: string; body: { settings: AuditFamilyPayload[] } }> = [];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") {
      return fulfillJson({ auth_enabled: false });
    }
    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({ profiles: [profile], active_profile: profile, profile_limits: { max_profiles: 5 } });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/auth") {
      return fulfillJson(createAuthSettings());
    }
    if (pathname === "/api/settings/log-retention") {
      return fulfillJson(createRetentionSettings());
    }
    if (pathname === "/api/settings/audit" && request.method() === "GET") {
      return fulfillJson({ profile_id: profile.id, settings: auditSettings });
    }
    if (pathname === "/api/settings/audit" && request.method() === "PUT") {
      const body = request.postDataJSON() as { settings: AuditFamilyPayload[] };
      auditUpdates.push(captureRequest(request, pathname, body));
      auditSettings = body.settings;
      return fulfillJson({ profile_id: profile.id, settings: auditSettings });
    }
    if (pathname === "/api/models") {
      return fulfillJson([]);
    }
    if (pathname === "/api/config/header-blocklist-rules") {
      return fulfillJson([]);
    }
    if (pathname === "/api/config/user-agent-client-rules") {
      return fulfillJson([]);
    }

    throw new Error(`Unhandled API request: ${request.method()} ${pathname}`);
  });

  await page.addInitScript(() => window.localStorage.setItem("prism.locale", "en"));

  return { auditUpdates };
}

function captureRequest<TBody>(request: Request, pathname: string, body: TBody) {
  return { method: request.method(), pathname, body };
}

async function openAuditSettings(page: Page) {
  await page.goto("/system/settings?tab=profile&section=audit-configuration#audit-configuration");
  const auditSection = page.locator("section#audit-configuration");
  await expect(auditSection).toBeVisible();
  return auditSection;
}

test("settings audit controls normalize API families and submit a full replacement payload", async ({ page }) => {
  fs.mkdirSync(evidenceDir, { recursive: true });
  const consoleMessages: string[] = [];
  page.on("console", (message) => {
    if (["error", "warning"].includes(message.type())) {
      consoleMessages.push(`${message.type()}: ${message.text()}`);
    }
  });
  const { auditUpdates } = await mockSettingsRoutes(page);

  const auditSection = await openAuditSettings(page);
  const apiFamilyCard = page.getByTestId("audit-api-family-card");
  await expect(apiFamilyCard).toBeVisible();
  await expect(auditSection.getByTestId("audit-user-agent-client-rules-card")).toBeVisible();
  await expect(auditSection.locator("[data-testid^='audit-api-family-row-']")).toHaveText([
    "OpenAI",
    "Anthropic",
    "Gemini",
  ]);

  await expect(apiFamilyCard.getByRole("switch", { name: "OpenAI Capture bodies" })).toBeDisabled();
  await expect(apiFamilyCard.getByRole("switch", { name: "Gemini Capture bodies" })).toBeDisabled();

  await apiFamilyCard.getByRole("switch", { name: "OpenAI Audit enabled" }).click();
  await expect(apiFamilyCard.getByRole("switch", { name: "OpenAI Capture bodies" })).toBeEnabled();
  await apiFamilyCard.getByRole("switch", { name: "OpenAI Capture bodies" }).click();
  await apiFamilyCard.getByRole("switch", { name: "Anthropic Capture bodies" }).click();
  await apiFamilyCard.getByRole("switch", { name: "Anthropic Audit enabled" }).click();
  await expect(apiFamilyCard.getByRole("switch", { name: "Anthropic Capture bodies" })).toBeDisabled();

  await apiFamilyCard.getByRole("button", { name: "Save audit settings" }).click();

  await expect.poll(() => auditUpdates.length).toBe(1);
  expect(auditUpdates[0]).toEqual({
    method: "PUT",
    pathname: "/api/settings/audit",
    body: {
      settings: [
        { api_family: "openai", audit_enabled: true, audit_capture_bodies: true },
        { api_family: "anthropic", audit_enabled: false, audit_capture_bodies: false },
        { api_family: "gemini", audit_enabled: false, audit_capture_bodies: false },
      ],
    },
  });
  await expect(page.getByText("Audit settings saved")).toBeVisible();
  await expect(apiFamilyCard.getByRole("button", { name: "Save audit settings" })).toBeDisabled();

  fs.writeFileSync(networkEvidencePath, JSON.stringify({ auditUpdates, consoleMessages }, null, 2));
  await page.screenshot({ path: controlsEvidencePath, fullPage: true });
  expect(consoleMessages).toEqual([]);
});
