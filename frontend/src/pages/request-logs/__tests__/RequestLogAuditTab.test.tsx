import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { AuditLogDetail } from "@/lib/types";
import { RequestLogAuditTab } from "../detail/RequestLogAuditTab";

const baseAudit: AuditLogDetail = {
  id: 9,
  request_log_id: 42,
  profile_id: 7,
  vendor_id: 1,
  model_id: "gpt-5.4",
  endpoint_id: 12,
  connection_id: 34,
  endpoint_base_url: "https://api.example.com/v1",
  endpoint_description: "Primary endpoint",
  request_method: "POST",
  request_url: "https://api.example.com/v1/chat/completions",
  request_headers: 'x-request-id: req_123\ncontent-type: application/json',
  request_body: '{"messages":[]}',
  response_status: 200,
  response_headers: '{"content-type":"application/json"}',
  response_body: '{"id":"resp_1"}',
  is_stream: false,
  duration_ms: 450,
  created_at: "2026-03-16T00:00:00.000Z",
};

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
    expect(screen.getByText("多次尝试后仍无法加载审计详情。")).toBeInTheDocument();
  });

  it("copies the exact visible request-headers block text", async () => {
    const writeTextMock = vi.fn<Clipboard["writeText"]>().mockResolvedValue(undefined);
    const originalClipboard = navigator.clipboard;
    const originalExecCommand = document.execCommand;

    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: writeTextMock },
    });

    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: vi.fn(() => true),
    });

    try {
      renderWithLocale(
        <RequestLogAuditTab
          audits={[baseAudit]}
          loading={false}
          error={null}
          formatTimestamp={(iso) => iso}
        />,
      );

      const visibleHeadersText = screen.getAllByText(
        (_, element) => element?.tagName === "PRE" && element.textContent === baseAudit.request_headers,
      )[0]?.textContent;

      fireEvent.click(screen.getAllByRole("button", { name: /copy/i })[0]!);

      await waitFor(() => {
        expect(writeTextMock).toHaveBeenCalledWith(visibleHeadersText);
      });
    } finally {
      Object.defineProperty(navigator, "clipboard", {
        configurable: true,
        value: originalClipboard,
      });

      Object.defineProperty(document, "execCommand", {
        configurable: true,
        value: originalExecCommand,
      });
    }
  });
});
