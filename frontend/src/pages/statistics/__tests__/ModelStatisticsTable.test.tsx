import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { UsageModelStatistic } from "@/lib/types";
import { ModelStatisticsTable } from "../tables/ModelStatisticsTable";
import { installLocalStorageMock } from "./storage";

function createModelStatistic(
  overrides: Partial<UsageModelStatistic> = {},
): UsageModelStatistic {
  return {
    model_id: "gpt-5.4",
    model_label: "GPT-5.4",
    request_count: 7,
    success_rate: 100,
    total_cost_micros: 4200,
    total_tokens: 410,
    ...overrides,
  };
}

describe("ModelStatisticsTable", () => {
  beforeEach(() => {
    installLocalStorageMock();
    localStorage.clear();
  });

  it("renders a sortable model statistics table without the removed api-family column", () => {
    render(
      <LocaleProvider>
        <ModelStatisticsTable
          currency={{ code: "USD", symbol: "$" }}
          items={[
            createModelStatistic(),
            createModelStatistic({
              model_id: "claude-sonnet-4-6",
              model_label: "Claude Sonnet 4.6",
              request_count: 3,
              success_rate: 33.3,
              total_cost_micros: 0,
              total_tokens: 90,
            }),
          ]}
        />
      </LocaleProvider>,
    );

    const table = screen.getByTestId("statistics-model-table");
    expect(screen.getByText("Top Models by Requests")).toBeInTheDocument();
    const rows = within(table)
      .getAllByRole("row")
      .slice(1)
      .map((row) => row.textContent ?? "");
    expect(rows[0]).toContain("GPT-5.4");
    expect(rows[1]).toContain("Claude Sonnet 4.6");
    expect(screen.queryByText("OpenAI")).not.toBeInTheDocument();

    fireEvent.click(within(table).getByRole("button", { name: "Success Rate" }));
    fireEvent.click(within(table).getByRole("button", { name: "Success Rate" }));

    const sortedRows = within(table)
      .getAllByRole("row")
      .slice(1)
      .map((row) => row.textContent ?? "");
    expect(sortedRows[0]).toContain("GPT-5.4");
    expect(sortedRows[1]).toContain("Claude Sonnet 4.6");
  });
});
