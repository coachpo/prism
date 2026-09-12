import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { ChainIngressItem, FinalizedSummary } from "@/lib/types/request-logs";
import { RequestMobileSummary } from "./RequestMobileSummary";

vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({ format: (value: string) => value }),
}));

afterEach(cleanup);

function renderSummary(overrides: Partial<FinalizedSummary> = {}) {
  // A retained finalized summary can outlive its request rows: no navigation is
  // available in this fixture, while its final metrics remain observable.
  const chain = {
    ingress_request_id: "retained-summary",
    started_at: "2026-09-10T00:00:00Z",
    elapsed_ms: 100,
    elapsed_evidence_state: "authoritative",
    retained_rows: [],
    retained_upstream_attempt_count: 0,
    finalized_summary: {
      request_log_id: null,
      final_status_code: 200,
      final_result: "completed",
      ingress_model: { id: "entry", label: "Entry" },
      final_pricing_status: "priced",
      final_pricing_evidence_trust: "trusted",
      total_cost_user_currency_micros: 1_000_000,
      report_currency_code: "USD",
      report_currency_symbol: "$",
      ...overrides,
    },
  } as unknown as ChainIngressItem;
  return render(
    <LocaleProvider>
      <RequestMobileSummary
        chains={[chain]}
        onSelect={vi.fn()}
        loading={false}
        hasPrevious={false}
        hasNext={false}
        onPrevious={vi.fn()}
        onNext={vi.fn()}
      >
        <div />
      </RequestMobileSummary>
    </LocaleProvider>,
  );
}

function costValue() {
  return screen.getByText(getStaticMessages().requestComparison.cost)
    .nextElementSibling;
}

describe("mobile retained summary evidence", () => {
  it("renders a missing status instead of the literal null", () => {
    // The server can omit a terminal HTTP status; exercise the wire null even
    // while the older shared DTO still describes the field as number.
    const { container } = renderSummary({
      final_status_code: null as unknown as number,
    });
    expect(screen.queryByText("null", { exact: true })).not.toBeInTheDocument();
    expect(screen.getByText(getStaticMessages().honesty.noValue)).toBeInTheDocument();
    expect(container.querySelector('[data-slot="missing-value"]')).toHaveTextContent("—");
  });

  it.each(["unpriced", "ineligible", "unknown"] as const)(
    "rejects a nonempty retained amount when pricing state is %s",
    (final_pricing_status) => {
      renderSummary({ final_pricing_status });
      expect(costValue()).toHaveTextContent("—");
      expect(costValue()).toHaveTextContent(getStaticMessages().requestComparison.missingCost);
      expect(costValue()).not.toHaveTextContent("$1.00");
    },
  );

  it("rejects untrusted priced history and permits a trusted priced zero", () => {
    renderSummary({ final_pricing_evidence_trust: "legacy_untrusted" });
    expect(costValue()).toHaveTextContent("—");
    cleanup();
    renderSummary({ total_cost_user_currency_micros: 0 });
    expect(costValue()).toHaveTextContent("$0.00 USD");
    expect(costValue()).not.toHaveTextContent("—");
    expect(screen.getByText("完成", { exact: true })).toBeInTheDocument();
  });
});
