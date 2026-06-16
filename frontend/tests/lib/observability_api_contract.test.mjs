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
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init });
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
        headers: { "Content-Type": "application/json" },
      },
    },
    {
      url: "/api/stats/dashboard/recent-activity?limit=12",
      init: {
        credentials: "include",
        headers: { "Content-Type": "application/json" },
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
  const { api, setApiProfileId } = loadApi();

  try {
    setApiProfileId(17);

    assert.deepEqual(await api.settings.audit.get(), response);
    assert.deepEqual(
      await api.settings.audit.update({ settings: response.settings }),
      response,
    );
  } finally {
    setApiProfileId(null);
    restoreFetch();
  }

  assert.deepEqual(requests, [
    {
      url: "/api/settings/audit",
      init: {
        credentials: "include",
        headers: { "Content-Type": "application/json", "X-Profile-Id": "17" },
      },
    },
    {
      url: "/api/settings/audit",
      init: {
        body: JSON.stringify({ settings: response.settings }),
        credentials: "include",
        headers: { "Content-Type": "application/json", "X-Profile-Id": "17" },
        method: "PUT",
      },
    },
  ]);
});
