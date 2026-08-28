import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { ObserveActivityResponse } from "@/lib/api/observability";
import { ObserveActivityTable } from "./ObserveActivityTable";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  observeActivity: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => mocks.navigate,
}));

vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({
    format: (value: string) => value,
    timezone: "UTC",
  }),
}));

vi.mock("@/lib/api/observability", () => ({
  observe: { observeActivity: mocks.observeActivity },
}));

const response: ObserveActivityResponse = {
  generated_at: "2026-08-09T00:00:00Z",
  coverage: {
    requested_preset: "24h",
    from_time: "2026-08-08T00:00:00Z",
    to_time: "2026-08-09T00:00:00Z",
    retention_from_time: null,
    source: "raw",
    complete: true,
    gaps: [],
  },
  items: [
    {
      usage_event_id: "event-1",
      final_ingress_request_id: "ingress-1",
      created_at: "2026-08-09T00:00:00Z",
      ingress_model_id: "entry-a",
      ingress_model_label: "Model A",
      final_target_model_id: "target-c",
      final_target_model_label: "Model C",
      route_changed: true,
      attempt_count: 2,
      routing_evidence_complete: true,
      endpoint_id: 7,
      endpoint_label: "Endpoint C",
      terminal_target_id: 17,
      status_code: 200,
      final_result: "completed",
      outcome_detail: "completed",
      is_stream: false,
      stream_outcome: "not_streaming",
      stream_error_kind: null,
      ttft_ms: 610,
      total_duration_ms: 1_430,
      output_tokens: 80,
      total_tokens: 120,
      known_cost_micros: "5000",
      final_pricing_status: "priced",
      final_unpriced_reason: null,
      reporting_currency_epoch: 1,
      report_currency_code: "USD",
      report_currency_symbol: "$",
    },
  ],
  has_more: false,
};

describe("ObserveActivityTable ingress attribution", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.observeActivity.mockResolvedValue(response);
  });

  it("shows A to C, attempt count and the actual outlet on one finalized request row", async () => {
    render(
      <LocaleProvider>
        <ObserveActivityTable preset="24h" queryContext="ingress-context" />
      </LocaleProvider>,
    );

    const row = await screen.findByTestId("activity-row");
    expect(row).toHaveTextContent(/入口\s+Model A/);
    expect(row).toHaveTextContent(/最终\s+Model C/);
    expect(within(row).getByText("2")).toBeInTheDocument();
    expect(within(row).getByText("终端目标 #17")).toBeInTheDocument();
    expect(within(row).getByText("Endpoint C")).toBeInTheDocument();
    expect(within(row).getByText("$0.0050")).toBeInTheDocument();
    expect(
      screen.getByRole("columnheader", { name: "入口 → 最终目标" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("columnheader", { name: "实际执行出口" }),
    ).toBeInTheDocument();

    expect(mocks.observeActivity).toHaveBeenCalledWith("ingress-context", {
      limit: 20,
      before: undefined,
    });
  });

  it("opens the retained ingress chain for the finalized request", async () => {
    const user = userEvent.setup();
    render(
      <LocaleProvider>
        <ObserveActivityTable preset="7d" queryContext="ingress-context" />
      </LocaleProvider>,
    );

    await user.click(await screen.findByRole("button", { name: "查看请求" }));
    await waitFor(() =>
      expect(mocks.navigate).toHaveBeenCalledWith({
        to: "/observe/requests",
        search: {
          view: "ingress_chains",
          ingress_request_id: "ingress-1",
          time_range: "7d",
        },
      }),
    );
  });
});
