import { expect, test, type Locator, type Page, type Request, type Route } from "@playwright/test";
import type { ProxyApiKey, ProxyApiKeyCreate, ProxyApiKeyUpdate } from "../../src/lib/types";

const timestamp = "2026-04-28T12:00:00Z";
const routeReadyTimeout = 15_000;

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

function createProxyKey(overrides: Partial<ProxyApiKey> = {}): ProxyApiKey {
  return {
    id: 101,
    name: "Current key",
    key_prefix: "pk_current",
    key_preview: "pk_current••••1234",
    is_active: true,
    expires_at: null,
    last_used_at: null,
    last_used_ip: null,
    notes: "Main production client",
    rotated_from_id: null,
    created_at: timestamp,
    updated_at: timestamp,
    ...overrides,
  };
}

async function seedLocale(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem("prism.locale", "en");
  });
}

async function fulfillJson(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

async function readProfileHeader(request: Request) {
  const headers = await request.allHeaders();
  return headers["x-profile-id"] ?? null;
}

function keyRow(page: Page, name: string): Locator {
  return page.getByRole("row").filter({ hasText: name });
}

async function installProxyKeyRoutes(page: Page, options: { authEnabled?: boolean; proxyKeyLimit?: number } = {}) {
  const profile = createProfile();
  const authEnabled = options.authEnabled ?? false;
  const proxyKeyLimit = options.proxyKeyLimit ?? 10;
  const createPayloads: ProxyApiKeyCreate[] = [];
  const updatePayloads: ProxyApiKeyUpdate[] = [];
  const rotatePayloads: number[] = [];
  const proxyKeyProfileHeaders: Array<string | null> = [];
  let nextId = 200;
  let proxyKeys: ProxyApiKey[] = [
    createProxyKey(),
    createProxyKey({
      id: 102,
      name: "Expired key",
      key_prefix: "pk_expired",
      key_preview: "pk_expired••••9876",
      is_active: true,
      expires_at: "2026-04-20T09:00:00Z",
      notes: "Old integration",
    }),
  ];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

    if (!pathname.startsWith("/api/")) {
      await route.continue();
      return;
    }

    if (pathname === "/api/auth/status") {
      await fulfillJson(route, { auth_enabled: authEnabled });
      return;
    }

    if (pathname === "/api/auth/session") {
      await fulfillJson(route, { authenticated: true, auth_enabled: authEnabled, username: authEnabled ? "admin" : null });
      return;
    }

    if (pathname === "/api/profiles/bootstrap") {
      await fulfillJson(route, {
        profiles: [profile],
        active_profile: profile,
        profile_limits: { max_profiles: 5 },
      });
      return;
    }

    if (pathname === "/api/settings/costing") {
      await fulfillJson(route, {
        report_currency_code: "USD",
        report_currency_symbol: "$",
        endpoint_fx_mappings: [],
        timezone_preference: null,
      });
      return;
    }

    if (pathname === "/api/settings/auth" && request.method() === "GET") {
      await fulfillJson(route, {
        auth_enabled: authEnabled,
        username: null,
        email: null,
        email_bound_at: null,
        pending_email: null,
        email_verification_required: false,
        has_password: false,
        proxy_key_limit: proxyKeyLimit,
      });
      return;
    }

    if (pathname.startsWith("/api/settings/auth/proxy-keys")) {
      proxyKeyProfileHeaders.push(await readProfileHeader(request));
    }

    if (pathname === "/api/settings/auth/proxy-keys" && request.method() === "GET") {
      await fulfillJson(route, proxyKeys);
      return;
    }

    if (pathname === "/api/settings/auth/proxy-keys" && request.method() === "POST") {
      const payload = request.postDataJSON() as ProxyApiKeyCreate;
      createPayloads.push(payload);
      const item = createProxyKey({
        id: nextId,
        name: payload.name,
        notes: payload.notes ?? null,
        expires_at: payload.expires_at ?? null,
        key_prefix: `pk_${nextId}`,
        key_preview: `pk_${nextId}••••${String(nextId).slice(-4).padStart(4, "0")}`,
        created_at: "2026-05-01T08:30:00Z",
        updated_at: "2026-05-01T08:30:00Z",
      });
      nextId += 1;
      proxyKeys = [item, ...proxyKeys];
      await fulfillJson(route, { key: `sk-live-created-${item.id}`, item }, 201);
      return;
    }

    const patchMatch = pathname.match(/^\/api\/settings\/auth\/proxy-keys\/(\d+)$/);
    if (patchMatch && request.method() === "PATCH") {
      const keyId = Number.parseInt(patchMatch[1]!, 10);
      const payload = request.postDataJSON() as ProxyApiKeyUpdate;
      updatePayloads.push(payload);
      proxyKeys = proxyKeys.map((item) =>
        item.id === keyId
          ? {
              ...item,
              name: payload.name,
              notes: payload.notes,
              is_active: payload.is_active,
              expires_at: payload.expires_at,
              updated_at: "2026-05-01T09:00:00Z",
            }
          : item
      );
      const updated = proxyKeys.find((item) => item.id === keyId)!;
      await fulfillJson(route, updated);
      return;
    }

    const rotateMatch = pathname.match(/^\/api\/settings\/auth\/proxy-keys\/(\d+)\/rotate$/);
    if (rotateMatch && request.method() === "POST") {
      const keyId = Number.parseInt(rotateMatch[1]!, 10);
      rotatePayloads.push(keyId);
      const source = proxyKeys.find((item) => item.id === keyId)!;
      const rotationTime = "2026-05-01T10:00:00Z";
      const successor = createProxyKey({
        id: nextId,
        name: source.name,
        notes: source.notes,
        key_prefix: `pk_${nextId}`,
        key_preview: `pk_${nextId}••••${String(nextId).slice(-4).padStart(4, "0")}`,
        rotated_from_id: keyId,
        expires_at: source.expires_at,
        created_at: rotationTime,
        updated_at: rotationTime,
      });
      nextId += 1;
      proxyKeys = [
        successor,
        ...proxyKeys.map((item) =>
          item.id === keyId
            ? {
                ...item,
                is_active: false,
                expires_at: rotationTime,
                updated_at: rotationTime,
              }
            : item
        ),
      ];
      await fulfillJson(route, { key: `sk-live-rotated-${successor.id}`, item: successor });
      return;
    }

    if (patchMatch && request.method() === "DELETE") {
      const keyId = Number.parseInt(patchMatch[1]!, 10);
      proxyKeys = proxyKeys.filter((item) => item.id !== keyId);
      await fulfillJson(route, { deleted: true });
      return;
    }

    throw new Error(`Unhandled API request: ${request.method()} ${pathname}`);
  });

  return {
    getCreatePayloads: () => createPayloads,
    getProxyKeyProfileHeaders: () => proxyKeyProfileHeaders,
    getRotatePayloads: () => rotatePayloads,
    getUpdatePayloads: () => updatePayloads,
  };
}

