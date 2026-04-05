import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { UsageStatisticsPageSkeleton } from "../UsageStatisticsPageSkeleton";

describe("UsageStatisticsPageSkeleton", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders placeholder accessibility copy from the locale catalog", () => {
    render(
      <LocaleProvider>
        <UsageStatisticsPageSkeleton />
      </LocaleProvider>,
    );

    expect(screen.getByRole("status", { name: "用量统计页面占位中" })).toBeInTheDocument();
    expect(screen.getByText("用量统计页面占位中")).toBeInTheDocument();
  });
});
