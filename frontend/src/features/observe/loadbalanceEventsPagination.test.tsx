// C1 regression: the event timeline must issue a real read for the URL's
// target page — both when the operator clicks a pagination control and when a
// deep link carries an `event_cursor`. Before the fix, clicking 下一页 only
// rewrote the URL; no request ever followed.
import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import { LoadbalanceEventsFragment } from "@/features/observe/LoadbalanceEventsFragment";

const mocks = vi.hoisted(() => ({
  costingGet: vi.fn(),
  issueContext: vi.fn(),
  listEvents: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  api: {
    loadbalance: {
      issueEventsQueryContext: mocks.issueContext,
      listEvents: mocks.listEvents,
    },
    settings: {
      costing: { get: mocks.costingGet },
    },
  },
}));

function eventsPage(
  items: Array<{ event_id: string }>,
  nextCursor: string | null,
) {
  return {
    items,
    has_more: nextCursor !== null,
    next_cursor: nextCursor,
    coverage: { complete: true, gaps: [] },
  };
}

function eventItem(id: string) {
  return {
    event_id: id,
    event_type: "banned",
    created_at: "2026-08-13T12:00:00Z",
    summary: { reason_code: "consecutive_failures" },
    model: { id: "m1", model_id: "gpt-test", label: "GPT Test" },
    terminal_target: { id: 7, label: "TT-7" },
    endpoint: { id: 3, label: "EP-3" },
    failure_kind: null,
    admission_reason: null,
    banned_until_at: null,
    next_retry_at: null,
    last_success_at: null,
  };
}

/**
 * The parent owns the URL. This wrapper holds it as real state so a pagination
 * click that patches `event_cursor` reaches the fragment exactly like the
 * router would.
 */
function StatefulHarness({
  initialSearch,
}: {
  initialSearch: Record<string, unknown>;
}) {
  const [search, setSearch] = useState<Record<string, unknown>>(initialSearch);
  return (
    <QueryClientProvider client={new QueryClient()}>
      <LocaleProvider>
        <LoadbalanceEventsFragment
          search={search}
          onSearchChange={(patch) =>
            setSearch((current) => ({ ...current, ...patch }))
          }
        />
      </LocaleProvider>
    </QueryClientProvider>
  );
}

describe("loadbalance events cursor pagination (C1)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.costingGet.mockResolvedValue({ timezone_preference: "UTC" });
    mocks.issueContext.mockResolvedValue({ query_context: "signed-context" });
  });

  it("issues a real read of the target page when 下一页 is clicked", async () => {
    const user = userEvent.setup();
    let call = 0;
    mocks.listEvents.mockImplementation(async () => {
      call += 1;
      return call === 1
        ? eventsPage([eventItem("e1")], "cursor-2")
        : eventsPage([eventItem("e2")], null);
    });

    render(<StatefulHarness initialSearch={{}} />);
    await waitFor(() =>
      expect(screen.getByTestId("event-row-e1")).toBeInTheDocument(),
    );
    expect(mocks.listEvents).toHaveBeenLastCalledWith(
      expect.objectContaining({ cursor: undefined }),
    );

    await user.click(screen.getByRole("button", { name: "下一页" }));

    await waitFor(() =>
      expect(mocks.listEvents).toHaveBeenLastCalledWith(
        expect.objectContaining({ cursor: "cursor-2" }),
      ),
    );
    // The new page commits atomically: the old rows never masquerade as the
    // new cursor's cohort.
    await waitFor(() =>
      expect(screen.getByTestId("event-row-e2")).toBeInTheDocument(),
    );
    expect(screen.queryByTestId("event-row-e1")).not.toBeInTheDocument();
  });

  it("deep-links straight to the cursor's page instead of resetting to page one", async () => {
    mocks.listEvents.mockResolvedValue(eventsPage([eventItem("e9")], null));

    render(<StatefulHarness initialSearch={{ event_cursor: "deep-cursor" }} />);

    await waitFor(() =>
      expect(mocks.listEvents).toHaveBeenCalledWith(
        expect.objectContaining({ cursor: "deep-cursor" }),
      ),
    );
    await waitFor(() =>
      expect(screen.getByTestId("event-row-e9")).toBeInTheDocument(),
    );
    // The deep-linked cursor was honored once — never cleared back to page one
    // and never re-read.
    expect(mocks.listEvents).toHaveBeenCalledTimes(1);
    expect(mocks.issueContext).toHaveBeenCalledTimes(1);
  });
});
