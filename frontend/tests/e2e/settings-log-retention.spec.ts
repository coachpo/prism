import { expect, test, type Page, type Request } from "@playwright/test";

const timestamp = "2026-05-09T12:00:00Z";
const saveEvidencePath = "../.sisyphus/evidence/task-11-retention-save.png";
const deleteFailureEvidencePath = "../.sisyphus/evidence/task-11-delete-failure.png";

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

function createRetentionSettings() {
  return {
    request_logs_retention_days: 30,
    statistics_retention_days: 30,
    audit_logs_retention_days: 30,
    loadbalance_events_retention_days: 30,
  };
}
type RetentionPayload = ReturnType<typeof createRetentionSettings>;

type RetentionJobPayload = {
  table: string;
  cutoff?: string | null;
  delete_all?: boolean;
  reason: string;
};

type CapturedRetentionRequest<TPayload> = {
  method: string;
  pathname: string;
  body: TPayload;
};

type RetentionRouteOptions = {
  failNextRetentionJob?: boolean;
};

async function mockSettingsRoutes(page: Page, options: RetentionRouteOptions = {}) {
  const profile = createProfile();
  let retentionSettings = createRetentionSettings();
  let failNextRetentionJob = options.failNextRetentionJob ?? false;
  const retentionUpdates: CapturedRetentionRequest<RetentionPayload>[] = [];
  const retentionJobs: CapturedRetentionRequest<RetentionJobPayload>[] = [];

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
      return fulfillJson({ auth_enabled: false, username: null, has_password: false, email: null, pending_email: null, email_bound_at: null, email_verification_required: false });
    }
    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: null, effective_timezone: "UTC" });
    }
    if (pathname === "/api/settings/log-retention" && request.method() === "GET") {
      return fulfillJson(retentionSettings);
    }
    if (pathname === "/api/settings/log-retention" && request.method() === "PUT") {
      retentionSettings = request.postDataJSON() as RetentionPayload;
      retentionUpdates.push(captureRequest(request, pathname, retentionSettings));
      return fulfillJson(retentionSettings);
    }
    if (pathname === "/api/maintenance/log-retention/jobs" && request.method() === "POST") {
      const payload = request.postDataJSON() as RetentionJobPayload;
      retentionJobs.push(captureRequest(request, pathname, payload));
      if (failNextRetentionJob) {
        failNextRetentionJob = false;
        return fulfillJson({ detail: "Cleanup queue unavailable" }, 500);
      }

      return fulfillJson(
        {
          job_id: "job_retention_1",
          state: "queued",
          status_url: "/api/management/jobs/job_retention_1",
          scope: {
            table: payload.table,
            cutoff: payload.cutoff ?? null,
            delete_all: payload.delete_all ?? false,
          },
        },
        202,
      );
    }
    if (pathname === "/api/models") {
      return fulfillJson([]);
    }
    if (pathname === "/api/vendors") {
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

  return { retentionJobs, retentionUpdates };
}

function captureRequest<TPayload>(request: Request, pathname: string, body: TPayload): CapturedRetentionRequest<TPayload> {
  return { method: request.method(), pathname, body };
}
async function selectCardOption(page: Page, label: string, option: string) {
  await page.getByText(label).locator("..").getByRole("combobox").click();
  await page.getByRole("option", { name: option }).click();
}

