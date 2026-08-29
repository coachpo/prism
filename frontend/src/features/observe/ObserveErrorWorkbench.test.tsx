import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { UsageErrorsResponse } from "@/lib/api/observability";
import { ObserveErrorWorkbench } from "./ObserveErrorWorkbench";

const mocks = vi.hoisted(() => ({
  usageErrors: vi.fn(),
}));

vi.mock("@/lib/api/observability", () => ({
  observe: { usageErrors: mocks.usageErrors },
}));

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    search,
  }: {
    children: ReactNode;
    search: Record<string, string>;
  }) => (
    <a href="#requests" data-search={JSON.stringify(search)}>
      {children}
    </a>
  ),
}));

function errorsResponse(queryContext: string): UsageErrorsResponse {
  const emptyRemainder = {
    count: 0,
    denominator: 0,
    percentage: null,
    request_filters: null,
  };
  return {
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
    requests_context: {
      view: "attempts",
      query_context: queryContext,
      final_from_time: "2026-08-08T00:00:00Z",
      final_to_time: "2026-08-09T00:00:00Z",
      base_request_filters: { row_kind: ["upstream"] },
    },
    summary: {
      request_count: 1,
      http_error_count: 1,
      stream_error_count: 0,
      failed_count: 1,
      client_disconnected_count: 0,
      diagnostic_stream_anomaly_count: 0,
    },
    timeline: [],
    http_statuses: [
      {
        status_code: 503,
        count: 1,
        denominator: 1,
        percentage: 100,
        last_seen_at: "2026-08-08T01:00:00Z",
        request_filters: {
          row_kind: ["upstream"],
          status_code: ["503"],
          attempt_result: ["http_error"],
        },
      },
    ],
    stream_outcomes: [],
    groups: [],
    other: {
      http_statuses: emptyRemainder,
      stream_outcomes: emptyRemainder,
      groups: emptyRemainder,
    },
  };
}

describe("ObserveErrorWorkbench request binding", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("withdraws an old ranking and selection before a new token resolves", async () => {
    const nextResponse = new Promise<UsageErrorsResponse>(() => undefined);
    mocks.usageErrors.mockImplementation((queryContext: string) =>
      queryContext === "token-a"
        ? Promise.resolve(errorsResponse("token-a"))
        : nextResponse,
    );
    const user = userEvent.setup();
    const { rerender } = render(
      <LocaleProvider>
        <ObserveErrorWorkbench
          groupBy="attempt_result"
          queryContext="token-a"
          scope="route_attempt"
        />
      </LocaleProvider>,
    );

    await user.click(await screen.findByTestId("error-status-503"));
    expect(
      screen.getByRole("link", { name: "在请求日志中查看全部" }),
    ).toBeInTheDocument();

    rerender(
      <LocaleProvider>
        <ObserveErrorWorkbench
          groupBy="attempt_trigger"
          queryContext="token-b"
          scope="route_attempt"
        />
      </LocaleProvider>,
    );
    expect(screen.queryByTestId("error-status-503")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "在请求日志中查看全部" }),
    ).not.toBeInTheDocument();
  });
});
