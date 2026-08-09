import { describe, expect, it } from "vitest";
import { parsePageSearch, stateToSearch } from "@/pages/request-logs/queryParams";
import { getColumns } from "@/pages/request-logs/columns";
import type { RequestLogListItem } from "@/lib/types";
import { getStaticMessages } from "@/i18n/staticMessages";
import { rowValue } from "@/pages/request-logs/requestLogsCsv";

function makeRow(overrides: Partial<RequestLogListItem>): RequestLogListItem {
  return {
    id: 101,
    created_at: "2026-04-18T12:34:56Z",
    model_id: "gpt-4o",
    model_label: "gpt-4o",
    resolved_target_model_id: "gpt-4o-native",
    resolved_target_model_label: "gpt-4o-native",
    caller_client_display: "Codex",
    upstream_client_display: "OpenAI SDK",
    user_agent_overridden: true,
    api_family: "openai",
    endpoint_id: 12,
    endpoint_label: "Primary OpenAI",
    connection_id: 34,
    terminal_target_id: 34,
    ttft_ms: 320,
    completion_duration_ms: 914,
    status_code: 200,
    response_time_ms: 1234,
    is_stream: false,
    stream_outcome: "not_streaming",
    stream_error_kind: null,
    reasoning_effort: null,
    output_tokens: 42,
    total_tokens: 57,
    total_cost_user_currency_micros: 1250,
    priced_flag: true,
    unpriced_reason: null,
    report_currency_symbol: "$",
    proxy_api_key_id: 42,
    proxy_api_key_name_snapshot: "production-client",
    proxy_api_key_attribution_state: "identified",
    proxy_api_key_auth_enforced_at_request: false,
    ...overrides,
  };
}

describe("request-logs CSV export carries proxy key identity", () => {
  it("CSV header includes the proxy key column and exported rows carry key identity", () => {
    const messages = getStaticMessages();
    const columns = getColumns();
    const proxyColumn = columns.find((column) => column.key === "proxy_api_key");
    expect(proxyColumn).toBeTruthy();
    expect(proxyColumn?.label).toBe(messages.requestLogs.proxyKey);
  });

  it("row value projection matches the table render for all three buckets", () => {
    const messages = getStaticMessages();
    const identified = rowValue(makeRow({}), "proxy_api_key");
    expect(identified).toBe("production-client");
    const none = rowValue(
      makeRow({ proxy_api_key_id: null, proxy_api_key_name_snapshot: null, proxy_api_key_attribution_state: "none", proxy_api_key_auth_enforced_at_request: false }),
      "proxy_api_key",
    );
    expect(none).toBe(messages.requestLogs.noIdentifiedProxyKey);
    const unknown = rowValue(
      makeRow({ proxy_api_key_id: null, proxy_api_key_name_snapshot: null, proxy_api_key_attribution_state: "unknown", proxy_api_key_auth_enforced_at_request: null }),
      "proxy_api_key",
    );
    expect(unknown).toBe(messages.requestLogs.proxyKeyAttributionUnknown);
  });
});

describe("proxy_api_key_id and view URL round-trip", () => {
  it("parses, serializes and round-trips the ordinary filter and chain view", () => {
    const parsed = parsePageSearch({ proxy_api_key_id: "42", view: "ingress_chains", time_range: "7d", limit: 100 });
    expect(parsed.proxy_api_key_id).toBe("42");
    expect(parsed.view).toBe("ingress_chains");
    expect(parsed.time_range).toBe("7d");

    const serialized = stateToSearch(parsed);
    expect(serialized.proxy_api_key_id).toBe("42");
    expect(serialized.view).toBe("ingress_chains");
    expect(serialized.time_range).toBe("7d");

    const reparsed = parsePageSearch(serialized);
    expect(reparsed.proxy_api_key_id).toBe("42");
    expect(reparsed.view).toBe("ingress_chains");
  });

  it("rejects invalid view values and falls back to empty", () => {
    const parsed = parsePageSearch({ view: "other" });
    expect(parsed.view).toBe("");
  });

  it("clearing the key filter resets pagination", () => {
    const parsed = parsePageSearch({ proxy_api_key_id: "42", cursor: "100" });
    expect(parsed.offset).toBe(100);
    const cleared = { ...parsed, proxy_api_key_id: "", offset: 0 };
    const serialized = stateToSearch(cleared);
    expect(serialized.proxy_api_key_id).toBeUndefined();
    expect(serialized.cursor).toBeUndefined();
  });
});
