import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

function loadApi() {
  const { load } = createTsModuleLoader({ rootDir: frontendDir });
  return load(path.join(frontendDir, "src/lib/api.ts"));
}

function installFetchRecorder(requests, responses) {
  const originalFetch = globalThis.fetch;
  void normalizeFetchInit;
  globalThis.fetch = async (url, init) => {
    requests.push({
        url: String(url),
        init: normalizeFetchInit(init),
      });
    const body = responses.shift() ?? {};
    return {
      ok: true,
      status: 200,
      text: async () => JSON.stringify(body),
    };
  };

  return () => {
    globalThis.fetch = originalFetch;
  };
}

test("observability API fetches dashboard snapshot and recent activity separately", async () => {
  const requests = [];
  const snapshot = {
    generated_at: "2026-05-04T00:00:00Z",
    snapshot_revision: "01HVVYV9XG0000000000000000",
    source_watermark: {
      latest_usage_event_created_at: null,
      latest_usage_event_id: null,
    },
  };
  const recentActivity = {
    generated_at: "2026-05-04T00:00:01Z",
    activity_watermark: {
      latest_request_log_created_at: "2026-05-04T00:00:01Z",
      latest_request_log_id: 101,
    },
    items: [],
  };
  const restoreFetch = installFetchRecorder(requests, [snapshot, recentActivity]);
  const { api, stats } = loadApi();

  try {
    assert.equal(typeof stats.dashboardRecentActivity, "function");
    assert.equal(api.stats.dashboardRecentActivity, stats.dashboardRecentActivity);
    assert.deepEqual(await api.stats.dashboard(), snapshot);
    assert.deepEqual(
      await api.stats.dashboardRecentActivity({ limit: 12 }),
      recentActivity,
    );
  } finally {
    restoreFetch();
  }

  assert.deepEqual(requests, [
    {
      url: "/api/stats/dashboard",
      init: {
        credentials: "include",
        headers: { "Content-Type": "application/json", "X-Profile-Id": "1" },
      },
    },
    {
      url: "/api/stats/dashboard/recent-activity?limit=12",
      init: {
        credentials: "include",
        headers: { "Content-Type": "application/json", "X-Profile-Id": "1" },
      },
    },
  ]);
});

test("settings audit API stays profile-scoped and uses the exact family payload", async () => {
  const requests = [];
  const response = {
    profile_id: 17,
    settings: [
      { api_family: "openai", audit_enabled: true, audit_capture_bodies: false },
      { api_family: "anthropic", audit_enabled: false, audit_capture_bodies: false },
      { api_family: "gemini", audit_enabled: true, audit_capture_bodies: true },
    ],
  };
  const restoreFetch = installFetchRecorder(requests, [response, response]);
  const { api } = loadApi();

  try {
    assert.deepEqual(await api.settings.audit.get(), response);
    assert.deepEqual(
      await api.settings.audit.update({ settings: response.settings }),
      response,
    );
  } finally {
    restoreFetch();
  }

  assert.deepEqual(requests, [
    {
      url: "/api/settings/audit",
      init: {
        credentials: "include",
        headers: { "Content-Type": "application/json", "X-Profile-Id": "1" },
      },
    },
    {
      url: "/api/settings/audit",
      init: {
        body: JSON.stringify({ settings: response.settings }),
        credentials: "include",
        headers: { "Content-Type": "application/json", "X-Profile-Id": "1" },
        method: "PUT",
      },
    },
  ]);
});

test("request detail and audit lookup preserve BIGINT request-log IDs", async () => {
  const requests = [];
  const restoreFetch = installFetchRecorder(requests, [{ summary: {} }, { items: [] }]);
  const { api } = loadApi();
  const requestLogId = "9007199254740997";

  try {
    await api.stats.requestDetail(requestLogId);
    await api.audit.listForRequestLog(requestLogId, {
      from: "2026-08-13T00:00:00.000Z",
      to: "2026-08-14T00:00:00.000Z",
      limit: 20,
      cursor: "signed-cursor",
    });
  } finally {
    restoreFetch();
  }

  assert.equal(requests.length, 2);
  assert.equal(requests[0].url, `/api/stats/requests/${requestLogId}`);
  const auditUrl = new URL(requests[1].url, "https://prism.test");
  assert.equal(auditUrl.pathname, "/api/audit/logs");
  assert.equal(auditUrl.searchParams.get("request_log_id"), requestLogId);
  assert.equal(auditUrl.searchParams.get("from"), "2026-08-13T00:00:00.000Z");
  assert.equal(auditUrl.searchParams.get("to"), "2026-08-14T00:00:00.000Z");
  assert.equal(auditUrl.searchParams.get("limit"), "20");
  assert.equal(auditUrl.searchParams.get("cursor"), "signed-cursor");
});

// The auth session coordinator attaches a live epoch AbortSignal to every
// protected fetch; the route contract is the remaining init surface.
function normalizeFetchInit(init) {
  if (!init) return init;
  const { signal: _signal, ...rest } = init;
  return rest;
}
