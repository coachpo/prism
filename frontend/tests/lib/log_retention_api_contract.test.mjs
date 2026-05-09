import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

function loadSettingsRetentionModule(requests) {
  const { load } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      "./core": {
        buildQuery: (params) => new URLSearchParams(params).toString(),
        request: async (url, init) => {
          requests.push({ url, init });
          return { ok: true };
        },
      },
    },
  });

  return load(path.join(frontendDir, "src/lib/api/observability.ts")).settingsRetention;
}

test("settings retention API client uses the global retention endpoints", async () => {
  const requests = [];
  const settingsRetention = loadSettingsRetentionModule(requests);
  const update = {
    request_logs_retention_days: 7,
    statistics_retention_days: 365,
    audit_logs_retention_days: null,
    loadbalance_events_retention_days: 90,
  };
  const job = {
    table: "request_logs",
    cutoff: "2026-05-09T12:00:00.000Z",
    delete_all: false,
    reason: "manual_ui_cleanup",
  };

  await settingsRetention.get();
  await settingsRetention.update(update);
  await settingsRetention.createJob(job);

  assert.deepEqual(requests, [
    { url: "/api/settings/log-retention", init: undefined },
    {
      url: "/api/settings/log-retention",
      init: {
        method: "PUT",
        body: JSON.stringify(update),
      },
    },
    {
      url: "/api/maintenance/log-retention/jobs",
      init: {
        method: "POST",
        body: JSON.stringify(job),
      },
    },
  ]);
});
