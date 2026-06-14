import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");
const realtimeHookPath = path.join(
  frontendDir,
  "src/pages/dashboard/useDashboardRealtime.ts",
);

function createReactHarness() {
  const states = [];
  const refs = [];
  const effects = [];
  let stateIndex = 0;
  let refIndex = 0;

  return {
    react: {
      useCallback: (callback) => callback,
      useEffect: (effect) => {
        effects.push(effect);
        effect();
      },
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
    effects,
    refs,
    resetRender: () => {
      stateIndex = 0;
      refIndex = 0;
    },
    states,
  };
}
function loadRealtimeHook({ reactHarness = createReactHarness() } = {}) {
  let realtimeOptions = null;
  const markSyncCalls = [];
  const { load } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      react: reactHarness.react,
      "@/hooks/useRealtimeData": {
        useRealtimeData: (options) => {
          realtimeOptions = options;
          return {
            connectionState: "connected",
            isSyncing: false,
            markSyncComplete: () => markSyncCalls.push("complete"),
          };
        },
      },
    },
  });
  return {
    getRealtimeOptions: () => realtimeOptions,
    harness: reactHarness,
    markSyncCalls,
    module: load(realtimeHookPath),
  };
}

function activityPayload(requestLogId) {
  const activity = {
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

  return {
    type: "dashboard.activity",
    profile_id: 7,
    activity_watermark: {
      latest_request_log_created_at: activity.created_at,
      latest_request_log_id: activity.request_log_id,
    },
    activity,
  };
}

function snapshotPayload(revision) {
  return {
    type: "dashboard.snapshot",
    profile_id: 7,
    snapshot: {
      generated_at: "2026-05-04T00:00:00Z",
      snapshot_revision: revision,
      source_watermark: {
        latest_usage_event_created_at: null,
        latest_usage_event_id: null,
      },
    },
  };
}

test("dashboard activity updates activity state and highlights by request-log ID only", () => {
  const { getRealtimeOptions, harness, module } = loadRealtimeHook();
  const seenActivityIds = new Set();
  const appliedActivityIds = [];
  const hook = module.useDashboardRealtime({
    applyDashboardActivity: (activity) => {
      if (seenActivityIds.has(activity.request_log_id)) {
        return false;
      }
      seenActivityIds.add(activity.request_log_id);
      appliedActivityIds.push(activity.request_log_id);
      return true;
    },
    fetchDashboardData: async () => ({ recentActivityApplied: true, snapshotApplied: false }),
    reconcileDashboardSnapshot: () => {
      throw new Error("activity must not reconcile snapshots");
    },
    selectedProfileId: 7,
    setRoutingDiagramError: () => undefined,
  });
  const options = getRealtimeOptions();

  options.onData(activityPayload(101));
  options.onData(activityPayload(101));

  assert.deepEqual(appliedActivityIds, [101]);
  assert.deepEqual(Array.from(harness.states[0]), [101]);
  assert.equal(hook.connectionState, "connected");
});

test("dashboard snapshot realtime reconciliation highlights only applied newer revisions", () => {
  const { getRealtimeOptions, harness, module } = loadRealtimeHook();
  const routingErrors = [];
  const hook = module.useDashboardRealtime({
    applyDashboardActivity: () => false,
    fetchDashboardData: async () => ({ recentActivityApplied: true, snapshotApplied: false }),
    reconcileDashboardSnapshot: (snapshot) => snapshot.snapshot_revision === "02",
    selectedProfileId: 7,
    setRoutingDiagramError: (value) => routingErrors.push(value),
  });
  const options = getRealtimeOptions();

  options.onData(snapshotPayload("01"));
  assert.equal(harness.states[2], false);
  assert.deepEqual(routingErrors, []);

  options.onData(snapshotPayload("02"));
  assert.equal(harness.states[2], true);
  assert.deepEqual(routingErrors, [null]);
  assert.equal(typeof hook.refreshDashboard, "function");
});

test("manual refresh does not force metric highlight for equal snapshot revision", async () => {
  const { harness, module } = loadRealtimeHook();
  const hook = module.useDashboardRealtime({
    applyDashboardActivity: () => false,
    fetchDashboardData: async () => ({ recentActivityApplied: true, snapshotApplied: false }),
    reconcileDashboardSnapshot: () => false,
    selectedProfileId: 7,
    setRoutingDiagramError: () => undefined,
  });

  await hook.refreshDashboard();

  assert.equal(harness.states[2], false);
});

test("reconnect repair refetches dashboard data and completes sync after both contracts resolve", async () => {
  const { getRealtimeOptions, markSyncCalls, module } = loadRealtimeHook();
  const fetchCalls = [];
  let resolveFetch;
  module.useDashboardRealtime({
    applyDashboardActivity: () => false,
    fetchDashboardData: (args) => {
      fetchCalls.push(args);
      return new Promise((resolve) => {
        resolveFetch = resolve;
      });
    },
    reconcileDashboardSnapshot: () => false,
    selectedProfileId: 7,
    setRoutingDiagramError: () => undefined,
  });
  const options = getRealtimeOptions();

  options.onReconnect();
  assert.deepEqual(fetchCalls, [{ silent: true }]);
  assert.deepEqual(markSyncCalls, []);

  resolveFetch({ recentActivityApplied: true, snapshotApplied: false });
  await Promise.resolve();

  assert.deepEqual(markSyncCalls, ["complete"]);
});
