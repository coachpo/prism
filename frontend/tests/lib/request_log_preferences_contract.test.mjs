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
const { loadSavedViews, saveRequestLogView, deleteRequestLogView, applySavedView, savedViewStateOf } = load(
  path.join(frontendDir, "src/pages/request-logs/requestLogSavedViews.ts"),
);
const { DEFAULT_COLUMN_PREFERENCES, allColumnKeys, loadColumnPreferences, saveColumnPreferences, resetColumnPreferences } = load(
  path.join(frontendDir, "src/pages/request-logs/requestLogColumnPreferences.ts"),
);

function makeState(overrides = {}) {
  return {
    ingress_request_id: "",
    model_id: "",
    endpoint_id: "",
    client_rule_id: "",
    resolved_target_model_id: "",
    status_code: "",
    error_text: "",
    pricing_status: "all",
    unpriced_reason: "",
    pricing_card_role: "",
    pricing_selection_state: "",
    time_range: "24h",
    from_time: "",
    to_time: "",
    observe_return: "",
    query_context: "",
    final_result: "",
    final_target_model_id: "",
    final_endpoint_id: "",
    final_terminal_target_id: "",
    final_pricing_status: "",
    final_unpriced_reason: "",
    status_family: "all",
    limit: 100,
    offset: 0,
    request_id: "",
    selected_request_id: "",
    view: "ingress_chains",
    sort_by: "created_at",
    sort_order: "desc",
    chain_cursor: "",
    ingress_final_result: "",
    confirmed_failover: false,
    ...overrides,
  };
}

test("saved views round-trip through localStorage with versioned schema", () => {
  localStorage.clear();
  const state = makeState({ model_id: "gpt-5.6", pricing_status: "unpriced", unpriced_reason: "MISSING_PRICE_DATA", time_range: "7d", view: "attempts" });
  const view = saveRequestLogView("unpriced gpt-5.6", state);
  assert.equal(view.name, "unpriced gpt-5.6");
  assert.ok(view.id.length > 0);
  assert.ok(view.createdAt.length > 0);

  const loaded = loadSavedViews();
  assert.equal(loaded.length, 1);
  assert.equal(loaded[0].state.model_id, "gpt-5.6");
  assert.equal(loaded[0].state.pricing_status, "unpriced");
});

test("saved views omit transient pagination and selection anchors", () => {
  const state = makeState({ offset: 300, chain_cursor: "cursor-1", request_id: "42", selected_request_id: "43" });
  const stateOnly = savedViewStateOf(state);
  assert.equal("offset" in stateOnly, false);
  assert.equal("chain_cursor" in stateOnly, false);
  assert.equal("request_id" in stateOnly, false);
  assert.equal("selected_request_id" in stateOnly, false);
});

test("applying a saved view resets pagination and preserves nothing transient", () => {
  const view = saveRequestLogView("failed view", makeState({ ingress_final_result: "failed", confirmed_failover: true }));
  const applied = applySavedView(view, makeState({ offset: 300, request_id: "99" }));
  assert.equal(applied.ingress_final_result, "failed");
  assert.equal(applied.confirmed_failover, true);
  assert.equal(applied.offset, 0);
  assert.equal(applied.chain_cursor, "");
  assert.equal(applied.request_id, "");
  assert.equal(applied.selected_request_id, "");
});

test("deleting a saved view removes it", () => {
  localStorage.clear();
  const first = saveRequestLogView("first", makeState());
  const second = saveRequestLogView("second", makeState());
  assert.equal(loadSavedViews().length, 2);
  deleteRequestLogView(first.id);
  const remaining = loadSavedViews();
  assert.equal(remaining.length, 1);
  assert.equal(remaining[0].id, second.id);
});

test("saving the same view name updates in place", () => {
  localStorage.clear();
  saveRequestLogView("same-name", makeState({ model_id: "a" }));
  saveRequestLogView("same-name", makeState({ model_id: "b" }));
  const views = loadSavedViews();
  assert.equal(views.length, 1);
  assert.equal(views[0].state.model_id, "b");
});

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

test("sortable column keys map to backend sort_by values", async () => {
  // The table offers server-driven sorting on the five attempt-view keys.
  const sortable = new Map([
    ["created_at", "created_at"],
    ["status_code", "display_status"],
    ["ttft_ms", "ttft_ms"],
    ["total_tokens", "total_tokens"],
    ["total_cost", "total_cost_user_currency_micros"],
  ]);
  assert.equal(sortable.size, 5);
  for (const [column, backend] of sortable) {
    assert.ok(column.length > 0);
    assert.ok(backend.length > 0);
  }
});
