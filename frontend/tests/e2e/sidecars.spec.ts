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

type AuthFile = {
  id?: number;
  sidecar_id: number;
  auth_id: string;
  mutation_safe: boolean;
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

function authFile(overrides: Partial<AuthFile>): AuthFile {
  return {
    id: 11,
    sidecar_id: 1,
    auth_id: "auth-primary",
    mutation_safe: true,
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

function defaultAuthFiles(): AuthFile[] {
  return [
    authFile({}),
    authFile({ id: 12, auth_id: "auth-zero-priority", name: "zero-priority.json", status: "active", priority: 0 }),
    authFile({ id: 13, auth_id: "auth-disabled", name: "disabled-oauth.json", provider: "claude", disabled: true, unavailable: true, priority: undefined }),
  ];
}

function authFilterSortFiles(): AuthFile[] {
  const paginatedMatches = Array.from({ length: 101 }, (_, index) => {
    const ordinal = String(index + 1).padStart(3, "0");
    return authFile({
      id: 1000 + index,
      auth_id: `auth-gamma-${ordinal}`,
      name: `gamma-page-${ordinal}.json`,
      label: "gamma-bulk",
      priority: 10,
    });
  });

  return [
    authFile({ id: 101, auth_id: "auth-zeta-high", name: "zeta-high.json", label: "sort-fixture", priority: 90 }),
    authFile({ id: 102, auth_id: "auth-alpha-a", name: "alpha-shared.json", label: "tie-fixture", priority: 50 }),
    authFile({ id: 103, auth_id: "auth-alpha-b", name: "alpha-shared.json", label: "tie-fixture", priority: 50 }),
    authFile({ id: 104, auth_id: "auth-beta", name: "beta-shared.json", label: "tie-fixture", priority: 50 }),
    authFile({ id: 105, auth_id: "auth-omega-low", name: "omega-low.json", label: "sort-fixture", priority: 1 }),
    authFile({ id: 106, auth_id: "auth-alpha-missing-a", name: "alpha-missing.json", provider: "claude", label: "provider-fixture", priority: undefined }),
    authFile({ id: 107, auth_id: "auth-alpha-missing-b", name: "alpha-missing.json", provider: "openai", label: "missing-priority-fixture", priority: undefined }),
    authFile({ id: 108, auth_id: "auth-priority-floor", name: "priority-floor.json", provider: "gemini", label: "floor-fixture", status: "active", priority: 0 }),
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

type AuthMutationOutcome = "succeeded" | "live_not_found" | "unsafe_identity" | "upstream_failure" | "succeeded_sync_failed";

const retiredAuthInventorySegment = ["auth", "snapshots"].join("-");
const liveAuthInventorySegment = "auth-files";

type MockSidecarsApiOptions = {
  authFiles?: AuthFile[];
  authFilesBySidecarId?: Record<number, AuthFile[]>;
  authFilesFailureAfterRequestBySidecarId?: Record<number, number>;
  authFilesFailureDetailBySidecarId?: Record<number, string>;
  authModelsByName?: Record<string, AuthModel[]>;
  authModelsUnsupportedNames?: string[];
  detailDelayBySidecarId?: Record<number, number>;
  deleteMutationOutcomesByAuthId?: Record<string, AuthMutationOutcome[]>;
  deleteSyncErrorByAuthId?: Record<string, string>;
  fieldsMutationOutcomesByAuthId?: Record<string, AuthMutationOutcome[]>;
  fieldsSyncErrorByAuthId?: Record<string, string>;
  statusMutationOutcomesByAuthId?: Record<string, AuthMutationOutcome[]>;
  statusSyncErrorByAuthId?: Record<string, string>;
  syncStateBySidecarId?: Record<number, "succeeded" | "succeeded_sync_failed" | "failed">;
  syncErrorBySidecarId?: Record<number, string>;
};

function countCalls(calls: string[], target: string) {
  return calls.filter((call) => call === target).length;
}

function expectLiveAuthInventoryOnly(calls: string[], sidecarId = 1) {
  expect(calls.some((call) => call.includes(retiredAuthInventorySegment))).toBe(false);
  expect(countCalls(calls, `GET /api/sidecars/${sidecarId}/${liveAuthInventorySegment}`)).toBeGreaterThan(0);
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
  let authFiles = options.authFiles ? [...options.authFiles] : defaultAuthFiles();
  const providerSnapshots = defaultProviderSnapshots();
  const calls: string[] = [];
  const deletePayloads: Array<{ authId: string; payload: Record<string, unknown> }> = [];
  const fieldPatchPayloads: Array<{ authId: string; payload: Record<string, unknown> }> = [];
  const statusPatchPayloads: Array<{ authId: string; payload: Record<string, unknown> }> = [];
  const deleteMutationOutcomeQueues = Object.fromEntries(
    Object.entries(options.deleteMutationOutcomesByAuthId ?? {}).map(([authId, outcomes]) => [authId, [...outcomes]]),
  );
  const fieldsMutationOutcomeQueues = Object.fromEntries(
    Object.entries(options.fieldsMutationOutcomesByAuthId ?? {}).map(([authId, outcomes]) => [authId, [...outcomes]]),
  );
  const statusMutationOutcomeQueues = Object.fromEntries(
    Object.entries(options.statusMutationOutcomesByAuthId ?? {}).map(([authId, outcomes]) => [authId, [...outcomes]]),
  );
  const authFilesRequestCounts = new Map<number, number>();
  const detailRequestCounts = new Map<number, number>();

  const delayDetail = async (sidecarId: number) => {
    const delay = options.detailDelayBySidecarId?.[sidecarId] ?? 0;
    if (delay > 0) {
      await new Promise((resolve) => setTimeout(resolve, delay));
    }
  };

  const authFilesFor = (sidecarId: number) => {
    const scopedFiles = options.authFilesBySidecarId?.[sidecarId];
    if (scopedFiles) {
      return scopedFiles;
    }
    return sidecarId === 2
      ? [authFile({ id: 22, sidecar_id: 2, auth_id: "auth-edge", name: "edge-oauth.json", provider: "codex", priority: 5 })]
      : authFiles;
  };

  const failMutation = (route: Route, outcome: AuthMutationOutcome, detail: string) => {
    if (outcome === "live_not_found") {
      return json(route, { detail: "auth file not found in live sidecar state" }, 404);
    }
    if (outcome === "unsafe_identity") {
      return json(route, { detail: "unsafe_auth_identity: duplicate live auth id" }, 409);
    }
    if (outcome === "upstream_failure") {
      return json(route, { detail }, 424);
    }
    return null;
  };

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname } = url;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    calls.push(`${request.method()} ${pathname}`);

    if (pathname.includes(retiredAuthInventorySegment)) {
      throw new Error(`sidecars page must not call removed auth inventory route: ${pathname}`);
    }
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

    const authFilesMatch = pathname.match(/^\/api\/sidecars\/(\d+)\/auth-files$/);
    if (authFilesMatch && request.method() === "GET") {
      const sidecarId = Number(authFilesMatch[1]);
      await delayDetail(sidecarId);
      const requestNumber = (authFilesRequestCounts.get(sidecarId) ?? 0) + 1;
      authFilesRequestCounts.set(sidecarId, requestNumber);
      const failAfter = options.authFilesFailureAfterRequestBySidecarId?.[sidecarId];
      if (failAfter !== undefined && requestNumber > failAfter) {
        return json(route, { detail: options.authFilesFailureDetailBySidecarId?.[sidecarId] ?? "auth files refresh failed" }, 500);
      }
      return json(route, { items: authFilesFor(sidecarId) });
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
      const failure = failMutation(route, outcome, "upstream refused auth delete");
      if (failure) {
        return failure;
      }
      const previousAuthFile = authFiles.find((authFile) => authFile.auth_id === authId);
      if (outcome === "succeeded_sync_failed") {
        return json(route, {
          state: "succeeded_sync_failed",
          snapshot: previousAuthFile,
          sync_error: options.deleteSyncErrorByAuthId?.[authId] ?? "auth delete refresh failed",
        });
      }
      authFiles = authFiles.filter((authFile) => authFile.auth_id !== authId);
      return json(route, { state: "succeeded" });
    }

    const mutationMatch = pathname.match(/^\/api\/sidecars\/(\d+)\/auth-files\/([^/]+)\/(status|fields)$/);
    if (mutationMatch && request.method() === "PATCH") {
      const authId = decodeURIComponent(mutationMatch[2]);
      const mutationKind = mutationMatch[3];
      const payload = JSON.parse(request.postData() ?? "{}") as Partial<AuthFile> & Record<string, unknown>;
      if (mutationKind === "status") {
        statusPatchPayloads.push({ authId, payload });
        const outcomeQueue = statusMutationOutcomeQueues[authId] ?? [];
        const outcome = outcomeQueue.shift() ?? "succeeded";
        const failure = failMutation(route, outcome, "upstream refused auth status");
        if (failure) {
          return failure;
        }
        const previousAuthFile = authFiles.find((authFile) => authFile.auth_id === authId);
        if (outcome === "succeeded_sync_failed") {
          return json(route, {
            state: "succeeded_sync_failed",
            snapshot: previousAuthFile,
            sync_error: options.statusSyncErrorByAuthId?.[authId] ?? "auth detail refresh failed",
          });
        }
        authFiles = authFiles.map((authFile) => authFile.auth_id === authId ? { ...authFile, disabled: Boolean(payload.disabled) } : authFile);
        return json(route, { state: "succeeded", snapshot: authFiles.find((authFile) => authFile.auth_id === authId) });
      }
      fieldPatchPayloads.push({ authId, payload });
      const outcomeQueue = fieldsMutationOutcomeQueues[authId] ?? [];
      const outcome = outcomeQueue.shift() ?? "succeeded";
      const failure = failMutation(route, outcome, "upstream refused auth fields");
      if (failure) {
        return failure;
      }
      const previousAuthFile = authFiles.find((authFile) => authFile.auth_id === authId);
      if (outcome === "succeeded_sync_failed") {
        return json(route, {
          state: "succeeded_sync_failed",
          snapshot: previousAuthFile,
          sync_error: options.fieldsSyncErrorByAuthId?.[authId] ?? "auth field refresh failed",
        });
      }
      authFiles = authFiles.map((authFile) => {
        if (authFile.auth_id !== authId) {
          return authFile;
        }
        const nextAuthFile: AuthFile = { ...authFile, ...payload };
        if (payload.priority === 0) {
          delete nextAuthFile.priority;
        }
        return nextAuthFile;
      });
      return json(route, { state: "succeeded", snapshot: authFiles.find((authFile) => authFile.auth_id === authId) });
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
    await expect.poll(() => api.calls).toContain("GET /api/sidecars/1/auth-files");
    await expect.poll(() => api.calls).toContain("GET /api/sidecars/1/provider-snapshots");
    expectLiveAuthInventoryOnly(api.calls);
    await expectNoRawSecrets(page);

    const zeroPriorityRow = page.getByRole("row").filter({ hasText: "zero-priority.json" });
    await page.getByLabel("Priority for zero-priority.json").fill("-1");
    await expect(zeroPriorityRow.getByRole("button", { name: "Save" })).toBeDisabled();
    await expect(zeroPriorityRow).toContainText("Enter 0 to clear/reset via PATCH, or a positive whole-number priority.");

    await page.getByLabel("Priority for zero-priority.json").fill("0");
    await expect(zeroPriorityRow.getByRole("button", { name: "Save" })).toBeEnabled();
    await zeroPriorityRow.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("alertdialog", { name: "Confirm manual auth mutation" })).toContainText("Saving 0 sends PATCH priority: 0");
    const authFilesBeforeSave = countCalls(api.calls, "GET /api/sidecars/1/auth-files");
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect.poll(() => api.fieldPatchPayloads.map(({ authId, payload }) => `${authId}:${String(payload.priority)}`)).toContain("auth-zero-priority:0");
    await expect.poll(() => countCalls(api.calls, "GET /api/sidecars/1/auth-files")).toBeGreaterThan(authFilesBeforeSave);
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
      authFiles: [authFile({ model_states: [{ id: "snapshot-only-model" }] })],
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
      authFiles: [
        authFile({ id: 201, auth_id: "auth-runtime", name: "runtime-only.json", snapshot: { delete_supported: true, runtime_only: true, source: "file", path_present: true } }),
        authFile({ id: 202, auth_id: "auth-memory", name: "memory-only.json", snapshot: { delete_supported: true, runtime_only: false, source: "memory", path_present: true } }),
        authFile({ id: 203, auth_id: "auth-missing-path", name: "missing-path.json", snapshot: { delete_supported: true, runtime_only: false, source: "file", path_present: false } }),
        authFile({ id: 204, auth_id: "auth-path-like", name: "nested/path-like.json", snapshot: { delete_supported: true, runtime_only: false, source: "file", path_present: true } }),
        authFile({ id: 205, auth_id: "name-derived.json", name: "name-derived.json", mutation_safe: false, snapshot: { delete_supported: true, runtime_only: false, source: "file", path_present: true } }),
        authFile({ id: 206, auth_id: "auth-unknown-delete", name: "unknown-delete.json", snapshot: { delete_supported: false, runtime_only: false, source: "file", path_present: true } }),
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
    const authFilesBeforeSave = countCalls(api.calls, "GET /api/sidecars/1/auth-files");
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect.poll(() => api.fieldPatchPayloads.map(({ authId, payload }) => `${authId}:${String(payload.priority)}`)).toContain("auth-primary:42");
    await expect.poll(() => countCalls(api.calls, "GET /api/sidecars/1/auth-files")).toBeGreaterThan(authFilesBeforeSave);
    await expect(primaryRow).toContainText("priority 42");
    expectLiveAuthInventoryOnly(api.calls);
    await expectNoRawSecrets(page);
  });

  test("authfile detail refresh failure preserves last state", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      authFilesFailureAfterRequestBySidecarId: { 1: 1 },
      authFilesFailureDetailBySidecarId: { 1: "detail refresh failed after priority patch" },
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
      authFiles: [
        authFile({
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
        authFile({ id: 32, auth_id: "name-derived-fields.json", name: "name-derived-fields.json", mutation_safe: false, disabled: false }),
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

  test("authfile priority surfaces live not-found and unsafe identity failures", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      authFiles: [
        authFile({ id: 41, auth_id: "auth-priority-missing", name: "priority-missing.json", priority: 20 }),
        authFile({ id: 42, auth_id: "auth-priority-unsafe", name: "priority-unsafe.json", priority: 30 }),
      ],
      fieldsMutationOutcomesByAuthId: {
        "auth-priority-missing": ["live_not_found"],
        "auth-priority-unsafe": ["unsafe_identity"],
      },
    });

    await page.goto("/sidecars");

    const missingRow = page.getByRole("row").filter({ hasText: "priority-missing.json" });
    await page.getByLabel("Priority for priority-missing.json").fill("42");
    await missingRow.getByRole("button", { name: "Save" }).click();
    const authFilesBeforeMissingSave = countCalls(api.calls, "GET /api/sidecars/1/auth-files");
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect.poll(() => api.fieldPatchPayloads.map(({ authId, payload }) => `${authId}:${String(payload.priority)}`)).toContain("auth-priority-missing:42");
    await expect.poll(() => countCalls(api.calls, "GET /api/sidecars/1/auth-files")).toBeGreaterThan(authFilesBeforeMissingSave);
    await expect(missingRow).toContainText("Live auth file was not found in the current sidecar state");

    const unsafeRow = page.getByRole("row").filter({ hasText: "priority-unsafe.json" });
    await page.getByLabel("Priority for priority-unsafe.json").fill("43");
    await unsafeRow.getByRole("button", { name: "Save" }).click();
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect.poll(() => api.fieldPatchPayloads.map(({ authId, payload }) => `${authId}:${String(payload.priority)}`)).toContain("auth-priority-unsafe:43");
    await expect(unsafeRow).toContainText("Prism blocked this row because its live auth identity is unsafe: duplicate live auth id.");
    expectLiveAuthInventoryOnly(api.calls);
  });

  test("renders status controls only for safe auth rows and handles normal status success", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      authFiles: [
        authFile({ id: 31, auth_id: "auth-safe-enabled", name: "safe-enabled.json", disabled: false }),
        authFile({ id: 32, auth_id: "auth_safe_disabled", auth_index: "auth_032", name: "safe-disabled.json", disabled: true, unavailable: false }),
        authFile({ id: 33, auth_id: "name-derived.json", name: "name-derived.json", mutation_safe: false, disabled: false }),
        authFile({ id: 34, auth_id: "auth-duplicate-a", name: "duplicate-name.json", mutation_safe: false, disabled: false }),
        authFile({ id: 35, auth_id: "auth-duplicate-b", name: "duplicate-name.json", mutation_safe: false, disabled: false }),
        authFile({ id: 36, auth_id: "auth-unavailable", name: "unavailable.json", disabled: true, unavailable: true }),
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
    const authFilesBeforeStatusSave = countCalls(api.calls, "GET /api/sidecars/1/auth-files");
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect.poll(() => api.statusPatchPayloads.map(({ authId, payload }) => `${authId}:${String(payload.disabled)}`)).toContain("auth-safe-enabled:true");
    await expect.poll(() => countCalls(api.calls, "GET /api/sidecars/1/auth-files")).toBeGreaterThan(authFilesBeforeStatusSave);
    await expect(safeEnabledRow).toContainText("Disabled");
    await expect(safeEnabledRow).toContainText("Auth status updated and live auth files refreshed.");
    expectLiveAuthInventoryOnly(api.calls);
  });

  test("authfile status mutation reports live not-found and refreshes live files", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      statusMutationOutcomesByAuthId: { "auth-primary": ["live_not_found"] },
    });

    await page.goto("/sidecars");

    const primaryRow = page.getByRole("row").filter({ hasText: "primary-oauth.json" });
    await primaryRow.getByRole("button", { name: "Disable auth primary-oauth.json" }).click();
    const authFilesBeforeStatusSave = countCalls(api.calls, "GET /api/sidecars/1/auth-files");
    await page.getByRole("alertdialog", { name: "Confirm manual auth mutation" }).getByRole("button", { name: "Apply change" }).click();

    await expect.poll(() => api.statusPatchPayloads.map(({ authId, payload }) => `${authId}:${String(payload.disabled)}`)).toContain("auth-primary:true");
    await expect.poll(() => countCalls(api.calls, "GET /api/sidecars/1/auth-files")).toBeGreaterThan(authFilesBeforeStatusSave);
    await expect(primaryRow).toContainText("Auth status update failed: Live auth file was not found in the current sidecar state");
    expectLiveAuthInventoryOnly(api.calls);
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
    const authFilesBeforeDelete = countCalls(api.calls, "GET /api/sidecars/1/auth-files");
    await dialog.getByRole("button", { name: "Delete auth file" }).click();

    await expect.poll(() => api.deletePayloads).toEqual([{ authId: "auth-primary", payload: { confirm_name: "primary-oauth.json" } }]);
    await expect.poll(() => countCalls(api.calls, "GET /api/sidecars/1/auth-files")).toBeGreaterThan(authFilesBeforeDelete);
    await expect(authFiles).not.toContainText("primary-oauth.json");
    await expect(authFiles).toContainText("zero-priority.json");
    expectLiveAuthInventoryOnly(api.calls);
    await expectNoRawSecrets(page);
  });

  test("authfile delete distinguishes refresh and upstream failures", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      authFiles: [
        authFile({ id: 41, auth_id: "auth-delete-refresh-fail", name: "delete-refresh-fail.json" }),
        authFile({ id: 42, auth_id: "auth-delete-upstream-fail", name: "delete-upstream-fail.json" }),
      ],
      authFilesFailureAfterRequestBySidecarId: { 1: 1 },
      authFilesFailureDetailBySidecarId: { 1: "auth files refresh failed after delete" },
      deleteMutationOutcomesByAuthId: {
        "auth-delete-upstream-fail": ["upstream_failure"],
      },
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
    await expect(detail).toContainText("auth files refresh failed after delete");
    await expect(authFiles).toContainText("delete-refresh-fail.json");

    const upstreamFailRow = page.getByRole("row").filter({ hasText: "delete-upstream-fail.json" });
    await upstreamFailRow.getByRole("button", { name: "Delete auth file delete-upstream-fail.json" }).click();
    await page.getByLabel("Confirm auth file name").fill("delete-upstream-fail.json");
    await page.getByRole("dialog", { name: "Delete auth file" }).getByRole("button", { name: "Delete auth file" }).click();

    await expect.poll(() => api.deletePayloads.map(({ authId, payload }) => `${authId}:${String(payload.confirm_name)}`)).toContain("auth-delete-upstream-fail:delete-upstream-fail.json");
    await expect(upstreamFailRow).toContainText("Auth file delete failed: Sidecar upstream mutation failed: upstream refused auth delete");
    await expect(authFiles).toContainText("delete-upstream-fail.json");
    expectLiveAuthInventoryOnly(api.calls);
    await expectNoRawSecrets(page);
  });

  test("authfile status mutation distinguishes refresh and upstream failures", async ({ page }) => {
    const api = await mockSidecarsApi(page, [sidecar({ id: 1 })], {
      authFiles: [
        authFile({ id: 41, auth_id: "auth-refresh-fail", name: "refresh-fail.json", disabled: false }),
        authFile({ id: 42, auth_id: "auth-upstream-fail", name: "upstream-fail.json", disabled: false }),
      ],
      authFilesFailureAfterRequestBySidecarId: { 1: 1 },
      authFilesFailureDetailBySidecarId: { 1: "detail refresh failed after status patch" },
      statusMutationOutcomesByAuthId: {
        "auth-upstream-fail": ["upstream_failure"],
      },
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

    await expect(upstreamFailRow).toContainText("Auth status update failed: Sidecar upstream mutation failed: upstream refused auth status");
    expectLiveAuthInventoryOnly(api.calls);
  });

  test("filters, sorts, tie-breaks, and paginates auth files", async ({ page }) => {
    await mockSidecarsApi(page, [sidecar({ id: 1 })], { authFiles: authFilterSortFiles() });
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
        authFilesFailureAfterRequestBySidecarId: { 1: 1 },
        authFilesFailureDetailBySidecarId: { 1: "auth files refresh failed after sync" },
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
