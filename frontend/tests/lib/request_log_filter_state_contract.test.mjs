import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { DEFAULTS, parsePageState, stateToParams } = load(
  path.join(frontendDir, "src/pages/request-logs/queryParams.ts"),
);
test("request-log filter state defaults to 24h and preserves exact status/error filters", () => {
  assert.equal(DEFAULTS.time_range, "24h");

  const state = parsePageState(new URLSearchParams("status=success&status_code=429&error_text=timeout"));

  assert.equal(state.status_family, "2xx");
  assert.equal(state.status_code, "429");
  assert.equal(state.error_text, "timeout");

  const params = stateToParams(state);
  assert.equal(params.get("status"), "success");
  assert.equal(params.get("status_code"), "429");
  assert.equal(params.get("error_text"), "timeout");
  assert.equal(params.has("time_range"), false);
});
test("request-log filter state round-trips caller client and final target model params", () => {
  const state = parsePageState(
    new URLSearchParams(
      "client_rule_id=123&resolved_target_model_id=terminal-model&request_id=101&selected_request_id=202",
    ),
  );

  assert.equal(state.client_rule_id, "123");
  assert.equal(state.resolved_target_model_id, "terminal-model");
  assert.equal(state.request_id, "101");
  assert.equal(state.selected_request_id, "202");

  const params = stateToParams(state);
  assert.equal(params.get("client_rule_id"), "123");
  assert.equal(params.get("resolved_target_model_id"), "terminal-model");
  assert.equal(params.get("request_id"), "101");
  assert.equal(params.get("selected_request_id"), "202");
  assert.equal(params.has("clientRuleId"), false);
});
test("request-log filter state round-trips pricing filters", () => {
  const state = parsePageState(
    new URLSearchParams("pricing_status=unpriced&unpriced_reason=MISSING_PRICE_DATA"),
  );

  assert.equal(state.pricing_status, "unpriced");
  assert.equal(state.unpriced_reason, "MISSING_PRICE_DATA");

  const params = stateToParams(state);
  assert.equal(params.get("pricing_status"), "unpriced");
  assert.equal(params.get("unpriced_reason"), "MISSING_PRICE_DATA");
});
test("request-log filter state omits empty browse filters but keeps exact anchors", () => {
  const params = stateToParams({
    ingress_request_id: "",
    model_id: "",
    endpoint_id: "",
    client_rule_id: "",
    resolved_target_model_id: "",
    status_code: "",
    error_text: "",
    pricing_status: "all",
    unpriced_reason: "",
    time_range: "24h",
    status_family: "all",
    limit: 100,
    offset: 0,
    request_id: "303",
    selected_request_id: "404",
  });

  assert.equal(params.has("client_rule_id"), false);
  assert.equal(params.has("resolved_target_model_id"), false);
  assert.equal(params.get("request_id"), "303");
  assert.equal(params.get("selected_request_id"), "404");
});
