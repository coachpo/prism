import { mkdir, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { expect, test, type Page } from "@playwright/test";
import {
  createDashboardRecentActivityResponse,
  createDashboardSnapshot,
  createEmptyDashboardSnapshot,
  dashboardAggregateTimestamp,
  legacyOverviewFanOutPaths,
} from "./dashboard-aggregate-fixtures";

const evidenceDirectory = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
  "artifacts",
  "evidence",
);
const networkEvidencePath = resolve(evidenceDirectory, "task-8-overview-network.json");
const emptyConsoleEvidencePath = resolve(evidenceDirectory, "task-8-overview-empty-console.json");
const emptyScreenshotPath = resolve(evidenceDirectory, "task-8-overview-empty.png");
const task9OverviewScreenshotPath = resolve(evidenceDirectory, "task-9-overview.png");
const task9EmptyActivityScreenshotPath = resolve(evidenceDirectory, "task-9-empty-activity.png");
const routeReadyTimeout = 15_000;

type ApiRequestRecord = {
  method: string;
  pathname: string;
  search: string;
  status: number;
};

function createProfile(id: number, name: string, isActive = false) {
  return {
    id,
    name,
    description: null,
    is_active: isActive,
    is_default: id === 1,
    is_editable: true,
    version: 1,
    created_at: dashboardAggregateTimestamp,
    deleted_at: null,
    updated_at: dashboardAggregateTimestamp,
  };
}

async function writeJsonEvidence(path: string, value: unknown) {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

function isLegacyOverviewFanOut(pathname: string) {
  return legacyOverviewFanOutPaths.includes(pathname);
}
async function mockAggregateOverviewRoutes(
  page: Page,
  options: {
    emptyProfileId?: number;
    emptyProfileName?: string;
    recordRequests?: ApiRequestRecord[];
    slowDashboardResponse?: boolean;
  } = {},
) {
  const emptyProfile = options.emptyProfileId
    ? createProfile(options.emptyProfileId, options.emptyProfileName ?? "Task 8 Empty Profile", true)
    : null;
  let emptyProfileDeleted = false;

  await page.addInitScript(
    ({ selectedProfileId }) => {
      localStorage.setItem("prism.locale", "en");
      if (selectedProfileId) {
        localStorage.setItem("prism.selectedProfileId", String(selectedProfileId));
      }
    },
    { selectedProfileId: options.emptyProfileId ?? null },
  );

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname, search } = url;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) => {
      options.recordRequests?.push({
        method: request.method(),
        pathname,
        search,
        status,
      });
      return route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    };

    if (pathname === "/api/auth/status") {
      return fulfillJson({ auth_enabled: false });
    }

    if (pathname === "/api/settings/costing") {
      return fulfillJson({
        report_currency_code: "USD",
        report_currency_symbol: "$",
        endpoint_fx_mappings: [],
        timezone_preference: null,
      });
    }

    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }

    if (pathname === "/api/stats/dashboard") {
      if (options.slowDashboardResponse) {
        await new Promise((resolveDelay) => setTimeout(resolveDelay, 100));
      }
      return fulfillJson(
        emptyProfile && !emptyProfileDeleted
          ? createEmptyDashboardSnapshot()
          : createDashboardSnapshot(),
      );
    }

    if (pathname === "/api/stats/dashboard/recent-activity") {
      return fulfillJson(
        emptyProfile && !emptyProfileDeleted
          ? createDashboardRecentActivityResponse([])
          : createDashboardRecentActivityResponse(),
      );
    }

    if (isLegacyOverviewFanOut(pathname)) {
      return fulfillJson({ error: "legacy overview fan-out is forbidden" }, 500);
    }

    return fulfillJson({}, 404);
  });

  return {
    cleanupEmptyProfile: () => {
      emptyProfileDeleted = true;
      return Boolean(emptyProfile);
    },
    get emptyProfileDeleted() {
      return emptyProfileDeleted;
    },
  };
}

