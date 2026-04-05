import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { EndpointsPage } from "@/pages/EndpointsPage";

const mockUseEndpointsPageData = vi.hoisted(() => vi.fn());

vi.mock("../useEndpointsPageData", () => ({
  useEndpointsPageData: () => mockUseEndpointsPageData(),
}));

function buildEndpoint(id: number, name: string) {
  return {
    id,
    profile_id: 1,
    name,
    base_url: `https://example-${id}.com`,
    has_api_key: true,
    masked_api_key: "sk-***",
    position: id,
    created_at: "",
    updated_at: "",
  };
}

describe("EndpointsPage shell i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");

    const endpoints = [
      buildEndpoint(10, "主端点"),
      buildEndpoint(20, "备用端点"),
      buildEndpoint(30, "沙盒端点"),
      buildEndpoint(40, "归档端点"),
    ];

    mockUseEndpointsPageData.mockReset();
    mockUseEndpointsPageData.mockReturnValue({
      isLoading: false,
      endpoints,
      filteredEndpoints: endpoints,
      visibleEndpointIds: endpoints.map((endpoint) => endpoint.id),
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
      formatTime: vi.fn(),
      isCreateOpen: false,
      handleCreate: vi.fn(),
      editingEndpoint: null,
      handleUpdate: vi.fn(),
      deleteTarget: null,
      isDeletingEndpoint: false,
      handleDeleteDialogOpenChange: vi.fn(),
      handleDelete: vi.fn(),
    });
  });

  it("renders localized page shell copy", () => {
    render(
      <LocaleProvider>
        <EndpointsPage />
      </LocaleProvider>,
    );

    expect(screen.getByText("端点")).toBeInTheDocument();
    expect(screen.getByText("管理按配置档案划分的 API 凭证和模型路由目标。")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "新增端点" }).length).toBeGreaterThan(0);
    expect(screen.getByPlaceholderText("搜索端点...")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "全部" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "使用中" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "未使用" })).toBeInTheDocument();
    expect(screen.queryByText("已配置端点")).not.toBeInTheDocument();
  });
});
