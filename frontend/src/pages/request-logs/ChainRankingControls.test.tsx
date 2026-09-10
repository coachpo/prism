import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { ChainRankingControls } from "./ChainRankingControls";
import { parsePageSearch } from "./queryParams";

it("renders backend whole-cohort rankability and missing reasons independently of the current page", () => {
  render(<LocaleProvider><ChainRankingControls actions={{ state: parsePageSearch({ sort_by: "elapsed_ms" }), setSort: vi.fn() }} ranking={{ metric: "elapsed_ms", rankable_ingress_count: 240, unrankable_ingress_count: 3, unrankable_reasons: { missing_elapsed: 2, missing_finalized: 1, unknown_currency: 0, untrusted_cost: 0 } }} /></LocaleProvider>);
  expect(screen.getByText("全筛选范围：可排名 240 个入口 · 无法排名 3 个入口")).toBeInTheDocument();
  expect(screen.getByText("缺少有效入口起止时间：2")).toBeInTheDocument();
  expect(screen.getByText("缺少最终化记录：1")).toBeInTheDocument();
});
