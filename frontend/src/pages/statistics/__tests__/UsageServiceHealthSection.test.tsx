import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { clearUserTimezonePreference } from "@/lib/timezone";
import type { UsageServiceHealth, UsageServiceHealthCell } from "@/lib/types";
import { UsageServiceHealthSection } from "@/pages/statistics/sections/UsageServiceHealthSection";
import { installLocalStorageMock } from "./storage";

const api = vi.hoisted(() => ({
  settings: {
    timezone: {
      get: vi.fn(),
    },
  },
}));

const profileState = vi.hoisted(() => ({
  revision: 1,
  selectedProfileId: 7,
}));

vi.mock("@/lib/api", () => ({ api }));
vi.mock("@/context/ProfileContext", () => ({
  useProfileContext: () => profileState,
}));

const OriginalDateTimeFormat = Intl.DateTimeFormat;
const HEATMAP_ROW_COUNT = 12;
const HEATMAP_COLUMN_COUNT = 56;
const HEATMAP_CELL_COUNT = HEATMAP_ROW_COUNT * HEATMAP_COLUMN_COUNT;
const HEATMAP_INTERVAL_MINUTES = 15;

class ResizeObserverMock {
  disconnect() {}
  observe() {}
  unobserve() {}
}

function createOrderedCells({
  count = HEATMAP_CELL_COUNT,
  intervalMinutes = HEATMAP_INTERVAL_MINUTES,
  overrides = {},
  startAt = "2026-03-20T12:00:00Z",
}: {
  count?: number;
  intervalMinutes?: number;
  overrides?: Partial<Record<number, Partial<UsageServiceHealthCell>>>;
  startAt?: string;
}): UsageServiceHealthCell[] {
  const startAtMs = Date.parse(startAt);

  return Array.from({ length: count }, (_, index) => ({
    availability_percentage: null,
    bucket_start: new Date(startAtMs + index * intervalMinutes * 60_000)
      .toISOString()
      .replace(".000Z", "Z"),
    failed_count: 0,
    request_count: 0,
    status: "empty",
    success_count: 0,
    ...overrides[index],
  }));
}

function createServiceHealth(overrides: Partial<UsageServiceHealth> = {}): UsageServiceHealth {
  return {
    availability_percentage: 64.3,
    cells: createOrderedCells({
      overrides: {
        0: {
          availability_percentage: 75,
          failed_count: 3,
          request_count: 12,
          status: "degraded",
          success_count: 9,
        },
        55: {
          availability_percentage: 0,
          failed_count: 4,
          request_count: 4,
          status: "down",
          success_count: 0,
        },
        56: {
          availability_percentage: 100,
          failed_count: 0,
          request_count: 8,
          status: "ok",
          success_count: 8,
        },
        659: {
          availability_percentage: 0,
          failed_count: 2,
          request_count: 2,
          status: "down",
          success_count: 0,
        },
        670: {
          availability_percentage: 100,
          failed_count: 0,
          request_count: 6,
          status: "ok",
          success_count: 6,
        },
        671: {
          availability_percentage: 25,
          failed_count: 3,
          request_count: 4,
          status: "degraded",
          success_count: 1,
        },
      },
    }),
    failed_count: 10,
    interval_minutes: HEATMAP_INTERVAL_MINUTES,
    request_count: 28,
    success_count: 18,
    ...overrides,
  };
}

function expectStrictMatrixLayout() {
  const grid = screen.getByTestId("usage-health-grid");
  const rows = within(grid).getAllByTestId("usage-health-row");

  expect(rows).toHaveLength(HEATMAP_ROW_COUNT);

  for (const row of rows) {
    expect(within(row).getAllByTestId("usage-health-cell")).toHaveLength(
      HEATMAP_COLUMN_COUNT,
    );
  }

  return rows;
}

function getMatrixCell(rows: HTMLElement[], rowIndex: number, columnIndex: number) {
  return within(rows[rowIndex]).getAllByTestId("usage-health-cell")[columnIndex];
}

function renderSection(serviceHealth: UsageServiceHealth) {
  return render(
    <LocaleProvider>
      <UsageServiceHealthSection serviceHealth={serviceHealth} />
    </LocaleProvider>,
  );
}

