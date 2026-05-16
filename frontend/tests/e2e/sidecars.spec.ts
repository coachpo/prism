import { expect, test, type Page, type Route } from "@playwright/test";

type Sidecar = {
  id: number;
  name: string;
  base_url: string;
  base_url_canonical: string;
  enabled: boolean;
  environment_label?: string;
  allow_private_network: boolean;
  allow_insecure_http: boolean;
  skip_tls_verify: boolean;
  sync_interval_seconds: number;
  request_timeout_seconds: number;
  credential_state: { management_password_configured: boolean };
  management_auth_state: "unknown" | "valid" | "invalid_management_auth";
  pause_metadata?: { reason: string; paused_until?: string };
  last_sync_at?: string;
  last_successful_sync_at?: string;
  snapshot_stale_after?: string;
  last_sync_error?: string;
  created_at: string;
  updated_at: string;
};

type AuthSnapshot = {
  id: number;
  sidecar_id: number;
  auth_id: string;
  name: string;
  provider?: string;
  label?: string;
  status?: string;
  disabled?: boolean;
  unavailable?: boolean;
  priority?: number;
  next_retry_after?: string;
  success_count?: number;
  failed_count?: number;
  recent_requests?: unknown;
  observed_at: string;
};

type ProviderSnapshot = {
  id: number;
  sidecar_id: number;
  provider_key: string;
  provider_item_key: string;
  name?: string;
  label?: string;
  status?: string;
  disabled?: boolean;
  observed_at: string;
  snapshot?: unknown;
};

const now = "2026-05-10T12:00:00.000Z";
const future = "2099-05-10T12:00:00.000Z";
const past = "2025-05-10T12:00:00.000Z";
const existingManagementPassword = "existing-management-secret";
const replacementManagementPassword = "replacement-secret";
const rotatedManagementPassword = "rotated-super-secret";
const rawSecretValues = [existingManagementPassword, replacementManagementPassword, rotatedManagementPassword];
function sidecar(overrides: Partial<Sidecar>): Sidecar {
  return {
    id: 1,
    name: "CLIProxyAPI primary",
    base_url: "https://cliproxyapi.internal:8443",
    base_url_canonical: "https://cliproxyapi.internal:8443",
    enabled: true,
    environment_label: "production",
    allow_private_network: false,
    allow_insecure_http: false,
    skip_tls_verify: false,
    sync_interval_seconds: 300,
    request_timeout_seconds: 30,
    credential_state: { management_password_configured: true },
    management_auth_state: "valid",
    last_sync_at: now,
    last_successful_sync_at: now,
    snapshot_stale_after: future,
    created_at: now,
    updated_at: now,
    ...overrides,
  };
}

function authSnapshot(overrides: Partial<AuthSnapshot>): AuthSnapshot {
  return {
    id: 11,
    sidecar_id: 1,
    auth_id: "auth-primary",
    name: "primary-oauth.json",
    provider: "gemini",
    label: "primary",
    status: "active",
    disabled: false,
    unavailable: false,
    priority: 20,
    success_count: 12,
    failed_count: 1,
    recent_requests: [{ minute: now, count: 2 }],
    observed_at: now,
    ...overrides,
  };
}

function defaultAuthSnapshots(): AuthSnapshot[] {
  return [
    authSnapshot({}),
    authSnapshot({ id: 12, auth_id: "auth-zero-priority", name: "zero-priority.json", status: "active", priority: 0, next_retry_after: future }),
    authSnapshot({ id: 13, auth_id: "auth-disabled", name: "disabled-oauth.json", provider: "claude", disabled: true, unavailable: true, priority: undefined }),
  ];
}

