import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { LoadbalanceEventsTab } from "../LoadbalanceEventsTab";

const mockUseModelLoadbalanceEvents = vi.hoisted(() => vi.fn());

vi.mock("@/context/ProfileContext", () => ({
  useProfileContext: () => ({
    revision: 3,
    selectedProfile: { id: 1 },
  }),
}));

vi.mock("../useModelLoadbalanceEvents", () => ({
  useModelLoadbalanceEvents: mockUseModelLoadbalanceEvents,
}));

vi.mock("@/components/loadbalance/LoadbalanceEventDetailSheet", () => ({
  LoadbalanceEventDetailSheet: () => null,
}));

vi.mock("@/components/loadbalance/LoadbalanceEventsTable", () => ({
  LoadbalanceEventsTable: () => <div>mock-events-table</div>,
}));

function renderSubject() {
  return render(
    <LocaleProvider>
      <LoadbalanceEventsTab modelId="gpt-5.4" />
    </LocaleProvider>,
  );
}

describe("LoadbalanceEventsTab", () => {
  it("renders refresh as a square icon-only outline button with centered icon spacing", () => {
    mockUseModelLoadbalanceEvents.mockReturnValue({
      events: [],
      loading: false,
      total: 0,
      offset: 0,
      limit: 25,
      refresh: vi.fn(),
      goToPreviousPage: vi.fn(),
      goToNextPage: vi.fn(),
    });

    renderSubject();

    const refreshButton = screen.getByRole("button", { name: "Refresh loadbalance events" });
    const refreshIcon = refreshButton.querySelector("svg");

    expect(refreshButton).toHaveAttribute("data-variant", "outline");
    expect(refreshButton).toHaveAttribute("data-size", "icon");
    expect(refreshButton).not.toHaveTextContent(/\S/);
    expect(refreshIcon).toBeTruthy();
    expect(refreshIcon).not.toHaveClass("mr-1.5");
  });
});
