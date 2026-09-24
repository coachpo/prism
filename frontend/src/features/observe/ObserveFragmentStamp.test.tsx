import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import { ObserveFragmentStamp } from "./ObserveFragmentStamp";
import { ObservePageGeneratedAtProvider } from "./observeStampReference";

vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({
    format: (value: string) => value,
    timezone: "UTC",
  }),
}));

const PAGE_TIME = "2026-09-24T10:00:00Z";

/** `page: null` renders without a freshness bar reference at all. */
function renderStamp(
  props: { generatedAt: string; stale?: boolean },
  page: { generatedAt: string | null } | null = { generatedAt: PAGE_TIME },
) {
  const stamp = <ObserveFragmentStamp {...props} />;
  render(
    <LocaleProvider>
      {page === null ? (
        stamp
      ) : (
        <ObservePageGeneratedAtProvider value={page.generatedAt}>
          {stamp}
        </ObservePageGeneratedAtProvider>
      )}
    </LocaleProvider>,
  );
}

describe("ObserveFragmentStamp", () => {
  it("leaves out a stamp read in the same refresh as the freshness bar", () => {
    renderStamp({ generatedAt: "2026-09-24T10:00:02Z" });
    expect(screen.queryByText(/数据更新时间/)).not.toBeInTheDocument();
  });

  it("keeps a stamp read at another time than the freshness bar", () => {
    renderStamp({ generatedAt: "2026-09-24T10:05:00Z" });
    expect(screen.getByText(/数据更新时间/)).toHaveTextContent(
      "2026-09-24T10:05:00Z",
    );
  });

  it("keeps a stale fragment's stamp even when its time agrees", () => {
    renderStamp({ generatedAt: "2026-09-24T10:00:02Z", stale: true });
    expect(screen.getByText(/数据更新时间/)).toBeInTheDocument();
  });

  it("keeps every stamp on a page without a freshness bar", () => {
    renderStamp({ generatedAt: PAGE_TIME }, null);
    expect(screen.getByText(/数据更新时间/)).toBeInTheDocument();
  });

  it("keeps the stamp while the freshness bar has no time yet", () => {
    renderStamp({ generatedAt: PAGE_TIME }, { generatedAt: null });
    expect(screen.getByText(/数据更新时间/)).toBeInTheDocument();
  });
});
