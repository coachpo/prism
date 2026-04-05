import React from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { ModelConfigListItem, RequestLogListItem } from "@/lib/types";
import { RequestLogsTable } from "../RequestLogsTable";
import { formatCost, getColumns } from "../columns";

class ResizeObserverMock {
  observe() {}
  disconnect() {}
  unobserve() {}
}

Object.defineProperty(window, "ResizeObserver", {
  writable: true,
  configurable: true,
  value: ResizeObserverMock,
});

function createResolveModelLabel(
  labels: Record<string, string>,
  modelTypes: Record<string, "native" | "proxy"> = {},
) {
  return Object.assign(
    (modelId: string) => labels[modelId] ?? modelId,
    {
      getModelMetadata: (modelId: string) =>
        modelTypes[modelId]
          ? ({
              model_id: modelId,
              model_type: modelTypes[modelId],
            } as ModelConfigListItem)
          : undefined,
    },
  );
}

function renderModelCell(
  row: RequestLogListItem,
  resolveModelLabel: ReturnType<typeof createResolveModelLabel>,
) {
  const modelColumn = getColumns().find((column) => column.key === "model_id");

  expect(modelColumn).toBeDefined();

  render(
    React.createElement(
      React.Fragment,
      null,
      modelColumn?.render(
        row,
        () => "formatted:2026-03-16T00:00:00.000Z",
        resolveModelLabel,
        () => "—",
      ),
    ),
  );
}

function buildRow(overrides: Partial<RequestLogListItem> = {}): RequestLogListItem {
  return {
    id: 42,
    model_id: "claude-sonnet-4-5",
    resolved_target_model_id: null,
    created_at: "2026-03-16T00:00:00.000Z",
    api_family: "anthropic",
    vendor_id: 1,
    vendor_key: "anthropic",
    vendor_name: "Anthropic",
    endpoint_id: null,
    connection_id: null,
    status_code: 200,
    response_time_ms: 123,
    is_stream: false,
    total_tokens: null,
    total_cost_user_currency_micros: null,
    report_currency_symbol: null,
    ...overrides,
  } as RequestLogListItem;
}