test.describe("dashboard aggregate overview regression", () => {
  test("canonicalizes an invalid dashboard tab back to overview", async ({ page }) => {
    await mockAggregateOverviewRoutes(page);

    await page.goto("/observe?tab=bogus");

    await expect(page).toHaveURL(/\/observe\?tab=overview$/);
    await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible({ timeout: routeReadyTimeout });
    await expect(page.getByRole("tab")).toHaveText(["Overview", "Analytics", "Routing"]);
    await expect(page.getByRole("tab", { name: "Overview" })).toHaveAttribute("aria-selected", "true");
  });

  test("loads overview with one aggregate request and no legacy fan-out", async ({ page }) => {
    const requests: ApiRequestRecord[] = [];

    await mockAggregateOverviewRoutes(page, {
      recordRequests: requests,
      slowDashboardResponse: true,
    });

    await page.goto("/observe?tab=overview");

    await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: routeReadyTimeout });
    await expect(page.getByText("Loading application...")).toHaveCount(0, { timeout: routeReadyTimeout });
    await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible({ timeout: routeReadyTimeout });
    await expect(page.getByRole("tab")).toHaveText(["Overview", "Analytics", "Routing"]);
    await expect(page.getByRole("tab", { name: "Overview" })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByTestId("routing-diagram-card")).toHaveCount(0);
    await expect(page.getByText("Routing Target Health")).toHaveCount(0);
    await expect(page.getByText("Top Models by Spend")).toBeVisible();
    await expect(page.getByText("Model A Spend Label")).toBeVisible();
    await expect(page.locator('[data-slot="card"]').filter({ hasText: "Recent Activity" }).first()).toContainText("Model A");

    await mkdir(dirname(task9OverviewScreenshotPath), { recursive: true });
    await page.screenshot({ path: task9OverviewScreenshotPath, fullPage: true });

    await expect
      .poll(() => requests.filter((request) => request.pathname === "/api/stats/dashboard").length)
      .toBe(1);
    await expect
      .poll(() => requests.filter((request) => request.pathname === "/api/stats/dashboard/recent-activity").length)
      .toBe(1);

    const aggregateRequests = requests.filter((request) => request.pathname === "/api/stats/dashboard");
    const activityRequests = requests.filter((request) => request.pathname === "/api/stats/dashboard/recent-activity");
    const legacyRequests = requests.filter((request) => isLegacyOverviewFanOut(request.pathname));

    expect(aggregateRequests).toHaveLength(1);
    expect(activityRequests).toHaveLength(1);
    expect(legacyRequests).toEqual([]);

    await writeJsonEvidence(networkEvidencePath, {
      scenario: "dashboard-overview-aggregate-bootstrap",
      route: "/observe?tab=overview",
      aggregateRequestCount: aggregateRequests.length,
      aggregateRequests,
      activityRequestCount: activityRequests.length,
      activityRequests,
      task9OverviewScreenshotPath,
      legacyOverviewFanOutPaths,
      legacyRequestCount: legacyRequests.length,
      legacyRequests,
      allApiRequests: requests,
    });
  });

  test("renders the empty overview without console errors", async ({ page }) => {
    const consoleErrors: string[] = [];
    const consoleWarnings: string[] = [];
    const emptyProfileId = 808;
    const emptyProfileName = "Task 8 Empty Profile";

    page.on("console", (message) => {
      if (message.type() === "error") {
        consoleErrors.push(message.text());
      }
      if (message.type() === "warning") {
        consoleWarnings.push(message.text());
      }
    });
    page.on("pageerror", (error) => consoleErrors.push(error.message));

    const routeState = await mockAggregateOverviewRoutes(page, {
      emptyProfileId,
      emptyProfileName,
    });

    await page.goto("/observe?tab=overview");

    await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();
    await expect(page.getByRole("tab")).toHaveText(["Overview", "Analytics", "Routing"]);
    await expect(page.getByRole("tab", { name: "Overview" })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByTestId("routing-diagram-card")).toHaveCount(0);
    await expect(page.getByText("No active routes")).toHaveCount(0);
    await expect(page.getByText("No recent activity")).toBeVisible();
    await expect(page.getByText("No spending data")).toBeVisible();
    await expect(page.getByText("0 total requests")).toBeVisible();
    expect(consoleErrors).toEqual([]);

    await mkdir(dirname(emptyScreenshotPath), { recursive: true });
    await page.screenshot({ path: emptyScreenshotPath, fullPage: true });
    await page.screenshot({ path: task9EmptyActivityScreenshotPath, fullPage: true });
    const cleanupCompleted = routeState.cleanupEmptyProfile();

    await writeJsonEvidence(emptyConsoleEvidencePath, {
      scenario: "dashboard-overview-empty-state",
      route: "/observe?tab=overview",
      profile: {
        id: emptyProfileId,
        name: emptyProfileName,
        createdInRouteState: true,
        cleanupCompleted,
        deletedInRouteState: routeState.emptyProfileDeleted,
      },
      consoleErrors,
      consoleWarnings,
      screenshotPath: emptyScreenshotPath,
      task9EmptyActivityScreenshotPath,
      visibleEmptyStates: [
        "No recent activity",
        "No spending data",
        "0 total requests",
      ],
    });
  });
});
