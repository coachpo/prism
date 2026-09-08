import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { OperationalTablePagination } from "./paginationControls";

it("locks every page control while a remote page loads and restores navigation afterward", async () => {
  const navigate = vi.fn();
  const props = {
    currentPageIndex: 1, startIndex: 10, endIndex: 20, totalRows: 30,
    formatNumber: String, hasNextPage: true, hasPreviousPage: true,
    nextLabel: "下一页", previousLabel: "上一页", zeroLabel: "无数据",
    resultsLabel: () => "11–20", pageLabel: (page: string) => `第 ${page} 页`,
    onNextPage: navigate, onPreviousPage: navigate, onGoToPage: navigate,
    pageSize: { ariaLabel: "每页条数", value: 10, options: [10, 20], onChange: navigate },
    loadingLabel: "正在加载目标页…",
  };
  const { rerender } = render(<OperationalTablePagination {...props} pending />);
  expect(screen.getByRole("status")).toBeVisible();
  for (const button of screen.getAllByRole("button")) expect(button).toBeDisabled();
  expect(screen.getByRole("combobox")).toBeDisabled();
  rerender(<OperationalTablePagination {...props} pending={false} />);
  await userEvent.click(screen.getByRole("button", { name: "下一页" }));
  expect(navigate).toHaveBeenCalledOnce();
});
