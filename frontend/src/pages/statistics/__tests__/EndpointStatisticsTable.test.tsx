import { useMemo, useState } from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { UsageEndpointStatistic, UsageModelStatistic } from "@/lib/types";
import { EndpointStatisticsTable } from "../tables/EndpointStatisticsTable";
import { installLocalStorageMock } from "./storage";

function createEndpointStatistic(
  overrides: Partial<UsageEndpointStatistic> = {},
): UsageEndpointStatistic {
  return {
    endpoint_id: 10,
    endpoint_label: "Primary Endpoint",
    request_count: 7,
    success_rate: 85.7,
    total_cost_micros: 4200,
    total_tokens: 410,
    ...overrides,
  };
}

function createModelStatistic(
  overrides: Partial<UsageModelStatistic> = {},
): UsageModelStatistic {
  return {
    model_id: "claude-sonnet-4-6",
    model_label: "Claude Sonnet 4.6",
    request_count: 3,
    success_rate: 100,
    total_cost_micros: 2100,
    total_tokens: 240,
    ...overrides,
  };
}

describe("EndpointStatisticsTable", () => {
  beforeEach(() => {
    installLocalStorageMock();
    localStorage.clear();
  });

  it("renders collapsed endpoint rows and lazy-loads nested model details on expand", async () => {
    const handleLoad = vi.fn();

    function Harness() {
      const items = useMemo(
        () => [
          createEndpointStatistic(),
          createEndpointStatistic({
            endpoint_id: 11,
            endpoint_label: "Secondary Endpoint",
            request_count: 1,
            success_rate: 100,
            total_cost_micros: 1200,
            total_tokens: 80,
          }),
        ],
        [],
      );
      const [detailsByEndpointId, setDetailsByEndpointId] = useState<
        Record<number, UsageModelStatistic[]>
      >({});
      const [loadingByEndpointId, setLoadingByEndpointId] = useState<Record<number, boolean>>({});

      return (
        <EndpointStatisticsTable
          currency={{ code: "USD", symbol: "$" }}
          endpointModelStatisticsByEndpointId={detailsByEndpointId}
          endpointModelStatisticsErrors={{}}
          endpointModelStatisticsLoading={loadingByEndpointId}
          items={items}
          onLoadEndpointModelStatistics={async (endpointId) => {
            handleLoad(endpointId);
            setLoadingByEndpointId((current) => ({ ...current, [endpointId]: true }));
            setDetailsByEndpointId((current) => ({
              ...current,
              [endpointId]: [
                createModelStatistic(),
                createModelStatistic({
                  model_id: "gpt-5.4",
                  model_label: "GPT-5.4",
                  request_count: 1,
                  success_rate: 0,
                  total_cost_micros: 0,
                  total_tokens: 60,
                }),
              ],
            }));
            setLoadingByEndpointId((current) => ({ ...current, [endpointId]: false }));
          }}
        />
      );
    }

    render(
      <LocaleProvider>
        <Harness />
      </LocaleProvider>,
    );

    const table = screen.getByTestId("statistics-endpoint-table");
    expect(screen.getByText("Top Endpoints by Requests")).toBeInTheDocument();
    expect(screen.getAllByTestId("statistics-endpoint-collapsible")).toHaveLength(2);
    expect(screen.queryByText("Claude Sonnet 4.6")).not.toBeInTheDocument();

    const triggers = within(table).getAllByRole("button");
    fireEvent.click(triggers[0]!);

    await waitFor(() => {
      expect(handleLoad).toHaveBeenCalledWith(10);
    });

    await waitFor(() => {
      expect(screen.getByText("Claude Sonnet 4.6")).toBeInTheDocument();
      expect(screen.getByText("GPT-5.4")).toBeInTheDocument();
    });

    expect(screen.getByTestId("statistics-endpoint-model-table-10")).toBeInTheDocument();
  });
});