function authFilterSortSnapshots(): AuthSnapshot[] {
  const paginatedMatches = Array.from({ length: 101 }, (_, index) => {
    const ordinal = String(index + 1).padStart(3, "0");
    return authSnapshot({
      id: 1000 + index,
      auth_id: `auth-gamma-${ordinal}`,
      name: `gamma-page-${ordinal}.json`,
      label: "gamma-bulk",
      priority: 10,
    });
  });

  return [
    authSnapshot({ id: 101, auth_id: "auth-zeta-high", name: "zeta-high.json", label: "sort-fixture", priority: 90 }),
    authSnapshot({ id: 102, auth_id: "auth-alpha-a", name: "alpha-shared.json", label: "tie-fixture", priority: 50 }),
    authSnapshot({ id: 103, auth_id: "auth-alpha-b", name: "alpha-shared.json", label: "tie-fixture", priority: 50 }),
    authSnapshot({ id: 104, auth_id: "auth-beta", name: "beta-shared.json", label: "tie-fixture", priority: 50 }),
    authSnapshot({ id: 105, auth_id: "auth-omega-low", name: "omega-low.json", label: "sort-fixture", priority: 1 }),
    authSnapshot({ id: 106, auth_id: "auth-alpha-missing-a", name: "alpha-missing.json", provider: "claude", label: "provider-fixture", priority: undefined }),
    authSnapshot({ id: 107, auth_id: "auth-alpha-missing-b", name: "alpha-missing.json", provider: "openai", label: "missing-priority-fixture", priority: undefined }),
    authSnapshot({ id: 108, auth_id: "auth-priority-floor", name: "priority-floor.json", provider: "gemini", label: "floor-fixture", status: "active", priority: 0 }),
    ...paginatedMatches,
  ];
}

function defaultProviderSnapshots(): ProviderSnapshot[] {
  return [
    { id: 21, sidecar_id: 1, provider_key: "gemini", provider_item_key: "gemini", name: "Gemini", status: "available", disabled: false, observed_at: now, snapshot: { models: 4, api_key: "masked" } },
  ];
}

