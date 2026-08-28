import { describe, expect, it, vi, beforeEach } from "vitest";
import {
  applySavedView,
  deleteRequestLogView,
  loadSavedViews,
  saveRequestLogView,
  savedViewStateOf,
} from "./requestLogSavedViews";
import type { RequestLogPageState } from "./queryParams";

const STORAGE = new Map<string, string>();

beforeEach(() => {
  STORAGE.clear();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => STORAGE.get(key) ?? null,
    setItem: (key: string, value: string) => {
      STORAGE.set(key, value);
    },
    removeItem: (key: string) => {
      STORAGE.delete(key);
    },
  });
});

function makeState(overrides: Partial<RequestLogPageState> = {}): RequestLogPageState {
  return {
    ingress_final_result: "",
    confirmed_failover: false,
    ingress_request_id: "",
    model_id: "gpt-4o",
    endpoint_id: "",
    terminal_target_id: "",
    client_rule_id: "",
    proxy_api_key_id: "",
    resolved_target_model_id: "",
    api_family: "",
    row_kind: "",
    status_code: "",
    error_text: "rate limit",
    pricing_status: "all",
    unpriced_reason: "",
    pricing_card_role: "",
    pricing_selection_state: "",
    time_range: "7d",
    from_time: "",
    to_time: "",
    observe_return: "",
    query_context: "",
    final_result: "",
    outcome_detail: "",
    final_status_code: "",
    final_stream_outcome: "",
    final_stream_error_kind: "",
    final_target_model_id: "",
    final_endpoint_id: "",
    final_terminal_target_id: "",
    final_pricing_status: "",
    final_unpriced_reason: "",
    reporting_currency_epoch: "",
    cost_segment_key: "",
    attempt_trigger: "",
    attempt_result: "",
    status_family: "all",
    limit: 300,
    offset: 0,
    request_id: "",
    selected_request_id: "",
    view: "attempts",
    sort_by: "created_at",
    sort_order: "desc",
    chain_cursor: "",
    ...overrides,
  };
}

describe("requestLogSavedViews", () => {
  it("extracts canonical values from state without pagination or selection", () => {
    const canonical = savedViewStateOf(makeState({ request_id: "42", selected_request_id: "7", offset: 300, chain_cursor: "abc" }));
    expect(canonical.model_id).toBe("gpt-4o");
    expect(canonical.error_text).toBe("rate limit");
    expect(canonical.time_range).toBe("7d");
    expect(canonical.limit).toBe(300);
    expect("offset" in canonical).toBe(false);
    expect("request_id" in canonical).toBe(false);
    expect("selected_request_id" in canonical).toBe(false);
    expect("chain_cursor" in canonical).toBe(false);
  });

  it("omits signed-context selectors and resets them when applying a view", () => {
    const tokenBound = {
      query_context: "signed-context",
      final_result: "failed,client_disconnected",
      outcome_detail: "http_error",
      final_status_code: "429,503",
      final_stream_outcome: "stream_error",
      final_stream_error_kind: "__null__,protocol_error",
      final_target_model_id: "winner-a,__null__",
      final_endpoint_id: "7,__null__",
      final_terminal_target_id: "11,12",
      final_pricing_status: "unpriced",
      final_unpriced_reason: "MISSING_PRICE_DATA",
      reporting_currency_epoch: "3",
      attempt_trigger: "initial,failover",
      attempt_result: "http_error,__null__",
    } satisfies Partial<RequestLogPageState>;
    const stateOnly = savedViewStateOf(makeState(tokenBound));

    for (const key of Object.keys(tokenBound)) {
      expect(key in stateOnly).toBe(false);
    }

    const view = saveRequestLogView("ordinary", makeState());
    const applied = applySavedView(view, makeState(tokenBound));
    for (const key of Object.keys(tokenBound) as Array<keyof typeof tokenBound>) {
      expect(applied[key]).toBe("");
    }
  });

  it("preserves ordinary attempt identity filters in an attempts saved view", () => {
    const canonical = savedViewStateOf(
      makeState({ api_family: "openai", row_kind: "upstream" }),
    );
    expect(canonical.api_family).toBe("openai");
    expect(canonical.row_kind).toBe("upstream");
  });

  it("round-trips saved views through localStorage", () => {
    const view = saveRequestLogView("Errors 7d", makeState());
    const views = loadSavedViews();
    expect(views).toHaveLength(1);
    expect(views[0].name).toBe("Errors 7d");
    expect(views[0].state.model_id).toBe("gpt-4o");
    expect(views[0].state.error_text).toBe("rate limit");
    expect(view.id).toBe(views[0].id);
  });

  it("drops legacy unknown shapes wholesale", () => {
    localStorage.setItem(
      "prism.request-logs.saved-views.v1",
      JSON.stringify({
        version: 1,
        views: [
          { id: "a", name: "missing state", createdAt: "", updatedAt: "", state: null },
          { id: "b", name: "bad id", createdAt: "", updatedAt: "", state: null },
          { id: "c", name: "valid", createdAt: "", updatedAt: "", state: { model_id: "gpt-4o", time_range: "24h" } },
        ],
      }),
    );
    const views = loadSavedViews();
    expect(views.map((view) => view.name)).toEqual(["valid"]);
  });

  it("applies a view and resets transient anchors", () => {
    const view = saveRequestLogView("Deep link", makeState({ view: "ingress_chains", offset: 300, request_id: "9" }));
    const applied = applySavedView(view, makeState({ offset: 10, request_id: "x" }));
    expect(applied.model_id).toBe("gpt-4o");
    expect(applied.view).toBe("ingress_chains");
    expect(applied.offset).toBe(0);
    expect(applied.request_id).toBe("");
    expect(applied.chain_cursor).toBe("");
    deleteRequestLogView(view.id);
    expect(loadSavedViews()).toHaveLength(0);
  });
});
