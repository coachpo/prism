import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");
const hookPath = path.join(frontendDir, "src/pages/dashboard/useDashboardPolling.ts");

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
        return effect();
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
    resetRender: () => {
      stateIndex = 0;
      refIndex = 0;
    },
    states,
  };
}

function loadHook({ reactHarness = createReactHarness(), timers } = {}) {
  const { load } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      react: reactHarness.react,
    },
  });
  const previousWindow = global.window;
  global.window = timers;
  return {
    cleanup: () => {
      global.window = previousWindow;
    },
    harness: reactHarness,
    module: load(hookPath),
  };
}

test("dashboard live hook polls REST silently and highlights new snapshot and request IDs", async () => {
  const intervals = [];
  const cleared = [];
  const timers = {
    setInterval: (callback, delay) => {
      intervals.push({ callback, delay });
      return intervals.length;
    },
    clearInterval: (id) => cleared.push(id),
    setTimeout: () => 99,
    clearTimeout: () => undefined,
  };
  const { cleanup, harness, module } = loadHook({ timers });
  const fetchCalls = [];
  try {
    module.useDashboardPolling({
      fetchDashboardData: async (args) => {
        fetchCalls.push(args);
        return {
          newRecentActivityIds: [101],
          recentActivityApplied: true,
          snapshotApplied: true,
        };
      },
      selectedProfileId: 1,
    });

    assert.equal(intervals.length, 1);
    assert.equal(intervals[0].delay, 30_000);

    await intervals[0].callback();

    assert.deepEqual(fetchCalls, [{ silent: true }]);
    assert.deepEqual([...harness.states[0]], [101]);
    assert.equal(harness.states[2], true);
  } finally {
    cleanup();
  }
});
