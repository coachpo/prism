import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { RequestLogAuditTab } from "../detail/RequestLogAuditTab";

function renderWithLocale(ui: React.ReactElement) {
  return render(<LocaleProvider>{ui}</LocaleProvider>);
}

describe("RequestLogAuditTab", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("renders localized capture-unavailable copy in English", () => {
    renderWithLocale(
      <RequestLogAuditTab
        audits={[]}
        loading={false}
        error="capture_unavailable"
        formatTimestamp={(iso) => iso}
      />,
    );

    expect(screen.getByText("Audit capture unavailable")).toBeInTheDocument();
    expect(screen.getByText("Audit logging may be disabled for this vendor.")).toBeInTheDocument();
  });

  it("renders localized load-failed copy in Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderWithLocale(
      <RequestLogAuditTab
        audits={[]}
        loading={false}
        error="load_failed"
        formatTimestamp={(iso) => iso}
      />,
    );

    expect(screen.getByText("审计详情加载失败")).toBeInTheDocument();
    expect(screen.getByText("多次尝试后仍无法加载审计详情。")) .toBeInTheDocument();
  });
});
