import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { LoadbalanceEventDetailSheet } from "../LoadbalanceEventDetailSheet";

const api = vi.hoisted(() => ({
  loadbalance: {
    getEvent: vi.fn(),
  },
}));

vi.mock("@/lib/api", () => ({ api }));
vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({
    format: (value: string) => `formatted:${value}`,
  }),
}));

describe("LoadbalanceEventDetailSheet", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized ban-related detail rows when the event payload includes them", async () => {
    api.loadbalance.getEvent.mockResolvedValue({
      id: 41,
      profile_id: 3,
      connection_id: 11,
      event_type: "banned",
      failure_kind: "timeout",
      consecutive_failures: 4,
      cooldown_seconds: 720,
      blocked_until_mono: null,
      model_id: "gpt-5.4",
      endpoint_id: 7,
      vendor_id: 1,
      summary: {
        event: "Connection banned",
        reason: "Reached the max-cooldown strike threshold",
        operation: "Routing paused",
        cooldown: "1800s ban",
      },
      created_at: "2026-03-25T08:00:00Z",
      failure_threshold: 4,
      backoff_multiplier: 3.5,
      max_cooldown_seconds: 720,
      max_cooldown_strikes: 3,
      ban_mode: "temporary",
      banned_until_at: "2026-03-25T08:30:00Z",
    });

    render(
      <LocaleProvider>
        <LoadbalanceEventDetailSheet eventId={41} onClose={vi.fn()} />
      </LocaleProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("最大冷却次数")).toBeInTheDocument();
    });

    expect(screen.getByText("负载均衡事件详情")).toBeInTheDocument();
    expect(screen.getByText("封禁模式")).toBeInTheDocument();
    expect(screen.getByText("临时")).toBeInTheDocument();
    expect(screen.getByText("封禁至")).toBeInTheDocument();
    expect(screen.getByText("formatted:2026-03-25T08:30:00Z")).toBeInTheDocument();
  });
});
