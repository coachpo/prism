import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { RequestLogsPage } from "@/pages/RequestLogsPage";

const mockNavigate = vi.hoisted(() => vi.fn());
const mockUseRequestLogDetail = vi.hoisted(() => vi.fn());
const mockUseRequestLogPageState = vi.hoisted(() => vi.fn());
const mockUseRequestLogsPageData = vi.hoisted(() => vi.fn());

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

vi.mock("@/context/ProfileContext", () => ({
  useProfileContext: () => ({
    revision: 1,
    selectedProfileId: 7,
  }),
}));

vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({
    format: (isoString: string) => `formatted:${isoString}`,
  }),
}));

vi.mock("../useRequestLogPageState", () => ({
  useRequestLogPageState: () => mockUseRequestLogPageState(),
}));

vi.mock("../useRequestLogsPageData", () => ({
  useRequestLogsPageData: () => mockUseRequestLogsPageData(),
}));

vi.mock("../useRequestLogDetail", () => ({
  useRequestLogDetail: () => mockUseRequestLogDetail(),
}));

vi.mock("../RequestFocusBanner", () => ({
  RequestFocusBanner: () => <div>request-focus-banner</div>,
}));

vi.mock("../FiltersBar", () => ({
  FiltersBar: () => <div>filters-bar</div>,
}));

vi.mock("../RequestLogsTable", () => ({
  RequestLogsTable: () => <div>request-logs-table</div>,
}));

vi.mock("../RequestLogDetailSheet", () => ({
  RequestLogDetailSheet: () => null,
}));

describe("RequestLogsPage shell i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
    mockNavigate.mockReset();
    mockUseRequestLogDetail.mockReset();
    mockUseRequestLogPageState.mockReset();
    mockUseRequestLogsPageData.mockReset();

    mockUseRequestLogPageState.mockReturnValue({
      state: {
        ingress_request_id: "",
        model_id: "",
        api_family: "",
        endpoint_id: "",
        time_range: "24h",
        status_family: "all",
        search: "",
        outcome_filter: "all",
        stream_filter: "all",
        latency_bucket: "all",
        token_min: "",
        token_max: "",
        triage: false,
        request_id: "404",
        detail_tab: "overview",
        limit: 25,
        offset: 0,
      },
      isExactMode: true,
      clearRequest: vi.fn(),
      setDetailTab: vi.fn(),
      setLimit: vi.fn(),
      goToNextPage: vi.fn(),
      goToPreviousPage: vi.fn(),
    });

    mockUseRequestLogsPageData.mockReturnValue({
      items: [],
      total: 0,
      loading: false,
      error: null,
      filterOptions: { apiFamilies: [], endpoints: [], models: [] },
      filterOptionsLoaded: true,
      refresh: vi.fn(),
    });

    mockUseRequestLogDetail.mockReturnValue({
      request: null,
      loading: false,
      error: null,
      notFound: true,
      refresh: vi.fn(),
    });
  });

  it("renders localized page shell copy in exact-request mode", () => {
    render(
      <LocaleProvider>
        <RequestLogsPage />
      </LocaleProvider>,
    );

    expect(screen.getByRole("heading", { name: "请求日志" })).toBeInTheDocument();
    expect(screen.getByText("浏览并调查代理请求")).toBeInTheDocument();
    expect(screen.getByText("未找到请求")).toBeInTheDocument();
    expect(screen.getByText("找不到请求 #404。它可能已被删除，或者你无权访问它。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "返回请求列表" })).toBeInTheDocument();
  });
});
