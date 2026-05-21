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
  auth_index?: string;
  name: string;
  provider?: string;
  label?: string;
  status?: string;
  status_message?: string;
  disabled?: boolean;
  unavailable?: boolean;
  priority?: number;
  success_count?: number;
  failed_count?: number;
  recent_requests?: unknown;
  model_states?: unknown;
  observed_at: string;
  snapshot?: unknown;
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

type AuthModel = {
  id: string;
  display_name?: string;
  type?: string;
  owned_by?: string;
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
    snapshot: { delete_supported: true, runtime_only: false, source: "file", path_present: true },
    ...overrides,
  };
}

function defaultAuthSnapshots(): AuthSnapshot[] {
  return [
    authSnapshot({}),
    authSnapshot({ id: 12, auth_id: "auth-zero-priority", name: "zero-priority.json", status: "active", priority: 0 }),
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

type AuthStatusMutationOutcome = "succeeded" | "stale_snapshot" | "upstream_failure" | "succeeded_sync_failed";

type MockSidecarsApiOptions = {
  authModelsByName?: Record<string, AuthModel[]>;
  authModelsUnsupportedNames?: string[];
  authSnapshots?: AuthSnapshot[];
  authSnapshotsBySidecarId?: Record<number, AuthSnapshot[]>;
  authSnapshotFailureAfterRequestBySidecarId?: Record<number, number>;
  authSnapshotFailureDetailBySidecarId?: Record<number, string>;
  detailDelayBySidecarId?: Record<number, number>;
  deleteMutationOutcomesByAuthId?: Record<string, AuthStatusMutationOutcome[]>;
  deleteSyncErrorByAuthId?: Record<string, string>;
  fieldsMutationOutcomesByAuthId?: Record<string, AuthStatusMutationOutcome[]>;
  fieldsSyncErrorByAuthId?: Record<string, string>;
  statusMutationOutcomesByAuthId?: Record<string, AuthStatusMutationOutcome[]>;
  statusSyncErrorByAuthId?: Record<string, string>;
  syncStateBySidecarId?: Record<number, "succeeded" | "succeeded_sync_failed" | "failed">;
  syncErrorBySidecarId?: Record<number, string>;
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
  const deletePayloads: Array<{ authId: string; payload: Record<string, unknown> }> = [];
  const fieldPatchPayloads: Array<{ authId: string; forceLive: boolean; payload: Record<string, unknown> }> = [];
  const statusPatchPayloads: Array<{ authId: string; forceLive: boolean; payload: Record<string, unknown> }> = [];
  const deleteMutationOutcomeQueues = Object.fromEntries(
    Object.entries(options.deleteMutationOutcomesByAuthId ?? {}).map(([authId, outcomes]) => [authId, [...outcomes]]),
  );
  const fieldsMutationOutcomeQueues = Object.fromEntries(
    Object.entries(options.fieldsMutationOutcomesByAuthId ?? {}).map(([authId, outcomes]) => [authId, [...outcomes]]),
  );
  const statusMutationOutcomeQueues = Object.fromEntries(
    Object.entries(options.statusMutationOutcomesByAuthId ?? {}).map(([authId, outcomes]) => [authId, [...outcomes]]),
  );
  const authSnapshotRequestCounts = new Map<number, number>();
  const detailRequestCounts = new Map<number, number>();

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
      const requestNumber = (authSnapshotRequestCounts.get(sidecarId) ?? 0) + 1;
      authSnapshotRequestCounts.set(sidecarId, requestNumber);
      const failAfter = options.authSnapshotFailureAfterRequestBySidecarId?.[sidecarId];
      if (failAfter !== undefined && requestNumber > failAfter) {
        return json(route, { detail: options.authSnapshotFailureDetailBySidecarId?.[sidecarId] ?? "auth snapshot refresh failed" }, 500);
      }
      return json(route, { items: authSnapshotsFor(sidecarId) });
    }
    const providerSnapshotsMatch = pathname.match(/^\/api\/sidecars\/(\d+)\/provider-snapshots$/);
    if (providerSnapshotsMatch && request.method() === "GET") {
      const sidecarId = Number(providerSnapshotsMatch[1]);
      await delayDetail(sidecarId);
      const providerRequestNumber = (detailRequestCounts.get(sidecarId) ?? 0) + 1;
      detailRequestCounts.set(sidecarId, providerRequestNumber);
      return json(route, { items: providerSnapshots });
    }
    const modelsMatch = pathname.match(/^\/api\/sidecars\/(\d+)\/auth-files\/models$/);
    if (modelsMatch && request.method() === "GET") {
      const name = url.searchParams.get("name") ?? "";
      if (options.authModelsUnsupportedNames?.includes(name)) {
        return json(route, { detail: "authfile models discovery unsupported" }, 404);
      }
      return json(route, { models: options.authModelsByName?.[name] ?? [] });
    }
    const deleteAuthMatch = pathname.match(/^\/api\/sidecars\/(\d+)\/auth-files\/([^/]+)$/);
    if (deleteAuthMatch && request.method() === "DELETE") {
      const authId = decodeURIComponent(deleteAuthMatch[2]);
      const payload = JSON.parse(request.postData() ?? "{}") as Record<string, unknown>;
      deletePayloads.push({ authId, payload });
      const outcomeQueue = deleteMutationOutcomeQueues[authId] ?? [];
      const outcome = outcomeQueue.shift() ?? "succeeded";
      if (outcome === "stale_snapshot") {
        return json(route, { detail: "stale_auth_confirmation" }, 409);
      }
      if (outcome === "upstream_failure") {
        return json(route, { detail: "upstream refused auth delete" }, 424);
      }
      const previousSnapshot = authSnapshots.find((snapshot) => snapshot.auth_id === authId);
      if (outcome === "succeeded_sync_failed") {
        return json(route, {
          state: "succeeded_sync_failed",
          snapshot: previousSnapshot,
          sync_error: options.deleteSyncErrorByAuthId?.[authId] ?? "auth delete refresh failed",
        });
      }
      authSnapshots = authSnapshots.filter((snapshot) => snapshot.auth_id !== authId);
      return json(route, { state: "succeeded" });
    }

    const mutationMatch = pathname.match(/^\/api\/sidecars\/(\d+)\/auth-files\/([^/]+)\/(status|fields)$/);
    if (mutationMatch && request.method() === "PATCH") {
      const authId = decodeURIComponent(mutationMatch[2]);
      const mutationKind = mutationMatch[3];
      const payload = JSON.parse(request.postData() ?? "{}") as Partial<AuthSnapshot> & Record<string, unknown>;
      if (mutationKind === "status") {
        const forceLive = url.searchParams.get("force_live") === "true";
        statusPatchPayloads.push({ authId, forceLive, payload });
        const outcomeQueue = statusMutationOutcomeQueues[authId] ?? [];
        const outcome = outcomeQueue.shift() ?? "succeeded";
        if (outcome === "stale_snapshot") {
          return json(route, { detail: "stale_snapshot" }, 409);
        }
        if (outcome === "upstream_failure") {
          return json(route, { detail: "upstream refused auth status" }, 424);
        }
        const previousSnapshot = authSnapshots.find((snapshot) => snapshot.auth_id === authId);
        if (outcome === "succeeded_sync_failed") {
          return json(route, {
            state: "succeeded_sync_failed",
            snapshot: previousSnapshot,
            sync_error: options.statusSyncErrorByAuthId?.[authId] ?? "auth detail refresh failed",
          });
        }
        authSnapshots = authSnapshots.map((snapshot) => snapshot.auth_id === authId ? { ...snapshot, disabled: Boolean(payload.disabled) } : snapshot);
        return json(route, { state: "succeeded", snapshot: authSnapshots.find((snapshot) => snapshot.auth_id === authId) });
      }
      const forceLive = url.searchParams.get("force_live") === "true";
      fieldPatchPayloads.push({ authId, forceLive, payload });
      const outcomeQueue = fieldsMutationOutcomeQueues[authId] ?? [];
      const outcome = outcomeQueue.shift() ?? "succeeded";
      if (outcome === "stale_snapshot") {
        return json(route, { detail: "stale_snapshot" }, 409);
      }
      if (outcome === "upstream_failure") {
        return json(route, { detail: "upstream refused auth fields" }, 424);
      }
      const previousSnapshot = authSnapshots.find((snapshot) => snapshot.auth_id === authId);
      if (outcome === "succeeded_sync_failed") {
        return json(route, {
          state: "succeeded_sync_failed",
          snapshot: previousSnapshot,
          sync_error: options.fieldsSyncErrorByAuthId?.[authId] ?? "auth field refresh failed",
        });
      }
      authSnapshots = authSnapshots.map((snapshot) => {
        if (snapshot.auth_id !== authId) {
          return snapshot;
        }
        const nextSnapshot: AuthSnapshot = { ...snapshot, ...payload };
        if (payload.priority === 0) {
          delete nextSnapshot.priority;
        }
        return nextSnapshot;
      });
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
        const state = options.syncStateBySidecarId?.[id] ?? "succeeded";
        const errorDetail = options.syncErrorBySidecarId?.[id] ?? "detail refresh failed";
        const syncResponse = {
          state,
          sidecar: existing,
          sync_status: { ...syncStatus(existing), last_sync_error: state === "succeeded" ? undefined : errorDetail },
          auth_snapshot_count: authSnapshots.length,
          provider_snapshot_count: providerSnapshots.length,
          error_detail: state === "failed" ? errorDetail : undefined,
        };
        return json(route, syncResponse, state === "failed" ? 502 : 200);
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

  return { calls, deletePayloads, fieldPatchPayloads, statusPatchPayloads };
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

  test("authfile priority uses frozen zero semantics", async ({ page }) => {
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
    await expect(page.getByTestId("sidecar-auth-files")).toContainText("priority 0 (baseline)");
    await expect(page.getByTestId("sidecar-auth-files")).toContainText("missing routes in baseline 0 bucket");
    await expect(page.getByTestId("sidecar-auth-files")).toContainText("Enter 0 to clear/reset via PATCH, or a positive whole-number priority.");
    await expect(page.getByTestId("sidecar-provider-inventory")).toContainText("Provider inventory");
    await expect(page.getByTestId("sidecar-provider-inventory")).toContainText("Masked fields");
    await expect.poll(() => api.calls).toContain("GET /api/sidecars/1/auth-snapshots");
    await expect.poll(() => api.calls).toContain("GET /api/sidecars/1/provider-snapshots");
    await expectNoRawSecrets(page);

    const zeroPriorityRow = page.getByRole("row").filter({ hasText: "zero-priority.json" });
    await page.getByLabel("Priority for zero-priority.json").fill("-1");
    await expect(zeroPriorityRow.getByRole("button", { name: "Save" })).toBeDisabled();
    await expect(zeroPriorityRow).toContainText("Enter 0 to clear/reset via PATCH, or a positive whole-number priority.");

    await page.getByLabel("Priority for zero-priority.json").fill("0");
    await expect(zeroPriorityRow.getByRole("button", { name: "Save" })).toBeEnabled();
    await zeroPriorityRow.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("alertdialog", { name: "Confirm manual auth mutation" })).toContainText("Saving 0 sends PATCH priority: 0");
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect.poll(() => api.fieldPatchPayloads.map(({ authId, payload }) => `${authId}:${String(payload.priority)}`)).toContain("auth-zero-priority:0");
    await expect(zeroPriorityRow).toContainText("missing routes in baseline 0 bucket");
    await expectNoRawSecrets(page);
    expect(consoleErrors).toEqual([]);
  });

  test("authfile models modal shows supported data", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      authModelsByName: {
        "primary-oauth.json": [
          { id: "gemini-1.5-pro", display_name: "Gemini 1.5 Pro", type: "chat", owned_by: "google" },
          { id: "gemini-1.5-flash" },
        ],
      },
      authSnapshots: [authSnapshot({ model_states: [{ id: "snapshot-only-model" }] })],
    });

    await page.goto("/sidecars");

    const primaryRow = page.getByRole("row").filter({ hasText: "primary-oauth.json" });
    await primaryRow.getByRole("button", { name: "View read-only models for primary-oauth.json" }).click();

    const dialog = page.getByRole("dialog", { name: "primary-oauth.json models" });
    await expect(dialog).toContainText("Observational only");
    await expect(dialog).toContainText("gemini-1.5-pro");
    await expect(dialog).toContainText("Gemini 1.5 Pro");
    await expect(dialog).toContainText("gemini-1.5-flash");
    await expect(dialog).not.toContainText("snapshot-only-model");
    await expect.poll(() => api.calls).toContain("GET /api/sidecars/1/auth-files/models");
    expect(api.fieldPatchPayloads).toEqual([]);
    expect(api.statusPatchPayloads).toEqual([]);
    await expectNoRawSecrets(page);
  });

  test("authfile models modal reports unsupported sidecar", async ({ page }) => {
    await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      authModelsUnsupportedNames: ["primary-oauth.json"],
    });

    await page.goto("/sidecars");

    const primaryRow = page.getByRole("row").filter({ hasText: "primary-oauth.json" });
    await primaryRow.getByRole("button", { name: "View read-only models for primary-oauth.json" }).click();

    const dialog = page.getByRole("dialog", { name: "primary-oauth.json models" });
    await expect(dialog).toContainText("Models discovery unsupported");
    await expect(dialog).toContainText("does not expose the read-only auth-file models route yet");
    await expect(dialog).not.toContainText("No models returned");
    await expectNoRawSecrets(page);
  });

  test("hides authfile delete action for unsupported and unsafe rows", async ({ page }) => {
    await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      authSnapshots: [
        authSnapshot({ id: 201, auth_id: "auth-runtime", name: "runtime-only.json", snapshot: { delete_supported: true, runtime_only: true, source: "file", path_present: true } }),
        authSnapshot({ id: 202, auth_id: "auth-memory", name: "memory-only.json", snapshot: { delete_supported: true, runtime_only: false, source: "memory", path_present: true } }),
        authSnapshot({ id: 203, auth_id: "auth-missing-path", name: "missing-path.json", snapshot: { delete_supported: true, runtime_only: false, source: "file", path_present: false } }),
        authSnapshot({ id: 204, auth_id: "auth-path-like", name: "nested/path-like.json", snapshot: { delete_supported: true, runtime_only: false, source: "file", path_present: true } }),
        authSnapshot({ id: 205, auth_id: "name-derived.json", name: "name-derived.json", snapshot: { delete_supported: true, runtime_only: false, source: "file", path_present: true } }),
        authSnapshot({ id: 206, auth_id: "auth-unknown-delete", name: "unknown-delete.json", snapshot: { delete_supported: false, runtime_only: false, source: "file", path_present: true } }),
      ],
    });

    await page.goto("/sidecars");

    await expect(page.getByRole("button", { name: /Delete auth file/ })).toHaveCount(0);
    await expectNoRawSecrets(page);
  });

  test("authfile priority positive values still update", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })]);

    await page.goto("/sidecars");

    const primaryRow = page.getByRole("row").filter({ hasText: "primary-oauth.json" });
    await expect(primaryRow).toContainText("priority 20");

    await page.getByLabel("Priority for primary-oauth.json").fill("42");
    await expect(primaryRow.getByRole("button", { name: "Save" })).toBeEnabled();
    await primaryRow.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("alertdialog", { name: "Confirm manual auth mutation" })).toContainText("Positive priorities are written as explicit routing priorities");
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect.poll(() => api.fieldPatchPayloads.map(({ authId, payload }) => `${authId}:${String(payload.priority)}`)).toContain("auth-primary:42");
    await expect(primaryRow).toContainText("priority 42");
    await expectNoRawSecrets(page);
  });

  test("authfile detail refresh failure preserves last state", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      fieldsMutationOutcomesByAuthId: { "auth-primary": ["succeeded_sync_failed"] },
      fieldsSyncErrorByAuthId: { "auth-primary": "detail refresh failed after priority patch" },
    });

    await page.goto("/sidecars");

    const detail = page.getByTestId("sidecar-detail");
    const primaryRow = page.getByRole("row").filter({ hasText: "primary-oauth.json" });
    await page.getByLabel("Priority for primary-oauth.json").fill("42");
    await primaryRow.getByRole("button", { name: "Save" }).click();
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect.poll(() => api.fieldPatchPayloads.map(({ authId, payload }) => `${authId}:${String(payload.priority)}`)).toContain("auth-primary:42");
    await expect(primaryRow).toContainText("Priority changed upstream, but Prism could not refresh auth details");
    await expect(detail).toContainText("detail refresh failed after priority patch");
    await expect(detail.getByTestId("sidecar-auth-files")).toContainText("primary-oauth.json");
    await expectNoRawSecrets(page);
  });

  test("authfile browser hides live field editor controls while keeping priority save", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })]);

    await page.goto("/sidecars");

    const primaryRow = page.getByRole("row").filter({ hasText: "primary-oauth.json" });
    await expect(primaryRow).toContainText("priority 20");
    await expect(page.getByRole("button", { name: /Edit live auth fields/ })).toHaveCount(0);
    await expect(page.getByRole("dialog", { name: "Edit live auth fields" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Apply field edits" })).toHaveCount(0);

    await page.getByLabel("Priority for primary-oauth.json").fill("42");
    await expect(primaryRow.getByRole("button", { name: "Save" })).toBeEnabled();
    await primaryRow.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("alertdialog", { name: "Confirm manual auth mutation" })).toContainText("Positive priorities are written as explicit routing priorities");
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect.poll(() => api.fieldPatchPayloads.length).toBe(1);
    expect(api.fieldPatchPayloads[0]).toEqual({
      authId: "auth-primary",
      forceLive: false,
      payload: { priority: 42 },
    });
    expect(api.fieldPatchPayloads[0].payload).not.toHaveProperty("prefix");
    expect(api.fieldPatchPayloads[0].payload).not.toHaveProperty("proxy_url");
    expect(api.fieldPatchPayloads[0].payload).not.toHaveProperty("note");
    expect(api.fieldPatchPayloads[0].payload).not.toHaveProperty("headers");
    expect(api.fieldPatchPayloads[0].payload).not.toHaveProperty("custom_headers");
    await expect(primaryRow).toContainText("priority 42");
    await expectNoRawSecrets(page);
  });

  test("authfile browser does not expose unresolved live fields", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      authSnapshots: [
        authSnapshot({
          id: 31,
          auth_id: "auth-safe-fields",
          auth_index: "auth_031",
          name: "safe-fields.json",
          snapshot: {
            authorization: "Bearer raw-secret",
            custom_headers: { "x-extra-id": "hidden" },
            refresh_token: "raw-secret",
          },
        }),
        authSnapshot({ id: 32, auth_id: "name-derived-fields.json", name: "name-derived-fields.json", disabled: false }),
      ],
    });

    await page.goto("/sidecars");

    const safeRow = page.getByRole("row").filter({ hasText: "safe-fields.json" });
    await expect(safeRow).toContainText("priority 20");
    await expect(safeRow.getByRole("button", { name: /Edit live auth fields/ })).toHaveCount(0);
    await expect(page.getByRole("button", { name: /Edit live auth fields/ })).toHaveCount(0);
    await expect(page.getByRole("dialog", { name: "Edit live auth fields" })).toHaveCount(0);
    await expect(page.getByText("authorization")).toHaveCount(0);
    await expect(page.getByText("refresh_token")).toHaveCount(0);
    await expect(page.getByText("custom_headers")).toHaveCount(0);
    await expect(page.getByText("x-extra-id")).toHaveCount(0);
    await expect(page.getByText("raw-secret")).toHaveCount(0);
    expect(api.fieldPatchPayloads).toEqual([]);
    await expectNoRawSecrets(page);
  });

  test("retries stale priority edits with live snapshot and preserves detail on degraded refresh", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      fieldsMutationOutcomesByAuthId: { "auth-primary": ["stale_snapshot", "succeeded_sync_failed"] },
      fieldsSyncErrorByAuthId: { "auth-primary": "detail refresh failed after priority patch" },
    });

    await page.goto("/sidecars");

    const detail = page.getByTestId("sidecar-detail");
    const primaryRow = page.getByRole("row").filter({ hasText: "primary-oauth.json" });
    await page.getByLabel("Priority for primary-oauth.json").fill("42");
    await expect(primaryRow.getByRole("button", { name: "Save" })).toBeEnabled();
    await primaryRow.getByRole("button", { name: "Save" }).click();
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect.poll(() => api.fieldPatchPayloads.map(({ authId, forceLive, payload }) => `${authId}:${String(forceLive)}:${String(payload.priority)}`)).toContain("auth-primary:false:42");
    await expect(primaryRow).toContainText("Snapshot is stale");
    await expect(primaryRow.getByRole("button", { name: "Retry with live snapshot" })).toBeVisible();
    expect(api.fieldPatchPayloads[0].payload).toEqual({ priority: 42 });

    await primaryRow.getByRole("button", { name: "Retry with live snapshot" }).click();
    await expect.poll(() => api.fieldPatchPayloads.map(({ authId, forceLive, payload }) => `${authId}:${String(forceLive)}:${String(payload.priority)}`)).toContain("auth-primary:true:42");
    expect(api.fieldPatchPayloads[1].payload).toEqual({ priority: 42 });
    await expect(primaryRow).toContainText("Priority changed upstream, but Prism could not refresh auth details");
    await expect(detail).toContainText("detail refresh failed after priority patch");
    await expect(detail.getByTestId("sidecar-auth-files")).toContainText("primary-oauth.json");
  });

  test("renders status controls only for safe auth rows and handles normal status success", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      authSnapshots: [
        authSnapshot({ id: 31, auth_id: "auth-safe-enabled", name: "safe-enabled.json", disabled: false }),
        authSnapshot({ id: 32, auth_id: "auth_safe_disabled", auth_index: "auth_032", name: "safe-disabled.json", disabled: true, unavailable: false }),
        authSnapshot({ id: 33, auth_id: "name-derived.json", name: "name-derived.json", disabled: false }),
        authSnapshot({ id: 34, auth_id: "auth-duplicate-a", name: "duplicate-name.json", disabled: false }),
        authSnapshot({ id: 35, auth_id: "auth-duplicate-b", name: "duplicate-name.json", disabled: false }),
        authSnapshot({ id: 36, auth_id: "auth-unavailable", name: "unavailable.json", disabled: true, unavailable: true }),
      ],
    });

    await page.goto("/sidecars");

    const safeEnabledRow = page.getByRole("row").filter({ hasText: "safe-enabled.json" });
    const safeDisabledRow = page.getByRole("row").filter({ hasText: "safe-disabled.json" });
    await expect(safeEnabledRow.getByRole("button", { name: "Disable auth safe-enabled.json" })).toBeVisible();
    await expect(safeDisabledRow.getByRole("button", { name: "Enable auth safe-disabled.json" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Disable auth name-derived.json" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Disable auth duplicate-name.json" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Enable auth unavailable.json" })).toHaveCount(0);

    await safeEnabledRow.getByRole("button", { name: "Disable auth safe-enabled.json" }).click();
    await expect(page.getByRole("alertdialog", { name: "Confirm manual auth mutation" })).toContainText("Disabling an auth file uses the backend safety gate");
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect.poll(() => api.statusPatchPayloads.map(({ authId, payload }) => `${authId}:${String(payload.disabled)}`)).toContain("auth-safe-enabled:true");
    await expect(safeEnabledRow).toContainText("Disabled");
    await expect(safeEnabledRow).toContainText("Auth status updated and refreshed local snapshot.");
  });

  test("authfile status toggle retries stale snapshot with force live", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      statusMutationOutcomesByAuthId: { "auth-primary": ["stale_snapshot", "succeeded"] },
    });

    await page.goto("/sidecars");

    const primaryRow = page.getByRole("row").filter({ hasText: "primary-oauth.json" });
    await primaryRow.getByRole("button", { name: "Disable auth primary-oauth.json" }).click();
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect(primaryRow).toContainText("Snapshot is stale");
    await expect(primaryRow.getByRole("button", { name: "Retry with live snapshot" })).toBeVisible();
    await expect.poll(() => api.statusPatchPayloads.map(({ authId, forceLive }) => `${authId}:${String(forceLive)}`)).toContain("auth-primary:false");

    await primaryRow.getByRole("button", { name: "Retry with live snapshot" }).click();
    await expect.poll(() => api.statusPatchPayloads.map(({ authId, forceLive }) => `${authId}:${String(forceLive)}`)).toContain("auth-primary:true");
    await expect(primaryRow).toContainText("Disabled");
    await expect(primaryRow).toContainText("Auth status updated and refreshed local snapshot.");
  });

  test("authfile single delete requires name confirmation", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })]);

    await page.goto("/sidecars");

    const authFiles = page.getByTestId("sidecar-auth-files");
    const primaryRow = page.getByRole("row").filter({ hasText: "primary-oauth.json" });
    await primaryRow.getByRole("button", { name: "Delete auth file primary-oauth.json" }).click();

    const dialog = page.getByRole("dialog", { name: "Delete auth file" });
    await expect(dialog).toContainText("This removes the live auth file upstream");
    await expect(dialog.getByRole("button", { name: "Delete auth file" })).toBeDisabled();
    await page.getByLabel("Confirm auth file name").fill("wrong.json");
    await expect(dialog).toContainText("The name must match the current live auth file exactly.");
    await expect(dialog.getByRole("button", { name: "Delete auth file" })).toBeDisabled();
    await page.getByLabel("Confirm auth file name").fill("primary-oauth.json");
    await dialog.getByRole("button", { name: "Delete auth file" }).click();

    await expect.poll(() => api.deletePayloads).toEqual([{ authId: "auth-primary", payload: { confirm_name: "primary-oauth.json" } }]);
    await expect(authFiles).not.toContainText("primary-oauth.json");
    await expect(authFiles).toContainText("zero-priority.json");
    await expectNoRawSecrets(page);
  });

  test("authfile delete distinguishes refresh failure", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      authSnapshots: [
        authSnapshot({ id: 41, auth_id: "auth-delete-refresh-fail", name: "delete-refresh-fail.json" }),
        authSnapshot({ id: 42, auth_id: "auth-delete-upstream-fail", name: "delete-upstream-fail.json" }),
      ],
      deleteMutationOutcomesByAuthId: {
        "auth-delete-refresh-fail": ["succeeded_sync_failed"],
        "auth-delete-upstream-fail": ["upstream_failure"],
      },
      deleteSyncErrorByAuthId: { "auth-delete-refresh-fail": "detail refresh failed after delete" },
    });

    await page.goto("/sidecars");

    const detail = page.getByTestId("sidecar-detail");
    const authFiles = detail.getByTestId("sidecar-auth-files");
    const refreshFailRow = page.getByRole("row").filter({ hasText: "delete-refresh-fail.json" });
    await refreshFailRow.getByRole("button", { name: "Delete auth file delete-refresh-fail.json" }).click();
    await page.getByLabel("Confirm auth file name").fill("delete-refresh-fail.json");
    await page.getByRole("dialog", { name: "Delete auth file" }).getByRole("button", { name: "Delete auth file" }).click();

    await expect.poll(() => api.deletePayloads.map(({ authId, payload }) => `${authId}:${String(payload.confirm_name)}`)).toContain("auth-delete-refresh-fail:delete-refresh-fail.json");
    await expect(refreshFailRow).toContainText("Auth file was deleted upstream, but Prism could not refresh auth details");
    await expect(detail).toContainText("detail refresh failed after delete");
    await expect(authFiles).toContainText("delete-refresh-fail.json");

    const upstreamFailRow = page.getByRole("row").filter({ hasText: "delete-upstream-fail.json" });
    await upstreamFailRow.getByRole("button", { name: "Delete auth file delete-upstream-fail.json" }).click();
    await page.getByLabel("Confirm auth file name").fill("delete-upstream-fail.json");
    await page.getByRole("dialog", { name: "Delete auth file" }).getByRole("button", { name: "Delete auth file" }).click();

    await expect.poll(() => api.deletePayloads.map(({ authId, payload }) => `${authId}:${String(payload.confirm_name)}`)).toContain("auth-delete-upstream-fail:delete-upstream-fail.json");
    await expect(upstreamFailRow).toContainText("Auth file delete failed: upstream refused auth delete");
    await expect(authFiles).toContainText("delete-upstream-fail.json");
    await expectNoRawSecrets(page);
  });

  test("authfile status mutation distinguishes refresh failure", async ({ page }) => {
    await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      authSnapshots: [
        authSnapshot({ id: 41, auth_id: "auth-refresh-fail", name: "refresh-fail.json", disabled: false }),
        authSnapshot({ id: 42, auth_id: "auth-upstream-fail", name: "upstream-fail.json", disabled: false }),
      ],
      statusMutationOutcomesByAuthId: {
        "auth-refresh-fail": ["succeeded_sync_failed"],
        "auth-upstream-fail": ["upstream_failure"],
      },
      statusSyncErrorByAuthId: { "auth-refresh-fail": "detail refresh failed after status patch" },
    });

    await page.goto("/sidecars");

    const detail = page.getByTestId("sidecar-detail");
    const refreshFailRow = page.getByRole("row").filter({ hasText: "refresh-fail.json" });
    await refreshFailRow.getByRole("button", { name: "Disable auth refresh-fail.json" }).click();
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect(refreshFailRow).toContainText("Enabled");
    await expect(refreshFailRow).toContainText("Status changed upstream, but Prism could not refresh auth details");
    await expect(detail).toContainText("detail refresh failed after status patch");
    await expect(detail.getByTestId("sidecar-auth-files")).toContainText("refresh-fail.json");

    const upstreamFailRow = page.getByRole("row").filter({ hasText: "upstream-fail.json" });
    await upstreamFailRow.getByRole("button", { name: "Disable auth upstream-fail.json" }).click();
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect(upstreamFailRow).toContainText("Auth status update failed: upstream refused auth status");
    await expect(upstreamFailRow).not.toContainText("Retry with live snapshot");
  });

  test("filters, sorts, tie-breaks, and paginates auth files", async ({ page }) => {
    await mockSidecarsApi(page, [sidecar({ id: 1 })], { authSnapshots: authFilterSortSnapshots() });
    await page.setViewportSize({ width: 700, height: 720 });

    await page.goto("/sidecars");

    const authFiles = page.getByTestId("sidecar-auth-files");
    const authSearch = page.getByLabel("Filter auth files");

    await expect(authFiles.getByTestId("sidecar-auth-page-size-select")).toHaveCount(0);
    await expect(authFiles.getByRole("columnheader", { name: "Actions" })).toHaveCount(0);
    await expect(authFiles).toContainText("1-30 of 109");
    await expect.poll(async () => authFiles.getByTestId("sidecar-auth-files-scroll").evaluate((element) => {
      element.scrollLeft = element.scrollWidth;
      return element.scrollWidth > element.clientWidth && element.scrollLeft > 0;
    })).toBe(true);
    await authSearch.fill("gamma-page");
    await expect(authFiles).toContainText("gamma-page-001.json");
    await expect(authFiles).toContainText("gamma-page-030.json");
    await expect(authFiles).not.toContainText("gamma-page-031.json");
    await expect(authFiles).toContainText("1-30 of 101");
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
    await expect(authFiles).not.toContainText("1-30 of 101");

    await authSearch.fill("");
    await expect(authFiles).toContainText("1-30 of 109");
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

  test("preserves the last sidecar detail when refresh fails", async ({ page }) => {
    await mockSidecarsApi(
      page,
      [sidecar({ id: 1, name: "CLIProxyAPI primary" })],
      {
        authSnapshotFailureAfterRequestBySidecarId: { 1: 1 },
        authSnapshotFailureDetailBySidecarId: { 1: "auth snapshot refresh failed after sync" },
      },
    );

    await page.goto("/sidecars");
    const detail = page.getByTestId("sidecar-detail");
    await expect(detail).toContainText("CLIProxyAPI primary detail");
    await expect(detail.getByTestId("sidecar-auth-files")).toContainText("primary-oauth.json", { timeout: 10000 });
    await expect(detail.getByTestId("sidecar-provider-inventory")).toContainText("Provider inventory");

    const primaryRow = page.getByRole("row").filter({ hasText: "CLIProxyAPI primary" });
    await primaryRow.getByRole("button", { name: "Sync now" }).click();

    await expect(detail).toContainText("Failed to load sidecar details.");
    await expect(detail).toContainText("CLIProxyAPI primary detail");
    await expect(detail.getByTestId("sidecar-auth-files")).toContainText("primary-oauth.json", { timeout: 10000 });
    await expect(detail.getByTestId("sidecar-provider-inventory")).toContainText("Provider inventory");
  });

  test("surfaces backend detail when manual sidecar sync fails", async ({ page }) => {
    await mockSidecarsApi(
      page,
      [sidecar({ id: 1, name: "CLIProxyAPI primary" })],
      { syncStateBySidecarId: { 1: "failed" }, syncErrorBySidecarId: { 1: "upstream auth-files contract failed" } },
    );

    await page.goto("/sidecars");
    const detail = page.getByTestId("sidecar-detail");
    await expect(detail.getByTestId("sidecar-auth-files")).toContainText("primary-oauth.json", { timeout: 10000 });

    const primaryRow = page.getByRole("row").filter({ hasText: "CLIProxyAPI primary" });
    await primaryRow.getByRole("button", { name: "Sync now" }).click();

    await expect(detail).toContainText("Sidecar sync did not complete: upstream auth-files contract failed");
    await expect(detail.getByTestId("sidecar-auth-files")).toContainText("primary-oauth.json", { timeout: 10000 });
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
