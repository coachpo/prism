import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { UsageProxyApiKeyStatistic } from "@/lib/types";
import { ProxyApiKeyStatisticsTable } from "../tables/ProxyApiKeyStatisticsTable";
import { installLocalStorageMock } from "./storage";

function createProxyApiKeyStatistic(
  overrides: Partial<UsageProxyApiKeyStatistic> = {},
): UsageProxyApiKeyStatistic {
  return {
    proxy_api_key_id: 77,
    proxy_api_key_label: "Primary runtime key",
    request_count: 5,
    success_rate: 80,
    total_cost_micros: 4200,
    total_tokens: 245,
    ...overrides,
  };
}

describe("ProxyApiKeyStatisticsTable", () => {
  beforeEach(() => {
    installLocalStorageMock();
    localStorage.clear();
  });

  it("renders a sortable proxy-key table without summary cards or key prefixes", () => {
    render(
      <LocaleProvider>
        <ProxyApiKeyStatisticsTable
          items={[
            createProxyApiKeyStatistic(),
            createProxyApiKeyStatistic({
              proxy_api_key_id: 81,
              proxy_api_key_label: "Fallback runtime key",
              request_count: 2,
              success_rate: 20,
              total_cost_micros: 0,
              total_tokens: 64,
            }),
          ]}
        />
      </LocaleProvider>,
    );

    const table = screen.getByTestId("statistics-proxy-key-table");
    expect(screen.queryByTestId("proxy-key-summary-grid")).not.toBeInTheDocument();
    expect(screen.queryByText("Key Prefix")).not.toBeInTheDocument();

    const rows = within(table)
      .getAllByRole("row")
      .slice(1)
      .map((row) => row.textContent ?? "");
    expect(rows[0]).toContain("Primary runtime key");
    expect(rows[1]).toContain("Fallback runtime key");

    fireEvent.click(within(table).getByRole("button", { name: "Total Spend" }));

    const sortedRows = within(table)
      .getAllByRole("row")
      .slice(1)
      .map((row) => row.textContent ?? "");
    expect(sortedRows[0]).toContain("Fallback runtime key");

    fireEvent.click(within(table).getByRole("button", { name: "Total Spend" }));
    const resortedRows = within(table)
      .getAllByRole("row")
      .slice(1)
      .map((row) => row.textContent ?? "");
    expect(resortedRows[0]).toContain("Primary runtime key");
  });
});
