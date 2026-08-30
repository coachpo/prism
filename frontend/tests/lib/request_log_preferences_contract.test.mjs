import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

// Minimal localStorage shim for the node test runner.
const storage = new Map();
globalThis.localStorage = {
  getItem: (key) => (storage.has(key) ? storage.get(key) : null),
  setItem: (key, value) => { storage.set(key, String(value)); },
  removeItem: (key) => { storage.delete(key); },
  clear: () => { storage.clear(); },
  key: (index) => [...storage.keys()][index] ?? null,
  get length() { return storage.size; },
};

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { DEFAULT_COLUMN_PREFERENCES, allColumnKeys, loadColumnPreferences, saveColumnPreferences, resetColumnPreferences } = load(
  path.join(frontendDir, "src/pages/request-logs/requestLogColumnPreferences.ts"),
);

test("column preferences default to explicit ingress and attempt target identity", () => {
  localStorage.clear();
  const prefs = loadColumnPreferences();
  assert.equal(prefs.version, 4);
  assert.deepEqual(prefs.visibleKeys, DEFAULT_COLUMN_PREFERENCES.visibleKeys);
  assert.ok(prefs.visibleKeys.includes("pricing_state"));
  assert.ok(prefs.visibleKeys.includes("created_at"));
  assert.ok(prefs.visibleKeys.includes("requested_model"));
  assert.ok(prefs.visibleKeys.includes("attempt_target_model"));
});

test("column preferences persist and ignore unknown keys", () => {
  localStorage.clear();
  saveColumnPreferences({ version: 4, visibleKeys: ["created_at", "pricing_state", "status_code", "not-a-real-column"] });
  const prefs = loadColumnPreferences();
  assert.ok(prefs.visibleKeys.includes("created_at"));
  assert.ok(!prefs.visibleKeys.includes("not-a-real-column"));
});

test("reset column preferences restores the default set", () => {
  localStorage.clear();
  saveColumnPreferences({ version: 4, visibleKeys: ["created_at"] });
  const reset = resetColumnPreferences();
  assert.deepEqual(reset.visibleKeys, DEFAULT_COLUMN_PREFERENCES.visibleKeys);
  assert.equal(loadColumnPreferences().visibleKeys.length, DEFAULT_COLUMN_PREFERENCES.visibleKeys.length);
});

test("all column keys resolve from the column registry", () => {
  const keys = allColumnKeys();
  assert.ok(keys.includes("pricing_state"));
  assert.ok(keys.includes("total_cost"));
  assert.ok(keys.includes("is_stream"));
});
