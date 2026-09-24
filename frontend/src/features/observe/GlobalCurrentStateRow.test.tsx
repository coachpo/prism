import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { GlobalCurrentStateItem } from "@/lib/types";
import { useLocale } from "@/i18n/useLocale";
import { GlobalCurrentStateRow } from "./GlobalCurrentStateRow";
import { canResetCooldown } from "./useRoutingHealthCurrentStateReset";

function stateItem(
  id: number,
  overrides: Partial<GlobalCurrentStateItem>,
): GlobalCurrentStateItem {
  // SAFETY: the row reads identity, observation state, and the timing fields
  // populated here; the remaining wire fields stay null.
  return {
    model: { id: "model-a", label: "Model A" },
    endpoint: { id: 1, label: "Endpoint A" },
    terminal_target: { id, label: `Target ${id}` },
    observation_state: "observed",
    state: "available",
    available: true,
    cycle_retry_attempts: 0,
    cumulative_retry_attempts: 0,
    next_retry_at: null,
    last_retry_delay_ms: null,
    ban_mode: null,
    banned_until_at: null,
    last_failure_kind: null,
    last_success_at: null,
    last_success_response_headers_latency_ms: null,
    in_flight_stream: null,
    in_flight_non_stream: null,
    qps_window_started_at: null,
    qps_window_request_count: null,
    created_at: null,
    updated_at: null,
    routing_schedule: null,
    ...overrides,
  } as unknown as GlobalCurrentStateItem;
}

function Rows({ items }: { items: GlobalCurrentStateItem[] }) {
  const { messages } = useLocale();
  const showActions = items.some(canResetCooldown);
  return (
    <table>
      <tbody>
        {items.map((item) => (
          <GlobalCurrentStateRow
            key={item.terminal_target.id}
            copy={messages.routingHealth}
            formatNumber={String}
            formatTime={(value) => value}
            item={item}
            onRequestReset={vi.fn()}
            resetting={false}
            showActions={showActions}
          />
        ))}
      </tbody>
    </table>
  );
}

function renderRows(items: GlobalCurrentStateItem[]) {
  render(
    <LocaleProvider>
      <Rows items={items} />
    </LocaleProvider>,
  );
}

describe("GlobalCurrentStateRow reset action", () => {
  it("offers 恢复请求 only on a service that is waiting or paused", () => {
    renderRows([
      stateItem(1, { state: "retry_wait" }),
      stateItem(2, { state: "available" }),
      stateItem(3, { observation_state: "unobserved", state: null }),
    ]);

    expect(
      within(screen.getByTestId("runtime-row-1")).getByRole("button", {
        name: "恢复请求",
      }),
    ).toBeInTheDocument();
    for (const id of [2, 3]) {
      expect(
        within(screen.getByTestId(`runtime-row-${id}`)).queryByRole("button"),
      ).not.toBeInTheDocument();
    }
  });

  it("drops the actions cell when no service has anything to recover", () => {
    renderRows([
      stateItem(1, { state: "available" }),
      stateItem(2, { observation_state: "unobserved", state: null }),
    ]);

    // Seven data cells, no empty eighth actions cell.
    expect(
      within(screen.getByTestId("runtime-row-1")).getAllByRole("cell"),
    ).toHaveLength(7);
  });

  it("treats only observed retry_wait and banned states as resettable", () => {
    expect(canResetCooldown(stateItem(1, { state: "banned" }))).toBe(true);
    expect(canResetCooldown(stateItem(1, { state: "retry_wait" }))).toBe(true);
    expect(canResetCooldown(stateItem(1, { state: "available" }))).toBe(false);
    expect(
      canResetCooldown(
        stateItem(1, { observation_state: "unobserved", state: "banned" }),
      ),
    ).toBe(false);
  });
});
