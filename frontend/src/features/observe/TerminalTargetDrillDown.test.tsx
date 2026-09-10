import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { ObserveReadCycleProvider } from "./ObserveReadCycleProvider";
import { useObserveReadCycle } from "./observeReadCycleContext";
import { TerminalTargetDrillDown } from "./TerminalTargetDrillDown";

const mocks = vi.hoisted(() => ({ targets: vi.fn(), endpoints: vi.fn() }));
vi.mock("@/lib/api", () => ({ api: { endpoints: { list: mocks.endpoints }, stats: { endpointTerminalTargets: mocks.targets } } }));
vi.mock("@/hooks/useTimezone", () => ({ useTimezone: () => ({ format: (value: string) => value }) }));
vi.mock("@/context/ReportingCurrencyContext", () => ({ useReportingCurrencyContext: () => ({ currencyState: { currency: { symbol: "$" } } }) }));
function Panel() {
  const cycle = useObserveReadCycle();
  return <><button onClick={cycle.advance}>refresh observations</button><TerminalTargetDrillDown preset="24h" scope="final_execution" onScopeChange={() => {}} /></>;
}
it("keeps a collapsed last-good target visibly stale after a failed page refresh", async () => {
  mocks.endpoints.mockResolvedValue([{ id: 1, name: "controlled endpoint", base_url: "http://localhost" }]);
  mocks.targets.mockResolvedValue({ items: [], total: 0, generated_at: "2026-09-10T00:00:00Z", scope: "final_execution", coverage: { complete: true, effective_from_time: "2026-09-09T00:00:00Z", effective_to_time: "2026-09-10T00:00:00Z" } });
  const user = userEvent.setup();
  const { unmount } = render(<LocaleProvider><ObserveReadCycleProvider><Panel /></ObserveReadCycleProvider></LocaleProvider>);
  const endpoint = await screen.findByRole("button", { name: /controlled endpoint/ });
  await user.click(endpoint);
  await screen.findByTestId("tt-empty");
  await user.click(endpoint);
  mocks.targets.mockRejectedValue(new Error("target refresh failed"));
  await user.click(screen.getByRole("button", { name: "refresh observations" }));
  await screen.findByText("target refresh failed");
  expect(screen.queryByTestId("tt-empty")).not.toBeInTheDocument();
  expect(screen.getByText("2026-09-10T00:00:00Z")).toBeInTheDocument();
  const signal = mocks.targets.mock.calls.at(-1)?.[2] as AbortSignal;
  unmount();
  expect(signal.aborted).toBe(true);
});