test("proxy key lifecycle shows expiry, retirement, and rotation lineage without dropping history", async ({ page }) => {
  const routes = await installProxyKeyRoutes(page, { authEnabled: true });
  await seedLocale(page);

  await page.goto("/control/proxy-keys");
  await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: routeReadyTimeout });
  await expect(page.getByText("Loading application...")).toHaveCount(0, {
    timeout: routeReadyTimeout,
  });
  await expect(page.getByRole("heading", { name: "Proxy API Keys" })).toBeVisible({
    timeout: routeReadyTimeout,
  });

  const expiredKeyRow = keyRow(page, "Expired key");
  await expect(expiredKeyRow).toBeVisible({ timeout: routeReadyTimeout });
  await expect(expiredKeyRow.getByText("Expired", { exact: true })).toBeVisible();

  await page.getByLabel("Name").fill("Created with expiry");
  await page.getByLabel("Notes").fill("Short lived client");
  await page.getByLabel("Expires").fill("2026-05-02T08:30");
  await page.getByRole("button", { name: "Create key" }).click();

  const expectedExpiry = new Date("2026-05-02T08:30").toISOString();
  expect(routes.getCreatePayloads()).toEqual([
    {
      name: "Created with expiry",
      notes: "Short lived client",
      expires_at: expectedExpiry,
    },
  ]);
  const createdSecretDialog = page.getByRole("dialog", { name: "New secret" });
  await expect(createdSecretDialog.getByTestId("proxy-key-secret")).toContainText("sk-live-created-200", {
    timeout: routeReadyTimeout,
  });
  await createdSecretDialog.screenshot({ path: "../artifacts/evidence/frontend-rewrite/task-15-secret-reveal.png" });
  await createdSecretDialog.getByRole("button", { name: "Close" }).first().click();
  await expect(page.getByText("sk-live-created-200")).toHaveCount(0);
  await expect(page.getByText("Created with expiry")).toBeVisible({
    timeout: routeReadyTimeout,
  });

  await page.getByRole("button", { name: "Edit proxy key Created with expiry" }).click();

  const editDialog = page.getByRole("dialog", { name: "Edit Proxy API Key" });
  await expect(editDialog).toBeVisible({ timeout: routeReadyTimeout });
  await editDialog.getByRole("button", { name: "Clear expiry" }).click();
  await editDialog.locator('[data-slot="switch"]').click();
  await editDialog.getByRole("button", { name: "Save" }).click();

  expect(routes.getUpdatePayloads()).toEqual([
    {
      name: "Created with expiry",
      notes: "Short lived client",
      is_active: false,
      expires_at: null,
    },
  ]);
  await expect(keyRow(page, "Created with expiry").getByText("Retired", { exact: true })).toBeVisible({
    timeout: routeReadyTimeout,
  });

  await page.getByRole("button", { name: "Rotate proxy key Current key" }).click();
  expect(routes.getRotatePayloads()).toEqual([101]);
  const rotatedSecretDialog = page.getByRole("dialog", { name: "New secret" });
  await expect(rotatedSecretDialog.getByTestId("proxy-key-secret")).toContainText("sk-live-rotated-201", {
    timeout: routeReadyTimeout,
  });
  await rotatedSecretDialog.getByRole("button", { name: "Close" }).first().click();
  await expect(page.getByText("sk-live-rotated-201")).toHaveCount(0);

  const predecessorRow = page.getByRole("row").filter({ hasText: "Rotated to #201" });
  const successorRow = page.getByRole("row").filter({ hasText: "Rotated from #101" });

  await expect(predecessorRow.getByText("Rotated to #201", { exact: true })).toBeVisible({
    timeout: routeReadyTimeout,
  });
  await expect(predecessorRow.getByText("Rotated", { exact: true })).toBeVisible({
    timeout: routeReadyTimeout,
  });
  await expect(successorRow.getByText("Rotated from #101", { exact: true })).toBeVisible({
    timeout: routeReadyTimeout,
  });

  await predecessorRow.getByRole("button", { name: "Delete proxy key Current key" }).click();
  const deleteDialog = page.getByRole("alertdialog", { name: "Delete Proxy API Key" });
  await expect(deleteDialog.getByText("Live proxy traffic may be interrupted")).toBeVisible();
  await expect(deleteDialog.getByText("Rotation lineage warning")).toBeVisible();
  await deleteDialog.screenshot({ path: "../artifacts/evidence/frontend-rewrite/task-15-delete-warning.png" });
  await deleteDialog.getByRole("button", { name: "Cancel" }).click();

  expect(routes.getProxyKeyProfileHeaders().every((value) => value === null)).toBe(true);
});

test("quota-limited create disables submit and explains the limit", async ({ page }) => {
  const routes = await installProxyKeyRoutes(page, { proxyKeyLimit: 2 });
  await seedLocale(page);

  await page.goto("/control/proxy-keys");
  await expect(page.getByRole("heading", { name: "Proxy API Keys" })).toBeVisible({
    timeout: routeReadyTimeout,
  });

  await expect(page.getByRole("button", { name: "Key limit reached" })).toBeDisabled();
  await expect(page.getByText("2 / 2 keys used").first()).toBeVisible();
  await expect(page.getByText("Key limit reached").first()).toBeVisible();
  expect(routes.getCreatePayloads()).toEqual([]);
  expect(routes.getProxyKeyProfileHeaders().every((value) => value === null)).toBe(true);
});