function syncStatus(target: Sidecar) {
  return {
    sidecar_id: target.id,
    enabled: target.enabled,
    sync_interval_seconds: target.sync_interval_seconds,
    management_auth_state: target.management_auth_state,
    last_sync_at: target.last_sync_at,
    last_successful_sync_at: target.last_successful_sync_at,
    snapshot_stale_after: target.snapshot_stale_after,
    last_sync_error: target.last_sync_error,
    stale: false,
    due: false,
    paused: false,
  };
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

async function expectNoRawSecrets(page: Page) {
  const bodyText = await page.locator("body").innerText();
  const pageMarkup = await page.content();
  rawSecretValues.forEach((secret) => {
    expect(bodyText).not.toContain(secret);
    expect(pageMarkup).not.toContain(secret);
  });
}

type MockSidecarsApiOptions = {
  authSnapshots?: AuthSnapshot[];
  authSnapshotsBySidecarId?: Record<number, AuthSnapshot[]>;
  detailDelayBySidecarId?: Record<number, number>;
};

async function expectAuthOrder(page: Page, orderedText: string[]) {
  const tableText = await page.getByTestId("sidecar-auth-files").innerText();
  let previousIndex = -1;
  for (const text of orderedText) {
    const nextIndex = tableText.indexOf(text);
    expect(nextIndex, `${text} should be visible in auth table order proof`).toBeGreaterThan(previousIndex);
    previousIndex = nextIndex;
  }
}

async function mockSidecarsApi(
  page: Page,
  initialSidecars: Sidecar[],
  options: MockSidecarsApiOptions = {},
) {
  let sidecars = [...initialSidecars];
  let authSnapshots = options.authSnapshots ? [...options.authSnapshots] : defaultAuthSnapshots();
  const providerSnapshots = defaultProviderSnapshots();
  const calls: string[] = [];

  const delayDetail = async (sidecarId: number) => {
    const delay = options.detailDelayBySidecarId?.[sidecarId] ?? 0;
    if (delay > 0) {
      await new Promise((resolve) => setTimeout(resolve, delay));
    }
  };

  const authSnapshotsFor = (sidecarId: number) => {
    const scopedSnapshots = options.authSnapshotsBySidecarId?.[sidecarId];
    if (scopedSnapshots) {
      return scopedSnapshots;
    }
    return sidecarId === 2
      ? [authSnapshot({ id: 22, sidecar_id: 2, auth_id: "auth-edge", name: "edge-oauth.json", provider: "codex", priority: 5 })]
      : authSnapshots;
  };

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname } = url;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    calls.push(`${request.method()} ${pathname}`);

    if (pathname === "/api/usage-queue") {
      throw new Error(`sidecars page must not call removed route: ${pathname}`);
    }
    if (pathname === "/api/auth/status" && request.method() === "GET") {
      return json(route, { auth_enabled: false });
    }
    if (pathname === "/api/profiles/bootstrap" && request.method() === "GET") {
      const profile = { id: 1, name: "Default", description: null, is_active: true, is_default: true, is_editable: true, version: 1, created_at: now, deleted_at: null, updated_at: now };
      return json(route, { profiles: [profile], active_profile: profile, profile_limits: { max_profiles: 5 } });
    }
    if (pathname === "/api/settings/costing" && request.method() === "GET") {
      return json(route, { report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/sidecars" && request.method() === "GET") {
      return json(route, { items: sidecars });
    }
    if (pathname === "/api/sidecars" && request.method() === "POST") {
      const payload = JSON.parse(request.postData() ?? "{}");
      const created = sidecar({ id: 99, ...payload, base_url_canonical: payload.base_url });
      sidecars = [...sidecars, created];
      return json(route, created, 201);
    }

    const authSnapshotsMatch = pathname.match(/^\/api\/sidecars\/(\d+)\/auth-snapshots$/);
    if (authSnapshotsMatch && request.method() === "GET") {
      const sidecarId = Number(authSnapshotsMatch[1]);
      await delayDetail(sidecarId);
      return json(route, { items: authSnapshotsFor(sidecarId) });
    }
    const providerSnapshotsMatch = pathname.match(/^\/api\/sidecars\/(\d+)\/provider-snapshots$/);
    if (providerSnapshotsMatch && request.method() === "GET") {
      const sidecarId = Number(providerSnapshotsMatch[1]);
      await delayDetail(sidecarId);
      return json(route, { items: providerSnapshots });
    }
    const mutationMatch = pathname.match(/^\/api\/sidecars\/(\d+)\/auth-files\/([^/]+)\/(status|fields)$/);
    if (mutationMatch && request.method() === "PATCH") {
      const authId = decodeURIComponent(mutationMatch[2]);
      const payload = JSON.parse(request.postData() ?? "{}");
      authSnapshots = authSnapshots.map((snapshot) => snapshot.auth_id === authId ? { ...snapshot, ...payload } : snapshot);
      return json(route, { state: "succeeded", snapshot: authSnapshots.find((snapshot) => snapshot.auth_id === authId) });
    }

    const match = pathname.match(/^\/api\/sidecars\/(\d+)(?:\/(test-connection|sync))?$/);
    if (match) {
      const id = Number(match[1]);
      const action = match[2];
      const existing = sidecars.find((item) => item.id === id) ?? sidecars[0];
      if (request.method() === "GET") {
        return json(route, existing);
      }
      if (action === "test-connection") {
        return json(route, { state: "succeeded", management_auth_state: "valid", status_code: 200 });
      }
      if (action === "sync") {
        return json(route, { state: "succeeded", sidecar: existing, sync_status: syncStatus(existing), auth_snapshot_count: authSnapshots.length, provider_snapshot_count: providerSnapshots.length });
      }
      if (request.method() === "PATCH") {
        const payload = JSON.parse(request.postData() ?? "{}");
        const updated = { ...existing, ...payload, base_url_canonical: payload.base_url ?? existing.base_url_canonical };
        sidecars = sidecars.map((item) => (item.id === id ? updated : item));
        return json(route, updated);
      }
      if (request.method() === "DELETE") {
        sidecars = sidecars.filter((item) => item.id !== id);
        return json(route, null, 204);
      }
    }

    throw new Error(`Unhandled sidecars mock API route: ${request.method()} ${pathname}`);
  });

  return { calls };
}