async function openSettingsGlobalRetention(page: Page) {
  await page.addInitScript(() => window.localStorage.setItem("prism.locale", "en"));
  await page.goto("/settings");
  await page.getByRole("tab", { name: "Global" }).click();
  await expect(page.getByText("Changes here apply to all profiles and the entire Prism instance.")).toBeVisible();
  await expect(page.getByText("Set instance-wide log retention for all profiles")).toBeVisible();
  await expect(page.getByText("retained across every profile before cleanup jobs apply")).toBeVisible();
  await expect(page.getByText("Cleanup jobs apply across all profiles")).toBeVisible();
  await expect(page.getByText("Load-balance event retention")).toBeVisible();
}
test("global retention settings save all log tables through the global endpoint", async ({ page }) => {
  const { retentionUpdates } = await mockSettingsRoutes(page);

  await openSettingsGlobalRetention(page);
  await selectCardOption(page, "Request log retention", "7 days");
  await selectCardOption(page, "Statistics retention", "365 days");
  await selectCardOption(page, "Audit log retention", "Keep forever");
  await selectCardOption(page, "Load-balance event retention", "90 days");
  await page.getByRole("button", { name: "Save retention" }).click();

  await expect.poll(() => retentionUpdates.length).toBe(1);
  expect(retentionUpdates[0]).toEqual({
    method: "PUT",
    pathname: "/api/settings/log-retention",
    body: {
      request_logs_retention_days: 7,
      statistics_retention_days: 365,
      audit_logs_retention_days: null,
      loadbalance_events_retention_days: 90,
    },
  });
  await expect(page.getByText("Retention settings updated")).toBeVisible();
  await page.screenshot({ path: saveEvidencePath, fullPage: true });
});

test("global retention cleanup creates a job and surfaces job tracking details", async ({ page }) => {
  const { retentionJobs } = await mockSettingsRoutes(page);

  await openSettingsGlobalRetention(page);
  await selectCardOption(page, "Data type", "Request Logs");
  await selectCardOption(page, "Delete data older than", "30 days");
  await page.getByRole("button", { name: "Delete data" }).click();
  await expect(page.getByText("This creates an instance-wide cleanup job")).toBeVisible();
  await page.getByLabel("Type DELETE to proceed").fill("DELETE");
  await page.getByRole("button", { name: "Delete" }).click();

  await expect.poll(() => retentionJobs.length).toBe(1);
  expect(retentionJobs[0]).toMatchObject({
    method: "POST",
    pathname: "/api/maintenance/log-retention/jobs",
    body: {
      table: "request_logs",
      delete_all: false,
      reason: "manual_ui_cleanup",
    },
  });
  expect(retentionJobs[0].body.cutoff).toEqual(expect.any(String));
  await expect(page.getByText("Request Logs cleanup job job_retention_1 created")).toBeVisible();
  await expect(page.getByText("Track it at /api/management/jobs/job_retention_1")).toBeVisible();
  await expect(page.getByRole("dialog", { name: "Confirm Deletion" })).toBeHidden();
});
test("global retention cleanup failure keeps confirmation recoverable", async ({ page }) => {
  const { retentionJobs } = await mockSettingsRoutes(page, { failNextRetentionJob: true });

  await openSettingsGlobalRetention(page);
  await selectCardOption(page, "Data type", "Audit Logs");
  await selectCardOption(page, "Delete data older than", "7 days");
  await page.getByRole("button", { name: "Delete data" }).click();
  await page.getByLabel("Type DELETE to proceed").fill("DELETE");
  await page.getByRole("button", { name: "Delete" }).click();

  await expect.poll(() => retentionJobs.length).toBe(1);
  expect(retentionJobs[0]).toMatchObject({
    method: "POST",
    pathname: "/api/maintenance/log-retention/jobs",
    body: {
      table: "audit_logs",
      delete_all: false,
      reason: "manual_ui_cleanup",
    },
  });
  await expect(page.getByText("Deletion failed")).toBeVisible();
  await expect(page.getByRole("dialog", { name: "Confirm Deletion" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Delete" })).toBeEnabled();
  await page.screenshot({ path: deleteFailureEvidencePath, fullPage: true });

  await page.getByRole("button", { name: "Delete" }).click();
  await expect.poll(() => retentionJobs.length).toBe(2);
  await expect(page.getByText("Audit Logs cleanup job job_retention_1 created")).toBeVisible();
  await expect(page.getByRole("dialog", { name: "Confirm Deletion" })).toBeHidden();
});
