import { describe, expect, it } from "vitest";

import {
  buildRequestLogQueryParams,
  requestLogQuerySignature,
} from "./requestLogQuery";
import { parsePageSearch } from "./queryParams";
import { buildExportParams } from "./requestLogsCsv";

describe("request-log query projection", () => {
  it("preserves explicit custom time bounds in the wire query", () => {
    const state = parsePageSearch({
      from_time: "2026-08-01T00:00:00Z",
      to_time: "2026-08-02T00:00:00Z",
      view: "attempts",
    });

    expect(buildRequestLogQueryParams(state)).toMatchObject({
      time_range: "custom",
      from_time: "2026-08-01T00:00:00Z",
      to_time: "2026-08-02T00:00:00Z",
      view: "attempts",
    });
  });

  it("lets the backend resolve an exact ingress against retained all bounds", () => {
    const state = parsePageSearch({
      ingress_request_id: "old-ingress",
      view: "ingress_chains",
    });

    const listParams = buildRequestLogQueryParams(state);
    expect(listParams.ingress_request_id).toBe("old-ingress");
    expect(listParams).not.toHaveProperty("time_range");
    expect(buildExportParams(state)).not.toHaveProperty("time_range");
  });

  it("keeps chain cursor navigation in one query scope", () => {
    const firstState = parsePageSearch({
      chain_cursor: "cursor-1",
      view: "ingress_chains",
    });
    const secondState = parsePageSearch({
      chain_cursor: "cursor-2",
      view: "ingress_chains",
    });

    expect(
      requestLogQuerySignature(
        firstState,
        buildRequestLogQueryParams(firstState),
      ),
    ).toBe(
      requestLogQuerySignature(
        secondState,
        buildRequestLogQueryParams(secondState),
      ),
    );

    const params = buildRequestLogQueryParams(firstState);
    expect(params).toMatchObject({
      time_range: "24h",
      view: "ingress_chains",
      chain_cursor: "cursor-1",
    });
    expect(params).not.toHaveProperty("limit");
    expect(params).not.toHaveProperty("offset");
    // 链视图有自己的页大小（后端 chain_limit，上限 50），与尝试视图的 limit 分开。
    expect(params.chain_limit).toBe(20);
  });

  it("sends cost segments only to the chain grammar that owns them", () => {
    const attempts = parsePageSearch({
      view: "attempts",
      cost_segment_key: "e.3",
    });
    const chains = parsePageSearch({
      view: "ingress_chains",
      cost_segment_key: "e.3",
    });

    expect(buildRequestLogQueryParams(attempts).cost_segment_key).toBeUndefined();
    expect(buildRequestLogQueryParams(chains)).toMatchObject({
      cost_segment_key: "e.3",
    });
  });

  it("projects every signed Observe selector without truncating list values", () => {
    const state = parsePageSearch({
      query_context: "signed-context",
      view: "attempts",
      ingress_final_result: "failed",
      confirmed_failover: "true",
      attempt_target_model_id: "target-a,__null__",
      api_family: "openai,__null__",
      row_kind: "upstream",
      attempt_trigger: "initial,failover,__null__",
      attempt_result: "http_error,transport_error,__null__",
      status_code: "429,503",
      final_result: "failed,client_disconnected",
      outcome_detail: "http_error,stream_error",
      final_status_code: "429,503",
      final_stream_outcome: "stream_error,client_disconnected",
      final_stream_error_kind: "__null__,protocol_error",
      final_exclude: "stream_error_kind,__null__,protocol_error",
      final_target_model_id: "winner-a,__null__",
      final_endpoint_id: "7,__null__,9",
      final_terminal_target_id: "11,12,__null__",
      final_pricing_status: "unpriced",
      final_unpriced_reason: "MISSING_PRICE_DATA,STREAM_USAGE_UNAVAILABLE",
      reporting_currency_epoch: "3",
    });

    const params = buildRequestLogQueryParams(state);
    expect(params).toMatchObject({
      query_context: "signed-context",
      view: "attempts",
      ingress_final_result: "failed",
      confirmed_failover: "true",
      limit: 100,
      offset: 0,
      attempt_target_model_id: ["target-a", "__null__"],
      api_family: ["openai", "__null__"],
      row_kind: ["upstream"],
      attempt_trigger: ["initial", "failover", "__null__"],
      attempt_result: ["http_error", "transport_error", "__null__"],
      status_code: ["429", "503"],
      final_result: ["failed", "client_disconnected"],
      outcome_detail: ["http_error", "stream_error"],
      final_status_code: ["429", "503"],
      final_stream_outcome: ["stream_error", "client_disconnected"],
      final_stream_error_kind: ["__null__", "protocol_error"],
      final_exclude: ["stream_error_kind", "__null__", "protocol_error"],
      final_target_model_id: ["winner-a", "__null__"],
      final_endpoint_id: ["7", "__null__", "9"],
      final_terminal_target_id: ["11", "12", "__null__"],
      final_pricing_status: "unpriced",
      final_unpriced_reason: [
        "MISSING_PRICE_DATA",
        "STREAM_USAGE_UNAVAILABLE",
      ],
      reporting_currency_epoch: "3",
    });
    expect(params).not.toHaveProperty("time_range");
    expect(params).not.toHaveProperty("from_time");
    expect(params).not.toHaveProperty("to_time");
    expect(params).not.toHaveProperty("chain_cursor");
  });

  it("keeps signed CSV filters while omitting browser time and pagination", () => {
    const state = parsePageSearch({
      query_context: "signed-context",
      view: "attempts",
      ingress_final_result: "failed",
      confirmed_failover: "true",
      time_range: "7d",
      from_time: "2026-08-01T00:00:00Z",
      to_time: "2026-08-02T00:00:00Z",
      final_endpoint_id: "7,__null__,9",
      final_exclude: "final_endpoint_id,7,__null__,9",
      api_family: "openai",
      row_kind: "upstream",
      attempt_trigger: "initial,failover",
      attempt_result: "http_error,__null__",
      cursor: "300",
      limit: "300",
    });

    const params = buildExportParams(state);
    expect(params).toMatchObject({
      query_context: "signed-context",
      view: "attempts",
      ingress_final_result: "failed",
      confirmed_failover: "true",
      final_endpoint_id: ["7", "__null__", "9"],
      final_exclude: ["final_endpoint_id", "7", "__null__", "9"],
      api_family: ["openai"],
      row_kind: ["upstream"],
      attempt_trigger: ["initial", "failover"],
      attempt_result: ["http_error", "__null__"],
    });
    for (const key of [
      "time_range",
      "from_time",
      "to_time",
      "limit",
      "offset",
      "chain_cursor",
      "row_cursor",
    ]) {
      expect(params).not.toHaveProperty(key);
    }
  });
});