async function renderSectionAndWait(serviceHealth: UsageServiceHealth) {
  const view = renderSection(serviceHealth);
  await waitFor(() => {
    expect(api.settings.timezone.get).toHaveBeenCalledTimes(1);
  });
  return view;
}

function stubBrowserTimezone(timeZone: string) {
  Object.defineProperty(Intl, "DateTimeFormat", {
    configurable: true,
    value: function mockDateTimeFormat(
      locale?: string | string[],
      options?: Intl.DateTimeFormatOptions,
    ) {
      return new OriginalDateTimeFormat(locale, {
        ...options,
        timeZone: options?.timeZone ?? timeZone,
      });
    },
    writable: true,
  });
}

describe("UsageServiceHealthSection", () => {
  beforeEach(() => {
    installLocalStorageMock();
    localStorage.clear();
    vi.clearAllMocks();
    clearUserTimezonePreference();
    api.settings.timezone.get.mockResolvedValue({ timezone_preference: "UTC" });
    stubBrowserTimezone("UTC");
    vi.stubGlobal("ResizeObserver", ResizeObserverMock);
    vi.stubGlobal("innerHeight", 800);
    vi.stubGlobal("innerWidth", 1280);
    Object.defineProperty(globalThis, "ResizeObserver", {
      configurable: true,
      value: ResizeObserverMock,
      writable: true,
    });
  });

  afterEach(() => {
    clearUserTimezonePreference();
    Object.defineProperty(Intl, "DateTimeFormat", {
      configurable: true,
      value: OriginalDateTimeFormat,
      writable: true,
    });
    vi.unstubAllGlobals();
  });

  it("renders a strict 12 by 56 service-health matrix in chronological column-major order with a single active tooltip", async () => {
    const { container } = await renderSectionAndWait(createServiceHealth());

    const metricCards = container.querySelectorAll('[data-slot="metric-card"]');
    expect(metricCards).toHaveLength(0);

    expect(screen.getByRole("heading", { name: "Service Health" })).toBeInTheDocument();
    expect(screen.getByTestId("usage-service-health-card")).toBeInTheDocument();
    expect(screen.queryByTestId("usage-health-inline-legend")).not.toBeInTheDocument();
    expect(screen.queryByTestId("usage-health-summary-tiles")).not.toBeInTheDocument();
    expect(screen.queryByTestId("usage-health-scroll-window")).not.toBeInTheDocument();
    expect(screen.queryByTestId("usage-health-time-direction")).not.toBeInTheDocument();

    expect(screen.getByTestId("usage-health-header-meta")).toBeInTheDocument();
    expect(screen.getByTestId("usage-health-window-label")).toHaveTextContent("Last 7 days");
    expect(
      screen
        .getByTestId("usage-service-health-card")
        .querySelector('[data-slot="card-header"]'),
    ).not.toHaveClass("border-b");

    const availabilityBadge = screen.getByTestId("usage-health-availability-badge");
    expect(within(availabilityBadge).getByText("64.3%")).toBeInTheDocument();
    expect(within(availabilityBadge).queryByText("Availability")).not.toBeInTheDocument();
    expect(screen.queryByTestId("usage-health-availability-value")).not.toBeInTheDocument();

    expect(screen.queryByTestId("usage-health-strip")).not.toBeInTheDocument();

    const gridScroll = screen.getByTestId("usage-health-grid-scroll");
    expect(gridScroll).toHaveAttribute("data-slot", "scroll-area");

    const calendar = screen.getByTestId("usage-health-heatmap-calendar");
    expect(calendar).toBeInTheDocument();

    const grid = screen.getByTestId("usage-health-grid");
    expect(grid).toBeInTheDocument();
    expect(grid).toHaveAccessibleName("Service Health");
    expect(grid).not.toHaveAttribute("data-rows");
    expect(calendar).toContainElement(grid);
    expect(grid.parentElement).toHaveClass("flex", "w-full", "justify-center");
    expect(grid.parentElement).not.toHaveClass("min-w-max");
    expect(grid).toHaveClass("flex", "min-w-max", "flex-col");

    const rows = expectStrictMatrixLayout();
    const oldestCell = getMatrixCell(rows, 0, 0);
    const previousCell = getMatrixCell(rows, HEATMAP_ROW_COUNT - 2, HEATMAP_COLUMN_COUNT - 1);
    const leftOfLatestCell = getMatrixCell(
      rows,
      HEATMAP_ROW_COUNT - 1,
      HEATMAP_COLUMN_COUNT - 2,
    );
    const latestCell = getMatrixCell(rows, HEATMAP_ROW_COUNT - 1, HEATMAP_COLUMN_COUNT - 1);

    expect(oldestCell).toHaveAttribute("data-status", "degraded");
    expect(previousCell).toHaveAttribute("data-status", "ok");
    expect(leftOfLatestCell).toHaveAttribute("data-status", "down");
    expect(latestCell).toHaveAttribute("data-status", "degraded");

    const legend = screen.getByTestId("usage-health-legend");
    expect(within(legend).getByTestId("usage-health-legend-oldest")).toHaveTextContent("Oldest");
    expect(within(legend).getByTestId("usage-health-legend-latest")).toHaveTextContent("Latest");
    expect(within(legend).queryByText("Lower availability")).not.toBeInTheDocument();
    expect(within(legend).queryByText("Higher availability")).not.toBeInTheDocument();
    expect(within(legend).queryByText("OK")).not.toBeInTheDocument();
    expect(within(legend).queryByText("Degraded")).not.toBeInTheDocument();
    expect(within(legend).queryByText("Down")).not.toBeInTheDocument();
    expect(within(legend).queryByText("Idle")).not.toBeInTheDocument();
    expect(legend).toHaveClass("mx-auto", "flex", "w-fit", "items-center", "justify-center", "text-center");
    expect(legend).not.toHaveClass("flex-col");
    expect(legend.children).toHaveLength(3);
    expect(legend.children[1]).toHaveClass("flex", "items-center", "justify-center");
    expect(
      within(legend)
        .getAllByTestId("usage-health-legend-swatch")
        .map((item) => item.dataset.level),
    ).toEqual(["0", "1", "2", "3", "4"]);

    const cells = within(grid).getAllByTestId("usage-health-cell");
    expect(cells).toHaveLength(HEATMAP_CELL_COUNT);
    expect(oldestCell).toHaveStyle({ backgroundColor: "rgb(142, 201, 58)" });
    expect(previousCell).toHaveStyle({ backgroundColor: "rgb(34, 197, 94)" });
    expect(leftOfLatestCell).toHaveStyle({ backgroundColor: "rgb(239, 68, 68)" });
    expect(latestCell).toHaveStyle({ backgroundColor: "rgb(250, 204, 21)" });

    expect(oldestCell).toHaveAccessibleName("Mar 20, 12:00 PM Degraded Availability 75.0%");
    expect(previousCell).toHaveAccessibleName("Mar 27, 11:30 AM OK Availability 100.0%");
    expect(leftOfLatestCell).toHaveAccessibleName("Mar 27, 8:45 AM Down Availability 0.0%");
    expect(latestCell).toHaveAccessibleName("Mar 27, 11:45 AM Degraded Availability 25.0%");

    fireEvent.focus(oldestCell);

    const oldestTooltip = await screen.findByTestId("usage-health-tooltip");
    expect(screen.getAllByRole("tooltip")).toHaveLength(1);
    expect(oldestTooltip).toHaveTextContent("Mar 20, 12:00 PM - Mar 20, 12:15 PM");
    expect(oldestTooltip).toHaveTextContent("Degraded");
    expect(oldestTooltip).toHaveTextContent("Availability 75.0%");
    expect(oldestTooltip).toHaveTextContent("Requests 12");
    expect(oldestTooltip).toHaveTextContent("9 successful · 3 failed");
    expect(oldestTooltip).not.toHaveTextContent(/15 minutes/);

    fireEvent.focus(previousCell);

    const previousTooltip = await screen.findByTestId("usage-health-tooltip");
    expect(previousTooltip).toHaveTextContent("Mar 27, 11:30 AM - Mar 27, 11:45 AM");
    expect(previousTooltip).toHaveTextContent("OK");
    expect(previousTooltip).toHaveTextContent("Availability 100.0%");
    expect(previousTooltip).toHaveTextContent("Requests 6");
    expect(previousTooltip).toHaveTextContent("6 successful · 0 failed");

    fireEvent.focus(latestCell);

    const latestTooltip = await screen.findByTestId("usage-health-tooltip");
    expect(latestTooltip).toHaveTextContent("Mar 27, 11:45 AM - Mar 27, 12:00 PM");
    expect(latestTooltip).toHaveTextContent("Degraded");
    expect(latestTooltip).toHaveTextContent("Availability 25.0%");
    expect(latestTooltip).toHaveTextContent("Requests 4");
    expect(latestTooltip).toHaveTextContent("1 successful · 3 failed");
    expect(screen.queryByText(/Mar 20, 12:00 PM/)).not.toBeInTheDocument();
    expect(screen.getAllByRole("tooltip")).toHaveLength(1);
  });

  it("derives the service-health window label from ordered cell count and interval minutes", async () => {
    await renderSectionAndWait(
      createServiceHealth({
        availability_percentage: 100,
        cells: createOrderedCells({
          count: 4,
          intervalMinutes: 720,
          overrides: {
            0: {
              availability_percentage: 100,
              failed_count: 0,
              request_count: 1,
              status: "ok",
              success_count: 1,
            },
          },
          startAt: "2026-03-20T00:00:00Z",
        }),
        failed_count: 0,
        interval_minutes: 720,
        request_count: 1,
        success_count: 1,
      }),
    );

    expect(screen.getByTestId("usage-health-window-label")).toHaveTextContent(
      "Last 2 days",
    );
  });

  it("renders a full idle matrix instead of an empty state when there is no traffic", async () => {
    await renderSectionAndWait(
      createServiceHealth({
        availability_percentage: null,
        cells: [],
        failed_count: 0,
        request_count: 0,
        success_count: 0,
      }),
    );

    expect(screen.queryByText("No data available")).not.toBeInTheDocument();
    const rows = expectStrictMatrixLayout();
    const cells = screen.getAllByTestId("usage-health-cell");
    expect(cells).toHaveLength(HEATMAP_CELL_COUNT);
    expect(rows).toHaveLength(HEATMAP_ROW_COUNT);
    expect(cells.every((cell) => cell.dataset.status === "empty")).toBe(true);
    expect(
      cells.every((cell) => cell.style.backgroundColor === "rgb(203, 213, 225)"),
    ).toBe(true);
  });

  it("keeps null availability distinct from zero request and error totals", async () => {
    await renderSectionAndWait(
      createServiceHealth({
        availability_percentage: null,
        cells: [],
        failed_count: 0,
        request_count: 0,
        success_count: 0,
      }),
    );

    expect(screen.getByTestId("usage-health-availability-badge")).toHaveTextContent("—");
    expect(
      within(screen.getByTestId("usage-health-availability-badge")).queryByText(
        "Availability",
      ),
    ).not.toBeInTheDocument();

    const idleCell = screen.getAllByTestId("usage-health-cell")[0];
    fireEvent.focus(idleCell);

    const tooltip = await screen.findByTestId("usage-health-tooltip");
    expect(tooltip).toHaveTextContent("Availability —");
    expect(tooltip).toHaveTextContent("Requests 0");
    expect(tooltip).toHaveTextContent("0 successful · 0 failed");
  });

  it("localizes service-health header copy and tooltip status labels in zh-CN", async () => {
    localStorage.setItem("prism.locale", "zh-CN");

    await renderSectionAndWait(createServiceHealth());

    expect(screen.getByRole("heading", { name: "服务健康" })).toBeInTheDocument();
    expect(screen.getByTestId("usage-health-window-label")).toHaveTextContent("最近 7 天");
    expect(screen.getByTestId("usage-health-legend-oldest")).toHaveTextContent("最早");
    expect(screen.getByTestId("usage-health-legend-latest")).toHaveTextContent("最新");
    expect(screen.queryByText("正常")).not.toBeInTheDocument();
    expect(screen.queryByText("降级")).not.toBeInTheDocument();
    expect(screen.queryByText("故障")).not.toBeInTheDocument();
    expect(screen.queryByText("空闲")).not.toBeInTheDocument();
    expect(screen.queryByText("较低可用率")).not.toBeInTheDocument();
    expect(screen.queryByText("较高可用率")).not.toBeInTheDocument();

    const degradedCell = screen.getAllByTestId("usage-health-cell")[0];
    fireEvent.focus(degradedCell);

    const tooltip = await screen.findByTestId("usage-health-tooltip");
    expect(tooltip).toHaveTextContent("降级");
    expect(tooltip).toHaveTextContent("3月20日 12:00 - 3月20日 12:15");
    expect(tooltip).not.toHaveTextContent(/15 分钟/);
  });

  it("uses the saved timezone preference for bucket labels and tooltips while keeping the latest bucket at the bottom-right", async () => {
    api.settings.timezone.get.mockResolvedValue({ timezone_preference: "Europe/Helsinki" });
    stubBrowserTimezone("America/Los_Angeles");

    await renderSectionAndWait(
      createServiceHealth({
        availability_percentage: 100,
        cells: createOrderedCells({
          intervalMinutes: HEATMAP_INTERVAL_MINUTES,
          overrides: {
            670: {
              availability_percentage: 100,
              failed_count: 0,
              request_count: 6,
              status: "ok",
              success_count: 6,
            },
            671: {
              availability_percentage: 25,
              failed_count: 3,
              request_count: 4,
              status: "degraded",
              success_count: 1,
            },
          },
          startAt: "2026-03-21T23:00:00Z",
        }),
        failed_count: 3,
        interval_minutes: HEATMAP_INTERVAL_MINUTES,
        request_count: 10,
        success_count: 7,
      }),
    );

    const rows = expectStrictMatrixLayout();
    const previousCell = getMatrixCell(rows, HEATMAP_ROW_COUNT - 2, HEATMAP_COLUMN_COUNT - 1);
    const latestCell = getMatrixCell(rows, HEATMAP_ROW_COUNT - 1, HEATMAP_COLUMN_COUNT - 1);

    expect(previousCell).toHaveAttribute("data-status", "ok");
    expect(latestCell).toHaveAttribute("data-status", "degraded");

    await waitFor(() => {
      expect(previousCell).toHaveAccessibleName("Mar 29, 12:30 AM OK Availability 100.0%");
      expect(latestCell).toHaveAccessibleName("Mar 29, 12:45 AM Degraded Availability 25.0%");
    });

    fireEvent.focus(latestCell);

    const tooltip = await screen.findByTestId("usage-health-tooltip");
    expect(tooltip).toHaveTextContent("Mar 29, 12:45 AM - Mar 29, 1:00 AM");
    expect(tooltip).toHaveTextContent("Degraded");
    expect(tooltip).toHaveTextContent("Availability 25.0%");
    expect(tooltip).toHaveTextContent("Requests 4");
    expect(tooltip).toHaveTextContent("1 successful · 3 failed");
  });

  it("falls back to the browser timezone for bucket labels and tooltips when no saved preference exists", async () => {
    api.settings.timezone.get.mockResolvedValue({ timezone_preference: null });
    stubBrowserTimezone("America/Los_Angeles");

    await renderSectionAndWait(
      createServiceHealth({
        availability_percentage: 100,
        cells: createOrderedCells({
          count: 1,
          intervalMinutes: 1440,
          overrides: {
            0: {
              availability_percentage: 100,
              failed_count: 0,
              request_count: 1,
              status: "ok",
              success_count: 1,
            },
          },
          startAt: "2026-03-21T07:00:00Z",
        }),
        failed_count: 0,
        interval_minutes: 1440,
        request_count: 1,
        success_count: 1,
      }),
    );

    const firstCell = screen.getAllByTestId("usage-health-cell")[0];

    await waitFor(() => {
      expect(firstCell).toHaveAccessibleName("Mar 21, 12:00 AM OK Availability 100.0%");
    });

    fireEvent.focus(firstCell);

    const tooltip = await screen.findByTestId("usage-health-tooltip");
    expect(tooltip).toHaveTextContent("Mar 21, 12:00 AM - Mar 22, 12:00 AM");
  });
});