test.describe("sidecars management", () => {
  test("lists sidecars with healthy, stale, and degraded status summaries", async ({ page }) => {
    await mockSidecarsApi(page, [
      sidecar({ id: 1, name: "CLIProxyAPI primary" }),
      sidecar({ id: 2, name: "CLIProxyAPI stale", snapshot_stale_after: past }),
      sidecar({ id: 3, name: "CLIProxyAPI degraded", management_auth_state: "invalid_management_auth", last_sync_error: "401" }),
    ]);

    await page.goto("/sidecars");

    await expect(page.getByRole("heading", { name: /Sidecars/i })).toBeVisible();
    await expect(page.getByTestId("sidecars-state")).toContainText("healthy:1 stale:1 degraded:1");
    await expect(page.getByTestId("sidecars-summary")).toContainText("CLIProxyAPI primary");
    await expect(page.getByTestId("sidecars-summary")).toContainText("Password configured");
    await expect(page.getByText("401")).toBeVisible();
    await expectNoRawSecrets(page);
  });

  test("renders retained sidecar detail inventory and priority validation", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })]);
    const consoleErrors: string[] = [];
    page.on("console", (message) => {
      if (message.type() === "error") {
        consoleErrors.push(message.text());
      }
    });
    page.on("pageerror", (error) => consoleErrors.push(error.message));

    await page.goto("/sidecars");

    await expect(page.getByTestId("sidecar-detail")).toContainText("CLIProxyAPI primary detail");
    await expect(page.getByTestId("sidecar-auth-files")).toContainText("zero-priority.json");
    await expect(page.getByTestId("sidecar-auth-files")).toContainText("Enter a positive priority.");
    await expect(page.getByTestId("sidecar-auth-files")).toContainText("missing resolves to initial");
    await expect(page.getByTestId("sidecar-provider-inventory")).toContainText("Provider inventory");
    await expect(page.getByTestId("sidecar-provider-inventory")).toContainText("Masked fields");
    await expect.poll(() => api.calls).toContain("GET /api/sidecars/1/auth-snapshots");
    await expect.poll(() => api.calls).toContain("GET /api/sidecars/1/provider-snapshots");
    await expectNoRawSecrets(page);

    const zeroPriorityRow = page.getByRole("row").filter({ hasText: "zero-priority.json" });
    await page.getByLabel("Priority for zero-priority.json").fill("0");
    await expect(zeroPriorityRow.getByRole("button", { name: "Save" })).toBeDisabled();
    await expect(page.getByTestId("sidecar-auth-files")).toContainText("Enter a positive priority.");
    await expectNoRawSecrets(page);
    expect(consoleErrors).toEqual([]);
  });

  test("filters, sorts, tie-breaks, and paginates auth files", async ({ page }) => {
    await mockSidecarsApi(page, [sidecar({ id: 1 })], { authSnapshots: authFilterSortSnapshots() });

    await page.goto("/sidecars");

    const authFiles = page.getByTestId("sidecar-auth-files");
    const authSearch = page.getByLabel("Filter auth files");

    await expect(authFiles.getByTestId("sidecar-auth-page-size-select")).toHaveCount(0);
    await expect(authFiles.getByRole("columnheader", { name: "Actions" })).toHaveCount(0);
    await expect(authFiles).toContainText("1-50 of 109");
    await authSearch.fill("gamma-page");
    await expect(authFiles).toContainText("gamma-page-001.json");
    await expect(authFiles).toContainText("gamma-page-050.json");
    await expect(authFiles).not.toContainText("gamma-page-051.json");
    await expect(authFiles).toContainText("1-50 of 101");
    await expect(authFiles).not.toContainText("zeta-high.json");

    await authSearch.fill("claude");
    await expect(authFiles).toContainText("alpha-missing.json");
    await expect(authFiles).toContainText("1-1 of 1");
    await expect(authFiles).not.toContainText("zeta-high.json");

    await authSearch.fill("floor-fixture");
    await expect(authFiles).toContainText("priority-floor.json");
    await expect(authFiles).toContainText("1-1 of 1");

    await authSearch.fill("priority 90");
    await expect(authFiles).toContainText("zeta-high.json");
    await expect(authFiles).toContainText("1-1 of 1");
    await expect(authFiles).not.toContainText("omega-low.json");

    await authSearch.fill("missing-auth-file");
    await expect(authFiles).toContainText("No auth files match");
    await expect(authFiles).not.toContainText("1-50 of 101");

    await authSearch.fill("");
    await expect(authFiles).toContainText("1-50 of 109");
    await expect(authFiles).toContainText("alpha-shared.json");

    await page.getByTestId("sidecar-auth-sort-select").click();
    await page.getByRole("option", { name: "Routing priority: high to low" }).click();
    await expect(authFiles).toContainText("zeta-high.json");
    await expectAuthOrder(page, ["auth-zeta-high", "auth-alpha-a", "auth-alpha-b", "auth-beta", "auth-gamma-001"]);

    await page.getByTestId("sidecar-auth-sort-select").click();
    await page.getByRole("option", { name: "Routing priority: low to high" }).click();
    await expect(authFiles).toContainText("alpha-missing.json");
    await expectAuthOrder(page, ["auth-alpha-missing-a", "auth-alpha-missing-b", "auth-priority-floor", "auth-omega-low", "auth-gamma-001"]);

    await expectNoRawSecrets(page);
  });

  test("scrolls selected sidecar detail into view from row action", async ({ page }) => {
    await mockSidecarsApi(page, [sidecar({ id: 1 })]);
    await page.setViewportSize({ width: 1280, height: 360 });

    await page.goto("/sidecars");
    const detail = page.getByTestId("sidecar-detail");
    await expect(detail).toBeAttached();
    await page.evaluate(() => window.scrollTo(0, 0));

    const primaryRow = page.getByRole("row").filter({ hasText: "CLIProxyAPI primary" });
    await primaryRow.getByRole("button", { name: "View details" }).click();

    await expect.poll(async () => detail.evaluate((element) => {
      const rect = element.getBoundingClientRect();
      return rect.bottom > 0 && rect.top < window.innerHeight;
    })).toBe(true);
  });

  test("keeps detail responses scoped to the selected sidecar", async ({ page }) => {
    await mockSidecarsApi(
      page,
      [sidecar({ id: 1, name: "AAA CLIProxyAPI slow" }), sidecar({ id: 2, name: "ZZZ CLIProxyAPI edge" })],
      { detailDelayBySidecarId: { 1: 300 } },
    );

    await page.goto("/sidecars");
    const edgeRow = page.getByRole("row").filter({ hasText: "ZZZ CLIProxyAPI edge" });
    await edgeRow.getByRole("button", { name: "View details" }).click();

    await expect(page.getByTestId("sidecar-detail")).toContainText("CLIProxyAPI edge detail");
    await expect(page.getByTestId("sidecar-auth-files")).toContainText("edge-oauth.json");
    await expect(page.getByTestId("sidecar-auth-files")).not.toContainText("primary-oauth.json");
  });

  test("creates, edits, tests, syncs, and deletes sidecars through backend API routes", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })]);

    await page.goto("/sidecars");
    await page.getByRole("button", { name: "Add sidecar" }).click();
    await page.getByLabel("Name").fill("CLIProxyAPI edge");
    await page.getByLabel("Base URL").fill("https://edge.example.test");
    await page.getByLabel("Management password").fill(replacementManagementPassword);
    await page.getByRole("button", { name: "Save sidecar" }).click();

    await expect(page.getByTestId("sidecars-summary").getByText("CLIProxyAPI edge")).toBeVisible();
    await expect.poll(() => api.calls).toContain("POST /api/sidecars");
    await expectNoRawSecrets(page);

    const createdSidecarRow = page.getByRole("row").filter({ hasText: "CLIProxyAPI edge" });
    await createdSidecarRow.getByRole("button", { name: "Test connection" }).click();
    await expect.poll(() => api.calls).toContain("POST /api/sidecars/99/test-connection");
    await createdSidecarRow.getByRole("button", { name: "Sync now" }).click();
    await expect.poll(() => api.calls).toContain("POST /api/sidecars/99/sync");

    await createdSidecarRow.getByRole("button", { name: "Edit sidecar" }).click();
    await expect(page.getByLabel("Management password")).toHaveValue("");
    await page.getByLabel("Environment label").fill("staging");
    await page.getByLabel("Management password").fill(rotatedManagementPassword);
    await page.getByRole("button", { name: "Save sidecar" }).click();
    await expect.poll(() => api.calls).toContain("PATCH /api/sidecars/99");
    await expect(page.getByTestId("sidecars-summary").getByText("staging")).toBeVisible();
    await expectNoRawSecrets(page);

    const updatedSidecarRow = page.getByRole("row").filter({ hasText: "CLIProxyAPI edge" });
    await updatedSidecarRow.getByRole("button", { name: "Delete sidecar" }).click();
    await page.getByRole("dialog", { name: "Delete sidecar" }).getByRole("button", { name: "Delete sidecar" }).click();
    await expect.poll(() => api.calls).toContain("DELETE /api/sidecars/99");
    await expect(page.getByTestId("sidecars-summary").getByText("CLIProxyAPI edge")).toBeHidden();
    expect(api.calls.some((call) => call.endsWith("/api/usage-queue"))).toBe(false);
  });
});
