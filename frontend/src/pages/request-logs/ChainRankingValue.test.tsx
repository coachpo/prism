import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { ChainRankingValue } from "./ChainRankingValue";

describe("chain ranking evidence presentation", () => {
  it("keeps trusted zero and its historical currency group", () => {
    render(<LocaleProvider><ChainRankingValue ranking={{ metric: "total_cost_user_currency_micros", value: 0, group: "e.7:EUR", state: "ranked" }} /></LocaleProvider>);
    expect(screen.getByText("EUR 0")).toBeInTheDocument();
    expect(screen.getByText("币种版本 7")).toBeInTheDocument();
  });
  it("does not turn unknown-currency cost into a comparable amount", () => {
    render(<LocaleProvider><ChainRankingValue ranking={{ metric: "total_cost_user_currency_micros", value: null, group: "", state: "unknown_currency" }} /></LocaleProvider>);
    expect(screen.getAllByText("无法排名：历史报告币种未知").some((element) => !element.classList.contains("sr-only"))).toBe(true);
    expect(screen.queryByText(/USD|EUR|\$0/)).not.toBeInTheDocument();
  });
});
