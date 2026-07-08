import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");
const hookPath = path.join(
  frontendDir,
  "src/pages/dashboard/useDashboardBootstrapData.ts",
);

function createReactHarness() {
  const states = [];
  const refs = [];
  let stateIndex = 0;
  let refIndex = 0;

  return {
    react: {
      useCallback: (callback) => callback,
      useEffect: () => undefined,
      useRef: (initialValue) => {
        const index = refIndex;
        refIndex += 1;
        if (!Object.prototype.hasOwnProperty.call(refs, index)) {
          refs[index] = { current: initialValue };
        }
        return refs[index];
      },
      useState: (initialValue) => {
        const index = stateIndex;
        stateIndex += 1;
        if (!Object.prototype.hasOwnProperty.call(states, index)) {
          states[index] = typeof initialValue === "function" ? initialValue() : initialValue;
        }
        return [
          states[index],
          (nextValue) => {
            states[index] = typeof nextValue === "function" ? nextValue(states[index]) : nextValue;
          },
        ];
      },
    },
    refs,
    resetRender: () => {
      stateIndex = 0;
      refIndex = 0;
    },
    states,
  };
}

function loadBootstrapModule({ api, reactHarness = createReactHarness() } = {}) {
  const { load } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      react: reactHarness.react,
      "@/lib/api": { api: api ?? { stats: {} } },
    },
  });
  return {
    harness: reactHarness,
    module: load(hookPath),
  };
}

function snapshot(revision, extra = {}) {
  return {
    generated_at: `2026-05-04T00:00:${revision}Z`,
    snapshot_revision: revision,
    source_watermark: {
      latest_usage_event_created_at: null,
      latest_usage_event_id: null,
    },
    ...extra,
  };
}
function activityItem(requestLogId) {
  return {
    request_log_id: requestLogId,
    created_at: `2026-05-04T00:00:${requestLogId}Z`,
    model_id: `model-${requestLogId}`,
    model_label: `Model ${requestLogId}`,
    resolved_target_model_id: null,
    resolved_target_model_label: null,
    endpoint_id: null,
    endpoint_label: "Endpoint",
    status_code: 200,
    response_time_ms: 123,
    ttft_ms: null,
    completion_duration_ms: null,
    is_stream: false,
    stream_outcome: "not_streaming",
    total_tokens: null,
    total_cost_user_currency_micros: null,
    priced_flag: null,
    unpriced_reason: null,
    report_currency_symbol: null,
  };
}

function recentActivity(items) {
  return {
    generated_at: "2026-05-04T00:00:00Z",
    activity_watermark: {
      latest_request_log_created_at: items[0]?.created_at ?? null,
      latest_request_log_id: items[0]?.request_log_id ?? null,
    },
    items,
  };
}

function incidents() {
  return {
    active_bans: [],
    recent_events: [],
    generated_at: "2026-05-04T00:00:00Z",
  };
}

test("dashboard bootstrap production code keeps snapshot revision and recent activity hooks", () => {
  const source = readFileSync(hookPath, "utf8");
  assert.match(source, /shouldApplyDashboardSnapshotRevision/);
  assert.match(source, /snapshot_revision/);
  assert.match(source, /DashboardRecentActivityWatermark/);
  assert.match(source, /dashboardRecentActivity/);
});

test("snapshot reconciliation accepts only lexicographically newer revisions", () => {
  const { harness, module } = loadBootstrapModule();
  const hook = module.useDashboardBootstrapData({ revision: 1, selectedProfileId: 7 });

  assert.equal(module.shouldApplyDashboardSnapshotRevision(null, "01"), true);
  assert.equal(module.shouldApplyDashboardSnapshotRevision("01", "02"), true);
  assert.equal(module.shouldApplyDashboardSnapshotRevision("02", "02"), false);
  assert.equal(module.shouldApplyDashboardSnapshotRevision("02", "01"), false);

  assert.equal(hook.reconcileDashboardSnapshot(snapshot("02")), true);
  assert.equal(harness.states[1].snapshot_revision, "02");
  assert.equal(
    hook.reconcileDashboardSnapshot(
      snapshot("02", { generated_at: "2099-01-01T00:00:00Z" }),
    ),
    false,
  );
  assert.equal(
    hook.reconcileDashboardSnapshot(
      snapshot("01", {
        source_watermark: {
          latest_usage_event_created_at: "2099-01-01T00:00:00Z",
          latest_usage_event_id: 9999,
        },
      }),
    ),
    false,
  );
  assert.equal(harness.states[1].snapshot_revision, "02");
});

test("bootstrap fetches snapshot and recent activity through separate typed API calls", async () => {
  const calls = [];
  const expectedSnapshot = snapshot("03");
  const expectedActivity = recentActivity([activityItem(101)]);
  const expectedIncidents = incidents();
  const api = {
    loadbalance: {
      listIncidents: async (params) => {
        calls.push(["listIncidents", params]);
        return expectedIncidents;
      },
    },
    stats: {
      dashboard: async () => {
        calls.push(["dashboard"]);
        return expectedSnapshot;
      },
      dashboardRecentActivity: async (params) => {
        calls.push(["dashboardRecentActivity", params]);
        return expectedActivity;
      },
    },
  };
  const { harness, module } = loadBootstrapModule({ api });
  const hook = module.useDashboardBootstrapData({ revision: 1, selectedProfileId: 7 });

  const result = await hook.fetchDashboardData({ reuseInFlight: true });

  assert.deepEqual(calls, [
    ["dashboard"],
    ["dashboardRecentActivity", { limit: 12 }],
    ["listIncidents", { limit: 10, since_hours: 24 }],
  ]);
  assert.deepEqual(result, { newRecentActivityIds: [101], recentActivityApplied: true, snapshotApplied: true });
  assert.equal(harness.states[1], expectedSnapshot);
  assert.equal(harness.states[2], expectedActivity);
  assert.equal(harness.states[3], expectedIncidents);
});

test("recent activity merge dedupes by request-log ID without changing snapshot freshness", () => {
  const { module } = loadBootstrapModule();
  const current = recentActivity([activityItem(101)]);
  const nextItem = activityItem(102);
  const watermark = {
    latest_request_log_created_at: nextItem.created_at,
    latest_request_log_id: nextItem.request_log_id,
  };

  const unchanged = module.mergeDashboardRecentActivityItem(current, activityItem(101), watermark);
  assert.equal(unchanged, current);

  const merged = module.mergeDashboardRecentActivityItem(current, nextItem, watermark);
  assert.deepEqual(
    merged.items.map((item) => item.request_log_id),
    [102, 101],
  );
  assert.deepEqual(merged.activity_watermark, watermark);
});
