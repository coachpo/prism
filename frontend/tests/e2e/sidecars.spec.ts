import { mkdirSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
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
  quota_exceeded?: boolean;
  quota_reason?: string;
  quota_next_recover_at?: string;
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

type WatchdogPolicy = {
  id: number;
  sidecar_id: number;
  enabled: boolean;
  failure_threshold: number;
  failure_window_seconds: number;
  fallback_cooldown_seconds: number;
  deprioritized_priority: number;
  prioritized_priority: number;
  manual_override_pause_seconds: number;
  probe_batch_size: number;
  probe_timeout_seconds: number;
  created_at: string;
  updated_at: string;
};

type ActionItem = {
  id: number;
  sidecar_id: number;
  auth_id?: string;
  provider?: string;
  action_type: string;
  status: string;
  reason?: string;
  previous_priority?: number;
  target_priority?: number;
  hold_until?: string;
  error_message?: string;
  created_at: string;
  updated_at: string;
  completed_at?: string;
};

const now = "2026-05-10T12:00:00.000Z";
const future = "2099-05-10T12:00:00.000Z";
const past = "2025-05-10T12:00:00.000Z";
const existingManagementPassword = "existing-management-secret";
const replacementManagementPassword = "replacement-secret";
const rotatedManagementPassword = "rotated-super-secret";
const rawSecretValues = [existingManagementPassword, replacementManagementPassword, rotatedManagementPassword];
const evidencePaths = {
  list: "../.sisyphus/evidence/task-13-sidecars-list-stale-degraded.png",
  detail: "../.sisyphus/evidence/task-13-sidecars-detail-inventory.png",
  priorityWarning: "../.sisyphus/evidence/task-13-sidecars-priority-warning.png",
  watchdogActions: "../.sisyphus/evidence/task-13-sidecars-watchdog-action-history.png",
  secretProof: "../.sisyphus/evidence/task-13-sidecars-secret-proof.txt",
};

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
    status: "healthy",
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

function defaultProviderSnapshots(): ProviderSnapshot[] {
  return [
    { id: 21, sidecar_id: 1, provider_key: "gemini", provider_item_key: "gemini", name: "Gemini", status: "available", disabled: false, observed_at: now, snapshot: { models: 4, api_key: "masked" } },
  ];
}

function defaultWatchdogPolicy(): WatchdogPolicy {
  return {
    id: 31,
    sidecar_id: 1,
    enabled: true,
    failure_threshold: 3,
    failure_window_seconds: 3600,
    fallback_cooldown_seconds: 86400,
    deprioritized_priority: 0,
    prioritized_priority: 1,
    manual_override_pause_seconds: 1800,
    probe_batch_size: 3,
    probe_timeout_seconds: 8,
    created_at: now,
    updated_at: now,
  };
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

function defaultActions(): ActionItem[] {
  return [
    { id: 41, sidecar_id: 1, auth_id: "auth-primary", provider: "gemini", action_type: "watchdog.deprioritize", status: "succeeded", reason: "quota_exceeded", previous_priority: 20, target_priority: 0, hold_until: future, created_at: now, updated_at: now, completed_at: now },
    { id: 42, sidecar_id: 1, auth_id: "auth-disabled", provider: "claude", action_type: "watchdog.restore", status: "skipped", reason: "manual_pause", previous_priority: 0, target_priority: 10, created_at: now, updated_at: now },
    { id: 43, sidecar_id: 1, auth_id: "auth-quota", provider: "gemini", action_type: "operator_patch", status: "failed", reason: "status", error_message: "upstream rejected", created_at: now, updated_at: now },
  ];
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
  return bodyText;
}

function writeSecretProofEvidence(bodyText: string, calls: string[]) {
  const proof = [
    "Sidecar browser QA secret proof",
    "Visible body text and page markup were checked against raw management secret fixtures.",
    `Raw secret fixtures checked: ${rawSecretValues.length}`,
    "Rendered UI proof text:",
    bodyText,
    "Mocked backend calls exercised:",
    ...calls,
  ].join("\n");

  rawSecretValues.forEach((secret) => expect(proof).not.toContain(secret));
  mkdirSync(dirname(evidencePaths.secretProof), { recursive: true });
  writeFileSync(evidencePaths.secretProof, proof);
}

type MockSidecarsApiOptions = {
  authSnapshots?: AuthSnapshot[];
  authSnapshotsBySidecarId?: Record<number, AuthSnapshot[]>;
  detailDelayBySidecarId?: Record<number, number>;
};

function defaultAuthSnapshots(): AuthSnapshot[] {
  return [
    authSnapshot({}),
    authSnapshot({ id: 12, auth_id: "auth-quota", name: "quota-oauth.json", status: "quota_exceeded", priority: 0, quota_exceeded: true, quota_reason: "daily limit", quota_next_recover_at: future, next_retry_after: future }),
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
    authSnapshot({ id: 108, auth_id: "auth-quota-zero", name: "quota-zero.json", provider: "gemini", label: "quota-fixture", status: "quota_exceeded", priority: 0, quota_exceeded: true, quota_reason: "daily limit" }),
    ...paginatedMatches,
  ];
}

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
  let watchdogPolicy = defaultWatchdogPolicy();
  const actions = defaultActions();
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
      throw new Error("sidecars page must not call /api/usage-queue");
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
    const watchdogPolicyMatch = pathname.match(/^\/api\/sidecars\/(\d+)\/watchdog-policy$/);
    if (watchdogPolicyMatch && request.method() === "GET") {
      const sidecarId = Number(watchdogPolicyMatch[1]);
      await delayDetail(sidecarId);
      return json(route, watchdogPolicy);
    }
    if (pathname.match(/^\/api\/sidecars\/\d+\/watchdog-policy$/) && request.method() === "PUT") {
      watchdogPolicy = { ...watchdogPolicy, ...JSON.parse(request.postData() ?? "{}"), updated_at: now };
      return json(route, watchdogPolicy);
    }
    const actionsMatch = pathname.match(/^\/api\/sidecars\/(\d+)\/actions$/);
    if (actionsMatch && request.method() === "GET") {
      const sidecarId = Number(actionsMatch[1]);
      await delayDetail(sidecarId);
      return json(route, { items: sidecarId === 2 ? [] : actions });
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
    await page.screenshot({ path: evidencePaths.list, fullPage: true });
  });

  test("renders sidecar detail inventory, policy, actions, and priority warning", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })]);

    await page.goto("/sidecars");

    await expect(page.getByTestId("sidecar-detail")).toContainText("CLIProxyAPI primary detail");
    await expect(page.getByTestId("sidecar-auth-files")).toContainText("quota-oauth.json");
    await expect(page.getByTestId("sidecar-auth-files")).toContainText("Priority 0 is not exclusion");
    await expect(page.getByTestId("sidecar-provider-inventory")).toContainText("Provider inventory");
    await expect(page.getByTestId("sidecar-provider-inventory")).toContainText("Masked fields");
    await expect(page.getByTestId("sidecar-watchdog-policy")).toContainText("Probe priority safety note");
    await expect(page.getByTestId("sidecar-watchdog-policy")).toContainText("due holds may still probe lower-priority auth");
    await expect(page.getByTestId("sidecar-action-history")).toContainText("watchdog.restore");
    await expect(page.getByTestId("sidecar-action-history")).toContainText("Failed");
    await expectNoRawSecrets(page);
    await page.screenshot({ path: evidencePaths.detail, fullPage: true });

    const quotaAuthRow = page.getByRole("row").filter({ hasText: "quota-oauth.json" });
    await page.getByLabel("Priority for quota-oauth.json").fill("0");
    await quotaAuthRow.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("alertdialog")).toContainText("Priority 0 is lowest/last resort, not guaranteed exclusion");
    await expectNoRawSecrets(page);
    await page.screenshot({ path: evidencePaths.priorityWarning, fullPage: true });
    await page.getByLabel("Allow watchdog immediately").check();
    await page.getByRole("button", { name: "Apply change" }).click();
    await expect.poll(() => api.calls).toContain("PATCH /api/sidecars/1/auth-files/auth-quota/fields");

    await page.getByLabel("Failure threshold").fill("5");
    await page.getByLabel("Fallback cooldown (seconds)").fill("86400");
    await page.getByRole("button", { name: "Save watchdog policy" }).click();
    await expect.poll(() => api.calls).toContain("PUT /api/sidecars/1/watchdog-policy");
    await expect(page.getByLabel("Failure threshold")).toHaveValue("5");
    await expect(page.getByTestId("sidecar-action-history")).toContainText("watchdog.deprioritize");
    await expectNoRawSecrets(page);
    await page.screenshot({ path: evidencePaths.watchdogActions, fullPage: true });
  });

  test("filters, sorts, tie-breaks, and paginates auth files", async ({ page }) => {
    await mockSidecarsApi(page, [sidecar({ id: 1 })], { authSnapshots: authFilterSortSnapshots() });

    await page.goto("/sidecars");

    const authFiles = page.getByTestId("sidecar-auth-files");
    const authSearch = page.getByLabel("Filter auth files");

    await expect(authFiles).toContainText("1-100 of 109");
    await authSearch.fill("gamma-page");
    await expect(authFiles).toContainText("gamma-page-001.json");
    await expect(authFiles).toContainText("1-100 of 101");
    await expect(authFiles).not.toContainText("zeta-high.json");

    await authSearch.fill("claude");
    await expect(authFiles).toContainText("alpha-missing.json");
    await expect(authFiles).toContainText("1-1 of 1");
    await expect(authFiles).not.toContainText("zeta-high.json");

    await authSearch.fill("quota_exceeded");
    await expect(authFiles).toContainText("quota-zero.json");
    await expect(authFiles).toContainText("1-1 of 1");
    await authSearch.fill("quota-fixture");
    await expect(authFiles).toContainText("quota-zero.json");
    await expect(authFiles).toContainText("1-1 of 1");

    await authSearch.fill("priority 90");
    await expect(authFiles).toContainText("zeta-high.json");
    await expect(authFiles).toContainText("1-1 of 1");
    await expect(authFiles).not.toContainText("omega-low.json");

    await authSearch.fill("missing-auth-file");
    await expect(authFiles).toContainText("No auth files match");
    await expect(authFiles).not.toContainText("1-100 of 101");

    await authSearch.fill("");
    await expect(authFiles).toContainText("1-100 of 109");
    await expect(authFiles).toContainText("alpha-shared.json");

    await page.getByTestId("sidecar-auth-sort-select").click();
    await page.getByRole("option", { name: "Routing priority: high to low" }).click();
    await expect(authFiles).toContainText("zeta-high.json");
    await expectAuthOrder(page, ["auth-zeta-high", "auth-alpha-a", "auth-alpha-b", "auth-beta", "auth-gamma-001"]);

    await page.getByTestId("sidecar-auth-sort-select").click();
    await page.getByRole("option", { name: "Routing priority: low to high" }).click();
    await expect(authFiles).toContainText("alpha-missing.json");
    await expectAuthOrder(page, ["auth-alpha-missing-a", "auth-alpha-missing-b", "auth-quota-zero", "auth-omega-low", "auth-gamma-001"]);

    await expectNoRawSecrets(page);
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
    const bodyText = await expectNoRawSecrets(page);
    writeSecretProofEvidence(bodyText, api.calls);

    const updatedSidecarRow = page.getByRole("row").filter({ hasText: "CLIProxyAPI edge" });
    await updatedSidecarRow.getByRole("button", { name: "Delete sidecar" }).click();
    await page.getByRole("dialog", { name: "Delete sidecar" }).getByRole("button", { name: "Delete sidecar" }).click();
    await expect.poll(() => api.calls).toContain("DELETE /api/sidecars/99");
    await expect(page.getByTestId("sidecars-summary").getByText("CLIProxyAPI edge")).toBeHidden();
    expect(api.calls.some((call) => call.endsWith("/api/usage-queue"))).toBe(false);
  });
});