describe("formatCost", () => {
  it("preserves up to six fractional digits for small costs", () => {
    expect(formatCost(23_412, "$" )).toBe("$0.023412");
  });

  it("returns an em dash for zero and missing costs", () => {
    expect(formatCost(null, "$" )).toBe("—");
    expect(formatCost(0, "$" )).toBe("—");
  });

  it("renders a proxy-origin sign for routed proxy rows", () => {
    renderModelCell(
      buildRow({
        resolved_target_model_id: "claude-sonnet-4-5-20250929",
        vendor_id: null,
        vendor_key: null,
        vendor_name: null,
      }),
      createResolveModelLabel(
        {
          "claude-sonnet-4-5": "Claude Sonnet 4.5 Proxy",
          "claude-sonnet-4-5-20250929": "Claude Sonnet 4.5 (20250929)",
        },
        { "claude-sonnet-4-5": "proxy" },
      ),
    );

    expect(screen.getByText("Claude Sonnet 4.5 Proxy")).toBeInTheDocument();
    expect(screen.getByText(/proxy origin/i)).toBeInTheDocument();
    expect(screen.getByText("Resolved target → Claude Sonnet 4.5 (20250929)")).toBeInTheDocument();
  });

  it("renders a proxy-origin sign for unroutable proxy rows from current model metadata", () => {
    renderModelCell(
      buildRow({
        id: 43,
        resolved_target_model_id: null,
        status_code: 503,
        response_time_ms: 45,
        vendor_id: null,
        vendor_key: null,
        vendor_name: null,
      }),
      createResolveModelLabel(
        { "claude-sonnet-4-5": "Gateway proxy" },
        { "claude-sonnet-4-5": "proxy" },
      ),
    );

    expect(screen.getByText("Gateway proxy")).toBeInTheDocument();
    expect(screen.getByText(/proxy origin/i)).toBeInTheDocument();
    expect(screen.queryByText(/Resolved target/i)).not.toBeInTheDocument();
  });

  it("renders the fixed denser request-log column set and drops separate vendor or id columns", () => {
    const columns = getColumns();
    const columnKeys = columns.map((column) => column.key);
    const columnLayout = columns.map(({ key, width, grow }) => ({
      key,
      width,
      grow: grow ?? 0,
    }));

    expect(columnKeys).toEqual([
      "created_at",
      "status_code",
      "response_time_ms",
      "model_id",
      "endpoint_id",
      "vendor_api_family",
      "total_tokens",
      "total_cost",
      "is_stream",
    ]);
    expect(columnLayout).toEqual([
      { key: "created_at", width: 168, grow: 0 },
      { key: "status_code", width: 84, grow: 0 },
      { key: "response_time_ms", width: 108, grow: 0 },
      { key: "model_id", width: 240, grow: 3 },
      { key: "endpoint_id", width: 180, grow: 2 },
      { key: "vendor_api_family", width: 150, grow: 1 },
      { key: "total_tokens", width: 110, grow: 0 },
      { key: "total_cost", width: 104, grow: 0 },
      { key: "is_stream", width: 92, grow: 0 },
    ]);
    expect(columnKeys).not.toContain("vendor_name");
    expect(columnKeys).not.toContain("api_family");
    expect(columnKeys).not.toContain("request_id");
    expect(columnKeys).not.toContain("ingress_request_id");

    render(
      React.createElement(
        LocaleProvider,
        null,
        React.createElement(RequestLogsTable, {
          items: [buildRow({ vendor_name: null, vendor_key: null, vendor_id: null })],
          total: 1,
          loading: false,
          limit: 100,
          offset: 0,
          activeRequestId: null,
          onSelectRequest: () => undefined,
          onSetLimit: () => undefined,
          onNextPage: () => undefined,
          onPreviousPage: () => undefined,
          formatTimestamp: () => "formatted:2026-03-16T00:00:00.000Z",
          resolveModelLabel: createResolveModelLabel({ "claude-sonnet-4-5": "Claude Sonnet 4.5 Proxy" }),
          resolveEndpointLabel: () => "—",
        })
      )
    );

    expect(screen.getByText(/vendor \/ api/i)).toBeInTheDocument();
    expect(screen.getByTestId("request-log-page-size-select")).toBeInTheDocument();
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });

  it("resolves endpoint labels from current endpoint metadata instead of raw ids", () => {
    render(
      React.createElement(
        LocaleProvider,
        null,
        React.createElement(RequestLogsTable, {
          items: [buildRow({ endpoint_id: 12 })],
          total: 1,
          loading: false,
          limit: 100,
          offset: 0,
          activeRequestId: null,
          onSelectRequest: () => undefined,
          onSetLimit: () => undefined,
          onNextPage: () => undefined,
          onPreviousPage: () => undefined,
          formatTimestamp: () => "formatted:2026-03-16T00:00:00.000Z",
          resolveModelLabel: createResolveModelLabel({ "claude-sonnet-4-5": "Claude Sonnet 4.5 Proxy" }),
          resolveEndpointLabel: (endpointId) =>
            endpointId === 12 ? "Primary endpoint" : endpointId === null ? "—" : `#${endpointId}`,
        }),
      ),
    );

    expect(screen.getByText("Primary endpoint")).toBeInTheDocument();
    expect(screen.queryByText(/^#12$/)).not.toBeInTheDocument();
  });

  it("renders the archived successful request row fields and pagination controls", () => {
    render(
      React.createElement(
        LocaleProvider,
        null,
        React.createElement(RequestLogsTable, {
          items: [
            buildRow({
              created_at: "2026-03-31T09:55:16.000Z",
              model_id: "gpt-5.4",
              api_family: "openai",
              vendor_name: "OpenAI",
              endpoint_id: 12,
              status_code: 200,
              response_time_ms: 1500,
              total_tokens: 38,
            }),
          ],
          total: 3,
          loading: false,
          limit: 100,
          offset: 0,
          activeRequestId: null,
          onSelectRequest: () => undefined,
          onSetLimit: () => undefined,
          onNextPage: () => undefined,
          onPreviousPage: () => undefined,
          formatTimestamp: () => "formatted:2026-03-31T09:55:16.000Z",
          resolveModelLabel: createResolveModelLabel({ "gpt-5.4": "GPT-5.4" }),
          resolveEndpointLabel: (endpointId) => (endpointId === null ? "—" : "Primary endpoint"),
        }),
      ),
    );

    expect(screen.getByText("formatted:2026-03-31T09:55:16.000Z")).toBeInTheDocument();
    expect(screen.getByText("GPT-5.4")).toBeInTheDocument();
    expect(screen.getAllByText("OpenAI").length).toBeGreaterThan(0);
    expect(screen.getByText("200")).toBeInTheDocument();
    expect(screen.getByText("38")).toBeInTheDocument();
    expect(screen.getByText("Primary endpoint")).toBeInTheDocument();
    expect(screen.getByText("1,500ms")).toBeInTheDocument();
    expect(screen.getByText("1-3 of 3")).toBeInTheDocument();
    expect(screen.getByTestId("request-log-page-size-select")).toBeInTheDocument();
  });
});
