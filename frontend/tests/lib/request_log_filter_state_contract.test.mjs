import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const {
  DEFAULTS,
  TOKEN_BOUND_REQUEST_FILTER_DEFAULTS,
  parsePageState,
  requestLogStateForView,
  stateToParams,
} = load(
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
test("request-log filter state round-trips caller client and attempt target model params", () => {
  const state = parsePageState(
    new URLSearchParams(
      "client_rule_id=123&ingress_model_id=entry-model&attempt_target_model_id=terminal-model&final_target_model_id=winner-model&request_id=101&selected_request_id=202",
    ),
  );

  assert.equal(state.client_rule_id, "123");
  assert.equal(state.model_id, "entry-model");
  assert.equal(state.resolved_target_model_id, "terminal-model");
  assert.equal(state.final_target_model_id, "winner-model");
  assert.equal(state.request_id, "101");
  assert.equal(state.selected_request_id, "202");

  const params = stateToParams(state);
  assert.equal(params.get("client_rule_id"), "123");
  assert.equal(params.get("ingress_model_id"), "entry-model");
  assert.equal(params.get("attempt_target_model_id"), "terminal-model");
  assert.equal(params.get("final_target_model_id"), "winner-model");
  assert.equal(params.get("request_id"), "101");
  assert.equal(params.get("selected_request_id"), "202");
  assert.equal(params.has("clientRuleId"), false);
});
test("request-log filter state round-trips pricing filters", () => {
  const state = parsePageState(
    new URLSearchParams("pricing_status=unpriced&unpriced_reason=MISSING_PRICE_DATA&pricing_card_role=peak&pricing_selection_state=selected"),
  );

  assert.equal(state.pricing_status, "unpriced");
  assert.equal(state.unpriced_reason, "MISSING_PRICE_DATA");
  assert.equal(state.pricing_card_role, "peak");
  assert.equal(state.pricing_selection_state, "selected");

  const params = stateToParams(state);
  assert.equal(params.get("pricing_status"), "unpriced");
  assert.equal(params.get("unpriced_reason"), "MISSING_PRICE_DATA");
  assert.equal(params.get("pricing_card_role"), "peak");
  assert.equal(params.get("pricing_selection_state"), "selected");
});
test("request-log filter state canonicalizes every signed Observe selector", () => {
  const source = new URLSearchParams();
  source.set("view", "attempts");
  source.set("query_context", "signed-context");
  source.append("final_result", "failed");
  source.append("final_result", "client_disconnected");
  source.set("outcome_detail", "http_error,stream_error");
  source.set("final_status_code", "429,503");
  source.set("final_stream_outcome", "stream_error,client_disconnected");
  source.set("final_stream_error_kind", "__null__,protocol_error");
  source.set("final_target_model_id", "winner-a,__null__");
  source.set("final_endpoint_id", "7,__null__,9");
  source.set("final_terminal_target_id", "11,12,__null__");
  source.set("final_pricing_status", "unpriced");
  source.set(
    "final_unpriced_reason",
    "MISSING_PRICE_DATA,STREAM_USAGE_UNAVAILABLE",
  );
  source.set("reporting_currency_epoch", "3");
  source.set("cost_segment_key", "e.3");
  source.set("api_family", "openai,__null__");
  source.set("row_kind", "upstream");
  source.set("attempt_trigger", "initial,failover,__null__");
  source.set("attempt_result", "http_error,transport_error,__null__");

  const state = parsePageState(source);
  assert.equal(state.final_result, "failed,client_disconnected");
  assert.equal(state.final_endpoint_id, "7,__null__,9");
  assert.equal(state.api_family, "openai,__null__");
  assert.equal(state.row_kind, "upstream");
  assert.equal(state.attempt_trigger, "initial,failover,__null__");
  assert.equal(state.attempt_result, "http_error,transport_error,__null__");

  const canonical = stateToParams(state);
  for (const key of [
    "query_context",
    "final_result",
    "outcome_detail",
    "final_status_code",
    "final_stream_outcome",
    "final_stream_error_kind",
    "final_target_model_id",
    "final_endpoint_id",
    "final_terminal_target_id",
    "final_pricing_status",
    "final_unpriced_reason",
    "reporting_currency_epoch",
    "cost_segment_key",
    "api_family",
    "row_kind",
    "attempt_trigger",
    "attempt_result",
  ]) {
    assert.equal(canonical.has(key), true, key);
  }
  assert.equal(canonical.get("final_result"), "failed,client_disconnected");
  assert.equal(canonical.get("final_endpoint_id"), "7,__null__,9");
});
test("request-log URL state drops pagination keys owned by the other view", () => {
  const chains = parsePageState(
    new URLSearchParams(
      "view=ingress_chains&limit=300&cursor=300&sort_by=ttft_ms&chain_cursor=chain-2",
    ),
  );
  assert.equal(chains.limit, 100);
  assert.equal(chains.offset, 0);
  assert.equal(chains.sort_by, "created_at");
  assert.equal(chains.chain_cursor, "chain-2");
  const chainParams = stateToParams(chains);
  assert.equal(chainParams.has("limit"), false);
  assert.equal(chainParams.has("cursor"), false);
  assert.equal(chainParams.has("sort_by"), false);
  assert.equal(chainParams.get("chain_cursor"), "chain-2");

  const attempts = parsePageState(
    new URLSearchParams(
      "view=attempts&limit=300&cursor=300&sort_by=ttft_ms&chain_cursor=chain-2",
    ),
  );
  assert.equal(attempts.limit, 300);
  assert.equal(attempts.offset, 300);
  assert.equal(attempts.sort_by, "ttft_ms");
  assert.equal(attempts.chain_cursor, "");
  const attemptParams = stateToParams(attempts);
  assert.equal(attemptParams.get("limit"), "300");
  assert.equal(attemptParams.get("cursor"), "300");
  assert.equal(attemptParams.get("sort_by"), "ttft_ms");
  assert.equal(attemptParams.has("chain_cursor"), false);
});
test("switching an Observe attempts deep link clears chain-incompatible state", () => {
  const attempts = parsePageState(
    new URLSearchParams(
      "view=attempts&query_context=signed-context&final_result=failed&final_endpoint_id=7%2C__null__&api_family=openai&row_kind=upstream&attempt_trigger=failover&attempt_result=http_error&status_code=503&cost_segment_key=e.3&cursor=300",
    ),
  );
  const chains = requestLogStateForView(attempts, "ingress_chains");

  for (const key of Object.keys(TOKEN_BOUND_REQUEST_FILTER_DEFAULTS)) {
    assert.equal(chains[key], "", key);
  }
  assert.equal(chains.api_family, "");
  assert.equal(chains.row_kind, "");
  assert.equal(chains.status_code, "503");
  assert.equal(chains.cost_segment_key, "e.3");
  assert.equal(chains.view, "ingress_chains");
  assert.equal(chains.limit, 100);
  assert.equal(chains.offset, 0);
  assert.equal(chains.chain_cursor, "");

  const params = stateToParams(chains);
  assert.equal(params.has("query_context"), false);
  assert.equal(params.has("final_result"), false);
  assert.equal(params.has("final_endpoint_id"), false);
  assert.equal(params.has("api_family"), false);
  assert.equal(params.has("row_kind"), false);
  assert.equal(params.has("attempt_trigger"), false);
  assert.equal(params.has("attempt_result"), false);
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
    pricing_card_role: "",
    pricing_selection_state: "",
    time_range: "24h",
    status_family: "all",
    limit: 100,
    offset: 0,
    request_id: "303",
    selected_request_id: "404",
  });

  assert.equal(params.has("client_rule_id"), false);
  assert.equal(params.has("attempt_target_model_id"), false);
  assert.equal(params.get("request_id"), "303");
  assert.equal(params.get("selected_request_id"), "404");
});
