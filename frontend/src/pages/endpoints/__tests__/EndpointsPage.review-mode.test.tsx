import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { EndpointsPage } from "@/pages/EndpointsPage";

const mockUseEndpointsPageData = vi.hoisted(() => vi.fn());

vi.mock("../useEndpointsPageData", () => ({
  useEndpointsPageData: () => mockUseEndpointsPageData(),
}));

vi.mock("../EndpointCard", () => ({
  SortableEndpointCard: ({
    endpoint,
    reorderDisabled,
  }: {
    endpoint: { name: string };
    reorderDisabled?: boolean;
  }) => (
    <button type="button" aria-label={`Drag handle for ${endpoint.name}`} disabled={reorderDisabled}>
      {endpoint.name}
    </button>
  ),
  EndpointCardView: ({ endpoint }: { endpoint: { name: string } }) => <div>{endpoint.name}</div>,
}));

vi.mock("../EndpointDialog", () => ({
  EndpointDialog: () => null,
}));

vi.mock("../DeleteEndpointDialog", () => ({
  DeleteEndpointDialog: () => null,
}));

function buildEndpoint(overrides: Record<string, unknown> = {}) {
  return {
    id: 10,
    profile_id: 1,
    name: "Primary",
    base_url: "https://api.openai.com",
    has_api_key: true,
    masked_api_key: "sk-***",
    position: 0,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

function createPageData(overrides: Record<string, unknown> = {}) {
  const endpoints = (overrides.endpoints as ReturnType<typeof buildEndpoint>[]) ?? [buildEndpoint()];
  const filteredEndpoints =
    (overrides.filteredEndpoints as ReturnType<typeof buildEndpoint>[]) ?? endpoints;
  const visibleEndpointIds =
    (overrides.visibleEndpointIds as number[]) ?? filteredEndpoints.map((endpoint) => endpoint.id);

  return {
    isLoading: false,
    endpoints,
    filteredEndpoints,
    visibleEndpointIds,
    searchQuery: "",
    setSearchQuery: vi.fn(),
    reviewFilter: "all",
    setReviewFilter: vi.fn(),
    hasActiveReviewFilters: false,
    totalAttachedModels: 0,
    uniqueAttachedModels: 0,
    endpointsInUse: 0,
    setIsCreateOpen: vi.fn(),
    sensors: [],
    collisionDetection: vi.fn(),
    handleDragStart: vi.fn(),
    handleDragCancel: vi.fn(),
    handleDragEnd: vi.fn(),
    endpointIds: endpoints.map((endpoint) => endpoint.id),
    endpointModels: Object.fromEntries(endpoints.map((endpoint) => [endpoint.id, []])),
    duplicatingEndpointId: null,
    canReorder: true,
    handleDuplicateEndpoint: vi.fn(),
    setEditingEndpoint: vi.fn(),
    setDeleteTarget: vi.fn(),
    activeDragEndpoint: null,
    formatTime: vi.fn(() => "formatted"),
    isCreateOpen: false,
    handleCreate: vi.fn(),
    editingEndpoint: null,
    handleUpdate: vi.fn(),
    deleteTarget: null,
    isDeletingEndpoint: false,
    handleDeleteDialogOpenChange: vi.fn(),
    handleDelete: vi.fn(),
    ...overrides,
  };
}

describe("EndpointsPage review mode", () => {
  const configuredSummaryLabel = ["Configured", "Endpoints"].join(" ");
  const attachedSummaryLabel = ["Attached", "Models"].join(" ");

  beforeEach(() => {
    localStorage.clear();
    mockUseEndpointsPageData.mockReset();
    mockUseEndpointsPageData.mockReturnValue(createPageData());
  });

  it("renders the review toolbar, removes summary cards, and only shows filtered endpoints", () => {
    const endpoints = [
      buildEndpoint({ id: 10, name: "Primary", position: 0 }),
      buildEndpoint({ id: 20, name: "Backup", position: 1 }),
      buildEndpoint({ id: 30, name: "Sandbox", position: 2 }),
      buildEndpoint({ id: 40, name: "Archive", position: 3 }),
    ];

    mockUseEndpointsPageData.mockReturnValue(
      createPageData({
        endpoints,
        filteredEndpoints: [endpoints[1], endpoints[3]],
        visibleEndpointIds: [20, 40],
      })
    );

    render(
      <LocaleProvider>
        <EndpointsPage />
      </LocaleProvider>
    );

    expect(screen.queryByText(configuredSummaryLabel)).not.toBeInTheDocument();
    expect(screen.queryByText(attachedSummaryLabel)).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText("Search endpoints...")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "All" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "In Use" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Unused" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Drag handle for Backup" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Drag handle for Archive" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Drag handle for Primary" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Drag handle for Sandbox" })).not.toBeInTheDocument();
  });

  it("keeps the review toolbar visible while a filter is active on a short list", () => {
    mockUseEndpointsPageData.mockReturnValue(
      createPageData({
        endpoints: [buildEndpoint({ id: 20, name: "Backup" })],
        filteredEndpoints: [buildEndpoint({ id: 20, name: "Backup" })],
        visibleEndpointIds: [20],
        searchQuery: "backup",
        hasActiveReviewFilters: true,
      })
    );

    render(
      <LocaleProvider>
        <EndpointsPage />
      </LocaleProvider>
    );

    expect(screen.getByPlaceholderText("Search endpoints...")).toBeInTheDocument();
    expect(screen.getByDisplayValue("backup")).toBeInTheDocument();
  });

  it("shows filtered-empty copy instead of the true-empty state and avoids an extra empty-state add CTA", () => {
    mockUseEndpointsPageData.mockReturnValue(
      createPageData({
        endpoints: [buildEndpoint({ id: 10, name: "Primary" })],
        filteredEndpoints: [],
        visibleEndpointIds: [],
        searchQuery: "anthropic",
        hasActiveReviewFilters: true,
        canReorder: false,
      })
    );

    render(
      <LocaleProvider>
        <EndpointsPage />
      </LocaleProvider>
    );

    expect(screen.getByText("No endpoints match your filters")).toBeInTheDocument();
    expect(screen.getByText("Try a different search or clear the review filters.")).toBeInTheDocument();
    expect(screen.queryByText("No endpoints configured")).not.toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Add Endpoint" })).toHaveLength(1);
  });

  it("shows the reorder-disabled hint and disables the drag handle while review filters are active", () => {
    mockUseEndpointsPageData.mockReturnValue(
      createPageData({
        endpoints: [buildEndpoint({ id: 10, name: "Primary" }), buildEndpoint({ id: 20, name: "Backup", position: 1 })],
        filteredEndpoints: [buildEndpoint({ id: 10, name: "Primary" })],
        visibleEndpointIds: [10],
        searchQuery: "primary",
        hasActiveReviewFilters: true,
        canReorder: false,
      })
    );

    render(
      <LocaleProvider>
        <EndpointsPage />
      </LocaleProvider>
    );

    expect(screen.getByText("Reordering is disabled while review filters are active.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Drag handle for Primary" })).toBeDisabled();
  });
});
