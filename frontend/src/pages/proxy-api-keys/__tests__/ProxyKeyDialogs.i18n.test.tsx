import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { EditProxyKeyDialog } from "../EditProxyKeyDialog";

describe("proxy key dialogs i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
    vi.stubGlobal(
      "ResizeObserver",
      class ResizeObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
  });

  it("renders localized edit dialog copy", () => {
    render(
      <LocaleProvider>
        <EditProxyKeyDialog
          open={true}
          proxyKeyActive={true}
          proxyKeyName="Primary runtime key"
          proxyKeyNotes="notes"
          saving={false}
          onOpenChange={vi.fn()}
          onSubmit={vi.fn()}
          setProxyKeyActive={vi.fn()}
          setProxyKeyName={vi.fn()}
          setProxyKeyNotes={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("编辑代理 API 密钥")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存" })).toBeInTheDocument();
  });

  it("exposes stable form names for proxy key edit fields", () => {
    render(
      <LocaleProvider>
        <EditProxyKeyDialog
          open={true}
          proxyKeyActive={true}
          proxyKeyName="Primary runtime key"
          proxyKeyNotes="notes"
          saving={false}
          onOpenChange={vi.fn()}
          onSubmit={vi.fn()}
          setProxyKeyActive={vi.fn()}
          setProxyKeyName={vi.fn()}
          setProxyKeyNotes={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByDisplayValue("Primary runtime key")).toHaveAttribute("name", "proxy-key-name");
    expect(screen.getByDisplayValue("notes")).toHaveAttribute("name", "proxy-key-notes");
  });
});
