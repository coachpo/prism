// Every state the badge shows must come from the server. The two cases that
// matter most are the ones where the client could be tempted to guess: a
// configured schedule with no state (say nothing rather than "open"), and a
// server answer that has outlived the boundary it shipped with.
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { RoutingSchedule, RoutingScheduleState } from "@/lib/types/routing";
import { TargetRoutingScheduleBadge } from "@/pages/models/TargetRoutingScheduleBadge";

const schedule: RoutingSchedule = {
  timezone: "Asia/Shanghai",
  windows: [{ weekday_mask: 31, start_minute: 540, end_minute: 1080 }],
};

function state(overrides: Partial<RoutingScheduleState>): RoutingScheduleState {
  return {
    status: "open",
    timezone: "Asia/Shanghai",
    evaluated_at: "2026-08-16T01:00:00Z",
    next_open_at_known: false,
    next_close_at_known: false,
    ...overrides,
  };
}

function renderBadge(props: { schedule: RoutingSchedule | null; state: RoutingScheduleState | null }) {
  return render(
    <LocaleProvider>
      <TargetRoutingScheduleBadge {...props} />
    </LocaleProvider>,
  );
}

afterEach(() => {
  vi.useRealTimers();
});

describe("TargetRoutingScheduleBadge", () => {
  it("renders nothing when no schedule is configured", () => {
    const { container } = renderBadge({ schedule: null, state: null });
    expect(container).toBeEmptyDOMElement();
  });

  it("says the state was not read rather than guessing open or closed", () => {
    renderBadge({ schedule, state: null });
    expect(screen.getByText("时段状态未读到")).toBeInTheDocument();
  });

  it.each([
    ["open", state({ status: "open" }), "时段内"],
    ["closed without a known reopen", state({ status: "closed" }), "时段外"],
    ["unresolved", state({ status: "unresolved" }), "时段时区不可解析"],
    [
      "not evaluated because the connection is inactive",
      state({ status: "not_evaluated", not_evaluated_reason: "connection_inactive" }),
      "连接已停用，时段未生效",
    ],
  ])("renders the %s state from the server verdict alone", (_name, value, expected) => {
    renderBadge({ schedule, state: value });
    expect(screen.getByText(expected)).toBeInTheDocument();
  });

  it("downgrades to staleness once the server's own boundary has passed", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-16T12:00:00Z"));
    renderBadge({
      schedule,
      state: state({ status: "open", next_close_at: "2026-08-16T10:00:00Z", next_close_at_known: true }),
    });
    expect(screen.getByText(/时段状态已过期/)).toBeInTheDocument();
  });
});
